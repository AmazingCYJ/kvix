package main

import (
	"errors"
	common "kvix/common"

	"github.com/gofiber/fiber/v2"
)

// httpError 表示已经完成 HTTP 状态码映射的应用层错误。
type httpError struct {
	Status  int
	Message string
}

func (e *httpError) Error() string {
	return e.Message
}

func newHTTPError(status int, message string) error {
	return &httpError{Status: status, Message: message}
}

// mapDomainError 把底层数据库错误映射到 HTTP 语义。
func mapDomainError(err error) error {
	if errors.Is(err, common.ErrKeyNotFound) {
		return newHTTPError(fiber.StatusNotFound, "key not found")
	}
	return err
}
