package fio

import (
	"bitcask-my/common"
	"os"

	"golang.org/x/exp/mmap"
)

// MMap 是基于内存映射文件的 IO 管理器实现，提供高性能的读操作。
type MMap struct {
	readerAt *mmap.ReaderAt // 内存映射文件的读取器
}

// NewMMapIOManager 创建一个 mmap 读取器。
func NewMMapIOManager(filePath string) (*MMap, error) {
	_, err := os.OpenFile(filePath, os.O_CREATE, DataFilePerm)
	if err != nil {
		return nil, err
	}
	readerAt, err := mmap.Open(filePath)
	if err != nil {
		return nil, err
	}

	return &MMap{readerAt: readerAt}, nil
}

// ReadAt 从指定偏移量读取数据。
func (m *MMap) ReadAt(p []byte, off int64) (n int, err error) {
	return m.readerAt.ReadAt(p, off)
}

// Write mmap 模式下不支持写入。
func (m *MMap) Write(_ []byte) (n int, err error) {
	return 0, common.ErrMMapWriteNotSupported
}

// Sync mmap 只读模式下无需刷盘。
func (m *MMap) Sync() error {
	return nil
}

// Close 关闭 mmap 读取器。
func (m *MMap) Close() error {
	if m.readerAt == nil {
		return nil
	}
	err := m.readerAt.Close()
	m.readerAt = nil
	return err
}

// Size 返回映射文件大小。
func (m *MMap) Size() (int64, error) {
	return int64(m.readerAt.Len()), nil
}
