package redis

import (
	"encoding/binary"
	"errors"

	"kvix/common"
)

// setMemberPlaceholder 是 set 成员子键的占位值。
// 对集合来说，“键存在”就已经表示成员存在，因此 value 本身不需要保存真实负载。
var setMemberPlaceholder = []byte{1}

// SAdd 通过 set:<key>:<version>:<member> 子键记录元素，metadata.size 始终反映这些子键的个数。
func (rds *RedisDataStore) SAdd(key []byte, members ...[]byte) (uint32, error) {
	// 1. 没有成员输入时直接返回 0，避免制造空集合元数据。
	if len(members) == 0 {
		return 0, nil
	}

	// 2. 加载或创建集合 metadata，确定当前生效版本。
	meta, err := rds.findMetadata(key)
	if err != nil {
		return 0, err
	}

	var next metadata
	if meta == nil {
		next, err = rds.newMetadataForType(key, redisTypeSet)
		if err != nil {
			return 0, err
		}
	} else {
		if err := expectType(meta, redisTypeSet); err != nil {
			return 0, err
		}
		next = *meta
	}

	// 3. 先去重，再过滤出当前版本下还不存在的成员。
	unique := uniqueMembers(members)
	newMembers := make([][]byte, 0, len(unique))
	for _, member := range unique {
		dataKey := setDataKey(key, next.version, member)
		if _, err := rds.db.Get(dataKey); err == nil {
			continue
		} else if errors.Is(err, common.ErrKeyNotFound) {
			newMembers = append(newMembers, member)
		} else {
			return 0, err
		}
	}

	if len(newMembers) == 0 {
		return 0, nil
	}

	// 4. 通过 WriteBatch 一次写入所有新增成员，并同步更新 metadata.size。
	next.size += uint32(len(newMembers))
	wb := rds.db.NewWriteBatch(common.DefaultWriteBatchOptions)
	for _, member := range newMembers {
		if err := wb.Put(setDataKey(key, next.version, member), setMemberPlaceholder); err != nil {
			return 0, err
		}
	}
	if err := wb.Put(metaKey(key), encodeMetadata(next)); err != nil {
		return 0, err
	}
	if err := wb.Commit(); err != nil {
		return 0, err
	}
	return uint32(len(newMembers)), nil
}

// SRem 删除当前集合中的成员，并返回实际删除的成员数。
func (rds *RedisDataStore) SRem(key []byte, members ...[]byte) (uint32, error) {
	if len(members) == 0 {
		return 0, nil
	}
	meta, err := rds.loadSetMetadata(key)
	if err != nil {
		return 0, err
	}
	if meta == nil {
		return 0, nil
	}

	unique := uniqueMembers(members)
	removals := make([][]byte, 0, len(unique))
	for _, member := range unique {
		dataKey := setDataKey(key, meta.version, member)
		if _, err := rds.db.Get(dataKey); err == nil {
			removals = append(removals, member)
		} else if errors.Is(err, common.ErrKeyNotFound) {
			continue
		} else {
			return 0, err
		}
	}

	removed := uint32(len(removals))
	if removed == 0 {
		return 0, nil
	}

	wb := rds.db.NewWriteBatch(common.DefaultWriteBatchOptions)
	for _, member := range removals {
		if err := wb.Delete(setDataKey(key, meta.version, member)); err != nil {
			return 0, err
		}
	}
	if removed >= meta.size {
		if err := wb.Delete(metaKey(key)); err != nil {
			return 0, err
		}
	} else {
		next := *meta
		next.size -= removed
		if err := wb.Put(metaKey(key), encodeMetadata(next)); err != nil {
			return 0, err
		}
	}
	if err := wb.Commit(); err != nil {
		return 0, err
	}
	return removed, nil
}

// SIsMember 检查成员是否存在于当前集合版本中。
func (rds *RedisDataStore) SIsMember(key, member []byte) (bool, error) {
	meta, err := rds.findMetadata(key)
	if err != nil {
		return false, err
	}
	if meta == nil {
		return false, nil
	}
	if err := expectType(meta, redisTypeSet); err != nil {
		return false, err
	}
	if _, err := rds.db.Get(setDataKey(key, meta.version, member)); err == nil {
		return true, nil
	} else if errors.Is(err, common.ErrKeyNotFound) {
		return false, nil
	} else {
		return false, err
	}
}

// SCard 返回集合大小。
// 不存在的 key 返回 0，而不是报错。
func (rds *RedisDataStore) SCard(key []byte) (uint32, error) {
	meta, err := rds.loadSetMetadata(key)
	if err != nil {
		return 0, err
	}
	if meta == nil {
		return 0, nil
	}
	return meta.size, nil
}

// SMembers 返回当前版本的所有成员。
// 它通过前缀迭代扫描 set 子键，再从复合键中把 member 片段解出来。
func (rds *RedisDataStore) SMembers(key []byte) ([][]byte, error) {
	meta, err := rds.loadSetMetadata(key)
	if err != nil {
		return nil, err
	}
	if meta == nil || meta.size == 0 {
		return [][]byte{}, nil
	}
	prefix := buildSetPrefix(key, meta.version)
	it := rds.db.NewIterator(common.IteratorOptions{Prefix: prefix})
	defer it.Close()
	members := make([][]byte, 0, meta.size)
	for it.Rewind(); it.Valid(); it.Next() {
		fullKey := it.Key()
		if len(fullKey) < len(prefix)+lenMarkerSize {
			continue
		}
		start := len(prefix)
		memberLen := binary.BigEndian.Uint32(fullKey[start : start+lenMarkerSize])
		start += lenMarkerSize
		end := start + int(memberLen)
		if end > len(fullKey) {
			continue
		}
		member := make([]byte, memberLen)
		copy(member, fullKey[start:end])
		members = append(members, member)
	}
	return members, nil
}

// loadSetMetadata 加载并校验 set 类型 metadata。
func (rds *RedisDataStore) loadSetMetadata(key []byte) (*metadata, error) {
	meta, err := rds.findMetadata(key)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, nil
	}
	if err := expectType(meta, redisTypeSet); err != nil {
		return nil, err
	}
	return meta, nil
}

// uniqueMembers 去掉一次命令里的重复成员，避免重复写入和重复计数。
func uniqueMembers(members [][]byte) [][]byte {
	seen := make(map[string]struct{}, len(members))
	out := make([][]byte, 0, len(members))
	for _, member := range members {
		key := string(member)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, member)
	}
	return out
}

// buildSetPrefix 构造 set 子键的公共前缀，供前缀迭代器扫描当前版本的全部成员。
func buildSetPrefix(key []byte, version uint64) []byte {
	dst := make([]byte, 0, len(prefixSet)+1+lenMarkerSize+len(key)+8)
	dst = appendPrefix(dst, prefixSet)
	dst = appendLengthPrefixed(dst, key)
	dst = appendUint64(dst, version)
	return dst
}
