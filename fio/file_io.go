package fio

import "os"

type FileIO struct {
	fd *os.File
}

func NewFileIO(filePath string) (*FileIO, error) {
	fd, err := os.OpenFile(
		filePath,
		os.O_RDWR|os.O_CREATE,
		DataFilePerm,
	)
	if err != nil {
		return nil, err
	}
	return &FileIO{fd: fd}, nil
}

func (f *FileIO) ReadAt(p []byte, off int64) (n int, err error) {
	return f.fd.ReadAt(p, off)
}

func (f *FileIO) Write(p []byte) (n int, err error) {
	return f.fd.Write(p)
}

func (f *FileIO) Sync() error {
	return f.fd.Sync()
}

func (f *FileIO) Close() error {
	return f.fd.Close()
}

func (f *FileIO) Size() (int64, error) {
	stat, err := f.fd.Stat()
	if err != nil {
		return 0, err
	}
	return stat.Size(), nil
}
