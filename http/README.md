# http

`http` 模块是一个基于 Fiber 的示例服务，用来演示如何把 `kvix.DB` 以及建立在它之上的 Redis 风格数据结构能力，暴露成一组简单、稳定、可直接调试的 JSON API。

它的定位不是完整业务系统，而是“数据库能力到 HTTP API 的一层清晰适配器”。

## 目录

- [1. 模块目标](#1-模块目标)
- [2. 当前接口能力总览](#2-当前接口能力总览)
- [3. Redis HTTP 设计要点](#3-redis-http-设计要点)
- [4. 目录与文件职责](#4-目录与文件职责)
- [5. 运行方式](#5-运行方式)
- [6. 接口示例](#6-接口示例)
- [7. 错误处理设计](#7-错误处理设计)
- [8. 与核心引擎的关系](#8-与核心引擎的关系)
- [9. 测试方式](#9-测试方式)

## 1. 模块目标

这个模块主要解决三个问题：

1. 给新读者一个可直接运行的外部接口示例。
2. 演示如何把领域错误、DTO 和路由组织成清晰边界。
3. 演示如何在同一个 `kvix.DB` 上同时暴露基础 KV API 和 Redis 风格结构 API。

它当前不承担：

- 用户认证
- 多租户
- 复杂业务规则
- Redis TCP/RESP 协议兼容

## 2. 当前接口能力总览

### 2.1 基础 KV 路由

- `GET /healthz`
- `POST /api/v1/entries`
- `POST /api/v1/entries/batch`
- `GET /api/v1/entries/:key`
- `DELETE /api/v1/entries/:key`
- `GET /api/v1/keys`
- `GET /api/v1/stats`

### 2.2 Redis 命令风格路由

所有 Redis 风格接口统一挂在：

```text
/api/v1/redis
```

并且全部使用 `POST`。

#### string

- `POST /api/v1/redis/string/set`
- `POST /api/v1/redis/string/get`
- `POST /api/v1/redis/string/del`
- `POST /api/v1/redis/string/expire`
- `POST /api/v1/redis/string/ttl`

#### hash

- `POST /api/v1/redis/hash/hset`
- `POST /api/v1/redis/hash/hget`
- `POST /api/v1/redis/hash/hdel`
- `POST /api/v1/redis/hash/hexists`
- `POST /api/v1/redis/hash/hlen`

#### list

- `POST /api/v1/redis/list/lpush`
- `POST /api/v1/redis/list/rpush`
- `POST /api/v1/redis/list/lpop`
- `POST /api/v1/redis/list/rpop`
- `POST /api/v1/redis/list/llen`
- `POST /api/v1/redis/list/lrange`

#### set

- `POST /api/v1/redis/set/sadd`
- `POST /api/v1/redis/set/srem`
- `POST /api/v1/redis/set/sismember`
- `POST /api/v1/redis/set/scard`
- `POST /api/v1/redis/set/smembers`

#### zset

- `POST /api/v1/redis/zset/zadd`
- `POST /api/v1/redis/zset/zrem`
- `POST /api/v1/redis/zset/zscore`
- `POST /api/v1/redis/zset/zcard`
- `POST /api/v1/redis/zset/zrange`

### 2.3 统一响应结构

所有接口统一返回：

```json
{
  "code": 200,
  "message": "ok",
  "data": {}
}
```

这样做的目的很直接：

- 调用方不需要为不同命令写多套响应外壳解析逻辑。
- 错误和成功都能走同一套结构。
- 对初学者更友好，更容易从 handler 代码一路对到实际返回值。

## 3. Redis HTTP 设计要点

### 3.1 不是第二套数据库实例

HTTP 层不会为了 Redis 命令再打开第二个 `kvix.DB`。

实际启动链路是：

1. `main.go` 只打开一次 `kvix.DB`
2. 基于这个已打开的 `db` 构造共享 `redis.RedisDataStore`
3. `server` 同时持有：
   - `db *kvix.DB`
   - `redisStore *redis.RedisDataStore`

这样做可以避免：

- 同一目录重复加锁
- 同一服务里出现两套彼此不一致的数据库状态

### 3.2 为什么 Redis HTTP 全部使用 POST

虽然其中很多命令是“读”，但这里仍统一使用 `POST`，原因是：

- 请求体里经常需要结构化参数，例如 `field`、`values`、`members`、`start`、`stop`
- 不必把复杂参数塞进 query string
- 路由风格更统一
- 测试代码和文档示例更整齐

### 3.3 错误语义是显式映射的

HTTP 层不会直接把底层错误字符串原样抛给调用方，而是先做稳定映射：

- `common.ErrKeyNotFound` -> `404 key not found`
- `redis.ErrWrongType` -> `400 wrong type`
- 请求体缺字段 / JSON 类型错误 -> `400`
- 其他未识别内部错误 -> `500 internal server error`

这让调用方能明确区分：

- key 不存在
- 类型不匹配
- 参数不合法
- 服务端内部故障

## 4. 目录与文件职责

- [http/main.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/main.go)
  负责启动服务、打开数据库、构造共享 `RedisDataStore` 和优雅关闭。
- [http/app.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/app.go)
  负责 Fiber 应用初始化与中间件装配。
- [http/routes.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/routes.go)
  负责定义基础 KV 路由和 Redis 命令风格路由。
- [http/handler.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/handler.go)
  负责基础 KV handler。
- [http/redis_string_handler.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/redis_string_handler.go)
  负责 string 相关 HTTP handler。
- [http/redis_hash_handler.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/redis_hash_handler.go)
  负责 hash 相关 HTTP handler。
- [http/redis_list_handler.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/redis_list_handler.go)
  负责 list 相关 HTTP handler。
- [http/redis_set_handler.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/redis_set_handler.go)
  负责 set 相关 HTTP handler。
- [http/redis_zset_handler.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/redis_zset_handler.go)
  负责 zset 相关 HTTP handler。
- [http/dto.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/dto.go)
  定义基础 KV DTO。
- [http/redis_dto.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/redis_dto.go)
  定义 Redis 命令请求/响应 DTO 以及字符串与字节切片转换辅助函数。
- [http/response.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/response.go)
  定义统一响应包装结构。
- [http/errors.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/http/errors.go)
  负责领域错误到 HTTP 状态码的映射。

## 5. 运行方式

### 5.1 启动

在仓库根目录执行：

```bash
go run ./http
```

默认监听地址：

```text
127.0.0.1:8080
```

### 5.2 自定义监听地址

```bash
KVIX_HTTP_ADDR=127.0.0.1:9090 go run ./http
```

### 5.3 固定数据目录

如果你希望服务重启后保留数据，推荐显式指定数据目录：

```bash
KVIX_HTTP_DATA_DIR=/var/lib/kvix-http/data go run ./http
```

服务器长期运行时，推荐一起指定监听地址：

```bash
KVIX_HTTP_ADDR=0.0.0.0:8080 \
KVIX_HTTP_DATA_DIR=/var/lib/kvix-http/data \
go run ./http
```

如果没有设置 `KVIX_HTTP_DATA_DIR`，服务会创建临时目录，适合：

- 本地演示
- 临时调试
- 自动化测试

## 6. 接口示例

### 6.1 健康检查

```bash
curl http://127.0.0.1:8080/healthz
```

### 6.2 基础 KV 写入和读取

```bash
curl -X POST http://127.0.0.1:8080/api/v1/entries \
  -H 'Content-Type: application/json' \
  -d '{"key":"name","value":"alice"}'

curl http://127.0.0.1:8080/api/v1/entries/name
```

### 6.3 string

```bash
curl -X POST http://127.0.0.1:8080/api/v1/redis/string/set \
  -H 'Content-Type: application/json' \
  -d '{"key":"name","value":"alice","ttl_seconds":60}'

curl -X POST http://127.0.0.1:8080/api/v1/redis/string/get \
  -H 'Content-Type: application/json' \
  -d '{"key":"name"}'

curl -X POST http://127.0.0.1:8080/api/v1/redis/string/ttl \
  -H 'Content-Type: application/json' \
  -d '{"key":"name"}'
```

### 6.4 hash

```bash
curl -X POST http://127.0.0.1:8080/api/v1/redis/hash/hset \
  -H 'Content-Type: application/json' \
  -d '{"key":"profile","field":"name","value":"alice"}'

curl -X POST http://127.0.0.1:8080/api/v1/redis/hash/hget \
  -H 'Content-Type: application/json' \
  -d '{"key":"profile","field":"name"}'
```

### 6.5 list

```bash
curl -X POST http://127.0.0.1:8080/api/v1/redis/list/lpush \
  -H 'Content-Type: application/json' \
  -d '{"key":"numbers","values":["a","b"]}'

curl -X POST http://127.0.0.1:8080/api/v1/redis/list/lrange \
  -H 'Content-Type: application/json' \
  -d '{"key":"numbers","start":0,"stop":-1}'
```

### 6.6 set

```bash
curl -X POST http://127.0.0.1:8080/api/v1/redis/set/sadd \
  -H 'Content-Type: application/json' \
  -d '{"key":"users","members":["alice","bob"]}'

curl -X POST http://127.0.0.1:8080/api/v1/redis/set/smembers \
  -H 'Content-Type: application/json' \
  -d '{"key":"users"}'
```

### 6.7 zset

```bash
curl -X POST http://127.0.0.1:8080/api/v1/redis/zset/zadd \
  -H 'Content-Type: application/json' \
  -d '{"key":"ranking","member":"alice","score":10}'

curl -X POST http://127.0.0.1:8080/api/v1/redis/zset/zrange \
  -H 'Content-Type: application/json' \
  -d '{"key":"ranking","start":0,"stop":-1}'
```

## 7. 错误处理设计

错误处理采用两层思路：

### 7.1 应用层错误

`httpError` 用来表示已经完成状态码映射的错误，例如：

- `400 bad request`
- `404 key not found`

### 7.2 领域错误映射

底层数据库错误先转成 HTTP 语义，再统一输出响应。

例如：

- `common.ErrKeyNotFound` -> `404`
- `redis.ErrWrongType` -> `400`

这使得：

- handler 不需要在每个地方重复写状态码逻辑
- API 返回结构更稳定
- 新增命令时更容易保持一致的外部契约

## 8. 与核心引擎的关系

HTTP 层本身不维护额外状态，所有实际数据仍然保存在同一个 `kvix.DB` 中。

这个模块的价值在于：

- 演示如何使用根包 API
- 演示如何复用 `redis.RedisDataStore`
- 演示如何做 DTO 与错误映射
- 演示如何把嵌入式数据库包装成外部接口

## 9. 测试方式

```bash
GOCACHE=$(pwd)/.cache/go go test ./http -count=1
```

当前测试主要覆盖：

- 基础 KV CRUD
- 批量写入
- Redis string/hash/list/set/zset 命令接口
- DTO 输出结构
- wrong type / key not found / 缺字段 等错误路径
