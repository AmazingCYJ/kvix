package index

import (
	"bytes"
	"kvix/data"
	"sort"
	"sync"

	goart "github.com/plar/go-adaptive-radix-tree"
)

// AdaptiveRadixTree 是基于自适应基数树（Adaptive Radix Tree）的内存索引实现。
//
// ART 是一种针对内存优化的前缀树变体，它会根据子节点数量动态调整节点大小（4/16/48/256），
// 在保持 O(k) 查找复杂度（k 为 key 长度）的同时，显著降低内存占用。
// 适合 key 具有公共前缀的场景（如文件路径、URL 等）。
// 通过读写锁保护底层树结构，保证并发安全。
type AdaptiveRadixTree struct {
	tree goart.Tree    // 底层自适应基数树实例
	lock *sync.RWMutex // 读写锁，保证并发安全
}

// NewARTree 创建一个基于自适应基数树的内存索引实例。
//
// 使用读写锁保护底层树结构，适合在并发读写场景下使用。
// 与 BTree 相比，ART 在 key 具有公共前缀时有更好的内存效率和查找性能。
func NewARTree() *AdaptiveRadixTree {
	return &AdaptiveRadixTree{
		tree: goart.New(),
		lock: &sync.RWMutex{},
	}
}

// Put 写入或更新 key 对应的数据文件位置索引。
//
// 如果 key 已存在，用新的 pos 覆盖旧值并返回旧的 LogRecordPos；
// 如果 key 不存在，插入新条目并返回 nil。
// 该方法通过写锁保证并发安全。
func (art *AdaptiveRadixTree) Put(key []byte, pos *data.LogRecordPos) *data.LogRecordPos {
	art.lock.Lock()
	defer art.lock.Unlock()

	// 1. Insert 返回"旧值 + 是否更新已有键"，用于判断是否发生了覆盖。
	oldValue, updated := art.tree.Insert(goart.Key(key), pos)
	if !updated {
		return nil
	}
	// 2. 类型断言取出旧的位置信息。
	oldPos, ok := oldValue.(*data.LogRecordPos)
	if !ok {
		return nil
	}
	return oldPos
}

// Get 根据 key 查询对应的数据文件位置信息。
//
// 如果 key 存在，返回对应的 LogRecordPos；如果不存在，返回 nil。
// 该方法通过读锁保证并发安全，多个 Get 可以并行执行。
func (art *AdaptiveRadixTree) Get(key []byte) *data.LogRecordPos {
	art.lock.RLock()
	defer art.lock.RUnlock()

	// 1. go-adaptive-radix-tree 返回的是泛型 interface{}，需要类型断言。
	value, found := art.tree.Search(goart.Key(key))
	if !found {
		return nil
	}
	// 2. 断言回具体的位置类型。
	pos, ok := value.(*data.LogRecordPos)
	if !ok {
		return nil
	}
	return pos
}

// Delete 从索引中删除指定 key 的映射。
//
// 返回被删除的旧位置信息和是否成功删除的标志。
// 如果 key 不存在，返回 (nil, false)。
func (art *AdaptiveRadixTree) Delete(key []byte) (*data.LogRecordPos, bool) {
	art.lock.Lock()
	defer art.lock.Unlock()

	// 1. Delete 同时返回旧值和是否真的删掉了节点。
	oldValue, deleted := art.tree.Delete(goart.Key(key))
	if !deleted {
		return nil, false
	}
	// 2. 类型断言取出旧的位置信息。
	oldPos, ok := oldValue.(*data.LogRecordPos)
	if !ok {
		return nil, true
	}
	return oldPos, true
}

// Size 返回索引中当前存储的键值对总数。
//
// 通过读锁保证并发安全，直接委托给底层 ART 的 Size() 方法。
func (art *AdaptiveRadixTree) Size() int {
	art.lock.RLock()
	defer art.lock.RUnlock()

	return art.tree.Size()
}

// Iterator 创建并返回一个 ART 索引迭代器。
//
// 参数 reverse 控制遍历方向：false 为升序，true 为降序。
// 迭代器基于创建时刻的快照，不受后续索引变更影响。
// 调用方在使用完毕后必须调用 Close() 释放资源。
func (art *AdaptiveRadixTree) Iterator(reverse bool) IndexIterator {
	art.lock.RLock()
	defer art.lock.RUnlock()

	return newARTIterator(art.tree, reverse)
}

// Close 关闭 ART 索引。
//
// 对纯内存 ART 来说没有额外资源需要释放，直接返回 nil。
func (art *AdaptiveRadixTree) Close() error {
	return nil
}

// ARTIterator 是基于 ART 快照切片实现的索引迭代器。
//
// 创建时会将整棵树的叶子节点拷贝到一个有序切片中，
// 后续所有迭代操作都在该切片上进行，不会长时间占用 ART 的读锁。
type ARTIterator struct {
	currIndex int     // 当前遍历位置在快照切片中的下标
	reverse   bool    // 是否为反向（降序）迭代
	items     []*Item // 快照切片，存储 key + 位置索引信息
}

// newARTIterator 从 ART 树中构建迭代快照。
//
// 该函数会遍历整棵树的所有叶子节点，将 key 和位置信息拷贝到切片中。
// 反向迭代时，会对快照切片进行原地翻转。
func newARTIterator(tree goart.Tree, reverse bool) *ARTIterator {
	// 1. 预分配切片容量，避免频繁扩容。
	items := make([]*Item, 0, tree.Size())

	// 2. 遍历 ART 树的所有叶子节点，逐个拷贝到 items 中。
	it := tree.Iterator()
	for it.HasNext() {
		node, err := it.Next()
		if err != nil {
			break
		}
		value, ok := node.Value().(*data.LogRecordPos)
		if !ok {
			continue
		}
		items = append(items, &Item{key: []byte(node.Key()), pos: value})
	}

	// 3. 反向迭代时，对快照切片原地翻转为降序。
	if reverse {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}

	return &ARTIterator{
		currIndex: 0,
		reverse:   reverse,
		items:     items,
	}
}

// Rewind 将迭代器重置到起始位置（快照切片的第一个元素）。
func (ai *ARTIterator) Rewind() {
	ai.currIndex = 0
}

// Seek 将迭代器定位到目标 key 的位置。
//
// 正向迭代时：使用二分查找定位第一个 >= key 的元素。
// 反向迭代时：使用二分查找定位第一个 <= key 的元素。
// 如果没有满足条件的元素，迭代器将变为无效状态。
func (ai *ARTIterator) Seek(key []byte) {
	if ai.reverse {
		// 1. 反向时 items 已翻转为降序，查找条件变为 <= key。
		ai.currIndex = sort.Search(len(ai.items), func(i int) bool {
			return bytes.Compare(ai.items[i].key, key) <= 0
		})
	} else {
		// 2. 正向时定位到第一个 >= key 的位置。
		ai.currIndex = sort.Search(len(ai.items), func(i int) bool {
			return bytes.Compare(ai.items[i].key, key) >= 0
		})
	}
}

// Next 将迭代器向前移动一个位置。
//
// 如果已经到达末尾，调用 Next 不会产生任何效果。
func (ai *ARTIterator) Next() {
	if ai.currIndex < len(ai.items) {
		ai.currIndex++
	}
}

// Valid 检查迭代器当前位置是否有效。
//
// 返回 true 表示 currIndex 在快照切片的合法范围内。
func (ai *ARTIterator) Valid() bool {
	return ai.currIndex < len(ai.items)
}

// Key 返回迭代器当前位置的 key。
//
// 仅在 Valid() 返回 true 时有意义；否则返回 nil。
func (ai *ARTIterator) Key() []byte {
	if ai.Valid() {
		return ai.items[ai.currIndex].key
	}
	return nil
}

// Value 返回迭代器当前位置的数据文件位置信息。
//
// 仅在 Valid() 返回 true 时有意义；否则返回 nil。
func (ai *ARTIterator) Value() *data.LogRecordPos {
	if ai.Valid() {
		return ai.items[ai.currIndex].pos
	}
	return nil
}

// Close 关闭迭代器，将快照切片置为 nil 以帮助 GC 回收内存。
func (ai *ARTIterator) Close() {
	ai.items = nil
}
