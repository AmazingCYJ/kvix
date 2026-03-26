package index

import (
	"bitcask-my/data"
	"testing"
)

func putTestItem(t *testing.T, bt *BTree, key string, fid uint32, offset int64) {
	t.Helper()
	oldPos := bt.Put([]byte(key), &data.LogRecordPos{Fid: fid, Offset: offset})
	if oldPos != nil {
		t.Fatalf("Put(%q) oldPos = %v, want nil", key, oldPos)
	}
}

func TestBTreeIteratorForward(t *testing.T) {
	bt := NewBTree()
	putTestItem(t, bt, "c", 3, 30)
	putTestItem(t, bt, "a", 1, 10)
	putTestItem(t, bt, "b", 2, 20)

	it := bt.Iterator(false)
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

func TestBTreeIteratorReverse(t *testing.T) {
	bt := NewBTree()
	putTestItem(t, bt, "a", 1, 10)
	putTestItem(t, bt, "b", 2, 20)
	putTestItem(t, bt, "c", 3, 30)

	it := bt.Iterator(true)
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

func TestBTreeIteratorSeekForward(t *testing.T) {
	bt := NewBTree()
	putTestItem(t, bt, "a", 1, 10)
	putTestItem(t, bt, "c", 3, 30)
	putTestItem(t, bt, "e", 5, 50)

	it := bt.Iterator(false)

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

func TestBTreeIteratorSeekReverse(t *testing.T) {
	bt := NewBTree()
	putTestItem(t, bt, "a", 1, 10)
	putTestItem(t, bt, "c", 3, 30)
	putTestItem(t, bt, "e", 5, 50)

	it := bt.Iterator(true)

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

func TestBTreeIteratorRewindAndClose(t *testing.T) {
	bt := NewBTree()
	putTestItem(t, bt, "a", 1, 10)
	putTestItem(t, bt, "b", 2, 20)

	it := bt.Iterator(false)
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
