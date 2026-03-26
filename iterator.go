package bitcaskmy

import (
	"bitcask-my/common"
	"bitcask-my/index"
	"bytes"
)

// Iterator 迭代器 面向用户
type Iterator struct {
	indexIter index.IndexIterator    // 索引迭代器
	db        *DB                    // 数据库实例
	options   common.IteratorOptions // 迭代器选项
}

func (db *DB) NewIterator(options common.IteratorOptions) *Iterator {
	indexIter := db.index.Iterator(options.Reverse)
	return &Iterator{
		indexIter: indexIter,
		db:        db,
		options:   options,
	}
}

// Rewind 将迭代器重置到起始位置。
func (it *Iterator) Rewind() {
	it.indexIter.Rewind()
	it.skipPrefix()
}

// Seek 将迭代器移动到指定 key 的位置。
func (it *Iterator) Seek(key []byte) {
	it.indexIter.Seek(key)
	it.skipPrefix()
}

// Next 将迭代器移动到下一个位置。
func (it *Iterator) Next() {
	it.indexIter.Next()
	it.skipPrefix()
}

// Vaild 检查迭代器当前是否有效。
func (it *Iterator) Vaild() bool {
	return it.indexIter.Valid()
}

// Key 返回当前迭代器位置的 key。
func (it *Iterator) Key() []byte {
	return it.indexIter.Key()
}

// Value 返回当前迭代器位置的 value。
func (it *Iterator) Value() ([]byte, error) {
	logRecordPos := it.indexIter.Value()
	it.db.mu.RLock()
	defer it.db.mu.RUnlock()
	return it.db.getValueByPos(logRecordPos)
}

// Close 关闭迭代器，释放相关资源。
func (it *Iterator) Close() {
	it.indexIter.Close()
}

// skipPrefix 跳过不匹配前缀的 key，直到找到第一个匹配前缀的 key 或者迭代器无效。
func (it *Iterator) skipPrefix() {
	prefixLen := len(it.options.Prefix)
	if prefixLen == 0 {
		return
	}
	for ; it.indexIter.Valid(); it.indexIter.Next() {
		key := it.indexIter.Key()
		if len(key) >= prefixLen && bytes.Equal(it.options.Prefix, key[:prefixLen]) {
			break
		}
	}
}
