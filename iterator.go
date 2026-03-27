package kvix

import (
	"bytes"
	"kvix/common"
	"kvix/index"
)

// Iterator 对外提供按索引顺序遍历键值对的能力，并支持前缀过滤。
type Iterator struct {
	indexIter index.IndexIterator    // 索引迭代器
	db        *DB                    // 数据库实例
	options   common.IteratorOptions // 迭代器选项
}

// NewIterator 基于当前索引创建一个新的用户迭代器。
func (db *DB) NewIterator(options common.IteratorOptions) *Iterator {
	indexIter := db.index.Iterator(options.Reverse)
	return &Iterator{
		indexIter: indexIter,
		db:        db,
		options:   options,
	}
}

// Rewind 将迭代器重置到起始位置，并跳过不符合前缀条件的项。
func (it *Iterator) Rewind() {
	it.indexIter.Rewind()
	it.skipPrefix()
}

// Seek 将迭代器移动到目标 key 附近，并继续对齐到符合前缀条件的位置。
func (it *Iterator) Seek(key []byte) {
	it.indexIter.Seek(key)
	it.skipPrefix()
}

// Next 将迭代器推进到下一个满足前缀条件的位置。
func (it *Iterator) Next() {
	it.indexIter.Next()
	it.skipPrefix()
}

// Vaild 返回当前迭代器是否仍指向一个有效位置。
func (it *Iterator) Vaild() bool {
	return it.indexIter.Valid()
}

// Key 返回当前迭代器指向的 key。
func (it *Iterator) Key() []byte {
	return it.indexIter.Key()
}

// Value 根据当前位置的日志索引读取对应 value。
func (it *Iterator) Value() ([]byte, error) {
	logRecordPos := it.indexIter.Value()
	it.db.mu.RLock()
	defer it.db.mu.RUnlock()
	return it.db.getValueByPos(logRecordPos)
}

// Close 关闭底层索引迭代器并释放相关资源。
func (it *Iterator) Close() {
	it.indexIter.Close()
}

// skipPrefix 在迭代器当前位置向前推进，直到命中前缀过滤条件或迭代结束。
func (it *Iterator) skipPrefix() {
	prefixLen := len(it.options.Prefix)
	if prefixLen == 0 {
		return
	}

	// 1. 前缀过滤只在用户配置了 Prefix 时生效。
	// 2. 当前 key 不满足前缀条件时持续向后推进。
	// 3. 命中首个满足前缀的 key 或迭代器失效时停止。
	for ; it.indexIter.Valid(); it.indexIter.Next() {
		key := it.indexIter.Key()
		if len(key) >= prefixLen && bytes.Equal(it.options.Prefix, key[:prefixLen]) {
			break
		}
	}
}
