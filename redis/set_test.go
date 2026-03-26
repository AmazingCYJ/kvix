package redis

import (
	"errors"
	"testing"
	"time"

	"bitcask-my/common"
)

func TestSAddMembersAndCard(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("set:members")

	added, err := store.SAdd(key, []byte("alpha"), []byte("beta"))
	if err != nil {
		t.Fatalf("SAdd() error = %v", err)
	}
	if added != 2 {
		t.Fatalf("SAdd() added = %d, want 2", added)
	}

	card, err := store.SCard(key)
	if err != nil {
		t.Fatalf("SCard() error = %v", err)
	}
	if card != 2 {
		t.Fatalf("SCard() = %d, want 2", card)
	}

	for _, member := range [][]byte{[]byte("alpha"), []byte("beta")} {
		ok, err := store.SIsMember(key, member)
		if err != nil {
			t.Fatalf("SIsMember(%q) error = %v", member, err)
		}
		if !ok {
			t.Fatalf("SIsMember(%q) = false, want true", member)
		}
	}
}

func TestSAddDeduplicates(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("set:dedupe")

	added, err := store.SAdd(key, []byte("a"), []byte("b"), []byte("a"))
	if err != nil {
		t.Fatalf("SAdd() error = %v", err)
	}
	if added != 2 {
		t.Fatalf("SAdd() added = %d, want 2", added)
	}

	added, err = store.SAdd(key, []byte("a"), []byte("c"), []byte("c"))
	if err != nil {
		t.Fatalf("SAdd() error = %v", err)
	}
	if added != 1 {
		t.Fatalf("SAdd() added again = %d, want 1", added)
	}

	card, err := store.SCard(key)
	if err != nil {
		t.Fatalf("SCard() error = %v", err)
	}
	if card != 3 {
		t.Fatalf("SCard() = %d, want 3", card)
	}
}

func TestSRemAndIsMember(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("set:rem")
	members := [][]byte{[]byte("one"), []byte("two"), []byte("three")}

	if _, err := store.SAdd(key, members...); err != nil {
		t.Fatalf("SAdd() error = %v", err)
	}

	removed, err := store.SRem(key, []byte("two"), []byte("four"))
	if err != nil {
		t.Fatalf("SRem() error = %v", err)
	}
	if removed != 1 {
		t.Fatalf("SRem() removed = %d, want 1", removed)
	}

	ok, err := store.SIsMember(key, []byte("two"))
	if err != nil {
		t.Fatalf("SIsMember() error = %v", err)
	}
	if ok {
		t.Fatal("SIsMember(two) = true, want false")
	}

	card, err := store.SCard(key)
	if err != nil {
		t.Fatalf("SCard() error = %v", err)
	}
	if card != 2 {
		t.Fatalf("SCard() = %d, want 2", card)
	}

	membersLeft, err := store.SMembers(key)
	if err != nil {
		t.Fatalf("SMembers() error = %v", err)
	}
	if len(membersLeft) != 2 {
		t.Fatalf("SMembers() = %v, want 2 members remaining", membersLeft)
	}
}

func TestSMembersReturnsCurrentVersionOnly(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("set:version")

	if _, err := store.SAdd(key, []byte("legacy"), []byte("old")); err != nil {
		t.Fatalf("SAdd() error = %v", err)
	}

	meta := metadata{
		typ:     redisTypeSet,
		version: 2,
		size:    1,
	}
	if err := store.db.Put(metaKey(key), encodeMetadata(meta)); err != nil {
		t.Fatalf("put new metadata: %v", err)
	}
	if err := store.db.Put(metaVersionKey(key), encodeVersion(meta.version)); err != nil {
		t.Fatalf("put version tracker: %v", err)
	}
	if err := store.db.Put(setDataKey(key, meta.version, []byte("fresh")), setMemberPlaceholder); err != nil {
		t.Fatalf("put fresh member: %v", err)
	}

	members, err := store.SMembers(key)
	if err != nil {
		t.Fatalf("SMembers() error = %v", err)
	}
	if len(members) != 1 || string(members[0]) != "fresh" {
		t.Fatalf("SMembers() = %v, want only [fresh]", members)
	}
}

func TestSetMissingKeySemantics(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("set:missing")

	if card, err := store.SCard(key); err != nil {
		t.Fatalf("SCard() error = %v", err)
	} else if card != 0 {
		t.Fatalf("SCard() = %d, want 0 for missing key", card)
	}

	if ok, err := store.SIsMember(key, []byte("x")); err != nil {
		t.Fatalf("SIsMember() error = %v", err)
	} else if ok {
		t.Fatal("SIsMember() = true, want false for missing key")
	}

	if removed, err := store.SRem(key, []byte("x")); err != nil {
		t.Fatalf("SRem() error = %v", err)
	} else if removed != 0 {
		t.Fatalf("SRem() = %d, want 0 for missing key", removed)
	}

	if members, err := store.SMembers(key); err != nil {
		t.Fatalf("SMembers() error = %v", err)
	} else if len(members) != 0 {
		t.Fatalf("SMembers() = %v, want empty slice for missing key", members)
	}
}

func TestSetWrongType(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("set:wrong")

	if err := store.db.Put(metaKey(key), encodeMetadata(metadata{typ: redisTypeString, version: 1})); err != nil {
		t.Fatalf("put wrong metadata: %v", err)
	}

	if _, err := store.SAdd(key, []byte("x")); err != ErrWrongType {
		t.Fatalf("SAdd() = %v, want ErrWrongType", err)
	}
}

func TestSetLazyExpiration(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("set:expired")

	expired := metadata{
		typ:      redisTypeSet,
		version:  1,
		size:     1,
		expireAt: time.Now().Add(-time.Minute).UnixNano(),
	}
	if err := store.db.Put(metaKey(key), encodeMetadata(expired)); err != nil {
		t.Fatalf("put expired metadata: %v", err)
	}
	if err := store.db.Put(setDataKey(key, expired.version, []byte("gone")), setMemberPlaceholder); err != nil {
		t.Fatalf("put stale member: %v", err)
	}

	if card, err := store.SCard(key); err != nil {
		t.Fatalf("SCard() error = %v", err)
	} else if card != 0 {
		t.Fatalf("SCard() = %d, want 0 for expired key", card)
	}

	if members, err := store.SMembers(key); err != nil {
		t.Fatalf("SMembers() error = %v", err)
	} else if len(members) != 0 {
		t.Fatalf("SMembers() = %v, want empty slice for expired key", members)
	}

	if ok, err := store.SIsMember(key, []byte("gone")); err != nil {
		t.Fatalf("SIsMember() error = %v", err)
	} else if ok {
		t.Fatal("SIsMember() = true, want false for expired key")
	}

	if _, err := store.db.Get(metaKey(key)); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("metadata not deleted for expired key: %v", err)
	}
}

func TestSetMetadataRemovedWhenEmpty(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("set:clean")

	if _, err := store.SAdd(key, []byte("solo")); err != nil {
		t.Fatalf("SAdd() error = %v", err)
	}

	if removed, err := store.SRem(key, []byte("solo")); err != nil {
		t.Fatalf("SRem() error = %v", err)
	} else if removed != 1 {
		t.Fatalf("SRem() = %d, want 1", removed)
	}

	if _, err := store.db.Get(metaKey(key)); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("metadata not deleted after removing last member: %v", err)
	}

	ver, err := store.loadVersionNumber(key)
	if err != nil {
		t.Fatalf("loadVersionNumber() error = %v", err)
	}
	if ver == 0 {
		t.Fatalf("expected version tracker preserved, got %d", ver)
	}

	if _, err := store.SAdd(key, []byte("rebuild")); err != nil {
		t.Fatalf("SAdd() error after cleanup = %v", err)
	}

	meta, err := store.loadSetMetadata(key)
	if err != nil {
		t.Fatalf("loadSetMetadata() error = %v", err)
	}
	if meta == nil {
		t.Fatal("loadSetMetadata() = nil, want metadata after rebuild")
	}
	if meta.version <= ver {
		t.Fatalf("meta.version = %d, want > %d", meta.version, ver)
	}
}
