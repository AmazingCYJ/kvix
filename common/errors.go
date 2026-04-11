package common

import "errors"

var (
	// ErrKeyNotFound 在索引中查找指定 key 时未命中任何记录时返回。
	// 常见场景：Get、Delete 操作传入了不存在或已被删除的 key。
	ErrKeyNotFound = errors.New("key not found")

	// ErrIndexUpdateFailed 在向内存索引写入新的位置信息时失败时返回。
	// 可能原因：底层 BTree / ART / B+Tree 写入异常。
	ErrIndexUpdateFailed = errors.New("index update failed")

	// ErrDataFileNotFound 在尝试读取指定 fileID 对应的数据文件时，
	// 发现该文件不存在或已被移除时返回。
	ErrDataFileNotFound = errors.New("data file not found")

	// ErrDataDirectoryCorrupted 在启动阶段扫描数据目录时，
	// 发现目录结构不符合预期（例如存在非法文件或缺少必要文件）时返回。
	ErrDataDirectoryCorrupted = errors.New("data directory is corrupted")

	// ErrInvalidCRC 在读取 LogRecord 后进行 CRC32 校验时，
	// 计算值与记录头中存储的值不一致时返回，表明数据可能已损坏。
	ErrInvalidCRC = errors.New("invalid CRC, data may be corrupted")

	// ErrBatchTooLarge 在 WriteBatch 提交时，
	// 暂存的操作数超过 WriteBatchOptions.MaxBatchSize 限制时返回。
	ErrBatchTooLarge = errors.New("batch size exceeds the maximum limit")

	// ErrMergeIsProgress 在发起 Merge 操作时，
	// 检测到另一个 Merge 流程正在执行时返回，同一时刻只允许一个 Merge。
	ErrMergeIsProgress = errors.New("other file is merging,try again later")

	// ErrDataBaseIsUsing 在打开数据库时，
	// 检测到文件锁已被其他进程持有时返回，防止多进程并发写入。
	ErrDataBaseIsUsing = errors.New("the database is using by other process")

	// ErrMMapWriteNotSupported 在 mmap IO 管理器上调用 Write 方法时返回。
	// mmap 实现仅支持只读操作，写入需切换到标准文件 IO。
	ErrMMapWriteNotSupported = errors.New("mmap does not support write")

	// ErrMergeRatioUnreached 在发起 Merge 操作时，
	// 废弃数据占比尚未达到 DataFileMergeRatio 阈值时返回，避免无意义的重写。
	ErrMergeRatioUnreached = errors.New("merge ratio is unreached")

	// ErrNoEnoughDiskSpace 在发起 Merge 操作前检查磁盘剩余空间时，
	// 发现可用空间不足以容纳重写后的数据文件时返回。
	ErrNoEnoughDiskSpace = errors.New("not enough disk space for merge")
)
