# common

`common` 模块集中存放 `kvix` 的公共配置、共享常量和跨模块复用的错误值。它本身不承载读写逻辑，但整个项目几乎每一层都会依赖它。

如果把 `kvix` 看成一个分层系统，那么 `common` 就是这些层之间共享的“基础语言”：

- 用哪些配置驱动数据库启动
- 用哪些错误表达统一失败语义
- 用哪些常量约定文件命名和索引类型

## 目录

- [1. 模块职责](#1-模块职责)
- [2. 配置模型](#2-配置模型)
- [3. 错误模型](#3-错误模型)
- [4. 常量与约定](#4-常量与约定)
- [5. 为什么单独拆成 common](#5-为什么单独拆成-common)
- [6. 使用方法](#6-使用方法)
- [7. 与其他模块的关系](#7-与其他模块的关系)

## 1. 模块职责

`common` 主要负责三类内容：

1. 数据库启动与运行配置
2. 通用错误定义
3. 跨模块共享常量

它不负责：

- 数据文件编码
- 索引实现
- Redis 结构语义
- HTTP 请求处理

## 2. 配置模型

实现文件：

- [common/options.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/common/options.go)

### 2.1 `Options`

`Options` 是打开数据库时最重要的配置结构。

当前字段包括：

- `DirPath`
- `DataFileSize`
- `IndexType`
- `SyncWrites`
- `BytesPerSync`
- `MMapAtStartup`
- `DataFileMergeRatio`

这些字段分别影响：

- 数据目录位置
- 数据文件滚动阈值
- 索引实现
- 写入刷盘策略
- 启动时的读取方式
- merge 是否值得执行

### 2.2 `IteratorOptions`

`IteratorOptions` 用来配置数据库级迭代器：

- `Prefix`
- `Reverse`

它只影响“遍历视图”，不会改变底层索引内容。

### 2.3 `WriteBatchOptions`

`WriteBatchOptions` 用来控制批量写入：

- `MaxBatchSize`
- `SyncWrite`

它影响的是：

- 单次批量写入允许积累多少操作
- 提交后是否立即刷盘

### 2.4 默认配置

`DefaultOptions` 给出项目默认配置，当前默认索引是：

```go
common.BPlusTreeIndex
```

这个默认值的含义不是“别的索引不能用”，而是当前项目更偏向：

- 持久化索引
- 更快的启动恢复

## 3. 错误模型

实现文件：

- [common/errors.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/common/errors.go)

这些错误值的作用，不只是“报错”，更重要的是让上层能做稳定判断。

例如：

- `ErrKeyNotFound`
- `ErrDataFileNotFound`
- `ErrMergeIsProgress`
- `ErrNoEnoughDiskSpace`
- `ErrMMapWriteNotSupported`

统一错误值的好处是：

- 根包可以清晰表达语义
- Redis 层可以复用底层错误
- HTTP 层可以稳定映射状态码

## 4. 常量与约定

`common` 里还包含若干全局约定，例如：

- 数据文件后缀
- Hint 文件名
- merge 完成标记文件名
- 事务序列号文件名
- 索引类型枚举

这些约定的价值在于：

- 避免字符串散落在不同模块
- 减少命名不一致导致的恢复错误
- 让启动恢复和 merge 接管逻辑更统一

## 5. 为什么单独拆成 common

如果没有 `common`，配置和错误很容易分散在：

- 根包
- `index`
- `redis`
- `http`

这样会产生几个问题：

- 循环依赖风险上升
- 错误语义不统一
- 同一配置在多处重复定义

把这些定义收敛到 `common` 后，模块边界更清晰。

## 6. 使用方法

### 6.1 打开数据库时设置配置

```go
opts := common.DefaultOptions
opts.DirPath = "/tmp/kvix"
opts.IndexType = common.BPlusTreeIndex
opts.SyncWrites = true
```

### 6.2 创建迭代器时设置选项

```go
it := db.NewIterator(common.IteratorOptions{
	Prefix:  []byte("user:"),
	Reverse: false,
})
```

### 6.3 创建批量写入时设置选项

```go
wb := db.NewWriteBatch(common.WriteBatchOptions{
	MaxBatchSize: 500,
	SyncWrite:    true,
})
```

## 7. 与其他模块的关系

- 根包通过 `Options` 决定数据库启动和运行方式。
- `index` 通过 `IndexerType` 决定实际索引实现。
- `redis` 和 `http` 复用通用错误值做语义映射。
- merge、恢复、迭代和批量写入都依赖这里定义的配置模型。
