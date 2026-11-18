package federation_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/federation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// pendingClasses are the conformance classes of §15 this package does not
// build yet. The list shrinks as each is built, and is empty once the last
// is: TestRows fails when a listed class is run, and when a class of the
// rows is neither listed nor run. scripts/check-mutation-table.py reads it
// too: a code it lists as pending is allowed only while a class is.
var pendingClasses = []string{"lock-writer", "lock-checker", "lock-signature-verifier"}

// rowCounts are how many rows each class run here has.
var rowCounts = map[string]int{"assembler": 46, "index-checker": 5}

type rowSource struct {
	Commit string            `json:"commit"`
	Files  map[string]string `json:"files"`
}

type row struct {
	ID         string               `json:"id"`
	Operation  string               `json:"operation"`
	Class      string               `json:"class"`
	Federation string               `json:"federation"`
	Sources    map[string]rowSource `json:"sources"`
	Known      struct {
		Indexes []string `json:"indexes"`
		Locks   []string `json:"locks"`
	} `json:"known"`
	Index    string `json:"index"`
	Expected string `json:"expected"`
	Writes   *struct {
		Registry string `json:"registry"`
		Index    string `json:"index"`
	} `json:"writes"`
}

type rowManifest struct {
	Companion     string `json:"companion"`
	FormatVersion string `json:"formatVersion"`
	Status        string `json:"status"`
	Vectors       []row  `json:"vectors"`
}

func readRows(t *testing.T) rowManifest {
	t.Helper()
	var m rowManifest
	raw := readRowFile(t, "manifest.json")
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m.Companion != "registry" || m.FormatVersion != "1.1.0" || m.Status != "proposed" {
		t.Fatalf("the rows are for %s %s, %s", m.Companion, m.FormatVersion, m.Status)
	}
	return m
}

func readRowFile(t testing.TB, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(rowsDir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// rowInput is a row's inputs, read from its files.
func rowInput(t testing.TB, r row) federation.Input {
	t.Helper()
	in := federation.Input{
		Manifest: registry.Document{Data: readRowFile(t, r.Federation)},
		Sources:  map[string]federation.SourceInput{},
	}
	for name, s := range r.Sources {
		files := map[string]registry.Document{}
		for path, file := range s.Files {
			files[path] = registry.Document{Data: readRowFile(t, file)}
		}
		in.Sources[name] = federation.SourceInput{Commit: s.Commit, Files: files}
	}
	in.Known.Indexes = []registry.Document{}
	for _, file := range r.Known.Indexes {
		in.Known.Indexes = append(in.Known.Indexes, registry.Document{Data: readRowFile(t, file)})
	}
	in.Known.Locks = []registry.Document{}
	for _, file := range r.Known.Locks {
		in.Known.Locks = append(in.Known.Locks, registry.Document{Data: readRowFile(t, file)})
	}
	return in
}

// canonicalReport is a report's canonical form with every message removed,
// as the rows compare it.
func canonicalReport(t testing.TB, raw []byte) []byte {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("a report that is not JSON: %v", err)
	}
	diagnostics, _ := value["diagnostics"].([]any)
	for _, item := range diagnostics {
		if d, ok := item.(map[string]any); ok {
			delete(d, "message")
		}
	}
	out, err := jcs.EncodeDocument(value)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func compareReport(t *testing.T, r row, report federation.Report) {
	t.Helper()
	for _, d := range report.Diagnostics {
		if d.Message == "" {
			t.Errorf("%s: %s has no message", r.ID, d.Code)
		}
	}
	raw, err := report.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	got, want := canonicalReport(t, raw), canonicalReport(t, readRowFile(t, r.Expected))
	if !bytes.Equal(got, want) {
		t.Errorf("%s: report\n got %s\nwant %s", r.ID, got, want)
	}
}

func runAssembler(t *testing.T, r row) {
	out, err := federation.Assemble(rowInput(t, r))
	if err != nil {
		t.Fatal(err)
	}
	compareReport(t, r, out.Report)
	if r.Writes == nil {
		if out.Registry != nil || out.Index != nil {
			t.Errorf("%s: an assembly the row refuses wrote its output", r.ID)
		}
		return
	}
	if want := readRowFile(t, r.Writes.Registry); !bytes.Equal(out.Registry, want) {
		t.Errorf("%s: assembled registry\n got %s\nwant %s", r.ID, out.Registry, want)
	}
	if want := readRowFile(t, r.Writes.Index); !bytes.Equal(out.Index, want) {
		t.Errorf("%s: index\n got %s\nwant %s", r.ID, out.Index, want)
	}
}

func runIndexChecker(t *testing.T, r row) {
	report, err := federation.CheckIndex(rowInput(t, r), registry.Document{Data: readRowFile(t, r.Index)})
	if err != nil {
		t.Fatal(err)
	}
	compareReport(t, r, report)
}

// TestRows runs every row of each class this package builds, and holds the
// list of those it does not build yet.
func TestRows(t *testing.T) {
	implemented := map[string]func(*testing.T, row){
		"assembler":     runAssembler,
		"index-checker": runIndexChecker,
	}
	m := readRows(t)
	run := map[string]int{}
	present := map[string]bool{}
	for _, r := range m.Vectors {
		present[r.Class] = true
		check, built := implemented[r.Class]
		switch {
		case built:
			run[r.Class]++
			t.Run(r.ID, func(t *testing.T) { check(t, r) })
		case slices.Contains(pendingClasses, r.Class):
		default:
			t.Errorf("%s: class %q is neither run nor listed as pending", r.ID, r.Class)
		}
	}
	for _, class := range pendingClasses {
		if _, built := implemented[class]; built {
			t.Errorf("class %q is built and still listed as pending", class)
		}
		if !present[class] {
			t.Errorf("pending class %q has no rows", class)
		}
	}
	for class, want := range rowCounts {
		if run[class] != want {
			t.Errorf("class %q: ran %d rows, want %d", class, run[class], want)
		}
	}
	for class := range implemented {
		if _, counted := rowCounts[class]; !counted {
			t.Errorf("class %q is run without a row count", class)
		}
	}
}

// TestEveryCodeIsRaisedByARow runs, for each code of §12.1, one row whose
// report holds it, and compares the whole report: the test of each code's
// row in docs/registry-mutation-table.md. A code of §12.2 is pending (see
// pendingClasses) and has no case here.
func TestEveryCodeIsRaisedByARow(t *testing.T) {
	rows := map[string]row{}
	for _, r := range readRows(t).Vectors {
		rows[r.ID] = r
	}
	cases := []struct{ code, row string }{
		{"FED-MANIFEST", "assemble-layer-role"},
		{"FED-COMMIT", "assemble-commit-abbreviated"},
		{"FED-REPOSITORY", "assemble-repository-userinfo"},
		{"FED-SOURCE-MISSING", "assemble-path-missing"},
		{"FED-ENVIRONMENTS", "assemble-environments-mismatch"},
		{"FED-ENVIRONMENT-GAP", "assemble-environment-gap"},
		{"FED-KNOWN-LIMIT", "assemble-known-limit"},
		{"FED-SOURCE-ASSEMBLED", "assemble-source-assembled-by-index"},
		{"FED-REGISTRY-DUPLICATE", "assemble-registry-duplicate"},
		{"FED-KIND-NOT-STANDARD", "assemble-kind-not-standard"},
		{"FED-KIND-CONFLICT", "assemble-kind-conflict"},
		{"FED-SCOPE-WIDENS", "assemble-scope-widens"},
		{"FED-BOUND-FORM", "assemble-bound-form"},
		{"FED-BOUND-ENVIRONMENT", "assemble-bound-environment"},
		{"FED-BOUND-KIND", "assemble-bound-kind"},
		{"FED-BOUND-POINTER-UNREACHABLE", "assemble-bound-pointer-unreachable"},
		{"FED-BOUND-NOT-COMPARABLE", "assemble-bound-not-comparable"},
		{"FED-BOUND-OVERLAP", "assemble-bound-overlap"},
		{"FED-BOUND-WIDENS", "assemble-widens-type"},
		{"FED-BOUND-ENTRIES", "assemble-entries-per-source"},
		{"FED-VALUE-BOUND", "assemble-value-bound"},
		{"FED-INDEX-MISMATCH", "index-check-content-digest"},
	}
	covered := map[string]bool{}
	for _, c := range cases {
		covered[c.code] = true
		t.Run(c.code, func(t *testing.T) {
			r, ok := rows[c.row]
			if !ok {
				t.Fatalf("no row %s", c.row)
			}
			var expected federation.Report
			if err := json.Unmarshal(readRowFile(t, r.Expected), &expected); err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(expected.Diagnostics, func(d federation.Diagnostic) bool { return d.Code == c.code }) {
				t.Fatalf("row %s does not report %s", c.row, c.code)
			}
			switch r.Class {
			case "assembler":
				runAssembler(t, r)
			case "index-checker":
				runIndexChecker(t, r)
			default:
				t.Fatalf("row %s is of class %s", c.row, r.Class)
			}
		})
	}
	for code := range federation.Codes {
		if strings.HasPrefix(code, "FED-") != covered[code] {
			t.Errorf("%s: a code of §12.1 with no case, or a case that is not one", code)
		}
	}
}
