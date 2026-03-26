package redis

import (
	"encoding/binary"
	"errors"

	"bitcask-my/common"
)

var setMemberPlaceholder = []byte{1}

// SAdd 通过 set:<key>:<version>:<member> 子键记录元素，metadata.size 始终反映这些子键的个数。
func (rds *RedisDataStore) SAdd(key []byte, members ...[]byte) (uint32, error) {
	if len(members) == 0 {
		return 0, nil
	}

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

// SRem 删除当前集合中的成员。
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

// SIsMember 检查元素是否在集合中。
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
	for it.Rewind(); it.Vaild(); it.Next() {
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

func buildSetPrefix(key []byte, version uint64) []byte {
	dst := make([]byte, 0, len(prefixSet)+1+lenMarkerSize+len(key)+8)
	dst = appendPrefix(dst, prefixSet)
	dst = appendLengthPrefixed(dst, key)
	dst = appendUint64(dst, version)
	return dst
}
