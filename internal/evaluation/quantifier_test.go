package evaluation

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

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
// the dominant value must not make it change either. This is a claim about the
// condition's value only; the work charge is invariant under permutation but
// not under duplication, which
// TestRFC0008DuplicationRaisesTheChargeAndKeepsTheValue pins separately.
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
//
// Every row is conformance to the five clauses as published; none pins an
// extension of them. The rows over numbers no arithmetic type can hold are
// where that used to be in doubt: they are ordinary clause 3 and clause 5 rows,
// because §7.4 equality is total and decides them on the tokens. See the note
// on uniform.
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

		// Numbers past every arithmetic type, under the same two clauses as any
		// other value. Equality is decided on the tokens, so an at-value of
		// 1e999999999 is a counterexample against 2e999999999 exactly as 1 is
		// against 2, and 1e999999999 confirms 1.0e999999999 exactly as 1000
		// confirms 1e3. No row here is unknown: unknown is what a missing at
		// earns (clause 4), not what an expensive number earns.
		{"clause-3-huge-token-against-small-ones", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":1e999999999},{"cabin":1},{"cabin":2}]}`, triFalse},
		{"clause-3-huge-token-differs-from-its-equal-neighbours", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":1e999999999},{"cabin":1},{"cabin":1}]}`, triFalse},
		{"clause-3-distinct-huge-tokens-are-unequal", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":1e999999999},{"cabin":2e999999999}]}`, triFalse},
		{"clause-3-huge-token-inside-a-composite-at-value", `{"op":"uniform","path":"/list","at":"/seats"}`,
			`{"list":[{"seats":[1,1e999999999]},{"seats":[2,1e999999999]}]}`, triFalse},
		{"clause-5-equal-huge-tokens-written-differently", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":1e999999999},{"cabin":1.0e999999999},{"cabin":10e999999998}]}`, triTrue},
		{"clause-5-equal-numbers-written-differently", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":1000},{"cabin":1e3},{"cabin":1.0e3}]}`, triTrue},
		{"clause-5-signed-zero-is-one-value", `{"op":"uniform","path":"/list","at":"/cabin"}`,
			`{"list":[{"cabin":-0},{"cabin":0},{"cabin":0.0e10}]}`, triTrue},

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

// The charge is a sum over the elements present and over the distinct authored
// pointers the tree names, and neither summand depends on the order elements
// arrive in, so the charge is identical under any permutation of them — the
// property that makes short-circuiting safe, since it cannot change whether the
// limit was exceeded. The pointer cache does not weaken it: whichever element
// happens to be preflighted first pays the one scan, and the total is the same.
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

// Duplication is not permutation, and the accounting model does not claim it
// is. A duplicated element is one more element and is charged like any other,
// so the charge strictly increases; what survives duplication is the condition's
// value, and only while both inputs remain within the limits, which is what RFC
// 0008's result-invariance wording actually says.
func TestRFC0008DuplicationRaisesTheChargeAndKeepsTheValue(t *testing.T) {
	const where = `{"op":"fact","path":"/ok","operator":"equals","value":true}`
	const condition = `{"op":"every","path":"/list","where":` + where + `}`
	const once = `{"list":[{"ok":true},{"ok":false}]}`
	const twice = `{"list":[{"ok":true},{"ok":false},{"ok":false},{"ok":true}]}`

	onceCharge := chargeOf(t, condition, once)
	twiceCharge := chargeOf(t, condition, twice)
	// Each duplicate costs a fact node, one step of the already-compiled "/ok",
	// the boolean it selects, and the boolean operand: six units apiece, twelve
	// for the two.
	if twiceCharge-onceCharge != 12 {
		t.Fatalf("duplicating two elements raised the charge from %d to %d; the model says it must rise by 12", onceCharge, twiceCharge)
	}
	if twiceCharge <= onceCharge {
		t.Fatalf("duplication must raise the charge: %d then %d", onceCharge, twiceCharge)
	}

	// Both fit the budget, so the value is unchanged — the invariance the RFC
	// states, and the only one duplication has.
	first := draftEval(t, condition, once, map[string]tri{})
	second := draftEval(t, condition, twice, map[string]tri{})
	if first != second || first != triFalse {
		t.Fatalf("duplication changed the value: %v then %v, want false", first, second)
	}

	// And the increase is not cosmetic: a budget the un-duplicated input fits
	// refuses the duplicated one, which is precisely why the charge cannot be
	// called duplication-invariant. The budget is the condition's charge plus the
	// one unit §8's iteration over the pack's single rule costs, which chargeOf
	// does not see because it charges a condition tree and not a §8 walk.
	engine := newTestEngine(t)
	pack := draftPack(condition, "escalate")
	if _, failure := engine.EvaluateWith(pack, []byte(once), nil, draftOptions(onceCharge+1)); failure != nil {
		t.Fatalf("a budget equal to the charge must admit the un-duplicated input: %+v", failure)
	}
	_, failure := engine.EvaluateWith(pack, []byte(twice), nil, draftOptions(onceCharge+1))
	if failure == nil || failure.Code != "JPS-RESOURCE-EVALUATION-WORK-LIMIT" {
		t.Fatalf("the same budget must refuse the duplicated input: %+v", failure)
	}
}

// The accounting model's per-shape obligations, each as one row with the exact
// charge the model produces. Stating the numbers rather than an inequality is
// the point: an accounting model that cannot be recomputed by hand is not a
// model.
//
// Two quantities recur, so they are stated once here rather than in every row.
// A pointer costs 1+len(path) the first time this evaluation compiles it and
// max(1,tokens)+Σlen(token) for each resolution — one traversal per reference
// token, empty ones included — so the single-token "/ok" costs 4+3 = 7 on first
// use and 3 on every later one, "/list" costs 6+5 = 11 then 5, "/value" and
// "/cabin" cost 7+6 = 13 then 6, "/rows" costs 6+5 = 11 then 5, "/cells" costs
// 7+6 = 13 then 6, "/fare" costs 6+5 = 11 then 5, and "/missing" costs 9+8 =
// 17. A scalar value costs 1 plus its token length, so true costs 1, "Y" costs
// 2, and the number 1 costs 2.
//
// A fact charges both sides of its comparison: the operand its author wrote and
// the value its pointer selected. Every row over a resolving fact therefore
// carries a summand the facts document supplies, and a row whose pointer does
// not resolve carries none.
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
			want:      10,
			why:       "one node, the compile and one resolution of \"/ok\" (7), one unit for the boolean it selected, and one for the boolean operand",
		},
		{
			name:      "pointer-costs-one-step-per-reference-token-including-empty-ones",
			condition: `{"op":"fact","path":"/a////b","operator":"equals","value":true}`,
			facts:     `{"a":{"":{"":{"":{"b":true}}}}}`,
			want:      18,
			why: "one node, 8 to compile the seven-byte path, and 7 to resolve it — five reference tokens, three of them empty, is five map traversals over two bytes of token — plus one unit for the boolean selected and one for the operand; " +
				"charging a flat step would have priced those five traversals at three units",
		},
		{
			name:      "fact-charges-the-selected-value-as-well-as-the-operand",
			condition: `{"op":"fact","path":"/value","operator":"equals","value":"gold"}`,
			facts:     `{"value":"platinum"}`,
			want:      28,
			why:       "one node, 13 for \"/value\", 9 for the eight-character string the pointer selected, and 5 for the four-character operand; the runtime side is the larger one here and is priced like the authored side",
		},
		{
			name:      "fact-whose-pointer-does-not-resolve-charges-no-selected-value",
			condition: `{"op":"fact","path":"/value","operator":"equals","value":"gold"}`,
			facts:     `{"other":"platinum"}`,
			want:      19,
			why:       "one node, 13 for the lookup that failed, and 5 for the operand; nothing was selected, so nothing is charged for it",
		},
		{
			name:      "deep-equality-charged-by-both-sides",
			condition: `{"op":"fact","path":"/value","operator":"equals","value":{"a":[1,2,3]}}`,
			facts:     `{"value":{"a":[1,2,3]}}`,
			want:      32,
			why:       "one node, 13 for \"/value\", and 9 twice — for the selected value and for the operand — where 9 is the object (1) plus its member name (1) plus the array (1) plus three two-unit number tokens",
		},
		{
			name:      "in-charges-the-selected-value-once-per-candidate",
			condition: `{"op":"fact","path":"/value","operator":"in","value":["a","b","c","d"]}`,
			facts:     `{"value":"a"}`,
			want:      31,
			why:       "one node, 13 for \"/value\", 9 for the operand (the array plus four one-character strings at two units each), and the two-unit selected value once per candidate: 8",
		},
		{
			name:      "evidence-present-charges-its-requirement-id",
			condition: `{"op":"evidence-present","evidenceRequirement":"intake-form"}`,
			facts:     `{}`,
			want:      12,
			why:       "one node plus the eleven bytes of the id every lookup hashes",
		},
		{
			name:      "unresolved-aggregate-path-costs-its-lookup",
			condition: `{"op":"exists","path":"/missing","where":` + elementPredicate + `}`,
			facts:     `{"list":[]}`,
			want:      18,
			why:       "the node and the compile-and-resolve of the pointer that failed to resolve (17), and nothing more",
		},
		{
			name:      "non-array-aggregate-path-costs-its-lookup",
			condition: `{"op":"exists","path":"/value","where":` + elementPredicate + `}`,
			facts:     `{"value":"not an array"}`,
			want:      14,
			why:       "the node and \"/value\" (13), with no elements to charge for",
		},
		{
			name:      "empty-array-costs-only-the-aggregate",
			condition: `{"op":"every","path":"/list","where":` + elementPredicate + `}`,
			facts:     `{"list":[]}`,
			want:      12,
			why:       "the node and \"/list\" (11); the vacuous value costs no predicate",
		},
		{
			name:      "per-element-predicate",
			condition: `{"op":"exists","path":"/list","where":` + elementPredicate + `}`,
			facts:     `{"list":[{"ok":true},{"ok":false},{}]}`,
			want:      33,
			why:       "12 for the aggregate, 10 for the first element, 6 for the second, and 5 for the third, whose \"/ok\" selects nothing: \"/ok\" is scanned once and stepped three times",
		},
		{
			name:      "boolean-subtree-charges-branches-never-reached",
			condition: `{"op":"any","conditions":[{"op":"literal","value":true},{"op":"exists","path":"/list","where":` + elementPredicate + `}]}`,
			facts:     `{"list":[{"ok":true},{"ok":false},{}]}`,
			want:      35,
			why:       "the any node, the literal that short-circuits it, and the whole 33-unit aggregate the evaluator never reaches",
		},
		{
			name: "sibling-aggregates-add",
			condition: `{"op":"all","conditions":[` +
				`{"op":"exists","path":"/list","where":` + elementPredicate + `},` +
				`{"op":"every","path":"/list","where":` + elementPredicate + `}]}`,
			facts: `{"list":[{"ok":true},{"ok":false},{}]}`,
			want:  57,
			why:   "the all node, the first aggregate at 33, and the second at 23 because both its pointers are already compiled; no single product bounds them",
		},
		{
			name:      "ragged-nesting-sums-inner-lengths",
			condition: `{"op":"exists","path":"/rows","where":{"op":"every","path":"/cells","where":` + elementPredicate + `}}`,
			facts:     `{"rows":[{"cells":[{"ok":true}]},{"cells":[{"ok":true},{"ok":true}]},{"cells":[{"ok":true},{"ok":true},{"ok":true}]}]}`,
			want:      80,
			why:       "12 for the outer aggregate, 14 then 7 then 7 for the three inner ones, 10 for the first cell, and 6 for each of the other five",
		},
		{
			name:      "uniform-charges-members-and-selected-values",
			condition: `{"op":"uniform","path":"/list","at":"/fare"}`,
			facts:     `{"list":[{"fare":{"code":"Y"}},{"fare":{"code":"Y"}},{}]}`,
			want:      54,
			why: "12 for the aggregate, 11 then 5 then 5 for the three resolutions of \"/fare\", 7 for each of the two objects that resolved (the object, its four-byte member name, and the two-unit string), " +
				"and a reread term of 7: two values resolved, so the representative is compared once, and that comparison can read all 7 units of the larger of them — the third member resolves nothing and joins neither term",
		},
		{
			name:      "uniform-charges-a-reread-of-the-largest-value-per-comparison",
			condition: `{"op":"uniform","path":"/list","at":"/cabin"}`,
			facts:     `{"list":[{"cabin":1},{"cabin":2},{"cabin":3}]}`,
			want:      47,
			why:       "12 for the aggregate, 13 then 6 then 6 for \"/cabin\", two units for each one-digit number, and (3-1)×2 = 4 for the two comparisons the representative takes part in",
		},
		{
			name:      "uniform-charges-a-long-numeric-token-by-its-bytes-and-by-its-rereads",
			condition: `{"op":"uniform","path":"/list","at":"/cabin"}`,
			facts:     `{"list":[{"cabin":1e999999999},{"cabin":2},{"cabin":3}]}`,
			want:      77,
			why: "the 47 above with the first value's eleven-character token costing 12 rather than 2 (ten extra units in the per-member term) and the reread term rising from (3-1)×2 to (3-1)×12 (twenty more): " +
				"the long token is read once per member however the members are ordered, because the maximum is taken over the set and not over whichever member happens to be elected",
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

// The adversarial pointer row. A depth-two aggregate over tens of thousands of
// elements whose inner path is a valid megabyte-long pointer used to cost two
// units per element and scan a megabyte per element anyway: 60,002 units
// charged against a 100,000-unit budget for tens of gigabytes of processing,
// which is the whole defect. The charge is now byte-sensitive and is reserved
// before the scan, so the budget refuses the input with the path's bytes still
// unread.
//
// Both halves are asserted, because they fail differently. Under a flat pointer
// charge the input is not refused at all, which the code assertion catches; the
// wall clock then catches a charge that is byte-sensitive but levied after the
// scan rather than before it. The clock bound is deliberately loose — this
// refusal is milliseconds' work and the unbounded version is tens of seconds.
func TestRFC0008LongUnresolvedPointerIsRefusedBeforeItIsScanned(t *testing.T) {
	engine := newTestEngine(t)
	// One byte under the carrier's per-string limit, so the pack still decodes
	// and the refusal is the work limit rather than a resource limit on input.
	innerPath := "/" + strings.Repeat("a", 1<<20-1)
	condition := fmt.Sprintf(
		`{"op":"exists","path":"/list","where":{"op":"exists","path":%q,"where":{"op":"fact","path":"/ok","operator":"equals","value":true}}}`,
		innerPath)
	const count = 60_000
	elements := make([]string, 0, count)
	for index := 0; index < count; index++ {
		elements = append(elements, `{}`)
	}
	facts := []byte(fmt.Sprintf(`{"list":[%s]}`, strings.Join(elements, ",")))

	start := time.Now()
	_, failure := engine.EvaluateWith(draftPack(condition, "escalate"), facts, nil, draftOptions(0))
	elapsed := time.Since(start)

	if failure == nil || failure.Code != "JPS-RESOURCE-EVALUATION-WORK-LIMIT" {
		t.Fatalf("a megabyte-long pointer over %d elements must be refused by the work limit: %+v", count, failure)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("the refusal took %s; the budget must stop this before the path is scanned, not after", elapsed)
	}

	// The same shape with a pointer short enough to afford is not refused, so
	// the row above turns on the path's length and not on the element count.
	short := `{"op":"exists","path":"/list","where":{"op":"exists","path":"/a","where":{"op":"fact","path":"/ok","operator":"equals","value":true}}}`
	if _, failure := engine.EvaluateWith(draftPack(short, "escalate"), []byte(`{"list":[{},{}]}`), nil, draftOptions(0)); failure != nil {
		t.Fatalf("the same shape over a short pointer must still evaluate: %+v", failure)
	}
}

// The adversarial operand row. A long decimal operand is parsed once per
// element by RFC 0006's ordered comparison, so it costs its token length per
// element rather than one unit per element, and a token long enough to make
// that expensive is refused rather than run.
func TestRFC0008LongDecimalOperandIsChargedByItsLength(t *testing.T) {
	const elements = 200
	items := make([]string, 0, elements)
	for index := 0; index < elements; index++ {
		items = append(items, `{"amount":"7"}`)
	}
	facts := fmt.Sprintf(`{"list":[%s]}`, strings.Join(items, ","))
	condition := func(operand string) string {
		return fmt.Sprintf(
			`{"op":"exists","path":"/list","where":{"op":"fact","path":"/amount","operator":"greater-than","value":%q}}`,
			operand)
	}
	const longOperand = 1_000
	short := "5"
	long := strings.Repeat("9", longOperand)

	shortCharge := chargeOf(t, condition(short), facts)
	longCharge := chargeOf(t, condition(long), facts)
	// Every element pays the operand's bytes, so the difference is the token
	// lengths' difference times the element count and nothing else.
	if want := elements * (len(long) - len(short)); longCharge-shortCharge != want {
		t.Fatalf("charges %d and %d differ by %d; a per-element operand must cost its length per element (%d)",
			shortCharge, longCharge, longCharge-shortCharge, want)
	}

	// And the difference is load-bearing: the short operand fits the default
	// budget and the long one does not, so the 200,000 decimal parses the long
	// one would force are refused rather than performed.
	engine := newTestEngine(t)
	if _, failure := engine.EvaluateWith(draftPack(condition(short), "escalate"), []byte(facts), nil, draftOptions(0)); failure != nil {
		t.Fatalf("a one-digit operand over 200 elements must evaluate: %+v", failure)
	}
	_, failure := engine.EvaluateWith(draftPack(condition(long), "escalate"), []byte(facts), nil, draftOptions(0))
	if failure == nil || failure.Code != "JPS-RESOURCE-EVALUATION-WORK-LIMIT" {
		t.Fatalf("a thousand-digit operand over the same 200 elements must be refused: %+v", failure)
	}
}

// The adversarial selected-value row, and the second defect of the same class
// as the long pointer. An authored operand says nothing about the value it is
// compared against: a two-byte operand can be compared, once per element,
// against as much runtime JSON as the carrier admits, and a model that charged
// only what the author wrote priced the one half of the comparison an attacker
// does not supply. Both shapes below are charged by their bytes, per element,
// and a facts document whose selected values are long enough to be expensive is
// refused under the same budget that admits the short one.
func TestRFC0008LongSelectedValueIsChargedByItsLength(t *testing.T) {
	engine := newTestEngine(t)
	const elements = 200
	const longWidth = 1_000
	shapes := []struct {
		name    string
		element func(width int) string
		operand string
	}{
		{"string", func(width int) string { return fmt.Sprintf(`{"blob":%q}`, strings.Repeat("a", width)) }, `"a"`},
		{"number", func(width int) string { return fmt.Sprintf(`{"blob":%s}`, strings.Repeat("9", width)) }, `9`},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			facts := func(width int) string {
				items := make([]string, 0, elements)
				for index := 0; index < elements; index++ {
					items = append(items, shape.element(width))
				}
				return fmt.Sprintf(`{"list":[%s]}`, strings.Join(items, ","))
			}
			condition := fmt.Sprintf(
				`{"op":"exists","path":"/list","where":{"op":"fact","path":"/blob","operator":"equals","value":%s}}`,
				shape.operand)
			short, long := facts(1), facts(longWidth)

			// The operand is one byte wide in both runs, so the difference is
			// the selected values' difference and nothing else.
			shortCharge := chargeOf(t, condition, short)
			longCharge := chargeOf(t, condition, long)
			if want := elements * (longWidth - 1); longCharge-shortCharge != want {
				t.Fatalf("charges %d and %d differ by %d; a selected value must cost its length per element (%d)",
					shortCharge, longCharge, longCharge-shortCharge, want)
			}

			// And the difference decides admission: the 200 one-byte
			// comparisons run, the 200 thousand-byte ones are refused.
			pack := draftPack(condition, "escalate")
			if _, failure := engine.EvaluateWith(pack, []byte(short), nil, draftOptions(0)); failure != nil {
				t.Fatalf("one-byte selected values over 200 elements must evaluate: %+v", failure)
			}
			_, failure := engine.EvaluateWith(pack, []byte(long), nil, draftOptions(0))
			if failure == nil || failure.Code != "JPS-RESOURCE-EVALUATION-WORK-LIMIT" {
				t.Fatalf("thousand-byte selected values over the same 200 elements must be refused: %+v", failure)
			}
		})
	}
}

// The adversarial evidence-requirement row. evidence-present was charged as one
// flat node however long the identifier it looks up, so a where could hash a
// long id once per element for one unit per element — the flat-unit defect
// again, in the one node shape that had kept it. The id's bytes are now charged
// like any other name a lookup reads.
func TestRFC0008LongEvidenceRequirementIdIsChargedByItsLength(t *testing.T) {
	engine := newTestEngine(t)
	const elements = 200
	items := make([]string, 0, elements)
	for index := 0; index < elements; index++ {
		items = append(items, `{}`)
	}
	facts := fmt.Sprintf(`{"list":[%s]}`, strings.Join(items, ","))
	condition := func(id string) string {
		return fmt.Sprintf(
			`{"op":"exists","path":"/list","where":{"op":"evidence-present","evidenceRequirement":%q}}`, id)
	}
	// Both are localIds the schema admits; only their lengths differ.
	short, long := "e", strings.Repeat("e", 1_000)

	shortCharge := chargeOf(t, condition(short), facts)
	longCharge := chargeOf(t, condition(long), facts)
	if want := elements * (len(long) - len(short)); longCharge-shortCharge != want {
		t.Fatalf("charges %d and %d differ by %d; a requirement id looked up per element must cost its length per element (%d)",
			shortCharge, longCharge, longCharge-shortCharge, want)
	}

	// The pack declares the requirement, so both runs are conformant packs that
	// differ only in the identifier's length — and the long one is refused.
	pack := func(id string) []byte {
		return []byte(fmt.Sprintf(`{
  "specVersion": "0.2.0-draft",
  "id": "https://example.invalid/judgment-packs/rfc0008-evidence-id",
  "version": "0.1.0",
  "title": "Synthetic draft RFC 0008 evidence-identifier row",
  "description": "Invented content for specification testing; it authorizes nothing.",
  "decision": {
    "intent": "Exercise the work charge of an evidence-present inside a where.",
    "question": "Was the identifier charged by its length?"
  },
  "outcomes": [
    {"id": "held", "label": "Held"},
    {"id": "did-not-hold", "label": "Did not hold"}
  ],
  "evidenceRequirements": [
    {"id": %q, "description": "Declared so the reference resolves.", "required": false}
  ],
  "rules": [
    {
      "id": "the-rule",
      "description": "Looks the requirement up once per element.",
      "when": %s,
      "outcome": "held",
      "onUnknown": "ignore"
    }
  ],
  "fallbackOutcome": "did-not-hold"
}`, id, condition(id)))
	}
	if _, failure := engine.EvaluateWith(pack(short), []byte(facts), nil, draftOptions(0)); failure != nil {
		t.Fatalf("a one-character requirement id over 200 elements must evaluate: %+v", failure)
	}
	_, failure := engine.EvaluateWith(pack(long), []byte(facts), nil, draftOptions(0))
	if failure == nil || failure.Code != "JPS-RESOURCE-EVALUATION-WORK-LIMIT" {
		t.Fatalf("a thousand-character requirement id over the same 200 elements must be refused: %+v", failure)
	}
}

// The adversarial empty-token pointer row. A resolution loops once per
// reference token and a reference token may be empty, so "////…" performs one
// map traversal per token over no token bytes at all: a charge of one flat step
// plus the token bytes priced a hundred traversals at one unit — on every
// resolution, and a where resolves once per element. The step count is now the
// token count, so the charge is what the loop does.
func TestRFC0008EmptyReferenceTokensAreChargedPerStep(t *testing.T) {
	const elements = 1_000
	items := make([]string, 0, elements)
	for index := 0; index < elements; index++ {
		items = append(items, `{}`)
	}
	facts := fmt.Sprintf(`{"list":[%s]}`, strings.Join(items, ","))
	condition := func(path string) string {
		return fmt.Sprintf(
			`{"op":"exists","path":"/list","where":{"op":"fact","path":%q,"operator":"equals","value":true}}`, path)
	}
	// A hundred empty reference tokens against one, both carrying zero token
	// bytes: the pointers differ in traversals and in nothing else.
	const tokens = 100
	one, hundred := "/", strings.Repeat("/", tokens)

	oneCharge := chargeOf(t, condition(one), facts)
	hundredCharge := chargeOf(t, condition(hundred), facts)
	// 99 further steps on each of the 1,000 resolutions, and 99 further path
	// bytes on the single compile: 3,014 units against 102,113.
	if want := (tokens - 1) * (elements + 1); hundredCharge-oneCharge != want {
		t.Fatalf("charges %d and %d differ by %d; each further reference token must cost a step per resolution (%d)",
			oneCharge, hundredCharge, hundredCharge-oneCharge, want)
	}

	// And the difference decides admission: the one-token pointer evaluates and
	// the hundred-token one is refused rather than walked 100,000 times.
	engine := newTestEngine(t)
	if _, failure := engine.EvaluateWith(draftPack(condition(one), "escalate"), []byte(facts), nil, draftOptions(0)); failure != nil {
		t.Fatalf("a one-token pointer over %d elements must evaluate: %+v", elements, failure)
	}
	_, failure := engine.EvaluateWith(draftPack(condition(hundred), "escalate"), []byte(facts), nil, draftOptions(0))
	if failure == nil || failure.Code != "JPS-RESOURCE-EVALUATION-WORK-LIMIT" {
		t.Fatalf("a hundred-token pointer over the same %d elements must be refused: %+v", elements, failure)
	}
}

// The adversarial uniform row, and the fourth defect of the class the long
// pointer, the unpriced selected value, and the flat pointer step belong to.
// uniform elects one representative and compares it against every other member,
// so the representative is read once per member: one 80,001-byte token among
// 999 short tokens of the same value is work proportional to long×members, and
// the per-member charge alone came to long+members — 94,013 units against the
// default 100,000, admitted, with one permutation taking milliseconds and the
// other hundreds of times longer. The reread term prices those comparisons at
// (n-1)×max, so the input is refused, in either order and with the same error.
func TestRFC0008UniformRepeatedRepresentativeComparisonIsCharged(t *testing.T) {
	engine := newTestEngine(t)
	const condition = `{"op":"uniform","path":"/list","at":"/cabin"}`
	pack := draftPack(condition, "escalate")
	// The long token and the short one denote the same value, so no clause-3
	// counterexample can cut the pass short: every member is compared, and
	// every comparison is against the long representative in one order or the
	// short-then-long representative in the other.
	facts := func(long, short string, copies int, longFirst bool) string {
		items := make([]string, 0, copies+1)
		for index := 0; index < copies; index++ {
			items = append(items, fmt.Sprintf(`{"cabin":%s}`, short))
		}
		if longFirst {
			items = append([]string{fmt.Sprintf(`{"cabin":%s}`, long)}, items...)
		} else {
			items = append(items, fmt.Sprintf(`{"cabin":%s}`, long))
		}
		return fmt.Sprintf(`{"list":[%s]}`, strings.Join(items, ","))
	}

	// 10^80000 written out in full, against 999 copies of 1e80000.
	huge := "1" + strings.Repeat("0", 80_000)
	first := facts(huge, "1e80000", 999, true)
	last := facts(huge, "1e80000", 999, false)
	if firstCharge, lastCharge := chargeOf(t, condition, first), chargeOf(t, condition, last); firstCharge != lastCharge {
		t.Fatalf("the two permutations charge %d and %d; the charge must not depend on the order", firstCharge, lastCharge)
	}
	_, firstFailure := engine.EvaluateWith(pack, []byte(first), nil, draftOptions(0))
	_, lastFailure := engine.EvaluateWith(pack, []byte(last), nil, draftOptions(0))
	if firstFailure == nil || lastFailure == nil {
		t.Fatalf("both permutations must be refused: %+v / %+v", firstFailure, lastFailure)
	}
	if firstFailure.Code != "JPS-RESOURCE-EVALUATION-WORK-LIMIT" {
		t.Fatalf("exhaustion must be the work limit: %+v", firstFailure)
	}
	if !reflect.DeepEqual(firstFailure, lastFailure) {
		t.Fatalf("the two permutations must produce the same error:\n first=%+v\n  last=%+v", firstFailure, lastFailure)
	}

	// The same shape small enough to afford: identical charges and identical
	// dispositions in both orders, so the refusal above turns on the size and
	// not on the permutation. 12 for the aggregate, 13 then nine 6s for
	// "/cabin", 302 for the 301-byte token and 6 for each of the nine
	// five-byte ones, and a reread term of (10-1)×302.
	small := "1" + strings.Repeat("0", 300)
	smallFirst := facts(small, "1e300", 9, true)
	smallLast := facts(small, "1e300", 9, false)
	firstCharge, lastCharge := chargeOf(t, condition, smallFirst), chargeOf(t, condition, smallLast)
	if want := 12 + 13 + 9*6 + 302 + 9*6 + 9*302; firstCharge != want || lastCharge != want {
		t.Fatalf("charges %d and %d, want %d in both orders", firstCharge, lastCharge, want)
	}
	firstOutput, firstFailure := engine.EvaluateWith(pack, []byte(smallFirst), nil, draftOptions(0))
	lastOutput, lastFailure := engine.EvaluateWith(pack, []byte(smallLast), nil, draftOptions(0))
	if firstFailure != nil || lastFailure != nil {
		t.Fatalf("the affordable variant must evaluate in both orders: %+v / %+v", firstFailure, lastFailure)
	}
	if !reflect.DeepEqual(firstOutput.Disposition, lastOutput.Disposition) {
		t.Fatalf("permuted members must produce one disposition: %+v / %+v", firstOutput.Disposition, lastOutput.Disposition)
	}
	// 10^300 and 1e300 are one value, so uniform is true and the rule fires.
	if firstOutput.Disposition.Kind != "outcome" || firstOutput.Disposition.OutcomeID != "held" {
		t.Fatalf("disposition = %+v; the members are equal, so uniform holds", firstOutput.Disposition)
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
	// sum, which is exactly three cells cheaper. A cell after the first costs
	// six units — its fact node, one step of the already-compiled "/ok", the
	// boolean it selects, and the boolean operand — so the difference is 18.
	if rectangularCharge-raggedCharge != 18 {
		t.Fatalf("ragged %d and rectangular %d must differ by the three absent cells (18 units)", raggedCharge, rectangularCharge)
	}
}
