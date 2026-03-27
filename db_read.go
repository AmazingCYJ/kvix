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
		return nil, ErrKeyNotFound
	}
	logRecordPos := db.index.Get(key)
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
		value, err := db.getValueByPos(iter.Value())
		if err != nil {
			return err
		}
		if !fn(iter.Key(), value) {
			break
		}
	}
	return nil
}

// getValueByPos 根据索引位置从对应数据文件读取 value，并统一处理已删除或文件缺失等情况。
func (db *DB) getValueByPos(logRecordPos *data.LogRecordPos) ([]byte, error) {
	if logRecordPos == nil {
		return nil, ErrKeyNotFound
	}

	var dataFile *data.DataFile
	if db.activeFile.FileID == logRecordPos.Fid {
		dataFile = db.activeFile
	} else {
		dataFile = db.oldfiles[logRecordPos.Fid]
	}
	if dataFile == nil {
		return nil, ErrDataFileNotFound
	}

	logRecord, _, err := dataFile.ReadLogRecord(logRecordPos.Offset)
	if err != nil {
		return nil, err
	}
	if logRecord.Type == data.LogRecordDeleted {
		return nil, ErrKeyNotFound
	}
	return logRecord.Value, nil
}
