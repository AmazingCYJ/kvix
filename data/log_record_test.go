package data

import (
	"encoding/binary"
	"hash/crc32"
	"testing"
)

func TestEncodeLogRecord(t *testing.T) {
	tests := []struct {
		name      string
		record    *LogRecord
		wantKey   string
		wantValue string
	}{
		{
			name: "normal record",
			record: &LogRecord{
				Key:   []byte("name"),
				Value: []byte("bitcask"),
				Type:  LogRecordNormal,
			},
			wantKey:   "name",
			wantValue: "bitcask",
		},
		{
			name: "deleted record with empty value",
			record: &LogRecord{
				Key:   []byte("k1"),
				Value: []byte{},
				Type:  LogRecordDeleted,
			},
			wantKey:   "k1",
			wantValue: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, size := EncodeLogRecord(tt.record)
			if int64(len(enc)) != size {
				t.Fatalf("EncodeLogRecord() size = %d, len = %d", size, len(enc))
			}

			header, headerSize := decodeLogRecord(enc)
			if header == nil {
				t.Fatal("decodeLogRecord() got nil header")
			}
			if header.recordType != tt.record.Type {
				t.Fatalf("record type = %d, want %d", header.recordType, tt.record.Type)
			}
			if header.keySize != uint32(len(tt.record.Key)) {
				t.Fatalf("key size = %d, want %d", header.keySize, len(tt.record.Key))
			}
			if header.valueSize != uint32(len(tt.record.Value)) {
				t.Fatalf("value size = %d, want %d", header.valueSize, len(tt.record.Value))
			}

			gotKey := string(enc[headerSize : headerSize+int64(header.keySize)])
			gotValue := string(enc[headerSize+int64(header.keySize):])
			if gotKey != tt.wantKey {
				t.Fatalf("key = %q, want %q", gotKey, tt.wantKey)
			}
			if gotValue != tt.wantValue {
				t.Fatalf("value = %q, want %q", gotValue, tt.wantValue)
			}

			wantCRC := crc32.ChecksumIEEE(enc[4:])
			gotCRC := binary.BigEndian.Uint32(enc[:4])
			if gotCRC != wantCRC {
				t.Fatalf("crc = %d, want %d", gotCRC, wantCRC)
			}
		})
	}
}

func TestDecodeLogRecord(t *testing.T) {
	t.Run("too short returns nil", func(t *testing.T) {
		header, n := decodeLogRecord([]byte{1, 2, 3, 4})
		if header != nil || n != 0 {
			t.Fatalf("decodeLogRecord() = (%v, %d), want (nil, 0)", header, n)
		}
	})

	t.Run("decode encoded header", func(t *testing.T) {
		record := &LogRecord{Key: []byte("a"), Value: []byte("bcd"), Type: LogRecordNormal}
		enc, _ := EncodeLogRecord(record)
		header, n := decodeLogRecord(enc)
		if header == nil {
			t.Fatal("decodeLogRecord() got nil")
		}
		if n <= 0 {
			t.Fatalf("header size = %d, want > 0", n)
		}
		if header.keySize != 1 || header.valueSize != 3 {
			t.Fatalf("sizes = (%d,%d), want (1,3)", header.keySize, header.valueSize)
		}
	})
}

func TestGetLogRecordCRC(t *testing.T) {
	t.Run("nil record returns zero", func(t *testing.T) {
		got := getLogRecordCRC(nil, []byte{1, 2, 3})
		if got != 0 {
			t.Fatalf("getLogRecordCRC() = %d, want 0", got)
		}
	})

	t.Run("crc equals encoded crc", func(t *testing.T) {
		record := &LogRecord{Key: []byte("key"), Value: []byte("value"), Type: LogRecordNormal}
		enc, _ := EncodeLogRecord(record)
		header, headerSize := decodeLogRecord(enc)
		if header == nil {
			t.Fatal("decodeLogRecord() got nil")
		}

		got := getLogRecordCRC(record, enc[crc32.Size:headerSize])
		want := binary.BigEndian.Uint32(enc[:4])
		if got != want {
			t.Fatalf("getLogRecordCRC() = %d, want %d", got, want)
		}
	})
}
