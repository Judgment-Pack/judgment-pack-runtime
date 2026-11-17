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

// readVectors reads every vector directory below dir, in name order. Every
// vector is read whole, and checked against the layout, before any emitter
// runs. Under update, a vector may hold neither expected/ nor
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
		vector, problems := readVector(filepath.Join(dir, name), name, update)
		refused = append(refused, problems...)
		if vector != nil {
			vectors = append(vectors, vector)
		}
	}
	if len(refused) > 0 {
		return nil, refused
	}
	return vectors, nil
}

// readVector reads one vector directory.
func readVector(dir, name string, update bool) (*Vector, []emit.Diagnostic) {
	at := func(file string) string {
		if file == "" {
			return name
		}
		return name + "/" + file
	}
	var refused []emit.Diagnostic
	fail := func(code, file, pointer string) { refused = append(refused, vectorDiagnostic(code, at(file), pointer)) }
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []emit.Diagnostic{vectorDiagnostic("EMIT-PATH-UNREADABLE", name, "")}
	}
	present := map[string]bool{}
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
	v := &Vector{Name: name, Dir: dir}
	unreadable := func(file string) { fail("EMIT-PATH-UNREADABLE", file, "") }

	if present[fileRequest] {
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
		kinds, err := os.ReadDir(filepath.Join(dir, dirKinds))
		if err != nil {
			unreadable(dirKinds)
		}
		// As `jpack emit --kinds DIR` reads a directory: its *.json files, in
		// name order.
		var names []string
		for _, kind := range kinds {
			if !kind.IsDir() && strings.HasSuffix(kind.Name(), ".json") {
				names = append(names, kind.Name())
			}
		}
		slices.Sort(names)
		if len(names) > MaxKindFiles {
			fail("EMIT-VECTOR", dirKinds, "")
			names = nil
		}
		for _, file := range names {
			v.Kinds = append(v.Kinds, readBounded(filepath.Join(dir, dirKinds, file), registry.MaxKindBytes, func() { unreadable(dirKinds + "/" + file) }))
		}
		if present[fileOptions] {
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

	if present[fileExpectedFailure] {
		value, problems := readSmallDocument(filepath.Join(dir, fileExpectedFailure), at(fileExpectedFailure))
		refused = append(refused, problems...)
		object, _ := value.(map[string]any)
		code, _ := object["code"].(string)
		if problems == nil && (len(object) != 1 || !failureCode(code)) {
			fail("EMIT-VECTOR", fileExpectedFailure, "/code")
		}
		v.ExpectedFailure = code
	}
	if present[dirExpected] {
		expected, problems := readExpected(filepath.Join(dir, dirExpected), at(dirExpected))
		refused = append(refused, problems...)
		v.Expected = expected
	}
	if len(refused) > 0 {
		return nil, refused
	}
	return v, nil
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

// readExpected reads expected/: every regular file below it, by its path
// relative to it, with its bytes and its mode. A link, or anything that is not
// a file or a directory, refuses the vector, and so does a set past the
// response bounds.
func readExpected(dir, at string) (map[string]expectedFile, []emit.Diagnostic) {
	files := map[string]expectedFile{}
	var refused []emit.Diagnostic
	total := 0
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
		total += int(info.Size())
		if len(files) == emit.MaxFiles || info.Size() > emit.MaxFileBytes || total > emit.MaxResponseBytes {
			refused = append(refused, vectorDiagnostic("EMIT-VECTOR", where, ""))
			return fs.SkipAll
		}
		data, err := fssecure.ReadRegular(full, emit.MaxFileBytes)
		if err != nil {
			refused = append(refused, vectorDiagnostic("EMIT-PATH-UNREADABLE", where, ""))
			return nil
		}
		files[path] = expectedFile{data: data, mode: emit.ModeOf(info)}
		return nil
	})
	if err != nil && len(refused) == 0 {
		refused = append(refused, vectorDiagnostic("EMIT-PATH-UNREADABLE", at, ""))
	}
	return files, refused
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

// writeExpected replaces expected/ with a run's files (--update). It writes
// only below the vector's expected/, which must be a directory and not a
// link, or absent; each path is one the host's path rules admitted.
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
