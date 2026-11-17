package conformance

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
)

// The rule for a Windows reparse point, by an injected mode: a junction
// (ModeIrregular since Go 1.23, with or without ModeDir) is refused, as is
// any reparse point Go reads as neither a link nor a regular file; a link is
// left to the link check, and a regular file, a deduplicated one included,
// stays. A refusal is EMIT-HARNESS-BOUND at the reparse point's path.
func TestRefusesReparse(t *testing.T) {
	cases := []struct {
		name    string
		mode    fs.FileMode
		reparse bool
		want    bool
	}{
		{"a junction", fs.ModeIrregular | 0o666, true, true},
		{"a junction Go reads as a directory too", fs.ModeDir | fs.ModeIrregular | 0o777, true, true},
		{"irregular, the attribute unread", fs.ModeIrregular, false, true},
		{"a directory reparse point Go reads as a directory", fs.ModeDir | 0o777, true, true},
		{"a socket reparse point", fs.ModeSocket, true, true},
		{"a symbolic link", fs.ModeSymlink | 0o666, true, false},
		{"a junction under winsymlink=0, a link", fs.ModeSymlink, true, false},
		{"a deduplicated file", 0o666, true, false},
		{"a directory", fs.ModeDir | 0o755, false, false},
		{"a regular file", 0o644, false, false},
		{"a socket elsewhere", fs.ModeSocket, false, false},
	}
	for _, c := range cases {
		if got := refusesReparse(c.mode, c.reparse); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	d := copyFailure(reparseError{path: "docs/out"})
	if d == nil || d.Code != "EMIT-HARNESS-BOUND" || d.Location.Document != emit.DocumentRepository || d.Location.Path != "docs/out" {
		t.Fatalf("diagnostic: %+v", d)
	}
}

// A reparse point that is not a link is refused by the link check itself,
// before the copy is made and before any run, on every system: reparsePoint is
// injected to report the directory docs/out as one, and an attribute that
// cannot be read as unreadable.
func TestTheLinkCheckRefusesAReportedReparsePoint(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "docs", "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "docs", "out", "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	real := reparsePoint
	t.Cleanup(func() { reparsePoint = real })
	cases := []struct {
		name string
		fake func(fs.DirEntry) (bool, error)
		code string
	}{
		{"a reparse point", func(fs.DirEntry) (bool, error) { return true, nil }, "EMIT-HARNESS-BOUND"},
		{"attributes unread", func(fs.DirEntry) (bool, error) { return false, errors.New("unread") }, "EMIT-PATH-UNREADABLE"},
	}
	for _, c := range cases {
		reparsePoint = func(entry fs.DirEntry) (bool, error) {
			if entry.Name() == "out" {
				return c.fake(entry)
			}
			return real(entry)
		}
		if d := copyFailure(checkLinks(source)); d == nil || d.Code != c.code || d.Location.Path != "docs/out" {
			t.Errorf("%s: checkLinks: %+v, want %s at docs/out", c.name, d, c.code)
		}
		w, d := newWorkspace(source)
		if w != nil {
			t.Errorf("%s: a workspace was made", c.name)
		}
		if d == nil || d.Code != c.code || d.Location.Document != emit.DocumentRepository || d.Location.Path != "docs/out" {
			t.Errorf("%s: diagnostic %+v, want %s at docs/out", c.name, d, c.code)
		}
	}
}
