# common

## 模块职责

`common` 负责集中定义 kvix 的公共配置、共享常量和跨模块复用的错误值。

## 设计思路

底层存储、索引、Redis 封装和 HTTP 示例都会依赖同一组配置和错误语义，因此把这些定义收敛到 `common`，可以避免循环依赖和重复定义。

## 关键类型与关键文件

- `options.go`：定义 `Options`、`IteratorOptions`、`WriteBatchOptions` 与默认配置。
- `errors.go`：定义常见错误值，供上层统一判断和映射。

## 核心流程

1. 启动阶段读取 `Options`，决定目录、索引实现、刷盘策略和 merge 阈值。
2. 运行阶段通过共享错误值向上层暴露统一的失败语义。

## 使用方式

```go
opts := common.DefaultOptions
opts.DirPath = "/tmp/kvix"
opts.IndexType = common.BPlusTreeIndex
```

## 与其他模块的关系

- 根包在 `Open`、`WriteBatch`、迭代器等流程中读取这些配置。
- `redis` 和 `http` 会复用公共错误值做语义映射。
