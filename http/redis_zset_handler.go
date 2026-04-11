package main

import (
	"errors"

	common "kvix/common"

	"github.com/gofiber/fiber/v2"
)

// redisZSetZAdd 处理 Redis 风格的 ZADD 命令。
func (s *server) redisZSetZAdd(c *fiber.Ctx) error {
	// 1. 解析请求体。
	var req redisZSetAddRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}

	// 2. 校验 key、member、score 都已提供。
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if req.Member == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing member")
	}
	if req.Score == nil {
		return newHTTPError(fiber.StatusBadRequest, "missing score")
	}

	// 3. 先读取旧 score，用来判断这次 ZADD 是否真的改变了 zset 状态。
	previousScore, existed, err := s.lookupZSetScore([]byte(req.Key), []byte(req.Member))
	if err != nil {
		return err
	}

	// 4. 执行底层 ZADD；底层返回值只告诉我们“是不是新增成员”。
	added, err := s.redisStore.ZAdd([]byte(req.Key), *req.Score, []byte(req.Member))
	if err != nil {
		return mapDomainError(err)
	}

	// 5. HTTP 层对外暴露 updated 语义：新增成员或修改 score 都算一次有效更新。
	updated := added
	if !updated && existed && previousScore != *req.Score {
		updated = true
	}

	// 6. 返回统一更新结果。
	return writeJSON(c, fiber.StatusOK, "ok", redisUpdatedResult{
		Updated: updated,
	})
}

// redisZSetZRem 处理 Redis 风格的 ZREM 命令。
func (s *server) redisZSetZRem(c *fiber.Ctx) error {
	// 1. 解析请求并校验 key/member。
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

	// 2. 底层 ZRem 返回是否真的删除了一个现存 member。
	removed, err := s.redisStore.ZRem([]byte(req.Key), []byte(req.Member))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 对外统一映射成 updated 语义。
	return writeJSON(c, fiber.StatusOK, "ok", redisUpdatedResult{
		Updated: removed,
	})
}

// redisZSetZScore 处理 Redis 风格的 ZSCORE 命令。
func (s *server) redisZSetZScore(c *fiber.Ctx) error {
	// 1. 解析请求并校验参数。
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

	// 2. 查询 member 当前 score。
	score, err := s.redisStore.ZScore([]byte(req.Key), []byte(req.Member))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 返回 score 数值。
	return writeJSON(c, fiber.StatusOK, "ok", redisScoreResult{
		Score: score,
	})
}

// redisZSetZCard 处理 Redis 风格的 ZCARD 命令。
func (s *server) redisZSetZCard(c *fiber.Ctx) error {
	// 1. 解析请求并校验 key。
	var req redisKeyOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}

	// 2. 获取当前有序集合大小。
	count, err := s.redisStore.ZCard([]byte(req.Key))
	if err != nil {
		return mapDomainError(err)
	}

	// 3. 返回 count。
	return writeJSON(c, fiber.StatusOK, "ok", redisCountResult{
		Count: count,
	})
}

// redisZSetZRange 处理 Redis 风格的 ZRANGE 命令。
func (s *server) redisZSetZRange(c *fiber.Ctx) error {
	// 1. 解析请求体。
	var req redisRangeRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}

	// 2. 校验 key、start、stop。
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if req.Start == nil {
		return newHTTPError(fiber.StatusBadRequest, "missing start")
	}
	if req.Stop == nil {
		return newHTTPError(fiber.StatusBadRequest, "missing stop")
	}

	// 3. 获取按 score/member 顺序排序后的成员切片。
	members, err := s.redisStore.ZRange([]byte(req.Key), *req.Start, *req.Stop)
	if err != nil {
		return mapDomainError(err)
	}

	// 4. 返回成员数组。
	return writeJSON(c, fiber.StatusOK, "ok", redisMembersResult{
		Members: byteSlicesToStrings(members),
	})
}

// lookupZSetScore 用于在 HTTP 层判断某个 member 在 ZADD 之前是否存在、旧分数是多少。
func (s *server) lookupZSetScore(key, member []byte) (float64, bool, error) {
	score, err := s.redisStore.ZScore(key, member)
	if err == nil {
		return score, true, nil
	}
	if errors.Is(err, common.ErrKeyNotFound) {
		return 0, false, nil
	}
	return 0, false, mapDomainError(err)
}
