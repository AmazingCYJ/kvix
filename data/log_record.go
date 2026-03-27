package data

import (
	"encoding/binary"
	"hash/crc32"
)

type LogRecordType = byte

const (
	LogRecordNormal LogRecordType = iota
	LogRecordDeleted
	LogRecordTxnFinished
)

// LogRecordPos 数据内存索引,描述数据在磁盘的位置
type LogRecordPos struct {
	Fid    uint32 // 文件id
	Offset int64  // 文件内偏移
	Size   uint32 // 记录的总大小（包括头部和数据），用于合并时计算废弃数据的大小
}

// LogRecord 数据记录
type LogRecord struct {
	Key   []byte
	Value []byte
	Type  LogRecordType
}

// LogRecordHeader 日志记录头部信息
type LogRecordHeader struct {
	crc        uint32        // 数据校验码
	recordType LogRecordType // 记录类型
	keySize    uint32        // key的长度
	valueSize  uint32        // value的长度
}

// TransactionRecord 事务记录，包含一个 LogRecord 和它在数据文件中的位置（LogRecordPos）。
type TransactionRecord struct {
	Record *LogRecord
	Pos    *LogRecordPos
}

// EncodeLogRecord 将 LogRecord 序列化为字节数组，并返回序列化后的字节数组和记录的总大小（包括头部和数据）。
// +------------------+-----------------+-----------------+-----------------+-----------------+-----------------+
// | CRC (4 bytes)    | Record Type (1 byte) | Key Size ( 5 bytes) | Value Size (5  bytes) | Key (variable)  | Value (variable) |
// +------------------+-----------------+-----------------+-----------------+-----------------+-----------------+
func EncodeLogRecord(logRecord *LogRecord) ([]byte, int64) {
	header := make([]byte, maxLogRecordHeardSize) // 4 bytes for CRC, 1 byte for Record Type, 5 bytes for Key Size, 5 bytes for Value Siz
	// 第5个字节是type
	header[4] = logRecord.Type
	var index = 5
	// 接下来是Key Size，使用binary.PutVarint编码为可变长度整数
	index += binary.PutVarint(header[index:], int64(len(logRecord.Key)))
	// 紧接着是Value Size
	index += binary.PutVarint(header[index:], int64(len(logRecord.Value)))
	var size = len(logRecord.Key) + len(logRecord.Value) + index

	encBytes := make([]byte, size)
	// crc 和 type 拷贝
	copy(encBytes[:index], header[:index])
	// key 和 value 拷贝
	copy(encBytes[index:], logRecord.Key)
	copy(encBytes[index+len(logRecord.Key):], logRecord.Value)

	// 计算CRC并将其写入头部的前4个字节
	crc := crc32.ChecksumIEEE(encBytes[4:])
	binary.BigEndian.PutUint32(encBytes[:4], crc)

	return encBytes, int64(size)
}

// decodeLogRecord 从字节数组中解析出 LogRecordHeader，并返回解析出的 LogRecordHeader 和头部的总大小（包括 CRC、recordType、keySize 和 valueSize）。
func decodeLogRecord(data []byte) (*LogRecordHeader, int64) {
	if len(data) <= 4 {
		return nil, 0
	}
	header := &LogRecordHeader{
		crc:        binary.BigEndian.Uint32(data[:4]),
		recordType: data[4],
	}
	var index = 5
	// 解析 Key Size
	keySize, n := binary.Varint(data[index:])
	header.keySize = uint32(keySize)
	index += n
	// 解析 Value Size
	valueSize, n := binary.Varint(data[index:])
	header.valueSize = uint32(valueSize)
	index += n
	return header, int64(index)
}

// getLogRecordCRC 计算 LogRecord 的 CRC 校验码，参数 logRecord 是要计算 CRC 的日志记录，header 是日志记录头部信息的字节数组。
func getLogRecordCRC(logRecord *LogRecord, header []byte) uint32 {
	if logRecord == nil {
		return 0
	}
	crc := crc32.ChecksumIEEE(header[:])
	crc = crc32.Update(crc, crc32.IEEETable, logRecord.Key)
	crc = crc32.Update(crc, crc32.IEEETable, logRecord.Value)
	return crc
}

// EncodeLogRecordPos 对位置索引进行编码
func EncodeLogRecordPos(pos *LogRecordPos) []byte {
	buf := make([]byte, binary.MaxVarintLen32*2+binary.MaxVarintLen64) // 4 bytes for Fid and 8 bytes for Offset
	var index = 0
	index += binary.PutVarint(buf[index:], int64(pos.Fid))
	index += binary.PutVarint(buf[index:], pos.Offset)
	index += binary.PutVarint(buf[index:], int64(pos.Size))
	return buf[:index]
}

// DecodeLogRecordPos 对位置索引进行解码
func DecodeLogRecordPos(buf []byte) *LogRecordPos {
	var index = 0
	fileId, n := binary.Varint(buf[index:])
	index += n
	offset, n := binary.Varint(buf[index:])
	index += n
	size, _ := binary.Varint(buf[index:])
	return &LogRecordPos{
		Fid:    uint32(fileId),
		Offset: offset,
		Size:   uint32(size),
	}
}
