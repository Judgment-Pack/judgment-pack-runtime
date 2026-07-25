package artifacts

import (
	"errors"
	"sort"
	"testing"
)

func TestEmbeddedReleaseArtifactIntegrityAndProvenance(t *testing.T) {
	set, err := Load(DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	lock := set.Lock()
	if lock.Source.Repository != "https://github.com/Judgment-Pack/judgment-pack-spec" ||
		lock.Source.Kind != "immutable-git-ref" ||
		lock.Source.BaseCommit != "5df1f5502a61eed2ce7509d03b00e3d387558183" ||
		lock.Source.Ref != "5df1f5502a61eed2ce7509d03b00e3d387558183" ||
		lock.Source.WorktreeDirty {
		t.Fatalf("embedded artifacts must remain pinned to the approved JPS release: %#v", lock.Source)
	}
	if len(lock.Files) != 50 || len(lock.BundleDigest.Value) != 64 {
		t.Fatalf("unexpected lock contents: files=%d digest=%q", len(lock.Files), lock.BundleDigest.Value)
	}
}

func TestUnknownVersionIsNotSubstituted(t *testing.T) {
	if _, err := Load("0.1"); err == nil {
		t.Fatal("expected an exact-version error")
	}
}

// Examples surfaces only the valid conformance cases, named by their file stem
// and sorted, so a client sees a stable catalog of embedded fixtures.
func TestExamplesEnumeratesValidCasesSortedByName(t *testing.T) {
	set, err := Load(DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	examples, err := set.Examples()
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) == 0 {
		t.Fatal("expected at least one valid example")
	}
	names := make([]string, len(examples))
	for i, example := range examples {
		names[i] = example.Name
		if example.Focus == "" {
			t.Errorf("example %q must carry its manifest focus", example.Name)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("examples must be sorted by name: %v", names)
	}
}

// Example resolves a name to the exact bytes of that valid case, and rejects a
// name that is not one of the enumerated examples so caller input can never
// address an arbitrary embedded path.
func TestExampleReturnsBytesAndRejectsUnknownNames(t *testing.T) {
	set, err := Load(DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	data, info, err := set.Example("minimal-literal")
	if err != nil {
		t.Fatal(err)
	}
	want, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(want) {
		t.Fatal("Example returned bytes that differ from the embedded valid case")
	}
	if info.Name != "minimal-literal" {
		t.Errorf("info.Name = %q, want minimal-literal", info.Name)
	}

	// Names that resolve onto real in-lock artifacts under a naive
	// build-a-path-and-let-Read-fail implementation must still be rejected, and
	// no bytes may be returned on any rejection — otherwise a coerced error could
	// leak a non-example artifact.
	for _, bad := range []string{
		"", " ", "no-such-example",
		"../schema", "../../schema", "../../manifest", "valid/../../schema",
		"valid/minimal-literal", "minimal-literal.json", "MINIMAL-LITERAL",
		"manifest", "schema", "lock",
	} {
		data, _, err := set.Example(bad)
		var unknown *UnknownExampleError
		if !errors.As(err, &unknown) {
			t.Errorf("Example(%q) = %v, want *UnknownExampleError", bad, err)
		}
		if data != nil {
			t.Errorf("Example(%q) leaked %d bytes for a rejected name", bad, len(data))
		}
	}
}
