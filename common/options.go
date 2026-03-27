package common

import "os"

// Options 定义 kvix 在启动和运行阶段使用的核心配置。
// 这些配置会影响数据文件滚动、索引实现、刷盘策略以及 Merge 触发条件。
type Options struct {
	DirPath            string      // 数据文件存储路径
	DataFileSize       int64       // 每个数据文件的最大大小，单位字节
	IndexType          IndexerType // 索引类型
	SyncWrites         bool        // 是否在每次写入后立即将数据刷新到磁盘
	BytesPerSync       uint        // 每次同步写入的字节数，默认为 4KB
	MMapAtStartup      bool        // 是否在启动时使用内存映射文件加载数据
	DataFileMergeRatio float32     // 数据文件合并的大小比例阈值，默认为 0.5（即当废弃数据占比超过 50% 时触发合并）
}

// IteratorOptions 定义通用迭代器的扫描方式。
// 它只负责范围筛选和顺序控制，不改变底层索引遍历语义。
type IteratorOptions struct {
	Prefix  []byte // 迭代器只返回以该前缀开头的 key
	Reverse bool   // 是否反向迭代
}

// WriteBatchOptions 定义批量写入的事务边界和刷盘策略。
// WriteBatch 先在内存中暂存操作，再统一写入数据文件并更新索引。
type WriteBatchOptions struct {
	MaxBatchSize uint // 批量写入的最大操作数
	SyncWrite    bool // 是否在批量写入后立即将数据刷新到磁盘
}

// IndexerType 表示内存索引或持久化索引的实现类型。
type IndexerType = int8

const (
	// BTreeIndex 基于 BTree 实现的索引
	BTreeIndex IndexerType = iota + 1
	// 未来可以添加其他索引类型，如 HashIndex、LSMTreeIndex 等
	//ARTreeIndex
	ARTreeIndex
	// BPlusTreeIndex 基于 B+ Tree 实现的索引
	BPlusTreeIndex
)

const (
	DataFileSuffix        = ".data"          // 数据文件后缀
	HintFileName          = "hin-index"      // Hint 文件名
	MergeFinishedFileName = "merge-finished" // Merge 完成标识文件名
	SeqNoFileName         = "seq-no"         // 序列号文件名
)

// DefaultOptions 给出 kvix 的默认数据库配置。
var DefaultOptions = Options{
	DirPath:      os.TempDir(),      // 默认使用系统临时目录
	DataFileSize: 256 * 1024 * 1024, // 默认数据文件大小为 256MB
	// IndexType:    BTreeIndex,        // 默认使用 BTree 索引
	IndexType:          BPlusTreeIndex, // 默认使用 B+ Tree 索引
	SyncWrites:         false,          // 默认启用同步写入
	BytesPerSync:       4 * 1024,       // 默认每次同步写入 4KB 数据
	MMapAtStartup:      true,           // 默认不使用内存映射文件加载数据
	DataFileMergeRatio: 0.5,            // 默认合并阈值为 50%
}

// DefaultIteratorOptions 给出默认的正向全量遍历配置。
var DefaultIteratorOptions = IteratorOptions{
	Prefix:  nil,   // 默认不使用前缀过滤
	Reverse: false, // 默认正向迭代
}

// DefaultWriteBatchOptions 给出默认的批量写入配置。
var DefaultWriteBatchOptions = WriteBatchOptions{
	MaxBatchSize: 100,   // 默认批量写入的最大操作数为 100
	SyncWrite:    false, // 默认启用同步写入
}
