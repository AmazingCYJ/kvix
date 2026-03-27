# data

`data` 模块负责表达 `kvix` 最底层的数据事实：一条记录在磁盘上长什么样、一个数据文件如何打开和读写、索引里保存的位置到底指向什么。

如果把根包看成“数据库协调层”，那么 `data` 就是“磁盘记录层”。

## 目录

- [1. 模块职责](#1-模块职责)
- [2. 记录模型](#2-记录模型)
- [3. 数据文件模型](#3-数据文件模型)
- [4. 编码与解码流程](#4-编码与解码流程)
- [5. Hint 与 merge 相关能力](#5-hint-与-merge-相关能力)
- [6. 设计取舍](#6-设计取舍)
- [7. 与其他模块的关系](#7-与其他模块的关系)
- [8. 使用与测试](#8-使用与测试)

## 1. 模块职责

`data` 主要做四件事：

1. 定义日志记录结构
2. 定义位置索引结构
3. 负责数据文件打开、读取、写入和刷盘
4. 为 merge / Hint / 启动恢复提供底层格式支持

它不负责：

- 判断一个 key 是否最新
- 维护索引树
- 决定事务何时提交
- 判断 Redis 类型语义

## 2. 记录模型

实现文件：

- [data/log_record.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/data/log_record.go)

### 2.1 `LogRecord`

`LogRecord` 表示一条逻辑记录，包含：

- `Key`
- `Value`
- `Type`

`Type` 当前有三种：

- 普通记录
- 删除记录
- 事务完成记录

### 2.2 `LogRecordPos`

`LogRecordPos` 表示一条记录在磁盘上的物理位置：

- `Fid`
- `Offset`
- `Size`

索引层实际保存的就是这个结构，而不是 value 本体。

### 2.3 `TransactionRecord`

启动恢复时，批量写事务中的记录会暂存为 `TransactionRecord`，直到读到事务完成标记，再整体写入索引。

## 3. 数据文件模型

实现文件：

- [data/data_file.go](/Users/amazing/code/go/kvix/.worktrees/refactor-kvix-medium-layered/data/data_file.go)

`DataFile` 表示一个已经打开的数据文件。它内部维护：

- 文件 ID
- 当前写偏移
- IOManager

`kvix` 运行时通常会同时存在：

- 一个活跃文件
- 若干旧文件

索引里的 `Fid + Offset` 最终都会落到某个 `DataFile` 上。

## 4. 编码与解码流程

### 4.1 记录编码

一条日志记录编码后的布局大致是：

```text
CRC | Type | KeySize | ValueSize | Key | Value
```

其中：

- 头部包含校验和与长度信息
- key/value 长度使用变长整数编码
- 末尾是实际 payload

### 4.2 写入流程

写入时大致会经历：

1. 构造 `LogRecord`
2. 调用 `EncodeLogRecord`
3. 把编码字节追加到数据文件
4. 记录写入前偏移
5. 返回 `LogRecordPos`

### 4.3 读取流程

读取时大致会经历：

1. 从指定偏移读取记录头
2. 解析出 key/value 长度
3. 再读取完整载荷区
4. 还原成 `LogRecord`
5. 校验边界或 CRC

这种设计能让根包只通过位置就找到最新记录，而不需要额外目录结构。

## 5. Hint 与 merge 相关能力

`data` 层本身也承担了 merge 与 Hint 的格式基础。

### 5.1 Hint 文件

Hint 文件本质上还是由 `LogRecord` 组成，只是：

- key 是逻辑 key
- value 是编码后的 `LogRecordPos`

这样启动恢复时就可以直接重建索引，而不必重扫所有旧数据。

### 5.2 merge 完成标记

merge 完成标记文件也建立在同一套数据文件读写能力上，只是保存的内容变成：

- merge 边界对应的活跃文件 ID

## 6. 设计取舍

### 6.1 为什么位置索引不直接存 value

这样可以：

- 减小索引体积
- 让索引结构更轻
- 避免数据重复保存

代价是：

- 读路径需要二次跳转到数据文件

### 6.2 为什么记录采用追加写格式

追加写有几个明显优势：

- 写路径简单
- 容易做崩溃恢复
- 更新和删除都能统一表示成新记录

### 6.3 为什么单独封装 `DataFile`

这样可以把：

- 文件 ID
- 偏移管理
- IO 模式切换
- 读写方法

统一挂在一个对象上，减少根包直接操作文件细节的负担。

## 7. 与其他模块的关系

- 根包通过 `DataFile` 管理活跃文件和旧文件。
- `index` 保存的 value 就是 `LogRecordPos`。
- merge 和 Hint 文件都复用这里定义的记录与文件格式。
- `fio` 为 `DataFile` 提供底层 IO 抽象。

## 8. 使用与测试

业务层一般不会直接操作 `data` 包，但理解它对读懂整个项目非常关键。

测试命令：

```bash
GOCACHE=$(pwd)/.cache/go go test ./data -count=1
```

重点测试覆盖：

- 日志记录编码与解码
- 位置信息编码与解码
- 数据文件读写行为
