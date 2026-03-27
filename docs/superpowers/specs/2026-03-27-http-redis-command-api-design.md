# HTTP Redis Command API 设计文档

## 背景

当前 `http` 模块只暴露了基础 KV 能力：

- `POST /api/v1/entries`
- `POST /api/v1/entries/batch`
- `GET /api/v1/entries/:key`
- `DELETE /api/v1/entries/:key`
- `GET /api/v1/keys`
- `GET /api/v1/stats`

与此同时，项目的 [redis/README.md](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/redis/README.md) 已经在 Go API 层支持：

- `string`
- `hash`
- `list`
- `set`
- `zset`

因此当前 HTTP 层和底层能力之间存在明显缺口：

- Redis 风格数据结构已经实现
- 但外部 HTTP 调用方还无法直接使用这些结构

本设计文档的目标就是把这层能力差补齐。

## 目标

在现有 `http` 示例服务中新增一组 Redis 命令风格 HTTP API，使外部调用方可以通过 HTTP 直接使用：

- string
- hash
- list
- set
- zset

同时满足以下约束：

- 保持当前项目“示例服务 + 可读性优先”的风格
- 不实现 RESP 协议
- 不引入第二套数据库实例打开流程
- 不扩展 `redis` 包里尚未存在的新命令
- 保持现有统一 JSON 响应结构

## 非目标

这一轮明确不做：

- Redis TCP 协议兼容
- 通用命令执行器
- `EVAL` / `MULTI` / `WATCH` 等高级语义
- 现有 `redis` 包未支持的新命令，例如 `HGetAll`、`Type`、`Exists`
- 权限控制、鉴权、限流

## 方案选择

### 已评估方案

#### 方案 1：命令分组路由

示例：

- `/api/v1/redis/string/set`
- `/api/v1/redis/hash/hset`
- `/api/v1/redis/list/lpush`

优点：

- 与 Redis 命令风格最一致
- 路由可读性最高
- 最贴合当前 `http` 模块作为示例服务的定位

缺点：

- 路由数量更多
- 一部分只读操作也会使用 `POST`

#### 方案 2：类型前缀 + 操作字段

示例：

- `POST /api/v1/redis/hash`
- body 中通过 `op=hset` 区分命令

优点：

- 路由较少

缺点：

- handler 分支会明显变复杂
- 日志和测试可读性差

#### 方案 3：统一执行入口

示例：

- `POST /api/v1/redis/execute`

优点：

- 最灵活

缺点：

- 已经在向“伪协议层”演化
- 不符合当前项目示例服务定位

### 结论

采用 **方案 1：命令分组路由**。

理由：

- 它最符合用户已经确认的“Redis 命令风格”
- 它与当前 `http` 模块的简洁示例风格一致
- 对 Go 初学者最友好，阅读成本最低

## 总体架构

### 现状

当前 HTTP 层的 `server` 只持有：

- `db *kvix.DB`

而 Redis 结构能力位于：

- `redis.RedisDataStore`

### 关键设计点

HTTP 层不能为了接 Redis 结构而再次调用 `kvix.Open()`。

原因是：

- 同一个目录会持有文件锁
- 重复打开会触发目录锁冲突

### 设计决定

新增一个“基于已有 `*kvix.DB` 构造 `RedisDataStore`”的入口，让 HTTP 层继续只打开一次数据库。

最终 `server` 将同时持有：

- `db *kvix.DB`
- `redisStore *redis.RedisDataStore`

这样：

- 现有 KV 路由继续走 `db`
- 新增 Redis 命令路由走 `redisStore`

## 路由设计

所有 Redis HTTP 路由统一挂在：

```text
/api/v1/redis
```

### string

- `POST /api/v1/redis/string/set`
- `POST /api/v1/redis/string/get`
- `POST /api/v1/redis/string/del`
- `POST /api/v1/redis/string/expire`
- `POST /api/v1/redis/string/ttl`

### hash

- `POST /api/v1/redis/hash/hset`
- `POST /api/v1/redis/hash/hget`
- `POST /api/v1/redis/hash/hdel`
- `POST /api/v1/redis/hash/hexists`
- `POST /api/v1/redis/hash/hlen`

### list

- `POST /api/v1/redis/list/lpush`
- `POST /api/v1/redis/list/rpush`
- `POST /api/v1/redis/list/lpop`
- `POST /api/v1/redis/list/rpop`
- `POST /api/v1/redis/list/llen`
- `POST /api/v1/redis/list/lrange`

### set

- `POST /api/v1/redis/set/sadd`
- `POST /api/v1/redis/set/srem`
- `POST /api/v1/redis/set/sismember`
- `POST /api/v1/redis/set/scard`
- `POST /api/v1/redis/set/smembers`

### zset

- `POST /api/v1/redis/zset/zadd`
- `POST /api/v1/redis/zset/zrem`
- `POST /api/v1/redis/zset/zscore`
- `POST /api/v1/redis/zset/zcard`
- `POST /api/v1/redis/zset/zrange`

## 为什么统一使用 POST

虽然其中一部分命令具有“读取”语义，但本设计仍统一使用 `POST`。

理由：

- 一些命令天然需要复杂请求体，例如 `values`、`members`、`range`
- 避免把结构化参数塞进 query string
- 路由风格更一致
- handler 和 DTO 设计更简单

## DTO 设计

不设计一个“万能 Redis 请求体”，而是按命令族拆分请求结构。

### string 请求体

- `key`
- `value`
- `ttl_seconds`

### hash 请求体

- `key`
- `field`
- `value`

### list 请求体

- `key`
- `values`
- `start`
- `stop`

### set 请求体

- `key`
- `member`
- `members`

### zset 请求体

- `key`
- `member`
- `score`
- `start`
- `stop`

## 响应设计

保持现有统一响应包装：

```json
{
  "code": 200,
  "message": "ok",
  "data": {}
}
```

### data 约定

#### string

- `set`：返回 `key`、`value`
- `get`：返回 `key`、`value`
- `del`：返回 `deleted`
- `expire`：返回 `updated`
- `ttl`：返回 `ttl_seconds`

#### hash

- `hset`：返回 `added`
- `hget`：返回 `key`、`field`、`value`
- `hdel`：返回 `deleted`
- `hexists`：返回 `exists`
- `hlen`：返回 `count`

#### list

- `lpush` / `rpush`：返回 `count`
- `lpop` / `rpop`：返回 `value`
- `llen`：返回 `count`
- `lrange`：返回 `values`

#### set

- `sadd` / `srem`：返回 `count`
- `sismember`：返回 `exists`
- `scard`：返回 `count`
- `smembers`：返回 `members`

#### zset

- `zadd` / `zrem`：返回 `updated`
- `zscore`：返回 `score`
- `zcard`：返回 `count`
- `zrange`：返回 `members`

## 错误映射

### 保持现有风格

HTTP 层继续做“统一错误语义映射”，而不是直接透传底层错误文本。

### 新增映射规则

- `common.ErrKeyNotFound` -> `404 key not found`
- `redis.ErrWrongType` -> `400 wrong type`
- 请求体缺失字段 / 字段类型不正确 -> `400`
- 未识别内部错误 -> `500 internal server error`

### 预期效果

外部调用方可以稳定区分：

- key 不存在
- 类型不匹配
- 参数不合法
- 服务内部故障

## 代码结构设计

### 修改文件

- `redis/store.go`
  - 新增基于已有 `*kvix.DB` 构造 `RedisDataStore` 的入口

- `http/routes.go`
  - 新增 Redis 命令路由

- `http/handler.go`
  - 为 string/hash/list/set/zset 增加 handler

- `http/dto.go`
  - 新增 Redis 命令请求/响应 DTO

- `http/errors.go`
  - 增加 `redis.ErrWrongType` 的 HTTP 映射

- `http/app_test.go`
  - 新增 Redis HTTP 集成测试

- `http/README.md`
  - 增加 Redis HTTP 接口文档和请求示例

- `README.md`
  - 在总览中补充 Redis HTTP 对外能力说明

## 测试设计

### 集成测试入口

复用现有 HTTP 测试方式：

- 启动测试用 app
- 通过 HTTP 请求验证接口行为

### 最低覆盖范围

#### string

- `set/get/del`
- `expire/ttl`

#### hash

- `hset/hget/hdel`
- `hexists/hlen`

#### list

- `lpush/rpush`
- `lpop/rpop`
- `llen/lrange`

#### set

- `sadd/srem`
- `sismember/scard/smembers`

#### zset

- `zadd/zrem`
- `zscore/zcard/zrange`

#### 错误路径

- key 不存在
- wrong type
- body 缺字段
- body 字段类型错误

## 风险与注意事项

### 1. 不要重复打开数据库

这是本次实现最重要的边界。

如果 HTTP 层为了 Redis 路由再次 `kvix.Open()`：

- 会造成文件锁冲突
- 也会让同一个 HTTP 服务内部出现两套不同数据库对象

### 2. DTO 不要过度抽象

当前项目强调可读性。

因此：

- 宁可多几个小 DTO
- 也不要做一个巨大的 `RedisCommandRequest`

### 3. 错误语义必须稳定

HTTP 层对外是接口契约。

如果错误信息在不同命令之间随意变化，会让调用方难以使用。

## 结果预期

完成后，HTTP 示例服务将从“只支持基础 KV”扩展为“同时支持基础 KV + Redis 风格结构命令”。

对外使用者将可以通过 HTTP 直接操作：

- string
- hash
- list
- set
- zset

而底层仍然只维持一套 `kvix.DB` 实例和一套持久化数据目录。
