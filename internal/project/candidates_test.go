package project

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// walkFixture is one pack the walk-invariance golden is taken over.
type walkFixture struct {
	name string
	pack map[string]any
}

// walkFixtures is the battery the golden covers: every pack document this
// repository ships that states an ordered comparison — the three the evaluator
// and graph packages carry as testdata and the five bundled JPS artifacts,
// across both draft versions — plus the synthetic shapes the boundary tests
// exercise: nesting through all, any, and not; a condition-shaped object
// carried as data; two spellings of one value at one pointer; a comparison at
// each of §8's three condition sites; an unnamed declaration; a non-decimal
// operand.
//
// The bundled artifacts are in the battery because they are the documents this
// repository publishes as what a pack looks like, and two of them
// (invalid-ordered-decimal) exist precisely to state an ordered comparison the
// decimal rule refuses — a shape no synthetic fixture would think to write, and
// one the walk must keep declining to collect.
func walkFixtures(t *testing.T) []walkFixture {
	t.Helper()
	var fixtures []walkFixture
	for _, path := range []string{
		filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"),
		filepath.Join("..", "graph", "testdata", "project", "sanctions-screening-0.1.0.pack.json"),
		filepath.Join("..", "graph", "testdata", "project", "vendor-onboarding-0.1.0.pack.json"),
		filepath.Join("..", "artifacts", "jps", "0.2.0-draft", "evaluation", "packs", "decimal-threshold-fee.json"),
		filepath.Join("..", "artifacts", "jps", "0.2.0-draft", "cases", "valid", "minimal-expense-approval.json"),
		filepath.Join("..", "artifacts", "jps", "0.2.0-draft", "cases", "structural", "invalid-ordered-decimal.json"),
		filepath.Join("..", "artifacts", "jps", "0.1.0-draft", "cases", "valid", "minimal-expense-approval.json"),
		filepath.Join("..", "artifacts", "jps", "0.1.0-draft", "cases", "structural", "invalid-ordered-decimal.json"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		// The whole relative path names the fixture, not its base: two of the
		// bundled artifacts share a filename across the draft versions, and a
		// golden that called both "minimal-expense-approval.json" would not say
		// which one moved.
		fixtures = append(fixtures, walkFixture{name: filepath.ToSlash(path), pack: document})
	}
	return append(fixtures,
		walkFixture{name: "condition-shaped-data", pack: map[string]any{
			"rules": []any{map[string]any{"id": "carries-data", "outcome": "review", "when": map[string]any{
				"op": "fact", "path": "/payload", "operator": "equals",
				"value": map[string]any{"op": "fact", "path": "/hidden", "operator": "greater-than", "value": "9"},
			}}},
		}},
		walkFixture{name: "nested-and-negated", pack: map[string]any{
			"applicability": map[string]any{"op": "not", "condition": map[string]any{
				"op": "fact", "path": "/scope/tier", "operator": "less-than", "value": "2",
			}},
			"rules": []any{map[string]any{"id": "nested", "outcome": "review", "when": map[string]any{
				"op": "all", "conditions": []any{
					map[string]any{"op": "any", "conditions": []any{
						map[string]any{"op": "fact", "path": "/expense/amount", "operator": "greater-than", "value": "5000"},
						map[string]any{"op": "fact", "path": "/expense/amount", "operator": "less-than-or-equal", "value": "70.0"},
					}},
					map[string]any{"op": "fact", "path": "/vendor/riskScore", "operator": "greater-than-or-equal", "value": "70"},
				},
			}}},
			"exceptions": []any{map[string]any{"id": "waiver", "effect": "escalate", "when": map[string]any{
				"op": "fact", "path": "/vendor/riskScore", "operator": "less-than", "value": "70.0",
			}}},
		}},
		walkFixture{name: "two-spellings-one-boundary", pack: map[string]any{
			"rules": []any{
				map[string]any{"id": "coarse", "outcome": "review", "when": map[string]any{
					"op": "fact", "path": "/expense/amount", "operator": "greater-than", "value": "5000",
				}},
				map[string]any{"id": "fine", "outcome": "review", "when": map[string]any{
					"op": "fact", "path": "/expense/amount", "operator": "less-than-or-equal", "value": "5000.0",
				}},
			},
		}},
		walkFixture{name: "unnamed-and-non-decimal", pack: map[string]any{
			"rules": []any{map[string]any{"outcome": "review", "when": map[string]any{
				"op": "fact", "path": "/a", "operator": "greater-than", "value": "5,000",
			}}},
			"exceptions": []any{map[string]any{"effect": "escalate", "when": map[string]any{
				"op": "fact", "path": "/b", "operator": "greater-than", "value": "-3.500",
			}}},
		}},
	)
}

// walkGolden is the exact rendering of comparisonSites over walkFixtures,
// captured from the recursion as ADR-0023 shipped it and before ADR-0024
// parameterized that recursion's leaf. It was taken by building that recursion
// from `git show HEAD:internal/project/coverage.go` in a scratch tree and
// running this same rendering over this same battery, so the lines below are
// the old implementation's output and not the new one's restated.
//
// It is here because widening the ordered walk would widen a DEMAND — a probe
// the coverage report says no row satisfies — and the generator needed the same
// enumeration to make an OFFER. Splitting the walk into an enumeration half and
// a leaf half is what lets the second reader take the enumeration without
// touching what the first collects, and this golden is what holds the split to
// a pure refactor: the sites collected, in order, byte for byte unchanged.
const walkGolden = `# ../evaluation/testdata/data-request-intake-triage.json
# ../graph/testdata/project/sanctions-screening-0.1.0.pack.json
/screening/matches|1|1|greater-than-or-equal|rule "hit-when-match"|2
# ../graph/testdata/project/vendor-onboarding-0.1.0.pack.json
# ../artifacts/jps/0.2.0-draft/evaluation/packs/decimal-threshold-fee.json
/request/amount|1000|1000|greater-than-or-equal|rule "standard-fee-at-or-above-threshold"|2
/request/amount|1000|1000|less-than|rule "reduced-fee-below-threshold"|2
# ../artifacts/jps/0.2.0-draft/cases/valid/minimal-expense-approval.json
/expense/amount|5000|5000|greater-than|rule "large-expense"|2
/expense/amount|5000|5000|less-than-or-equal|rule "ordinary-expense"|2
# ../artifacts/jps/0.2.0-draft/cases/structural/invalid-ordered-decimal.json
# ../artifacts/jps/0.1.0-draft/cases/valid/minimal-expense-approval.json
/expense/amount|5000|5000|greater-than|rule "large-expense"|2
/expense/amount|5000|5000|less-than-or-equal|rule "ordinary-expense"|2
# ../artifacts/jps/0.1.0-draft/cases/structural/invalid-ordered-decimal.json
# condition-shaped-data
# nested-and-negated
/scope/tier|2|2|less-than|applicability|0
/expense/amount|5000|5000|greater-than|rule "nested"|2
/expense/amount|70.0|70|less-than-or-equal|rule "nested"|2
/vendor/riskScore|70|70|greater-than-or-equal|rule "nested"|2
/vendor/riskScore|70.0|70|less-than|exception "waiver"|1
# two-spellings-one-boundary
/expense/amount|5000|5000|greater-than|rule "coarse"|2
/expense/amount|5000.0|5000|less-than-or-equal|rule "fine"|2
# unnamed-and-non-decimal
/b|-3.500|-7/2|greater-than|exception 0|1
`

func TestTheOrderedWalkIsUnchangedByTheLeafParameterization(t *testing.T) {
	var rendered strings.Builder
	for _, fixture := range walkFixtures(t) {
		fmt.Fprintf(&rendered, "# %s\n", fixture.name)
		for _, site := range comparisonSites(fixture.pack) {
			fmt.Fprintf(&rendered, "%s|%s|%s|%s|%s|%d\n", site.path, site.literal, site.key, site.operator, site.owner, site.stage)
		}
	}
	if rendered.String() != walkGolden {
		t.Fatalf("the ordered walk moved; a widened demand is not a refactor.\ngot:\n%s\nwant:\n%s", rendered.String(), walkGolden)
	}
}

// thresholdPack is the motivating shape: one threshold, one pointer, one rule
// whose prose and whose operator disagree at exactly one input.
const thresholdPack = `{
  "specVersion": "0.2.0-draft",
  "id": "expense-approval",
  "version": "0.1.0",
  "title": "Expense approval",
  "description": "Decide whether an expense needs manager signoff.",
  "decision": {"question": "Does this expense need manager signoff?", "subject": "expense"},
  "outcomes": [{"id": "requires-signoff", "label": "Requires signoff"}, {"id": "auto-approve", "label": "Auto approve"}],
  "rules": [
    {"id": "over-threshold", "description": "5000 or more spend requires review",
     "when": {"op": "fact", "path": "/expense/amountUsd", "operator": "greater-than", "value": "5000"},
     "outcome": "requires-signoff", "onUnknown": "escalate"}
  ],
  "fallbackOutcome": "auto-approve"
}`

// suggestProject lays out a one-pack project and returns the loaded project.
func suggestProject(t *testing.T, pack string, files map[string]string, entry string) *Project {
	t.Helper()
	all := map[string]string{"packs/pack.json": pack}
	for name, body := range files {
		all[name] = body
	}
	return mustLoad(t, writeProject(t, `{"configVersion":"1","packs":{"expense":`+entry+`}}`, all))
}

// suggestOne derives one pack's candidates and fails the test on a refusal.
func suggestOne(t *testing.T, loaded *Project, options SuggestOptions) Candidates {
	t.Helper()
	_, document, failure := loaded.Suggest(options, "packs suggest")
	if failure != nil {
		t.Fatalf("suggest: %s: %s", failure.Code, failure.Message)
	}
	return document
}

// latticeSet is the accumulator the dimension tests exercise in isolation,
// built the one way production builds one: with the run's own byte budget
// attached, so no test exercises a set that composes an unbounded document.
func latticeSet() *candidateSet {
	return newCandidateSet("d", nil, false, SuggestOptions{Max: MaxCandidatesUnset},
		&outputBudget{limit: DefaultMaxOutputBytes}, &candidateCap{limit: DefaultMaxCandidates})
}

// hugSet is latticeSet with --include-hugs, for the clamp's own correctness
// cases: the pair the flag names is exact only below the generator's step
// floor, and what the floor swallows has to be reported rather than delivered
// quietly.
func hugSet() *candidateSet {
	return newCandidateSet("d", nil, false, SuggestOptions{Max: MaxCandidatesUnset, IncludeHugs: true},
		&outputBudget{limit: DefaultMaxOutputBytes}, &candidateCap{limit: DefaultMaxCandidates})
}

// candidateFacts indexes one document's candidates by id.
func candidateFacts(document Candidates) map[string]string {
	facts := map[string]string{}
	for _, candidate := range document.Candidates {
		facts[candidate.ID] = string(candidate.Facts)
	}
	return facts
}

// The step is one unit at the precision the literal was authored in — not a
// fixed 0.01. "5000" steps by 1 and "70.0" by 0.1, and a literal finer than the
// generator steps at is clamped with the rationale saying so rather than
// implying the value is the pack's own next distinguishable one.
func TestTheStepIsOneUnitAtTheAuthoredPrecision(t *testing.T) {
	for _, row := range []struct {
		literal string
		want    []string
	}{
		{"5000", []string{"4999", "5000", "5001"}},
		{"70.0", []string{"69.9", "70.0", "70.1"}},
		{"0.5", []string{"0.4", "0.5", "0.6"}},
		{"-3.50", []string{"-3.51", "-3.50", "-3.49"}},
	} {
		pack := map[string]any{
			"outcomes": []any{map[string]any{"id": "review"}},
			"rules": []any{map[string]any{"id": "one", "outcome": "review", "when": map[string]any{
				"op": "fact", "path": "/p", "operator": "greater-than", "value": row.literal,
			}}},
			"fallbackOutcome": "review",
		}
		set := latticeSet()
		set.deriveValues(pack)
		var got []string
		for _, candidate := range set.candidates {
			var facts struct {
				P string `json:"p"`
			}
			if err := json.Unmarshal(candidate.Facts, &facts); err != nil {
				t.Fatal(err)
			}
			got = append(got, facts.P)
		}
		// One literal derives at + two steps; the outer edges land on the steps
		// only where the step is a whole unit, so the set is deduplicated by the
		// evaluator's own decimal identity rather than by spelling.
		for _, want := range row.want {
			if !slices.Contains(got, want) {
				t.Fatalf("literal %q must derive %q: %v", row.literal, want, got)
			}
		}
		// The literal's own authored spelling survives: "70.0" is the probe
		// point ADR-0023 names, and rewriting it to "70" would emit a value the
		// pack does not state.
		if !slices.Contains(got, row.literal) {
			t.Fatalf("the authored spelling of %q must be emitted as-is: %v", row.literal, got)
		}
	}
}

// A literal authored finer than the generator steps at is clamped, and the
// rationale says the value is not the pack's own next distinguishable one.
func TestAFinerPrecisionThanTheGeneratorStepsAtIsClampedAndSaidSo(t *testing.T) {
	if digits, clamped := stepPrecision("0.123456"); digits != 6 || clamped {
		t.Fatalf("six digits is the last unclamped precision: %d %v", digits, clamped)
	}
	digits, clamped := stepPrecision("0.1234567")
	if digits != maxStepPrecision || !clamped {
		t.Fatalf("seven digits clamps: %d %v", digits, clamped)
	}
	pack := map[string]any{
		"outcomes": []any{map[string]any{"id": "review"}},
		"rules": []any{map[string]any{"id": "one", "outcome": "review", "when": map[string]any{
			"op": "fact", "path": "/p", "operator": "less-than", "value": "0.12345678",
		}}},
	}
	set := latticeSet()
	set.deriveValues(pack)
	said := false
	var values []string
	for _, candidate := range set.candidates {
		if strings.Contains(candidate.Rationale, "not the pack's own next distinguishable value") {
			said = true
		}
		var facts struct {
			P string `json:"p"`
		}
		if err := json.Unmarshal(candidate.Facts, &facts); err != nil {
			t.Fatal(err)
		}
		values = append(values, facts.P)
	}
	if !said {
		t.Fatal("a clamped step must say so in the rationale rather than pass as the authored precision")
	}
	// The clamped step is still exact arithmetic at 10^-6, never a rounding of
	// the authored value: 0.12345678 ∓ 0.000001.
	for _, want := range []string{"0.12345578", "0.12345678", "0.12345778"} {
		if !slices.Contains(values, want) {
			t.Fatalf("the clamped step is exact at 10^-%d: %v", maxStepPrecision, values)
		}
	}
}

// A midpoint of two adjacent literals is always itself a §2.2 decimal — 2
// divides 10 — which is why midpoints invent no granularity where "adjacent"
// would. The generator emits them; it never rounds one.
func TestMidpointsTerminateAndAreEmitted(t *testing.T) {
	pack := map[string]any{
		"outcomes": []any{map[string]any{"id": "review"}},
		"rules": []any{
			map[string]any{"id": "low", "outcome": "review", "when": map[string]any{"op": "fact", "path": "/p", "operator": "less-than", "value": "40"}},
			map[string]any{"id": "mid", "outcome": "review", "when": map[string]any{"op": "fact", "path": "/p", "operator": "greater-than", "value": "41"}},
			map[string]any{"id": "high", "outcome": "review", "when": map[string]any{"op": "fact", "path": "/p", "operator": "greater-than-or-equal", "value": "70.5"}},
		},
	}
	set := latticeSet()
	set.deriveValues(pack)
	var values []string
	for _, candidate := range set.candidates {
		var facts struct {
			P string `json:"p"`
		}
		if err := json.Unmarshal(candidate.Facts, &facts); err != nil {
			t.Fatal(err)
		}
		values = append(values, facts.P)
	}
	// (40+41)/2 and (41+70.5)/2 are both terminating decimals, and both are
	// values no literal names.
	for _, want := range []string{"40.5", "55.75"} {
		if !slices.Contains(values, want) {
			t.Fatalf("the interior midpoints must be emitted exactly: %v", values)
		}
	}
	// The outer edges are one unit beyond the outermost literals.
	for _, want := range []string{"39", "71.5"} {
		if !slices.Contains(values, want) {
			t.Fatalf("the outer edges must be emitted: %v", values)
		}
	}
	// Nothing is emitted twice, judged by the evaluator's decimal identity and
	// not by spelling, and the lattice stays inside its 4n+1 bound.
	if len(values) != len(slices.Compact(slices.Sorted(slices.Values(values)))) {
		t.Fatalf("values must be deduplicated by decimal identity: %v", values)
	}
	if len(values) > 4*3+1 {
		t.Fatalf("the per-pointer lattice is bounded at 4n+1: %v", values)
	}
	// Ordered along the number line, which is how a reviewer reads them.
	if !slices.IsSortedFunc(values, func(left, right string) int {
		leftValue, leftOK := evaluation.DecimalValue(left)
		rightValue, rightOK := evaluation.DecimalValue(right)
		if !leftOK || !rightOK {
			t.Fatalf("every emitted value is a §2.2 decimal: %q %q", left, right)
		}
		return leftValue.Cmp(rightValue)
	}) {
		t.Fatalf("one pointer's candidates are ordered by value: %v", values)
	}
}

// Two spellings of one value at one pointer derive one lattice, not two: the
// grouping is ADR-0023's own, by pointer and decimal value.
func TestOneValueSpelledTwoWaysIsOneLattice(t *testing.T) {
	pack := map[string]any{
		"outcomes": []any{map[string]any{"id": "review"}},
		"rules": []any{
			map[string]any{"id": "a", "outcome": "review", "when": map[string]any{"op": "fact", "path": "/p", "operator": "greater-than", "value": "70"}},
			map[string]any{"id": "b", "outcome": "review", "when": map[string]any{"op": "fact", "path": "/p", "operator": "less-than-or-equal", "value": "70.0"}},
		},
	}
	set := latticeSet()
	set.deriveValues(pack)
	var values []string
	for _, candidate := range set.candidates {
		var facts struct {
			P string `json:"p"`
		}
		if err := json.Unmarshal(candidate.Facts, &facts); err != nil {
			t.Fatal(err)
		}
		values = append(values, facts.P)
	}
	if slices.Contains(values, "70") && slices.Contains(values, "70.0") {
		t.Fatalf("one value spelled twice is one boundary: %v", values)
	}
}

// One value spelled two ways is one boundary, and the step it derives comes
// from the FINEST precision any of its spellings was authored at — not from
// whichever rule happens to be declared first.
//
// The lattice is compared by the evaluator's own decimal identity rather than
// by the emitted spellings, and that is the honest comparison rather than a
// weaker one: the at-literal candidate deliberately carries the first-authored
// spelling, because that spelling is ADR-0023's probe point and rewriting it
// would emit a value the pack does not state. What must not move when two rules
// are reordered is the set of *values* offered — a policy that reads the same
// cannot derive a different lattice.
func TestTheStepPrecisionComesFromTheFinestSpellingAndNotTheFirst(t *testing.T) {
	rule := func(id, operator, literal string) any {
		return map[string]any{"id": id, "outcome": "review", "when": map[string]any{
			"op": "fact", "path": "/p", "operator": operator, "value": literal,
		}}
	}
	coarse, fine := rule("coarse", "greater-than", "5000"), rule("fine", "less-than-or-equal", "5000.0")
	lattice := func(rules ...any) (string, []string) {
		set := latticeSet()
		set.deriveValues(map[string]any{
			"outcomes": []any{map[string]any{"id": "review"}},
			"rules":    rules,
		})
		var rendered strings.Builder
		var texts []string
		for _, candidate := range set.candidates {
			var facts struct {
				P string `json:"p"`
			}
			if err := json.Unmarshal(candidate.Facts, &facts); err != nil {
				t.Fatal(err)
			}
			key, decimal := evaluation.DecimalKey(facts.P)
			if !decimal {
				t.Fatalf("every emitted value is a §2.2 decimal: %q", facts.P)
			}
			fmt.Fprintf(&rendered, "%s\n", key)
			texts = append(texts, facts.P)
		}
		return rendered.String(), texts
	}
	first, firstTexts := lattice(coarse, fine)
	second, secondTexts := lattice(fine, coarse)
	if first != second {
		t.Fatalf("reordering two rules must not change the lattice:\n%s\nversus\n%s", first, second)
	}
	// The step is one tenth in both orderings, because a tenth is the finest
	// precision this policy wrote the value at. Under the first-spelling rule the
	// "5000"-first ordering stepped by a whole unit and derived neither.
	for index, texts := range [][]string{firstTexts, secondTexts} {
		for _, want := range []string{"4999.9", "5000.1"} {
			if !slices.Contains(texts, want) {
				t.Fatalf("ordering %d must step at 10^-1: %v", index, texts)
			}
		}
	}
	// Precision is read over every spelling the boundary's sites carry, and the
	// clamp is the disjunction: one spelling finer than the generator steps at
	// makes the step a clamped one whichever spelling the group carries.
	if digits, clamped := groupStepPrecision(boundaryGroup{literal: "5000", sites: []comparisonSite{
		{literal: "5000"}, {literal: "5000.0"}, {literal: "5000.00"},
	}}); digits != 2 || clamped {
		t.Fatalf("the finest authored precision wins, unclamped: %d %v", digits, clamped)
	}
	if digits, clamped := groupStepPrecision(boundaryGroup{literal: "5000.0", sites: []comparisonSite{
		{literal: "5000.0"}, {literal: "5000.0000000"},
	}}); digits != maxStepPrecision || !clamped {
		t.Fatalf("a spelling finer than the generator steps at clamps the group: %d %v", digits, clamped)
	}
}

// No candidate varies more than one member of the base: a value or membership
// candidate moves exactly one pointer and holds every other member at what the
// reviewed row said, an absence candidate withholds exactly one, and an
// evidence candidate moves none at all. That is what makes a candidate read as
// "this reviewed row, with one thing changed" rather than as a policy world the
// generator invented.
func TestEveryCandidateVariesExactlyOnePointerOfTheBase(t *testing.T) {
	matrix := `{"matrixVersion":"1","cases":[{"id":"reviewed","facts":{"expense":{"amountUsd":"10","category":"travel"},"employee":{"costCentre":"R&D"}},"expectedDisposition":{"kind":"outcome","outcomeId":"auto-approve","reasons":[],"handoff":{"state":"none"}}}]}`
	pack := strings.Replace(thresholdPack,
		`"fallbackOutcome": "auto-approve"`,
		`"fallbackOutcome": "auto-approve", "exceptions": [{"id":"cc","effect":"escalate","when":{"op":"fact","path":"/employee/costCentre","operator":"equals","value":"R&D"},"onUnknown":"escalate"}]`, 1)
	loaded := suggestProject(t, pack, map[string]string{"packs/pack.matrix.json": matrix},
		`{"path":"packs/pack.json","matrix":"packs/pack.matrix.json"}`)
	document := suggestOne(t, loaded, SuggestOptions{ID: "expense", BaseRow: "reviewed", Max: MaxCandidatesUnset})
	if len(document.Candidates) == 0 {
		t.Fatal("the base row's pack derives candidates")
	}
	for _, candidate := range document.Candidates {
		var facts map[string]any
		if err := json.Unmarshal(candidate.Facts, &facts); err != nil {
			t.Fatal(err)
		}
		differences := 0
		amount, _ := facts["expense"].(map[string]any)
		employee, _ := facts["employee"].(map[string]any)
		if amount == nil || amount["amountUsd"] != "10" {
			differences++
		}
		if amount == nil || amount["category"] != "travel" {
			differences++
		}
		if employee == nil || employee["costCentre"] != "R&D" {
			differences++
		}
		if differences > 1 {
			t.Fatalf("candidate %q varies %d members of the base; one factor at a time means one: %s", candidate.ID, differences, candidate.Facts)
		}
	}
}

// Nothing the generator emits is a row. The document's root is not a matrix's,
// and a candidate lifted into a cases array carries no expectation — so the
// matrix loader refuses it twice over, through refusals that already existed.
func TestNoCandidateLoadsAsARowUntilAnExpectationIsAuthored(t *testing.T) {
	loaded := suggestProject(t, thresholdPack, nil, `{"path":"packs/pack.json"}`)
	document := suggestOne(t, loaded, SuggestOptions{Max: MaxCandidatesUnset})
	encoded, err := EncodeCandidates(document)
	if err != nil {
		t.Fatal(err)
	}
	// Layer 1: the emitted document is not a matrix. Point a configuration's
	// matrix path at it and the loader refuses the root members.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{
		"packs/pack.json":         []byte(thresholdPack),
		"packs/candidates.json":   encoded,
		string(DefaultConfigName): []byte(`{"configVersion":"1","packs":{"expense":{"path":"packs/pack.json","matrix":"packs/candidates.json"}}}`),
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	aimed := mustLoad(t, filepath.Join(root, DefaultConfigName))
	_, err = aimed.LoadMatrix(aimed.Config.Packs["expense"])
	if err == nil || !strings.Contains(err.Error(), "member this runtime does not know") {
		t.Fatalf("a candidate document is not a matrix, and the loader must say so: %v", err)
	}

	// Layer 2: a candidate pasted VERBATIM carries its rationale, which is a
	// member of no row, so strict decoding refuses the whole matrix before any
	// row is examined. That refusal comes first, and it is its own layer: the
	// generator's prose cannot ride into anything that gets scored.
	var pasted []map[string]any
	for _, candidate := range document.Candidates[:1] {
		var row map[string]any
		encodedRow, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encodedRow, &row); err != nil {
			t.Fatal(err)
		}
		if _, present := row["expectedDisposition"]; present {
			t.Fatal("a candidate must carry no expectedDisposition, not even an empty one")
		}
		if _, present := row["expectedErrorClass"]; present {
			t.Fatal("a candidate must carry no expectedErrorClass, not even a sentinel")
		}
		if _, present := row["rationale"]; !present {
			t.Fatal("a candidate carries the sentence saying what is still owed")
		}
		pasted = append(pasted, row)
	}
	pasteInto := func(name string, rows []map[string]any) error {
		cases, err := json.Marshal(map[string]any{"matrixVersion": "1", "cases": rows})
		if err != nil {
			t.Fatal(err)
		}
		loadedPaste := mustLoad(t, writeProject(t, `{"configVersion":"1","packs":{"expense":{"path":"packs/pack.json","matrix":"packs/`+name+`.json"}}}`, map[string]string{
			"packs/pack.json":         thresholdPack,
			"packs/" + name + ".json": string(cases),
		}))
		_, err = loadedPaste.LoadMatrix(loadedPaste.Config.Packs["expense"])
		return err
	}
	err = pasteInto("verbatim", pasted)
	if err == nil || !strings.Contains(err.Error(), "member this runtime does not know") ||
		!strings.Contains(err.Error(), "rationale") {
		t.Fatalf("a verbatim paste is refused first for the rationale, a member of no row: %v", err)
	}
	if strings.Contains(err.Error(), "must declare exactly one of") {
		t.Fatalf("the missing-expectation refusal comes second, not first: %v", err)
	}

	// Layer 3: with the rationale removed the row declares neither expectation,
	// and THAT is the refusal naming the work still to do.
	for index := range pasted {
		delete(pasted[index], "rationale")
	}
	err = pasteInto("pasted", pasted)
	if err == nil || !strings.Contains(err.Error(), "must declare exactly one of expectedDisposition and expectedErrorClass") {
		t.Fatalf("a candidate without its rationale must fail closed with the message naming the missing work: %v", err)
	}
	// The origin member, in contrast, loads: it is provenance the row carries
	// into the report, never a gate (ADR-0024).
	for index := range pasted {
		pasted[index]["expectedDisposition"] = json.RawMessage(`{"kind":"outcome","outcomeId":"auto-approve","reasons":[],"handoff":{"state":"none"}}`)
	}
	authored, err := json.Marshal(map[string]any{"matrixVersion": "1", "cases": pasted})
	if err != nil {
		t.Fatal(err)
	}
	third := mustLoad(t, writeProject(t, `{"configVersion":"1","packs":{"expense":{"path":"packs/pack.json","matrix":"packs/authored.json"}}}`, map[string]string{
		"packs/pack.json":     thresholdPack,
		"packs/authored.json": string(authored),
	}))
	loadedMatrix, err := third.LoadMatrix(third.Config.Packs["expense"])
	if err != nil {
		t.Fatalf("once the expectation is authored the row loads: %v", err)
	}
	origins := MatrixOrigins(loadedMatrix)
	if len(origins) != 1 || origins[0].Origin != CandidateOrigin || origins[0].Rows != 1 {
		t.Fatalf("the origin is counted, not gated: %+v", origins)
	}
}

// Two runs over one unchanged pack write identical bytes. A generator that
// reshuffled its own output would make a re-run's diff say something changed
// when nothing did, which is the opposite of what a reviewable artifact does.
func TestTwoRunsWriteIdenticalBytes(t *testing.T) {
	loaded := suggestProject(t, thresholdPack, nil, `{"path":"packs/pack.json"}`)
	first, err := EncodeCandidates(suggestOne(t, loaded, SuggestOptions{IncludeHugs: true, Max: MaxCandidatesUnset}))
	if err != nil {
		t.Fatal(err)
	}
	for range 8 {
		again, err := EncodeCandidates(suggestOne(t, loaded, SuggestOptions{IncludeHugs: true, Max: MaxCandidatesUnset}))
		if err != nil {
			t.Fatal(err)
		}
		if string(again) != string(first) {
			t.Fatal("the candidate document must be byte-identical on every run")
		}
	}
}

// Past the cap the run refuses and names the flag. A truncated candidate set
// looks exactly like a complete one, and a reviewer cannot tell that the
// dimensions past the cut were never offered.
//
// The cap is charged as candidates are composed, not read off the finished
// document, and that is what makes "lower --max" advice that changes anything:
// a cap checked at the end bounds what is returned and nothing about the work
// done to reach it. So the refusal names the cap rather than a total — the
// total is exactly what a run stopped at the cap did not go on to find out, and
// a number it did not measure is not a number it may report.
func TestTheCapRefusesRatherThanTruncates(t *testing.T) {
	loaded := suggestProject(t, thresholdPack, nil, `{"path":"packs/pack.json"}`)
	whole := suggestOne(t, loaded, SuggestOptions{Max: MaxCandidatesUnset})
	bound := len(whole.Candidates) - 1
	_, document, failure := loaded.Suggest(SuggestOptions{Max: bound}, "packs suggest")
	if failure == nil || !strings.Contains(failure.Message, "--max") {
		t.Fatalf("past the cap the run refuses and names the flag: %+v", failure)
	}
	if !strings.Contains(failure.Message, "nothing was written") {
		t.Fatalf("the refusal must say nothing was written: %s", failure.Message)
	}
	if !strings.Contains(failure.Message, strconv.Itoa(bound)) {
		t.Fatalf("the refusal names the cap the run stopped at: %s", failure.Message)
	}
	if strings.Contains(failure.Message, strconv.Itoa(len(whole.Candidates))) {
		t.Fatalf("a run stopped at the cap never counted the rest, so it states no total: %s", failure.Message)
	}
	if len(document.Candidates) != 0 {
		t.Fatalf("a refused run returns no partial document: %d candidate(s)", len(document.Candidates))
	}
	if _, _, failure := loaded.Suggest(SuggestOptions{Max: len(whole.Candidates)}, "packs suggest"); failure != nil {
		t.Fatalf("exactly at the cap is inside it: %+v", failure)
	}
	// The bound is on the WORK and not only on the answer: a cap of one stops
	// after one candidate is composed, whatever the pack would go on to derive.
	set := newCandidateSet("d", nil, false, SuggestOptions{Max: 1},
		&outputBudget{limit: DefaultMaxOutputBytes}, &candidateCap{limit: 1})
	set.deriveValues(map[string]any{
		"outcomes": []any{map[string]any{"id": "review"}},
		"rules": []any{map[string]any{"id": "one", "outcome": "review", "when": map[string]any{
			"op": "fact", "path": "/p", "operator": "greater-than", "value": "5000",
		}}},
	})
	if len(set.candidates) != 1 || !set.stopped() {
		t.Fatalf("the derivation stops at the cap rather than running on: %d composed, stopped=%v", len(set.candidates), set.stopped())
	}
}

// A pack stating a draft-RFC collection quantifier is reported as skipped, never
// silently derived-around: a candidate set that quietly left a dimension out
// would read as the whole derivable set.
func TestAQuantifierPackIsReportedSkippedAndNeverSilentlyOmitted(t *testing.T) {
	quantifier := `{
	  "specVersion": "0.2.0-draft", "id": "q", "version": "0.1.0", "title": "Q",
	  "description": "A pack using a draft collection quantifier.",
	  "decision": {"question": "Any line over the limit?", "subject": "order"},
	  "outcomes": [{"id": "review", "label": "Review"}],
	  "rules": [{"id": "any-line", "outcome": "review", "onUnknown": "escalate",
	    "when": {"op": "exists", "path": "/lines", "where": {"op": "fact", "path": "/amount", "operator": "greater-than", "value": "100"}}}],
	  "fallbackOutcome": "review"
	}`
	loaded := suggestProject(t, quantifier, nil, `{"path":"packs/pack.json"}`)
	report, document, failure := loaded.Suggest(SuggestOptions{Max: MaxCandidatesUnset}, "packs suggest")
	if failure != nil {
		t.Fatalf("suggest: %s", failure.Message)
	}
	if len(document.Candidates) != 0 {
		t.Fatalf("a quantifier's element-relative comparison derives no candidate: %+v", document.Candidates)
	}
	if len(report.Packs) != 1 || len(report.Packs[0].Skipped) == 0 {
		t.Fatalf("the skipped dimension must be reported: %+v", report.Packs)
	}
	if report.Packs[0].Skipped[0].Name != "draft-rfc-quantifiers" {
		t.Fatalf("the skip must be named: %+v", report.Packs[0].Skipped)
	}
	if report.Status != "skipped" {
		t.Fatalf("a run that derived nothing is skipped, never passed: %q", report.Status)
	}
}

// The root pointer and an unplaceable base member are refused with the reason
// reported, rather than silently producing a candidate that varies more of the
// base than one pointer.
func TestUnplaceablePointersAreReportedRatherThanForced(t *testing.T) {
	set := latticeSet()
	set.deriveValues(map[string]any{
		"rules": []any{map[string]any{"id": "root", "outcome": "review", "when": map[string]any{
			"op": "fact", "path": "", "operator": "greater-than", "value": "5",
		}}},
	})
	if len(set.candidates) != 0 {
		t.Fatalf("nothing can be placed beside a root comparison: %+v", set.candidates)
	}
	if skips := set.skipped(); len(skips) != 1 || skips[0].Name != "root-pointer" {
		t.Fatalf("the root pointer is reported as skipped: %+v", skips)
	}

	// A base whose member is a scalar where the pointer needs a container: the
	// placement is refused rather than overwriting the scalar.
	if _, _, ok := placeFact(map[string]any{"expense": "flat"}, true, "/expense/amountUsd", "5000"); ok {
		t.Fatal("overwriting a scalar would change the base beyond the one varied pointer")
	}
	// An explicit JSON null is a value the base STATES, not an absence: growing
	// an object under it would edit a stated answer rather than vary a pointer.
	if _, _, ok := placeFact(map[string]any{"expense": nil}, true, "/expense/amountUsd", "5000"); ok {
		t.Fatal("a member stated as null is a stated scalar, not a place to build in")
	}
	// The pointer's own final position is a different question: replacing the
	// null AT the varied pointer is exactly the one factor a candidate varies.
	replaced, _, ok := placeFact(map[string]any{"expense": nil}, true, "/expense", "5000")
	if !ok {
		t.Fatal("the varied pointer's own value is replaced, null or not")
	}
	if rendered, err := encodeCandidateJSON(replaced); err != nil || string(rendered) != `{"expense":"5000"}` {
		t.Fatalf("the varied pointer is placed over the stated null: %s %v", rendered, err)
	}
	// Where the base simply does not state the path, the containers are created.
	placed, _, ok := placeFact(map[string]any{"other": "kept"}, true, "/expense/amountUsd", "5000")
	if !ok {
		t.Fatal("a path the base does not state is created as objects")
	}
	rendered, err := encodeCandidateJSON(placed)
	if err != nil {
		t.Fatal(err)
	}
	if string(rendered) != `{"expense":{"amountUsd":"5000"},"other":"kept"}` {
		t.Fatalf("the rest of the base is preserved verbatim: %s", rendered)
	}
	// An array position is descended into by index and never renumbered.
	placed, _, ok = placeFact(map[string]any{"lines": []any{map[string]any{"amount": "1"}}}, true, "/lines/0/amount", "5000")
	if !ok {
		t.Fatal("an array index in a pointer addresses an element of the base")
	}
	rendered, _ = encodeCandidateJSON(placed)
	if string(rendered) != `{"lines":[{"amount":"5000"}]}` {
		t.Fatalf("an array element is replaced in place: %s", rendered)
	}
	// A token this runtime's own RFC 6901 resolution refuses addresses no
	// element, whatever strconv.Atoi would make of it. Placing there would put
	// the value where no condition ever reads.
	for _, token := range []string{"00", "+0", "-", "01", " 0"} {
		if _, bad, ok := placeFact(map[string]any{"lines": []any{map[string]any{"amount": "1"}}}, true,
			"/lines/"+token+"/amount", "5000"); ok || bad != token {
			t.Fatalf("the array token %q is not an index: placed=%v token=%q", token, ok, bad)
		}
	}
}

// The absence axis declines a withholding for two structurally different
// reasons, and only one of them is a declined dimension.
//
// A JSON array on the pointer's path is the declined one: an array member cannot
// be withheld without renumbering every element past it, which changes more of
// the base than the one answer a withholding withholds. That is the absence
// axis's counterpart to emit()'s unplaceable pointer, and like it, it is reported
// under its own name — a pack comparing an array element is an ordinary pack, so
// a bare drop here would leave a whole dimension missing in silence, which
// ADR-0024's cited demotion discipline forbids.
//
// A pointer the base row never states is the empty case and correctly silent:
// there is no answer to withhold, the candidate would be the base row under a new
// id, and nothing was declined, so nothing is reported. One boolean conflated the
// two.
func TestAnArrayBlockedWithholdingIsReportedAndAnUnstatedPointerIsSilent(t *testing.T) {
	// One pack and one pointer throughout: what varies across the three runs is
	// only the shape of the base row the withholding is taken from.
	pack := strings.Replace(thresholdPack, "/expense/amountUsd", "/items/0/amount", 1)
	expectation := `"expectedDisposition":{"kind":"outcome","outcomeId":"auto-approve","reasons":[],"handoff":{"state":"none"}}`
	matrix := `{"matrixVersion":"1","cases":[
	  {"id":"through-array","facts":{"items":[{"amount":"1"}]},` + expectation + `},
	  {"id":"through-object","facts":{"items":{"0":{"amount":"1"}}},` + expectation + `},
	  {"id":"pointer-unstated","facts":{"other":"kept"},` + expectation + `}
	]}`
	loaded := suggestProject(t, pack, map[string]string{"packs/pack.matrix.json": matrix},
		`{"path":"packs/pack.json","matrix":"packs/pack.matrix.json"}`)

	for _, row := range []struct {
		base     string
		absence  bool
		declined bool
	}{
		{base: "through-array", absence: false, declined: true},
		{base: "through-object", absence: true, declined: false},
		{base: "pointer-unstated", absence: false, declined: false},
	} {
		report, document, failure := loaded.Suggest(SuggestOptions{ID: "expense", BaseRow: row.base, Max: MaxCandidatesUnset}, "packs suggest")
		if failure != nil {
			t.Fatalf("%s: suggest: %s: %s", row.base, failure.Code, failure.Message)
		}
		if _, present := candidateFacts(document)["suggest:expense:absent:/items/0/amount"]; present != row.absence {
			t.Fatalf("%s: absence witness present=%v, want %v: %v", row.base, present, row.absence, candidateFacts(document))
		}
		// The value lattice is derived from all three bases alike — "5000" and its
		// two steps — so the only difference between the runs is the withholding.
		want := 3
		if row.absence {
			want++
		}
		if len(document.Candidates) != want {
			t.Fatalf("%s: %d candidate(s), want %d: %v", row.base, len(document.Candidates), want, candidateFacts(document))
		}
		if len(report.Packs) != 1 {
			t.Fatalf("%s: one selected pack is reported: %+v", row.base, report.Packs)
		}
		skips := report.Packs[0].Skipped
		if !row.declined {
			if len(skips) != 0 {
				t.Fatalf("%s: nothing was declined, so nothing is reported: %+v", row.base, skips)
			}
			continue
		}
		if len(skips) != 1 || skips[0].Name != "unremovable-pointer" {
			t.Fatalf("%s: the declined dimension is reported under its own name: %+v", row.base, skips)
		}
		if !strings.Contains(skips[0].Detail, "renumbering") {
			t.Fatalf("%s: the reason names the constraint it declined on: %s", row.base, skips[0].Detail)
		}
		if !strings.Contains(skips[0].Detail, "Not derived for: ") || !strings.Contains(skips[0].Detail, "/items/0/amount") {
			t.Fatalf("%s: the pointer it was declined for is named: %s", row.base, skips[0].Detail)
		}
	}
}

// --max bounds a COUNT; the document's size is that count times a base row, and
// a reviewed row is bounded only by MaxMatrixBytes. A wide base multiplied by an
// ordinary candidate count is gigabytes of output nobody asked for, so the run
// refuses at the byte budget with the size, the budget, and the two remedies
// named — and, as everywhere in this family, writes nothing and truncates
// nothing.
func TestAWideBaseRowRefusesAtTheByteBudgetRatherThanMultiplyingItselfOut(t *testing.T) {
	var rules []string
	for index, literal := range []string{"1000", "2000", "3000", "4000", "5000", "6000"} {
		rules = append(rules, fmt.Sprintf(`{"id":"r%d","when":{"op":"fact","path":"/expense/amountUsd","operator":"greater-than","value":%q},"outcome":"requires-signoff","onUnknown":"escalate"}`,
			index, literal))
	}
	pack := strings.Replace(thresholdPack,
		`{"id": "over-threshold", "description": "5000 or more spend requires review",
     "when": {"op": "fact", "path": "/expense/amountUsd", "operator": "greater-than", "value": "5000"},
     "outcome": "requires-signoff", "onUnknown": "escalate"}`,
		strings.Join(rules, ","), 1)
	expectation := `"expectedDisposition":{"kind":"outcome","outcomeId":"auto-approve","reasons":[],"handoff":{"state":"none"}}`
	// One reviewed row a person could plausibly have reviewed — an attachment
	// under the carrier's own per-string bound — and one narrow row beside it.
	wide := strings.Repeat("x", 800*1024)
	matrix := `{"matrixVersion":"1","cases":[
	  {"id":"wide","facts":{"expense":{"amountUsd":"10"},"attachment":"` + wide + `"},` + expectation + `},
	  {"id":"narrow","facts":{"expense":{"amountUsd":"10"}},` + expectation + `}
	]}`
	loaded := suggestProject(t, pack, map[string]string{"packs/pack.matrix.json": matrix},
		`{"path":"packs/pack.json","matrix":"packs/pack.matrix.json"}`)

	_, document, failure := loaded.Suggest(SuggestOptions{ID: "expense", BaseRow: "wide", Max: MaxCandidatesUnset}, "packs suggest")
	if failure == nil {
		t.Fatalf("a base row that multiplies past the byte budget must refuse: %d candidate(s)", len(document.Candidates))
	}
	if failure.Code != "JPS-RESOURCE-SUGGEST-OUTPUT-BYTES" {
		t.Fatalf("the refusal names the resource it is about: %s", failure.Code)
	}
	if len(document.Candidates) != 0 {
		t.Fatalf("a refused run returns no partial document: %d candidate(s)", len(document.Candidates))
	}
	for _, required := range []string{
		strconv.Itoa(DefaultMaxOutputBytes), "nothing was written", "--max", "--base", "would look exactly like a complete one",
	} {
		if !strings.Contains(failure.Message, required) {
			t.Fatalf("the refusal must name %q: %s", required, failure.Message)
		}
	}

	// Ordinary runs are untouched: the same pack over a narrow reviewed row
	// derives its whole lattice, and the document it composes is inside the
	// budget rather than merely under some cut.
	narrow := suggestOne(t, loaded, SuggestOptions{ID: "expense", BaseRow: "narrow", Max: MaxCandidatesUnset})
	if len(narrow.Candidates) > 4*6+1+1 {
		t.Fatalf("one pointer's lattice stays inside 4n+1, plus its absence witness: %d candidate(s)", len(narrow.Candidates))
	}
	// Nothing was cut: both outer edges and an interior midpoint are all there.
	narrowFacts := candidateFacts(narrow)
	for _, id := range []string{
		"suggest:expense:value:/expense/amountUsd:999",
		"suggest:expense:value:/expense/amountUsd:5500",
		"suggest:expense:value:/expense/amountUsd:6001",
	} {
		if _, present := narrowFacts[id]; !present {
			t.Fatalf("an ordinary run offers its whole lattice; %q is missing from %d candidate(s)", id, len(narrow.Candidates))
		}
	}
	encoded, err := EncodeCandidates(narrow)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > DefaultMaxOutputBytes {
		t.Fatalf("a run that was allowed to finish is inside the budget: %d bytes", len(encoded))
	}
}

// The budget bounds the file this run WRITES, and the written form is not the
// composed one: EncodeCandidates indents, so every container level adds two
// spaces to every line beneath it and the multiplier is the base row's nesting
// depth rather than a constant.
//
// The probe class this holds is a base row that is deep rather than wide: a
// hundred nested containers wrapping very many one-character tokens, legal on
// every carrier bound there is — under MaxDepth, under MaxNodes, no string near
// MaxStringBytes — and small enough composed that the whole lattice over it
// charged compact sits comfortably inside 16 MiB. Written, that same lattice is
// hundreds of megabytes. Charging what the writer will write is what makes the
// refusal fire at the first candidate, before any such document exists.
func TestADeeplyNestedBaseRowRefusesOnWhatWouldBeWrittenRatherThanWhatWasComposed(t *testing.T) {
	const levels, tokens = 100, 150_000
	leaves := make([]string, tokens)
	for index := range leaves {
		leaves[index] = `"a"`
	}
	nested := "[" + strings.Join(leaves, ",") + "]"
	for level := 0; level < levels; level++ {
		nested = `{"n":` + nested + `}`
	}
	deep := `{"expense":{"amountUsd":"10"},"deep":` + nested + `}`
	expectation := `"expectedDisposition":{"kind":"outcome","outcomeId":"auto-approve","reasons":[],"handoff":{"state":"none"}}`
	matrix := `{"matrixVersion":"1","cases":[
	  {"id":"deep","facts":` + deep + `,` + expectation + `},
	  {"id":"narrow","facts":{"expense":{"amountUsd":"10"}},` + expectation + `}
	]}`
	loaded := suggestProject(t, thresholdPack, map[string]string{"packs/pack.matrix.json": matrix},
		`{"path":"packs/pack.json","matrix":"packs/pack.matrix.json"}`)

	// The probe is only a probe if a compact charge would have admitted it.
	// thresholdPack states one literal, so the lattice is 4n+1 values plus one
	// absence witness; six candidates at this base's composed size is a small
	// fraction of the budget, and every one of them would have been kept.
	if composed := 6 * len(deep); composed > DefaultMaxOutputBytes/4 {
		t.Fatalf("this base must be one a composed charge would let through: %d composed bytes", composed)
	}

	_, document, failure := loaded.Suggest(SuggestOptions{ID: "expense", BaseRow: "deep", Max: MaxCandidatesUnset}, "packs suggest")
	if failure == nil {
		t.Fatalf("a base row whose indentation multiplies past the budget must refuse: %d candidate(s)", len(document.Candidates))
	}
	if failure.Code != "JPS-RESOURCE-SUGGEST-OUTPUT-BYTES" {
		t.Fatalf("the refusal is the budget's: %s: %s", failure.Code, failure.Message)
	}
	// Nothing was kept and nothing was returned to write: the contract is that
	// the run holds at most the budget plus the one candidate that crossed it,
	// and a document nobody composed is a document nobody can be handed.
	if len(document.Candidates) != 0 {
		t.Fatalf("a refused run returns no partial document: %d candidate(s)", len(document.Candidates))
	}
	for _, required := range []string{
		strconv.Itoa(DefaultMaxOutputBytes), "nothing was written", "indentation included", "--max", "--base",
	} {
		if !strings.Contains(failure.Message, required) {
			t.Fatalf("the refusal must name %q: %s", required, failure.Message)
		}
	}

	// Ordinary runs over the same pack are untouched, and the charge is a bound
	// on the file rather than merely correlated with it: what each candidate is
	// charged, envelope included, covers every byte EncodeCandidates emits.
	narrow := suggestOne(t, loaded, SuggestOptions{ID: "expense", BaseRow: "narrow", Max: MaxCandidatesUnset})
	if len(narrow.Candidates) == 0 {
		t.Fatal("an ordinary reviewed row still derives its whole lattice")
	}
	encoded, err := EncodeCandidates(narrow)
	if err != nil {
		t.Fatal(err)
	}
	charged := 0
	for _, candidate := range narrow.Candidates {
		written, err := candidateWrittenBytes(candidate)
		if err != nil {
			t.Fatal(err)
		}
		charged += written + candidateEnvelopeBytes
	}
	if charged < len(encoded) {
		t.Fatalf("the budget must charge at least what is written: charged %d, wrote %d", charged, len(encoded))
	}
}

// --max bounds how many candidates a run may emit, so a non-positive one bounds
// nothing it could offer. It is refused rather than read as the default: a
// caller that asked for at most zero candidates and silently received five
// hundred was not answered.
func TestANonPositiveMaxIsRefusedRatherThanReadAsTheDefault(t *testing.T) {
	loaded := suggestProject(t, thresholdPack, nil, `{"path":"packs/pack.json"}`)
	for _, stated := range []int{0, -2, -500} {
		_, _, failure := loaded.Suggest(SuggestOptions{Max: stated}, "packs suggest")
		if failure == nil {
			t.Fatalf("--max %d bounds nothing and must be refused", stated)
		}
		if failure.Code != "JPS-INVOCATION-SUGGEST-MAX" || !strings.Contains(failure.Message, "--max") {
			t.Fatalf("the refusal names the flag: %s: %s", failure.Code, failure.Message)
		}
	}
	// The sentinel, and only the sentinel, states no bound of the caller's own.
	// No command line produces it: packs suggest registers --max carrying the
	// default and refuses every non-positive value a caller could type.
	if _, _, failure := loaded.Suggest(SuggestOptions{Max: MaxCandidatesUnset}, "packs suggest"); failure != nil {
		t.Fatalf("an unset bound is the default, not a refusal: %s", failure.Message)
	}
}

// A declared evidence requirement this generator cannot vary is reported, never
// dropped. An id nothing states and an id no reviewer can read are the same
// defect from the axis's point of view — neither can be named in an
// evidenceAvailability somebody checks — and a dimension missing in silence
// reads as one the pack never declared (ADR-0022's skipped-not-passed rule,
// which ADR-0024 cites and therefore owes).
func TestAnUnusableEvidenceRequirementIdIsReportedRatherThanDropped(t *testing.T) {
	set := latticeSet()
	set.deriveEvidence(map[string]any{"evidenceRequirements": []any{
		map[string]any{"id": strings.Repeat("e", maxCandidateLiteralBytes+1), "required": true},
		map[string]any{"required": true},
		map[string]any{"id": "readable", "required": false},
	}})
	// The one requirement a reviewer can read still derives its three tri-states.
	if len(set.candidates) != 3 {
		t.Fatalf("a usable requirement derives its own axis: %+v", set.candidates)
	}
	skips := set.skipped()
	if len(skips) != 1 || skips[0].Name != "unusable-evidence-id" {
		t.Fatalf("the unusable requirements are reported under their own name: %+v", skips)
	}
	if !strings.Contains(skips[0].Detail, strconv.Itoa(maxCandidateLiteralBytes)) {
		t.Fatalf("the reason states the bound it is about: %s", skips[0].Detail)
	}
	// The oversize id is named as a subject, under ADR-0023's rendering budget
	// rather than repeated whole — a value no reviewer can read is not made
	// readable by printing it in the skip.
	if !strings.Contains(skips[0].Detail, "Not derived for: ") || len(skips[0].Detail) > 1024 {
		t.Fatalf("the subject is named under the budget: %s", skips[0].Detail)
	}
}

// --base requires --id, names a row of that pack's matrix, and refuses with the
// rows it does know rather than deriving from a row nobody reviewed.
func TestTheBaseRowMustBeAReviewedRowOfTheSelectedPack(t *testing.T) {
	matrix := `{"matrixVersion":"1","cases":[{"id":"reviewed","facts":{"expense":{"amountUsd":"10"}},"expectedDisposition":{"kind":"outcome","outcomeId":"auto-approve","reasons":[],"handoff":{"state":"none"}}}]}`
	loaded := suggestProject(t, thresholdPack, map[string]string{"packs/pack.matrix.json": matrix},
		`{"path":"packs/pack.json","matrix":"packs/pack.matrix.json"}`)
	if _, _, failure := loaded.Suggest(SuggestOptions{BaseRow: "reviewed", Max: MaxCandidatesUnset}, "packs suggest"); failure == nil ||
		!strings.Contains(failure.Message, "--id") {
		t.Fatalf("a row id names a row inside one pack's matrix: %+v", failure)
	}
	_, _, failure := loaded.Suggest(SuggestOptions{ID: "expense", BaseRow: "invented", Max: MaxCandidatesUnset}, "packs suggest")
	if failure == nil || !strings.Contains(failure.Message, `"reviewed"`) {
		t.Fatalf("an unknown base row is refused with the rows that do exist: %+v", failure)
	}
	document := suggestOne(t, loaded, SuggestOptions{ID: "expense", BaseRow: "reviewed", Max: MaxCandidatesUnset})
	facts := candidateFacts(document)
	if facts["suggest:expense:value:/expense/amountUsd:5000"] != `{"expense":{"amountUsd":"5000"}}` {
		t.Fatalf("the base's other members are carried and the varied pointer is moved: %v", facts)
	}
	// The absence witness is derived from the base, because withholding an
	// answer a reviewed row gave is a statement; withholding one it never gave
	// is the same row with a new id.
	if _, present := facts["suggest:expense:absent:/expense/amountUsd"]; !present {
		t.Fatalf("a base row derives a per-pointer absence witness: %v", facts)
	}
}

// The value derivation's non-decimal branch is defensive, and defensive is not
// the same as silent. No pack can reach it today: boundaryGroups admits only
// literals the ordered walk collected, and that walk collects only §2.2
// decimals. But the invariant lives in another function, and a bare continue
// here would be this file's one drop with no record — the shape every other
// decline in it refuses. Called with the group that invariant forbids, the
// derivation reports the dimension under its own name instead of a pointer
// quietly deriving nothing.
func TestANonDecimalLiteralIsReportedRatherThanDroppedInSilence(t *testing.T) {
	set := latticeSet()
	set.derivePointerLattice("/p", []boundaryGroup{{path: "/p", literal: "5,000"}})
	if len(set.candidates) != 0 {
		t.Fatalf("no value can be derived around a literal §2.2 cannot read: %+v", set.candidates)
	}
	skips := set.skipped()
	if len(skips) != 1 || skips[0].Name != "non-decimal-literal" {
		t.Fatalf("the dimension is reported under its own name: %+v", skips)
	}
	if !strings.Contains(skips[0].Detail, "/p") {
		t.Fatalf("the reason names the pointer it is about: %s", skips[0].Detail)
	}
	// The ordinary path is untouched: a decimal literal at the same pointer
	// derives its values and opens no record. One literal spelled at unit
	// precision has no interior midpoint and its outer edges land on its own two
	// steps, so the deduplicated lattice is exactly those three.
	ordinary := latticeSet()
	ordinary.derivePointerLattice("/p", []boundaryGroup{{path: "/p", literal: "5000"}})
	var derived []string
	for _, candidate := range ordinary.candidates {
		derived = append(derived, candidate.ID)
	}
	want := []string{"suggest:d:value:/p:4999", "suggest:d:value:/p:5000", "suggest:d:value:/p:5001"}
	if !slices.Equal(derived, want) {
		t.Fatalf("a §2.2 literal derives its lattice in numeric order: %v", derived)
	}
	if len(ordinary.skipped()) != 0 {
		t.Fatalf("an ordinary derivation declines nothing: %+v", ordinary.skipped())
	}
}

// A base row that STATES a member as null states a scalar there, and a scalar
// is not a place to build a container in.
//
// The distinction the one nil check conflated: a member the base never
// mentioned has no answer to preserve, so the containers a pointer needs are
// created under it; a member stated as JSON null is an answer the reviewed row
// gave, and growing an object under it would edit that answer rather than vary
// one pointer — the same thing overwriting any other scalar would do. It is
// reported under the placement's own name, because a dimension missing in
// silence reads as one the pack never stated.
func TestAnExplicitNullBaseMemberIsAStatedScalarAndNotAnAbsence(t *testing.T) {
	expectation := `"expectedDisposition":{"kind":"outcome","outcomeId":"auto-approve","reasons":[],"handoff":{"state":"none"}}`
	matrix := `{"matrixVersion":"1","cases":[
	  {"id":"stated-null","facts":{"expense":null},` + expectation + `},
	  {"id":"unstated","facts":{"other":"kept"},` + expectation + `}
	]}`
	loaded := suggestProject(t, thresholdPack, map[string]string{"packs/pack.matrix.json": matrix},
		`{"path":"packs/pack.json","matrix":"packs/pack.matrix.json"}`)

	report, document, failure := loaded.Suggest(SuggestOptions{ID: "expense", BaseRow: "stated-null", Max: MaxCandidatesUnset}, "packs suggest")
	if failure != nil {
		t.Fatalf("suggest: %s: %s", failure.Code, failure.Message)
	}
	if len(document.Candidates) != 0 {
		t.Fatalf("nothing may be placed under a stated null: %v", candidateFacts(document))
	}
	skips := report.Packs[0].Skipped
	if len(skips) != 1 || skips[0].Name != "unplaceable-pointer" {
		t.Fatalf("the declined placement is reported under its own name: %+v", skips)
	}
	// The full report content this skip owes: the constraint it declined on, the
	// null it declined on in particular, and the subject it was declined for.
	for _, required := range []string{"explicit null", "container", "Not derived for: ", "/expense/amountUsd"} {
		if !strings.Contains(skips[0].Detail, required) {
			t.Fatalf("the skip must carry %q: %s", required, skips[0].Detail)
		}
	}

	// The unstated member is the contrast and is not a decline at all: the base
	// never answered, so the containers are created and the lattice derives.
	_, unstated, failure := loaded.Suggest(SuggestOptions{ID: "expense", BaseRow: "unstated", Max: MaxCandidatesUnset}, "packs suggest")
	if failure != nil {
		t.Fatalf("suggest: %s", failure.Message)
	}
	if unstated.Candidates == nil || len(unstated.Candidates) == 0 {
		t.Fatal("a pointer the base never states is created rather than declined")
	}
	if facts := candidateFacts(unstated)["suggest:expense:value:/expense/amountUsd:5000"]; facts != `{"expense":{"amountUsd":"5000"},"other":"kept"}` {
		t.Fatalf("the rest of the base is preserved and the pointer is created: %s", facts)
	}
}

// A pointer whose array token this runtime's own RFC 6901 resolution refuses
// addresses no element, so nothing is placed at it.
//
// strconv.Atoi reads "00" and "+0" as zero and the evaluator does not, and the
// gap is not academic: a candidate placed at /items/00 would sit at element 0
// of a reviewed row while every condition reading that pointer resolves
// nothing, so the candidate would probe a value no evaluation ever sees. The
// decline is reported with the token named, because "unplaceable pointer" alone
// would not tell a reader which token was refused.
func TestAnArrayTokenTheResolutionRefusesIsDeclinedRatherThanPlaced(t *testing.T) {
	pack := strings.Replace(thresholdPack, "/expense/amountUsd", "/items/00/amount", 1)
	expectation := `"expectedDisposition":{"kind":"outcome","outcomeId":"auto-approve","reasons":[],"handoff":{"state":"none"}}`
	matrix := `{"matrixVersion":"1","cases":[{"id":"array","facts":{"items":[{"amount":"1"}]},` + expectation + `}]}`
	loaded := suggestProject(t, pack, map[string]string{"packs/pack.matrix.json": matrix},
		`{"path":"packs/pack.json","matrix":"packs/pack.matrix.json"}`)

	report, document, failure := loaded.Suggest(SuggestOptions{ID: "expense", BaseRow: "array", Max: MaxCandidatesUnset}, "packs suggest")
	if failure != nil {
		t.Fatalf("suggest: %s: %s", failure.Code, failure.Message)
	}
	if len(document.Candidates) != 0 {
		t.Fatalf("a pointer the evaluator refuses derives no candidate: %v", candidateFacts(document))
	}
	var placement result.SuggestionSkip
	for _, skip := range report.Packs[0].Skipped {
		if skip.Name == "unplaceable-pointer" {
			placement = skip
		}
	}
	if placement.Name == "" {
		t.Fatalf("the declined placement is reported: %+v", report.Packs[0].Skipped)
	}
	for _, required := range []string{"RFC 6901", `(array token "00")`, "/items/00/amount"} {
		if !strings.Contains(placement.Detail, required) {
			t.Fatalf("the skip must name %q: %s", required, placement.Detail)
		}
	}
}

// One value spelled two ways is one boundary, and whether that boundary derives
// a lattice at all must not depend on which spelling a rule declared first.
//
// Under the first-spelling reading, "1" and a 146-byte spelling of the same
// value derived a whole lattice in one order — the oversize spelling never
// looked at — and in the other derived nothing at all: no values, no absence
// witness, only a skip. The same policy, two candidate sets, with nothing in
// the pack to explain the difference. Both spellings are read now: the oversize
// one is reported, the readable one derives, and the two orderings write
// identical bytes.
func TestAnOversizeSpellingOfOneBoundaryIsOrderIndependent(t *testing.T) {
	oversize := "1." + strings.Repeat("0", 145)
	if len(oversize) <= maxCandidateLiteralBytes {
		t.Fatalf("the probe needs a spelling past the budget: %d bytes", len(oversize))
	}
	// Both comparisons sit in one rule under one operator, so the only thing
	// that differs between the two runs is which spelling is declared first.
	lattice := func(first, second string) ([]byte, []result.SuggestionSkip) {
		set := latticeSet()
		set.deriveValues(map[string]any{
			"outcomes": []any{map[string]any{"id": "review"}},
			"rules": []any{map[string]any{"id": "one", "outcome": "review", "when": map[string]any{
				"op": "any", "conditions": []any{
					map[string]any{"op": "fact", "path": "/p", "operator": "greater-than", "value": first},
					map[string]any{"op": "fact", "path": "/p", "operator": "greater-than", "value": second},
				},
			}}},
		})
		encoded, err := EncodeCandidates(Candidates{CandidatesVersion: CandidatesVersion, Candidates: set.candidates})
		if err != nil {
			t.Fatal(err)
		}
		return encoded, set.skipped()
	}
	readableFirst, readableSkips := lattice("1", oversize)
	oversizeFirst, oversizeSkips := lattice(oversize, "1")
	if string(readableFirst) != string(oversizeFirst) {
		t.Fatalf("reordering two spellings of one boundary must not change the bytes:\n%s\nversus\n%s", readableFirst, oversizeFirst)
	}
	if len(readableSkips) != 1 || len(oversizeSkips) != 1 || readableSkips[0] != oversizeSkips[0] {
		t.Fatalf("the oversize spelling is reported in both orderings: %+v versus %+v", readableSkips, oversizeSkips)
	}
	if readableSkips[0].Name != "oversize-literal" {
		t.Fatalf("the declined spelling is reported under its own name: %+v", readableSkips[0])
	}
	// The lattice is derived and rendered in the spelling a reviewer can read,
	// and the unreadable one appears nowhere in the document.
	if !strings.Contains(string(readableFirst), `"p": "1"`) {
		t.Fatalf("the readable spelling is what the candidates carry: %s", readableFirst)
	}
	if strings.Contains(string(readableFirst), oversize) {
		t.Fatalf("a value no reviewer can read is in no candidate: %s", readableFirst)
	}
}

// --include-hugs promises the pair two decimal places finer than each literal's
// authored precision, and this generator's step floor is 10^-6. Where the floor
// swallows one or both of them the run says so rather than delivering something
// narrower under the flag's name.
func TestTheHugPairIsClampedAtTheStepFloorAndSaidSo(t *testing.T) {
	for _, row := range []struct {
		literal string
		hugs    []string
		skip    string
	}{
		// Under the floor: exactly the pair the flag names, two places finer.
		{literal: "1.00", hugs: []string{"0.9999", "1.0001"}},
		// One place off the floor: the pair lands one finer rather than two.
		{literal: "1.00000", hugs: []string{"0.999999", "1.000001"}, skip: "clamped-hug"},
		// At the floor: there is no finer pair to offer, and none is offered.
		{literal: "1.000000", skip: "unavailable-hug"},
	} {
		pack := map[string]any{
			"outcomes": []any{map[string]any{"id": "review"}},
			"rules": []any{map[string]any{"id": "one", "outcome": "review", "when": map[string]any{
				"op": "fact", "path": "/p", "operator": "greater-than", "value": row.literal,
			}}},
		}
		set := hugSet()
		set.deriveValues(pack)
		values := map[string]string{}
		for _, candidate := range set.candidates {
			var facts struct {
				P string `json:"p"`
			}
			if err := json.Unmarshal(candidate.Facts, &facts); err != nil {
				t.Fatal(err)
			}
			values[facts.P] = candidate.Rationale
		}
		for _, want := range row.hugs {
			rationale, offered := values[want]
			if !offered {
				t.Fatalf("literal %q must offer the hug %q: %v", row.literal, want, slices.Sorted(maps.Keys(values)))
			}
			if !strings.Contains(rationale, "--include-hugs") {
				t.Fatalf("a hug says which flag asked for it: %s", rationale)
			}
			if (row.skip == "clamped-hug") != strings.Contains(rationale, "one place finer rather than two") {
				t.Fatalf("a clamped hug names the distance it actually carries: %s", rationale)
			}
		}
		names := []string{}
		for _, skip := range set.skipped() {
			names = append(names, skip.Name)
		}
		if row.skip == "" {
			if len(names) != 0 {
				t.Fatalf("literal %q declines nothing: %v", row.literal, names)
			}
		} else if !slices.Contains(names, row.skip) {
			t.Fatalf("literal %q must report %q: %v", row.literal, row.skip, names)
		}
		// A clamped hug is a narrowing, not a decline: its pair IS derived, so
		// its subject tail must say so rather than contradict the record body.
		for _, skip := range set.skipped() {
			if skip.Name == "clamped-hug" {
				if !strings.Contains(skip.Detail, "Narrowed for: ") || strings.Contains(skip.Detail, "Not derived for: ") {
					t.Fatalf("a clamped-hug skip narrows rather than declines: %q", skip.Detail)
				}
			}
		}
		// The floor is reported, never silently applied: at the floor the pair
		// is absent from the values entirely.
		if row.skip == "unavailable-hug" && len(values) != 5 {
			t.Fatalf("literal %q offers its lattice and no hug pair: %v", row.literal, slices.Sorted(maps.Keys(values)))
		}
	}
	// Without the flag neither record is opened at all: nothing was asked for,
	// so nothing was declined.
	plain := latticeSet()
	plain.deriveValues(map[string]any{
		"outcomes": []any{map[string]any{"id": "review"}},
		"rules": []any{map[string]any{"id": "one", "outcome": "review", "when": map[string]any{
			"op": "fact", "path": "/p", "operator": "greater-than", "value": "1.000000",
		}}},
	})
	if len(plain.skipped()) != 0 {
		t.Fatalf("a run that asked for no hugs declines none: %+v", plain.skipped())
	}
}

// The quantifier report is keyed to the positions a condition can occupy, not
// to any object that looks like one. A pack carrying a condition-shaped operand
// as DATA states no quantifier, and reporting one would say a whole dimension
// was skipped when nothing was.
func TestConditionShapedDataIsNotADraftQuantifier(t *testing.T) {
	for _, row := range []struct {
		name      string
		when      string
		quantifer bool
	}{
		{
			name: "a quantifier-shaped operand is data",
			when: `{"op":"fact","path":"/expense/amountUsd","operator":"equals",
			        "value":{"op":"exists","path":"/lines","where":{"op":"fact","path":"/amount","operator":"greater-than","value":"1"}}}`,
		},
		{
			name:      "a quantifier at a condition position is a quantifier",
			when:      `{"op":"exists","path":"/lines","where":{"op":"fact","path":"/amount","operator":"greater-than","value":"1"}}`,
			quantifer: true,
		},
		{
			name:      "and so is one nested under all, any, or not",
			when:      `{"op":"all","conditions":[{"op":"not","condition":{"op":"every","path":"/lines","where":{"op":"fact","path":"/amount","operator":"greater-than","value":"1"}}}]}`,
			quantifer: true,
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			pack := strings.Replace(thresholdPack,
				`{"op": "fact", "path": "/expense/amountUsd", "operator": "greater-than", "value": "5000"}`,
				row.when, 1)
			loaded := suggestProject(t, pack, nil, `{"path":"packs/pack.json"}`)
			report, _, failure := loaded.Suggest(SuggestOptions{Max: MaxCandidatesUnset}, "packs suggest")
			if failure != nil {
				t.Fatalf("suggest: %s", failure.Message)
			}
			reported := false
			for _, skip := range report.Packs[0].Skipped {
				if skip.Name == "draft-rfc-quantifiers" {
					reported = true
				}
			}
			if reported != row.quantifer {
				t.Fatalf("quantifier reported=%v, want %v: %+v", reported, row.quantifer, report.Packs[0].Skipped)
			}
		})
	}
}

// A compared literal or operand no reviewer can read derives no candidate and
// is reported: a candidate would repeat it in an id, in a rationale, and in a
// facts document, and a value nobody can read is a candidate nobody can review.
func TestAnOversizeLiteralAndOperandAreReportedRatherThanRendered(t *testing.T) {
	oversize := strings.Repeat("7", maxCandidateLiteralBytes+1)
	pack := map[string]any{
		"outcomes": []any{map[string]any{"id": "review"}},
		"rules": []any{
			map[string]any{"id": "ordered", "outcome": "review", "when": map[string]any{
				"op": "fact", "path": "/p", "operator": "greater-than", "value": oversize,
			}},
			map[string]any{"id": "member", "outcome": "review", "when": map[string]any{
				"op": "fact", "path": "/q", "operator": "equals", "value": oversize,
			}},
		},
	}
	set := latticeSet()
	set.deriveValues(pack)
	set.deriveMembers(pack)
	if len(set.candidates) != 0 {
		t.Fatalf("no candidate renders a value past the budget: %v", candidateFacts(Candidates{Candidates: set.candidates}))
	}
	skips := set.skipped()
	if len(skips) != 1 || skips[0].Name != "oversize-literal" {
		t.Fatalf("both halves are reported under one name: %+v", skips)
	}
	for _, required := range []string{strconv.Itoa(maxCandidateLiteralBytes), "/p", "/q"} {
		if !strings.Contains(skips[0].Detail, required) {
			t.Fatalf("the skip names the bound and both subjects: %s", skips[0].Detail)
		}
	}
	if strings.Contains(skips[0].Detail, oversize) {
		t.Fatalf("a value no reviewer can read is not repeated in the report: %s", skips[0].Detail)
	}
}

// The negative witness is an ABSENCE and never an invented non-member. Every
// value a membership candidate places is a value the pack document itself
// carries, which is what keeps this dimension in scope where synthesizing one
// would not be: a made-up value is the generator inventing a policy world.
func TestNoCandidatePlacesAValueThePackDoesNotState(t *testing.T) {
	pack := map[string]any{
		"outcomes": []any{map[string]any{"id": "review"}},
		"rules": []any{
			map[string]any{"id": "listed", "outcome": "review", "when": map[string]any{
				"op": "fact", "path": "/tier", "operator": "in", "value": []any{"gold", "silver"},
			}},
			map[string]any{"id": "excluded", "outcome": "review", "when": map[string]any{
				"op": "fact", "path": "/region", "operator": "not-equals", "value": "eu",
			}},
		},
	}
	set := latticeSet()
	set.deriveMembers(pack)
	set.deriveAbsences()
	stated := map[string]bool{`{"tier":"gold"}`: true, `{"tier":"silver"}`: true, `{"region":"eu"}`: true}
	absences := 0
	for _, candidate := range set.candidates {
		if strings.Contains(candidate.ID, ":absent") {
			absences++
			if string(candidate.Facts) != "{}" {
				t.Fatalf("the absence witness withholds rather than invents: %s", candidate.Facts)
			}
			continue
		}
		if !stated[string(candidate.Facts)] {
			t.Fatalf("a candidate placed a value the pack never states: %s", candidate.Facts)
		}
	}
	if len(set.candidates) != len(stated)+absences || absences != 1 {
		t.Fatalf("every stated operand derives one candidate and the negative witness is the absence: %d candidate(s)", len(set.candidates))
	}
	// In particular: nothing anywhere in the run is a value invented to fail the
	// not-equals — the not-member witness is the withheld pointer.
	if len(set.skipped()) != 0 {
		t.Fatalf("nothing was declined in this shape: %+v", set.skipped())
	}
}

// A declared pack this runtime cannot read derives nothing, is reported as
// unreadable rather than as stating no comparison, and does not fail the run:
// packs validate is the surface that fails on an unreadable declared document,
// and duplicating that verdict here would give a generator a gate's exit code.
func TestAnUnreadablePackIsDemotedAndNeverFailsTheRun(t *testing.T) {
	loaded := suggestProject(t, `{ not json`, nil, `{"path":"packs/pack.json"}`)
	report, document, failure := loaded.Suggest(SuggestOptions{Max: MaxCandidatesUnset}, "packs suggest")
	if failure != nil {
		t.Fatalf("an unreadable pack is not this run's failure: %s", failure.Message)
	}
	if len(document.Candidates) != 0 || report.Status != "skipped" {
		t.Fatalf("nothing was derived and the run is skipped: %q %d", report.Status, len(document.Candidates))
	}
	entry := report.Packs[0]
	if !entry.Unreadable {
		t.Fatalf("the entry says the pack could not be read: %+v", entry)
	}
	if !strings.Contains(entry.Detail, "packs validate") {
		t.Fatalf("the detail points at the surface that diagnoses the read: %s", entry.Detail)
	}
	// A document that decodes but is not an object is the same fact: nothing
	// about its conditions is knowable either.
	notAnObject := suggestProject(t, `"a pack document that is a string"`, nil, `{"path":"packs/pack.json"}`)
	other, _, failure := notAnObject.Suggest(SuggestOptions{Max: MaxCandidatesUnset}, "packs suggest")
	if failure != nil {
		t.Fatalf("suggest: %s", failure.Message)
	}
	if !other.Packs[0].Unreadable {
		t.Fatalf("a document that does not decode as an object is unreadable too: %+v", other.Packs[0])
	}
}
