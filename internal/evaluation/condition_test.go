package evaluation

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
)

// evalCondition applies §7's experimental interpretation, as pinned by spec
// RFC 0006, to one condition node, with no draft grammar and no work charge: the
// surface every row in this file is written against. It calls condition rather
// than evaluate, so a row states the §7 verdict alone; the limit the engine
// charges around it is coreEvaluator's and is exercised in limits_test.go.
func evalCondition(node any, facts any, evidence map[string]tri) tri {
	return (&evaluator{evidence: evidence}).condition(node, facts)
}

// coreEvaluator is one Core-path evaluator with this runtime's documented §10
// evaluation-work limit in force, which is what the engine builds. The limit is
// stated rather than left at the zero value because a zero budget is exhausted by
// the first unit charged: every §8 walk is metered, so an unmetered evaluator is
// not a lighter version of the real one but a refused evaluation.
func coreEvaluator() *evaluator {
	return &evaluator{evidence: map[string]tri{}, budget: DefaultCoreWorkLimit}
}

func TestDecimalCompare(t *testing.T) {
	cases := []struct {
		fact, operand any
		want          int
		comparable    bool
	}{
		{"100", "99", 1, true},
		{"99", "100", -1, true},
		{"5000", "5000", 0, true},
		{"5000.00", "5000", 0, true}, // mathematical value, not lexical
		{"-0.5", "0", -1, true},
		{json.Number("100"), "99", 0, false}, // JSON numbers are not decimals (RFC 0006)
		{"0100", "99", 0, false},             // leading zero not admitted
		{"1e3", "99", 0, false},              // exponent not admitted
		{"+1", "1", 0, false},                // leading plus not admitted
		{"1.", "1", 0, false},                // fraction needs digits
		{true, "1", 0, false},
		{nil, "1", 0, false},
	}
	for _, testCase := range cases {
		got, comparable := decimalCompare(testCase.fact, testCase.operand)
		if comparable != testCase.comparable || (comparable && got != testCase.want) {
			t.Errorf("decimalCompare(%v, %v) = (%d, %v), want (%d, %v)",
				testCase.fact, testCase.operand, got, comparable, testCase.want, testCase.comparable)
		}
	}
}

func TestResolvePointer(t *testing.T) {
	document := map[string]any{
		"a":   map[string]any{"b/c": "slash", "~d": "tilde"},
		"arr": []any{"zero", "one"},
	}
	cases := []struct {
		pointer  string
		want     any
		resolved bool
	}{
		{"", document, true},
		{"/a/b~1c", "slash", true},
		{"/a/~0d", "tilde", true},
		{"/arr/0", "zero", true},
		{"/arr/1", "one", true},
		{"/arr/2", nil, false},  // out of range
		{"/arr/01", nil, false}, // leading zero
		{"/arr/-", nil, false},  // past-the-end never resolves
		{"/missing", nil, false},
		{"/arr/0/x", nil, false}, // traversal into a non-container
		{"a", nil, false},        // no leading slash
	}
	for _, testCase := range cases {
		got, resolved := resolvePointer(document, testCase.pointer)
		if resolved != testCase.resolved {
			t.Errorf("resolvePointer(%q) resolved = %v, want %v", testCase.pointer, resolved, testCase.resolved)
			continue
		}
		if resolved && !reflect.DeepEqual(got, testCase.want) {
			t.Errorf("resolvePointer(%q) = %v, want %v", testCase.pointer, got, testCase.want)
		}
	}
}

// §7.4 equality, including its totality: every pair below is equal or unequal,
// and no pair is anything else. The rows carrying numbers no machine word or
// big.Rat can hold are the point — they are decided on the tokens, so the
// answer is a property of the two numbers rather than of the arithmetic that
// happened to be available.
func TestJSONEqualIsTypePreserving(t *testing.T) {
	cases := []struct {
		a, b any
		want bool
	}{
		{json.Number("5.0"), json.Number("5"), true}, // mathematical value
		{json.Number("5"), "5", false},               // no coercion across types
		{"5", json.Number("5"), false},
		{true, "true", false},
		{nil, nil, true},
		{[]any{json.Number("1"), "x"}, []any{json.Number("1.0"), "x"}, true},
		{[]any{"x", "y"}, []any{"y", "x"}, false}, // arrays compare in order
		{map[string]any{"a": json.Number("1"), "b": "z"}, map[string]any{"b": "z", "a": json.Number("1.00")}, true},
		{map[string]any{"a": "1"}, map[string]any{"a": "1", "b": "2"}, false},
		// Normal forms: an exponent, a shifted point, and a signed zero are
		// notations, not values.
		{json.Number("1e3"), json.Number("1000"), true},
		{json.Number("1000"), json.Number("1.0e3"), true},
		{json.Number("-0"), json.Number("0"), true},
		{json.Number("0.0e10"), json.Number("-0.000"), true},
		{json.Number("1.23e4"), json.Number("12300"), true},
		{json.Number("1E+3"), json.Number("1000"), true},
		{json.Number("1e-2"), json.Number("0.01"), true},
		{json.Number("0e999999999"), json.Number("-0"), true},
		{json.Number("-1.5"), json.Number("1.5"), false},
		{json.Number("1e3"), json.Number("1e4"), false},
		{json.Number("1e-2"), json.Number("0.1"), false},
		// Numbers past every arithmetic type still compare exactly, in both
		// directions: unequal ones are unequal...
		{json.Number("1e999999999"), json.Number("2e999999999"), false},
		{json.Number("1e999999999"), json.Number("1e999999998"), false},
		{[]any{json.Number("1e999999999")}, []any{json.Number("2e999999999")}, false},
		// ...and equal ones are equal, whether the tokens match or not.
		{json.Number("1e999999999"), json.Number("1e999999999"), true},
		{json.Number("1e999999999"), json.Number("1.0e999999999"), true},
		{json.Number("10e999999998"), json.Number("1e999999999"), true},
		{json.Number("-1e999999999"), json.Number("1e999999999"), false},
		// A definite mismatch elsewhere still decides the comparison.
		{[]any{json.Number("1e999999999"), "x"}, []any{json.Number("2e999999999"), "y"}, false},
	}
	for _, testCase := range cases {
		if got := jsonEqual(testCase.a, testCase.b); got != testCase.want {
			t.Errorf("jsonEqual(%v, %v) = %v, want %v", testCase.a, testCase.b, got, testCase.want)
		}
		// Equality is symmetric, which a normal-form comparison must not lose.
		if got := jsonEqual(testCase.b, testCase.a); got != testCase.want {
			t.Errorf("jsonEqual(%v, %v) = %v, want %v", testCase.b, testCase.a, got, testCase.want)
		}
	}
}

func TestThreeValuedOperators(t *testing.T) {
	facts := map[string]any{"known": "yes"}
	unknownFact := map[string]any{"op": "fact", "path": "/missing", "operator": "equals", "value": "x"}
	trueFact := map[string]any{"op": "fact", "path": "/known", "operator": "equals", "value": "yes"}
	falseFact := map[string]any{"op": "fact", "path": "/known", "operator": "equals", "value": "no"}

	cases := []struct {
		name string
		node map[string]any
		want tri
	}{
		{"all-false-beats-unknown", map[string]any{"op": "all", "conditions": []any{unknownFact, falseFact}}, triFalse},
		{"all-unknown", map[string]any{"op": "all", "conditions": []any{unknownFact, trueFact}}, triUnknown},
		{"all-true", map[string]any{"op": "all", "conditions": []any{trueFact, trueFact}}, triTrue},
		{"any-true-beats-unknown", map[string]any{"op": "any", "conditions": []any{unknownFact, trueFact}}, triTrue},
		{"any-unknown", map[string]any{"op": "any", "conditions": []any{unknownFact, falseFact}}, triUnknown},
		{"any-false", map[string]any{"op": "any", "conditions": []any{falseFact, falseFact}}, triFalse},
		{"not-unknown-stays-unknown", map[string]any{"op": "not", "condition": unknownFact}, triUnknown},
		{"not-true", map[string]any{"op": "not", "condition": trueFact}, triFalse},
		{"literal", map[string]any{"op": "literal", "value": true}, triTrue},
		{"in-no-match-is-false", map[string]any{"op": "fact", "path": "/known", "operator": "in", "value": []any{"a", "b"}}, triFalse},
		{"in-match", map[string]any{"op": "fact", "path": "/known", "operator": "in", "value": []any{"yes"}}, triTrue},
		{"ordered-number-fact-unknown", map[string]any{"op": "fact", "path": "/known", "operator": "greater-than", "value": "1"}, triUnknown},
	}
	for _, testCase := range cases {
		if got := evalCondition(testCase.node, facts, map[string]tri{}); got != testCase.want {
			t.Errorf("%s = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

// The draft RFC 0008 aggregates are inert without the opt-in. No published
// surface reaches this state — the validator rejects a pack carrying an
// aggregate before evaluation begins — so the rows are written against the
// evaluator directly, which is the only caller that could ever bypass the gate.
func TestAggregatesAreUnknownWithoutTheOptIn(t *testing.T) {
	facts := map[string]any{"list": []any{map[string]any{"ok": true}}}
	predicate := map[string]any{"op": "fact", "path": "/ok", "operator": "equals", "value": true}
	cases := []struct {
		name string
		node map[string]any
	}{
		{"exists", map[string]any{"op": "exists", "path": "/list", "where": predicate}},
		{"every", map[string]any{"op": "every", "path": "/list", "where": predicate}},
		{"uniform", map[string]any{"op": "uniform", "path": "/list", "at": "/ok"}},
	}
	for _, testCase := range cases {
		if got := evalCondition(testCase.node, facts, map[string]tri{}); got != triUnknown {
			t.Errorf("%s without the opt-in = %v, want unknown", testCase.name, got)
		}
	}
}

// Resolver edges exercised directly, without the validation gate, so states no
// conformant single pack can combine are still pinned.
func TestResolverEdges(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"outcomes": []any{
				map[string]any{"id": "a"}, map[string]any{"id": "b"},
			},
		}
	}

	t.Run("direct-escalation-without-escalation-object", func(t *testing.T) {
		pack := base()
		pack["rules"] = []any{map[string]any{"id": "r", "when": map[string]any{"op": "literal", "value": true}, "outcome": "a", "onUnknown": "ignore"}}
		pack["exceptions"] = []any{map[string]any{"id": "x", "when": map[string]any{"op": "literal", "value": true}, "effect": "escalate", "onUnknown": "ignore"}}
		disposition, target, _, _ := resolve(pack, map[string]any{}, coreEvaluator())
		if disposition.Kind != "unresolved" || !reflect.DeepEqual(disposition.Reasons, []string{"exception-escalation"}) {
			t.Fatalf("direct escalation must be unresolved: %+v", disposition)
		}
		if disposition.Handoff.State != "requested" || !reflect.DeepEqual(disposition.Handoff.TriggeredBy, []string{"exception-escalation"}) {
			t.Fatalf("a direct request is triggered by exception-escalation: %+v", disposition.Handoff)
		}
		if target != nil {
			t.Fatalf("a direct request without an escalation object has no Core-defined destination: %+v", target)
		}
	})

	t.Run("conflicting-forced-outcomes", func(t *testing.T) {
		pack := base()
		pack["exceptions"] = []any{
			map[string]any{"id": "x1", "when": map[string]any{"op": "literal", "value": true}, "effect": "force-outcome", "outcome": "a", "onUnknown": "ignore"},
			map[string]any{"id": "x2", "when": map[string]any{"op": "literal", "value": true}, "effect": "force-outcome", "outcome": "b", "onUnknown": "ignore"},
		}
		disposition, _, _, _ := resolve(pack, map[string]any{}, coreEvaluator())
		if disposition.Kind != "unresolved" || !reflect.DeepEqual(disposition.Reasons, []string{"conflict"}) {
			t.Fatalf("incompatible forced outcomes must conflict: %+v", disposition)
		}
	})

	t.Run("suppressed-rule-restores-fallback", func(t *testing.T) {
		pack := base()
		pack["rules"] = []any{map[string]any{"id": "r", "when": map[string]any{"op": "literal", "value": true}, "outcome": "a", "onUnknown": "ignore"}}
		pack["exceptions"] = []any{map[string]any{"id": "x", "when": map[string]any{"op": "literal", "value": true}, "effect": "suppress-rule", "targetRule": "r", "onUnknown": "ignore"}}
		pack["fallbackOutcome"] = "b"
		disposition, _, trace, _ := resolve(pack, map[string]any{}, coreEvaluator())
		if disposition.Kind != "outcome" || disposition.OutcomeID != "b" {
			t.Fatalf("with the only rule suppressed the fallback applies: %+v", disposition)
		}
		found := false
		for _, entry := range trace {
			if entry.Stage == "rule" && entry.ID == "r" && entry.Suppressed {
				found = true
			}
		}
		if !found {
			t.Fatalf("the suppressed rule must stay visible in the trace: %+v", trace)
		}
	})

	t.Run("no-match-without-fallback", func(t *testing.T) {
		pack := base()
		pack["rules"] = []any{map[string]any{"id": "r", "when": map[string]any{"op": "literal", "value": false}, "outcome": "a", "onUnknown": "ignore"}}
		disposition, _, _, _ := resolve(pack, map[string]any{}, coreEvaluator())
		if disposition.Kind != "unresolved" || !reflect.DeepEqual(disposition.Reasons, []string{"no-match"}) {
			t.Fatalf("no rule and no fallback is no-match: %+v", disposition)
		}
	})

	t.Run("unknown-rule-ignore-does-not-block-fallback", func(t *testing.T) {
		pack := base()
		pack["rules"] = []any{map[string]any{"id": "r", "when": map[string]any{"op": "fact", "path": "/missing", "operator": "equals", "value": "x"}, "outcome": "a", "onUnknown": "ignore"}}
		pack["fallbackOutcome"] = "b"
		disposition, _, trace, _ := resolve(pack, map[string]any{}, coreEvaluator())
		if disposition.Kind != "outcome" || disposition.OutcomeID != "b" {
			t.Fatalf("an ignored unknown must not block the fallback: %+v", disposition)
		}
		visible := false
		for _, entry := range trace {
			if entry.Stage == "rule" && entry.Condition == "unknown" && entry.OnUnknown == "ignore" {
				visible = true
			}
		}
		if !visible {
			t.Fatalf("an ignored unknown stays visible in the trace: %+v", trace)
		}
	})

	t.Run("unknown-rule-escalate-blocks-fallback", func(t *testing.T) {
		pack := base()
		pack["rules"] = []any{map[string]any{"id": "r", "when": map[string]any{"op": "fact", "path": "/missing", "operator": "equals", "value": "x"}, "outcome": "a", "onUnknown": "escalate"}}
		pack["fallbackOutcome"] = "b"
		disposition, _, _, _ := resolve(pack, map[string]any{}, coreEvaluator())
		if disposition.Kind != "unresolved" || !reflect.DeepEqual(disposition.Reasons, []string{"unknown"}) {
			t.Fatalf("an escalating unknown blocks both candidate and fallback: %+v", disposition)
		}
	})

	t.Run("retains-both-unknown-and-conflict", func(t *testing.T) {
		pack := base()
		pack["rules"] = []any{
			map[string]any{"id": "r1", "when": map[string]any{"op": "literal", "value": true}, "outcome": "a", "onUnknown": "ignore"},
			map[string]any{"id": "r2", "when": map[string]any{"op": "literal", "value": true}, "outcome": "b", "onUnknown": "ignore"},
			map[string]any{"id": "r3", "when": map[string]any{"op": "fact", "path": "/missing", "operator": "equals", "value": "x"}, "outcome": "a", "onUnknown": "escalate"},
		}
		disposition, _, _, _ := resolve(pack, map[string]any{}, coreEvaluator())
		if !reflect.DeepEqual(disposition.Reasons, []string{"conflict", "unknown"}) {
			t.Fatalf("neither unknown nor conflict is discarded: %+v", disposition)
		}
	})
}

// evidence-present maps the tri-state availability directly (§7.5): present is
// true, absent is false, unknown or undeclared is unknown.
func TestEvidencePresentTriState(t *testing.T) {
	evidence := map[string]tri{"a": triTrue, "b": triFalse, "c": triUnknown}
	cases := []struct {
		name string
		want tri
	}{{"a", triTrue}, {"b", triFalse}, {"c", triUnknown}, {"undeclared", triUnknown}}
	for _, testCase := range cases {
		node := map[string]any{"op": "evidence-present", "evidenceRequirement": testCase.name}
		if got := evalCondition(node, nil, evidence); got != testCase.want {
			t.Errorf("evidence-present(%s) = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

// The two comparison families at the condition level, and the line between
// them. Ordered comparison is defined only over §2.2 decimal strings, so a JSON
// number or a non-grammar string degrades to unknown; equality is total, so
// every equals, not-equals, and in below decides — including over numbers no
// arithmetic type can hold, which produce false or true and never unknown.
func TestOrderedAndEqualityConditions(t *testing.T) {
	facts := map[string]any{
		"amount": "150",
		"count":  json.Number("150"),
		"huge":   json.Number("1e999999999"),
	}
	cases := []struct {
		name string
		node map[string]any
		want tri
	}{
		{"decimal-string-greater", map[string]any{"op": "fact", "path": "/amount", "operator": "greater-than", "value": "100"}, triTrue},
		{"decimal-string-not-less", map[string]any{"op": "fact", "path": "/amount", "operator": "less-than", "value": "100"}, triFalse},
		{"json-number-ordered-unknown", map[string]any{"op": "fact", "path": "/count", "operator": "greater-than", "value": "100"}, triUnknown},
		{"huge-number-equals-false", map[string]any{"op": "fact", "path": "/huge", "operator": "equals", "value": json.Number("2e999999999")}, triFalse},
		{"huge-number-not-equals-true", map[string]any{"op": "fact", "path": "/huge", "operator": "not-equals", "value": json.Number("2e999999999")}, triTrue},
		{"huge-identical-token-equal", map[string]any{"op": "fact", "path": "/huge", "operator": "equals", "value": json.Number("1e999999999")}, triTrue},
		{"huge-equal-under-a-different-token", map[string]any{"op": "fact", "path": "/huge", "operator": "equals", "value": json.Number("1.0e999999999")}, triTrue},
		{"in-over-huge-tokens-decides-false", map[string]any{"op": "fact", "path": "/huge", "operator": "in", "value": []any{json.Number("2e999999999"), "x"}}, triFalse},
		{"in-over-huge-tokens-decides-true", map[string]any{"op": "fact", "path": "/huge", "operator": "in", "value": []any{json.Number("2e999999999"), json.Number("10e999999998")}}, triTrue},
	}
	for _, testCase := range cases {
		if got := evalCondition(testCase.node, facts, map[string]tri{}); got != testCase.want {
			t.Errorf("%s = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

// §8.1: the configured target is requested only when a retained reason appears
// in escalation.triggers — except a direct exception escalation, which is a
// request regardless of the trigger list.
func TestHandoffTriggerMatching(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"outcomes": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}},
			"rules": []any{
				map[string]any{"id": "r", "when": map[string]any{"op": "literal", "value": false}, "outcome": "a", "onUnknown": "ignore"},
			},
		}
	}

	t.Run("reason-not-in-triggers-means-no-handoff", func(t *testing.T) {
		pack := base() // no fallback -> unresolved{no-match}
		pack["escalation"] = map[string]any{
			"triggers": []any{"conflict"},
			"target":   map[string]any{"kind": "human-role", "name": "Reviewer"},
		}
		disposition, _, _, _ := resolve(pack, map[string]any{}, coreEvaluator())
		if !reflect.DeepEqual(disposition.Reasons, []string{"no-match"}) {
			t.Fatalf("expected no-match: %+v", disposition)
		}
		if disposition.Handoff.State != "none" {
			t.Fatalf("no-match is not in the trigger list, so no handoff is configured: %+v", disposition.Handoff)
		}
	})

	t.Run("matching-trigger-echoes-target", func(t *testing.T) {
		pack := base()
		pack["escalation"] = map[string]any{
			"triggers": []any{"no-match"},
			"target":   map[string]any{"kind": "team", "name": "Intake board"},
		}
		disposition, target, _, _ := resolve(pack, map[string]any{}, coreEvaluator())
		if disposition.Handoff.State != "requested" || !reflect.DeepEqual(disposition.Handoff.TriggeredBy, []string{"no-match"}) {
			t.Fatalf("a matching trigger must be named by triggeredBy: %+v", disposition.Handoff)
		}
		if target == nil || target.Kind != "team" || target.Name != "Intake board" {
			t.Fatalf("a requested handoff must echo the declared target exactly, beside the disposition: %+v", target)
		}
	})

	t.Run("direct-escalation-ignores-trigger-list", func(t *testing.T) {
		pack := base()
		pack["exceptions"] = []any{
			map[string]any{"id": "x", "when": map[string]any{"op": "literal", "value": true}, "effect": "escalate", "onUnknown": "ignore"},
		}
		pack["escalation"] = map[string]any{
			"triggers": []any{"conflict"}, // exception-escalation is deliberately absent
			"target":   map[string]any{"kind": "human-role", "name": "Escalation desk"},
		}
		disposition, target, _, _ := resolve(pack, map[string]any{}, coreEvaluator())
		if disposition.Handoff.State != "requested" || !reflect.DeepEqual(disposition.Handoff.TriggeredBy, []string{"exception-escalation"}) {
			t.Fatalf("a direct request is triggered by exception-escalation even when the trigger list omits it: %+v", disposition.Handoff)
		}
		if target == nil || target.Name != "Escalation desk" {
			t.Fatalf("a direct request uses the configured target regardless of the trigger list: %+v", target)
		}
	})

	t.Run("escalate-takes-precedence-over-force", func(t *testing.T) {
		pack := base()
		pack["exceptions"] = []any{
			map[string]any{"id": "force", "when": map[string]any{"op": "literal", "value": true}, "effect": "force-outcome", "outcome": "b", "onUnknown": "ignore"},
			map[string]any{"id": "esc", "when": map[string]any{"op": "literal", "value": true}, "effect": "escalate", "onUnknown": "ignore"},
		}
		disposition, _, _, _ := resolve(pack, map[string]any{}, coreEvaluator())
		if disposition.Kind != "unresolved" || !reflect.DeepEqual(disposition.Reasons, []string{"exception-escalation"}) {
			t.Fatalf("a direct escalation takes precedence over a compatible forced outcome: %+v", disposition)
		}
	})
}

// The ordered-operator set is stated once and read by two packages, so it is
// held to the bundled schema rather than to a second hand-written list: the
// schema pins exactly the operators whose operand must be a §2.2 decimal
// string, and those are exactly the operators decimalCompare decides. An
// operator added to one and not the other fails here. The test reads the set
// through the exported predicate as an importer does, and the unexported map
// for the reverse direction, which is the half a predicate cannot answer.
func TestOrderedOperatorsMatchTheSchemasDecimalOperandRule(t *testing.T) {
	set, err := artifacts.Load(artifacts.EvaluatorDraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	data, err := set.Schema()
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs struct {
			Condition struct {
				OneOf []struct {
					AllOf []struct {
						If struct {
							Properties struct {
								Operator struct {
									Enum []string `json:"enum"`
								} `json:"operator"`
							} `json:"properties"`
						} `json:"if"`
						Then struct {
							Properties struct {
								Value struct {
									Ref string `json:"$ref"`
								} `json:"value"`
							} `json:"properties"`
						} `json:"then"`
					} `json:"allOf"`
				} `json:"oneOf"`
			} `json:"condition"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, member := range schema.Defs.Condition.OneOf {
		for _, rule := range member.AllOf {
			if rule.Then.Properties.Value.Ref != "#/$defs/decimalString" {
				continue
			}
			for _, operator := range rule.If.Properties.Operator.Enum {
				declared[operator] = true
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("the schema must pin some operator's operand to a decimal string, or this test guards nothing")
	}
	for operator := range declared {
		if !OrderedOperator(operator) {
			t.Fatalf("the schema pins %q to a decimal operand and the evaluator does not order it", operator)
		}
	}
	if !reflect.DeepEqual(declared, orderedOperators) {
		t.Fatalf("the schema declares %v, the evaluator orders %v", declared, orderedOperators)
	}
}
