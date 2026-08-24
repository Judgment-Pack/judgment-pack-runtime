package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rawJSON marshals a payload the way the wire does, so the assertions below
// discriminate the contract — the member's spelling and presence in bytes —
// not just the Go field a renderer might never emit.
func rawJSON(t *testing.T, payload any) string {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func fixtureDigest(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "project", "onboarding.graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Every graph matrix run and validation binds itself to the exact document
// bytes it loaded (ADR-0030): the bare-hex digest a consumer holds against
// the served document's own sha256, so rows fetched in one call and structure
// fetched in another are provably about one revision. The digest is read off
// the one load, and it is absent exactly when the document did not load.
func TestGraphRunsCarryTheDocumentDigest(t *testing.T) {
	want := fixtureDigest(t)
	binding := `"graphSha256":"` + want + `"`

	loaded := fixtureProject(t)
	tested, failure := TestProject(loaded, newEngine(t), "", Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if len(tested.Graphs) != 1 || !strings.Contains(rawJSON(t, tested.Graphs[0]), binding) {
		t.Fatalf("the test entry binds the loaded bytes on the wire: %s", rawJSON(t, tested.Graphs[0]))
	}

	validated, failure := ValidateProject(loaded, "", "validate")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if len(validated.Graphs) != 1 || !strings.Contains(rawJSON(t, validated.Graphs[0]), binding) {
		t.Fatalf("the validation entry binds the loaded bytes on the wire: %s", rawJSON(t, validated.Graphs[0]))
	}
}

// The direct single-graph envelope echoes the digest of the document it was
// handed — the one load its caller performed — never a second read of the
// path. A digest planted on the document and impossible for the on-disk
// bytes proves which one the envelope reports.
func TestDirectTestEchoesTheHandedDocumentDigest(t *testing.T) {
	loaded := fixtureProject(t)
	graphBytes, err := os.ReadFile(filepath.Join("testdata", "project", "onboarding.graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	document, loadFailure := Load(graphBytes, "onboarding.graph.json")
	if loadFailure != nil {
		t.Fatal(loadFailure.Message)
	}
	planted := strings.Repeat("ab", 32)
	document.Digest = "sha256:" + planted
	rowsBytes, err := os.ReadFile(filepath.Join("testdata", "project", "onboarding.rows.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, rowsFailure := LoadRows(rowsBytes, "onboarding.rows.json")
	if rowsFailure != nil {
		t.Fatal(rowsFailure.Message)
	}
	output, failure := Test(loaded, newEngine(t), document, "onboarding.graph.json", "onboarding.rows.json", rows, Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if !strings.Contains(rawJSON(t, output), `"graphSha256":"`+planted+`"`) {
		t.Fatalf("the envelope echoes the handed document's digest, bare hex: got %q", output.GraphSHA256)
	}
}

// A rows failure after a successful document load keeps the digest: the
// document did load, those are the bytes the binding names, and the detail
// says what stopped the run.
func TestRowsFailureRetainsTheDocumentDigest(t *testing.T) {
	files := map[string]string{}
	for _, name := range []string{"jpack.json", "onboarding.graph.json", "sanctions-screening-0.1.0.pack.json", "vendor-onboarding-0.1.0.pack.json"} {
		data, err := os.ReadFile(filepath.Join("testdata", "project", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(data)
	}
	files["onboarding.rows.json"] = "{not json"
	loaded := writeProject(t, files)
	tested, failure := TestProject(loaded, newEngine(t), "", Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	entry := tested.Graphs[0]
	if len(tested.Graphs) != 1 || entry.Status != "mismatch" || entry.Detail == "" ||
		!strings.Contains(rawJSON(t, entry), `"graphSha256":"`+fixtureDigest(t)+`"`) {
		t.Fatalf("loaded document, failed rows: digest stays beside the detail: %s", rawJSON(t, entry))
	}
}

// A document that did not load has no bytes to bind: the member is absent
// from the wire — not empty — beside the detail or diagnostics that say why,
// on the test walk and the validation walk both.
func TestUnloadableGraphCarriesNoDigest(t *testing.T) {
	files := map[string]string{}
	for _, name := range []string{"jpack.json", "onboarding.rows.json", "sanctions-screening-0.1.0.pack.json", "vendor-onboarding-0.1.0.pack.json"} {
		data, err := os.ReadFile(filepath.Join("testdata", "project", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(data)
	}
	files["onboarding.graph.json"] = "{not json"
	loaded := writeProject(t, files)

	tested, failure := TestProject(loaded, newEngine(t), "", Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if len(tested.Graphs) != 1 || tested.Graphs[0].Detail == "" ||
		strings.Contains(rawJSON(t, tested.Graphs[0]), "graphSha256") {
		t.Fatalf("no load, no digest member, a detail instead: %s", rawJSON(t, tested.Graphs[0]))
	}

	validated, failure := ValidateProject(loaded, "", "validate")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if len(validated.Graphs) != 1 || validated.Graphs[0].Status != "invalid" ||
		len(validated.Graphs[0].Diagnostics) == 0 ||
		strings.Contains(rawJSON(t, validated.Graphs[0]), "graphSha256") {
		t.Fatalf("no load, no digest member, diagnostics instead: %s", rawJSON(t, validated.Graphs[0]))
	}
}
