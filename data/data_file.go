package data

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	. "kvix/common"
	"kvix/fio"
	"path/filepath"
)

// DataFile 表示一个已经打开的数据文件，负责日志记录的读写、刷盘和 IO 管理器切换。
// kvix 会同时维护活跃文件和若干只读旧文件，内存索引中的位置最终都落到某个 DataFile 上。
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

// WriteHintRecord 将 hint 记录写入 hint 文件，供 merge 后快速重建索引。
func (df *DataFile) WriteHintRecord(key []byte, pos *LogRecordPos) error {
	// 1. 将索引位置信息编码成普通日志记录的 value，复用现有日志编码格式。
	// 2. 使用 key 作为 hint 键，保证恢复阶段可以直接按 key 重建索引。
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

// ReadLogRecord 从指定偏移读取一条日志记录，并返回记录内容与占用字节数。
func (df *DataFile) ReadLogRecord(off int64) (*LogRecord, int64, error) {
	// 1. 先获取文件大小，避免偏移越界或读过文件尾部。
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
	// 2. 读取记录头，确认 CRC、记录类型以及 key/value 的长度。
	headerBuf, err := df.readNBytes(headerBytes, off)
	if err != nil {
		return nil, 0, err
	}
	// 3. 解析头部，计算整条记录的物理长度。
	header, headerSize := decodeLogRecord(headerBuf)
	if header == nil {
		return nil, 0, fmt.Errorf("failed to decode log record header at offset %d", off)
	}
	if header.crc == 0 && header.keySize == 0 && header.valueSize == 0 {
		return nil, 0, io.EOF
	}
	// 4. 根据 key/value 长度决定是否继续读取载荷区。
	keySize, valueSize := int64(header.keySize), int64(header.valueSize)
	var recordSize = headerSize + keySize + valueSize
	var logRecord *LogRecord
	if keySize > 0 || valueSize > 0 {
		kvBuf, err := df.readNBytes(keySize+valueSize, off+headerSize)
		if err != nil {
			return nil, 0, err
		}
		// 4.1 将载荷区按 key/value 切分回 LogRecord。
		logRecord = &LogRecord{
			Key:   kvBuf[:keySize],
			Value: kvBuf[keySize:],
			Type:  header.recordType,
		}
		return logRecord, recordSize, nil
	}
	// 5. 对空载荷记录执行 CRC 校验，避免读取到损坏数据。
	crc := getLogRecordCRC(logRecord, headerBuf[crc32.Size:headerSize])
	if crc != header.crc {
		return nil, 0, ErrInvlidCRC
	}
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
