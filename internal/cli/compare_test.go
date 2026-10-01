package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// expensePack is a small pack whose limit and escalation target the tests
// move between versions.
func expensePack(limit, reviewer string) string {
	return `{
  "specVersion": "0.2.0-draft",
  "id": "https://example.invalid/judgment-packs/expense-approval",
  "version": "0.1.0",
  "title": "Expense approval",
  "decision": {"intent": "Approve ordinary expenses without review.", "question": "May this expense be approved?"},
  "outcomes": [{"id": "approve", "label": "Approve"}, {"id": "decline", "label": "Decline"}],
  "rules": [{"id": "over-limit", "description": "An amount over the limit is declined.",
    "when": {"op": "fact", "path": "/expense/amount", "operator": "greater-than", "value": "` + limit + `"},
    "outcome": "decline", "onUnknown": "escalate"}],
  "fallbackOutcome": "approve",
  "escalation": {"triggers": ["unknown"], "target": {"kind": "human-role", "name": "` + reviewer + `"}}
}`
}

// compareJSON runs experimental compare with --format json and decodes it,
// failing on anything but exit 0.
func compareJSON(t *testing.T, args ...string) result.PackComparison {
	t.Helper()
	code, stdout, stderr := runTest(t, append([]string{"experimental", "compare", "--format", "json"}, args...), "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var comparison result.PackComparison
	if err := json.Unmarshal([]byte(stdout), &comparison); err != nil {
		t.Fatal(err)
	}
	return comparison
}

// Two versions of a pack over one matrix: the inputs whose results differ are
// listed with both sides and what changed, the rest are counted, the rows'
// expectations are never read, and the run exits 0 because a comparison ran.
func TestCompareListsTheInputsTwoVersionsDecideDifferently(t *testing.T) {
	oldPack := writeDocument(t, "old.json", expensePack("5000", "Finance reviewer"))
	newPack := writeDocument(t, "new.json", expensePack("4000", "Finance lead"))
	// Every expectation says approve, true or not: compare never reads one.
	approve := `"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}`
	matrix := writeDocument(t, "matrix.json", `{"matrixVersion":"1","cases":[
	  {"id":"small","facts":{"expense":{"amount":"100"}},`+approve+`},
	  {"id":"between","facts":{"expense":{"amount":"4500"}},`+approve+`},
	  {"id":"large","facts":{"expense":{"amount":"6000"}},`+approve+`},
	  {"id":"no-amount","facts":{"expense":{}},`+approve+`}
	]}`)

	comparison := compareJSON(t, oldPack, newPack, "--inputs", matrix)
	if !comparison.Experimental || !comparison.Rehearsal || comparison.Label != result.ComparisonLabel || comparison.Command != "experimental compare" {
		t.Fatalf("the payload names what it is: %+v", comparison)
	}
	oldBytes, _ := os.ReadFile(oldPack)
	newBytes, _ := os.ReadFile(newPack)
	if comparison.Old.Digest != audit.Digest(oldBytes) || comparison.New.Digest != audit.Digest(newBytes) ||
		comparison.Old.PackVersion != "0.1.0" || comparison.New.PackID == "" || comparison.Old.Path != oldPack {
		t.Fatalf("both packs are named by their bytes and identity: %+v %+v", comparison.Old, comparison.New)
	}
	// no-amount is unresolved under both versions, and differs all the same:
	// it is handed to someone else.
	if comparison.Inputs != (result.ComparedInputs{Kind: "matrix", Count: 4, Same: 2, Different: 2, UnresolvedUnderBoth: 1}) {
		t.Fatalf("inputs = %+v", comparison.Inputs)
	}
	if len(comparison.Differences) != 2 {
		t.Fatalf("differences = %+v", comparison.Differences)
	}
	moved := comparison.Differences[0]
	if moved.ID != "between" || !slices.Equal(moved.Changed, []string{"outcomeId"}) ||
		moved.Old.Disposition.OutcomeID != "approve" || moved.New.Disposition.OutcomeID != "decline" {
		t.Fatalf("the input nearest the moved line: %+v", moved)
	}
	escalated := comparison.Differences[1]
	if escalated.ID != "no-amount" || !slices.Equal(escalated.Changed, []string{"handoffTarget"}) ||
		escalated.Old.HandoffTarget.Name != "Finance reviewer" || escalated.New.HandoffTarget.Name != "Finance lead" {
		t.Fatalf("the same disposition handed to someone else is a difference: %+v", escalated)
	}

	code, stdout, stderr := runTest(t, []string{"experimental", "compare", oldPack, newPack, "--inputs", matrix}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	for _, line := range []string{
		"REHEARSAL: not a decision",
		"inputs: 4 from a matrix (its expectations play no part); 2 differ, 2 the same; 1 of the 4 unresolved under both versions\n",
		"- between: outcome approve -> outcome decline [outcomeId]",
		`- no-amount: unresolved (unknown), handoff to human-role "Finance reviewer" -> unresolved (unknown), handoff to human-role "Finance lead" [handoffTarget]`,
	} {
		if !strings.Contains(stdout, line) {
			t.Fatalf("the human output lacks %q:\n%s", line, stdout)
		}
	}
	// Two versions of one decision, with something resolved: neither warning.
	if comparison.DifferentDecisions || strings.Contains(stdout, "DIFFERENT DECISIONS") || strings.Contains(stdout, "NOTHING RESOLVED") {
		t.Fatalf("a warning with nothing to warn of: %v\n%s", comparison.DifferentDecisions, stdout)
	}
}

// A candidates document from packs suggest is an input as it stands: no
// expectation is needed to compare two versions.
func TestCompareReadsACandidatesDocument(t *testing.T) {
	oldPack := writeDocument(t, "old.json", expensePack("5000", "Finance reviewer"))
	newPack := writeDocument(t, "new.json", expensePack("4000", "Finance reviewer"))
	candidates := writeDocument(t, "candidates.json", `{"candidatesVersion":"1","candidates":[
	  {"id":"at-limit","origin":"generated","facts":{"expense":{"amount":"4000"}},"rationale":"At the literal."},
	  {"id":"above-limit","origin":"generated","facts":{"expense":{"amount":"4001"}},"rationale":"One unit above."}
	]}`)
	comparison := compareJSON(t, oldPack, newPack, "--inputs", candidates)
	if comparison.Inputs != (result.ComparedInputs{Kind: "candidates", Count: 2, Same: 1, Different: 1}) ||
		comparison.Differences[0].ID != "above-limit" {
		t.Fatalf("comparison = %+v", comparison)
	}
}

// A refusal on one side and a disposition on the other is a difference; the
// same refusal on both sides is not.
func TestCompareReportsRefusals(t *testing.T) {
	good := writeDocument(t, "good.json", expensePack("5000", "Finance reviewer"))
	older := writeDocument(t, "older.json", strings.Replace(expensePack("5000", "Finance reviewer"), `"0.2.0-draft"`, `"0.1.0-draft"`, 1))
	matrix := writeDocument(t, "matrix.json", `{"matrixVersion":"1","cases":[
	  {"id":"small","facts":{"expense":{"amount":"100"}},"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}}
	]}`)

	refused := compareJSON(t, good, older, "--inputs", matrix)
	if refused.Inputs.Different != 1 || !slices.Equal(refused.Differences[0].Changed, []string{"refusal"}) ||
		refused.Differences[0].New.EvaluationError == nil || refused.Differences[0].New.EvaluationError.Class != "pack-not-conformant" ||
		refused.Differences[0].Old.Disposition == nil || refused.New.PackID != "" {
		t.Fatalf("a refused side: %+v", refused)
	}
	both := compareJSON(t, older, older, "--inputs", matrix)
	if both.Inputs.Same != 1 || len(both.Differences) != 0 {
		t.Fatalf("the same refusal on both sides is no difference: %+v", both)
	}
}

// The command opens no project: run beside a project that records every
// evaluation and keeps a lock, it writes nothing there and is refused by
// nothing there.
func TestCompareOpensNoProject(t *testing.T) {
	configPath := auditProject(t)
	mustLock(t, configPath)
	t.Setenv(project.ConfigEnv, configPath)
	oldPack := writeDocument(t, "old.json", expensePack("5000", "Finance reviewer"))
	newPack := writeDocument(t, "new.json", expensePack("4000", "Finance reviewer"))
	matrix := writeDocument(t, "matrix.json", `{"matrixVersion":"1","cases":[
	  {"id":"between","facts":{"expense":{"amount":"4500"}},"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}}
	]}`)
	compareJSON(t, oldPack, newPack, "--inputs", matrix)
	if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)); err == nil {
		t.Fatal("a comparison appended an audit record")
	}
}

// Every argument and input defect is refused with its class, before anything
// is evaluated.
func TestCompareRefusesWhatItCannotRead(t *testing.T) {
	pack := writeDocument(t, "pack.json", expensePack("5000", "Finance reviewer"))
	matrix := writeDocument(t, "matrix.json", `{"matrixVersion":"1","cases":[
	  {"id":"small","facts":{"expense":{"amount":"100"}},"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}}
	]}`)
	neither := writeDocument(t, "neither.json", `{"rows":[]}`)
	badCandidates := writeDocument(t, "bad.json", `{"candidatesVersion":"2","candidates":[]}`)
	for name, run := range map[string]struct {
		args []string
		exit int
		code string
	}{
		"one pack":                   {[]string{pack, "--inputs", matrix}, result.ExitInvocation, "JPS-INVOCATION-COMPARE"},
		"no inputs":                  {[]string{pack, pack}, result.ExitInvocation, "JPS-INVOCATION-COMPARE"},
		"standard input":             {[]string{pack, "-", "--inputs", matrix}, result.ExitInvocation, "JPS-INVOCATION-STDIN"},
		"a missing pack":             {[]string{pack, filepath.Join(t.TempDir(), "absent.json"), "--inputs", matrix}, result.ExitIO, "JPS-INPUT-READ"},
		"inputs of neither kind":     {[]string{pack, pack, "--inputs", neither}, result.ExitInvalid, "JPS-COMPARE-INPUTS"},
		"a later candidates version": {[]string{pack, pack, "--inputs", badCandidates}, result.ExitInvalid, "JPS-COMPARE-INPUTS"},
	} {
		t.Run(name, func(t *testing.T) {
			code, stdout, _ := runTest(t, append([]string{"experimental", "compare", "--format", "json"}, run.args...), "")
			if code != run.exit || !strings.Contains(stdout, run.code) || strings.Contains(stdout, "differences") {
				t.Fatalf("exit=%d stdout=%q, want exit %d and %s", code, stdout, run.exit, run.code)
			}
		})
	}
}

// What packs suggest writes, compare reads: the one writer of the candidates
// document and its one reader agree on its shape. A pack compared with itself
// differs on nothing.
func TestCompareReadsWhatPacksSuggestWrites(t *testing.T) {
	configPath := auditProject(t)
	written := filepath.Join(t.TempDir(), "candidates.json")
	code, stdout, stderr := runTest(t, []string{"packs", "suggest", "--config", configPath, "--id", "intake", "--write", written}, "")
	if code != 0 {
		t.Fatalf("packs suggest: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	pack := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")
	comparison := compareJSON(t, pack, pack, "--inputs", written)
	if comparison.Inputs.Kind != "candidates" || comparison.Inputs.Count == 0 || comparison.Inputs.Different != 0 || comparison.Inputs.Same != comparison.Inputs.Count {
		t.Fatalf("comparison = %+v", comparison.Inputs)
	}
}

// Each input is evaluated with its own supported extensions joined by the
// caller's: a pack requiring an extension is refused where neither names it,
// and evaluated where the flag or the row does.
func TestCompareJoinsTheSupportedExtensions(t *testing.T) {
	plain := writeDocument(t, "plain.json", expensePack("5000", "Finance reviewer"))
	requiring := writeDocument(t, "requiring.json", strings.Replace(strings.Replace(expensePack("5000", "Finance reviewer"),
		`"version": "0.1.0",`, `"version": "0.1.0", "metadata": {"requiredExtensions": ["com.example.review-policy"]},`, 1),
		`"onUnknown": "escalate"}]`, `"onUnknown": "escalate", "extensions": {"com.example.review-policy": {"reviewMode": "two-person"}}}]`, 1))
	approve := `"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}`
	unnamed := writeDocument(t, "unnamed.json", `{"matrixVersion":"1","cases":[{"id":"small","facts":{"expense":{"amount":"100"}},`+approve+`}]}`)
	named := writeDocument(t, "named.json", `{"matrixVersion":"1","cases":[{"id":"small","facts":{"expense":{"amount":"100"}},"supportedExtensions":["com.example.review-policy"],`+approve+`}]}`)

	if refused := compareJSON(t, plain, requiring, "--inputs", unnamed); refused.Inputs.Different != 1 || refused.Differences[0].New.EvaluationError == nil {
		t.Fatalf("an extension nobody names refuses the pack requiring it: %+v", refused)
	}
	if flagged := compareJSON(t, plain, requiring, "--inputs", unnamed, "--supported-extension", "com.example.review-policy"); flagged.Inputs.Same != 1 {
		t.Fatalf("the caller's extension is joined to the input's: %+v", flagged)
	}
	if rowed := compareJSON(t, plain, requiring, "--inputs", named); rowed.Inputs.Same != 1 {
		t.Fatalf("the row's own extension is applied: %+v", rowed)
	}
}

// Evidence reaches both evaluations: an input whose evidence decides the
// difference differs, and one whose evidence makes both versions agree does
// not.
func TestCompareEvaluatesEachInputsEvidence(t *testing.T) {
	withReceipt := func(required string) string {
		return strings.Replace(expensePack("5000", "Finance reviewer"), `"outcomes":`,
			`"evidenceRequirements": [{"id": "receipt", "description": "A receipt.", "required": `+required+`, "kind": "document"}],
  "outcomes":`, 1)
	}
	oldPack := writeDocument(t, "old.json", withReceipt("false"))
	newPack := writeDocument(t, "new.json", withReceipt("true"))
	approve := `"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}`
	matrix := writeDocument(t, "matrix.json", `{"matrixVersion":"1","cases":[
	  {"id":"with-receipt","facts":{"expense":{"amount":"100"}},"evidenceAvailability":{"receipt":"present"},`+approve+`},
	  {"id":"without-receipt","facts":{"expense":{"amount":"100"}},"evidenceAvailability":{"receipt":"absent"},`+approve+`}
	]}`)
	comparison := compareJSON(t, oldPack, newPack, "--inputs", matrix)
	if comparison.Inputs.Same != 1 || comparison.Inputs.Different != 1 || comparison.Differences[0].ID != "without-receipt" ||
		!slices.Contains(comparison.Differences[0].New.Disposition.Reasons, "missing-required-evidence") {
		t.Fatalf("evidence decides this comparison: %+v", comparison)
	}
	// The same pair the other way round: the old side's evaluation must see the
	// evidence too.
	reversed := compareJSON(t, newPack, oldPack, "--inputs", matrix)
	if reversed.Inputs.Same != 1 || reversed.Inputs.Different != 1 ||
		!slices.Contains(reversed.Differences[0].Old.Disposition.Reasons, "missing-required-evidence") {
		t.Fatalf("evidence reaches the old side too: %+v", reversed)
	}
}

// Two refusals of the same class and phase with different codes differ, and a
// pack over the byte limit is named with no digest, because its bytes were
// never whole in hand.
func TestCompareTellsRefusalsApartByCode(t *testing.T) {
	malformed := writeDocument(t, "malformed.json", strings.Replace(expensePack("5000", "Finance reviewer"), `"title": "Expense approval",`, ``, 1))
	oversized := writeDocument(t, "oversized.json", strings.Replace(expensePack("5000", "Finance reviewer"),
		`"title": "Expense approval",`, `"title": "Expense approval", "x-padding": "`+strings.Repeat("a", 11<<20)+`",`, 1))
	matrix := writeDocument(t, "matrix.json", `{"matrixVersion":"1","cases":[
	  {"id":"small","facts":{"expense":{"amount":"100"}},"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}}
	]}`)
	comparison := compareJSON(t, malformed, oversized, "--inputs", matrix)
	if comparison.Inputs.Different != 1 {
		t.Fatalf("different codes differ: %+v", comparison)
	}
	before, after := comparison.Differences[0].Old.EvaluationError, comparison.Differences[0].New.EvaluationError
	if before == nil || after == nil || before.Class != after.Class || before.Phase != after.Phase || before.Code == after.Code {
		t.Fatalf("the two refusals share class and phase and differ in code: %+v %+v", before, after)
	}
	if comparison.New.Digest != "" || comparison.Old.Digest == "" {
		t.Fatalf("an oversized pack has no digest: old %q new %q", comparison.Old.Digest, comparison.New.Digest)
	}
	_, human, _ := runTest(t, []string{"experimental", "compare", malformed, oversized, "--inputs", matrix}, "")
	if !strings.Contains(human, "digest unavailable (over the byte limit)") {
		t.Fatalf("the human output says the digest is unavailable: %q", human)
	}
}

// A row's own supported extensions and the caller's are joined: a pack that
// requires two extensions is evaluated only when the row names one and the
// flag the other.
func TestCompareJoinsARowsExtensionsWithTheCallers(t *testing.T) {
	requiring := writeDocument(t, "requiring.json", strings.Replace(strings.Replace(expensePack("5000", "Finance reviewer"),
		`"version": "0.1.0",`, `"version": "0.1.0", "metadata": {"requiredExtensions": ["com.example.first", "com.example.second"]},`, 1),
		`"onUnknown": "escalate"}]`, `"onUnknown": "escalate", "extensions": {"com.example.first": {"on": true}, "com.example.second": {"on": true}}}]`, 1))
	named := writeDocument(t, "named.json", `{"matrixVersion":"1","cases":[{"id":"small","facts":{"expense":{"amount":"100"}},"supportedExtensions":["com.example.first"],"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}}]}`)
	plain := writeDocument(t, "plain.json", expensePack("5000", "Finance reviewer"))
	if alone := compareJSON(t, plain, requiring, "--inputs", named); alone.Inputs.Different != 1 {
		t.Fatalf("the row's one extension is not enough: %+v", alone)
	}
	if joined := compareJSON(t, plain, requiring, "--inputs", named, "--supported-extension", "com.example.second"); joined.Inputs.Same != 1 {
		t.Fatalf("the row's and the caller's together are: %+v", joined)
	}
}

// A report that would exceed its byte limit is refused rather than truncated:
// every difference here carries two large handoff targets.
func TestCompareRefusesAReportPastItsLimit(t *testing.T) {
	// Just under the carrier's string limit, so both packs are conformant and
	// every row differs in its target.
	long := strings.Repeat("r", (1<<20)-8)
	oldPack := writeDocument(t, "old.json", expensePack("5000", "old "+long))
	newPack := writeDocument(t, "new.json", expensePack("5000", "new "+long))
	var rows []string
	for index := range 10 {
		rows = append(rows, `{"id":"row-`+string(rune('a'+index))+`","facts":{"expense":{}},"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}}`)
	}
	matrix := writeDocument(t, "matrix.json", `{"matrixVersion":"1","cases":[`+strings.Join(rows, ",")+`]}`)
	code, stdout, _ := runTest(t, []string{"experimental", "compare", "--format", "json", oldPack, newPack, "--inputs", matrix}, "")
	if code != result.ExitIO || !strings.Contains(stdout, "JPS-RESOURCE-COMPARE-REPORT-LIMIT") || strings.Contains(stdout, `"differences"`) {
		t.Fatalf("exit=%d stdout=%.300q", code, stdout)
	}
}

// The command opens no project whatever the environment names: a
// configuration that does not parse, beside a lock that matches nothing, is
// not read.
func TestCompareReadsNoConfigurationItIsPointedAt(t *testing.T) {
	root := t.TempDir()
	broken := filepath.Join(root, "jpack.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "jpack.lock.json"), []byte(`{"lockVersion":"1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, broken)
	pack := writeDocument(t, "pack.json", expensePack("5000", "Finance reviewer"))
	matrix := writeDocument(t, "matrix.json", `{"matrixVersion":"1","cases":[{"id":"small","facts":{"expense":{"amount":"100"}},"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}}]}`)
	if comparison := compareJSON(t, pack, pack, "--inputs", matrix); comparison.Inputs.Same != 1 {
		t.Fatalf("comparison = %+v", comparison)
	}
}

// A --base row may state a facts document of null, and packs suggest carries
// it into every candidate; compare reads what suggest wrote.
func TestCompareReadsSuggestOutputFromANullFactsRow(t *testing.T) {
	matrix := `{"matrixVersion":"1","cases":[
	  {"id":"null-facts","facts":null,"evidenceAvailability":` + presentEvidence + `,"expectedDisposition":` + declineRedirect + `}
	]}`
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{"intake":{
	  "path":"packs/intake-0.1.0.pack.json",
	  "matrix":"packs/intake.matrix.json"
	}}}`, map[string]string{
		"packs/intake-0.1.0.pack.json": evaluatorPack(t),
		"packs/intake.matrix.json":     matrix,
	})
	written := filepath.Join(t.TempDir(), "candidates.json")
	code, stdout, stderr := runTest(t, []string{"packs", "suggest", "--config", configPath, "--id", "intake", "--base", "null-facts", "--write", written}, "")
	if code != 0 {
		t.Fatalf("packs suggest: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	body, err := os.ReadFile(written)
	if err != nil || !strings.Contains(string(body), `"facts": null`) {
		t.Fatalf("the candidates carry the base row's null facts: %v %.300q", err, body)
	}
	pack := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")
	if comparison := compareJSON(t, pack, pack, "--inputs", written); comparison.Inputs.Count == 0 || comparison.Inputs.Same != comparison.Inputs.Count {
		t.Fatalf("comparison = %+v", comparison.Inputs)
	}
}

// A candidates document with no candidates, which packs suggest writes when it
// derives nothing, is read as zero inputs rather than refused.
func TestCompareReadsAnEmptyCandidatesDocument(t *testing.T) {
	pack := writeDocument(t, "pack.json", expensePack("5000", "Finance reviewer"))
	empty := writeDocument(t, "empty.json", `{"candidatesVersion":"1","candidates":[]}`)
	if comparison := compareJSON(t, pack, pack, "--inputs", empty); comparison.Inputs != (result.ComparedInputs{Kind: "candidates"}) || len(comparison.Differences) != 0 {
		t.Fatalf("comparison = %+v", comparison)
	}
}

// The differences are charged as the CLI writes them, without HTML escaping:
// targets made of characters an HTML-escaping encoder would inflate sixfold
// stay within the limit when their written size does.
func TestCompareChargesTheWrittenBytes(t *testing.T) {
	angled := strings.Repeat("<", 700<<10)
	oldPack := writeDocument(t, "old.json", expensePack("5000", "a"+angled))
	newPack := writeDocument(t, "new.json", expensePack("5000", "b"+angled))
	var rows []string
	for index := range 4 {
		rows = append(rows, `{"id":"row-`+string(rune('a'+index))+`","facts":{"expense":{}},"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}}`)
	}
	matrix := writeDocument(t, "matrix.json", `{"matrixVersion":"1","cases":[`+strings.Join(rows, ",")+`]}`)
	if comparison := compareJSON(t, oldPack, newPack, "--inputs", matrix); comparison.Inputs.Different != 4 {
		t.Fatalf("about 5.6 MiB written is within the limit: %+v", comparison.Inputs)
	}
}

// differentDecisionsLine and nothingResolvedLine are the two warnings, word for
// word, so a test can tell a warning from a line that merely mentions it.
const (
	differentDecisionsLine = "DIFFERENT DECISIONS: the two packs have different ids, so this compares two decisions, not two versions of one"
	nothingResolvedLine    = "NOTHING RESOLVED: every input was unresolved under both versions, so this comparison could not see a change in any outcome; " +
		"packs suggest --base <row-id> writes candidates that carry a reviewed row's other facts and evidence"
)

// compareHuman runs experimental compare in its human format, failing on
// anything but exit 0.
func compareHuman(t *testing.T, args ...string) string {
	t.Helper()
	code, stdout, stderr := runTest(t, append([]string{"experimental", "compare"}, args...), "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	return stdout
}

// compareMembers runs experimental compare with --format json and decodes the
// payload as members, so a test can tell an omitted member from a false one.
func compareMembers(t *testing.T, args ...string) map[string]any {
	t.Helper()
	code, stdout, stderr := runTest(t, append([]string{"experimental", "compare", "--format", "json"}, args...), "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var members map[string]any
	if err := json.Unmarshal([]byte(stdout), &members); err != nil {
		t.Fatal(err)
	}
	return members
}

// Two packs with different ids are two decisions. They are compared all the
// same, the human output says so on its first line, and the payload carries
// "differentDecisions": true.
func TestCompareSaysWhenThePacksAreDifferentDecisions(t *testing.T) {
	expense := writeDocument(t, "expense.json", expensePack("5000", "Finance reviewer"))
	travel := writeDocument(t, "travel.json", strings.Replace(expensePack("4000", "Finance reviewer"), "/expense-approval", "/travel-approval", 1))
	approve := `"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}`
	matrix := writeDocument(t, "matrix.json", `{"matrixVersion":"1","cases":[
	  {"id":"small","facts":{"expense":{"amount":"100"}},`+approve+`},
	  {"id":"between","facts":{"expense":{"amount":"4500"}},`+approve+`}
	]}`)

	comparison := compareJSON(t, expense, travel, "--inputs", matrix)
	if !comparison.DifferentDecisions || comparison.Old.PackID == comparison.New.PackID {
		t.Fatalf("two ids, two decisions: %+v %+v %v", comparison.Old, comparison.New, comparison.DifferentDecisions)
	}
	if comparison.Inputs.Different != 1 || comparison.Differences[0].ID != "between" {
		t.Fatalf("the comparison still runs: %+v", comparison)
	}
	if members := compareMembers(t, expense, travel, "--inputs", matrix); members["differentDecisions"] != true {
		t.Fatalf("the payload states it: %v", members["differentDecisions"])
	}

	human := compareHuman(t, expense, travel, "--inputs", matrix)
	if first, _, _ := strings.Cut(human, "\n"); first != differentDecisionsLine {
		t.Fatalf("the first line says so: %q", first)
	}
	if !strings.Contains(human, "- between: outcome approve -> outcome decline [outcomeId]") {
		t.Fatalf("the report follows:\n%s", human)
	}
}

// Two versions of one decision carry no such warning, whatever their versions,
// and neither does a pair one of whose ids was never read: a pack every
// evaluation refused names no id to differ.
func TestCompareSaysNothingOfDecisionsItCannotTellApart(t *testing.T) {
	older := writeDocument(t, "older.json", expensePack("5000", "Finance reviewer"))
	newer := writeDocument(t, "newer.json", strings.Replace(expensePack("4000", "Finance reviewer"), `"version": "0.1.0"`, `"version": "0.2.0"`, 1))
	unread := writeDocument(t, "unread.json", strings.Replace(strings.Replace(expensePack("5000", "Finance reviewer"),
		`"0.2.0-draft"`, `"0.1.0-draft"`, 1), "/expense-approval", "/travel-approval", 1))
	matrix := writeDocument(t, "matrix.json", `{"matrixVersion":"1","cases":[
	  {"id":"small","facts":{"expense":{"amount":"100"}},"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}}
	]}`)
	for name, pair := range map[string]struct {
		old, new string
		unread   bool
	}{
		"two versions of one decision": {older, newer, false},
		"an id never read":             {older, unread, true},
	} {
		t.Run(name, func(t *testing.T) {
			comparison := compareJSON(t, pair.old, pair.new, "--inputs", matrix)
			if comparison.DifferentDecisions || comparison.Old.PackID == "" || (comparison.New.PackID == "") != pair.unread {
				t.Fatalf("no warning, and the premise holds: %+v %+v %v", comparison.Old, comparison.New, comparison.DifferentDecisions)
			}
			if members := compareMembers(t, pair.old, pair.new, "--inputs", matrix); members["differentDecisions"] != nil {
				t.Fatalf("the member is omitted: %v", members["differentDecisions"])
			}
			human := compareHuman(t, pair.old, pair.new, "--inputs", matrix)
			if !strings.HasPrefix(human, "EXPERIMENTAL SURFACE ") || strings.Contains(human, "DIFFERENT DECISIONS") {
				t.Fatalf("the label leads and no warning is given:\n%s", human)
			}
		})
	}
}

// The inputs unresolved under both versions are counted beside the totals,
// whether they differ or not; an input unresolved under one version only, or
// refused, is not one of them. When every input was, the human output says the
// comparison could not see a change and points to packs suggest --base.
func TestCompareCountsTheInputsUnresolvedUnderBoth(t *testing.T) {
	approve := `"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}`
	rows := func(cases ...string) string {
		return writeDocument(t, "matrix.json", `{"matrixVersion":"1","cases":[`+strings.Join(cases, ",")+`]}`)
	}
	small := `{"id":"small","facts":{"expense":{"amount":"100"}},` + approve + `}`
	between := `{"id":"between","facts":{"expense":{"amount":"4500"}},` + approve + `}`
	noAmount := `{"id":"no-amount","facts":{"expense":{}},` + approve + `}`
	noExpense := `{"id":"no-expense","facts":{},` + approve + `}`
	limit5000 := writeDocument(t, "5000.json", expensePack("5000", "Finance reviewer"))
	limit4000 := writeDocument(t, "4000.json", expensePack("4000", "Finance reviewer"))
	lead := writeDocument(t, "lead.json", expensePack("4000", "Finance lead"))
	ignoring := writeDocument(t, "ignoring.json", strings.Replace(expensePack("5000", "Finance reviewer"), `"onUnknown": "escalate"`, `"onUnknown": "ignore"`, 1))
	refused := writeDocument(t, "refused.json", strings.Replace(expensePack("5000", "Finance reviewer"), `"0.2.0-draft"`, `"0.1.0-draft"`, 1))
	empty := writeDocument(t, "empty.json", `{"candidatesVersion":"1","candidates":[]}`)
	// A pack that applies to claims only, so a mileage input is not-applicable
	// under both versions: no outcome, and not unresolved either.
	claimsOnly := func(name, limit string) string {
		return writeDocument(t, name, strings.Replace(expensePack(limit, "Finance reviewer"), `"rules":`,
			`"applicability": {"op": "fact", "path": "/expense/kind", "operator": "equals", "value": "claim"},
  "rules":`, 1))
	}
	claims5000, claims4000 := claimsOnly("claims-5000.json", "5000"), claimsOnly("claims-4000.json", "4000")
	mileage := `{"id":"mileage","facts":{"expense":{"kind":"mileage","amount":"4500"}},` + approve + `}`

	for name, run := range map[string]struct {
		old, new, inputs string
		want             result.ComparedInputs
		line             string
		warned           bool
	}{
		"some but not all": {limit5000, limit4000, rows(small, between, noAmount),
			result.ComparedInputs{Kind: "matrix", Count: 3, Same: 2, Different: 1, UnresolvedUnderBoth: 1},
			"inputs: 3 from a matrix (its expectations play no part); 1 differ, 2 the same; 1 of the 3 unresolved under both versions\n", false},
		"every input, all the same": {limit5000, limit4000, rows(noAmount, noExpense),
			result.ComparedInputs{Kind: "matrix", Count: 2, Same: 2, UnresolvedUnderBoth: 2},
			"inputs: 2 from a matrix (its expectations play no part); 0 differ, 2 the same; 2 of the 2 unresolved under both versions\n", true},
		"every input, each handed to someone else": {limit5000, lead, rows(noAmount, noExpense),
			result.ComparedInputs{Kind: "matrix", Count: 2, Different: 2, UnresolvedUnderBoth: 2},
			"inputs: 2 from a matrix (its expectations play no part); 2 differ, 0 the same; 2 of the 2 unresolved under both versions\n", true},
		"unresolved under the new version only": {ignoring, limit5000, rows(noAmount),
			result.ComparedInputs{Kind: "matrix", Count: 1, Different: 1},
			"inputs: 1 from a matrix (its expectations play no part); 1 differ, 0 the same; 0 of the 1 unresolved under both versions\n", false},
		"unresolved under the old version only": {limit5000, ignoring, rows(noAmount),
			result.ComparedInputs{Kind: "matrix", Count: 1, Different: 1},
			"inputs: 1 from a matrix (its expectations play no part); 1 differ, 0 the same; 0 of the 1 unresolved under both versions\n", false},
		"unresolved under one, refused under the other": {limit5000, refused, rows(noAmount),
			result.ComparedInputs{Kind: "matrix", Count: 1, Different: 1},
			"inputs: 1 from a matrix (its expectations play no part); 1 differ, 0 the same; 0 of the 1 unresolved under both versions\n", false},
		"refused under both": {refused, refused, rows(noAmount),
			result.ComparedInputs{Kind: "matrix", Count: 1, Same: 1},
			"inputs: 1 from a matrix (its expectations play no part); 0 differ, 1 the same; 0 of the 1 unresolved under both versions\n", false},
		"not applicable under both": {claims5000, claims4000, rows(mileage),
			result.ComparedInputs{Kind: "matrix", Count: 1, Same: 1},
			"inputs: 1 from a matrix (its expectations play no part); 0 differ, 1 the same; 0 of the 1 unresolved under both versions\n", false},
		"no inputs": {limit5000, limit4000, empty,
			result.ComparedInputs{Kind: "candidates"},
			"inputs: 0 from a candidates document; 0 differ, 0 the same; 0 of the 0 unresolved under both versions\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			if comparison := compareJSON(t, run.old, run.new, "--inputs", run.inputs); comparison.Inputs != run.want {
				t.Fatalf("inputs = %+v, want %+v", comparison.Inputs, run.want)
			}
			inputs, _ := compareMembers(t, run.old, run.new, "--inputs", run.inputs)["inputs"].(map[string]any)
			if inputs["unresolvedUnderBoth"] != float64(run.want.UnresolvedUnderBoth) {
				t.Fatalf("the payload states the count, zero included: %v", inputs)
			}
			human := compareHuman(t, run.old, run.new, "--inputs", run.inputs)
			if !strings.Contains(human, run.line) {
				t.Fatalf("the totals line lacks the count; want %q in:\n%s", run.line, human)
			}
			if warned := strings.Contains(human, nothingResolvedLine+"\n"); warned != run.warned || strings.Contains(human, "NOTHING RESOLVED") != run.warned {
				t.Fatalf("warned=%v, want %v:\n%s", warned, run.warned, human)
			}
		})
	}

	// The premises of two cases above: the pack that ignores an unknown reaches
	// an outcome where the other is unresolved, and the claims-only pack is not
	// applicable to mileage rather than refused.
	premises := compareJSON(t, ignoring, claims5000, "--inputs", rows(noAmount, mileage))
	if len(premises.Differences) != 2 {
		t.Fatalf("premises = %+v", premises)
	}
	if ignored := premises.Differences[0]; ignored.Old.Disposition == nil || ignored.Old.Disposition.Kind != "outcome" ||
		ignored.New.Disposition == nil || ignored.New.Disposition.Kind != "unresolved" {
		t.Fatalf("an ignored unknown reaches an outcome, an escalated one does not: %+v", ignored)
	}
	if mileaged := premises.Differences[1]; mileaged.New.Disposition == nil || mileaged.New.Disposition.Kind != "not-applicable" {
		t.Fatalf("mileage is not applicable to the claims-only pack: %+v", mileaged)
	}
}

// The case that asked for the warning, end to end: a threshold moved between
// two versions of a pack that reads two facts. Candidates from plain packs
// suggest state one fact each, so every one is unresolved under both versions
// and none differs; the report says the comparison could not see the change.
// Candidates from packs suggest --base carry the row's other fact, and the
// change shows.
func TestCompareSaysWhenPlainCandidatesCouldNotSeeAMovedLine(t *testing.T) {
	twoFacts := func(limit string) string {
		return strings.Replace(expensePack(limit, "Finance reviewer"), `"outcome": "decline", "onUnknown": "escalate"}]`,
			`"outcome": "decline", "onUnknown": "escalate"},
    {"id": "gifts", "description": "A gift is declined.",
     "when": {"op": "fact", "path": "/expense/category", "operator": "in", "value": ["gifts"]},
     "outcome": "decline", "onUnknown": "escalate"}]`, 1)
	}
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{"expense":{
	  "path":"packs/expense.pack.json",
	  "matrix":"packs/expense.matrix.json"
	}}}`, map[string]string{
		"packs/expense.pack.json": twoFacts("5000"),
		"packs/expense.matrix.json": `{"matrixVersion":"1","cases":[
		  {"id":"ordinary","facts":{"expense":{"amount":"100","category":"travel"}},"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}}
		]}`,
	})
	before := filepath.Join(filepath.Dir(configPath), "packs", "expense.pack.json")
	after := writeDocument(t, "after.json", twoFacts("4000"))

	suggest := func(extra ...string) string {
		written := filepath.Join(t.TempDir(), "candidates.json")
		code, stdout, stderr := runTest(t, append([]string{"packs", "suggest", "--config", configPath, "--id", "expense", "--write", written}, extra...), "")
		if code != 0 {
			t.Fatalf("packs suggest: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		return written
	}

	plain := suggest()
	blind := compareJSON(t, before, after, "--inputs", plain)
	if blind.Inputs.Count == 0 || blind.Inputs.Different != 0 || blind.Inputs.UnresolvedUnderBoth != blind.Inputs.Count {
		t.Fatalf("plain candidates are unresolved under both versions: %+v", blind.Inputs)
	}
	if human := compareHuman(t, before, after, "--inputs", plain); !strings.Contains(human, nothingResolvedLine+"\n") {
		t.Fatalf("the report says the comparison could not see the change:\n%s", human)
	}

	based := suggest("--base", "ordinary")
	seen := compareJSON(t, before, after, "--inputs", based)
	if seen.Inputs.Different == 0 || seen.Inputs.UnresolvedUnderBoth >= seen.Inputs.Count {
		t.Fatalf("candidates from a base row show the moved line: %+v", seen.Inputs)
	}
	if human := compareHuman(t, before, after, "--inputs", based); strings.Contains(human, "NOTHING RESOLVED") {
		t.Fatalf("no warning once something resolved:\n%s", human)
	}
}
