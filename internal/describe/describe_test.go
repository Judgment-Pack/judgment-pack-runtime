package describe

import (
	"crypto/sha256"
	"encoding/hex"
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
