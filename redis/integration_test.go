package redis

import (
	"errors"
	"testing"
	"time"

	"kvix/common"
)

func TestRedisStoreReopenPersistsData(t *testing.T) {
	dir := t.TempDir()

	store, err := NewRedisDataStore(testRedisOptions(dir))
	if err != nil {
		t.Fatalf("NewRedisDataStore() error = %v", err)
	}

	if err := store.Set([]byte("name"), []byte("alice"), 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if _, err := store.HSet([]byte("user:1"), []byte("name"), []byte("tom")); err != nil {
		t.Fatalf("HSet() error = %v", err)
	}
	if _, err := store.RPush([]byte("tasks"), []byte("a"), []byte("b")); err != nil {
		t.Fatalf("RPush() error = %v", err)
	}
	if _, err := store.SAdd([]byte("tags"), []byte("go"), []byte("bitcask")); err != nil {
		t.Fatalf("SAdd() error = %v", err)
	}
	if _, err := store.ZAdd([]byte("rank"), 9.5, []byte("alice")); err != nil {
		t.Fatalf("ZAdd() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := NewRedisDataStore(testRedisOptions(dir))
	if err != nil {
		t.Fatalf("NewRedisDataStore(reopen) error = %v", err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("reopened.Close() error = %v", err)
		}
	}()

	value, err := reopened.Get([]byte("name"))
	if err != nil {
		t.Fatalf("Get() after reopen error = %v", err)
	}
	if string(value) != "alice" {
		t.Fatalf("Get() after reopen = %q, want %q", value, "alice")
	}

	field, err := reopened.HGet([]byte("user:1"), []byte("name"))
	if err != nil {
		t.Fatalf("HGet() after reopen error = %v", err)
	}
	if string(field) != "tom" {
		t.Fatalf("HGet() after reopen = %q, want %q", field, "tom")
	}

	listValues, err := reopened.LRange([]byte("tasks"), 0, -1)
	if err != nil {
		t.Fatalf("LRange() after reopen error = %v", err)
	}
	assertEqualLists(t, listValues, [][]byte{[]byte("a"), []byte("b")})

	members, err := reopened.SMembers([]byte("tags"))
	if err != nil {
		t.Fatalf("SMembers() after reopen error = %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("SMembers() after reopen len = %d, want 2", len(members))
	}

	score, err := reopened.ZScore([]byte("rank"), []byte("alice"))
	if err != nil {
		t.Fatalf("ZScore() after reopen error = %v", err)
	}
	if score != 9.5 {
		t.Fatalf("ZScore() after reopen = %g, want 9.5", score)
	}
}

func TestRedisStoreReopenPreservesDeletionAndExpiration(t *testing.T) {
	dir := t.TempDir()

	store, err := NewRedisDataStore(testRedisOptions(dir))
	if err != nil {
		t.Fatalf("NewRedisDataStore() error = %v", err)
	}

	if err := store.Set([]byte("deleted"), []byte("gone"), 0); err != nil {
		t.Fatalf("Set(deleted) error = %v", err)
	}
	if deleted, err := store.Del([]byte("deleted")); err != nil {
		t.Fatalf("Del() error = %v", err)
	} else if !deleted {
		t.Fatal("Del() = false, want true")
	}

	if err := store.Set([]byte("expired"), []byte("soon"), 20*time.Millisecond); err != nil {
		t.Fatalf("Set(expired) error = %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := NewRedisDataStore(testRedisOptions(dir))
	if err != nil {
		t.Fatalf("NewRedisDataStore(reopen) error = %v", err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("reopened.Close() error = %v", err)
		}
	}()

	if _, err := reopened.Get([]byte("deleted")); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("Get(deleted) after reopen error = %v, want ErrKeyNotFound", err)
	}
	if _, err := reopened.Get([]byte("expired")); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("Get(expired) after reopen error = %v, want ErrKeyNotFound", err)
	}
}
