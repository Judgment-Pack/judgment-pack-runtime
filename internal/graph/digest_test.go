package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// Every graph run and validation binds itself to the exact document bytes it
// loaded (ADR-0030): the bare-hex digest a consumer holds against the served
// document's own sha256, so rows fetched in one call and structure fetched in
// another are provably about one revision. The digest is read off the one
// load, and it is absent exactly when the document did not load.
func TestGraphRunsCarryTheDocumentDigest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "project", "onboarding.graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])

	loaded := fixtureProject(t)
	tested, failure := TestProject(loaded, newEngine(t), "", Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if len(tested.Graphs) != 1 || tested.Graphs[0].GraphSHA256 != want {
		t.Fatalf("the test entry binds the loaded bytes: got %q, want %q", tested.Graphs[0].GraphSHA256, want)
	}

	validated, failure := ValidateProject(loaded, "", "validate")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if len(validated.Graphs) != 1 || validated.Graphs[0].GraphSHA256 != want {
		t.Fatalf("the validation entry binds the loaded bytes: got %q, want %q", validated.Graphs[0].GraphSHA256, want)
	}
}

// A document that did not load has no bytes to bind: the member is absent
// beside the detail, never a digest of nothing.
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
	if len(tested.Graphs) != 1 || tested.Graphs[0].GraphSHA256 != "" || tested.Graphs[0].Detail == "" {
		t.Fatalf("no load, no digest, a detail instead: %+v", tested.Graphs[0])
	}
}
