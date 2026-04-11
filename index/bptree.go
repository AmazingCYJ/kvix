package index

import (
	"bytes"
	"fmt"
	"kvix/data"
	"os"
	"path/filepath"
	"sort"
	"time"

	"go.etcd.io/bbolt"
)

// bptreeIndexFileName 是 B+Tree 索引文件的名称。
// 该文件与数据文件位于同一目录，便于整体备份和移动。
const bptreeIndexFileName = "bptree-index"

// indexBucketName 是 bbolt 中存储索引数据的 bucket 名称。
// 所有 key -> LogRecordPos 的映射都存储在该 bucket 下。
var indexBucketName = []byte("kvix-index")

// BPlusTree 是基于 bbolt 的持久化 B+Tree 索引实现。
//
// 与纯内存索引（BTree、ART）不同，BPlusTree 将 key -> pos 映射单独落盘到 bbolt 文件中。
// 这意味着数据库重启后无需遍历数据文件即可恢复索引，启动速度更快。
// 代价是每次写入都涉及磁盘 I/O，写入延迟高于纯内存索引。
type BPlusTree struct {
	tree *bbolt.DB // 底层 bbolt 数据库实例
	bkt  []byte    // bucket 名称，用于在 bbolt 中定位索引数据
}

// NewBPlusTree 创建一个基于 bbolt 的 B+Tree 持久化索引实例。
//
// 参数说明：
//   - dirPath:    索引文件所在目录路径，索引文件将创建在该目录下。
//   - syncWrites: 是否在每次写入后立即刷盘。设为 true 可保证数据安全但会降低写入性能。
//
// 返回值：
//   - *BPlusTree: 创建成功的索引实例。
//   - error:      创建失败时返回具体错误信息（如文件打开失败、bucket 创建失败等）。
func NewBPlusTree(dirPath string, syncWrites bool) (*BPlusTree, error) {
	// 1. 配置 bbolt 打开选项。
	opts := bbolt.DefaultOptions
	opts.NoSync = !syncWrites
	// 2. 设置超时时间，避免在其他进程持有文件锁时无限阻塞。
	opts.Timeout = 100 * time.Millisecond

	// 3. 拼接索引文件完整路径并打开 bbolt 数据库。
	path := filepath.Join(dirPath, bptreeIndexFileName)
	db, err := bbolt.Open(path, os.ModePerm, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open bptree index at %s: %w", path, err)
	}

	// 4. 构造 BPlusTree 实例。
	bpt := &BPlusTree{tree: db, bkt: indexBucketName}

	// 5. 首次打开时确保 bucket 已存在，后续 Put/Get/Delete 都假定它可用。
	if err := bpt.tree.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bpt.bkt)
		return err
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to create index bucket: %w", err)
	}

	return bpt, nil
}

// Put 写入或更新 key 对应的位置索引，返回旧值（若不存在则为 nil）。
//
// 该方法会开启一次 bbolt 写事务，将新的位置编码后写入 bucket。
// 如果 key 已存在，先读取并解码旧值，再用新值覆盖。
func (bpt *BPlusTree) Put(key []byte, pos *data.LogRecordPos) *data.LogRecordPos {
	var oldPos *data.LogRecordPos
	err := bpt.tree.Update(func(tx *bbolt.Tx) error {
		// 1. 获取 bucket 引用。
		bucket := tx.Bucket(bpt.bkt)
		if bucket == nil {
			var err error
			// 2. 正常情况下 bucket 已在构造函数中创建，这里只是兜底。
			bucket, err = tx.CreateBucketIfNotExists(bpt.bkt)
			if err != nil {
				return err
			}
		}
		// 3. 读取旧值（如果存在），需要复制一份因为 bbolt 切片只在事务内有效。
		if oldValue := bucket.Get(key); len(oldValue) != 0 {
			buf := make([]byte, len(oldValue))
			copy(buf, oldValue)
			oldPos = data.DecodeLogRecordPos(buf)
		}
		// 4. 写入编码后的新位置信息。
		return bucket.Put(key, data.EncodeLogRecordPos(pos))
	})
	if err != nil {
		return nil
	}
	return oldPos
}

// Get 根据 key 查询对应的数据文件位置信息。
//
// 该方法使用 bbolt 只读事务，不会阻塞写操作。
// 如果 key 不存在或 bucket 不存在，返回 nil。
func (bpt *BPlusTree) Get(key []byte) *data.LogRecordPos {
	var result *data.LogRecordPos
	_ = bpt.tree.View(func(tx *bbolt.Tx) error {
		// 1. 获取 bucket 引用。
		bucket := tx.Bucket(bpt.bkt)
		if bucket == nil {
			return nil
		}
		// 2. 查询 key 对应的值。
		value := bucket.Get(key)
		if len(value) == 0 {
			return nil
		}
		// 3. 复制一份再解码，避免事务结束后引用失效。
		buf := make([]byte, len(value))
		copy(buf, value)
		result = data.DecodeLogRecordPos(buf)
		return nil
	})
	return result
}

// Delete 从索引中删除指定 key 的映射。
//
// 返回被删除的旧位置信息和是否成功删除的标志。
// 如果 key 不存在，返回 (nil, false)。
func (bpt *BPlusTree) Delete(key []byte) (*data.LogRecordPos, bool) {
	var (
		deleted bool
		oldPos  *data.LogRecordPos
	)
	err := bpt.tree.Update(func(tx *bbolt.Tx) error {
		// 1. 获取 bucket 引用。
		bucket := tx.Bucket(bpt.bkt)
		if bucket == nil {
			return nil
		}
		// 2. 查询旧值是否存在。
		oldValue := bucket.Get(key)
		if oldValue == nil {
			return nil
		}
		// 3. 先解析出旧位置信息，供上层统计可回收空间。
		buf := make([]byte, len(oldValue))
		copy(buf, oldValue)
		oldPos = data.DecodeLogRecordPos(buf)
		deleted = true
		// 4. 从 bucket 中删除该条目。
		return bucket.Delete(key)
	})
	if err != nil {
		return nil, false
	}
	if !deleted {
		return nil, false
	}
	return oldPos, true
}

// Size 返回索引中当前存储的键值对总数。
//
// 通过 bbolt 只读事务读取 bucket 的统计信息，不需要遍历所有条目。
func (bpt *BPlusTree) Size() int {
	count := 0
	_ = bpt.tree.View(func(tx *bbolt.Tx) error {
		// 1. 获取 bucket 引用。
		bucket := tx.Bucket(bpt.bkt)
		if bucket == nil {
			return nil
		}
		// 2. bbolt 内部维护了 bucket 统计信息，直接复用，无需遍历计数。
		count = bucket.Stats().KeyN
		return nil
	})
	return count
}

// Iterator 创建并返回一个 B+Tree 索引迭代器。
//
// 当前实现会先把 bucket 内所有条目复制成切片快照，再在切片上完成后续遍历。
// 参数 reverse 控制遍历方向：false 为升序，true 为降序。
func (bpt *BPlusTree) Iterator(reverse bool) IndexIterator {
	items := make([]*Item, 0)
	_ = bpt.tree.View(func(tx *bbolt.Tx) error {
		// 1. 获取 bucket 引用。
		bucket := tx.Bucket(bpt.bkt)
		if bucket == nil {
			return nil
		}
		// 2. 使用游标遍历 bucket 中的所有条目，逐个拷贝到快照切片中。
		cursor := bucket.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			// 3. 拷贝 key 和 value，避免持有 bbolt 事务内存引用。
			key := make([]byte, len(k))
			val := make([]byte, len(v))
			copy(key, k)
			copy(val, v)
			items = append(items, &Item{key: key, pos: data.DecodeLogRecordPos(val)})
		}
		return nil
	})

	// 4. 反向迭代时，对快照切片原地翻转为降序。
	if reverse {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}

	return &BPlusTreeIterator{currIndex: 0, reverse: reverse, items: items}
}

// Close 关闭底层 bbolt 数据库文件，释放文件句柄等系统资源。
//
// 关闭后不应再调用该索引的任何方法。
// 如果底层数据库已经为 nil（重复关闭），直接返回 nil。
func (bpt *BPlusTree) Close() error {
	if bpt.tree == nil {
		return nil
	}
	err := bpt.tree.Close()
	bpt.tree = nil
	return err
}

// BPlusTreeIterator 是基于 bbolt 快照切片实现的索引迭代器。
//
// 创建时会将 bucket 中的所有条目拷贝到内存切片中，
// 后续所有迭代操作都在该切片上进行，不会持有 bbolt 事务。
type BPlusTreeIterator struct {
	currIndex int     // 当前遍历位置在快照切片中的下标
	reverse   bool    // 是否为反向（降序）迭代
	items     []*Item // 快照切片，存储 key + 位置索引信息
}

// Rewind 将迭代器重置到起始位置（快照切片的第一个元素）。
func (it *BPlusTreeIterator) Rewind() {
	it.currIndex = 0
}

// Seek 将迭代器定位到目标 key 的位置。
//
// 正向迭代时：使用二分查找定位第一个 >= key 的元素。
// 反向迭代时：使用二分查找定位第一个 <= key 的元素。
func (it *BPlusTreeIterator) Seek(key []byte) {
	if it.reverse {
		// 1. 反向快照是降序切片，使用 <= 作为命中条件。
		it.currIndex = sort.Search(len(it.items), func(i int) bool {
			return bytes.Compare(it.items[i].key, key) <= 0
		})
	} else {
		// 2. 正向快照保持升序，定位第一个 >= key 的位置。
		it.currIndex = sort.Search(len(it.items), func(i int) bool {
			return bytes.Compare(it.items[i].key, key) >= 0
		})
	}
}

// Next 将迭代器向前移动一个位置。
//
// 如果已经到达末尾，调用 Next 不会产生任何效果。
func (it *BPlusTreeIterator) Next() {
	if it.currIndex < len(it.items) {
		it.currIndex++
	}
}

// Valid 检查迭代器当前位置是否有效。
//
// 返回 true 表示 currIndex 在快照切片的合法范围内。
func (it *BPlusTreeIterator) Valid() bool {
	return it.currIndex < len(it.items)
}

// Key 返回迭代器当前位置的 key。
//
// 仅在 Valid() 返回 true 时有意义；否则返回 nil。
func (it *BPlusTreeIterator) Key() []byte {
	if it.Valid() {
		return it.items[it.currIndex].key
	}
	return nil
}

// Value 返回迭代器当前位置的数据文件位置信息。
//
// 仅在 Valid() 返回 true 时有意义；否则返回 nil。
func (it *BPlusTreeIterator) Value() *data.LogRecordPos {
	if it.Valid() {
		return it.items[it.currIndex].pos
	}
	return nil
}

// Close 关闭迭代器，将快照切片置为 nil 以帮助 GC 回收内存。
func (it *BPlusTreeIterator) Close() {
	it.items = nil
}
