# http

`http` 模块是一个基于 Fiber 的示例服务，用来演示如何把 `kvix.DB` 暴露为一组简单、稳定的 JSON 接口。

它的定位不是完整业务系统，而是“数据库能力到 HTTP API 的一层清晰适配器”。

## 目录

- [1. 模块目标](#1-模块目标)
- [2. 当前接口能力](#2-当前接口能力)
- [3. 目录与文件职责](#3-目录与文件职责)
- [4. 请求处理流程](#4-请求处理流程)
- [5. 错误处理设计](#5-错误处理设计)
- [6. 运行方式](#6-运行方式)
- [7. 接口示例](#7-接口示例)
- [8. 与核心引擎的关系](#8-与核心引擎的关系)
- [9. 测试方式](#9-测试方式)

## 1. 模块目标

这个模块主要解决两个问题：

1. 给新读者一个可直接运行的外部接口示例
2. 演示如何把领域错误、DTO 和路由组织成清晰边界

它当前不承担：

- 用户认证
- 多租户
- 复杂业务规则
- Redis 协议兼容

## 2. 当前接口能力

当前主要接口包括：

- `GET /healthz`
- `POST /api/v1/entries`
- `POST /api/v1/entries/batch`
- `GET /api/v1/entries/:key`
- `DELETE /api/v1/entries/:key`
- `GET /api/v1/keys`
- `GET /api/v1/stats`

返回结构统一为：

```json
{
  "code": 200,
  "message": "ok",
  "data": {}
}
```

## 3. 目录与文件职责

- [http/main.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/main.go)
  负责启动服务、打开临时数据库和优雅关闭。
- [http/app.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/app.go)
  负责 Fiber 应用初始化与中间件装配。
- [http/routes.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/routes.go)
  负责路由表定义。
- [http/handler.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/handler.go)
  负责请求解析、调用数据库和组装响应。
- [http/dto.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/dto.go)
  定义请求与响应 DTO。
- [http/response.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/response.go)
  定义统一响应包装结构。
- [http/errors.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/errors.go)
  负责领域错误到 HTTP 状态码的映射。

## 4. 请求处理流程

一个典型请求的路径是：

1. 路由命中 handler
2. handler 解析请求体或路径参数
3. 调用 `kvix.DB`
4. 处理领域错误
5. 输出统一 JSON 响应

以 `POST /api/v1/entries` 为例：

1. 解析 JSON 请求体
2. 校验 `key` 和 `value`
3. 调用 `db.Put`
4. 返回固定结构的 DTO

## 5. 错误处理设计

错误处理采用两层思路：

### 5.1 应用层错误

`httpError` 用来表示已经完成状态码映射的错误，例如：

- `400 bad request`
- `404 key not found`

### 5.2 领域错误映射

底层数据库错误先转成 HTTP 语义，再统一输出响应。

例如：

- `common.ErrKeyNotFound` -> `404`

这使得：

- handler 不需要在每个地方重复写状态码逻辑
- API 返回结构更稳定

## 6. 运行方式

### 6.1 启动

在仓库根目录执行：

```bash
go run ./http
```

默认监听地址：

```text
127.0.0.1:8080
```

### 6.2 自定义监听地址

```bash
KVIX_HTTP_ADDR=127.0.0.1:9090 go run ./http
```

### 6.3 数据目录说明

示例服务默认会创建一个临时数据库目录，而不是直接用固定路径。这样做的好处是：

- 不污染本地已有数据
- 示例服务更容易直接启动
- 测试与演示环境更隔离

## 7. 接口示例

### 7.1 健康检查

```bash
curl http://127.0.0.1:8080/healthz
```

### 7.2 写入单条记录

```bash
curl -X POST http://127.0.0.1:8080/api/v1/entries \
  -H 'Content-Type: application/json' \
  -d '{"key":"name","value":"alice"}'
```

### 7.3 读取单条记录

```bash
curl http://127.0.0.1:8080/api/v1/entries/name
```

### 7.4 批量写入

```bash
curl -X POST http://127.0.0.1:8080/api/v1/entries/batch \
  -H 'Content-Type: application/json' \
  -d '{"entries":[{"key":"k1","value":"v1"},{"key":"k2","value":"v2"}]}'
```

### 7.5 删除

```bash
curl -X DELETE http://127.0.0.1:8080/api/v1/entries/name
```

### 7.6 查看 key 列表和统计

```bash
curl http://127.0.0.1:8080/api/v1/keys
curl http://127.0.0.1:8080/api/v1/stats
```

## 8. 与核心引擎的关系

HTTP 层本身不维护额外状态，所有实际数据仍然保存在 `kvix.DB` 中。

这个模块的价值在于：

- 演示如何使用根包 API
- 演示如何做 DTO 与错误映射
- 演示如何把嵌入式数据库包装成外部接口

## 9. 测试方式

```bash
GOCACHE=$(pwd)/.cache/go go test ./http -count=1
```

当前测试主要覆盖：

- 基本 CRUD
- 批量写入
- 路由优先级
- DTO 输出结构
- 错误映射与接口契约
