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

// main 启动 kvix HTTP 示例服务，并在进程退出时优雅关闭。
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

// openTempDB 为 HTTP 示例创建一个临时目录数据库，并返回清理函数。
func openTempDB() (*kvix.DB, func(), error) {
	// 1. 基于默认配置创建临时目录，避免示例服务污染用户已有数据目录。
	option := common.DefaultOptions
	dir, err := os.MkdirTemp("", "kvix-http")
	if err != nil {
		return nil, nil, err
	}
	option.DirPath = dir

	// 2. 打开数据库；如果失败，立即回收临时目录。
	db, err := kvix.Open(option)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, err
	}

	// 3. 返回清理函数，供调用方在退出时统一删除临时目录。
	cleanup := func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("failed to remove temp dir %s: %v", dir, err)
		}
	}

	log.Printf("kvix http db initialized at %s", dir)
	return db, cleanup, nil
}

// errorHandler 统一把领域错误和 Fiber 错误转换为固定 JSON 响应。
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
