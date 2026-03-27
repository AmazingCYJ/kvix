package kvix

import (
	"encoding/binary"
	"kvix/common"
	"kvix/data"
	"sync"
	"sync/atomic"
)

const nonTransactionSeqNo uint64 = 0

// txnFinKey 是批量写入事务完成标记使用的特殊 key。
var txnFinKey = []byte("txn_fin")

// WriteBatch 封装一次批量写入过程，在提交前暂存写入和删除请求。
type WriteBatch struct {
	options       common.WriteBatchOptions
	mu            *sync.Mutex
	db            *DB
	pendingWrites map[string]*data.LogRecord // 暂存待提交的操作，key 为原始 key 的字符串形式。
}

// NewWriteBatch 创建一个新的批量写入器，并绑定当前数据库实例。
func (db *DB) NewWriteBatch(options common.WriteBatchOptions) *WriteBatch {
	if db.options.IndexType == common.BPlusTreeIndex && !db.seqNoFileExist && !db.isInitial {
		panic("B+树索引必须存在序列号文件")
	}
	return &WriteBatch{
		options:       options,
		mu:            &sync.Mutex{},
		db:            db,
		pendingWrites: make(map[string]*data.LogRecord),
	}
}

// Put 将一次写操作加入批次，真正落盘要等到 Commit 执行。
func (wb *WriteBatch) Put(key, value []byte) error {
	if len(key) == 0 {
		return common.ErrKeyNotFound
	}
	wb.mu.Lock()
	defer wb.mu.Unlock()
	logRecord := &data.LogRecord{
		Key:   key,
		Value: value,
	}
	wb.pendingWrites[string(key)] = logRecord
	return nil
}

// Delete 将一次删除操作加入批次，不存在的 key 会直接返回错误。
func (wb *WriteBatch) Delete(key []byte) error {
	if len(key) == 0 {
		return common.ErrKeyNotFound
	}
	wb.mu.Lock()
	defer wb.mu.Unlock()
	// 数据不存在
	logRecordPos := wb.db.index.Get(key)
	if logRecordPos == nil {
		if wb.pendingWrites[string(key)] != nil {
			delete(wb.pendingWrites, string(key))
		}
		return common.ErrKeyNotFound
	}
	// 通过删除类型记录表达删除语义，真正生效仍依赖 Commit。
	logRecord := &data.LogRecord{
		Key:  key,
		Type: data.LogRecordDeleted,
	}
	wb.pendingWrites[string(key)] = logRecord
	return nil
}

// Commit 以单个事务序列号提交当前批次，保证批次内操作原子生效。
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
			// 如果 key 已经存在，旧位置对应的数据已经失效，需要计入可回收空间。
			wb.db.reclaimSize += int64(oldPos.Size)
		}
	}

	// 6. 清空本次批次缓存，避免已提交的数据在后续重复提交。
	wb.pendingWrites = make(map[string]*data.LogRecord)
	return nil
}

// logRecordKeyWithSeq 将事务序列号编码到 key 前缀，用于事务批次恢复和识别。
func logRecordKeyWithSeq(key []byte, seqNo uint64) []byte {
	seq := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(seq[:], seqNo)
	encKey := make([]byte, len(key)+n)
	copy(encKey[:n], seq[:n])
	copy(encKey[n:], key)
	return encKey
}
