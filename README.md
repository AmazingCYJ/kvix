# kvix

## 项目简介

`kvix` 是一个 Bitcask 风格的 Go 键值存储项目，提供基础 KV 能力、批量写入、迭代器、merge、可切换索引实现，以及基于底层引擎封装的 Redis 风格数据结构与 HTTP 示例。

## 核心特性

- 追加写数据文件与内存索引结合的基础 KV 引擎
- `WriteBatch`、迭代器、merge 压缩整理
- `BTree`、`ART`、`B+Tree` 多种索引实现
- `redis` 模块提供字符串、哈希、列表、集合、有序集合封装
- `http` 模块提供基于 Fiber 的示例 API

## 目录结构

- `db*.go`、`batch.go`、`iterator.go`、`merge.go`：核心数据库能力
- `common/`：公共配置、常量和错误定义
- `data/`：日志记录与数据文件编码
- `fio/`：文件 IO 抽象
- `index/`：索引接口与实现
- `redis/`：Redis 风格数据结构封装
- `http/`：HTTP 示例服务
- `examples/`：最小可运行示例
- `benchmark/`：性能基准

## 快速开始

```go
opts := common.DefaultOptions
opts.DirPath = "/tmp/kvix"

db, err := kvix.Open(opts)
if err != nil {
	panic(err)
}
defer db.Close()
```

## 基本读写示例

```go
if err := db.Put([]byte("name"), []byte("molly")); err != nil {
	panic(err)
}

value, err := db.Get([]byte("name"))
if err != nil {
	panic(err)
}

fmt.Printf("name=%s\n", value)
```

## Redis 风格模块简介

`redis` 模块把复杂数据结构落到底层 KV 引擎上，采用 metadata、版本号和惰性过期机制来表达结构语义。详情见 `redis/README.md`。

## HTTP 示例简介

`http` 目录演示如何把 `kvix.DB` 封装为一个简单的 JSON API，默认监听 `127.0.0.1:8080`。详情见 `http/README.md`。

## 测试方式

常用验证命令如下：

```bash
GOCACHE=$(pwd)/.cache/go go test ./... -count=1
GOCACHE=$(pwd)/.cache/go go test ./redis -count=1
GOCACHE=$(pwd)/.cache/go go test ./http -count=1
```
