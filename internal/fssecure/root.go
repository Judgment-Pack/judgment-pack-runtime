package fssecure

import (
	"errors"
	"fmt"
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

// ContainsDir reports whether one declared relative path names a directory
// inside this root, resolving the final component when it exists.
//
// Contains deliberately leaves the final component unresolved, because for a
// *file* a final symlink is not a containment question — the open refuses it
// whatever it points at. For a directory it is exactly a containment question:
// the declared name becomes an intermediate component of everything written
// beneath it, and an intermediate symlink out of the root is the escape the
// handle exists to catch. So this follows it, through the handle, and reports
// the escape as one.
//
// A directory that is not there yet is contained. A caller that creates what it
// declared — and MakeDir is how that is done here — must not be told at validate
// time that a directory it is about to make is missing.
func (r *Root) ContainsDir(relative string) error {
	cleaned, err := Relative(relative)
	if err != nil {
		return err
	}
	info, err := r.root.Stat(cleaned)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return classify(err)
	}
	if !info.IsDir() {
		return errors.New("path is not a directory")
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
	if _, err := r.sameRegularFile(file, cleaned); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

// sameRegularFile makes the two checks that are about the opened file itself
// rather than about where it is, against the same handle the open used. Lstat
// is what refuses a final symlink, since os.Root follows one that stays inside
// the root; SameFile is what refuses a final component swapped between the open
// and that Lstat, because then the opened file is not the file the check just
// described. Both fail closed, and the caller closes the file. The opened file's
// own information is returned so a caller that has more to ask of it does not
// stat it a second time.
func (r *Root) sameRegularFile(file *os.File, cleaned string) (os.FileInfo, error) {
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}
	linkInfo, err := r.root.Lstat(cleaned)
	if err != nil || linkInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(openedInfo, linkInfo) {
		return nil, errors.New("path changed or resolves through a final symlink")
	}
	return openedInfo, nil
}

// MakeDir creates one directory beneath this root, and every parent of it that
// is also beneath this root, treating a directory already there as made.
//
// Each component is created through the retained handle rather than by a joined
// pathname, so the containment that bounds every read bounds the creation too:
// a component that would leave the root is refused by os.Root itself, and there
// is no interval between deciding where the directory goes and making it there.
//
// The mode is a creation mode and nothing more: a directory that was already
// there keeps whatever mode it was given, because tightening a directory this
// package did not make would be deciding something about a tree the project
// owns.
func (r *Root) MakeDir(relative string) error {
	cleaned, err := Relative(relative)
	if err != nil {
		return err
	}
	if cleaned == "." {
		return nil
	}
	made := ""
	for _, component := range strings.Split(cleaned, string(filepath.Separator)) {
		made = filepath.Join(made, component)
		err := r.root.Mkdir(made, 0o700)
		if err == nil {
			continue
		}
		if !errors.Is(err, fs.ErrExist) {
			return classify(err)
		}
		// mkdirat reports "exists" for a regular file too, so "already there"
		// has to be checked for being a directory. Without this the refusal
		// arrives one call later and names the file that could not be opened
		// instead of the component that is not a directory. The check follows
		// the component through the handle rather than Lstat-ing it: a symlink
		// to a directory inside the root is a legitimate project layout and
		// reads as one everywhere else here, and a symlink pointing out of the
		// root is refused as the escape it is rather than as a non-directory.
		info, statErr := r.root.Stat(made)
		if statErr != nil {
			return classify(statErr)
		}
		if !info.IsDir() {
			return fmt.Errorf("%q exists and is not a directory", made)
		}
	}
	return nil
}

// Append adds one record to the end of a file beneath this root, creating the
// file when it is not there and never rewriting a byte already in it.
//
// It is the only write this package performs, and it is bounded exactly as
// every read is: the path is resolved against the retained handle, and the
// checks about the file itself are made against that same handle. O_APPEND is
// what makes the write one operation at the end of the file rather than a seek
// and a write with an interval in between, so two writers appending whole
// records interleave records and never halves of one.
//
// Which flags the open carries is decided by what is already at the path, and
// that is the whole of how a refusal avoids creating something. Lstat first: a
// final component that is a symlink is refused outright, and an existing regular
// file is opened WITHOUT O_CREATE so no link swapped in behind the check can be
// followed into existence. Only an absent name is opened with O_CREATE|O_EXCL,
// which loses atomically to anything that appeared in the meantime — a symlink
// included — rather than following it. Each branch can be invalidated by the
// other's condition arriving in between, so the two are retried a bounded number
// of times and then refused; a caller racing itself forever is a caller who has
// lost the file to something else. (O_NOFOLLOW in the flag word does not help:
// os.Root resolves the path itself and the open still succeeds.) The post-open
// same-file check stays for the swap neither branch can see.
//
// What that buys, exactly: a refusal reached before or at the open creates
// nothing. A failure *after* a successful create — a stat, a mode change, a
// write, a sync — leaves the file there, empty or partly written, because an
// appender cannot un-append. internal/audit's run ids and its commit marker are
// what let a reader tell such a line from a complete one.
//
// A file with more than one link is refused where the platform reports the
// count. That is not a containment question — a hardlinked alias is inside the
// project, in the same trust domain as editing the files it aliases — but every
// neighbouring case fails closed, and appending a record into whatever else
// names the same inode should not be the one that does not.
//
// The mode is set on every append rather than only at creation, because a trail
// file put there by something else would otherwise keep whatever mode it was
// given, and these records carry the documents the project asked to have
// recorded. It is a unix guarantee: on Windows a Go file mode sets only the
// read-only attribute and does not touch the DACL, so what may read a trail
// there is whatever the containing directory's ACL allows.
//
// Sync is what makes the record's bytes reach the disk before this returns. It
// says nothing about the directory entry of a file this same call created:
// nothing here fsyncs a directory, and a crash between the two can lose a
// freshly created trail file that was reported written.
func (r *Root) Append(relative string, record []byte) error {
	cleaned, err := Relative(relative)
	if err != nil {
		return err
	}
	file, err := r.openForAppend(cleaned)
	if err != nil {
		return err
	}
	openedInfo, err := r.sameRegularFile(file, cleaned)
	if err != nil {
		file.Close()
		return err
	}
	if hardLinked(openedInfo) {
		file.Close()
		return errors.New("path has more than one link, so it names a file something else also names")
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(record); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// appendOpenAttempts bounds the existing/absent race. Two attempts would do for
// one competing writer; three is the same rule with room for the second one.
const appendOpenAttempts = 3

// openForAppend opens the trail for appending without ever creating something
// the caller did not name. See Append for why the flags differ by branch.
func (r *Root) openForAppend(cleaned string) (*os.File, error) {
	for attempt := 0; attempt < appendOpenAttempts; attempt++ {
		info, err := r.root.Lstat(cleaned)
		switch {
		case err == nil:
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, errors.New("path resolves through a final symlink")
			}
			// It is there, so nothing needs creating: an open without O_CREATE
			// cannot bring a swapped-in link's target into existence.
			file, openErr := r.root.OpenFile(cleaned, os.O_WRONLY|os.O_APPEND|nonBlockingOpen, 0)
			if errors.Is(openErr, fs.ErrNotExist) {
				// Removed between the Lstat and the open. Look again.
				continue
			}
			if openErr != nil {
				return nil, classify(openErr)
			}
			return file, nil
		case errors.Is(err, fs.ErrNotExist):
			// Nothing is there, so this open is the one that puts it there —
			// exclusively, which is what makes anything that arrives first win
			// the race instead of being followed.
			file, openErr := r.root.OpenFile(cleaned, os.O_WRONLY|os.O_CREATE|os.O_EXCL|nonBlockingOpen, 0o600)
			if errors.Is(openErr, fs.ErrExist) {
				// Something arrived between the Lstat and the open. Look again;
				// if it is a symlink, the branch above refuses it.
				continue
			}
			if openErr != nil {
				return nil, classify(openErr)
			}
			return file, nil
		default:
			return nil, classify(err)
		}
	}
	return nil, errors.New("path kept changing between the check and the open")
}

// Replace writes one whole file beneath this root, creating it or replacing
// what a previous call put there.
//
// It exists for a *generated* file — one this runtime produces in full from
// something else it read, so there is nothing in the old contents to preserve
// and appending would be wrong. The containment is Append's exactly: the same
// flag choice by what is already at the path, so a symlink swapped in behind the
// check loses the race rather than being followed; the same post-open
// same-file, regular-file, and link-count refusals; and no pathname handed to
// anything.
//
// The mode is set on every write, not only on creation, exactly as Append does
// and for the same reason.
//
// It is not atomic. Truncating and rewriting through one handle is what os.Root
// offers on this module's Go floor — Root.Rename arrived after it — so an I/O
// failure partway through leaves a short file rather than either version. That
// is acceptable for a file whose one producer can be run again and whose reader
// refuses a document it cannot decode; it would not be acceptable for anything a
// reader must trust to be either old or new.
func (r *Root) Replace(relative string, contents []byte) error {
	cleaned, err := Relative(relative)
	if err != nil {
		return err
	}
	file, err := r.openForWrite(cleaned)
	if err != nil {
		return err
	}
	openedInfo, err := r.sameRegularFile(file, cleaned)
	if err != nil {
		file.Close()
		return err
	}
	if hardLinked(openedInfo) {
		file.Close()
		return errors.New("path has more than one link, so it names a file something else also names")
	}
	// The mode is set on every write rather than only at creation, on Append's
	// rule and for its reason: a file something else left at this name would
	// otherwise keep whatever mode it was given, and this one carries the
	// project's statement of which documents it reviewed. It is a unix
	// guarantee — a Go file mode on Windows sets only the read-only attribute.
	if err := file.Chmod(0o644); err != nil {
		file.Close()
		return err
	}
	if err := file.Truncate(0); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(contents); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// openForWrite is openForAppend's rule for a file that is rewritten rather than
// extended: the same branch on what is already there, and the same bounded retry
// when the two conditions swap under it. The truncation is the caller's, after
// the checks, so nothing is discarded before this open is known to have found
// the file the check described.
func (r *Root) openForWrite(cleaned string) (*os.File, error) {
	for attempt := 0; attempt < appendOpenAttempts; attempt++ {
		info, err := r.root.Lstat(cleaned)
		switch {
		case err == nil:
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, errors.New("path resolves through a final symlink")
			}
			file, openErr := r.root.OpenFile(cleaned, os.O_WRONLY|nonBlockingOpen, 0)
			if errors.Is(openErr, fs.ErrNotExist) {
				continue
			}
			if openErr != nil {
				return nil, classify(openErr)
			}
			return file, nil
		case errors.Is(err, fs.ErrNotExist):
			file, openErr := r.root.OpenFile(cleaned, os.O_WRONLY|os.O_CREATE|os.O_EXCL|nonBlockingOpen, 0o644)
			if errors.Is(openErr, fs.ErrExist) {
				continue
			}
			if openErr != nil {
				return nil, classify(openErr)
			}
			return file, nil
		default:
			return nil, classify(err)
		}
	}
	return nil, errors.New("path kept changing between the check and the open")
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
