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

// CopyDir 复制目录,srcDir 是源目录路径，destDir 是目标目录路径，excluded 是一个字符串切片，包含不需要复制的文件或目录的名称。
func CopyDir(srcDir, destDir string, exclude []string) error {
	//1.检查源目录是否存在
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
			return err
		}
	}
	//2.遍历源目录下的所有文件和子目录
	filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		fileName := strings.Replace(path, srcDir, "", 1)
		if fileName == "" || fileName == string(os.PathSeparator) {
			return nil
		}
		// 检查是否在排除列表中
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
		data, err := os.ReadFile(filepath.Join(srcDir, fileName))
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(destDir, fileName), data, info.Mode())
	})
	return nil
}
