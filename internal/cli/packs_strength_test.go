package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// strengthPack is a Core 0.2.0-draft pack with one non-strict bound, one
// outcome per side and an escalating onUnknown. Every fixture below is
// written by hand, and so is every expected variant: the ids follow from
// where each change is, and each status from reading the pack against the
// rows (ADR-0084).
const strengthPack = `{
  "specVersion":"0.2.0-draft",
  "id":"https://example.invalid/judgment-packs/expense-strength",
  "version":"0.1.0",
  "title":"Synthetic expense review threshold",
  "decision":{"intent":"Route an expense to review once its amount reaches a stated spend threshold.","question":"Does this expense require review?"},
  "outcomes":[{"id":"review","label":"Requires review"},{"id":"auto-approve","label":"Auto-approved"}],
  "rules":[{"id":"expense-threshold","description":"5000 or more spend requires review",
    "when":{"op":"fact","path":"/expense/amount","operator":"greater-than-or-equal","value":"5000"},
    "outcome":"review","onUnknown":"escalate"}],
  "fallbackOutcome":"auto-approve",
  "escalation":{"triggers":["unknown"],"target":{"kind":"human-role","name":"Expense reviewer"}}
}`

const (
	strengthAutoApprove = `{"kind":"outcome","outcomeId":"auto-approve","reasons":[],"handoff":{"state":"none"}}`
	strengthReview      = `{"kind":"outcome","outcomeId":"review","reasons":[],"handoff":{"state":"none"}}`
	strengthUnknown     = `{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"requested","triggeredBy":["unknown"]}}`
)

// strengthProject writes a one-pack project. withBoundaryRow adds the row at
// the bound itself, which is the only row that tells greater-than-or-equal
// "5000" from greater-than "5000" or from greater-than-or-equal "5001".
func strengthProject(t *testing.T, withBoundaryRow bool) string {
	t.Helper()
	at := ""
	if withBoundaryRow {
		at = `{"id":"at","facts":{"expense":{"amount":"5000"}},"expectedDisposition":` + strengthReview + `},`
	}
	matrix := `{"matrixVersion":"1","cases":[
	  {"id":"under","facts":{"expense":{"amount":"4999"}},"expectedDisposition":` + strengthAutoApprove + `},
	  ` + at + `
	  {"id":"over","facts":{"expense":{"amount":"5001"}},"expectedDisposition":` + strengthReview + `},
	  {"id":"unknown","facts":{"expense":{}},"expectedDisposition":` + strengthUnknown + `}
	]}`
	return writeProjectFixture(t, `{"configVersion":"1","packs":{"expense":{
	  "path":"packs/expense-0.1.0.pack.json",
	  "matrix":"packs/expense.matrix.json"
	}}}`, map[string]string{
		"packs/expense-0.1.0.pack.json": strengthPack,
		"packs/expense.matrix.json":     matrix,
	})
}

type expectedVariant struct{ id, status, noticedBy string }

func checkVariants(t *testing.T, strength *result.PackStrength, want []expectedVariant) {
	t.Helper()
	if strength == nil || strength.Status != result.StrengthRan {
		t.Fatalf("strength = %+v, want a run", strength)
	}
	got := make([]expectedVariant, 0, len(strength.Variants))
	for _, variant := range strength.Variants {
		got = append(got, expectedVariant{variant.ID, variant.Status, variant.NoticedBy})
	}
	if !slices.Equal(got, want) {
		t.Fatalf("variants:\n got %v\nwant %v", got, want)
	}
}

// The acceptance fixture: a >= bound whose boundary row is missing. The two
// variants only that row could tell apart survive and are reported; every
// other kind the pack derives is noticed by the row that first differs.
func TestStrengthReportsTheVariantsAMissingBoundaryRowLetsSurvive(t *testing.T) {
	code, report, stdout := runPackTest(t, strengthProject(t, false), "--strength")
	if code != result.ExitSuccess || report.Status != "passed" {
		t.Fatalf("survivors inform and move nothing without --fail-on-survivor: exit=%d status=%s", code, report.Status)
	}
	entry := report.Packs[0]
	checkVariants(t, entry.Strength, []expectedVariant{
		{"fallbackOutcome:outcome-swapped:review", result.VariantNoticed, "under"},
		{"rules/expense-threshold/onUnknown:on-unknown-changed:ignore", result.VariantNoticed, "unknown"},
		{"rules/expense-threshold/outcome:outcome-swapped:auto-approve", result.VariantNoticed, "over"},
		{"rules/expense-threshold/when:bound-moved:4999", result.VariantNoticed, "under"},
		{"rules/expense-threshold/when:bound-moved:5001", result.VariantSurvived, ""},
		{"rules/expense-threshold/when:operator-reversed:greater-than", result.VariantSurvived, ""},
		{"rules/expense-threshold/when:operator-reversed:less-than", result.VariantNoticed, "under"},
	})
	if got, want := entry.Strength.Counts, (result.StrengthCounts{Derived: 7, Noticed: 5, Survived: 2}); got != want {
		t.Fatalf("counts = %+v, want %+v", got, want)
	}
	if report.Strength == nil || report.Strength.Summary != entry.Strength.Counts || report.Strength.MaxVariants != 1000 || report.Strength.Label != result.StrengthLabel || report.Strength.FailOnSurvivor {
		t.Fatalf("run-level strength = %+v", report.Strength)
	}
	moved := entry.Strength.Variants[4]
	if moved.Kind != result.VariantBoundMoved || moved.Declaration != `rule "expense-threshold"` ||
		moved.Change != `/expense/amount greater-than-or-equal "5000" → greater-than-or-equal "5001"` ||
		moved.Hint != "a row that places /expense/amount at 5000 and reaches this comparison would notice it (the boundary:/expense/amount:5000 probe)" {
		t.Fatalf("survivor = %+v", moved)
	}
	if !strings.Contains(stdout, `"strength":`) {
		t.Fatalf("payload carries no strength member: %s", stdout)
	}

	code, text, stderr := runTest(t, []string{"packs", "test", "--config", strengthProject(t, false), "--strength"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("text: exit=%d stderr=%q", code, stderr)
	}
	for _, line := range []string{
		"EXPERIMENTAL test strength: 7 variants derived, 5 noticed, 2 survived; 0 changes not derivable across the tested packs (at most 1000 changes per pack)\n",
		"this run cannot tell which, and its result proves nothing about the pack\n",
		"  strength: 7 variants derived, 5 noticed, 2 survived; 0 changes not derivable\n",
		`    survived rules/expense-threshold/when:bound-moved:5001 (rule "expense-threshold"): /expense/amount greater-than-or-equal "5000" → greater-than-or-equal "5001"` + "\n",
		"      hint: a row that places /expense/amount at 5000 and reaches this comparison would notice it (the boundary:/expense/amount:5000 probe)\n",
		`    survived rules/expense-threshold/when:operator-reversed:greater-than (rule "expense-threshold"): /expense/amount greater-than-or-equal "5000" → greater-than "5000"` + "\n",
	} {
		if !strings.Contains(text, line) {
			t.Fatalf("text output lacks %q:\n%s", line, text)
		}
	}
	if strings.Contains(text, "survived rules/expense-threshold/when:bound-moved:4999") {
		t.Fatalf("a noticed variant is listed as a survivor:\n%s", text)
	}
}

// The fully tested fixture: with the row at the bound, every variant is
// noticed, and --fail-on-survivor passes.
func TestStrengthReportsNoSurvivorForAFullyTestedPack(t *testing.T) {
	code, report, _ := runPackTest(t, strengthProject(t, true), "--strength", "--fail-on-survivor")
	if code != result.ExitSuccess || report.Status != "passed" || len(report.Packs[0].UnmetRequirements) != 0 {
		t.Fatalf("exit=%d status=%s unmet=%v", code, report.Status, report.Packs[0].UnmetRequirements)
	}
	checkVariants(t, report.Packs[0].Strength, []expectedVariant{
		{"fallbackOutcome:outcome-swapped:review", result.VariantNoticed, "under"},
		{"rules/expense-threshold/onUnknown:on-unknown-changed:ignore", result.VariantNoticed, "unknown"},
		{"rules/expense-threshold/outcome:outcome-swapped:auto-approve", result.VariantNoticed, "at"},
		{"rules/expense-threshold/when:bound-moved:4999", result.VariantNoticed, "under"},
		{"rules/expense-threshold/when:bound-moved:5001", result.VariantNoticed, "at"},
		{"rules/expense-threshold/when:operator-reversed:greater-than", result.VariantNoticed, "at"},
		{"rules/expense-threshold/when:operator-reversed:less-than", result.VariantNoticed, "under"},
	})
	if !report.Strength.FailOnSurvivor {
		t.Fatal("the run does not record that survivors fail it")
	}
}

// --fail-on-survivor fails a pack with a survivor, names the requirement,
// and exits 1; the same run without it exits 0.
func TestFailOnSurvivorFailsAPackWithASurvivor(t *testing.T) {
	code, report, _ := runPackTest(t, strengthProject(t, false), "--strength", "--fail-on-survivor")
	entry := report.Packs[0]
	if code != result.ExitInvalid || report.Status != "mismatch" || entry.Status != "mismatch" ||
		!slices.Equal(entry.UnmetRequirements, []string{result.RequirementNoSurvivor}) ||
		entry.Detail != "This run fails a pack with a surviving variant, and 2 of 7 variants survived." {
		t.Fatalf("exit=%d status=%s entry=%s unmet=%v detail=%q", code, report.Status, entry.Status, entry.UnmetRequirements, entry.Detail)
	}
	code, text, _ := runTest(t, []string{"packs", "test", "--config", strengthProject(t, false), "--strength", "--fail-on-survivor"}, "")
	if code != result.ExitInvalid || !strings.Contains(text, "mismatch: 0/3 matrix rows did not match their expectation, and 1 pack had a surviving variant, or none that ran, under --fail-on-survivor\n") ||
		!strings.Contains(text, "required: no variant of a tested pack survives\n") {
		t.Fatalf("exit=%d\n%s", code, text)
	}
}

// A requirement that could not be measured is not met (ADR-0080): a pack
// whose own rows mismatch runs no variant, says why, and fails the
// survivor requirement as well as its rows.
func TestStrengthRunsNoVariantForAPackWhoseRowsMismatch(t *testing.T) {
	config := writeProjectFixture(t, `{"configVersion":"1","packs":{"expense":{"path":"p.json","matrix":"m.json"}}}`, map[string]string{
		"p.json": strengthPack,
		"m.json": `{"matrixVersion":"1","cases":[{"id":"wrong","facts":{"expense":{"amount":"1"}},"expectedDisposition":` + strengthReview + `}]}`,
	})
	code, report, _ := runPackTest(t, config, "--strength", "--fail-on-survivor")
	entry := report.Packs[0]
	if code != result.ExitInvalid || entry.Strength == nil || entry.Strength.Status != result.StrengthSkipped ||
		len(entry.Strength.Variants) != 0 || !strings.Contains(entry.Strength.Detail, "did not match their expectation") ||
		!slices.Equal(entry.UnmetRequirements, []string{result.RequirementNoSurvivor}) {
		t.Fatalf("exit=%d entry=%+v strength=%+v", code, entry, entry.Strength)
	}
}

// A change whose result is not an admitted pack is discarded: dropping the
// only member of an all leaves an empty conditions array, which the schema
// refuses, so it is counted not derivable and never reported as a survivor,
// and it does not fail --fail-on-survivor.
func TestStrengthDiscardsAChangeThatIsNotAPack(t *testing.T) {
	pack := strings.Replace(strengthPack,
		`"when":{"op":"fact","path":"/expense/amount","operator":"greater-than-or-equal","value":"5000"}`,
		`"when":{"op":"all","conditions":[{"op":"fact","path":"/expense/amount","operator":"greater-than-or-equal","value":"5000"}]}`, 1)
	config := strengthProject(t, true)
	if err := os.WriteFile(filepath.Join(filepath.Dir(config), "packs", "expense-0.1.0.pack.json"), []byte(pack), 0o600); err != nil {
		t.Fatal(err)
	}
	code, report, _ := runPackTest(t, config, "--strength", "--fail-on-survivor")
	strength := report.Packs[0].Strength
	if code != result.ExitSuccess || strength == nil || strength.Counts != (result.StrengthCounts{Derived: 7, Noticed: 7, NotDerivable: 1}) {
		t.Fatalf("exit=%d strength=%+v", code, strength)
	}
	var dropped *result.StrengthVariant
	for index := range strength.Variants {
		if strength.Variants[index].ID == "rules/expense-threshold/when/conditions/0:condition-dropped" {
			dropped = &strength.Variants[index]
		}
	}
	if dropped == nil || dropped.Status != result.VariantNotDerivable || dropped.Kind != result.VariantConditionDropped ||
		!strings.HasPrefix(dropped.Detail, "The change does not leave a pack this runtime admits") || dropped.Hint != "" {
		t.Fatalf("dropped = %+v", dropped)
	}
}

// strengthValueSetPack is a Core 0.5.0 pack whose one rule reads a
// three-entry value set; the matrix places only the middle entry.
const strengthValueSetPack = `{"specVersion":"0.5.0","id":"https://example.invalid/judgment-packs/tiers-strength","version":"0.1.0",
  "title":"Priority tiers",
  "decision":{"intent":"Decide whether a request is on the priority track.","question":"Is the request accepted?"},
  "outcomes":[{"id":"accept","label":"Accept"},{"id":"refer","label":"Refer for review"}],
  "rules":[{"id":"priority-track","description":"A request in a priority tier is accepted.",
    "when":{"op":"fact","path":"/request/tier","operator":"in","valueSet":"priority-tiers"},
    "outcome":"accept","onUnknown":"escalate"}],
  "valueSets":[{"id":"priority-tiers","entries":[{"value":"tier-1"},{"value":"tier-2"},{"value":"tier-3"}]},
               {"id":"unread","entries":[{"value":"x"},{"value":"y"}]}],
  "escalation":{"triggers":["unknown","no-match"],"target":{"kind":"queue","name":"Priority review"}}}`

// On Core 0.5.0 a referenced value set derives one removal per entry; the
// entries no row places survive, and a set no condition reads derives none.
func TestStrengthRemovesValueSetEntriesOnCore050(t *testing.T) {
	config := writeProjectFixture(t, `{"configVersion":"1","packs":{"tiers":{"path":"p.json","matrix":"m.json"}}}`, map[string]string{
		"p.json": strengthValueSetPack,
		"m.json": `{"matrixVersion":"1","cases":[
		  {"id":"match","facts":{"request":{"tier":"tier-2"}},"expectedDisposition":{"kind":"outcome","outcomeId":"accept","reasons":[],"handoff":{"state":"none"}}},
		  {"id":"no-match","facts":{"request":{"tier":"tier-9"}},"expectedDisposition":{"kind":"unresolved","reasons":["no-match"],"handoff":{"state":"requested","triggeredBy":["no-match"]}}},
		  {"id":"unknown","facts":{"request":{}},"expectedDisposition":{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"requested","triggeredBy":["unknown"]}}}
		]}`,
	})
	code, report, _ := runPackTest(t, config, "--strength")
	if code != result.ExitSuccess || report.EvaluatorSpecVersion != "0.5.0" {
		t.Fatalf("exit=%d version=%s", code, report.EvaluatorSpecVersion)
	}
	checkVariants(t, report.Packs[0].Strength, []expectedVariant{
		{"rules/priority-track/onUnknown:on-unknown-changed:ignore", result.VariantNoticed, "unknown"},
		{"rules/priority-track/outcome:outcome-swapped:refer", result.VariantNoticed, "match"},
		{`valueSets/priority-tiers/entries:value-set-entry-removed:"tier-1"`, result.VariantSurvived, ""},
		{`valueSets/priority-tiers/entries:value-set-entry-removed:"tier-2"`, result.VariantNoticed, "match"},
		{`valueSets/priority-tiers/entries:value-set-entry-removed:"tier-3"`, result.VariantSurvived, ""},
	})
	survivor := report.Packs[0].Strength.Variants[2]
	if survivor.Change != `removes entry "tier-1" from value set "priority-tiers"` ||
		survivor.Hint != `a row that places /request/tier at "tier-1" and reaches a condition reading this set would notice it` {
		t.Fatalf("survivor = %+v", survivor)
	}
}

// Two runs over an unchanged project write the same bytes, and the variants
// are in id order whatever order the derivation met them in.
func TestStrengthOutputIsByteIdenticalAcrossRuns(t *testing.T) {
	config := strengthProject(t, false)
	_, first, firstBytes := runPackTest(t, config, "--strength")
	_, _, secondBytes := runPackTest(t, config, "--strength")
	if firstBytes != secondBytes {
		t.Fatalf("two runs differ:\n%s\n%s", firstBytes, secondBytes)
	}
	_, textFirst, _ := runTest(t, []string{"packs", "test", "--config", config, "--strength"}, "")
	_, textSecond, _ := runTest(t, []string{"packs", "test", "--config", config, "--strength"}, "")
	if textFirst != textSecond {
		t.Fatalf("two text runs differ:\n%s\n%s", textFirst, textSecond)
	}
	ids := []string{}
	for _, variant := range first.Packs[0].Strength.Variants {
		ids = append(ids, variant.ID)
	}
	if !slices.IsSorted(ids) || len(slices.Compact(slices.Clone(ids))) != len(ids) {
		t.Fatalf("variant ids are not strictly ascending: %v", ids)
	}
}

// A pack that derives more changes than the ceiling refuses the whole run
// before any variant runs and names the ceiling; at the ceiling it runs.
func TestStrengthCeilingRefusesThePack(t *testing.T) {
	config := strengthProject(t, false)
	code, stdout, _ := runTest(t, []string{"packs", "test", "--config", config, "--format", "json", "--strength", "--max-variants", "6"}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout, "JPS-RESOURCE-STRENGTH-VARIANTS") || !strings.Contains(stdout, "more than 6 variants") || strings.Contains(stdout, `"variants"`) {
		t.Fatalf("exit=%d %s", code, stdout)
	}
	code, report, _ := runPackTest(t, config, "--strength", "--max-variants", "7")
	if code != result.ExitSuccess || report.Packs[0].Strength.Counts.Derived != 7 || report.Strength.MaxVariants != 7 {
		t.Fatalf("at the ceiling: exit=%d strength=%+v", code, report.Packs[0].Strength)
	}
}

// The options that only a test-strength run reads are refused without it,
// and a ceiling below one is refused.
func TestStrengthOptionsAreRefusedOutOfPlace(t *testing.T) {
	for _, args := range [][]string{{"--fail-on-survivor"}, {"--max-variants", "5"}} {
		code, stdout, _ := runTest(t, append([]string{"packs", "test", "--config", "does-not-exist.json", "--format", "json"}, args...), "")
		if code != result.ExitInvocation || !strings.Contains(stdout, "JPS-INVOCATION-STRENGTH-OPTION") {
			t.Fatalf("%v: exit=%d %s", args, code, stdout)
		}
	}
	for _, level := range []string{"0", "-3"} {
		code, stdout, _ := runTest(t, []string{"packs", "test", "--config", "does-not-exist.json", "--format", "json", "--strength", "--max-variants", level}, "")
		if code != result.ExitInvocation || !strings.Contains(stdout, "JPS-INVOCATION-MAX-VARIANTS") {
			t.Fatalf("level %s: exit=%d %s", level, code, stdout)
		}
	}
}

// Without --strength the payload carries no strength member at all, so a run
// that did not ask writes what it always wrote.
func TestNoStrengthMemberWithoutTheOption(t *testing.T) {
	_, report, stdout := runPackTest(t, strengthProject(t, false))
	if report.Strength != nil || report.Packs[0].Strength != nil || strings.Contains(stdout, `"strength":`) {
		t.Fatalf("a run without --strength carries strength: %s", stdout)
	}
	_, text, _ := runTest(t, []string{"packs", "test", "--config", strengthProject(t, false)}, "")
	if strings.Contains(text, "strength:") || strings.Contains(text, "test strength") {
		t.Fatalf("text output mentions strength without --strength:\n%s", text)
	}
}

// A pack with no matrix runs no variant and is untouched by
// --fail-on-survivor, as ADR-0080 leaves it to --require-matrix.
func TestStrengthLeavesAPackWithNoMatrixToRequireMatrix(t *testing.T) {
	config := writeProjectFixture(t, `{"configVersion":"1","packs":{"bare":{"path":"p.json"},"expense":{"path":"q.json","matrix":"m.json"}}}`, map[string]string{
		"p.json": strengthPack,
		"q.json": strengthPack,
		"m.json": `{"matrixVersion":"1","cases":[
		  {"id":"under","facts":{"expense":{"amount":"4999"}},"expectedDisposition":` + strengthAutoApprove + `},
		  {"id":"at","facts":{"expense":{"amount":"5000"}},"expectedDisposition":` + strengthReview + `},
		  {"id":"unknown","facts":{"expense":{}},"expectedDisposition":` + strengthUnknown + `}]}`,
	})
	code, report, _ := runPackTest(t, config, "--strength", "--fail-on-survivor")
	bare := report.Packs[0]
	if code != result.ExitSuccess || bare.ID != "bare" || bare.Status != "skipped" || len(bare.UnmetRequirements) != 0 ||
		bare.Strength == nil || bare.Strength.Status != result.StrengthSkipped || bare.Strength.Detail != "The entry declares no matrix, so no variant ran." {
		t.Fatalf("exit=%d bare=%+v strength=%+v", code, bare, bare.Strength)
	}
}

// The run reads the pack and its matrix and writes nothing: every file under
// the project is byte-identical afterwards and no file appears or goes.
func TestStrengthWritesNothingBesideThePack(t *testing.T) {
	config := strengthProject(t, false)
	root := filepath.Dir(config)
	before := hashTree(t, root)
	if code, _, _ := runPackTest(t, config, "--strength"); code != result.ExitSuccess {
		t.Fatalf("exit=%d", code)
	}
	if code, _, _ := runTest(t, []string{"packs", "test", "--config", config, "--strength", "--fail-on-survivor"}, ""); code != result.ExitInvalid {
		t.Fatalf("exit=%d", code)
	}
	after := hashTree(t, root)
	if !slices.Equal(before, after) {
		t.Fatalf("the project changed:\nbefore %v\nafter  %v", before, after)
	}
}

func hashTree(t *testing.T, root string) []string {
	t.Helper()
	var listing []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		if entry.IsDir() {
			listing = append(listing, relative+"/")
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(body)
		listing = append(listing, relative+" "+hex.EncodeToString(digest[:]))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return listing
}
