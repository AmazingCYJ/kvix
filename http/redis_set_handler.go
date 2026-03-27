package main

import "github.com/gofiber/fiber/v2"

// redisSetSAdd 处理 Redis 风格的 SADD 命令。
func (s *server) redisSetSAdd(c *fiber.Ctx) error {
	// 1. 解析请求体。
	var req redisMembersRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}

	// 2. 校验 key 和 members 都存在。
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if len(req.Members) == 0 {
		return newHTTPError(fiber.StatusBadRequest, "missing members")
	}

	// 3. 执行集合新增，并返回这次真正新增了多少个成员。
	count, err := s.redisStore.SAdd([]byte(req.Key), stringsToByteSlices(req.Members)...)
	if err != nil {
		return mapDomainError(err)
	}

	// 4. 用统一 count 结构输出结果。
	return writeJSON(c, fiber.StatusOK, "ok", redisCountResult{
		Count: count,
	})
}

// redisSetSRem 处理 Redis 风格的 SREM 命令。
func (s *server) redisSetSRem(c *fiber.Ctx) error {
	// 1. 解析请求体。
	var req redisMembersRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}

	// 2. 校验输入参数。
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if len(req.Members) == 0 {
		return newHTTPError(fiber.StatusBadRequest, "missing members")
	}

	// 3. 删除集合成员，并返回实际删除数量。
	count, err := s.redisStore.SRem([]byte(req.Key), stringsToByteSlices(req.Members)...)
	if err != nil {
		return mapDomainError(err)
	}

	// 4. 输出删除计数。
	return writeJSON(c, fiber.StatusOK, "ok", redisCountResult{
		Count: count,
	})
}

// redisSetSIsMember 处理 Redis 风格的 SISMEMBER 命令。
func (s *server) redisSetSIsMember(c *fiber.Ctx) error {
	// 1. 解析请求。
	var req redisMemberOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if req.Member == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing member")
	}

	// 2. 查询成员是否存在。
	exists, err := s.redisStore.SIsMember([]byte(req.Key), []byte(req.Member))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 返回 exists 布尔值。
	return writeJSON(c, fiber.StatusOK, "ok", redisExistsResult{
		Exists: exists,
	})
}

// redisSetSCard 处理 Redis 风格的 SCARD 命令。
func (s *server) redisSetSCard(c *fiber.Ctx) error {
	// 1. 解析请求并校验 key。
	var req redisKeyOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}

	// 2. 获取集合当前元素个数。
	count, err := s.redisStore.SCard([]byte(req.Key))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 返回 count。
	return writeJSON(c, fiber.StatusOK, "ok", redisCountResult{
		Count: count,
	})
}

// redisSetSMembers 处理 Redis 风格的 SMEMBERS 命令。
func (s *server) redisSetSMembers(c *fiber.Ctx) error {
	// 1. 解析请求并校验 key。
	var req redisKeyOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}

	// 2. 读取集合当前所有成员。
	members, err := s.redisStore.SMembers([]byte(req.Key))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 把底层二进制成员列表转换成 HTTP 层更直观的字符串数组。
	return writeJSON(c, fiber.StatusOK, "ok", redisMembersResult{
		Members: byteSlicesToStrings(members),
	})
}
