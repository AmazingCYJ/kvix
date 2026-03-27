package main

import (
	"errors"
	kvix "kvix"
	common "kvix/common"
	redisstore "kvix/redis"

	"github.com/gofiber/fiber/v2"
)

// server 负责把 HTTP 请求转换为对 kvix DB 的调用，并把领域结果映射为响应 DTO。
type server struct {
	db         *kvix.DB
	redisStore *redisstore.RedisDataStore
}

func newServer(db *kvix.DB, redisStore *redisstore.RedisDataStore) *server {
	return &server{db: db, redisStore: redisStore}
}

func (s *server) health(c *fiber.Ctx) error {
	return writeJSON(c, fiber.StatusOK, "ok", map[string]string{"status": "ok"})
}

// put 处理单条写入请求。
func (s *server) put(c *fiber.Ctx) error {
	// 1. 解析并校验请求体，确保 key 与 value 都具备最小合法性。
	var req entryWriteRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if req.Key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	if req.Value == nil {
		return newHTTPError(fiber.StatusBadRequest, "missing value")
	}

	// 2. 调用底层数据库写入数据。
	if err := s.db.Put([]byte(req.Key), []byte(*req.Value)); err != nil {
		return err
	}

	// 3. 将结果封装成固定响应 DTO，保持接口契约稳定。
	return writeJSON(c, fiber.StatusOK, "ok", entryDTO{
		Key:   req.Key,
		Value: *req.Value,
	})
}

// batchPut 处理批量写入请求，并返回实际写入条数。
func (s *server) batchPut(c *fiber.Ctx) error {
	// 1. 解析批量请求体，确认 entries 至少包含一条记录。
	var req batchWriteRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if len(req.Entries) == 0 {
		return newHTTPError(fiber.StatusBadRequest, "empty entries")
	}

	// 2. 逐条写入数据库；如果中途失败，就返回已成功写入的数量。
	writtenCount := 0
	for _, entry := range req.Entries {
		if entry.Key == "" || entry.Value == nil {
			return writeJSON(c, fiber.StatusBadRequest, "invalid batch entry", batchWriteResult{
				WrittenCount: intPtr(writtenCount),
			})
		}
		if err := s.db.Put([]byte(entry.Key), []byte(*entry.Value)); err != nil {
			return writeJSON(c, fiber.StatusInternalServerError, "batch write failed", batchWriteResult{
				WrittenCount: intPtr(writtenCount),
			})
		}
		writtenCount++
	}

	// 3. 全部成功后返回批量写入结果。
	return writeJSON(c, fiber.StatusOK, "ok", batchWriteResult{
		Count: intPtr(writtenCount),
	})
}

func (s *server) getByParam(c *fiber.Ctx) error {
	key := c.Params("key")
	if key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	return s.get(c, key)
}

// get 负责读取单个 key 并映射成固定响应结构。
func (s *server) get(c *fiber.Ctx, key string) error {
	// 1. 调用底层数据库读取 value。
	value, err := s.db.Get([]byte(key))
	if err != nil {
		return mapDomainError(err)
	}

	// 2. 将数据库结果转换成 API DTO。
	return writeJSON(c, fiber.StatusOK, "ok", entryDTO{
		Key:   key,
		Value: string(value),
	})
}

func (s *server) deleteByParam(c *fiber.Ctx) error {
	key := c.Params("key")
	if key == "" {
		return newHTTPError(fiber.StatusBadRequest, "missing key")
	}
	return s.delete(c, key)
}

// delete 删除单个 key，并保持删除接口的幂等语义。
func (s *server) delete(c *fiber.Ctx, key string) error {
	// 1. 调用底层删除逻辑。
	if err := s.db.Delete([]byte(key)); err != nil {
		// 2. key 不存在时仍然返回 200，保持 HTTP 删除接口幂等。
		if key != "" && errors.Is(err, common.ErrKeyNotFound) {
			return writeJSON(c, fiber.StatusOK, "ok", nil)
		}
		return err
	}
	// 3. 删除成功后返回统一响应结构。
	return writeJSON(c, fiber.StatusOK, "ok", nil)
}

func (s *server) listKeys(c *fiber.Ctx) error {
	keys, err := s.db.ListKeys()
	if err != nil {
		return err
	}

	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, string(key))
	}

	return writeJSON(c, fiber.StatusOK, "ok", result)
}

func (s *server) stat(c *fiber.Ctx) error {
	return writeJSON(c, fiber.StatusOK, "ok", s.db.Stat())
}

func intPtr(v int) *int {
	return &v
}
