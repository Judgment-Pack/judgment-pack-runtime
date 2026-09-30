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
	if comparison.Inputs != (result.ComparedInputs{Kind: "matrix", Count: 4, Same: 2, Different: 2}) {
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
		"inputs: 4 from a matrix (expectations unread); 2 differ, 2 the same",
		"- between: outcome approve -> outcome decline [outcomeId]",
		`- no-amount: unresolved (unknown), handoff to human-role "Finance reviewer" -> unresolved (unknown), handoff to human-role "Finance lead" [handoffTarget]`,
	} {
		if !strings.Contains(stdout, line) {
			t.Fatalf("the human output lacks %q:\n%s", line, stdout)
		}
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
