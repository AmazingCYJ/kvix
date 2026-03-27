package redis

import (
	"encoding/binary"
	"math"
)

const (
	prefixMeta        = "meta:"
	prefixMetaVersion = "meta:ver:"
	prefixHash        = "hash:"
	prefixList        = "list:"
	prefixSet         = "set:"
	prefixZSetDict    = "zset:dict:"
	prefixZSetScore   = "zset:score:"
)

const (
	lenMarkerSize   = 4
	memberSeparator = byte(0)
)

func metaKey(key []byte) []byte {
	dst := make([]byte, 0, len(prefixMeta)+1+lenMarkerSize+len(key))
	dst = appendPrefix(dst, prefixMeta)
	dst = appendLengthPrefixed(dst, key)
	return dst
}

func metaVersionKey(key []byte) []byte {
	dst := make([]byte, 0, len(prefixMetaVersion)+1+lenMarkerSize+len(key))
	dst = appendPrefix(dst, prefixMetaVersion)
	dst = appendLengthPrefixed(dst, key)
	return dst
}

func hashDataKey(key []byte, version uint64, field []byte) []byte {
	dst := make([]byte, 0, len(prefixHash)+1+lenMarkerSize+len(key)+8+lenMarkerSize+len(field))
	dst = appendPrefix(dst, prefixHash)
	dst = appendLengthPrefixed(dst, key)
	dst = appendUint64(dst, version)
	dst = appendLengthPrefixed(dst, field)
	return dst
}

func listDataKey(key []byte, version uint64, index int64) []byte {
	dst := make([]byte, 0, len(prefixList)+1+lenMarkerSize+len(key)+8+8)
	dst = appendPrefix(dst, prefixList)
	dst = appendLengthPrefixed(dst, key)
	dst = appendUint64(dst, version)
	dst = appendSortableInt64(dst, index)
	return dst
}

func setDataKey(key []byte, version uint64, member []byte) []byte {
	dst := make([]byte, 0, len(prefixSet)+1+lenMarkerSize+len(key)+8+lenMarkerSize+len(member))
	dst = appendPrefix(dst, prefixSet)
	dst = appendLengthPrefixed(dst, key)
	dst = appendUint64(dst, version)
	dst = appendLengthPrefixed(dst, member)
	return dst
}

func zsetDictKey(key []byte, version uint64, member []byte) []byte {
	dst := make([]byte, 0, len(prefixZSetDict)+1+lenMarkerSize+len(key)+8+lenMarkerSize+len(member))
	dst = appendPrefix(dst, prefixZSetDict)
	dst = appendLengthPrefixed(dst, key)
	dst = appendUint64(dst, version)
	dst = appendLengthPrefixed(dst, member)
	return dst
}

// zsetScoreKey 需要在 score 相同比较 member 的字典序，因此在 score 之后直接添加分隔符再追加原始 member，让 member 的字典序决定最终顺序。
func zsetScoreKey(key []byte, version uint64, score float64, member []byte) []byte {
	dst := make([]byte, 0, len(prefixZSetScore)+1+lenMarkerSize+len(key)+8+8+1+len(member))
	dst = appendPrefix(dst, prefixZSetScore)
	dst = appendLengthPrefixed(dst, key)
	dst = appendUint64(dst, version)
	dst = append(dst, encodeZSetScore(score)...)
	dst = append(dst, memberSeparator)
	dst = append(dst, member...)
	return dst
}

func appendPrefix(dst []byte, prefix string) []byte {
	dst = append(dst, prefix...)
	dst = append(dst, 0)
	return dst
}

func appendLengthPrefixed(dst, comp []byte) []byte {
	var buf [lenMarkerSize]byte
	binary.BigEndian.PutUint32(buf[:], uint32(len(comp)))
	dst = append(dst, buf[:]...)
	return append(dst, comp...)
}

func appendUint64(dst []byte, v uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	return append(dst, buf[:]...)
}

func appendSortableInt64(dst []byte, v int64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(v)^0x8000000000000000)
	return append(dst, buf[:]...)
}

func encodeZSetScore(score float64) []byte {
	if math.IsNaN(score) {
		panic("redis: zset score NaN")
	}
	var buf [8]byte
	bits := math.Float64bits(score)
	if bits&(1<<63) != 0 {
		bits = ^bits
	} else {
		bits ^= 1 << 63
	}
	binary.BigEndian.PutUint64(buf[:], bits)
	return buf[:]
}
