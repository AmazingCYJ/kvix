# redis

## 模块目标

`redis` 在 `kvix` 的 KV 引擎之上提供一层 Redis 风格数据结构封装。它当前暴露的是 Go API，而不是 Redis 网络协议服务端。

## 设计原则

- 优先复用 `kvix.DB` 已有的 `Put`、`Get`、`Delete`、`WriteBatch` 和迭代器能力。
- 通过 metadata 明确保存类型、TTL、版本和集合边界，避免把结构语义散落到各个命令里。
- 采用“版本号 + 惰性失效”方案，避免复杂结构在删除或过期时做高成本全量清理。
- 尽量保持 Redis 风格语义，但不强求网络协议和内部实现细节完全一致。

## 支持的数据类型

当前已经实现并通过测试的能力如下：

- `string`：`Set`、`Get`、`Del`、`Expire`、`TTL`
- `hash`：`HSet`、`HGet`、`HDel`、`HExists`、`HLen`
- `list`：`LPush`、`RPush`、`LPop`、`RPop`、`LLen`、`LRange`
- `set`：`SAdd`、`SRem`、`SIsMember`、`SCard`、`SMembers`
- `zset`：`ZAdd`、`ZRem`、`ZScore`、`ZCard`、`ZRange`

## 元数据模型

每个逻辑 key 都对应一条 `meta:<key>` 记录，核心字段如下：

- `typ`：逻辑数据类型。
- `expireAt`：过期时间，`0` 表示永不过期。
- `version`：当前逻辑版本号，用于隔离历史子键。
- `size`：当前逻辑元素数量。
- `head` / `tail`：列表左右边界，仅 `list` 使用。

所有 Redis 风格命令都会先读取 metadata，再决定是否允许继续读写子键。

## 键编码策略

模块统一采用“元数据键 + 数据子键”的编码方式：

- `meta:<key>`：保存 metadata，`string` 的 payload 也直接跟 metadata 一起编码。
- `hash:<key>:<version>:<field>`：hash 字段子键。
- `list:<key>:<version>:<index>`：list 元素子键。
- `set:<key>:<version>:<member>`：set 成员子键。
- `zset:dict:<key>:<version>:<member>`：member 到 score 的字典索引。
- `zset:score:<key>:<version>:<score>:<member>`：按 score 排序的范围扫描索引。

真实落盘编码会补充前缀、长度和定宽字段，以避免分隔符冲突并保证扫描顺序稳定。

## 过期机制

当前实现采用惰性过期：

1. 所有命令入口都先读取 metadata。
2. 如果发现 `expireAt` 已到期，就删除 metadata，并保留历史子键等待后续 merge 清理。
3. key 被重新创建时会分配更高版本号，旧版本子键因此自然失效。

这个策略更贴合 Bitcask 风格追加写引擎，能降低集合删除和过期处理的复杂度。

## 使用示例

```go
opts := common.DefaultOptions
opts.DirPath = "/tmp/kvix-redis"

store, err := redis.NewRedisDataStore(opts)
if err != nil {
	panic(err)
}
defer store.Close()

if err := store.Set([]byte("name"), []byte("molly"), time.Minute); err != nil {
	panic(err)
}

ok, err := store.HSet([]byte("user:1"), []byte("city"), []byte("shanghai"))
if err != nil {
	panic(err)
}
_ = ok
```

## 测试方式

在仓库根目录执行：

```bash
GOCACHE=$(pwd)/.cache/go go test ./redis -count=1
```

测试覆盖了字符串、哈希、列表、集合、有序集合以及元数据/过期相关的关键行为。
