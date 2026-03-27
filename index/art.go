package index

import (
	"bytes"
	"kvix/data"
	"sort"
	"sync"

	goart "github.com/plar/go-adaptive-radix-tree"
)

// AdaptiveRadixTree 自适应基数树索引实现
// 封装了 go-adaptive-radix-tree 库，提供了 Indexer 接口的实现。
type AdaptiveRadixTree struct {
	tree goart.Tree
	lock *sync.RWMutex
}

// NewARTree 创建一个基于自适应基数树的内存索引实例。
// 使用读写锁保护底层树结构，适合在并发读写场景下使用。
func NewARTree() *AdaptiveRadixTree {
	return &AdaptiveRadixTree{
		tree: goart.New(),
		lock: &sync.RWMutex{},
	}
}

// Put 写入或更新 key 对应的位置索引，返回旧值（若不存在则为 nil）。
func (art *AdaptiveRadixTree) Put(key []byte, pos *data.LogRecordPos) *data.LogRecordPos {
	art.lock.Lock()
	defer art.lock.Unlock()

	// Insert 返回“旧值 + 是否更新已有键”，正好能让上层判断是否覆盖了旧记录。
	oldValue, updated := art.tree.Insert(goart.Key(key), pos)
	if !updated {
		return nil
	}
	oldPos, ok := oldValue.(*data.LogRecordPos)
	if !ok {
		return nil
	}
	return oldPos
}

// Get 根据 key 查询位置索引。
// 若 key 不存在或类型断言失败，返回 nil。
func (art *AdaptiveRadixTree) Get(key []byte) *data.LogRecordPos {
	art.lock.RLock()
	defer art.lock.RUnlock()

	// go-adaptive-radix-tree 返回的是泛型 interface{}，这里需要再断言回具体位置类型。
	value, found := art.tree.Search(goart.Key(key))
	if !found {
		return nil
	}
	pos, ok := value.(*data.LogRecordPos)
	if !ok {
		return nil
	}
	return pos
}

// Delete 删除指定 key 的索引，返回旧值以及是否删除成功。
func (art *AdaptiveRadixTree) Delete(key []byte) (*data.LogRecordPos, bool) {
	art.lock.Lock()
	defer art.lock.Unlock()

	// Delete 同时返回旧值和是否真的删掉了节点。
	oldValue, deleted := art.tree.Delete(goart.Key(key))
	if !deleted {
		return nil, false
	}
	oldPos, ok := oldValue.(*data.LogRecordPos)
	if !ok {
		return nil, true
	}
	return oldPos, true
}

// Size 返回当前索引中的键值对数量。
// 常用于统计或调试场景。
func (art *AdaptiveRadixTree) Size() int {
	art.lock.RLock()
	defer art.lock.RUnlock()

	return art.tree.Size()
}

// Iterator 获取索引迭代器。
// reverse=false 为正向（升序）遍历，reverse=true 为反向（降序）遍历。
// 迭代器基于创建时的快照，不受后续树内变更影响。
func (art *AdaptiveRadixTree) Iterator(reverse bool) IndexIterator {
	art.lock.RLock()
	defer art.lock.RUnlock()

	// 和其他索引实现一样，这里返回的是快照迭代器，避免后续遍历长期占用树锁。
	return newARTIterator(art.tree, reverse)
}

// Close 对纯内存 ART 没有额外资源需要回收。
func (art *AdaptiveRadixTree) Close() error {
	return nil
}

// ARTIterator 基于 Adaptive Radix Tree 实现的索引迭代器。
type ARTIterator struct {
	currIndex int
	reverse   bool
	items     []*Item
}

// newARTIterator 从 ART 树中构建迭代快照。
// 这里会把叶子节点复制到 items 中，后续迭代仅在内存切片上进行。
func newARTIterator(tree goart.Tree, reverse bool) *ARTIterator {
	items := make([]*Item, 0, tree.Size())
	it := tree.Iterator()
	for it.HasNext() {
		// 逐个把树节点复制进 items，后续 Seek/Next 都只在切片上运行。
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

	// 反向迭代时，对快照切片原地翻转。
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

// Rewind 将迭代器重置到起始位置。
func (ai *ARTIterator) Rewind() {
	ai.currIndex = 0
}

// Seek 将迭代器移动到指定 key 的位置。
// 正向迭代时定位到第一个 >= key 的位置；
// 反向迭代时定位到第一个 <= key 的位置。
func (ai *ARTIterator) Seek(key []byte) {
	if ai.reverse {
		// 反向时 items 已经翻转为降序，因此查找条件也要变成 <= key。
		ai.currIndex = sort.Search(len(ai.items), func(i int) bool {
			return bytes.Compare(ai.items[i].key, key) <= 0
		})
	} else {
		// 正向时定位到第一个大于等于目标 key 的位置。
		ai.currIndex = sort.Search(len(ai.items), func(i int) bool {
			return bytes.Compare(ai.items[i].key, key) >= 0
		})
	}
}

// Next 将迭代器移动到下一个位置。
func (ai *ARTIterator) Next() {
	if ai.currIndex < len(ai.items) {
		ai.currIndex++
	}
}

// Valid 检查迭代器当前是否有效。
func (ai *ARTIterator) Valid() bool {
	return ai.currIndex < len(ai.items)
}

// Key 返回当前迭代器位置的 key。
func (ai *ARTIterator) Key() []byte {
	if ai.Valid() {
		return ai.items[ai.currIndex].key
	}
	return nil
}

// Value 返回当前迭代器位置的 value。
func (ai *ARTIterator) Value() *data.LogRecordPos {
	if ai.Valid() {
		return ai.items[ai.currIndex].pos
	}
	return nil
}

// Close 关闭迭代器，释放相关资源。
func (ai *ARTIterator) Close() {
	ai.items = nil
}
