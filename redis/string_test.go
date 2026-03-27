package redis

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"kvix/common"
)

func TestSetGetAndTTL(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("name")
	value := []byte("alice")

	if err := store.Set(key, value, 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Fatalf("Get() = %q, want %q", got, value)
	}

	ttl, err := store.TTL(key)
	if err != nil {
		t.Fatalf("TTL() error = %v", err)
	}
	if ttl != time.Duration(-1) {
		t.Fatalf("TTL() = %v, want -1 for no expire", ttl)
	}

	ok, err := store.Expire(key, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Expire() error = %v", err)
	}
	if !ok {
		t.Fatalf("Expire() = false, want true")
	}

	ttl, err = store.TTL(key)
	if err != nil {
		t.Fatalf("TTL() after Expire error = %v", err)
	}
	if ttl <= 0 || ttl > 200*time.Millisecond {
		t.Fatalf("TTL() = %v, want between 0 and 200ms", ttl)
	}

	got, err = store.Get(key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Fatalf("Get() after Expire = %q, want %q", got, value)
	}
}

func TestSetOverwrite(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("overwrite")

	if err := store.Set(key, []byte("first"), 0); err != nil {
		t.Fatalf("Set(first) error = %v", err)
	}
	if err := store.Set(key, []byte("second"), 0); err != nil {
		t.Fatalf("Set(second) error = %v", err)
	}

	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("Get() = %q, want %q after overwrite", got, "second")
	}

	ttl, err := store.TTL(key)
	if err != nil {
		t.Fatalf("TTL() error = %v", err)
	}
	if ttl != time.Duration(-1) {
		t.Fatalf("TTL() = %v, want -1 after overwrite", ttl)
	}
}

func TestDelReturnsBool(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("greet")

	if err := store.Set(key, []byte("hello"), 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	deleted, err := store.Del(key)
	if err != nil {
		t.Fatalf("Del() error = %v", err)
	}
	if !deleted {
		t.Fatalf("Del() = false, want true for existing key")
	}

	if _, err := store.Get(key); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("Get() after Del error = %v, want ErrKeyNotFound", err)
	}

	deleted, err = store.Del(key)
	if err != nil {
		t.Fatalf("Del() error = %v", err)
	}
	if deleted {
		t.Fatalf("Del() = true, want false for missing key")
	}
}

func TestExpireUpdatesExistingString(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("expire-string")

	if err := store.Set(key, []byte("value"), 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	ok, err := store.Expire(key, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Expire() error = %v", err)
	}
	if !ok {
		t.Fatalf("Expire() = false, want true")
	}

	ttl, err := store.TTL(key)
	if err != nil {
		t.Fatalf("TTL() error = %v", err)
	}
	if ttl <= 0 || ttl > 50*time.Millisecond {
		t.Fatalf("TTL() = %v, want between 0 and 50ms", ttl)
	}

	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(got) != "value" {
		t.Fatalf("Get() = %q, want %q", got, "value")
	}
}

func TestGetExpiredStringReturnsNotFound(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("transient")

	if err := store.Set(key, []byte("bye"), 20*time.Millisecond); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	time.Sleep(60 * time.Millisecond)

	if _, err := store.Get(key); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("Get() after expiration error = %v, want ErrKeyNotFound", err)
	}

	if _, err := store.TTL(key); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("TTL() after expiration error = %v, want ErrKeyNotFound", err)
	}

	if _, err := store.db.Get(metaKey(key)); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("meta key still exists after lazy delete: %v", err)
	}
}

func TestStringWrongType(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("wrong-type")

	if err := store.db.Put(metaKey(key), encodeMetadata(metadata{typ: redisTypeHash, version: 1})); err != nil {
		t.Fatalf("put wrong type metadata: %v", err)
	}

	if _, err := store.Get(key); err != ErrWrongType {
		t.Fatalf("Get() = %v, want ErrWrongType for non-string metadata", err)
	}
}

func TestSetOverridesExpiredCompositeKey(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("expired-composite")
	expired := metadata{
		typ:      redisTypeList,
		version:  5,
		expireAt: time.Now().Add(-time.Second).UnixNano(),
	}
	if err := store.db.Put(metaKey(key), encodeMetadata(expired)); err != nil {
		t.Fatalf("put expired metadata: %v", err)
	}

	if err := store.Set(key, []byte("fresh"), 0); err != nil {
		t.Fatalf("Set() after expiration error = %v", err)
	}

	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(got) != "fresh" {
		t.Fatalf("Get() = %q, want %q", got, "fresh")
	}

	meta, err := store.findMetadata(key)
	if err != nil {
		t.Fatalf("findMetadata() error = %v", err)
	}
	if meta == nil {
		t.Fatalf("expected metadata after Set, got nil")
	}
	if meta.typ != redisTypeString {
		t.Fatalf("metadata type = %v, want string", meta.typ)
	}
	if meta.version <= expired.version {
		t.Fatalf("metadata version = %d, want > %d", meta.version, expired.version)
	}
}

func TestSetOverridesCompositeKeyWithMissingVersionTracker(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("missing-version-tracker")
	base := metadata{
		typ:     redisTypeHash,
		version: 7,
		size:    2,
	}
	if err := store.db.Put(metaKey(key), encodeMetadata(base)); err != nil {
		t.Fatalf("put hash metadata: %v", err)
	}

	if err := store.Set(key, []byte("fresh"), 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	meta, err := store.findMetadata(key)
	if err != nil {
		t.Fatalf("findMetadata() error = %v", err)
	}
	if meta == nil {
		t.Fatalf("expected metadata after Set, got nil")
	}
	if meta.typ != redisTypeString {
		t.Fatalf("metadata type = %v, want string", meta.typ)
	}
	if meta.version <= base.version {
		t.Fatalf("metadata version = %d, want > %d", meta.version, base.version)
	}
	verNum, err := store.loadVersionNumber(key)
	if err != nil {
		t.Fatalf("loadVersionNumber() error = %v", err)
	}
	if verNum <= base.version {
		t.Fatalf("version-tracker = %d, want > %d", verNum, base.version)
	}
}

func TestTTLRemovesExpiredMetadata(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("ttl-expired")
	expired := metadata{
		typ:      redisTypeString,
		version:  2,
		expireAt: time.Now().Add(-time.Millisecond).UnixNano(),
	}
	enc, err := encodeMetadataWithValue(expired, []byte("bye"))
	if err != nil {
		t.Fatalf("encode expired string metadata: %v", err)
	}
	if err := store.db.Put(metaKey(key), enc); err != nil {
		t.Fatalf("put expired metadata: %v", err)
	}

	if _, err := store.TTL(key); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("TTL() = %v, want ErrKeyNotFound for expired metadata", err)
	}

	if _, err := store.db.Get(metaKey(key)); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("meta key was not deleted lazily: %v", err)
	}
}

func TestExpirePreservesNonStringMetadata(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("nonstring-expire")
	base := metadata{
		typ:     redisTypeHash,
		version: 3,
		size:    5,
		head:    -10,
		tail:    10,
	}
	if err := store.db.Put(metaKey(key), encodeMetadata(base)); err != nil {
		t.Fatalf("put hash metadata: %v", err)
	}
	if err := store.storeVersionNumber(key, base.version); err != nil {
		t.Fatalf("store version number: %v", err)
	}

	ok, err := store.Expire(key, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Expire() error = %v", err)
	}
	if !ok {
		t.Fatalf("Expire() = false, want true")
	}

	meta, _, err := store.findMetadataAndValue(key)
	if err != nil {
		t.Fatalf("findMetadataAndValue() error = %v", err)
	}
	if meta == nil {
		t.Fatalf("expected metadata after Expire, got nil")
	}
	if meta.typ != base.typ {
		t.Fatalf("metadata type = %v, want %v", meta.typ, base.typ)
	}
	if meta.version != base.version {
		t.Fatalf("metadata version = %d, want %d", meta.version, base.version)
	}
	if meta.size != base.size {
		t.Fatalf("metadata size = %d, want %d", meta.size, base.size)
	}
	if meta.head != base.head {
		t.Fatalf("metadata head = %d, want %d", meta.head, base.head)
	}
	if meta.tail != base.tail {
		t.Fatalf("metadata tail = %d, want %d", meta.tail, base.tail)
	}
	if meta.expireAt <= 0 {
		t.Fatalf("expireAt = %d, want > 0", meta.expireAt)
	}

	raw, err := store.db.Get(metaKey(key))
	if err != nil {
		t.Fatalf("get meta key: %v", err)
	}
	if len(raw) != metadataEncodedSize {
		t.Fatalf("meta payload length = %d, want %d", len(raw), metadataEncodedSize)
	}
}
