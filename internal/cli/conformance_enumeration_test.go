package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CONFORMANCE.md scopes the claim to the surfaces that reach the one shared
// evaluator, and names them. That enumeration went stale once already — it said
// three while six existed — so it is now held mechanically: every surface that
// reaches the evaluator constructs it, each at its own call site, and this test
// counts those sites and requires the document to name exactly that many
// surfaces, each one verbatim. A new surface fails here until CONFORMANCE.md
// names it; a refactor that shares one constructor across surfaces also fails
// here, deliberately — the count stops being the number of surfaces at that
// point, and this test's counting rule is what must be rethought alongside it.
func TestConformanceClaimNamesEverySurfaceReachingTheEvaluator(t *testing.T) {
	enumerated := []string{
		"`experimental evaluate`",
		"`experimental evaluate-corpus`",
		"`packs test`",
		"`experimental graph evaluate`",
		"`experimental graph test`",
		"`experimental_evaluate`",
		"`experimental_test_packs`",
		"`experimental_test_graphs`",
	}

	// Which file constructs the evaluator, and how many times, is part of what
	// is held: a bare total would still read six when one surface is removed
	// and another added in the same change. The scan is textual and flat on
	// purpose — both packages are flat, and a count perturbed by a comment or a
	// moved constructor fails toward a human reading this test, never away.
	expectedSites := map[string]int{
		"app.go":                               2, // experimental evaluate, experimental evaluate-corpus
		"packs.go":                             1, // packs test
		"graph.go":                             2, // experimental graph evaluate, experimental graph test
		filepath.Join("..", "mcp", "tools.go"): 3, // experimental_evaluate, experimental_test_packs, experimental_test_graphs
	}
	total := 0
	for _, count := range expectedSites {
		total += count
	}
	if total != len(enumerated) {
		t.Fatalf("this test expects %d constructor sites but enumerates %d surfaces; its own two lists diverged", total, len(enumerated))
	}
	for _, dir := range []string{".", filepath.Join("..", "mcp")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("reading %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			sites := strings.Count(string(source), "evaluation.NewEngine(")
			if sites != expectedSites[path] {
				t.Fatalf("%s constructs the evaluator %d times and this test expects %d: a surface reaching the evaluator moved, appeared, or vanished, and CONFORMANCE.md's claim-scope sentence must change with it (then this test's two lists)", path, sites, expectedSites[path])
			}
		}
	}

	document, err := os.ReadFile(filepath.Join("..", "..", "CONFORMANCE.md"))
	if err != nil {
		t.Fatalf("reading CONFORMANCE.md: %v", err)
	}
	text := string(document)
	for _, surface := range enumerated {
		if !strings.Contains(text, surface) {
			t.Fatalf("CONFORMANCE.md's claim-scope enumeration does not name %s", surface)
		}
	}
	if !strings.Contains(text, "the eight surfaces that reach it") {
		t.Fatalf("CONFORMANCE.md no longer states the enumeration's count as %q; update the sentence and this test together", "the eight surfaces that reach it")
	}
}
