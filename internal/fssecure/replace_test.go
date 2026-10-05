package fssecure

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// entries is a directory's names, as one short string.
func entries(t *testing.T, dir string) string {
	t.Helper()
	listed, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range listed {
		names = append(names, entry.Name())
	}
	return strings.Join(names, ",")
}

// ReplaceByRename puts the new file in place whole, creating it or replacing
// the file its caller checked, and leaves nothing beside it.
func TestReplaceByRenamePutsTheFileInPlaceWhole(t *testing.T) {
	dir := t.TempDir()
	root, err := OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.ReplaceByRename("saved.json", []byte("first\n"), nil); err != nil {
		t.Fatal(err)
	}
	info, err := root.Lstat("saved.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := root.ReplaceByRename("saved.json", []byte("second\n"), info); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "saved.json")); err != nil || string(data) != "second\n" || entries(t, dir) != "saved.json" {
		t.Fatalf("%q %v; the directory holds %s", data, err, entries(t, dir))
	}
}

// What is at the name must still be what the caller checked, and a name must
// be one file directly in the directory. Every refusal leaves the name as it
// was and no temporary file beside it.
func TestReplaceByRenameReplacesOnlyWhatWasChecked(t *testing.T) {
	dir := t.TempDir()
	root, err := OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	write := func(name, text string) os.FileInfo {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		info, err := root.Lstat(name)
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	checked := write("checked.json", "checked\n")
	other := write("other.json", "other\n")
	for _, each := range []struct {
		name   string
		expect os.FileInfo
		want   error
	}{
		{"checked.json", nil, ErrReplacedChanged},    // something appeared after a check that found nothing
		{"checked.json", other, ErrReplacedChanged},  // another file is there than the one checked
		{"absent.json", checked, ErrReplacedChanged}, // the file checked is gone
		{"sub/saved.json", nil, ErrNotOneName},
		{"../saved.json", nil, ErrOutsideRoot},
	} {
		if err := root.ReplaceByRename(each.name, []byte("new\n"), each.expect); !errors.Is(err, each.want) {
			t.Errorf("%s: %v, want %v", each.name, err, each.want)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "checked.json")); string(data) != "checked\n" || entries(t, dir) != "checked.json,other.json" {
		t.Fatalf("%q; the directory holds %s", data, entries(t, dir))
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink("checked.json", filepath.Join(dir, "linked.json")); err != nil {
			t.Fatal(err)
		}
		if err := root.ReplaceByRename("linked.json", []byte("new\n"), checked); !errors.Is(err, ErrReplacedChanged) {
			t.Fatalf("a link where the checked file was: %v", err)
		}
		if data, _ := os.ReadFile(filepath.Join(dir, "checked.json")); string(data) != "checked\n" || entries(t, dir) != "checked.json,linked.json,other.json" {
			t.Fatalf("%q; the directory holds %s", data, entries(t, dir))
		}
	}
	// A write that fails leaves nothing behind: here the directory cannot be
	// written. Root ignores the mode, so this holds only for other users.
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		locked := t.TempDir()
		lockedRoot, err := OpenRoot(locked)
		if err != nil {
			t.Fatal(err)
		}
		defer lockedRoot.Close()
		if err := os.Chmod(locked, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0o700) })
		if err := lockedRoot.ReplaceByRename("saved.json", []byte("new\n"), nil); err == nil || entries(t, locked) != "" {
			t.Fatalf("an unwritable directory: %v; it holds %s", err, entries(t, locked))
		}
	}
}

// The directory written to is the one the handle holds: a path re-pointed
// after the open does not move the write. A build before Go 1.25 renames by
// the path, after checking that it still names the directory held, and then
// refuses instead.
func TestReplaceByRenameWritesToTheDirectoryHeld(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory links are not made here")
	}
	parent := t.TempDir()
	first, second := filepath.Join(parent, "first"), filepath.Join(parent, "second")
	for _, dir := range []string{first, second} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(link)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	err = root.ReplaceByRename("saved.json", []byte("new\n"), nil)
	if entries(t, second) != "" {
		t.Fatalf("the write followed the re-pointed path: %s", entries(t, second))
	}
	if renamesThroughHandle && (err != nil || entries(t, first) != "saved.json") {
		t.Fatalf("%v; the directory held holds %s", err, entries(t, first))
	}
	if !renamesThroughHandle && (err == nil || entries(t, first) != "") {
		t.Fatalf("a rename by path after the path was re-pointed: %v; the directory held holds %s", err, entries(t, first))
	}
}
