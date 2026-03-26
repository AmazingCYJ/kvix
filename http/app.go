package main

import (
	bitcaskmy "bitcask-my"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func newApp(db *bitcaskmy.DB) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      "bitcask-my-http",
		ErrorHandler: errorHandler,
	})

	app.Use(recover.New())
	app.Use(logger.New())

	registerRoutes(app, newServer(db))
	return app
}
