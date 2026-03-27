# HTTP Redis Command API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `http` 示例服务补齐 `redis` 包当前已经实现的 `string/hash/list/set/zset` 命令式 HTTP API，并保持现有统一响应结构与错误语义。

**Architecture:** HTTP 层继续只打开一套 `kvix.DB`，再通过“基于已有 DB 构造 RedisDataStore”的方式把 Redis 语义层挂进 `server`。路由采用 `/api/v1/redis/<type>/<command>` 命令风格，DTO 按数据结构拆分，小步补齐 handler、路由、错误映射、测试和文档。

**Tech Stack:** Go 1.25, Fiber v2, 现有 `kvix.DB`, 现有 `redis.RedisDataStore`, Go `testing`

---

## File Map

### Create

- `http/redis_dto.go`
  - Redis HTTP 请求/响应 DTO，避免把所有命令塞进一个大 struct。
- `http/redis_string_handler.go`
  - string 相关 HTTP handler。
- `http/redis_hash_handler.go`
  - hash 相关 HTTP handler。
- `http/redis_list_handler.go`
  - list 相关 HTTP handler。
- `http/redis_set_handler.go`
  - set 相关 HTTP handler。
- `http/redis_zset_handler.go`
  - zset 相关 HTTP handler。
- `redis/store_test.go`
  - RedisDataStore 与共享 `*kvix.DB` 生命周期测试。

### Modify

- `redis/store.go`
  - 新增基于已有 `*kvix.DB` 构造 `RedisDataStore` 的入口，并处理底层 DB 所有权。
- `http/app.go`
  - `newApp` 改为接收 `redisStore`。
- `http/main.go`
  - `openConfiguredDB` 后构造共享 `redisStore`，再传给 `newApp`。
- `http/handler.go`
  - 扩展 `server` 结构，保留现有 KV handler。
- `http/routes.go`
  - 注册 Redis 命令风格路由。
- `http/errors.go`
  - 增加 `redis.ErrWrongType` 等错误映射。
- `http/app_test.go`
  - 增加 Redis HTTP 接口集成测试。
- `http/README.md`
  - 补 Redis HTTP 路由清单和示例。
- `README.md`
  - 补根级 Redis HTTP 能力总览。

### Keep Unchanged

- `redis/string.go`
- `redis/hash.go`
- `redis/list.go`
- `redis/set.go`
- `redis/zset.go`

说明：
这些底层 Redis 结构实现本轮只复用，不扩新命令，不改内部语义。

---

### Task 1: 共享 RedisDataStore 接入 HTTP 启动链路

**Files:**
- Create: `redis/store_test.go`
- Modify: `redis/store.go`
- Modify: `http/app.go`
- Modify: `http/main.go`
- Modify: `http/handler.go`

- [ ] **Step 1: 写一个失败测试，锁定“基于已有 DB 构造 RedisDataStore”的共享生命周期**

测试目标：
- 可以从已有 `*kvix.DB` 构造 `RedisDataStore`
- 共享模式下调用 `store.Close()` 不应关闭底层共享 DB

示例测试骨架：

```go
func TestNewRedisDataStoreFromDBDoesNotOwnSharedDB(t *testing.T) {
	opts := common.DefaultOptions
	opts.DirPath = t.TempDir()

	db, err := kvix.Open(opts)
	if err != nil {
		t.Fatal(err)
	}
	store := redis.NewRedisDataStoreFromDB(db)

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatalf("shared db should still be writable: %v", err)
	}
}
```

- [ ] **Step 2: 运行测试确认它先失败**

Run:

```bash
GOCACHE=$(pwd)/.cache/go go test ./redis -run TestNewRedisDataStoreFromDBDoesNotOwnSharedDB -count=1
```

Expected:
- FAIL
- `undefined: redis.NewRedisDataStoreFromDB` 或等价失败

- [ ] **Step 3: 在 `redis/store.go` 实现共享 DB 构造入口**

实现要点：
- `RedisDataStore` 增加底层 DB 所有权标记，例如 `ownsDB bool`
- 保留 `NewRedisDataStore(options)` 现有行为
- 新增 `NewRedisDataStoreFromDB(db *kvix.DB) *RedisDataStore`
- `Close()` 仅在 `ownsDB=true` 时关闭底层 DB

最小实现轮廓：

```go
type RedisDataStore struct {
	db     *kvix.DB
	ownsDB bool
}

func NewRedisDataStoreFromDB(db *kvix.DB) *RedisDataStore {
	return &RedisDataStore{db: db, ownsDB: false}
}
```

- [ ] **Step 4: 重新运行共享生命周期测试**

Run:

```bash
GOCACHE=$(pwd)/.cache/go go test ./redis -run TestNewRedisDataStoreFromDBDoesNotOwnSharedDB -count=1
```

Expected:
- PASS

- [ ] **Step 5: 接入 HTTP 启动链路**

实现要点：
- `server` 新增 `redisStore *redis.RedisDataStore`
- `newServer` 改为同时接收 `db` 和 `redisStore`
- `newApp` 改为接收 `db` 和 `redisStore`
- `main.go` 在 `openConfiguredDB()` 成功后调用 `redis.NewRedisDataStoreFromDB(db)`

代码方向：

```go
redisStore := redis.NewRedisDataStoreFromDB(db)
app := newApp(db, redisStore)
```

- [ ] **Step 6: 运行现有 HTTP 基础测试，确认共享接线未破坏原有 KV API**

Run:

```bash
GOCACHE=$(pwd)/.cache/go go test ./http -run 'TestNewRoutesPutGetDelete|TestHealthKeysAndStatsContracts' -count=1
```

Expected:
- PASS

- [ ] **Step 7: 提交**

```bash
git add redis/store.go redis/store_test.go http/app.go http/main.go http/handler.go
git commit -m "feat: wire shared redis store into http app"
```

---

### Task 2: 实现 string 命令风格 HTTP API

**Files:**
- Create: `http/redis_dto.go`
- Create: `http/redis_string_handler.go`
- Modify: `http/routes.go`
- Modify: `http/errors.go`
- Modify: `http/app_test.go`

- [ ] **Step 1: 在 `http/app_test.go` 写 string 接口失败测试**

至少覆盖：
- `set`
- `get`
- `del`
- `expire`
- `ttl`

示例测试骨架：

```go
func TestRedisStringSetGetDelTTL(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/redis/string/set", `{"key":"name","value":"alice"}`)
	assertStatus(t, resp, http.StatusOK)
}
```

- [ ] **Step 2: 运行 string HTTP 测试，确认它先失败**

Run:

```bash
GOCACHE=$(pwd)/.cache/go go test ./http -run TestRedisStringSetGetDelTTL -count=1
```

Expected:
- FAIL
- 404 或未注册路由

- [ ] **Step 3: 在 `http/redis_dto.go` 添加 string DTO**

至少包含：
- `redisStringSetRequest`
- `redisKeyOnlyRequest`
- `redisStringExpireRequest`

示例：

```go
type redisStringSetRequest struct {
	Key        string  `json:"key"`
	Value      *string `json:"value"`
	TTLSeconds *int64  `json:"ttl_seconds,omitempty"`
}
```

- [ ] **Step 4: 在 `http/redis_string_handler.go` 实现 string handler**

至少实现：
- `redisStringSet`
- `redisStringGet`
- `redisStringDel`
- `redisStringExpire`
- `redisStringTTL`

实现要求：
- 参数缺失返回 `400`
- `TTLSeconds` 转成 `time.Duration`
- 输出统一 `writeJSON`

- [ ] **Step 5: 在 `http/routes.go` 注册 string 路由**

加入：

```go
redis := v1.Group("/redis")
str := redis.Group("/string")
str.Post("/set", srv.redisStringSet)
str.Post("/get", srv.redisStringGet)
```

- [ ] **Step 6: 在 `http/errors.go` 增加 wrong type 映射**

要求：
- `redis.ErrWrongType` -> `400 wrong type`

- [ ] **Step 7: 重跑 string HTTP 测试**

Run:

```bash
GOCACHE=$(pwd)/.cache/go go test ./http -run TestRedisStringSetGetDelTTL -count=1
```

Expected:
- PASS

- [ ] **Step 8: 提交**

```bash
git add http/redis_dto.go http/redis_string_handler.go http/routes.go http/errors.go http/app_test.go
git commit -m "feat: add redis string http commands"
```

---

### Task 3: 实现 hash 与 list HTTP API

**Files:**
- Create: `http/redis_hash_handler.go`
- Create: `http/redis_list_handler.go`
- Modify: `http/redis_dto.go`
- Modify: `http/routes.go`
- Modify: `http/app_test.go`

- [ ] **Step 1: 写 hash/list 失败测试**

至少覆盖：
- hash：`hset/hget/hdel/hexists/hlen`
- list：`lpush/rpush/lpop/rpop/llen/lrange`

建议测试名：

```go
func TestRedisHashCommands(t *testing.T) {}
func TestRedisListCommands(t *testing.T) {}
func TestRedisWrongTypeReturns400(t *testing.T) {}
```

- [ ] **Step 2: 运行 hash/list 测试，确认先失败**

Run:

```bash
GOCACHE=$(pwd)/.cache/go go test ./http -run 'TestRedisHashCommands|TestRedisListCommands|TestRedisWrongTypeReturns400' -count=1
```

Expected:
- FAIL
- 路由未注册或 handler 未定义

- [ ] **Step 3: 在 `http/redis_dto.go` 补 hash/list 请求体**

至少包含：
- `key`
- `field`
- `value`
- `values`
- `start`
- `stop`

- [ ] **Step 4: 在 `http/redis_hash_handler.go` 实现 hash handler**

至少实现：
- `redisHashHSet`
- `redisHashHGet`
- `redisHashHDel`
- `redisHashHExists`
- `redisHashHLen`

- [ ] **Step 5: 在 `http/redis_list_handler.go` 实现 list handler**

至少实现：
- `redisListLPush`
- `redisListRPush`
- `redisListLPop`
- `redisListRPop`
- `redisListLLen`
- `redisListLRange`

- [ ] **Step 6: 在 `http/routes.go` 注册 hash/list 路由**

示例：

```go
hash := redis.Group("/hash")
hash.Post("/hset", srv.redisHashHSet)

list := redis.Group("/list")
list.Post("/lpush", srv.redisListLPush)
```

- [ ] **Step 7: 重新运行 hash/list 测试**

Run:

```bash
GOCACHE=$(pwd)/.cache/go go test ./http -run 'TestRedisHashCommands|TestRedisListCommands|TestRedisWrongTypeReturns400' -count=1
```

Expected:
- PASS

- [ ] **Step 8: 提交**

```bash
git add http/redis_dto.go http/redis_hash_handler.go http/redis_list_handler.go http/routes.go http/app_test.go
git commit -m "feat: add redis hash and list http commands"
```

---

### Task 4: 实现 set 与 zset HTTP API

**Files:**
- Create: `http/redis_set_handler.go`
- Create: `http/redis_zset_handler.go`
- Modify: `http/redis_dto.go`
- Modify: `http/routes.go`
- Modify: `http/app_test.go`

- [ ] **Step 1: 写 set/zset 失败测试**

至少覆盖：
- set：`sadd/srem/sismember/scard/smembers`
- zset：`zadd/zrem/zscore/zcard/zrange`

建议测试名：

```go
func TestRedisSetCommands(t *testing.T) {}
func TestRedisZSetCommands(t *testing.T) {}
```

- [ ] **Step 2: 运行 set/zset 测试，确认先失败**

Run:

```bash
GOCACHE=$(pwd)/.cache/go go test ./http -run 'TestRedisSetCommands|TestRedisZSetCommands' -count=1
```

Expected:
- FAIL

- [ ] **Step 3: 在 `http/redis_dto.go` 补 set/zset DTO**

至少包含：
- `member`
- `members`
- `score`
- `start`
- `stop`

- [ ] **Step 4: 在 `http/redis_set_handler.go` 实现 set handler**

至少实现：
- `redisSetSAdd`
- `redisSetSRem`
- `redisSetSIsMember`
- `redisSetSCard`
- `redisSetSMembers`

- [ ] **Step 5: 在 `http/redis_zset_handler.go` 实现 zset handler**

至少实现：
- `redisZSetZAdd`
- `redisZSetZRem`
- `redisZSetZScore`
- `redisZSetZCard`
- `redisZSetZRange`

- [ ] **Step 6: 在 `http/routes.go` 注册 set/zset 路由**

示例：

```go
set := redis.Group("/set")
set.Post("/sadd", srv.redisSetSAdd)

zset := redis.Group("/zset")
zset.Post("/zadd", srv.redisZSetZAdd)
```

- [ ] **Step 7: 重跑 set/zset 测试**

Run:

```bash
GOCACHE=$(pwd)/.cache/go go test ./http -run 'TestRedisSetCommands|TestRedisZSetCommands' -count=1
```

Expected:
- PASS

- [ ] **Step 8: 提交**

```bash
git add http/redis_dto.go http/redis_set_handler.go http/redis_zset_handler.go http/routes.go http/app_test.go
git commit -m "feat: add redis set and zset http commands"
```

---

### Task 5: 完整回归、文档补齐和接口示例

**Files:**
- Modify: `http/README.md`
- Modify: `README.md`
- Modify: `http/app_test.go`

- [ ] **Step 1: 为 HTTP 文档补 Redis 路由清单**

在 `http/README.md` 增加：
- Redis HTTP 功能小节
- 路由列表
- 每类结构至少一个请求示例

- [ ] **Step 2: 在根 README 增加 Redis HTTP 能力总览**

补充：
- HTTP 现在同时支持 KV 与 Redis 风格结构
- 指向 `http/README.md`

- [ ] **Step 3: 为 `http/app_test.go` 补全错误路径断言**

至少补：
- wrong type -> `400`
- key not found -> `404`
- body 缺字段 -> `400`

- [ ] **Step 4: 运行 HTTP 包全量测试**

Run:

```bash
GOCACHE=$(pwd)/.cache/go go test ./http -count=1
```

Expected:
- PASS

- [ ] **Step 5: 运行全仓全量测试**

Run:

```bash
GOCACHE=$(pwd)/.cache/go go test ./... -count=1
```

Expected:
- PASS

- [ ] **Step 6: 提交**

```bash
git add http/README.md README.md http/app_test.go
git commit -m "docs: document redis http command api"
```

---

### Task 6: 最终人工验收清单

**Files:**
- None

- [ ] **Step 1: 本地启动 HTTP 服务**

Run:

```bash
KVIX_HTTP_DATA_DIR=$(mktemp -d) GOCACHE=$(pwd)/.cache/go go run ./http
```

Expected:
- 服务成功启动
- 控制台显示监听地址

- [ ] **Step 2: 手动验证 string 命令**

Run:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/redis/string/set \
  -H 'Content-Type: application/json' \
  -d '{"key":"name","value":"alice"}'
```

Expected:
- `200`
- 返回 `{"message":"ok"}`

- [ ] **Step 3: 手动验证 hash/list/set/zset 各至少一条命令**

建议：
- `hset`
- `lpush`
- `sadd`
- `zadd`

- [ ] **Step 4: 停止本地服务并确认无残留进程**

Expected:
- 服务优雅退出
- 数据目录可清理

- [ ] **Step 5: 最终提交**

```bash
git status --short
```

Expected:
- 工作树干净

