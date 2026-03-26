package redis

import (
	"time"

	"bitcask-my/common"
)

func expireAtFromTTL(ttl time.Duration) int64 {
	if ttl <= 0 {
		return 0
	}
	return time.Now().Add(ttl).UnixNano()
}

// Set 将字符串 payload 和 metadata 联合写入 meta:<key>，并依赖惰性过期来处理失效。
func (rds *RedisDataStore) Set(key, value []byte, ttl time.Duration) error {
	meta, err := rds.findMetadata(key)
	if err != nil {
		return err
	}

	var next metadata
	switch {
	case meta == nil:
		next, err = rds.newMetadataForType(key, redisTypeString)
	case meta.typ != redisTypeString:
		if err := rds.recordVersionIfHigher(key, meta.version); err != nil {
			return err
		}
		next, err = rds.newMetadataForType(key, redisTypeString)
	default:
		next, err = rds.nextMetadataVersion(key, *meta)
	}
	if err != nil {
		return err
	}

	next.expireAt = expireAtFromTTL(ttl)
	next.size = 1
	next.head = 0
	next.tail = 0

	encoded, err := encodeMetadataWithValue(next, value)
	if err != nil {
		return err
	}
	return rds.db.Put(metaKey(key), encoded)
}

// Get 依赖惰性过期和类型检查返回字符串 payload。
func (rds *RedisDataStore) Get(key []byte) ([]byte, error) {
	meta, payload, err := rds.findMetadataAndValue(key)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, common.ErrKeyNotFound
	}
	if err := expectType(meta, redisTypeString); err != nil {
		return nil, err
	}
	return payload, nil
}

// Del 删除 meta:<key> 使当前版本失效。
func (rds *RedisDataStore) Del(key []byte) (bool, error) {
	meta, err := rds.findMetadata(key)
	if err != nil {
		return false, err
	}
	if meta == nil {
		return false, nil
	}
	if err := rds.deleteMetadata(key); err != nil {
		return false, err
	}
	return true, nil
}

// Expire 在 metadata 上设置新的过期时间，不会更换类型或 payload。
func (rds *RedisDataStore) Expire(key []byte, ttl time.Duration) (bool, error) {
	meta, payload, err := rds.findMetadataAndValue(key)
	if err != nil {
		return false, err
	}
	if meta == nil {
		return false, nil
	}

	meta.expireAt = expireAtFromTTL(ttl)
	var encoded []byte
	if meta.typ == redisTypeString {
		encoded, err = encodeMetadataWithValue(*meta, payload)
	} else {
		encoded = encodeMetadata(*meta)
	}
	if err != nil {
		return false, err
	}
	if err := rds.db.Put(metaKey(key), encoded); err != nil {
		return false, err
	}
	return true, nil
}

// TTL 通过 metadata 的 expireAt 计算剩余时间，过期会被惰性删除。
func (rds *RedisDataStore) TTL(key []byte) (time.Duration, error) {
	meta, err := rds.findMetadata(key)
	if err != nil {
		return 0, err
	}
	if meta == nil {
		return 0, common.ErrKeyNotFound
	}
	if meta.expireAt == 0 {
		return time.Duration(-1), nil
	}
	remaining := time.Until(time.Unix(0, meta.expireAt))
	if remaining < 0 {
		remaining = 0
	}
	return remaining, nil
}
