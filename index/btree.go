package index

import (
	// . "bitcask-my/common"
	"bitcask-my/data"
	"bytes"
	"sort"

	"sync"

	"github.com/google/btree"
)

// BTree 基于 google/btree 实现内存索引，并通过读写锁保证并发安全。
type BTree struct {
	tree *btree.BTree
	lock *sync.RWMutex
}

func NewBTree() *BTree {
	return &BTree{
		tree: btree.New(32), // 32 是 BTree 的 degree，可以根据实际情况调整
		lock: &sync.RWMutex{},
	}
}

// Put 写入或更新 key 对应的位置。
func (bt *BTree) Put(key []byte, pos *data.LogRecordPos) *data.LogRecordPos {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	oldItem := bt.tree.ReplaceOrInsert(&Item{key: key, pos: pos})
	if oldItem == nil {
		return nil
	}
	return oldItem.(*Item).pos
}

// Get 查询 key 对应的位置，不存在时返回 ErrKeyNotFound。
func (bt *BTree) Get(key []byte) *data.LogRecordPos {
	bt.lock.RLock()
	defer bt.lock.RUnlock()

	item := bt.tree.Get(&Item{key: key})
	if item == nil {
		return nil
	}
	return item.(*Item).pos
}

// Delete 删除 key 对应的位置。
func (bt *BTree) Delete(key []byte) (*data.LogRecordPos, bool) {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	oldItem := bt.tree.Delete(&Item{key: key})
	if oldItem == nil {
		return nil, false
	}
	return oldItem.(*Item).pos, true
}

// / Size 返回索引中键值对的数量。
func (bt *BTree) Size() int {
	bt.lock.RLock()
	defer bt.lock.RUnlock()

	return bt.tree.Len()
}

// Iterator 获取索引迭代器，支持正向和反向迭代。
func (bt *BTree) Iterator(reverse bool) IndexIterator {
	bt.lock.RLock()
	defer bt.lock.RUnlock()

	return newBTreeIterator(bt.tree, reverse)
}
func (bt *BTree) Close() error {
	return nil
}

// BTreeIterator 基于 BTree 实现的索引迭代器。
type BTreeIterator struct {
	currIndex int     // 当前索引位置
	reverse   bool    // 是否反向迭代
	item      []*Item // key + 位置索引信息
}

// newBTreeIterator 创建一个新的 BTreeIterator 实例。
func newBTreeIterator(tree *btree.BTree, reverse bool) *BTreeIterator {
	var idx int
	values := make([]*Item, tree.Len())
	if reverse {
		tree.Descend(func(i btree.Item) bool {
			values[idx] = i.(*Item)
			idx++
			return true
		})
	} else {
		tree.Ascend(func(i btree.Item) bool {
			values[idx] = i.(*Item)
			idx++
			return true
		})
	}
	return &BTreeIterator{
		currIndex: 0,
		reverse:   reverse,
		item:      values,
	}
}

// Rewind 将迭代器重置到起始位置。
func (bti *BTreeIterator) Rewind() {
	bti.currIndex = 0
}

// Seek 将迭代器移动到指定 key 的位置。
func (bti *BTreeIterator) Seek(key []byte) {
	if bti.reverse {
		bti.currIndex = sort.Search(len(bti.item), func(i int) bool {
			return bytes.Compare(bti.item[i].key, key) <= 0
		})
	} else {
		bti.currIndex = sort.Search(len(bti.item), func(i int) bool {
			return bytes.Compare(bti.item[i].key, key) >= 0
		})
	}
}

// Next 将迭代器移动到下一个位置。
func (bti *BTreeIterator) Next() {
	if bti.currIndex < len(bti.item) {
		bti.currIndex++
	}
}

// Vaild 检查迭代器当前是否有效。
func (bti *BTreeIterator) Valid() bool {
	return bti.currIndex < len(bti.item)
}

// Key 返回当前迭代器位置的 key。
func (bti *BTreeIterator) Key() []byte {
	if bti.Valid() {
		return bti.item[bti.currIndex].key
	}
	return nil
}

// Value 返回当前迭代器位置的 value。
func (bti *BTreeIterator) Value() *data.LogRecordPos {
	if bti.Valid() {
		return bti.item[bti.currIndex].pos
	}
	return nil
}

// Close 关闭迭代器，释放相关资源。
func (bti *BTreeIterator) Close() {
	bti.item = nil
}
