package main

import "github.com/gofiber/fiber/v2"

// apiResponse 定义 HTTP 示例统一输出的 JSON 包装结构。
type apiResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    any         `json:"data,omitempty"`
}

// writeJSON 统一输出固定结构的 JSON 响应。
func writeJSON(c *fiber.Ctx, status int, message string, data any) error {
	return c.Status(status).JSON(apiResponse{
		Code:    status,
		Message: message,
		Data:    data,
	})
}
