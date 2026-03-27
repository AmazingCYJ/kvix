package redis

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"

	"kvix/common"
)

const (
	// zsetScoreEncodedSize 是 score 排序编码后的固定字节长度。
	zsetScoreEncodedSize = 8
)

var (
	// zsetScorePlaceholder 是 score 索引子键的占位值；真正重要的信息在键本身的排序顺序里。
	zsetScorePlaceholder        = []byte{1}
	errInvalidZSetScoreEncoding = errors.New("redis: invalid zset score encoding")
)

// zset 通过 dict 索引记录 member -> score，同时通过 score 索引按 score/member 字典序排好序，双轨结构确保 ZRange 能稳定取得排序结果。
func (rds *RedisDataStore) ZAdd(key []byte, score float64, member []byte) (bool, error) {
	// 1. 先校验 score，并读取当前 zset metadata。
	if math.IsNaN(score) {
		return false, ErrZSetScoreNaN
	}
	meta, err := rds.findMetadata(key)
	if err != nil {
		return false, err
	}

	var next metadata
	if meta == nil {
		next, err = rds.newMetadataForType(key, redisTypeZSet)
		if err != nil {
			return false, err
		}
	} else {
		if err := expectType(meta, redisTypeZSet); err != nil {
			return false, err
		}
		next = *meta
	}

	// 2. dict 索引负责 member -> score，先看当前 member 是否已存在。
	dictKey := zsetDictKey(key, next.version, member)
	encodedScore := encodeZSetScore(score)
	existing, getErr := rds.db.Get(dictKey)
	if getErr != nil && !errors.Is(getErr, common.ErrKeyNotFound) {
		return false, getErr
	}
	if getErr == nil {
		// 2.1 已存在 member 时只更新 score，并同步替换排序索引。
		if bytes.Equal(existing, encodedScore) {
			return false, nil
		}
		oldScore, err := decodeZSetScore(existing)
		if err != nil {
			return false, err
		}
		wb := rds.db.NewWriteBatch(common.DefaultWriteBatchOptions)
		if err := wb.Put(dictKey, encodedScore); err != nil {
			return false, err
		}
		oldScoreKey := zsetScoreKey(key, next.version, oldScore, member)
		if _, oldErr := rds.db.Get(oldScoreKey); oldErr == nil {
			if err := wb.Delete(oldScoreKey); err != nil {
				return false, err
			}
		} else if !errors.Is(oldErr, common.ErrKeyNotFound) {
			return false, oldErr
		}
		if err := wb.Put(zsetScoreKey(key, next.version, score, member), zsetScorePlaceholder); err != nil {
			return false, err
		}
		if err := wb.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}

	// 3. 新 member 需要同时写入 dict 索引、score 索引和 metadata.size。
	wb := rds.db.NewWriteBatch(common.DefaultWriteBatchOptions)
	if err := wb.Put(dictKey, encodedScore); err != nil {
		return false, err
	}
	if err := wb.Put(zsetScoreKey(key, next.version, score, member), zsetScorePlaceholder); err != nil {
		return false, err
	}
	next.size++
	if err := wb.Put(metaKey(key), encodeMetadata(next)); err != nil {
		return false, err
	}
	if err := wb.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// ZRem 删除一个 member，同时清理 dict 索引和 score 排序索引。
func (rds *RedisDataStore) ZRem(key, member []byte) (bool, error) {
	meta, err := rds.loadZSetMetadata(key)
	if err != nil {
		return false, err
	}
	if meta == nil {
		return false, nil
	}

	dictKey := zsetDictKey(key, meta.version, member)
	encoded, err := rds.db.Get(dictKey)
	if err != nil {
		if errors.Is(err, common.ErrKeyNotFound) {
			return false, nil
		}
		return false, err
	}

	score, err := decodeZSetScore(encoded)
	if err != nil {
		return false, err
	}

	wb := rds.db.NewWriteBatch(common.DefaultWriteBatchOptions)
	if err := wb.Delete(dictKey); err != nil {
		return false, err
	}
	if err := wb.Delete(zsetScoreKey(key, meta.version, score, member)); err != nil {
		return false, err
	}
	if meta.size == 1 {
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

// ZScore 查询 member 对应的 score。
func (rds *RedisDataStore) ZScore(key, member []byte) (float64, error) {
	meta, err := rds.loadZSetMetadata(key)
	if err != nil {
		return 0, err
	}
	if meta == nil {
		return 0, common.ErrKeyNotFound
	}
	data, err := rds.db.Get(zsetDictKey(key, meta.version, member))
	if err != nil {
		if errors.Is(err, common.ErrKeyNotFound) {
			return 0, common.ErrKeyNotFound
		}
		return 0, err
	}
	score, err := decodeZSetScore(data)
	if err != nil {
		return 0, err
	}
	return score, nil
}

// ZCard 返回有序集合大小。
func (rds *RedisDataStore) ZCard(key []byte) (uint32, error) {
	meta, err := rds.loadZSetMetadata(key)
	if err != nil {
		return 0, err
	}
	if meta == nil {
		return 0, nil
	}
	return meta.size, nil
}

// ZRange 依赖 score 索引按 score 升序（相同 score 按 member 字典序）遍历，同时支持负数索引并自动裁剪越界区间。
func (rds *RedisDataStore) ZRange(key []byte, start, stop int64) ([][]byte, error) {
	meta, err := rds.loadZSetMetadata(key)
	if err != nil {
		return nil, err
	}
	if meta == nil || meta.size == 0 {
		return [][]byte{}, nil
	}

	length := int64(meta.size)
	from, to, ok := normalizeZRange(start, stop, length)
	if !ok {
		return [][]byte{}, nil
	}

	prefix := buildZSetScorePrefix(key, meta.version)
	it := rds.db.NewIterator(common.IteratorOptions{Prefix: prefix})
	defer it.Close()

	result := make([][]byte, 0, int(to-from+1))
	expected := int(to - from + 1)
	pos := int64(0)
	for it.Rewind(); it.Vaild(); it.Next() {
		fullKey := it.Key()
		base := len(prefix) + zsetScoreEncodedSize
		if len(fullKey) <= base {
			continue
		}
		suffix := fullKey[base:]
		sepIdx := bytes.IndexByte(suffix, memberSeparator)
		if sepIdx < 0 {
			continue
		}
		memberStart := base + sepIdx + 1
		if memberStart > len(fullKey) {
			continue
		}
		if pos > to {
			break
		}
		if pos >= from {
			memberBytes := make([]byte, len(fullKey)-memberStart)
			copy(memberBytes, fullKey[memberStart:])
			result = append(result, memberBytes)
			if len(result) >= expected {
				break
			}
		}
		pos++
	}
	return result, nil
}

// loadZSetMetadata 加载并校验 zset 类型 metadata。
func (rds *RedisDataStore) loadZSetMetadata(key []byte) (*metadata, error) {
	meta, err := rds.findMetadata(key)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, nil
	}
	if err := expectType(meta, redisTypeZSet); err != nil {
		return nil, err
	}
	return meta, nil
}

// buildZSetScorePrefix 构造 score 索引前缀，便于按 score 顺序扫描当前版本全部成员。
func buildZSetScorePrefix(key []byte, version uint64) []byte {
	dst := make([]byte, 0, len(prefixZSetScore)+1+lenMarkerSize+len(key)+8)
	dst = appendPrefix(dst, prefixZSetScore)
	dst = appendLengthPrefixed(dst, key)
	dst = appendUint64(dst, version)
	return dst
}

// normalizeZRange 把 start/stop 规范化为合法闭区间，并兼容负数下标。
func normalizeZRange(start, stop, length int64) (int64, int64, bool) {
	if length == 0 {
		return 0, -1, false
	}
	if start < 0 {
		start = length + start
	}
	if stop < 0 {
		stop = length + stop
	}
	if start < 0 {
		start = 0
	}
	if stop < 0 {
		return 0, -1, false
	}
	if start >= length {
		return 0, -1, false
	}
	if stop >= length {
		stop = length - 1
	}
	if start > stop {
		return 0, -1, false
	}
	return start, stop, true
}

// decodeZSetScore 把可排序的 8 字节编码还原成 float64。
// 这个编码方案的目标不是最省空间，而是让字节序遍历顺序与 score 数值顺序一致。
func decodeZSetScore(encoded []byte) (float64, error) {
	if len(encoded) != zsetScoreEncodedSize {
		return 0, errInvalidZSetScoreEncoding
	}
	bits := binary.BigEndian.Uint64(encoded)
	if bits&(1<<63) != 0 {
		bits ^= 1 << 63
	} else {
		bits = ^bits
	}
	return math.Float64frombits(bits), nil
}
