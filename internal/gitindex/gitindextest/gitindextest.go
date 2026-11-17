// Package gitindextest builds git index files for tests, as bytes, without
// running git.
package gitindextest

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Entry is one entry of an index.
type Entry struct {
	Path string
	// Mode is the entry's mode; zero is a regular file, 0o100644. A symbolic
	// link is 0o120000.
	Mode  uint32
	Stage int
	// Extended is the entry's extended flags (version 3 and later): any
	// sets the entry's extended flag.
	Extended uint16
}

// Builder is an index file to build: its entries in the order given, then
// its extensions, each whole, then its checksum.
type Builder struct {
	Version    uint32
	Entries    []Entry
	Extensions [][]byte
	// SHA256 builds the index of a repository whose object format is
	// SHA-256: 32-byte object names, and a SHA-256 checksum.
	SHA256 bool
}

// Bytes returns the index file. Version 4's paths are compressed as git
// compresses them, against the entry before.
func (b Builder) Bytes() []byte {
	hashSize := sha1.Size
	if b.SHA256 {
		hashSize = sha256.Size
	}
	var out bytes.Buffer
	out.WriteString("DIRC")
	out.Write(binary.BigEndian.AppendUint32(nil, b.Version))
	out.Write(binary.BigEndian.AppendUint32(nil, uint32(len(b.Entries))))
	previous := ""
	for _, e := range b.Entries {
		start := out.Len()
		mode := e.Mode
		if mode == 0 {
			mode = 0o100644
		}
		out.Write(make([]byte, 24)) // ctime, mtime, dev, ino
		out.Write(binary.BigEndian.AppendUint32(nil, mode))
		out.Write(make([]byte, 12))       // uid, gid, size
		out.Write(make([]byte, hashSize)) // object name
		flags := uint16(min(len(e.Path), 0xfff)) | uint16(e.Stage)<<12
		if e.Extended != 0 {
			flags |= 0x4000
		}
		out.Write(binary.BigEndian.AppendUint16(nil, flags))
		if e.Extended != 0 {
			out.Write(binary.BigEndian.AppendUint16(nil, e.Extended))
		}
		if b.Version == 4 {
			common := 0
			for common < len(previous) && common < len(e.Path) && previous[common] == e.Path[common] {
				common++
			}
			out.Write(Varint(uint64(len(previous) - common)))
			out.WriteString(e.Path[common:])
			out.WriteByte(0)
		} else {
			out.WriteString(e.Path)
			end := start + (out.Len()-start+8)/8*8
			out.Write(make([]byte, end-out.Len()))
		}
		previous = e.Path
	}
	for _, extension := range b.Extensions {
		out.Write(extension)
	}
	if b.SHA256 {
		sum := sha256.Sum256(out.Bytes())
		out.Write(sum[:])
	} else {
		sum := sha1.Sum(out.Bytes())
		out.Write(sum[:])
	}
	return out.Bytes()
}

// Varint is git's offset varint of v (varint.c's encode_varint).
func Varint(v uint64) []byte {
	out := []byte{byte(v & 0x7f)}
	for v >>= 7; v != 0; v >>= 7 {
		v--
		out = append([]byte{0x80 | byte(v&0x7f)}, out...)
	}
	return out
}

// Extension is an extension whole: its four-byte signature, its size, and
// data.
func Extension(signature string, data []byte) []byte {
	out := append([]byte(signature), binary.BigEndian.AppendUint32(nil, uint32(len(data)))...)
	return append(out, data...)
}

// Write makes dir a repository's working tree whose index holds paths, each
// a regular file, at stage 0 and in git's order: it makes dir/.git and
// writes dir/.git/index, of version 2.
func Write(t testing.TB, dir string, paths ...string) {
	t.Helper()
	sorted := slices.Clone(paths)
	slices.Sort(sorted)
	b := Builder{Version: 2}
	for _, path := range slices.Compact(sorted) {
		b.Entries = append(b.Entries, Entry{Path: path})
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "index"), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
