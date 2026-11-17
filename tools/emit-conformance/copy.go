package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
)

// copyTracked copies the files git tracks under root, as the working tree
// holds them (local edits included), into a fresh temporary directory, and
// returns that directory's real path and a function that removes it. Ignored
// and untracked paths, such as an in-tree Python venv whose interpreter links
// out of the tree, are not copied, so the harness never sees them. A tracked
// symbolic link stays a link with the same target, so the harness still
// refuses one that points out of the tree. A tracked file missing from the
// working tree is left out, as the working tree has it; a submodule is left
// out too.
func copyTracked(root string) (string, func(), error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--stage")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", nil, fmt.Errorf("git ls-files: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	dir, err := os.MkdirTemp("", "emit-conformance-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if err := copyEntries(root, dir, out); err != nil {
		cleanup()
		return "", nil, err
	}
	real, err := emit.RealRoot(dir)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return real, cleanup, nil
}

// copyEntries copies each entry of `git ls-files -z --stage` output from root
// into dir.
func copyEntries(root, dir string, listing []byte) error {
	seen := map[string]bool{}
	for _, entry := range bytes.Split(listing, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		meta, path, ok := strings.Cut(string(entry), "\t")
		if !ok {
			return fmt.Errorf("git ls-files: an entry without a path: %q", entry)
		}
		if strings.HasPrefix(meta, "160000 ") || seen[path] {
			continue // a submodule, or another stage of a conflicted path
		}
		seen[path] = true
		if err := copyOne(filepath.Join(root, filepath.FromSlash(path)), filepath.Join(dir, filepath.FromSlash(path))); err != nil {
			return err
		}
	}
	return nil
}

func copyOne(src, dst string) error {
	info, err := os.Lstat(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case info.Mode().IsRegular():
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	default:
		return fmt.Errorf("%s: a tracked path that is neither a file nor a symbolic link", src)
	}
}
