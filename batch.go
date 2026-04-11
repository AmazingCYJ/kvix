package kvix

import (
	"encoding/binary"
	"kvix/common"
	"kvix/data"
	"sync"
	"sync/atomic"
)

// nonTransactionSeqNo 表示非事务写入使用的固定序列号。
// 普通 Put/Delete 操作不属于任何事务批次，使用 0 作为标识。
const nonTransactionSeqNo uint64 = 0

// txnFinKey 是批量写入事务完成标记使用的特殊 key。
// 恢复阶段读到这个 key 时，说明对应 seqNo 的整批事务已经完整提交。
var txnFinKey = []byte("txn_fin")

// WriteBatch 封装一次批量写入过程，在提交前暂存写入和删除请求。
//
// 使用方式：
//
//	wb := db.NewWriteBatch(common.DefaultWriteBatchOptions)
//	wb.Put([]byte("k1"), []byte("v1"))
//	wb.Put([]byte("k2"), []byte("v2"))
//	wb.Commit()
//
// 批次内的所有操作共享同一个事务序列号，提交时原子生效。
// 恢复阶段只有看到对应的完成标记，这些记录才会被写回索引。
type WriteBatch struct {
	options       common.WriteBatchOptions   // 批量写入配置，包含最大批次大小和刷盘策略。
	mu            *sync.Mutex                // 保护 pendingWrites 的互斥锁。
	db            *DB                        // 绑定的数据库实例。
	pendingWrites map[string]*data.LogRecord // 暂存待提交的操作，key 为原始 key 的字符串形式。
}

// NewWriteBatch 创建一个新的批量写入器，并绑定当前数据库实例。
//
// 参数:
//   - options: 批量写入配置。
//
// 返回:
//   - *WriteBatch: 可用于暂存和提交批量操作的写入器。
func (db *DB) NewWriteBatch(options common.WriteBatchOptions) *WriteBatch {
	if db.options.IndexType == common.BPlusTreeIndex && !db.seqNoFileExist && !db.isInitial {
		// B+Tree 索引在重启恢复时依赖 seq-no 文件延续事务编号；缺失时说明历史状态不完整。
		panic("B+Tree index requires seq-no file to exist")
	}
	return &WriteBatch{
		options:       options,
		mu:            &sync.Mutex{},
		db:            db,
		pendingWrites: make(map[string]*data.LogRecord),
	}
}

// Put 将一次写操作加入批次，真正落盘要等到 Commit 执行。
//
// 参数:
//   - key: 目标键，不能为空。
//   - value: 要写入的值。
//
// 返回:
//   - error: key 为空时返回 ErrKeyNotFound。
func (wb *WriteBatch) Put(key, value []byte) error {
	if len(key) == 0 {
		return common.ErrKeyNotFound
	}
	wb.mu.Lock()
	defer wb.mu.Unlock()

	// 批量写阶段先把用户请求暂存在内存里，真正落盘要等 Commit。
	logRecord := &data.LogRecord{
		Key:   key,
		Value: value,
	}
	// 同一个 batch 里后来的 Put 会覆盖前面同 key 的暂存结果，符合"最后一次操作生效"的直觉。
	wb.pendingWrites[string(key)] = logRecord
	return nil
}

// Delete 将一次删除操作加入批次。
//
// 如果 key 既不在索引中也不在当前批次的暂存表中，返回 nil（幂等语义）。
// 如果 key 只在暂存表中（尚未提交），直接从暂存表移除。
//
// 参数:
//   - key: 要删除的键，不能为空。
//
// 返回:
//   - error: key 为空时返回 ErrKeyNotFound。
func (wb *WriteBatch) Delete(key []byte) error {
	if len(key) == 0 {
		return common.ErrKeyNotFound
	}
	wb.mu.Lock()
	defer wb.mu.Unlock()

	// 1. 检查 key 是否在索引中存在。
	logRecordPos := wb.db.index.Get(key)
	if logRecordPos == nil {
		if wb.pendingWrites[string(key)] != nil {
			// 如果这个 key 只是本批次里暂存过、但尚未真正提交，那么直接从待提交表里删掉即可。
			delete(wb.pendingWrites, string(key))
		}
		// key 不存在时返回 nil，保持与 DB.Delete 一致的幂等语义。
		return nil
	}

	// 2. 通过删除类型记录表达删除语义，真正生效仍依赖 Commit。
	logRecord := &data.LogRecord{
		Key:  key,
		Type: data.LogRecordDeleted,
	}
	wb.pendingWrites[string(key)] = logRecord
	return nil
}

// Commit 以单个事务序列号提交当前批次，保证批次内操作原子生效。
//
// 提交流程：分配事务序列号 → 逐条写入日志 → 追加完成标记 → 可选刷盘 → 更新索引。
//
// 返回:
//   - error: 批次超限、日志写入失败或刷盘失败时返回错误。
func (wb *WriteBatch) Commit() error {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	// 1. 先做批次级校验，避免空提交或超出批次限制。
	if len(wb.pendingWrites) == 0 {
		return nil
	}
	if uint(len(wb.pendingWrites)) > wb.options.MaxBatchSize {
		return common.ErrBatchTooLarge
	}

	// 2. 锁住数据库级写路径，确保日志追加和索引更新对外表现为一次原子提交。
	wb.db.mu.Lock()
	defer wb.db.mu.Unlock()

	// 3. 为当前批次分配新的事务序列号，并记录每条日志写入后的位置信息。
	seqNo := atomic.AddUint64(&wb.db.seqNo, 1)
	positions := make(map[string]*data.LogRecordPos, len(wb.pendingWrites))

	// 3.1 逐条写入批次数据，所有记录都带上同一个事务序列号。
	for _, logRecord := range wb.pendingWrites {
		logRecordPos, err := wb.db.appendLogRecord(&data.LogRecord{
			Key:   logRecordKeyWithSeq(logRecord.Key, seqNo),
			Value: logRecord.Value,
			Type:  logRecord.Type,
		})
		if err != nil {
			return err
		}
		positions[string(logRecord.Key)] = logRecordPos
	}

	// 3.2 追加事务完成标记，恢复或重放时据此判断整批数据是否有效。
	finishLogRecord := &data.LogRecord{
		Key:  logRecordKeyWithSeq(txnFinKey, seqNo),
		Type: data.LogRecordTxnFinished,
	}
	if _, err := wb.db.appendLogRecord(finishLogRecord); err != nil {
		return err
	}

	// 4. 按配置决定是否立刻刷盘，保证批次数据和完成标记一起持久化。
	if wb.options.SyncWrite && wb.db.activeFile != nil {
		if err := wb.db.activeFile.Sync(); err != nil {
			return err
		}
	}

	// 5. 根据日志写入结果更新内存索引，并累计旧记录带来的可回收空间。
	for _, record := range wb.pendingWrites {
		pos := positions[string(record.Key)]
		var oldPos *data.LogRecordPos

		if record.Type == data.LogRecordDeleted {
			oldPos, _ = wb.db.index.Delete(record.Key)
		}
		if record.Type == data.LogRecordNormal {
			oldPos = wb.db.index.Put(record.Key, pos)
		}
		if oldPos != nil {
			wb.db.reclaimSize += int64(oldPos.Size)
		}
	}

	// 6. 清空本次批次缓存，避免已提交的数据在后续重复提交。
	wb.pendingWrites = make(map[string]*data.LogRecord)
	return nil
}

// logRecordKeyWithSeq 将事务序列号编码到 key 前缀，用于事务批次恢复和识别。
//
// 编码格式：[seqNo varint] + [原始 key]
func logRecordKeyWithSeq(key []byte, seqNo uint64) []byte {
	seq := make([]byte, binary.MaxVarintLen64)
	// 先把事务序列号编码成变长整数，尽量节省前缀空间。
	n := binary.PutUvarint(seq[:], seqNo)
	encKey := make([]byte, len(key)+n)
	// 前缀部分保存 seqNo，后半部分保存原始 key。
	copy(encKey[:n], seq[:n])
	copy(encKey[n:], key)
	return encKey
}
