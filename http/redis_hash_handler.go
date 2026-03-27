package main

import "github.com/gofiber/fiber/v2"

// redisHashHSet 处理 Redis 风格的 HSET 命令。
func (s *server) redisHashHSet(c *fiber.Ctx) error {
	// 1. 解析请求体，把命令参数映射到清晰的 DTO。
	var req redisHashSetRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}

	// 2. 校验 key、field、value 都已提供。
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if req.Field == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing field")
	}
	if req.Value == nil {
		return newHTTPError(fiber.StatusBadRequest, "missing value")
	}

	// 3. 调用底层 HSet，并拿到“本次是不是新增 field”的结果。
	added, err := s.redisStore.HSet([]byte(req.Key), []byte(req.Field), []byte(*req.Value))
	if err != nil {
		return mapDomainError(err)
	}

	// 4. 返回 added 字段，帮助调用方区分新增和覆盖写。
	return writeJSON(c, fiber.StatusOK, "ok", redisAddedResult{
		Added: added,
	})
}

// redisHashHGet 处理 Redis 风格的 HGET 命令。
func (s *server) redisHashHGet(c *fiber.Ctx) error {
	// 1. 解析请求并校验必填参数。
	var req redisFieldOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if req.Field == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing field")
	}

	// 2. 读取底层 hash field；如果 key/field 不存在或类型不匹配，会交给统一错误映射。
	value, err := s.redisStore.HGet([]byte(req.Key), []byte(req.Field))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 把读取结果返回给调用方，并显式带回 key/field 上下文。
	return writeJSON(c, fiber.StatusOK, "ok", redisHashValueResult{
		Key:   req.Key,
		Field: req.Field,
		Value: string(value),
	})
}

// redisHashHDel 处理 Redis 风格的 HDEL 命令。
func (s *server) redisHashHDel(c *fiber.Ctx) error {
	// 1. 解析并校验请求参数。
	var req redisFieldOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if req.Field == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing field")
	}

	// 2. HDel 返回 deleted，表示本次是否真的删除了一个现存 field。
	deleted, err := s.redisStore.HDel([]byte(req.Key), []byte(req.Field))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 返回稳定的删除响应结构。
	return writeJSON(c, fiber.StatusOK, "ok", redisDeletedResult{
		Deleted: deleted,
	})
}

// redisHashHExists 处理 Redis 风格的 HEXISTS 命令。
func (s *server) redisHashHExists(c *fiber.Ctx) error {
	// 1. 解析请求。
	var req redisFieldOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if req.Field == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing field")
	}

	// 2. 读取 field 是否存在。
	exists, err := s.redisStore.HExists([]byte(req.Key), []byte(req.Field))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 用 exists 字段把结果返回给调用方。
	return writeJSON(c, fiber.StatusOK, "ok", redisExistsResult{
		Exists: exists,
	})
}

// redisHashHLen 处理 Redis 风格的 HLEN 命令。
func (s *server) redisHashHLen(c *fiber.Ctx) error {
	// 1. 解析请求并确保 key 存在。
	var req redisKeyOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}

	// 2. 获取 hash 当前 field 数量；不存在的 key 会返回 0。
	count, err := s.redisStore.HLen([]byte(req.Key))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 统一包装成 count 响应结构。
	return writeJSON(c, fiber.StatusOK, "ok", redisCountResult{
		Count: count,
	})
}
