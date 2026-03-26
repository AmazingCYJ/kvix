package fio

import (
	"bitcask-my/common"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func createMMapTestFile(t *testing.T, content []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mmap-test.data")
	if err := os.WriteFile(path, content, DataFilePerm); err != nil {
		t.Fatalf("write test file error = %v", err)
	}
	return path
}

func TestNewMMapIOManagerAndReadAt(t *testing.T) {
	content := []byte("hello mmap world")
	path := createMMapTestFile(t, content)

	m, err := NewMMapIOManager(path)
	if err != nil {
		t.Fatalf("NewMMapIOManager() error = %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })

	sz, err := m.Size()
	if err != nil {
		t.Fatalf("Size() error = %v", err)
	}
	if sz != int64(len(content)) {
		t.Fatalf("Size() = %d, want %d", sz, len(content))
	}

	buf := make([]byte, 4)
	n, err := m.ReadAt(buf, 6)
	if err != nil {
		t.Fatalf("ReadAt(off=6) error = %v", err)
	}
	if n != 4 {
		t.Fatalf("ReadAt(off=6) n = %d, want 4", n)
	}
	if string(buf) != "mmap" {
		t.Fatalf("ReadAt(off=6) = %q, want %q", string(buf), "mmap")
	}

	partial := make([]byte, 4)
	n, err = m.ReadAt(partial, int64(len(content)-2))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt(partial) error = %v, want io.EOF", err)
	}
	if n != 2 {
		t.Fatalf("ReadAt(partial) n = %d, want 2", n)
	}
	if string(partial[:2]) != "ld" {
		t.Fatalf("ReadAt(partial) bytes = %q, want %q", string(partial[:2]), "ld")
	}
}

func TestMMapWriteSyncAndClose(t *testing.T) {
	path := createMMapTestFile(t, []byte("abc"))

	m, err := NewMMapIOManager(path)
	if err != nil {
		t.Fatalf("NewMMapIOManager() error = %v", err)
	}

	n, err := m.Write([]byte("x"))
	if n != 0 {
		t.Fatalf("Write() n = %d, want 0", n)
	}
	if !errors.Is(err, common.ErrMMapWriteNotSupported) {
		t.Fatalf("Write() error = %v, want %v", err, common.ErrMMapWriteNotSupported)
	}

	if err := m.Sync(); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if err := m.Close(); err != nil {
		t.Fatalf("Close() first error = %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close() second error = %v", err)
	}
}

func TestNewMMapIOManagerCreateFileIfNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-exist.data")
	m, err := NewMMapIOManager(path)
	if err != nil {
		t.Fatalf("NewMMapIOManager(not-exist) error = %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })

	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("stat created file error = %v", statErr)
	}

	sz, err := m.Size()
	if err != nil {
		t.Fatalf("Size() error = %v", err)
	}
	if sz != 0 {
		t.Fatalf("Size() = %d, want 0 for new file", sz)
	}
}

func createMMapPerfFile(t testing.TB, size int64) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mmap-perf.data")

	fd, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, DataFilePerm)
	if err != nil {
		t.Fatalf("open perf file error = %v", err)
	}
	defer func() { _ = fd.Close() }()

	buf := make([]byte, 1024*1024)
	for i := range buf {
		buf[i] = byte(i % 251)
	}

	remain := size
	for remain > 0 {
		toWrite := int64(len(buf))
		if remain < toWrite {
			toWrite = remain
		}
		n, wErr := fd.Write(buf[:toWrite])
		if wErr != nil {
			t.Fatalf("write perf file error = %v", wErr)
		}
		if int64(n) != toWrite {
			t.Fatalf("write perf file n = %d, want %d", n, toWrite)
		}
		remain -= toWrite
	}

	if err := fd.Sync(); err != nil {
		t.Fatalf("sync perf file error = %v", err)
	}

	return path
}

func measureReadAtCost(t testing.TB, mgr IOManager, fileSize int64, readSize, readCount int) time.Duration {
	t.Helper()
	buf := make([]byte, readSize)
	maxOff := int(fileSize) - readSize

	start := time.Now()
	for i := 0; i < readCount; i++ {
		off := int64((i * 104729) % maxOff)
		n, err := mgr.ReadAt(buf, off)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("ReadAt error = %v", err)
		}
		if n != readSize {
			t.Fatalf("ReadAt n = %d, want %d", n, readSize)
		}
	}
	return time.Since(start)
}

func TestMMapReadPerformance(t *testing.T) {
	if os.Getenv("BITCASK_RUN_PERF_TEST") != "1" {
		t.Skip("set BITCASK_RUN_PERF_TEST=1 to run perf comparison")
	}

	const (
		fileSize  = 64 * 1024 * 1024
		readSize  = 4 * 1024
		readCount = 200000
	)

	path := createMMapPerfFile(t, fileSize)

	stdMgr, err := NewIOManager(path, StandardFIO)
	if err != nil {
		t.Fatalf("NewIOManager(StandardFIO) error = %v", err)
	}
	defer func() { _ = stdMgr.Close() }()

	mmapMgr, err := NewIOManager(path, MemoryMap)
	if err != nil {
		t.Fatalf("NewIOManager(MemoryMap) error = %v", err)
	}
	defer func() { _ = mmapMgr.Close() }()

	stdCost := measureReadAtCost(t, stdMgr, fileSize, readSize, readCount)
	mmapCost := measureReadAtCost(t, mmapMgr, fileSize, readSize, readCount)

	if mmapCost == 0 {
		t.Fatalf("mmap duration = 0, invalid measurement")
	}

	speedup := float64(stdCost) / float64(mmapCost)
	t.Logf("ReadAt perf: fileIO=%v mmap=%v speedup=%.2fx", stdCost, mmapCost, speedup)

	// 保持断言宽松，避免不同机器和负载导致偶发误报。
	if mmapCost > stdCost*2 {
		t.Fatalf("mmap too slow: fileIO=%v mmap=%v", stdCost, mmapCost)
	}
}

func BenchmarkReadAtFileIOVsMMap(b *testing.B) {
	const (
		fileSize = 64 * 1024 * 1024
		readSize = 4 * 1024
	)

	path := createMMapPerfFile(b, fileSize)

	stdMgr, err := NewIOManager(path, StandardFIO)
	if err != nil {
		b.Fatalf("NewIOManager(StandardFIO) error = %v", err)
	}
	defer func() { _ = stdMgr.Close() }()

	mmapMgr, err := NewIOManager(path, MemoryMap)
	if err != nil {
		b.Fatalf("NewIOManager(MemoryMap) error = %v", err)
	}
	defer func() { _ = mmapMgr.Close() }()

	b.Run("FileIO", func(b *testing.B) {
		buf := make([]byte, readSize)
		maxOff := fileSize - readSize
		b.SetBytes(readSize)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			off := int64((i * 104729) % maxOff)
			n, err := stdMgr.ReadAt(buf, off)
			if err != nil && !errors.Is(err, io.EOF) {
				b.Fatalf("ReadAt error = %v", err)
			}
			if n != readSize {
				b.Fatalf("ReadAt n = %d, want %d", n, readSize)
			}
		}
	})

	b.Run("MMap", func(b *testing.B) {
		buf := make([]byte, readSize)
		maxOff := fileSize - readSize
		b.SetBytes(readSize)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			off := int64((i * 104729) % maxOff)
			n, err := mmapMgr.ReadAt(buf, off)
			if err != nil && !errors.Is(err, io.EOF) {
				b.Fatalf("ReadAt error = %v", err)
			}
			if n != readSize {
				b.Fatalf("ReadAt n = %d, want %d", n, readSize)
			}
		}
	})
}
