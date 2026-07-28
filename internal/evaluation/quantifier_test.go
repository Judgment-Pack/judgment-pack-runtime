package evaluation

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
)

// documentOf decodes one JSON document exactly as the engine decodes its
// inputs, so numbers arrive as json.Number and every row below reads as the
// document an author or a producer would actually supply.
func documentOf(t *testing.T, document string) any {
	t.Helper()
	value, failure := carrier.Decode([]byte(document), carrier.DefaultLimits())
	if failure != nil {
		t.Fatalf("undecodable document %s: %s", document, failure.Diagnostic.Message)
	}
	return value
}

func conditionOf(t *testing.T, document string) map[string]any {
	t.Helper()
	condition, ok := documentOf(t, document).(map[string]any)
	if !ok {
		t.Fatalf("condition must be a JSON object: %s", document)
	}
	return condition
}

// draftEval evaluates one condition under the draft RFC 0008 grammar with the
// default budget, which every semantic row in this file fits. A row that
// exhausts the budget is a bug in the row: exhaustion is an error, not a value.
func draftEval(t *testing.T, condition, facts string, evidence map[string]tri) tri {
	t.Helper()
	evaluator := &evaluator{evidence: evidence, quantifiers: true, budget: DefaultWorkBudget}
	verdict := evaluator.evaluate(conditionOf(t, condition), documentOf(t, facts))
	if evaluator.exceeded {
		t.Fatalf("row exhausted the work budget after %d units", evaluator.charged)
	}
	return verdict
}

// The RFC's positive and negative rows, its boundary rows, and its scoping
// rows, one table entry each. Where a row is pinned per operator the operator
// is named in the row rather than inferred.
func TestRFC0008QuantifierConformanceRows(t *testing.T) {
	const segments = `{"reservation":{"segments":[{"cancelledByAirline":false},{"cancelledByAirline":true},{"cancelledByAirline":false}]}}`
	const allTrue = `{"reservation":{"segments":[{"cancelledByAirline":true},{"cancelledByAirline":true}]}}`
	const allFalse = `{"reservation":{"segments":[{"cancelledByAirline":false},{"cancelledByAirline":false}]}}`
	const oneFalse = `{"reservation":{"segments":[{"cancelledByAirline":true},{"cancelledByAirline":false}]}}`
	const empty = `{"reservation":{"segments":[]}}`
	const cancelled = `{"op":"fact","path":"/cancelledByAirline","operator":"equals","value":true}`

	existsOver := func(path, where string) string {
		return fmt.Sprintf(`{"op":"exists","path":%q,"where":%s}`, path, where)
	}
	everyOver := func(path, where string) string {
		return fmt.Sprintf(`{"op":"every","path":%q,"where":%s}`, path, where)
	}

	cases := []struct {
		name      string
		condition string
		facts     string
		want      tri
	}{
		// Positive and negative.
		{"exists-one-of-three-matches", existsOver("/reservation/segments", cancelled), segments, triTrue},
		{"every-all-match", everyOver("/reservation/segments", cancelled), allTrue, triTrue},
		{"exists-non-empty-all-false", existsOver("/reservation/segments", cancelled), allFalse, triFalse},
		{"every-exactly-one-false", everyOver("/reservation/segments", cancelled), oneFalse, triFalse},
		{"exists-where-names-absent-member", existsOver("/reservation/segments",
			`{"op":"fact","path":"/noSuchMember","operator":"equals","value":true}`), allTrue, triUnknown},
		{"every-where-names-absent-member", everyOver("/reservation/segments",
			`{"op":"fact","path":"/noSuchMember","operator":"equals","value":true}`), allTrue, triUnknown},

		// Boundary: the empty array is pinned for both operators.
		{"exists-empty-array-is-false", existsOver("/reservation/segments", cancelled), empty, triFalse},
		{"every-empty-array-is-true", everyOver("/reservation/segments", cancelled), empty, triTrue},

		// Boundary: unknown dominance in all four directions.
		{"exists-unknown-and-none-true", existsOver("/list", cancelled),
			`{"list":[{"cancelledByAirline":false},{}]}`, triUnknown},
		{"exists-unknown-and-one-true", existsOver("/list", cancelled),
			`{"list":[{},{"cancelledByAirline":true}]}`, triTrue},
		{"every-unknown-and-rest-true", everyOver("/list", cancelled),
			`{"list":[{"cancelledByAirline":true},{}]}`, triUnknown},
		{"every-unknown-and-one-false", everyOver("/list", cancelled),
			`{"list":[{},{"cancelledByAirline":false}]}`, triFalse},

		// Ragged arrays, one row per operator and per position of the dominant
		// element, exactly as the RFC tabulates them.
		{"ragged-exists-false-false-missing", existsOver("/list", cancelled),
			`{"list":[{"cancelledByAirline":false},{"cancelledByAirline":false},{}]}`, triUnknown},
		{"ragged-exists-missing-true-false", existsOver("/list", cancelled),
			`{"list":[{},{"cancelledByAirline":true},{"cancelledByAirline":false}]}`, triTrue},
		{"ragged-every-true-true-missing", everyOver("/list", cancelled),
			`{"list":[{"cancelledByAirline":true},{"cancelledByAirline":true},{}]}`, triUnknown},
		{"ragged-every-missing-false-true", everyOver("/list", cancelled),
			`{"list":[{},{"cancelledByAirline":false},{"cancelledByAirline":true}]}`, triFalse},

		// Singleton with a predicate.
		{"singleton-true-exists", existsOver("/list", `{"op":"fact","path":"/ok","operator":"equals","value":true}`),
			`{"list":[{"ok":true}]}`, triTrue},
		{"singleton-true-every", everyOver("/list", `{"op":"fact","path":"/ok","operator":"equals","value":true}`),
			`{"list":[{"ok":true}]}`, triTrue},
		{"singleton-false-exists", existsOver("/list", `{"op":"fact","path":"/ok","operator":"equals","value":true}`),
			`{"list":[{"ok":false}]}`, triFalse},
		{"singleton-false-every", everyOver("/list", `{"op":"fact","path":"/ok","operator":"equals","value":true}`),
			`{"list":[{"ok":false}]}`, triFalse},
		{"singleton-missing-pointer-exists", existsOver("/list", `{"op":"fact","path":"/ok","operator":"equals","value":true}`),
			`{"list":[{}]}`, triUnknown},
		{"singleton-missing-pointer-every", everyOver("/list", `{"op":"fact","path":"/ok","operator":"equals","value":true}`),
			`{"list":[{}]}`, triUnknown},

		// Non-array values at path, one row per JSON type, plus the unresolved
		// pointer. Each is unknown, mirroring §7.4.
		{"non-array-object", everyOver("/value", cancelled), `{"value":{"a":1}}`, triUnknown},
		{"non-array-string", everyOver("/value", cancelled), `{"value":"segments"}`, triUnknown},
		{"non-array-number", everyOver("/value", cancelled), `{"value":3}`, triUnknown},
		{"non-array-null", everyOver("/value", cancelled), `{"value":null}`, triUnknown},
		{"non-array-true", everyOver("/value", cancelled), `{"value":true}`, triUnknown},
		{"unresolved-path-exists", existsOver("/missing", cancelled), `{"value":[]}`, triUnknown},
		{"unresolved-path-every", everyOver("/missing", cancelled), `{"value":[]}`, triUnknown},

		// Scope and re-rooting.
		{"empty-pointer-over-scalar-elements-true",
			everyOver("/list", `{"op":"fact","path":"","operator":"equals","value":"gold"}`),
			`{"list":["gold","gold"]}`, triTrue},
		{"empty-pointer-over-scalar-elements-false",
			everyOver("/list", `{"op":"fact","path":"","operator":"equals","value":"gold"}`),
			`{"list":["gold","silver"]}`, triFalse},
		{"empty-aggregate-path-selects-current-root",
			existsOver("", `{"op":"fact","path":"","operator":"equals","value":"gold"}`),
			`["silver","gold"]`, triTrue},
		{"collision-inner-root-wins",
			existsOver("/items", `{"op":"fact","path":"/status","operator":"equals","value":"inner"}`),
			`{"status":"outer","items":[{"status":"inner"}]}`, triTrue},
		{"collision-outer-value-is-not-visible",
			existsOver("/items", `{"op":"fact","path":"/status","operator":"equals","value":"outer"}`),
			`{"status":"outer","items":[{"status":"inner"}]}`, triFalse},
		{"outer-pointer-intent-is-read-element-relative",
			existsOver("/list", `{"op":"fact","path":"/threshold","operator":"equals","value":"5"}`),
			`{"threshold":"5","list":[{"amount":"7"}]}`, triUnknown},
		{"nesting-one-level-true",
			existsOver("/rows", everyOver("/cells", `{"op":"fact","path":"/ok","operator":"equals","value":true}`)),
			`{"rows":[{"cells":[{"ok":false}]},{"cells":[{"ok":true},{"ok":true}]}]}`, triTrue},
		{"nested-inner-path-resolves-only-at-facts-root",
			existsOver("/rows", existsOver("/other", `{"op":"fact","path":"/ok","operator":"equals","value":true}`)),
			`{"other":[{"ok":true}],"rows":[{"cells":[]}]}`, triUnknown},
		{"inner-where-re-roots-per-level",
			existsOver("/rows", existsOver("/cells", `{"op":"fact","path":"/tag","operator":"equals","value":"t"}`)),
			`{"rows":[{"tag":"t","cells":[{"z":1}]}]}`, triUnknown},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := draftEval(t, testCase.condition, testCase.facts, map[string]tri{}); got != testCase.want {
				t.Fatalf("%s = %v, want %v", testCase.name, got, testCase.want)
			}
		})
	}
}

// evidence-present reads no facts document, so it is element-invariant, and an
// empty array overrides it in both directions: every is true and exists is
// false whether the evidence is present or absent.
func TestRFC0008EmptyArrayOverridesElementInvariantEvidence(t *testing.T) {
	const where = `{"op":"evidence-present","evidenceRequirement":"intake-form"}`
	cases := []struct {
		name      string
		condition string
		presence  tri
		want      tri
	}{
		{"every-empty-evidence-present", fmt.Sprintf(`{"op":"every","path":"/list","where":%s}`, where), triTrue, triTrue},
		{"every-empty-evidence-absent", fmt.Sprintf(`{"op":"every","path":"/list","where":%s}`, where), triFalse, triTrue},
		{"exists-empty-evidence-present", fmt.Sprintf(`{"op":"exists","path":"/list","where":%s}`, where), triTrue, triFalse},
		{"exists-empty-evidence-absent", fmt.Sprintf(`{"op":"exists","path":"/list","where":%s}`, where), triFalse, triFalse},
		// Non-empty, for contrast: with elements present the element-invariant
		// value decides both operators.
		{"every-non-empty-evidence-absent", fmt.Sprintf(`{"op":"every","path":"/two","where":%s}`, where), triFalse, triFalse},
		{"exists-non-empty-evidence-present", fmt.Sprintf(`{"op":"exists","path":"/two","where":%s}`, where), triTrue, triTrue},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			evidence := map[string]tri{"intake-form": testCase.presence}
			if got := draftEval(t, testCase.condition, `{"list":[],"two":[{},{}]}`, evidence); got != testCase.want {
				t.Fatalf("%s = %v, want %v", testCase.name, got, testCase.want)
			}
		})
	}
}

// Element order carries no meaning: within the limits, a permutation or a
// duplicated element cannot change the result. Permitted short-circuiting on
// the dominant value must not make it change either.
func TestRFC0008PermutationAndDuplicateInvariance(t *testing.T) {
	const where = `{"op":"fact","path":"/ok","operator":"equals","value":true}`
	elements := []string{`{"ok":true}`, `{"ok":false}`, `{}`}

	// The labels name which element leads, not which value dominates: the
	// dominant value is true for exists and false for every, so one arrangement
	// cannot be "dominant-first" for both operators, and every position is
	// covered for each of them either way.
	arrangements := map[string][]int{
		"true-first":    {0, 1, 2},
		"false-first":   {1, 0, 2},
		"missing-first": {2, 0, 1},
		"reversed":      {2, 1, 0},
		"duplicated":    {1, 0, 2, 0, 1},
	}
	for _, op := range []string{"exists", "every"} {
		want := tri(-1)
		for _, name := range []string{"true-first", "false-first", "missing-first", "reversed", "duplicated"} {
			items := make([]string, 0, len(arrangements[name]))
			for _, index := range arrangements[name] {
				items = append(items, elements[index])
			}
			facts := fmt.Sprintf(`{"list":[%s]}`, strings.Join(items, ","))
			got := draftEval(t, fmt.Sprintf(`{"op":%q,"path":"/list","where":%s}`, op, where), facts, map[string]tri{})
			if want == tri(-1) {
				want = got
				continue
			}
			if got != want {
				t.Fatalf("%s over %s = %v, want %v (the true-first order's value)", op, name, got, want)
			}
		}
		// The invariant value is itself pinned, so an evaluator cannot satisfy
		// this test by being uniformly wrong.
		expected := triTrue
		if op == "every" {
			expected = triFalse
		}
		if want != expected {
			t.Fatalf("%s over one true, one false, and one missing pointer = %v, want %v", op, want, expected)
		}
	}
}

// uniform's five clauses, applied in order, one row each, plus the §7.4
// equality rows the RFC pins and a permutation of every one of them.
func TestRFC0008UniformConformanceRows(t *testing.T) {
	cases := []struct {
		name      string
		condition string
		facts     string
		want      tri
	}{
		{"clause-1-unresolved-path", `{"op":"uniform","path":"/missing","at":"/cabin"}`, `{"list":[]}`, triUnknown},
		{"clause-1-non-array-path", `{"op":"uniform","path":"/value","at":"/cabin"}`, `{"value":{"cabin":"economy"}}`, triUnknown},
		{"clause-2-empty-array", `{"op":"uniform","path":"/list","at":"/cabin"}`, `{"list":[]}`, triTrue},
		{"clause-3-unequal", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":"economy"},{"cabin":"business"}]}`, triFalse},
		{"clause-3-beats-clause-4", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":1},{"cabin":2},{}]}`, triFalse},

		// A value §7.4 cannot compare at all is not a counterexample, and it must
		// not hide one either: the determinable unequal pair still decides, in
		// whichever order the members arrive.
		{"clause-3-beats-an-undeterminable-at-value", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":1e999999999},{"cabin":1},{"cabin":2}]}`, triFalse},
		{"undeterminable-at-value-among-equals", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":1e999999999},{"cabin":1},{"cabin":1}]}`, triUnknown},
		{"all-at-values-undeterminable", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":1e999999999},{"cabin":2e999999999}]}`, triUnknown},
		{"undeterminable-inside-a-composite-at-value", `{"op":"uniform","path":"/list","at":"/seats"}`,
			`{"list":[{"seats":[1,1e999999999]},{"seats":[2,1e999999999]}]}`, triFalse},

		{"clause-4-missing-at-among-equals", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":"economy"},{"cabin":"economy"},{}]}`, triUnknown},
		{"clause-4-singleton-missing-at-is-not-true", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{}]}`, triUnknown},
		{"clause-5-all-equal", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":"economy"},{"cabin":"economy"}]}`, triTrue},
		{"singleton-with-at-present", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":"economy"}]}`, triTrue},
		{"at-is-member-relative-not-root-relative", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"cabin":"economy","list":[{},{}]}`, triUnknown},

		// The empty at selects the whole member and compares members to each
		// other under §7.4 recursive equality.
		{"empty-at-equal-members", `{"op":"uniform","path":"/list","at":""}`,
			`{"list":[{"a":1,"b":[2]},{"b":[2],"a":1.0}]}`, triTrue},
		{"empty-at-unequal-members", `{"op":"uniform","path":"/list","at":""}`,
			`{"list":[{"a":1},{"a":2}]}`, triFalse},

		// §7.4 equality, exactly as it behaves for a fact operand.
		{"null-equals-null", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":null},{"cabin":null}]}`, triTrue},
		{"array-at-in-order", `{"op":"uniform","path":"/list","at":"/seats"}`,
			`{"list":[{"seats":[1,2]},{"seats":[1,2]}]}`, triTrue},
		{"array-at-out-of-order", `{"op":"uniform","path":"/list","at":"/seats"}`,
			`{"list":[{"seats":[1,2]},{"seats":[2,1]}]}`, triFalse},
		{"object-at-disregards-member-order", `{"op":"uniform","path":"/list","at":"/fare"}`,
			`{"list":[{"fare":{"code":"Y","refundable":true}},{"fare":{"refundable":true,"code":"Y"}}]}`, triTrue},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := draftEval(t, testCase.condition, testCase.facts, map[string]tri{})
			if got != testCase.want {
				t.Fatalf("%s = %v, want %v", testCase.name, got, testCase.want)
			}
			// Permutation of the elements in every row above: identical result.
			if permuted, ok := reversedElements(t, testCase.facts); ok {
				if got := draftEval(t, testCase.condition, permuted, map[string]tri{}); got != testCase.want {
					t.Fatalf("%s permuted = %v, want %v", testCase.name, got, testCase.want)
				}
			}
		})
	}
}

// reversedElements rewrites a facts document's /list array in reverse order,
// which is the permutation every uniform row is also run under. It reports
// false for a document carrying no such array.
func reversedElements(t *testing.T, facts string) (string, bool) {
	t.Helper()
	document, ok := documentOf(t, facts).(map[string]any)
	if !ok {
		return "", false
	}
	elements, ok := document["list"].([]any)
	if !ok || len(elements) < 2 {
		return "", false
	}
	prefix := strings.Index(facts, "[")
	suffix := strings.LastIndex(facts, "]")
	if prefix < 0 || suffix < prefix {
		return "", false
	}
	items := splitTopLevel(facts[prefix+1 : suffix])
	if len(items) != len(elements) {
		return "", false
	}
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
	return facts[:prefix+1] + strings.Join(items, ",") + facts[suffix:], true
}

// splitTopLevel splits a JSON array body on the commas that separate its own
// elements, ignoring commas nested inside members.
func splitTopLevel(body string) []string {
	items := []string{}
	depth, start, inString, escaped := 0, 0, false, false
	for index, character := range body {
		switch {
		case escaped:
			escaped = false
		case character == '\\' && inString:
			escaped = true
		case character == '"':
			inString = !inString
		case inString:
		case character == '{' || character == '[':
			depth++
		case character == '}' || character == ']':
			depth--
		case character == ',' && depth == 0:
			items = append(items, body[start:index])
			start = index + 1
		}
	}
	return append(items, body[start:])
}

// chargeOf runs the preflight alone, with a budget no test row can reach, and
// reports the units charged. It never evaluates a predicate.
func chargeOf(t *testing.T, condition, facts string) int {
	t.Helper()
	evaluator := &evaluator{quantifiers: true, budget: math.MaxInt}
	evaluator.preflight(conditionOf(t, condition), documentOf(t, facts))
	return evaluator.charged
}

// The charge is a sum over the elements present, so it is identical under any
// permutation or duplication of them — the property that makes short-circuiting
// safe, since it cannot change whether the limit was exceeded.
func TestRFC0008WorkChargeIsOrderIndependent(t *testing.T) {
	const condition = `{"op":"exists","path":"/list","where":{"op":"fact","path":"/ok","operator":"equals","value":true}}`
	arrangements := []string{
		`{"list":[{"ok":true},{"ok":false},{}]}`,
		`{"list":[{},{"ok":false},{"ok":true}]}`,
		`{"list":[{"ok":false},{"ok":true},{}]}`,
	}
	want := chargeOf(t, condition, arrangements[0])
	for _, facts := range arrangements[1:] {
		if got := chargeOf(t, condition, facts); got != want {
			t.Fatalf("charge for %s = %d, want %d", facts, got, want)
		}
	}
}

// The accounting model's per-shape obligations, each as one row with the exact
// charge the model produces. Stating the numbers rather than an inequality is
// the point: an accounting model that cannot be recomputed by hand is not a
// model.
func TestRFC0008WorkChargeModel(t *testing.T) {
	const elementPredicate = `{"op":"fact","path":"/ok","operator":"equals","value":true}`
	cases := []struct {
		name      string
		condition string
		facts     string
		want      int
		why       string
	}{
		{
			name:      "fact-node-and-pointer",
			condition: elementPredicate,
			facts:     `{"ok":true}`,
			want:      3,
			why:       "one node, one pointer resolution, one scalar operand",
		},
		{
			name:      "deep-equality-charged-by-operand-size",
			condition: `{"op":"fact","path":"/value","operator":"equals","value":{"a":[1,2,3]}}`,
			facts:     `{"value":{"a":[1,2,3]}}`,
			want:      7,
			why:       "one node, one pointer, and five JSON nodes of operand: the object, the array, and three numbers",
		},
		{
			name:      "in-operand-charged-by-size",
			condition: `{"op":"fact","path":"/value","operator":"in","value":["a","b","c","d"]}`,
			facts:     `{"value":"a"}`,
			want:      7,
			why:       "one node, one pointer, and five JSON nodes of operand: the array and four strings",
		},
		{
			name:      "unresolved-aggregate-path-costs-its-lookup",
			condition: `{"op":"exists","path":"/missing","where":` + elementPredicate + `}`,
			facts:     `{"list":[]}`,
			want:      2,
			why:       "the node and the pointer that failed to resolve, and nothing more",
		},
		{
			name:      "non-array-aggregate-path-costs-its-lookup",
			condition: `{"op":"exists","path":"/value","where":` + elementPredicate + `}`,
			facts:     `{"value":"not an array"}`,
			want:      2,
			why:       "the node and the pointer, with no elements to charge for",
		},
		{
			name:      "empty-array-costs-only-the-aggregate",
			condition: `{"op":"every","path":"/list","where":` + elementPredicate + `}`,
			facts:     `{"list":[]}`,
			want:      2,
			why:       "the node and the pointer; the vacuous value costs no predicate",
		},
		{
			name:      "per-element-predicate",
			condition: `{"op":"exists","path":"/list","where":` + elementPredicate + `}`,
			facts:     `{"list":[{"ok":true},{"ok":false},{}]}`,
			want:      11,
			why:       "two for the aggregate plus three units for each of three elements",
		},
		{
			name:      "boolean-subtree-charges-branches-never-reached",
			condition: `{"op":"any","conditions":[{"op":"literal","value":true},{"op":"exists","path":"/list","where":` + elementPredicate + `}]}`,
			facts:     `{"list":[{"ok":true},{"ok":false},{}]}`,
			want:      13,
			why:       "the any node, the literal that short-circuits it, and the whole aggregate the evaluator never reaches",
		},
		{
			name: "sibling-aggregates-add",
			condition: `{"op":"all","conditions":[` +
				`{"op":"exists","path":"/list","where":` + elementPredicate + `},` +
				`{"op":"every","path":"/list","where":` + elementPredicate + `}]}`,
			facts: `{"list":[{"ok":true},{"ok":false},{}]}`,
			want:  23,
			why:   "the all node plus two aggregates of eleven; no single product bounds them",
		},
		{
			name:      "ragged-nesting-sums-inner-lengths",
			condition: `{"op":"exists","path":"/rows","where":{"op":"every","path":"/cells","where":` + elementPredicate + `}}`,
			facts:     `{"rows":[{"cells":[{"ok":true}]},{"cells":[{"ok":true},{"ok":true}]},{"cells":[{"ok":true},{"ok":true},{"ok":true}]}]}`,
			want:      26,
			why:       "two for the outer aggregate, two for each of three inner aggregates, and three units for each of the six cells",
		},
		{
			name:      "uniform-charges-members-and-selected-values",
			condition: `{"op":"uniform","path":"/list","at":"/fare"}`,
			facts:     `{"list":[{"fare":{"code":"Y"}},{"fare":{"code":"Y"}},{}]}`,
			want:      9,
			why:       "two for the aggregate, one pointer per member, and two JSON nodes for each of the two values that resolved",
		},
		{
			name:      "uniform-charges-no-extra-pass-without-an-undeterminable-value",
			condition: `{"op":"uniform","path":"/list","at":"/cabin"}`,
			facts:     `{"list":[{"cabin":1},{"cabin":2},{"cabin":3}]}`,
			want:      8,
			why:       "two for the aggregate and two per member; an ordinary collection pays for one comparison pass and no more",
		},
		{
			name:      "uniform-charges-a-pass-per-undeterminable-value",
			condition: `{"op":"uniform","path":"/list","at":"/cabin"}`,
			facts:     `{"list":[{"cabin":1e999999999},{"cabin":2},{"cabin":3}]}`,
			want:      11,
			why:       "the eight above plus one further pass over the three selected values, since one value can create a second representative",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := chargeOf(t, testCase.condition, testCase.facts); got != testCase.want {
				t.Fatalf("charge = %d, want %d (%s)", got, testCase.want, testCase.why)
			}
		})
	}
}

// A ragged outer array costs the sum of its inner lengths, never the product of
// the outer length with any single inner length — the row the RFC states as
// Σᵢ|Bᵢ| rather than |A|×|B|.
func TestRFC0008RaggedNestingIsNotAProduct(t *testing.T) {
	const condition = `{"op":"exists","path":"/rows","where":{"op":"every","path":"/cells","where":{"op":"fact","path":"/ok","operator":"equals","value":true}}}`
	cell := `{"ok":true}`
	row := func(cells int) string {
		items := make([]string, 0, cells)
		for index := 0; index < cells; index++ {
			items = append(items, cell)
		}
		return fmt.Sprintf(`{"cells":[%s]}`, strings.Join(items, ","))
	}
	ragged := fmt.Sprintf(`{"rows":[%s,%s,%s]}`, row(1), row(2), row(3))
	rectangular := fmt.Sprintf(`{"rows":[%s,%s,%s]}`, row(3), row(3), row(3))

	raggedCharge := chargeOf(t, condition, ragged)
	rectangularCharge := chargeOf(t, condition, rectangular)
	// Σ|Bᵢ| = 6 cells against |A|×max|B| = 9: the ragged charge must be the
	// sum, which is exactly three cells (nine units) cheaper.
	if rectangularCharge-raggedCharge != 9 {
		t.Fatalf("ragged %d and rectangular %d must differ by the three absent cells (9 units)", raggedCharge, rectangularCharge)
	}
}
