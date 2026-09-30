package evaluation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// These rows pin ADR-0040: what an unknown entry records as its causes, what an
// entry records about equality comparisons across JSON types, and what the
// payload records about the evidence §8 step 2 found wanting. Each is checked
// as the serialized bytes a caller receives, as ADR-0027's goldens are.

func fact(path, operator string, value any) map[string]any {
	return map[string]any{"op": "fact", "path": path, "operator": operator, "value": value}
}

// stage evaluates one condition as a stage is evaluated and serializes what
// the stage would record: the verdict, and the two ADR-0040 members.
func stage(t *testing.T, e *evaluator, node any, facts any) (string, string, string) {
	t.Helper()
	verdict, causes, mismatches := e.evaluateStage(node, facts)
	encodedCauses, err := json.Marshal(causes)
	if err != nil {
		t.Fatal(err)
	}
	encodedMismatches, err := json.Marshal(mismatches)
	if err != nil {
		t.Fatal(err)
	}
	return verdict.String(), string(encodedCauses), string(encodedMismatches)
}

func facts(t *testing.T, text string) any {
	t.Helper()
	var document any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	return document
}

// A cause is recorded where a leaf goes unknown, and names why: a pointer that
// selects nothing, a value an ordered comparison cannot compare (with the JSON
// type it has), an evidence requirement whose presence is unknown whether
// stated or omitted.
func TestAnUnknownLeafNamesItsCause(t *testing.T) {
	e := &evaluator{
		evidence: map[string]tri{"receipt": triUnknown, "stated": triTrue},
		budget:   DefaultCoreWorkLimit,
	}
	document := facts(t, `{"refund":{"amount":9000,"code":"A1"}}`)
	for _, tc := range []struct {
		name   string
		node   map[string]any
		causes string
	}{
		{"absent", fact("/refund/missing", "equals", "x"), `[{"path":"/refund/missing","cause":"absent"}]`},
		{"number where a decimal string is required", fact("/refund/amount", "greater-than", "500"), `[{"path":"/refund/amount","cause":"not-comparable","factType":"number"}]`},
		{"string that is not a decimal", fact("/refund/code", "less-than", "5"), `[{"path":"/refund/code","cause":"not-comparable","factType":"string"}]`},
		{"evidence stated unknown", map[string]any{"op": "evidence-present", "evidenceRequirement": "receipt"}, `[{"evidenceRequirement":"receipt","cause":"unknown"}]`},
		{"evidence omitted", map[string]any{"op": "evidence-present", "evidenceRequirement": "omitted"}, `[{"evidenceRequirement":"omitted","cause":"unknown"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verdict, causes, _ := stage(t, e, tc.node, document)
			if verdict != "unknown" || causes != tc.causes {
				t.Fatalf("verdict %s causes %s, want unknown %s", verdict, causes, tc.causes)
			}
		})
	}
}

// Only a cause of the verdict is a cause. Under strong three-valued logic an
// all is false as soon as one child is false and an any is true as soon as one
// child is true, whatever the others are; an unknown those children overrode
// explained nothing, and recording it would name a fact whose absence changed
// no outcome. When the combinator is itself unknown, every unknown child it saw
// is a cause, in the order the walk met them.
func TestAnOverriddenUnknownIsNotACause(t *testing.T) {
	e := &evaluator{evidence: map[string]tri{}, budget: DefaultCoreWorkLimit}
	document := facts(t, `{"a":"1"}`)
	absentB := fact("/b", "equals", "x")
	absentC := fact("/c", "equals", "x")
	falseA := fact("/a", "equals", "2")
	trueA := fact("/a", "equals", "1")
	for _, tc := range []struct {
		name    string
		node    map[string]any
		verdict string
		causes  string
	}{
		{"all overridden by a false child", map[string]any{"op": "all", "conditions": []any{absentB, falseA}}, "false", `null`},
		{"any overridden by a true child", map[string]any{"op": "any", "conditions": []any{absentB, trueA}}, "true", `null`},
		{"all left unknown by two children", map[string]any{"op": "all", "conditions": []any{absentB, trueA, absentC}}, "unknown", `[{"path":"/b","cause":"absent"},{"path":"/c","cause":"absent"}]`},
		{"an inner override inside an outer unknown", map[string]any{"op": "all", "conditions": []any{
			map[string]any{"op": "any", "conditions": []any{absentB, trueA}},
			absentC,
		}}, "unknown", `[{"path":"/c","cause":"absent"}]`},
		{"an inner all overridden inside an outer unknown", map[string]any{"op": "any", "conditions": []any{
			map[string]any{"op": "all", "conditions": []any{absentB, falseA}},
			absentC,
		}}, "unknown", `[{"path":"/c","cause":"absent"}]`},
		{"not passes its child's unknown through", map[string]any{"op": "not", "condition": absentB}, "unknown", `[{"path":"/b","cause":"absent"}]`},
		{"one pointer read twice is one cause", map[string]any{"op": "all", "conditions": []any{absentB, fact("/b", "not-equals", "y")}}, "unknown", `[{"path":"/b","cause":"absent"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verdict, causes, _ := stage(t, e, tc.node, document)
			if verdict != tc.verdict || causes != tc.causes {
				t.Fatalf("verdict %s causes %s, want %s %s", verdict, causes, tc.verdict, tc.causes)
			}
		})
	}
}

// An equality comparison across JSON types is recorded whatever its verdict:
// §7.4 does not coerce, so "true" never equals true and 1 never equals "1", and
// the verdict alone cannot say that the values could not have been equal. A
// comparison of one type is not recorded, nor an in whose operand shares the
// fact's type in any member.
func TestAnEqualityAcrossJSONTypesIsRecorded(t *testing.T) {
	e := &evaluator{evidence: map[string]tri{}, budget: DefaultCoreWorkLimit}
	document := facts(t, `{"expense":{"flag":"true","count":1,"state":"open"}}`)
	for _, tc := range []struct {
		name       string
		node       map[string]any
		verdict    string
		mismatches string
	}{
		{"string against boolean", fact("/expense/flag", "equals", true), "false", `[{"path":"/expense/flag","operator":"equals","factType":"string","operandTypes":["boolean"]}]`},
		{"number against boolean, not-equals", fact("/expense/count", "not-equals", true), "true", `[{"path":"/expense/count","operator":"not-equals","factType":"number","operandTypes":["boolean"]}]`},
		{"string against no member's type", fact("/expense/state", "in", []any{true, json.Number("2")}), "false", `[{"path":"/expense/state","operator":"in","factType":"string","operandTypes":["boolean","number"]}]`},
		{"in with a member of the fact's type", fact("/expense/state", "in", []any{true, "closed"}), "false", `null`},
		{"same type", fact("/expense/state", "equals", "closed"), "false", `null`},
		{"inside an all a sibling decided", map[string]any{"op": "all", "conditions": []any{fact("/expense/flag", "equals", true), fact("/expense/state", "equals", "open")}}, "false", `[{"path":"/expense/flag","operator":"equals","factType":"string","operandTypes":["boolean"]}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verdict, _, mismatches := stage(t, e, tc.node, document)
			if verdict != tc.verdict || mismatches != tc.mismatches {
				t.Fatalf("verdict %s mismatches %s, want %s %s", verdict, mismatches, tc.verdict, tc.mismatches)
			}
		})
	}
}

// Under the draft RFC 0008 opt-in, a leaf inside a quantifier's where is
// resolved against an element, so its cause names the collection it was read
// in; a collection pointer that selects nothing, or selects something that is
// not an array, is a cause of its own at the level the aggregate was written.
func TestACauseInsideAQuantifierNamesItsCollection(t *testing.T) {
	e := &evaluator{evidence: map[string]tri{}, quantifiers: true, budget: DefaultWorkBudget}
	document := facts(t, `{"lines":[{"sku":"a"},{}],"notArray":"x"}`)
	for _, tc := range []struct {
		name   string
		node   map[string]any
		causes string
	}{
		{"element leaf", map[string]any{"op": "every", "path": "/lines", "where": fact("/sku", "equals", "a")},
			`[{"path":"/sku","within":"/lines","cause":"absent"}]`},
		{"absent collection", map[string]any{"op": "exists", "path": "/missing", "where": fact("/sku", "equals", "a")},
			`[{"path":"/missing","cause":"absent"}]`},
		{"collection that is not an array", map[string]any{"op": "exists", "path": "/notArray", "where": fact("/sku", "equals", "a")},
			`[{"path":"/notArray","cause":"not-an-array","factType":"string"}]`},
		{"uniform whose at a member lacks", map[string]any{"op": "uniform", "path": "/lines", "at": "/sku"},
			`[{"path":"/sku","within":"/lines","cause":"absent"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verdict, causes, _ := stage(t, e, tc.node, document)
			if verdict != "unknown" || causes != tc.causes {
				t.Fatalf("verdict %s causes %s, want unknown %s", verdict, causes, tc.causes)
			}
		})
	}
	// A quantifier that decided despite an unknown element records no cause.
	verdict, causes, _ := stage(t, e, map[string]any{"op": "exists", "path": "/lines", "where": fact("/sku", "equals", "a")}, document)
	if verdict != "true" || causes != "null" {
		t.Fatalf("an exists a member decided records no cause: %s %s", verdict, causes)
	}
}

// Step 2 names the requirements it found wanting, beside the trace and in the
// order the pack declares them: absent and unknown both stop the walk, so both
// are named. A present requirement, an optional one, and a pack whose required
// evidence is all present name nothing, and the member is then absent from
// the payload rather than empty.
func TestStepTwoNamesTheRequirementsItFoundWanting(t *testing.T) {
	pack := map[string]any{
		"outcomes": []any{map[string]any{"id": "a"}},
		"evidenceRequirements": []any{
			map[string]any{"id": "e-maybe", "required": true},
			map[string]any{"id": "e-yes", "required": true},
			map[string]any{"id": "e-optional", "required": false},
			map[string]any{"id": "e-no", "required": true},
		},
		"rules": []any{map[string]any{"id": "r1", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"}},
	}
	walk := &evaluator{
		evidence: map[string]tri{"e-yes": triTrue, "e-no": triFalse, "e-maybe": triUnknown, "e-optional": triFalse},
		budget:   DefaultCoreWorkLimit,
	}
	disposition, _, trace, failure := resolve(pack, map[string]any{}, walk)
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if disposition.Kind != "unresolved" || len(trace) != 0 {
		t.Fatalf("the walk stops at step 2 and traces no stage: %+v %s", disposition, traceJSON(t, trace))
	}
	encoded, err := json.Marshal(walk.unmetEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if want := `[{"requirement":"e-maybe","state":"unknown"},{"requirement":"e-no","state":"absent"}]`; string(encoded) != want {
		t.Fatalf("unmet evidence = %s, want %s", encoded, want)
	}

	met := &evaluator{evidence: map[string]tri{"e-yes": triTrue, "e-no": triTrue, "e-maybe": triTrue}, budget: DefaultCoreWorkLimit}
	metDisposition, _, _, failure := resolve(pack, map[string]any{}, met)
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if met.unmetEvidence != nil {
		t.Fatalf("met evidence names nothing: %v", met.unmetEvidence)
	}
	payload, err := json.Marshal(result.Evaluation{Disposition: metDisposition, UnmetEvidence: met.unmetEvidence, Trace: []result.TraceEntry{}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "unmetEvidence") {
		t.Fatalf("an evaluation nothing stopped at step 2 carries no unmetEvidence member: %s", payload)
	}
}

// The engine carries step 2's findings into the payload, where a caller reads
// them, and a successful evaluation that step 2 did not stop carries none.
func TestThePayloadCarriesUnmetEvidence(t *testing.T) {
	engine := admittedEngine(t)
	pack := []byte(`{
	  "specVersion": "0.2.0-draft", "id": "https://example.invalid/p", "version": "0.1.0", "title": "P",
	  "decision": {"intent": "i", "question": "q"},
	  "outcomes": [{"id": "a", "label": "A"}, {"id": "b", "label": "B"}],
	  "evidenceRequirements": [{"id": "receipt", "description": "r", "required": true}, {"id": "cost-center", "description": "c", "required": true}],
	  "rules": [{"id": "r1", "description": "d", "when": {"op": "literal", "value": true}, "outcome": "a", "onUnknown": "escalate"}]
	}`)
	stopped, failure := engine.Evaluate(pack, []byte(`{}`), []byte(`{"receipt":"absent"}`), nil, "test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if want := []result.UnmetEvidence{{Requirement: "receipt", State: "absent"}, {Requirement: "cost-center", State: "unknown"}}; !reflect.DeepEqual(stopped.UnmetEvidence, want) {
		t.Fatalf("unmetEvidence = %+v, want %+v", stopped.UnmetEvidence, want)
	}
	passed, failure := engine.Evaluate(pack, []byte(`{}`), []byte(`{"receipt":"present","cost-center":"present"}`), nil, "test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if passed.UnmetEvidence != nil || passed.Disposition.OutcomeID != "a" {
		t.Fatalf("met evidence stops nothing and names nothing: %+v", passed)
	}
}
