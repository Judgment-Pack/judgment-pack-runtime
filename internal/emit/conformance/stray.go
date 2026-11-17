package conformance

import (
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
)

// The bounds of the repository copy the emitter runs in (check 6, ADR-0099).
// A repository past them refuses the run (EMIT-HARNESS-BOUND).
const (
	// MaxCopyBytes bounds the bytes of the repository's files.
	MaxCopyBytes = 256 << 20
	// MaxCopyEntries bounds its files, directories and links together.
	MaxCopyEntries = 100000
)

// The bounds as the code applies them; a test lowers them.
var (
	maxCopyBytes   int64 = MaxCopyBytes
	maxCopyEntries       = MaxCopyEntries
)

var (
	errCopyBound = errors.New("conformance: the repository is past the copy's bounds")
	errCopyLink  = errors.New("conformance: a symbolic link could not be made")
)

// unreadableError names a path of the repository that could not be copied.
type unreadableError struct{ path string }

func (e unreadableError) Error() string { return "conformance: a path could not be copied" }

// entryKind is what a path of the repository is.
type entryKind byte

const (
	kindFile entryKind = 'f'
	kindDir  entryKind = 'd'
	kindLink entryKind = 'l'
)

// entryState is what the stray-write check compares of one path: its kind
// and permission bits, and a file's size and digest or a link's target.
type entryState struct {
	kind   entryKind
	perm   fs.FileMode
	size   int64
	sum    [sha256.Size]byte
	target string
}

// workspace is the fresh copy of the repository the emitter runs in, and
// what it held when it was made. In fallback the emitter runs in the
// repository itself, and stray writes are not checked.
type workspace struct {
	source   string
	dir      string
	root     string
	fallback bool
	before   map[string]entryState
}

// newWorkspace copies the repository root into a fresh temporary directory,
// passing over anything named ".git" in any case. A system that cannot make
// a symbolic link the repository holds (Windows without the privilege) gets
// the fallback.
func newWorkspace(source string) (*workspace, *emit.Diagnostic) {
	w := &workspace{source: source}
	if diagnostic := w.fresh(); diagnostic != nil {
		return nil, diagnostic
	}
	return w, nil
}

func (w *workspace) fresh() *emit.Diagnostic {
	dir, err := os.MkdirTemp("", "jpack-harness-repo-")
	if err != nil {
		d := emit.NewDiagnostic("EMIT-PATH-UNREADABLE", emit.Location{Document: emit.DocumentRepository})
		return &d
	}
	root, err := emit.RealRoot(dir)
	if err != nil {
		removeTree(dir)
		d := emit.NewDiagnostic("EMIT-PATH-UNREADABLE", emit.Location{Document: emit.DocumentRepository})
		return &d
	}
	before, err := copyTree(w.source, root)
	var unreadable unreadableError
	switch {
	case errors.Is(err, errCopyLink):
		removeTree(dir)
		w.dir, w.root, w.fallback, w.before = "", w.source, true, nil
		return nil
	case errors.Is(err, errCopyBound):
		removeTree(dir)
		d := emit.NewDiagnostic("EMIT-HARNESS-BOUND", emit.Location{Document: emit.DocumentRepository})
		return &d
	case errors.As(err, &unreadable):
		removeTree(dir)
		d := emit.NewDiagnostic("EMIT-PATH-UNREADABLE", emit.Location{Document: emit.DocumentRepository, Path: unreadable.path})
		return &d
	case err != nil:
		removeTree(dir)
		d := emit.NewDiagnostic("EMIT-PATH-UNREADABLE", emit.Location{Document: emit.DocumentRepository})
		return &d
	}
	w.dir, w.root, w.before = dir, root, before
	return nil
}

// close removes the copy.
func (w *workspace) close() {
	if w.dir != "" {
		removeTree(w.dir)
		w.dir = ""
	}
}

// strayWrites compares the copy with what it held when it was made. When
// the emitter changed it, the copy is made again, so that every vector runs
// in a fresh one.
func (w *workspace) strayWrites() ([]StrayWrite, *emit.Diagnostic) {
	if w.fallback {
		return nil, nil
	}
	changes := diffTree(w.root, w.before)
	if len(changes) == 0 {
		return nil, nil
	}
	w.close()
	return changes, w.fresh()
}

// isGitName says whether a name is git's directory in any case, as the host
// reads it.
func isGitName(name string) bool { return strings.EqualFold(name, ".git") }

// copyTree copies the regular files, directories and symbolic links below
// source into target, which exists and is empty, keeping permission bits and
// link targets and never following a link; it passes over anything named
// ".git" and over target itself. It returns what it copied, by slash path.
func copyTree(source, target string) (map[string]entryState, error) {
	states := map[string]entryState{}
	var total int64
	var dirs []string
	err := filepath.WalkDir(source, func(full string, entry fs.DirEntry, err error) error {
		relative, relErr := filepath.Rel(source, full)
		if relErr != nil {
			return relErr
		}
		name := filepath.ToSlash(relative)
		if err != nil {
			return unreadableError{path: name}
		}
		if name == "." {
			return nil
		}
		if isGitName(entry.Name()) || full == target {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if len(states) >= maxCopyEntries {
			return errCopyBound
		}
		info, err := entry.Info()
		if err != nil {
			return unreadableError{path: name}
		}
		destination := filepath.Join(target, relative)
		switch kind := entry.Type(); {
		case kind&fs.ModeSymlink != 0:
			link, err := os.Readlink(full)
			if err != nil {
				return unreadableError{path: name}
			}
			if err := os.Symlink(link, destination); err != nil {
				return errCopyLink
			}
			states[name] = entryState{kind: kindLink, target: link}
		case kind.IsDir():
			if err := os.Mkdir(destination, 0o700); err != nil {
				return err
			}
			dirs = append(dirs, destination)
			states[name] = entryState{kind: kindDir, perm: info.Mode().Perm()}
		case kind.IsRegular():
			total += info.Size()
			if total > maxCopyBytes {
				return errCopyBound
			}
			state, err := copyFile(full, destination, info.Mode().Perm())
			if err != nil {
				return unreadableError{path: name}
			}
			states[name] = state
		default:
			return unreadableError{path: name}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// A directory gets its own bits once everything below it is written.
	slices.Reverse(dirs)
	for _, dir := range dirs {
		relative, _ := filepath.Rel(target, dir)
		if err := os.Chmod(dir, states[filepath.ToSlash(relative)].perm); err != nil {
			return nil, err
		}
	}
	return states, nil
}

func copyFile(source, destination string, perm fs.FileMode) (entryState, error) {
	in, err := os.Open(source)
	if err != nil {
		return entryState{}, err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return entryState{}, err
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(in, maxCopyBytes+1))
	if err = errors.Join(err, out.Close()); err != nil {
		return entryState{}, err
	}
	if err := os.Chmod(destination, perm); err != nil {
		return entryState{}, err
	}
	state := entryState{kind: kindFile, perm: perm, size: size}
	copy(state.sum[:], hash.Sum(nil))
	return state, nil
}

// diffTree lists what differs in root from before: a path removed, or changed
// in kind, bits, size, bytes or link target; and a path created, reported at
// the nearest directory that existed before, so that no name the emitter
// chose reaches the report. A file is read only when its size is unchanged.
// The walk stops once it has seen MaxCopyEntries paths more than before, with
// a creation reported at the root. The copy holds nothing named ".git"
// (copyTree passes over the repository's own), so a ".git" in it, at any
// depth, is one the emitter made, and is a creation like any other.
func diffTree(root string, before map[string]entryState) []StrayWrite {
	found := map[StrayWrite]bool{}
	add := func(path, change string) {
		found[StrayWrite{Code: "EMIT-STRAY-WRITE", Path: path, Change: change}] = true
	}
	seen := map[string]bool{}
	created := 0
	_ = filepath.WalkDir(root, func(full string, entry fs.DirEntry, err error) error {
		relative, relErr := filepath.Rel(root, full)
		if relErr != nil {
			return relErr
		}
		name := filepath.ToSlash(relative)
		if name == "." {
			return nil
		}
		if err != nil {
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		was, existed := before[name]
		if !existed {
			add(nearestBefore(name, before), StrayCreated)
			if created++; created > maxCopyEntries {
				add(".", StrayCreated)
				return fs.SkipAll
			}
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		seen[name] = true
		if changed(full, entry, was) {
			add(name, StrayChanged)
			if entry.IsDir() && was.kind != kindDir {
				return fs.SkipDir
			}
		}
		return nil
	})
	for name := range before {
		if !seen[name] {
			add(name, StrayRemoved)
		}
	}
	out := make([]StrayWrite, 0, len(found))
	for change := range found {
		out = append(out, change)
	}
	slices.SortFunc(out, func(a, b StrayWrite) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return strings.Compare(a.Change, b.Change)
	})
	return bounded(out)
}

// changed says whether a path differs from its state before.
func changed(full string, entry fs.DirEntry, was entryState) bool {
	info, err := entry.Info()
	if err != nil {
		return true
	}
	switch kind := entry.Type(); {
	case kind&fs.ModeSymlink != 0:
		link, err := os.Readlink(full)
		return was.kind != kindLink || err != nil || link != was.target
	case kind.IsDir():
		return was.kind != kindDir || info.Mode().Perm() != was.perm
	case kind.IsRegular():
		if was.kind != kindFile || info.Mode().Perm() != was.perm || info.Size() != was.size {
			return true
		}
		in, err := os.Open(full)
		if err != nil {
			return true
		}
		defer in.Close()
		hash := sha256.New()
		if _, err := io.Copy(hash, io.LimitReader(in, was.size+1)); err != nil {
			return true
		}
		var sum [sha256.Size]byte
		copy(sum[:], hash.Sum(nil))
		return sum != was.sum
	}
	return true
}

// nearestBefore is the nearest directory above name that the repository
// held before, or "." for its root.
func nearestBefore(name string, before map[string]entryState) string {
	for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
		if state, ok := before[dir]; ok && state.kind == kindDir {
			return dir
		}
	}
	return "."
}

// removeTree removes a directory the harness made, first giving its owner
// the bits to do so, which an emitter may have taken away.
func removeTree(dir string) {
	_ = filepath.WalkDir(dir, func(full string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			_ = os.Chmod(full, 0o700)
		}
		return nil
	})
	_ = os.RemoveAll(dir)
}
