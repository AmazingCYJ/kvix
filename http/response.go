package main

import "github.com/gofiber/fiber/v2"

type apiResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func writeJSON(c *fiber.Ctx, status int, message string, data interface{}) error {
	return c.Status(status).JSON(apiResponse{
		Code:    status,
		Message: message,
		Data:    data,
	})
}
