# fio

## 模块职责

`fio` 为 kvix 提供统一的文件 IO 抽象，屏蔽标准文件读写与 mmap 读取之间的差异。

## 设计思路

上层只依赖 `IOManager` 接口，不直接关心底层是 `os.File` 还是 `mmap.ReaderAt`。这样既能保持读写路径简单，也能在启动恢复场景下切换更合适的 IO 模式。

## 关键类型与关键文件

- `io_manager.go`：定义 `IOManager` 接口和 `NewIOManager` 工厂。
- `file_io.go`：标准文件 IO 实现。
- `mmap.go`：只读 mmap 实现。

## 核心流程

1. 启动或文件切换阶段根据配置选择具体 IO 实现。
2. `data.DataFile` 通过 `IOManager` 统一执行读、写、刷盘和关闭。

## 使用方式

```go
ioManager, err := fio.NewIOManager(path, fio.StandardFIO)
```

## 与其他模块的关系

- `data` 通过 `IOManager` 读写日志文件。
- 根包会在启动恢复后根据配置切换回标准 IO。
