package index

import (
	"bitcask-my/data"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"time"

	"go.etcd.io/bbolt"
)

const bptreeIndexFiuleName = "bptree-index"

var indexBucketName = []byte("bitcask-index")

// B+树索引实现
// 主要封装了 bbolt 库，提供了 Indexer 接口的实现。
type BPlusTree struct {
	tree *bbolt.DB
	bkt  []byte
}

// NewBPlusTree 创建一个基于 bbolt 的 B+ 树索引。
// dirPath 为 bbolt 数据文件路径；若文件不存在会自动创建。
func NewBPlusTree(dirPath string, syncWrites bool) *BPlusTree {
	opts := bbolt.DefaultOptions
	opts.NoSync = !syncWrites
	// Avoid blocking indefinitely when another process already holds the file lock.
	opts.Timeout = 100 * time.Millisecond
	path := filepath.Join(dirPath, bptreeIndexFiuleName)
	db, err := bbolt.Open(path, os.ModePerm, opts)
	if err != nil {
		return nil
	}
	bpt := &BPlusTree{tree: db, bkt: indexBucketName}
	if err := bpt.tree.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bpt.bkt)
		return err
	}); err != nil {
		_ = db.Close()
		return nil
	}
	return bpt
}

// Put 写入或更新 key 对应的位置索引，返回旧值（若不存在则为 nil）。
func (bpt *BPlusTree) Put(key []byte, pos *data.LogRecordPos) *data.LogRecordPos {
	var oldPos *data.LogRecordPos
	err := bpt.tree.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bpt.bkt)
		if bucket == nil {
			var err error
			bucket, err = tx.CreateBucketIfNotExists(bpt.bkt)
			if err != nil {
				return err
			}
		}
		if oldValue := bucket.Get(key); len(oldValue) != 0 {
			buf := make([]byte, len(oldValue))
			copy(buf, oldValue)
			oldPos = data.DecodeLogRecordPos(buf)
		}
		return bucket.Put(key, data.EncodeLogRecordPos(pos))
	})
	if err != nil {
		return nil
	}
	return oldPos
}

// Get 根据 key 查询位置索引。
func (bpt *BPlusTree) Get(key []byte) *data.LogRecordPos {
	var result *data.LogRecordPos
	_ = bpt.tree.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bpt.bkt)
		if bucket == nil {
			return nil
		}
		value := bucket.Get(key)
		if len(value) == 0 {
			return nil
		}
		buf := make([]byte, len(value))
		copy(buf, value)
		result = data.DecodeLogRecordPos(buf)
		return nil
	})
	return result
}

// Delete 删除指定 key 的索引，返回旧值以及是否删除成功。
func (bpt *BPlusTree) Delete(key []byte) (*data.LogRecordPos, bool) {
	var (
		deleted bool
		oldPos  *data.LogRecordPos
	)
	err := bpt.tree.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bpt.bkt)
		if bucket == nil {
			return nil
		}
		oldValue := bucket.Get(key)
		if oldValue == nil {
			return nil
		}
		buf := make([]byte, len(oldValue))
		copy(buf, oldValue)
		oldPos = data.DecodeLogRecordPos(buf)
		deleted = true
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

// Size 返回索引中键值对的数量。
func (bpt *BPlusTree) Size() int {
	count := 0
	_ = bpt.tree.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bpt.bkt)
		if bucket == nil {
			return nil
		}
		count = bucket.Stats().KeyN
		return nil
	})
	return count
}

// Iterator 获取索引迭代器。
func (bpt *BPlusTree) Iterator(reverse bool) IndexIterator {
	items := make([]*Item, 0)
	_ = bpt.tree.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bpt.bkt)
		if bucket == nil {
			return nil
		}
		cursor := bucket.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			key := make([]byte, len(k))
			val := make([]byte, len(v))
			copy(key, k)
			copy(val, v)
			items = append(items, &Item{key: key, pos: data.DecodeLogRecordPos(val)})
		}
		return nil
	})

	if reverse {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}

	return &BPlusTreeIterator{currIndex: 0, reverse: reverse, items: items}
}

// Close 关闭底层 bbolt 数据库文件。
func (bpt *BPlusTree) Close() error {
	if bpt.tree == nil {
		return nil
	}
	err := bpt.tree.Close()
	bpt.tree = nil
	return err
}

// BPlusTreeIterator 基于 bbolt 快照切片的迭代器。
type BPlusTreeIterator struct {
	currIndex int
	reverse   bool
	items     []*Item
}

// Rewind 将迭代器重置到起始位置。
func (it *BPlusTreeIterator) Rewind() {
	it.currIndex = 0
}

// Seek 将迭代器移动到指定 key 的位置。
func (it *BPlusTreeIterator) Seek(key []byte) {
	if it.reverse {
		it.currIndex = sort.Search(len(it.items), func(i int) bool {
			return bytes.Compare(it.items[i].key, key) <= 0
		})
	} else {
		it.currIndex = sort.Search(len(it.items), func(i int) bool {
			return bytes.Compare(it.items[i].key, key) >= 0
		})
	}
}

// Next 将迭代器移动到下一个位置。
func (it *BPlusTreeIterator) Next() {
	if it.currIndex < len(it.items) {
		it.currIndex++
	}
}

// Valid 检查迭代器当前是否有效。
func (it *BPlusTreeIterator) Valid() bool {
	return it.currIndex < len(it.items)
}

// Key 返回当前迭代器位置的 key。
func (it *BPlusTreeIterator) Key() []byte {
	if it.Valid() {
		return it.items[it.currIndex].key
	}
	return nil
}

// Value 返回当前迭代器位置的 value。
func (it *BPlusTreeIterator) Value() *data.LogRecordPos {
	if it.Valid() {
		return it.items[it.currIndex].pos
	}
	return nil
}

// Close 关闭迭代器，释放相关资源。
func (it *BPlusTreeIterator) Close() {
	it.items = nil
}
