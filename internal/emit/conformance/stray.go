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
	errCopyLink  = errors.New("conformance: the system does not let the user make a symbolic link")
)

// unreadableError names a path of the repository that could not be copied.
type unreadableError struct{ path string }

func (e unreadableError) Error() string { return "conformance: a path could not be copied" }

// linkOutError names a symbolic link of the repository that leads out of it.
type linkOutError struct{ path string }

func (e linkOutError) Error() string {
	return "conformance: a symbolic link leads out of the repository"
}

// reparseError names a Windows reparse point of the repository that is not a
// symbolic link, a junction for one, which the link check cannot follow.
type reparseError struct{ path string }

func (e reparseError) Error() string {
	return "conformance: a reparse point that is not a symbolic link"
}

// refusesReparse is check 6's rule for a Windows reparse point, by the
// entry's mode and whether it carries FILE_ATTRIBUTE_REPARSE_POINT: one that
// Go reads as neither a symbolic link nor a regular file is refused. A
// junction is one: since Go 1.23 it is ModeIrregular, not ModeSymlink, so
// the link check would never see where it leads. A symbolic link is left to
// the link check, and a regular file stays, as Go reads a deduplicated file,
// which is a reparse point too. Outside Windows no entry is a reparse point,
// and none is ModeIrregular.
func refusesReparse(mode fs.FileMode, reparse bool) bool {
	if mode&fs.ModeSymlink != 0 || mode.IsRegular() {
		return false
	}
	return reparse || mode&fs.ModeIrregular != 0
}

// checkReparse refuses an entry by refusesReparse (reparseError).
func checkReparse(name string, entry fs.DirEntry) error {
	reparse, err := reparsePoint(entry)
	if err != nil {
		return unreadableError{path: name}
	}
	if refusesReparse(entry.Type(), reparse) {
		return reparseError{path: name}
	}
	return nil
}

// maxLinkHops bounds the links followed to find where one link leads.
const maxLinkHops = 40

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
// passing over anything named ".git" in any case. Every link of the
// repository is first checked to stay in it, so that one that leads out is
// refused whether or not the copy can be made, the fallback included. Only
// Windows refusing a link for want of the privilege gets the fallback; any
// other link that cannot be made refuses the run, naming it.
func newWorkspace(source string) (*workspace, *emit.Diagnostic) {
	if diagnostic := copyFailure(checkLinks(source)); diagnostic != nil {
		return nil, diagnostic
	}
	w := &workspace{source: source}
	if diagnostic := w.fresh(); diagnostic != nil {
		return nil, diagnostic
	}
	return w, nil
}

// checkLinks walks source as copyTree does, under its entry bound, and
// refuses the first symbolic link that leads out of it (linkOutError), or
// the first reparse point that is not a link (reparseError).
func checkLinks(source string) error {
	entries := 0
	return filepath.WalkDir(source, func(full string, entry fs.DirEntry, err error) error {
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
		if isGitName(entry.Name()) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entries++; entries > maxCopyEntries {
			return errCopyBound
		}
		if err := checkReparse(name, entry); err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		link, err := os.Readlink(full)
		if err != nil {
			return unreadableError{path: name}
		}
		if leadsOut(source, name, link) {
			return linkOutError{path: name}
		}
		return nil
	})
}

// copyFailure is the diagnostic that refuses the run for an error of
// checkLinks or copyTree, nil for none and for errCopyLink, the fallback.
func copyFailure(err error) *emit.Diagnostic {
	var unreadable unreadableError
	var linkOut linkOutError
	var reparse reparseError
	var d emit.Diagnostic
	switch {
	case err == nil, errors.Is(err, errCopyLink):
		return nil
	case errors.As(err, &linkOut):
		d = emit.NewDiagnostic("EMIT-HARNESS-BOUND", emit.Location{Document: emit.DocumentRepository, Path: linkOut.path})
	case errors.As(err, &reparse):
		d = emit.NewDiagnostic("EMIT-HARNESS-BOUND", emit.Location{Document: emit.DocumentRepository, Path: reparse.path})
	case errors.Is(err, errCopyBound):
		d = emit.NewDiagnostic("EMIT-HARNESS-BOUND", emit.Location{Document: emit.DocumentRepository})
	case errors.As(err, &unreadable):
		d = emit.NewDiagnostic("EMIT-PATH-UNREADABLE", emit.Location{Document: emit.DocumentRepository, Path: unreadable.path})
	default:
		d = emit.NewDiagnostic("EMIT-PATH-UNREADABLE", emit.Location{Document: emit.DocumentRepository})
	}
	return &d
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
	if err != nil {
		removeTree(dir)
		if diagnostic := copyFailure(err); diagnostic != nil {
			return diagnostic
		}
		// errCopyLink: every link was checked by newWorkspace to stay in.
		w.dir, w.root, w.fallback, w.before = "", w.source, true, nil
		return nil
	}
	// The copy's root is compared too: its bits are the copy's own.
	info, err := os.Lstat(root)
	if err != nil {
		removeTree(dir)
		d := emit.NewDiagnostic("EMIT-PATH-UNREADABLE", emit.Location{Document: emit.DocumentRepository})
		return &d
	}
	before["."] = entryState{kind: kindDir, perm: info.Mode().Perm()}
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
// never following a link; it passes over anything named ".git" and over
// target itself. A link keeps its target, except that an absolute target
// below source is moved to the same path below target, so that a write
// through it stays in the copy; a link that leads out of source is
// refused (linkOutError). A link it cannot make is unreadableError naming
// it, except for the one failure of the fallback, a system that does not
// let the user make links (errCopyLink). It returns what it copied, by slash
// path.
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
		if err := checkReparse(name, entry); err != nil {
			return err
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
			if leadsOut(source, name, link) {
				return linkOutError{path: name}
			}
			if relative, ok := belowSource(source, link); ok && filepath.IsAbs(link) {
				// As written, not cleaned: a ".." is the link's to resolve.
				link = target
				if relative != "." {
					link += string(filepath.Separator) + relative
				}
			}
			if err := os.Symlink(link, destination); err != nil {
				if linkUnprivileged(err) {
					return errCopyLink
				}
				return unreadableError{path: name}
			}
			states[name] = entryState{kind: kindLink, target: link}
		case kind.IsDir():
			if err := os.Mkdir(destination, 0o700); err != nil {
				return err
			}
			dirs = append(dirs, destination)
			states[name] = entryState{kind: kindDir, perm: info.Mode().Perm()}
		case kind.IsRegular():
			// The size before the copy refuses a file already past the bound;
			// the bytes copied, which a file that grew may add to, are what
			// count.
			if total+info.Size() > maxCopyBytes {
				return errCopyBound
			}
			state, err := copyFile(full, destination, info.Mode().Perm(), maxCopyBytes-total)
			switch {
			case errors.Is(err, errCopyBound):
				return errCopyBound
			case err != nil:
				return unreadableError{path: name}
			}
			total += state.size
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

// leadsOut says whether the link at name, a slash path below source, with
// the target link, leads out of source when it is followed there, link by
// link: each ".." is taken from where the parts before it led, and a part
// that does not exist is taken as written. An absolute target is taken as
// written too, so one below source only by a link of its own leads out.
func leadsOut(source, name, link string) bool {
	rest, ok := linkParts(source, link)
	if !ok {
		return true
	}
	var at []string
	if dir := path.Dir(name); !filepath.IsAbs(link) && dir != "." {
		at = strings.Split(dir, "/")
	}
	for hops := 0; len(rest) > 0; {
		part := rest[0]
		rest = rest[1:]
		switch part {
		case ".":
			continue
		case "..":
			if len(at) == 0 {
				return true
			}
			at = at[:len(at)-1]
			continue
		}
		next := append(slices.Clone(at), part)
		full := filepath.Join(append([]string{source}, next...)...)
		info, err := os.Lstat(full)
		if err != nil || info.Mode()&fs.ModeSymlink == 0 {
			at = next
			continue
		}
		if hops++; hops > maxLinkHops {
			return true
		}
		target, err := os.Readlink(full)
		if err != nil {
			return true
		}
		more, ok := linkParts(source, target)
		if !ok {
			return true
		}
		if filepath.IsAbs(target) {
			at = nil
		}
		rest = append(more, rest...)
	}
	return false
}

// linkParts splits a link's target into its parts, an absolute target
// relative to source; ok is false for an absolute target that is not below
// source, and for a target rooted on a drive but not absolute (Windows).
func linkParts(source, link string) ([]string, bool) {
	switch {
	case filepath.IsAbs(link):
		relative, ok := belowSource(source, link)
		if !ok {
			return nil, false
		}
		link = relative
	case filepath.VolumeName(link) != "" || link != "" && os.IsPathSeparator(link[0]):
		return nil, false
	}
	return strings.FieldsFunc(link, func(r rune) bool { return r < 0x80 && os.IsPathSeparator(uint8(r)) }), true
}

// belowSource is an absolute target's path below source, as written: the
// target is source itself, or begins with source and a separator.
func belowSource(source, link string) (string, bool) {
	if link == source {
		return ".", true
	}
	prefix := source
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return strings.CutPrefix(link, prefix)
}

// copyFile copies one regular file, at most limit bytes of it: one longer
// than limit when it is read, grown since its size was read, is errCopyBound.
func copyFile(source, destination string, perm fs.FileMode, limit int64) (entryState, error) {
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
	size, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(in, limit+1))
	if err = errors.Join(err, out.Close()); err != nil {
		return entryState{}, err
	}
	if size > limit {
		return entryState{}, errCopyBound
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
		if err != nil {
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if name == "." {
			// The root is compared when its state was kept (the workspace
			// keeps it), and is never removed or created.
			seen[name] = true
			if was, kept := before[name]; kept && changed(full, entry, was) {
				add(name, StrayChanged)
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
