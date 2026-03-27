package common

import "errors"

var (
	// ErrKeyNotFound 表示索引中不存在对应 key。
	ErrKeyNotFound            = errors.New("key not found")
	ErrIndexUpdateFailed      = errors.New("index update failed")
	ErrDataFileNotFound       = errors.New("data file not found")
	ErrDataDirectoryCorrupted = errors.New("data directory is corrupted")
	ErrInvlidCRC              = errors.New("invalid CRC, data may be corrupted")
	ErrBatchTooLarge          = errors.New("batch size exceeds the maximum limit")
	ErrMergeIsProgress        = errors.New("other file is merging,try again later")
	ErrDataBaseIsUsing        = errors.New("the database is using by other process")
	ErrMMapWriteNotSupported  = errors.New("mmap does not support write")
	ErrMergeRatioUnreached    = errors.New("merge ratio is unreached")
	ErrNoEnoughDiskSpace      = errors.New("not enough disk space for merge")
)
