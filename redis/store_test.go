package redis

import (
	"testing"

	kvix "kvix"
	"kvix/common"
)

func TestNewRedisDataStoreFromDBDoesNotOwnSharedDB(t *testing.T) {
	opts := common.DefaultOptions
	opts.DirPath = t.TempDir()

	db, err := kvix.Open(opts)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	store := NewRedisDataStoreFromDB(db)
	if store == nil {
		t.Fatal("NewRedisDataStoreFromDB() = nil, want non-nil")
	}

	if err := store.Close(); err != nil {
		t.Fatalf("store.Close() error = %v", err)
	}

	if err := db.Put([]byte("shared-key"), []byte("shared-value")); err != nil {
		t.Fatalf("db.Put() after store.Close() error = %v, want nil", err)
	}
}
