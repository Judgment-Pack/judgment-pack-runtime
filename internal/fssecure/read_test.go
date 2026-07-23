package fssecure

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadRegularReadsFileAndRejectsFinalSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := ReadRegular(target, 2)
	if err != nil || string(data) != "{}" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	if _, err := ReadRegular(target, 1); err == nil {
		t.Fatal("expected byte-limit failure")
	}
	if runtime.GOOS == "windows" {
		return
	}
	link := filepath.Join(root, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if _, err := ReadRegular(link, 2); err == nil {
		t.Fatal("expected final-symlink rejection")
	}
}
