package emit

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// irregularAt is a FileInfo Go would give a Windows junction.
type irregularAt struct{ fs.FileInfo }

func (irregularAt) Mode() fs.FileMode { return fs.ModeIrregular }

// A root any of whose ancestors reads as a reparse point is refused with
// ErrRootReparse; a root whose ancestors are plain directories is read.
func TestARootBelowAReparsePointIsRefused(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "a", "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := RealRoot(root); err != nil {
		t.Fatalf("a plain root: %v", err)
	}
	real, err := filepath.EvalSymlinks(filepath.Join(parent, "a"))
	if err != nil {
		t.Fatal(err)
	}
	saved := rootLstat
	defer func() { rootLstat = saved }()
	rootLstat = func(name string) (fs.FileInfo, error) {
		info, err := saved(name)
		if err == nil && name == real {
			return irregularAt{info}, nil
		}
		return info, err
	}
	if _, err := RealRoot(root); !errors.Is(err, ErrRootReparse) {
		t.Fatalf("a root below a reparse point: %v", err)
	}
}

// When the root cannot be resolved at all, as a root below a Windows junction
// cannot, an ancestor that reads as a reparse point is still what the refusal
// names (ErrRootReparse); with none, the resolution's own error is returned.
func TestAnUnresolvableRootBelowAReparsePointIsRefused(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "a", "missing", "repo")
	if _, err := RealRoot(root); err == nil || errors.Is(err, ErrRootReparse) {
		t.Fatalf("an unresolvable plain root: %v", err)
	}
	junction := filepath.Join(parent, "a")
	saved := rootLstat
	defer func() { rootLstat = saved }()
	rootLstat = func(name string) (fs.FileInfo, error) {
		info, err := saved(name)
		if err == nil && name == junction {
			return irregularAt{info}, nil
		}
		return info, err
	}
	if _, err := RealRoot(root); !errors.Is(err, ErrRootReparse) {
		t.Fatalf("an unresolvable root below a reparse point: %v", err)
	}
}
