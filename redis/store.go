package redis

import (
	bitcaskmy "bitcask-my"
	"bitcask-my/common"
)

// RedisDataStore 封装底层 bitcask 实例。
type RedisDataStore struct {
	db *bitcaskmy.DB
}

// NewRedisDataStore 构造 RedisDataStore。
func NewRedisDataStore(options common.Options) (*RedisDataStore, error) {
	db, err := bitcaskmy.Open(options)
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
