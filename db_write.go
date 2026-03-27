package kvix

import (
	. "kvix/common"
	"kvix/data"
)

// Put 写入或覆盖一个 key 对应的 value。
// 参数 key 是目标键，不能为空；参数 value 是要持久化的字节内容，可为空切片。
// 返回值表示写入和索引更新是否成功；该方法会追加日志记录、更新内存索引并累加废弃空间统计。
func (db *DB) Put(key, value []byte) error {
	if len(key) == 0 {
		return ErrKeyNotFound
	}

	logRecord := &data.LogRecord{
		Key:   logRecordKeyWithSeq(key, nonTransactionSeqNo),
		Value: value,
		Type:  data.LogRecordNormal,
	}
	pos, err := db.appendLogRecordWithLock(logRecord)
	if err != nil {
		return err
	}
	if oldPos := db.index.Put(key, pos); oldPos != nil {
		db.reclaimSize += int64(oldPos.Size)
	}
	return nil
}

// Delete 为指定 key 追加删除标记，并从内存索引中移除该 key。
// 参数 key 是要删除的键，不能为空；如果 key 当前不存在，则直接返回 nil。
// 返回值表示删除记录写入和索引删除是否成功；该方法会增加可回收空间统计。
func (db *DB) Delete(key []byte) error {
	if len(key) == 0 {
		return ErrKeyNotFound
	}
	if pos := db.index.Get(key); pos == nil {
		return nil
	}

	logRecord := &data.LogRecord{
		Key:  logRecordKeyWithSeq(key, nonTransactionSeqNo),
		Type: data.LogRecordDeleted,
	}
	pos, err := db.appendLogRecordWithLock(logRecord)
	if err != nil {
		return err
	}
	db.reclaimSize += int64(pos.Size)

	oldPos, ok := db.index.Delete(key)
	if ok {
		db.reclaimSize += int64(oldPos.Size)
	}

	return nil
}

// appendLogRecord 把一条日志记录追加到当前活跃数据文件，并返回其磁盘位置。
// 参数 logRecord 是已经准备好编码的逻辑记录。
// 返回值为写入后的物理位置信息；该方法会按配置更新活跃文件、写偏移和同步计数。
func (db *DB) appendLogRecord(logRecord *data.LogRecord) (*data.LogRecordPos, error) {
	// 1. 确保当前存在活跃数据文件；首次写入或活跃文件尚未初始化时必须先打开文件。
	if db.activeFile == nil {
		if err := db.setActiveDataFile(); err != nil {
			return nil, err
		}
	}

	// 2. 先把日志记录编码出来，再依据编码后的大小判断当前活跃文件是否还能容纳这条记录。
	encodedStr, size := data.EncodeLogRecord(logRecord)

	// 2.1 当前文件写满时，先把旧活跃文件落盘并转入 oldfiles，再切换到新文件继续写。
	if db.activeFile.WriteOff+size > db.options.DataFileSize {
		if err := db.activeFile.Sync(); err != nil {
			return nil, err
		}

		db.oldfiles[db.activeFile.FileID] = db.activeFile
		if err := db.setActiveDataFile(); err != nil {
			return nil, err
		}
	}

	// 3. 记录写入前偏移，索引恢复和后续读取都依赖这个偏移定位到刚写入的数据。
	writeOff := db.activeFile.WriteOff

	// 4. 追加写入编码结果，并根据 SyncWrites/BytesPerSync 决定是否在本次写入后立刻落盘。
	if err := db.activeFile.Write(encodedStr); err != nil {
		return nil, err
	}
	db.bytesWrite += uint(size)
	var needSync = db.options.SyncWrites
	if !needSync && db.options.BytesPerSync > 0 && db.bytesWrite >= db.options.BytesPerSync {
		needSync = true
	}

	if needSync {
		if err := db.activeFile.Sync(); err != nil {
			return nil, err
		}
		if db.bytesWrite > 0 {
			db.bytesWrite = 0
		}
	}

	// 5. 把文件 ID、偏移和记录大小组合成位置结果，供内存索引和事务逻辑复用。
	pos := &data.LogRecordPos{
		Fid:    db.activeFile.FileID,
		Offset: writeOff,
		Size:   uint32(size),
	}
	return pos, nil
}

// appendLogRecordWithLock 在持有写锁的情况下追加日志记录，避免多个写入并发篡改活跃文件状态。
func (db *DB) appendLogRecordWithLock(logRecord *data.LogRecord) (*data.LogRecordPos, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.appendLogRecord(logRecord)
}
