package kvix

import (
	"errors"
	"kvix/common"
	"testing"
)

func TestWriteBatchPutCommit(t *testing.T) {
	db, err := Open(testOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	wb := db.NewWriteBatch(common.DefaultWriteBatchOptions)
	if err := wb.Put([]byte("k1"), []byte("v1")); err != nil {
		t.Fatalf("Put(k1) error = %v", err)
	}
	if err := wb.Put([]byte("k2"), []byte("v2")); err != nil {
		t.Fatalf("Put(k2) error = %v", err)
	}
	if err := wb.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	v1, err := db.Get([]byte("k1"))
	if err != nil {
		t.Fatalf("Get(k1) error = %v", err)
	}
	if string(v1) != "v1" {
		t.Fatalf("Get(k1) = %q, want %q", v1, "v1")
	}

	v2, err := db.Get([]byte("k2"))
	if err != nil {
		t.Fatalf("Get(k2) error = %v", err)
	}
	if string(v2) != "v2" {
		t.Fatalf("Get(k2) = %q, want %q", v2, "v2")
	}
}

func TestWriteBatchDeleteCommit(t *testing.T) {
	db, err := Open(testOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := db.Put([]byte("name"), []byte("alice")); err != nil {
		t.Fatalf("db.Put(name) error = %v", err)
	}

	wb := db.NewWriteBatch(common.DefaultWriteBatchOptions)
	if err := wb.Delete([]byte("name")); err != nil {
		t.Fatalf("Delete(name) error = %v", err)
	}
	if err := wb.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	if _, err := db.Get([]byte("name")); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("Get(name) error = %v, want ErrKeyNotFound", err)
	}
}

func TestWriteBatchCommitTooLarge(t *testing.T) {
	db, err := Open(testOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	wb := db.NewWriteBatch(common.WriteBatchOptions{MaxBatchSize: 1, SyncWrite: false})
	if err := wb.Put([]byte("k1"), []byte("v1")); err != nil {
		t.Fatalf("Put(k1) error = %v", err)
	}
	if err := wb.Put([]byte("k2"), []byte("v2")); err != nil {
		t.Fatalf("Put(k2) error = %v", err)
	}

	err = wb.Commit()
	if !errors.Is(err, common.ErrBatchTooLarge) {
		t.Fatalf("Commit() error = %v, want ErrBatchTooLarge", err)
	}

	if _, err := db.Get([]byte("k1")); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("Get(k1) error = %v, want ErrKeyNotFound", err)
	}
	if _, err := db.Get([]byte("k2")); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("Get(k2) error = %v, want ErrKeyNotFound", err)
	}
}

func TestWriteBatchEmptyCommit(t *testing.T) {
	db, err := Open(testOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	wb := db.NewWriteBatch(common.DefaultWriteBatchOptions)
	if err := wb.Commit(); err != nil {
		t.Fatalf("Commit(empty) error = %v", err)
	}
}

func TestWriteBatchDeleteMissingKey(t *testing.T) {
	db, err := Open(testOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	wb := db.NewWriteBatch(common.DefaultWriteBatchOptions)
	err = wb.Delete([]byte("missing"))
	// 删除不存在的 key 应返回 nil（幂等语义），与 DB.Delete 行为一致。
	if err != nil {
		t.Fatalf("Delete(missing) error = %v, want nil", err)
	}
}

func TestWriteBatchCommitStateAfterApply(t *testing.T) {
	db, err := Open(testOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := db.Put([]byte("keep"), []byte("v0")); err != nil {
		t.Fatalf("db.Put(keep) error = %v", err)
	}

	wb := db.NewWriteBatch(common.DefaultWriteBatchOptions)
	if err := wb.Put([]byte("k1"), []byte("v1")); err != nil {
		t.Fatalf("wb.Put(k1) error = %v", err)
	}
	if err := wb.Put([]byte("k2"), []byte("v2")); err != nil {
		t.Fatalf("wb.Put(k2) error = %v", err)
	}
	if err := wb.Delete([]byte("keep")); err != nil {
		t.Fatalf("wb.Delete(keep) error = %v", err)
	}

	seqBefore := db.seqNo
	if err := wb.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	if db.seqNo != seqBefore+1 {
		t.Fatalf("db.seqNo = %d, want %d", db.seqNo, seqBefore+1)
	}
	if len(wb.pendingWrites) != 0 {
		t.Fatalf("pendingWrites len = %d, want 0", len(wb.pendingWrites))
	}

	v1, err := db.Get([]byte("k1"))
	if err != nil {
		t.Fatalf("db.Get(k1) error = %v", err)
	}
	if string(v1) != "v1" {
		t.Fatalf("db.Get(k1) = %q, want %q", v1, "v1")
	}

	v2, err := db.Get([]byte("k2"))
	if err != nil {
		t.Fatalf("db.Get(k2) error = %v", err)
	}
	if string(v2) != "v2" {
		t.Fatalf("db.Get(k2) = %q, want %q", v2, "v2")
	}

	if _, err := db.Get([]byte("keep")); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("db.Get(keep) error = %v, want ErrKeyNotFound", err)
	}
}

func TestWriteBatchReopenLoadsCommittedKeys(t *testing.T) {
	dir := t.TempDir()
	opts := testOptions(dir)

	db1, err := Open(opts)
	if err != nil {
		t.Fatalf("Open(first) error = %v", err)
	}

	wb := db1.NewWriteBatch(common.DefaultWriteBatchOptions)
	if err := wb.Put([]byte("txn:k1"), []byte("v1")); err != nil {
		t.Fatalf("wb.Put(txn:k1) error = %v", err)
	}
	if err := wb.Put([]byte("txn:k2"), []byte("v2")); err != nil {
		t.Fatalf("wb.Put(txn:k2) error = %v", err)
	}
	if err := wb.Commit(); err != nil {
		t.Fatalf("wb.Commit() error = %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}

	db2, err := Open(opts)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}

	v1, err := db2.Get([]byte("txn:k1"))
	if err != nil {
		t.Fatalf("db2.Get(txn:k1) error = %v", err)
	}
	if string(v1) != "v1" {
		t.Fatalf("db2.Get(txn:k1) = %q, want %q", v1, "v1")
	}

	v2, err := db2.Get([]byte("txn:k2"))
	if err != nil {
		t.Fatalf("db2.Get(txn:k2) error = %v", err)
	}
	if string(v2) != "v2" {
		t.Fatalf("db2.Get(txn:k2) = %q, want %q", v2, "v2")
	}
}

func TestWriteBatchOnFreshBPlusTreeDir(t *testing.T) {
	opts := common.DefaultOptions
	opts.DirPath = t.TempDir()
	opts.SyncWrites = true

	db, err := Open(opts)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	wb := db.NewWriteBatch(common.DefaultWriteBatchOptions)
	if err := wb.Put([]byte("fresh"), []byte("value")); err != nil {
		t.Fatalf("wb.Put(fresh) error = %v", err)
	}
	if err := wb.Commit(); err != nil {
		t.Fatalf("wb.Commit() error = %v", err)
	}

	value, err := db.Get([]byte("fresh"))
	if err != nil {
		t.Fatalf("db.Get(fresh) error = %v", err)
	}
	if string(value) != "value" {
		t.Fatalf("db.Get(fresh) = %q, want %q", value, "value")
	}
}
