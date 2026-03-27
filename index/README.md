# index

## 模块职责

`index` 负责维护 key 到磁盘位置的映射，并为 kvix 提供统一的索引接口与迭代能力。

## 设计思路

根包只依赖 `Indexer` 接口，不绑定具体实现。这样可以在不改写主流程的前提下切换 BTree、ART 或 B+Tree。

## 关键类型与关键文件

- `index.go`：定义 `Indexer`、`IndexIterator` 和工厂方法。
- `btree.go`：基于 `google/btree` 的内存索引实现。
- `art.go`：基于自适应基数树的内存索引实现。
- `bptree.go`：基于 bbolt 的持久化 B+Tree 实现。

## 核心流程

1. 写路径调用 `Put` / `Delete` 更新 key 的最新位置。
2. 读路径调用 `Get` 查到磁盘偏移后再回到数据文件读取 value。
3. 遍历路径通过 `Iterator` 输出按 key 有序的结果。

## 使用方式

索引由根包在 `Open` 阶段创建，业务层通常不直接创建索引实例。

## 与其他模块的关系

- `kvix` 根包负责驱动索引更新与恢复。
- `data` 提供索引条目指向的 `LogRecordPos`。
