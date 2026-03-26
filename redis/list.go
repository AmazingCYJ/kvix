package redis

import (
	"errors"

	"bitcask-my/common"
)

// 列表元数据通过 head/tail 维护逻辑索引边界：head 指向最左元素，tail 指向最右元素，
// dataKey 的索引就是 head+offset，保证逻辑序列与物理键顺序一致。
func (rds *RedisDataStore) LPush(key []byte, values ...[]byte) (uint32, error) {
	if len(values) == 0 {
		meta, err := rds.loadListMetadata(key)
		if err != nil {
			return 0, err
		}
		if meta == nil {
			return 0, nil
		}
		return meta.size, nil
	}

	meta, err := rds.findMetadata(key)
	if err != nil {
		return 0, err
	}

	var next metadata
	if meta == nil {
		next, err = rds.newMetadataForType(key, redisTypeList)
		if err != nil {
			return 0, err
		}
		next.head = 0
		next.tail = -1
		next.size = 0
	} else {
		if err := expectType(meta, redisTypeList); err != nil {
			return 0, err
		}
		next = *meta
		if next.size == 0 {
			next.head = 0
			next.tail = -1
		}
	}

	wb := rds.db.NewWriteBatch(common.DefaultWriteBatchOptions)
	for _, value := range values {
		if next.size == 0 {
			next.head--
			next.tail = next.head
		} else {
			next.head--
		}
		if err := wb.Put(listDataKey(key, next.version, next.head), value); err != nil {
			return 0, err
		}
		next.size++
	}
	if err := wb.Put(metaKey(key), encodeMetadata(next)); err != nil {
		return 0, err
	}
	if err := wb.Commit(); err != nil {
		return 0, err
	}
	return next.size, nil
}

func (rds *RedisDataStore) RPush(key []byte, values ...[]byte) (uint32, error) {
	if len(values) == 0 {
		meta, err := rds.loadListMetadata(key)
		if err != nil {
			return 0, err
		}
		if meta == nil {
			return 0, nil
		}
		return meta.size, nil
	}

	meta, err := rds.findMetadata(key)
	if err != nil {
		return 0, err
	}

	var next metadata
	if meta == nil {
		next, err = rds.newMetadataForType(key, redisTypeList)
		if err != nil {
			return 0, err
		}
		next.head = 0
		next.tail = -1
		next.size = 0
	} else {
		if err := expectType(meta, redisTypeList); err != nil {
			return 0, err
		}
		next = *meta
		if next.size == 0 {
			next.head = 0
			next.tail = -1
		}
	}

	wb := rds.db.NewWriteBatch(common.DefaultWriteBatchOptions)
	for _, value := range values {
		if next.size == 0 {
			next.tail++
			next.head = next.tail
		} else {
			next.tail++
		}
		if err := wb.Put(listDataKey(key, next.version, next.tail), value); err != nil {
			return 0, err
		}
		next.size++
	}
	if err := wb.Put(metaKey(key), encodeMetadata(next)); err != nil {
		return 0, err
	}
	if err := wb.Commit(); err != nil {
		return 0, err
	}
	return next.size, nil
}

func (rds *RedisDataStore) LPop(key []byte) ([]byte, error) {
	meta, err := rds.loadListMetadata(key)
	if err != nil {
		return nil, err
	}
	if meta == nil || meta.size == 0 {
		return nil, common.ErrKeyNotFound
	}

	idx := meta.head
	dataKey := listDataKey(key, meta.version, idx)
	value, err := rds.db.Get(dataKey)
	if err != nil {
		if errors.Is(err, common.ErrKeyNotFound) {
			return nil, common.ErrKeyNotFound
		}
		return nil, err
	}

	wb := rds.db.NewWriteBatch(common.DefaultWriteBatchOptions)
	if err := wb.Delete(dataKey); err != nil {
		return nil, err
	}
	if meta.size == 1 {
		if err := wb.Delete(metaKey(key)); err != nil {
			return nil, err
		}
	} else {
		next := *meta
		next.size--
		next.head++
		if err := wb.Put(metaKey(key), encodeMetadata(next)); err != nil {
			return nil, err
		}
	}
	if err := wb.Commit(); err != nil {
		return nil, err
	}
	return value, nil
}

func (rds *RedisDataStore) RPop(key []byte) ([]byte, error) {
	meta, err := rds.loadListMetadata(key)
	if err != nil {
		return nil, err
	}
	if meta == nil || meta.size == 0 {
		return nil, common.ErrKeyNotFound
	}

	idx := meta.tail
	dataKey := listDataKey(key, meta.version, idx)
	value, err := rds.db.Get(dataKey)
	if err != nil {
		if errors.Is(err, common.ErrKeyNotFound) {
			return nil, common.ErrKeyNotFound
		}
		return nil, err
	}

	wb := rds.db.NewWriteBatch(common.DefaultWriteBatchOptions)
	if err := wb.Delete(dataKey); err != nil {
		return nil, err
	}
	if meta.size == 1 {
		if err := wb.Delete(metaKey(key)); err != nil {
			return nil, err
		}
	} else {
		next := *meta
		next.size--
		next.tail--
		if err := wb.Put(metaKey(key), encodeMetadata(next)); err != nil {
			return nil, err
		}
	}
	if err := wb.Commit(); err != nil {
		return nil, err
	}
	return value, nil
}

func (rds *RedisDataStore) LLen(key []byte) (uint32, error) {
	meta, err := rds.loadListMetadata(key)
	if err != nil {
		return 0, err
	}
	if meta == nil {
		return 0, nil
	}
	return meta.size, nil
}

// LRange 支持负数索引且自动裁剪范围，结果按照 head...tail 的逻辑顺序输出。
func (rds *RedisDataStore) LRange(key []byte, start, stop int64) ([][]byte, error) {
	meta, err := rds.loadListMetadata(key)
	if err != nil {
		return nil, err
	}
	if meta == nil || meta.size == 0 {
		return [][]byte{}, nil
	}

	size := int64(meta.size)
	normalizedStart, normalizedStop, ok := normalizeListRange(size, start, stop)
	if !ok {
		return [][]byte{}, nil
	}

	results := make([][]byte, 0, normalizedStop-normalizedStart+1)
	for offset := normalizedStart; offset <= normalizedStop; offset++ {
		idx := meta.head + offset
		value, err := rds.db.Get(listDataKey(key, meta.version, idx))
		if err != nil {
			if errors.Is(err, common.ErrKeyNotFound) {
				return nil, common.ErrKeyNotFound
			}
			return nil, err
		}
		results = append(results, value)
	}
	return results, nil
}

func (rds *RedisDataStore) loadListMetadata(key []byte) (*metadata, error) {
	meta, err := rds.findMetadata(key)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, nil
	}
	if err := expectType(meta, redisTypeList); err != nil {
		return nil, err
	}
	return meta, nil
}

func normalizeListRange(size, start, stop int64) (int64, int64, bool) {
	if size <= 0 {
		return 0, -1, false
	}
	if start < 0 {
		start += size
	}
	if stop < 0 {
		stop += size
	}
	if start < 0 {
		start = 0
	}
	if stop < 0 {
		return 0, -1, false
	}
	if start >= size {
		return 0, -1, false
	}
	if stop >= size {
		stop = size - 1
	}
	if start > stop {
		return 0, -1, false
	}
	return start, stop, true
}
