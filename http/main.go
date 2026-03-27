package main

import (
	"context"
	"errors"
	"fmt"
	kvix "kvix"
	common "kvix/common"
	redisstore "kvix/redis"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gofiber/fiber/v2"
)

const (
	defaultListenAddr = "127.0.0.1:8080"
	httpAddrEnvKey    = "KVIX_HTTP_ADDR"
	httpDataDirEnvKey = "KVIX_HTTP_DATA_DIR"
)

// main 启动 kvix HTTP 示例服务，并在进程退出时优雅关闭。
func main() {
	db, cleanup, err := openConfiguredDB()
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer cleanup()
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("failed to close db: %v", err)
		}
	}()

	redisStore := redisstore.NewRedisDataStoreFromDB(db)
	app := newApp(db, redisStore)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("shutting down http server")
		if err := app.Shutdown(); err != nil {
			log.Printf("failed to shutdown server: %v", err)
		}
	}()

	addr := os.Getenv(httpAddrEnvKey)
	if addr == "" {
		addr = defaultListenAddr
	}

	if err := app.Listen(addr); err != nil {
		log.Printf("http server stopped with error: %v", err)
	}
}

// openConfiguredDB 按环境变量决定使用固定数据目录还是临时目录，并返回对应清理函数。
func openConfiguredDB() (*kvix.DB, func(), error) {
	// 1. 先解析本次启动应该使用哪个数据目录。
	dir, cleanup, err := resolveDataDir()
	if err != nil {
		return nil, nil, err
	}

	// 2. 基于默认数据库配置打开 kvix 实例，只把数据目录替换成解析结果。
	option := common.DefaultOptions
	option.DirPath = dir
	db, err := kvix.Open(option)
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	// 3. 启动日志里显式输出最终数据目录，方便部署排障和确认持久化位置。
	log.Printf("kvix http db initialized at %s", dir)
	return db, cleanup, nil
}

// resolveDataDir 返回 HTTP 服务本次运行应使用的数据目录和对应清理函数。
func resolveDataDir() (string, func(), error) {
	// 1. 显式配置了 KVIX_HTTP_DATA_DIR 时，优先使用固定目录，适合服务器部署。
	if dir := strings.TrimSpace(os.Getenv(httpDataDirEnvKey)); dir != "" {
		return dir, func() {}, nil
	}

	// 2. 未配置时沿用原来的示例行为：创建临时目录，退出后再清理。
	dir, err := os.MkdirTemp("", "kvix-http")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("failed to remove temp dir %s: %v", dir, err)
		}
	}
	return dir, cleanup, nil
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
