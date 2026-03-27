package redis

import (
	kvix "kvix"
	"kvix/common"
)

// RedisDataStore 封装底层 kvix 实例，并在其上提供 Redis 风格数据结构语义。
// 模块本身不实现 Redis 网络协议，而是把字符串、集合等结构映射到底层 KV 引擎。
type RedisDataStore struct {
	db     *kvix.DB
	ownsDB bool
}

// NewRedisDataStore 构造 RedisDataStore。
func NewRedisDataStore(options common.Options) (*RedisDataStore, error) {
	// Redis 语义层本身不管理文件或索引，它直接复用底层 kvix 的打开流程。
	db, err := kvix.Open(options)
	if err != nil {
		return nil, err
	}
	return &RedisDataStore{db: db, ownsDB: true}, nil
}

// NewRedisDataStoreFromDB 基于已打开的 kvix.DB 构造 RedisDataStore。
// 共享模式下 RedisDataStore 不拥有底层 DB 的生命周期。
func NewRedisDataStoreFromDB(db *kvix.DB) *RedisDataStore {
	if db == nil {
		return nil
	}
	return &RedisDataStore{db: db, ownsDB: false}
}

// Close 关闭底层数据库。
func (rds *RedisDataStore) Close() error {
	if rds == nil || rds.db == nil {
		// 允许对空对象或已关闭对象重复调用 Close，方便上层做 defer。
		return nil
	}
	if !rds.ownsDB {
		return nil
	}
	return rds.db.Close()
}
