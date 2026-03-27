package main

import "github.com/gofiber/fiber/v2"

// redisListLPush 处理 Redis 风格的 LPUSH 命令。
func (s *server) redisListLPush(c *fiber.Ctx) error {
	// 1. 解析请求体。
	var req redisValuesRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}

	// 2. 校验 key 和 values。
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if len(req.Values) == 0 {
		return newHTTPError(fiber.StatusBadRequest, "missing values")
	}

	// 3. 把 HTTP 层的 []string 转成底层需要的 [][]byte，再执行左侧压入。
	count, err := s.redisStore.LPush([]byte(req.Key), stringsToByteSlices(req.Values)...)
	if err != nil {
		return mapDomainError(err)
	}

	// 4. 返回压入后的列表长度。
	return writeJSON(c, fiber.StatusOK, "ok", redisCountResult{
		Count: count,
	})
}

// redisListRPush 处理 Redis 风格的 RPUSH 命令。
func (s *server) redisListRPush(c *fiber.Ctx) error {
	// 1. 解析请求体。
	var req redisValuesRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}

	// 2. 校验输入参数。
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if len(req.Values) == 0 {
		return newHTTPError(fiber.StatusBadRequest, "missing values")
	}

	// 3. 执行右侧追加。
	count, err := s.redisStore.RPush([]byte(req.Key), stringsToByteSlices(req.Values)...)
	if err != nil {
		return mapDomainError(err)
	}

	// 4. 返回追加后的长度。
	return writeJSON(c, fiber.StatusOK, "ok", redisCountResult{
		Count: count,
	})
}

// redisListLPop 处理 Redis 风格的 LPOP 命令。
func (s *server) redisListLPop(c *fiber.Ctx) error {
	// 1. 解析请求并校验 key。
	var req redisKeyOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}

	// 2. 弹出左侧元素；空列表或不存在的 key 会映射成 404。
	value, err := s.redisStore.LPop([]byte(req.Key))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 返回被弹出的值。
	return writeJSON(c, fiber.StatusOK, "ok", redisValueResult{
		Value: string(value),
	})
}

// redisListRPop 处理 Redis 风格的 RPOP 命令。
func (s *server) redisListRPop(c *fiber.Ctx) error {
	// 1. 解析请求并校验 key。
	var req redisKeyOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}

	// 2. 弹出右侧元素。
	value, err := s.redisStore.RPop([]byte(req.Key))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 返回结果值。
	return writeJSON(c, fiber.StatusOK, "ok", redisValueResult{
		Value: string(value),
	})
}

// redisListLLen 处理 Redis 风格的 LLEN 命令。
func (s *server) redisListLLen(c *fiber.Ctx) error {
	// 1. 解析请求。
	var req redisKeyOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}

	// 2. 读取当前列表长度；不存在的 key 返回 0。
	count, err := s.redisStore.LLen([]byte(req.Key))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 输出 count 字段。
	return writeJSON(c, fiber.StatusOK, "ok", redisCountResult{
		Count: count,
	})
}

// redisListLRange 处理 Redis 风格的 LRANGE 命令。
func (s *server) redisListLRange(c *fiber.Ctx) error {
	// 1. 解析请求体。
	var req redisRangeRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}

	// 2. 校验 key、start、stop 都存在。
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if req.Start == nil {
		return newHTTPError(fiber.StatusBadRequest, "missing start")
	}
	if req.Stop == nil {
		return newHTTPError(fiber.StatusBadRequest, "missing stop")
	}

	// 3. 读取范围结果，并把底层 [][]byte 转成适合 JSON 的 []string。
	values, err := s.redisStore.LRange([]byte(req.Key), *req.Start, *req.Stop)
	if err != nil {
		return mapDomainError(err)
	}

	// 4. 保持输出顺序与底层 list 逻辑顺序一致。
	return writeJSON(c, fiber.StatusOK, "ok", redisValuesResult{
		Values: byteSlicesToStrings(values),
	})
}
