package conformance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
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

// emitterCommand is where a test repository holds this test binary, and the
// command every manifest of these tests names: Windows runs only a file whose
// name has an executable extension.
var emitterCommand = map[bool]string{true: "bin/emitter.exe", false: "bin/emitter"}[runtime.GOOS == "windows"]

// Synthetic kinds: the emitter reads queue, which has a secret position;
// topic is a kind it does not read.
const (
	queueKind = `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/queue","version":"1.2.0","kind":"queue","owner":"platform",
"schema":{"type":"object","required":["retention"],"additionalProperties":false,"properties":{"retention":{"type":"integer","minimum":1,"maximum":30},"password":{"type":"string"}}},
"secrets":["/password"],"overlayable":["/retention","/password"]}`
	topicKind = `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/topic","version":"2.0.0","kind":"topic","owner":"platform",
"schema":{"type":"object","additionalProperties":false,"properties":{"partitions":{"type":"integer"}}}}`
)

// The synthetic secret references of the test registry, one an environment.
const (
	devReference  = "secret:gcp-sm:projects/example-dev/secrets/orders-queue/versions/3"
	prodReference = "secret:gcp-sm:projects/example-prod/secrets/orders-queue/versions/7"
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

// registryText is a valid registry of both kinds in two environments, with a
// secret reference in each.
func registryText(t *testing.T) string {
	t.Helper()
	return `{"registryFormatVersion":"1.0.0","id":"https://example.invalid/registries/r","version":"1.0.0",
"environments":["dev","prod"],"owners":["platform"],
"secretScopes":{"dev":{"gcp-sm":["example-dev"]},"prod":{"gcp-sm":["example-prod"]}},
"kinds":[{"kind":"queue","id":"https://example.invalid/kinds/queue","version":"1.2.0","digest":"` + kindDigest(t, queueKind) + `"},
{"kind":"topic","id":"https://example.invalid/kinds/topic","version":"2.0.0","digest":"` + kindDigest(t, topicKind) + `"}],
"entries":[{"kind":"queue","id":"orders","owner":"platform","value":{"retention":3,"password":"` + devReference + `"}},
{"kind":"topic","id":"events","owner":"platform","value":{"partitions":8}}],
"overlays":[{"environment":"prod","patches":[{"kind":"queue","id":"orders","set":{"/retention":14,"/password":"` + prodReference + `"}}]}]}`
}

// fixture is a repository holding this test binary at emitterCommand, and a
// vector directory beside it.
type fixture struct {
	t       *testing.T
	root    string
	vectors string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	if err := emittest.Link(dir, emitterCommand); err != nil {
		t.Fatal(err)
	}
	root, err := emit.RealRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, root: root, vectors: t.TempDir()}
}

// manifest is a manifest that runs the helper in mode, with members replaced
// or, set to nil, removed.
func (f *fixture) manifest(mode []string, changes map[string]any) *emit.Manifest {
	f.t.Helper()
	command := []any{emitterCommand, emittest.Switch}
	for _, item := range mode {
		command = append(command, item)
	}
	doc := map[string]any{
		"emitterManifestVersion": "1.0.0",
		"id":                     "https://example.invalid/emitters/test",
		"version":                "1.0.0",
		"owner":                  "platform",
		"reads":                  []any{map[string]any{"kind": "queue", "id": "https://example.invalid/kinds/queue", "versions": "^1.0.0"}},
		"requestVersions":        []any{"1.0.0"},
		"environments":           "each",
		"command":                command,
		"owns":                   []any{"gen/**"},
	}
	for name, value := range changes {
		if value == nil {
			delete(doc, name)
			continue
		}
		doc[name] = value
	}
	data, err := json.Marshal(doc)
	if err != nil {
		f.t.Fatal(err)
	}
	m, diagnostics := emit.ReadManifest(data)
	if m == nil {
		f.t.Fatalf("manifest refused: %+v", diagnostics)
	}
	return m
}

// write writes files below dir, by slash path.
func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for path, text := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// registryVector writes a vector of the test registry and its kinds, with
// files added.
func (f *fixture) registryVector(name string, files map[string]string) {
	f.t.Helper()
	all := map[string]string{"registry.json": registryText(f.t), "kinds/queue.json": queueKind, "kinds/topic.json": topicKind}
	for path, text := range files {
		all[path] = text
	}
	write(f.t, filepath.Join(f.vectors, name), all)
}

func (f *fixture) run(m *emit.Manifest, update bool) *Report {
	f.t.Helper()
	return Run(context.Background(), m, f.vectors, Options{Root: f.root, Timeout: time.Minute, Update: update})
}

// rowOf is the report's row of one vector and check.
func rowOf(t *testing.T, report *Report, vector string, check int) Row {
	t.Helper()
	for _, row := range report.Rows {
		if row.Vector == vector && row.Check == check {
			return row
		}
	}
	t.Fatalf("no row %s/%d in %s", vector, check, dump(report))
	return Row{}
}

func dump(report *Report) string {
	data, _ := report.Canonical()
	return string(data)
}

// wantRows checks the statuses of one vector's six rows, in check order.
func wantRows(t *testing.T, report *Report, vector string, statuses ...string) {
	t.Helper()
	for i, status := range statuses {
		if row := rowOf(t, report, vector, i+1); row.Status != status {
			t.Errorf("%s check %d: %s (%s), want %s", vector, i+1, row.Status, row.Reason, status)
		}
	}
}
