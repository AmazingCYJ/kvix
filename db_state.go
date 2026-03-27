package kvix

import (
	"fmt"
	"kvix/data"
	"kvix/utils"
	"strconv"
)

// Close 关闭数据库实例并释放相关资源。
// 该方法不接收额外参数，会在关闭前持久化当前事务序列号、关闭活跃文件和旧文件，并释放目录锁。
// 返回值表示关闭流程是否成功；一旦成功，当前 DB 实例不应再继续使用。
func (db *DB) Close() error {
	defer func() {
		if err := db.fileLock.Unlock(); err != nil {
			panic("failed to release file lock")
		}
	}()
	if db.activeFile == nil {
		// 空库或尚未真正打开数据文件时，直接结束关闭流程即可。
		return nil
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	defer db.index.Close()

	seqNoFile, err := data.OpenSeqNoFile(db.options.DirPath)
	if err != nil {
		return err
	}
	// 1. 把当前事务序列号写到独立文件里，供下次启动恢复继续递增。
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

	// 2. 再关闭活跃文件和所有旧文件，释放底层文件句柄。
	if err := db.activeFile.Close(); err != nil {
		return err
	}
	for _, file := range db.oldfiles {
		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
}

// Sync 把当前活跃文件的内存缓冲刷新到磁盘。
// 该方法不接收额外参数；如果当前没有活跃文件，则直接返回 nil。
// 返回值表示同步是否成功；成功后可确保活跃文件中已有写入已经持久化。
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
// 该方法不接收额外参数，会在读锁下统计索引项数量、数据文件数量、可回收空间和目录磁盘大小。
// 返回值中的统计信息是调用时刻的快照，不会持续跟随后续写入自动更新。
func (db *DB) Stat() *Stat {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var dataFiles = uint(len(db.oldfiles))
	if db.activeFile != nil {
		// 统计时别忘了把当前活跃文件也算进去。
		dataFiles++
	}
	diskSize, err := utils.GetDirSize(db.options.DirPath)
	if err != nil {
		panic(fmt.Sprintf("failed to get dir size:%v", err))
	}

	return &Stat{
		KeyNum:         uint(db.index.Size()),
		DataFileNum:    dataFiles,
		ReclaimbleSize: db.reclaimSize,
		DiskSize:       diskSize,
	}
}

// BackUp 把当前数据库目录复制到指定备份目录。
// 参数 dir 是备份输出目录，复制时会显式跳过进程锁文件，避免把运行态锁带入备份。
// 返回值表示目录复制是否成功；该方法会在持有读锁期间执行目录级复制。
func (db *DB) BackUp(dir string) error {
	db.mu.RLock()
	defer db.mu.RUnlock()
	// flock 文件只对“当前运行中的进程互斥”有意义，备份里不应复制它。
	return utils.CopyDir(db.options.DirPath, dir, []string{fileLockName})
}
