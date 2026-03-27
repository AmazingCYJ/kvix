package main

import "github.com/gofiber/fiber/v2"

// registerRoutes 注册健康检查和 kvix 示例 API 路由。
func registerRoutes(app *fiber.App, srv *server) {
	app.Get("/healthz", srv.health)

	v1 := app.Group("/api/v1")
	v1.Post("/entries", srv.put)
	v1.Post("/entries/batch", srv.batchPut)
	v1.Get("/entries/:key", srv.getByParam)
	v1.Delete("/entries/:key", srv.deleteByParam)
	v1.Get("/keys", srv.listKeys)
	v1.Get("/stats", srv.stat)

	// Redis 风格命令统一挂在 /api/v1/redis 下，便于按数据结构分组阅读和维护。
	redis := v1.Group("/redis")

	// string 命令组当前先补齐 set/get/del/expire/ttl 这组基础能力。
	redisString := redis.Group("/string")
	redisString.Post("/set", srv.redisStringSet)
	redisString.Post("/get", srv.redisStringGet)
	redisString.Post("/del", srv.redisStringDel)
	redisString.Post("/expire", srv.redisStringExpire)
	redisString.Post("/ttl", srv.redisStringTTL)

	// hash 命令组暴露 field 级读写与存在性判断能力。
	redisHash := redis.Group("/hash")
	redisHash.Post("/hset", srv.redisHashHSet)
	redisHash.Post("/hget", srv.redisHashHGet)
	redisHash.Post("/hdel", srv.redisHashHDel)
	redisHash.Post("/hexists", srv.redisHashHExists)
	redisHash.Post("/hlen", srv.redisHashHLen)

	// list 命令组暴露双端写入、双端弹出、长度查询和范围读取能力。
	redisList := redis.Group("/list")
	redisList.Post("/lpush", srv.redisListLPush)
	redisList.Post("/rpush", srv.redisListRPush)
	redisList.Post("/lpop", srv.redisListLPop)
	redisList.Post("/rpop", srv.redisListRPop)
	redisList.Post("/llen", srv.redisListLLen)
	redisList.Post("/lrange", srv.redisListLRange)

	// set 命令组暴露集合成员增删、成员判断和全集读取能力。
	redisSet := redis.Group("/set")
	redisSet.Post("/sadd", srv.redisSetSAdd)
	redisSet.Post("/srem", srv.redisSetSRem)
	redisSet.Post("/sismember", srv.redisSetSIsMember)
	redisSet.Post("/scard", srv.redisSetSCard)
	redisSet.Post("/smembers", srv.redisSetSMembers)

	// zset 命令组暴露有序集合成员写入、删除、打分和区间读取能力。
	redisZSet := redis.Group("/zset")
	redisZSet.Post("/zadd", srv.redisZSetZAdd)
	redisZSet.Post("/zrem", srv.redisZSetZRem)
	redisZSet.Post("/zscore", srv.redisZSetZScore)
	redisZSet.Post("/zcard", srv.redisZSetZCard)
	redisZSet.Post("/zrange", srv.redisZSetZRange)
}
