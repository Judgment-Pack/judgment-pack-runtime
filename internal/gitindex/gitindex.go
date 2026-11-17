// Package gitindex reads the paths of a git repository's index file, the
// list of what git tracks. It only reads: it never writes a file and never
// runs a program, git included.
//
// It reads index versions 2, 3 and 4, version 4's compressed path names
// included, for a repository whose object format is SHA-1. Anything else is
// refused, never guessed at: a checksum that does not match, a version or a
// flag it does not know, a malformed entry or path, entries out of git's
// order, and an extension git requires a reader to understand (one whose
// signature does not begin with an upper-case letter), such as a split index
// (`link`) or a sparse index (`sdir`), either of which changes the set of
// paths the entries give. An optional extension (signature 'A' to 'Z') is
// passed over, as git's format allows.
package gitindex

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The reader's bounds. An index past either is refused (ErrBound).
const (
	// MaxBytes bounds the index file's size.
	MaxBytes = 64 << 20
	// MaxEntries bounds the entries the index header gives.
	MaxEntries = 1 << 20
)

// The bounds as the code applies them; a test lowers them.
var (
	maxBytes   = MaxBytes
	maxEntries = MaxEntries
)

var (
	// ErrNoRepository is a root with no .git in it.
	ErrNoRepository = errors.New("gitindex: there is no .git at the root")
	// ErrNotDirectory is a root whose .git is not a directory: a file, as a
	// linked worktree or a submodule has, whose index lies elsewhere; a
	// symbolic link; or another kind of entry, a Windows junction for one.
	ErrNotDirectory = errors.New("gitindex: the root's .git is not a directory")
	// ErrUnreadable is an index file that is missing, is not a regular file,
	// or could not be read.
	ErrUnreadable = errors.New("gitindex: the index file could not be read")
	// ErrBound is an index past MaxBytes or MaxEntries.
	ErrBound = errors.New("gitindex: the index is past the reader's bounds")
	// ErrFormat is an index this reader does not read: see the package
	// documentation.
	ErrFormat = errors.New("gitindex: the index is not of a form this reader reads")
)

// Entry is what the reader gives of one entry of an index.
type Entry struct {
	// Path is the entry's slash path, relative to the working tree's root.
	Path string
	// Mode is the entry's mode: a regular file (0o100644 or 0o100755), a
	// symbolic link (0o120000), or a gitlink, a submodule (0o160000).
	Mode uint32
	// Stage is the entry's merge stage: 0, or 1 to 3 for a path in
	// conflict, which has an entry at each.
	Stage int
}

// Mode types of an entry.
const (
	ModeRegular = 0o100000
	ModeLink    = 0o120000
	ModeGitlink = 0o160000
	modeType    = 0o170000
)

// IsGitlink says whether the entry is a submodule, whose own files are in
// its own repository's index, not in this one.
func (e Entry) IsGitlink() bool { return e.Mode&modeType == ModeGitlink }

// ReadWorkTree reads the index of the repository whose working tree is the
// directory root: root/.git must be a directory, and root/.git/index a
// regular file. It calls visit with each entry, in index order. When it
// returns an error, the calls it made are to be disregarded: the entries
// they gave are not the index's.
func ReadWorkTree(root string, visit func(Entry)) error {
	gitDir := filepath.Join(root, ".git")
	info, err := os.Lstat(gitDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ErrNoRepository
	case err != nil:
		return ErrUnreadable
	case info.Mode().Type() != fs.ModeDir:
		return ErrNotDirectory
	}
	data, err := readFile(filepath.Join(gitDir, "index"))
	if err != nil {
		return err
	}
	_, err = Parse(data, visit)
	return err
}

// readFile reads a regular file of at most maxBytes.
func readFile(name string) ([]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, ErrUnreadable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrUnreadable
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	switch {
	case err != nil:
		return nil, ErrUnreadable
	case len(data) > maxBytes:
		return nil, ErrBound
	}
	return data, nil
}

// The layout of an index (git's Documentation/gitformat-index.txt).
const (
	headerSize = 12
	hashSize   = sha1.Size
	// statSize is an entry's ctime, mtime, dev, ino, mode, uid, gid and
	// size; the object name and the flags follow.
	statSize  = 40
	fixedSize = statSize + hashSize + 2
	// minEntrySize is the fewest bytes an entry takes in any version.
	minEntrySize = fixedSize + 2

	flagExtended  = 0x4000
	flagStageMask = 0x3000
	flagNameMask  = 0x0fff
	// The extended flags a version 3 or 4 entry may carry: skip-worktree
	// and intent-to-add. The rest are reserved and must be zero.
	extendedKnown = 0x4000 | 0x2000
)

// Parse reads the bytes of an index file, as ReadWorkTree does, and returns
// its version.
func Parse(data []byte, visit func(Entry)) (uint32, error) {
	if len(data) > maxBytes {
		return 0, ErrBound
	}
	if len(data) < headerSize+hashSize {
		return 0, ErrFormat
	}
	// The checksum first: no byte of an index whose checksum does not match
	// is read. An index of another object format, SHA-256, ends in a longer
	// checksum of another function, so it does not match either.
	body, sum := data[:len(data)-hashSize], data[len(data)-hashSize:]
	if want := sha1.Sum(body); !bytes.Equal(want[:], sum) {
		return 0, ErrFormat
	}
	if string(body[:4]) != "DIRC" {
		return 0, ErrFormat
	}
	version := binary.BigEndian.Uint32(body[4:8])
	if version < 2 || version > 4 {
		return 0, ErrFormat
	}
	count := binary.BigEndian.Uint32(body[8:12])
	if uint64(count) > uint64(maxEntries) {
		return 0, ErrBound
	}
	if uint64(count)*minEntrySize > uint64(len(body)-headerSize) {
		return 0, ErrFormat
	}
	r := &reader{data: body, at: headerSize}
	var previous Entry
	for i := range count {
		e, err := r.entry(version, previous.Path)
		if err != nil {
			return 0, err
		}
		if i > 0 {
			// git's order: by path, byte by byte, then by stage; a path held
			// at stage 0 is held at no other, and none twice at one stage.
			switch c := strings.Compare(previous.Path, e.Path); {
			case c > 0, c == 0 && (previous.Stage == 0 || previous.Stage >= e.Stage):
				return 0, ErrFormat
			}
		}
		visit(e)
		previous = e
	}
	if err := r.extensions(); err != nil {
		return 0, err
	}
	return version, nil
}

// reader reads an index's body, the bytes before its checksum.
type reader struct {
	data []byte
	at   int
}

// entry reads one entry. previous is the path of the entry before, from
// which version 4 builds this one's.
func (r *reader) entry(version uint32, previous string) (Entry, error) {
	start := r.at
	if len(r.data)-start < fixedSize {
		return Entry{}, ErrFormat
	}
	// The mode, after ctime, mtime, dev and ino. A directory (a sparse
	// index's), or any other type, is not a path this reader reads.
	mode := binary.BigEndian.Uint32(r.data[start+24:])
	switch mode & modeType {
	case ModeRegular, ModeLink, ModeGitlink:
	default:
		return Entry{}, ErrFormat
	}
	flags := binary.BigEndian.Uint16(r.data[start+statSize+hashSize:])
	r.at = start + fixedSize
	if flags&flagExtended != 0 {
		if version < 3 || len(r.data)-r.at < 2 {
			return Entry{}, ErrFormat
		}
		if binary.BigEndian.Uint16(r.data[r.at:])&^extendedKnown != 0 {
			return Entry{}, ErrFormat
		}
		r.at += 2
	}
	var name string
	if version == 4 {
		// The count of bytes to take off the end of the previous path, then
		// the rest of this one, ended by a NUL; no padding.
		strip, ok := r.varint()
		if !ok || strip > uint64(len(previous)) {
			return Entry{}, ErrFormat
		}
		suffix, ok := r.cstring()
		if !ok {
			return Entry{}, ErrFormat
		}
		name = previous[:len(previous)-int(strip)] + suffix
	} else {
		// The path, ended by a NUL and padded with NULs to a multiple of
		// eight bytes from the entry's start, one to eight of them in all.
		fixed := r.at - start
		end := r.at + bytes.IndexByte(r.data[r.at:], 0)
		if end < r.at {
			return Entry{}, ErrFormat
		}
		name = string(r.data[r.at:end])
		next := start + ((fixed + len(name) + 8) &^ 7)
		if next > len(r.data) || !allZero(r.data[end:next]) {
			return Entry{}, ErrFormat
		}
		r.at = next
	}
	// The flags give the path's length, or flagNameMask for one at least
	// that long.
	if int(flags&flagNameMask) != min(len(name), flagNameMask) || !validPath(name) {
		return Entry{}, ErrFormat
	}
	return Entry{Path: name, Mode: mode, Stage: int(flags&flagStageMask) >> 12}, nil
}

// varint reads git's offset varint (varint.c): each byte gives seven bits,
// high bits first, and each byte after the first adds one before its bits
// are shifted in, so that every number has one encoding.
func (r *reader) varint() (uint64, bool) {
	if r.at >= len(r.data) {
		return 0, false
	}
	c := r.data[r.at]
	r.at++
	value := uint64(c & 0x7f)
	for c&0x80 != 0 {
		value++
		if value > uint64(len(r.data)) || r.at >= len(r.data) {
			// No path is longer than the index.
			return 0, false
		}
		c = r.data[r.at]
		r.at++
		value = value<<7 | uint64(c&0x7f)
	}
	return value, true
}

// cstring reads bytes up to a NUL and the NUL.
func (r *reader) cstring() (string, bool) {
	end := bytes.IndexByte(r.data[r.at:], 0)
	if end < 0 {
		return "", false
	}
	s := string(r.data[r.at : r.at+end])
	r.at += end + 1
	return s, true
}

// extensions reads the extensions after the entries, which must fill the
// body exactly: each a four-byte signature, a four-byte size and that many
// bytes. One whose signature does not begin with 'A' to 'Z' must be
// understood to read the index, and this reader understands none.
func (r *reader) extensions() error {
	for r.at < len(r.data) {
		if len(r.data)-r.at < 8 {
			return ErrFormat
		}
		signature := r.data[r.at]
		size := uint64(binary.BigEndian.Uint32(r.data[r.at+4:]))
		r.at += 8
		if signature < 'A' || signature > 'Z' || size > uint64(len(r.data)-r.at) {
			return ErrFormat
		}
		r.at += int(size)
	}
	return nil
}

// validPath says whether name is a path git writes in an index: not empty,
// slash-separated, with no empty, "." or ".." segment.
func validPath(name string) bool {
	if name == "" {
		return false
	}
	for start := 0; start <= len(name); {
		end := start + strings.IndexByte(name[start:], '/')
		if end < start {
			end = len(name)
		}
		switch name[start:end] {
		case "", ".", "..":
			return false
		}
		start = end + 1
	}
	return true
}

func allZero(data []byte) bool {
	for _, c := range data {
		if c != 0 {
			return false
		}
	}
	return true
}
