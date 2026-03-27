package redis

import (
	"time"

	"kvix/common"
)

func expireAtFromTTL(ttl time.Duration) int64 {
	if ttl <= 0 {
		// 约定 TTL<=0 表示不设置过期时间。
		return 0
	}
	return time.Now().Add(ttl).UnixNano()
}

// Set 将字符串 payload 和 metadata 联合写入 meta:<key>，并依赖惰性过期来处理失效。
func (rds *RedisDataStore) Set(key, value []byte, ttl time.Duration) error {
	// 1. 先读取现有元数据，判断 key 是否存在以及是否需要执行惰性过期。
	meta, err := rds.findMetadata(key)
	if err != nil {
		return err
	}

	var next metadata
	switch {
	case meta == nil:
		// 2.1 新 key 直接分配 string 类型的首个版本。
		next, err = rds.newMetadataForType(key, redisTypeString)
	case meta.typ != redisTypeString:
		// 2.2 旧 key 类型不兼容时，先保留历史版本号，再创建新的 string 版本。
		if err := rds.recordVersionIfHigher(key, meta.version); err != nil {
			return err
		}
		next, err = rds.newMetadataForType(key, redisTypeString)
	default:
		// 2.3 同类型覆盖写时递增版本，让旧 payload 自动失效。
		next, err = rds.nextMetadataVersion(key, *meta)
	}
	if err != nil {
		return err
	}

	// 3. 根据 TTL 写入新的过期时间，并复位 string 不需要的集合边界字段。
	next.expireAt = expireAtFromTTL(ttl)
	next.size = 1
	next.head = 0
	next.tail = 0
	// 对 string 来说，size/head/tail 这些字段只是占位统一结构，不参与真实集合计算。

	// 4. 将 metadata 和 payload 一次编码后写入 meta:<key>。
	encoded, err := encodeMetadataWithValue(next, value)
	if err != nil {
		return err
	}
	return rds.db.Put(metaKey(key), encoded)
}

// Get 依赖惰性过期和类型检查返回字符串 payload。
func (rds *RedisDataStore) Get(key []byte) ([]byte, error) {
	// findMetadataAndValue 会先执行惰性过期，因此这里拿到的结果已经是“当前仍可见”的状态。
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
		// 不存在的 key 删除后仍然视为“未发生删除”。
		return false, nil
	}
	if err := rds.deleteMetadata(key); err != nil {
		return false, err
	}
	return true, nil
}

// Expire 在 metadata 上设置新的过期时间，不会更换类型或 payload。
func (rds *RedisDataStore) Expire(key []byte, ttl time.Duration) (bool, error) {
	// 1. 统一走 metadata 读取流程，让过期判断和类型恢复保持一致。
	meta, payload, err := rds.findMetadataAndValue(key)
	if err != nil {
		return false, err
	}
	if meta == nil {
		return false, nil
	}

	// 2. 只更新 expireAt，保留现有类型、版本和 payload。
	meta.expireAt = expireAtFromTTL(ttl)
	var encoded []byte
	if meta.typ == redisTypeString {
		// string 的 payload 和 metadata 共存在一个值里，所以更新 TTL 时要把 payload 一起带回去。
		encoded, err = encodeMetadataWithValue(*meta, payload)
	} else {
		// 复合类型只需要更新 metadata 本体，真实元素仍在各自子键里。
		encoded = encodeMetadata(*meta)
	}
	if err != nil {
		return false, err
	}
	// 3. 回写 metadata，让后续访问按新的 TTL 执行惰性过期。
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
		// 没有过期时间时返回 -1，保持与 Redis 常见语义一致。
		return time.Duration(-1), nil
	}
	remaining := time.Until(time.Unix(0, meta.expireAt))
	if remaining < 0 {
		// 即使时钟刚好跨过过期点，也不要返回负持续时间。
		remaining = 0
	}
	return remaining, nil
}
