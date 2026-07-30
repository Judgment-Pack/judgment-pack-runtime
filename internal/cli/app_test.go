package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/describe"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func TestHelpUsesSpecNamespace(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"--help"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "spec") || strings.Contains(stdout, "jps validate") {
		t.Fatalf("unexpected help: %s", stdout)
	}
}

// claimSurface is one inventoried surface. mustReference marks the surfaces that
// speak about this runtime's evaluator and therefore have to say where the claim is
// stated; the forbidden-phrase scans apply to every surface either way, so a new
// surface that starts denying or restating the claim fails wherever it is added.
//
// dated marks a surface that records a decision at a point in time rather than
// describing the runtime as it is now — the ADR that establishes the current
// posture is the only one. Such a record has to be able to quote the language it
// removed, so the denial vocabulary and the present-tense posture phrases are not
// scanned over it; the claim-statement phrases and the claim-element co-occurrence
// check are, because no record may state the claim either.
type claimSurface struct {
	name          string
	text          string
	mustReference bool
	dated         bool
}

// claimSurfaces is the inventory of public surfaces that mention this runtime's
// conformance posture, gathered from the surfaces themselves rather than from a
// list of strings: the CLI help texts, the *actual* MCP tools/list descriptions and
// rendered prompts as a client receives them, the in-band draft-RFC prototype note,
// and the maintained prose of claimProse below. Adding a surface without adding it
// here is the failure mode this inventory exists to make visible, so each entry is
// produced by running the thing or by reading the file that ships.
func claimSurfaces(t *testing.T) []claimSurface {
	t.Helper()
	surfaces := []claimSurface{}

	// Every help text a user can reach that speaks about this runtime's posture.
	// mustReference is per row rather than blanket: a help text that never mentions
	// the evaluator has nothing to point at, and requiring the reference there would
	// push the claim document's name onto surfaces that are not about it. The
	// forbidden-phrase scans below apply to all of them either way, which is what
	// makes adding a row cheap and forgetting to add one the visible failure.
	for _, surface := range []struct {
		args          []string
		mustReference bool
	}{
		{args: []string{"--help"}, mustReference: true},
		{args: []string{"experimental", "--help"}, mustReference: true},
		{args: []string{"experimental", "evaluate", "--help"}, mustReference: true},
		{args: []string{"experimental", "evaluate-corpus", "--help"}, mustReference: true},
		// The graph surface (ADR-0015). The group and its evaluate verb speak
		// about the evaluator, so they say where the claim is stated; the other
		// verbs evaluate nothing and are inventoried for the phrase scans.
		{args: []string{"experimental", "graph", "--help"}, mustReference: true},
		{args: []string{"experimental", "graph", "validate", "--help"}},
		{args: []string{"experimental", "graph", "evaluate", "--help"}, mustReference: true},
		{args: []string{"experimental", "graph", "explain", "--help"}},
		{args: []string{"experimental", "graph", "schema", "--help"}},
		{args: []string{"mcp", "--help"}, mustReference: true},
		// The project convention (ADR-0012). Only the two that speak about the
		// evaluator have to say where the claim is stated.
		{args: []string{"packs", "--help"}, mustReference: true},
		{args: []string{"packs", "test", "--help"}, mustReference: true},
		{args: []string{"packs", "list", "--help"}},
		{args: []string{"packs", "validate", "--help"}},
		{args: []string{"packs", "schema", "--help"}},
	} {
		code, stdout, stderr := runTest(t, surface.args, "")
		if code != 0 || stderr != "" {
			t.Fatalf("%v: exit=%d stderr=%q", surface.args, code, stderr)
		}
		surfaces = append(surfaces, claimSurface{
			name:          "cli " + strings.Join(surface.args, " "),
			text:          stdout,
			mustReference: surface.mustReference,
		})
	}

	// The MCP descriptions a client actually receives, from a real tools/list
	// response over the same stdio transport a client uses. Reading the Go
	// definitions instead would test a value rather than a surface.
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"
	code, stdout, stderr := runTest(t, []string{"mcp"}, input)
	if code != 0 || stderr != "" {
		t.Fatalf("mcp tools/list: exit=%d stderr=%q", code, stderr)
	}
	var listed struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &listed); err != nil {
		t.Fatalf("undecodable tools/list response %q: %v", stdout, err)
	}
	if len(listed.Result.Tools) == 0 {
		t.Fatalf("tools/list returned no tool: %q", stdout)
	}
	evaluationTool := false
	for _, tool := range listed.Result.Tools {
		if tool.Description == "" {
			t.Fatalf("tool %q has no description", tool.Name)
		}
		// Only the evaluation tool speaks about the evaluator, so only it must carry
		// the reference; the other descriptions are inventoried for the phrase scans,
		// which is where a stray denial would otherwise hide.
		surfaces = append(surfaces, claimSurface{
			name:          "mcp tools/list " + tool.Name,
			text:          tool.Description,
			mustReference: tool.Name == "experimental_evaluate",
		})
		if tool.Name == "experimental_evaluate" {
			evaluationTool = true
		}
	}
	if !evaluationTool {
		t.Fatalf("the inventory must include the evaluation tool's own description: %q", stdout)
	}

	// The rendered prompts, from real prompts/list and prompts/get responses over the
	// same transport a client uses. A prompt is text a client's model reads and may
	// repeat, so it is a claim surface exactly as a tool description is, and rendering
	// it is the only way to see what a client receives.
	code, stdout, stderr = runTest(t, []string{"mcp"}, `{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`+"\n")
	if code != 0 || stderr != "" {
		t.Fatalf("mcp prompts/list: exit=%d stderr=%q", code, stderr)
	}
	var prompts struct {
		Result struct {
			Prompts []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"prompts"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &prompts); err != nil {
		t.Fatalf("undecodable prompts/list response %q: %v", stdout, err)
	}
	if len(prompts.Result.Prompts) == 0 {
		t.Fatalf("prompts/list returned no prompt: %q", stdout)
	}
	for _, prompt := range prompts.Result.Prompts {
		surfaces = append(surfaces, claimSurface{
			name: "mcp prompts/list " + prompt.Name,
			text: prompt.Description,
		})
		code, stdout, stderr = runTest(t, []string{"mcp"},
			`{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"`+prompt.Name+`"}}`+"\n")
		if code != 0 || stderr != "" {
			t.Fatalf("mcp prompts/get %s: exit=%d stderr=%q", prompt.Name, code, stderr)
		}
		var rendered struct {
			Result struct {
				Messages []struct {
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"messages"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &rendered); err != nil {
			t.Fatalf("undecodable prompts/get response %q: %v", stdout, err)
		}
		if len(rendered.Result.Messages) == 0 || rendered.Result.Messages[0].Content.Text == "" {
			t.Fatalf("prompt %q rendered no text: %q", prompt.Name, stdout)
		}
		text := rendered.Result.Messages[0].Content.Text
		surfaces = append(surfaces, claimSurface{
			name: "mcp prompts/get " + prompt.Name,
			text: text,
			// Only the prompts that speak about the evaluator say where its
			// claim is stated: the one that drives it and the one that
			// explains its payloads.
			mustReference: prompt.Name == "test_pack" || prompt.Name == "explain_disposition",
		})
	}

	// The in-band prototype note, read off a real payload: it is the surface that
	// carried a direct contradiction of the same payload's claim reference.
	pack := filepath.Join("..", "evaluation", "testdata", "rfc0008", "airline-cancellation-quantifier.json")
	facts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(facts, []byte(`{"reservation":{"segments":[{"cancelledByAirline":false},{"cancelledByAirline":true}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0008-quantifiers", "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("prototype evaluation: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var prototype struct {
		DraftPrototype *struct {
			Note string `json:"note"`
		} `json:"draftPrototype"`
	}
	if err := json.Unmarshal([]byte(stdout), &prototype); err != nil {
		t.Fatal(err)
	}
	if prototype.DraftPrototype == nil || prototype.DraftPrototype.Note == "" {
		t.Fatalf("a draft-grammar evaluation must carry its in-band note: %q", stdout)
	}
	surfaces = append(surfaces, claimSurface{
		name:          "payload draftPrototype.note",
		text:          prototype.DraftPrototype.Note,
		mustReference: true,
	})

	return append(surfaces, claimProse(t)...)
}

// historicalRecords are the two files the scans never read, named one by one rather
// than matched by a pattern: ADR-0007 and ADR-0010.
//
// Each is a dated decision record whose no-claim language was the accurate
// description of the posture it recorded — 0007 shipped the evaluator with no §10
// evaluation limits, 0010 aligned it and deliberately claimed nothing — and ADR-0011
// supersedes those two determinations without superseding either record, so neither
// file is edited and neither is rewritten to match a later posture. Their dated
// denials therefore stay as written, and only these two are exempt: every other
// maintained document is scanned, including the record that establishes the current
// posture.
var historicalRecords = map[string]bool{
	"0007-experimental-evaluator.md":                true,
	"0010-evaluator-aligned-to-core-0.2.0-draft.md": true,
}

// claimProse is the half of the inventory that ships as prose rather than as
// output: the root README and CHANGELOG, every document under docs/, and the ADR
// that establishes the current claim posture. The reviewer who found CHANGELOG.md
// and ADR-0011 restating the claim found them because the inventory stopped at the
// runtime's own output; it no longer does.
//
// The CHANGELOG is scanned as its Unreleased section only. Released sections are
// dated entries about shipped versions — several of them accurately recorded, at the
// time, that nothing was claimed — and rewriting release history to match a later
// posture would be a worse defect than the one this test guards. An ADR is
// inventoried when it names the claim document, which is what makes a record speak
// about the current posture rather than about the state it was written in; the two
// records in historicalRecords are exempt regardless.
func claimProse(t *testing.T) []claimSurface {
	t.Helper()
	root := filepath.Join("..", "..")
	paths := []string{filepath.Join(root, "README.md"), filepath.Join(root, "CHANGELOG.md")}
	for _, pattern := range []string{
		filepath.Join(root, "docs", "*.md"),
		filepath.Join(root, "docs", "adr", "*.md"),
	} {
		matched, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		if len(matched) == 0 {
			t.Fatalf("no document matched %q, so the walk is scanning nothing", pattern)
		}
		paths = append(paths, matched...)
	}
	// The exemption must name files that exist, or it is a stale list that silently
	// exempts nothing while looking like it exempts something.
	for name := range historicalRecords {
		if _, err := os.Stat(filepath.Join(root, "docs", "adr", name)); err != nil {
			t.Fatalf("historicalRecords names a file that is not there: %v", err)
		}
	}

	// The documents that speak about the evaluator have to say where the claim is
	// stated, keyed by repository path so the two README.md files stay distinct.
	mustReference := map[string]bool{
		"README.md":            true,
		"CHANGELOG.md":         true,
		"docs/architecture.md": true,
		"docs/adr/0011-first-evaluator-conformance-claim.md": true,
	}
	surfaces := []claimSurface{}
	for _, path := range paths {
		base := filepath.Base(path)
		if historicalRecords[base] {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		rel := strings.TrimPrefix(filepath.ToSlash(strings.TrimPrefix(path, root)), "/")
		record := strings.HasPrefix(rel, "docs/adr/") && base != "README.md"
		if record && !strings.Contains(text, result.EvaluationClaimReference) {
			continue
		}
		if rel == "CHANGELOG.md" {
			text = unreleasedSection(t, text)
		}
		surfaces = append(surfaces, claimSurface{
			name:          "prose " + rel,
			text:          text,
			mustReference: mustReference[rel],
			dated:         record,
		})
	}
	return surfaces
}

// unreleasedSection is the CHANGELOG's maintained section: Unreleased when one
// exists, else the topmost version section. On a release commit "Unreleased"
// has just been renamed to the new version, but that text is still this
// change's live prose until the tag exists, so it is scanned rather than
// exempted — the guard must not go blind at exactly the moment a release PR
// touches the most words.
func unreleasedSection(t *testing.T, changelog string) string {
	t.Helper()
	start := strings.Index(changelog, "## Unreleased")
	if start < 0 {
		start = strings.Index(changelog, "\n## ")
		if start < 0 {
			t.Fatal("the CHANGELOG has no section to scan")
		}
		start++
	}
	rest := changelog[start:]
	if end := strings.Index(rest[3:], "\n## "); end >= 0 {
		return rest[:end+3]
	}
	return rest
}

// statements splits one surface into the units a claim could be made in: a
// paragraph, a list item, a table row, or a heading.
//
// The claim-element check below runs per statement rather than per surface, because
// a document that names the class in one place and a corpus result in another has
// not stated anything, while a document that puts them in one sentence has — and a
// whole-file check would flag every document that discusses the evaluator at all,
// which is how a guard stops being read.
func statements(text string) []string {
	var out []string
	var current []string
	flush := func() {
		if len(current) > 0 {
			out = append(out, strings.Join(current, "\n"))
			current = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		switch {
		case strings.TrimSpace(line) == "":
			flush()
		case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "),
			strings.HasPrefix(trimmed, "#"), strings.HasPrefix(trimmed, "|"):
			flush()
			current = append(current, line)
		default:
			current = append(current, line)
		}
	}
	flush()
	return out
}

// The elements §3.4.1's form is made of, case-folded: the class, the one exact
// specVersion, and the corpus version or the results obtained. Any statement outside
// CONFORMANCE.md that holds one of each has made the claim, with or without a
// claiming verb — the defect the phrase lists missed twice.
var (
	claimClassElements    = []string{"evaluator conformance", "evaluator-conformance"}
	claimVersionElements  = []string{strings.ToLower(result.EvaluatorSpecVersion)}
	claimEvidenceElements = []string{
		"suiteversion", "suite version", "corpus version passed",
		"20/20", "20 rows", "twenty rows", "twenty-row", "all twenty",
		"rows passed", "every row of", "rows pass",
	}
)

// containsAny reports whether folded text holds any of these case-folded needles.
func containsAny(folded string, needles []string) (string, bool) {
	for _, needle := range needles {
		if strings.Contains(folded, needle) {
			return needle, true
		}
	}
	return "", false
}

// Denials, case-folded and in the variants that actually occurred: the
// case-sensitive scan this replaces missed "Nothing here claims conformance of any
// kind" in the prototype note.
var claimDenials = []string{
	"claims no conformance",
	"claim no conformance",
	"no conformance claim",
	"nothing here claims",
	"nothing claims",
	"claims nothing",
	"claims none",
	"makes none",
	"non-conformance-claiming",
	"conformanceclaim\": \"none\"",
	"claims conformance of any kind",
}

// Present-tense posture: a live surface saying this runtime claims something is
// making the claim outside the one file that may.
var claimPosture = []string{
	"this runtime claims",
	"the runtime claims",
	"runtime claims evaluator conformance",
	"claims conformance to it",
	"evaluator-conformance:",
}

// Claim statements, in the semantic variants a list of claiming verbs misses. These
// are scanned over every surface, dated records included, because a sentence that
// says where the claim was made, what it excludes, or what evidence is its own has
// stated part of the claim as surely as one that asserts it: "made against" names
// the version scope, "outside that claim's scope" states an exclusion, and "that
// claim's required evidence" states the evidence relation. All three occurred — in
// the payload note, in README.md, and in docs/architecture.md — and each is now
// written as a contract fact about the class instead.
var claimStatements = []string{
	"claims evaluator conformance",
	"made against",
	"outside that claim's scope",
	"outside the claim's scope",
	"that claim's required",
	"that claim's scope",
}

// claimDefect names the reference-only rule one surface breaks, or returns "" when
// it breaks none. It is the whole vocabulary in one place so the inventory test and
// the reach test below cannot drift apart.
func claimDefect(surface claimSurface) string {
	folded := strings.ToLower(surface.text)
	if !surface.dated {
		if denial, found := containsAny(folded, claimDenials); found {
			return "no live surface may deny the claim (" + denial + ")"
		}
		if phrase, found := containsAny(folded, claimPosture); found {
			return "no live surface outside CONFORMANCE.md may state the claim (" + phrase + ")"
		}
	}
	if phrase, found := containsAny(folded, claimStatements); found {
		return "no surface outside CONFORMANCE.md may state part of the claim (" + phrase + ")"
	}
	// Claim-element co-occurrence: §3.4.1's form is the class, the exact specVersion,
	// the corpus version, and the results, so a statement that assembles them has made
	// the claim whatever verb it used. CHANGELOG.md and ADR-0011 both did exactly that
	// while passing every phrase scan above.
	for _, statement := range statements(surface.text) {
		folded := strings.ToLower(statement)
		class, hasClass := containsAny(folded, claimClassElements)
		version, hasVersion := containsAny(folded, claimVersionElements)
		evidence, hasEvidence := containsAny(folded, claimEvidenceElements)
		if hasClass && hasVersion && hasEvidence {
			return "this statement assembles §3.4.1's form (" + class + " + " + version + " + " + evidence + "), which only CONFORMANCE.md may"
		}
	}
	return ""
}

// The scans have to fire on the text that got past their predecessor, or the
// vocabulary grew without gaining reach. Each row below is a sentence this
// repository actually shipped, quoted from the review that found it: the CHANGELOG
// entry that assembled the whole §3.4.1 form under a heading no verb list matched,
// the ADR bullet that enumerated the same elements, the ADR bullet that restated the
// result, the payload note that named where the claim was made, and the two prose
// lines that called corpus results the claim's own required evidence.
//
// The rows are quoted in order to be rejected, and the assertion is that each one is.
// This is the only place outside CONFORMANCE.md where §3.4.1's assembled form appears
// at all, and it appears as the fixture of a test that fails if the scans ever stop
// reaching it.
func TestTheClaimScansReachWhatGotPastThem(t *testing.T) {
	shipped := map[string]claimSurface{
		"CHANGELOG entry": {text: "- **Claim evaluator conformance against JPS Core `0.2.0-draft`** (ADR-0011), in the one form §3.4.1\n" +
			"  permits and in one place only: the new root CONFORMANCE.md, which names the class,\n" +
			"  the exact `specVersion`, the corpus `suiteVersion` `0.2.0-draft`, the results obtained — **every one of\n" +
			"  its twenty rows passed**"},
		"ADR claim bullet": {dated: true, text: "- **The claim, in §3.4.1's form and nowhere else:** CONFORMANCE.md, at the\n" +
			"  repository root, naming the class, the exact `specVersion` `0.2.0-draft`, the corpus `suiteVersion`\n" +
			"  `0.2.0-draft`, the results obtained, and in its own words that every row of that corpus version\n" +
			"  passed. This is the evaluator conformance class."},
		"ADR evidence bullet": {dated: true, text: "- **The evidence, complete:** the bundled twenty-row corpus of `suiteVersion` `0.2.0-draft` passes\n" +
			"  20/20, and this is the evaluator conformance class it is evidence for."},
		"payload note": {text: "The evaluator-conformance claim in CONFORMANCE.md, made against JPS Core 0.2.0-draft, does not cover these operators."},
		"README line":  {text: "runs the bundled evaluation corpus and reports its rows: that claim's required, non-exhaustive evidence."},
		"architecture line": {text: "reports row results, which §3.4.1 makes that claim's required and non-exhaustive evidence rather than\n" +
			"the claim."},
	}
	for name, surface := range shipped {
		t.Run(name, func(t *testing.T) {
			if defect := claimDefect(surface); defect == "" {
				t.Fatalf("the scans no longer reach the text that got past them: %q", surface.text)
			}
		})
	}
}

// Every public surface in the inventory is reference-only: it says where the claim
// is stated and states no part of it, and none of them denies a claim either.
//
// Both directions are failures. The blanket "claims no conformance" wording was
// accurate only while the §10 requirement was unmet (ADR-0010) and after ADR-0011
// would be false; a surface that restates the claim is the same defect inverted,
// because §3.4.1 fixes the entire form a claim may take and a surface naming the
// class and the version while omitting the corpus version, the results, and the
// every-row statement makes the partial claim that section forbids. So the only
// permitted posture outside CONFORMANCE.md is a reference to it.
func TestEveryClaimSurfaceIsReferenceOnly(t *testing.T) {
	inventory := claimSurfaces(t)
	if len(inventory) < 30 {
		t.Fatalf("the inventory is thinner than the surfaces that exist: %d entries", len(inventory))
	}
	// The surfaces added with the project convention are in the inventory by name.
	// A count floor alone would be satisfied by any thirty surfaces; naming these
	// is what fails when a new user-facing text is added and not inventoried. The
	// MCP rows arrive from a live tools/list response, so their presence here also
	// proves the walk reaches a tool added after this test was written.
	named := map[string]bool{}
	for _, surface := range inventory {
		named[surface.name] = true
	}
	for _, required := range []string{
		"cli packs --help",
		"cli packs list --help",
		"cli packs validate --help",
		"cli packs test --help",
		"cli packs schema --help",
		"mcp tools/list list_packs",
		"mcp tools/list get_pack",
		"prose docs/building-with-packs.md",
	} {
		if !named[required] {
			t.Fatalf("the inventory must include %q; it has %v", required, named)
		}
	}
	for _, surface := range inventory {
		t.Run(surface.name, func(t *testing.T) {
			if surface.mustReference && !strings.Contains(surface.text, "CONFORMANCE.md") {
				t.Fatalf("the surface must say where the claim is stated: %q", first(surface.text, 400))
			}
			if defect := claimDefect(surface); defect != "" {
				t.Fatalf("%s: %q", defect, first(surface.text, 400))
			}
		})
	}

	// The command names are unchanged: the namespace is stability, not conformance.
	_, stdout, _ := runTest(t, []string{"experimental", "--help"}, "")
	for _, verb := range []string{"evaluate", "evaluate-corpus", "graph"} {
		if !strings.Contains(stdout, verb) {
			t.Fatalf("%q must still be spelled the same: %q", verb, stdout)
		}
	}
}

// The claim document is the one place exempt from the rule above, and it must in
// fact make the claim: the inventory test would otherwise pass on a repository that
// references a file stating nothing. The document's own §3.4.1 form is asserted in
// internal/result; this row is the exemption's other half.
func TestTheClaimDocumentIsTheOnePlaceThatStatesTheClaim(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "..", result.EvaluationClaimReference))
	if err != nil {
		t.Fatalf("every surface references this file, so it must exist: %v", err)
	}
	text := strings.Join(strings.Fields(string(document)), " ")
	if !strings.Contains(text, "claims **evaluator conformance**") {
		t.Fatalf("%s must state the claim it is referenced for", result.EvaluationClaimReference)
	}
	for _, denial := range []string{"claims no conformance", "no conformance claim"} {
		if strings.Contains(strings.ToLower(text), denial) {
			t.Fatalf("%s must not deny the claim it makes (%q)", result.EvaluationClaimReference, denial)
		}
	}
}

func TestValidateJSONContractAndExitCodes(t *testing.T) {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	validPath := writeFixture(t, valid)
	code, stdout, stderr := runTest(t, []string{"spec", "validate", validPath, "--format", "json"}, "")
	if code != 0 || stderr != "" || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var validResult map[string]any
	if err := json.Unmarshal([]byte(stdout), &validResult); err != nil {
		t.Fatal(err)
	}
	if validResult["command"] != "spec validate" || validResult["status"] != "valid" {
		t.Fatalf("unexpected result: %#v", validResult)
	}

	invalid, err := set.Case("structural/missing-decision.json")
	if err != nil {
		t.Fatal(err)
	}
	invalidPath := writeFixture(t, invalid)
	code, stdout, stderr = runTest(t, []string{"spec", "validate", invalidPath, "--format", "json"}, "")
	if code != 1 || stderr != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var invalidResult map[string]any
	if err := json.Unmarshal([]byte(stdout), &invalidResult); err != nil {
		t.Fatal(err)
	}
	if invalidResult["status"] != "invalid" {
		t.Fatalf("unexpected result: %#v", invalidResult)
	}
}

func TestValidateUnknownVersionIsUnsupported(t *testing.T) {
	document := `{"specVersion":"9.9.9"}`
	code, stdout, stderr := runTest(t, []string{"spec", "validate", "-", "--format", "json"}, document)
	if code != 2 || stderr != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output["status"] != "unsupported" || output["command"] != "spec validate" {
		t.Fatalf("unexpected result: %#v", output)
	}
}

func TestConformanceAndSchemaCommands(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"spec", "test-conformance", "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var suite map[string]any
	if err := json.Unmarshal([]byte(stdout), &suite); err != nil {
		t.Fatal(err)
	}
	if suite["command"] != "spec test-conformance" || suite["status"] != "valid" {
		t.Fatalf("unexpected suite result: %#v", suite)
	}

	code, stdout, stderr = runTest(t, []string{"spec", "schema", artifacts.DraftVersion, "--write", "-"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"$schema"`) {
		t.Fatalf("exit=%d stderr=%q output prefix=%q", code, stderr, first(stdout, 80))
	}
}

// spec examples lists the embedded valid fixtures and prints one by name; the
// bytes it writes must round-trip through validate, since they are the exact
// conformant fixture. An unknown name is an unsupported result, mirroring
// spec schema for a version this CLI does not bundle.
func TestExamplesCommandListsPrintsAndValidates(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"spec", "examples", "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var catalog map[string]any
	if err := json.Unmarshal([]byte(stdout), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog["kind"] != "version-pinned-conformance-fixture" {
		t.Fatalf("catalog must label its kind as a fixture: %#v", catalog["kind"])
	}
	if len(catalog["examples"].([]any)) == 0 {
		t.Fatal("the example catalog must not be empty")
	}

	// Print one example to stdout and pipe it back through validate.
	code, document, stderr := runTest(t, []string{"spec", "examples", "minimal-literal", "--write", "-"}, "")
	if code != 0 || stderr != "" || !strings.Contains(document, `"specVersion"`) {
		t.Fatalf("exit=%d stderr=%q output prefix=%q", code, stderr, first(document, 80))
	}
	code, _, stderr = runTest(t, []string{"spec", "validate", "-", "--quiet"}, document)
	if code != 0 || stderr != "" {
		t.Fatalf("a printed example must be valid: exit=%d stderr=%q", code, stderr)
	}

	// An unknown name is unsupported (exit 2), not an internal error.
	code, stdout, stderr = runTest(t, []string{"spec", "examples", "no-such-example", "--format", "json"}, "")
	if code != 2 || stderr != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output["status"] != "unsupported" {
		t.Fatalf("unknown example status = %v, want unsupported", output["status"])
	}

	// No drift: the CLI single-example JSON must be the exact payload the shared
	// describe seam produces, so the CLI cannot diverge from the MCP surface
	// (which is pinned to the same seam in the mcp package's tests).
	code, single, stderr := runTest(t, []string{"spec", "examples", "minimal-literal", "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := describe.Example(set, "minimal-literal", "spec examples")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(single), &got); err != nil {
		t.Fatal(err)
	}
	if want := jsonRoundTrip(t, meta); !reflect.DeepEqual(got, want) {
		t.Fatalf("CLI JSON drifted from describe.Example:\n got=%v\nwant=%v", got, want)
	}
}

// spec examples --write to a file must write the exact fixture bytes and refuse
// to overwrite an existing file (O_EXCL), and the two write-specific invocation
// guards must reject their misuse — none of which the stdout path exercises.
func TestExamplesWriteToFileAndInvocationGuards(t *testing.T) {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	want, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "seed.json")

	// Write to a new file: exit 0, bytes identical to the embedded fixture.
	code, stdout, stderr := runTest(t, []string{"spec", "examples", "minimal-literal", "--write", target}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "written:") {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, want) {
		t.Fatal("written bytes differ from the embedded fixture")
	}

	// Writing again to the same path is refused, and the existing file is intact.
	code, _, _ = runTest(t, []string{"spec", "examples", "minimal-literal", "--write", target}, "")
	if code != result.ExitIO {
		t.Fatalf("re-write to an existing path must fail with the IO exit class, got exit=%d", code)
	}
	if after, err := os.ReadFile(target); err != nil || !bytes.Equal(after, want) {
		t.Fatal("a refused overwrite must leave the existing file unchanged")
	}

	// --write with no example name is an invocation error.
	code, stdout, _ = runTest(t, []string{"spec", "examples", "--write", filepath.Join(dir, "x.json"), "--format", "json"}, "")
	if code != result.ExitInvocation {
		t.Fatalf("--write without a name must be an invocation error, got exit=%d", code)
	}
	assertDiagnosticCode(t, stdout, "JPS-INVOCATION-OUTPUT")

	// --write - cannot be combined with --format json.
	code, stdout, _ = runTest(t, []string{"spec", "examples", "minimal-literal", "--write", "-", "--format", "json"}, "")
	if code != result.ExitInvocation {
		t.Fatalf("--write - with --format json must be an invocation error, got exit=%d", code)
	}
	assertDiagnosticCode(t, stdout, "JPS-INVOCATION-STDOUT")
}

func assertDiagnosticCode(t *testing.T, jsonOutput, wantCode string) {
	t.Helper()
	var env struct {
		Diagnostics []struct {
			Code string `json:"code"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &env); err != nil {
		t.Fatalf("output is not a JSON envelope: %v (%q)", err, jsonOutput)
	}
	if len(env.Diagnostics) == 0 || env.Diagnostics[0].Code != wantCode {
		t.Fatalf("diagnostic code = %+v, want %q", env.Diagnostics, wantCode)
	}
}

// jsonRoundTrip marshals a value and decodes it back into a generic map, so a
// typed payload and a decoded JSON payload can be compared with reflect.DeepEqual.
func jsonRoundTrip(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestJSONOptionErrorIsOneMachineResult(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"spec", "validate", "-", "--format", "json", "--quiet"}, `{}`)
	if code != 3 || stderr != "" || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output["status"] != "error" {
		t.Fatalf("unexpected result: %#v", output)
	}
}

func TestJSONParseAndArityErrorsUseMachineEnvelope(t *testing.T) {
	for name, args := range map[string][]string{
		"missing argument": {"spec", "validate", "--format", "json"},
		"unknown flag":     {"spec", "validate", "--bogus", "--format", "json", "-"},
		"extra argument":   {"spec", "test-conformance", "one", "two", "--format=json"},
	} {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := runTest(t, args, "{}")
			if code != 3 || stderr != "" || strings.Count(stdout, "\n") != 1 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			var output map[string]any
			if err := json.Unmarshal([]byte(stdout), &output); err != nil {
				t.Fatal(err)
			}
			if output["status"] != "error" {
				t.Fatalf("unexpected result: %#v", output)
			}
		})
	}
}

func TestUnsupportedCommandResultsHaveUnsupportedStatus(t *testing.T) {
	for name, args := range map[string][]string{
		"schema":      {"spec", "schema", "9.9.9", "--format", "json"},
		"conformance": {"spec", "test-conformance", "--spec-version", "9.9.9", "--format", "json"},
	} {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := runTest(t, args, "")
			if code != 2 || stderr != "" {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			var output map[string]any
			if err := json.Unmarshal([]byte(stdout), &output); err != nil {
				t.Fatal(err)
			}
			if output["status"] != "unsupported" {
				t.Fatalf("unexpected result: %#v", output)
			}
		})
	}

	code, stdout, stderr := runTest(t, []string{"spec", "schema", "9.9.9"}, "")
	if code != 2 || stderr != "" || !strings.HasPrefix(stdout, "unsupported:") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestConformanceHelpNamesExactDefaultVersion(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"spec", "test-conformance", "--help"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, artifacts.DraftVersion) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestJPSNamespaceDoesNotExist(t *testing.T) {
	code, _, stderr := runTest(t, []string{"jps", "validate", "-"}, `{}`)
	if code != 3 || !strings.Contains(stderr, "unknown command") {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
}

func runTest(t *testing.T, args []string, stdin string) (int, string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(args, strings.NewReader(stdin), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func writeFixture(t *testing.T, data []byte) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "pack.json")
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return target
}

func first(value string, count int) string {
	if len(value) <= count {
		return value
	}
	return value[:count]
}

func TestPrettyFlagIndentsJSONOutput(t *testing.T) {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	validPath := writeFixture(t, valid)
	code, stdout, stderr := runTest(t, []string{"spec", "validate", validPath, "--format", "json", "--pretty"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if strings.Count(stdout, "\n") < 2 {
		t.Fatalf("pretty JSON should span multiple lines: %q", stdout)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output["status"] != "valid" {
		t.Fatalf("unexpected result: %#v", output)
	}
}

func TestInputSizeAndReadErrorsUseDistinctCodes(t *testing.T) {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	validPath := writeFixture(t, valid)

	// A byte limit below the document size reports a dedicated resource code,
	// distinct from a generic unreadable-input failure.
	code, stdout, stderr := runTest(t, []string{"spec", "validate", validPath, "--max-bytes", "10", "--format", "json"}, "")
	if code != 4 || stderr != "" {
		t.Fatalf("oversized: exit=%d stderr=%q", code, stderr)
	}
	if got := diagnosticCode(t, stdout); got != "JPS-RESOURCE-INPUT-BYTE-LIMIT" {
		t.Fatalf("oversized input should report the byte-limit code, got %q", got)
	}

	// A genuinely unreadable input keeps the generic read code.
	missing := filepath.Join(t.TempDir(), "absent.json")
	code, stdout, stderr = runTest(t, []string{"spec", "validate", missing, "--format", "json"}, "")
	if code != 4 || stderr != "" {
		t.Fatalf("missing: exit=%d stderr=%q", code, stderr)
	}
	if got := diagnosticCode(t, stdout); got != "JPS-INPUT-READ" {
		t.Fatalf("missing input should report the generic read code, got %q", got)
	}
}

func diagnosticCode(t *testing.T, jsonOutput string) string {
	t.Helper()
	var output struct {
		Diagnostics []struct {
			Code string `json:"code"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &output); err != nil {
		t.Fatalf("decode: %v (output %q)", err, jsonOutput)
	}
	if len(output.Diagnostics) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %d: %q", len(output.Diagnostics), jsonOutput)
	}
	return output.Diagnostics[0].Code
}

func TestMCPSubcommandRespondsToInitialize(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n"
	code, stdout, stderr := runTest(t, []string{"mcp"}, input)
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &response); err != nil {
		t.Fatalf("undecodable response %q: %v", stdout, err)
	}
	result, ok := response["result"].(map[string]any)
	if !ok || result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("unexpected initialize response: %#v", response)
	}
}

// The experimental evaluate command produces a labeled disposition and exits 0
// for any produced disposition; its invocation guards use the standard codes.
func TestExperimentalEvaluateCommand(t *testing.T) {
	pack := filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json")
	dir := t.TempDir()
	facts := filepath.Join(dir, "facts.json")
	if err := os.WriteFile(facts, []byte(`{"request":{"type":"data-access","completeness":"complete","appropriateness":"hard-fail","embargoedInformationToUnauthorizedRecipients":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence := filepath.Join(dir, "evidence.json")
	if err := os.WriteFile(evidence, []byte(`{"intake-form":"present","sponsor-endorsement":"present"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--evidence", evidence, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	// The payload names the surface's stability and references the claim document;
	// it states no part of the claim itself (§3.4.1), and it names the exact contract
	// version applied.
	if output["experimental"] != true || output["conformanceClaimReference"] != result.EvaluationClaimReference {
		t.Fatalf("evaluation output must name the surface and reference the claim document: %v", output)
	}
	if output["conformanceClaimReference"] != "CONFORMANCE.md" || output["evaluatorSpecVersion"] != "0.2.0-draft" {
		t.Fatalf("the in-band member is a locator, beside the exact contract version: %v", output)
	}
	if _, present := output["conformanceClaim"]; present {
		t.Fatalf("the removed conformanceClaim member must be gone (outputVersion %q): %v", result.OutputVersion, output)
	}
	disposition := output["disposition"].(map[string]any)
	if disposition["kind"] != "outcome" || disposition["outcomeId"] != "decline-redirect" {
		t.Fatalf("disposition = %v", disposition)
	}

	// --facts is required.
	code, stdout, _ = runTest(t, []string{"experimental", "evaluate", pack, "--format", "json"}, "")
	if code != result.ExitInvocation {
		t.Fatalf("missing --facts must be an invocation error, got exit=%d", code)
	}
	assertDiagnosticCode(t, stdout, "JPS-INVOCATION-FACTS")
}

// The draft RFC 0008 grammar is reachable only through the opt-in flag on
// experimental evaluate. spec validate is untouched and still rejects a pack
// using the operators; without the flag the evaluator refuses it for the same
// reason; with the flag it evaluates, and the output says in band that the pack
// is not valid under the published specification.
// A terminal applicability is visible on the human surface as its stage
// alone: no id and no stray separator — the same entry the JSON records, so
// the two surfaces cannot tell different stories about why nothing else ran.
func TestHumanTraceShowsTerminalApplicability(t *testing.T) {
	dir := t.TempDir()
	pack := filepath.Join(dir, "pack.json")
	if err := os.WriteFile(pack, []byte(evaluatorPack(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	facts := filepath.Join(dir, "facts.json")
	if err := os.WriteFile(facts, []byte(`{"request":{"type":"dataset-deletion"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "disposition: not-applicable") {
		t.Fatalf("the fixture's applicability excludes this request type: %q", stdout)
	}
	if !strings.Contains(stdout, "trace: applicability: false") {
		t.Fatalf("the terminal applicability renders as its stage alone: %q", stdout)
	}
	if strings.Contains(stdout, "applicability :") {
		t.Fatalf("the empty id must not leave a stray separator: %q", stdout)
	}
}

func TestExperimentalEvaluateRFC0008QuantifierFlag(t *testing.T) {
	pack := filepath.Join("..", "evaluation", "testdata", "rfc0008", "airline-cancellation-quantifier.json")
	facts := filepath.Join(t.TempDir(), "facts.json")
	document := `{"reservation":{"segments":[{"cancelledByAirline":false},{"cancelledByAirline":true}]}}`
	if err := os.WriteFile(facts, []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}

	// spec validate is untouched: the pack is structurally non-conforming.
	code, stdout, stderr := runTest(t, []string{"spec", "validate", pack, "--format", "json"}, "")
	if code != result.ExitInvalid || stderr != "" {
		t.Fatalf("spec validate must still reject a draft-operator pack: exit=%d stderr=%q", code, stderr)
	}
	assertDiagnosticCode(t, stdout, "JPS-STRUCTURE-CONDITION-SHAPE")

	// Without the flag the evaluator refuses it exactly as it does today.
	code, stdout, _ = runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--format", "json"}, "")
	if code != result.ExitInvalid {
		t.Fatalf("without the flag the pack must be refused, got exit=%d", code)
	}
	assertDiagnosticCode(t, stdout, "JPS-EVALUATION-PACK-NOT-CONFORMANT")

	// With the flag it evaluates, and the marker travels with the result.
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0008-quantifiers", "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var output struct {
		SpecVersion    string `json:"specVersion"`
		DraftPrototype *struct {
			RFC                       string   `json:"rfc"`
			Status                    string   `json:"status"`
			Operators                 []string `json:"operators"`
			PackValidUnderSpecVersion bool     `json:"packValidUnderSpecVersion"`
			Note                      string   `json:"note"`
		} `json:"draftPrototype"`
		Disposition struct {
			Kind      string `json:"kind"`
			OutcomeID string `json:"outcomeId"`
		} `json:"disposition"`
	}
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output.Disposition.Kind != "outcome" || output.Disposition.OutcomeID != "free-cancellation" {
		t.Fatalf("disposition = %+v", output.Disposition)
	}
	if output.DraftPrototype == nil {
		t.Fatal("a draft-grammar evaluation must carry the prototype marker")
	}
	if output.DraftPrototype.RFC != "0008" || output.DraftPrototype.Status != "draft-rfc-prototype" {
		t.Fatalf("marker = %+v", output.DraftPrototype)
	}
	if !reflect.DeepEqual(output.DraftPrototype.Operators, []string{"exists"}) {
		t.Fatalf("marker must name the operators used: %v", output.DraftPrototype.Operators)
	}
	if output.DraftPrototype.PackValidUnderSpecVersion || !strings.Contains(output.DraftPrototype.Note, "NOT valid") {
		t.Fatalf("the marker must deny validity under %s: %+v", output.SpecVersion, output.DraftPrototype)
	}

	// The human surface carries the same warning, above the disposition.
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0008-quantifiers"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	marker := strings.Index(stdout, "DRAFT-RFC PROTOTYPE")
	disposition := strings.Index(stdout, "disposition:")
	if marker < 0 || disposition < 0 || marker > disposition {
		t.Fatalf("the prototype marker must precede the disposition: %q", stdout)
	}
	if !strings.Contains(stdout, "NOT valid under JPS "+artifacts.EvaluatorDraftVersion) {
		t.Fatalf("the human marker must name the specification version it is not valid under: %q", stdout)
	}

	// The flag on a pack that uses no draft operator changes nothing about it,
	// and the human marker says so rather than accusing a valid pack. The JSON
	// marker of the same run reports packValidUnderSpecVersion: true, so the two
	// surfaces must not contradict each other.
	corePack := filepath.Join("..", "evaluation", "testdata", "rfc0008", "airline-cancellation-prepared.json")
	coreFacts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(coreFacts, []byte(`{"reservation":{"anySegmentCancelledByAirline":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", corePack, "--facts", coreFacts, "--rfc0008-quantifiers"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "DRAFT-RFC PROTOTYPE") || strings.Contains(stdout, "NOT valid") {
		t.Fatalf("a pack using no draft operator must not be called invalid: %q", stdout)
	}
	if !strings.Contains(stdout, "uses no draft operator and remains a plain JPS "+artifacts.EvaluatorDraftVersion+" pack") {
		t.Fatalf("the human marker must say the pack is unchanged by the flag: %q", stdout)
	}
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", corePack, "--facts", coreFacts, "--rfc0008-quantifiers", "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	output.DraftPrototype = nil
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output.DraftPrototype == nil || !output.DraftPrototype.PackValidUnderSpecVersion || len(output.DraftPrototype.Operators) != 0 {
		t.Fatalf("the JSON marker must agree with the human one: %+v", output.DraftPrototype)
	}
}

// The experimental corpus verb runs the bundled evaluation corpus and labels the
// run as results: §3.4.1 makes them the required and non-exhaustive evidence for
// the claim in CONFORMANCE.md, so the output points at that claim and never
// restates it as a verdict on the rows it just ran.
func TestExperimentalEvaluateCorpusReportsResultsAndPointsAtTheClaim(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate-corpus"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if !strings.Contains(stdout, result.EvaluationCorpusLabel) || !strings.Contains(stdout, "CONFORMANCE.md") {
		t.Fatalf("the run must be labeled results and name where the claim is: %q", stdout)
	}
	if !strings.Contains(stdout, "passed: 20/20 corpus rows matched their expectation") {
		t.Fatalf("every bundled row must run: %q", stdout)
	}
	// A passing run is evidence, not the claim, and not a verdict about a pack.
	// The claim's own wording lives in one file; nothing here may overstate it.
	for _, forbidden := range []string{"conformant", "claims conformance", "is evaluator conforming", "proves", "authorized"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("corpus output must not overstate the claim (%q): %q", forbidden, stdout)
		}
	}

	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate-corpus", "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var output struct {
		Status                    string `json:"status"`
		Experimental              bool   `json:"experimental"`
		ConformanceClaimReference string `json:"conformanceClaimReference"`
		Label                     string `json:"label"`
		SpecVersion               string `json:"specVersion"`
		SuiteVersion              string `json:"suiteVersion"`
		Summary                   struct {
			Total, Passed, Mismatched int
		} `json:"summary"`
		Cases []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Actual string `json:"actual"`
		} `json:"cases"`
	}
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != "passed" || output.Summary.Total != 20 || output.Summary.Passed != 20 {
		t.Fatalf("summary = %+v status=%q", output.Summary, output.Status)
	}
	if !output.Experimental || output.ConformanceClaimReference != result.EvaluationClaimReference || output.Label != result.EvaluationCorpusLabel {
		t.Fatalf("the payload must reference the claim document and label the run as its evidence: %+v", output)
	}
	if output.SpecVersion != artifacts.EvaluatorDraftVersion || output.SuiteVersion != artifacts.EvaluatorDraftVersion {
		t.Fatalf("the corpus must name its own version: %+v", output)
	}
	// Each row reports the canonical bytes it produced, which is what a harness
	// in another project compares against its own implementation.
	for _, item := range output.Cases {
		if item.Status != "passed" || !strings.HasPrefix(item.Actual, `{"handoff":`) {
			t.Fatalf("row %s = %+v", item.ID, item)
		}
	}

	// A version whose Core defines no evaluator class publishes no corpus.
	code, stdout, _ = runTest(t, []string{"experimental", "evaluate-corpus", "--spec-version", artifacts.DraftVersion, "--format", "json"}, "")
	if code != result.ExitUnsupported {
		t.Fatalf("expected an unsupported result, got exit=%d: %q", code, stdout)
	}
	assertDiagnosticCode(t, stdout, "JPS-CAPABILITY-EVALUATION-CORPUS")
}

// A refused evaluation names its JPS §8.4 class and phase in band and carries no
// disposition at all. The finer JPS-* code stays beside the class as its detail.
func TestExperimentalEvaluateReportsTheEvaluationErrorClass(t *testing.T) {
	pack := filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json")
	dir := t.TempDir()
	facts := filepath.Join(dir, "facts.json")
	if err := os.WriteFile(facts, []byte(`{"request":{"type":"data-access"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence := filepath.Join(dir, "evidence.json")
	if err := os.WriteFile(evidence, []byte(`{"not-a-requirement":"present"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--evidence", evidence, "--format", "json"}, "")
	if code != result.ExitInvocation || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var output struct {
		Status          string `json:"status"`
		EvaluationError *struct {
			Class string `json:"class"`
			Phase string `json:"phase"`
		} `json:"evaluationError"`
		Disposition any `json:"disposition"`
	}
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output.EvaluationError == nil || output.EvaluationError.Class != "malformed-input" || output.EvaluationError.Phase != "preflight" {
		t.Fatalf("the payload must name the §8.4 class and phase: %q", stdout)
	}
	if output.Disposition != nil {
		t.Fatalf("an evaluation error carries no disposition: %q", stdout)
	}
	assertDiagnosticCode(t, stdout, "JPS-EVALUATION-EVIDENCE-KEY")

	code, _, stderr = runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--evidence", evidence}, "")
	if code != result.ExitInvocation {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stderr, "evaluation error: malformed-input (preflight phase; JPS-EVALUATION-EVIDENCE-KEY)") {
		t.Fatalf("human output must name the class: %q", stderr)
	}
}

// The byte limit is a §8.2 preflight condition, not a read failure, so the CLI
// hands an oversized input to the engine instead of refusing it first. §8.4 then
// classes it and orders it: an oversized facts document is malformed-input, and a
// non-conformant pack presented with one is still pack-not-conformant, which the
// old read-time refusal reported as an unclassified failure.
func TestOversizedEvaluationInputIsAClassifiedPreflightError(t *testing.T) {
	pack := filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json")
	dir := t.TempDir()
	oversized := filepath.Join(dir, "facts.json")
	// One byte past the documented hard limit is the whole of what makes it
	// oversized; the content beyond the opening brace never has to be JSON.
	bulk := append([]byte(`{"pad":"`), bytes.Repeat([]byte("x"), int(carrier.HardMaxBytes))...)
	if err := os.WriteFile(oversized, append(bulk, []byte(`"}`)...), 0o644); err != nil {
		t.Fatal(err)
	}
	notConformant := filepath.Join(dir, "pack.json")
	if err := os.WriteFile(notConformant, []byte(`{"specVersion":"0.2.0-draft"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		name      string
		pack      string
		wantClass string
		wantCode  string
		wantExit  int
	}{
		{
			name:      "a conformant pack with an oversized facts document",
			pack:      pack,
			wantClass: "malformed-input",
			wantCode:  "JPS-RESOURCE-INPUT-BYTE-LIMIT",
			wantExit:  result.ExitIO,
		},
		{
			name:      "a non-conformant pack outranks the oversized facts document",
			pack:      notConformant,
			wantClass: "pack-not-conformant",
			wantCode:  "JPS-EVALUATION-PACK-NOT-CONFORMANT",
			wantExit:  result.ExitInvalid,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", testCase.pack, "--facts", oversized, "--format", "json"}, "")
			if code != testCase.wantExit || stderr != "" {
				t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, first(stdout, 400))
			}
			var output struct {
				EvaluationError *struct {
					Class                string `json:"class"`
					Phase                string `json:"phase"`
					EvaluatorSpecVersion string `json:"evaluatorSpecVersion"`
				} `json:"evaluationError"`
				Disposition any `json:"disposition"`
			}
			if err := json.Unmarshal([]byte(stdout), &output); err != nil {
				t.Fatal(err)
			}
			if output.EvaluationError == nil || output.EvaluationError.Class != testCase.wantClass || output.EvaluationError.Phase != "preflight" {
				t.Fatalf("the payload must name the §8.4 class and phase: %q", first(stdout, 400))
			}
			if output.EvaluationError.EvaluatorSpecVersion != artifacts.EvaluatorDraftVersion {
				t.Fatalf("the class came from the %s contract: %q", artifacts.EvaluatorDraftVersion, first(stdout, 400))
			}
			if output.Disposition != nil {
				t.Fatalf("an evaluation error carries no disposition: %q", first(stdout, 400))
			}
			assertDiagnosticCode(t, stdout, testCase.wantCode)
		})
	}
}

// The evaluator contract version is named in band and independently of the pack's
// own, and the two must agree for the pack to be evaluated at all: §11 makes the
// declared value exact, so a pack declaring 0.1.0-draft is refused rather than
// evaluated under the 0.2.0-draft contract, on this surface as in the engine.
func TestEvaluationNamesTheEvaluatorContractVersionAndAdmitsOnlyThatVersion(t *testing.T) {
	facts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(facts, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// The same fixture from each bundle: identical but for the one member §11 says
	// re-declaration edits.
	for _, version := range []string{artifacts.EvaluatorDraftVersion, artifacts.DraftVersion} {
		set, err := artifacts.Load(version)
		if err != nil {
			t.Fatal(err)
		}
		valid, err := set.Case("valid/minimal-literal.json")
		if err != nil {
			t.Fatal(err)
		}
		packPath := writeFixture(t, valid)
		code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", packPath, "--facts", facts, "--format", "json"}, "")

		if version != artifacts.EvaluatorDraftVersion {
			if code != result.ExitInvalid {
				t.Fatalf("a pack declaring %s must be refused: exit=%d stdout=%q", version, code, stdout)
			}
			assertDiagnosticCode(t, stdout, "JPS-EVALUATION-PACK-SPEC-VERSION")
			var refusal struct {
				EvaluationError *struct {
					Class string `json:"class"`
					Phase string `json:"phase"`
				} `json:"evaluationError"`
				Diagnostics []struct {
					Message string `json:"message"`
				} `json:"diagnostics"`
			}
			if err := json.Unmarshal([]byte(stdout), &refusal); err != nil {
				t.Fatal(err)
			}
			if refusal.EvaluationError == nil || refusal.EvaluationError.Class != "pack-not-conformant" || refusal.EvaluationError.Phase != "preflight" {
				t.Fatalf("the refusal must name its §8.4 class in band: %q", stdout)
			}
			// The remedy travels with the refusal: §11's rule, and the one edit.
			for _, required := range []string{"§11", "re-declared", "one edit"} {
				if !strings.Contains(refusal.Diagnostics[0].Message, required) {
					t.Fatalf("the refusal must cite %q: %q", required, refusal.Diagnostics[0].Message)
				}
			}
			continue
		}

		if code != 0 || stderr != "" {
			t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
		}
		var output struct {
			SpecVersion          string `json:"specVersion"`
			EvaluatorSpecVersion string `json:"evaluatorSpecVersion"`
		}
		if err := json.Unmarshal([]byte(stdout), &output); err != nil {
			t.Fatal(err)
		}
		if output.SpecVersion != artifacts.EvaluatorDraftVersion {
			t.Fatalf("specVersion is the pack's own: got %q", output.SpecVersion)
		}
		if output.EvaluatorSpecVersion != artifacts.EvaluatorDraftVersion {
			t.Fatalf("evaluatorSpecVersion is the contract's: got %q", output.EvaluatorSpecVersion)
		}
	}
}

// --format json writes the disposition as the exact RFC 8785 canonical bytes §8.3
// compares. --pretty re-indents the whole payload, and encoding/json re-indents a
// Marshaler's output too, so those exact bytes are not present under it: the
// member order and both sets survive, the whitespace does not. Both halves are
// pinned here because both are documented, and §8.3 requires canonicalization
// where a byte comparison is required — so a comparison recanonicalizes.
func TestPrettyKeepsDispositionMemberOrderButNotItsCanonicalBytes(t *testing.T) {
	pack := filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json")
	dir := t.TempDir()
	facts := filepath.Join(dir, "facts.json")
	if err := os.WriteFile(facts, []byte(`{"request":{"type":"data-access","completeness":"complete","appropriateness":"hard-fail","embargoedInformationToUnauthorizedRecipients":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	code, plain, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var produced struct {
		Disposition result.Disposition `json:"disposition"`
	}
	if err := json.Unmarshal([]byte(plain), &produced); err != nil {
		t.Fatal(err)
	}
	canonical, err := produced.Disposition.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain, string(canonical)) {
		t.Fatalf("the canonical bytes must appear verbatim without --pretty: %s not in %s", canonical, plain)
	}

	code, pretty, stderr := runTest(t, []string{"--pretty", "experimental", "evaluate", pack, "--facts", facts, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if strings.Contains(pretty, string(canonical)) {
		t.Fatal("--pretty re-indents the disposition, so the documented caveat is what holds; if this now passes, the caveat can be dropped")
	}
	var indented struct {
		Disposition result.Disposition `json:"disposition"`
	}
	if err := json.Unmarshal([]byte(pretty), &indented); err != nil {
		t.Fatal(err)
	}
	recanonicalized, err := indented.Disposition.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if string(recanonicalized) != string(canonical) {
		t.Fatalf("recanonicalizing the --pretty payload must recover the same bytes: %s, want %s", recanonicalized, canonical)
	}
}
