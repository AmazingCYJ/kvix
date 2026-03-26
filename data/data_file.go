package data

import (
	. "bitcask-my/common"
	"bitcask-my/fio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"path/filepath"
)

type DataFile struct {
	FileID    uint32        // 文件ID
	WriteOff  int64         // 当前写入偏移
	IoManager fio.IOManager //io读写管理
}

// CRC type Keysize ValueSize
const maxLogRecordHeardSize = 4 + 1 + binary.MaxVarintLen32 + binary.MaxVarintLen32

// OpenDataFile 打开一个数据文件
func OpenDataFile(dirPath string, fileID uint32, ioType fio.FileIOType) (*DataFile, error) {
	//1.构建数据文件路径
	filePath := filepath.Join(dirPath, fmt.Sprintf("%09d", fileID)+DataFileSuffix)
	//2.创建或打开数据文件
	return newDatafile(filePath, fileID, ioType)
}

// OpenHintFile 打开Hint文件
func OpenHintFile(dirPath string) (*DataFile, error) {
	filePath := filepath.Join(dirPath, HintFileName)
	return newDatafile(filePath, 0, fio.StandardFIO)
}

// OpenMergeDataFile 打开Merge完成标记文件
func OpenMergeDataFile(dirPath string) (*DataFile, error) {
	filePath := filepath.Join(dirPath, MergeFinishedFileName)
	return newDatafile(filePath, 0, fio.StandardFIO)
}

// OpenSeqNoFile 打开序列号文件
func OpenSeqNoFile(dirPath string) (*DataFile, error) {
	filePath := filepath.Join(dirPath, SeqNoFileName)
	return newDatafile(filePath, 0, fio.StandardFIO)
}

func GetDataFileName(dirPath string, fileID uint32) string {
	return filepath.Join(dirPath, fmt.Sprintf("%09d", fileID)+DataFileSuffix)
}

// WriteHintRecord 将 Hint 记录写入 Hint 文件
func (df *DataFile) WriteHintRecord(key []byte, pos *LogRecordPos) error {
	// Hint记录格式: Key Size (4 bytes) | Key (variable) | FileID (4 bytes) | Offset (8 bytes)
	record := &LogRecord{
		Key:   key,
		Value: EncodeLogRecordPos(pos),
		Type:  LogRecordNormal,
	}
	encRecord, _ := EncodeLogRecord(record)
	return df.Write(encRecord)
}

func newDatafile(filePath string, fileID uint32, ioType fio.FileIOType) (*DataFile, error) {
	//1.创建IOManager实例
	ioManager, err := fio.NewIOManager(filePath, ioType)
	if err != nil {
		return nil, err
	}
	//2.创建DataFile实例
	return &DataFile{
		FileID:    fileID,
		WriteOff:  0,
		IoManager: ioManager,
	}, nil
}

func (df *DataFile) Sync() error {
	return df.IoManager.Sync()
}

func (df *DataFile) Close() error {
	return df.IoManager.Close()
}
func (df *DataFile) Write(p []byte) error {
	n, err := df.IoManager.Write(p)
	if err != nil {
		return err
	}
	df.WriteOff += int64(n)
	return nil
}

// ReadLogRecord 从数据文件指定偏移读取一条日志记录，返回LogRecord、记录大小和错误信息。
func (df *DataFile) ReadLogRecord(off int64) (*LogRecord, int64, error) {
	//获取文件大小，确保读取不会越界
	size, err := df.IoManager.Size()
	if err != nil {
		return nil, 0, err
	}
	if off >= size {
		return nil, 0, io.EOF
	}
	var headerBytes int64 = maxLogRecordHeardSize
	// 如果剩余数据不足一个完整的记录头部，则只读取剩余的数据
	if off+maxLogRecordHeardSize > size {
		headerBytes = size - off
	}
	//1.读取记录头部，获取CRC、KeySize、ValueSize等信息
	headerBuf, err := df.readNBytes(headerBytes, off)
	if err != nil {
		return nil, 0, err
	}
	//2.解析记录头部，获取KeySize和ValueSize
	header, headerSize := decodeLogRecord(headerBuf)
	if header == nil {
		return nil, 0, fmt.Errorf("failed to decode log record header at offset %d", off)
	}
	if header.crc == 0 && header.keySize == 0 && header.valueSize == 0 {
		return nil, 0, io.EOF
	}
	//3.取出key和value的长度
	keySize, valueSize := int64(header.keySize), int64(header.valueSize)
	var recordSize = headerSize + keySize + valueSize
	//4.读取key和value数据
	var logRecord *LogRecord
	if keySize > 0 || valueSize > 0 {
		kvBuf, err := df.readNBytes(keySize+valueSize, off+headerSize)
		if err != nil {
			return nil, 0, err
		}
		//4.1解析key和value数据
		logRecord = &LogRecord{
			Key:   kvBuf[:keySize],
			Value: kvBuf[keySize:],
			Type:  header.recordType,
		}
		return logRecord, recordSize, nil
	}
	// 5.校验数据完整性
	// 5.1计算CRC并与头部CRC进行比较,如果不匹配则返回错误
	crc := getLogRecordCRC(logRecord, headerBuf[crc32.Size:headerSize])
	if crc != header.crc {
		return nil, 0, ErrInvlidCRC
	}
	// 6.返回LogRecord、记录大小和nil错误
	return logRecord, recordSize, nil
}

func (df *DataFile) readNBytes(n int64, offset int64) (b []byte, err error) {
	b = make([]byte, n)
	_, err = df.IoManager.ReadAt(b, offset)
	return
}

func (df *DataFile) SetIOManager(dirPath string, ioType fio.FileIOType) error {
	if err := df.IoManager.Close(); err != nil {
		return err
	}
	ioManager, err := fio.NewIOManager(GetDataFileName(dirPath, df.FileID), ioType)
	if err != nil {
		return err
	}
	df.IoManager = ioManager
	return nil
}
