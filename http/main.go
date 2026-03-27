package main

import (
	"context"
	"errors"
	"fmt"
	kvix "kvix"
	common "kvix/common"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v2"
)

const defaultListenAddr = "127.0.0.1:8080"

func main() {
	db, cleanup, err := openTempDB()
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer cleanup()
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("failed to close db: %v", err)
		}
	}()

	app := newApp(db)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("shutting down http server")
		if err := app.Shutdown(); err != nil {
			log.Printf("failed to shutdown server: %v", err)
		}
	}()

	addr := os.Getenv("KVIX_HTTP_ADDR")
	if addr == "" {
		addr = defaultListenAddr
	}

	if err := app.Listen(addr); err != nil {
		log.Printf("http server stopped with error: %v", err)
	}
}

func openTempDB() (*kvix.DB, func(), error) {
	option := common.DefaultOptions
	dir, err := os.MkdirTemp("", "kvix-http")
	if err != nil {
		return nil, nil, err
	}
	option.DirPath = dir

	db, err := kvix.Open(option)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, err
	}

	cleanup := func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("failed to remove temp dir %s: %v", dir, err)
		}
	}

	log.Printf("kvix http db initialized at %s", dir)
	return db, cleanup, nil
}

func errorHandler(c *fiber.Ctx, err error) error {
	var httpErr *httpError
	if errors.As(err, &httpErr) {
		return writeJSON(c, httpErr.Status, httpErr.Message, nil)
	}
	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		return writeJSON(c, fiberErr.Code, fiberErr.Message, nil)
	}

	log.Printf("http server error: %v", err)
	return writeJSON(c, fiber.StatusInternalServerError, fmt.Sprintf("internal server error"), nil)
}
