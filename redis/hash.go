package redis

import (
	"errors"

	"kvix/common"
)

// RedisHash 相关数据布局说明：
//    meta:<key>                                      // metadata 记录类型、版本、field count
//    hash:<key>:<version>:<field>                    // 每个字段的 payload 直接存储于对应子键，version 避免复用旧字段
// metadata.size 永远反映当前版本下的 field 数量，HSet/HDel 要同步更新，最后一个 field 删除时还要删除 metadata。

// HSet 将 field 写入 hash 结构，返回是否新增字段。新增字段和 metadata 更新需通过 WriteBatch 原子写 meta:<key> + hash 子键。
func (rds *RedisDataStore) HSet(key, field, value []byte) (bool, error) {
	// 1. 加载 metadata，决定是新建 hash 还是在现有版本上继续写 field。
	meta, err := rds.findMetadata(key)
	if err != nil {
		return false, err
	}

	var next metadata
	needMetaWrite := false
	isNewField := false

	if meta == nil {
		// 2.1 首次写入时创建 hash 类型 metadata，并把 size 初始化为 1。
		next, err = rds.newMetadataForType(key, redisTypeHash)
		if err != nil {
			return false, err
		}
		next.size = 1
		needMetaWrite = true
		isNewField = true
	} else {
		// 2.2 已存在 key 时先校验类型，再检查 field 是覆盖还是新增。
		if err := expectType(meta, redisTypeHash); err != nil {
			return false, err
		}
		next = *meta
		dataKey := hashDataKey(key, next.version, field)
		if _, err := rds.db.Get(dataKey); err == nil {
			isNewField = false
		} else if errors.Is(err, common.ErrKeyNotFound) {
			isNewField = true
		} else {
			return false, err
		}
		if isNewField {
			next.size++
			needMetaWrite = true
		}
	}

	dataKey := hashDataKey(key, next.version, field)

	if needMetaWrite {
		// 3. 新字段需要原子写 metadata + field 数据，确保 size 与子键集合一致。
		wb := rds.db.NewWriteBatch(common.DefaultWriteBatchOptions)
		if err := wb.Put(metaKey(key), encodeMetadata(next)); err != nil {
			return false, err
		}
		if err := wb.Put(dataKey, value); err != nil {
			return false, err
		}
		if err := wb.Commit(); err != nil {
			return false, err
		}
		return isNewField, nil
	}

	// 4. 覆盖已有 field 时只更新子键 payload，不改 metadata.size。
	if err := rds.db.Put(dataKey, value); err != nil {
		return false, err
	}
	return false, nil
}

// HGet 读取 field，类型错或 key/field 缺失均返回 ErrWrongType/ErrKeyNotFound。
func (rds *RedisDataStore) HGet(key, field []byte) ([]byte, error) {
	meta, err := rds.loadHashMetadata(key)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, common.ErrKeyNotFound
	}
	dataKey := hashDataKey(key, meta.version, field)
	value, err := rds.db.Get(dataKey)
	if err != nil {
		if errors.Is(err, common.ErrKeyNotFound) {
			return nil, common.ErrKeyNotFound
		}
		return nil, err
	}
	return value, nil
}

// HDel 删除 field，若最后一个 field 被删则一并删除 metadata。
func (rds *RedisDataStore) HDel(key, field []byte) (bool, error) {
	meta, err := rds.loadHashMetadata(key)
	if err != nil {
		return false, err
	}
	if meta == nil {
		return false, nil
	}
	dataKey := hashDataKey(key, meta.version, field)
	if _, err := rds.db.Get(dataKey); err != nil {
		if errors.Is(err, common.ErrKeyNotFound) {
			return false, nil
		}
		return false, err
	}

	wb := rds.db.NewWriteBatch(common.DefaultWriteBatchOptions)
	if err := wb.Delete(dataKey); err != nil {
		return false, err
	}

	if meta.size <= 1 {
		if err := wb.Delete(metaKey(key)); err != nil {
			return false, err
		}
	} else {
		next := *meta
		next.size--
		if err := wb.Put(metaKey(key), encodeMetadata(next)); err != nil {
			return false, err
		}
	}

	if err := wb.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// HExists 判断 field 是否存在，错误仅在类型不匹配或底层出错时返回。
func (rds *RedisDataStore) HExists(key, field []byte) (bool, error) {
	meta, err := rds.loadHashMetadata(key)
	if err != nil {
		return false, err
	}
	if meta == nil {
		return false, nil
	}
	dataKey := hashDataKey(key, meta.version, field)
	if _, err := rds.db.Get(dataKey); err != nil {
		if errors.Is(err, common.ErrKeyNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// HLen 返回 hash 的 field 数量，key 不存在视为 0，保证 Redis 风格行为。
func (rds *RedisDataStore) HLen(key []byte) (uint32, error) {
	meta, err := rds.loadHashMetadata(key)
	if err != nil {
		return 0, err
	}
	if meta == nil {
		return 0, nil
	}
	return meta.size, nil
}

func (rds *RedisDataStore) loadHashMetadata(key []byte) (*metadata, error) {
	meta, err := rds.findMetadata(key)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, nil
	}
	if err := expectType(meta, redisTypeHash); err != nil {
		return nil, err
	}
	return meta, nil
}
