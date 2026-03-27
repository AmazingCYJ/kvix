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

// metaKey 构造 metadata 主键。
// 这里不直接拼接原始字符串，而是带长度前缀，避免 key 本身包含分隔符时产生歧义。
func metaKey(key []byte) []byte {
	dst := make([]byte, 0, len(prefixMeta)+1+lenMarkerSize+len(key))
	dst = appendPrefix(dst, prefixMeta)
	dst = appendLengthPrefixed(dst, key)
	return dst
}

// metaVersionKey 构造版本追踪键，用来记录某个逻辑 key 最近使用到的版本号。
func metaVersionKey(key []byte) []byte {
	dst := make([]byte, 0, len(prefixMetaVersion)+1+lenMarkerSize+len(key))
	dst = appendPrefix(dst, prefixMetaVersion)
	dst = appendLengthPrefixed(dst, key)
	return dst
}

// hashDataKey 构造 hash field 的子键。
func hashDataKey(key []byte, version uint64, field []byte) []byte {
	dst := make([]byte, 0, len(prefixHash)+1+lenMarkerSize+len(key)+8+lenMarkerSize+len(field))
	dst = appendPrefix(dst, prefixHash)
	dst = appendLengthPrefixed(dst, key)
	dst = appendUint64(dst, version)
	dst = appendLengthPrefixed(dst, field)
	return dst
}

// listDataKey 构造 list 元素子键。
// index 会先转换成“按字节序可排序”的 int64，保证迭代器扫描时逻辑顺序正确。
func listDataKey(key []byte, version uint64, index int64) []byte {
	dst := make([]byte, 0, len(prefixList)+1+lenMarkerSize+len(key)+8+8)
	dst = appendPrefix(dst, prefixList)
	dst = appendLengthPrefixed(dst, key)
	dst = appendUint64(dst, version)
	dst = appendSortableInt64(dst, index)
	return dst
}

// setDataKey 构造 set 成员子键。
func setDataKey(key []byte, version uint64, member []byte) []byte {
	dst := make([]byte, 0, len(prefixSet)+1+lenMarkerSize+len(key)+8+lenMarkerSize+len(member))
	dst = appendPrefix(dst, prefixSet)
	dst = appendLengthPrefixed(dst, key)
	dst = appendUint64(dst, version)
	dst = appendLengthPrefixed(dst, member)
	return dst
}

// zsetDictKey 构造 zset 中 member -> score 的字典索引键。
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

// appendPrefix 在键开头写入模块前缀，并追加一个 0 字节分隔符。
// 这样不同逻辑空间之间不会因为前缀重叠而混淆。
func appendPrefix(dst []byte, prefix string) []byte {
	dst = append(dst, prefix...)
	dst = append(dst, 0)
	return dst
}

// appendLengthPrefixed 先写入长度，再写入原始组件内容。
// 这是本模块避免“简单字符串拼接歧义”的关键手段。
func appendLengthPrefixed(dst, comp []byte) []byte {
	var buf [lenMarkerSize]byte
	binary.BigEndian.PutUint32(buf[:], uint32(len(comp)))
	dst = append(dst, buf[:]...)
	return append(dst, comp...)
}

// appendUint64 把版本号等定宽整数按大端序写入键中。
func appendUint64(dst []byte, v uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	return append(dst, buf[:]...)
}

// appendSortableInt64 把有符号整数映射成按字节序仍能保持数值顺序的编码。
// list 的 head/tail 索引可能为负值，因此普通大端序不能直接满足排序要求。
func appendSortableInt64(dst []byte, v int64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(v)^0x8000000000000000)
	return append(dst, buf[:]...)
}

// encodeZSetScore 把 float64 score 编码成可排序字节序列。
// 这样按字节序遍历 zset:score 前缀时，就等价于按 score 升序遍历。
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
