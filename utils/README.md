# utils

`utils` 模块存放与具体业务语义无关、但会被核心流程反复复用的文件系统辅助函数。

## 目录

- [1. 模块职责](#1-模块职责)
- [2. 当前提供的能力](#2-当前提供的能力)
- [3. 为什么这些函数值得单独抽出](#3-为什么这些函数值得单独抽出)
- [4. 使用方式](#4-使用方式)
- [5. 与其他模块的关系](#5-与其他模块的关系)

## 1. 模块职责

`utils` 当前主要提供：

- 目录大小统计
- 磁盘剩余空间统计
- 目录复制

它不关心：

- 记录编码
- 索引实现
- Redis 语义

## 2. 当前提供的能力

实现文件：

- [utils/file.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/utils/file.go)

### 2.1 `GetDirSize`

递归统计目录下所有普通文件的总大小。

它在项目中的主要用途是：

- merge 前估算当前数据目录总大小

### 2.2 `GetDiskFreeSpace`

获取当前工作目录所在磁盘的可用空间。

它在项目中的主要用途是：

- merge 前判断磁盘是否还有足够空间写出新的有效数据集

### 2.3 `CopyDir`

递归复制目录，并允许按模式排除某些文件或目录。

它在项目中的主要用途是：

- `BackUp`
- 某些测试辅助目录复制场景

## 3. 为什么这些函数值得单独抽出

如果把这类函数直接散落在：

- `merge.go`
- `db_state.go`
- 测试代码

会导致：

- 文件职责混乱
- 同类逻辑重复实现
- 文件系统细节侵入核心存储逻辑

单独放在 `utils` 后，根包可以更专注于数据库本身的状态流转。

## 4. 使用方式

```go
size, err := utils.GetDirSize("/tmp/kvix")
if err != nil {
	panic(err)
}
fmt.Println(size)
```

```go
free, err := utils.GetDiskFreeSpace()
if err != nil {
	panic(err)
}
fmt.Println(free)
```

```go
if err := utils.CopyDir(src, dst, []string{"flock"}); err != nil {
	panic(err)
}
```

## 5. 与其他模块的关系

- 根包的 merge 流程依赖目录大小和磁盘空间检测。
- 备份流程依赖目录复制能力。
- 工具函数让核心数据库逻辑不必直接处理太多文件系统细节。
