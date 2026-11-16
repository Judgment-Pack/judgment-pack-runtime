package registry

import (
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
)

// An overlay carries a value from shallow in the registry to deep in the
// resolved document. Counted as admission counts (the root object is level
// 1): a set value's own containers start at level 7 in the registry (root,
// overlays, an overlay, patches, a patch, set), so one of 26 levels fits
// there; /a/a/a/a ends at level 7 of the entry's value (root, entries, an
// entry, value, then three objects), so the value set there starts at level 8
// in the resolved document and ends at 7 + levels.
const overlayDepthKind = `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/deep","version":"1.0.0","kind":"deep","owner":"platform",
"schema":{"type":"object"},"overlayable":["/a/a/a/a"]}`

func overlayDepthRegistry(t *testing.T, levels int) string {
	t.Helper()
	return `{"registryFormatVersion":"1.0.0","id":"https://example.invalid/registries/deep","version":"1.0.0",
"environments":["prod"],"owners":["platform"],
"kinds":[{"kind":"deep","id":"https://example.invalid/kinds/deep","version":"1.0.0","digest":"` + digestOf(t, overlayDepthKind) + `"}],
"entries":[{"kind":"deep","id":"nested","owner":"platform","value":{"a":{"a":{"a":{"a":0}}}}}],
"overlays":[{"environment":"prod","patches":[{"kind":"deep","id":"nested","set":{"/a/a/a/a":` +
		strings.Repeat("[", levels) + strings.Repeat("]", levels) + `}}]}]}`
}

// levelsOf is a document's depth as admission counts it, found as the
// smallest carrier depth limit that admits it.
func levelsOf(t *testing.T, document []byte) int {
	t.Helper()
	for depth := 1; depth <= carrier.DefaultMaxDepth; depth++ {
		if _, failure := carrier.Decode(document, carrier.Limits{MaxDepth: depth, MaxNodes: len(document), MaxStringBytes: len(document)}); failure == nil {
			return depth
		}
	}
	t.Fatal("the document nests past the carrier's default depth")
	return 0
}

func TestAResolvedDocumentAtTheDepthBoundResolves(t *testing.T) {
	registry := overlayDepthRegistry(t, 25)
	if got := levelsOf(t, []byte(registry)); got != 31 {
		t.Fatalf("the registry nests %d deep, want 31", got)
	}
	result := run(registry, []string{overlayDepthKind})
	if !result.Report.Valid {
		t.Fatalf("want valid, got %+v", result.Report.Diagnostics)
	}
	resolved, err := result.Resolve("prod")
	if err != nil {
		t.Fatal(err)
	}
	if got := levelsOf(t, resolved); got != MaxDepth {
		t.Fatalf("the resolved document nests %d deep, want %d", got, MaxDepth)
	}
}

func TestAResolvedDocumentPastTheDepthBoundIsAFinding(t *testing.T) {
	// The registry itself is admitted, its overlay value 26 levels deep at
	// levels 7 to 32; resolved, that value ends at level 33.
	registry := overlayDepthRegistry(t, 26)
	if got := levelsOf(t, []byte(registry)); got != MaxDepth {
		t.Fatalf("the registry nests %d deep, want %d", got, MaxDepth)
	}
	result := run(registry, []string{overlayDepthKind})
	if result.Report.Valid || len(result.Report.Diagnostics) != 1 {
		t.Fatalf("want one finding, got %+v", result.Report.Diagnostics)
	}
	got := result.Report.Diagnostics[0]
	if got.Code != "REG-DOC-DEPTH" || got.Location != (Location{Document: DocumentRegistry, Environment: "prod", Pointer: ""}) {
		t.Fatalf("want REG-DOC-DEPTH at the prod resolved document, got %+v", got)
	}
	if _, err := result.Resolve("prod"); err == nil {
		t.Fatal("a registry that resolves past the depth bound resolved")
	}
}
