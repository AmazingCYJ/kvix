package data

import (
	"fmt"
	"hash/crc32"
	"io"
	"kvix/common"
	"kvix/fio"
	"path/filepath"
)

// DataFile 表示一个已经打开的数据文件，负责日志记录的读写、刷盘和 IO 管理器切换。
// kvix 引擎会同时维护一个活跃文件（可读写）和若干只读旧文件；
// 内存索引中的 LogRecordPos 最终都指向某个 DataFile 中的具体偏移。
type DataFile struct {
	FileID    uint32        // 数据文件的唯一标识，对应磁盘上的 000000001.data 等文件名
	WriteOff  int64         // 当前写入偏移，始终指向下一次追加写入的起始位置
	IoManager fio.IOManager // 底层 IO 抽象层，屏蔽标准文件 IO 与 mmap 的差异
}

// OpenDataFile 按文件 ID 打开（或创建）一个普通数据文件。
// 文件名格式为 9 位零填充的十进制数加 .data 后缀，例如 fileID=1 对应 000000001.data。
// ioType 参数决定使用标准文件 IO 还是 mmap。
func OpenDataFile(dirPath string, fileID uint32, ioType fio.FileIOType) (*DataFile, error) {
	// 1. 根据目录路径和文件 ID 构建完整的数据文件路径。
	filePath := filepath.Join(dirPath, fmt.Sprintf("%09d", fileID)+common.DataFileSuffix)

	// 2. 调用统一构造函数打开文件并返回 DataFile 实例。
	return newDatafile(filePath, fileID, ioType)
}

// OpenHintFile 打开 Hint 索引文件。
// Hint 文件本质上也是一个数据文件，只是里面保存的是 key -> LogRecordPos 的索引快照。
// merge 完成后会生成此文件，启动时据此快速重建内存索引，避免全量扫描数据文件。
func OpenHintFile(dirPath string) (*DataFile, error) {
	// 1. 拼接 Hint 文件的完整路径。
	filePath := filepath.Join(dirPath, common.HintFileName)

	// 2. 以标准文件 IO 方式打开。
	return newDatafile(filePath, 0, fio.StandardFIO)
}

// OpenMergeDataFile 打开 merge 完成标记文件。
// 此文件用于告诉启动恢复逻辑：上次 merge 是否已经完整结束，以及边界文件 ID 是多少。
// 如果该文件不存在或内容不完整，恢复阶段会忽略 merge 目录中的数据。
func OpenMergeDataFile(dirPath string) (*DataFile, error) {
	// 1. 拼接 merge 完成标记文件的完整路径。
	filePath := filepath.Join(dirPath, common.MergeFinishedFileName)

	// 2. 以标准文件 IO 方式打开。
	return newDatafile(filePath, 0, fio.StandardFIO)
}

// OpenSeqNoFile 打开事务序列号持久化文件。
// 引擎关闭时会将当前最大事务序列号写入此文件，
// 下次启动时读取它以保证序列号的单调递增。
func OpenSeqNoFile(dirPath string) (*DataFile, error) {
	// 1. 拼接序列号文件的完整路径。
	filePath := filepath.Join(dirPath, common.SeqNoFileName)

	// 2. 以标准文件 IO 方式打开。
	return newDatafile(filePath, 0, fio.StandardFIO)
}

// GetDataFileName 根据目录路径和文件 ID 生成标准数据文件的完整路径。
// 此函数不执行任何 IO 操作，仅用于路径拼接。
func GetDataFileName(dirPath string, fileID uint32) string {
	return filepath.Join(dirPath, fmt.Sprintf("%09d", fileID)+common.DataFileSuffix)
}

// WriteHintRecord 将一条 hint 记录写入 hint 文件，供 merge 后快速重建索引。
// 它复用现有的日志编码格式：以 key 为记录键，以编码后的 LogRecordPos 为记录值。
func (df *DataFile) WriteHintRecord(key []byte, pos *LogRecordPos) error {
	// 1. 将索引位置信息编码成字节序列，作为日志记录的 value。
	record := &LogRecord{
		Key:   key,
		Value: EncodeLogRecordPos(pos),
		Type:  LogRecordNormal,
	}

	// 2. 使用标准日志编码函数生成完整的落盘字节序列。
	encRecord, _ := EncodeLogRecord(record)

	// 3. 追加写入 hint 文件。
	return df.Write(encRecord)
}

// newDatafile 是统一的数据文件构造入口。
// 它负责根据 ioType 选择底层 IO 实现，并把文件句柄包装成上层可用的 DataFile。
func newDatafile(filePath string, fileID uint32, ioType fio.FileIOType) (*DataFile, error) {
	// 1. 根据文件路径和 IO 类型创建底层 IOManager 实例。
	ioManager, err := fio.NewIOManager(filePath, ioType)
	if err != nil {
		return nil, err
	}

	// 2. 组装 DataFile 结构并返回，初始写入偏移为 0。
	return &DataFile{
		FileID:    fileID,
		WriteOff:  0,
		IoManager: ioManager,
	}, nil
}

// Sync 将当前 DataFile 的操作系统缓冲区显式刷盘（fsync）。
// 在事务提交或引擎关闭时调用，确保数据持久化到物理介质。
func (df *DataFile) Sync() error {
	return df.IoManager.Sync()
}

// Close 关闭当前 DataFile 持有的底层 IO 资源。
// 关闭后不应再对此 DataFile 执行任何读写操作。
func (df *DataFile) Close() error {
	return df.IoManager.Close()
}

// Write 以追加写方式把一段字节写入数据文件，并同步推进 WriteOff。
// WriteOff 始终表示"下一次写入应该落到的偏移位置"。
func (df *DataFile) Write(p []byte) error {
	// 1. 调用底层 IOManager 执行实际写入。
	n, err := df.IoManager.Write(p)
	if err != nil {
		return err
	}

	// 2. 将写入偏移向前推进实际写入的字节数。
	df.WriteOff += int64(n)
	return nil
}

// ReadLogRecord 从指定偏移读取并解析一条完整的日志记录。
// 返回值包括：解析后的 LogRecord、该记录占用的总字节数（用于推进偏移）以及可能的错误。
// 此方法会对所有记录执行 CRC 校验，确保返回的数据未被损坏。
func (df *DataFile) ReadLogRecord(off int64) (*LogRecord, int64, error) {
	// 1. 获取文件总大小，避免偏移越界或读过文件尾部。
	size, err := df.IoManager.Size()
	if err != nil {
		return nil, 0, err
	}
	if off >= size {
		return nil, 0, io.EOF
	}

	// 2. 确定本次需要读取的头部字节数；如果剩余数据不足最大头部长度，则只读剩余部分。
	var headerBytes int64 = maxLogRecordHeaderSize
	if off+maxLogRecordHeaderSize > size {
		headerBytes = size - off
	}

	// 3. 从文件中读取头部原始字节。
	headerBuf, err := df.readNBytes(headerBytes, off)
	if err != nil {
		return nil, 0, err
	}

	// 4. 解析头部，获取 CRC、记录类型以及 key/value 的长度。
	header, headerSize := decodeLogRecord(headerBuf)
	if header == nil {
		return nil, 0, fmt.Errorf("failed to decode log record header at offset %d", off)
	}

	// 5. 如果头部全为零值，说明已到达文件有效数据的末尾。
	if header.crc == 0 && header.keySize == 0 && header.valueSize == 0 {
		return nil, 0, io.EOF
	}

	// 6. 计算整条记录的物理长度：头部 + key + value。
	keySize, valueSize := int64(header.keySize), int64(header.valueSize)
	var recordSize = headerSize + keySize + valueSize

	// 7. 根据 key/value 长度决定是否需要继续读取载荷区，并构造 LogRecord。
	var logRecord *LogRecord
	if keySize > 0 || valueSize > 0 {
		// 7a. 读取 key + value 载荷区的原始字节。
		kvBuf, err := df.readNBytes(keySize+valueSize, off+headerSize)
		if err != nil {
			return nil, 0, err
		}

		// 7b. 按 key/value 长度将载荷区切分，组装成 LogRecord。
		logRecord = &LogRecord{
			Key:   kvBuf[:keySize],
			Value: kvBuf[keySize:],
			Type:  header.recordType,
		}
	} else {
		// 7c. 空载荷记录（如事务结束标记），只需记录类型。
		logRecord = &LogRecord{
			Type: header.recordType,
		}
	}

	// 8. 对所有记录执行 CRC 校验，确保数据完整性。
	crc := getLogRecordCRC(logRecord, headerBuf[crc32.Size:headerSize])
	if crc != header.crc {
		return nil, 0, common.ErrInvalidCRC
	}

	// 9. 校验通过，返回解析后的记录和记录总长度。
	return logRecord, recordSize, nil
}

// readNBytes 是对底层 ReadAt 的薄封装。
// 给定偏移和长度后，返回该物理区间的原始字节切片。
func (df *DataFile) readNBytes(n int64, offset int64) (b []byte, err error) {
	// 1. 分配指定长度的缓冲区。
	b = make([]byte, n)

	// 2. 调用底层 IOManager 从指定偏移读取数据。
	_, err = df.IoManager.ReadAt(b, offset)
	return
}

// SetIOManager 关闭当前 IO 实现并按同一文件路径重新打开另一种 IO 实现。
// 典型场景：启动恢复阶段使用 mmap 加速读取，恢复完成后切回标准文件 IO 以支持写入。
func (df *DataFile) SetIOManager(dirPath string, ioType fio.FileIOType) error {
	// 1. 先关闭当前的 IOManager，释放文件句柄。
	if err := df.IoManager.Close(); err != nil {
		return err
	}

	// 2. 根据新的 IO 类型重新打开同一数据文件。
	ioManager, err := fio.NewIOManager(GetDataFileName(dirPath, df.FileID), ioType)
	if err != nil {
		return err
	}

	// 3. 替换为新的 IOManager。
	df.IoManager = ioManager
	return nil
}
