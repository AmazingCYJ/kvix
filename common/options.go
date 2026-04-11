package common

import "os"

// Options 定义 kvix 在启动和运行阶段使用的核心配置。
// 这些配置会影响数据文件滚动、索引实现、刷盘策略以及 Merge 触发条件。
type Options struct {
	// DirPath 数据文件存储路径，所有数据文件、Hint 文件和序列号文件都保存在此目录下。
	DirPath string
	// DataFileSize 每个数据文件的最大大小（单位：字节）。
	// 当活跃文件写满后会自动滚动为只读文件并创建新的活跃文件。
	DataFileSize int64
	// IndexType 内存或持久化索引的实现类型，决定使用 BTree、ART 还是 B+Tree。
	IndexType IndexerType
	// SyncWrites 是否在每次写入后立即将数据刷新到磁盘。
	// 开启后可保证单条写入的持久性，但会显著降低写入吞吐量。
	SyncWrites bool
	// BytesPerSync 累计写入达到该字节数后触发一次 fsync。
	// 仅在 SyncWrites 为 false 时生效，用于在性能和持久性之间取得平衡。
	BytesPerSync uint
	// MMapAtStartup 是否在启动阶段使用内存映射文件加载数据。
	// 开启后可加速启动时的数据文件读取，启动完成后会切换回标准文件 IO。
	MMapAtStartup bool
	// DataFileMergeRatio 废弃数据占比阈值（0.0 ~ 1.0）。
	// 当废弃数据占总数据的比例超过该值时，才允许触发 Merge 操作。
	DataFileMergeRatio float32
}

// IteratorOptions 定义通用迭代器的扫描方式。
// 它只负责范围筛选和顺序控制，不改变底层索引遍历语义。
type IteratorOptions struct {
	// Prefix 迭代器只返回以该前缀开头的 key；为 nil 时不做前缀过滤。
	Prefix []byte
	// Reverse 是否反向迭代。为 true 时从最大 key 向最小 key 遍历。
	Reverse bool
}

// WriteBatchOptions 定义批量写入的事务边界和刷盘策略。
// WriteBatch 先在内存中暂存操作，再统一写入数据文件并更新索引。
type WriteBatchOptions struct {
	// MaxBatchSize 单次批量写入允许的最大操作数。
	// 超过此限制时 Commit 会返回 ErrBatchTooLarge。
	MaxBatchSize uint
	// SyncWrite 是否在批量写入完成后立即将数据刷新到磁盘。
	SyncWrite bool
}

// IndexerType 表示内存索引或持久化索引的实现类型。
// 使用 defined type 而非 type alias，以获得更强的类型安全性。
type IndexerType int8

const (
	// BTreeIndex 基于 Google BTree 实现的纯内存索引，适用于一般读写场景。
	BTreeIndex IndexerType = iota + 1
	// ARTreeIndex 基于自适应基数树（Adaptive Radix Tree）实现的纯内存索引，
	// 适合按字节前缀组织 key 的场景。
	ARTreeIndex
	// BPlusTreeIndex 基于 B+Tree 实现的持久化索引，索引结构单独落盘，
	// 适合需要索引持久化或数据量超出内存容量的场景。
	BPlusTreeIndex
)

const (
	// DataFileSuffix 数据文件的扩展名后缀。
	DataFileSuffix = ".data"
	// HintFileName Hint 索引文件名，Merge 完成后生成，用于加速启动时的索引重建。
	HintFileName = "hint-index"
	// MergeFinishedFileName Merge 完成标识文件名，存在该文件表示上一次 Merge 已成功结束。
	MergeFinishedFileName = "merge-finished"
	// SeqNoFileName 全局递增序列号文件名，用于 WriteBatch 的事务编号持久化。
	SeqNoFileName = "seq-no"
)

// DefaultOptions 给出 kvix 的默认数据库配置。
var DefaultOptions = Options{
	DirPath:            os.TempDir(),      // 默认使用系统临时目录
	DataFileSize:       256 * 1024 * 1024, // 默认数据文件大小为 256MB
	IndexType:          BPlusTreeIndex,    // 默认使用 B+Tree 索引
	SyncWrites:         false,             // 默认不启用同步写入
	BytesPerSync:       4 * 1024,          // 默认每次同步写入 4KB 数据
	MMapAtStartup:      true,              // 默认在启动阶段使用 mmap 加速数据文件加载
	DataFileMergeRatio: 0.5,               // 默认合并阈值为 50%
}

// DefaultIteratorOptions 给出默认的正向全量遍历配置。
var DefaultIteratorOptions = IteratorOptions{
	Prefix:  nil,   // 默认不使用前缀过滤
	Reverse: false, // 默认正向迭代
}

// DefaultWriteBatchOptions 给出默认的批量写入配置。
var DefaultWriteBatchOptions = WriteBatchOptions{
	MaxBatchSize: 100,   // 默认批量写入的最大操作数为 100
	SyncWrite:    false, // 默认不启用同步写入
}
