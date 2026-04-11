package data

import (
	"encoding/binary"
	"hash/crc32"
)

// LogRecordType 表示日志记录的类型标识。
// 使用 defined type（而非类型别名）以获得更强的类型安全性，
// 防止调用方意外传入未定义的 byte 值。
type LogRecordType byte

const (
	// LogRecordNormal 表示一条普通的数据写入记录。
	// 当用户调用 Put(key, value) 时，引擎会生成此类型的记录并追加到活跃数据文件。
	LogRecordNormal LogRecordType = iota

	// LogRecordDeleted 表示逻辑删除标记（墓碑记录）。
	// 当用户调用 Delete(key) 时，引擎不会物理删除旧记录，
	// 而是追加一条此类型的记录；后续读取遇到它时应视 key 为不存在。
	LogRecordDeleted

	// LogRecordTxnFinished 表示一个批量事务已经完整写入结束。
	// 事务提交时，引擎会在所有事务记录之后追加此标记，
	// 恢复阶段据此判断事务是否完整——不完整的事务将被丢弃。
	LogRecordTxnFinished
)

// maxLogRecordHeaderSize 是单条日志记录头部可能占用的最大字节数。
// 组成：CRC(4 字节) + Type(1 字节) + KeySize(varint, 最多 5 字节) + ValueSize(varint, 最多 5 字节)。
// 实际头部通常远小于此值，但分配缓冲区时需要按最大值预留。
const maxLogRecordHeaderSize = 4 + 1 + binary.MaxVarintLen32 + binary.MaxVarintLen32

// LogRecordPos 描述一条日志记录在磁盘上的物理位置。
// 内存索引中保存的不是 value 本体，而是此结构；
// 读取 value 时通过它定位到具体的数据文件和偏移量，再执行一次磁盘 IO。
type LogRecordPos struct {
	Fid    uint32 // 数据文件 ID，对应磁盘上的 000000001.data 等文件
	Offset int64  // 记录在数据文件内的起始偏移（字节）
	Size   uint32 // 记录的总大小（头部 + key + value），用于 merge 时统计废弃数据量
}

// LogRecord 表示一条逻辑上的 KV 记录。
// 无论是 Put、Delete 还是事务结束标记，最终都会先转换成此结构，
// 再经过 EncodeLogRecord 编码后追加写入数据文件。
type LogRecord struct {
	Key   []byte        // 用户传入的原始 key（事务场景下会带上序列号前缀）
	Value []byte        // 用户传入的原始 value（删除记录和事务结束标记此字段为空）
	Type  LogRecordType // 记录类型，决定恢复阶段如何处理这条记录
}

// LogRecordHeader 表示落盘记录头部的解析结果。
// 头部包含解码后续载荷所必需的元信息：CRC 校验码、记录类型以及 key/value 的长度。
// 调用方通常先解析头部拿到长度信息，再决定继续读取多少字节的载荷。
type LogRecordHeader struct {
	crc        uint32        // CRC32 校验码，用于检测数据是否损坏
	recordType LogRecordType // 记录类型（Normal / Deleted / TxnFinished）
	keySize    uint32        // key 的字节长度
	valueSize  uint32        // value 的字节长度
}

// TransactionRecord 表示事务中的一条记录及其写入后的磁盘位置。
// 事务提交阶段会收集所有 TransactionRecord，统一更新内存索引。
type TransactionRecord struct {
	Record *LogRecord    // 逻辑记录内容
	Pos    *LogRecordPos // 记录写入数据文件后返回的物理位置
}

// EncodeLogRecord 将一条逻辑记录编码成可以直接追加写入数据文件的字节序列，
// 同时返回编码后的总字节数。
//
// 落盘格式如下：
// +----------------+------------------+---------------------+-----------------------+-----------------+-------------------+
// | CRC (4 bytes)  | Type (1 byte)    | KeySize (≤5 bytes)  | ValueSize (≤5 bytes)  | Key (variable)  | Value (variable)  |
// +----------------+------------------+---------------------+-----------------------+-----------------+-------------------+
//
// 其中 KeySize 和 ValueSize 使用 varint 编码以节省空间。
func EncodeLogRecord(logRecord *LogRecord) ([]byte, int64) {
	// 1. 分配一个按最大头部长度预留的缓冲区，后续裁剪到实际使用的长度。
	header := make([]byte, maxLogRecordHeaderSize)

	// 2. 第 5 个字节（索引 4）固定保存记录类型。
	header[4] = byte(logRecord.Type)

	// 3. 从索引 5 开始，依次用 varint 编码 key 长度和 value 长度。
	var index = 5
	index += binary.PutVarint(header[index:], int64(len(logRecord.Key)))
	index += binary.PutVarint(header[index:], int64(len(logRecord.Value)))

	// 4. 计算整条记录的总字节数：实际头部长度 + key 长度 + value 长度。
	var size = len(logRecord.Key) + len(logRecord.Value) + index

	// 5. 分配最终的编码缓冲区，依次拷贝头部、key、value。
	encBytes := make([]byte, size)
	copy(encBytes[:index], header[:index])
	copy(encBytes[index:], logRecord.Key)
	copy(encBytes[index+len(logRecord.Key):], logRecord.Value)

	// 6. 基于"除 CRC 本身以外的全部内容"计算 CRC32 校验和，写回前 4 个字节。
	crc := crc32.ChecksumIEEE(encBytes[4:])
	binary.BigEndian.PutUint32(encBytes[:4], crc)

	return encBytes, int64(size)
}

// decodeLogRecord 从原始字节中解析出记录头部信息。
// 它只负责解析头部，不读取后面的 key/value 载荷区。
// 返回解析后的头部结构和头部实际占用的字节数；
// 调用方据此决定还需要继续读取多少字节来获取完整的 key/value。
func decodeLogRecord(data []byte) (*LogRecordHeader, int64) {
	// 1. 至少需要 4 字节才能读取 CRC，否则数据不完整。
	if len(data) <= 4 {
		return nil, 0
	}

	// 2. 解析固定部分：前 4 字节为 CRC，第 5 字节为记录类型。
	header := &LogRecordHeader{
		crc:        binary.BigEndian.Uint32(data[:4]),
		recordType: LogRecordType(data[4]),
	}

	// 3. 从索引 5 开始，依次用 varint 解码 key 长度和 value 长度。
	var index = 5
	keySize, n := binary.Varint(data[index:])
	header.keySize = uint32(keySize)
	index += n

	valueSize, n := binary.Varint(data[index:])
	header.valueSize = uint32(valueSize)
	index += n

	// 4. 返回头部结构和头部实际占用的字节数。
	return header, int64(index)
}

// getLogRecordCRC 基于"头部有效区（不含 CRC 字段本身）+ key + value"重新计算 CRC32。
// 读取记录后会用此函数的返回值与记录头部中存储的 CRC 做比对，
// 若不一致则说明数据已损坏，应拒绝返回该记录。
func getLogRecordCRC(logRecord *LogRecord, header []byte) uint32 {
	// 1. 空记录直接返回 0，避免空指针。
	if logRecord == nil {
		return 0
	}

	// 2. 先对头部有效区（Type + KeySize + ValueSize 部分）计算初始 CRC。
	crc := crc32.ChecksumIEEE(header[:])

	// 3. 依次将 key 和 value 的内容追加到 CRC 计算中。
	crc = crc32.Update(crc, crc32.IEEETable, logRecord.Key)
	crc = crc32.Update(crc, crc32.IEEETable, logRecord.Value)

	return crc
}

// EncodeLogRecordPos 把位置结构编码成紧凑的字节序列。
// 编码结果可以作为 Hint 文件中记录的 value，也可用于其他需要持久化索引位置的场景。
// 三个字段均使用 varint 编码以最大限度节省空间。
func EncodeLogRecordPos(pos *LogRecordPos) []byte {
	// 1. 分配足够容纳三个 varint 的缓冲区。
	buf := make([]byte, binary.MaxVarintLen32*2+binary.MaxVarintLen64)

	// 2. 依次编码 Fid、Offset、Size。
	var index = 0
	index += binary.PutVarint(buf[index:], int64(pos.Fid))
	index += binary.PutVarint(buf[index:], pos.Offset)
	index += binary.PutVarint(buf[index:], int64(pos.Size))

	// 3. 裁剪到实际使用的长度后返回。
	return buf[:index]
}

// DecodeLogRecordPos 把紧凑字节序列还原成 LogRecordPos 结构。
// 它是 EncodeLogRecordPos 的逆操作，用于从 Hint 文件或持久化索引中恢复位置信息。
func DecodeLogRecordPos(buf []byte) *LogRecordPos {
	// 1. 依次解码 Fid、Offset、Size。
	var index = 0
	fileId, n := binary.Varint(buf[index:])
	index += n

	offset, n := binary.Varint(buf[index:])
	index += n

	size, _ := binary.Varint(buf[index:])

	// 2. 组装并返回位置结构。
	return &LogRecordPos{
		Fid:    uint32(fileId),
		Offset: offset,
		Size:   uint32(size),
	}
}
