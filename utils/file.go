package utils

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// GetDirSize 计算指定目录下所有文件的总大小。
func GetDirSize(dirPath string) (int64, error) {
	var size int64
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// GetDiskFreeSpace 获取指定路径所在磁盘的剩余空间大小。
func GetDiskFreeSpace() (uint64, error) {
	wd, err := syscall.Getwd()
	if err != nil {
		return 0, err
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(wd, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

// CopyDir 递归复制目录，并按名称模式排除不需要继承的文件或子目录。
func CopyDir(srcDir, destDir string, exclude []string) error {
	// 1. 先准备目标目录，确保后续 Walk 过程中可以直接落盘文件。
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
			return err
		}
	}
	// 2. 遍历源目录，按排除规则跳过不需要复制的节点。
	filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		fileName := strings.Replace(path, srcDir, "", 1)
		if fileName == "" || fileName == string(os.PathSeparator) {
			return nil
		}
		// 2.1 目录命中排除规则时直接跳过整棵子树，避免复制无关文件。
		for _, e := range exclude {
			matched, err := filepath.Match(e, info.Name())
			if err != nil {
				return err
			}
			if matched {
				return filepath.SkipDir
			}
		}
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(destDir, fileName), info.Mode())
		}
		// 2.2 普通文件按原路径关系复制，保持目录结构和权限一致。
		data, err := os.ReadFile(filepath.Join(srcDir, fileName))
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(destDir, fileName), data, info.Mode())
	})
	return nil
}
