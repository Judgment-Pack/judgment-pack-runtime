package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
	// The count is written in more places than the list is, and two of them
	// still read eight after the ninth surface was named. Each sentence is held
	// to the number enumerated above rather than to a number written here, and
	// the sentence that carries the list is held to the list by name: each code
	// span in it names one enumerated surface, and each surface is named once.
	count := countWord(t, len(enumerated))
	flat := flatten(text)
	if sentence := "One evaluator sits behind all " + count + ","; !strings.Contains(flat, sentence) {
		t.Fatalf("CONFORMANCE.md does not state %q, and the enumeration names %d surfaces", sentence, len(enumerated))
	}
	claimed := sentenceAfter(t, "CONFORMANCE.md", flat, "the "+count+" surfaces that reach it", len(enumerated))
	heldToList(t, "CONFORMANCE.md", enumerated, codeSpans(t, "CONFORMANCE.md", claimed))

	// README.md spells the surfaces its own way — `jpack packs test`, and
	// `graph test` once the verb group is named — which surfaceSpelled admits.
	readme := flatten(readDocument(t, "README.md"))
	listed := sentenceAfter(t, "README.md", readme, "and "+count+" surfaces reach it:", len(enumerated))
	heldToList(t, "README.md", enumerated, codeSpans(t, "README.md", listed))

	// The MCP tools among them are counted and listed twice more, in the client
	// guide and in the mcp command's help, and both still counted three after
	// the fourth was added. The guide's sentence names CLI counterparts beside
	// the tools, so only its tool names are held.
	tools := []string{}
	for _, surface := range enumerated {
		if strings.HasPrefix(surface, "`experimental_") {
			tools = append(tools, surface)
		}
	}
	if sites := expectedSites[filepath.Join("..", "mcp", "tools.go")]; sites != len(tools) {
		t.Fatalf("this test expects %d constructor sites in the MCP package but enumerates %d MCP tools", sites, len(tools))
	}
	guide := flatten(readDocument(t, filepath.Join("docs", "mcp-clients.md")))
	reaching := sentenceAfter(t, "docs/mcp-clients.md", guide, "except the "+countWord(t, len(tools))+" that reach the evaluator:", len(tools))
	named := []string{}
	for _, span := range codeSpans(t, "docs/mcp-clients.md", reaching) {
		if strings.HasPrefix(span, "experimental_") {
			named = append(named, span)
		}
	}
	heldToList(t, "docs/mcp-clients.md", tools, named)

	help := sentenceAfter(t, "jpack mcp --help", mcpHelp(t), "tools on this runtime's experimental surface (", len(tools))
	before, _, stated := strings.Cut(help, ", which reach the evaluator")
	if !stated {
		t.Fatalf("jpack mcp --help no longer says which of its tools reach the evaluator: %q", help)
	}
	heldToList(t, "jpack mcp --help", tools, experimentalName.FindAllString(before, -1))
}

// The mcp command's help counts the tools on the experimental surface and names
// each, and it said six when the server listed nine. The names are read from a
// real tools/list response, so a tool added to the server fails here until the
// help counts and names it.
func TestMCPHelpNamesEveryExperimentalTool(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"mcp"}, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n")
	if code != 0 || stderr != "" {
		t.Fatalf("mcp tools/list: exit=%d stderr=%q", code, stderr)
	}
	var listed struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &listed); err != nil {
		t.Fatalf("undecodable tools/list response %q: %v", stdout, err)
	}
	tools := []string{}
	for _, tool := range listed.Result.Tools {
		if strings.HasPrefix(tool.Name, "experimental_") {
			tools = append(tools, tool.Name)
		}
	}
	if len(tools) == 0 {
		t.Fatal("tools/list names no experimental tool, so nothing would be held")
	}
	opening := "plus " + countWord(t, len(tools)) + " tools on this runtime's experimental surface ("
	heldToList(t, "jpack mcp --help", tools, experimentalName.FindAllString(sentenceAfter(t, "jpack mcp --help", mcpHelp(t), opening, len(tools)), -1))
}

var experimentalName = regexp.MustCompile(`experimental_[a-z_]+`)

func mcpHelp(t *testing.T) string {
	t.Helper()
	code, stdout, stderr := runTest(t, []string{"mcp", "--help"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("mcp --help: exit=%d stderr=%q", code, stderr)
	}
	return flatten(stdout)
}

func readDocument(t *testing.T, name string) string {
	t.Helper()
	document, err := os.ReadFile(filepath.Join("..", "..", name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(document)
}

// flatten folds each run of white space to one space, so a sentence is found
// however its lines are wrapped.
func flatten(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// countWord spells a count the way the documents do.
func countWord(t *testing.T, n int) string {
	t.Helper()
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve"}
	if n < 0 || n >= len(words) {
		t.Fatalf("no spelling for a count of %d; extend countWord", n)
	}
	return words[n]
}

// sentenceAfter returns what follows opening, up to the end of its sentence. A
// missing opening and a sentence with no end are two failures, reported apart:
// the first is a wrong count, the second a list this test can no longer bound.
func sentenceAfter(t *testing.T, document, text, opening string, surfaces int) string {
	t.Helper()
	_, rest, stated := strings.Cut(text, opening)
	if !stated {
		t.Fatalf("%s does not state %q, and %d are enumerated", document, opening, surfaces)
	}
	sentence, _, ended := strings.Cut(rest, ". ")
	if !ended {
		t.Fatalf("%s: the sentence after %q has no end this test can find, so its list cannot be bounded", document, opening)
	}
	return sentence
}

func codeSpans(t *testing.T, document, text string) []string {
	t.Helper()
	parts := strings.Split(text, "`")
	if len(parts)%2 == 0 {
		t.Fatalf("%s leaves a code span open in %q", document, text)
	}
	spans := []string{}
	for i := 1; i < len(parts); i += 2 {
		spans = append(spans, parts[i])
	}
	return spans
}

// surfaceSpelled returns the one surface a spelling names: the surface itself,
// a longer spelling that ends with it (`jpack packs test`), or a shorter one
// that ends it (`graph test`). It returns "" when the spelling names none, or
// more than one.
func surfaceSpelled(surfaces []string, spelling string) string {
	match := ""
	for _, surface := range surfaces {
		name := strings.Trim(surface, "`")
		if spelling == name || strings.HasSuffix(spelling, " "+name) || strings.HasSuffix(name, " "+spelling) {
			if match != "" {
				return ""
			}
			match = surface
		}
	}
	return match
}

// heldToList requires the spellings to name each surface once and nothing
// else. A surface written without a code span is reported as not named: the
// list is code spans in every document that carries it.
func heldToList(t *testing.T, document string, surfaces, spellings []string) {
	t.Helper()
	named := map[string]string{}
	for _, spelling := range spellings {
		surface := surfaceSpelled(surfaces, spelling)
		if surface == "" {
			t.Fatalf("%s lists %q, which names no one surface among the %d enumerated", document, spelling, len(surfaces))
		}
		if earlier, twice := named[surface]; twice {
			t.Fatalf("%s names %s twice, as %q and %q", document, surface, earlier, spelling)
		}
		named[surface] = spelling
	}
	for _, surface := range surfaces {
		if _, found := named[surface]; !found {
			t.Fatalf("%s does not name %s in the sentence that counts %d", document, surface, len(surfaces))
		}
	}
}
