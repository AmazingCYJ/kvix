package main

import (
	"math"
	"time"

	"github.com/gofiber/fiber/v2"
)

// redisStringSet 处理 Redis 风格的 SET 命令。
// 这个接口既支持普通写入，也支持“写入时顺带设置 TTL”。
func (s *server) redisStringSet(c *fiber.Ctx) error {
	// 1. 先把 JSON 请求体解析到明确的 DTO 中。
	var req redisStringSetRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}

	// 2. 明确校验必填字段，避免把不完整数据带到底层存储层。
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if req.Value == nil {
		return newHTTPError(fiber.StatusBadRequest, "missing value")
	}

	// 3. 如果调用方传了 ttl_seconds，就把秒数转换成 time.Duration；
	//    没传时保持 0，底层会把它解释为“不过期”。
	ttl := time.Duration(0)
	if req.TTLSeconds != nil {
		ttl = time.Duration(*req.TTLSeconds) * time.Second
	}

	// 4. 把 HTTP 请求转换成底层 RedisDataStore 的字符串写入调用。
	if err := s.redisStore.Set([]byte(req.Key), []byte(*req.Value), ttl); err != nil {
		return mapDomainError(err)
	}

	// 5. 成功后返回统一响应包装，并把写入结果显式回显给调用方。
	return writeJSON(c, fiber.StatusOK, "ok", entryDTO{
		Key:   req.Key,
		Value: *req.Value,
	})
}

// redisStringGet 处理 Redis 风格的 GET 命令。
func (s *server) redisStringGet(c *fiber.Ctx) error {
	// 1. 读取请求体，并确保调用方真的提供了 key。
	var req redisKeyOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}

	// 2. 读取底层字符串值；如果 key 不存在或类型不匹配，会交给统一错误映射处理。
	value, err := s.redisStore.Get([]byte(req.Key))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 用固定 DTO 返回 key 和 value，保持接口契约稳定。
	return writeJSON(c, fiber.StatusOK, "ok", entryDTO{
		Key:   req.Key,
		Value: string(value),
	})
}

// redisStringDel 处理 Redis 风格的 DEL 命令。
func (s *server) redisStringDel(c *fiber.Ctx) error {
	// 1. 解析请求并验证 key。
	var req redisKeyOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}

	// 2. 底层返回 deleted 布尔值，表示这次删除是否真的命中了现存 key。
	deleted, err := s.redisStore.Del([]byte(req.Key))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 即使 deleted=false，也仍然返回 200，让 HTTP 层保持 Redis 风格的幂等删除语义。
	return writeJSON(c, fiber.StatusOK, "ok", redisDeletedResult{
		Deleted: deleted,
	})
}

// redisStringExpire 处理 Redis 风格的 EXPIRE 命令。
func (s *server) redisStringExpire(c *fiber.Ctx) error {
	// 1. 解析请求体。
	var req redisStringExpireRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}

	// 2. 校验 key 和 ttl_seconds 都存在，避免模糊语义。
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if req.TTLSeconds == nil {
		return newHTTPError(fiber.StatusBadRequest, "missing ttl_seconds")
	}

	// 3. 把秒数转换成底层 API 使用的 time.Duration。
	updated, err := s.redisStore.Expire([]byte(req.Key), time.Duration(*req.TTLSeconds)*time.Second)
	if err != nil {
		return mapDomainError(err)
	}

	// 4. 返回本次是否成功更新了一个已存在 key 的 TTL。
	return writeJSON(c, fiber.StatusOK, "ok", redisUpdatedResult{
		Updated: updated,
	})
}

// redisStringTTL 处理 Redis 风格的 TTL 命令。
func (s *server) redisStringTTL(c *fiber.Ctx) error {
	// 1. 解析并校验请求体。
	var req redisKeyOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}

	// 2. 读取底层剩余 TTL；key 不存在时要返回 404，而不是伪造一个数值。
	ttl, err := s.redisStore.TTL([]byte(req.Key))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 把 time.Duration 转成更适合 HTTP/JSON 表达的秒数。
	return writeJSON(c, fiber.StatusOK, "ok", redisTTLResult{
		TTLSeconds: ttlDurationToSeconds(ttl),
	})
}

// ttlDurationToSeconds 把底层 Duration 统一换算成接口层暴露的 ttl_seconds。
func ttlDurationToSeconds(ttl time.Duration) int64 {
	// -1 表示 key 没有设置过期时间，这个语义需要原样保留给调用方。
	if ttl < 0 {
		return -1
	}

	// 0 表示已经没有剩余寿命了，或者时间刚好被裁到 0。
	if ttl == 0 {
		return 0
	}

	// 正数 TTL 向上取整成秒，避免“刚设置 3 秒就立刻看到 2 秒”的体验过于跳变。
	seconds := int64(math.Ceil(ttl.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}
