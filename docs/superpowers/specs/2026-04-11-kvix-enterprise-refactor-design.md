# kvix 企业级代码规范重构设计

## 1. 背景

kvix 项目此前由 Codex 完成了一轮中度分层重构，将源项目 `bitcask-my` 迁移为 `kvix`，并完成了基本的文件拆分和中文注释。但经过全面审计，发现以下问题：

- **8 个实际 bug**：包括 CRC 校验缺失、资源泄漏、错误吞没等
- **13 处命名拼写错误**：涉及导出 API（`Vaild`、`ErrInvlidCRC`、`ReclaimbleSize`）和功能性常量（`HintFileName` 值缺字母）
- **3 个类型定义问题**：使用类型别名而非定义类型，失去编译时安全性
- **注释质量不足**：部分注释与实际值矛盾，函数内部逻辑注释覆盖不全
- **API 设计缺陷**：`Stat()` 和 `Close()` 遇到错误时 panic 而非返回 error
- **6 处 dot import**：违反 Go 风格指南

本次重构在 Codex 已完成的文件拆分基础上，全面修正上述问题，使代码达到企业开发规范。

## 2. 重构目标

1. 修复所有已发现的 bug
2. 修正所有命名拼写错误和不规范命名
3. 改进 API 签名使其符合 Go 最佳实践
4. 重写全部中文注释，确保详尽准确，包括函数体内部逻辑注释
5. 重写所有模块 README，提供完整使用方法介绍
6. 去除 dot import、类型别名等不规范用法

## 3. 非目标

- 不重写底层存储引擎逻辑
- 不重新设计数据文件编码格式
- 不引入新的包层级或架构范式
- 不添加新功能特性

## 4. 命名与类型修正清单

### 4.1 拼写错误修正

| 当前命名 | 修正为 | 位置 | 影响范围 |
|---------|--------|------|---------|
| `Vaild()` | `Valid()` | `iterator.go`、`index/btree.go` | 导出 API + 所有调用点 |
| `loadMegreFiles` | `loadMergeFiles` | `merge.go` | 内部方法 + db_open.go 调用 |
| `mergeFinisedKey` | `mergeFinishedKey` | `merge.go` | 常量 |
| `bptreeIndexFiuleName` | `bptreeIndexFileName` | `index/bptree.go` | 常量 |
| `maxLogRecordHeardSize` | `maxLogRecordHeaderSize` | `data/data_file.go`、`data/log_record.go` | 常量 + 引用 |
| `ErrInvlidCRC` | `ErrInvalidCRC` | `common/errors.go` | 导出变量 + 引用 |
| `ReclaimbleSize` | `ReclaimableSize` | `db.go` | 导出字段 |
| `HintFileName` value `"hin-index"` | `"hint-index"` | `common/options.go` | 功能性修复 |
| `idextype` | `indexType` | `index/index.go` | 参数名 |
| `filedId` | `fileID` | `merge.go` | 局部变量 |
| `oldfiles` | `oldFiles` | `db.go` 及所有引用 | 结构体字段 |

### 4.2 类型定义修正

将以下类型别名改为定义类型以获得编译时类型安全：

```go
// 修正前
type IndexerType = int8
type LogRecordType = byte
type FileIOType = byte

// 修正后
type IndexerType int8
type LogRecordType byte
type FileIOType byte
```

### 4.3 其他规范化

- `interface{}` 改为 `any`（Go 1.18+ 规范）
- `sync` 参数名改为 `syncWrites`（避免遮蔽标准库包名）
- 移除 `common/options.go` 中注释掉的代码
- panic 消息统一用英文（与 `common/errors.go` 保持一致）

## 5. Bug 修复清单

### 5.1 CRC 校验缺失（data/data_file.go）

**问题**：`ReadLogRecord` 仅在 keySize==0 && valueSize==0 时执行 CRC 校验，大部分记录跳过校验。

**修复**：所有记录读取完成后均执行 CRC 校验，不通过时返回 `ErrInvalidCRC`。

### 5.2 logSeqNo 错误吞没（db_files.go）

**问题**：`ReadLogRecord` 的 error 被下一行 `ParseUint` 的 err 遮蔽。

**修复**：检查 `ReadLogRecord` 返回的 error，失败时提前返回。

### 5.3 mergeDB 资源泄漏（merge.go）

**问题**：`Open(mergeOptions)` 创建的 mergeDB 从未关闭。

**修复**：添加 `defer mergeDB.Close()`。

### 5.4 MMap 文件描述符泄漏（fio/mmap.go）

**问题**：`os.OpenFile` 仅用于确保文件存在，但未关闭返回的 fd。

**修复**：打开后立即关闭 fd。

### 5.5 CopyDir 忽略 Walk 错误（utils/file.go）

**问题**：`filepath.Walk` 的返回值被丢弃。

**修复**：返回 `filepath.Walk` 的 error。

### 5.6 mergeFinFileNames 收集中断（merge.go）

**问题**：找到 `MergeFinishedFileName` 后 break，导致后续文件未被收集。

**修复**：将 `mergeFinished` 标记设为 true 但不 break，继续收集文件名。

### 5.7 NewBPlusTree 返回 nil（index/bptree.go）

**问题**：构造函数出错时返回 nil，无 error 信息，下游引发 nil panic。

**修复**：`NewIndexer` 函数签名改为返回 `(Indexer, error)`，`NewBPlusTree` 同步返回 error。调用链同步调整。

### 5.8 Stat()/Close() panic（db_state.go）

**问题**：公开方法遇到可恢复错误时 panic。

**修复**：
- `Stat()` 签名改为 `Stat() (*Stat, error)`
- `Close()` 签名改为 `Close() error`，文件锁释放失败时返回 error 而非 panic

## 6. 代码规范化

### 6.1 去除 dot import

所有非测试文件中的 `. "kvix/common"` 改为普通 import `"kvix/common"`，代码中使用 `common.Options` ���完整引用。

涉及文件：`db_open.go`、`db_read.go`、`db_write.go`、`db_files.go`、`data/data_file.go`、`index/index.go`

### 6.2 统一错误比较

所有 `err == someError` 改为 `errors.Is(err, someError)`。

### 6.3 矛盾注释修正

```go
// 修正前（common/options.go）
SyncWrites: false, // 默认启用同步写入  ← 矛盾
MMapAtStartup: true, // 默认不使用内存映射文件加载数据  ← 矛盾

// 修正后
SyncWrites: false, // 默认不启用同步写入，由调用方根据场景决定
MMapAtStartup: true, // 默认在启动阶段使用 mmap 加速数据文件加载
```

## 7. 注释规范

### 7.1 导出符号注释标准

所有导出的类型、方法、常量、变量必须有中文 godoc 注释，格式：

```go
// Open 打开或创建一个 kvix 数据库实例，并在返回前恢复可用的运行状态。
//
// 启动流程包括：校验配置、创建数据目录、获取目录文件锁、加载 merge 遗留文件、
// 加载数据文件、重建内存索引。如果使用 B+Tree 索引，则跳过内存索引重建。
//
// 参数:
//   - options: 数据库启动配置，包含目录路径、索引类型、同步策略等。
//
// 返回:
//   - *DB: 初始化完成后可直接读写的数据库句柄。
//   - error: 目录不可访问、文件锁冲突或索引恢复失败时返回错误。
func Open(options common.Options) (*DB, error) {
```

### 7.2 函数体内部注释标准

所有非平凡函数（超过 10 行逻辑）内部按步骤编号注释：

```go
func (db *DB) appendLogRecord(record *data.LogRecord) (*data.LogRecordPos, error) {
    // 1. 判断活跃文件是否存在，首次写入时需要初始化。
    if db.activeFile == nil {
        if err := db.setActiveDataFile(); err != nil {
            return nil, err
        }
    }

    // 2. 将逻辑记录编码为字节序列。
    encodedRecord, size := data.EncodeLogRecord(record)

    // 3. 检查当前活跃文件剩余空间，空间不足时滚动到新文件。
    if db.activeFile.WriteOff+size > db.options.DataFileSize {
        // 3.1 先将当前活跃文件刷盘，确保数据落地。
        // 3.2 将当前文件转为旧文件。
        // 3.3 创建新的活跃文件。
    }

    // 4. 追加写入活跃文件。

    // 5. 根据配置决定是否刷盘。

    // 6. 返回写入位置信息。
}
```

### 7.3 接口注释标准

接口方法需要详细说明语义和约束：

```go
// Indexer 定义内存索引或持久化索引的统一操作接口。
// 所有索引实现必须是并发安全的。
type Indexer interface {
    // Put 向索引中写入一条 key 到数据位置的映射。
    // 如果 key 已存在，返回被覆盖的旧位置信息；否则返回 nil。
    Put(key []byte, pos *data.LogRecordPos) *data.LogRecordPos

    // Get 根据 key 查询数据在磁盘上的位置。
    // 如果 key 不存在，返回 nil。
    Get(key []byte) *data.LogRecordPos

    // Delete 从索引中移除指定 key 的映射。
    // 如果 key 存在且成功删除，返回被移除的位置信息和 true；
    // 如果 key 不存在，返回 nil 和 false。
    Delete(key []byte) (*data.LogRecordPos, bool)
    ...
}
```

## 8. 模块 README 规范

每个模块 README 统一使用以下结构：

```markdown
# 模块名

## 模块职责
简述模块在系统中的定位和负责的能力边界。

## 设计思路
解释关键设计决策和为什么这样设计。

## 关键类型与文件
列出核���类型和文件，说明各自职责。

## 核心流程
用文字或伪代码描述关键流程的步骤。

## 使用示例
提供可直接运行的 Go 代码示例。

## 与其他模块的关系
说明依赖关系和被依赖关系。
```

涉及模块：root（根 README）、common、data、fio、index、redis、http、utils、examples、benchmark

所有路径使用相对路径，不使用绝对路径。

## 9. 实施策略

由于各模块之间存在依赖关系，实施顺序如下：

1. **基础层先行**：先修正 `common`（错误定义、类型定义、配置）→ `fio` → `data`
2. **索引层**：修正 `index`（依赖 common、data）
3. **核心层**：修正根包 `db_*.go`、`batch.go`、`iterator.go`、`merge.go`（依赖上述所有）
4. **上层**：修正 `redis`（依赖核心层）→ `http`（依赖 redis + 核心层）
5. **辅助**：`utils`、`examples`、`benchmark`
6. **文档**：所有模块 README + 根 README

其中独立模块可并行处理。

## 10. 验证策略

每完成一组修改后运行对应测试：

```bash
GOCACHE=$(pwd)/.cache/go go test ./... -count=1
```

最终验证：
- 全部测试通过
- `go vet ./...` 无警告
- 代码中不残留 `Vaild`、`Megre`、`Fiule`、`Heard`、`Invlid`、`Reclaimble` 等拼写错误
- 所有导出符号有 godoc 注释
- 所有模块目录有 README.md
