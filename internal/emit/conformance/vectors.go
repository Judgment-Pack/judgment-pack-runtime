package conformance

import (
	"errors"
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
	// MaxRequestBytes bounds a vector's request.json.
	MaxRequestBytes = 64 << 20
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
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-PATH-UNREADABLE", "", "")}
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
		if len(vectors) == MaxVectors {
			return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-VECTOR", "", "")}
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

// layoutVector checks one vector directory against the layout, by its
// entries and their sizes, reading none of its files.
func layoutVector(dir, name string, update bool) (*Vector, []emit.Diagnostic) {
	v := &Vector{Name: name, Dir: dir, present: map[string]bool{}}
	var refused []emit.Diagnostic
	fail := func(code, file, pointer string) {
		refused = append(refused, vectorDiagnostic(code, v.at(file), pointer))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
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
		kinds, err := os.ReadDir(filepath.Join(dir, dirKinds))
		if err != nil {
			fail("EMIT-PATH-UNREADABLE", dirKinds, "")
		}
		// As `jpack emit --kinds DIR` reads a directory: its *.json files, in
		// name order.
		for _, kind := range kinds {
			if !kind.IsDir() && strings.HasSuffix(kind.Name(), ".json") {
				v.kinds = append(v.kinds, kind.Name())
			}
		}
		slices.Sort(v.kinds)
		if len(v.kinds) > MaxKindFiles {
			fail("EMIT-VECTOR", dirKinds, "")
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

// load reads a laid-out vector's files, each under its bound, just before
// its run. What refuses a vector here refuses the run, as its layout does.
func (v *Vector) load() []emit.Diagnostic {
	var refused []emit.Diagnostic
	fail := func(code, file, pointer string) {
		refused = append(refused, vectorDiagnostic(code, v.at(file), pointer))
	}
	unreadable := func(file string) { fail("EMIT-PATH-UNREADABLE", file, "") }
	dir := v.Dir

	if v.present[fileRequest] {
		data, err := fssecure.ReadRegular(filepath.Join(dir, fileRequest), MaxRequestBytes)
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
		v.Registry = readBounded(filepath.Join(dir, fileRegistry), registry.MaxRegistryBytes, func() { unreadable(fileRegistry) })
		for _, file := range v.kinds {
			v.Kinds = append(v.Kinds, readBounded(filepath.Join(dir, dirKinds, file), registry.MaxKindBytes, func() { unreadable(dirKinds + "/" + file) }))
		}
		if v.present[fileOptions] {
			data, err := fssecure.ReadRegular(filepath.Join(dir, fileOptions), emit.MaxOptionsBytes)
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
		value, problems := readSmallDocument(filepath.Join(dir, fileExpectedFailure), v.at(fileExpectedFailure))
		refused = append(refused, problems...)
		object, _ := value.(map[string]any)
		code, _ := object["code"].(string)
		if problems == nil && (len(object) != 1 || !failureCode(code)) {
			fail("EMIT-VECTOR", fileExpectedFailure, "/code")
		}
		v.ExpectedFailure = code
	}
	if v.present[dirExpected] {
		expected, problems := readExpected(filepath.Join(dir, dirExpected), v.at(dirExpected), v.expected)
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

func readBounded(path string, limit int, unreadable func()) registry.Document {
	data, err := fssecure.ReadRegular(path, int64(limit))
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
	err := filepath.WalkDir(dir, func(full string, entry fs.DirEntry, err error) error {
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
	if err != nil && len(refused) == 0 {
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
func readExpected(dir, at string, entries []expectedEntry) (map[string]expectedFile, []emit.Diagnostic) {
	files := map[string]expectedFile{}
	total := 0
	for _, entry := range entries {
		where := at + "/" + entry.path
		data, err := fssecure.ReadRegular(filepath.Join(dir, filepath.FromSlash(entry.path)), emit.MaxFileBytes)
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
// vector's expected/, which must be a directory and not a link, or absent;
// each path is one the host's path rules admitted.
func writeExpected(dir string, files []emit.File) error {
	target := filepath.Join(dir, dirExpected)
	if info, err := os.Lstat(target); err == nil {
		if !info.IsDir() {
			return errors.New("conformance: expected/ is not a directory")
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
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
// against its schema, and resolved or factsContracts as its input says), and
// written in its RFC 8785 canonical form. What it carries inside is the
// author's.
func givenRequest(data []byte, m *emit.Manifest) (emit.Request, []emit.Diagnostic) {
	refuse := func(code, document, pointer string) (emit.Request, []emit.Diagnostic) {
		return emit.Request{}, []emit.Diagnostic{emit.NewDiagnostic(code, emit.Location{Document: document, Pointer: pointer})}
	}
	value, code, pointer := registry.AdmitValue(data, MaxRequestBytes)
	if code != "" {
		return refuse("EMIT-REQUEST", emit.DocumentRequest, pointer)
	}
	root, ok := value.(map[string]any)
	member := "resolved"
	if m.Input == emit.InputFactsContracts {
		member = "factsContracts"
	}
	if !ok || len(root) != 4 {
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
	if err != nil {
		return refuse("EMIT-REQUEST", emit.DocumentRequest, "")
	}
	request.Bytes = canonical
	return request, nil
}

func firstItem(items []any) any {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}
