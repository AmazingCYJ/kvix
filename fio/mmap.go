package fio

import (
	"kvix/common"
	"os"

	"golang.org/x/exp/mmap"
)

// MMap 是基于内存映射文件的 IO 管理器实现，提供高性能的读操作。
// 它仅支持只读访问，写入操作会返回 ErrMMapWriteNotSupported。
type MMap struct {
	readerAt *mmap.ReaderAt // 内存映射文件的读取器
}

// NewMMapIOManager 创建一个基于 mmap 的只读 IO 管理器。
// 1. 确保目标文件存在（不存在则创建空文件），然后立即关闭文件描述符。
// 2. 通过 mmap.Open 将文件映射到内存，后续读取直接走内存页。
func NewMMapIOManager(filePath string) (*MMap, error) {
	// 1. 确保文件存在；若文件不存在则创建，随后关闭文件描述符以避免泄漏。
	f, err := os.OpenFile(filePath, os.O_CREATE, DataFilePerm)
	if err != nil {
		return nil, err
	}
	f.Close()

	// 2. 将文件映射到内存，获取只读的 ReaderAt。
	readerAt, err := mmap.Open(filePath)
	if err != nil {
		return nil, err
	}

	return &MMap{readerAt: readerAt}, nil
}

// ReadAt 从内存映射的指定偏移量读取数据到 p 中。
func (m *MMap) ReadAt(p []byte, off int64) (n int, err error) {
	return m.readerAt.ReadAt(p, off)
}

// Write mmap 模式下不支持写入，调用时直接返回 ErrMMapWriteNotSupported。
func (m *MMap) Write(_ []byte) (n int, err error) {
	return 0, common.ErrMMapWriteNotSupported
}

// Sync mmap 只读模式下无需刷盘，直接返回 nil。
func (m *MMap) Sync() error {
	return nil
}

// Close 关闭 mmap 读取器并释放映射的内存资源。
// 若 readerAt 已为 nil（重复关闭），则安全返回 nil。
func (m *MMap) Close() error {
	if m.readerAt == nil {
		return nil
	}
	// 1. 关闭底层映射，释放内存页。
	err := m.readerAt.Close()
	// 2. 置空引用，防止重复关闭。
	m.readerAt = nil
	return err
}

// Size 返回映射文件的大小（单位：字节）。
func (m *MMap) Size() (int64, error) {
	return int64(m.readerAt.Len()), nil
}
