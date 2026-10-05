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

// keyDirectoryStandIn is what the walk holds a directory to: the information
// of the directory it holds open. It is a variable only so a test can stand in
// another owner or mode for a directory, which a test cannot make another
// user own.
var keyDirectoryStandIn = func(path string, info os.FileInfo) os.FileInfo { return info }

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
// comparison cannot be fooled about. Every directory the walk holds, the root
// and the key's own included, must then be one nobody else can remove or
// replace the key from (keyDirectoryCheck). The key file itself must be one
// regular file with one name, owned by the user this runtime runs as and
// neither readable nor writable by its group or by others; anything else is
// refused before a byte of it is read.
func readKey(keyPath string, project os.FileInfo) (*Signer, error) {
	components := strings.Split(strings.TrimPrefix(keyPath, "/"), "/")
	walked, err := holdKeyDirectories(components[:len(components)-1])
	if err != nil {
		return nil, err
	}
	dir := walked.dir
	defer dir.Close()
	for _, each := range walked.held {
		if project != nil && os.SameFile(each, project) {
			return nil, ErrKeyInsideProject
		}
	}
	if walked.placement != nil {
		return nil, walked.placement
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

// holdKeyPlace is CheckKeyPlace on unix: the walk readKey makes to the
// directory of an absolute, cleaned key path, and that directory's rule for
// every directory it holds.
func holdKeyPlace(keyPath string) error {
	components := strings.Split(strings.TrimPrefix(keyPath, "/"), "/")
	walked, err := holdKeyDirectories(components[:len(components)-1])
	if err != nil {
		return err
	}
	walked.dir.Close()
	return walked.placement
}

// keyDirectories is what the walk to a key's directory holds.
type keyDirectories struct {
	// dir is the last directory held, open: the key's own.
	dir *os.Root
	// held is every directory held, the root first, by identity.
	held []os.FileInfo
	// placement is the first directory, from the root, that
	// keyDirectoryCheck refuses, or nil. It is reported after the walk, so
	// that the identity checks keep their order before it.
	placement error
}

// holdKeyDirectories walks the directories names lists from the filesystem
// root, holding each open and opening the next relative to it. It refuses a
// symbolic link at any of them and one swapped between its look and its open,
// and holds each directory it opens, by the information of the handle it
// holds, to keyDirectoryCheck.
func holdKeyDirectories(names []string) (keyDirectories, error) {
	dir, err := os.OpenRoot("/")
	if err != nil {
		return keyDirectories{}, err
	}
	self, err := dir.Stat(".")
	if err != nil {
		dir.Close()
		return keyDirectories{}, err
	}
	euid := os.Geteuid()
	walked := keyDirectories{held: []os.FileInfo{self}}
	walked.placement = keyDirectoryCheck("/", keyDirectoryStandIn("/", self), euid)
	for index, name := range names {
		seen, err := dir.Lstat(name)
		if err != nil {
			dir.Close()
			return keyDirectories{}, err
		}
		if seen.Mode()&os.ModeSymlink != 0 {
			dir.Close()
			return keyDirectories{}, ErrKeyThroughLink
		}
		afterLook(dir, name)
		// OpenRoot follows a symbolic link that stays inside the directory
		// held, so the identity check below is what refuses one swapped in.
		next, err := dir.OpenRoot(name)
		if err != nil {
			dir.Close()
			return keyDirectories{}, err
		}
		opened, err := next.Stat(".")
		if err != nil {
			next.Close()
			dir.Close()
			return keyDirectories{}, err
		}
		if !os.SameFile(seen, opened) {
			next.Close()
			dir.Close()
			return keyDirectories{}, ErrKeyPathChanged
		}
		dir.Close()
		dir = next
		walked.held = append(walked.held, opened)
		if walked.placement == nil {
			path := "/" + strings.Join(names[:index+1], "/")
			walked.placement = keyDirectoryCheck(path, keyDirectoryStandIn(path, opened), euid)
		}
	}
	walked.dir = dir
	return walked, nil
}

// keyDirectoryCheck holds one directory on a signing key's path, at path, to
// what keeps the key its owner's: owned by root or by the user this runtime
// runs as (euid), and writable by nobody else unless its sticky bit keeps
// others from removing or renaming what they do not own. Whoever else may
// write a directory on the path can remove or rename the key, or a directory
// between it and the root, and put something else in its place; a directory
// another user owns is that user's to change whatever its mode says. Root's
// ownership excuses no writable mode, and group membership is not read: a
// directory its group may write is refused whoever is in the group. The
// sticky bit excuses the key's own directory too: nothing in it but the key is
// read, and a file another user makes under the key's name before it is there
// is refused as the key, as not owned.
func keyDirectoryCheck(path string, info os.FileInfo, euid int) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return &keyDirectoryError{kind: ErrKeyDirectoryNotOwned, dir: path, uid: -1, euid: euid}
	}
	if uid := int(stat.Uid); uid != 0 && uid != euid {
		return &keyDirectoryError{kind: ErrKeyDirectoryNotOwned, dir: path, uid: uid, euid: euid}
	}
	if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
		return &keyDirectoryError{kind: ErrKeyDirectoryWritable, dir: path, mode: info.Mode(), euid: euid}
	}
	return nil
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
