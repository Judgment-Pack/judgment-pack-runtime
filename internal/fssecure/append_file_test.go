package fssecure

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func heldAppend(t *testing.T) (string, *AppendFile) {
	t.Helper()
	dir := t.TempDir()
	root := mustOpenRoot(t, dir)
	held, err := root.OpenAppend("held.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { held.Close() })
	return filepath.Join(dir, "held.jsonl"), held
}

func appendLine(held *AppendFile) error {
	return held.AppendLocked(nil, func(AppendState) ([]byte, error) { return []byte("line\n"), nil })
}

// A file that another name also names is refused when it is opened to be held.
func TestOpenAppendRefusesAHardLinkedFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the link count is not read on Windows")
	}
	dir := t.TempDir()
	name := filepath.Join(dir, "held.jsonl")
	if err := os.WriteFile(name, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(name, filepath.Join(dir, "other")); err != nil {
		t.Fatal(err)
	}
	root := mustOpenRoot(t, dir)
	if held, err := root.OpenAppend("held.jsonl"); err == nil {
		held.Close()
		t.Fatal("a hard-linked file was held")
	}
}

// A link made to the held file while it is held refuses the next append, and
// nothing is written through either name.
func TestHeldAppendRefusesALinkMadeWhileHeld(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the link count is not read on Windows")
	}
	name, held := heldAppend(t)
	other := filepath.Join(t.TempDir(), "other")
	if err := os.Link(name, other); err != nil {
		t.Fatal(err)
	}
	if err := appendLine(held); !errors.Is(err, ErrHeldFileChanged) {
		t.Fatalf("append through a linked file: %v", err)
	}
	if data, err := os.ReadFile(other); err != nil || len(data) != 0 {
		t.Fatalf("linked file: %q %v", data, err)
	}
}

// A held file whose name was removed, or moved and replaced, is refused rather
// than appended to where nothing, or something else, names it.
func TestHeldAppendRefusesARemovedOrReplacedName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot remove or rename these open files")
	}
	t.Run("removed", func(t *testing.T) {
		name, held := heldAppend(t)
		if err := os.Remove(name); err != nil {
			t.Fatal(err)
		}
		if err := appendLine(held); !errors.Is(err, ErrHeldFileChanged) {
			t.Fatalf("append after removal: %v", err)
		}
	})
	t.Run("replaced", func(t *testing.T) {
		name, held := heldAppend(t)
		if err := os.Rename(name, name+".old"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := appendLine(held); !errors.Is(err, ErrHeldFileChanged) {
			t.Fatalf("append after replacement: %v", err)
		}
		for _, each := range []string{name, name + ".old"} {
			if data, err := os.ReadFile(each); err != nil || len(data) != 0 {
				t.Fatalf("%s: %q %v", each, data, err)
			}
		}
	})
	t.Run("unchanged", func(t *testing.T) {
		name, held := heldAppend(t)
		if err := appendLine(held); err != nil {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(name); err != nil || string(data) != "line\n" {
			t.Fatalf("held file: %q %v", data, err)
		}
	})
}
