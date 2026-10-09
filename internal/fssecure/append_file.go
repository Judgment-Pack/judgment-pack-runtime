package fssecure

import (
	"errors"
	"os"
)

// AppendFile retains an append handle without holding a lock between appends.
// Unlike AppendLocked, it never follows a replacement of the file's name.
// The root must stay open until Close has been called.
type AppendFile struct {
	root *Root
	name string
	file *os.File
}

// OpenAppend opens or creates a file with Append's containment and link checks.
func (r *Root) OpenAppend(relative string) (*AppendFile, error) {
	cleaned, err := Relative(relative)
	if err != nil {
		return nil, err
	}
	file, err := r.openForAppend(cleaned, os.O_RDWR)
	if err != nil {
		return nil, err
	}
	info, err := r.sameRegularFile(file, cleaned)
	if err == nil && hardLinked(info) {
		err = errors.New("path has more than one link, so it names a file something else also names")
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return &AppendFile{root: r, name: cleaned, file: file}, nil
}

func (f *AppendFile) Close() error                            { return f.file.Close() }
func (f *AppendFile) Stat() (os.FileInfo, error)              { return f.file.Stat() }
func (f *AppendFile) ReadAt(p []byte, off int64) (int, error) { return f.file.ReadAt(p, off) }

// AppendLocked locks only for compose, append and sync, with the same lock
// fallback and durability rules as Root.AppendLocked. A moved file stays the
// target; callers must refuse a stale transaction inside compose if needed.
func (f *AppendFile) AppendLocked(compose func(AppendState) ([]byte, error)) error {
	locked := true
	if err := lockExclusive(f.file); errors.Is(err, errNoLock) {
		locked = false
	} else if err != nil {
		return err
	}
	if locked {
		defer unlockFile(f.file)
	}
	return f.root.appendHeld(f.file, f.name, locked, compose)
}
