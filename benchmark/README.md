# benchmark

## 模块职责

`benchmark` 提供 kvix 的性能基准，重点覆盖基础写入、热读以及重启恢复后的读路径。

## 目录说明

- `bench_test.go`：定义 benchmark 配置、数据准备逻辑和三个基准场景。

## 启动方式或运行方式

在仓库根目录执行：

```bash
GOCACHE=$(pwd)/.cache/go go test ./benchmark -bench . -benchmem
```

## 示例输入输出

基准结果会按照 `go test -bench` 的标准格式输出，每个 value 大小都会单独给出吞吐和分配信息。

## 与核心引擎的关系

benchmark 直接面向 `kvix.DB`，适合观察不同 value 大小、目录重开与索引恢复对底层引擎的影响。
