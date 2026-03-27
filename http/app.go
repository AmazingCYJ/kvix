package main

import (
	kvix "kvix"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func newApp(db *kvix.DB) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      "kvix-http",
		ErrorHandler: errorHandler,
	})

	app.Use(recover.New())
	app.Use(logger.New())

	registerRoutes(app, newServer(db))
	return app
}
