# http

## 模块职责

`http` 提供一个基于 Fiber 的 kvix 示例服务，用来演示如何把底层数据库能力暴露成 HTTP API。

## 目录说明

- `main.go`：程序入口、数据库初始化和优雅关闭。
- `app.go`：Fiber 应用初始化与中间件装配。
- `routes.go`：路由注册。
- `handler.go`：请求到数据库调用的转换逻辑。
- `dto.go`：请求与响应 DTO。
- `response.go`：统一 JSON 响应包装。
- `errors.go`：领域错误到 HTTP 状态码的映射。

## 启动方式或运行方式

在仓库根目录执行：

```bash
go run ./http
```

可通过环境变量 `KVIX_HTTP_ADDR` 指定监听地址，默认是 `127.0.0.1:8080`。

## 示例输入输出

- `POST /api/v1/entries`
- 请求体：`{"key":"name","value":"alice"}`
- 成功响应：`{"code":200,"message":"ok","data":{"key":"name","value":"alice"}}`

## 与核心引擎的关系

HTTP 层本身不维护额外存储状态，所有请求最终都委托给 `kvix.DB`。它的价值在于展示一层清晰的适配器边界，而不是实现完整业务系统。
