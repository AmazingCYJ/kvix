package fio

import "fmt"

const (
	// DataFilePerm 是 kvix 创建数据文件时使用的默认权限位。
	DataFilePerm = 0644
)

// FileIOType 用来标识底层采用哪一种 IO 实现。
type FileIOType = byte

const (
	// StandardFIO 是基于标准文件读写的 IO 管理器实现，适用于一般场景。
	StandardFIO FileIOType = iota
	// MemoryMap 是基于内存映射文件的 IO 管理器实现，提供高性能的读操作，适用于只读或读多写少的场景。
	MemoryMap
)

// IOManager 抽象了数据文件访问所需的最小能力集合。
// 上层只依赖这个接口，因此不需要关心当前底层究竟是 os.File 还是 mmap。
type IOManager interface {
	// ReadAt 从指定偏移读取数据，不改变当前文件读写位置。
	ReadAt(p []byte, off int64) (n int, err error)
	// Write 从当前写入位置写入数据，通常用于顺序追加写。
	Write(p []byte) (n int, err error)
	// Sync 强制把缓冲区数据刷新到磁盘，确保持久化。
	Sync() error
	// Close 关闭底层资源。
	Close() error
	// Size 返回文件当前大小，供恢复和边界判断使用。
	Size() (int64, error)
}

// NewIOManager 根据配置选择具体的 IO 实现。
// 1. 标准文件 IO 适合常规读写路径。
// 2. mmap IO 适合启动阶段或读多写少的只读场景。
// 3. 若类型不受支持，则立即返回错误，避免上层带着错误配置继续运行。
func NewIOManager(filePath string, ioType FileIOType) (IOManager, error) {
	switch ioType {
	case StandardFIO:
		return NewFileIO(filePath)
	case MemoryMap:
		return NewMMapIOManager(filePath)
	default:
		return nil, fmt.Errorf("unsupported IO type: %d", ioType)
	}
}
