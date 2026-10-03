//go:build unix

package audit

import (
	"os"
	"strings"
	"syscall"
)

// keyPrivacyChecked says this platform can establish that a key is its
// owner's alone: unix ownership and modes. It is a variable only so a test
// can run LoadSigner as a platform without them does.
var keyPrivacyChecked = true

// afterLook runs between the look at a component of a key's path and its
// open. It does nothing; a test swaps the component there, in the window an
// attacker would race for, to hold the walk to refusing what it then opens.
var afterLook = func(dir *os.Root, name string) {}

// readKey opens and reads the key at an absolute, cleaned path so that what is
// checked is what is opened (ADR-0047 §2b). It walks the path from the
// filesystem root one component at a time, holding each directory open and
// opening the next relative to it, and refuses a symbolic link at any
// component: a link is the one thing that can make a pathname resolve
// somewhere else between a check and an open, so with none the path is the
// key's real path. Each component is held to the entry the walk saw, by device
// and inode, so one swapped in between the look and the open is refused rather
// than followed. A key is inside the project when any directory the walk holds
// is the project's directory, by the same identity, which a pathname
// comparison cannot be fooled about. The key file itself must be one regular
// file with one name, owned by the user this runtime runs as and neither
// readable nor writable by its group or by others; anything else is refused
// before a byte of it is read.
func readKey(keyPath string, project os.FileInfo) (*Signer, error) {
	dir, err := os.OpenRoot("/")
	if err != nil {
		return nil, err
	}
	defer func() { dir.Close() }()
	self, err := dir.Stat(".")
	if err != nil {
		return nil, err
	}
	held := []os.FileInfo{self}
	components := strings.Split(strings.TrimPrefix(keyPath, "/"), "/")
	for _, name := range components[:len(components)-1] {
		seen, err := dir.Lstat(name)
		if err != nil {
			return nil, err
		}
		if seen.Mode()&os.ModeSymlink != 0 {
			return nil, ErrKeyThroughLink
		}
		afterLook(dir, name)
		// OpenRoot follows a symbolic link that stays inside the directory
		// held, so the identity check below is what refuses one swapped in.
		next, err := dir.OpenRoot(name)
		if err != nil {
			return nil, err
		}
		opened, err := next.Stat(".")
		if err != nil {
			next.Close()
			return nil, err
		}
		if !os.SameFile(seen, opened) {
			next.Close()
			return nil, ErrKeyPathChanged
		}
		dir.Close()
		dir = next
		held = append(held, opened)
	}
	for _, each := range held {
		if project != nil && os.SameFile(each, project) {
			return nil, ErrKeyInsideProject
		}
	}
	name := components[len(components)-1]
	seen, err := dir.Lstat(name)
	if err != nil {
		return nil, err
	}
	if seen.Mode()&os.ModeSymlink != 0 {
		return nil, ErrKeyThroughLink
	}
	if !seen.Mode().IsRegular() {
		return nil, ErrKeyNotRegular
	}
	afterLook(dir, name)
	// os.Root follows a final symbolic link that stays inside the directory
	// held, O_NOFOLLOW or not, so the identity check below is what refuses a
	// link swapped in after the look; O_NONBLOCK keeps a FIFO from blocking.
	file, err := dir.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(seen, info) {
		return nil, ErrKeyPathChanged
	}
	if !info.Mode().IsRegular() {
		return nil, ErrKeyNotRegular
	}
	if err := keyAccessCheck(info); err != nil {
		return nil, err
	}
	return readSeedFrom(file)
}

// keyAccessCheck holds an opened signing key to its owner alone: one name,
// owned by the user this runtime runs as, and neither readable nor writable by
// its group or by others. A key another user owns is one that user can read
// whatever its mode says, a key others can read is one they can sign with, and
// a key with a second name may have that name anywhere.
func keyAccessCheck(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return ErrKeyNotOwned
	}
	if stat.Nlink > 1 {
		return ErrKeyLinked
	}
	if info.Mode().Perm()&0o077 != 0 {
		return ErrKeyTooOpen
	}
	return nil
}
