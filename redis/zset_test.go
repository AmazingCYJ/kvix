package redis

import (
	"errors"
	"math"
	"testing"
	"time"

	"kvix/common"
)

func TestZAddZScoreAndRange(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("zset:add-range")

	added, err := store.ZAdd(key, 1.5, []byte("alice"))
	if err != nil {
		t.Fatalf("ZAdd() error = %v", err)
	}
	if !added {
		t.Fatal("ZAdd() returned false for new member")
	}

	added, err = store.ZAdd(key, 2.5, []byte("bob"))
	if err != nil {
		t.Fatalf("ZAdd() error = %v", err)
	}
	if !added {
		t.Fatal("ZAdd() returned false for new member")
	}

	score, err := store.ZScore(key, []byte("alice"))
	if err != nil {
		t.Fatalf("ZScore() error = %v", err)
	}
	if score != 1.5 {
		t.Fatalf("ZScore() = %g, want 1.5", score)
	}

	members, err := store.ZRange(key, 0, -1)
	if err != nil {
		t.Fatalf("ZRange() error = %v", err)
	}
	assertMembers(t, members, []string{"alice", "bob"})

	members, err = store.ZRange(key, 1, 1)
	if err != nil {
		t.Fatalf("ZRange() error = %v", err)
	}
	assertMembers(t, members, []string{"bob"})
}

func TestZAddUpdateScore(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("zset:update")

	if _, err := store.ZAdd(key, 5.0, []byte("alpha")); err != nil {
		t.Fatalf("ZAdd() error = %v", err)
	}
	if _, err := store.ZAdd(key, 2.0, []byte("beta")); err != nil {
		t.Fatalf("ZAdd() error = %v", err)
	}

	added, err := store.ZAdd(key, 1.0, []byte("alpha"))
	if err != nil {
		t.Fatalf("ZAdd() error = %v", err)
	}
	if added {
		t.Fatal("ZAdd() returned true when updating existing score")
	}

	score, err := store.ZScore(key, []byte("alpha"))
	if err != nil {
		t.Fatalf("ZScore() error = %v", err)
	}
	if score != 1.0 {
		t.Fatalf("ZScore() = %g, want 1.0", score)
	}

	members, err := store.ZRange(key, 0, -1)
	if err != nil {
		t.Fatalf("ZRange() error = %v", err)
	}
	assertMembers(t, members, []string{"alpha", "beta"})

	card, err := store.ZCard(key)
	if err != nil {
		t.Fatalf("ZCard() error = %v", err)
	}
	if card != 2 {
		t.Fatalf("ZCard() = %d, want 2", card)
	}
}

func TestDecodeZSetScoreRoundTrip(t *testing.T) {
	scores := []float64{
		math.Inf(-1),
		-123.456,
		0,
		3.14,
		math.Inf(1),
	}
	for _, score := range scores {
		encoded := encodeZSetScore(score)
		decoded, err := decodeZSetScore(encoded)
		if err != nil {
			t.Fatalf("decodeZSetScore(%g) error = %v", score, err)
		}
		if math.Float64bits(decoded) != math.Float64bits(score) {
			t.Fatalf("decoded %g differs from original %g", decoded, score)
		}
	}
}

func TestDecodeZSetScoreRejectsWrongSize(t *testing.T) {
	if _, err := decodeZSetScore([]byte{1, 2, 3}); err != errInvalidZSetScoreEncoding {
		t.Fatalf("expected errInvalidZSetScoreEncoding, got %v", err)
	}
}

func TestZAddUpdateMissingOldScoreIndex(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("zset:update-missing-score")

	if _, err := store.ZAdd(key, 1.0, []byte("ghost")); err != nil {
		t.Fatalf("ZAdd() error = %v", err)
	}

	meta, err := store.loadZSetMetadata(key)
	if err != nil {
		t.Fatalf("loadZSetMetadata() error = %v", err)
	}
	if meta == nil {
		t.Fatal("metadata nil after ZAdd")
	}

	if err := store.db.Delete(zsetScoreKey(key, meta.version, 1.0, []byte("ghost"))); err != nil {
		t.Fatalf("delete old score index: %v", err)
	}

	added, err := store.ZAdd(key, 2.0, []byte("ghost"))
	if err != nil {
		t.Fatalf("ZAdd() error = %v", err)
	}
	if added {
		t.Fatal("ZAdd() returned true when updating existing member with missing score index")
	}

	score, err := store.ZScore(key, []byte("ghost"))
	if err != nil {
		t.Fatalf("ZScore() error = %v", err)
	}
	if score != 2.0 {
		t.Fatalf("ZScore() = %g, want 2.0", score)
	}

	members, err := store.ZRange(key, 0, -1)
	if err != nil {
		t.Fatalf("ZRange() error = %v", err)
	}
	assertMembers(t, members, []string{"ghost"})
}

func TestZRemAndCard(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("zset:rem-card")

	if removed, err := store.ZRem(key, []byte("missing")); err != nil {
		t.Fatalf("ZRem() error = %v", err)
	} else if removed {
		t.Fatal("ZRem() = true for missing key")
	}

	if _, err := store.ZAdd(key, 1.0, []byte("a")); err != nil {
		t.Fatalf("ZAdd() error = %v", err)
	}
	if _, err := store.ZAdd(key, 2.0, []byte("b")); err != nil {
		t.Fatalf("ZAdd() error = %v", err)
	}

	removed, err := store.ZRem(key, []byte("a"))
	if err != nil {
		t.Fatalf("ZRem() error = %v", err)
	}
	if !removed {
		t.Fatal("ZRem() = false, want true")
	}

	card, err := store.ZCard(key)
	if err != nil {
		t.Fatalf("ZCard() error = %v", err)
	}
	if card != 1 {
		t.Fatalf("ZCard() = %d, want 1", card)
	}

	removed, err = store.ZRem(key, []byte("b"))
	if err != nil {
		t.Fatalf("ZRem() error = %v", err)
	}
	if !removed {
		t.Fatal("ZRem() = false, want true")
	}

	if _, err := store.db.Get(metaKey(key)); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("metadata not deleted after removing last member: %v", err)
	}

	prevVersion, err := store.loadVersionNumber(key)
	if err != nil {
		t.Fatalf("loadVersionNumber() error = %v", err)
	}
	if prevVersion == 0 {
		t.Fatal("expected version tracker to preserve a non-zero version")
	}

	if _, err := store.ZAdd(key, 3.0, []byte("rebuild")); err != nil {
		t.Fatalf("ZAdd() after rebuild error = %v", err)
	}

	meta, err := store.loadZSetMetadata(key)
	if err != nil {
		t.Fatalf("loadZSetMetadata() error = %v", err)
	}
	if meta == nil {
		t.Fatal("loadZSetMetadata() = nil after rebuild")
	}
	if meta.version <= prevVersion {
		t.Fatalf("meta.version = %d, want > %d", meta.version, prevVersion)
	}
}

func TestZSetWrongType(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("zset:wrong")

	if err := store.db.Put(metaKey(key), encodeMetadata(metadata{typ: redisTypeString, version: 1})); err != nil {
		t.Fatalf("put wrong metadata: %v", err)
	}

	if _, err := store.ZAdd(key, 1.0, []byte("x")); err != ErrWrongType {
		t.Fatalf("ZAdd() = %v, want ErrWrongType", err)
	}
	if _, err := store.ZScore(key, []byte("x")); err != ErrWrongType {
		t.Fatalf("ZScore() = %v, want ErrWrongType", err)
	}
	if _, err := store.ZRange(key, 0, -1); err != ErrWrongType {
		t.Fatalf("ZRange() = %v, want ErrWrongType", err)
	}
	if _, err := store.ZCard(key); err != ErrWrongType {
		t.Fatalf("ZCard() = %v, want ErrWrongType", err)
	}
	if _, err := store.ZRem(key, []byte("x")); err != ErrWrongType {
		t.Fatalf("ZRem() = %v, want ErrWrongType", err)
	}
}

func TestZSetLazyExpiration(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("zset:expired")

	expired := metadata{
		typ:      redisTypeZSet,
		version:  1,
		size:     1,
		expireAt: time.Now().Add(-time.Minute).UnixNano(),
	}
	member := []byte("gone")
	score := 2.5

	if err := store.db.Put(metaKey(key), encodeMetadata(expired)); err != nil {
		t.Fatalf("put expired metadata: %v", err)
	}
	if err := store.db.Put(zsetDictKey(key, expired.version, member), encodeZSetScore(score)); err != nil {
		t.Fatalf("put stale dict entry: %v", err)
	}
	if err := store.db.Put(zsetScoreKey(key, expired.version, score, member), zsetScorePlaceholder); err != nil {
		t.Fatalf("put stale score entry: %v", err)
	}

	if card, err := store.ZCard(key); err != nil {
		t.Fatalf("ZCard() error = %v", err)
	} else if card != 0 {
		t.Fatalf("ZCard() = %d, want 0 for expired key", card)
	}

	if members, err := store.ZRange(key, 0, -1); err != nil {
		t.Fatalf("ZRange() error = %v", err)
	} else if len(members) != 0 {
		t.Fatalf("ZRange() = %v, want empty slice for expired key", members)
	}

	if _, err := store.ZScore(key, member); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("ZScore() = %v, want ErrKeyNotFound for expired key", err)
	}

	if removed, err := store.ZRem(key, member); err != nil {
		t.Fatalf("ZRem() error = %v", err)
	} else if removed {
		t.Fatal("ZRem() = true for expired key")
	}

	if _, err := store.db.Get(metaKey(key)); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("metadata not deleted for expired key: %v", err)
	}
}

func TestZRangeHandlesNegativeIndexes(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("zset:neg")

	entries := []struct {
		score  float64
		member string
	}{
		{1.0, "charlie"},
		{2.0, "bravo"},
		{2.0, "alpha"},
	}
	for _, entry := range entries {
		if _, err := store.ZAdd(key, entry.score, []byte(entry.member)); err != nil {
			t.Fatalf("ZAdd() error = %v", err)
		}
	}

	members, err := store.ZRange(key, 0, -1)
	if err != nil {
		t.Fatalf("ZRange() error = %v", err)
	}
	assertMembers(t, members, []string{"charlie", "alpha", "bravo"})

	members, err = store.ZRange(key, -2, -1)
	if err != nil {
		t.Fatalf("ZRange() error = %v", err)
	}
	assertMembers(t, members, []string{"alpha", "bravo"})

	members, err = store.ZRange(key, 1, 100)
	if err != nil {
		t.Fatalf("ZRange() error = %v", err)
	}
	assertMembers(t, members, []string{"alpha", "bravo"})

	members, err = store.ZRange(key, 100, 200)
	if err != nil {
		t.Fatalf("ZRange() error = %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("ZRange() = %v, want empty slice when start >= len", members)
	}
}

func TestZAddRejectsNaN(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("zset:nan")

	if _, err := store.ZAdd(key, math.NaN(), []byte("nan")); err != ErrZSetScoreNaN {
		t.Fatalf("ZAdd() = %v, want ErrZSetScoreNaN", err)
	}
}

func TestZMissingKeySemantics(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("zset:missing")

	if card, err := store.ZCard(key); err != nil {
		t.Fatalf("ZCard() error = %v", err)
	} else if card != 0 {
		t.Fatalf("ZCard() = %d, want 0 for missing key", card)
	}

	if members, err := store.ZRange(key, 0, -1); err != nil {
		t.Fatalf("ZRange() error = %v", err)
	} else if len(members) != 0 {
		t.Fatalf("ZRange() = %v, want empty slice for missing key", members)
	}

	if removed, err := store.ZRem(key, []byte("x")); err != nil {
		t.Fatalf("ZRem() error = %v", err)
	} else if removed {
		t.Fatal("ZRem() = true for missing key")
	}

	if _, err := store.ZScore(key, []byte("x")); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("ZScore() = %v, want ErrKeyNotFound", err)
	}
}

func assertMembers(t *testing.T, got [][]byte, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("unexpected member count: got %d want %d", len(got), len(want))
	}
	for i, wantMember := range want {
		if string(got[i]) != wantMember {
			t.Fatalf("member[%d] = %q, want %q", i, got[i], wantMember)
		}
	}
}
