package redis

import (
	"testing"

	"kvix/common"
)

func testRedisOptions(dir string) common.Options {
	return common.Options{
		DirPath:      dir,
		DataFileSize: 1024 * 1024,
		IndexType:    common.BTreeIndex,
		SyncWrites:   true,
	}
}

func openTestRedisStore(t *testing.T) *RedisDataStore {
	t.Helper()
	store, err := NewRedisDataStore(testRedisOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("openTestRedisStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close redis store: %v", err)
		}
	})
	return store
}

func openTestRedisStoreWithDir(t *testing.T, dir string) *RedisDataStore {
	t.Helper()
	store, err := NewRedisDataStore(testRedisOptions(dir))
	if err != nil {
		t.Fatalf("openTestRedisStoreWithDir: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close redis store: %v", err)
		}
	})
	return store
}
