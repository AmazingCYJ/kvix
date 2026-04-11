package kvix

import (
	"kvix/common"
	"kvix/data"
)

// Put 写入或覆盖一个 key 对应的 value。
//
// 写入流程：将逻辑 Put 包装成普通日志记录 → 追加写入活跃数据文件 →
// 更新内存索引到新位置 → 将旧位置计入可回收空间。
//
// 参数:
//   - key: 目标键，不能为空。
//   - value: 要持久化的字节内容，可为空切片。
//
// 返回:
//   - error: 写入和索引更新是否成功。
func (db *DB) Put(key, value []byte) error {
	if len(key) == 0 {
		// 根包约定空 key 视为非法输入，直接拒绝写入。
		return common.ErrKeyNotFound
	}

	// 1. 把一次逻辑 Put 包装成普通日志记录。
	// key 会额外带上"非事务序列号"前缀，保持和批量事务同一套编码格式。
	logRecord := &data.LogRecord{
		Key:   logRecordKeyWithSeq(key, nonTransactionSeqNo),
		Value: value,
		Type:  data.LogRecordNormal,
	}

	// 2. 追加写入日志文件，拿到这条记录真实落盘的位置。
	pos, err := db.appendLogRecordWithLock(logRecord)
	if err != nil {
		return err
	}

	// 3. 用新位置覆盖索引中的旧位置；如果 key 原来存在，旧记录就变成可回收垃圾。
	if oldPos := db.index.Put(key, pos); oldPos != nil {
		db.reclaimSize += int64(oldPos.Size)
	}
	return nil
}

// Delete 为指定 key 追加删除标记，并从内存索引中移除该 key。
//
// 删除不是立即抹掉旧 value，而是追加一条"删除类型"的墓碑记录。
// 旧记录和墓碑记录都会被计入可回收空间，等待后续 merge 清理。
//
// 参数:
//   - key: 要删除的键，不能为空。
//
// 返回:
//   - error: 如果 key 当前不存在，返回 nil（幂等语义）。
func (db *DB) Delete(key []byte) error {
	if len(key) == 0 {
		// 删除和写入保持同一套输入约束，空 key 直接视为非法。
		return common.ErrKeyNotFound
	}
	if pos := db.index.Get(key); pos == nil {
		// 索引里查不到说明这个 key 当前本来就不存在，不需要再写墓碑记录。
		return nil
	}

	// 1. 追加一条"删除类型"的墓碑记录到数据文件。
	logRecord := &data.LogRecord{
		Key:  logRecordKeyWithSeq(key, nonTransactionSeqNo),
		Type: data.LogRecordDeleted,
	}

	// 2. 先把墓碑记录落盘，确保即使进程崩溃，恢复时也能知道这个 key 已被删除。
	pos, err := db.appendLogRecordWithLock(logRecord)
	if err != nil {
		return err
	}

	// 3. 删除记录本身也占用了磁盘空间，后续 merge 可以把它回收掉。
	db.reclaimSize += int64(pos.Size)

	// 4. 再把内存索引里的 key 删掉，并把原来那条旧 value 记录也计入可回收空间。
	oldPos, ok := db.index.Delete(key)
	if ok {
		db.reclaimSize += int64(oldPos.Size)
	}

	return nil
}

// appendLogRecord 把一条日志记录追加到当前活跃数据文件，并返回其磁盘位置。
//
// 该方法不持有锁，调用方需要自行保证并发安全。
//
// 参数:
//   - logRecord: 已经准备好编码的逻辑记录。
//
// 返回:
//   - *data.LogRecordPos: 写入后的物理位置信息。
//   - error: 文件操作失败时返回错误。
func (db *DB) appendLogRecord(logRecord *data.LogRecord) (*data.LogRecordPos, error) {
	// 1. 确保当前存在活跃数据文件；首次写入或活跃文件尚未初始化时必须先打开文件。
	if db.activeFile == nil {
		if err := db.setActiveDataFile(); err != nil {
			return nil, err
		}
	}

	// 2. 先把日志记录编码出来，再依据编码后的大小判断当前活跃文件是否还能容纳这条记录。
	encodedRecord, size := data.EncodeLogRecord(logRecord)

	// 2.1 当前文件写满时，先把旧活跃文件落盘并转入 oldFiles，再切换到新文件继续写。
	if db.activeFile.WriteOff+size > db.options.DataFileSize {
		// 旧活跃文件在转为只读旧文件前先做一次刷盘，减少切文件时的数据丢失窗口。
		if err := db.activeFile.Sync(); err != nil {
			return nil, err
		}

		// oldFiles 保存所有已经封存的数据文件，读路径会通过 fileID 从这里回查旧记录。
		db.oldFiles[db.activeFile.FileID] = db.activeFile
		if err := db.setActiveDataFile(); err != nil {
			return nil, err
		}
	}

	// 3. 记录写入前偏移，索引恢复和后续读取都依赖这个偏移定位到刚写入的数据。
	writeOff := db.activeFile.WriteOff

	// 4. 追加写入编码结果，并根据 SyncWrites/BytesPerSync 决定是否在本次写入后立刻落盘。
	if err := db.activeFile.Write(encodedRecord); err != nil {
		return nil, err
	}

	// 4.1 bytesWrite 记录"自上次显式 Sync 之后累计写了多少字节"。
	db.bytesWrite += uint(size)
	var needSync = db.options.SyncWrites
	if !needSync && db.options.BytesPerSync > 0 && db.bytesWrite >= db.options.BytesPerSync {
		// 当累计写入达到 BytesPerSync 阈值时，触发一次批量刷盘。
		needSync = true
	}

	if needSync {
		if err := db.activeFile.Sync(); err != nil {
			return nil, err
		}
		if db.bytesWrite > 0 {
			// 已经完成刷盘后，把累计字节数归零，开始统计下一轮。
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
	// 活跃文件切换、写偏移推进和 bytesWrite 统计都属于共享可变状态，必须串行化。
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.appendLogRecord(logRecord)
}
