package index

import (
	"kvix/data"

	. "kvix/common"

	"github.com/google/btree"
)

// Indexer 索引器接口
type Indexer interface {
	// Put 索引数据
	Put(key []byte, pos *data.LogRecordPos) *data.LogRecordPos
	// Get 获取索引数据
	Get(key []byte) *data.LogRecordPos
	// Delete 删除索引数据
	Delete(key []byte) (*data.LogRecordPos, bool)
	// Size 返回索引中键值对的数量。
	Size() int
	// Iterator 获取索引迭代器
	Iterator(reverse bool) IndexIterator
	// Close 关闭索引器，释放相关资源。
	Close() error
}

// NewIndexer 根据配置创建索引实现。
// 1. BTree 和 ART 使用纯内存索引，适合常规读写路径。
// 2. B+Tree 依赖独立文件存储索引，适合需要索引持久化的场景。
// 3. 未知类型直接 panic，确保错误配置在启动阶段暴露。
func NewIndexer(idextype IndexerType, dirPath string, sync bool) Indexer {
	switch idextype {
	case BTreeIndex:
		// 纯内存 BTree，简单直接。
		return NewBTree()
	case ARTreeIndex:
		// 纯内存自适应基数树，适合按字节前缀组织 key。
		return NewARTree()
	case BPlusTreeIndex:
		// 持久化 B+Tree，索引结构单独落盘。
		return NewBPlusTree(dirPath, sync)
	default:
		panic("unsupported index type")
	}
}

// Item 表示索引中的一条键位置信息。
type Item struct {
	key []byte
	pos *data.LogRecordPos
}

// Less 定义 BTree 中 Item 的有序比较规则（按 key 字典序）。
func (i *Item) Less(than btree.Item) bool {
	// BTree 通过 Less 决定节点内排序规则，这里按 key 的字典序排列。
	return string(i.key) < string(than.(*Item).key)
}

// IndexIterator 通用索引迭代器
type IndexIterator interface {
	// Rewind 将迭代器重置到起始位置。
	Rewind()
	// Seek 将迭代器移动到指定 key 的位置。
	Seek(key []byte)
	// Next 将迭代器移动到下一个位置。
	Next()
	// Valid 检查迭代器当前是否有效。
	Valid() bool
	// Key 返回当前迭代器位置的 key。
	Key() []byte
	// Value 返回当前迭代器位置的 value。
	Value() *data.LogRecordPos
	// Close 关闭迭代器，释放相关资源。
	Close()
}
