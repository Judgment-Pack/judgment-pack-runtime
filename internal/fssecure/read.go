package fssecure

import (
	"errors"
	"io"
)

// ErrTooLarge reports that a read stopped because the input exceeded the
// caller's byte limit, rather than being missing or not a regular file. It lets
// a caller distinguish a size-limit failure from a generic read failure.
var ErrTooLarge = errors.New("input exceeds the byte limit")

func ReadRegular(filePath string, limit int64) ([]byte, error) {
	file, err := openRegular(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}
	if info.Size() > limit {
		return nil, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	return data, nil
}
