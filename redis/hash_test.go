package redis

import (
	"errors"
	"testing"
	"time"

	"kvix/common"
)

func TestHSetHGetAndHLen(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("hash:profile")

	added, err := store.HSet(key, []byte("name"), []byte("alice"))
	if err != nil {
		t.Fatalf("HSet() error = %v", err)
	}
	if !added {
		t.Fatalf("HSet() = false, want true for new field")
	}

	value, err := store.HGet(key, []byte("name"))
	if err != nil {
		t.Fatalf("HGet() error = %v", err)
	}
	if string(value) != "alice" {
		t.Fatalf("HGet() = %q, want %q", value, "alice")
	}

	length, err := store.HLen(key)
	if err != nil {
		t.Fatalf("HLen() error = %v", err)
	}
	if length != 1 {
		t.Fatalf("HLen() = %d, want 1", length)
	}

	added, err = store.HSet(key, []byte("age"), []byte("30"))
	if err != nil {
		t.Fatalf("HSet(age) error = %v", err)
	}
	if !added {
		t.Fatalf("HSet(age) = false, want true for new field")
	}

	length, err = store.HLen(key)
	if err != nil {
		t.Fatalf("HLen() error = %v", err)
	}
	if length != 2 {
		t.Fatalf("HLen() = %d, want 2", length)
	}
}

func TestHSetOverwriteDoesNotIncreaseLen(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("hash:overwrite")

	if _, err := store.HSet(key, []byte("label"), []byte("v1")); err != nil {
		t.Fatalf("HSet() error = %v", err)
	}
	if length, err := store.HLen(key); err != nil || length != 1 {
		t.Fatalf("HLen() = %d, %v; want 1, nil", length, err)
	}

	added, err := store.HSet(key, []byte("label"), []byte("v2"))
	if err != nil {
		t.Fatalf("HSet(overwrite) error = %v", err)
	}
	if added {
		t.Fatalf("HSet(overwrite) = true, want false for existing field")
	}

	length, err := store.HLen(key)
	if err != nil {
		t.Fatalf("HLen() error = %v", err)
	}
	if length != 1 {
		t.Fatalf("HLen() = %d, want 1 after overwrite", length)
	}

	value, err := store.HGet(key, []byte("label"))
	if err != nil {
		t.Fatalf("HGet() error = %v", err)
	}
	if string(value) != "v2" {
		t.Fatalf("HGet() = %q, want %q", value, "v2")
	}
}

func TestHashDeleteAndExists(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("hash:delete")

	if _, err := store.HSet(key, []byte("token"), []byte("abc")); err != nil {
		t.Fatalf("HSet() error = %v", err)
	}

	ok, err := store.HExists(key, []byte("token"))
	if err != nil {
		t.Fatalf("HExists() error = %v", err)
	}
	if !ok {
		t.Fatalf("HExists() = false, want true")
	}

	deleted, err := store.HDel(key, []byte("token"))
	if err != nil {
		t.Fatalf("HDel() error = %v", err)
	}
	if !deleted {
		t.Fatalf("HDel() = false, want true for existing field")
	}

	ok, err = store.HExists(key, []byte("token"))
	if err != nil {
		t.Fatalf("HExists() error = %v", err)
	}
	if ok {
		t.Fatalf("HExists() = true, want false after delete")
	}

	length, err := store.HLen(key)
	if err != nil {
		t.Fatalf("HLen() error = %v", err)
	}
	if length != 0 {
		t.Fatalf("HLen() = %d, want 0 after deleting all fields", length)
	}

	if _, err := store.db.Get(metaKey(key)); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("meta key still exists after deleting last field: %v", err)
	}
}

func TestHashWrongType(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("hash:wrong")

	if err := store.db.Put(metaKey(key), encodeMetadata(metadata{typ: redisTypeString, version: 1})); err != nil {
		t.Fatalf("put wrong type metadata: %v", err)
	}

	if _, err := store.HSet(key, []byte("field"), []byte("value")); err != ErrWrongType {
		t.Fatalf("HSet() = %v, want ErrWrongType", err)
	}

	if _, err := store.HGet(key, []byte("field")); err != ErrWrongType {
		t.Fatalf("HGet() = %v, want ErrWrongType", err)
	}
}

func TestHashLazyExpiration(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("hash:expired")
	expiredMeta := metadata{
		typ:      redisTypeHash,
		version:  3,
		expireAt: time.Now().Add(-time.Minute).UnixNano(),
		size:     1,
	}
	if err := store.db.Put(metaKey(key), encodeMetadata(expiredMeta)); err != nil {
		t.Fatalf("put expired metadata: %v", err)
	}
	if err := store.db.Put(hashDataKey(key, expiredMeta.version, []byte("name")), []byte("alice")); err != nil {
		t.Fatalf("put old field: %v", err)
	}

	if _, err := store.HGet(key, []byte("name")); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("HGet() after expiration error = %v, want ErrKeyNotFound", err)
	}

	if _, err := store.db.Get(metaKey(key)); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("expired metadata still exists: %v", err)
	}

	if length, err := store.HLen(key); err != nil || length != 0 {
		t.Fatalf("HLen() after expiration = %d, %v; want 0, nil", length, err)
	}
}

func TestHSetUsesNewVersionWhenReplacingExpiredHash(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("hash:version")
	oldMeta := metadata{
		typ:      redisTypeHash,
		version:  5,
		expireAt: time.Now().Add(-time.Minute).UnixNano(),
		size:     1,
	}
	if err := store.db.Put(metaKey(key), encodeMetadata(oldMeta)); err != nil {
		t.Fatalf("put old metadata: %v", err)
	}
	if err := store.db.Put(hashDataKey(key, oldMeta.version, []byte("field")), []byte("old")); err != nil {
		t.Fatalf("put old field: %v", err)
	}

	added, err := store.HSet(key, []byte("fresh"), []byte("new"))
	if err != nil {
		t.Fatalf("HSet() error = %v", err)
	}
	if !added {
		t.Fatalf("HSet() = false, want true for new field after expiration")
	}

	rawMeta, err := store.db.Get(metaKey(key))
	if err != nil {
		t.Fatalf("get current metadata: %v", err)
	}
	currentMeta, err := decodeMetadata(rawMeta)
	if err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if currentMeta.version <= oldMeta.version {
		t.Fatalf("metadata version = %d, want > %d", currentMeta.version, oldMeta.version)
	}
	if length, err := store.HLen(key); err != nil || length != 1 {
		t.Fatalf("HLen() = %d, %v; want 1", length, err)
	}

	if _, err := store.db.Get(hashDataKey(key, currentMeta.version, []byte("fresh"))); err != nil {
		t.Fatalf("new field missing after HSet: %v", err)
	}
}
