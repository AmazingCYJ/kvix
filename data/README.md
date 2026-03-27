# data

## 模块职责

`data` 负责定义 kvix 的日志记录格式、数据文件读写能力以及内存索引落点所需的位置编码。

## 设计思路

该模块只关心“记录如何编码”和“文件如何读写”，不负责索引维护与业务语义。这样根包可以专注事务边界、索引更新和恢复流程。

## 关键类型与关键文件

- `log_record.go`：定义 `LogRecord`、`LogRecordPos` 以及编码解码逻辑。
- `data_file.go`：定义 `DataFile`，负责读写、刷盘和 hint 文件相关能力。

## 核心流程

1. 写路径将 `LogRecord` 编码为追加写字节流。
2. 读路径按偏移读取记录头，再读取载荷区并还原为 `LogRecord`。
3. merge 或启动恢复阶段通过 `LogRecordPos` 重建内存索引。

## 使用方式

根包通常通过 `OpenDataFile` 打开数据文件，并调用 `ReadLogRecord` / `Write` 完成底层访问；业务代码不直接依赖该模块。

## 与其他模块的关系

- `kvix` 根包使用 `data` 维护活跃文件、旧文件和索引位置。
- `index` 中保存的位置信息来自 `LogRecordPos`。
