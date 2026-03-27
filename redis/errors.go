package redis

import "errors"

// ErrWrongType 表示对类型不匹配的逻辑 key 执行了非法命令。
var ErrWrongType = errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")

// ErrZSetScoreNaN 表示在有序集合的 score 中不能使用 NaN。
var ErrZSetScoreNaN = errors.New("redis: zset score NaN")

// ErrDecodeMetadata 表示元数据结构无法解码（长度不足或字段非法）。
var ErrDecodeMetadata = errors.New("redis: metadata decode error")

// ErrMetadataPayloadNonString 表示非 string 类型的 metadata 带了额外 payload。
var ErrMetadataPayloadNonString = errors.New("redis: metadata payload requires string type")

// ErrNotImplemented 表示暂未实现的 Redis 操作。
var ErrNotImplemented = errors.New("redis: not implemented")
