# examples

## 模块职责

`examples` 存放最小可运行示例，帮助读者快速理解如何在代码里使用 kvix。

## 目录说明

- `basic_operation.go`：展示数据库打开、写入、读取和删除的基础流程。

## 启动方式或运行方式

在仓库根目录执行：

```bash
go run ./examples/basic_operation.go
```

## 示例输入输出

示例会向 `/tmp/kvix-data` 写入一条 `name -> 茉莉` 记录，随后读取并打印结果。

## 与核心引擎的关系

示例直接调用 `kvix.Open`、`Put`、`Get`、`Delete`，适合作为阅读根包 API 的最短路径。
