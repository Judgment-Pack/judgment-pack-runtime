//go:build unix

package federation

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The directory is created 0700 and the record written 0600.
func TestRecordModesWritten(t *testing.T) {
	store := NewRecordStore(filepath.Join(t.TempDir(), "records"))
	if code, problem := store.record(recordID, nil, 1, digestA, one()); code != "" || problem != nil {
		t.Fatalf("%s %v", code, problem)
	}
	path, _, _ := store.paths(recordID)
	for name, want := range map[string]os.FileMode{store.dir: 0o700, path: 0o600} {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm()&^want != 0 {
			t.Errorf("%s: %v %v", filepath.Base(name), info.Mode(), err)
		}
	}
}

// A record directory or file that its group or other users can write, a
// record that is a link or not a regular file, and a directory that is a
// link, are refused when read and when written, and nothing is written.
func TestRecordRefusesWhatOthersCanChange(t *testing.T) {
	cases := map[string]func(t *testing.T, store *RecordStore, path string){
		"group-writable directory": func(t *testing.T, store *RecordStore, _ string) { chmod(t, store.dir, 0o770) },
		"world-writable directory": func(t *testing.T, store *RecordStore, _ string) { chmod(t, store.dir, 0o703) },
		"group-writable record":    func(t *testing.T, _ *RecordStore, path string) { chmod(t, path, 0o620) },
		"world-writable record":    func(t *testing.T, _ *RecordStore, path string) { chmod(t, path, 0o602) },
		"record through a link": func(t *testing.T, store *RecordStore, path string) {
			real := filepath.Join(store.dir, "elsewhere")
			if err := os.Rename(path, real); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, path); err != nil {
				t.Fatal(err)
			}
		},
		"record not a regular file": func(t *testing.T, _ *RecordStore, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			store := NewRecordStore(filepath.Join(t.TempDir(), "records"))
			writeRecord(t, store, Record{3, digestA})
			path, _, _ := store.paths(recordID)
			arrange(t, store, path)
			before := snapshot(t, store.dir)
			if _, problem := store.read(recordID); problem == nil {
				t.Fatal("read")
			}
			if code, problem := store.record(recordID, &Record{3, digestA}, 4, digestB, nil); code != "" || problem == nil {
				t.Fatalf("written: %s", code)
			}
			if after := snapshot(t, store.dir); after != before {
				t.Fatalf("changed:\n%s\n%s", before, after)
			}
		})
	}
	t.Run("directory through a link", func(t *testing.T) {
		dir := t.TempDir()
		real := filepath.Join(dir, "real")
		if err := os.Mkdir(real, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "records")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		store := NewRecordStore(link)
		if _, problem := store.read(recordID); problem == nil || !strings.Contains(problem.Reason, "link") {
			t.Fatalf("%v", problem)
		}
		if _, problem := store.record(recordID, nil, 1, digestA, one()); problem == nil {
			t.Fatal("written through a link")
		}
		if entries, _ := os.ReadDir(real); len(entries) != 0 {
			t.Fatalf("%v", entries)
		}
	})
}

func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// snapshot is a directory's names, modes and contents.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var out strings.Builder
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, _ := entry.Info()
		data, _ := os.ReadFile(filepath.Join(dir, entry.Name()))
		out.WriteString(entry.Name() + " " + info.Mode().String() + " " + string(data) + "\n")
	}
	return out.String()
}

// A record replaced by a pipe between its check and its open is refused at
// once, never waited on, so no verification hangs holding the lock file.
func TestRecordRefusesAPipeSwappedInBeforeTheOpen(t *testing.T) {
	store := NewRecordStore(filepath.Join(t.TempDir(), "records"))
	writeRecord(t, store, Record{3, digestA})
	path, _, _ := store.paths(recordID)
	t.Cleanup(StandInRecordHooksForTests(RecordHooks{BeforeOpen: func(path string) {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Error(err)
		}
	}}))
	t.Cleanup(func() {
		// Release a reader waiting on the pipe, whatever the read did.
		if writer, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			writer.Close()
		}
	})
	done := make(chan *RecordError, 1)
	go func() {
		_, problem := store.read(recordID)
		done <- problem
	}()
	select {
	case problem := <-done:
		if problem == nil || !strings.Contains(problem.Reason, "one regular file") {
			t.Fatalf("%v", problem)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the read waited on a pipe")
	}
}
