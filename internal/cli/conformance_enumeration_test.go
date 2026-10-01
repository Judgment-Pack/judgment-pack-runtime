package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
		"`experimental compare`",
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
		"compare.go":                           1, // experimental compare
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

	// Each place that counts the surfaces is held to the number enumerated
	// above, and the list beside the count to the names: between the count and
	// the phrase that closes the list, every entry is one surface, spelled the
	// way that document spells it, and every surface is there once. Two of the
	// counts still read eight after the ninth surface was named. What is held is
	// numbers and names: the prose around them is not read, so a sentence that
	// named a surface in order to exclude it would pass.
	count := countWord(t, len(enumerated))
	conformance := flatten(readDocument(t, "CONFORMANCE.md"))
	if sentence := "One evaluator sits behind all " + count + ","; !strings.Contains(conformance, sentence) {
		t.Fatalf("CONFORMANCE.md does not state %q, and the enumeration names %d surfaces", sentence, len(enumerated))
	}
	claimed := between(t, "CONFORMANCE.md", conformance, "the "+count+" surfaces that reach it —", " MCP tools —")
	if fault := listFault(asWritten(enumerated), codeSpans(t, "CONFORMANCE.md", claimed)); fault != "" {
		t.Fatalf("CONFORMANCE.md's claim-scope enumeration %s", fault)
	}

	tools := []string{}
	for _, surface := range enumerated {
		if strings.HasPrefix(surface, "`experimental_") {
			tools = append(tools, surface)
		}
	}
	if sites := expectedSites[filepath.Join("..", "mcp", "tools.go")]; sites != len(tools) {
		t.Fatalf("this test expects %d constructor sites in the MCP package but enumerates %d MCP tools", sites, len(tools))
	}

	// README.md writes the commands in full and shortens two of them once their
	// group is named. The spellings are written out here rather than derived: a
	// rule loose enough to admit them admits commands that do not exist.
	readme := asWritten(tools)
	for spelling, surface := range map[string]string{
		"jpack experimental evaluate":       "`experimental evaluate`",
		"evaluate-corpus":                   "`experimental evaluate-corpus`",
		"jpack packs test":                  "`packs test`",
		"jpack experimental graph evaluate": "`experimental graph evaluate`",
		"graph test":                        "`experimental graph test`",
		"jpack experimental compare":        "`experimental compare`",
	} {
		readme[spelling] = surface
	}
	if len(readme) != len(enumerated) {
		t.Fatalf("this test spells %d surfaces for README.md and enumerates %d", len(readme), len(enumerated))
	}
	for _, surface := range enumerated {
		spelled := false
		for _, named := range readme {
			spelled = spelled || named == surface
		}
		if !spelled {
			t.Fatalf("this test gives no README.md spelling for %s", surface)
		}
	}
	listed := between(t, "README.md", flatten(readDocument(t, "README.md")), "and "+count+" surfaces reach it:", " MCP tools.")
	if fault := listFault(readme, codeSpans(t, "README.md", listed)); fault != "" {
		t.Fatalf("README.md's list of surfaces %s", fault)
	}

	// The MCP tools among them are counted and listed twice more, in the client
	// guide and in the mcp command's help, and both still counted three after
	// the fourth was added. The guide names `packs test` beside the tools, as
	// the command two of them share an evaluator and comparison with; nothing
	// else may stand in its list.
	guide := flatten(readDocument(t, filepath.Join("docs", "mcp-clients.md")))
	reaching := between(t, "docs/mcp-clients.md", guide, "except the "+countWord(t, len(tools))+" that reach the evaluator:", "(ADR-0036).")
	if fault := listFault(asWritten(tools), without(codeSpans(t, "docs/mcp-clients.md", reaching), "packs test")); fault != "" {
		t.Fatalf("docs/mcp-clients.md's list of tools reaching the evaluator %s", fault)
	}

	group := between(t, "jpack mcp --help", mcpHelp(t), "tools on this runtime's experimental surface (", ", which reach the evaluator")
	if fault := listFault(asWritten(tools), listEntries(group)); fault != "" {
		t.Fatalf("jpack mcp --help's list of tools reaching the evaluator %s", fault)
	}
}

// The mcp command's help counts the tools on the experimental surface and names
// each, and it said six when the server listed nine. The names are read from a
// real tools/list response, so a tool added to the server fails here until the
// help counts and names it. Every identifier in the list is held, whole, so a
// name the server does not list fails whatever it begins with.
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
	list := between(t, "jpack mcp --help", mcpHelp(t), opening, "; ADR-")
	if fault := listFault(asWritten(tools), identifier.FindAllString(list, -1)); fault != "" {
		t.Fatalf("jpack mcp --help's list of experimental tools %s", fault)
	}
}

// The guards above are only as good as what they refuse, so each refusal is
// held here, on lists small enough to read.
func TestListGuardsRefuseWhatTheyShould(t *testing.T) {
	spellings := map[string]string{"jpack packs test": "`packs test`", "experimental_test_cases": "`experimental_test_cases`"}
	for name, c := range map[string]struct {
		entries []string
		fault   string
	}{
		"each surface once":                {[]string{"jpack packs test", "experimental_test_cases"}, ""},
		"order does not matter":            {[]string{"experimental_test_cases", "jpack packs test"}, ""},
		"a surface twice":                  {[]string{"jpack packs test", "jpack packs test"}, "names `packs test` twice"},
		"a surface missing":                {[]string{"jpack packs test"}, "does not name `experimental_test_cases`"},
		"nothing listed":                   {nil, "does not name `experimental_test_cases`, `packs test`"},
		"a spelling this document lacks":   {[]string{"packs test", "experimental_test_cases"}, `lists "packs test", which is not how any of the 2 is spelled here`},
		"a command that only ends like it": {[]string{"jpack nonexistent packs test", "experimental_test_cases"}, `lists "jpack nonexistent packs test", which is not how any of the 2 is spelled here`},
		"a name that only begins like it":  {[]string{"jpack packs test", "experimental_test_cases2"}, `lists "experimental_test_cases2", which is not how any of the 2 is spelled here`},
		"one more than the surfaces":       {[]string{"jpack packs test", "experimental_test_cases", "validate"}, `lists "validate", which is not how any of the 2 is spelled here`},
	} {
		if fault := listFault(spellings, c.entries); fault != c.fault {
			t.Errorf("%s: fault %q, want %q", name, fault, c.fault)
		}
	}

	if got := identifier.FindAllString("experimental_test_cases2, which is read-only, and get_schema (§8.3)", -1); strings.Join(got, " ") != "experimental_test_cases2 get_schema" {
		t.Errorf("identifiers read as %q", got)
	}
	if got := without([]string{"packs test", "validate", "experimental_x", "packs test", "jpack packs test"}, "packs test"); strings.Join(got, "|") != "validate|experimental_x|jpack packs test" {
		t.Errorf("entries left after setting one aside read as %q", got)
	}
	if got := listEntries("a_b, c_d and e_f"); strings.Join(got, "|") != "a_b|c_d|e_f" {
		t.Errorf("list entries read as %q", got)
	}
	if got := flatten("and nine\n  surfaces (i.e. `jpack\npacks test`)"); got != "and nine surfaces (i.e. `jpack packs test`)" {
		t.Errorf("flattened to %q", got)
	}
}

// identifier matches a whole name of more than one part, such as a tool's.
var identifier = regexp.MustCompile(`[A-Za-z0-9]+(?:_[A-Za-z0-9]+)+`)

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

// flatten folds each run of white space to one space, so a phrase is found
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

// between returns the text from the end of opening to the start of closing. Its
// two failures are reported apart: without the opening the count no longer
// matches, and without the closing the list can no longer be bounded, which
// says nothing about what the list holds.
func between(t *testing.T, document, text, opening, closing string) string {
	t.Helper()
	_, rest, stated := strings.Cut(text, opening)
	if !stated {
		t.Fatalf("%s does not state %q: the count there no longer matches", document, opening)
	}
	list, _, closed := strings.Cut(rest, closing)
	if !closed {
		t.Fatalf("%s: the list after %q no longer ends at %q, so this test cannot bound it; give it the new ending", document, opening, closing)
	}
	return list
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

// listEntries splits a plain list, "a, b and c", into its entries.
func listEntries(list string) []string {
	return strings.Split(strings.ReplaceAll(list, " and ", ", "), ", ")
}

// without drops what a document names beside a list, which is no part of it.
// Every other entry stays, whatever it is, for listFault to judge.
func without(entries []string, beside string) []string {
	kept := []string{}
	for _, entry := range entries {
		if entry != beside {
			kept = append(kept, entry)
		}
	}
	return kept
}

// asWritten spells each surface as the enumeration does, without its code span.
func asWritten(surfaces []string) map[string]string {
	spellings := map[string]string{}
	for _, surface := range surfaces {
		spellings[strings.Trim(surface, "`")] = surface
	}
	return spellings
}

// listFault says what is wrong with a list, or "" when each entry is, exactly,
// the spelling of one surface and each surface is there once. A surface written
// without its code span is reported as not named: the lists are code spans in
// every document that carries one.
func listFault(spellings map[string]string, entries []string) string {
	named := map[string]bool{}
	for _, entry := range entries {
		surface, spelled := spellings[entry]
		if !spelled {
			return fmt.Sprintf("lists %q, which is not how any of the %d is spelled here", entry, len(spellings))
		}
		if named[surface] {
			return fmt.Sprintf("names %s twice", surface)
		}
		named[surface] = true
	}
	missing := []string{}
	for _, surface := range spellings {
		if !named[surface] {
			missing = append(missing, surface)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return "does not name " + strings.Join(missing, ", ")
	}
	return ""
}
