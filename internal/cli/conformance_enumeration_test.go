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
		"`experimental_test_cases`",
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
		filepath.Join("..", "mcp", "tools.go"): 4, // experimental_evaluate, experimental_test_packs, experimental_test_cases, experimental_test_graphs
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
	if !strings.Contains(text, "the nine surfaces that reach it") {
		t.Fatalf("CONFORMANCE.md no longer states the enumeration's count as %q; update the sentence and this test together", "the nine surfaces that reach it")
	}

	// The count is written three times, and two of the three still read eight
	// after the ninth surface was named. Each sentence is held to the number
	// enumerated above, and README.md's own list to the same number: it spells
	// the surfaces its own way, so what is counted there is the code spans
	// between the count and the end of its sentence.
	count := countWord(t, len(enumerated))
	flat := strings.Join(strings.Fields(text), " ")
	for _, sentence := range []string{"the " + count + " surfaces that reach it", "One evaluator sits behind all " + count + ","} {
		if !strings.Contains(flat, sentence) {
			t.Fatalf("CONFORMANCE.md does not state %q, and the enumeration names %d surfaces", sentence, len(enumerated))
		}
	}
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	opening := "and " + count + " surfaces reach it:"
	_, list, stated := strings.Cut(strings.Join(strings.Fields(string(readme)), " "), opening)
	if !stated {
		t.Fatalf("README.md does not state %q, and the enumeration names %d surfaces", opening, len(enumerated))
	}
	list, closed := cutBefore(list, " MCP tools.")
	if spans := strings.Count(list, "`") / 2; !closed || spans != len(enumerated) {
		t.Fatalf("README.md names %d surfaces after %q and the enumeration names %d", spans, opening, len(enumerated))
	}
}

// countWord spells a surface count the way the two documents do.
func countWord(t *testing.T, n int) string {
	t.Helper()
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve"}
	if n < 0 || n >= len(words) {
		t.Fatalf("no spelling for a count of %d surfaces; extend countWord", n)
	}
	return words[n]
}

func cutBefore(text, end string) (string, bool) {
	before, _, found := strings.Cut(text, end)
	return before, found
}
