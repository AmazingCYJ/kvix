package fio

import "fmt"

const (
	DataFilePerm = 0644
)

type FileIOType = byte

const (
	// StandardFIO 是基于标准文件读写的 IO 管理器实现，适用于一般场景。
	StandardFIO FileIOType = iota
	// MemoryMap 是基于内存映射文件的 IO 管理器实现，提供高性能的读操作，适用于只读或读多写少的场景。
	MemoryMap
)

// IOManager 定义了文件读写操作的接口，提供了基本的读写、同步和关闭功能。
type IOManager interface {
	//从文件指定位置读,返回读取的字节数和错误信息
	ReadAt(p []byte, off int64) (n int, err error)
	//向文件指定位置写,返回写入的字节数和错误信息
	Write(p []byte) (n int, err error)
	//将内存中的数据刷新到磁盘,确保数据持久化
	Sync() error
	//关闭文件,释放资源
	Close() error
	// Size 返回文件的当前大小
	Size() (int64, error)
}

// NewIOManager 创建一个新的 IOManager 实例，负责管理文件的读写操作。
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
