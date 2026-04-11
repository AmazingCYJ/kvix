package index

import (
	"fmt"
	"kvix/common"
	"kvix/data"

	"github.com/google/btree"
)

// Indexer 是 kvix 存储引擎的核心索引抽象接口。
//
// 所有索引实现（BTree、ART、B+Tree 等）都必须满足该接口。
// 索引负责维护 key 到数据文件位置（LogRecordPos）的映射关系，
// 使得上层读写路径可以通过 key 快速定位到对应的数据记录在磁盘上的物理位置。
//
// 并发安全性：所有实现必须保证在多 goroutine 并发调用时的线程安全。
// 迭代器语义：Iterator 返回的是创建时刻的快照，后续对索引的修改不会影响已创建的迭代器。
type Indexer interface {
	// Put 向索引中写入或更新一条 key -> pos 的映射。
	//
	// 如果 key 已存在，则用新的 pos 覆盖旧值，并返回旧的 LogRecordPos；
	// 如果 key 不存在，则插入新条目，返回 nil。
	// 上层可以通过返回值判断是否发生了覆盖，从而统计可回收的旧数据空间。
	Put(key []byte, pos *data.LogRecordPos) *data.LogRecordPos

	// Get 根据 key 查询对应的数据文件位置信息。
	//
	// 如果 key 存在，返回对应的 LogRecordPos；
	// 如果 key 不存在，返回 nil。
	// 该方法为只读操作，不会修改索引状态。
	Get(key []byte) *data.LogRecordPos

	// Delete 从索引中删除指定 key 的映射。
	//
	// 返回值：
	//   - *data.LogRecordPos: 被删除的旧位置信息，用于上层统计可回收空间；若 key 不存在则为 nil。
	//   - bool: 是否成功删除了条目。若 key 不存在，返回 false。
	Delete(key []byte) (*data.LogRecordPos, bool)

	// Size 返回索引中当前存储的键值对总数。
	//
	// 该方法常用于统计、监控和调试场景。
	// 对于持久化索引（如 B+Tree），该值反映的是磁盘上实际存储的条目数。
	Size() int

	// Iterator 创建并返回一个索引迭代器。
	//
	// 参数 reverse 控制遍历方向：
	//   - false: 按 key 字典序升序遍历（正向）
	//   - true:  按 key 字典序降序遍历（反向）
	//
	// 返回的迭代器基于创建时刻的快照，后续对索引的增删改不会影响迭代结果。
	// 调用方在使用完毕后必须调用 Close() 释放迭代器资源。
	Iterator(reverse bool) IndexIterator

	// Close 关闭索引器并释放底层资源。
	//
	// 对于纯内存索引（BTree、ART），该方法通常为空操作；
	// 对于持久化索引（B+Tree），该方法会关闭底层数据库文件句柄。
	// 关闭后不应再调用索引的任何方法，否则行为未定义。
	Close() error
}

// NewIndexer 根据索引类型创建对应的索引实现实例。
//
// 参数说明：
//   - indexType:   索引类型枚举，决定使用哪种底层索引实现。
//   - dirPath:     索引文件所在目录路径，仅对持久化索引（B+Tree）有效。
//   - syncWrites:  是否在每次写入后立即刷盘，仅对持久化索引有效。
//
// 返回值：
//   - Indexer: 创建成功的索引实例。
//   - error:   创建失败时返回具体错误信息（如不支持的索引类型、文件打开失败等）。
func NewIndexer(indexType common.IndexerType, dirPath string, syncWrites bool) (Indexer, error) {
	switch indexType {
	case common.BTreeIndex:
		// 1. 纯内存 BTree 索引，无需额外初始化，直接返回。
		return NewBTree(), nil
	case common.ARTreeIndex:
		// 2. 纯内存自适应基数树索引，适合按字节前缀组织 key 的场景。
		return NewARTree(), nil
	case common.BPlusTreeIndex:
		// 3. 持久化 B+Tree 索引，索引结构单独落盘到 dirPath 目录。
		return NewBPlusTree(dirPath, syncWrites)
	default:
		return nil, fmt.Errorf("unsupported index type: %d", indexType)
	}
}

// Item 表示索引中的一条键位置映射信息。
//
// key 是用户写入的原始键（字节切片），pos 是该键对应的数据文件物理位置。
// Item 同时实现了 btree.Item 接口，使其可以直接存入 google/btree 中。
type Item struct {
	key []byte
	pos *data.LogRecordPos
}

// Less 定义 BTree 中 Item 的有序比较规则。
//
// 按 key 的字典序（lexicographic order）进行比较，
// 这保证了 BTree 中的节点始终按 key 的自然顺序排列。
func (i *Item) Less(than btree.Item) bool {
	return string(i.key) < string(than.(*Item).key)
}

// IndexIterator 是通用的索引迭代器接口。
//
// 所有索引实现的迭代器都必须满足该接口。
// 迭代器基于创建时刻的快照工作，提供顺序访问索引条目的能力。
// 典型使用模式：创建迭代器 -> Rewind/Seek 定位 -> 循环 Valid/Key/Value/Next -> Close 释放。
//
// 注意事项：
//   - 迭代器创建后，底层索引的变更不会反映到迭代器中（快照隔离）。
//   - 使用完毕后必须调用 Close() 释放资源，避免内存泄漏。
//   - 在 Valid() 返回 false 时调用 Key() 或 Value() 将返回 nil。
type IndexIterator interface {
	// Rewind 将迭代器重置到起始位置（第一个元素）。
	//
	// 正向迭代器重置到最小 key 处，反向迭代器重置到最大 key 处。
	// 调用后迭代器重新变为有效状态（前提是索引非空）。
	Rewind()

	// Seek 将迭代器定位到目标 key 的位置。
	//
	// 正向迭代时：定位到第一个 >= key 的元素。
	// 反向迭代时：定位到第一个 <= key 的元素。
	// 如果没有满足条件的元素，迭代器将变为无效状态（Valid() 返回 false）。
	Seek(key []byte)

	// Next 将迭代器向前移动一个位置。
	//
	// 如果迭代器已经处于末尾（Valid() 为 false），调用 Next 不会产生任何效果。
	Next()

	// Valid 检查迭代器当前位置是否有效。
	//
	// 返回 true 表示当前位置有合法的 key/value 可以读取；
	// 返回 false 表示迭代器已越界或已关闭，此时不应再调用 Key()/Value()。
	Valid() bool

	// Key 返回迭代器当前位置的 key。
	//
	// 仅在 Valid() 返回 true 时有意义；否则返回 nil。
	// 返回的切片是快照副本，调用方可以安全地持有和修改。
	Key() []byte

	// Value 返回迭代器当前位置的数据文件位置信息。
	//
	// 仅在 Valid() 返回 true 时有意义；否则返回 nil。
	// 返回的 LogRecordPos 包含文件 ID 和偏移量，用于从数据文件中读取实际记录。
	Value() *data.LogRecordPos

	// Close 关闭迭代器并释放其持有的资源。
	//
	// 对于基于快照切片的迭代器，Close 会将内部切片置为 nil 以帮助 GC 回收内存。
	// 关闭后迭代器不应再被使用。
	Close()
}
