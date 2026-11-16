package emit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit/emittest"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// TestMain makes this test binary the emitter when it is run as one: every
// emitter these tests run is this binary, linked into a temporary repository.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == emittest.Switch {
		os.Exit(emittest.Main(os.Args[2:]))
	}
	os.Exit(m.Run())
}

// Synthetic kinds: the emitter reads queue; topic is a kind it does not.
const (
	queueKind = `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/queue","version":"1.2.0","kind":"queue","owner":"platform",
"schema":{"type":"object","required":["retention"],"additionalProperties":false,"properties":{"retention":{"type":"integer","minimum":1,"maximum":30}}},
"overlayable":["/retention"]}`
	topicKind = `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/topic","version":"2.0.0","kind":"topic","owner":"platform",
"schema":{"type":"object","additionalProperties":false,"properties":{"partitions":{"type":"integer"}}}}`
)

func kindDigest(t *testing.T, text string) string {
	t.Helper()
	var value any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	digest, err := registry.Digest(value)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

// testRegistry is a valid registry of both kinds in two environments.
func testRegistry(t *testing.T) *registry.Result {
	t.Helper()
	text := `{"registryFormatVersion":"1.0.0","id":"https://example.invalid/registries/r","version":"1.0.0",
"environments":["dev","prod"],"owners":["platform"],
"kinds":[{"kind":"queue","id":"https://example.invalid/kinds/queue","version":"1.2.0","digest":"` + kindDigest(t, queueKind) + `"},
{"kind":"topic","id":"https://example.invalid/kinds/topic","version":"2.0.0","digest":"` + kindDigest(t, topicKind) + `"}],
"entries":[{"kind":"queue","id":"orders","owner":"platform","value":{"retention":3}},
{"kind":"topic","id":"events","owner":"platform","value":{"partitions":8}}],
"overlays":[{"environment":"prod","patches":[{"kind":"queue","id":"orders","set":{"/retention":14}}]}]}`
	result := registry.Validate([]byte(text), [][]byte{[]byte(queueKind), []byte(topicKind)})
	if !result.Report.Valid {
		t.Fatalf("the test registry is not valid: %+v", result.Report.Diagnostics)
	}
	return result
}

// emitterCommand is where testRepo puts this test binary: Windows runs only
// a file whose name has an executable extension.
var emitterCommand = map[bool]string{true: "bin/emitter.exe", false: "bin/emitter"}[runtime.GOOS == "windows"]

// testRepo is a temporary repository holding this test binary at
// emitterCommand, as its real path.
func testRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := emittest.Link(dir, emitterCommand); err != nil {
		t.Fatal(err)
	}
	root, err := RealRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// manifestDoc is a manifest document that runs the helper in mode, with
// members replaced or, set to nil, removed.
func manifestDoc(mode []string, changes map[string]any) map[string]any {
	doc := map[string]any{
		"emitterManifestVersion": "1.0.0",
		"id":                     "https://example.invalid/emitters/test",
		"version":                "1.0.0",
		"owner":                  "platform",
		"reads":                  []any{map[string]any{"kind": "queue", "id": "https://example.invalid/kinds/queue", "versions": "^1.0.0"}},
		"requestVersions":        []any{"1.0.0"},
		"environments":           "each",
		"command":                append([]any{"bin/emitter", emittest.Switch}, toAny(mode)...),
		"owns":                   []any{"gen/**"},
	}
	for name, value := range changes {
		if value == nil {
			delete(doc, name)
			continue
		}
		doc[name] = value
	}
	return doc
}

func toAny(items []string) []any {
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = item
	}
	return out
}

func mustManifest(t *testing.T, doc map[string]any) *Manifest {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	m, diagnostics := ReadManifest(data)
	if m == nil {
		t.Fatalf("manifest refused: %+v", diagnostics)
	}
	return m
}

// writeFiles writes files under root, creating directories.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, text := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// responseJSON is a response document holding files.
func responseJSON(t *testing.T, files ...map[string]any) string {
	t.Helper()
	if files == nil {
		files = []map[string]any{}
	}
	data, err := json.Marshal(map[string]any{"emitResponseVersion": "1.0.0", "files": files, "diagnostics": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// echoHost runs the helper answering with response, from a fixture outside
// the owned paths, in write or check mode. It runs once, for all
// environments, unless changes say otherwise.
func echoHost(t *testing.T, root, response string, check bool, changes map[string]any) *Report {
	t.Helper()
	writeFiles(t, root, map[string]string{"fixtures/response.json": response})
	all := map[string]any{"environments": "all"}
	for name, value := range changes {
		all[name] = value
	}
	m := mustManifest(t, manifestDoc([]string{"echo", "fixtures/response.json"}, all))
	return Emit(context.Background(), m, nil, Inputs{Registry: testRegistry(t)}, Options{Root: root, Check: check, Timeout: 30 * time.Second})
}

// codes lists a report's diagnostic codes with their locations' pointers or
// paths.
func codes(diagnostics []Diagnostic) []string {
	var out []string
	for _, d := range diagnostics {
		where := d.Location.Pointer
		if d.Location.Path != "" {
			where = d.Location.Path
		}
		out = append(out, d.Code+"@"+where)
	}
	slices.Sort(out)
	return out
}

func onDisk(t *testing.T, root, path string) (string, os.FileMode, bool) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	data, err := os.ReadFile(full)
	if err != nil {
		return "", 0, false
	}
	info, err := os.Lstat(full)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), info.Mode().Perm(), true
}
