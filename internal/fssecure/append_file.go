package fssecure

import (
	"errors"
	"io/fs"
	"os"
)

// ErrHeldFileChanged refuses an append through an AppendFile whose name no
// longer names the held file alone: it was moved, removed or replaced, or
// another name was linked to it, after it was opened. Nothing is written.
var ErrHeldFileChanged = errors.New("the held file was moved, removed, replaced or linked elsewhere after it was opened")

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

// AppendLocked locks only for before, compose, append and sync, with the same
// lock fallback and durability rules as Root.AppendLocked. Under the lock it
// first calls before, when it is not nil, so a caller can refuse a stale
// transaction for its own reasons first; then it checks again that the name
// still names the held file and that no other name does, and refuses with
// ErrHeldFileChanged when either no longer holds, so bytes never go to a file
// the name has stopped naming. A name that could not be looked up for another
// reason is that failure, not ErrHeldFileChanged. Any refusal writes nothing.
func (f *AppendFile) AppendLocked(before func() error, compose func(AppendState) ([]byte, error)) error {
	locked := true
	if err := lockExclusive(f.file); errors.Is(err, errNoLock) {
		locked = false
	} else if err != nil {
		return err
	}
	if locked {
		defer unlockFile(f.file)
	}
	if before != nil {
		if err := before(); err != nil {
			return err
		}
	}
	if err := f.stillNamed(); err != nil {
		return err
	}
	return f.root.appendHeld(f.file, f.name, locked, compose)
}

// stillNamed is sameRegularFile and the hard-link refusal again, with a name
// that is no longer there told apart from one that could not be looked up.
func (f *AppendFile) stillNamed() error {
	opened, err := f.file.Stat()
	if err != nil {
		return err
	}
	named, err := f.root.root.Lstat(f.name)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrHeldFileChanged
	}
	if err != nil {
		return classify(err)
	}
	if named.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, named) || hardLinked(opened) {
		return ErrHeldFileChanged
	}
	return nil
}
