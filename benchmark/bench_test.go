package benchmark

import (
	"encoding/binary"
	kvix "kvix"
	"kvix/common"
	"testing"
)

var benchmarkValueSink []byte

const benchmarkNumKeys = 64 * 1024
const benchmarkDataFileSize = 256 * 1024 * 1024
const benchmarkDataFileMergeRatio = 0.5

type benchmarkCase struct {
	name      string
	valueSize int
}

var benchmarkCases = []benchmarkCase{
	{name: "128B", valueSize: 128},
	{name: "1KB", valueSize: 1024},
	{name: "4KB", valueSize: 4096},
}

func benchmarkOptions(dir string) common.Options {
	// 显式固定 benchmark 配置，避免不同运行之间的结果语义漂移。
	return common.Options{
		DirPath:            dir,
		DataFileSize:       benchmarkDataFileSize,
		SyncWrites:         false,
		BytesPerSync:       0,
		IndexType:          common.BPlusTreeIndex,
		MMapAtStartup:      false,
		DataFileMergeRatio: benchmarkDataFileMergeRatio,
	}
}

func openBenchmarkDB(b *testing.B) (*kvix.DB, string) {
	b.Helper()
	// 使用 b.TempDir 为每个子基准隔离目录，避免文件锁和目录状态互相污染。
	dir := b.TempDir()

	db, err := kvix.Open(benchmarkOptions(dir))
	if err != nil {
		b.Fatalf("打开 benchmark db 失败: %v", err)
	}
	return db, dir
}

func reopenBenchmarkDB(b *testing.B, dir string) *kvix.DB {
	b.Helper()

	db, err := kvix.Open(benchmarkOptions(dir))
	if err != nil {
		b.Fatalf("重新打开 benchmark db 失败: %v", err)
	}
	return db
}

func mustCloseBenchmarkDB(b *testing.B, db *kvix.DB) {
	b.Helper()
	if err := db.Close(); err != nil {
		b.Fatalf("关闭 benchmark db 失败: %v", err)
	}
}

func makeBenchmarkValue(size int) []byte {
	if size <= 0 {
		return nil
	}

	value := make([]byte, size)
	for i := range value {
		value[i] = byte('a' + (i % 26))
	}
	return value
}

func makeBenchmarkKeys(n int) [][]byte {
	if n <= 0 {
		return nil
	}

	keys := make([][]byte, n)
	backing := make([]byte, n*9)
	for i := 0; i < n; i++ {
		offset := i * 9
		backing[offset] = 'k'
		binary.BigEndian.PutUint64(backing[offset+1:offset+9], uint64(i))
		keys[i] = backing[offset : offset+9]
	}
	return keys
}

func seedBenchmarkData(b *testing.B, db *kvix.DB, keys [][]byte, value []byte) {
	b.Helper()
	for i := range keys {
		if err := db.Put(keys[i], value); err != nil {
			b.Fatalf("预写入 benchmark 数据失败: %v", err)
		}
	}
}

func BenchmarkKvixPut(b *testing.B) {
	for _, tc := range benchmarkCases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			db, _ := openBenchmarkDB(b)
			value := makeBenchmarkValue(tc.valueSize)
			keys := makeBenchmarkKeys(b.N)

			b.ReportAllocs()
			b.SetBytes(int64(tc.valueSize))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				if err := db.Put(keys[i], value); err != nil {
					b.Fatalf("put 失败: %v", err)
				}
			}

			b.StopTimer()
			mustCloseBenchmarkDB(b, db)
		})
	}
}

func BenchmarkKvixGet(b *testing.B) {
	for _, tc := range benchmarkCases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			db, _ := openBenchmarkDB(b)
			value := makeBenchmarkValue(tc.valueSize)
			keys := makeBenchmarkKeys(benchmarkNumKeys)

			// 数据准备放在计时区间外，避免把预写入成本算进 Get 吞吐。
			seedBenchmarkData(b, db, keys, value)

			b.ReportAllocs()
			b.SetBytes(int64(tc.valueSize))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				got, err := db.Get(keys[i%benchmarkNumKeys])
				if err != nil {
					b.Fatalf("get 失败: %v", err)
				}
				if len(got) != tc.valueSize {
					b.Fatalf("get 结果长度不匹配: got=%d want=%d", len(got), tc.valueSize)
				}
				benchmarkValueSink = got
			}

			b.StopTimer()
			mustCloseBenchmarkDB(b, db)
		})
	}
}

func BenchmarkKvixReadAfterLoad(b *testing.B) {
	for _, tc := range benchmarkCases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			db, dir := openBenchmarkDB(b)
			value := makeBenchmarkValue(tc.valueSize)
			keys := makeBenchmarkKeys(benchmarkNumKeys)

			// 这个基准显式覆盖 write -> close -> reopen -> read 路径。
			seedBenchmarkData(b, db, keys, value)
			mustCloseBenchmarkDB(b, db)

			db = reopenBenchmarkDB(b, dir)

			b.ReportAllocs()
			b.SetBytes(int64(tc.valueSize))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				got, err := db.Get(keys[i%benchmarkNumKeys])
				if err != nil {
					b.Fatalf("read-after-load get 失败: %v", err)
				}
				if len(got) != tc.valueSize {
					b.Fatalf("read-after-load 结果长度不匹配: got=%d want=%d", len(got), tc.valueSize)
				}
				benchmarkValueSink = got
			}

			b.StopTimer()
			mustCloseBenchmarkDB(b, db)
		})
	}
}
