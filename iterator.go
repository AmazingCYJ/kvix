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
	// 先向底层索引申请一个方向确定的索引迭代器。
	indexIter := db.index.Iterator(options.Reverse)
	return &Iterator{
		indexIter: indexIter,
		db:        db,
		options:   options,
	}
}

// Rewind 将迭代器重置到起始位置，并跳过不符合前缀条件的项。
func (it *Iterator) Rewind() {
	// 1. 底层迭代器先回到自己的起点。
	it.indexIter.Rewind()
	// 2. 再把当前位置推进到第一个满足 Prefix 条件的元素。
	it.skipPrefix()
}

// Seek 将迭代器移动到目标 key 附近，并继续对齐到符合前缀条件的位置。
func (it *Iterator) Seek(key []byte) {
	// 1. 先让底层索引做“按有序 key 查找最近位置”。
	it.indexIter.Seek(key)
	// 2. 再做上层前缀过滤，确保暴露给用户的第一项符合 Prefix 约束。
	it.skipPrefix()
}

// Next 将迭代器推进到下一个满足前缀条件的位置。
func (it *Iterator) Next() {
	// 先走到底层的下一个位置，再继续跳过不满足前缀的项。
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
	// 读取 value 时要回到 DB 上下文，因为真正的数据仍然在数据文件里。
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
		// 用户没有设置前缀时，不需要额外过滤。
		return
	}

	// 1. 前缀过滤只在用户配置了 Prefix 时生效。
	// 2. 当前 key 不满足前缀条件时持续向后推进。
	// 3. 命中首个满足前缀的 key 或迭代器失效时停止。
	for ; it.indexIter.Valid(); it.indexIter.Next() {
		key := it.indexIter.Key()
		// 当前 key 只要“长度足够 + 前缀完全相等”，就说明命中过滤条件。
		if len(key) >= prefixLen && bytes.Equal(it.options.Prefix, key[:prefixLen]) {
			break
		}
		// 不满足前缀的项直接跳过，继续检查下一项。
	}
}
