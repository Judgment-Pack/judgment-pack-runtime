package conformance

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// The harness's own bounds on what it reads (ADR-0099). The registry, kind
// and options bounds are the host's.
const (
	// MaxVectors bounds the vectors of one directory.
	MaxVectors = 1000
	// MaxRequestBytes bounds a vector's request.json, as the host bounds a
	// request it forms.
	MaxRequestBytes = emit.MaxRequestBytes
	// MaxSmallDocumentBytes bounds harness.json and expected-failure.json.
	MaxSmallDocumentBytes = 4 << 10
	// MaxKindFiles bounds a vector's kinds/, as `jpack emit --kinds` is
	// bounded.
	MaxKindFiles = 1024
)

// The files a vector directory may hold (RFC 0036 §5).
const (
	fileRequest         = "request.json"
	fileRegistry        = "registry.json"
	dirKinds            = "kinds"
	fileOptions         = "options.json"
	dirExpected         = "expected"
	fileExpectedFailure = "expected-failure.json"
	fileHarness         = "harness.json"
	// fileEmpty is expected/'s marker of a run that gives no files, which git
	// can track where it cannot track an empty directory. No response path
	// can name it: the host refuses a segment that begins with "~".
	fileEmpty = "~empty"
)

// The bounds as the code applies them; a test lowers them.
var (
	maxVectors   = MaxVectors
	maxKindFiles = MaxKindFiles
)

// listBatch is how many entries a directory is listed by at a time.
const listBatch = 256

// listBounded lists dir by batches and keeps the entries keep admits, in
// name order, holding no other: past limit kept, it stops listing and over
// is true, so a directory past its bound is never held whole.
func listBounded(dir string, keep func(fs.DirEntry) bool, limit int) (kept []fs.DirEntry, over bool, err error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	for {
		batch, err := f.ReadDir(listBatch)
		for _, entry := range batch {
			if !keep(entry) {
				continue
			}
			if len(kept) == limit {
				return nil, true, nil
			}
			kept = append(kept, entry)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false, err
		}
	}
	slices.SortFunc(kept, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return kept, false, nil
}

var vectorMembers = map[string]bool{fileRequest: false, fileRegistry: false, dirKinds: true, fileOptions: false, dirExpected: true, fileExpectedFailure: false}

// Vector is one vector, read.
type Vector struct {
	// Name is the vector directory's name; Dir its path.
	Name, Dir string
	// Request is request.json's bytes, for a vector that gives its request
	// whole; nil for one formed from a registry.
	Request []byte
	// Registry and Kinds are registry.json and kinds/*.json, as `jpack emit`
	// reads them: a document past its bound is marked Oversize.
	Registry registry.Document
	Kinds    []registry.Document
	// Options is options.json, admitted; nil for none.
	Options any
	// Refused are what refuses the request while its inputs are read, as
	// `jpack emit` refuses an options document before it forms one.
	Refused []emit.Diagnostic
	// Expected is expected/, by path; nil for an expected-failure vector, or
	// for a vector --update is to give one.
	Expected map[string]expectedFile
	// ExpectedFailure is the code expected-failure.json names.
	ExpectedFailure string

	// The vector's layout, as readVectors found it: its members, the names of
	// its kinds/*.json, and expected/'s files.
	present  map[string]bool
	kinds    []string
	expected []expectedEntry
}

// expectedEntry is one file of expected/ as its layout gives it: its path
// relative to expected/, and its mode.
type expectedEntry struct {
	path, mode string
}

func (v *Vector) hasRegistry() bool { return v.Request == nil }

type expectedFile struct {
	data []byte
	mode string
}

func vectorDiagnostic(code, path, pointer string) emit.Diagnostic {
	return emit.NewDiagnostic(code, emit.Location{Document: DocumentVector, Path: path, Pointer: pointer})
}

// readLevel reads vectors/harness.json: {"harnessLevel": "1.0"}, or level
// 1.0 when there is none. A level this harness does not know refuses the run.
func readLevel(dir string) (string, []emit.Diagnostic) {
	path := filepath.Join(dir, fileHarness)
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return DefaultLevel, nil
	}
	value, refused := readSmallDocument(path, fileHarness)
	if refused != nil {
		return "", refused
	}
	object, ok := value.(map[string]any)
	if !ok || len(object) != 1 {
		return "", []emit.Diagnostic{vectorDiagnostic("EMIT-VECTOR", fileHarness, "")}
	}
	level, ok := object["harnessLevel"].(string)
	if _, known := levels[level]; !ok || !known {
		return "", []emit.Diagnostic{vectorDiagnostic("EMIT-VECTOR", fileHarness, "/harnessLevel")}
	}
	return level, nil
}

func readSmallDocument(path, at string) (any, []emit.Diagnostic) {
	data, err := fssecure.ReadRegular(path, MaxSmallDocumentBytes)
	switch {
	case errors.Is(err, fssecure.ErrTooLarge):
		return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-VECTOR", at, "")}
	case err != nil:
		return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-PATH-UNREADABLE", at, "")}
	}
	value, code, pointer := registry.AdmitValue(data, MaxSmallDocumentBytes)
	if code != "" {
		return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-VECTOR", at, pointer)}
	}
	return value, nil
}

// readVectors lays out every vector directory below dir, in name order:
// each is checked against the layout, by its entries and their sizes, before
// any emitter runs, and none of its files is read; load reads a vector just
// before its run. Under update, a vector may hold neither expected/ nor
// expected-failure.json: update gives it expected/.
func readVectors(dir string, update bool) ([]*Vector, []emit.Diagnostic) {
	// The files beside the vectors are passed over as they are listed; the
	// directories and links are kept, under the vectors' bound.
	entries, over, err := listBounded(dir, func(entry fs.DirEntry) bool {
		return entry.IsDir() || entry.Type()&fs.ModeSymlink != 0
	}, maxVectors)
	switch {
	case err != nil:
		return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-PATH-UNREADABLE", "", "")}
	case over:
		return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-VECTOR", "", "")}
	}
	var vectors []*Vector
	var refused []emit.Diagnostic
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			refused = append(refused, vectorDiagnostic("EMIT-VECTOR", name, ""))
			continue
		case !entry.IsDir():
			// harness.json, or a file of the author's beside the vectors.
			continue
		}
		vector, problems := layoutVector(filepath.Join(dir, name), name, update)
		refused = append(refused, problems...)
		if vector != nil {
			vectors = append(vectors, vector)
		}
	}
	if len(refused) > 0 {
		return nil, refused
	}
	if len(vectors) == 0 {
		// No vector: the hostile rows alone would pass, which says nothing
		// about the emitter.
		return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-VECTOR", "", "")}
	}
	return vectors, nil
}

// at is the vector-relative path of one of its files, for a diagnostic.
func (v *Vector) at(file string) string {
	if file == "" {
		return v.Name
	}
	return v.Name + "/" + file
}

// errTooManyEntries is walkBounded's: more entries than its limit.
var errTooManyEntries = errors.New("conformance: more directory entries than the bound")

// walkBounded walks root as filepath.WalkDir does, in lexical order and
// without following a link, but lists each directory with listBounded, and
// stops with errTooManyEntries once the entries below root pass limit, so no
// directory is held whole past the bound. fn may return fs.SkipDir for a
// directory, and fs.SkipAll.
func walkBounded(root string, limit int, fn fs.WalkDirFunc) error {
	info, err := os.Lstat(root)
	if err != nil {
		return skipped(fn(root, nil, err))
	}
	seen := 0
	var walk func(full string, entry fs.DirEntry) error
	walk = func(full string, entry fs.DirEntry) error {
		if err := fn(full, entry, nil); err != nil || !entry.IsDir() {
			return err
		}
		children, over, err := listBounded(full, func(fs.DirEntry) bool { return true }, limit-seen)
		switch {
		case over:
			return errTooManyEntries
		case err != nil:
			return fn(full, entry, err)
		}
		seen += len(children)
		for _, child := range children {
			err := walk(filepath.Join(full, child.Name()), child)
			if err == fs.SkipDir && child.IsDir() {
				continue
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	return skipped(walk(root, fs.FileInfoToDirEntry(info)))
}

// skipped is a walk's result with fs.SkipDir and fs.SkipAll ending it
// without error, as filepath.WalkDir treats them.
func skipped(err error) error {
	if err == fs.SkipDir || err == fs.SkipAll {
		return nil
	}
	return err
}

// maxVectorEntries bounds the entries of one vector's directory: it holds at
// most six known members, so a listing past this bound is refused unread.
var maxVectorEntries = 64

// maxExpectedEntries bounds the entries of a vector's expected/, files and
// directories together.
var maxExpectedEntries = 2 * emit.MaxFiles

// layoutVector checks one vector directory against the layout, by its
// entries and their sizes, reading none of its files.
func layoutVector(dir, name string, update bool) (*Vector, []emit.Diagnostic) {
	v := &Vector{Name: name, Dir: dir, present: map[string]bool{}}
	var refused []emit.Diagnostic
	fail := func(code, file, pointer string) {
		refused = append(refused, vectorDiagnostic(code, v.at(file), pointer))
	}
	entries, over, err := listBounded(dir, func(fs.DirEntry) bool { return true }, maxVectorEntries)
	switch {
	case over:
		return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-VECTOR", name, "")}
	case err != nil:
		return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-PATH-UNREADABLE", name, "")}
	}
	present := v.present
	for _, entry := range entries {
		wantDir, known := vectorMembers[entry.Name()]
		if !known || entry.Type()&fs.ModeSymlink != 0 || entry.IsDir() != wantDir {
			fail("EMIT-VECTOR", entry.Name(), "")
			continue
		}
		present[entry.Name()] = true
	}
	switch {
	case present[fileRequest] == present[fileRegistry]:
		fail("EMIT-VECTOR", "", "")
	case present[fileRequest] && (present[dirKinds] || present[fileOptions]):
		fail("EMIT-VECTOR", "", "")
	case present[fileRegistry] && !present[dirKinds]:
		fail("EMIT-VECTOR", dirKinds, "")
	case present[dirExpected] && present[fileExpectedFailure]:
		fail("EMIT-VECTOR", "", "")
	case !present[dirExpected] && !present[fileExpectedFailure] && !update:
		fail("EMIT-VECTOR", dirExpected, "")
	}
	if len(refused) > 0 {
		return nil, refused
	}
	if present[dirKinds] {
		// As `jpack emit --kinds DIR` reads a directory: its *.json files, in
		// name order, listed only to one past the bound.
		kinds, over, err := listBounded(filepath.Join(dir, dirKinds), func(kind fs.DirEntry) bool {
			return !kind.IsDir() && strings.HasSuffix(kind.Name(), ".json")
		}, maxKindFiles)
		switch {
		case err != nil:
			fail("EMIT-PATH-UNREADABLE", dirKinds, "")
		case over:
			fail("EMIT-VECTOR", dirKinds, "")
		}
		for _, kind := range kinds {
			v.kinds = append(v.kinds, kind.Name())
		}
	}
	if present[dirExpected] {
		expected, problems := layoutExpected(filepath.Join(dir, dirExpected), v.at(dirExpected), update)
		refused = append(refused, problems...)
		v.expected = expected
	}
	if len(refused) > 0 {
		return nil, refused
	}
	return v, nil
}

// errNotPlainDirectory is a directory of a vector that is no longer one, or
// is now a link, since its layout.
var errNotPlainDirectory = errors.New("conformance: a vector directory is no longer a directory")

// plainDirectory checks that a path is a directory and not a link: by
// Lstat, so a link, or a Windows junction, is not one.
func plainDirectory(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errNotPlainDirectory
	}
	return nil
}

// path is a file of the vector by its slash path, once the vector directory
// and every directory between it and the file are checked again, at the
// read, to be directories and not links: the layout checked them, and one
// swapped for a link since would be read through. The file itself is opened
// without following a link (fssecure.ReadRegular).
func (v *Vector) path(file string) (string, error) {
	at := v.Dir
	if err := plainDirectory(at); err != nil {
		return "", err
	}
	parts := strings.Split(file, "/")
	for _, part := range parts[:len(parts)-1] {
		at = filepath.Join(at, part)
		if err := plainDirectory(at); err != nil {
			return "", err
		}
	}
	return filepath.Join(at, parts[len(parts)-1]), nil
}

// read reads a file of the vector by its slash path under limit, through
// path.
func (v *Vector) read(file string, limit int64) ([]byte, error) {
	full, err := v.path(file)
	if err != nil {
		return nil, err
	}
	return fssecure.ReadRegular(full, limit)
}

// load reads a laid-out vector's files, each under its bound, just before
// its run. What refuses a vector here refuses the run, as its layout does.
// Each is read through path: a directory of the vector swapped for a link
// since its layout is EMIT-PATH-UNREADABLE.
func (v *Vector) load() []emit.Diagnostic {
	var refused []emit.Diagnostic
	fail := func(code, file, pointer string) {
		refused = append(refused, vectorDiagnostic(code, v.at(file), pointer))
	}
	unreadable := func(file string) { fail("EMIT-PATH-UNREADABLE", file, "") }

	if v.present[fileRequest] {
		data, err := v.read(fileRequest, MaxRequestBytes)
		switch {
		case errors.Is(err, fssecure.ErrTooLarge):
			v.Request = []byte{}
			v.Refused = append(v.Refused, emit.NewDiagnostic("EMIT-REQUEST", emit.Location{Document: emit.DocumentRequest}))
		case err != nil:
			unreadable(fileRequest)
		default:
			v.Request = data
		}
	} else {
		v.Registry = v.readBounded(fileRegistry, registry.MaxRegistryBytes, func() { unreadable(fileRegistry) })
		for _, file := range v.kinds {
			v.Kinds = append(v.Kinds, v.readBounded(dirKinds+"/"+file, registry.MaxKindBytes, func() { unreadable(dirKinds + "/" + file) }))
		}
		if v.present[fileOptions] {
			data, err := v.read(fileOptions, emit.MaxOptionsBytes)
			switch {
			case errors.Is(err, fssecure.ErrTooLarge):
				v.Refused = append(v.Refused, emit.NewDiagnostic("EMIT-OPTIONS", emit.Location{Document: emit.DocumentOptions}))
			case err != nil:
				unreadable(fileOptions)
			default:
				value, code, pointer := registry.AdmitValue(data, emit.MaxOptionsBytes)
				if code != "" {
					v.Refused = append(v.Refused, emit.NewDiagnostic("EMIT-OPTIONS", emit.Location{Document: emit.DocumentOptions, Pointer: pointer}))
				}
				v.Options = value
			}
		}
	}

	if v.present[fileExpectedFailure] {
		var value any
		problems := []emit.Diagnostic{vectorDiagnostic("EMIT-PATH-UNREADABLE", v.at(fileExpectedFailure), "")}
		if full, err := v.path(fileExpectedFailure); err == nil {
			value, problems = readSmallDocument(full, v.at(fileExpectedFailure))
		}
		refused = append(refused, problems...)
		object, _ := value.(map[string]any)
		code, _ := object["code"].(string)
		if problems == nil && (len(object) != 1 || !failureCode(code)) {
			fail("EMIT-VECTOR", fileExpectedFailure, "/code")
		}
		v.ExpectedFailure = code
	}
	if v.present[dirExpected] {
		expected, problems := v.readExpected()
		refused = append(refused, problems...)
		v.Expected = expected
	}
	if len(refused) > 0 {
		v.release()
	}
	return refused
}

// release drops what load read, once the vector's checks are done.
func (v *Vector) release() {
	v.Request, v.Registry, v.Kinds, v.Options, v.Refused, v.Expected = nil, registry.Document{}, nil, nil, nil, nil
}

func (v *Vector) readBounded(file string, limit int, unreadable func()) registry.Document {
	data, err := v.read(file, int64(limit))
	switch {
	case errors.Is(err, fssecure.ErrTooLarge):
		return registry.Document{Oversize: true}
	case err != nil:
		unreadable()
		return registry.Document{}
	}
	return registry.Document{Data: data}
}

// emitterCode is the grammar of an emitter diagnostic's code, as the host
// reads it.
var emitterCode = regexp.MustCompile(`^x-[a-z0-9]+(-[a-z0-9]+)*$`)

// failureCode says whether a code is one a run can fail with: a host code of
// error severity, or an emitter's code, whose error diagnostic fails the run.
func failureCode(code string) bool {
	if emitterCode.MatchString(code) && len(code) <= 64 {
		return true
	}
	if !strings.HasPrefix(code, "EMIT-") {
		return false
	}
	_, listed := emit.Codes[code]
	return listed && emit.NewDiagnostic(code, emit.Location{}).Severity == emit.SeverityError
}

// layoutExpected lays out expected/: every regular file below it, by its
// path relative to it, with its mode, found by its entries alone. A link, or
// anything that is not a file or a directory, refuses the vector, and so
// does a set past the response bounds, by the files' sizes. The marker
// ~empty, a zero-byte regular file alone at the root of expected/, gives no
// files; ~empty anywhere else, of another kind or size, or beside anything,
// refuses the vector. An expected/ with neither a regular file nor the marker
// (empty, or holding only directories) refuses it too, as no record of a run
// at all, except under --update, which replaces it.
func layoutExpected(dir, at string, update bool) ([]expectedEntry, []emit.Diagnostic) {
	var entries []expectedEntry
	var refused []emit.Diagnostic
	var total int64
	marker, beside := false, false
	err := walkBounded(dir, maxExpectedEntries, func(full string, entry fs.DirEntry, err error) error {
		relative, _ := filepath.Rel(dir, full)
		path := filepath.ToSlash(relative)
		where := at + "/" + path
		if err != nil {
			refused = append(refused, vectorDiagnostic("EMIT-PATH-UNREADABLE", where, ""))
			return fs.SkipDir
		}
		switch {
		case path == ".":
			if !entry.IsDir() {
				refused = append(refused, vectorDiagnostic("EMIT-VECTOR", at, ""))
			}
			return nil
		case entry.Name() == fileEmpty:
			info, err := entry.Info()
			if path != fileEmpty || !entry.Type().IsRegular() || err != nil || info.Size() != 0 {
				refused = append(refused, vectorDiagnostic("EMIT-VECTOR", where, ""))
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			marker = true
			return nil
		}
		beside = true
		switch {
		case entry.IsDir():
			return nil
		case !entry.Type().IsRegular():
			refused = append(refused, vectorDiagnostic("EMIT-VECTOR", where, ""))
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			refused = append(refused, vectorDiagnostic("EMIT-PATH-UNREADABLE", where, ""))
			return nil
		}
		total += info.Size()
		if len(entries) == emit.MaxFiles || info.Size() > emit.MaxFileBytes || total > emit.MaxResponseBytes {
			refused = append(refused, vectorDiagnostic("EMIT-VECTOR", where, ""))
			return fs.SkipAll
		}
		entries = append(entries, expectedEntry{path: path, mode: emit.ModeOf(info)})
		return nil
	})
	switch {
	case errors.Is(err, errTooManyEntries):
		return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-VECTOR", at, "")}
	case err != nil && len(refused) == 0:
		refused = append(refused, vectorDiagnostic("EMIT-PATH-UNREADABLE", at, ""))
	}
	switch {
	case marker && beside:
		refused = append(refused, vectorDiagnostic("EMIT-VECTOR", at+"/"+fileEmpty, ""))
	case !marker && len(entries) == 0 && len(refused) == 0 && !update:
		refused = append(refused, vectorDiagnostic("EMIT-VECTOR", at, ""))
	}
	return entries, refused
}

// readExpected reads the files of expected/ that its layout gives, each
// under the response's bounds, which a file that grew since cannot pass.
func (v *Vector) readExpected() (map[string]expectedFile, []emit.Diagnostic) {
	files := map[string]expectedFile{}
	total := 0
	for _, entry := range v.expected {
		where := v.at(dirExpected) + "/" + entry.path
		data, err := v.read(dirExpected+"/"+entry.path, emit.MaxFileBytes)
		switch {
		case errors.Is(err, fssecure.ErrTooLarge):
			return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-VECTOR", where, "")}
		case err != nil:
			return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-PATH-UNREADABLE", where, "")}
		}
		if total += len(data); total > emit.MaxResponseBytes {
			return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-VECTOR", where, "")}
		}
		files[entry.path] = expectedFile{data: data, mode: entry.mode}
	}
	return files, nil
}

// compareExpected compares a run's files with expected/, as check mode
// compares them with the disk: a path the response holds and expected/ does
// not is missing, one only expected/ holds is extra. Modes are not compared
// on Windows, which has none.
func compareExpected(files []emit.File, expected map[string]expectedFile) []emit.Difference {
	var out []emit.Difference
	seen := map[string]bool{}
	for _, file := range files {
		seen[file.Path] = true
		want, ok := expected[file.Path]
		switch {
		case !ok:
			out = append(out, emit.Difference{Path: file.Path, Difference: emit.DifferenceMissing})
		case string(want.data) != string(file.Data):
			out = append(out, emit.Difference{Path: file.Path, Difference: emit.DifferenceChanged})
		case want.mode != file.Mode && runtime.GOOS != "windows":
			out = append(out, emit.Difference{Path: file.Path, Difference: emit.DifferenceMode})
		}
	}
	for path := range expected {
		if !seen[path] {
			out = append(out, emit.Difference{Path: path, Difference: emit.DifferenceExtra})
		}
	}
	slices.SortFunc(out, func(a, b emit.Difference) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// writeExpected replaces expected/ with a run's files (--update), or with
// the marker ~empty alone for a run that gave none. It writes only below the
// vector's expected/, which must be a directory and not a link, or absent,
// in a vector directory that is still a directory and not a link; each path
// is one the host's path rules admitted. The new expected/ is written whole
// in a temporary directory beside it and renamed into place, so a write that
// fails partway leaves expected/ as it was.
func writeExpected(dir string, files []emit.File) error {
	if err := plainDirectory(dir); err != nil {
		return err
	}
	target := filepath.Join(dir, dirExpected)
	existed := false
	if info, err := os.Lstat(target); err == nil {
		if !info.IsDir() {
			return errors.New("conformance: expected/ is not a directory")
		}
		existed = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// The temporary directory holds the new expected/ until it is renamed
	// into place, and then the old one; it is removed with what it holds.
	work, err := os.MkdirTemp(dir, expectedUpdatePrefix)
	if err != nil {
		return err
	}
	// When the old expected/ cannot be put back, the temporary directory
	// still holds it, so it is kept and named: the vector is refused until
	// it is removed, and the old files are not lost.
	keep := false
	defer func() {
		if !keep {
			removeTree(work)
		}
	}()
	fresh, old := filepath.Join(work, "new"), filepath.Join(work, "old")
	if err := fillExpected(fresh, files); err != nil {
		return err
	}
	if existed {
		if err := rename(target, old); err != nil {
			return err
		}
	}
	if err := rename(fresh, target); err != nil {
		if existed {
			if restore := rename(old, target); restore != nil {
				keep = true
				return fmt.Errorf("conformance: expected/ could not be replaced or put back; the old one is kept in %s: %w", old, errors.Join(err, restore))
			}
		}
		return err
	}
	return nil
}

// rename is os.Rename, replaced in tests to make a rename fail.
var rename = os.Rename

// expectedUpdatePrefix names the temporary directory --update writes a new
// expected/ in, beside it. One left by a run that was stopped is not of the
// layout, so the vector is refused until it is removed.
const expectedUpdatePrefix = ".expected-update-"

// fillExpected writes a run's files, or the marker alone, to a new
// directory target.
func fillExpected(target string, files []emit.File) error {
	if err := os.Mkdir(target, 0o755); err != nil {
		return err
	}
	if len(files) == 0 {
		marker, err := os.OpenFile(filepath.Join(target, fileEmpty), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		return marker.Close()
	}
	for _, file := range files {
		full := filepath.Join(target, filepath.FromSlash(file.Path))
		if relative, err := filepath.Rel(target, full); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return errors.New("conformance: a path leaves expected/")
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		perm := os.FileMode(0o644)
		if file.Mode == emit.ModeExecutable {
			perm = 0o755
		}
		out, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
		if err != nil {
			return err
		}
		_, err = out.Write(file.Data)
		if err = errors.Join(err, out.Close()); err != nil {
			return err
		}
		// The umask may have taken bits off; the mode is the response's.
		if err := os.Chmod(full, perm); err != nil {
			return err
		}
	}
	return nil
}

// givenRequest forms the request of a vector that gives it whole: it is
// admitted, held to the envelope the host writes for the manifest (its
// selector the one the host writes, the manifest's emitter, options valid
// against its schema, and declarations and resolved, or factsContracts, as
// its input says), and written in its RFC 8785 canonical form. What it
// carries inside is the author's: a declaration that does not match its pin
// is a vector an emitter must refuse.
func givenRequest(data []byte, m *emit.Manifest) (emit.Request, []emit.Diagnostic) {
	refuse := func(code, document, pointer string) (emit.Request, []emit.Diagnostic) {
		return emit.Request{}, []emit.Diagnostic{emit.NewDiagnostic(code, emit.Location{Document: document, Pointer: pointer})}
	}
	value, code, pointer := registry.AdmitValue(data, maxGivenRequestBytes)
	if code != "" {
		return refuse("EMIT-REQUEST", emit.DocumentRequest, pointer)
	}
	root, ok := value.(map[string]any)
	// The resolved form has five members, declarations among them; the
	// facts-contracts form four.
	member, members := "resolved", 5
	if m.Input == emit.InputFactsContracts {
		member, members = "factsContracts", 4
	}
	if !ok || len(root) != members {
		return refuse("EMIT-REQUEST", emit.DocumentRequest, "")
	}
	version, writes := emit.RequestVersion(m)
	if !writes {
		return refuse("EMIT-REQUEST-VERSION", emit.DocumentManifest, "/requestVersions")
	}
	if given, _ := root["emitRequestVersion"].(string); given != version {
		return refuse("EMIT-REQUEST-VERSION", emit.DocumentRequest, "/emitRequestVersion")
	}
	emitter, _ := root["emitter"].(map[string]any)
	if id, _ := emitter["id"].(string); len(emitter) != 2 || id != m.ID || emitter["version"] != m.Version {
		return refuse("EMIT-REQUEST", emit.DocumentRequest, "/emitter")
	}
	options, present := root["options"]
	if !present {
		return refuse("EMIT-REQUEST", emit.DocumentRequest, "/options")
	}
	if findings := m.Options.Validate(options); len(findings) > 0 {
		var refused []emit.Diagnostic
		for _, finding := range findings {
			refused = append(refused, emit.NewDiagnostic("EMIT-OPTIONS", emit.Location{Document: emit.DocumentOptions, Pointer: finding.Pointer}))
		}
		return emit.Request{}, refused
	}
	if m.Input == emit.InputResolved {
		if _, ok := root["declarations"].([]any); !ok {
			return refuse("EMIT-REQUEST", emit.DocumentRequest, "/declarations")
		}
	}
	items, ok := root[member].([]any)
	if !ok {
		return refuse("EMIT-REQUEST", emit.DocumentRequest, "/"+member)
	}
	request := emit.Request{Version: version}
	if m.Input == emit.InputResolved && m.Environments == emit.EnvironmentsEach {
		document, _ := firstItem(items).(map[string]any)
		environment, ok := document["environment"].(string)
		if len(items) != 1 || !ok {
			return refuse("EMIT-REQUEST", emit.DocumentRequest, "/resolved")
		}
		request.Environment = environment
	}
	canonical, err := jcs.EncodeDocument(root)
	// The canonical form can be longer than the file (1e20 is written as 21
	// digits), and it is what the emitter reads, so it is held to the bound.
	if err != nil || len(canonical) > maxGivenRequestBytes {
		return refuse("EMIT-REQUEST", emit.DocumentRequest, "")
	}
	request.Bytes = canonical
	return request, nil
}

// maxGivenRequestBytes is MaxRequestBytes, as a variable so that a test can
// lower it.
var maxGivenRequestBytes = MaxRequestBytes

func firstItem(items []any) any {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}
