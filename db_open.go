package kvix

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"kvix/common"
	"kvix/data"
	"kvix/index"

	"github.com/gofrs/flock"
)

// Open 打开或创建一个 kvix 数据库实例，并在返回前恢复可用的运行状态。
//
// 启动流程包括：校验配置、创建数据目录、获取目录文件锁、加载 merge 遗留文件、
// 加载数据文件、重建内存索引。如果使用 B+Tree 索引，则跳过内存索引重建，
// 只恢复事务序列号并校正活跃文件写偏移。
//
// 参数:
//   - options: 数据库启动配置，包含目录路径、索引类型、同步策略等。
//
// 返回:
//   - *DB: 初始化完成后可直接读写的数据库句柄。
//   - error: 目录不可访问、文件锁冲突或索引恢复失败时返回错误。
func Open(options common.Options) (*DB, error) {
	// 1. 先校验配置，避免带着无效目录或文件大小继续执行启动流程。
	if err := checkOptions(options); err != nil {
		return nil, err
	}
	var isInitial bool

	// 2. 准备数据目录；目录不存在时需要先创建，并标记这次启动属于空库初始化。
	if _, err := os.Stat(options.DirPath); os.IsNotExist(err) {
		isInitial = true
		if err := os.MkdirAll(options.DirPath, os.ModePerm); err != nil {
			return nil, err
		}
	}

	// 2.1 获取目录级文件锁，阻止多个进程同时写同一个数据库目录导致文件状态混乱。
	fileLock := flock.New(filepath.Join(options.DirPath, fileLockName))
	hold, err := fileLock.TryLock()
	if err != nil {
		// 锁文件本身访问失败时，说明目录权限或文件状态已经不正常，直接终止启动。
		return nil, err
	}
	if !hold {
		// TryLock 返回 hold=false 代表别的进程已经持有这个目录锁。
		return nil, common.ErrDataBaseIsUsing
	}

	// 2.2 再次检查目录内容；空目录同样属于首次初始化场景，后续统计逻辑会依赖该标记。
	entries, err := os.ReadDir(options.DirPath)
	if err != nil {
		return nil, err
	}
	if isInitialDataDir(entries) {
		isInitial = true
	}

	// 3. 构造 DB 实例，准备好锁、旧文件集合和索引实现，后续加载流程都挂在这个实例上。
	idx, err := index.NewIndexer(options.IndexType, options.DirPath, options.SyncWrites)
	if err != nil {
		return nil, err
	}
	db := &DB{
		options:   options,
		mu:        &sync.RWMutex{},
		oldFiles:  make(map[uint32]*data.DataFile),
		index:     idx,
		isInitial: isInitial,
		fileLock:  fileLock,
	}

	// 3.1 先处理 merge 目录遗留文件，保证后续看到的数据文件集合已经是可恢复状态。
	if err := db.loadMergeFiles(); err != nil {
		return nil, err
	}

	// 4. 扫描并打开现有数据文件，确定活跃文件和旧文件集合。
	if err := db.loadDataFiles(); err != nil {
		return nil, err
	}

	// 5. 非 B+Tree 索引需要在启动时把 hint 和数据文件重新回放到内存索引里。
	if options.IndexType != common.BPlusTreeIndex {
		// 5.1 优先从 Hint 文件恢复索引，减少全量数据文件扫描成本。
		if err := db.loadIndexFromHintFile(); err != nil {
			return nil, err
		}
		// 5.2 再从数据文件中回放剩余记录，补全 Hint 文件未覆盖的部分。
		if err := db.loadIndexFromDataFiles(); err != nil {
			return nil, err
		}
		// 5.3 恢复完成后，如果启动阶段使用了 mmap，需要切回标准文件 IO。
		if db.options.MMapAtStartup {
			if err := db.resetIOType(); err != nil {
				return nil, err
			}
		}
	}

	// 6. B+Tree 索引自身会持久化索引结构，这里只需恢复事务序列号并校正活跃文件写偏移。
	if options.IndexType == common.BPlusTreeIndex {
		if err := db.loadSeqNo(); err != nil {
			return nil, err
		}
		if db.options.MMapAtStartup {
			// B+Tree 虽然不需要重放内存索引，但如果启动时用 mmap 打开了数据文件，
			// 恢复结束后仍然必须切回标准文件 IO，否则后续写入会直接失败。
			if err := db.resetIOType(); err != nil {
				return nil, err
			}
		}
		if db.activeFile != nil {
			// B+Tree 模式下索引不需要重放日志来恢复，但活跃文件下一次写入位置仍然要校正到文件末尾。
			size, err := db.activeFile.IoManager.Size()
			if err != nil {
				return nil, err
			}
			db.activeFile.WriteOff = size
		}
	}

	return db, nil
}

// checkOptions 校验打开数据库所需的最小配置，避免启动阶段出现明显非法输入。
func checkOptions(options common.Options) error {
	if options.DirPath == "" {
		// 没有目录就无法定位数据文件、索引文件和锁文件。
		return errors.New("data directory path cannot be empty")
	}
	if options.DataFileSize <= 0 {
		// 数据文件大小必须大于 0，否则活跃文件永远无法容纳任何记录。
		return errors.New("data file size must be greater than zero")
	}
	return nil
}

// isInitialDataDir 判断当前目录是否仍然可以视为"空库首次初始化"。
// 这里要忽略运行时自动创建的 flock 锁文件，否则一个刚创建的空目录也会被误判成"已有历史状态"。
func isInitialDataDir(entries []os.DirEntry) bool {
	if len(entries) == 0 {
		return true
	}
	for _, entry := range entries {
		if entry.Name() == fileLockName {
			continue
		}
		return false
	}
	return true
}
