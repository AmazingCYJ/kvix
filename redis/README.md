# redis

`redis` 模块在 `kvix` 的 KV 引擎之上实现了一层 Redis 风格数据结构封装。它不是 Redis 协议服务端，而是一组可以在 Go 代码中直接调用的结构化 API。

这个模块真正有价值的地方，不只是“方法名像 Redis”，而是它用 metadata、版本号、惰性过期和子键编码，把复杂结构稳定地落到了一个追加写 KV 引擎上。

## 目录

- [1. 模块目标](#1-模块目标)
- [2. 当前支持范围](#2-当前支持范围)
- [3. 整体设计思路](#3-整体设计思路)
- [4. 元数据模型](#4-元数据模型)
- [5. 键编码策略](#5-键编码策略)
- [6. 过期与版本机制](#6-过期与版本机制)
- [7. 各数据结构设计](#7-各数据结构设计)
- [8. 使用方法](#8-使用方法)
- [9. 行为边界与语义说明](#9-行为边界与语义说明)
- [10. 与底层引擎的关系](#10-与底层引擎的关系)
- [11. 测试与扩展](#11-测试与扩展)

## 1. 模块目标

`redis` 模块的目标是：

- 在 `kvix.DB` 之上表达更丰富的数据结构语义
- 尽量保持接近 Redis 的使用方式和返回语义
- 不引入独立存储引擎
- 不重写底层读写模型

它当前不追求：

- Redis TCP 协议兼容
- RESP 编解码
- 完整命令覆盖
- 与官方 Redis 完全一致的内部实现细节

可以把它理解成：

- “建立在 `kvix` 上的 Redis 风格数据结构库”

而不是：

- “一个完整的 Redis 替代服务端”

## 2. 当前支持范围

### 2.1 string

- `Set`
- `Get`
- `Del`
- `Expire`
- `TTL`

### 2.2 hash

- `HSet`
- `HGet`
- `HDel`
- `HExists`
- `HLen`

### 2.3 list

- `LPush`
- `RPush`
- `LPop`
- `RPop`
- `LLen`
- `LRange`

### 2.4 set

- `SAdd`
- `SRem`
- `SIsMember`
- `SCard`
- `SMembers`

### 2.5 zset

- `ZAdd`
- `ZRem`
- `ZScore`
- `ZCard`
- `ZRange`

## 3. 整体设计思路

这个模块的核心设计不是“为每个类型单独做一个存储系统”，而是统一采用下面四个机制：

1. metadata
2. 版本号
3. 子键编码
4. 惰性过期

这四个机制组合起来后，复杂结构就能稳定地映射到底层 KV 存储。

### 3.1 为什么需要 metadata

Redis 风格语义有一个根本要求：

- 同一个逻辑 key 在任一时刻只能是一个类型

因此必须有一条统一的元数据记录来保存：

- 类型
- TTL
- 版本
- 元素数量
- 结构边界

### 3.2 为什么需要版本号

复杂结构删除或被别的类型覆盖时，如果立刻全量扫描删除所有历史子键，成本很高，也不符合追加写引擎的风格。

版本号的意义是：

- 不急着删旧子键
- 只切换 metadata 指向的新版本
- 后续读取自然只认当前版本

### 3.3 为什么需要子键编码

hash、list、set、zset 都不是单值结构，必须拆成多个底层子键。

例如：

- 一个 hash 会拆成多个 field 子键
- 一个 list 会拆成多个 index 子键
- 一个 zset 甚至会拆成两套索引键

### 3.4 为什么采用惰性过期

惰性过期更适合当前底层：

- 访问时再判断是否过期
- 过期后删除 metadata
- 历史子键等待后续 merge 清理

这样能减少后台任务复杂度，也能避免为了 TTL 再引入一套扫描器。

## 4. 元数据模型

每个逻辑 key 都对应一条 metadata 记录。

实现文件：

- [redis/meta.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/redis/meta.go)

### 4.1 字段

当前 metadata 字段包括：

- `typ`
- `expireAt`
- `version`
- `size`
- `head`
- `tail`

### 4.2 字段含义

#### `typ`

逻辑类型：

- `1 = string`
- `2 = hash`
- `3 = list`
- `4 = set`
- `5 = zset`

#### `expireAt`

过期时间，单位是 `UnixNano`。

- `0` 表示永不过期
- 大于 `0` 表示到期后视为过期

#### `version`

当前逻辑版本号。它决定哪些子键仍然有效。

#### `size`

逻辑元素数量：

- `string` 固定为 1
- `hash` 是 field 数
- `list` 是元素数
- `set` 是 member 数
- `zset` 是 member 数

#### `head` / `tail`

只对 `list` 有意义，用于描述双端队列边界。

### 4.3 二进制编码

metadata 不是 JSON，而是固定长度的二进制结构。

当前固定长度为：

```text
37 bytes
```

原因很直接：

- 紧凑
- 读写成本低
- 字段布局稳定
- 解码过程明确

### 4.4 string 的特殊点

`string` 不是额外拆子键，而是直接把 payload 拼接在 metadata 后面。

也就是说：

- metadata 仍然存在
- string value 直接跟在 metadata 之后

这样可以让 `string` 的读写路径更短。

## 5. 键编码策略

实现文件：

- [redis/keys.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/redis/keys.go)

文档里常用 `meta:<key>` 这种写法描述逻辑结构，但真实落盘键更严谨：

- 会带前缀
- 会带 `0x00` 分隔
- 会带长度前缀
- 会带定宽版本号、索引或 score 编码

这样做是为了：

- 避免分隔符碰撞
- 保证排序稳定
- 支持范围扫描

### 5.1 metadata 键

- `meta:<key>`
- `meta:ver:<key>`

其中：

- `meta:<key>` 保存实际 metadata
- `meta:ver:<key>` 追踪最新版本号

### 5.2 hash 键

```text
hash:<key>:<version>:<field>
```

每个 field 都是单独子键。

### 5.3 list 键

```text
list:<key>:<version>:<index>
```

这里的 `index` 不是普通 int64 原样编码，而是做了可排序转换，保证按字节序遍历时能得到正确的逻辑顺序。

### 5.4 set 键

```text
set:<key>:<version>:<member>
```

value 使用固定占位字节，不重复存 member 内容。

### 5.5 zset 键

zset 需要两套子键：

#### dict 索引

```text
zset:dict:<key>:<version>:<member>
```

用于：

- `member -> score`

#### score 索引

```text
zset:score:<key>:<version>:<score>:<member>
```

用于：

- 按 score 有序扫描
- score 相同时按 member 字典序稳定排序

### 5.6 为什么 zset 需要双轨结构

因为 zset 要同时支持两种读取方向：

- 已知 member，查 score
- 已知顺序，按 score 遍历 member

单一键布局无法同时把这两种读路径都做得足够直接，因此需要双索引。

## 6. 过期与版本机制

实现文件：

- [redis/expire.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/redis/expire.go)

### 6.1 统一入口

所有命令真正读写前，都会先走 metadata 流程：

1. 读取 metadata
2. 判断 key 是否存在
3. 判断是否过期
4. 判断类型是否匹配
5. 再决定访问哪个版本的子键

### 6.2 惰性过期流程

如果发现 key 已经过期：

1. 记录当前版本到 `meta:ver:<key>`
2. 删除 `meta:<key>`
3. 返回“逻辑上不存在”

这样旧子键即使仍留在底层，也不会再被当成当前数据读取。

### 6.3 版本递增策略

以下情况会推动版本变化：

- 新建一个新类型 key
- string 覆盖写
- 某些类型从别的类型切换过来
- 过期后重新创建

版本机制的核心价值是：

- 避免立刻清理大量历史子键
- 让复杂结构删除和覆盖写更便宜

### 6.4 TTL 语义

当前 `TTL` 行为：

- key 不存在：返回 `common.ErrKeyNotFound`
- key 存在但没有过期时间：返回 `-1`
- key 存在且设置了过期时间：返回剩余 `time.Duration`

## 7. 各数据结构设计

这一部分是模块的核心。

### 7.1 string

实现文件：

- [redis/string.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/redis/string.go)

设计特点：

- payload 与 metadata 一体编码
- `Set` 会处理新建、同类型覆盖、跨类型覆盖
- `Expire` 只改 metadata，不改 payload

适合原因：

- string 本身就是单值结构
- 不需要拆子键
- 读写路径最短

### 7.2 hash

实现文件：

- [redis/hash.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/redis/hash.go)

设计方式：

- metadata 保存类型、版本和 field 数量
- 每个 field 用单独子键保存

关键点：

- `HSet` 新增 field 时要同步增加 `size`
- `HSet` 覆盖旧 field 时不增加 `size`
- `HDel` 删除最后一个 field 时会删除 metadata
- 新字段写入和 metadata 更新通过 `WriteBatch` 原子提交

### 7.3 list

实现文件：

- [redis/list.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/redis/list.go)

设计方式：

- metadata 保存 `head`、`tail`、`size`
- 元素按逻辑索引保存为子键

关键点：

- `LPush` 通过向左扩展 `head` 实现
- `RPush` 通过向右扩展 `tail` 实现
- `LPop` / `RPop` 删除边界元素并更新 metadata
- `LRange` 支持负数索引和越界裁剪

为什么这样设计：

- 避免为了 list 做连续物理搬移
- 双端写入和弹出逻辑清晰
- 与追加写 KV 引擎兼容

### 7.4 set

实现文件：

- [redis/set.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/redis/set.go)

设计方式：

- 每个 member 是一个独立子键
- value 只是占位符
- metadata 保存 member 数量

关键点：

- `SAdd` 会先去重，再过滤当前不存在的 member
- `SRem` 删除最后一个 member 时会连 metadata 一起删除
- `SMembers` 借助前缀迭代器扫描当前版本下所有 member

### 7.5 zset

实现文件：

- [redis/zset.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/redis/zset.go)

设计方式：

- dict 索引负责 `member -> score`
- score 索引负责有序遍历

关键点：

- `ZAdd` 新 member 时同时写入两套索引
- `ZAdd` 更新已有 member 的 score 时，要删除旧 score 索引并写入新 score 索引
- `ZRange` 依赖 score 索引顺序输出
- `NaN` score 会被拒绝

这也是当前实现里最复杂的一类结构。

## 8. 使用方法

### 8.1 初始化

```go
opts := common.DefaultOptions
opts.DirPath = "/tmp/kvix-redis"

store, err := redis.NewRedisDataStore(opts)
if err != nil {
	panic(err)
}
defer store.Close()
```

### 8.2 string 示例

```go
if err := store.Set([]byte("name"), []byte("molly"), time.Minute); err != nil {
	panic(err)
}

value, err := store.Get([]byte("name"))
if err != nil {
	panic(err)
}

fmt.Println(string(value))

ttl, err := store.TTL([]byte("name"))
if err != nil {
	panic(err)
}
fmt.Println(ttl)
```

### 8.3 hash 示例

```go
created, err := store.HSet([]byte("user:1"), []byte("city"), []byte("shanghai"))
if err != nil {
	panic(err)
}
fmt.Println(created)

value, err := store.HGet([]byte("user:1"), []byte("city"))
if err != nil {
	panic(err)
}
fmt.Println(string(value))
```

### 8.4 list 示例

```go
if _, err := store.LPush([]byte("jobs"), []byte("a"), []byte("b")); err != nil {
	panic(err)
}

items, err := store.LRange([]byte("jobs"), 0, -1)
if err != nil {
	panic(err)
}
fmt.Println(items)
```

### 8.5 set 示例

```go
added, err := store.SAdd([]byte("tags"), []byte("go"), []byte("kv"))
if err != nil {
	panic(err)
}
fmt.Println(added)

members, err := store.SMembers([]byte("tags"))
if err != nil {
	panic(err)
}
fmt.Println(members)
```

### 8.6 zset 示例

```go
if _, err := store.ZAdd([]byte("rank"), 12.5, []byte("alice")); err != nil {
	panic(err)
}
if _, err := store.ZAdd([]byte("rank"), 8.3, []byte("bob")); err != nil {
	panic(err)
}

members, err := store.ZRange([]byte("rank"), 0, -1)
if err != nil {
	panic(err)
}
fmt.Println(members)
```

## 9. 行为边界与语义说明

### 9.1 它不是 Redis 服务端

你不能直接用 `redis-cli` 连接这个模块。

它当前只是一组 Go 方法。

### 9.2 类型错误会返回 `ErrWrongType`

如果一个 key 当前是 list，你去执行 string 或 hash 风格方法，会返回：

- `ErrWrongType`

### 9.3 缺失 key 的语义不是所有方法都一样

例如：

- `Get` 缺失时返回 `common.ErrKeyNotFound`
- `HLen` 缺失时返回 `0`
- `SCard` 缺失时返回 `0`
- `ZCard` 缺失时返回 `0`
- `LRange` 缺失时返回空切片

这些行为已经由当前测试固定下来，写文档时需要尊重实现现状。

### 9.4 过期清理不是后台主动扫描

当前没有定时清理器。过期完全依赖惰性访问路径。

### 9.5 未实现命令

当前模块还没有实现很多 Redis 命令，例如：

- `INCR`
- `HGETALL`
- `LINDEX`
- `SINTER`
- `ZRANK`

`ErrNotImplemented` 已经预留，但当前主要用于未来扩展。

## 10. 与底层引擎的关系

`redis` 模块并不是“旁路系统”，它直接复用 `kvix.DB` 的核心能力：

- `Put`
- `Get`
- `Delete`
- `WriteBatch`
- `Iterator`

这意味着：

- Redis 结构的持久化格式最终仍然落在 kvix 数据文件上
- Redis 层继承了底层刷盘与索引行为
- 过期后历史子键也会影响底层 `reclaimSize` 和 merge 价值

## 11. 测试与扩展

### 11.1 测试命令

```bash
GOCACHE=$(pwd)/.cache/go go test ./redis -count=1
```

当前测试覆盖了：

- metadata 编解码
- string 语义
- hash 语义
- list 语义
- set 语义
- zset 语义
- 惰性过期
- 类型错误
- 跨结构覆盖与版本行为

### 11.2 扩展建议

如果你要继续扩展这个模块，建议优先遵守这些边界：

1. 新命令先复用 metadata 流程，不要绕过类型检查和过期检查。
2. 涉及多个键或 metadata 同步更新时优先考虑 `WriteBatch`。
3. 不要破坏现有 key 编码排序规则，尤其是 list 和 zset。
4. 增加新类型时，先考虑 metadata 如何扩展，再考虑命令 API。
