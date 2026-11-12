package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"reflect"
	"slices"
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

// All four exact versions load independently and verify against their own
// locks. Only versions with an indexed evaluation corpus expose that corpus.
func TestEveryBundledVersionLoads(t *testing.T) {
	versions := SupportedVersions()
	if !slices.Equal(versions, []string{"0.1.0-draft", "0.2.0-draft", "0.3.0", "0.5.0"}) {
		t.Fatalf("SupportedVersions = %v, want 0.1.0-draft, 0.2.0-draft, 0.3.0 and 0.5.0", versions)
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
}

// The path grammar rejects malformed manifest paths before joining them; the
// lock independently rejects admissible names that are absent from the corpus.
func TestEvaluationPackPathLayers(t *testing.T) {
	set, err := Load(EvaluatorDraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		path    string
		wantErr string
	}{
		{
			name:    "grammar/missing-prefix",
			path:    "schema.json",
			wantErr: "not an evaluation-corpus pack path: schema.json",
		},
		{
			name:    "grammar/empty-name",
			path:    "packs/",
			wantErr: "not an evaluation-corpus pack path: packs/",
		},
		{
			name:    "grammar/nested-path",
			path:    "packs/nested/a.json",
			wantErr: "not an evaluation-corpus pack path: packs/nested/a.json",
		},
		{
			name:    "grammar/traversal-to-locked-schema",
			path:    "packs/../../schema.json",
			wantErr: "not an evaluation-corpus pack path: packs/../../schema.json",
		},
		{
			name:    "grammar/non-json-suffix",
			path:    "packs/a.txt",
			wantErr: "not an evaluation-corpus pack path: packs/a.txt",
		},
		{
			name:    "grammar/leading-slash",
			path:    "/packs/a.json",
			wantErr: "not an evaluation-corpus pack path: /packs/a.json",
		},
		{
			name:    "lock/uppercase-name",
			path:    "packs/A.json",
			wantErr: "artifact is not recorded in lock: evaluation/packs/A.json",
		},
		{
			name:    "lock/dot-name",
			path:    "packs/..json",
			wantErr: "artifact is not recorded in lock: evaluation/packs/..json",
		},
		{
			name: "accepted/known-corpus-pack",
			path: "packs/data-request-intake-triage.json",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := set.EvaluationPack(tc.path)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if len(data) == 0 {
					t.Fatal("a bundled corpus pack must not be empty")
				}
				return
			}
			if err == nil || err.Error() != tc.wantErr {
				t.Errorf("EvaluationPack(%q) error = %v, want %q", tc.path, err, tc.wantErr)
			}
			if data != nil {
				t.Errorf("EvaluationPack(%q) returned %d bytes for a rejected path", tc.path, len(data))
			}
		})
	}
}

func TestCore030ExactProvenanceAndCompleteIndexedFixtures(t *testing.T) {
	set, err := Load(CoreVersion)
	if err != nil {
		t.Fatal(err)
	}
	lock := set.Lock()
	const tag, pin = "v0.3.0", "27cb29bf13e4a2d58d69fe19c7ebfeb762b014fc"
	if len(lock.Files) != 165 {
		t.Fatalf("Core 0.3 lock files: %d", len(lock.Files))
	}
	if lock.Source.Repository != "https://github.com/Judgment-Pack/judgment-pack-spec" || lock.Source.Ref != tag || lock.Source.BaseCommit != pin || lock.Source.Kind != "immutable-git-ref" || lock.Source.WorktreeDirty {
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

// Core 0.5.0 is imported from its release tag like 0.3.0: an exact commit,
// the tag's schemas, and every indexed fixture present and digest-locked.
// The counts are the release notes': 369 conformance cases and 5 advisory
// cases, 103 evaluation rows over 41 packs; the lock holds those fixtures and
// the five index and schema files.
func TestCore050ExactProvenanceAndCompleteIndexedFixtures(t *testing.T) {
	set, err := Load(Core050Version)
	if err != nil {
		t.Fatal(err)
	}
	lock := set.Lock()
	const tag, pin = "v0.5.0", "171d4c21d4b13e03ee20e1adb6fc5cc6b182cfae"
	if len(lock.Files) != 3+369+5+2+41 {
		t.Fatalf("Core 0.5.0 lock files: %d", len(lock.Files))
	}
	if lock.Source.Repository != "https://github.com/Judgment-Pack/judgment-pack-spec" || lock.Source.Ref != tag || lock.Source.BaseCommit != pin || lock.Source.Kind != "immutable-git-ref" || lock.Source.WorktreeDirty || lock.Source.Stage != "" {
		t.Fatalf("unexpected source: %+v", lock.Source)
	}
	schemas := map[string]string{
		"schema.json":                     "fb182fd0",
		"manifest.schema.json":            "d835195e",
		"evaluation/manifest.schema.json": "7bc20cd1",
	}
	for name, prefix := range schemas {
		entry, ok := set.File(name)
		if !ok || !strings.HasPrefix(entry.SHA256, prefix) {
			t.Fatalf("%s: %+v, want the release notes' digest %s...", name, entry, prefix)
		}
	}
	if !set.HasEvaluationCorpus() {
		t.Fatal("Core 0.5.0 publishes an evaluation corpus")
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
	advisory := 0
	indexed := map[string]bool{}
	for _, item := range manifest.Cases {
		indexed["cases/"+item.Path] = true
		if strings.HasPrefix(item.Path, "advisory/") {
			advisory++
		}
	}
	if len(manifest.Cases) != 369+5 || advisory != 5 {
		t.Fatalf("document cases: %d, advisory %d", len(manifest.Cases), advisory)
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
	if len(evaluation.Cases) != 103 {
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

// No embedded bundle may state a stage: each is a release, and a lock the
// import tool wrote for a commit's staged directories is refused whatever
// version it names.
func TestNoBundleStatesAStage(t *testing.T) {
	for _, version := range SupportedVersions() {
		set, err := Load(version)
		if err != nil {
			t.Fatal(err)
		}
		if set.Lock().Source.Stage != "" {
			t.Fatalf("%s: stage %q", version, set.Lock().Source.Stage)
		}
	}
	full := strings.Repeat("a", 40)
	if err := checkStage(LockSource{Kind: "immutable-git-ref", Ref: "v0.5.0", BaseCommit: full}); err != nil {
		t.Fatalf("a released lock was refused: %v", err)
	}
	for _, stage := range []string{"staged-unreleased", "released", "x"} {
		if err := checkStage(LockSource{Kind: "immutable-git-ref", Ref: full, BaseCommit: full, Stage: stage}); err == nil {
			t.Errorf("a lock stating stage %q was accepted", stage)
		}
	}

	// The lock Load reads goes through the same check: each bundled lock,
	// given a stage, is refused, with the message Load reports, and as it is
	// it decodes to the lock Load returns.
	for _, version := range SupportedVersions() {
		lockBytes, err := embedded.ReadFile(path.Join("jps", version, "lock.json"))
		if err != nil {
			t.Fatal(err)
		}
		set, err := Load(version)
		if err != nil {
			t.Fatal(err)
		}
		if lock, err := decodeLock(lockBytes, version); err != nil || !reflect.DeepEqual(lock, set.Lock()) {
			t.Fatalf("%s: the bundled lock decodes to %+v (%v)", version, lock, err)
		}
		var raw map[string]any
		if err := json.Unmarshal(lockBytes, &raw); err != nil {
			t.Fatal(err)
		}
		raw["source"].(map[string]any)["stage"] = "staged-unreleased"
		staged, err := json.Marshal(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeLock(staged, version); err == nil || err.Error() != "embedded artifact lock states a stage; this runtime embeds released versions only" {
			t.Errorf("%s: a lock stating a stage decoded (%v)", version, err)
		}
	}
}

// 0.4.0-draft, the working selector 0.5.0 was staged under, and 0.4.0, which
// no release assigns, are not bundled.
func TestRetiredSelectorsAreNotBundled(t *testing.T) {
	for _, version := range []string{"0.4.0-draft", "0.4.0"} {
		var unsupported *UnsupportedVersionError
		if _, err := Load(version); !errors.As(err, &unsupported) {
			t.Fatalf("Load(%q) = %v, want an unsupported version", version, err)
		}
	}
}
