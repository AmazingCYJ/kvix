package kvix

import (
	"kvix/common"
	"kvix/data"
	"kvix/index"
	"sync"

	"github.com/gofrs/flock"
)

const (
	seqNoKey     = "seq.no"
	fileLockName = "flock"
)

// DB 表示一个打开中的 kvix 数据库实例。
// 它负责协调数据文件、内存索引、事务序列号和磁盘状态，
// 并通过读写锁保证根包核心读写路径在并发场景下的访问顺序。
type DB struct {
	options        common.Options            // 数据库启动配置，决定目录、索引类型和落盘策略。
	mu             *sync.RWMutex             // 保护活跃文件、旧文件、索引和统计状态的读写锁。
	fileIds        []int                     // 已发现的数据文件 ID，仅在启动阶段重建索引时使用。
	activeFile     *data.DataFile            // 当前追加写入的数据文件。
	oldfiles       map[uint32]*data.DataFile // 已封存的数据文件，按文件 ID 保存。
	index          index.Indexer             // 内存索引，负责把 key 映射到磁盘上的数据位置。
	seqNo          uint64                    // 最近一次使用的事务序列号，用于恢复事务边界。
	isMerging      bool                      // 标记是否正在执行 merge，避免并发 merge 冲突。
	seqNoFileExist bool                      // 标记事务序列号文件是否存在，供启动恢复流程判断。
	isInitial      bool                      // 标记当前目录是否为空库首次初始化。
	fileLock       *flock.Flock              // 目录级文件锁，确保同一目录只被一个进程打开。
	bytesWrite     uint                      // 自上次 Sync 以来累计写入的字节数，用于批量落盘控制。
	reclaimSize    int64                     // 已被覆盖或删除的无效数据总量，用于统计和触发 merge。
}

// Stat 描述数据库当前的关键运行统计。
// 这些统计用于观察索引规模、数据文件数量、可回收空间以及目录磁盘占用，
// 不会修改数据库状态。
type Stat struct {
	KeyNum         uint  // 内存索引中的 key 数量。
	DataFileNum    uint  // 当前目录中正在使用的数据文件数量。
	ReclaimbleSize int64 // 已失效、后续可以通过 merge 回收的字节数。
	DiskSize       int64 // 数据目录当前占用的总磁盘大小。
}
