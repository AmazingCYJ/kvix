package redis

import "encoding/binary"

const metadataEncodedSize = 37

type redisType uint8

const (
	redisTypeString redisType = 1
	redisTypeHash   redisType = 2
	redisTypeList   redisType = 3
	redisTypeSet    redisType = 4
	redisTypeZSet   redisType = 5
)

// metadata 描述逻辑 key 的类型、TTL、版本以及复合结构边界。
// Redis 风格命令在真正读写子键前，都先读取这份元数据来做类型检查和过期判断。
type metadata struct {
	typ      redisType
	expireAt int64
	version  uint64
	size     uint32
	head     int64
	tail     int64
}

// encodeMetadata 使用固定 big-endian 字节序将 metadata 编成 37 字节的紧凑序列。
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

// decodeMetadata 会验证长度并保证类型属于已定义枚举。
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

// encodeMetadataWithValue 允许 string 类型将值直接拼接在 metadata 后，其他类型附加 payload 会返回 ErrMetadataPayloadNonString。
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

// decodeMetadataAndValue 同时返回 metadata 和 string 类型的 payload（如果存在）。
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

func isValidType(typ redisType) bool {
	return typ >= redisTypeString && typ <= redisTypeZSet
}
