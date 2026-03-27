package redis

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"

	"kvix/common"
)

func TestMetaEncodeDecode(t *testing.T) {
	src := metadata{
		typ:      redisTypeHash,
		expireAt: 12345,
		version:  7,
		size:     42,
		head:     -5,
		tail:     5,
	}
	enc := encodeMetadata(src)
	got, err := decodeMetadata(enc)
	if err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if got != src {
		t.Fatalf("decode mismatch: %#v", got)
	}

	value := []byte("value-bytes")
	withValue, err := encodeMetadataWithValue(metadata{typ: redisTypeString}, value)
	if err != nil {
		t.Fatalf("encode metadata with value: %v", err)
	}
	meta, payload, err := decodeMetadataAndValue(withValue)
	if err != nil {
		t.Fatalf("decode with value: %v", err)
	}
	if meta.typ != redisTypeString {
		t.Fatalf("metadata type mismatch: %v", meta.typ)
	}
	if !bytes.Equal(payload, value) {
		t.Fatalf("payload mismatch: %v", payload)
	}
}

func TestDecodeMetadataError(t *testing.T) {
	_, err := decodeMetadata(make([]byte, metadataEncodedSize-1))
	if err != ErrDecodeMetadata {
		t.Fatalf("expected ErrDecodeMetadata, got %v", err)
	}

	bad := encodeMetadata(metadata{typ: redisTypeString})
	bad[0] = byte(99)
	if _, err := decodeMetadata(bad); err != ErrDecodeMetadata {
		t.Fatalf("expected ErrDecodeMetadata on invalid type, got %v", err)
	}
}

func TestMetaKeyAndDataKeyEncoding(t *testing.T) {
	key := []byte("user:1001")

	rest := requirePrefix(t, metaKey(key), prefixMeta)
	if comp, tail := readLenPrefixed(t, rest); !bytes.Equal(comp, key) || len(tail) != 0 {
		t.Fatalf("metaKey encoding mismatch")
	}

	rest = requirePrefix(t, hashDataKey(key, 3, []byte("name")), prefixHash)
	if comp, tail := readLenPrefixed(t, rest); !bytes.Equal(comp, key) {
		t.Fatalf("hash key component mismatch")
	} else if version, tail := readUint64(t, tail); version != 3 {
		t.Fatalf("hash version mismatch: %d", version)
	} else if field, tail := readLenPrefixed(t, tail); !bytes.Equal(field, []byte("name")) || len(tail) != 0 {
		t.Fatalf("hash field mismatch")
	}

	rest = requirePrefix(t, listDataKey(key, 2, -1), prefixList)
	if comp, tail := readLenPrefixed(t, rest); !bytes.Equal(comp, key) {
		t.Fatalf("list key component mismatch")
	} else if version, tail := readUint64(t, tail); version != 2 {
		t.Fatalf("list version mismatch: %d", version)
	} else if idx, tail := readSortableInt64(t, tail); idx != -1 || len(tail) != 0 {
		t.Fatalf("list index mismatch")
	}

	rest = requirePrefix(t, setDataKey(key, 4, []byte("go")), prefixSet)
	if comp, tail := readLenPrefixed(t, rest); !bytes.Equal(comp, key) {
		t.Fatalf("set key mismatch")
	} else if version, tail := readUint64(t, tail); version != 4 {
		t.Fatalf("set version mismatch: %d", version)
	} else if member, tail := readLenPrefixed(t, tail); !bytes.Equal(member, []byte("go")) || len(tail) != 0 {
		t.Fatalf("set member mismatch")
	}

	rest = requirePrefix(t, zsetDictKey(key, 5, []byte("alice")), prefixZSetDict)
	if comp, tail := readLenPrefixed(t, rest); !bytes.Equal(comp, key) {
		t.Fatalf("zset dict key mismatch")
	} else if version, tail := readUint64(t, tail); version != 5 {
		t.Fatalf("zset dict version mismatch: %d", version)
	} else if member, tail := readLenPrefixed(t, tail); !bytes.Equal(member, []byte("alice")) || len(tail) != 0 {
		t.Fatalf("zset dict member mismatch")
	}

	rest = requirePrefix(t, zsetScoreKey(key, 5, 3.14, []byte("bob")), prefixZSetScore)
	if comp, tail := readLenPrefixed(t, rest); !bytes.Equal(comp, key) {
		t.Fatalf("zset score key mismatch")
	} else if version, tail := readUint64(t, tail); version != 5 {
		t.Fatalf("zset score version mismatch: %d", version)
	} else if len(tail) < 8+1 {
		t.Fatalf("zset score bytes missing")
	} else if scoreBytes, rem := tail[:8], tail[8:]; !bytes.Equal(scoreBytes, encodeZSetScore(3.14)) {
		t.Fatalf("zset score encoding mismatch")
	} else if rem[0] != memberSeparator {
		t.Fatalf("zset score member separator missing")
	} else if !bytes.Equal(rem[1:], []byte("bob")) {
		t.Fatalf("zset score member mismatch")
	}
}

func TestCompositeKeyEncodingHasNoCollision(t *testing.T) {
	keys := [][]byte{
		[]byte("plain"),
		[]byte("colon:key"),
		[]byte("null\x00byte"),
		[]byte("multi:colon:"),
	}
	fields := [][]byte{
		[]byte("field"),
		[]byte("field\x00end"),
		[]byte("field:colon"),
	}
	members := [][]byte{
		[]byte("member"),
		[]byte("member\x00bin"),
		[]byte("member:colon"),
	}
	seen := map[string]struct{}{}
	tryAdd := func(data []byte) {
		key := string(data)
		if _, ok := seen[key]; ok {
			t.Fatalf("collision detected for key %q", key)
		}
		seen[key] = struct{}{}
	}
	for _, key := range keys {
		tryAdd(metaKey(key))
		for _, field := range fields {
			tryAdd(hashDataKey(key, 1, field))
		}
		for _, idx := range []int64{-2, 0, 1, 10} {
			tryAdd(listDataKey(key, 2, idx))
		}
		for _, member := range members {
			tryAdd(setDataKey(key, 3, member))
			tryAdd(zsetDictKey(key, 3, member))
			tryAdd(zsetScoreKey(key, 3, 0.5, member))
		}
	}
}

func TestListDataKeyOrdering(t *testing.T) {
	indexes := []int64{math.MinInt64, -5, -1, 0, 1, 5, math.MaxInt64}
	keys := make([][]byte, len(indexes))
	for i, idx := range indexes {
		keys[i] = listDataKey([]byte("rank"), 1, idx)
	}
	for i := 1; i < len(keys); i++ {
		if bytes.Compare(keys[i-1], keys[i]) >= 0 {
			t.Fatalf("list keys out of order: idx %d >= idx %d", indexes[i-1], indexes[i])
		}
	}
}

func TestDecodeMetadataAndValueRejectsNonStringPayload(t *testing.T) {
	meta := encodeMetadata(metadata{typ: redisTypeHash})
	data := append(meta, []byte("payload")...)
	if _, _, err := decodeMetadataAndValue(data); err != ErrMetadataPayloadNonString {
		t.Fatalf("expected ErrMetadataPayloadNonString, got %v", err)
	}
}

func TestEncodeMetadataWithValueRejectsNonStringPayload(t *testing.T) {
	if _, err := encodeMetadataWithValue(metadata{typ: redisTypeHash}, []byte("payload")); err != ErrMetadataPayloadNonString {
		t.Fatalf("expected ErrMetadataPayloadNonString on encode, got %v", err)
	}
}

func TestEncodeZSetScoreRejectsNaN(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for NaN score")
		}
	}()
	encodeZSetScore(math.NaN())
}

func TestZSetScoreKeyOrdering(t *testing.T) {
	scores := []float64{
		math.Inf(-1),
		-1000,
		-0.5,
		0,
		0.5,
		123.456,
		math.Inf(1),
	}
	keys := make([][]byte, len(scores))
	for i, score := range scores {
		keys[i] = zsetScoreKey([]byte("rank"), 9, score, []byte("member"))
	}
	for i := 1; i < len(keys); i++ {
		if bytes.Compare(keys[i-1], keys[i]) >= 0 {
			t.Fatalf("score order broken: %g >= %g", scores[i-1], scores[i])
		}
	}
}

func TestZSetScoreKeyTieBreakByMemberLexicographicalOrder(t *testing.T) {
	members := [][]byte{
		[]byte("alpha"),
		[]byte("alphabet"),
		[]byte("beta"),
	}
	score := 4.2
	keys := make([][]byte, len(members))
	for i, member := range members {
		keys[i] = zsetScoreKey([]byte("rank"), 10, score, member)
	}
	for i := 1; i < len(keys); i++ {
		if bytes.Compare(keys[i-1], keys[i]) >= 0 {
			t.Fatalf("zset score tie not ordered by member lex: %q >= %q", members[i-1], members[i])
		}
	}
}

func requirePrefix(t *testing.T, data []byte, prefix string) []byte {
	t.Helper()
	expect := append([]byte(prefix), 0)
	if len(data) < len(expect) {
		t.Fatalf("key too short for prefix %q", prefix)
	}
	if !bytes.Equal(data[:len(expect)], expect) {
		t.Fatalf("expected prefix %q", prefix)
	}
	return data[len(expect):]
}

func readLenPrefixed(t *testing.T, data []byte) ([]byte, []byte) {
	t.Helper()
	if len(data) < lenMarkerSize {
		t.Fatalf("not enough bytes for length prefix")
	}
	length := binary.BigEndian.Uint32(data[:lenMarkerSize])
	if len(data) < lenMarkerSize+int(length) {
		t.Fatalf("length prefix exceeds available bytes")
	}
	start := lenMarkerSize
	end := start + int(length)
	return data[start:end], data[end:]
}

func readUint64(t *testing.T, data []byte) (uint64, []byte) {
	t.Helper()
	if len(data) < 8 {
		t.Fatalf("expected uint64, got %d bytes", len(data))
	}
	return binary.BigEndian.Uint64(data[:8]), data[8:]
}

func readSortableInt64(t *testing.T, data []byte) (int64, []byte) {
	t.Helper()
	if len(data) < 8 {
		t.Fatalf("expected sortable int64, got %d bytes", len(data))
	}
	bits := binary.BigEndian.Uint64(data[:8])
	value := int64(bits ^ 0x8000000000000000)
	return value, data[8:]
}

func TestFindMetadataReturnsNilForMissingKey(t *testing.T) {
	store := openTestRedisStore(t)

	meta, err := store.findMetadata([]byte("missing"))
	if err != nil {
		t.Fatalf("findMetadata() error = %v", err)
	}
	if meta != nil {
		t.Fatalf("findMetadata() = %#v, want nil for missing key", meta)
	}
}

func TestExpiredMetadataIsDeletedLazily(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("expired")
	expired := metadata{
		typ:      redisTypeString,
		expireAt: time.Now().Add(-time.Second).UnixNano(),
	}
	if err := store.db.Put(metaKey(key), encodeMetadata(expired)); err != nil {
		t.Fatalf("put expired metadata: %v", err)
	}

	meta, err := store.findMetadata(key)
	if err != nil {
		t.Fatalf("findMetadata() error = %v", err)
	}
	if meta != nil {
		t.Fatalf("expected expired metadata to be treated as missing, got %#v", meta)
	}
	if _, err := store.db.Get(metaKey(key)); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("expected meta key to be deleted lazily, got %v", err)
	}
}

func TestExpectTypeRejectsWrongType(t *testing.T) {
	meta := metadata{typ: redisTypeString}
	if err := expectType(&meta, redisTypeString); err != nil {
		t.Fatalf("expectType should accept matching type, got %v", err)
	}
	if err := expectType(&meta, redisTypeList); err != ErrWrongType {
		t.Fatalf("expectType should reject wrong type, got %v", err)
	}
}

func TestExpectTypeRejectsNilMetadata(t *testing.T) {
	if err := expectType(nil, redisTypeString); err != ErrWrongType {
		t.Fatalf("expectType should return ErrWrongType when metadata is nil, got %v", err)
	}
}

func TestFindMetadataAndValueReturnsPayload(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("string-payload")
	payload := []byte("hello")
	enc, err := encodeMetadataWithValue(metadata{typ: redisTypeString}, payload)
	if err != nil {
		t.Fatalf("encode metadata with value: %v", err)
	}
	if err := store.db.Put(metaKey(key), enc); err != nil {
		t.Fatalf("put string metadata: %v", err)
	}

	meta, got, err := store.findMetadataAndValue(key)
	if err != nil {
		t.Fatalf("findMetadataAndValue error: %v", err)
	}
	if meta == nil {
		t.Fatalf("expected metadata to be returned")
	}
	if meta.typ != redisTypeString {
		t.Fatalf("expected string type, got %v", meta.typ)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: got %q want %q", got, payload)
	}
}

func TestExpiredStringPayloadIsDeletedLazily(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("string-expired")
	payload := []byte("bye")
	expired := metadata{
		typ:      redisTypeString,
		expireAt: time.Now().Add(-time.Second).UnixNano(),
	}
	enc, err := encodeMetadataWithValue(expired, payload)
	if err != nil {
		t.Fatalf("encode expired string metadata: %v", err)
	}
	if err := store.db.Put(metaKey(key), enc); err != nil {
		t.Fatalf("put expired string metadata: %v", err)
	}

	meta, value, err := store.findMetadataAndValue(key)
	if err != nil {
		t.Fatalf("findMetadataAndValue error: %v", err)
	}
	if meta != nil || value != nil {
		t.Fatalf("expected expired metadata to be treated as missing, got meta=%#v value=%q", meta, value)
	}
	if _, err := store.db.Get(metaKey(key)); !errors.Is(err, common.ErrKeyNotFound) {
		t.Fatalf("expected meta key deleted lazily, got %v", err)
	}
}

func TestNewMetadataVersionDoesNotRegressAfterExpiration(t *testing.T) {
	store := openTestRedisStore(t)
	key := []byte("versioned")
	base := metadata{
		typ:     redisTypeHash,
		version: 7,
	}
	if err := store.db.Put(metaKey(key), encodeMetadata(base)); err != nil {
		t.Fatalf("put base metadata: %v", err)
	}
	if err := store.storeVersionNumber(key, base.version); err != nil {
		t.Fatalf("store version number: %v", err)
	}
	if err := store.deleteMetadata(key); err != nil {
		t.Fatalf("delete metadata: %v", err)
	}

	newMeta, err := store.newMetadataForType(key, redisTypeHash)
	if err != nil {
		t.Fatalf("newMetadataForType error: %v", err)
	}
	if newMeta.version <= base.version {
		t.Fatalf("version regressed: got %d want > %d", newMeta.version, base.version)
	}
}
