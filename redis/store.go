package redis

import (
	kvix "kvix"
	"kvix/common"
)

// RedisDataStore 封装底层 kvix 实例。
type RedisDataStore struct {
	db *kvix.DB
}

// NewRedisDataStore 构造 RedisDataStore。
func NewRedisDataStore(options common.Options) (*RedisDataStore, error) {
	db, err := kvix.Open(options)
	if err != nil {
		return nil, err
	}
	return &RedisDataStore{db: db}, nil
}

// Close 关闭底层数据库。
func (rds *RedisDataStore) Close() error {
	if rds == nil || rds.db == nil {
		return nil
	}
	return rds.db.Close()
}
