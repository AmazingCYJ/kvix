package kvix

import (
	"io"
	"kvix/common"
	"kvix/data"
	"kvix/utils"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
)

const (
	mergeFileSuffix = "-merge"
	mergeFinisedKey = "merge.finished"
)

// Merge 清理无效数据 生成 Hint文件
func (db *DB) merge() error {
	//1.如果数据库中没有数据文件，则不需要合并
	if db.activeFile == nil {
		return nil
	}
	//2.如果正在进行合并操作，直接返回错误
	db.mu.Lock()
	if db.isMerging {
		db.mu.Unlock()
		return common.ErrMergeIsProgress
	}
	totalSize, err := utils.GetDirSize(db.options.DirPath)
	if err != nil {
		db.mu.Unlock()
		return err
	}
	//如果废弃数据占比小于合并阈值，也不需要进行合并
	if db.reclaimSize == 0 || float32(db.reclaimSize)/float32(totalSize) < db.options.DataFileMergeRatio {
		db.mu.Unlock()
		return nil
	}
	availableDiskSpace, err := utils.GetDiskFreeSpace()
	if err != nil {
		db.mu.Unlock()
		return err
	}
	//如果可用磁盘空间不足以存储合并后的数据文件，也不进行合并，避免磁盘空间不足导致数据库不可用
	if availableDiskSpace <= uint64(totalSize-db.reclaimSize) {
		db.mu.Unlock()
		return common.ErrNoEnoughDiskSpace
	}

	db.isMerging = true
	defer func() {
		db.isMerging = false
	}()
	//3.\*合并过程*:
	//3.1持久化活跃文件
	if err := db.activeFile.Sync(); err != nil {
		db.mu.Unlock()
		return err
	}
	//3.2将当前活跃文件转换为旧活跃文件
	db.oldfiles[db.activeFile.FileID] = db.activeFile
	//3.3打开一个新的活跃文件
	if err := db.setActiveDataFile(); err != nil {
		db.mu.Unlock()
		return err
	}

	// 记录最近没有参与merge的文件ID
	nonMergeFileID := db.activeFile.FileID
	//3.4取出所有旧的活跃文件
	var mergeFiles []*data.DataFile
	for _, oldFile := range db.oldfiles {
		mergeFiles = append(mergeFiles, oldFile)
	}
	//3.5 释放锁 此时其他数据可以写入活跃文件中
	db.mu.Unlock()
	//3.6将merge文件从小到大排序
	sort.Slice(mergeFiles, func(i, j int) bool {
		return mergeFiles[i].FileID < mergeFiles[j].FileID
	})
	mergePath := db.getMergePath()
	if _, err := os.Stat(mergePath); err == nil {
		if err := os.RemoveAll(mergePath); err != nil {
			return err
		}
	}
	//3.7创建merge 目录
	if err := os.Mkdir(mergePath, os.ModePerm); err != nil {
		return err
	}
	mergeOptions := db.options
	mergeOptions.DirPath = mergePath
	mergeOptions.SyncWrites = false //合并过程中不需要每次写入都同步磁盘，等合并完成后再统一同步一次

	//打开一个新的临时 kvix 数据库实例，使用 merge 目录作为数据目录
	mergeDB, err := Open(mergeOptions)
	if err != nil {
		return err
	}
	// 打开hint 文件存储索引
	hintFile, err := data.OpenHintFile(mergePath)
	if err != nil {
		return err
	}
	defer hintFile.Close()

	//3.8遍历处理每个数据文件
	for _, dataFile := range mergeFiles {
		var offset int64 = 0
		for {
			//从数据文件中读取一条日志记录
			logRecord, size, err := dataFile.ReadLogRecord(offset)
			if err != nil {
				if err == io.EOF {
					break //文件末尾，停止读取
				}
				return err
			}
			// 解析日志记录的 key，获取原始 key 和事务序列号
			realKey, _ := parseLogRecordKey(logRecord.Key)
			logRecordPos := db.index.Get(realKey)
			// 和内存中的索引位置进行比较,如果有效则重写
			if logRecordPos != nil &&
				logRecordPos.Fid == dataFile.FileID &&
				logRecordPos.Offset == offset {
				// 清除事务标记
				logRecord.Key = logRecordKeyWithSeq(realKey, nonTransactionSeqNo)
				pos, err := mergeDB.appendLogRecord(logRecord)
				if err != nil {
					return err
				}
				// 将位置索引写到Hint文件中
				if err := hintFile.WriteHintRecord(realKey, pos); err != nil {
					return err
				}
			}
			offset += size
		}
	}

	//sync hint 文件，确保索引数据持久化到磁盘
	if err := hintFile.Sync(); err != nil {
		return err
	}
	if err := mergeDB.Sync(); err != nil {
		return err
	}
	//添加merge完成标识
	finishFile, err := data.OpenMergeDataFile(mergePath)
	if err != nil {
		return err
	}
	defer finishFile.Close()
	mergeFinRecord := &data.LogRecord{
		Key:   []byte(mergeFinisedKey),
		Value: []byte(strconv.Itoa(int(nonMergeFileID))),
	}
	encRecord, _ := data.EncodeLogRecord(mergeFinRecord)
	if err := finishFile.Write(encRecord); err != nil {
		return err
	}
	// sync merge完成标识文件，确保数据持久化到磁盘
	if err := finishFile.Sync(); err != nil {
		return err
	}
	return nil
}

// tmp/kvix
// tmp/kvix-merge
func (db *DB) getMergePath() string {
	//1.返回tmp/
	dir := path.Dir(path.Clean(db.options.DirPath))
	///2.返回kvix目录名
	base := path.Base(db.options.DirPath)
	return filepath.Join(dir, base+mergeFileSuffix)
}

func (db *DB) loadMegreFiles() error {
	mergePath := db.getMergePath()
	//1.检查merge完成标识文件是否存在，如果不存在，说明没有未完成的合并操作，直接返回
	if _, err := os.Stat(mergePath); os.IsNotExist(err) {
		return nil
	}
	defer func() {
		_ = os.RemoveAll(mergePath)
	}()
	dirEntries, err := os.ReadDir(mergePath)
	if err != nil {
		return err
	}
	// 查找标识merege完成的文件,判断merge是否完成
	var mergeFinished bool
	var mergeFinFileNames []string
	for _, entry := range dirEntries {
		if entry.Name() == common.MergeFinishedFileName {
			mergeFinished = true
			break
		}
		if entry.Name() == common.SeqNoFileName {
			continue
		}
		mergeFinFileNames = append(mergeFinFileNames, entry.Name())
	}
	if !mergeFinished {
		return nil
	}
	nonMergeFileId, err := db.getNonMergeFileId(mergePath)
	if err != nil {
		return err
	}
	// 删除旧数据文件
	var filedId uint32
	for filedId = 0; filedId <= nonMergeFileId; filedId++ {
		fileName := data.GetDataFileName(db.options.DirPath, filedId)
		if _, err := os.Stat(fileName); err == nil {
			if err := os.Remove(fileName); err != nil {
				return err
			}
		}
	}
	// 将merge目录下的Hint文件和数据文件移动到原数据目录下
	for _, fileName := range mergeFinFileNames {
		srcPath := filepath.Join(mergePath, fileName)
		destPath := filepath.Join(db.options.DirPath, fileName)
		if err := os.Rename(srcPath, destPath); err != nil {
			return err
		}
	}
	return nil
}

// getNonMergeFileId 从merge完成标识文件中获取最近没有参与merge的文件ID
func (db *DB) getNonMergeFileId(dirPath string) (uint32, error) {
	mergeFinishedFile, err := data.OpenMergeDataFile(dirPath)
	if err != nil {
		return 0, err
	}
	record, _, err := mergeFinishedFile.ReadLogRecord(0)
	if err != nil {
		return 0, err
	}
	nonMergeFileId, err := strconv.Atoi(string(record.Value))
	if err != nil {
		return 0, err
	}
	return uint32(nonMergeFileId), nil
}

// 从hint文件中加载索引数据
func (db *DB) loadIndexFromHintFile() error {
	// 查看hint文件是否存在，如果不存在，说明没有可用的hint文件，直接返回
	hintFileName := filepath.Join(db.options.DirPath, common.HintFileName)
	if _, err := os.Stat(hintFileName); os.IsNotExist(err) {
		return nil
	}
	// 打开hint 文件
	hintFile, err := data.OpenHintFile(db.options.DirPath)
	if err != nil {
		return err
	}
	// 读取hint文件中的索引记录，更新内存索引
	var offset int64 = 0
	for {
		logRecord, size, err := hintFile.ReadLogRecord(offset)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		pos := data.DecodeLogRecordPos(logRecord.Value)
		db.index.Put(logRecord.Key, pos)
		offset += size
	}
	return nil
}
