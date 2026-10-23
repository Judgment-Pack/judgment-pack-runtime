package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
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

// Both drafts are bundled and load independently, and each verifies against its
// own lock. The evaluation corpus exists only in the version whose Core defines
// the evaluator conformance class.
func TestEveryBundledVersionLoads(t *testing.T) {
	versions := SupportedVersions()
	if len(versions) != 3 || versions[0] != DraftVersion || versions[1] != EvaluatorDraftVersion || versions[2] != CoreVersion {
		t.Fatalf("SupportedVersions = %v, want %v", versions, []string{DraftVersion, EvaluatorDraftVersion, CoreVersion})
	}
	for _, version := range versions {
		set, err := Load(version)
		if err != nil {
			t.Fatalf("load %s: %v", version, err)
		}
		if set.Lock().SpecVersion != version {
			t.Fatalf("lock names %q, want %q", set.Lock().SpecVersion, version)
		}
		if set.Lock().Source.Kind != "immutable-git-ref" || set.Lock().Source.WorktreeDirty {
			t.Fatalf("%s must be bundled from an immutable, clean source: %+v", version, set.Lock().Source)
		}
		if _, err := set.Schema(); err != nil {
			t.Fatalf("%s schema: %v", version, err)
		}
	}
	if _, err := Load("9.9.9-draft"); err == nil {
		t.Fatal("an unbundled version must not load")
	}
}

func TestEvaluationCorpusIsBundledOnlyWhereItIsPublished(t *testing.T) {
	older, err := Load(DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	if older.HasEvaluationCorpus() {
		t.Fatalf("JPS %s publishes no evaluation corpus", DraftVersion)
	}
	if _, err := older.EvaluationManifest(); err == nil {
		t.Fatal("reading an unbundled corpus manifest must fail")
	}

	newer, err := Load(EvaluatorDraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !newer.HasEvaluationCorpus() {
		t.Fatalf("JPS %s publishes an evaluation corpus", EvaluatorDraftVersion)
	}
	for _, read := range []func() ([]byte, error){
		newer.EvaluationManifest,
		newer.EvaluationManifestSchema,
		func() ([]byte, error) { return newer.EvaluationPack("packs/data-request-intake-triage.json") },
	} {
		data, err := read()
		if err != nil {
			t.Fatal(err)
		}
		if len(data) == 0 {
			t.Fatal("a bundled corpus artifact must not be empty")
		}
	}
	// A path the lock does not record is refused, so a corpus fixture cannot be
	// read from outside the digest-locked set.
	if _, err := newer.EvaluationPack("packs/../../schema.json"); err == nil {
		t.Fatal("an unrecorded path must be refused")
	}
}

func TestCore030ExactProvenanceAndCompleteIndexedFixtures(t *testing.T) {
	set, err := Load(CoreVersion)
	if err != nil {
		t.Fatal(err)
	}
	lock := set.Lock()
	const pin = "65c7cf5598a0f0fc532c80a9e2215090951104ce"
	if lock.Source.Repository != "https://github.com/Judgment-Pack/judgment-pack-spec" || lock.Source.Ref != pin || lock.Source.BaseCommit != pin || lock.Source.Kind != "immutable-git-ref" || lock.Source.WorktreeDirty {
		t.Fatalf("unexpected source: %+v", lock.Source)
	}
	if lock.BundleDigest.Value != "018b8221c424fa530d7482b160f4d7faa59cd9d2e80d3f6e2f8748dfb0d7e743" {
		t.Fatalf("bundle digest: %+v", lock.BundleDigest)
	}
	manifestBytes, err := set.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Cases []struct {
			Path string `json:"path"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Cases) != 156 {
		t.Fatalf("document cases: %d", len(manifest.Cases))
	}
	indexed := map[string]bool{}
	for _, item := range manifest.Cases {
		indexed["cases/"+item.Path] = true
	}
	evaluationBytes, err := set.EvaluationManifest()
	if err != nil {
		t.Fatal(err)
	}
	var evaluation struct {
		Cases []struct {
			Pack string `json:"pack"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(evaluationBytes, &evaluation); err != nil {
		t.Fatal(err)
	}
	if len(evaluation.Cases) != 20 {
		t.Fatalf("evaluation rows: %d", len(evaluation.Cases))
	}
	for _, item := range evaluation.Cases {
		indexed["evaluation/"+item.Pack] = true
	}
	for _, entry := range lock.Files {
		var data []byte
		switch {
		case strings.HasPrefix(entry.Path, "cases/"):
			data, err = set.Case(strings.TrimPrefix(entry.Path, "cases/"))
		case strings.HasPrefix(entry.Path, "evaluation/packs/"):
			data, err = set.EvaluationPack(strings.TrimPrefix(entry.Path, "evaluation/"))
		default:
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if len(data) != entry.Bytes || hex.EncodeToString(sum[:]) != entry.SHA256 {
			t.Fatalf("fixture digest mismatch: %s", entry.Path)
		}
		if !indexed[entry.Path] {
			t.Fatalf("unindexed fixture: %s", entry.Path)
		}
		delete(indexed, entry.Path)
	}
	if len(indexed) != 0 {
		t.Fatalf("indexed fixtures missing from hash inventory: %v", indexed)
	}
}
