package fssecure

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func exactTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "packs", "pack.json"), []byte(`{"in":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readExact(root *Root, relative string) (string, error) {
	file, err := root.OpenExact(relative)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := readBounded(file, 1024)
	return string(data), err
}

// OpenExact reads what Open reads at its own spelling, and an absent file
// is absent, as Open reports it.
func TestOpenExactReadsAPlainPath(t *testing.T) {
	root := mustOpenRoot(t, exactTree(t))
	if data, err := readExact(root, "packs/pack.json"); err != nil || data != `{"in":true}` {
		t.Fatalf("data=%q err=%v", data, err)
	}
	for _, absent := range []string{"packs/absent.json", "absent/pack.json"} {
		if _, err := root.OpenExact(absent); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: %v, want not exist", absent, err)
		}
	}
	for _, refused := range []string{"packs", "../pack.json"} {
		if _, err := root.OpenExact(refused); err == nil || errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: %v, want a read error", refused, err)
		}
	}
}

// A link in a middle component that stays inside the root is followed by
// Open and refused by OpenExact, as a final one is.
func TestOpenExactRefusesALinkInAnyComponent(t *testing.T) {
	dir := exactTree(t)
	if err := os.Symlink("packs", filepath.Join(dir, "link")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := os.Symlink("pack.json", filepath.Join(dir, "packs", "alias.json")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	root := mustOpenRoot(t, dir)
	if data, err := root.Read("link/pack.json", 1024); err != nil || string(data) != `{"in":true}` {
		t.Fatalf("Open must keep following an inward directory link: data=%q err=%v", data, err)
	}
	for _, linked := range []string{"link/pack.json", "packs/alias.json"} {
		if _, err := root.OpenExact(linked); !errors.Is(err, ErrNotExact) {
			t.Errorf("%s: %v, want ErrNotExact", linked, err)
		}
	}
}

// A component is spelled as its directory entry is, whatever the
// filesystem would resolve: the entry check refuses another spelling on a
// case-sensitive filesystem too.
func TestSpelledExactlyComparesTheEntry(t *testing.T) {
	root := mustOpenRoot(t, exactTree(t))
	if err := spelledExactly(root.root, "packs"); err != nil {
		t.Fatalf("packs: %v", err)
	}
	for _, other := range []string{"Packs", "PACKS", "packs "} {
		if err := spelledExactly(root.root, other); !errors.Is(err, ErrNotExact) {
			t.Errorf("%q: %v, want ErrNotExact", other, err)
		}
	}
}

// On a case-insensitive filesystem a path spelled otherwise names the same
// file, and Open opens it; OpenExact refuses it as a read error.
func TestOpenExactRefusesAnotherSpellingOnACaseInsensitiveFilesystem(t *testing.T) {
	dir := exactTree(t)
	if _, err := os.Lstat(filepath.Join(dir, "PACKS", "PACK.JSON")); err != nil {
		t.Skipf("the filesystem under %s is case-sensitive: %v", filepath.Base(dir), err)
	}
	root := mustOpenRoot(t, dir)
	if _, err := root.Read("PACKS/PACK.JSON", 1024); err != nil {
		t.Fatalf("Open resolves another spelling here: %v", err)
	}
	for _, spelled := range []string{"PACKS/pack.json", "packs/Pack.json", strings.ToUpper("packs/pack.json")} {
		if _, err := root.OpenExact(spelled); !errors.Is(err, ErrNotExact) {
			t.Errorf("%s: %v, want ErrNotExact", spelled, err)
		}
	}
}
