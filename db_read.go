package kvix

import (
	. "kvix/common"
	"kvix/data"
)

// Get 根据 key 读取对应的 value。
// 参数 key 是要查询的键，不能为空。
// 返回值为该 key 最新可见的 value；如果 key 不存在、已被删除或底层数据文件缺失，则返回错误。
func (db *DB) Get(key []byte) ([]byte, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if len(key) == 0 {
		// 与写路径一样，空 key 直接视为非法。
		return nil, ErrKeyNotFound
	}
	// 1. 先从索引拿到 key 最新版本在磁盘中的位置。
	logRecordPos := db.index.Get(key)
	// 2. 再根据物理位置跳转到具体数据文件中读出 value。
	return db.getValueByPos(logRecordPos)
}

// ListKeys 按索引迭代顺序返回当前数据库中的全部 key。
// 该方法不接收额外参数，会在持有读锁期间遍历当前内存索引。
// 返回值为 key 切片；如果迭代过程中发生异常，则返回对应错误。
func (db *DB) ListKeys() ([][]byte, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var keys [][]byte
	iter := db.index.Iterator(false)
	defer iter.Close()
	for iter.Rewind(); iter.Valid(); iter.Next() {
		// ListKeys 只关心索引中的逻辑 key，不需要回磁盘取 value。
		keys = append(keys, iter.Key())
	}
	return keys, nil
}

// Fold 遍历当前数据库中的全部 key/value，并把结果交给调用方提供的回调处理。
// 参数 fn 会按索引顺序接收每个 key 和其对应的 value，返回 false 时立即停止遍历。
// 返回值表示遍历过程中读取 value 是否成功；该方法本身不会修改数据库内容。
func (db *DB) Fold(fn func(key, value []byte) bool) error {
	db.mu.RLock()
	defer db.mu.RUnlock()

	iter := db.index.Iterator(false)
	defer iter.Close()
	for iter.Rewind(); iter.Valid(); iter.Next() {
		// Fold 会把当前位置的 LogRecordPos 还原成真实 value，再交给调用方处理。
		value, err := db.getValueByPos(iter.Value())
		if err != nil {
			return err
		}
		if !fn(iter.Key(), value) {
			// 回调返回 false 表示调用方已经拿够了数据，遍历立即终止。
			break
		}
	}
	return nil
}

// getValueByPos 根据索引位置从对应数据文件读取 value，并统一处理已删除或文件缺失等情况。
func (db *DB) getValueByPos(logRecordPos *data.LogRecordPos) ([]byte, error) {
	if logRecordPos == nil {
		// 索引里没有位置，说明这个 key 当前对外就是不存在。
		return nil, ErrKeyNotFound
	}

	var dataFile *data.DataFile
	if db.activeFile.FileID == logRecordPos.Fid {
		// 位置命中当前活跃文件时，直接从 activeFile 读取。
		dataFile = db.activeFile
	} else {
		// 否则去旧文件集合里按 fileID 回查。
		dataFile = db.oldfiles[logRecordPos.Fid]
	}
	if dataFile == nil {
		// 能命中索引但找不到文件对象，说明目录文件状态已经不一致。
		return nil, ErrDataFileNotFound
	}

	// 1. 从对应数据文件的指定偏移读取整条日志记录。
	logRecord, _, err := dataFile.ReadLogRecord(logRecordPos.Offset)
	if err != nil {
		return nil, err
	}
	if logRecord.Type == data.LogRecordDeleted {
		// 即使索引理论上不会指向删除记录，这里仍做一次兜底判断。
		return nil, ErrKeyNotFound
	}
	// 2. 正常记录返回其 value 部分。
	return logRecord.Value, nil
}
