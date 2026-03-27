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
	IoManager fio.IOManager // 底层 IO 抽象，屏蔽标准文件与 mmap 的差异
}

// maxLogRecordHeardSize 是单条日志记录头部可能占用的最大字节数。
// 组成分别是：CRC(4) + Type(1) + KeySize(varint) + ValueSize(varint)。
const maxLogRecordHeardSize = 4 + 1 + binary.MaxVarintLen32 + binary.MaxVarintLen32

// OpenDataFile 按文件 ID 打开一个普通数据文件。
// 例如 fileID=1 会映射到 000000001.data 这样的真实文件名。
func OpenDataFile(dirPath string, fileID uint32, ioType fio.FileIOType) (*DataFile, error) {
	//1.构建数据文件路径
	filePath := filepath.Join(dirPath, fmt.Sprintf("%09d", fileID)+DataFileSuffix)
	//2.创建或打开数据文件
	return newDatafile(filePath, fileID, ioType)
}

// OpenHintFile 打开 Hint 文件。
// Hint 文件本质上也是一个数据文件，只是里面保存的是 key -> LogRecordPos 的索引快照。
func OpenHintFile(dirPath string) (*DataFile, error) {
	filePath := filepath.Join(dirPath, HintFileName)
	return newDatafile(filePath, 0, fio.StandardFIO)
}

// OpenMergeDataFile 打开 merge 完成标记文件。
// 这个文件用来告诉启动恢复逻辑：上次 merge 是否已经完整结束，以及边界文件 ID 是多少。
func OpenMergeDataFile(dirPath string) (*DataFile, error) {
	filePath := filepath.Join(dirPath, MergeFinishedFileName)
	return newDatafile(filePath, 0, fio.StandardFIO)
}

// OpenSeqNoFile 打开事务序列号持久化文件。
func OpenSeqNoFile(dirPath string) (*DataFile, error) {
	filePath := filepath.Join(dirPath, SeqNoFileName)
	return newDatafile(filePath, 0, fio.StandardFIO)
}

// GetDataFileName 根据目录和文件 ID 生成标准数据文件路径。
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

// newDatafile 是统一的数据文件构造入口。
// 它负责选择底层 IO 实现，并把文件对象包装成上层可用的 DataFile。
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

// Sync 把当前 DataFile 的操作系统缓冲区显式落盘。
func (df *DataFile) Sync() error {
	return df.IoManager.Sync()
}

// Close 关闭当前 DataFile 持有的底层 IO 资源。
func (df *DataFile) Close() error {
	return df.IoManager.Close()
}

// Write 以追加写方式把一段字节写入数据文件，并同步推进 WriteOff。
// WriteOff 总是表示“下一次写入应该落到哪里”。
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

// readNBytes 是对底层 ReadAt 的一个薄封装。
// 调用方给定偏移和长度后，它返回该物理区间的原始字节。
func (df *DataFile) readNBytes(n int64, offset int64) (b []byte, err error) {
	b = make([]byte, n)
	_, err = df.IoManager.ReadAt(b, offset)
	return
}

// SetIOManager 关闭当前 IO 实现并按同一文件路径重新打开另一种实现。
// 它主要用于“启动恢复阶段使用 mmap，恢复完成后切回标准文件 IO”这类场景。
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
