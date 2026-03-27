package index

import (
	// . "kvix/common"
	"bytes"
	"kvix/data"
	"sort"

	"sync"

	"github.com/google/btree"
)

// BTree 基于 google/btree 实现内存索引，并通过读写锁保证并发安全。
type BTree struct {
	tree *btree.BTree
	lock *sync.RWMutex
}

// NewBTree 创建一个基于 google/btree 的内存索引。
// degree=32 是当前树的分叉因子，它影响节点容量与树高，是性能和内存之间的一个经验折中。
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

	// ReplaceOrInsert 会在 key 已存在时返回旧节点，不存在时返回 nil。
	oldItem := bt.tree.ReplaceOrInsert(&Item{key: key, pos: pos})
	if oldItem == nil {
		return nil
	}
	return oldItem.(*Item).pos
}

// Get 查询 key 对应的位置；如果不存在，返回 nil。
func (bt *BTree) Get(key []byte) *data.LogRecordPos {
	bt.lock.RLock()
	defer bt.lock.RUnlock()

	// 查询时构造一个只包含 key 的临时 Item 作为查找模板即可。
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

	// Delete 返回被删除的旧节点，便于上层统计旧记录占用的空间。
	oldItem := bt.tree.Delete(&Item{key: key})
	if oldItem == nil {
		return nil, false
	}
	return oldItem.(*Item).pos, true
}

// Size 返回索引中键值对的数量。
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

// Close 对纯内存 BTree 来说没有额外资源需要释放。
func (bt *BTree) Close() error {
	return nil
}

// BTreeIterator 基于 BTree 实现的索引迭代器。
type BTreeIterator struct {
	currIndex int     // 当前索引位置
	reverse   bool    // 是否反向迭代
	item      []*Item // key + 位置索引信息
}

// newBTreeIterator 会先把当前树内容拷贝成一个有序快照。
// 后续迭代只在切片上进行，因此不会长时间占用 BTree 的读锁。
func newBTreeIterator(tree *btree.BTree, reverse bool) *BTreeIterator {
	var idx int
	values := make([]*Item, tree.Len())
	if reverse {
		// Descend 会按降序遍历整棵树，得到的切片天然就是反向快照。
		tree.Descend(func(i btree.Item) bool {
			values[idx] = i.(*Item)
			idx++
			return true
		})
	} else {
		// Ascend 会按升序遍历整棵树，得到正向快照。
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

// Seek 基于当前快照用二分查找定位目标位置。
// 正向迭代找第一个 >= key 的元素，反向迭代找第一个 <= key 的元素。
func (bti *BTreeIterator) Seek(key []byte) {
	if bti.reverse {
		// 降序切片中，第一个 <= key 的位置就是反向查找的起点。
		bti.currIndex = sort.Search(len(bti.item), func(i int) bool {
			return bytes.Compare(bti.item[i].key, key) <= 0
		})
	} else {
		// 升序切片中，第一个 >= key 的位置就是正向查找的起点。
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

// Close 释放快照切片引用，帮助 GC 回收。
func (bti *BTreeIterator) Close() {
	bti.item = nil
}
