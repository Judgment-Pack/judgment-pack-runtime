package fssecure

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenSnapshotRetriesChangedIdentities(t *testing.T) {
	for _, changed := range []string{"trail", "companion", "absent"} {
		t.Run(changed, func(t *testing.T) {
			if runtime.GOOS == "windows" && changed != "absent" {
				t.Skip("replacement identity retries are not exercised because a held file cannot be renamed over on Windows")
			}
			dir := t.TempDir()
			trail, side := filepath.Join(dir, "trail"), filepath.Join(dir, "side")
			write := func(name, data string) {
				t.Helper()
				if err := os.WriteFile(name, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write(trail, "a\n")
			if changed != "absent" {
				write(side, "old\n")
			}
			trailOpens, sideOpens := 0, 0
			openTrail := func() (*os.File, error) {
				trailOpens++
				if trailOpens == 1 && changed == "trail" {
					write(trail+".new", "new trail\n")
				}
				file, err := os.Open(trail)
				if err == nil && trailOpens == 1 && changed == "trail" {
					if err := os.Rename(trail+".new", trail); err != nil {
						file.Close()
						t.Fatal(err)
					}
				}
				return file, err
			}
			openSide := func() (*os.File, error) {
				sideOpens++
				if sideOpens == 1 && changed != "trail" {
					write(side+".new", "new side\n")
				}
				file, err := os.Open(side)
				if sideOpens == 1 && changed != "trail" {
					if err := os.Rename(side+".new", side); err != nil {
						if file != nil {
							file.Close()
						}
						t.Fatal(err)
					}
				}
				if errors.Is(err, os.ErrNotExist) {
					return nil, nil
				}
				return file, err
			}
			files, sizes, _, err := OpenSnapshot(openTrail, openSide)
			if err != nil {
				t.Fatal(err)
			}
			defer files[0].Close()
			defer files[1].Close()
			if trailOpens < 3 || sideOpens < 3 {
				t.Fatalf("no retry: trail=%d side=%d", trailOpens, sideOpens)
			}
			for i, name := range []string{trail, side} {
				info, err := os.Stat(name)
				if err != nil {
					t.Fatal(err)
				}
				held, err := files[i].Stat()
				if err != nil || !os.SameFile(info, held) || sizes[i] != info.Size() {
					t.Fatalf("stale identity or size for %s", name)
				}
			}
		})
	}
}

func TestOpenSnapshotOpensCompanionsUnderTheLock(t *testing.T) {
	name := filepath.Join(t.TempDir(), "trail")
	if err := os.WriteFile(name, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalLock, originalUnlock := lockShared, unlockFile
	t.Cleanup(func() { lockShared, unlockFile = originalLock, originalUnlock })
	held := false
	lockShared = func(*os.File) error { held = true; return nil }
	unlockFile = func(*os.File) error { held = false; return nil }
	openTrail := func() (*os.File, error) { return os.Open(name) }
	openSide := func() (*os.File, error) {
		if !held {
			t.Error("companion opened outside shared lock")
		}
		return nil, nil
	}
	files, sizes, locked, err := OpenSnapshot(openTrail, openSide)
	if err != nil {
		t.Fatal(err)
	}
	files[0].Close()
	if !locked || held || sizes[0] != 2 || sizes[1] != 0 {
		t.Fatalf("snapshot: %v %v %v", sizes, locked, held)
	}
	lockShared = func(*os.File) error { return errNoLock }
	files, sizes, locked, err = OpenSnapshot(openTrail, func() (*os.File, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	files[0].Close()
	if locked || sizes[0] != 2 {
		t.Fatalf("no-lock snapshot: %v %v", sizes, locked)
	}
	lockShared = func(*os.File) error { return injected }
	if _, _, _, err := OpenSnapshot(openTrail); !errors.Is(err, injected) {
		t.Fatalf("lock failure: %v", err)
	}
}

func TestOpenSnapshotRefusesContinuallyChangingPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("continually replaced paths are not exercised because a held file cannot be renamed over on Windows")
	}
	name := filepath.Join(t.TempDir(), "trail")
	var opened []*os.File
	open := func() (*os.File, error) {
		if err := os.WriteFile(name+".new", []byte("x\n"), 0o600); err != nil {
			return nil, err
		}
		if err := os.Rename(name+".new", name); err != nil {
			return nil, err
		}
		file, err := os.Open(name)
		if err == nil {
			opened = append(opened, file)
		}
		return file, err
	}
	files, _, _, err := OpenSnapshot(open)
	if err == nil || files != nil || len(opened) != 2*appendOpenAttempts {
		t.Fatalf("unstable snapshot: files=%v opens=%d err=%v", files, len(opened), err)
	}
	for _, file := range opened {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("retry leaked a file: %v", err)
		}
	}
}
