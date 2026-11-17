package conformance

import (
	"io/fs"
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
