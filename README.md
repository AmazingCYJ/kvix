# kvix

`kvix` 是一个 Bitcask 风格的 Go 键值存储项目。它以“追加写数据文件 + 索引定位 + 启动恢复 + Redis 风格结构封装”为核心思路，在保持实现相对直接的前提下，提供了基础 KV 能力、批量写入、迭代器、索引可插拔、内部 merge 流程，以及基于底层引擎构建的 Redis 风格数据结构与 HTTP 示例。

如果你想快速判断这个项目适不适合你，可以先看这几个关键词：

- 追加写存储
- 可切换索引实现
- 面向嵌入式场景的 Go API
- Redis 风格数据结构封装
- 注重可读性和模块边界的实现

## 目录

- [1. 项目定位](#1-项目定位)
- [2. 当前实现能力](#2-当前实现能力)
- [3. 总体架构](#3-总体架构)
- [4. 数据组织方式](#4-数据组织方式)
- [5. 索引设计与优化](#5-索引设计与优化)
- [6. 读写路径与性能优化](#6-读写路径与性能优化)
- [7. Redis 设计支持](#7-redis-设计支持)
- [8. 快速开始](#8-快速开始)
- [9. 详细使用方法](#9-详细使用方法)
- [10. HTTP 示例服务](#10-http-示例服务)
- [11. 测试与基准](#11-测试与基准)
- [12. 目录结构说明](#12-目录结构说明)
- [13. 当前边界与注意事项](#13-当前边界与注意事项)
- [14. 部署说明](#14-部署说明)

## 1. 项目定位

`kvix` 更适合以下场景：

- 你需要一个本地嵌入式 KV 引擎，而不是独立部署的数据库服务。
- 你希望自己掌控数据目录、刷盘策略和索引实现。
- 你需要在 Go 代码里直接使用接近 Redis 语义的数据结构，而不是接入 Redis 协议服务端。
- 你希望看到一个相对清晰、可读、可二次改造的 Bitcask 风格实现。

它当前不以这些目标为主：

- 不追求完整 Redis 协议兼容。
- 不追求分布式能力、主从复制、集群调度。
- 不追求 MVCC、SQL、复杂查询规划器等数据库能力。
- 不把自己包装成一个已经产品化完成的“通用数据库服务”。

## 2. 当前实现能力

### 核心 KV 能力

- `Open` / `Close`
- `Put` / `Get` / `Delete`
- `ListKeys`
- `Fold`
- `Sync`
- `Stat`
- `BackUp`

### 批量写入

- `WriteBatch`
- 批次大小限制
- 事务序列号与完成标记
- 批量原子提交

### 迭代与遍历

- 正向迭代
- 反向迭代
- `Seek`
- 前缀过滤

### 索引实现

- `BTree`
- `ART`
- `B+Tree`

### Redis 风格数据结构

- `string`
- `hash`
- `list`
- `set`
- `zset`

### 附加能力

- 启动恢复
- Hint 文件辅助索引恢复
- 数据文件滚动
- 目录级文件锁
- 内部 merge 流程
- Fiber HTTP 示例
- benchmark 与单元测试

## 3. 总体架构

从结构上看，`kvix` 可以理解为三层：

```text
调用方 / Redis 封装 / HTTP 示例
            |
            v
          kvix.DB
   (读写协调、文件状态、统计、恢复)
            |
    +-------+--------+
    |                |
    v                v
 Indexer         Data Files
 (key -> pos)    (追加写日志记录)
```

其中：

- `DB` 是系统核心协调者，负责把“逻辑操作”拆成“日志追加 + 索引更新 + 状态维护”。
- `Indexer` 只负责保存 `key -> LogRecordPos` 的映射，不保存 value 本体。
- `data` 模块负责日志记录编码、数据文件读写、位置信息表达。
- `redis` 模块不是单独存储引擎，而是建立在 `kvix.DB` 之上的结构语义层。
- `http` 模块只是演示如何把 `kvix.DB` 暴露为接口，不引入额外状态。

## 4. 数据组织方式

### 4.1 数据文件模型

项目使用典型的 Bitcask 风格数据布局：

- 所有写入都先编码成日志记录
- 日志记录顺序追加到活跃数据文件
- 写满后滚动到新的数据文件
- 旧文件转为只读历史文件

这种方式的直接收益是：

- 写入路径简单
- 顺序 IO 友好
- 崩溃恢复时容易回放
- 更新和删除都可以表达为“追加新记录”

### 4.2 索引只保存位置，不保存 value

内存或持久化索引里保存的是 `LogRecordPos`：

- `Fid`
- `Offset`
- `Size`

也就是说，读路径先查索引拿到位置，再去对应数据文件读取 value。这样可以让索引足够轻，同时避免 value 被重复存储在索引层。

### 4.3 更新与删除的表达方式

- `Put` 不会原地改写旧记录，而是追加一条新的普通记录。
- `Delete` 不会立即清理旧 value，而是追加一条删除标记记录。
- 被覆盖或删除的旧记录会被计入 `reclaimSize`，用于后续 merge 判断。

## 5. 索引设计与优化

`kvix` 的一个核心设计点是：根包只依赖统一 `Indexer` 接口，不绑定单一索引实现。这样可以在不改写主读写流程的前提下切换索引策略。

### 5.1 统一索引接口

索引层对上提供的核心能力包括：

- `Put`
- `Get`
- `Delete`
- `Size`
- `Iterator`
- `Close`

根包不关心你用的是 BTree、ART 还是 B+Tree，只关心这些能力是否成立。

### 5.2 BTree 索引

`BTree` 基于 `github.com/google/btree` 实现，特点是：

- 完全内存索引
- 按 key 有序
- 读写都比较直观
- 迭代和 `Seek` 很自然

适合你想要：

- 简单稳定的有序索引
- 可预测的读写行为
- 容易理解的实现

### 5.3 ART 索引

`ART` 基于 `go-adaptive-radix-tree` 实现，特点是：

- 完全内存索引
- 对前缀结构友好
- 在某些 key 分布下具有更好的空间利用
- 依然保持有序遍历能力

适合你想要：

- 更偏前缀树风格的索引结构
- 对前缀型 key 组织更敏感的场景

### 5.4 B+Tree 索引

`B+Tree` 基于 `bbolt` 实现，特点是：

- 索引本身持久化到单独文件
- 启动时不需要像纯内存索引那样完整重放历史数据文件来构建索引
- 更适合重启频繁、希望减少启动恢复成本的场景

这也是当前默认索引类型。

对应的权衡是：

- 写入时除了数据文件追加，还要维护持久化索引
- 索引层会引入 bbolt 文件和事务开销

### 5.5 启动恢复优化

对索引恢复，项目做了两层优化：

- 对 `BTree` / `ART` 这类内存索引，优先尝试加载 Hint 文件，减少全量数据文件扫描成本。
- 对 `B+Tree`，索引本身就已经持久化，启动时不再走完整的索引重建路径。

### 5.6 迭代器优化

三种索引实现都提供了快照式迭代器思路：

- 创建迭代器时先取当前视图
- 后续遍历在迭代器自己的快照或切片上进行
- 降低遍历过程中受并发修改影响的复杂度

这让 `Rewind`、`Seek`、正反向遍历都更容易稳定实现。

## 6. 读写路径与性能优化

这部分是 README 最需要说明清楚的地方，因为项目的大部分性能特征都来自这里。

### 6.1 写路径

单条 `Put` 的核心流程是：

1. 校验 key。
2. 将逻辑记录编码成日志记录。
3. 追加写入当前活跃数据文件。
4. 必要时滚动到新文件。
5. 根据 `SyncWrites` / `BytesPerSync` 决定是否刷盘。
6. 更新索引到新的物理位置。
7. 将旧位置统计到可回收空间。

### 6.2 写入优化点

#### 追加写而不是原地改写

这避免了随机覆盖写带来的复杂性，也减少了写路径上的结构维护成本。

#### 活跃文件与旧文件分离

活跃文件只负责接受新的追加写；旧文件只读。这使得：

- 新写入路径足够简单
- 历史文件可安全参与恢复与 merge
- 读路径可以稳定地通过 `Fid + Offset` 定位记录

#### 可配置刷盘策略

通过 `Options` 可以控制：

- `SyncWrites`: 每次写完立即刷盘
- `BytesPerSync`: 累积写入到一定字节数再刷盘

这允许你在“吞吐”和“持久性时效”之间做取舍。

#### `WriteBatch` 原子提交

批量写入不是简单循环调用 `Put`。它有自己的事务边界设计：

- 每个批次分配唯一事务序列号
- 批次中的所有记录都携带该序列号
- 最后追加一条事务完成标记
- 恢复时据此识别“哪些批次完整提交”

这能显著提升批量写入场景下的一致性表达能力。

### 6.3 读路径

单条 `Get` 的核心流程是：

1. 通过索引查到 `LogRecordPos`
2. 根据 `Fid` 定位活跃文件或旧文件
3. 从 `Offset` 读取日志记录
4. 返回 value
5. 若读到删除标记则视为 key 不存在

### 6.4 读取优化点

#### 索引直达

读路径不需要扫描整个数据文件，只要索引命中，就能直接定位到具体记录。

#### 启动阶段可选 mmap

`MMapAtStartup` 允许启动恢复时使用 mmap 方式读取数据文件。这个设计的目标不是改变运行时写入逻辑，而是降低启动阶段大量顺序读取时的开销。

#### 遍历不直接扫描原始文件

`ListKeys`、`Iterator`、`Fold` 都优先基于索引遍历，再按需回读 value，而不是直接把数据文件当成遍历源。

### 6.5 merge 机制

项目内部已经实现完整 merge 流程，但当前根包没有导出 `Merge()` 公共方法。你可以把它理解为一套已经存在的内部能力，核心目标是：

- 只重写当前仍有效的记录
- 生成 Hint 文件，加速下一次启动
- 用 `merge-finished` 标记处理 merge 接管边界
- 回收被覆盖或删除产生的无效空间

merge 是否值得执行，主要看两个条件：

- `reclaimSize` 是否足够大
- `reclaimSize / totalSize` 是否达到 `DataFileMergeRatio`

### 6.6 并发与进程级保护

项目做了两类保护：

- 根包使用 `RWMutex` 协调核心读写路径
- 数据目录使用文件锁，避免多个进程同时打开同一目录

这不是分布式锁，也不是复杂事务系统，但对单机嵌入式存储已经足够关键。

## 7. Redis 设计支持

`redis` 目录不是“把协议翻译成 KV”这么简单，而是一套明确的结构语义层。

### 7.1 当前支持的数据类型

- `string`
- `hash`
- `list`
- `set`
- `zset`

### 7.2 当前支持的方法

#### string

- `Set`
- `Get`
- `Del`
- `Expire`
- `TTL`

#### hash

- `HSet`
- `HGet`
- `HDel`
- `HExists`
- `HLen`

#### list

- `LPush`
- `RPush`
- `LPop`
- `RPop`
- `LLen`
- `LRange`

#### set

- `SAdd`
- `SRem`
- `SIsMember`
- `SCard`
- `SMembers`

#### zset

- `ZAdd`
- `ZRem`
- `ZScore`
- `ZCard`
- `ZRange`

### 7.3 Redis 层为什么能建立在 KV 引擎上

关键不在于“把 Redis 命令名字换成 Go 方法”，而在于项目采用了：

- metadata
- 版本号
- 子键编码
- 惰性过期

这四个核心机制。

### 7.4 元数据模型

每个逻辑 key 都有一条 metadata，包含：

- `typ`：数据类型
- `expireAt`：过期时间
- `version`：当前逻辑版本
- `size`：逻辑元素数
- `head` / `tail`：列表边界

调用任何 Redis 风格方法时，都会先读取 metadata 再决定后续行为。

### 7.5 版本号机制

版本号解决的是“复杂结构删除或覆盖时，如何不去立刻全量清理所有历史子键”。

例如一个 list 被删除后：

- 不必立刻删除所有旧 `list:<key>:<version>:<index>` 子键
- 只要 metadata 失效，或者新建更高版本
- 后续读取只认新版本

这非常适合追加写存储引擎。

### 7.6 惰性过期

当前采用惰性过期：

1. 先访问 key
2. 再检查是否过期
3. 若过期则删除 metadata
4. 历史子键等待后续 merge 清理

这种策略降低了后台任务复杂度，也更贴合 Bitcask 风格实现。

### 7.7 子键编码策略

不同结构会映射成不同的子键模式：

- `meta:<key>`
- `hash:<key>:<version>:<field>`
- `list:<key>:<version>:<index>`
- `set:<key>:<version>:<member>`
- `zset:dict:<key>:<version>:<member>`
- `zset:score:<key>:<version>:<score>:<member>`

这使得复杂结构仍然能落到一个统一的底层 KV 引擎里。

### 7.8 Redis 层的边界

需要特别说明的是：

- 它当前是 Go API，不是 Redis TCP 协议服务。
- 它追求“接近 Redis 语义”，不是“逐字节兼容 Redis 内部实现”。

详细说明见 [redis/README.md](./redis/README.md)。

## 8. 快速开始

### 8.1 环境要求

- Go `1.25.0`
- 本地可写数据目录

### 8.2 安装依赖

```bash
go mod tidy
```

### 8.3 最小示例

```go
package main

import (
	"fmt"
	"kvix"
	"kvix/common"
)

func main() {
	opts := common.DefaultOptions
	opts.DirPath = "/tmp/kvix-demo"

	db, err := kvix.Open(opts)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	if err := db.Put([]byte("name"), []byte("molly")); err != nil {
		panic(err)
	}

	value, err := db.Get([]byte("name"))
	if err != nil {
		panic(err)
	}

	fmt.Printf("name=%s\n", value)
}
```

## 9. 详细使用方法

### 9.1 打开数据库

```go
opts := common.DefaultOptions
opts.DirPath = "/tmp/kvix-data"
opts.IndexType = common.BPlusTreeIndex

db, err := kvix.Open(opts)
if err != nil {
	panic(err)
}
defer db.Close()
```

### 9.2 配置项说明

#### `DirPath`

数据目录。所有数据文件、索引文件、Hint 文件和序列号文件都会放在这里。

#### `DataFileSize`

单个数据文件的最大大小。活跃文件写满后会滚动到新文件。

#### `IndexType`

可选值：

- `common.BTreeIndex`
- `common.ARTreeIndex`
- `common.BPlusTreeIndex`

#### `SyncWrites`

是否每次写入后立即刷盘。更安全，但吞吐通常更低。

#### `BytesPerSync`

累计写入多少字节后执行一次同步刷盘。适合在吞吐和持久性之间折中。

#### `MMapAtStartup`

是否在启动恢复阶段使用 mmap 方式读取数据文件。这个选项主要影响启动恢复路径。

#### `DataFileMergeRatio`

无效数据占比达到该阈值时，merge 才有执行价值。

### 9.3 基础读写

```go
if err := db.Put([]byte("user:1"), []byte("alice")); err != nil {
	panic(err)
}

value, err := db.Get([]byte("user:1"))
if err != nil {
	panic(err)
}

fmt.Println(string(value))

if err := db.Delete([]byte("user:1")); err != nil {
	panic(err)
}
```

### 9.4 批量写入

```go
wb := db.NewWriteBatch(common.DefaultWriteBatchOptions)

if err := wb.Put([]byte("k1"), []byte("v1")); err != nil {
	panic(err)
}
if err := wb.Put([]byte("k2"), []byte("v2")); err != nil {
	panic(err)
}

if err := wb.Commit(); err != nil {
	panic(err)
}
```

如果你要更明确地控制批量行为，可以自定义：

```go
wb := db.NewWriteBatch(common.WriteBatchOptions{
	MaxBatchSize: 1000,
	SyncWrite:    true,
})
```

### 9.5 迭代器

```go
it := db.NewIterator(common.IteratorOptions{
	Prefix:  []byte("user:"),
	Reverse: false,
})
defer it.Close()

for it.Rewind(); it.Vaild(); it.Next() {
	value, err := it.Value()
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s=%s\n", it.Key(), value)
}
```

也可以使用 `Seek`：

```go
it.Seek([]byte("user:100"))
```

### 9.6 遍历与键列表

```go
keys, err := db.ListKeys()
if err != nil {
	panic(err)
}

err = db.Fold(func(key, value []byte) bool {
	fmt.Printf("%s=%s\n", key, value)
	return true
})
if err != nil {
	panic(err)
}
```

### 9.7 刷盘、统计和备份

```go
if err := db.Sync(); err != nil {
	panic(err)
}

stat := db.Stat()
fmt.Printf("keys=%d files=%d reclaim=%d disk=%d\n",
	stat.KeyNum,
	stat.DataFileNum,
	stat.ReclaimbleSize,
	stat.DiskSize,
)

if err := db.BackUp("/tmp/kvix-backup"); err != nil {
	panic(err)
}
```

### 9.8 选择索引实现

```go
opts := common.DefaultOptions
opts.DirPath = "/tmp/kvix-btree"
opts.IndexType = common.BTreeIndex
```

```go
opts := common.DefaultOptions
opts.DirPath = "/tmp/kvix-art"
opts.IndexType = common.ARTreeIndex
```

```go
opts := common.DefaultOptions
opts.DirPath = "/tmp/kvix-bptree"
opts.IndexType = common.BPlusTreeIndex
```

一个很实用的经验判断是：

- 想要简单直接、内存索引：先试 `BTree`
- 想要前缀树风格实现：试 `ART`
- 想要更快的重启恢复和持久化索引：优先 `B+Tree`

### 9.9 使用 Redis 风格数据结构

```go
package main

import (
	"fmt"
	"time"

	"kvix/common"
	"kvix/redis"
)

func main() {
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

	value, err := store.Get([]byte("name"))
	if err != nil {
		panic(err)
	}
	fmt.Println(string(value))

	ok, err := store.HSet([]byte("user:1"), []byte("city"), []byte("shanghai"))
	if err != nil {
		panic(err)
	}
	fmt.Println("field created:", ok)
}
```

如果你重点关注 Redis 层设计与方法说明，建议继续阅读 [redis/README.md](./redis/README.md)。

## 10. HTTP 示例服务

项目包含一个基于 Fiber 的 HTTP 示例服务，用于演示如何把 `kvix.DB` 暴露为接口。

### 10.1 启动

```bash
go run ./http
```

默认监听：

```text
127.0.0.1:8080
```

也可以通过环境变量指定：

```bash
KVIX_HTTP_ADDR=127.0.0.1:9090 go run ./http
```

### 10.2 示例请求

写入：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/entries \
  -H 'Content-Type: application/json' \
  -d '{"key":"name","value":"alice"}'
```

读取：

```bash
curl http://127.0.0.1:8080/api/v1/entries/name
```

删除：

```bash
curl -X DELETE http://127.0.0.1:8080/api/v1/entries/name
```

更多说明见 [http/README.md](./http/README.md)。

## 11. 测试与基准

### 11.1 全量测试

```bash
GOCACHE=$(pwd)/.cache/go go test ./... -count=1
```

### 11.2 分模块测试

```bash
GOCACHE=$(pwd)/.cache/go go test . -count=1
GOCACHE=$(pwd)/.cache/go go test ./index -count=1
GOCACHE=$(pwd)/.cache/go go test ./redis -count=1
GOCACHE=$(pwd)/.cache/go go test ./http -count=1
```

### 11.3 benchmark

```bash
GOCACHE=$(pwd)/.cache/go go test ./benchmark -bench . -benchmem
```

当前 benchmark 主要覆盖：

- `Put`
- `Get`
- `ReadAfterLoad`

以及不同 value 大小下的行为差异。

## 12. 目录结构说明

```text
kvix/
├── README.md
├── go.mod
├── batch.go
├── db.go
├── db_open.go
├── db_read.go
├── db_write.go
├── db_state.go
├── db_files.go
├── iterator.go
├── merge.go
├── common/
├── data/
├── fio/
├── index/
├── redis/
├── http/
├── utils/
├── examples/
├── benchmark/
└── docs/
```

模块职责简述：

- `common/`：配置、常量、错误定义
- `data/`：日志记录与数据文件
- `fio/`：文件 IO 抽象
- `index/`：索引接口与实现
- `redis/`：Redis 风格结构封装
- `http/`：HTTP 示例服务
- `utils/`：文件系统辅助函数
- `examples/`：最小使用示例
- `benchmark/`：性能基准

## 13. 当前边界与注意事项

在真正把它用于生产或进一步扩展前，建议先明确这些边界：

- 当前已经有完整内部 merge 实现，但没有公开 `Merge()` 方法。
- Redis 层是 Go API，不是 Redis 协议服务。
- 当前 README 重点保证“与现有实现一致”，不会虚构未实现能力。
- 如果你要做线上使用，建议先根据你的场景明确索引类型、刷盘策略和备份策略。
- 项目目前更像“可读、可扩展、可研究的存储内核”，而不是包装好的数据库产品。

如果你要继续深入，建议阅读这些模块文档：

- [index/README.md](./index/README.md)
- [redis/README.md](./redis/README.md)
- [http/README.md](./http/README.md)
- [benchmark/README.md](./benchmark/README.md)

## 14. 部署说明

项目当前最适合部署的入口是：

```bash
./http
```

为了让这个 HTTP 服务适合真实服务器长期运行，项目支持以下环境变量：

- `KVIX_HTTP_ADDR`
- `KVIX_HTTP_DATA_DIR`

其中：

- `KVIX_HTTP_ADDR` 用来指定监听地址
- `KVIX_HTTP_DATA_DIR` 用来指定固定数据目录

如果没有设置 `KVIX_HTTP_DATA_DIR`，服务会继续沿用示例模式，自动创建临时目录并在退出时清理；这适合本地演示，但不适合服务器部署。

完整部署文档见：

- [DEPLOYMENT.md](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/DEPLOYMENT.md)
