package bitcaskmy

import (
	"bitcask-my/common"
	"bitcask-my/data"
	"encoding/binary"
	"sync"
	"sync/atomic"
)

const nonTransactionSeqNo uint64 = 0

var txnFinKey = []byte("txn_fin")

// WriteBatch 原子批量写数据 保证原子性
type WriteBatch struct {
	options       common.WriteBatchOptions
	mu            *sync.Mutex
	db            *DB
	pendingWrites map[string]*data.LogRecord //暂存的写入操作，key 是字符串形式的 key，value 是对应的 LogRecord
}

// NewWriteBatch 创建一个新的 WriteBatch 实例，接受批量写入选项。
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

// Put 批次写入数据，将 key/value 添加到批次中，key 不能为空。
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

// Delete 批次删除数据，将 key 添加到批次中，key 不能为空。
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
	// 通过将 LogRecord 的 Value 设置为 nil 来标记删除操作
	logRecord := &data.LogRecord{
		Key:  key,
		Type: data.LogRecordDeleted,
	}
	wb.pendingWrites[string(key)] = logRecord
	return nil
}

// Commit 提交批次中的所有写入和删除操作，确保原子性。
func (wb *WriteBatch) Commit() error {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	//1.如果没有待处理的写入操作，直接返回
	if len(wb.pendingWrites) == 0 {
		return nil
	}
	if uint(len(wb.pendingWrites)) > wb.options.MaxBatchSize {
		return common.ErrBatchTooLarge
	}
	// 加锁数据库，确保批量操作的原子性
	wb.db.mu.Lock()
	defer wb.db.mu.Unlock()

	//2.获取当前最新的事物序列号
	seqNo := atomic.AddUint64(&wb.db.seqNo, 1)
	postions := make(map[string]*data.LogRecordPos)
	//开始批量写入操作
	for _, logRecord := range wb.pendingWrites {
		logRecordPos, err := wb.db.appendLogRecord(&data.LogRecord{
			Key:   logRecordKeyWithSeq(logRecord.Key, seqNo),
			Value: logRecord.Value,
			Type:  logRecord.Type,
		})
		if err != nil {
			return err
		}
		postions[string(logRecord.Key)] = logRecordPos

	}
	// 3.写一条标识事务完成的数据
	finishLogRecord := &data.LogRecord{
		Key:  logRecordKeyWithSeq(txnFinKey, seqNo),
		Type: data.LogRecordTxnFinished,
	}
	if _, err := wb.db.appendLogRecord(finishLogRecord); err != nil {
		return err
	}
	// 4.根据配置决定是否持久化
	if wb.options.SyncWrite && wb.db.activeFile != nil {
		if err := wb.db.activeFile.Sync(); err != nil {
			return err
		}
	}
	// 5.更新内存索引
	for _, record := range wb.pendingWrites {
		pos := postions[string(record.Key)]
		var oldPos *data.LogRecordPos

		if record.Type == data.LogRecordDeleted {
			oldPos, _ = wb.db.index.Delete(record.Key)
		}
		if record.Type == data.LogRecordNormal {
			oldPos = wb.db.index.Put(record.Key, pos)
		}
		if oldPos != nil {
			//如果 key 已经存在，说明之前的记录已经废弃了，需要更新废弃数据的大小
			wb.db.reclaimSize += int64(oldPos.Size)
		}
	}
	// 6.清空批次中的待处理写入操作
	wb.pendingWrites = make(map[string]*data.LogRecord)
	return nil
}

// key +Seq 编码
func logRecordKeyWithSeq(key []byte, seqNo uint64) []byte {
	seq := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(seq[:], seqNo)
	encKey := make([]byte, len(key)+n)
	copy(encKey[:n], seq[:n])
	copy(encKey[n:], key)
	return encKey
}
