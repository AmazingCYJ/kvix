package kvix

import (
	"errors"
	"kvix/common"
	"kvix/data"
	"testing"
)

func testOptions(dir string) common.Options {
	return common.Options{
		DirPath:      dir,
		DataFileSize: 1024 * 1024,
		IndexType:    common.BTreeIndex,
		SyncWrites:   true,
	}
}

func TestCheckOptions(t *testing.T) {
	tests := []struct {
		name    string
		opt     common.Options
		wantErr bool
	}{
		{
			name: "empty dir path",
			opt: common.Options{
				DirPath:      "",
				DataFileSize: 1024,
			},
			wantErr: true,
		},
		{
			name: "invalid data file size",
			opt: common.Options{
				DirPath:      t.TempDir(),
				DataFileSize: 0,
			},
			wantErr: true,
		},
		{
			name:    "valid options",
			opt:     testOptions(t.TempDir()),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkOptions(tt.opt)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkOptions() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOpenAndBasicCRUD(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(testOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := db.Put([]byte{}, []byte("v")); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("Put(empty key) error = %v, want ErrKeyNotFound", err)
	}

	if err := db.Put([]byte("name"), []byte("alice")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	v, err := db.Get([]byte("name"))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(v) != "alice" {
		t.Fatalf("Get() = %q, want %q", v, "alice")
	}

	if err := db.Put([]byte("name"), []byte("bob")); err != nil {
		t.Fatalf("Put(overwrite) error = %v", err)
	}
	v, err = db.Get([]byte("name"))
	if err != nil {
		t.Fatalf("Get(overwrite) error = %v", err)
	}
	if string(v) != "bob" {
		t.Fatalf("Get(overwrite) = %q, want %q", v, "bob")
	}

	if err := db.Delete([]byte("name")); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := db.Get([]byte("name")); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("Get(after delete) error = %v, want ErrKeyNotFound", err)
	}

	if err := db.Delete([]byte("missing")); err != nil {
		t.Fatalf("Delete(missing) error = %v, want nil", err)
	}

	if _, err := db.Get([]byte{}); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("Get(empty key) error = %v, want ErrKeyNotFound", err)
	}
}

func TestOpenReloadIndexFromDataFiles(t *testing.T) {
	dir := t.TempDir()
	opts := testOptions(dir)

	db1, err := Open(opts)
	if err != nil {
		t.Fatalf("Open(first) error = %v", err)
	}
	if err := db1.Put([]byte("k1"), []byte("v1")); err != nil {
		t.Fatalf("Put(k1) error = %v", err)
	}
	if err := db1.Put([]byte("k2"), []byte("v2")); err != nil {
		t.Fatalf("Put(k2) error = %v", err)
	}
	if err := db1.Delete([]byte("k2")); err != nil {
		t.Fatalf("Delete(k2) error = %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}

	db2, err := Open(opts)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}

	v, err := db2.Get([]byte("k1"))
	if err != nil {
		t.Fatalf("Get(k1) after reopen error = %v", err)
	}
	if string(v) != "v1" {
		t.Fatalf("Get(k1) after reopen = %q, want %q", v, "v1")
	}

	if _, err := db2.Get([]byte("k2")); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("Get(k2) after reopen error = %v, want ErrKeyNotFound", err)
	}
}

func TestOpenBPlusTreeWithMMapAtStartupCanWriteAfterReopen(t *testing.T) {
	dir := t.TempDir()
	opts := common.Options{
		DirPath:       dir,
		DataFileSize:  1024 * 1024,
		IndexType:     common.BPlusTreeIndex,
		SyncWrites:    true,
		MMapAtStartup: true,
	}

	db1, err := Open(opts)
	if err != nil {
		t.Fatalf("Open(first) error = %v", err)
	}
	if err := db1.Put([]byte("k1"), []byte("v1")); err != nil {
		t.Fatalf("Put(first) error = %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}

	db2, err := Open(opts)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	defer func() { _ = db2.Close() }()

	if err := db2.Put([]byte("k2"), []byte("v2")); err != nil {
		t.Fatalf("Put(after reopen) error = %v", err)
	}

	value, err := db2.Get([]byte("k2"))
	if err != nil {
		t.Fatalf("Get(k2) error = %v", err)
	}
	if string(value) != "v2" {
		t.Fatalf("Get(k2) = %q, want %q", string(value), "v2")
	}
}

func TestIteratorForward(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(testOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	kvPairs := []struct {
		key   string
		value string
	}{
		{"c", "value_c"},
		{"a", "value_a"},
		{"b", "value_b"},
	}

	for _, kv := range kvPairs {
		if err := db.Put([]byte(kv.key), []byte(kv.value)); err != nil {
			t.Fatalf("Put(%q) error = %v", kv.key, err)
		}
	}

	it := db.NewIterator(common.DefaultIteratorOptions)
	defer it.Close()

	var gotKeys []string
	var gotValues []string
	for it.Rewind(); it.Vaild(); it.Next() {
		key := it.Key()
		value, err := it.Value()
		if err != nil {
			t.Fatalf("Value() error = %v", err)
		}
		gotKeys = append(gotKeys, string(key))
		gotValues = append(gotValues, string(value))
	}

	wantKeys := []string{"a", "b", "c"}
	wantValues := []string{"value_a", "value_b", "value_c"}

	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("len(keys) = %d, want %d", len(gotKeys), len(wantKeys))
	}
	for i := range wantKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Fatalf("key[%d] = %q, want %q", i, gotKeys[i], wantKeys[i])
		}
		if gotValues[i] != wantValues[i] {
			t.Fatalf("value[%d] = %q, want %q", i, gotValues[i], wantValues[i])
		}
	}
}

func TestIteratorReverse(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(testOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	kvPairs := []struct {
		key   string
		value string
	}{
		{"a", "value_a"},
		{"b", "value_b"},
		{"c", "value_c"},
	}

	for _, kv := range kvPairs {
		if err := db.Put([]byte(kv.key), []byte(kv.value)); err != nil {
			t.Fatalf("Put(%q) error = %v", kv.key, err)
		}
	}

	opts := common.IteratorOptions{Reverse: true}
	it := db.NewIterator(opts)
	defer it.Close()

	var gotKeys []string
	for it.Rewind(); it.Vaild(); it.Next() {
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

func TestIteratorWithPrefix(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(testOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	kvPairs := []struct {
		key   string
		value string
	}{
		{"user:1", "alice"},
		{"user:2", "bob"},
		{"post:1", "hello"},
		{"post:2", "world"},
		{"user:3", "charlie"},
	}

	for _, kv := range kvPairs {
		if err := db.Put([]byte(kv.key), []byte(kv.value)); err != nil {
			t.Fatalf("Put(%q) error = %v", kv.key, err)
		}
	}

	opts := common.IteratorOptions{Prefix: []byte("user:")}
	it := db.NewIterator(opts)
	defer it.Close()

	var gotKeys []string
	for it.Rewind(); it.Vaild(); it.Next() {
		gotKeys = append(gotKeys, string(it.Key()))
	}

	wantKeys := []string{"user:1", "user:2", "user:3"}
	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("len(keys with prefix) = %d, want %d", len(gotKeys), len(wantKeys))
	}
	for i := range wantKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Fatalf("key[%d] = %q, want %q", i, gotKeys[i], wantKeys[i])
		}
	}
}

func TestIteratorSeek(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(testOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	kvPairs := []struct {
		key string
	}{
		{"a"},
		{"c"},
		{"e"},
	}

	for _, kv := range kvPairs {
		if err := db.Put([]byte(kv.key), []byte("value")); err != nil {
			t.Fatalf("Put(%q) error = %v", kv.key, err)
		}
	}

	opts := common.DefaultIteratorOptions
	it := db.NewIterator(opts)
	defer it.Close()

	// Seek to "c", should find it
	it.Seek([]byte("c"))
	if !it.Vaild() || string(it.Key()) != "c" {
		t.Fatalf("Seek(c) key = %q, valid = %v, want key c and valid true", it.Key(), it.Vaild())
	}

	// Seek to "b", should find closest >= "b" which is "c"
	it.Seek([]byte("b"))
	if !it.Vaild() || string(it.Key()) != "c" {
		t.Fatalf("Seek(b) key = %q, valid = %v, want key c and valid true", it.Key(), it.Vaild())
	}

	// Seek to "z", should be invalid
	it.Seek([]byte("z"))
	if it.Vaild() {
		t.Fatalf("Seek(z) valid = true, want false")
	}
}

func TestListKeys(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(testOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Test empty database
	keys, err := db.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys() on empty db error = %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("ListKeys() on empty db = %d keys, want 0", len(keys))
	}

	// Add some key-value pairs
	kvPairs := []struct {
		key   string
		value string
	}{
		{"z", "value_z"},
		{"a", "value_a"},
		{"m", "value_m"},
		{"b", "value_b"},
	}

	for _, kv := range kvPairs {
		if err := db.Put([]byte(kv.key), []byte(kv.value)); err != nil {
			t.Fatalf("Put(%q) error = %v", kv.key, err)
		}
	}

	// List all keys
	keys, err = db.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys() error = %v", err)
	}

	wantKeys := []string{"a", "b", "m", "z"}
	if len(keys) != len(wantKeys) {
		t.Fatalf("ListKeys() len = %d, want %d", len(keys), len(wantKeys))
	}
	for i := range wantKeys {
		if string(keys[i]) != wantKeys[i] {
			t.Fatalf("key[%d] = %q, want %q", i, keys[i], wantKeys[i])
		}
	}

	// Delete one key and check again
	if err := db.Delete([]byte("m")); err != nil {
		t.Fatalf("Delete(m) error = %v", err)
	}
	keys, err = db.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys() after delete error = %v", err)
	}

	wantKeysAfterDelete := []string{"a", "b", "z"}
	if len(keys) != len(wantKeysAfterDelete) {
		t.Fatalf("ListKeys() after delete len = %d, want %d", len(keys), len(wantKeysAfterDelete))
	}
	for i := range wantKeysAfterDelete {
		if string(keys[i]) != wantKeysAfterDelete[i] {
			t.Fatalf("key[%d] = %q, want %q", i, keys[i], wantKeysAfterDelete[i])
		}
	}
}

func TestFold(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(testOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Test empty database
	callCount := 0
	err = db.Fold(func(key, value []byte) bool {
		callCount++
		return true
	})
	if err != nil {
		t.Fatalf("Fold() on empty db error = %v", err)
	}
	if callCount != 0 {
		t.Fatalf("Fold() on empty db callback count = %d, want 0", callCount)
	}

	// Add some key-value pairs
	kvPairs := []struct {
		key   string
		value string
	}{
		{"c", "value_c"},
		{"a", "value_a"},
		{"b", "value_b"},
	}

	for _, kv := range kvPairs {
		if err := db.Put([]byte(kv.key), []byte(kv.value)); err != nil {
			t.Fatalf("Put(%q) error = %v", kv.key, err)
		}
	}

	// Fold over all pairs
	var gotKeys []string
	var gotValues []string
	err = db.Fold(func(key, value []byte) bool {
		gotKeys = append(gotKeys, string(key))
		gotValues = append(gotValues, string(value))
		return true
	})
	if err != nil {
		t.Fatalf("Fold() error = %v", err)
	}

	wantKeys := []string{"a", "b", "c"}
	wantValues := []string{"value_a", "value_b", "value_c"}
	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("Fold() len = %d, want %d", len(gotKeys), len(wantKeys))
	}
	for i := range wantKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Fatalf("key[%d] = %q, want %q", i, gotKeys[i], wantKeys[i])
		}
		if gotValues[i] != wantValues[i] {
			t.Fatalf("value[%d] = %q, want %q", i, gotValues[i], wantValues[i])
		}
	}

	// Test early stop: return false on second key
	gotKeys = nil
	err = db.Fold(func(key, value []byte) bool {
		gotKeys = append(gotKeys, string(key))
		return len(gotKeys) < 2
	})
	if err != nil {
		t.Fatalf("Fold() with early stop error = %v", err)
	}
	if len(gotKeys) != 2 {
		t.Fatalf("Fold() with early stop len = %d, want 2", len(gotKeys))
	}
	if gotKeys[0] != "a" || gotKeys[1] != "b" {
		t.Fatalf("Fold() with early stop keys = %v, want [a b]", gotKeys)
	}
}

func TestOpenFileLockExclusive(t *testing.T) {
	dir := t.TempDir()
	opts := testOptions(dir)

	db1, err := Open(opts)
	if err != nil {
		t.Fatalf("Open(first) error = %v", err)
	}
	t.Cleanup(func() {
		_ = db1.Close()
	})

	_, err = Open(opts)
	if !errors.Is(err, common.ErrDataBaseIsUsing) {
		t.Fatalf("Open(second) error = %v, want ErrDataBaseIsUsing", err)
	}
}

func TestOpenFileLockReleaseAfterClose(t *testing.T) {
	dir := t.TempDir()
	opts := testOptions(dir)

	db1, err := Open(opts)
	if err != nil {
		t.Fatalf("Open(first) error = %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}

	db2, err := Open(opts)
	if err != nil {
		t.Fatalf("Open(second) after close error = %v", err)
	}
	defer func() {
		_ = db2.Close()
	}()
}

func TestStatTracksOldDataOnPutAndDelete(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(testOptions(dir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	key := []byte("name")
	if err := db.Put(key, []byte("alice")); err != nil {
		t.Fatalf("Put(first) error = %v", err)
	}

	st1 := db.Stat()
	if st1.KeyNum != 1 {
		t.Fatalf("Stat().KeyNum after first put = %d, want 1", st1.KeyNum)
	}
	if st1.ReclaimbleSize != 0 {
		t.Fatalf("Stat().ReclaimbleSize after first put = %d, want 0", st1.ReclaimbleSize)
	}

	posBeforeOverwrite := db.index.Get(key)
	if posBeforeOverwrite == nil {
		t.Fatalf("index.Get(name) before overwrite = nil")
	}

	if err := db.Put(key, []byte("bob")); err != nil {
		t.Fatalf("Put(overwrite) error = %v", err)
	}

	st2 := db.Stat()
	if st2.KeyNum != 1 {
		t.Fatalf("Stat().KeyNum after overwrite = %d, want 1", st2.KeyNum)
	}
	putDelta := st2.ReclaimbleSize - st1.ReclaimbleSize
	if putDelta != int64(posBeforeOverwrite.Size) {
		t.Fatalf("reclaim delta after overwrite = %d, want %d", putDelta, posBeforeOverwrite.Size)
	}

	posBeforeDelete := db.index.Get(key)
	if posBeforeDelete == nil {
		t.Fatalf("index.Get(name) before delete = nil")
	}

	deleteRecord := &data.LogRecord{
		Key:  logRecordKeyWithSeq(key, nonTransactionSeqNo),
		Type: data.LogRecordDeleted,
	}
	_, deleteRecordSize := data.EncodeLogRecord(deleteRecord)

	if err := db.Delete(key); err != nil {
		t.Fatalf("Delete(name) error = %v", err)
	}

	st3 := db.Stat()
	if st3.KeyNum != 0 {
		t.Fatalf("Stat().KeyNum after delete = %d, want 0", st3.KeyNum)
	}
	deleteDelta := st3.ReclaimbleSize - st2.ReclaimbleSize
	wantDeleteDelta := int64(posBeforeDelete.Size) + deleteRecordSize
	if deleteDelta != wantDeleteDelta {
		t.Fatalf("reclaim delta after delete = %d, want %d", deleteDelta, wantDeleteDelta)
	}
}
