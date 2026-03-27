package redis

import (
	"encoding/binary"
	"errors"
	"time"

	"kvix/common"
)

// findMetadata 负责加载元数据并执行惰性过期逻辑，供复合类型复用。
func (rds *RedisDataStore) findMetadata(key []byte) (*metadata, error) {
	data, err := rds.loadMetadataEntry(key)
	if err != nil {
		if errors.Is(err, common.ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}
	meta, err := decodeMetadata(data)
	if err != nil {
		return nil, err
	}
	expired, err := rds.handleMetadataExpiration(key, meta)
	if err != nil {
		return nil, err
	}
	if expired {
		return nil, nil
	}
	return &meta, nil
}

// findMetadataAndValue 提供字符串类型一次性读取 metadata + payload 的能力。
func (rds *RedisDataStore) findMetadataAndValue(key []byte) (*metadata, []byte, error) {
	data, err := rds.loadMetadataEntry(key)
	if err != nil {
		if errors.Is(err, common.ErrKeyNotFound) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	meta, payload, err := decodeMetadataAndValue(data)
	if err != nil {
		return nil, nil, err
	}
	expired, err := rds.handleMetadataExpiration(key, meta)
	if err != nil {
		return nil, nil, err
	}
	if expired {
		return nil, nil, nil
	}
	return &meta, payload, nil
}

func (rds *RedisDataStore) loadMetadataEntry(key []byte) ([]byte, error) {
	return rds.db.Get(metaKey(key))
}

func (rds *RedisDataStore) handleMetadataExpiration(key []byte, meta metadata) (bool, error) {
	if !meta.isExpired(time.Now()) {
		return false, nil
	}
	if err := rds.recordVersionIfHigher(key, meta.version); err != nil {
		return false, err
	}
	if err := rds.deleteMetadata(key); err != nil && !errors.Is(err, common.ErrKeyNotFound) {
		return false, err
	}
	return true, nil
}

func (rds *RedisDataStore) deleteMetadata(key []byte) error {
	return rds.db.Delete(metaKey(key))
}

// newMetadataForType 创建新的 meta 时参考 version 追踪，避免重用旧子键。
func (rds *RedisDataStore) newMetadataForType(key []byte, typ redisType) (metadata, error) {
	last, err := rds.loadVersionNumber(key)
	if err != nil {
		return metadata{}, err
	}
	next := last + 1
	if err := rds.storeVersionNumber(key, next); err != nil {
		return metadata{}, err
	}
	return metadata{
		typ:     typ,
		version: next,
	}, nil
}

func (rds *RedisDataStore) nextMetadataVersion(key []byte, meta metadata) (metadata, error) {
	meta.version++
	if err := rds.storeVersionNumber(key, meta.version); err != nil {
		return metadata{}, err
	}
	return meta, nil
}

func (rds *RedisDataStore) loadVersionNumber(key []byte) (uint64, error) {
	raw, err := rds.db.Get(metaVersionKey(key))
	if err != nil {
		if errors.Is(err, common.ErrKeyNotFound) {
			return 0, nil
		}
		return 0, err
	}
	if len(raw) < 8 {
		return 0, nil
	}
	return binary.BigEndian.Uint64(raw[:8]), nil
}

func (rds *RedisDataStore) storeVersionNumber(key []byte, version uint64) error {
	return rds.db.Put(metaVersionKey(key), encodeVersion(version))
}

func (rds *RedisDataStore) recordVersionIfHigher(key []byte, version uint64) error {
	current, err := rds.loadVersionNumber(key)
	if err != nil {
		return err
	}
	if version > current {
		return rds.storeVersionNumber(key, version)
	}
	return nil
}

func encodeVersion(version uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], version)
	return buf[:]
}

func expectType(meta *metadata, want redisType) error {
	if meta == nil {
		return ErrWrongType
	}
	if meta.typ != want {
		return ErrWrongType
	}
	return nil
}

func (meta metadata) isExpired(now time.Time) bool {
	return meta.expireAt > 0 && now.UnixNano() >= meta.expireAt
}
