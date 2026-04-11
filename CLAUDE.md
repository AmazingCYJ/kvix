# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

kvix is a Bitcask-style embedded key-value storage engine written in Go. It features append-only data files, pluggable in-memory/persistent indexing, atomic batch writes, iterators, an internal merge/compaction flow, a Redis-style data structure layer built on top of the KV engine, and an HTTP example service using Fiber.

All code comments, README files, and documentation are written in Chinese (中文). Maintain this convention when adding or modifying documentation.

## Build & Test Commands

```bash
# All tests (use local cache to avoid polluting global GOCACHE)
GOCACHE=$(pwd)/.cache/go go test ./... -count=1

# Single package tests
GOCACHE=$(pwd)/.cache/go go test . -count=1           # root package (DB core)
GOCACHE=$(pwd)/.cache/go go test ./index -count=1      # index implementations
GOCACHE=$(pwd)/.cache/go go test ./redis -count=1      # Redis-style structures
GOCACHE=$(pwd)/.cache/go go test ./http -count=1       # HTTP handlers
GOCACHE=$(pwd)/.cache/go go test ./data -count=1       # data file / log records
GOCACHE=$(pwd)/.cache/go go test ./fio -count=1        # file IO
GOCACHE=$(pwd)/.cache/go go test ./utils -count=1      # utility functions

# Benchmarks
GOCACHE=$(pwd)/.cache/go go test ./benchmark -bench . -benchmem

# Format all Go files
find . -name '*.go' -not -path './.cache/*' -print0 | xargs -0 gofmt -w

# Run HTTP example server
go run ./http
# With custom address: KVIX_HTTP_ADDR=127.0.0.1:9090 go run ./http
# With persistent data dir: KVIX_HTTP_DATA_DIR=/path/to/data go run ./http
```

## Architecture

```
Caller / Redis layer / HTTP example
            |
            v
         kvix.DB
  (read/write coordination, file state, stats, recovery)
            |
    +-------+--------+
    |                |
    v                v
 Indexer         Data Files
 (key -> pos)    (append-only log records)
```

### Core layers

- **Root package (`kvix`)**: DB entry point — Open/Close, Put/Get/Delete, WriteBatch, Iterator, Merge, Stat, Backup. Split across `db.go` (types), `db_open.go`, `db_read.go`, `db_write.go`, `db_state.go`, `db_files.go`, `batch.go`, `iterator.go`, `merge.go`.
- **`common/`**: Shared config (`Options`, `IteratorOptions`, `WriteBatchOptions`), error definitions, index type constants (`IndexerType`), file name constants.
- **`data/`**: Log record encoding/decoding with CRC validation, DataFile read/write/sync, LogRecordPos (Fid + Offset + Size).
- **`fio/`**: IO abstraction (`IOManager` interface) with standard file IO and mmap implementations. Mmap is used at startup for faster recovery reads.
- **`index/`**: Unified `Indexer` interface with three implementations — BTree, ART, B+Tree. `NewIndexer` returns `(Indexer, error)`. B+Tree persists to disk via bbolt; the others are in-memory.
- **`redis/`**: Redis-style data structures (string, hash, list, set, zset) built on `kvix.DB`. Uses metadata + version + sub-key encoding + lazy expiration. Go API, not Redis protocol.
- **`http/`**: Fiber-based HTTP example. Standalone `main` package.
- **`utils/`**: File system helpers (directory size, disk free space, directory copy).
- **`examples/`**: Minimal usage examples.
- **`benchmark/`**: Performance benchmarks for Put/Get operations.

### Key design patterns

- **Append-only writes**: Put/Delete both append new log records; old records become reclaimable space.
- **Index stores positions, not values**: The index maps keys to `LogRecordPos{Fid, Offset, Size}`. Reads go through index then fetch from the data file.
- **WriteBatch atomicity**: Each batch gets a unique sequence number; a completion marker is appended last. Recovery uses this to identify complete transactions.
- **Lazy expiration** (Redis layer): TTL is checked on access; expired metadata is deleted but old sub-keys wait for merge cleanup.
- **Version-based deletion** (Redis layer): Complex structures use version numbers so deletion doesn't require immediate cleanup of all sub-keys.
- **Directory-level file lock** (`gofrs/flock`): Prevents multiple processes from opening the same data directory.

### API notes

- `Stat()` returns `(*Stat, error)` — callers must handle the error.
- `Close()` returns `error` — file lock release failure is returned, not panicked.
- `NewIndexer()` returns `(Indexer, error)` — unsupported index types return error instead of panicking.
- `WriteBatch.Delete()` returns `nil` for non-existent keys (idempotent, consistent with `DB.Delete`).
- `IndexerType`, `LogRecordType`, `FileIOType` are defined types (not aliases) for compile-time safety.

## Key Dependencies

- `github.com/google/btree` — BTree index
- `github.com/plar/go-adaptive-radix-tree` — ART index
- `go.etcd.io/bbolt` — B+Tree persistent index
- `github.com/gofiber/fiber/v2` — HTTP framework
- `github.com/gofrs/flock` — Directory-level file locking
- Go 1.25

## Conventions

- All non-test files use qualified imports (`"kvix/common"`, not dot imports). Use `common.Options`, `common.ErrKeyNotFound`, etc.
- Tests create temporary directories and clean up after themselves.
- The `GOCACHE` env var is set to a local `.cache/go` directory to isolate test cache from the global Go cache.
- The `.cache/` directory is gitignored and used for build/test caches.
- The `.worktrees/` directory is gitignored and used for git worktree-based parallel development.
- Error comparison uses `errors.Is()`, not `==`.
- All comments and documentation are in Chinese. Error messages in `common/errors.go` are in English.
