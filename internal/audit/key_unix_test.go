//go:build unix

package audit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// identity is a directory's identity, as a project's held handle gives it.
func identity(t *testing.T, dir string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// A signing key is refused unless it is named by an absolute path with no
// symbolic link anywhere in it, outside the project's directory, one regular
// file with one name, and its owner's alone.
func TestASigningKeyIsHeldOutsideTheProjectAndPrivate(t *testing.T) {
	project := realTempDir(t)
	outside := realTempDir(t)
	self := identity(t, project)
	seed := vectorSeed1 + "\n"
	good := writeKeyFile(t, filepath.Join(outside, "seed"), seed, 0o600)
	signer, err := LoadSigner(good, self)
	if err != nil || signer.PublicKey() != signerOf(t, vectorSeed1).PublicKey() {
		t.Fatalf("a good key: %v", err)
	}
	if _, err := LoadSigner("seed", self); !errors.Is(err, ErrKeyNotAbsolute) {
		t.Fatalf("a relative path: %v", err)
	}
	inside := writeKeyFile(t, filepath.Join(project, "seed"), seed, 0o600)
	if _, err := LoadSigner(inside, self); !errors.Is(err, ErrKeyInsideProject) {
		t.Fatalf("a key inside the project: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(project, "keys", "deeper"), 0o700); err != nil {
		t.Fatal(err)
	}
	deeper := writeKeyFile(t, filepath.Join(project, "keys", "deeper", "seed"), seed, 0o600)
	if _, err := LoadSigner(deeper, self); !errors.Is(err, ErrKeyInsideProject) {
		t.Fatalf("a key beneath the project: %v", err)
	}
	// Without a project, the same key reads: the refusal is the project's.
	if _, err := LoadSigner(deeper, nil); err != nil {
		t.Fatalf("no project: %v", err)
	}
	for _, mode := range []os.FileMode{0o640, 0o604, 0o620, 0o602, 0o660} {
		if _, err := LoadSigner(writeKeyFile(t, filepath.Join(outside, fmt.Sprintf("open-%o", mode)), seed, mode), self); !errors.Is(err, ErrKeyTooOpen) {
			t.Fatalf("mode %o: %v", mode, err)
		}
	}
	if _, err := LoadSigner(writeKeyFile(t, filepath.Join(outside, "owner-only"), seed, 0o400), self); err != nil {
		t.Fatalf("an owner-read-only key: %v", err)
	}
	// A second name is refused, wherever it is.
	linked := writeKeyFile(t, filepath.Join(outside, "linked"), seed, 0o600)
	if err := os.Link(linked, filepath.Join(project, "linked")); err != nil {
		t.Logf("no hard links here: %v", err)
	} else if _, err := LoadSigner(linked, self); !errors.Is(err, ErrKeyLinked) {
		t.Fatalf("a key with a second name: %v", err)
	}
	// What is not a regular file is refused without waiting on it.
	if _, err := LoadSigner(outside, self); !errors.Is(err, ErrKeyNotRegular) {
		t.Fatalf("a directory: %v", err)
	}
	fifo := filepath.Join(outside, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err == nil {
		if _, err := LoadSigner(fifo, self); !errors.Is(err, ErrKeyNotRegular) {
			t.Fatalf("a FIFO: %v", err)
		}
	}
	// A symbolic link anywhere in the path is refused: at the end, in the
	// middle to a directory outside, and in the middle to the project.
	link := filepath.Join(outside, "link")
	if err := os.Symlink(good, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if _, err := LoadSigner(link, self); !errors.Is(err, ErrKeyThroughLink) {
		t.Fatalf("a final symlink: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outside, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeKeyFile(t, filepath.Join(outside, "real", "seed"), seed, 0o600)
	if err := os.Symlink(filepath.Join(outside, "real"), filepath.Join(outside, "dir-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSigner(filepath.Join(outside, "dir-link", "seed"), self); !errors.Is(err, ErrKeyThroughLink) {
		t.Fatalf("a linked directory outside: %v", err)
	}
	if err := os.Symlink(project, filepath.Join(outside, "project-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSigner(filepath.Join(outside, "project-link", "keys", "deeper", "seed"), self); !errors.Is(err, ErrKeyThroughLink) {
		t.Fatalf("a linked directory into the project: %v", err)
	}
	// The project is compared by identity, so a project named through a
	// link is still the project.
	if _, err := LoadSigner(deeper, identity(t, filepath.Join(outside, "project-link"))); !errors.Is(err, ErrKeyInsideProject) {
		t.Fatalf("a project named through a link: %v", err)
	}
}

// What is checked is what is opened. An intermediate component swapped, again
// and again, between a real directory outside the project holding one key and
// a symbolic link to a directory inside the project holding another, never
// gets the key inside the project read: the walk refuses a link, and a
// component swapped between its look and its open is refused as changed.
func TestAKeyIsCheckedAsItIsOpened(t *testing.T) {
	base := realTempDir(t)
	project := filepath.Join(base, "project")
	for _, dir := range []string{filepath.Join(project, "keys"), filepath.Join(base, "real")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeKeyFile(t, filepath.Join(project, "keys", "seed"), vectorSeed2+"\n", 0o600)
	writeKeyFile(t, filepath.Join(base, "real", "seed"), vectorSeed1+"\n", 0o600)
	self := identity(t, project)
	insideKey := signerOf(t, vectorSeed2).PublicKey()
	swap := filepath.Join(base, "swap")
	var stop atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			_ = os.Rename(filepath.Join(base, "real"), swap)
			_ = os.Rename(swap, filepath.Join(base, "real"))
			_ = os.Symlink(filepath.Join("project", "keys"), swap)
			_ = os.Remove(swap)
		}
	}()
	defer func() {
		stop.Store(true)
		<-done
	}()
	read, refused := 0, 0
	deadline := time.Now().Add(5 * time.Second)
	for attempt := 0; attempt < 20000 && time.Now().Before(deadline); attempt++ {
		signer, err := LoadSigner(filepath.Join(swap, "seed"), self)
		if err != nil {
			refused++
			continue
		}
		if signer.PublicKey() == insideKey {
			t.Fatalf("attempt %d read the key inside the project through a swapped component", attempt+1)
		}
		read++
	}
	if read == 0 || refused == 0 {
		t.Logf("read %d, refused %d: the swap did not interleave both ways", read, refused)
	}
}

// A component swapped in the window between the walk's look at it and its
// open, for a symbolic link into the project, is refused rather than followed:
// in the middle of the path and at its end. The swap is made exactly there,
// where a race would have to land.
func TestAComponentSwappedBetweenItsLookAndItsOpenIsRefused(t *testing.T) {
	for _, final := range []bool{false, true} {
		base := realTempDir(t)
		project := filepath.Join(base, "project")
		for _, dir := range []string{filepath.Join(project, "keys"), filepath.Join(base, "real")} {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		writeKeyFile(t, filepath.Join(project, "keys", "seed"), vectorSeed2+"\n", 0o600)
		writeKeyFile(t, filepath.Join(base, "real", "seed"), vectorSeed1+"\n", 0o600)
		keyPath, swapped, target := filepath.Join(base, "real", "seed"), filepath.Join(base, "real"), filepath.Join("project", "keys")
		if final {
			keyPath = filepath.Join(base, "seed")
			writeKeyFile(t, keyPath, vectorSeed1+"\n", 0o600)
			swapped, target = keyPath, filepath.Join("project", "keys", "seed")
		}
		swaps := 0
		afterLook = func(_ *os.Root, name string) {
			if name != filepath.Base(swapped) {
				return
			}
			swaps++
			if err := os.Rename(swapped, swapped+".moved"); err != nil {
				t.Error(err)
			}
			if err := os.Symlink(target, swapped); err != nil {
				t.Error(err)
			}
		}
		signer, err := LoadSigner(keyPath, identity(t, project))
		afterLook = func(*os.Root, string) {}
		if swaps != 1 || !errors.Is(err, ErrKeyPathChanged) || signer != nil {
			t.Fatalf("final=%v: swaps=%d err=%v read=%v", final, swaps, err, signer != nil)
		}
	}
}

// fakeOwner is a file's information with another owner and link count, as a
// file another user owns, or with a second name, reports them.
type fakeOwner struct {
	os.FileInfo
	stat *syscall.Stat_t
}

func (f fakeOwner) Sys() any {
	if f.stat == nil {
		return nil
	}
	return f.stat
}

// The owner check needs no second user: the information of a key another user
// owns differs only in its uid.
func TestTheOwnerCheckRefusesAnotherUsersKey(t *testing.T) {
	path := writeKeyFile(t, filepath.Join(realTempDir(t), "seed"), vectorSeed1+"\n", 0o600)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	real, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("no unix file information here")
	}
	if err := keyAccessCheck(info); err != nil {
		t.Fatalf("the user's own key: %v", err)
	}
	other := *real
	other.Uid = uint32(os.Geteuid() + 1)
	if err := keyAccessCheck(fakeOwner{info, &other}); !errors.Is(err, ErrKeyNotOwned) {
		t.Fatalf("another user's key: %v", err)
	}
	linked := *real
	linked.Nlink = 2
	if err := keyAccessCheck(fakeOwner{info, &linked}); !errors.Is(err, ErrKeyLinked) {
		t.Fatalf("a key with a second name: %v", err)
	}
	if err := keyAccessCheck(fakeOwner{info, nil}); !errors.Is(err, ErrKeyNotOwned) {
		t.Fatalf("no owner to read: %v", err)
	}
}

// Where a key's privacy cannot be checked, LoadSigner refuses every key, and
// ReadKey still reads one: this runs that rule here, as Windows runs it.
func TestLoadSignerRefusesWhereKeyPrivacyIsUnchecked(t *testing.T) {
	good := writeKeyFile(t, filepath.Join(realTempDir(t), "seed"), vectorSeed1+"\n", 0o600)
	keyPrivacyChecked = false
	defer func() { keyPrivacyChecked = true }()
	if _, err := LoadSigner(good, nil); !errors.Is(err, ErrKeyPrivacyUnchecked) {
		t.Fatalf("LoadSigner: %v", err)
	}
	if _, err := ReadKey(good); err != nil {
		t.Fatalf("ReadKey: %v", err)
	}
}

// standIn is a directory's information with another owner and mode, as one
// root or another user owns reports them, which a test cannot otherwise make.
type standIn struct {
	os.FileInfo
	mode os.FileMode
	stat *syscall.Stat_t
}

func (s standIn) Mode() os.FileMode { return s.mode }

func (s standIn) Sys() any {
	if s.stat == nil {
		return nil
	}
	return s.stat
}

// ownedBy stands in uid as info's owner and mode as its mode.
func ownedBy(t *testing.T, info os.FileInfo, uid int, mode os.FileMode) os.FileInfo {
	t.Helper()
	real, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("no unix file information here")
	}
	stat := *real
	stat.Uid = uint32(uid)
	return standIn{FileInfo: info, mode: os.ModeDir | mode, stat: &stat}
}

// owner is an owner and a mode to stand in for a directory the walk holds.
type owner struct {
	uid  int
	mode os.FileMode
}

// standInOwners has the walk to a key hold the directories named, by path, to
// the owners and modes given instead of their own, until the test ends.
func standInOwners(t *testing.T, owners map[string]owner) {
	t.Helper()
	keyDirectoryStandIn = func(path string, info os.FileInfo) os.FileInfo {
		if stood, ok := owners[path]; ok {
			return ownedBy(t, info, stood.uid, stood.mode)
		}
		return info
	}
	t.Cleanup(func() { keyDirectoryStandIn = func(_ string, info os.FileInfo) os.FileInfo { return info } })
}

// A directory on a key's path is held to its owner and its mode alone: owned
// by root or by the user the runtime runs as, and writable by nobody else
// unless its sticky bit is set. Root's ownership excuses no writable mode,
// and the sticky bit excuses no other owner (#221).
func TestADirectoryOnAKeysPathIsHeldToItsOwnerAndMode(t *testing.T) {
	info := identity(t, realTempDir(t))
	self := os.Geteuid()
	other := self + 1
	for _, row := range []struct {
		name string
		uid  int
		mode os.FileMode
		want error
	}{
		{"the user's own, 0700", self, 0o700, nil},
		{"the user's own, 0755", self, 0o755, nil},
		{"root's, 0755", 0, 0o755, nil},
		{"the user's own, sticky 1777", self, os.ModeSticky | 0o777, nil},
		{"root's, sticky 1777, as /tmp", 0, os.ModeSticky | 0o777, nil},
		{"the user's own, 0777", self, 0o777, ErrKeyDirectoryWritable},
		{"the user's own, 0775", self, 0o775, ErrKeyDirectoryWritable},
		{"the user's own, 0730", self, 0o730, ErrKeyDirectoryWritable},
		{"the user's own, 0702", self, 0o702, ErrKeyDirectoryWritable},
		{"root's, 0775", 0, 0o775, ErrKeyDirectoryWritable},
		{"root's, 0777", 0, 0o777, ErrKeyDirectoryWritable},
		{"another user's, 0700", other, 0o700, ErrKeyDirectoryNotOwned},
		{"another user's, sticky 1777", other, os.ModeSticky | 0o777, ErrKeyDirectoryNotOwned},
	} {
		err := keyDirectoryCheck("/srv/keys", ownedBy(t, info, row.uid, row.mode), self)
		if (row.want == nil && err != nil) || (row.want != nil && !errors.Is(err, row.want)) {
			t.Fatalf("%s: want %v, got %v", row.name, row.want, err)
		}
	}
	writable := keyDirectoryCheck("/srv/keys", ownedBy(t, info, self, 0o777), self)
	if got, want := KeyRefusal(writable), "the directory /srv/keys on the signing key's path can be written by its group or by other users (mode 0777) and has no sticky bit, so another user could remove or replace the key; chmod go-w /srv/keys fixes it"; got != want {
		t.Fatalf("the writable directory's reason:\n got %q\nwant %q", got, want)
	}
	owned := keyDirectoryCheck("/srv/keys", ownedBy(t, info, other, 0o700), self)
	if got, want := KeyRefusal(owned), fmt.Sprintf("the directory /srv/keys on the signing key's path is owned by uid %d, neither root nor the user this runtime runs as (uid %d), so that user could remove or replace the key", other, self); got != want {
		t.Fatalf("another user's directory's reason:\n got %q\nwant %q", got, want)
	}
	if err := keyDirectoryCheck("/srv/keys", standIn{FileInfo: info, mode: os.ModeDir | 0o700}, self); !errors.Is(err, ErrKeyDirectoryNotOwned) || !strings.Contains(KeyRefusal(err), "no owner this runtime can read") {
		t.Fatalf("a directory whose owner cannot be read: %v", err)
	}
	// A directory's name is printed with no terminal control in it.
	if reason := KeyRefusal(keyDirectoryCheck("/srv/\x1b[2Jkeys", ownedBy(t, info, self, 0o777), self)); strings.ContainsRune(reason, 0x1b) {
		t.Fatalf("a control character is printed: %q", reason)
	}
}

// dirMode is a directory to make, by name, and its mode.
type dirMode struct {
	name string
	mode os.FileMode
}

// keyUnder makes the directories dirs names, each in the one before, beneath
// base, writes a key in the last, and then sets each directory's mode,
// deepest first, so a directory made private does not stop a chmod beneath
// it. It returns the key's path.
func keyUnder(t *testing.T, base string, dirs ...dirMode) string {
	t.Helper()
	paths := make([]string, 0, len(dirs))
	path := base
	for _, dir := range dirs {
		path = filepath.Join(path, dir.name)
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	key := writeKeyFile(t, filepath.Join(path, "seed"), vectorSeed1+"\n", 0o600)
	for index := len(dirs) - 1; index >= 0; index-- {
		if err := os.Chmod(paths[index], dirs[index].mode); err != nil {
			t.Fatal(err)
		}
	}
	return key
}

// expectKeyPlace holds a key to what LoadSigner, ReadKey and CheckKeyPlace
// say of it: all three accept it, or all three refuse it as want, naming
// the directory named.
func expectKeyPlace(t *testing.T, name, key string, want error, named string) {
	t.Helper()
	_, loaded := LoadSigner(key, nil)
	_, read := ReadKey(key)
	placed := CheckKeyPlace(key)
	for which, err := range map[string]error{"LoadSigner": loaded, "ReadKey": read, "CheckKeyPlace": placed} {
		if want == nil {
			if err != nil {
				t.Fatalf("%s: %s refused it: %v", name, which, err)
			}
			continue
		}
		if !errors.Is(err, want) || !strings.Contains(KeyRefusal(err), "the directory "+named+" on the signing key's path") {
			t.Fatalf("%s: %s: want %v naming %s, got %v", name, which, want, named, err)
		}
	}
}

// Every directory the walk to a key holds, from the root to the key's own,
// must be owned by root or the user the runtime runs as and writable by
// nobody else unless its sticky bit is set; the first one that is not, from
// the root, is the one named (#221). The shapes are real directories; the
// owners a test cannot make, root's and another user's, are stood in.
func TestTheDirectoriesOnAKeysPathAreItsOwnersAlone(t *testing.T) {
	base := realTempDir(t)
	self := os.Geteuid()
	// (a) to (e): the key's own directory, and an ancestor.
	expectKeyPlace(t, "(a) a key directory its owner's alone", keyUnder(t, base, dirMode{"a", 0o700}), nil, "")
	expectKeyPlace(t, "(b) a key directory anyone can write", keyUnder(t, base, dirMode{"b", 0o777}), ErrKeyDirectoryWritable, filepath.Join(base, "b"))
	expectKeyPlace(t, "(c) a key directory its group can write", keyUnder(t, base, dirMode{"c", 0o775}), ErrKeyDirectoryWritable, filepath.Join(base, "c"))
	expectKeyPlace(t, "(d) a private key directory under one anyone can write", keyUnder(t, base, dirMode{"d", 0o777}, dirMode{"k", 0o700}), ErrKeyDirectoryWritable, filepath.Join(base, "d"))
	expectKeyPlace(t, "(e) a sticky key directory anyone can write", keyUnder(t, base, dirMode{"e", os.ModeSticky | 0o777}), nil, "")
	// The refusal says how to fix it, and the fix works.
	open := keyUnder(t, base, dirMode{"fixed", 0o777})
	if _, err := LoadSigner(open, nil); !strings.Contains(KeyRefusal(err), "chmod go-w "+filepath.Join(base, "fixed")+" fixes it") {
		t.Fatalf("the refusal names the fix: %v", err)
	}
	if err := os.Chmod(filepath.Join(base, "fixed"), 0o755); err != nil {
		t.Fatal(err)
	}
	expectKeyPlace(t, "after chmod go-w", open, nil, "")
	// The identity checks come first: a key inside the project, in a
	// directory anyone can write, is refused as inside the project.
	inside := keyUnder(t, base, dirMode{"project", 0o777})
	if _, err := LoadSigner(inside, identity(t, filepath.Join(base, "project"))); !errors.Is(err, ErrKeyInsideProject) {
		t.Fatalf("a key inside a project anyone can write: %v", err)
	}
	// Desk's placement: <config>/secrets/signing/<key>, each directory the
	// user's own and 0700, under ancestors root owns with mode 0755.
	desk := keyUnder(t, base, dirMode{"config", 0o700}, dirMode{"secrets", 0o700}, dirMode{"signing", 0o700})
	ancestors := map[string]owner{}
	for path := base; ; path = filepath.Dir(path) {
		ancestors[path] = owner{0, 0o755}
		if path == "/" {
			break
		}
	}
	standInOwners(t, ancestors)
	expectKeyPlace(t, "Desk's placement under root's 0755 ancestors", desk, nil, "")
	// A /tmp-like ancestor: root's, sticky, anyone may write it.
	tmp := keyUnder(t, base, dirMode{"tmp", os.ModeSticky | 0o777}, dirMode{"k", 0o700})
	standInOwners(t, map[string]owner{filepath.Join(base, "tmp"): {0, os.ModeSticky | 0o777}})
	expectKeyPlace(t, "a private directory under a /tmp-like one", tmp, nil, "")
	// Root's ownership excuses no writable mode.
	shared := keyUnder(t, base, dirMode{"shared", 0o775}, dirMode{"k", 0o700})
	standInOwners(t, map[string]owner{filepath.Join(base, "shared"): {0, 0o775}})
	expectKeyPlace(t, "an ancestor root owns that its group can write", shared, ErrKeyDirectoryWritable, filepath.Join(base, "shared"))
	// A directory another user owns is that user's to change.
	theirs := keyUnder(t, base, dirMode{"theirs", 0o755}, dirMode{"k", 0o700})
	standInOwners(t, map[string]owner{filepath.Join(base, "theirs"): {self + 1, 0o755}})
	expectKeyPlace(t, "an ancestor another user owns", theirs, ErrKeyDirectoryNotOwned, filepath.Join(base, "theirs"))
	// The root is held like any other directory, and before every other.
	private := keyUnder(t, base, dirMode{"root-test", 0o700})
	standInOwners(t, map[string]owner{"/": {0, 0o777}})
	expectKeyPlace(t, "a root directory anyone can write", private, ErrKeyDirectoryWritable, "/")
}
