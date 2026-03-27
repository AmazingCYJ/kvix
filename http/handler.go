package main

import (
	"errors"
	kvix "kvix"
	common "kvix/common"

	"github.com/gofiber/fiber/v2"
)

type server struct {
	db *kvix.DB
}

func newServer(db *kvix.DB) *server {
	return &server{db: db}
}

func (s *server) health(c *fiber.Ctx) error {
	return writeJSON(c, fiber.StatusOK, "ok", map[string]string{"status": "ok"})
}

func (s *server) put(c *fiber.Ctx) error {
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

	if err := s.db.Put([]byte(req.Key), []byte(*req.Value)); err != nil {
		return err
	}

	return writeJSON(c, fiber.StatusOK, "ok", entryDTO{
		Key:   req.Key,
		Value: *req.Value,
	})
}

func (s *server) batchPut(c *fiber.Ctx) error {
	var req batchWriteRequest
	if err := c.BodyParser(&req); err != nil {
		return newHTTPError(fiber.StatusBadRequest, "failed to decode json")
	}
	if len(req.Entries) == 0 {
		return newHTTPError(fiber.StatusBadRequest, "empty entries")
	}

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

func (s *server) get(c *fiber.Ctx, key string) error {
	value, err := s.db.Get([]byte(key))
	if err != nil {
		return mapDomainError(err)
	}

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

func (s *server) delete(c *fiber.Ctx, key string) error {
	if err := s.db.Delete([]byte(key)); err != nil {
		if key != "" && errors.Is(err, common.ErrKeyNotFound) {
			return writeJSON(c, fiber.StatusOK, "ok", nil)
		}
		return err
	}
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
