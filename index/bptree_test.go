package index

import (
	. "bitcask-my/common"
	"bitcask-my/data"
	"errors"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func openTestBPlusTree(t *testing.T) *BPlusTree {
	t.Helper()
	bpt := NewBPlusTree(t.TempDir(), false)
	if bpt == nil {
		t.Fatalf("NewBPlusTree() returned nil")
	}
	t.Cleanup(func() {
		_ = bpt.Close()
	})
	return bpt
}

func putBPlusTestItem(t *testing.T, bpt *BPlusTree, key string, fid uint32, offset int64) {
	t.Helper()
	oldPos := bpt.Put([]byte(key), &data.LogRecordPos{Fid: fid, Offset: offset})
	if oldPos != nil {
		t.Fatalf("Put(%q) oldPos = %v, want nil", key, oldPos)
	}
}

func TestBPlusTreeCRUDAndSize(t *testing.T) {
	bpt := openTestBPlusTree(t)

	if bpt.Size() != 0 {
		t.Fatalf("initial Size() = %d, want 0", bpt.Size())
	}

	putBPlusTestItem(t, bpt, "a", 1, 10)
	putBPlusTestItem(t, bpt, "b", 2, 20)
	putBPlusTestItem(t, bpt, "c", 3, 30)

	if bpt.Size() != 3 {
		t.Fatalf("Size() = %d, want 3", bpt.Size())
	}

	pos := bpt.Get([]byte("b"))
	if pos == nil || pos.Offset != 20 {
		t.Fatalf("Get(b) = %v, want offset 20", pos)
	}

	oldPos := bpt.Put([]byte("b"), &data.LogRecordPos{Fid: 22, Offset: 220})
	if oldPos == nil || oldPos.Fid != 2 || oldPos.Offset != 20 {
		t.Fatalf("Put(b) oldPos = %v, want fid 2 offset 20", oldPos)
	}
	if bpt.Size() != 3 {
		t.Fatalf("Size() after overwrite = %d, want 3", bpt.Size())
	}

	pos = bpt.Get([]byte("b"))
	if pos == nil || pos.Fid != 22 || pos.Offset != 220 {
		t.Fatalf("Get(b) after overwrite = %v, want fid 22 offset 220", pos)
	}

	oldPos, ok := bpt.Delete([]byte("b"))
	if !ok {
		t.Fatalf("Delete(b) ok = false, want true")
	}
	if oldPos == nil || oldPos.Fid != 22 || oldPos.Offset != 220 {
		t.Fatalf("Delete(b) oldPos = %v, want fid 22 offset 220", oldPos)
	}
	oldPos, ok = bpt.Delete([]byte("b"))
	if ok {
		t.Fatalf("Delete(b) second time ok = true, want false")
	}
	if oldPos != nil {
		t.Fatalf("Delete(b) second time oldPos = %v, want nil", oldPos)
	}
	if bpt.Get([]byte("b")) != nil {
		t.Fatalf("Get(b) after delete = non-nil, want nil")
	}
	if bpt.Size() != 2 {
		t.Fatalf("Size() after delete = %d, want 2", bpt.Size())
	}
}

func TestBPlusTreeIteratorForward(t *testing.T) {
	bpt := openTestBPlusTree(t)
	putBPlusTestItem(t, bpt, "c", 3, 30)
	putBPlusTestItem(t, bpt, "a", 1, 10)
	putBPlusTestItem(t, bpt, "b", 2, 20)

	it := bpt.Iterator(false)
	it.Rewind()

	var gotKeys []string
	var gotOffsets []int64
	for ; it.Valid(); it.Next() {
		gotKeys = append(gotKeys, string(it.Key()))
		gotOffsets = append(gotOffsets, it.Value().Offset)
	}

	wantKeys := []string{"a", "b", "c"}
	wantOffsets := []int64{10, 20, 30}
	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("len(keys) = %d, want %d", len(gotKeys), len(wantKeys))
	}
	for i := range wantKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Fatalf("key[%d] = %q, want %q", i, gotKeys[i], wantKeys[i])
		}
		if gotOffsets[i] != wantOffsets[i] {
			t.Fatalf("offset[%d] = %d, want %d", i, gotOffsets[i], wantOffsets[i])
		}
	}
}

func TestBPlusTreeIteratorReverse(t *testing.T) {
	bpt := openTestBPlusTree(t)
	putBPlusTestItem(t, bpt, "a", 1, 10)
	putBPlusTestItem(t, bpt, "b", 2, 20)
	putBPlusTestItem(t, bpt, "c", 3, 30)

	it := bpt.Iterator(true)
	it.Rewind()

	var gotKeys []string
	for ; it.Valid(); it.Next() {
		gotKeys = append(gotKeys, string(it.Key()))
	}

	wantKeys := []string{"c", "b", "a"}
	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("len(keys) = %d, want %d", len(gotKeys), len(wantKeys))
	}
	for i := range wantKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Fatalf("key[%d] = %q, want %q", i, gotKeys[i], wantKeys[i])
		}
	}
}

func TestBPlusTreeIteratorSeekForward(t *testing.T) {
	bpt := openTestBPlusTree(t)
	putBPlusTestItem(t, bpt, "a", 1, 10)
	putBPlusTestItem(t, bpt, "c", 3, 30)
	putBPlusTestItem(t, bpt, "e", 5, 50)

	it := bpt.Iterator(false)

	it.Seek([]byte("c"))
	if !it.Valid() || string(it.Key()) != "c" {
		t.Fatalf("Seek(c) key = %q, valid = %v, want key c and valid true", it.Key(), it.Valid())
	}

	it.Seek([]byte("b"))
	if !it.Valid() || string(it.Key()) != "c" {
		t.Fatalf("Seek(b) key = %q, valid = %v, want key c and valid true", it.Key(), it.Valid())
	}

	it.Seek([]byte("z"))
	if it.Valid() {
		t.Fatalf("Seek(z) valid = true, want false")
	}
}

func TestBPlusTreeIteratorSeekReverse(t *testing.T) {
	bpt := openTestBPlusTree(t)
	putBPlusTestItem(t, bpt, "a", 1, 10)
	putBPlusTestItem(t, bpt, "c", 3, 30)
	putBPlusTestItem(t, bpt, "e", 5, 50)

	it := bpt.Iterator(true)

	it.Seek([]byte("d"))
	if !it.Valid() || string(it.Key()) != "c" {
		t.Fatalf("Seek(d) key = %q, valid = %v, want key c and valid true", it.Key(), it.Valid())
	}

	it.Seek([]byte("z"))
	if !it.Valid() || string(it.Key()) != "e" {
		t.Fatalf("Seek(z) key = %q, valid = %v, want key e and valid true", it.Key(), it.Valid())
	}

	it.Seek([]byte("0"))
	if it.Valid() {
		t.Fatalf("Seek(0) valid = true, want false")
	}
}

func TestBPlusTreeIteratorRewindAndClose(t *testing.T) {
	bpt := openTestBPlusTree(t)
	putBPlusTestItem(t, bpt, "a", 1, 10)
	putBPlusTestItem(t, bpt, "b", 2, 20)

	it := bpt.Iterator(false)
	it.Next()
	it.Next()
	if it.Valid() {
		t.Fatalf("after moving twice, valid = true, want false")
	}

	it.Rewind()
	if !it.Valid() || string(it.Key()) != "a" {
		t.Fatalf("after Rewind key = %q, valid = %v, want key a and valid true", it.Key(), it.Valid())
	}

	it.Close()
	if it.Valid() {
		t.Fatalf("after Close valid = true, want false")
	}
	if it.Key() != nil {
		t.Fatalf("after Close key = %v, want nil", it.Key())
	}
	if it.Value() != nil {
		t.Fatalf("after Close value = %v, want nil", it.Value())
	}
}

func TestBPlusTreeReopenPersistence(t *testing.T) {
	dir := t.TempDir()
	bpt := NewBPlusTree(dir, false)
	if bpt == nil {
		t.Fatalf("NewBPlusTree() returned nil")
	}

	putBPlusTestItem(t, bpt, "persist-a", 1, 11)
	putBPlusTestItem(t, bpt, "persist-b", 2, 22)
	if oldPos, ok := bpt.Delete([]byte("persist-b")); !ok || oldPos == nil || oldPos.Fid != 2 || oldPos.Offset != 22 {
		t.Fatalf("Delete(persist-b) = (%v,%v), want oldPos fid 2 offset 22 and ok true", oldPos, ok)
	}
	if err := bpt.Close(); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}

	bpt2 := NewBPlusTree(dir, false)
	if bpt2 == nil {
		t.Fatalf("NewBPlusTree(reopen) returned nil")
	}
	t.Cleanup(func() { _ = bpt2.Close() })

	pos := bpt2.Get([]byte("persist-a"))
	if pos == nil || pos.Fid != 1 || pos.Offset != 11 {
		t.Fatalf("Get(persist-a) after reopen = %v, want fid 1 offset 11", pos)
	}
	if bpt2.Get([]byte("persist-b")) != nil {
		t.Fatalf("Get(persist-b) after reopen = non-nil, want nil")
	}
}

func TestBPlusTreeFactoryCreation(t *testing.T) {
	idx := NewIndexer(BPlusTreeIndex, t.TempDir(), false)
	if idx == nil {
		t.Fatalf("NewIndexer(BPlusTreeIndex) = nil, want non-nil")
	}
	if _, ok := idx.(*BPlusTree); !ok {
		t.Fatalf("NewIndexer(BPlusTreeIndex) type = %T, want *BPlusTree", idx)
	}
}

func TestBPlusTreePutEmptyKey(t *testing.T) {
	bpt := openTestBPlusTree(t)
	oldPos := bpt.Put([]byte{}, &data.LogRecordPos{Fid: 1, Offset: 1})
	if oldPos != nil {
		t.Fatalf("Put(empty key) oldPos = %v, want nil", oldPos)
	}
	if bpt.Size() != 0 {
		t.Fatalf("Size() after Put(empty key) = %d, want 0", bpt.Size())
	}
}

func TestBPlusTreeIteratorSnapshotIsolation(t *testing.T) {
	bpt := openTestBPlusTree(t)
	putBPlusTestItem(t, bpt, "a", 1, 10)
	putBPlusTestItem(t, bpt, "b", 2, 20)

	it := bpt.Iterator(false)
	// 迭代器创建后再修改索引，快照内容应保持不变。
	putBPlusTestItem(t, bpt, "c", 3, 30)
	if oldPos, ok := bpt.Delete([]byte("a")); !ok || oldPos == nil || oldPos.Fid != 1 || oldPos.Offset != 10 {
		t.Fatalf("Delete(a) = (%v,%v), want oldPos fid 1 offset 10 and ok true", oldPos, ok)
	}

	it.Rewind()
	var gotKeys []string
	for ; it.Valid(); it.Next() {
		gotKeys = append(gotKeys, string(it.Key()))
	}

	wantKeys := []string{"a", "b"}
	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("snapshot len(keys) = %d, want %d", len(gotKeys), len(wantKeys))
	}
	for i := range wantKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Fatalf("snapshot key[%d] = %q, want %q", i, gotKeys[i], wantKeys[i])
		}
	}
}

func TestBPlusTreeTransactionCommit(t *testing.T) {
	bpt := openTestBPlusTree(t)

	err := bpt.tree.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bpt.bkt)
		if bucket == nil {
			return errors.New("bucket not found")
		}
		if err := bucket.Put([]byte("tx-c1"), data.EncodeLogRecordPos(&data.LogRecordPos{Fid: 1, Offset: 101})); err != nil {
			return err
		}
		if err := bucket.Put([]byte("tx-c2"), data.EncodeLogRecordPos(&data.LogRecordPos{Fid: 2, Offset: 202})); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction commit error = %v", err)
	}

	pos1 := bpt.Get([]byte("tx-c1"))
	if pos1 == nil || pos1.Fid != 1 || pos1.Offset != 101 {
		t.Fatalf("Get(tx-c1) = %v, want fid 1 offset 101", pos1)
	}
	pos2 := bpt.Get([]byte("tx-c2"))
	if pos2 == nil || pos2.Fid != 2 || pos2.Offset != 202 {
		t.Fatalf("Get(tx-c2) = %v, want fid 2 offset 202", pos2)
	}
}

func TestBPlusTreeTransactionRollback(t *testing.T) {
	bpt := openTestBPlusTree(t)

	err := bpt.tree.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bpt.bkt)
		if bucket == nil {
			return errors.New("bucket not found")
		}
		if err := bucket.Put([]byte("tx-r1"), data.EncodeLogRecordPos(&data.LogRecordPos{Fid: 3, Offset: 303})); err != nil {
			return err
		}
		if err := bucket.Put([]byte("tx-r2"), data.EncodeLogRecordPos(&data.LogRecordPos{Fid: 4, Offset: 404})); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	if err == nil {
		t.Fatalf("transaction rollback error = nil, want non-nil")
	}

	if bpt.Get([]byte("tx-r1")) != nil {
		t.Fatalf("Get(tx-r1) after rollback = non-nil, want nil")
	}
	if bpt.Get([]byte("tx-r2")) != nil {
		t.Fatalf("Get(tx-r2) after rollback = non-nil, want nil")
	}
}
