package index

import (
	"bitcask-my/data"
	"testing"
)

func putARTTestItem(t *testing.T, art *AdaptiveRadixTree, key string, fid uint32, offset int64) {
	t.Helper()
	oldPos := art.Put([]byte(key), &data.LogRecordPos{Fid: fid, Offset: offset})
	if oldPos != nil {
		t.Fatalf("Put(%q) oldPos = %v, want nil", key, oldPos)
	}
}

func TestAdaptiveRadixTreeCRUDAndSize(t *testing.T) {
	art := NewARTree()

	if art.Size() != 0 {
		t.Fatalf("initial Size() = %d, want 0", art.Size())
	}

	putARTTestItem(t, art, "a", 1, 10)
	putARTTestItem(t, art, "b", 2, 20)
	putARTTestItem(t, art, "c", 3, 30)

	if art.Size() != 3 {
		t.Fatalf("Size() = %d, want 3", art.Size())
	}

	pos := art.Get([]byte("b"))
	if pos == nil || pos.Offset != 20 {
		t.Fatalf("Get(b) = %v, want offset 20", pos)
	}

	// 覆盖写入同一个 key，不应增长 Size。
	oldPos := art.Put([]byte("b"), &data.LogRecordPos{Fid: 22, Offset: 220})
	if oldPos == nil || oldPos.Fid != 2 || oldPos.Offset != 20 {
		t.Fatalf("Put(b) oldPos = %v, want fid 2 offset 20", oldPos)
	}
	if art.Size() != 3 {
		t.Fatalf("Size() after overwrite = %d, want 3", art.Size())
	}
	pos = art.Get([]byte("b"))
	if pos == nil || pos.Fid != 22 || pos.Offset != 220 {
		t.Fatalf("Get(b) after overwrite = %v, want fid 22 offset 220", pos)
	}

	oldPos, ok := art.Delete([]byte("b"))
	if !ok {
		t.Fatalf("Delete(b) ok = false, want true")
	}
	if oldPos == nil || oldPos.Fid != 22 || oldPos.Offset != 220 {
		t.Fatalf("Delete(b) oldPos = %v, want fid 22 offset 220", oldPos)
	}
	oldPos, ok = art.Delete([]byte("b"))
	if ok {
		t.Fatalf("Delete(b) second time ok = true, want false")
	}
	if oldPos != nil {
		t.Fatalf("Delete(b) second time oldPos = %v, want nil", oldPos)
	}
	if art.Get([]byte("b")) != nil {
		t.Fatalf("Get(b) after delete = non-nil, want nil")
	}
	if art.Size() != 2 {
		t.Fatalf("Size() after delete = %d, want 2", art.Size())
	}
}

func TestARTIteratorForward(t *testing.T) {
	art := NewARTree()
	putARTTestItem(t, art, "c", 3, 30)
	putARTTestItem(t, art, "a", 1, 10)
	putARTTestItem(t, art, "b", 2, 20)

	it := art.Iterator(false)
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

func TestARTIteratorReverse(t *testing.T) {
	art := NewARTree()
	putARTTestItem(t, art, "a", 1, 10)
	putARTTestItem(t, art, "b", 2, 20)
	putARTTestItem(t, art, "c", 3, 30)

	it := art.Iterator(true)
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

func TestARTIteratorSeekForward(t *testing.T) {
	art := NewARTree()
	putARTTestItem(t, art, "a", 1, 10)
	putARTTestItem(t, art, "c", 3, 30)
	putARTTestItem(t, art, "e", 5, 50)

	it := art.Iterator(false)

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

func TestARTIteratorSeekReverse(t *testing.T) {
	art := NewARTree()
	putARTTestItem(t, art, "a", 1, 10)
	putARTTestItem(t, art, "c", 3, 30)
	putARTTestItem(t, art, "e", 5, 50)

	it := art.Iterator(true)

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

func TestARTIteratorRewindAndClose(t *testing.T) {
	art := NewARTree()
	putARTTestItem(t, art, "a", 1, 10)
	putARTTestItem(t, art, "b", 2, 20)

	it := art.Iterator(false)
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
