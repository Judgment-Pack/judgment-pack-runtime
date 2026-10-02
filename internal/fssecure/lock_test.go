package fssecure

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// lockedPlatform says whether this platform has the lock AppendLocked takes.
// The three CI platforms all do.
func lockedPlatform() bool {
	switch runtime.GOOS {
	case "darwin", "dragonfly", "freebsd", "linux", "netbsd", "openbsd", "windows":
		return true
	}
	return false
}

// readAll reads what compose was handed, from the start.
func readAll(t *testing.T, state AppendState) string {
	t.Helper()
	data, err := io.ReadAll(io.NewSectionReader(state.Contents, 0, state.Size))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// compose is handed the file as it stands, under the lock, and what it returns
// is appended after it; a compose that fails writes nothing.
func TestAppendLockedHandsComposeTheFileAsItStands(t *testing.T) {
	dir := t.TempDir()
	root := mustOpenRoot(t, dir)
	called := 0
	if err := root.AppendLocked("log.jsonl", func(state AppendState) ([]byte, error) {
		called++
		if state.Size != 0 || readAll(t, state) != "" {
			t.Fatalf("a new file is empty: %d", state.Size)
		}
		return []byte("a\n"), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := root.AppendLocked("log.jsonl", func(state AppendState) ([]byte, error) {
		called++
		if lockedPlatform() && !state.Locked {
			t.Fatal("this platform has the lock, and compose is told it is held")
		}
		if state.Size != 2 || readAll(t, state) != "a\n" {
			t.Fatalf("compose sees the file as it stands: %d %q", state.Size, readAll(t, state))
		}
		return []byte("b\n"), nil
	}); err != nil {
		t.Fatal(err)
	}
	refusal := errors.New("refused")
	if err := root.AppendLocked("log.jsonl", func(AppendState) ([]byte, error) { return []byte("c\n"), refusal }); !errors.Is(err, refusal) {
		t.Fatalf("compose's failure is the append's: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "log.jsonl"))
	if err != nil || string(data) != "a\nb\n" || called != 2 {
		t.Fatalf("data=%q err=%v called=%d", data, err, called)
	}
	info, err := os.Stat(filepath.Join(dir, "log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("the file must not be group- or world-readable: %v", info.Mode())
	}
}

// Every refusal Append makes, AppendLocked makes, before compose is called.
func TestAppendLockedRefusesWhatAppendRefuses(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	root := mustOpenRoot(t, dir)
	compose := func(AppendState) ([]byte, error) {
		t.Fatal("compose must not be reached for a refused path")
		return nil, nil
	}
	for _, relative := range []string{"", "..", "../escape.jsonl", "/etc/passwd"} {
		if err := root.AppendLocked(relative, compose); !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("%q must be refused as outside the root, got %v", relative, err)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	target := filepath.Join(dir, "real.jsonl")
	if err := os.WriteFile(target, []byte("kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "alias.jsonl")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := root.AppendLocked("alias.jsonl", compose); err == nil {
		t.Fatal("a final symlink must be refused")
	}
	if err := os.Link(target, filepath.Join(dir, "linked.jsonl")); err != nil {
		t.Skipf("cannot create hardlink: %v", err)
	}
	if err := root.AppendLocked("linked.jsonl", compose); err == nil {
		t.Fatal("a file with more than one name must be refused")
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "kept\n" {
		t.Fatalf("the refused appends must not have reached the file: data=%q err=%v", data, err)
	}
}

// While one writer holds the lock, a second waits: its compose is not called
// until the first has written, and it then sees what the first wrote.
func TestAppendLockedExcludesAnotherWriter(t *testing.T) {
	if !lockedPlatform() {
		t.Skip("this platform has no lock")
	}
	dir := t.TempDir()
	root := mustOpenRoot(t, dir)
	var secondInside atomic.Bool
	secondSaw := make(chan string, 1)
	done := make(chan error, 1)
	if err := root.AppendLocked("log.jsonl", func(AppendState) ([]byte, error) {
		go func() {
			done <- root.AppendLocked("log.jsonl", func(state AppendState) ([]byte, error) {
				secondInside.Store(true)
				secondSaw <- readAll(t, state)
				return []byte("second\n"), nil
			})
		}()
		time.Sleep(200 * time.Millisecond)
		if secondInside.Load() {
			t.Error("a second writer reached compose while the first held the lock")
		}
		return []byte("first\n"), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if saw := <-secondSaw; saw != "first\n" {
		t.Fatalf("the second writer sees what the first wrote: %q", saw)
	}
	data, err := os.ReadFile(filepath.Join(dir, "log.jsonl"))
	if err != nil || string(data) != "first\nsecond\n" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

// Where no lock can be taken, compose is told so, and what it returns is still
// appended under Append's guarantees.
func TestAppendLockedWithoutALockTellsCompose(t *testing.T) {
	dir := t.TempDir()
	root := mustOpenRoot(t, dir)
	original := lockExclusive
	lockExclusive = func(*os.File) error { return errNoLock }
	t.Cleanup(func() { lockExclusive = original })
	for _, record := range []string{"a\n", "b\n"} {
		if err := root.AppendLocked("log.jsonl", func(state AppendState) ([]byte, error) {
			if state.Locked {
				t.Fatal("compose is told no lock is held")
			}
			return []byte(record), nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "log.jsonl"))
	if err != nil || string(data) != "a\nb\n" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	// A lock that fails for another reason fails the append.
	lockExclusive = func(*os.File) error { return errors.New("lock failed") }
	if err := root.AppendLocked("log.jsonl", func(AppendState) ([]byte, error) { return []byte("c\n"), nil }); err == nil {
		t.Fatal("a failed lock is a failed append")
	}
}

// A name given to another file while a writer waited for the lock is opened
// afresh: the record goes to the file the name names, never to the one nothing
// names any more.
func TestAppendLockedFollowsANameGivenToAnotherFileWhileItWaited(t *testing.T) {
	if !lockedPlatform() {
		t.Skip("this platform has no lock")
	}
	dir := t.TempDir()
	root := mustOpenRoot(t, dir)
	trail := filepath.Join(dir, "log.jsonl")
	if err := os.WriteFile(trail, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := lockExclusive
	swapped := false
	lockExclusive = func(file *os.File) error {
		if !swapped {
			swapped = true
			if err := os.Rename(trail, filepath.Join(dir, "log.jsonl.kept")); err != nil {
				return err
			}
			if err := os.WriteFile(trail, []byte("new\n"), 0o600); err != nil {
				return err
			}
		}
		return original(file)
	}
	t.Cleanup(func() { lockExclusive = original })
	if err := root.AppendLocked("log.jsonl", func(state AppendState) ([]byte, error) {
		if readAll(t, state) != "new\n" {
			t.Fatalf("compose must see the file the name names: %q", readAll(t, state))
		}
		return []byte("appended\n"), nil
	}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(trail); err != nil || string(data) != "new\nappended\n" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "log.jsonl.kept")); err != nil || string(data) != "old\n" {
		t.Fatalf("the file the name no longer names is untouched: data=%q err=%v", data, err)
	}
}

// A file an append creates is opened to append too: a second writer that opens
// it as an existing file and appends first is not written over.
func TestAFileAnAppendCreatesIsOpenedToAppend(t *testing.T) {
	dir := t.TempDir()
	root := mustOpenRoot(t, dir)
	for _, access := range []int{os.O_WRONLY, os.O_RDWR} {
		name := "created.jsonl"
		if access == os.O_RDWR {
			name = "created-rw.jsonl"
		}
		created, isNew, err := root.openForAppend(name, access)
		if err != nil || !isNew {
			t.Fatalf("the absent file is created: %v %v", isNew, err)
		}
		other, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := other.WriteString("first\n"); err != nil {
			t.Fatal(err)
		}
		other.Close()
		if _, err := created.WriteString("second\n"); err != nil {
			t.Fatal(err)
		}
		created.Close()
		if data, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(data) != "first\nsecond\n" {
			t.Fatalf("data=%q err=%v", data, err)
		}
	}
}

// Whichever writer puts the first bytes in the file syncs its directory, so the
// entry survives a crash with them; a later write does not.
func TestTheFirstBytesSyncTheDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a directory is not synced on Windows")
	}
	dir := t.TempDir()
	root := mustOpenRoot(t, dir)
	write := func(name string) int64 {
		t.Helper()
		before := directorySyncs.Load()
		if err := root.AppendLocked(name, func(AppendState) ([]byte, error) { return []byte("x\n"), nil }); err != nil {
			t.Fatal(err)
		}
		return directorySyncs.Load() - before
	}
	if synced := write("created.jsonl"); synced != 1 {
		t.Fatalf("a created file's directory is synced: %d", synced)
	}
	if synced := write("created.jsonl"); synced != 0 {
		t.Fatalf("a file with bytes in it needs no directory sync: %d", synced)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if synced := write("empty.jsonl"); synced != 1 {
		t.Fatalf("the first bytes in a file another writer created sync its directory: %d", synced)
	}
}
