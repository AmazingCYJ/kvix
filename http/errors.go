package main

import (
	"errors"
	common "kvix/common"

	"github.com/gofiber/fiber/v2"
)

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

func mapDomainError(err error) error {
	if errors.Is(err, common.ErrKeyNotFound) {
		return newHTTPError(fiber.StatusNotFound, "key not found")
	}
	return err
}
