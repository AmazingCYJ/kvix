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

	. "kvix/common"
	"kvix/data"
	"kvix/fio"
)

// setActiveDataFile 打开下一个可写数据文件，并把它设置为当前活跃文件。
func (db *DB) setActiveDataFile() error {
	var initialFileId uint32 = 0
	if db.activeFile != nil {
		initialFileId = db.activeFile.FileID + 1
	}
	datafile, err := data.OpenDataFile(db.options.DirPath, initialFileId, fio.StandardFIO)
	if err != nil {
		return err
	}
	db.activeFile = datafile
	return nil
}

// loadDataFiles 扫描数据目录、按文件 ID 顺序打开所有数据文件，并区分活跃文件和旧文件。
func (db *DB) loadDataFiles() error {
	files, err := os.ReadDir(db.options.DirPath)
	if err != nil {
		return err
	}

	var fileIds []int
	for _, file := range files {
		if strings.HasSuffix(file.Name(), DataFileSuffix) {
			splitNames := strings.Split(file.Name(), ".")
			fileId, err := strconv.Atoi(splitNames[0])
			if err != nil {
				return ErrDataDirectoryCorrupted
			}
			fileIds = append(fileIds, fileId)
		}
	}

	sort.Ints(fileIds)
	db.fileIds = fileIds

	for i, fileId := range fileIds {
		ioType := fio.StandardFIO
		if db.options.MMapAtStartup {
			ioType = fio.MemoryMap
		}
		dataFile, err := data.OpenDataFile(db.options.DirPath, uint32(fileId), ioType)
		if err != nil {
			return err
		}
		if i == len(fileIds)-1 {
			db.activeFile = dataFile
		} else {
			db.oldfiles[uint32(fileId)] = dataFile
		}
	}
	return nil
}

// loadIndexFromDataFiles 顺序回放数据文件中的日志记录，并把最终状态恢复到内存索引中。
// 该过程会处理 merge 遗留、事务完成标记和废弃空间统计，是非 B+Tree 索引启动恢复的核心步骤。
func (db *DB) loadIndexFromDataFiles() error {
	// 1. 没有任何数据文件时无需回放，说明数据库目录当前还是空的。
	if len(db.fileIds) == 0 {
		return nil
	}

	// 2. 检查是否存在 merge 完成标记；若存在，需要跳过已经被 merge 替换的旧文件区间。
	hasMerge, nonMergeFileId := false, uint32(0)
	mergeFinFileName := filepath.Join(db.options.DirPath, MergeFinishedFileName)
	if _, err := os.Stat(mergeFinFileName); err == nil {
		fid, err := db.getNonMergeFileId(db.options.DirPath)
		if err != nil {
			return err
		}
		hasMerge = true
		nonMergeFileId = fid
	}

	// 3. 封装统一的索引更新逻辑，确保普通记录、删除记录和废弃空间统计保持一致。
	updateIndex := func(key []byte, typ data.LogRecordType, pos *data.LogRecordPos) error {
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
		return nil
	}

	// 4. 事务中的数据只有在读到 TxnFinished 记录后才能整体生效，因此先按序列号暂存。
	TransactionRecords := make(map[uint64][]*data.TransactionRecord)
	var currentSeqNo uint64 = nonTransactionSeqNo

	// 5. 按文件 ID 升序回放每个数据文件，保证后写入的记录可以覆盖先前状态。
	for i, fid := range db.fileIds {
		var fileId = uint32(fid)
		if hasMerge && fileId < nonMergeFileId {
			continue
		}

		var dataFile *data.DataFile
		if db.activeFile.FileID == fileId {
			dataFile = db.activeFile
		} else {
			dataFile = db.oldfiles[fileId]
		}
		if dataFile == nil {
			return ErrDataFileNotFound
		}

		// 5.1 顺序扫描当前文件内的每一条记录，直到读到文件尾。
		var offset int64 = 0
		for {
			logRecord, size, err := dataFile.ReadLogRecord(offset)
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
					break
				}
				return err
			}

			// 5.1.1 为当前记录构造磁盘位置，后续无论写入索引还是暂存事务都要依赖它。
			pos := &data.LogRecordPos{
				Fid:    fileId,
				Offset: offset,
				Size:   uint32(size),
			}

			// 5.1.2 从编码后的 key 里拆出真实 key 和事务序列号，决定这条记录如何参与索引恢复。
			realKey, seqNo := parseLogRecordKey(logRecord.Key)
			if seqNo == nonTransactionSeqNo {
				updateIndex(realKey, logRecord.Type, pos)
			} else {
				if logRecord.Type == data.LogRecordTxnFinished {
					for _, record := range TransactionRecords[seqNo] {
						realKey, _ := parseLogRecordKey(record.Record.Key)
						updateIndex(realKey, record.Record.Type, record.Pos)
					}
					delete(TransactionRecords, seqNo)
				} else {
					TransactionRecords[seqNo] = append(TransactionRecords[seqNo], &data.TransactionRecord{
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

// logSeqNo 从事务序列号文件中恢复最近一次持久化的事务序号。
func (db *DB) logSeqNo() error {
	fileName := filepath.Join(db.options.DirPath, SeqNoFileName)
	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		return nil
	}
	seqNoFile, err := data.OpenSeqNoFile(db.options.DirPath)
	if err != nil {
		return err
	}
	record, _, err := seqNoFile.ReadLogRecord(0)
	seqNo, err := strconv.ParseUint(string(record.Value), 10, 64)
	if err != nil {
		return err
	}
	db.seqNo = seqNo
	db.seqNoFileExist = true
	return nil
}

// parseLogRecordKey 解析日志记录中的编码 key，分离出真实 key 和事务序列号。
func parseLogRecordKey(key []byte) ([]byte, uint64) {
	seqNo, n := binary.Uvarint(key)
	realKey := key[n:]
	return realKey, seqNo
}

// resetIoType 把启动阶段使用的内存映射 IO 切回标准文件 IO，避免后续运行期继续持有 mmap 句柄。
func (db *DB) resetIoType() error {
	if db.activeFile == nil {
		return nil
	}
	if err := db.activeFile.SetIOManager(db.options.DirPath, fio.StandardFIO); err != nil {
		return err
	}
	for _, dataFile := range db.oldfiles {
		if err := dataFile.SetIOManager(db.options.DirPath, fio.StandardFIO); err != nil {
			return err
		}
	}
	return nil
}
