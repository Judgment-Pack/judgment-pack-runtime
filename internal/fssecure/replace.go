package fssecure

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// ErrNotOneName is a name that is not one component directly beneath the
// root: a file ReplaceByRename puts in place is named by its own name alone.
var ErrNotOneName = errors.New("the name is not one file name directly in the directory")

// ErrReplacedChanged is a file ReplaceByRename was to replace that is no
// longer, at the rename, the file its caller checked: something was put in
// its place, or removed, after the check. Nothing is replaced then.
var ErrReplacedChanged = errors.New("the file to be replaced changed after it was checked")

// Stat describes a file beneath this root, through the handle, following a
// final symbolic link that stays beneath it.
func (r *Root) Stat(relative string) (os.FileInfo, error) {
	cleaned, err := Relative(relative)
	if err != nil {
		return nil, err
	}
	info, err := r.root.Stat(cleaned)
	return info, classify(err)
}

// Lstat describes a file beneath this root, through the handle, without
// following a final symbolic link.
func (r *Root) Lstat(relative string) (os.FileInfo, error) {
	cleaned, err := Relative(relative)
	if err != nil {
		return nil, err
	}
	info, err := r.root.Lstat(cleaned)
	return info, classify(err)
}

// ReadIdentified is Read, answering also the identity of the file read, from
// the descriptor it was read through: what a caller compares another file with
// by os.SameFile, which a later lookup of the name could not give once the
// name was given to another file.
func (r *Root) ReadIdentified(relative string, limit int64) ([]byte, os.FileInfo, error) {
	cleaned, err := Relative(relative)
	if err != nil {
		return nil, nil, err
	}
	file, err := r.root.OpenFile(cleaned, os.O_RDONLY|nonBlockingOpen, 0)
	if err != nil {
		return nil, nil, classify(err)
	}
	defer file.Close()
	info, err := r.sameRegularFile(file, cleaned)
	if err != nil {
		return nil, nil, err
	}
	data, err := readBounded(file, limit)
	if err != nil {
		return nil, nil, err
	}
	return data, info, nil
}

// ReplaceByRename puts contents at name, one file directly in the directory
// this root holds, so that a reader of name finds either the file that was
// there or the whole of the new one, never part of either:
//
//  1. a temporary file is created beside name, through the handle,
//     exclusively, under a name of its own with random bytes in it, and
//     written, synced and closed;
//  2. what is at name is checked, through the handle, to be still what the
//     caller checked: nothing, when expect is nil, or else the regular file
//     expect describes, by its identity, and not a symbolic link
//     (ErrReplacedChanged otherwise);
//  3. the temporary file is renamed onto name, and the directory is synced.
//
// On every failure the temporary file is removed and name is left as it was.
//
// Two things are not guarded, and are stated here rather than hidden:
//
//   - Steps 2 and 3 are two calls. A change of that one name by another
//     process in the instant between them is not detected: a file put there
//     then is replaced, and a symbolic link put there is replaced itself, the
//     file it names left untouched.
//   - Step 3 renames through the handle in a build with Go 1.25 or later, so
//     the rename is in the directory the handle holds whatever its pathname
//     names by then. A build with Go 1.24, this module's floor, renames by the
//     held directory's pathname, after checking that the pathname still names
//     the directory held, and refuses when it does not; a re-pointing of that
//     pathname between the check and the rename is not detected.
func (r *Root) ReplaceByRename(name string, contents []byte, expect os.FileInfo) error {
	cleaned, err := Relative(name)
	if err != nil {
		return err
	}
	if cleaned != name || cleaned == "." || strings.ContainsAny(cleaned, `/\`) {
		return ErrNotOneName
	}
	temporary, file, err := r.createTemporary(cleaned)
	if err != nil {
		return err
	}
	kept := false
	defer func() {
		if !kept {
			r.root.Remove(temporary)
		}
	}()
	_, err = file.Write(contents)
	if err == nil {
		err = file.Sync()
	}
	if closed := file.Close(); err == nil {
		err = closed
	}
	if err != nil {
		return err
	}
	if err := r.stillAsChecked(cleaned, expect); err != nil {
		return err
	}
	if err := r.renameWithin(temporary, cleaned); err != nil {
		return err
	}
	kept = true
	return r.syncDir(cleaned)
}

// createTemporary creates a file beside name, exclusively, under a name no
// other writer chose: name with random bytes after it, hidden.
func (r *Root) createTemporary(name string) (string, *os.File, error) {
	for attempt := 0; attempt < appendOpenAttempts; attempt++ {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", nil, err
		}
		temporary := "." + name + "." + hex.EncodeToString(random[:]) + ".tmp"
		file, err := r.root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, classify(err)
		}
		return temporary, file, nil
	}
	return "", nil, errors.New("no temporary name beside the file was free")
}

// stillAsChecked says whether name is still what its caller checked: nothing,
// when expect is nil, or the regular file expect describes.
func (r *Root) stillAsChecked(name string, expect os.FileInfo) error {
	info, err := r.root.Lstat(name)
	switch {
	case expect == nil && errors.Is(err, fs.ErrNotExist):
		return nil
	case expect == nil || err != nil:
		return ErrReplacedChanged
	case info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !os.SameFile(info, expect):
		return ErrReplacedChanged
	}
	return nil
}
