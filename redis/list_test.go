package redis

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"bitcask-my/common"
)

func TestLPushRPushAndRange(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("list:range")

	if _, err := store.LPush(key, []byte("b"), []byte("a")); err != nil {
		t.Fatalf("LPush() error = %v", err)
	}
	if _, err := store.RPush(key, []byte("c")); err != nil {
		t.Fatalf("RPush() error = %v", err)
	}

	assertList(t, store, key, [][]byte{[]byte("a"), []byte("b"), []byte("c")})

	partial, err := store.LRange(key, 1, 2)
	if err != nil {
		t.Fatalf("LRange() error = %v", err)
	}
	assertEqualLists(t, partial, [][]byte{[]byte("b"), []byte("c")})
}

func TestLPopRPopAndLen(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("list:pop")

	if _, err := store.RPush(key, []byte("a"), []byte("b"), []byte("c")); err != nil {
		t.Fatalf("RPush() error = %v", err)
	}

	if length, err := store.LLen(key); err != nil || length != 3 {
		t.Fatalf("LLen() = %d, %v; want 3, nil", length, err)
	}

	body, err := store.LPop(key)
	if err != nil {
		t.Fatalf("LPop() error = %v", err)
	}
	if string(body) != "a" {
		t.Fatalf("LPop() = %q, want %q", body, "a")
	}

	if length, err := store.LLen(key); err != nil || length != 2 {
		t.Fatalf("LLen() = %d, %v; want 2, nil", length, err)
	}

	body, err = store.RPop(key)
	if err != nil {
		t.Fatalf("RPop() error = %v", err)
	}
	if string(body) != "c" {
		t.Fatalf("RPop() = %q, want %q", body, "c")
	}

	if length, err := store.LLen(key); err != nil || length != 1 {
		t.Fatalf("LLen() = %d, %v; want 1, nil", length, err)
	}

	body, err = store.LPop(key)
	if err != nil {
		t.Fatalf("Final LPop() error = %v", err)
	}
	if string(body) != "b" {
		t.Fatalf("Final LPop() = %q, want %q", body, "b")
	}

	if _, err = store.LPop(key); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("LPop() on empty list = %v, want ErrKeyNotFound", err)
	}
}

func TestListWrongType(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("list:wrong")

	if err := store.Set(key, []byte("value"), 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if _, err := store.LPush(key, []byte("x")); err != ErrWrongType {
		t.Fatalf("LPush() = %v, want ErrWrongType", err)
	}
	if _, err := store.LRange(key, 0, -1); err != ErrWrongType {
		t.Fatalf("LRange() = %v, want ErrWrongType", err)
	}
	if _, err := store.LLen(key); err != ErrWrongType {
		t.Fatalf("LLen() = %v, want ErrWrongType", err)
	}
	if _, err := store.LPop(key); err != ErrWrongType {
		t.Fatalf("LPop() = %v, want ErrWrongType", err)
	}
	if _, err := store.RPop(key); err != ErrWrongType {
		t.Fatalf("RPop() = %v, want ErrWrongType", err)
	}
}

func TestListLazyExpiration(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("list:expired")
	expired := metadata{
		typ:      redisTypeList,
		version:  1,
		expireAt: time.Now().Add(-time.Minute).UnixNano(),
		size:     1,
		head:     0,
		tail:     0,
	}
	if err := store.db.Put(metaKey(key), encodeMetadata(expired)); err != nil {
		t.Fatalf("put expired metadata: %v", err)
	}
	if err := store.db.Put(listDataKey(key, expired.version, expired.head), []byte("gone")); err != nil {
		t.Fatalf("put stale entry: %v", err)
	}

	if _, err := store.LPop(key); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("LPop() on expired list = %v, want ErrKeyNotFound", err)
	}
	if _, err := store.db.Get(metaKey(key)); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("expired metadata still exists: %v", err)
	}
}

func TestLPushOrderWithMultipleValues(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("list:order")

	if _, err := store.LPush(key, []byte("first"), []byte("second"), []byte("third")); err != nil {
		t.Fatalf("LPush() error = %v", err)
	}
	assertList(t, store, key, [][]byte{[]byte("third"), []byte("second"), []byte("first")})
}

func TestLRangeHandlesNegativeIndexes(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("list:neg")
	if _, err := store.RPush(key, []byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e")); err != nil {
		t.Fatalf("RPush() error = %v", err)
	}

	lastThree, err := store.LRange(key, -3, -1)
	if err != nil {
		t.Fatalf("LRange(-3,-1) error = %v", err)
	}
	assertEqualLists(t, lastThree, [][]byte{[]byte("c"), []byte("d"), []byte("e")})

	trimmed, err := store.LRange(key, 0, -2)
	if err != nil {
		t.Fatalf("LRange(0,-2) error = %v", err)
	}
	assertEqualLists(t, trimmed, [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")})

	empty, err := store.LRange(key, 3, 1)
	if err != nil {
		t.Fatalf("LRange(3,1) error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("LRange with start>stop = %d elements, want 0", len(empty))
	}
}

func TestListMetadataRemovedWhenEmpty(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("list:clean")

	if _, err := store.RPush(key, []byte("x"), []byte("y")); err != nil {
		t.Fatalf("RPush() error = %v", err)
	}

	if _, err := store.LPop(key); err != nil {
		t.Fatalf("LPop() error = %v", err)
	}
	if _, err := store.LPop(key); err != nil {
		t.Fatalf("LPop() error = %v", err)
	}

	if _, err := store.db.Get(metaKey(key)); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("metadata not deleted: %v", err)
	}

	if _, err := store.RPush(key, []byte("fresh")); err != nil {
		t.Fatalf("RPush() error after cleanup = %v", err)
	}
	ver, err := store.loadVersionNumber(key)
	if err != nil {
		t.Fatalf("loadVersionNumber() error = %v", err)
	}
	if ver == 0 {
		t.Fatalf("expected version tracker to exist, got %d", ver)
	}
}

func TestListNonexistentKeySemantics(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("list:missing")

	if length, err := store.LLen(key); err != nil || length != 0 {
		t.Fatalf("LLen() = %d, %v; want 0, nil", length, err)
	}
	if entries, err := store.LRange(key, 0, -1); err != nil || len(entries) != 0 {
		t.Fatalf("LRange() = %v, %v; want empty slice", entries, err)
	}
	if _, err := store.LPop(key); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("LPop() missing = %v, want ErrKeyNotFound", err)
	}
	if _, err := store.RPop(key); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("RPop() missing = %v, want ErrKeyNotFound", err)
	}
}

func assertList(t *testing.T, store *RedisDataStore, key []byte, want [][]byte) {
	t.Helper()
	got, err := store.LRange(key, 0, -1)
	if err != nil {
		t.Fatalf("LRange() error = %v", err)
	}
	assertEqualLists(t, got, want)
}

func assertEqualLists(t *testing.T, got, want [][]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("list length = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("element %d = %q, want %q", i, got[i], want[i])
		}
	}
}
