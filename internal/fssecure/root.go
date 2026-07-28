package fssecure

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutsideRoot reports that a relative path did not resolve inside the root it
// was resolved against. It is distinct from a read failure: the path may name a
// perfectly readable file, and the refusal is about where that file is.
var ErrOutsideRoot = errors.New("path resolves outside the root")

// Relative reduces one caller-declared relative path to the cleaned form a Root
// opens, and refuses anything that is not a path inside the root to begin with.
//
// This is the lexical half of the containment rule, and it can be applied before
// the named file exists — a project configuration is checked for escaping paths
// at validate time, not only when a read is attempted. It is not the whole rule
// and no caller about to read a file may stop here: a lexical check cannot see a
// symlinked directory component, which is what the handle-bound open in Root
// refuses.
//
// An absolute path is refused rather than resolved: a root-relative convention
// that silently accepted "/etc/passwd" would have no root. A leading separator of
// either kind is refused on every platform, so a configuration written on Windows
// is refused identically on Unix and the reverse — a path is either inside the
// project or it is not, and that must not depend on who reads it.
func Relative(relative string) (string, error) {
	if relative == "" || strings.ContainsRune(relative, 0) {
		return "", ErrOutsideRoot
	}
	if filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", ErrOutsideRoot
	}
	if strings.HasPrefix(relative, "/") || strings.HasPrefix(relative, `\`) {
		return "", ErrOutsideRoot
	}
	if IsRemotePath(relative) {
		return "", ErrOutsideRoot
	}
	cleaned := filepath.Clean(relative)
	// Parent traversal is allowed only where normalization keeps the result inside
	// the root: "a/../b" is "b", and "a/../../b" is an escape.
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", ErrOutsideRoot
	}
	return cleaned, nil
}

// Resolve joins one caller-declared relative path onto a root directory and
// refuses any result outside it. It answers "where would this path be", which is
// what a diagnostic needs; it is not permission to open that pathname, because a
// pathname resolved in one instant can name a different file in the next. Only
// Root opens anything.
func Resolve(root, relative string) (string, error) {
	cleaned, err := Relative(relative)
	if err != nil {
		return "", err
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(absoluteRoot, cleaned), nil
}

// Root is a directory handle every read beneath it is resolved against.
//
// It exists because containment checked against a pathname is not containment
// through the open. A resolver that canonicalizes a pathname and hands the result
// to a separate open states a fact about the filesystem as it was, and the open
// acts on the filesystem as it is: between the two, an intermediate directory
// component can be renamed away and replaced with a symlink pointing out of the
// root, and the open follows it. Re-checking the opened file afterward does not
// catch that, because the file that was opened really is the file the pathname
// then named.
//
// Holding the directory open removes that window rather than narrowing it. Every
// open here is performed by os.Root relative to the retained handle, which
// resolves each component itself and refuses any traversal leaving the root, so
// the root a read is bounded by is the directory this handle was opened on and
// not whatever its pathname denotes at the moment of the read. Renaming or
// re-pointing a component after the handle exists cannot widen it.
//
// Two properties of the previous pathname-based reader are kept, because they are
// about *what* is read rather than *where* it is: a final component that is a
// symlink is refused even when it points inside the root, and anything that is
// not a regular file is refused. os.Root follows an inward symlink like any other
// path component, so those two are checked explicitly against the same handle
// after the open, and both fail closed.
type Root struct {
	root *os.Root
	dir  string
}

// OpenRoot opens dir as a containment root. The directory is resolved once,
// here, and every later read is relative to the handle this returns — so a
// caller that opens the root and then reads through it is reading beneath the
// directory it actually opened, whatever happens to the pathname afterward.
func OpenRoot(dir string) (*Root, error) {
	if IsRemotePath(dir) {
		return nil, ErrOutsideRoot
	}
	opened, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		opened.Close()
		return nil, err
	}
	return &Root{root: opened, dir: absolute}, nil
}

// Dir reports the pathname this root was opened on. It is for display, and it
// resolves nothing: the handle, not this string, is what reads are bounded by.
func (r *Root) Dir() string { return r.dir }

// Close releases the directory handle.
func (r *Root) Close() error {
	if r == nil || r.root == nil {
		return nil
	}
	return r.root.Close()
}

// Contains reports whether one declared relative path is inside this root,
// without reading the file it names.
//
// It is the check a validate-time surface makes, and it answers only the
// containment question: a path inside the root whose file or containing
// directory does not exist is contained, and the read is what reports that
// nothing is there. "The directory is missing" and "the path left the project"
// call for two different fixes, so they are two different errors.
//
// The final component is deliberately not resolved. A final symlink is not a
// containment question — it is refused at the open whatever it points at.
func (r *Root) Contains(relative string) error {
	cleaned, err := Relative(relative)
	if err != nil {
		return err
	}
	if _, err := r.root.Lstat(cleaned); err != nil {
		return classify(err)
	}
	return nil
}

// Open opens one regular file beneath this root, refusing a path that leaves it
// by any route and a final component that is a symlink or is not a regular file.
//
// The containment half is structural: os.Root resolves the path against the
// retained handle and refuses a traversal that would leave the root, so there is
// no interval in which a resolved path waits to be opened. What remains is the
// two checks about the file itself, made against the same handle — never against
// a pathname — and both fail closed. Lstat is what refuses a final symlink, since
// os.Root follows one that stays inside the root; SameFile is what refuses a
// final component swapped between the open and that Lstat, because then the
// opened file is not the file the check just described.
func (r *Root) Open(relative string) (*os.File, error) {
	cleaned, err := Relative(relative)
	if err != nil {
		return nil, err
	}
	// O_NONBLOCK keeps a FIFO named where a pack should be from blocking the read
	// until someone writes to it; the regular-file check below refuses it anyway.
	file, err := r.root.OpenFile(cleaned, os.O_RDONLY|nonBlockingOpen, 0)
	if err != nil {
		return nil, classify(err)
	}
	openedInfo, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("path is not a regular file")
	}
	linkInfo, err := r.root.Lstat(cleaned)
	if err != nil || linkInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(openedInfo, linkInfo) {
		file.Close()
		return nil, errors.New("path changed or resolves through a final symlink")
	}
	return file, nil
}

// Read reads one bounded regular file beneath this root. It is Open plus the
// byte limit, and it is how every caller that wants bytes rather than a handle
// gets exactly the containment Open provides.
func (r *Root) Read(relative string, limit int64) ([]byte, error) {
	file, err := r.Open(relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readBounded(file, limit)
}

// rootEscapeMessage is the message os.Root uses for its own refusal to resolve a
// path outside the root. The standard library exports no sentinel for it, so this
// is how the refusal is told apart from a failure the operating system named.
//
// Matching on a message is the weaker kind of coupling, so the failure mode is
// what makes it acceptable: if a future Go release renames it, the escape is
// still refused — os.Root refused it, and this function only decides which error
// to report — and the read degrades to a less precise message rather than to a
// permitted read. The root tests assert that a real escape arrives as
// ErrOutsideRoot, so such a rename fails the suite loudly instead of quietly.
const rootEscapeMessage = "path escapes from parent"

// classify separates os.Root's own containment refusal from an operating-system
// failure, so a caller can tell "the path left the project" from "the file is not
// there". A failure the system named keeps the system's own words: "a component
// is not a directory" and "the path left the project" call for different fixes.
func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) ||
		errors.Is(err, fs.ErrClosed) || errors.Is(err, fs.ErrInvalid) ||
		errors.Is(err, errors.ErrUnsupported) {
		return err
	}
	var pathError *fs.PathError
	if errors.As(err, &pathError) && pathError.Err != nil && pathError.Err.Error() == rootEscapeMessage {
		return ErrOutsideRoot
	}
	return err
}
