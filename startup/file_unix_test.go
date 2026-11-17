//go:build unix

package startup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const onUnix = true

func TestReadFileTrimsExactlyOneTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	for i, c := range newlineCases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(dir, "case-"+string(rune('a'+i)))
			writeFile(t, path, c.raw, 0o600)
			secret, err := ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(secret.Reveal()); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestReadFileRefusesAWorldReadableFile(t *testing.T) {
	dir := t.TempDir()
	for _, mode := range []os.FileMode{0o400, 0o440, 0o600, 0o640, 0o660, 0o700, 0o750, 0o620} {
		path := filepath.Join(dir, "allowed-"+mode.String())
		writeFile(t, path, "value\n", mode)
		if _, err := ReadFile(path); err != nil {
			t.Errorf("%v: got %v, want the value", mode, err)
		}
	}
	for _, mode := range []os.FileMode{0o604, 0o644, 0o444, 0o004, 0o666, 0o755, 0o777, 0o605} {
		path := filepath.Join(dir, "refused-"+mode.String())
		writeFile(t, path, "value\n", mode)
		_, err := ReadFile(path)
		if !errors.Is(err, ErrWorldReadable) {
			t.Errorf("%v: got %v, want ErrWorldReadable", mode, err)
		}
		if err != nil && !strings.Contains(err.Error(), path) {
			t.Errorf("%v: the refusal does not name the file: %v", mode, err)
		}
	}
}

func TestReadFileFollowsALinkAndChecksItsTarget(t *testing.T) {
	dir := t.TempDir()
	// A mounted secret volume links each key to a file in a versioned
	// directory; the target's bits are the ones that count.
	private := filepath.Join(dir, "private")
	public := filepath.Join(dir, "public")
	writeFile(t, private, "linked\n", 0o400)
	writeFile(t, public, "linked\n", 0o644)
	for target, want := range map[string]error{private: nil, public: ErrWorldReadable} {
		link := filepath.Join(dir, "link-"+filepath.Base(target))
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		secret, err := ReadFile(link)
		if !errors.Is(err, want) {
			t.Errorf("%s: got %v, want %v", link, err, want)
		}
		if want == nil && string(secret.Reveal()) != "linked" {
			t.Errorf("%s: got %q", link, secret.Reveal())
		}
	}
}

func TestReadFileRefusals(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadFile(filepath.Join(dir, "absent")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("absent: got %v", err)
	}
	directory := filepath.Join(dir, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(directory); !errors.Is(err, ErrNotRegular) {
		t.Errorf("directory: got %v, want ErrNotRegular", err)
	}
	pipe := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Fatal(err)
	}
	// Refused before the open, which on a pipe would wait for a writer.
	if _, err := ReadFile(pipe); !errors.Is(err, ErrNotRegular) {
		t.Errorf("pipe: got %v, want ErrNotRegular", err)
	}
	for _, raw := range []string{"", "\n", "\r\n"} {
		path := filepath.Join(dir, "empty")
		writeFile(t, path, raw, 0o600)
		if _, err := ReadFile(path); !errors.Is(err, ErrEmpty) {
			t.Errorf("%q: got %v, want ErrEmpty", raw, err)
		}
	}
	at := filepath.Join(dir, "at-bound")
	writeFile(t, at, strings.Repeat("v", MaxSecretBytes)+"\r\n", 0o600)
	if secret, err := ReadFile(at); err != nil || len(secret.Reveal()) != MaxSecretBytes {
		t.Errorf("at the bound: got %v", err)
	}
	for _, content := range []string{strings.Repeat("v", MaxSecretBytes+1), strings.Repeat("v", MaxSecretBytes+1) + "\r\n", strings.Repeat("v", 4*MaxSecretBytes)} {
		over := filepath.Join(dir, "over-bound")
		writeFile(t, over, content, 0o600)
		if _, err := ReadFile(over); !errors.Is(err, ErrLimit) {
			t.Errorf("%d bytes: got %v, want ErrLimit", len(content), err)
		}
	}
	unreadable := filepath.Join(dir, "unreadable")
	writeFile(t, unreadable, "value\n", 0o000)
	if os.Geteuid() != 0 {
		if _, err := ReadFile(unreadable); !errors.Is(err, os.ErrPermission) {
			t.Errorf("unreadable: got %v, want a permission error", err)
		}
	}
}

// The platforms' default secret mounts are world-readable: Cloud Run's
// secret volumes 0444, Kubernetes' 0644. They are refused by default and read
// under FileRules.AllowOtherRead, through ReadFileWith and through Bindings;
// the opt-in relaxes the permission check alone.
func TestReadFileWithAllowOtherReadAcceptsThePlatformDefaults(t *testing.T) {
	dir := t.TempDir()
	open := FileRules{AllowOtherRead: true}
	for _, mode := range []os.FileMode{0o444, 0o644, 0o400, 0o440} {
		path := filepath.Join(dir, "mounted-"+mode.String())
		writeFile(t, path, "value\n", mode)
		secret, err := ReadFileWith(path, open)
		if err != nil || string(secret.Reveal()) != "value" {
			t.Errorf("%v under AllowOtherRead: got %v", mode, err)
		}
		bindings := Bindings{Files: map[string]string{"secret:ref": path}, FileRules: open}
		if secret, err := bindings.ReadSecret(context.Background(), "secret:ref"); err != nil || string(secret.Reveal()) != "value" {
			t.Errorf("%v through Bindings under AllowOtherRead: got %v", mode, err)
		}
		if mode&0o004 == 0 {
			continue
		}
		if _, err := ReadFileWith(path, FileRules{}); !errors.Is(err, ErrWorldReadable) {
			t.Errorf("%v under the zero FileRules: got %v, want ErrWorldReadable", mode, err)
		}
		strict := Bindings{Files: map[string]string{"secret:ref": path}}
		if _, err := strict.ReadSecret(context.Background(), "secret:ref"); !errors.Is(err, ErrWorldReadable) {
			t.Errorf("%v through Bindings by default: got %v, want ErrWorldReadable", mode, err)
		}
	}
	// The opt-in does not relax the other checks.
	empty := filepath.Join(dir, "empty")
	writeFile(t, empty, "\n", 0o444)
	if _, err := ReadFileWith(empty, open); !errors.Is(err, ErrEmpty) {
		t.Errorf("an empty file under AllowOtherRead: got %v, want ErrEmpty", err)
	}
	if _, err := ReadFileWith(dir, open); !errors.Is(err, ErrNotRegular) {
		t.Errorf("a directory under AllowOtherRead: got %v, want ErrNotRegular", err)
	}
}

// A named pipe swapped in after ReadFile's first check is opened without
// waiting for a writer, so the check on the opened file can refuse it.
func TestReadFileOpensWithoutWaitingOnANamedPipe(t *testing.T) {
	pipe := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Fatal(err)
	}
	opened := make(chan error, 1)
	go func() {
		file, err := openFile(pipe)
		if err == nil {
			var info os.FileInfo
			if info, err = file.Stat(); err == nil {
				err = checkFile(info, FileRules{})
			}
			file.Close()
		}
		opened <- err
	}()
	select {
	case err := <-opened:
		if !errors.Is(err, ErrNotRegular) {
			t.Fatalf("got %v, want ErrNotRegular", err)
		}
	case <-time.After(10 * time.Second):
		// Release the open that waits, by opening the pipe's other end.
		if writer, err := os.OpenFile(pipe, os.O_WRONLY, 0); err == nil {
			writer.Close()
		}
		<-opened
		t.Fatal("the open waited for a writer on a named pipe")
	}
}
