package kvix

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"kvix/common"
	"kvix/data"
	"kvix/fio"
)

// setActiveDataFile 打开下一个可写数据文件，并把它设置为当前活跃文件。
// "活跃文件"是当前仍在持续追加写入的数据文件。
func (db *DB) setActiveDataFile() error {
	var initialFileID uint32 = 0
	if db.activeFile != nil {
		// 当前已经有活跃文件时，新文件 ID 直接在原基础上加 1。
		initialFileID = db.activeFile.FileID + 1
	}
	dataFile, err := data.OpenDataFile(db.options.DirPath, initialFileID, fio.StandardFIO)
	if err != nil {
		return err
	}
	db.activeFile = dataFile
	return nil
}

// loadDataFiles 扫描数据目录、按文件 ID 顺序打开所有数据文件，并区分活跃文件和旧文件。
// 这个过程本身不重建索引，它只负责"把磁盘上的文件集合恢复成内存里的文件对象集合"。
func (db *DB) loadDataFiles() error {
	// 1. 读取数据目录中的所有条目。
	files, err := os.ReadDir(db.options.DirPath)
	if err != nil {
		return err
	}

	// 2. 筛选出数据文件（以 .data 结尾），并解析文件 ID。
	var fileIDs []int
	for _, file := range files {
		if strings.HasSuffix(file.Name(), common.DataFileSuffix) {
			splitNames := strings.Split(file.Name(), ".")
			fileID, err := strconv.Atoi(splitNames[0])
			if err != nil {
				return common.ErrDataDirectoryCorrupted
			}
			fileIDs = append(fileIDs, fileID)
		}
	}

	// 3. 按文件 ID 升序排列，保证后续回放顺序正确。
	sort.Ints(fileIDs)
	db.fileIds = fileIDs

	// 4. 逐个打开数据文件，最后一个视为活跃文件，其余为旧文件。
	for i, fid := range fileIDs {
		// 启动恢复阶段可选择 mmap，提高大量顺序读取时的读取效率。
		ioType := fio.StandardFIO
		if db.options.MMapAtStartup {
			ioType = fio.MemoryMap
		}
		dataFile, err := data.OpenDataFile(db.options.DirPath, uint32(fid), ioType)
		if err != nil {
			return err
		}
		// 排序后的最后一个文件默认视为活跃文件，其余都是旧文件。
		if i == len(fileIDs)-1 {
			db.activeFile = dataFile
		} else {
			db.oldFiles[uint32(fid)] = dataFile
		}
	}
	return nil
}

// loadIndexFromDataFiles 顺序回放数据文件中的日志记录，并把最终状态恢复到内存索引中。
//
// 该过程会处理 merge 遗留、事务完成标记和废弃空间统计，
// 是非 B+Tree 索引启动恢复的核心步骤。
func (db *DB) loadIndexFromDataFiles() error {
	// 1. 没有任何数据文件时无需回放，说明数据库目录当前还是空的。
	if len(db.fileIds) == 0 {
		return nil
	}

	// 2. 检查是否存在 merge 完成标记；若存在，需要跳过已经被 merge 替换的旧文件区间。
	hasMerge, nonMergeFileID := false, uint32(0)
	mergeFinFileName := filepath.Join(db.options.DirPath, common.MergeFinishedFileName)
	if _, err := os.Stat(mergeFinFileName); err == nil {
		fid, err := db.getNonMergeFileID(db.options.DirPath)
		if err != nil {
			return err
		}
		hasMerge = true
		nonMergeFileID = fid
	}

	// 3. 封装统一的索引更新逻辑，确保普通记录、删除记录和废弃空间统计保持一致。
	updateIndex := func(key []byte, typ data.LogRecordType, pos *data.LogRecordPos) {
		var oldPos *data.LogRecordPos
		if typ == data.LogRecordDeleted {
			oldPos, _ = db.index.Delete(key)
			db.reclaimSize += int64(pos.Size)
		} else {
			oldPos = db.index.Put(key, pos)
		}
		if oldPos != nil {
			db.reclaimSize += int64(oldPos.Size)
		}
	}

	// 4. 事务中的数据只有在读到 TxnFinished 记录后才能整体生效，因此先按序列号暂存。
	transactionRecords := make(map[uint64][]*data.TransactionRecord)
	var currentSeqNo uint64 = nonTransactionSeqNo

	// 5. 按文件 ID 升序回放每个数据文件，保证后写入的记录可以覆盖先前状态。
	for i, fid := range db.fileIds {
		var fileID = uint32(fid)
		if hasMerge && fileID < nonMergeFileID {
			// 这些文件的数据已经被 merge 结果覆盖，重复回放只会把旧状态写回索引。
			continue
		}

		var dataFile *data.DataFile
		if db.activeFile.FileID == fileID {
			dataFile = db.activeFile
		} else {
			dataFile = db.oldFiles[fileID]
		}
		if dataFile == nil {
			return common.ErrDataFileNotFound
		}

		// 5.1 顺序扫描当前文件内的每一条记录，直到读到文件尾。
		var offset int64 = 0
		for {
			logRecord, size, err := dataFile.ReadLogRecord(offset)
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
					// 读到文件末尾或文件已关闭时，说明当前文件回放完成。
					break
				}
				return err
			}

			// 5.1.1 为当前记录构造磁盘位置，后续无论写入索引还是暂存事务都要依赖它。
			pos := &data.LogRecordPos{
				Fid:    fileID,
				Offset: offset,
				Size:   uint32(size),
			}

			// 5.1.2 从编码后的 key 里拆出真实 key 和事务序列号，决定这条记录如何参与索引恢复。
			realKey, seqNo := parseLogRecordKey(logRecord.Key)
			if seqNo == nonTransactionSeqNo {
				// 普通非事务记录可以立即参与索引恢复。
				updateIndex(realKey, logRecord.Type, pos)
			} else {
				// 带事务序列号的记录需要先看它是不是"事务完成标记"。
				if logRecord.Type == data.LogRecordTxnFinished {
					// 读到完成标记后，说明这个 seqNo 对应的整批事务终于可以整体生效了。
					for _, record := range transactionRecords[seqNo] {
						realKey, _ := parseLogRecordKey(record.Record.Key)
						updateIndex(realKey, record.Record.Type, record.Pos)
					}
					delete(transactionRecords, seqNo)
				} else {
					// 尚未完成的事务记录先暂存，等对应完成标记出现后再统一写索引。
					transactionRecords[seqNo] = append(transactionRecords[seqNo], &data.TransactionRecord{
						Record: logRecord,
						Pos:    pos,
					})
				}
			}

			// 5.1.3 维护已见到的最大事务序列号，供后续写入继续递增而不是重用旧值。
			if seqNo > currentSeqNo {
				currentSeqNo = seqNo
			}
			offset += size
		}

		// 5.2 最后一个文件就是当前活跃文件，需要把写偏移推进到已扫描的文件末尾。
		if i == len(db.fileIds)-1 {
			db.activeFile.WriteOff = offset
		}
	}

	// 6. 启动恢复完成后，把最大事务序列号写回 DB，保证后续事务继续按正确顺序编号。
	db.seqNo = currentSeqNo
	return nil
}

// loadSeqNo 从事务序列号文件中恢复最近一次持久化的事务序号。
// B+Tree 索引模式下，批量写事务恢复更依赖这个编号，因为索引自身已经持久化。
func (db *DB) loadSeqNo() error {
	fileName := filepath.Join(db.options.DirPath, common.SeqNoFileName)
	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		// 没有 seq-no 文件通常说明当前还是第一次启动，或尚未执行过事务批量写。
		return nil
	}

	seqNoFile, err := data.OpenSeqNoFile(db.options.DirPath)
	if err != nil {
		return err
	}

	// 1. 读取 seq-no 文件中的唯一一条记录。
	record, _, err := seqNoFile.ReadLogRecord(0)
	if err != nil {
		return err
	}

	// 2. 将十进制文本格式的事务序列号解析为 uint64。
	seqNo, err := strconv.ParseUint(string(record.Value), 10, 64)
	if err != nil {
		return err
	}

	db.seqNo = seqNo
	db.seqNoFileExist = true
	return nil
}

// parseLogRecordKey 解析日志记录中的编码 key，分离出真实 key 和事务序列号。
// 批量事务会把 seqNo 编到 key 前缀里，普通 Put/Delete 则使用固定的非事务序号。
func parseLogRecordKey(key []byte) ([]byte, uint64) {
	// Uvarint 返回"解析出的序列号 + 占用了前缀多少字节"。
	seqNo, n := binary.Uvarint(key)
	// 跳过前缀剩下的部分，就是真实业务 key。
	realKey := key[n:]
	return realKey, seqNo
}

// resetIOType 把启动阶段使用的内存映射 IO 切回标准文件 IO，
// 避免后续运行期继续持有只读 mmap 句柄。
func (db *DB) resetIOType() error {
	if db.activeFile == nil {
		return nil
	}
	// 活跃文件和旧文件都要统一切回标准文件 IO。
	if err := db.activeFile.SetIOManager(db.options.DirPath, fio.StandardFIO); err != nil {
		return err
	}
	for _, dataFile := range db.oldFiles {
		if err := dataFile.SetIOManager(db.options.DirPath, fio.StandardFIO); err != nil {
			return err
		}
	}
	return nil
}
