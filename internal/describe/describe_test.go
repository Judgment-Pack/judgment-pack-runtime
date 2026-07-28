package describe

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func loadSet(t *testing.T) *artifacts.Set {
	t.Helper()
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestRuntimeReportsBundledProvenance(t *testing.T) {
	set := loadSet(t)
	described := Runtime(set, "version")

	if described.OutputVersion != result.OutputVersion {
		t.Errorf("outputVersion = %q, want %q", described.OutputVersion, result.OutputVersion)
	}
	if described.Command != "version" {
		t.Errorf("command = %q, want %q", described.Command, "version")
	}
	if described.Status != "valid" {
		t.Errorf("status = %q, want %q", described.Status, "valid")
	}
	if described.ArtifactProvenance != set.Lock().Source.Kind {
		t.Errorf("provenance = %q, want the lock's %q", described.ArtifactProvenance, set.Lock().Source.Kind)
	}
	if len(described.SupportedSpecs) == 0 {
		t.Error("supported specification versions must not be empty")
	}
}

// The reported size and digest must describe the exact bundled bytes. A caller
// that re-encoded the schema before describing it would produce a digest that
// no longer identifies the artifact the lock verifies.
func TestSchemaDescribesTheExactBundledBytes(t *testing.T) {
	set := loadSet(t)
	schemaBytes, err := set.Schema()
	if err != nil {
		t.Fatal(err)
	}

	described, err := Schema(set, artifacts.DraftVersion, "spec schema", schemaBytes)
	if err != nil {
		t.Fatal(err)
	}

	if described.Bytes != len(schemaBytes) {
		t.Errorf("bytes = %d, want %d", described.Bytes, len(schemaBytes))
	}
	sum := sha256.Sum256(schemaBytes)
	if want := hex.EncodeToString(sum[:]); described.SHA256 != want {
		t.Errorf("sha256 = %q, want %q", described.SHA256, want)
	}
	if described.SpecVersion != artifacts.DraftVersion {
		t.Errorf("specVersion = %q, want %q", described.SpecVersion, artifacts.DraftVersion)
	}
	if described.SchemaID == "" {
		t.Error("schemaId must be reported from the schema's own $id")
	}
	// WrittenTo describes a caller's action, not the artifact, so this package
	// must never populate it.
	if described.WrittenTo != "" {
		t.Errorf("writtenTo = %q, want empty", described.WrittenTo)
	}
}

func TestSchemaRejectsNonObjectBytes(t *testing.T) {
	set := loadSet(t)
	if _, err := Schema(set, artifacts.DraftVersion, "spec schema", []byte("not a schema")); err == nil {
		t.Fatal("expected an error for bytes that are not a JSON object")
	}
}

func TestExamplesCatalogIsLabeledAndNonEmpty(t *testing.T) {
	set := loadSet(t)
	catalog, err := Examples(set, "spec examples")
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Examples) == 0 {
		t.Fatal("the bundled example catalog must not be empty")
	}
	if catalog.Kind != result.ExampleKind {
		t.Errorf("kind = %q, want the fixture label %q", catalog.Kind, result.ExampleKind)
	}
	if catalog.SpecVersion != set.Lock().SpecVersion {
		t.Errorf("specVersion = %q, want the lock's %q", catalog.SpecVersion, set.Lock().SpecVersion)
	}
	if catalog.Provenance != set.Lock().Source.Kind {
		t.Errorf("provenance = %q, want the lock's %q", catalog.Provenance, set.Lock().Source.Kind)
	}
}

// The reported size and digest must describe the exact bundled bytes the tool
// returns, and those bytes must match what the artifact set holds.
func TestExampleDescribesTheExactBundledBytes(t *testing.T) {
	set := loadSet(t)
	described, data, err := Example(set, "minimal-literal", "spec examples")
	if err != nil {
		t.Fatal(err)
	}
	want, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(want) {
		t.Fatal("Example returned bytes that differ from the embedded fixture")
	}
	if described.Bytes != len(want) {
		t.Errorf("bytes = %d, want %d", described.Bytes, len(want))
	}
	sum := sha256.Sum256(want)
	if wantDigest := hex.EncodeToString(sum[:]); described.SHA256 != wantDigest {
		t.Errorf("sha256 = %q, want %q", described.SHA256, wantDigest)
	}
	if described.Kind != result.ExampleKind {
		t.Errorf("kind = %q, want %q", described.Kind, result.ExampleKind)
	}
	if described.WrittenTo != "" {
		t.Errorf("writtenTo = %q, want empty; it is the caller's action, not the artifact's", described.WrittenTo)
	}
}

func TestExampleRejectsUnknownName(t *testing.T) {
	set := loadSet(t)
	_, _, err := Example(set, "no-such-example", "spec examples")
	var unknown *artifacts.UnknownExampleError
	if !errors.As(err, &unknown) {
		t.Fatalf("expected *artifacts.UnknownExampleError, got %v", err)
	}
}

// The runtime description lists every bundled specification version, and its one
// provenance string describes all of them: a payload that names two versions must
// not report the provenance of one.
func TestRuntimeDescribesEveryBundledVersion(t *testing.T) {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	described := Runtime(set, "version")
	if !reflect.DeepEqual(described.SupportedSpecs, artifacts.SupportedVersions()) {
		t.Fatalf("supportedSpecVersions = %v, want %v", described.SupportedSpecs, artifacts.SupportedVersions())
	}
	if described.ArtifactProvenance != "immutable-git-ref" {
		t.Fatalf("every bundled version is imported from an immutable ref: %q", described.ArtifactProvenance)
	}
	for _, version := range described.SupportedSpecs {
		other, err := artifacts.Load(version)
		if err != nil {
			t.Fatalf("a listed version must load: %s: %v", version, err)
		}
		if Runtime(other, "version").ArtifactProvenance != described.ArtifactProvenance {
			t.Fatalf("provenance must not depend on which set reports it: %s", version)
		}
	}
}

// bundledProvenance reports the *least-trusted* kind among the bundles, which is
// the one a release gate acts on, so the kinds carry an explicit order rather than
// the loop reporting whichever bundle it visited last. Both bundles are imported
// from an immutable ref today, so the ordering is pinned here directly.
func TestProvenanceKindsCarryAnExplicitTrustOrder(t *testing.T) {
	if provenanceRank("immutable-git-ref") >= provenanceRank("unreleased-local-snapshot") {
		t.Fatal("an immutable git ref is the more trusted kind")
	}
	if provenanceRank("unreleased-local-snapshot") >= provenanceRank("a-kind-this-runtime-does-not-mint") {
		t.Fatal("an unrecognized kind must rank below every known one: it is not evidence of a trustworthy bundle")
	}
}
