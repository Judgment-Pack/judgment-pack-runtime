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

// rowCounts are how many rows each class of §15 has: every class is run
// here.
var rowCounts = map[string]int{"assembler": 47, "index-checker": 5, "lock-writer": 11, "lock-checker": 36, "lock-signature-verifier": 63}

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
	lockRow
	verifyRow
	Writes *struct {
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
	if m.Companion != "registry" || m.FormatVersion != "1.1.0" || m.Status != "released" {
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

// TestRows runs every row of every class.
func TestRows(t *testing.T) {
	implemented := map[string]func(*testing.T, row){
		"assembler":               runAssembler,
		"index-checker":           runIndexChecker,
		"lock-writer":             runLockWriter,
		"lock-checker":            runLockChecker,
		"lock-signature-verifier": runVerifier,
	}
	m := readRows(t)
	run := map[string]int{}
	for _, r := range m.Vectors {
		check, built := implemented[r.Class]
		if !built {
			t.Errorf("%s: class %q is not run", r.ID, r.Class)
			continue
		}
		run[r.Class]++
		t.Run(r.ID, func(t *testing.T) { check(t, r) })
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

// TestEveryCodeIsRaisedByARow runs, for each code of §12 a class built here
// raises, one row whose report (or writer's refusal) holds it, and compares
// the whole report or refusal: the test of each code's row in
// docs/registry-mutation-table.md. A source signature code no writer row
// raises is an edit of write-assembled's inputs, refused with that code
// alone (§8: a source whose signature does not verify is refused with that
// signature's code). No key signs anything here: each edit breaks a document
// the rows signed, or the signer set. Every code has a case.
func TestEveryCodeIsRaisedByARow(t *testing.T) {
	rows := map[string]row{}
	for _, r := range readRows(t).Vectors {
		rows[r.ID] = r
	}
	type edit = func(testing.TB, *federation.WriteInput)
	cases := []struct {
		code, row string
		edit      edit
	}{
		{"FED-MANIFEST", "assemble-layer-role", nil},
		{"FED-COMMIT", "assemble-commit-abbreviated", nil},
		{"FED-REPOSITORY", "assemble-repository-userinfo", nil},
		{"FED-SOURCE-MISSING", "assemble-path-missing", nil},
		{"FED-ENVIRONMENTS", "assemble-environments-mismatch", nil},
		{"FED-ENVIRONMENT-GAP", "assemble-environment-gap", nil},
		{"FED-KNOWN-LIMIT", "assemble-known-limit", nil},
		{"FED-SOURCE-ASSEMBLED", "assemble-source-assembled-by-index", nil},
		{"FED-REGISTRY-DUPLICATE", "assemble-registry-duplicate", nil},
		{"FED-KIND-NOT-STANDARD", "assemble-kind-not-standard", nil},
		{"FED-KIND-CONFLICT", "assemble-kind-conflict", nil},
		{"FED-SCOPE-WIDENS", "assemble-scope-widens", nil},
		{"FED-BOUND-FORM", "assemble-bound-form", nil},
		{"FED-BOUND-ENVIRONMENT", "assemble-bound-environment", nil},
		{"FED-BOUND-KIND", "assemble-bound-kind", nil},
		{"FED-BOUND-POINTER-UNREACHABLE", "assemble-bound-pointer-unreachable", nil},
		{"FED-BOUND-NOT-COMPARABLE", "assemble-bound-not-comparable", nil},
		{"FED-BOUND-OVERLAP", "assemble-bound-overlap", nil},
		{"FED-BOUND-WIDENS", "assemble-widens-type", nil},
		{"FED-BOUND-ENTRIES", "assemble-entries-per-source", nil},
		{"FED-VALUE-BOUND", "assemble-value-bound", nil},
		{"FED-INDEX-MISMATCH", "index-check-content-digest", nil},
		{"LOCK-EXPECTED", "check-expected-missing", nil},
		{"LOCK-FORM", "check-lock-form", nil},
		{"LOCK-NOT-CANONICAL", "check-not-canonical", nil},
		{"LOCK-UNEXPECTED-REGISTRY", "check-unexpected-registry", nil},
		{"LOCK-INDEX-REQUIRED", "check-index-required", nil},
		{"LOCK-REGISTRY-INVALID", "check-registry-invalid", nil},
		{"LOCK-REGISTRY-MISMATCH", "check-registry-mismatch", nil},
		{"LOCK-KINDS-MISMATCH", "check-kinds-mismatch", nil},
		{"LOCK-RESOLVED-MISMATCH", "check-resolved-mismatch", nil},
		{"LOCK-INDEX-MISSING", "check-index-missing", nil},
		{"LOCK-INDEX-MISMATCH", "check-index-digest-mismatch", nil},
		{"LOCK-INDEX-UNLOCKED", "check-index-unlocked", nil},
		{"LOCK-SOURCES-MISMATCH", "check-sources-mismatch", nil},
		{"LOCK-EMITTER-MISSING", "check-emitters-missing", nil},
		{"LOCK-EMITTER-MISMATCH", "check-emitter-file-differs", nil},
		{"LOCK-NOT-COVERED", "coverage-wrong-environment", nil},
		{"LOCK-SERIAL-ROLLBACK", "verify-rollback", nil},
		{"LOCK-SERIAL-CONFLICT", "verify-serial-conflict", nil},
		{"LOCK-SERIAL-STATE", "verify-record-unreadable", nil},
		{"LOCK-SIG-UNEXPECTED-KEY", "write-source-signature-wrong-key", nil},
		{"LOCK-SIG-EXPECTED-KEY", "write-assembled", func(t testing.TB, in *federation.WriteInput) {
			in.Signers.Key = ""
		}},
		{"LOCK-SIG-MISSING", "write-assembled", func(t testing.TB, in *federation.WriteInput) {
			delete(in.SourceSignatures, "billing")
		}},
		{"LOCK-SIG-EXTRA", "write-assembled", func(t testing.TB, in *federation.WriteInput) {
			in.SourceSignatures["ledger"] = in.SourceSignatures["billing"]
		}},
		{"LOCK-SIG-DOCUMENT", "write-assembled", func(t testing.TB, in *federation.WriteInput) {
			data := in.SourceSignatures["billing"].Data
			in.SourceSignatures["billing"] = registry.Document{Data: append([]byte(`{"x\u0041":1,`), data[1:]...)}
		}},
		{"LOCK-SIG-VERSION", "write-assembled", func(t testing.TB, in *federation.WriteInput) {
			editSignature(t, in, func(v map[string]any) { v["sourceSignatureVersion"] = "1.2.0" })
		}},
		{"LOCK-SIG-MEMBER", "write-assembled", func(t testing.TB, in *federation.WriteInput) {
			editSignature(t, in, func(v map[string]any) { v["comment"] = "x" })
		}},
		{"LOCK-SIG-ALGORITHM", "write-assembled", func(t testing.TB, in *federation.WriteInput) {
			editSignature(t, in, func(v map[string]any) { v["algorithm"] = "ed448" })
		}},
		{"LOCK-SIG-PUBLIC-KEY", "write-assembled", func(t testing.TB, in *federation.WriteInput) {
			editSignature(t, in, func(v map[string]any) { v["publicKey"] = strings.Repeat("0", 64) })
		}},
		{"LOCK-SIG-FORM", "write-assembled", func(t testing.TB, in *federation.WriteInput) {
			editSignature(t, in, func(v map[string]any) { v["signature"] = v["signature"].(string)[:126] })
		}},
		{"LOCK-SIG-DIGEST-MISMATCH", "write-assembled", func(t testing.TB, in *federation.WriteInput) {
			editSignature(t, in, func(v map[string]any) { v["sourceDigest"] = "sha256:" + strings.Repeat("ab", 32) })
		}},
		{"LOCK-SIG-INVALID", "write-assembled", func(t testing.TB, in *federation.WriteInput) {
			editSignature(t, in, func(v map[string]any) {
				signature := []byte(v["signature"].(string))
				signature[0] ^= 1 // '3' to '2': still of its form
				v["signature"] = string(signature)
			})
		}},
	}
	covered := map[string]bool{}
	for _, c := range cases {
		if covered[c.code] {
			t.Errorf("%s: a second case", c.code)
		}
		covered[c.code] = true
		t.Run(c.code, func(t *testing.T) {
			r, ok := rows[c.row]
			if !ok {
				t.Fatalf("no row %s", c.row)
			}
			if c.edit != nil {
				in := writeInput(t, r)
				c.edit(t, &in)
				lock, err := federation.WriteLock(in)
				if got := refusalCodes(t, err); !slices.Equal(got, []string{c.code}) || lock != nil {
					t.Fatalf("refused %v, want [%s] and no lock", got, c.code)
				}
				return
			}
			if !slices.Contains(expectedCodes(t, r), c.code) {
				t.Fatalf("row %s does not report %s", c.row, c.code)
			}
			switch r.Class {
			case "assembler":
				runAssembler(t, r)
			case "index-checker":
				runIndexChecker(t, r)
			case "lock-writer":
				runLockWriter(t, r)
			case "lock-checker":
				runLockChecker(t, r)
			case "lock-signature-verifier":
				runVerifier(t, r)
			default:
				t.Fatalf("row %s is of class %s", c.row, r.Class)
			}
		})
	}
	for code := range federation.Codes {
		if !covered[code] {
			t.Errorf("%s: a code with no case", code)
		}
	}
}
