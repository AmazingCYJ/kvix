# index

`index` 模块负责维护 `key -> data.LogRecordPos` 的映射，是 `kvix` 读路径、遍历能力和启动恢复体验的关键支撑层。根包的所有读写流程都依赖它，但根包本身并不绑定某一种具体索引实现。

这份文档重点解释：

- 为什么 `kvix` 需要单独的索引层
- 当前三种索引实现分别适合什么场景
- 项目做了哪些索引相关优化
- 如果你要继续扩展索引能力，应该从哪里入手

## 目录

- [1. 模块职责](#1-模块职责)
- [2. 为什么需要索引层](#2-为什么需要索引层)
- [3. 统一接口设计](#3-统一接口设计)
- [4. 当前索引实现](#4-当前索引实现)
- [5. 索引相关优化](#5-索引相关优化)
- [6. 启动恢复与索引重建](#6-启动恢复与索引重建)
- [7. 迭代器设计](#7-迭代器设计)
- [8. 如何选择索引类型](#8-如何选择索引类型)
- [9. 与其他模块的关系](#9-与其他模块的关系)
- [10. 开发与扩展建议](#10-开发与扩展建议)
- [11. 使用与测试](#11-使用与测试)

## 1. 模块职责

`index` 模块只做一件事：把逻辑 key 映射到磁盘上的最新记录位置。

它不负责：

- 保存 value 本体
- 维护事务日志
- 编码日志记录
- 判断 Redis 语义
- 管理数据文件滚动

它负责的核心数据是：

- key
- `data.LogRecordPos{Fid, Offset, Size}`

也就是说，索引层只告诉上层“去哪个文件、哪个偏移读最新值”，真正的 value 仍然保存在数据文件里。

## 2. 为什么需要索引层

`kvix` 的底层是追加写数据文件。追加写有很多好处，但也带来一个直接问题：

- 写入很简单
- 读取时如果没有索引，就必须扫描数据文件

随着数据量变大，扫描成本会迅速上升。因此必须引入索引层，做到：

1. `Put` 时记录 key 的最新位置。
2. `Get` 时通过 key 直接定位到具体记录。
3. `Delete` 时把索引中的 key 移除。
4. 遍历时按有序 key 输出快照。

从系统角度看，索引层的价值主要有三点：

- 把读路径从“扫描日志”优化成“查 key -> 跳转读取”
- 把遍历能力从“按文件顺序”转成“按 key 顺序”
- 为启动恢复、Hint 文件和 B+Tree 持久化索引提供统一挂载点

## 3. 统一接口设计

根包通过 `index.Indexer` 接口依赖索引层，而不是直接依赖具体结构。

核心接口见 [index.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/index/index.go)：

```go
type Indexer interface {
	Put(key []byte, pos *data.LogRecordPos) *data.LogRecordPos
	Get(key []byte) *data.LogRecordPos
	Delete(key []byte) (*data.LogRecordPos, bool)
	Size() int
	Iterator(reverse bool) IndexIterator
	Close() error
}
```

这个设计有两个直接收益：

- 根包无需知道索引底层是树、基数树还是 bbolt。
- 新增索引实现时，不必改动 `DB` 的主读写流程。

### 3.1 `Put`

记录 key 的最新位置，返回旧位置。

根包会利用“旧位置”做两件事：

- 更新可回收空间统计
- 识别哪些历史记录已经失效

### 3.2 `Get`

按 key 读取最新位置。

这个调用本身不返回 value。根包拿到位置后，再去数据文件读取真正的 value。

### 3.3 `Delete`

从索引中移除 key，并返回旧位置。

这样根包就能知道：

- 删除是否真的发生
- 被删除记录占了多少空间

### 3.4 `Iterator`

返回索引级迭代器，而不是数据库级 value 迭代器。

这样做的好处是：

- 迭代器可以只关注 key 顺序和位置快照
- 上层需要 value 时再回到数据文件读取

## 4. 当前索引实现

当前项目内置三种索引实现：

- `BTree`
- `ART`
- `B+Tree`

### 4.1 BTree

实现文件：

- [index/btree.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/index/btree.go)

特点：

- 基于 `github.com/google/btree`
- 完全内存索引
- key 有序
- `Seek` 和正反向遍历实现直接
- 用 `RWMutex` 保护并发访问

适合场景：

- 追求实现稳定、可预测
- 需要有序 key 访问
- 希望索引层足够容易理解和调试

代价：

- 启动时需要恢复内存索引
- 索引不单独持久化

### 4.2 Adaptive Radix Tree

实现文件：

- [index/art.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/index/art.go)

特点：

- 基于 `go-adaptive-radix-tree`
- 完全内存索引
- 对前缀结构较友好
- 仍然提供有序遍历与 `Seek`
- 使用 `RWMutex` 保护并发访问

适合场景：

- key 呈现明显前缀结构
- 希望尝试基数树风格索引
- 想保留 prefix 类数据组织的自然优势

代价：

- 启动时同样需要恢复内存索引
- 迭代器需要从树结构构建快照切片

### 4.3 B+Tree

实现文件：

- [index/bptree.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/index/bptree.go)

特点：

- 基于 `bbolt`
- 索引单独持久化到 `bptree-index` 文件
- key 有序
- 启动时不需要像纯内存索引一样完整回放所有历史数据文件来重建索引

适合场景：

- 更关注重启后的恢复速度
- 希望索引本身具备持久化能力
- 可以接受额外的 bbolt 维护开销

代价：

- 写路径不只是数据文件追加，还要更新 bbolt 索引
- 索引文件会引入额外 IO 和事务成本

### 4.4 三种实现对比

| 维度 | BTree | ART | B+Tree |
| --- | --- | --- | --- |
| 索引存储位置 | 内存 | 内存 | 磁盘 |
| key 是否有序 | 是 | 是 | 是 |
| 启动恢复成本 | 较高 | 较高 | 较低 |
| 前缀友好度 | 中 | 高 | 中 |
| 实现复杂度 | 低 | 中 | 中 |
| 默认索引 | 否 | 否 | 是 |

## 5. 索引相关优化

这个模块最值得关注的不是“用了什么树”，而是围绕索引做了哪些系统级优化。

### 5.1 根包只依赖接口

这是一种架构层面的优化。

因为根包只依赖 `Indexer`，所以：

- 不需要为每种索引写一套不同的 `Put` / `Get` 逻辑
- 未来新增索引实现时，扩展成本较低
- 可以把“索引实现变化”控制在 `index/` 目录内部

### 5.2 旧位置回传

`Put` 和 `Delete` 都会返回旧位置。

这让根包能立即知道：

- 哪些历史记录已经失效
- 哪些空间之后可以被 merge 回收

这不是简单的 API 设计问题，而是索引和存储层协作的重要优化点。

### 5.3 启动恢复优化

对内存索引，项目优先尝试加载 Hint 文件，而不是直接全量扫描数据文件。

这意味着：

- merge 完成后会生成新的 Hint
- 下次启动时可先从 Hint 重建 key -> pos
- 只有必要时才继续补扫数据文件

这是索引恢复体验上的关键优化。

### 5.4 B+Tree 持久化索引

如果你使用 `BPlusTreeIndex`，索引不再完全依赖内存回放构建，而是直接从 bbolt 文件恢复。

这种优化最适合：

- 数据量较大
- 进程重启频繁
- 更希望把恢复时间压下去

### 5.5 快照式迭代器

三种索引实现都选择了“创建迭代器时先构建当前视图”的思路，而不是让遍历过程直接挂在底层可变结构上。

这样做的好处：

- `Seek` 简单
- 正反向遍历简单
- 遍历过程中不容易被并发写入打乱
- 使用切片 + 二分搜索实现 `Seek` 较直接

代价也很明确：

- 创建迭代器会复制当前视图
- 极大索引规模下会有额外内存开销

对 `kvix` 当前的设计目标来说，这个取舍是合理的。

## 6. 启动恢复与索引重建

索引恢复是 `kvix` 启动路径里很关键的一部分。

### 6.1 内存索引恢复路径

对 `BTree` 和 `ART`：

1. 打开数据库目录
2. 扫描并打开现有数据文件
3. 尝试加载 Hint 文件
4. 必要时继续回放数据文件
5. 重建最新 key -> pos 映射

### 6.2 B+Tree 恢复路径

对 `B+Tree`：

1. 打开独立索引文件
2. 恢复索引 bucket
3. 恢复事务序列号
4. 校正活跃文件写偏移

它的重点不在“完全不用恢复”，而在于“不再需要把索引完全依赖数据文件回放重建”。

### 6.3 merge 与 Hint 的关系

merge 结束后会写出 Hint 文件。Hint 文件本质上是一个“key -> 新位置”的快照。

这个设计让索引层获得两个收益：

- 启动时少扫大量历史记录
- merge 后索引恢复路径更短

## 7. 迭代器设计

索引层定义了统一的 `IndexIterator`：

```go
type IndexIterator interface {
	Rewind()
	Seek(key []byte)
	Next()
	Valid() bool
	Key() []byte
	Value() *data.LogRecordPos
	Close()
}
```

### 7.1 为什么索引层就要有迭代器

因为数据库级遍历能力依赖“按 key 顺序”的视图，而不是按文件写入顺序扫描。

根包的：

- `ListKeys`
- `Fold`
- 用户态 `Iterator`

本质上都建立在索引迭代器之上。

### 7.2 `Seek` 的实现思路

当前几种实现基本都采用：

- 先获得有序切片
- 再通过 `sort.Search` 做二分定位

这样在实现上很直接，也能保持 `Seek` 语义一致。

### 7.3 前缀过滤为什么不放在索引层

数据库级前缀过滤是在根包迭代器里做的，而不是强行塞进所有索引实现里。

这样做的原因是：

- 保持索引层职责纯粹
- 避免每种索引实现都维护一套前缀过滤逻辑
- 让“按 key 有序输出位置”和“按业务条件过滤”解耦

## 8. 如何选择索引类型

可以按下面的经验来选：

### 8.1 默认建议

如果你没有特别明确的约束，先用：

```go
opts.IndexType = common.BPlusTreeIndex
```

原因是当前项目默认就这样配置，且它对启动恢复更友好。

### 8.2 想要最直观的内存索引

用：

```go
opts.IndexType = common.BTreeIndex
```

适合：

- 调试
- 学习
- 结构简单优先

### 8.3 想要前缀树风格索引

用：

```go
opts.IndexType = common.ARTreeIndex
```

适合：

- key 前缀层级明显
- 想探索 ART 的行为表现

### 8.4 想要更快的重启恢复

继续用：

```go
opts.IndexType = common.BPlusTreeIndex
```

因为它的核心优势就在这里。

## 9. 与其他模块的关系

- [db_open.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/db_open.go) 在启动时创建索引并驱动恢复。
- [db_write.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/db_write.go) 在写入时更新索引。
- [db_read.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/db_read.go) 通过索引定位最新记录。
- [iterator.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/iterator.go) 基于索引迭代器向外暴露用户态遍历能力。
- [data/](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/data) 提供索引值指向的 `LogRecordPos` 结构。

## 10. 开发与扩展建议

如果你要新增一种索引实现，建议遵守这几个边界：

1. 只在 `index/` 内实现，不要把根包主流程和具体结构耦合起来。
2. 保持 `Put` / `Get` / `Delete` / `Iterator` / `Close` 语义一致。
3. 迭代器要保证 key 顺序稳定。
4. `Seek` 语义要与已有实现一致。
5. 返回旧位置的能力不要丢，否则会影响 `reclaimSize` 统计。

## 11. 使用与测试

### 使用方式

应用层通常不直接 new 某个索引，而是通过数据库配置选择：

```go
opts := common.DefaultOptions
opts.DirPath = "/tmp/kvix"
opts.IndexType = common.BPlusTreeIndex

db, err := kvix.Open(opts)
if err != nil {
	panic(err)
}
defer db.Close()
```

### 测试命令

```bash
GOCACHE=$(pwd)/.cache/go go test ./index -count=1
```

当前测试覆盖了：

- BTree 基础 CRUD 与迭代
- ART 基础 CRUD 与迭代
- B+Tree CRUD、持久化、事务回滚、迭代与快照行为
