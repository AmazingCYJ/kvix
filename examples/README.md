# examples

`examples` 模块提供最小可运行示例，帮助第一次接触 `kvix` 的读者快速完成“打开数据库 -> 写入 -> 读取 -> 删除”这条闭环。

## 目录

- [1. 模块目标](#1-模块目标)
- [2. 当前示例说明](#2-当前示例说明)
- [3. 运行方式](#3-运行方式)
- [4. 适合谁看](#4-适合谁看)
- [5. 与核心引擎的关系](#5-与核心引擎的关系)

## 1. 模块目标

这个目录的目标很明确：

- 不解释所有实现细节
- 不展示全部 API
- 先给你一条最短可跑通路径

它适合用来做：

- 首次上手
- 验证环境
- 快速确认项目能跑

## 2. 当前示例说明

当前示例文件：

- [examples/basic_operation.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/examples/basic_operation.go)

示例流程：

1. 使用默认配置打开数据库
2. 写入一条记录
3. 读取并打印
4. 删除记录
5. 再次读取已删除 key

这个示例的价值在于，它把最常用的几个 API 放在一段很短的代码里，适合初学者先看懂整体使用方式。

## 3. 运行方式

在仓库根目录执行：

```bash
go run ./examples/basic_operation.go
```

示例默认使用的数据目录是：

```text
/tmp/kvix-data
```

如果你反复运行示例，建议先确认这个目录是否符合你的预期。

## 4. 适合谁看

推荐阅读顺序：

1. 先看根目录 [README.md](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/README.md)
2. 再跑这个示例
3. 最后回头看根包 `Open`、`Put`、`Get`、`Delete`

如果你是 Go 初学者，这个目录就是最适合的入口。

## 5. 与核心引擎的关系

示例不引入任何额外封装，它直接调用：

- `kvix.Open`
- `db.Put`
- `db.Get`
- `db.Delete`

因此它可以看作“最接近真实使用方式”的最小样例。
