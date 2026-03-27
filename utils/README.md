# utils

## 模块职责

`utils` 存放与业务语义无关但被核心流程复用的文件系统辅助函数。

## 设计思路

这类能力通常围绕目录大小、剩余磁盘空间和目录复制展开，单独收敛后可以避免散落在 merge、备份和测试辅助代码中。

## 关键类型与关键文件

- `file.go`：目录大小、磁盘可用空间和目录复制相关函数。

## 核心流程

1. merge 或备份流程通过 `GetDirSize`、`GetDiskFreeSpace` 评估资源条件。
2. `CopyDir` 用于备份目录或复制测试数据目录。

## 使用方式

```go
if err := utils.CopyDir(src, dst, []string{"*.tmp"}); err != nil {
	panic(err)
}
```

## 与其他模块的关系

- 根包的 merge / backup 逻辑依赖这些文件系统工具。
- 测试代码也会复用同样的目录辅助函数。
