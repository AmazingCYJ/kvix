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

// merge 将仍然有效的历史数据重写到临时目录，并生成 Hint 文件加速后续启动。
// 可以把它理解成一次“只保留最新有效记录的整理与压缩”过程。
func (db *DB) merge() error {
	// 1. 先做合并前置检查，确认当前状态值得执行 merge。
	if db.activeFile == nil {
		return nil
	}

	// 1.1 合并过程需要独占切换活跃文件，先持有数据库写锁。
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
	// 1.2 废弃数据占比不足时直接退出，避免无意义的重写成本。
	if db.reclaimSize == 0 || float32(db.reclaimSize)/float32(totalSize) < db.options.DataFileMergeRatio {
		db.mu.Unlock()
		return nil
	}
	availableDiskSpace, err := utils.GetDiskFreeSpace()
	if err != nil {
		db.mu.Unlock()
		return err
	}
	// 1.3 预留足够磁盘空间，避免 merge 过程中写满磁盘导致实例不可用。
	if availableDiskSpace <= uint64(totalSize-db.reclaimSize) {
		db.mu.Unlock()
		return common.ErrNoEnoughDiskSpace
	}

	db.isMerging = true
	defer func() {
		db.isMerging = false
	}()

	// 2. 切换活跃文件，让后续新写入落到新文件中，旧文件集合才可以安全参与 merge。
	// 2.1 先持久化当前活跃文件，确保参与 merge 的数据完整可读。
	if err := db.activeFile.Sync(); err != nil {
		db.mu.Unlock()
		return err
	}

	// 2.2 当前活跃文件转入旧文件集合，后续作为 merge 输入。
	db.oldfiles[db.activeFile.FileID] = db.activeFile

	// 2.3 打开新的活跃文件，及时恢复前台写入能力。
	if err := db.setActiveDataFile(); err != nil {
		db.mu.Unlock()
		return err
	}

	// 2.4 记录本次 merge 边界处第一个未参与 merge 的活跃文件 ID，启动接管时据此识别 merge 边界。
	nonMergeFileID := db.activeFile.FileID

	// 2.5 收集所有需要参与 merge 的旧数据文件。
	var mergeFiles []*data.DataFile
	for _, oldFile := range db.oldfiles {
		mergeFiles = append(mergeFiles, oldFile)
	}

	// 2.6 释放写锁，后续重写旧文件时不阻塞新的前台写入。
	db.mu.Unlock()

	// 3. 准备临时 merge 目录和目标数据库实例。
	// 注意这里不是在原目录里直接改写旧文件，而是先在旁路目录构建一套全新结果。
	// 3.1 按文件 ID 从小到大处理，保持重写顺序稳定。
	sort.Slice(mergeFiles, func(i, j int) bool {
		return mergeFiles[i].FileID < mergeFiles[j].FileID
	})
	mergePath := db.getMergePath()
	if _, err := os.Stat(mergePath); err == nil {
		if err := os.RemoveAll(mergePath); err != nil {
			return err
		}
	}

	// 3.2 创建全新的 merge 目录，避免残留数据污染本次结果。
	if err := os.Mkdir(mergePath, os.ModePerm); err != nil {
		return err
	}
	mergeOptions := db.options
	mergeOptions.DirPath = mergePath
	mergeOptions.SyncWrites = false // 合并过程中统一在尾部刷盘即可。

	// 3.3 打开临时数据库实例，把有效记录重写到 merge 目录。
	mergeDB, err := Open(mergeOptions)
	if err != nil {
		return err
	}

	// 3.4 同时打开 Hint 文件，用来记录 merge 后的新索引位置。
	hintFile, err := data.OpenHintFile(mergePath)
	if err != nil {
		return err
	}
	defer hintFile.Close()

	// 4. 顺序扫描旧数据文件，只重写当前索引仍然指向的有效记录。
	for _, dataFile := range mergeFiles {
		var offset int64 = 0
		for {
			// 4.1 逐条读取旧文件中的日志记录。
			logRecord, size, err := dataFile.ReadLogRecord(offset)
			if err != nil {
				if err == io.EOF {
					break // 文件末尾，停止读取。
				}
				return err
			}

			// 4.2 解析出原始 key，并和内存索引比对确认这条记录仍然有效。
			realKey, _ := parseLogRecordKey(logRecord.Key)
			logRecordPos := db.index.Get(realKey)
			if logRecordPos != nil &&
				logRecordPos.Fid == dataFile.FileID &&
				logRecordPos.Offset == offset {
				// 4.3 重写有效记录时去掉事务序列号，merge 后数据按普通记录存储。
				logRecord.Key = logRecordKeyWithSeq(realKey, nonTransactionSeqNo)
				pos, err := mergeDB.appendLogRecord(logRecord)
				if err != nil {
					return err
				}
				// 4.4 同步把新位置写入 Hint 文件，供后续快速重建索引。
				if err := hintFile.WriteHintRecord(realKey, pos); err != nil {
					return err
				}
			}
			offset += size
		}
	}

	// 5. 刷盘 Hint 和 merge 数据，并写入 merge 完成标记。
	if err := hintFile.Sync(); err != nil {
		return err
	}
	if err := mergeDB.Sync(); err != nil {
		return err
	}

	// 5.1 完成标记保存本次 merge 边界处第一个未参与 merge 的活跃文件 ID，启动时据此判断哪些旧文件已被本次 merge 覆盖。
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

	// 5.2 单独刷盘完成标记，避免 merge 结果落地不完整。
	if err := finishFile.Sync(); err != nil {
		return err
	}
	return nil
}

// getMergePath 返回当前数据库对应的临时 merge 目录路径。
// 例如正式目录是 /tmp/kvix，那么 merge 临时目录就是 /tmp/kvix-merge。
func (db *DB) getMergePath() string {
	// 1. 取数据目录的父目录作为 merge 临时目录的根。
	dir := path.Dir(path.Clean(db.options.DirPath))

	// 2. 使用原目录名加 merge 后缀，保证和正式数据目录并列。
	base := path.Base(db.options.DirPath)
	return filepath.Join(dir, base+mergeFileSuffix)
}

// loadMegreFiles 在数据库启动阶段接管上次 merge 产生的临时结果。
// 如果上次 merge 已经完整完成，但结果还没搬回正式目录，就在这里做最后的“接管收尾”。
func (db *DB) loadMegreFiles() error {
	mergePath := db.getMergePath()

	// 1. merge 目录不存在时说明没有待接管的 merge 结果。
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

	// 2. 只有在完成标记存在时才接管目录内容，否则视为未完成 merge。
	// 这样可以避免把一半写完的 merge 结果误当成可用数据集。
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

	// 3. 删除本次 merge 已覆盖的旧数据文件，边界为所有文件 ID 小于该活跃文件 ID 的历史文件。
	var filedId uint32
	for filedId = 0; filedId <= nonMergeFileId; filedId++ {
		fileName := data.GetDataFileName(db.options.DirPath, filedId)
		if _, err := os.Stat(fileName); err == nil {
			if err := os.Remove(fileName); err != nil {
				return err
			}
		}
	}

	// 4. 将 merge 目录里的新数据文件和 Hint 文件搬回正式目录。
	for _, fileName := range mergeFinFileNames {
		srcPath := filepath.Join(mergePath, fileName)
		destPath := filepath.Join(db.options.DirPath, fileName)
		if err := os.Rename(srcPath, destPath); err != nil {
			return err
		}
	}
	return nil
}

// getNonMergeFileId 从 merge 完成标识中解析本次 merge 边界处第一个未参与 merge 的活跃文件 ID。
// 启动接管时需要它来判断：哪些旧文件已经被 merge 结果覆盖，哪些还应该保留。
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

// loadIndexFromHintFile 从 Hint 文件恢复索引，减少启动时的数据扫描成本。
// 对初学者来说，可以把 Hint 文件理解成“key -> 最新位置”的一份简化快照。
func (db *DB) loadIndexFromHintFile() error {
	// 查看 hint 文件是否存在，如果不存在，说明没有可复用的索引快照。
	hintFileName := filepath.Join(db.options.DirPath, common.HintFileName)
	if _, err := os.Stat(hintFileName); os.IsNotExist(err) {
		return nil
	}

	// 打开 Hint 文件并按顺序回放其中的索引记录。
	hintFile, err := data.OpenHintFile(db.options.DirPath)
	if err != nil {
		return err
	}

	// 逐条读取 Hint 记录，恢复 key 到日志位置的映射。
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
