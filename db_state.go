package kvix

import (
	"kvix/data"
	"kvix/utils"
	"strconv"
)

// Close 关闭数据库实例并释放所有相关资源。
//
// 关闭流程：持久化当前事务序列号 → 关闭活跃文件和旧文件 → 关闭索引 → 释放目录锁。
// 一旦成功返回，当前 DB 实例不应再继续使用。
//
// 返回:
//   - error: 关闭流程中任何步骤失败时返回错误。
func (db *DB) Close() error {
	// 1. 释放目录级文件锁，允许其他进程打开同一目录。
	defer func() {
		_ = db.fileLock.Unlock()
	}()

	if db.activeFile == nil {
		// 空库或尚未真正打开数据文件时，直接结束关闭流程即可。
		return nil
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	defer db.index.Close()

	// 2. 把当前事务序列号写到独立文件里，供下次启动恢复继续递增。
	seqNoFile, err := data.OpenSeqNoFile(db.options.DirPath)
	if err != nil {
		return err
	}
	record := &data.LogRecord{
		Key:   []byte(seqNoKey),
		Value: []byte(strconv.FormatUint(db.seqNo, 10)),
		Type:  data.LogRecordNormal,
	}
	encodedRecord, _ := data.EncodeLogRecord(record)
	if err := seqNoFile.Write(encodedRecord); err != nil {
		return err
	}
	if err := seqNoFile.Sync(); err != nil {
		return err
	}

	// 3. 关闭活跃文件和所有旧文件，释放底层文件句柄。
	if err := db.activeFile.Close(); err != nil {
		return err
	}
	for _, file := range db.oldFiles {
		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
}

// Sync 把当前活跃文件的内存缓冲刷新到磁盘。
//
// 如果当前没有活跃文件，则直接返回 nil。
//
// 返回:
//   - error: 同步失败时返回错误。
func (db *DB) Sync() error {
	if db.activeFile == nil {
		// 没有活跃文件说明当前没有可刷盘的数据。
		return nil
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	return db.activeFile.Sync()
}

// Stat 返回数据库当前的运行统计快照。
//
// 统计信息包括索引项数量、数据文件数量、可回收空间和目录磁盘大小。
// 返回值是调用时刻的快照，不会持续跟随后续写入自动更新。
//
// 返回:
//   - *Stat: 运行统计快照。
//   - error: 获取磁盘大小失败时返回错误。
func (db *DB) Stat() (*Stat, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	// 1. 统计数据文件数量，包括活跃文件和所有旧文件。
	var dataFiles = uint(len(db.oldFiles))
	if db.activeFile != nil {
		dataFiles++
	}

	// 2. 获取数据目录的磁盘占用大小。
	diskSize, err := utils.GetDirSize(db.options.DirPath)
	if err != nil {
		return nil, err
	}

	return &Stat{
		KeyNum:          uint(db.index.Size()),
		DataFileNum:     dataFiles,
		ReclaimableSize: db.reclaimSize,
		DiskSize:        diskSize,
	}, nil
}

// BackUp 把当前数据库目录复制到指定备份目录。
//
// 复制时会显式跳过进程锁文件，避免把运行态锁带入备份。
//
// 参数:
//   - dir: 备份输出目录。
//
// 返回:
//   - error: 目录复制失败时返回错误。
func (db *DB) BackUp(dir string) error {
	db.mu.RLock()
	defer db.mu.RUnlock()
	// flock 文件只对"当前运行中的进程互斥"有意义，备份里不应复制它。
	return utils.CopyDir(db.options.DirPath, dir, []string{fileLockName})
}
