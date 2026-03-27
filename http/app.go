package main

import (
	kvix "kvix"
	redisstore "kvix/redis"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

// newApp 创建 HTTP 应用，并注册统一错误处理、中间件和业务路由。
func newApp(db *kvix.DB, redisStore *redisstore.RedisDataStore) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      "kvix-http",
		ErrorHandler: errorHandler,
	})

	app.Use(recover.New())
	app.Use(logger.New())

	registerRoutes(app, newServer(db, redisStore))
	return app
}
