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
