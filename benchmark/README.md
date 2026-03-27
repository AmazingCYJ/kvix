# benchmark

`benchmark` 模块提供 `kvix` 的性能基准，用来观察不同 value 大小、不同读写阶段以及“重启后再读”这类路径的性能表现。

## 目录

- [1. 模块目标](#1-模块目标)
- [2. 当前 benchmark 覆盖范围](#2-当前-benchmark-覆盖范围)
- [3. 基准设计思路](#3-基准设计思路)
- [4. 为什么这样设计 benchmark](#4-为什么这样设计-benchmark)
- [5. 运行方式](#5-运行方式)
- [6. 如何理解结果](#6-如何理解结果)
- [7. 与核心引擎的关系](#7-与核心引擎的关系)

## 1. 模块目标

这些 benchmark 的目标不是“跑一个很大的压测平台”，而是稳定观察几条核心路径：

- `Put`
- `Get`
- `ReadAfterLoad`

这样可以更容易把结果和引擎设计联系起来。

## 2. 当前 benchmark 覆盖范围

实现文件：

- [benchmark/bench_test.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/benchmark/bench_test.go)

当前覆盖：

- 不同 value 大小下的 `Put`
- 热数据 `Get`
- 写入 -> 关闭 -> 重开 -> 再读

value 大小当前包括：

- `128B`
- `1KB`
- `4KB`

## 3. 基准设计思路

benchmark 里有几个重要设计点：

### 3.1 固定 benchmark 配置

benchmark 使用明确的 `common.Options`，避免不同运行之间配置漂移导致结果难比较。

### 3.2 子用例独立目录

每个 benchmark 子用例都使用独立目录，避免：

- 文件锁互相影响
- 历史数据残留污染结果

### 3.3 预热放在计时区间外

例如 `BenchmarkKvixGet` 中，预写入数据不计入正式读取吞吐。

这样测出来的结果更接近“读路径本身”的成本。

### 3.4 明确覆盖重启后读取

`BenchmarkKvixReadAfterLoad` 的价值在于，它不只测热读，还把：

- 写入
- 关闭
- 重新打开
- 再读取

这条实际很常见的路径单独拿出来看。

## 4. 为什么这样设计 benchmark

因为对 `kvix` 这种引擎来说，性能不只体现在“单次写入快不快”，还体现在：

- value 大小时的吞吐变化
- 索引恢复后读路径是否稳定
- 数据目录重开后读取成本如何

当前 benchmark 范围虽然不算大，但和项目真实设计强相关。

## 5. 运行方式

### 5.1 标准运行

```bash
GOCACHE=$(pwd)/.cache/go go test ./benchmark -bench . -benchmem
```

### 5.2 只跑某一类 benchmark

```bash
GOCACHE=$(pwd)/.cache/go go test ./benchmark -bench KvixGet -benchmem
```

## 6. 如何理解结果

结果里你通常会看到：

- ns/op
- B/op
- allocs/op

建议关注：

- value 变大后，`Put` 和 `Get` 的变化趋势
- `ReadAfterLoad` 是否明显慢于热读
- 分配次数是否稳定

## 7. 与核心引擎的关系

benchmark 直接面向 `kvix.DB`，因此结果会同时反映：

- 追加写路径
- 索引定位能力
- 数据文件读取成本
- 重启后的索引恢复与读取成本

它不是业务层 benchmark，而是引擎层 benchmark。
