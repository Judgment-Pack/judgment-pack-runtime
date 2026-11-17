//go:build windows

package fssecure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/junctiontest"
)

// A junction as the final component is refused as a link is: by the
// open of a named file, and by an append beneath a root, before anything
// is opened through it.
func TestAJunctionIsRefusedAsALink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	junctiontest.Make(t, filepath.Join(dir, "j"), target)
	if info, err := os.Lstat(filepath.Join(dir, "j")); err != nil || !LinkLike(info) {
		t.Fatalf("the junction does not read as link-like: %v %v", info, err)
	}
	if file, err := OpenRegular(filepath.Join(dir, "j")); err == nil || !strings.Contains(err.Error(), "link or junction") {
		if file != nil {
			file.Close()
		}
		t.Fatalf("a final junction was opened: %v", err)
	}
	root, err := OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Append("j", []byte("x\n")); err == nil || !strings.Contains(err.Error(), "link or junction") {
		t.Fatalf("an append to a final junction: %v", err)
	}
}
