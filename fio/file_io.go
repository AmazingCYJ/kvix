package fio

import "os"

// FileIO 是基于标准文件句柄的 IOManager 实现。
// 运行期正常读写路径默认使用它，因为它既支持随机读，也支持顺序追加写和刷盘。
type FileIO struct {
	fd *os.File
}

// NewFileIO 打开或创建一个标准文件 IO 管理器。
func NewFileIO(filePath string) (*FileIO, error) {
	fd, err := os.OpenFile(
		filePath,
		os.O_RDWR|os.O_CREATE,
		DataFilePerm,
	)
	if err != nil {
		return nil, err
	}
	return &FileIO{fd: fd}, nil
}

// ReadAt 从指定偏移读取数据，不会改变文件当前读写位置。
func (f *FileIO) ReadAt(p []byte, off int64) (n int, err error) {
	return f.fd.ReadAt(p, off)
}

// Write 把字节写入文件当前偏移，常用于顺序追加写。
func (f *FileIO) Write(p []byte) (n int, err error) {
	return f.fd.Write(p)
}

// Sync 把操作系统缓冲区中的数据显式刷新到磁盘。
func (f *FileIO) Sync() error {
	return f.fd.Sync()
}

// Close 关闭底层文件句柄。
func (f *FileIO) Close() error {
	return f.fd.Close()
}

// Size 返回当前文件大小，供数据文件读取边界判断和写偏移恢复使用。
func (f *FileIO) Size() (int64, error) {
	stat, err := f.fd.Stat()
	if err != nil {
		return 0, err
	}
	return stat.Size(), nil
}
