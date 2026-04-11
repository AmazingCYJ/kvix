package utils

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// GetDirSize 计算指定目录下所有文件（含子目录中的文件）的总大小。
// 返回值单位为字节。若目录不存在或遍历过程中出错，返回已累计的大小和错误。
func GetDirSize(dirPath string) (int64, error) {
	var size int64
	// 1. 递归遍历目录树，累加每个普通文件的大小。
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// 2. 只统计普通文件，跳过目录本身。
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// GetDiskFreeSpace 获取指定目录所在磁盘分区的剩余可用空间。
// 返回值单位为字节。通过 syscall.Statfs 获取文件系统统计信息。
func GetDiskFreeSpace(dirPath string) (uint64, error) {
	// 1. 对目标路径执行 statfs 系统调用，获取文件系统块信息。
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dirPath, &stat); err != nil {
		return 0, err
	}
	// 2. 可用块数 * 每块字节数 = 剩余可用空间。
	return stat.Bavail * uint64(stat.Bsize), nil
}

// CopyDir 递归复制源目录到目标目录，并按名称模式排除不需要复制的文件或子目录。
// exclude 中的每个元素使用 filepath.Match 语法进行匹配。
func CopyDir(srcDir, destDir string, exclude []string) error {
	// 1. 确保目标目录存在，若不存在则递归创建。
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
			return err
		}
	}
	// 2. 遍历源目录，按排除规则跳过不需要复制的节点。
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// 2.1 计算相对路径，跳过源目录根节点本身。
		fileName := strings.Replace(path, srcDir, "", 1)
		if fileName == "" || fileName == string(os.PathSeparator) {
			return nil
		}
		// 2.2 检查排除规则；目录命中时跳过整棵子树，避免复制无关文件。
		for _, e := range exclude {
			matched, err := filepath.Match(e, info.Name())
			if err != nil {
				return err
			}
			if matched {
				return filepath.SkipDir
			}
		}
		// 2.3 遇到子目录时在目标路径下创建对应目录。
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(destDir, fileName), info.Mode())
		}
		// 2.4 普通文件按原路径关系复制，保持目录结构和权限一致。
		data, err := os.ReadFile(filepath.Join(srcDir, fileName))
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(destDir, fileName), data, info.Mode())
	})
}
