package redis

import "encoding/binary"

// metadataEncodedSize 是 metadata 固定编码后的总字节数。
// 之所以固定长度，是为了让元数据读取与解码过程足够简单直接。
const metadataEncodedSize = 37

type redisType uint8

const (
	// redisTypeString 表示字符串类型，payload 直接附着在 metadata 后面。
	redisTypeString redisType = 1
	// redisTypeHash 表示哈希类型，field 数据保存在独立子键中。
	redisTypeHash redisType = 2
	// redisTypeList 表示列表类型，元素通过 head/tail 对应的索引子键保存。
	redisTypeList redisType = 3
	// redisTypeSet 表示集合类型，成员存在性通过独立子键表达。
	redisTypeSet redisType = 4
	// redisTypeZSet 表示有序集合类型，需要同时维护 dict 和 score 两类子键。
	redisTypeZSet redisType = 5
)

// metadata 描述逻辑 key 的类型、TTL、版本以及复合结构边界。
// Redis 风格命令在真正读写子键前，都先读取这份元数据来做类型检查和过期判断。
type metadata struct {
	typ      redisType // 逻辑类型，决定后续应如何解释子键
	expireAt int64     // 过期时间（UnixNano），0 表示永不过期
	version  uint64    // 版本号，用来隔离新旧逻辑 key 的子键空间
	size     uint32    // 当前逻辑 key 下有效元素数量
	head     int64     // list 左边界索引，仅 list 使用
	tail     int64     // list 右边界索引，仅 list 使用
}

// encodeMetadata 使用固定 big-endian 字节序将 metadata 编成 37 字节的紧凑序列。
// 固定布局虽然不如 varint 节省空间，但它让 decode 过程更直接，也更适合教学阅读。
func encodeMetadata(meta metadata) []byte {
	dst := make([]byte, metadataEncodedSize)
	dst[0] = byte(meta.typ)
	binary.BigEndian.PutUint64(dst[1:], uint64(meta.expireAt))
	binary.BigEndian.PutUint64(dst[9:], meta.version)
	binary.BigEndian.PutUint32(dst[17:], meta.size)
	binary.BigEndian.PutUint64(dst[21:], uint64(meta.head))
	binary.BigEndian.PutUint64(dst[29:], uint64(meta.tail))
	return dst
}

// decodeMetadata 按固定布局把字节序列还原成 metadata。
// 它会先验证长度和类型，防止上层把损坏数据误当成合法元数据使用。
func decodeMetadata(data []byte) (metadata, error) {
	if len(data) < metadataEncodedSize {
		return metadata{}, ErrDecodeMetadata
	}
	var meta metadata
	meta.typ = redisType(data[0])
	if !isValidType(meta.typ) {
		return metadata{}, ErrDecodeMetadata
	}
	meta.expireAt = int64(binary.BigEndian.Uint64(data[1:9]))
	meta.version = binary.BigEndian.Uint64(data[9:17])
	meta.size = binary.BigEndian.Uint32(data[17:21])
	meta.head = int64(binary.BigEndian.Uint64(data[21:29]))
	meta.tail = int64(binary.BigEndian.Uint64(data[29:37]))
	return meta, nil
}

// encodeMetadataWithValue 允许 string 类型把 payload 直接拼接在 metadata 后面。
// 这样 string 读取只需要一次底层 Get，而复合类型仍然通过 metadata + 子键来表达结构。
func encodeMetadataWithValue(meta metadata, value []byte) ([]byte, error) {
	if meta.typ != redisTypeString && len(value) > 0 {
		return nil, ErrMetadataPayloadNonString
	}
	base := encodeMetadata(meta)
	out := make([]byte, len(base)+len(value))
	copy(out, base)
	copy(out[len(base):], value)
	return out, nil
}

// decodeMetadataAndValue 同时解出 metadata 和 string payload。
// 对非 string 类型来说，如果 metadata 后面还带 payload，则视为非法编码。
func decodeMetadataAndValue(data []byte) (metadata, []byte, error) {
	meta, err := decodeMetadata(data)
	if err != nil {
		return metadata{}, nil, err
	}
	if len(data) == metadataEncodedSize {
		return meta, nil, nil
	}
	if meta.typ != redisTypeString {
		return metadata{}, nil, ErrMetadataPayloadNonString
	}
	return meta, data[metadataEncodedSize:], nil
}

// isValidType 判断 metadata 中的类型字段是否属于已定义枚举。
func isValidType(typ redisType) bool {
	return typ >= redisTypeString && typ <= redisTypeZSet
}
