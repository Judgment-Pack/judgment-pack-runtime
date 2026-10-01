package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// These rows pin the refusal ADR-0046 adds for a project that sets
// requireComparableFacts: a fact some comparison of the pack reads, present and
// of a JSON type that comparison can never match, refuses the evaluation once
// its inputs are admitted and before anything is evaluated.

func comparablePack(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "comparable-facts.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// comparableFacts is the fixture's facts document with every fact of the type
// its comparison reads, edited by setting members: a nil value removes one.
func comparableFacts(t *testing.T, edits map[string]any) []byte {
	t.Helper()
	document := map[string]map[string]any{
		"amount":  {"exceedsCap": false, "value": "10"},
		"request": {"exempt": false, "outsideWindow": false, "region": "domestic", "verified": true},
	}
	for pointer, value := range edits {
		parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
		if value == absentFact {
			delete(document[parts[0]], parts[1])
			continue
		}
		document[parts[0]][parts[1]] = value
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// absentFact marks a member comparableFacts removes, so that a JSON null can
// still be set as a value.
type absentMarker struct{}

var absentFact = absentMarker{}

// evaluateRequiring evaluates the fixture with the requirement on or off.
func evaluateRequiring(t *testing.T, require bool, pack, facts []byte) (result.Evaluation, *Failure) {
	t.Helper()
	return newTestEngine(t).EvaluateWith(pack, facts, nil, Options{Command: "test", RequireComparableFacts: require})
}

// refusedAsIncomparable requires the refusal: the code, the exit class, no §8.4
// class, a message naming every wanted phrase, and none of the values the
// facts carried.
func refusedAsIncomparable(t *testing.T, failure *Failure, want []string, values ...string) {
	t.Helper()
	if failure == nil {
		t.Fatal("the evaluation must be refused")
	}
	if failure.Code != ComparableFactsCode || failure.ExitCode != result.ExitInvalid || failure.Class != "" || failure.Phase != "" {
		t.Fatalf("refusal = %+v", failure)
	}
	for _, phrase := range want {
		if !strings.Contains(failure.Message, phrase) {
			t.Fatalf("the refusal names %q: %s", phrase, failure.Message)
		}
	}
	for _, value := range values {
		if strings.Contains(failure.Message, value) {
			t.Fatalf("the refusal quotes the value %q: %s", value, failure.Message)
		}
	}
}

// Each comparison kind, with the type that comparison can never match: an
// equals and a not-equals across types, an in whose operand has no member of
// the fact's type, null for an equals, and for an ordered comparison a JSON
// number and a string that is not a decimal. Each names the pointer, the fact's
// type and what the comparison can match, and never the value. Without the
// requirement every one of them evaluates to a disposition, as Core says.
func TestEachComparisonKindRefusesATypeItCanNeverMatch(t *testing.T) {
	pack := comparablePack(t)
	for _, tc := range []struct {
		name  string
		edits map[string]any
		want  string
		value string
	}{
		{"equals, a string where a boolean is compared", map[string]any{"/amount/exceedsCap": "SECRET-TRUE"}, `"/amount/exceedsCap" is a string, and equals can match only a boolean`, "SECRET-TRUE"},
		{"equals, null", map[string]any{"/amount/exceedsCap": nil}, `"/amount/exceedsCap" is null, and equals can match only a boolean`, ""},
		{"not-equals, a number where a boolean is compared", map[string]any{"/request/verified": 424242}, `"/request/verified" is a number, and not-equals can match only a boolean`, "424242"},
		{"in, a boolean where strings are compared", map[string]any{"/request/region": true}, `"/request/region" is a boolean, and in can match only a string`, ""},
		{"in, an object", map[string]any{"/request/region": map[string]any{"code": "SECRET-REGION"}}, `"/request/region" is an object, and in can match only a string`, "SECRET-REGION"},
		{"greater-than, a JSON number", map[string]any{"/amount/value": 987654}, `"/amount/value" is a number, and greater-than can match only a decimal string`, "987654"},
		{"greater-than, a string that is not a decimal", map[string]any{"/amount/value": "9,876,543"}, `"/amount/value" is a string that is not a decimal string, and greater-than can match only a decimal string`, "9,876,543"},
		{"greater-than, an array", map[string]any{"/amount/value": []any{"5"}}, `"/amount/value" is an array, and greater-than can match only a decimal string`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := comparableFacts(t, tc.edits)
			_, failure := evaluateRequiring(t, true, pack, facts)
			values := []string{}
			if tc.value != "" {
				values = append(values, tc.value)
			}
			refusedAsIncomparable(t, failure, []string{tc.want, "requireComparableFacts"}, values...)
			if output, failure := evaluateRequiring(t, false, pack, facts); failure != nil || output.Status != "evaluated" {
				t.Fatalf("without the requirement the evaluation answers: %+v %+v", output, failure)
			}
		})
	}
}

// The check is static over the whole pack. A comparison the walk never
// evaluates is checked all the same: one an any's true sibling short-circuits,
// and every rule an exception's forced outcome leaves unevaluated. Without the
// requirement both answer -- violation and exempt -- and the trace records no
// mismatch, because the walk never compared the fact.
func TestAComparisonTheWalkNeverReachesIsStillChecked(t *testing.T) {
	pack := comparablePack(t)
	for _, tc := range []struct {
		name    string
		edits   map[string]any
		want    string
		outcome string
	}{
		{"a short-circuited sibling", map[string]any{"/amount/exceedsCap": true, "/request/outsideWindow": "true"}, `"/request/outsideWindow" is a string, and equals can match only a boolean`, "violation"},
		{"a rule a forced outcome skips", map[string]any{"/request/exempt": true, "/request/verified": "yes"}, `"/request/verified" is a string, and not-equals can match only a boolean`, "exempt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := comparableFacts(t, tc.edits)
			output, failure := evaluateRequiring(t, false, pack, facts)
			if failure != nil || output.Disposition.OutcomeID != tc.outcome {
				t.Fatalf("without the requirement: %+v %+v", output.Disposition, failure)
			}
			for _, entry := range output.Trace {
				if len(entry.TypeMismatches) > 0 {
					t.Fatalf("the walk compared nothing across types, so the trace notes nothing: %+v", output.Trace)
				}
			}
			_, failure = evaluateRequiring(t, true, pack, facts)
			refusedAsIncomparable(t, failure, []string{tc.want})
		})
	}
}

// What the requirement does not refuse. An absent fact is unknown, and the
// pack's onUnknown governs it. Facts of the types their comparisons read are
// evaluated exactly as they are without the requirement, byte for byte. And
// an in whose operand carries two types matches a fact of either.
func TestWhatTheRequirementDoesNotRefuse(t *testing.T) {
	pack := comparablePack(t)
	for _, tc := range []struct {
		name  string
		edits map[string]any
		kind  string
	}{
		{"every fact of its comparison's type", nil, "outcome"},
		{"an absent fact", map[string]any{"/amount/exceedsCap": absentFact}, "unresolved"},
		{"an absent ordered fact", map[string]any{"/amount/value": absentFact}, "unresolved"},
		{"a decimal string an ordered comparison reads", map[string]any{"/amount/value": "1000.5"}, "outcome"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := comparableFacts(t, tc.edits)
			required, failure := evaluateRequiring(t, true, pack, facts)
			if failure != nil {
				t.Fatalf("refused: %+v", failure)
			}
			plain, failure := evaluateRequiring(t, false, pack, facts)
			if failure != nil {
				t.Fatal(failure.Message)
			}
			requiredBytes, _ := json.Marshal(required)
			plainBytes, _ := json.Marshal(plain)
			if string(requiredBytes) != string(plainBytes) || required.Disposition.Kind != tc.kind {
				t.Fatalf("the requirement changes nothing it passes:\n%s\n%s", requiredBytes, plainBytes)
			}
		})
	}

	mixed := strings.Replace(string(pack), `"value": ["embargoed", "sanctioned"]`, `"value": ["embargoed", false]`, 1)
	if mixed == string(pack) {
		t.Fatal("the fixture changed shape; update this test")
	}
	for _, region := range []any{"domestic", true} {
		if _, failure := evaluateRequiring(t, true, []byte(mixed), comparableFacts(t, map[string]any{"/request/region": region})); failure != nil {
			t.Fatalf("an in over a string and a boolean matches a %T: %+v", region, failure)
		}
	}
	_, failure := evaluateRequiring(t, true, []byte(mixed), comparableFacts(t, map[string]any{"/request/region": 7}))
	refusedAsIncomparable(t, failure, []string{`"/request/region" is a number, and in can match only a string or a boolean`})
}

// Every refusal §8.4 classes comes first: the requirement reads admitted
// inputs, so a pack that is not conformant, a facts document that is not JSON,
// a malformed evidence document and a required extension the caller does not
// support are each reported as what they are, even when the facts also hold a
// type no comparison can match.
func TestTheRequirementFollowsThePreflight(t *testing.T) {
	pack := comparablePack(t)
	incomparableFacts := comparableFacts(t, map[string]any{"/amount/exceedsCap": "true"})
	engine := newTestEngine(t)
	for _, tc := range []struct {
		name     string
		pack     []byte
		facts    []byte
		evidence []byte
		class    string
	}{
		{"a pack that is not conformant", []byte(strings.Replace(string(pack), `"fallbackOutcome": "permitted"`, `"fallbackOutcome": "undeclared"`, 1)), incomparableFacts, nil, result.ClassPackNotConformant},
		{"a facts document that is not JSON", pack, []byte(`{"amount":`), nil, result.ClassMalformedInput},
		{"an evidence document naming no requirement", pack, incomparableFacts, []byte(`{"undeclared":"present"}`), result.ClassMalformedInput},
		{"a required extension this caller does not support", []byte(strings.Replace(string(pack), `"fallbackOutcome": "permitted"`,
			`"fallbackOutcome": "permitted", "extensions": {"com.example.review-policy": {"reviewMode": "two-person"}}, "metadata": {"requiredExtensions": ["com.example.review-policy"]}`, 1)), incomparableFacts, nil, result.ClassUnsupportedRequiredExtension},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, failure := engine.EvaluateWith(tc.pack, tc.facts, tc.evidence, Options{Command: "test", RequireComparableFacts: true, EvidenceSupplied: tc.evidence != nil})
			if failure == nil || failure.Class != tc.class || failure.Code == ComparableFactsCode {
				t.Fatalf("want the %s refusal: %+v", tc.class, failure)
			}
		})
	}
}

// A refusal names each distinct finding once, in walk order, and at most ten
// of them; the rest are counted.
func TestARefusalNamesEachFindingOnceAndBoundsTheList(t *testing.T) {
	pack := comparablePack(t)
	twice := strings.Replace(string(pack), `{ "op": "fact", "path": "/request/outsideWindow", "operator": "equals", "value": true }`,
		`{ "op": "fact", "path": "/request/outsideWindow", "operator": "equals", "value": true }, { "op": "fact", "path": "/amount/exceedsCap", "operator": "equals", "value": false }`, 1)
	if twice == string(pack) {
		t.Fatal("the fixture changed shape; update this test")
	}
	_, failure := evaluateRequiring(t, true, []byte(twice), comparableFacts(t, map[string]any{"/amount/exceedsCap": "true", "/request/region": 1}))
	refusedAsIncomparable(t, failure, nil)
	if count := strings.Count(failure.Message, `"/amount/exceedsCap"`); count != 1 {
		t.Fatalf("one pointer compared twice the same way is named once, not %d times: %s", count, failure.Message)
	}
	if strings.Index(failure.Message, `"/amount/exceedsCap"`) > strings.Index(failure.Message, `"/request/region"`) {
		t.Fatalf("findings are in walk order: %s", failure.Message)
	}

	// Twelve rules over twelve pointers, every fact a string: ten are named.
	var document map[string]any
	if err := json.Unmarshal(pack, &document); err != nil {
		t.Fatal(err)
	}
	rules := []any{}
	facts := map[string]any{}
	for index := range 12 {
		name := fmt.Sprintf("flag%02d", index)
		rules = append(rules, map[string]any{
			"id": "rule-" + name, "description": "A detector.", "outcome": "violation", "onUnknown": "escalate",
			"when": map[string]any{"op": "fact", "path": "/" + name, "operator": "equals", "value": true},
		})
		facts[name] = "true"
	}
	document["rules"] = rules
	delete(document, "exceptions")
	many, _ := json.Marshal(document)
	manyFacts, _ := json.Marshal(facts)
	_, failure = evaluateRequiring(t, true, many, manyFacts)
	refusedAsIncomparable(t, failure, []string{`"/flag09"`, "; and 2 more."})
	if strings.Contains(failure.Message, `"/flag10"`) {
		t.Fatalf("at most ten findings are named: %s", failure.Message)
	}

	// A pointer the pack made long is shown by its first two hundred
	// characters, so ten of them still make a bounded message.
	name := strings.Repeat("é", 300)
	document["rules"] = []any{map[string]any{
		"id": "long", "description": "A detector.", "outcome": "violation", "onUnknown": "escalate",
		"when": map[string]any{"op": "fact", "path": "/" + name, "operator": "equals", "value": true},
	}}
	long, _ := json.Marshal(document)
	longFacts, _ := json.Marshal(map[string]any{name: "true"})
	_, failure = evaluateRequiring(t, true, long, longFacts)
	refusedAsIncomparable(t, failure, []string{`"/` + strings.Repeat("é", 199) + `…" is a string`})
	if strings.Contains(failure.Message, strings.Repeat("é", 200)) {
		t.Fatalf("a long pointer is cut: %s", failure.Message)
	}
}

// Under the draft RFC 0008 opt-in a comparison inside a quantifier's where
// reads each element, so each element is checked, and the finding names the
// collection as the trace does. An empty or absent collection gives the where
// nothing to read. The walk is charged against the evaluation's work limit,
// and a check that reaches it refuses rather than passing what it did not see.
func TestAComparisonInsideAnAggregateIsCheckedPerElement(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "rfc0008", "item-availability-quantifier.json"))
	if err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(t)
	options := Options{Command: "test", RFC0008Quantifiers: true, RequireComparableFacts: true}
	for _, items := range []string{`[{"availability":"available"},{"availability":"unavailable"}]`, `[]`, `[{}]`} {
		if _, failure := engine.EvaluateWith(data, []byte(`{"modification":{"items":`+items+`}}`), nil, options); failure != nil {
			t.Fatalf("items %s: %+v", items, failure)
		}
	}
	if _, failure := engine.EvaluateWith(data, []byte(`{"modification":{}}`), nil, options); failure != nil {
		t.Fatalf("an absent collection: %+v", failure)
	}
	_, failure := engine.EvaluateWith(data, []byte(`{"modification":{"items":[{"availability":"available"},{"availability":true}]}}`), nil, options)
	refusedAsIncomparable(t, failure, []string{`"/availability" in an element of "/modification/items" is a boolean, and equals can match only a string`})

	elements := strings.Repeat(`{"availability":"available"},`, 200)
	facts := []byte(`{"modification":{"items":[` + strings.TrimSuffix(elements, ",") + `]}}`)
	limited := options
	limited.WorkBudget = 50
	_, failure = engine.EvaluateWith(data, facts, nil, limited)
	refusedAsIncomparable(t, failure, []string{"exceeds this evaluation's work limit of 50 units"})
}

// The check reads the documents and nothing else: two runs over the same
// inputs give the same findings in the same order.
func TestTheCheckIsAFunctionOfItsInputs(t *testing.T) {
	var pack map[string]any
	if err := json.Unmarshal(comparablePack(t), &pack); err != nil {
		t.Fatal(err)
	}
	document := facts(t, string(comparableFacts(t, map[string]any{"/amount/exceedsCap": "true", "/request/outsideWindow": nil, "/request/region": 3})))
	first, exceeded := incomparableFacts(pack, document, Options{})
	second, _ := incomparableFacts(pack, document, Options{})
	if exceeded || len(first) != 3 || !reflect.DeepEqual(first, second) {
		t.Fatalf("findings %+v then %+v (exceeded %v)", first, second, exceeded)
	}
}

// The case that motivates the requirement, on the specification's own minimal
// expense example: an active investigation sent as the string "true" leaves
// the exception false, and the expense is approved. With the requirement the
// evaluation is refused instead, and so is one whose applicability reads a
// fact of the wrong type, which would otherwise answer not-applicable.
func TestTheSpecificationsOwnExampleIsRefusedRatherThanApproved(t *testing.T) {
	pack, err := os.ReadFile(filepath.Join("..", "artifacts", "jps", "0.2.0-draft", "cases", "valid", "minimal-expense-approval.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidence := []byte(`{"receipt":"present","cost-center":"present"}`)
	engine := newTestEngine(t)
	for _, tc := range []struct {
		name, facts, plain, want string
	}{
		{"an investigation flag sent as a string", `{"expense":{"type":"employee-expense","category":"travel","amount":"120.00","activeInvestigation":"true"}}`,
			"outcome", `"/expense/activeInvestigation" is a string, and equals can match only a boolean`},
		{"an applicability fact sent as a number", `{"expense":{"type":1,"category":"travel","amount":"120.00","activeInvestigation":false}}`,
			"not-applicable", `"/expense/type" is a number, and equals can match only a string`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plain, failure := engine.EvaluateWith(pack, []byte(tc.facts), evidence, Options{Command: "test", EvidenceSupplied: true})
			if failure != nil || plain.Disposition.Kind != tc.plain {
				t.Fatalf("without the requirement: %+v %+v", plain.Disposition, failure)
			}
			_, failure = engine.EvaluateWith(pack, []byte(tc.facts), evidence, Options{Command: "test", EvidenceSupplied: true, RequireComparableFacts: true})
			refusedAsIncomparable(t, failure, []string{tc.want})
		})
	}
}
