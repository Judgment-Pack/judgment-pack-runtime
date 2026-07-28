package evaluation

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// draftPack builds one minimal conformant-apart-from-the-draft-grammar pack
// whose single rule carries the given condition, so a pack-level row states
// only the thing it is about.
func draftPack(when, onUnknown string) []byte {
	return []byte(fmt.Sprintf(`{
  "specVersion": "0.1.0-draft",
  "id": "https://example.invalid/judgment-packs/rfc0008-row",
  "version": "0.1.0",
  "title": "Synthetic draft RFC 0008 row",
  "description": "Invented content for specification testing; it authorizes nothing.",
  "decision": {
    "intent": "Exercise one draft RFC 0008 condition against one facts document.",
    "question": "Does the condition hold?"
  },
  "outcomes": [
    {"id": "held", "label": "Held"},
    {"id": "did-not-hold", "label": "Did not hold"}
  ],
  "rules": [
    {
      "id": "the-rule",
      "description": "The one rule under test.",
      "when": %s,
      "outcome": "held",
      "onUnknown": %q
    }
  ],
  "fallbackOutcome": "did-not-hold"
}`, when, onUnknown))
}

func draftOptions(budget int) Options {
	return Options{Command: "test", RFC0008Quantifiers: true, WorkBudget: budget}
}

// The depth-indexed schema construction, as a declarative artifact, and the
// runtime's own grammar check must agree over what the draft grammar adds:
// aggregate shape and aggregate depth. Every row below is one of those, and is
// run twice — once through the testdata JSON-Schema tiers and once through the
// Go check that gates evaluation. The agreement is scoped to that: the Go check
// never looks inside a where for Core validity, which the Core projection owns
// instead (TestRFC0008CoreProjectionStillValidatesInsideWhere), so a row whose
// where is Core-invalid but whose aggregates are well-shaped belongs there and
// not here.
func TestRFC0008DepthIndexedGrammar(t *testing.T) {
	schemaBytes, err := os.ReadFile(filepath.Join("testdata", "rfc0008-condition-schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := validation.CompileSchema(schemaBytes, "urn:judgmentpack:rfc0008:condition:draft")
	if err != nil {
		t.Fatal(err)
	}

	const predicate = `{"op":"fact","path":"/ok","operator":"equals","value":true}`
	const depthOneExists = `{"op":"exists","path":"/cells","where":` + predicate + `}`
	const depthOneUniform = `{"op":"uniform","path":"/cells","at":"/cabin"}`

	cases := []struct {
		name      string
		condition string
		valid     bool
	}{
		{"core-condition-unchanged", predicate, true},
		{"top-level-exists", `{"op":"exists","path":"/rows","where":` + predicate + `}`, true},
		{"top-level-uniform", `{"op":"uniform","path":"/rows","at":"/cabin"}`, true},
		{"depth-two-nesting-of-both-operators",
			`{"op":"exists","path":"/rows","where":{"op":"every","path":"/cells","where":` + predicate + `}}`, true},
		{"sibling-aggregates-at-depth-two",
			`{"op":"exists","path":"/rows","where":{"op":"all","conditions":[` + depthOneExists + `,` + depthOneExists + `]}}`, true},
		{"aggregate-reached-through-all-inside-a-where",
			`{"op":"exists","path":"/rows","where":{"op":"all","conditions":[` + depthOneExists + `]}}`, true},
		{"aggregate-reached-through-not-inside-a-where",
			`{"op":"exists","path":"/rows","where":{"op":"not","condition":` + depthOneExists + `}}`, true},
		{"uniform-at-depth-two",
			`{"op":"exists","path":"/rows","where":` + depthOneUniform + `}`, true},
		{"uniform-at-depth-two-through-a-wrapper",
			`{"op":"exists","path":"/rows","where":{"op":"any","conditions":[` + depthOneUniform + `]}}`, true},

		{"depth-three-nesting", `{"op":"exists","path":"/rows","where":{"op":"exists","path":"/cells","where":` + depthOneExists + `}}`, false},
		{"all-wrapper-does-not-launder-depth",
			`{"op":"exists","path":"/rows","where":{"op":"all","conditions":[{"op":"exists","path":"/cells","where":` + depthOneExists + `}]}}`, false},
		{"not-wrapper-does-not-launder-depth",
			`{"op":"exists","path":"/rows","where":{"op":"not","condition":{"op":"exists","path":"/cells","where":` + depthOneExists + `}}}`, false},
		{"uniform-inside-an-inner-where-is-depth-three",
			`{"op":"exists","path":"/rows","where":{"op":"every","path":"/cells","where":` + depthOneUniform + `}}`, false},

		{"exists-missing-where", `{"op":"exists","path":"/rows"}`, false},
		{"exists-missing-path", `{"op":"exists","where":` + predicate + `}`, false},
		{"exists-with-an-extra-member", `{"op":"exists","path":"/rows","at":"/ok","where":` + predicate + `}`, false},
		{"exists-with-a-non-pointer-path", `{"op":"exists","path":"rows","where":` + predicate + `}`, false},
		{"uniform-missing-at", `{"op":"uniform","path":"/rows"}`, false},
		{"uniform-with-a-where", `{"op":"uniform","path":"/rows","at":"/cabin","where":` + predicate + `}`, false},
		{"uniform-with-a-non-pointer-at", `{"op":"uniform","path":"/rows","at":"cabin"}`, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			condition := documentOf(t, testCase.condition)
			schemaErr := schema.Validate(condition)
			if testCase.valid && schemaErr != nil {
				t.Fatalf("the depth-indexed schema rejected a valid condition: %v", schemaErr)
			}
			if !testCase.valid && schemaErr == nil {
				t.Fatal("the depth-indexed schema accepted an invalid condition")
			}

			root, ok := documentOf(t, string(draftPack(testCase.condition, "escalate"))).(map[string]any)
			if !ok {
				t.Fatal("the row's pack must decode to an object")
			}
			diagnostics := rfc0008Diagnostics(root)
			if testCase.valid && len(diagnostics) > 0 {
				t.Fatalf("the runtime grammar check rejected a valid condition: %+v", diagnostics)
			}
			if !testCase.valid && len(diagnostics) == 0 {
				t.Fatal("the runtime grammar check accepted a condition the schema rejects")
			}
		})
	}
}

// The RFC's structural conformance rows are corpus rows, and a depth-three
// aggregate is refused before any condition is evaluated, with the offending
// location named.
func TestRFC0008StructuralRefusals(t *testing.T) {
	engine := newTestEngine(t)
	const predicate = `{"op":"fact","path":"/ok","operator":"equals","value":true}`

	cases := []struct {
		name      string
		condition string
		wantCode  string
		wantPath  string
	}{
		{
			name:      "depth-three-through-an-all-wrapper",
			condition: `{"op":"exists","path":"/rows","where":{"op":"all","conditions":[{"op":"exists","path":"/cells","where":{"op":"exists","path":"/deeper","where":` + predicate + `}}]}}`,
			wantCode:  "JPS-EVALUATION-RFC0008-DEPTH",
			wantPath:  "/rules/0/when/where/conditions/0/where",
		},
		{
			name:      "depth-three-through-a-not-wrapper",
			condition: `{"op":"exists","path":"/rows","where":{"op":"not","condition":{"op":"exists","path":"/cells","where":{"op":"uniform","path":"/deeper","at":"/cabin"}}}}`,
			wantCode:  "JPS-EVALUATION-RFC0008-DEPTH",
			wantPath:  "/rules/0/when/where/condition/where",
		},
		{
			name:      "missing-where",
			condition: `{"op":"exists","path":"/rows"}`,
			wantCode:  "JPS-EVALUATION-RFC0008-SHAPE",
			wantPath:  "/rules/0/when/where",
		},
		{
			name:      "member-not-allowed",
			condition: `{"op":"uniform","path":"/rows","at":"/cabin","extra":true}`,
			wantCode:  "JPS-EVALUATION-RFC0008-SHAPE",
			wantPath:  "/rules/0/when/extra",
		},
		{
			name:      "path-is-not-a-pointer",
			condition: `{"op":"every","path":"rows","where":` + predicate + `}`,
			wantCode:  "JPS-EVALUATION-RFC0008-SHAPE",
			wantPath:  "/rules/0/when/path",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, failure := engine.EvaluateWith(draftPack(testCase.condition, "escalate"), []byte(`{}`), nil, draftOptions(0))
			if failure == nil {
				t.Fatal("a structurally invalid draft condition must be refused, never evaluated")
			}
			if failure.Code != "JPS-EVALUATION-RFC0008-GRAMMAR" || failure.ExitCode != result.ExitInvalid {
				t.Fatalf("failure = %+v", failure)
			}
			if !strings.Contains(failure.Message, testCase.wantCode) || !strings.Contains(failure.Message, testCase.wantPath) {
				t.Fatalf("the refusal must name %s at %s: %q", testCase.wantCode, testCase.wantPath, failure.Message)
			}
		})
	}
}

// Without the opt-in a pack using the draft operators fails exactly as it does
// today — the bundled schema's closed condition union rejects it — and with the
// opt-in the same pack evaluates, carrying a marker that says so.
func TestRFC0008OptInGate(t *testing.T) {
	engine := newTestEngine(t)
	pack := draftPack(`{"op":"exists","path":"/list","where":{"op":"fact","path":"/ok","operator":"equals","value":true}}`, "escalate")
	facts := []byte(`{"list":[{"ok":true}]}`)

	_, failure := engine.Evaluate(pack, facts, nil, nil, "test")
	if failure == nil || failure.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" || failure.ExitCode != result.ExitInvalid {
		t.Fatalf("without the opt-in the pack must be refused as non-conformant: %+v", failure)
	}
	if !strings.Contains(failure.Message, "JPS-STRUCTURE-CONDITION-SHAPE") {
		t.Fatalf("the refusal must be the ordinary structural one: %q", failure.Message)
	}

	output, failure := engine.EvaluateWith(pack, facts, nil, draftOptions(0))
	if failure != nil {
		t.Fatalf("with the opt-in the pack must evaluate: %s", failure.Message)
	}
	if output.Disposition.Kind != "outcome" || output.Disposition.OutcomeID != "held" {
		t.Fatalf("disposition = %+v", output.Disposition)
	}
	marker := output.DraftPrototype
	if marker == nil {
		t.Fatal("a draft-grammar evaluation must carry the prototype marker")
	}
	if marker.RFC != "0008" || marker.Status != "draft-rfc-prototype" {
		t.Fatalf("marker = %+v", marker)
	}
	if !reflect.DeepEqual(marker.Operators, []string{"exists"}) {
		t.Fatalf("marker must name the operators used: %+v", marker.Operators)
	}
	if marker.PackValidUnderSpecVersion {
		t.Fatal("a pack using a draft operator is not valid under the published specification")
	}
	if !strings.Contains(marker.Note, "NOT valid") || !strings.Contains(marker.Note, output.SpecVersion) {
		t.Fatalf("the marker note must say the pack is not valid under the named version: %q", marker.Note)
	}

	// The opt-in changes nothing for a pack that uses no draft operator, and the
	// marker says that too rather than implying an accusation.
	core := draftPack(`{"op":"fact","path":"/ok","operator":"equals","value":true}`, "escalate")
	output, failure = engine.EvaluateWith(core, []byte(`{"ok":true}`), nil, draftOptions(0))
	if failure != nil {
		t.Fatalf("a Core pack must still evaluate under the opt-in: %s", failure.Message)
	}
	if output.DraftPrototype == nil || len(output.DraftPrototype.Operators) != 0 || !output.DraftPrototype.PackValidUnderSpecVersion {
		t.Fatalf("marker = %+v", output.DraftPrototype)
	}
}

// Aggregates are structurally valid anywhere a condition is allowed, so the
// gate, the projection, and the evaluator must reach all three positions: the
// pack's applicability, an exception's when, and a rule's when.
func TestRFC0008AggregatesInEveryConditionPosition(t *testing.T) {
	engine := newTestEngine(t)
	pack := []byte(`{
  "specVersion": "0.1.0-draft",
  "id": "https://example.invalid/judgment-packs/rfc0008-positions",
  "version": "0.1.0",
  "title": "Synthetic draft RFC 0008 condition positions",
  "description": "Invented content for specification testing; it authorizes nothing.",
  "decision": {
    "intent": "Exercise a draft aggregate in every position a condition may appear.",
    "question": "Which position decided?"
  },
  "outcomes": [
    {"id": "held", "label": "Held"},
    {"id": "forced", "label": "Forced"}
  ],
  "applicability": {"op": "every", "path": "/segments", "where": {"op": "fact", "path": "/inScope", "operator": "equals", "value": true}},
  "rules": [
    {
      "id": "the-rule",
      "description": "Fires when any segment is flagged.",
      "when": {"op": "exists", "path": "/segments", "where": {"op": "fact", "path": "/flagged", "operator": "equals", "value": true}},
      "outcome": "held",
      "onUnknown": "escalate"
    }
  ],
  "exceptions": [
    {
      "id": "the-exception",
      "description": "Forces an outcome when the cabin is uniform.",
      "when": {"op": "uniform", "path": "/segments", "at": "/cabin"},
      "effect": "force-outcome",
      "outcome": "forced",
      "onUnknown": "ignore"
    }
  ],
  "fallbackOutcome": "held"
}`)

	cases := []struct {
		name        string
		facts       string
		wantKind    string
		wantOutcome string
	}{
		{
			name:     "applicability-aggregate-decides",
			facts:    `{"segments":[{"inScope":true,"cabin":"economy"},{"inScope":false,"cabin":"economy"}]}`,
			wantKind: "not-applicable",
		},
		{
			name:        "exception-aggregate-forces",
			facts:       `{"segments":[{"inScope":true,"cabin":"economy","flagged":false},{"inScope":true,"cabin":"economy","flagged":false}]}`,
			wantKind:    "outcome",
			wantOutcome: "forced",
		},
		{
			name:        "rule-aggregate-decides",
			facts:       `{"segments":[{"inScope":true,"cabin":"economy","flagged":true},{"inScope":true,"cabin":"business","flagged":false}]}`,
			wantKind:    "outcome",
			wantOutcome: "held",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			output, failure := engine.EvaluateWith(pack, []byte(testCase.facts), nil, draftOptions(0))
			if failure != nil {
				t.Fatalf("evaluation failed: %s", failure.Message)
			}
			if output.Disposition.Kind != testCase.wantKind || output.Disposition.OutcomeID != testCase.wantOutcome {
				t.Fatalf("disposition = %+v", output.Disposition)
			}
			if !reflect.DeepEqual(output.DraftPrototype.Operators, []string{"every", "exists", "uniform"}) {
				t.Fatalf("the marker must name every draft operator used: %v", output.DraftPrototype.Operators)
			}
		})
	}
}

// The Core projection is a validation instrument, not a hole: everything the
// draft grammar does not add is still held to full document conformance,
// including the conditions inside a where — a Core-invalid op among them, which
// the Go grammar gate does not check at all. RFC 0008's §3.3 amendment — the
// condition-walking semantic bullets must recurse into where — is what the
// evidence-present row pins.
//
// The reported instance path is the projected one, with the elided /where
// segments missing, and each row pins it rather than leaving the offset latent:
// the projection replaces each exists and every by its where, so a finding two
// levels inside a where surfaces at the aggregate's own location.
func TestRFC0008CoreProjectionStillValidatesInsideWhere(t *testing.T) {
	engine := newTestEngine(t)
	cases := []struct {
		name      string
		condition string
		wantCode  string
		wantPath  string
		original  string
	}{
		{
			name:      "evidence-present-inside-where-must-resolve",
			condition: `{"op":"exists","path":"/list","where":{"op":"evidence-present","evidenceRequirement":"no-such-requirement"}}`,
			wantCode:  "JPS-SEMANTIC-UNRESOLVED-EVIDENCE",
			wantPath:  "/rules/0/when/evidenceRequirement",
			original:  "/rules/0/when/where/evidenceRequirement",
		},
		{
			name:      "operand-shape-inside-where-is-structural",
			condition: `{"op":"exists","path":"/list","where":{"op":"fact","path":"/ok","operator":"in","value":"not-an-array"}}`,
			wantCode:  "JPS-STRUCTURE-IN-OPERAND",
			wantPath:  "/rules/0/when/value",
			original:  "/rules/0/when/where/value",
		},
		{
			name:      "ordered-operand-inside-a-nested-where",
			condition: `{"op":"exists","path":"/rows","where":{"op":"every","path":"/cells","where":{"op":"fact","path":"/amount","operator":"greater-than","value":5}}}`,
			wantCode:  "JPS-STRUCTURE-DECIMAL-OPERAND",
			wantPath:  "/rules/0/when/value",
			original:  "/rules/0/when/where/where/value",
		},
		{
			// The Go gate owns aggregate shape and depth only; an op §7 does not
			// define is Core's business, and the projection is what refuses it.
			name:      "core-invalid-op-inside-where",
			condition: `{"op":"exists","path":"/rows","where":{"op":"bogus"}}`,
			wantCode:  "JPS-STRUCTURE-CONDITION-SHAPE",
			wantPath:  "/rules/0/when",
			original:  "/rules/0/when/where",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			pack := draftPack(testCase.condition, "escalate")
			root, ok := documentOf(t, string(pack)).(map[string]any)
			if !ok {
				t.Fatal("the row's pack must decode to an object")
			}
			// The Go grammar gate reports nothing here: every row's aggregate is
			// well-shaped and within depth, and what is wrong sits inside the where.
			if diagnostics := rfc0008Diagnostics(root); len(diagnostics) > 0 {
				t.Fatalf("the draft grammar gate owns shape and depth only: %+v", diagnostics)
			}
			_, failure := engine.EvaluateWith(pack, []byte(`{"list":[]}`), nil, draftOptions(0))
			if failure == nil || failure.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" {
				t.Fatalf("failure = %+v", failure)
			}
			if !strings.Contains(failure.Message, testCase.wantCode) {
				t.Fatalf("the refusal must name %s: %q", testCase.wantCode, failure.Message)
			}
			if !strings.Contains(failure.Message, " at "+testCase.wantPath+":") {
				t.Fatalf("the refusal must report the projected path %s: %q", testCase.wantPath, failure.Message)
			}
			if strings.Contains(failure.Message, testCase.original) {
				t.Fatalf("the projected path is not the authored one (%s): %q", testCase.original, failure.Message)
			}
			// Because it is not the authored path, the refusal has to say so, and
			// must not send the author to spec validate, which for this pack
			// reports the aggregate itself and never this finding.
			if !strings.Contains(failure.Message, "relative to that projection") {
				t.Fatalf("the refusal must qualify the projected path: %q", failure.Message)
			}
			if strings.Contains(failure.Message, "Run spec validate") {
				t.Fatalf("a draft-operator pack must not be sent to spec validate for this report: %q", failure.Message)
			}
		})
	}
}

// The adversarial row: a facts document sized past the limit yields an explicit
// resource error rather than true, false, or unresolved — and it yields the
// same error whether the dominant element sits first or last, which is what
// order-independent precharged accounting buys. Under a budget the same inputs
// fit, both permutations produce the same disposition, so the permutation is
// not doing the work.
func TestRFC0008WorkLimitIsOrderIndependent(t *testing.T) {
	engine := newTestEngine(t)
	pack := draftPack(`{"op":"exists","path":"/list","where":{"op":"fact","path":"/ok","operator":"equals","value":true}}`, "escalate")

	elements := make([]string, 0, 64)
	for index := 0; index < 63; index++ {
		elements = append(elements, `{"ok":false}`)
	}
	dominantFirst := []byte(fmt.Sprintf(`{"list":[%s]}`, strings.Join(append([]string{`{"ok":true}`}, elements...), ",")))
	dominantLast := []byte(fmt.Sprintf(`{"list":[%s]}`, strings.Join(append(append([]string{}, elements...), `{"ok":true}`), ",")))

	// Over budget: the dominant element that lets an evaluator stop at index 0
	// must not buy it a disposition the other permutation cannot have.
	first, firstFailure := engine.EvaluateWith(pack, dominantFirst, nil, draftOptions(64))
	last, lastFailure := engine.EvaluateWith(pack, dominantLast, nil, draftOptions(64))
	if firstFailure == nil || lastFailure == nil {
		t.Fatalf("both permutations must be refused: %+v / %+v", firstFailure, lastFailure)
	}
	if !reflect.DeepEqual(firstFailure, lastFailure) {
		t.Fatalf("the two permutations must produce the same error:\n first=%+v\n  last=%+v", firstFailure, lastFailure)
	}
	if firstFailure.Code != "JPS-RESOURCE-EVALUATION-WORK-LIMIT" || firstFailure.ExitCode != result.ExitIO {
		t.Fatalf("exhaustion must be an explicit resource error: %+v", firstFailure)
	}
	if first.Disposition.Kind != "" || last.Disposition.Kind != "" {
		t.Fatalf("exhaustion is an error, never a disposition: %+v / %+v", first.Disposition, last.Disposition)
	}

	// Within budget: identical dispositions, so the rows above turn on the limit
	// and not on the permutation.
	first, firstFailure = engine.EvaluateWith(pack, dominantFirst, nil, draftOptions(0))
	last, lastFailure = engine.EvaluateWith(pack, dominantLast, nil, draftOptions(0))
	if firstFailure != nil || lastFailure != nil {
		t.Fatalf("within budget both permutations must evaluate: %+v / %+v", firstFailure, lastFailure)
	}
	if !reflect.DeepEqual(first.Disposition, last.Disposition) || first.Disposition.OutcomeID != "held" {
		t.Fatalf("permuted inputs must produce one disposition: %+v / %+v", first.Disposition, last.Disposition)
	}
}

// The budget is charged across the whole evaluation, so sibling aggregates in
// different conditions add, and the error names the configured limit.
func TestRFC0008WorkLimitIsConfigurable(t *testing.T) {
	engine := newTestEngine(t)
	pack := draftPack(`{"op":"every","path":"/list","where":{"op":"fact","path":"/ok","operator":"equals","value":true}}`, "escalate")
	facts := []byte(`{"list":[{"ok":true},{"ok":true},{"ok":true}]}`)

	// The charge is 12 for the aggregate — its node, the six-unit compile of
	// "/list" and the five-unit walk — then 10 for the first element, whose
	// "/ok" is compiled there and whose boolean is charged on both sides of the
	// comparison, and 6 for each of the other two: 34 exactly.
	if _, failure := engine.EvaluateWith(pack, facts, nil, draftOptions(34)); failure != nil {
		t.Fatalf("a budget equal to the charge must not trip: %+v", failure)
	}
	_, failure := engine.EvaluateWith(pack, facts, nil, draftOptions(33))
	if failure == nil || failure.Code != "JPS-RESOURCE-EVALUATION-WORK-LIMIT" {
		t.Fatalf("a budget one unit short must trip: %+v", failure)
	}
	if !strings.Contains(failure.Message, "33") {
		t.Fatalf("the error must name the configured limit: %q", failure.Message)
	}

	// The default budget is generous enough for a collection a pack plausibly
	// quantifies over, and small enough to refuse the RFC's attack sketch.
	if DefaultWorkBudget < 10_000 {
		t.Fatalf("DefaultWorkBudget = %d is too small to be usable", DefaultWorkBudget)
	}
	if DefaultWorkBudget > 1_000_000 {
		t.Fatalf("DefaultWorkBudget = %d does not bound the RFC's 10^6 attack sketch", DefaultWorkBudget)
	}
}

// A collection of numbers no arithmetic type can hold is ordinary work, and the
// charge says so. This row used to be the quadratic one: an arithmetic-based
// equality could not compare such values, every member became another
// representative of uniform's clause 3, and the preflight had to reserve one
// comparison pass per member — 2,204,711 units for the 400 members below, which
// the default budget refused. Deciding equality on the tokens removes both the
// quadratic and the unknown: one representative absorbs the members, the charge
// is a sum over them, and the collection produces a disposition.
func TestRFC0008HugeNumericTokensAreLinearAndDecided(t *testing.T) {
	engine := newTestEngine(t)
	pack := draftPack(`{"op":"uniform","path":"/list","at":"/cabin"}`, "escalate")
	// Distinct values, each a fourteen-byte token — 1001e999999999 and its
	// siblings — so every member costs the same and the arithmetic below is by
	// hand: six units to step the compiled "/cabin" and fifteen for the token.
	facts := func(members int) string {
		items := make([]string, 0, members)
		for index := 1; index <= members; index++ {
			items = append(items, fmt.Sprintf(`{"cabin":1%03de999999999}`, index))
		}
		return fmt.Sprintf(`{"list":[%s]}`, strings.Join(items, ","))
	}
	const condition = `{"op":"uniform","path":"/list","at":"/cabin"}`

	// Linear, with no quadratic term hiding in it: doubling the members adds
	// exactly one per-member charge per added member.
	small, large := chargeOf(t, condition, facts(200)), chargeOf(t, condition, facts(400))
	if want := 200 * (6 + 15); large-small != want {
		t.Fatalf("charges %d and %d differ by %d; 200 further members must cost %d", small, large, large-small, want)
	}

	// 8,419 units for the 400 members, against the default 100,000: the
	// collection is evaluated rather than refused, and it decides — the tokens
	// are pairwise unequal, so clause 3 makes the rule false and the pack falls
	// back rather than escalating an unknown.
	output, failure := engine.EvaluateWith(pack, []byte(facts(400)), nil, draftOptions(0))
	if failure != nil {
		t.Fatalf("400 members at 21 units apiece must fit the default budget: %+v", failure)
	}
	if output.Disposition.Kind != "outcome" || output.Disposition.OutcomeID != "did-not-hold" {
		t.Fatalf("disposition = %+v; distinct huge tokens are unequal, not unknown", output.Disposition)
	}

	// The same members written as one repeated value are equal, which is the
	// other half of determinacy: uniform is true rather than unknown.
	equal := make([]string, 0, 400)
	for index := 0; index < 400; index++ {
		equal = append(equal, `{"cabin":1.0e999999999}`)
	}
	output, failure = engine.EvaluateWith(pack, []byte(fmt.Sprintf(`{"list":[%s]}`, strings.Join(equal, ","))), nil, draftOptions(0))
	if failure != nil {
		t.Fatalf("400 equal members must fit the default budget too: %+v", failure)
	}
	if output.Disposition.Kind != "outcome" || output.Disposition.OutcomeID != "held" {
		t.Fatalf("disposition = %+v; equal huge tokens are equal, not unknown", output.Disposition)
	}
}

// A condition §8 never reaches is never charged, so the total is a property of
// the §8 path rather than of pack and facts alone. A suppressed rule is the
// clearest case: its aggregate costs nothing, and the same pack with the
// suppression withheld exhausts the same budget on the same facts. §8 fixes the
// path, so this is deterministic across evaluators rather than a portability
// hole — but it is the code's behavior, so it is pinned rather than described.
func TestRFC0008SuppressedRuleIsNeverCharged(t *testing.T) {
	engine := newTestEngine(t)
	pack := func(suppressWhen string) []byte {
		return []byte(fmt.Sprintf(`{
  "specVersion": "0.1.0-draft",
  "id": "https://example.invalid/judgment-packs/rfc0008-suppression",
  "version": "0.1.0",
  "title": "Synthetic draft RFC 0008 suppression row",
  "description": "Invented content for specification testing; it authorizes nothing.",
  "decision": {
    "intent": "Exercise the work charge of a rule §8 removes before evaluating it.",
    "question": "Was the expensive rule charged?"
  },
  "outcomes": [
    {"id": "held", "label": "Held"},
    {"id": "did-not-hold", "label": "Did not hold"}
  ],
  "rules": [
    {
      "id": "the-expensive-rule",
      "description": "Quantifies over every element of a long collection.",
      "when": {"op": "exists", "path": "/list", "where": {"op": "fact", "path": "/ok", "operator": "equals", "value": true}},
      "outcome": "held",
      "onUnknown": "escalate"
    }
  ],
  "exceptions": [
    {
      "id": "the-suppression",
      "description": "Removes the expensive rule before it is evaluated.",
      "when": %s,
      "effect": "suppress-rule",
      "targetRule": "the-expensive-rule",
      "onUnknown": "ignore"
    }
  ],
  "fallbackOutcome": "did-not-hold"
}`, suppressWhen))
	}

	elements := make([]string, 0, 20)
	for index := 0; index < 20; index++ {
		elements = append(elements, `{"ok":true}`)
	}
	facts := []byte(fmt.Sprintf(`{"list":[%s]}`, strings.Join(elements, ",")))

	// The exception's literal costs one unit; the rule's aggregate would cost
	// 136. A budget of one unit therefore admits the whole evaluation exactly
	// when the suppressed rule is never charged.
	output, failure := engine.EvaluateWith(pack(`{"op":"literal","value":true}`), facts, nil, draftOptions(1))
	if failure != nil {
		t.Fatalf("a suppressed rule must cost nothing: %+v", failure)
	}
	if output.Disposition.Kind != "outcome" || output.Disposition.OutcomeID != "did-not-hold" {
		t.Fatalf("disposition = %+v", output.Disposition)
	}

	// With the suppression withheld the rule is reached and the same budget is
	// exhausted, so the row above turns on §8's path and not on the numbers.
	_, failure = engine.EvaluateWith(pack(`{"op":"literal","value":false}`), facts, nil, draftOptions(1))
	if failure == nil || failure.Code != "JPS-RESOURCE-EVALUATION-WORK-LIMIT" {
		t.Fatalf("an evaluated rule must be charged: %+v", failure)
	}
}

// The two ragged unknown rows run twice each, once under onUnknown: ignore and
// once under escalate, so the divergence is visible in the disposition.
func TestRFC0008RaggedUnknownUnderBothPolicies(t *testing.T) {
	engine := newTestEngine(t)
	cases := []struct {
		name        string
		condition   string
		facts       string
		onUnknown   string
		wantKind    string
		wantOutcome string
		wantReasons []string
	}{
		{
			name:        "exists-ragged-unknown-ignored",
			condition:   `{"op":"exists","path":"/list","where":{"op":"fact","path":"/ok","operator":"equals","value":true}}`,
			facts:       `{"list":[{"ok":false},{"ok":false},{}]}`,
			onUnknown:   "ignore",
			wantKind:    "outcome",
			wantOutcome: "did-not-hold",
			wantReasons: []string{},
		},
		{
			name:        "exists-ragged-unknown-escalated",
			condition:   `{"op":"exists","path":"/list","where":{"op":"fact","path":"/ok","operator":"equals","value":true}}`,
			facts:       `{"list":[{"ok":false},{"ok":false},{}]}`,
			onUnknown:   "escalate",
			wantKind:    "unresolved",
			wantReasons: []string{"unknown"},
		},
		{
			name:        "every-ragged-unknown-ignored",
			condition:   `{"op":"every","path":"/list","where":{"op":"fact","path":"/ok","operator":"equals","value":true}}`,
			facts:       `{"list":[{"ok":true},{"ok":true},{}]}`,
			onUnknown:   "ignore",
			wantKind:    "outcome",
			wantOutcome: "did-not-hold",
			wantReasons: []string{},
		},
		{
			name:        "every-ragged-unknown-escalated",
			condition:   `{"op":"every","path":"/list","where":{"op":"fact","path":"/ok","operator":"equals","value":true}}`,
			facts:       `{"list":[{"ok":true},{"ok":true},{}]}`,
			onUnknown:   "escalate",
			wantKind:    "unresolved",
			wantReasons: []string{"unknown"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			output, failure := engine.EvaluateWith(draftPack(testCase.condition, testCase.onUnknown), []byte(testCase.facts), nil, draftOptions(0))
			if failure != nil {
				t.Fatalf("evaluation failed: %s", failure.Message)
			}
			if output.Disposition.Kind != testCase.wantKind || output.Disposition.OutcomeID != testCase.wantOutcome {
				t.Fatalf("disposition = %+v", output.Disposition)
			}
			if !reflect.DeepEqual(output.Disposition.Reasons, testCase.wantReasons) {
				t.Fatalf("reasons = %v, want %v", output.Disposition.Reasons, testCase.wantReasons)
			}
		})
	}
}

// The equivalence check the RFC asks any implementation to run, on all three
// census facts a quantifier actually reaches, each as its own pair of packs:
// A6:/reservation/anySegmentCancelledByAirline as an exists over segments
// carrying a cancelledByAirline boolean, R3:/modification/allNewItemsAvailable
// as an every over the modification's items, and
// R5:/request/allNewItemsAvailable as an every over the request's new items.
// R3 and R5 are separate rooms with separate facts, so they are separate
// artifacts here rather than one pack read twice. A prepared-boolean pack and
// its quantifier twin must produce identical dispositions from the facts each is
// given.
func TestRFC0008EquivalenceWithPreparedBooleanPacks(t *testing.T) {
	engine := newTestEngine(t)
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", "rfc0008", name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	airlinePrepared := read("airline-cancellation-prepared.json")
	airlineQuantifier := read("airline-cancellation-quantifier.json")
	itemsPrepared := read("item-availability-prepared.json")
	itemsQuantifier := read("item-availability-quantifier.json")
	exchangePrepared := read("exchange-item-availability-prepared.json")
	exchangeQuantifier := read("exchange-item-availability-quantifier.json")

	cases := []struct {
		name            string
		prepared        []byte
		preparedFacts   string
		quantifier      []byte
		quantifierFacts string
		wantKind        string
		wantOutcome     string
	}{
		{
			name:            "a6-one-segment-cancelled-by-airline",
			prepared:        airlinePrepared,
			preparedFacts:   `{"reservation":{"anySegmentCancelledByAirline":true}}`,
			quantifier:      airlineQuantifier,
			quantifierFacts: `{"reservation":{"segments":[{"cancelledByAirline":false},{"cancelledByAirline":true}]}}`,
			wantKind:        "outcome",
			wantOutcome:     "free-cancellation",
		},
		{
			name:            "a6-no-segment-cancelled-by-airline",
			prepared:        airlinePrepared,
			preparedFacts:   `{"reservation":{"anySegmentCancelledByAirline":false}}`,
			quantifier:      airlineQuantifier,
			quantifierFacts: `{"reservation":{"segments":[{"cancelledByAirline":false},{"cancelledByAirline":false}]}}`,
			wantKind:        "outcome",
			wantOutcome:     "standard-rules",
		},
		{
			name:            "a6-no-segments-at-all",
			prepared:        airlinePrepared,
			preparedFacts:   `{"reservation":{"anySegmentCancelledByAirline":false}}`,
			quantifier:      airlineQuantifier,
			quantifierFacts: `{"reservation":{"segments":[]}}`,
			wantKind:        "outcome",
			wantOutcome:     "standard-rules",
		},
		{
			name:            "a6-fact-absent-on-both-sides",
			prepared:        airlinePrepared,
			preparedFacts:   `{"reservation":{}}`,
			quantifier:      airlineQuantifier,
			quantifierFacts: `{"reservation":{}}`,
			wantKind:        "unresolved",
		},
		{
			name:            "a6-one-segment-missing-the-member",
			prepared:        airlinePrepared,
			preparedFacts:   `{"reservation":{}}`,
			quantifier:      airlineQuantifier,
			quantifierFacts: `{"reservation":{"segments":[{"cancelledByAirline":false},{}]}}`,
			wantKind:        "unresolved",
		},
		{
			name:            "r3-every-item-available",
			prepared:        itemsPrepared,
			preparedFacts:   `{"modification":{"allNewItemsAvailable":true}}`,
			quantifier:      itemsQuantifier,
			quantifierFacts: `{"modification":{"items":[{"availability":"available"},{"availability":"available"}]}}`,
			wantKind:        "outcome",
			wantOutcome:     "modification-allowed",
		},
		{
			name:            "r3-one-item-unavailable",
			prepared:        itemsPrepared,
			preparedFacts:   `{"modification":{"allNewItemsAvailable":false}}`,
			quantifier:      itemsQuantifier,
			quantifierFacts: `{"modification":{"items":[{"availability":"available"},{"availability":"out-of-stock"}]}}`,
			wantKind:        "outcome",
			wantOutcome:     "modification-refused",
		},
		{
			// Vacuous truth, agreed on both sides: a producer computing
			// allNewItemsAvailable over an empty request also reports true. The
			// equivalence holds only because the two artifacts agree about the
			// empty case, which is exactly the authoring footgun the RFC records.
			name:            "r3-no-items-requested",
			prepared:        itemsPrepared,
			preparedFacts:   `{"modification":{"allNewItemsAvailable":true}}`,
			quantifier:      itemsQuantifier,
			quantifierFacts: `{"modification":{"items":[]}}`,
			wantKind:        "outcome",
			wantOutcome:     "modification-allowed",
		},
		{
			name:            "r3-one-item-missing-availability",
			prepared:        itemsPrepared,
			preparedFacts:   `{"modification":{}}`,
			quantifier:      itemsQuantifier,
			quantifierFacts: `{"modification":{"items":[{"availability":"available"},{}]}}`,
			wantKind:        "unresolved",
		},
		{
			name:            "r5-every-new-item-available",
			prepared:        exchangePrepared,
			preparedFacts:   `{"request":{"allNewItemsAvailable":true}}`,
			quantifier:      exchangeQuantifier,
			quantifierFacts: `{"request":{"newItems":[{"availability":"available"},{"availability":"available"}]}}`,
			wantKind:        "outcome",
			wantOutcome:     "exchange-allowed",
		},
		{
			name:            "r5-one-new-item-unavailable",
			prepared:        exchangePrepared,
			preparedFacts:   `{"request":{"allNewItemsAvailable":false}}`,
			quantifier:      exchangeQuantifier,
			quantifierFacts: `{"request":{"newItems":[{"availability":"discontinued"},{"availability":"available"}]}}`,
			wantKind:        "outcome",
			wantOutcome:     "exchange-refused",
		},
		{
			// The same vacuous-truth agreement R3 needs, in the other room: a
			// producer computing allNewItemsAvailable over an empty exchange
			// request also reports true.
			name:            "r5-no-new-items-requested",
			prepared:        exchangePrepared,
			preparedFacts:   `{"request":{"allNewItemsAvailable":true}}`,
			quantifier:      exchangeQuantifier,
			quantifierFacts: `{"request":{"newItems":[]}}`,
			wantKind:        "outcome",
			wantOutcome:     "exchange-allowed",
		},
		{
			// R5's own residue records what the prepared boolean loses here: one
			// item without an availability makes the whole boolean unresolved,
			// and the quantifier is no better at saying which item it was.
			name:            "r5-one-new-item-missing-availability",
			prepared:        exchangePrepared,
			preparedFacts:   `{"request":{}}`,
			quantifier:      exchangeQuantifier,
			quantifierFacts: `{"request":{"newItems":[{"availability":"available"},{}]}}`,
			wantKind:        "unresolved",
		},
		{
			name:            "r5-fact-absent-on-both-sides",
			prepared:        exchangePrepared,
			preparedFacts:   `{"request":{}}`,
			quantifier:      exchangeQuantifier,
			quantifierFacts: `{"request":{}}`,
			wantKind:        "unresolved",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			prepared, failure := engine.Evaluate(testCase.prepared, []byte(testCase.preparedFacts), nil, nil, "test")
			if failure != nil {
				t.Fatalf("the prepared-boolean pack must evaluate without the opt-in: %s", failure.Message)
			}
			quantifier, failure := engine.EvaluateWith(testCase.quantifier, []byte(testCase.quantifierFacts), nil, draftOptions(0))
			if failure != nil {
				t.Fatalf("the quantifier pack must evaluate under the opt-in: %s", failure.Message)
			}
			if !reflect.DeepEqual(prepared.Disposition, quantifier.Disposition) {
				t.Fatalf("dispositions differ:\n prepared=%+v\n quantifier=%+v", prepared.Disposition, quantifier.Disposition)
			}
			if prepared.Disposition.Kind != testCase.wantKind || prepared.Disposition.OutcomeID != testCase.wantOutcome {
				t.Fatalf("disposition = %+v, want kind %q outcome %q", prepared.Disposition, testCase.wantKind, testCase.wantOutcome)
			}
			// Only the quantifier twin is a draft-RFC prototype, and only it is
			// refused by the published grammar.
			if prepared.DraftPrototype != nil {
				t.Fatal("a Core pack evaluated without the opt-in must carry no prototype marker")
			}
			if quantifier.DraftPrototype == nil || quantifier.DraftPrototype.PackValidUnderSpecVersion {
				t.Fatalf("marker = %+v", quantifier.DraftPrototype)
			}
		})
	}

	// The quantifier twins are exactly the packs the published validator
	// rejects; the prepared twins are exactly the packs it accepts.
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for name, pack := range map[string][]byte{
		"airline-cancellation-quantifier":       airlineQuantifier,
		"item-availability-quantifier":          itemsQuantifier,
		"exchange-item-availability-quantifier": exchangeQuantifier,
	} {
		output, operational := validator.Validate(pack, validation.Options{Through: "semantic", Limits: carrier.DefaultLimits()})
		if operational != nil || output.Status != "invalid" {
			t.Fatalf("%s must be invalid under the published schema: %+v", name, output.Status)
		}
	}
	for name, pack := range map[string][]byte{
		"airline-cancellation-prepared":       airlinePrepared,
		"item-availability-prepared":          itemsPrepared,
		"exchange-item-availability-prepared": exchangePrepared,
	} {
		output, operational := validator.Validate(pack, validation.Options{Through: "semantic", Limits: carrier.DefaultLimits()})
		if operational != nil || output.Status != "valid" {
			t.Fatalf("%s must be valid under the published schema: %+v", name, output.Status)
		}
	}
}
