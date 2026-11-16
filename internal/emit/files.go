package emit

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
)

// Kinds of difference check mode lists (RFC 0036 §4).
const (
	DifferenceMissing = "missing"
	DifferenceChanged = "changed"
	DifferenceMode    = "mode"
	DifferenceExtra   = "extra"
)

// Difference is one path check mode found different, without its contents.
type Difference struct {
	Path       string `json:"path"`
	Difference string `json:"difference"`
}

// diskFile is an owned regular file found on disk.
type diskFile struct {
	path string
	mode string
}

// scanOwned lists the regular files on disk that the globs own, walking only
// where a glob can match and never through a symbolic link. A link that a
// glob matches, or below which one can, refuses the run (EMIT-LINK), and so
// does an owned path that is neither a regular file nor a directory, or a
// directory that cannot be listed (EMIT-PATH-UNREADABLE).
func scanOwned(root string, owns []Glob) (map[string]diskFile, []Diagnostic) {
	files := map[string]diskFile{}
	var diagnostics []Diagnostic
	at := func(code, path string) {
		diagnostics = append(diagnostics, diagnostic(code, Location{Document: DocumentRepository, Path: path}))
	}
	matches := func(path string) bool { return owned(owns, path) }
	below := func(dir string) bool {
		for _, glob := range owns {
			if glob.MatchBelow(dir) {
				return true
			}
		}
		return false
	}
	err := filepath.WalkDir(root, func(full string, entry fs.DirEntry, err error) error {
		relative, relErr := filepath.Rel(root, full)
		if relErr != nil {
			return relErr
		}
		path := filepath.ToSlash(relative)
		if path == "." {
			path = ""
		}
		if err != nil {
			at("EMIT-PATH-UNREADABLE", path)
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == "" {
			return nil
		}
		switch kind := entry.Type(); {
		case kind&fs.ModeSymlink != 0:
			if matches(path) || below(path) {
				at("EMIT-LINK", path)
			}
		case kind.IsDir():
			if !below(path) {
				return fs.SkipDir
			}
		case kind.IsRegular():
			if matches(path) {
				info, err := entry.Info()
				if err != nil {
					at("EMIT-PATH-UNREADABLE", path)
					return nil
				}
				files[path] = diskFile{path: path, mode: modeOf(info)}
			}
		default:
			if matches(path) {
				at("EMIT-PATH-UNREADABLE", path)
			}
		}
		return nil
	})
	if err != nil {
		at("EMIT-PATH-UNREADABLE", "")
	}
	return files, diagnostics
}

// modeOf reads a file's mode as one of the two a response gives: executable
// when the owner may execute it, as git records a file's mode. The other
// permission bits follow the umask of whoever checked the file out, so they
// are not compared.
func modeOf(info fs.FileInfo) string {
	if info.Mode().Perm()&0o100 != 0 {
		return ModeExecutable
	}
	return ModeRegular
}

// modesRecorded says whether the file system records an executable bit. On
// Windows it does not, so check mode compares bytes only there, as git does
// with core.fileMode false.
var modesRecorded = runtime.GOOS != "windows"

// pathState is what is on disk at a path the response names.
type pathState int

const (
	stateAbsent pathState = iota
	stateFile
	stateBlocked // held by a directory or another non-regular file
)

// preflight checks every directory above each response path and the path
// itself, without following a link: a link at or above the path refuses the
// run (EMIT-LINK); a component that cannot be examined, or a special file at
// the path, cannot be checked (EMIT-PATH-UNREADABLE). It returns the state of
// each path, and for each blocked parent the path of the regular file that
// holds it.
func preflight(root string, response []File) (map[string]pathState, map[string]string, []Diagnostic) {
	states := map[string]pathState{}
	parents := map[string]string{}
	var diagnostics []Diagnostic
	at := func(code, path string) {
		diagnostics = append(diagnostics, diagnostic(code, Location{Document: DocumentRepository, Path: path}))
	}
	examined := map[string]fs.FileInfo{}
	lstat := func(path string) (fs.FileInfo, error) {
		if info, ok := examined[path]; ok {
			return info, nil
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
		if err == nil {
			examined[path] = info
		}
		return info, err
	}
	for _, file := range response {
		segments := strings.Split(file.Path, "/")
		state := stateAbsent
		for i := range segments {
			prefix := strings.Join(segments[:i+1], "/")
			info, err := lstat(prefix)
			if errors.Is(err, fs.ErrNotExist) {
				break
			}
			if err != nil {
				at("EMIT-PATH-UNREADABLE", prefix)
				break
			}
			if info.Mode()&fs.ModeSymlink != 0 {
				at("EMIT-LINK", prefix)
				break
			}
			last := i == len(segments)-1
			switch {
			case last && info.Mode().IsRegular():
				state = stateFile
			case last && info.IsDir():
				state = stateBlocked
			case last:
				at("EMIT-PATH-UNREADABLE", prefix)
			case info.IsDir():
				continue
			case info.Mode().IsRegular():
				parents[file.Path] = prefix
				state = stateAbsent
			default:
				at("EMIT-PATH-UNREADABLE", prefix)
			}
			break
		}
		states[file.Path] = state
	}
	return states, parents, diagnostics
}

// compare is check mode: it lists each response path that is missing,
// changed or of another mode, and each owned file on disk the response does
// not hold, by path, sorted. It reads files and writes nothing.
func compare(root string, response []File, disk map[string]diskFile, states map[string]pathState) ([]Difference, []Diagnostic) {
	var differences []Difference
	var diagnostics []Diagnostic
	inResponse := map[string]bool{}
	for _, file := range response {
		inResponse[file.Path] = true
		switch states[file.Path] {
		case stateAbsent:
			differences = append(differences, Difference{file.Path, DifferenceMissing})
			continue
		case stateBlocked:
			diagnostics = append(diagnostics, diagnostic("EMIT-PATH-UNREADABLE", Location{Document: DocumentRepository, Path: file.Path}))
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(file.Path))
		data, err := fssecure.ReadRegular(full, int64(len(file.Data)))
		switch {
		case errors.Is(err, fssecure.ErrTooLarge):
			differences = append(differences, Difference{file.Path, DifferenceChanged})
			continue
		case err != nil:
			diagnostics = append(diagnostics, diagnostic("EMIT-PATH-UNREADABLE", Location{Document: DocumentRepository, Path: file.Path}))
			continue
		case !bytes.Equal(data, file.Data):
			differences = append(differences, Difference{file.Path, DifferenceChanged})
			continue
		}
		info, err := os.Lstat(full)
		switch {
		case err != nil:
			diagnostics = append(diagnostics, diagnostic("EMIT-PATH-UNREADABLE", Location{Document: DocumentRepository, Path: file.Path}))
		case modesRecorded && modeOf(info) != file.Mode:
			differences = append(differences, Difference{file.Path, DifferenceMode})
		}
	}
	for path := range disk {
		if !inResponse[path] {
			differences = append(differences, Difference{path, DifferenceExtra})
		}
	}
	slices.SortFunc(differences, func(a, b Difference) int { return strings.Compare(a.Path, b.Path) })
	return differences, diagnostics
}

// write is write mode, after the response has been validated whole and the
// preflight has passed. It stages every file whose directory already exists
// as a temporary file there, so that most failures to write are met before
// anything changes; deletes the stale files; stages the rest, creating their
// directories; then renames each staged file into place. A failure stops it:
// it removes what it staged and has not renamed, and reports EMIT-WRITE at the
// path with what was written and deleted before it.
func write(root string, response []File, stale []string, states map[string]pathState, parents map[string]string) (written, deleted []string, diagnostics []Diagnostic) {
	written, deleted = []string{}, []string{}
	staleSet := map[string]bool{}
	for _, path := range stale {
		staleSet[path] = true
	}
	for _, file := range response {
		blocker, blocked := parents[file.Path]
		if states[file.Path] == stateBlocked || blocked && !staleSet[blocker] {
			diagnostics = append(diagnostics, diagnostic("EMIT-WRITE", Location{Document: DocumentRepository, Path: file.Path}))
		}
	}
	if len(diagnostics) > 0 {
		return written, deleted, diagnostics
	}
	staged := make(map[string]string, len(response))
	cleanup := func() {
		for _, temporary := range staged {
			_ = os.Remove(temporary)
		}
	}
	failAt := func(path string) ([]string, []string, []Diagnostic) {
		cleanup()
		return written, deleted, []Diagnostic{diagnostic("EMIT-WRITE", Location{Document: DocumentRepository, Path: path})}
	}
	stage := func(file File) bool {
		full := filepath.Join(root, filepath.FromSlash(file.Path))
		temporary, err := writeTemporary(filepath.Dir(full), file)
		if err != nil {
			return false
		}
		staged[file.Path] = temporary
		return true
	}
	for _, file := range response {
		if _, blocked := parents[file.Path]; blocked {
			continue
		}
		dir := filepath.Dir(filepath.Join(root, filepath.FromSlash(file.Path)))
		if info, err := os.Lstat(dir); err == nil && info.IsDir() && !stage(file) {
			return failAt(file.Path)
		}
	}
	for _, path := range stale {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(path))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return failAt(path)
		}
		deleted = append(deleted, path)
	}
	for _, file := range response {
		if _, done := staged[file.Path]; done {
			continue
		}
		dir := filepath.Dir(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err := os.MkdirAll(dir, 0o755); err != nil || !stage(file) {
			return failAt(file.Path)
		}
	}
	for _, file := range response {
		temporary := staged[file.Path]
		if err := os.Rename(temporary, filepath.Join(root, filepath.FromSlash(file.Path))); err != nil {
			return failAt(file.Path)
		}
		delete(staged, file.Path)
		written = append(written, file.Path)
	}
	return written, deleted, nil
}

// writeTemporary writes a file's bytes and mode to a new temporary file in
// dir and returns its path.
func writeTemporary(dir string, file File) (string, error) {
	temporary, err := os.CreateTemp(dir, ".jpack-emit-*")
	if err != nil {
		return "", err
	}
	name := temporary.Name()
	mode := os.FileMode(0o644)
	if file.Mode == ModeExecutable {
		mode = 0o755
	}
	_, err = temporary.Write(file.Data)
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		// By name, not by handle: Windows cannot change an open file's mode.
		err = os.Chmod(name, mode)
	}
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}
