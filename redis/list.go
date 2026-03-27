package redis

import (
	"errors"

	"kvix/common"
)

// 列表元数据通过 head/tail 维护逻辑索引边界：head 指向最左元素，tail 指向最右元素，
// dataKey 的索引就是 head+offset，保证逻辑序列与物理键顺序一致。
func (rds *RedisDataStore) LPush(key []byte, values ...[]byte) (uint32, error) {
	// 1. 空参数直接返回当前长度，保持与 Redis 风格接口兼容。
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

	// 2. 读取或创建列表元数据，必要时初始化 head/tail 边界。
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

	// 3. 每写入一个元素就向左扩展 head，并把元素写到当前版本的子键上。
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
	// 4. 最后统一回写 metadata，保证 head/tail/size 与批量写入结果一致。
	if err := wb.Put(metaKey(key), encodeMetadata(next)); err != nil {
		return 0, err
	}
	if err := wb.Commit(); err != nil {
		return 0, err
	}
	return next.size, nil
}

// RPush 把元素依次追加到列表右侧，并返回追加后的逻辑长度。
// 与 LPush 对称，它主要修改的是 tail 边界。
func (rds *RedisDataStore) RPush(key []byte, values ...[]byte) (uint32, error) {
	// 1. 空参数时只返回当前长度，不创建新 key。
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

	// 2. 读取或创建 metadata，确保后续 tail 扩展建立在最新版本上。
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

	// 3. 每写入一个元素就向右扩展 tail，并把元素落到对应索引子键。
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
	// 4. 最后统一回写 metadata，让边界和元素个数与本次写入保持一致。
	if err := wb.Put(metaKey(key), encodeMetadata(next)); err != nil {
		return 0, err
	}
	if err := wb.Commit(); err != nil {
		return 0, err
	}
	return next.size, nil
}

// LPop 弹出并返回列表最左侧元素。
// 当最后一个元素被弹出时，会顺带删除 metadata，让整个 key 逻辑上消失。
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

// RPop 弹出并返回列表最右侧元素。
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

// LLen 返回列表当前元素个数。
// 不存在的 key 视为长度 0，这与 Redis 的习惯保持一致。
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

// loadListMetadata 加载并校验列表类型的 metadata。
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

// normalizeListRange 把可能包含负数的范围转换成 [0, size) 内的合法闭区间。
// 返回 ok=false 代表范围裁剪后为空。
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
