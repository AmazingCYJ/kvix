package main

import "github.com/gofiber/fiber/v2"

func registerRoutes(app *fiber.App, srv *server) {
	app.Get("/healthz", srv.health)

	v1 := app.Group("/api/v1")
	v1.Post("/entries", srv.put)
	v1.Post("/entries/batch", srv.batchPut)
	v1.Get("/entries/:key", srv.getByParam)
	v1.Delete("/entries/:key", srv.deleteByParam)
	v1.Get("/keys", srv.listKeys)
	v1.Get("/stats", srv.stat)
}
