package data

import (
	"encoding/binary"
	"hash/crc32"
)

type LogRecordType = byte

const (
	// LogRecordNormal 表示一条普通的数据写入记录。
	LogRecordNormal LogRecordType = iota
	// LogRecordDeleted 表示逻辑删除标记，读取到它时应把 key 视为不存在。
	LogRecordDeleted
	// LogRecordTxnFinished 表示一个批量事务已经完整写入结束。
	LogRecordTxnFinished
)

// LogRecordPos 描述一条日志记录在磁盘上的物理位置。
// 索引层保存的不是 value 本体，而是这个结构；读取 value 时再通过它回到具体数据文件定位。
type LogRecordPos struct {
	Fid    uint32 // 文件id
	Offset int64  // 文件内偏移
	Size   uint32 // 记录的总大小（包括头部和数据），用于合并时计算废弃数据的大小
}

// LogRecord 表示一条逻辑上的 KV 记录。
// 对根包来说，一次 Put/Delete/事务结束标记最终都会先转换成这个结构，再写入数据文件。
type LogRecord struct {
	Key   []byte
	Value []byte
	Type  LogRecordType
}

// LogRecordHeader 表示落盘记录头部的解析结果。
// 头部只保存解码后必须先知道的信息：CRC、记录类型以及 key/value 长度。
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

// EncodeLogRecord 将一条逻辑记录编码成可以直接追加写入数据文件的字节序列。
// +------------------+-----------------+-----------------+-----------------+-----------------+-----------------+
// | CRC (4 bytes)    | Record Type (1 byte) | Key Size ( 5 bytes) | Value Size (5  bytes) | Key (variable)  | Value (variable) |
// +------------------+-----------------+-----------------+-----------------+-----------------+-----------------+
func EncodeLogRecord(logRecord *LogRecord) ([]byte, int64) {
	// 1. 先准备一个“足够大”的头部缓冲区，后续再把实际用到的部分裁剪出来。
	header := make([]byte, maxLogRecordHeardSize) // 4 bytes for CRC, 1 byte for Record Type, 5 bytes for Key Size, 5 bytes for Value Siz
	// 2. 第 5 个字节固定保存记录类型。
	header[4] = logRecord.Type
	var index = 5
	// 3. 把 key/value 长度编码到头部里，使用变长整数可以避免固定长度带来的浪费。
	index += binary.PutVarint(header[index:], int64(len(logRecord.Key)))
	index += binary.PutVarint(header[index:], int64(len(logRecord.Value)))
	var size = len(logRecord.Key) + len(logRecord.Value) + index

	encBytes := make([]byte, size)
	// 4. 先拷贝头部，再拷贝 key 和 value 的实际内容。
	copy(encBytes[:index], header[:index])
	copy(encBytes[index:], logRecord.Key)
	copy(encBytes[index+len(logRecord.Key):], logRecord.Value)

	// 5. 最后基于“除 CRC 本身以外的全部内容”计算校验和，并写回前 4 个字节。
	crc := crc32.ChecksumIEEE(encBytes[4:])
	binary.BigEndian.PutUint32(encBytes[:4], crc)

	return encBytes, int64(size)
}

// decodeLogRecord 只负责解析记录头，不读取后面的 key/value 载荷区。
// 调用方通常会先用它拿到 key/value 长度，再决定要继续读取多少字节。
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

// getLogRecordCRC 基于“头部有效区 + key + value”重新计算 CRC。
// 读取记录后会用它和记录头部中的 CRC 做比对，以确认数据没有损坏。
func getLogRecordCRC(logRecord *LogRecord, header []byte) uint32 {
	if logRecord == nil {
		return 0
	}
	crc := crc32.ChecksumIEEE(header[:])
	crc = crc32.Update(crc, crc32.IEEETable, logRecord.Key)
	crc = crc32.Update(crc, crc32.IEEETable, logRecord.Value)
	return crc
}

// EncodeLogRecordPos 把位置结构编码成紧凑字节序列，便于存进 Hint 文件或持久化索引。
func EncodeLogRecordPos(pos *LogRecordPos) []byte {
	buf := make([]byte, binary.MaxVarintLen32*2+binary.MaxVarintLen64) // 4 bytes for Fid and 8 bytes for Offset
	var index = 0
	index += binary.PutVarint(buf[index:], int64(pos.Fid))
	index += binary.PutVarint(buf[index:], pos.Offset)
	index += binary.PutVarint(buf[index:], int64(pos.Size))
	return buf[:index]
}

// DecodeLogRecordPos 把紧凑字节序列还原成位置结构。
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
