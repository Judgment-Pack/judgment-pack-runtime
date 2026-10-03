//go:build unix

package audit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
