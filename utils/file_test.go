package utils

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestGetDirSize(t *testing.T) {
	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		size, err := GetDirSize(dir)
		if err != nil {
			t.Fatalf("GetDirSize() error = %v", err)
		}
		if size != 0 {
			t.Fatalf("GetDirSize() size = %d, want 0", size)
		}
	})

	t.Run("nested files", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "sub")
		if err := os.MkdirAll(sub, os.ModePerm); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}

		files := []struct {
			path string
			size int
		}{
			{path: filepath.Join(dir, "a.txt"), size: 3},
			{path: filepath.Join(sub, "b.txt"), size: 5},
			{path: filepath.Join(sub, "c.bin"), size: 7},
		}

		var want int64
		for _, f := range files {
			content := make([]byte, f.size)
			if err := os.WriteFile(f.path, content, 0o644); err != nil {
				t.Fatalf("WriteFile(%s) error = %v", f.path, err)
			}
			want += int64(f.size)
		}

		size, err := GetDirSize(dir)
		if err != nil {
			t.Fatalf("GetDirSize() error = %v", err)
		}
		if size != want {
			t.Fatalf("GetDirSize() size = %d, want %d", size, want)
		}
	})

	t.Run("directory not found", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "not-exist")
		_, err := GetDirSize(missing)
		if err == nil {
			t.Fatalf("GetDirSize() error = nil, want non-nil")
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("GetDirSize() error = %v, want ErrNotExist", err)
		}
	})
}

func TestGetDiskFreeSpace(t *testing.T) {
	dir := t.TempDir()
	got, err := GetDiskFreeSpace(dir)
	if err != nil {
		t.Fatalf("GetDiskFreeSpace() error = %v", err)
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		t.Fatalf("syscall.Statfs() error = %v", err)
	}

	want := stat.Bavail * uint64(stat.Bsize)
	if got != want {
		t.Fatalf("GetDiskFreeSpace() = %d, want %d", got, want)
	}
}
