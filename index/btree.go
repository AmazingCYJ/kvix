package index

import (
	"bytes"
	"kvix/data"
	"sort"
	"sync"

	"github.com/google/btree"
)

// BTree 基于 google/btree 实现的内存索引，并通过读写锁保证并发安全。
//
// BTree 将所有 key -> LogRecordPos 映射保存在内存中，
// 适用于数据量适中、对读写延迟敏感的场景。
// 数据库重启后需要通过遍历数据文件重建索引。
type BTree struct {
	tree *btree.BTree
	lock *sync.RWMutex
}

// NewBTree 创建一个基于 google/btree 的内存索引实例。
//
// degree=32 是当前树的分叉因子（branching factor），它决定了每个节点最多容纳的子节点数。
// 较大的 degree 会降低树高、减少查找次数，但每个节点占用更多内存。
// 32 是性能和内存之间的一个经验折中值。
func NewBTree() *BTree {
	return &BTree{
		tree: btree.New(32),
		lock: &sync.RWMutex{},
	}
}

// Put 写入或更新 key 对应的数据文件位置。
//
// 如果 key 已存在，用新的 pos 覆盖旧值并返回旧的 LogRecordPos；
// 如果 key 不存在，插入新条目并返回 nil。
// 该方法通过写锁保证并发安全。
func (bt *BTree) Put(key []byte, pos *data.LogRecordPos) *data.LogRecordPos {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	// 1. ReplaceOrInsert 在 key 已存在时返回旧节点，不存在时返回 nil。
	oldItem := bt.tree.ReplaceOrInsert(&Item{key: key, pos: pos})
	if oldItem == nil {
		return nil
	}
	// 2. 类型断言取出旧的位置信息返回给调用方。
	return oldItem.(*Item).pos
}

// Get 查询 key 对应的数据文件位置信息。
//
// 如果 key 存在，返回对应的 LogRecordPos；如果不存在，返回 nil。
// 该方法通过读锁保证并发安全，多个 Get 可以并行执行。
func (bt *BTree) Get(key []byte) *data.LogRecordPos {
	bt.lock.RLock()
	defer bt.lock.RUnlock()

	// 1. 构造一个只包含 key 的临时 Item 作为查找模板。
	item := bt.tree.Get(&Item{key: key})
	if item == nil {
		return nil
	}
	// 2. 类型断言取出位置信息。
	return item.(*Item).pos
}

// Delete 删除 key 对应的索引条目。
//
// 返回被删除的旧位置信息和是否成功删除的标志。
// 上层可以通过旧位置信息统计可回收的数据空间。
func (bt *BTree) Delete(key []byte) (*data.LogRecordPos, bool) {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	// 1. Delete 返回被删除的旧节点，便于上层统计旧记录占用的空间。
	oldItem := bt.tree.Delete(&Item{key: key})
	if oldItem == nil {
		return nil, false
	}
	// 2. 返回旧位置信息和删除成功标志。
	return oldItem.(*Item).pos, true
}

// Size 返回索引中当前存储的键值对总数。
//
// 通过读锁保证并发安全，直接委托给底层 btree.Len()。
func (bt *BTree) Size() int {
	bt.lock.RLock()
	defer bt.lock.RUnlock()

	return bt.tree.Len()
}

// Iterator 创建并返回一个 BTree 索引迭代器。
//
// 参数 reverse 控制遍历方向：false 为升序，true 为降序。
// 迭代器基于创建时刻的快照，不受后续索引变更影响。
func (bt *BTree) Iterator(reverse bool) IndexIterator {
	bt.lock.RLock()
	defer bt.lock.RUnlock()

	return newBTreeIterator(bt.tree, reverse)
}

// Close 关闭 BTree 索引。
//
// 对纯内存 BTree 来说没有额外资源需要释放，直接返回 nil。
func (bt *BTree) Close() error {
	return nil
}

// BTreeIterator 是基于 BTree 快照切片实现的索引迭代器。
//
// 创建时会将整棵树的内容拷贝到一个有序切片中，
// 后续所有迭代操作都在该切片上进行，不会长时间占用 BTree 的读锁。
type BTreeIterator struct {
	currIndex int     // 当前遍历位置在快照切片中的下标
	reverse   bool    // 是否为反向（降序）迭代
	item      []*Item // 快照切片，存储 key + 位置索引信息
}

// newBTreeIterator 从 BTree 中构建迭代快照。
//
// 该函数会遍历整棵树，将所有节点拷贝到一个有序切片中。
// 正向迭代使用 Ascend（升序），反向迭代使用 Descend（降序）。
func newBTreeIterator(tree *btree.BTree, reverse bool) *BTreeIterator {
	var idx int
	values := make([]*Item, tree.Len())

	if reverse {
		// 1. Descend 按降序遍历整棵树，得到的切片天然就是反向快照。
		tree.Descend(func(i btree.Item) bool {
			values[idx] = i.(*Item)
			idx++
			return true
		})
	} else {
		// 2. Ascend 按升序遍历整棵树，得到正向快照。
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

// Rewind 将迭代器重置到起始位置（快照切片的第一个元素）。
func (bti *BTreeIterator) Rewind() {
	bti.currIndex = 0
}

// Seek 基于当前快照用二分查找定位目标位置。
//
// 正向迭代时：查找第一个 >= key 的元素位置。
// 反向迭代时：查找第一个 <= key 的元素位置。
// 如果没有满足条件的元素，currIndex 将越界，使 Valid() 返回 false。
func (bti *BTreeIterator) Seek(key []byte) {
	if bti.reverse {
		// 1. 降序切片中，第一个 <= key 的位置就是反向查找的起点。
		bti.currIndex = sort.Search(len(bti.item), func(i int) bool {
			return bytes.Compare(bti.item[i].key, key) <= 0
		})
	} else {
		// 2. 升序切片中，第一个 >= key 的位置就是正向查找的起点。
		bti.currIndex = sort.Search(len(bti.item), func(i int) bool {
			return bytes.Compare(bti.item[i].key, key) >= 0
		})
	}
}

// Next 将迭代器向前移动一个位置。
//
// 如果已经到达末尾（currIndex >= len），调用 Next 不会产生任何效果。
func (bti *BTreeIterator) Next() {
	if bti.currIndex < len(bti.item) {
		bti.currIndex++
	}
}

// Valid 检查迭代器当前位置是否有效。
//
// 返回 true 表示 currIndex 在快照切片的合法范围内，可以安全读取 Key/Value。
func (bti *BTreeIterator) Valid() bool {
	return bti.currIndex < len(bti.item)
}

// Key 返回迭代器当前位置的 key。
//
// 仅在 Valid() 返回 true 时有意义；否则返回 nil。
func (bti *BTreeIterator) Key() []byte {
	if bti.Valid() {
		return bti.item[bti.currIndex].key
	}
	return nil
}

// Value 返回迭代器当前位置的数据文件位置信息。
//
// 仅在 Valid() 返回 true 时有意义；否则返回 nil。
func (bti *BTreeIterator) Value() *data.LogRecordPos {
	if bti.Valid() {
		return bti.item[bti.currIndex].pos
	}
	return nil
}

// Close 关闭迭代器，将快照切片置为 nil 以帮助 GC 回收内存。
func (bti *BTreeIterator) Close() {
	bti.item = nil
}
