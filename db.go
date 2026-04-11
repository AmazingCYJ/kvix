package kvix

import (
	"kvix/common"
	"kvix/data"
	"kvix/index"
	"sync"

	"github.com/gofrs/flock"
)

const (
	// seqNoKey 是事务序列号文件里使用的固定键名。
	// 数据库关闭时会把当前最大的事务序列号以这条记录形式写入 seq-no 文件，
	// 下次启动时据此恢复事务编号的连续性。
	seqNoKey = "seq.no"

	// fileLockName 是数据库目录级锁文件名。
	// 只要这个锁被某个进程持有，其他进程就不能再打开同一目录，
	// 从而避免多进程并发写入导致数据文件状态混乱。
	fileLockName = "flock"
)

// DB 表示一个打开中的 kvix 数据库实例。
//
// 它是整个系统的核心协调者，负责管理数据文件集合、内存索引、事务序列号和磁盘状态。
// 所有对外暴露的读写操作最终都通过 DB 完成"日志追加 + 索引更新 + 状态维护"的协调。
//
// 并发安全性通过 mu 读写锁保证：写路径（Put/Delete/Commit）持有写锁，
// 读路径（Get/ListKeys/Fold）持有读锁。
type DB struct {
	options        common.Options            // 数据库启动配置，决定目录、索引类型和落盘策略。
	mu             *sync.RWMutex             // 保护活跃文件、旧文件、索引和统计状态的读写锁。
	fileIds        []int                     // 已发现的数据文件 ID，仅在启动阶段重建索引时使用。
	activeFile     *data.DataFile            // 当前追加写入的数据文件。
	oldFiles       map[uint32]*data.DataFile // 已封存的只读数据文件，按文件 ID 索引。
	index          index.Indexer             // 内存索引，负责把 key 映射到磁盘上的数据位置。
	seqNo          uint64                    // 最近一次使用的事务序列号，用于恢复事务边界。
	isMerging      bool                      // 标记是否正在执行 merge，避免并发 merge 冲突。
	seqNoFileExist bool                      // 标记事务序列号文件是否存在，供启动恢复流程判断。
	isInitial      bool                      // 标记当前目录是否为空库首次初始化。
	fileLock       *flock.Flock              // 目录级文件锁，确保同一目录只被一个进程打开。
	bytesWrite     uint                      // 自上次 Sync 以来累计写入的字节数，用于批量落盘控制。
	reclaimSize    int64                     // 已被覆盖或删除的无效数据总量，用于统计和触发 merge。
}

// Stat 描述数据库当前的关键运行统计快照。
//
// 这些统计用于观察索引规模、数据文件数量、可回收空间以及目录磁盘占用。
// Stat 是调用时刻的快照，不会持续跟随后续写入自动更新。
type Stat struct {
	// KeyNum 是内存索引中当前有效的 key 数量。
	KeyNum uint
	// DataFileNum 是当前目录中正在使用的数据文件数量（包括活跃文件和旧文件）。
	DataFileNum uint
	// ReclaimableSize 是已失效、后续可以通过 merge 回收的字节数。
	ReclaimableSize int64
	// DiskSize 是数据目录当前占用的总磁盘大小。
	DiskSize int64
}
