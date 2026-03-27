# fio

`fio` 模块为 `kvix` 提供统一的文件 IO 抽象，让上层不必关心当前到底是在用标准文件读写，还是在用 mmap 做只读访问。

## 目录

- [1. 模块职责](#1-模块职责)
- [2. 为什么要抽象 IOManager](#2-为什么要抽象-iomanager)
- [3. 当前两种实现](#3-当前两种实现)
- [4. IO 选择策略](#4-io-选择策略)
- [5. 设计取舍](#5-设计取舍)
- [6. 与其他模块的关系](#6-与其他模块的关系)
- [7. 使用与测试](#7-使用与测试)

## 1. 模块职责

`fio` 负责：

- 统一读写接口
- 标准文件 IO 实现
- mmap 只读实现
- IO 实现选择工厂

它不负责：

- 记录编码
- 索引维护
- 事务逻辑

## 2. 为什么要抽象 IOManager

如果根包和 `data` 直接依赖 `os.File`，会有两个问题：

- 启动恢复时很难平滑切换到 mmap
- 上层会直接感知不同 IO 模式的细节

引入 `IOManager` 后，上层只需要知道这些统一能力：

- `ReadAt`
- `Write`
- `Sync`
- `Close`
- `Size`

这样：

- `DataFile` 不需要关心底层实现细节
- 启动恢复阶段可以使用 mmap
- 运行期再切回标准文件 IO

## 3. 当前两种实现

### 3.1 标准文件 IO

实现文件：

- [fio/file_io.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/fio/file_io.go)

特点：

- 基于 `os.File`
- 支持读、写、刷盘
- 适合运行期正常读写路径

### 3.2 mmap 只读 IO

实现文件：

- [fio/mmap.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/fio/mmap.go)

特点：

- 基于 `golang.org/x/exp/mmap`
- 只支持读取
- 不支持写入
- 更适合启动恢复或读多写少场景

## 4. IO 选择策略

工厂方法：

- [fio/io_manager.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/fio/io_manager.go)

当前策略很直接：

- `StandardFIO` 用标准文件 IO
- `MemoryMap` 用 mmap

在 `kvix` 中常见的使用方式是：

1. 启动恢复阶段可选 mmap
2. 恢复完成后切回标准文件 IO

这样做的原因是：

- 启动时可能需要大量顺序读取
- 运行期仍然需要稳定可写的标准文件句柄

## 5. 设计取舍

### 5.1 为什么 mmap 不支持写

当前设计不是为了做统一“可读可写 mmap 文件层”，而是为了给恢复路径提供一个更轻量的读取模式。

因此：

- mmap 只承担读取职责
- 写入仍交给标准文件 IO

### 5.2 为什么不让上层自己决定每次调用用哪种 IO

因为这样会把 IO 细节泄漏到整个系统里。

现在的设计把“选择哪种 IO”收敛在：

- 打开文件时
- 启动恢复结束时切换

边界更清晰。

## 6. 与其他模块的关系

- `data.DataFile` 直接依赖 `IOManager`。
- 根包通过 `resetIoType` 在启动恢复后切回标准 IO。
- `mmap` 的使用主要服务于启动恢复路径，而不是正常写入路径。

## 7. 使用与测试

示例：

```go
ioManager, err := fio.NewIOManager(path, fio.StandardFIO)
if err != nil {
	panic(err)
}
defer ioManager.Close()
```

测试命令：

```bash
GOCACHE=$(pwd)/.cache/go go test ./fio -count=1
```

重点测试覆盖：

- mmap 创建与读取
- mmap 不支持写入
- 文件大小读取
