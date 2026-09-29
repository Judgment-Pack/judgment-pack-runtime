package evaluation

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The rows below follow the Conformance section of the specification's RFC 0016
// (Draft): its document cases, its evaluation rows and its error rows, each
// named after the case it runs. Every pack is invented for the row it is in.

// valuesPack is one synthetic pack for a draft RFC 0016 row. Every member is
// JSON text, so a row can write what a Go value cannot hold: a member given
// twice, an escape, an order of members.
type valuesPack struct {
	// outcomes is the outcomes array. Empty selects approve, which carries the
	// declaration of the RFC's own example, and decline, which carries none.
	outcomes string
	// rules is the rules array. Empty selects one rule that produces approve
	// where /go is true.
	rules string
	// more is further root members, each followed by a comma.
	more string
	// required is the items of metadata.requiredExtensions. Empty lists the
	// extension once, and "-" leaves metadata out.
	required string
	// decision is further members of the decision object, each preceded by a
	// comma.
	decision string
}

const (
	exampleDeclaration = `{"refundAmount": {"type": "decimal", "fromFact": "/proposed/refundAmount"}, "currency": {"type": "string", "constant": "CAD"}}`
	goRule             = `{"id": "the-rule", "description": "The one rule.", "when": {"op": "fact", "path": "/go", "operator": "equals", "value": true}, "outcome": "approve", "onUnknown": "ignore"}`
	escalateToQueue    = `"escalation": {"triggers": ["unknown"], "target": {"kind": "queue", "name": "refund-review"}},`
)

// declaring writes one outcome that carries a value declaration.
func declaring(id, declaration string) string {
	return fmt.Sprintf(`{"id": %q, "label": "An outcome", "extensions": {%q: %s}}`, id, OutcomeValuesExtension, declaration)
}

// plain writes one outcome that carries none.
func plain(id string) string {
	return fmt.Sprintf(`{"id": %q, "label": "An outcome"}`, id)
}

func (p valuesPack) bytes() []byte {
	outcomes, rules, required := p.outcomes, p.rules, p.required
	if outcomes == "" {
		outcomes = "[" + declaring("approve", exampleDeclaration) + ", " + plain("decline") + "]"
	}
	if rules == "" {
		rules = "[" + goRule + "]"
	}
	metadata := ""
	switch required {
	case "-":
	case "":
		metadata = fmt.Sprintf(`"metadata": {"requiredExtensions": [%q]},`, OutcomeValuesExtension)
	default:
		metadata = fmt.Sprintf(`"metadata": {"requiredExtensions": [%s]},`, required)
	}
	return []byte(fmt.Sprintf(`{
  "specVersion": "0.2.0-draft",
  "id": "https://example.invalid/judgment-packs/rfc0016-row",
  "version": "0.1.0",
  "title": "Synthetic draft RFC 0016 row",
  "description": "Invented content for specification testing; it authorizes nothing.",
  "decision": {
    "intent": "Exercise one draft RFC 0016 declaration against one facts document.",
    "question": "Is the outcome produced, and with which values?"%s
  },
  %s
  %s
  "outcomes": %s,
  "rules": %s
}`, p.decision, metadata, p.more, outcomes, rules))
}

func valuesOptions() Options {
	return Options{Command: "test", RFC0016OutcomeValues: true}
}

// disposed evaluates one row under the opt-in and returns the canonical
// disposition, which is what RFC 0016 extends §8.3's byte agreement to.
func disposed(t *testing.T, pack valuesPack, facts string) string {
	t.Helper()
	evaluated, failure := newTestEngine(t).EvaluateWith(pack.bytes(), []byte(facts), nil, valuesOptions())
	if failure != nil {
		t.Fatalf("the row must evaluate: %s: %s", failure.Code, failure.Message)
	}
	canonical, err := evaluated.Disposition.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return string(canonical)
}

// refused evaluates one row under the opt-in and returns its refusal.
func refused(t *testing.T, pack []byte, facts string) *Failure {
	t.Helper()
	evaluated, failure := newTestEngine(t).EvaluateWith(pack, []byte(facts), nil, valuesOptions())
	if failure == nil {
		t.Fatalf("the row must be refused, and was evaluated to %+v", evaluated.Disposition)
	}
	return failure
}

const (
	unresolvedNoHandoff = `{"handoff":{"state":"none"},"kind":"unresolved","reasons":["unknown"]}`
	unresolvedRequested = `{"handoff":{"state":"requested","triggeredBy":["unknown"]},"kind":"unresolved","reasons":["unknown"]}`
)

// The RFC's own example, to the byte: the pass-through pack with the facts that
// supply the amount, and the disposition shown under "The disposition".
func TestRFC0016TheExampleOfTheRFC(t *testing.T) {
	pack := valuesPack{
		outcomes: "[" + declaring("approve-refund", exampleDeclaration) + ", " + plain("decline") + "]",
		rules:    `[{"id": "good-standing", "description": "A customer in good standing is refunded.", "when": {"op": "fact", "path": "/customer/goodStanding", "operator": "equals", "value": true}, "outcome": "approve-refund", "onUnknown": "ignore"}]`,
	}
	got := disposed(t, pack, `{"customer": {"goodStanding": true}, "proposed": {"refundAmount": "149.50"}}`)
	want := `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"approve-refund","reasons":[],"value":{"currency":"CAD","refundAmount":"149.50"}}`
	if got != want {
		t.Fatalf("disposition = %s, want %s", got, want)
	}
	for name, facts := range map[string]string{
		"the amount absent":          `{"customer": {"goodStanding": true}}`,
		"the amount a JSON number":   `{"customer": {"goodStanding": true}, "proposed": {"refundAmount": 149.5}}`,
		"the amount's parent absent": `{"customer": {"goodStanding": true}, "proposed": null}`,
	} {
		if got := disposed(t, pack, facts); got != unresolvedNoHandoff {
			t.Errorf("%s: disposition = %s, want %s", name, got, unresolvedNoHandoff)
		}
	}
}

// Document cases, positive: each is admitted, and each is evaluated to the
// outcome with the values it declares.
func TestRFC0016DeclarationsTheGateAdmits(t *testing.T) {
	// A row that says it writes a surrogate pair has to hold the escape and
	// not the character, or it runs another case under that name.
	surrogatePair := string([]byte{0x5c}) + "ud83d" + string([]byte{0x5c}) + "ude00"
	for _, row := range []struct {
		name        string
		declaration string
		facts       string
		value       string
	}{
		{"a string constant", `{"currency": {"type": "string", "constant": "CAD"}}`, `{"go": true}`, `{"currency":"CAD"}`},
		{"a decimal constant", `{"creditLimit": {"type": "decimal", "constant": "10000"}}`, `{"go": true}`, `{"creditLimit":"10000"}`},
		{"a negative decimal constant with a fraction", `{"adjustment": {"type": "decimal", "constant": "-0.50"}}`, `{"go": true}`, `{"adjustment":"-0.50"}`},
		{"a Boolean constant", `{"needsReceipt": {"type": "boolean", "constant": true}}`, `{"go": true}`, `{"needsReceipt":true}`},
		{"a value drawn from a fact", `{"refundAmount": {"type": "decimal", "fromFact": "/amount"}}`, `{"go": true, "amount": "149.50"}`, `{"refundAmount":"149.50"}`},
		{"a constant and a value drawn from a fact", exampleDeclaration, `{"go": true, "proposed": {"refundAmount": "1"}}`, `{"currency":"CAD","refundAmount":"1"}`},
		{
			"a string constant outside the Basic Multilingual Plane, written as a surrogate pair",
			`{"mark": {"type": "string", "constant": "\ud83d\ude00"}}`, `{"go": true}`, "{\"mark\":\"\U0001F600\"}",
		},
		{
			"the same constant written as the character itself",
			"{\"mark\": {\"type\": \"string\", \"constant\": \"\U0001F600\"}}", `{"go": true}`, "{\"mark\":\"\U0001F600\"}",
		},
		{"a value name of one letter", `{"a": {"type": "boolean", "constant": false}}`, `{"go": true}`, `{"a":false}`},
		{"a value name with capitals and digits after the first letter", `{"aB9z": {"type": "string", "constant": ""}}`, `{"go": true}`, `{"aB9z":""}`},
		{"a pointer that selects the root", `{"all": {"type": "boolean", "fromFact": ""}}`, `true`, ``},
		{"a pointer with both escapes", `{"odd": {"type": "string", "fromFact": "/a~1b/c~0d"}}`, `{"go": true, "a/b": {"c~d": "found"}}`, `{"odd":"found"}`},
	} {
		t.Run(row.name, func(t *testing.T) {
			if strings.Contains(row.name, "surrogate pair") != strings.Contains(row.declaration, surrogatePair) {
				t.Fatalf("the row's name and its declaration disagree about the escape: %s", row.declaration)
			}
			pack := valuesPack{outcomes: "[" + declaring("approve", row.declaration) + ", " + plain("decline") + "]"}
			if row.value == "" {
				// The facts document is the Boolean true, so /go does not resolve
				// and no rule is true. The row is about admission: the pointer's
				// form is admitted, and the pack is evaluated.
				if got := disposed(t, pack, row.facts); got != `{"handoff":{"state":"none"},"kind":"unresolved","reasons":["no-match"]}` {
					t.Fatalf("disposition = %s", got)
				}
				return
			}
			want := `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"approve","reasons":[],"value":` + row.value + `}`
			if got := disposed(t, pack, row.facts); got != want {
				t.Fatalf("disposition = %s, want %s", got, want)
			}
		})
	}
}

// Document cases, negative: each is the pack-not-conformant error of §8.4, in
// the preflight, under the draft's own code, and the refusal names the fault.
func TestRFC0016DeclarationsTheGateRefuses(t *testing.T) {
	for _, row := range []struct {
		name        string
		declaration string
		at          string
		says        string
	}{
		{"both constant and fromFact", `{"v": {"type": "string", "constant": "a", "fromFact": "/a"}}`, "/v", "exactly one of constant and fromFact"},
		{"neither constant nor fromFact", `{"v": {"type": "string"}}`, "/v", "exactly one of constant and fromFact"},
		{"an unknown type", `{"v": {"type": "number", "constant": "1"}}`, "/v/type", `"string", "decimal" or "boolean"`},
		{"a type in capitals", `{"v": {"type": "String", "constant": "1"}}`, "/v/type", `"string", "decimal" or "boolean"`},
		{"a type that is not a string", `{"v": {"type": true, "constant": true}}`, "/v/type", `"string", "decimal" or "boolean"`},
		{"no type", `{"v": {"constant": "1"}}`, "/v/type", "requires the type"},
		{"a member the RFC does not define", `{"v": {"type": "string", "constant": "a", "unit": "CAD"}}`, "/v/unit", "Member is not allowed"},
		{"a decimal constant with an exponent", `{"v": {"type": "decimal", "constant": "1e3"}}`, "/v/constant", "decimal grammar of JPS §2.2"},
		{"a decimal constant with a leading zero", `{"v": {"type": "decimal", "constant": "01"}}`, "/v/constant", "decimal grammar of JPS §2.2"},
		{"a decimal constant with a leading plus", `{"v": {"type": "decimal", "constant": "+1"}}`, "/v/constant", "decimal grammar of JPS §2.2"},
		{"a decimal constant with surrounding whitespace", `{"v": {"type": "decimal", "constant": " 1"}}`, "/v/constant", "decimal grammar of JPS §2.2"},
		{"a decimal constant that ends in a line feed", `{"v": {"type": "decimal", "constant": "1\n"}}`, "/v/constant", "decimal grammar of JPS §2.2"},
		{"a decimal constant given as a JSON number", `{"v": {"type": "decimal", "constant": 5000}}`, "/v/constant", "decimal grammar of JPS §2.2"},
		{"a Boolean constant given as the string true", `{"v": {"type": "boolean", "constant": "true"}}`, "/v/constant", "a JSON Boolean"},
		{"a string constant given as a Boolean", `{"v": {"type": "string", "constant": true}}`, "/v/constant", "a JSON string"},
		{"a string constant given as null", `{"v": {"type": "string", "constant": null}}`, "/v/constant", "a JSON string"},
		{"an empty declaration", `{}`, "", "at least one value"},
		{"a declaration that is an array", `[]`, "", "must be a JSON object"},
		{"a declaration that is null", `null`, "", "must be a JSON object"},
		{"a value source that is a string", `{"v": "CAD"}`, "/v", "value source must be a JSON object"},
		{"a value name that begins with a capital", `{"Amount": {"type": "string", "constant": "a"}}`, "/Amount", "is not a draft RFC 0016 value name"},
		{"a value name that begins with a digit", `{"1st": {"type": "string", "constant": "a"}}`, "/1st", "is not a draft RFC 0016 value name"},
		{"a value name that ends in a line feed", `{"amount\n": {"type": "string", "constant": "a"}}`, "/amount\n", "is not a draft RFC 0016 value name"},
		{"a value name with a hyphen", `{"refund-amount": {"type": "string", "constant": "a"}}`, "/refund-amount", "is not a draft RFC 0016 value name"},
		{"a value name with a letter outside ASCII", `{"montré": {"type": "string", "constant": "a"}}`, "/montré", "is not a draft RFC 0016 value name"},
		{"an empty value name", `{"": {"type": "string", "constant": "a"}}`, "/", "is not a draft RFC 0016 value name"},
		{"a pointer that does not begin with a solidus", `{"v": {"type": "string", "fromFact": "amount"}}`, "/v/fromFact", "must be a JSON Pointer string"},
		{"a pointer with a tilde that escapes nothing", `{"v": {"type": "string", "fromFact": "/a~2"}}`, "/v/fromFact", "must be a JSON Pointer string"},
		{"a pointer that is not a string", `{"v": {"type": "string", "fromFact": ["/a"]}}`, "/v/fromFact", "must be a JSON Pointer string"},
	} {
		t.Run(row.name, func(t *testing.T) {
			pack := valuesPack{outcomes: "[" + plain("decline") + ", " + declaring("approve", row.declaration) + "]"}
			failure := refused(t, pack.bytes(), `{"go": true}`)
			if failure.Class != result.ClassPackNotConformant || failure.Phase != result.PhasePreflight || failure.Code != "JPS-EVALUATION-RFC0016-GRAMMAR" {
				t.Fatalf("refusal = %s/%s/%s: %s", failure.Class, failure.Phase, failure.Code, failure.Message)
			}
			location := carrier.Pointer([]string{"outcomes", "1", "extensions", OutcomeValuesExtension}) + row.at
			if !strings.Contains(failure.Message, "JPS-EVALUATION-RFC0016-DECLARATION at "+location+": ") {
				t.Errorf("the refusal must name %s: %s", location, failure.Message)
			}
			if !strings.Contains(failure.Message, row.says) {
				t.Errorf("the refusal must say %q: %s", row.says, failure.Message)
			}
		})
	}
}

// The rule between a declaration and the name's entry as required, in each
// direction, and the entry made twice.
func TestRFC0016TheExtensionIsRequiredWhereItIsDeclared(t *testing.T) {
	other := `"extensions": {"com.example.review": {"by": "finance"}},`
	for _, row := range []struct {
		name string
		pack valuesPack
		at   string
		says string
	}{
		{
			"a declaration in a pack with no metadata",
			valuesPack{required: "-"},
			"/metadata/requiredExtensions", "to list " + OutcomeValuesExtension,
		},
		{
			"a declaration in a pack that requires another extension only",
			valuesPack{required: `"com.example.review"`, more: other},
			"/metadata/requiredExtensions", "to list " + OutcomeValuesExtension,
		},
		{
			"the name required and no outcome carrying a declaration",
			valuesPack{outcomes: "[" + plain("approve") + ", " + plain("decline") + "]", required: `"com.example.review", "` + OutcomeValuesExtension + `"`, more: other},
			"/metadata/requiredExtensions/1", "no outcome carries a value declaration",
		},
		{
			"the name required and carried on the root object alone",
			valuesPack{outcomes: "[" + plain("approve") + ", " + plain("decline") + "]", more: `"extensions": {"` + OutcomeValuesExtension + `": ` + exampleDeclaration + `},`},
			"/metadata/requiredExtensions/0", "no outcome carries a value declaration",
		},
		{
			"the name required twice",
			valuesPack{required: `"` + OutcomeValuesExtension + `", "` + OutcomeValuesExtension + `"`},
			"/metadata/requiredExtensions/1", "listed more than once",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			failure := refused(t, row.pack.bytes(), `{"go": true}`)
			if failure.Class != result.ClassPackNotConformant || failure.Phase != result.PhasePreflight || failure.Code != "JPS-EVALUATION-RFC0016-GRAMMAR" {
				t.Fatalf("refusal = %s/%s/%s: %s", failure.Class, failure.Phase, failure.Code, failure.Message)
			}
			if !strings.Contains(failure.Message, "JPS-EVALUATION-RFC0016-REQUIRED at "+row.at+": ") || !strings.Contains(failure.Message, row.says) {
				t.Errorf("the refusal must name %s and say %q: %s", row.at, row.says, failure.Message)
			}
		})
	}
}

// The name is admitted on an outcome and nowhere else. The gate lists no other
// place: the projection keeps the name wherever else the author wrote it, and
// the untouched validator refuses the reserved name there. The places below are
// every object of the bundled schema that has an extensions member, read off the
// schema by TestRFC0016EveryOtherExtensionsObjectIsTried.
func TestRFC0016TheNameIsRefusedEverywhereButOnAnOutcome(t *testing.T) {
	for place, pack := range elsewhere() {
		t.Run(place, func(t *testing.T) {
			failure := refused(t, pack.bytes(), `{"go": true}`)
			if failure.Class != result.ClassPackNotConformant || failure.Phase != result.PhasePreflight || failure.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" {
				t.Fatalf("refusal = %s/%s/%s: %s", failure.Class, failure.Phase, failure.Code, failure.Message)
			}
			for _, says := range []string{"Core projection", "JPS-STRUCTURE-EXTENSION-NAME", place + "/extensions/" + OutcomeValuesExtension, "one place earlier"} {
				if !strings.Contains(failure.Message, says) {
					t.Errorf("the refusal must say %q: %s", says, failure.Message)
				}
			}
		})
	}
}

// elsewhere is one pack for each object other than an outcome on which the
// schema admits an extensions member. Each carries the name there, and on an
// outcome as well, so that the gate's own rules are met and the refusal is the
// validator's. The key is the object's location in the pack.
func elsewhere() map[string]valuesPack {
	also := `"extensions": {"` + OutcomeValuesExtension + `": ` + exampleDeclaration + `}`
	return map[string]valuesPack{
		"":                        {more: also + ","},
		"/decision":               {decision: ", " + also},
		"/rules/0":                {rules: "[" + strings.TrimSuffix(goRule, "}") + ", " + also + "}]"},
		"/exceptions/0":           {more: `"exceptions": [{"id": "the-exception", "description": "One exception.", "when": {"op": "literal", "value": false}, "effect": "escalate", "onUnknown": "ignore", ` + also + `}],`},
		"/escalation":             {more: `"escalation": {"triggers": ["unknown"], "target": {"kind": "queue", "name": "review"}, ` + also + `},`},
		"/evidenceRequirements/0": {more: `"evidenceRequirements": [{"id": "the-receipt", "description": "A receipt.", "required": false, ` + also + `}],`},
		"/sources/0":              {more: `"sources": [{"id": "the-policy", "title": "A policy", "locator": {"kind": "other", "value": "the policy binder"}, ` + also + `}],`},
		"/metadata":               {required: "-", more: `"metadata": {"requiredExtensions": ["` + OutcomeValuesExtension + `"], ` + also + `},`},
	}
}

// The places tried above are held to the bundled schema, so that a schema which
// admits an extensions member on one more object fails this test instead of
// leaving that object untried. Every object of the schema that declares the
// member is found by walking the schema, and each must be the outcome or a place
// elsewhere names.
func TestRFC0016EveryOtherExtensionsObjectIsTried(t *testing.T) {
	set, err := artifacts.Load(result.EvaluatorSpecVersion)
	if err != nil {
		t.Fatal(err)
	}
	schemaBytes, err := set.Schema()
	if err != nil {
		t.Fatal(err)
	}
	var schema any
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatal(err)
	}
	declaring := []string{}
	var walk func(node any, at string)
	walk = func(node any, at string) {
		switch typed := node.(type) {
		case map[string]any:
			if properties, ok := typed["properties"].(map[string]any); ok {
				if _, has := properties["extensions"]; has {
					declaring = append(declaring, at)
				}
			}
			for _, name := range sortedMembers(typed) {
				walk(typed[name], at+"/"+name)
			}
		case []any:
			for index, item := range typed {
				walk(item, fmt.Sprintf("%s/%d", at, index))
			}
		}
	}
	walk(schema, "")
	places := map[string]string{
		"":                           "",
		"/$defs/decision":            "/decision",
		"/$defs/rule":                "/rules/0",
		"/$defs/exception":           "/exceptions/0",
		"/$defs/escalation":          "/escalation",
		"/$defs/evidenceRequirement": "/evidenceRequirements/0",
		"/$defs/source":              "/sources/0",
		"/$defs/metadata":            "/metadata",
	}
	tried := elsewhere()
	found := 0
	for _, at := range declaring {
		if at == "/$defs/outcome" {
			continue
		}
		place, known := places[at]
		if !known {
			t.Errorf("the schema admits an extensions member at %s, and no row tries the name there", at)
			continue
		}
		if _, ok := tried[place]; !ok {
			t.Errorf("the schema admits an extensions member at %s, and elsewhere has no pack for %q", at, place)
		}
		found++
	}
	if found != len(tried) || len(tried) != len(places) {
		t.Errorf("the schema declares the member on %d objects other than an outcome, and %d are tried of %d known", found, len(tried), len(places))
	}
}

// The three ways §8 produces an outcome, as one pack each: a true rule (step 9),
// a forced outcome (step 6) and the fallback (step 10). Each produces approve.
func producedBy(declaration, more string) map[string]struct {
	pack  valuesPack
	facts string
} {
	outcomes := "[" + declaring("approve", declaration) + ", " + plain("decline") + "]"
	never := `[{"id": "the-rule", "description": "A rule that is not true.", "when": {"op": "fact", "path": "/go", "operator": "equals", "value": true}, "outcome": "decline", "onUnknown": "ignore"}]`
	forcing := `"exceptions": [{"id": "the-exception", "description": "Forces the outcome.", "when": {"op": "fact", "path": "/force", "operator": "equals", "value": true}, "effect": "force-outcome", "outcome": "approve", "onUnknown": "ignore"}],`
	return map[string]struct {
		pack  valuesPack
		facts string
	}{
		"a true rule":      {valuesPack{outcomes: outcomes, more: more}, `{"go": true, "amount": "149.50"}`},
		"a forced outcome": {valuesPack{outcomes: outcomes, rules: never, more: more + forcing}, `{"go": true, "force": true, "amount": "149.50"}`},
		"the fallback":     {valuesPack{outcomes: outcomes, rules: never, more: more + `"fallbackOutcome": "approve",`}, `{"go": false, "amount": "149.50"}`},
	}
}

// Evaluation rows, positive: a constant, and a value drawn from a fact, carried
// on an outcome produced each of the three ways.
func TestRFC0016ValuesAreCarriedHoweverTheOutcomeIsProduced(t *testing.T) {
	for _, row := range []struct {
		name        string
		declaration string
		value       string
	}{
		{"a constant", `{"creditLimit": {"type": "decimal", "constant": "5000"}}`, `{"creditLimit":"5000"}`},
		{"a value drawn from a fact", `{"refundAmount": {"type": "decimal", "fromFact": "/amount"}}`, `{"refundAmount":"149.50"}`},
	} {
		for way, produced := range producedBy(row.declaration, "") {
			t.Run(row.name+" by "+way, func(t *testing.T) {
				want := `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"approve","reasons":[],"value":` + row.value + `}`
				if got := disposed(t, produced.pack, produced.facts); got != want {
					t.Fatalf("disposition = %s, want %s", got, want)
				}
			})
		}
	}
}

// Evaluation rows, negative and handoff: a value that does not resolve
// withholds the outcome however it was produced. The fallback is not tried in
// its place, and the handoff follows §8.1 as for any other unknown.
func TestRFC0016AValueThatDoesNotResolveWithholdsTheOutcome(t *testing.T) {
	declaration := `{"refundAmount": {"type": "decimal", "fromFact": "/missing"}}`
	for _, handoff := range []struct {
		name string
		more string
		want string
	}{
		{"no escalation object", "", unresolvedNoHandoff},
		{"triggers that name unknown", escalateToQueue, unresolvedRequested},
		{"triggers that do not name unknown", `"escalation": {"triggers": ["conflict", "no-match"], "target": {"kind": "queue", "name": "refund-review"}},`, unresolvedNoHandoff},
	} {
		for way, produced := range producedBy(declaration, handoff.more) {
			t.Run(handoff.name+", by "+way, func(t *testing.T) {
				if got := disposed(t, produced.pack, produced.facts); got != handoff.want {
					t.Fatalf("disposition = %s, want %s", got, handoff.want)
				}
			})
		}
	}

	// The target is reported beside the disposition where the handoff is
	// requested, as for any other request, and not where it is not.
	produced := producedBy(declaration, escalateToQueue)["a true rule"]
	evaluated, failure := newTestEngine(t).EvaluateWith(produced.pack.bytes(), []byte(produced.facts), nil, valuesOptions())
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if evaluated.HandoffTarget == nil || evaluated.HandoffTarget.Kind != "queue" || evaluated.HandoffTarget.Name != "refund-review" {
		t.Fatalf("handoff target = %+v", evaluated.HandoffTarget)
	}
}

// The fallback is not tried in place of a withheld outcome: a pack whose one
// true rule produces an outcome with a value that does not resolve, and whose
// fallback declares nothing, is unresolved and not the fallback.
func TestRFC0016TheFallbackIsNotTriedInPlaceOfAWithheldOutcome(t *testing.T) {
	pack := valuesPack{
		outcomes: "[" + declaring("approve", `{"refundAmount": {"type": "decimal", "fromFact": "/missing"}}`) + ", " + plain("decline") + "]",
		more:     `"fallbackOutcome": "decline",`,
	}
	if got := disposed(t, pack, `{"go": true}`); got != unresolvedNoHandoff {
		t.Fatalf("disposition = %s, want %s", got, unresolvedNoHandoff)
	}
}

// Two values of which one does not resolve: the disposition carries no value
// member at all. A not-applicable result and an unresolved result carry none.
func TestRFC0016NoValueIsCarriedWithoutAnOutcome(t *testing.T) {
	for _, row := range []struct {
		name  string
		pack  valuesPack
		facts string
		want  string
	}{
		{"two values, the one drawn from a fact absent", valuesPack{}, `{"go": true}`, unresolvedNoHandoff},
		{
			"applicability false",
			valuesPack{more: `"applicability": {"op": "fact", "path": "/inScope", "operator": "equals", "value": true},`},
			`{"go": true, "inScope": false, "proposed": {"refundAmount": "1"}}`,
			`{"handoff":{"state":"none"},"kind":"not-applicable","reasons":["not-applicable"]}`,
		},
		{
			"no rule true and no fallback",
			valuesPack{}, `{"go": false, "proposed": {"refundAmount": "1"}}`,
			`{"handoff":{"state":"none"},"kind":"unresolved","reasons":["no-match"]}`,
		},
		{
			"two rules true for two outcomes",
			valuesPack{rules: "[" + goRule + `, {"id": "the-other", "description": "Another rule.", "when": {"op": "literal", "value": true}, "outcome": "decline", "onUnknown": "ignore"}]`},
			`{"go": true, "proposed": {"refundAmount": "1"}}`,
			`{"handoff":{"state":"none"},"kind":"unresolved","reasons":["conflict"]}`,
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			got := disposed(t, row.pack, row.facts)
			if got != row.want {
				t.Fatalf("disposition = %s, want %s", got, row.want)
			}
			if strings.Contains(got, `"value"`) {
				t.Fatalf("the disposition must carry no value member: %s", got)
			}
		})
	}
}

// Evaluation rows, boundary and adversarial: what a declared type admits of a
// value selected from the facts, and that what is admitted is copied as found.
func TestRFC0016WhatIsSelectedFromTheFacts(t *testing.T) {
	for _, row := range []struct {
		name     string
		declared string
		fact     string
		value    string // the canonical member, or empty where the value does not resolve
	}{
		{"decimal: trailing zeroes are copied unchanged", "decimal", `"0.10"`, `"0.10"`},
		{"decimal: a whole number of many digits", "decimal", `"123456789012345678901234567890"`, `"123456789012345678901234567890"`},
		{"decimal: negative zero", "decimal", `"-0"`, `"-0"`},
		{"decimal: a JSON number", "decimal", `149.5`, ``},
		{"decimal: a JSON number that is whole", "decimal", `5000`, ``},
		{"decimal: null", "decimal", `null`, ``},
		{"decimal: an object", "decimal", `{"amount": "1"}`, ``},
		{"decimal: an array", "decimal", `["1"]`, ``},
		{"decimal: a Boolean", "decimal", `true`, ``},
		{"decimal: surrounding whitespace", "decimal", `" 149.50 "`, ``},
		{"decimal: a trailing line feed", "decimal", `"149.50\n"`, ``},
		{"decimal: an exponent", "decimal", `"1e3"`, ``},
		{"decimal: a leading plus", "decimal", `"+1"`, ``},
		{"decimal: a leading zero", "decimal", `"007"`, ``},
		{"decimal: no digit after the point", "decimal", `"1."`, ``},
		{"decimal: the empty string", "decimal", `""`, ``},
		{"string: the empty string", "string", `""`, `""`},
		{"string: surrounding whitespace is copied unchanged", "string", `"  CAD \t"`, `"  CAD \t"`},
		{"string: a string that looks like a decimal", "string", `"149.50"`, `"149.50"`},
		{"string: a character outside the Basic Multilingual Plane, written as a surrogate pair", "string", `"\ud83d\ude00"`, "\"\U0001F600\""},
		{"string: the same character written as itself", "string", "\"\U0001F600\"", "\"\U0001F600\""},
		{"string: a control character is written with its escape", "string", `"a\u0000b"`, `"a\u0000b"`},
		{"string: a JSON number", "string", `1`, ``},
		{"string: a Boolean", "string", `true`, ``},
		{"string: null", "string", `null`, ``},
		{"string: an array of strings", "string", `["CAD"]`, ``},
		{"boolean: false", "boolean", `false`, `false`},
		{"boolean: true", "boolean", `true`, `true`},
		{"boolean: the string true", "boolean", `"true"`, ``},
		{"boolean: the number one", "boolean", `1`, ``},
		{"boolean: null", "boolean", `null`, ``},
	} {
		t.Run(row.name, func(t *testing.T) {
			if strings.Contains(row.name, "surrogate pair") != strings.Contains(row.fact, string([]byte{0x5c})+"ud83d"+string([]byte{0x5c})+"ude00") {
				t.Fatalf("the row's name and its fact disagree about the escape: %s", row.fact)
			}
			pack := valuesPack{outcomes: "[" + declaring("approve", `{"v": {"type": "`+row.declared+`", "fromFact": "/selected"}}`) + ", " + plain("decline") + "]"}
			want := unresolvedNoHandoff
			if row.value != "" {
				want = `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"approve","reasons":[],"value":{"v":` + row.value + `}}`
			}
			if got := disposed(t, pack, `{"go": true, "selected": `+row.fact+`}`); got != want {
				t.Fatalf("disposition = %s, want %s", got, want)
			}
		})
	}
}

// A pointer resolves by the rules of §7.4, and one that does not resolve
// withholds the outcome.
func TestRFC0016PointersResolveAsFactPathsDo(t *testing.T) {
	for _, row := range []struct {
		name    string
		pointer string
		found   bool
	}{
		{"a member of an object", "/lines/0/amount", true},
		{"the last element of an array", "/lines/1/amount", true},
		{"an array traversed out of range", "/lines/2/amount", false},
		{"the past-the-end token", "/lines/-/amount", false},
		{"an index with a leading zero", "/lines/00/amount", false},
		{"a member of a string", "/currency/0", false},
		{"a member that is absent", "/lines/0/total", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			pack := valuesPack{outcomes: "[" + declaring("approve", `{"v": {"type": "decimal", "fromFact": "`+row.pointer+`"}}`) + ", " + plain("decline") + "]"}
			got := disposed(t, pack, `{"go": true, "currency": "CAD", "lines": [{"amount": "1"}, {"amount": "2"}]}`)
			if produced := strings.Contains(got, `"kind":"outcome"`); produced != row.found {
				t.Fatalf("disposition = %s", got)
			}
		})
	}
}

// A pointer that selects the root of the facts document selects the whole
// document, and the declared type is asked of that.
func TestRFC0016APointerToTheRootSelectsTheDocument(t *testing.T) {
	always := `[{"id": "the-rule", "description": "A rule that is true.", "when": {"op": "literal", "value": true}, "outcome": "approve", "onUnknown": "ignore"}]`
	for _, row := range []struct {
		declared string
		facts    string
		value    string
	}{
		{"string", `" root "`, `" root "`},
		{"decimal", `"0.10"`, `"0.10"`},
		{"boolean", `false`, `false`},
		{"string", `{}`, ``},
		{"decimal", `1`, ``},
		{"boolean", `null`, ``},
		{"string", `["a"]`, ``},
	} {
		pack := valuesPack{outcomes: "[" + declaring("approve", `{"v": {"type": "`+row.declared+`", "fromFact": ""}}`) + ", " + plain("decline") + "]", rules: always}
		want := unresolvedNoHandoff
		if row.value != "" {
			want = `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"approve","reasons":[],"value":{"v":` + row.value + `}}`
		}
		if got := disposed(t, pack, row.facts); got != want {
			t.Errorf("type %s, facts %s: disposition = %s, want %s", row.declared, row.facts, got, want)
		}
	}
}

// Two true rules naming the same outcome carry its values once. The same
// declaration with its members authored in another order yields the
// byte-identical disposition. And only the produced outcome is inspected: a
// declaration on another outcome that the facts cannot supply changes nothing.
func TestRFC0016OnlyTheProducedOutcomeIsResolvedAndOnce(t *testing.T) {
	want := `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"approve","reasons":[],"value":{"currency":"CAD","refundAmount":"149.50"}}`
	facts := `{"go": true, "proposed": {"refundAmount": "149.50"}}`
	reordered := `{"currency": {"constant": "CAD", "type": "string"}, "refundAmount": {"fromFact": "/proposed/refundAmount", "type": "decimal"}}`
	for _, row := range []struct {
		name string
		pack valuesPack
	}{
		{"as the RFC writes it", valuesPack{}},
		{"two true rules naming the same outcome", valuesPack{rules: "[" + goRule + `, {"id": "the-other", "description": "Another rule.", "when": {"op": "literal", "value": true}, "outcome": "approve", "onUnknown": "ignore"}]`}},
		{"the members authored in another order", valuesPack{outcomes: "[" + declaring("approve", reordered) + ", " + plain("decline") + "]"}},
		{"another outcome declaring a value the facts cannot supply", valuesPack{outcomes: "[" + declaring("approve", exampleDeclaration) + ", " + declaring("decline", `{"reason": {"type": "string", "fromFact": "/missing"}}`) + "]"}},
		{"the produced outcome listed after another that declares other values", valuesPack{outcomes: "[" + declaring("decline", `{"reason": {"type": "string", "constant": "policy"}}`) + ", " + declaring("approve", exampleDeclaration) + "]"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := disposed(t, row.pack, facts); got != want {
				t.Fatalf("disposition = %s, want %s", got, want)
			}
			// Once: the trace holds one entry for each of the two values, and
			// none for a value of another outcome.
			evaluated, failure := newTestEngine(t).EvaluateWith(row.pack.bytes(), []byte(facts), nil, valuesOptions())
			if failure != nil {
				t.Fatal(failure.Message)
			}
			traced := []string{}
			for _, entry := range evaluated.Trace {
				if entry.Stage == traceStageOutcomeValue {
					traced = append(traced, entry.ID+" of "+entry.Outcome)
				}
			}
			if !reflect.DeepEqual(traced, []string{"currency of approve", "refundAmount of approve"}) {
				t.Fatalf("values traced = %v", traced)
			}
		})
	}
}

// Error rows. A malformed declaration is found in the preflight, whether or not
// the outcome that carries it would have been produced, and it is the first
// class in §8.4's order.
func TestRFC0016AMalformedDeclarationIsFoundInThePreflight(t *testing.T) {
	malformed := "[" + plain("approve") + ", " + declaring("decline", `{"v": {"type": "decimal", "constant": "1e3"}}`) + "]"
	engine := newTestEngine(t)
	for _, row := range []struct {
		name     string
		pack     valuesPack
		facts    string
		evidence string
	}{
		{"on an outcome no rule names", valuesPack{outcomes: malformed}, `{"go": true}`, ""},
		{"in a pack whose applicability is false", valuesPack{outcomes: malformed, more: `"applicability": {"op": "literal", "value": false},`}, `{"go": true}`, ""},
		{"with an evidence document carrying an undeclared member name", valuesPack{outcomes: malformed}, `{"go": true}`, `{"undeclared": "present"}`},
		{"with a facts document that is not JSON", valuesPack{outcomes: malformed}, `{"go": `, ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			options := valuesOptions()
			options.EvidenceSupplied = row.evidence != ""
			_, failure := engine.EvaluateWith(row.pack.bytes(), []byte(row.facts), []byte(row.evidence), options)
			if failure == nil || failure.Class != result.ClassPackNotConformant || failure.Phase != result.PhasePreflight || failure.Code != "JPS-EVALUATION-RFC0016-GRAMMAR" {
				t.Fatalf("refusal = %+v", failure)
			}
		})
	}
}

// A fault Core itself defines comes before the draft's rules are reached, and
// is the same class: a member name given twice inside a declaration, and a
// string that holds an unpaired surrogate, are both refused by the carrier
// layer over the pack's real bytes.
func TestRFC0016TheCarrierIsHeldOverTheRealBytes(t *testing.T) {
	for _, row := range []struct {
		name        string
		declaration string
		says        string
	}{
		{"a value name given twice", `{"v": {"type": "string", "constant": "a"}, "v": {"type": "string", "constant": "b"}}`, "JPS-CARRIER-DUPLICATE-MEMBER"},
		{"a member of a value source given twice", `{"v": {"type": "string", "type": "decimal", "constant": "1"}}`, "JPS-CARRIER-DUPLICATE-MEMBER"},
		{"a string constant holding an unpaired surrogate", `{"v": {"type": "string", "constant": "\ud800"}}`, "unpaired surrogate"},
	} {
		t.Run(row.name, func(t *testing.T) {
			pack := valuesPack{outcomes: "[" + declaring("approve", row.declaration) + ", " + plain("decline") + "]"}
			failure := refused(t, pack.bytes(), `{"go": true}`)
			if failure.Class != result.ClassPackNotConformant || failure.Phase != result.PhasePreflight {
				t.Fatalf("refusal = %s/%s/%s: %s", failure.Class, failure.Phase, failure.Code, failure.Message)
			}
			if failure.Code == "JPS-EVALUATION-RFC0016-GRAMMAR" || !strings.Contains(failure.Message, row.says) {
				t.Fatalf("the carrier must refuse it, and say %q: %s: %s", row.says, failure.Code, failure.Message)
			}
		})
	}
}

// The RFC has a row this runtime cannot reach, and this test holds the
// difference in place so that it is a stated one (ADR-0039). The RFC expects a
// fact string that holds an unpaired surrogate not to resolve, which is an
// unresolved disposition. This runtime's carrier refuses the facts document
// that holds one, for every evaluation and whatever the pack declares, so the
// answer is the malformed-input error and no disposition.
func TestRFC0016AFactHoldingAnUnpairedSurrogateIsAMalformedInput(t *testing.T) {
	pack := valuesPack{outcomes: "[" + declaring("approve", `{"v": {"type": "string", "fromFact": "/selected"}}`) + ", " + plain("decline") + "]"}
	failure := refused(t, pack.bytes(), `{"go": true, "selected": "\udc00"}`)
	if failure.Class != result.ClassMalformedInput || failure.Phase != result.PhasePreflight || !strings.Contains(failure.Message, "unpaired surrogate") {
		t.Fatalf("refusal = %s/%s/%s: %s", failure.Class, failure.Phase, failure.Code, failure.Message)
	}
}

// Without the opt-in nothing changed. The bundled schema refuses the reserved
// name, so the pack is not structurally conforming and is refused as it was
// before this draft was prototyped, whatever the caller says it supports. The
// RFC's unsupported-required-extension row needs a schema that admits the
// name, which no published version has.
func TestRFC0016WithoutTheOptInThePackIsRefusedAsBefore(t *testing.T) {
	engine := newTestEngine(t)
	// The RFC's three rows for a consumer that does not support the extension
	// expect unsupported-required-extension for a declaration that is well
	// formed, empty, or of an unknown type, and pack-not-conformant for one
	// with a member given twice. The first three differ here, for the reason
	// above, and the difference is a stated one (ADR-0039). The fourth agrees.
	for _, row := range []struct {
		name        string
		declaration string
		says        string
	}{
		{"a well-formed declaration", exampleDeclaration, "JPS-STRUCTURE-EXTENSION-NAME"},
		{"an empty declaration", `{}`, "JPS-STRUCTURE-EXTENSION-NAME"},
		{"a declaration of an unknown type", `{"v": {"type": "money", "constant": "1"}}`, "JPS-STRUCTURE-EXTENSION-NAME"},
		{"a declaration with a member given twice", `{"v": {"type": "string", "constant": "a"}, "v": {"type": "string", "constant": "b"}}`, "JPS-CARRIER-DUPLICATE-MEMBER"},
	} {
		pack := valuesPack{outcomes: "[" + declaring("approve", row.declaration) + ", " + plain("decline") + "]"}
		_, failure := engine.EvaluateWith(pack.bytes(), []byte(`{"go": true}`), nil, Options{Command: "test"})
		if failure == nil || failure.Class != result.ClassPackNotConformant || failure.Phase != result.PhasePreflight || !strings.Contains(failure.Message, row.says) {
			t.Fatalf("%s: refusal = %+v", row.name, failure)
		}
	}
	for _, supported := range [][]string{nil, {OutcomeValuesExtension}} {
		_, failure := engine.EvaluateWith(valuesPack{}.bytes(), []byte(`{"go": true}`), nil, Options{Command: "test", SupportedExtensions: supported})
		if failure == nil || failure.Class != result.ClassPackNotConformant || failure.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" {
			t.Fatalf("supported %v: refusal = %+v", supported, failure)
		}
		if !strings.Contains(failure.Message, "JPS-STRUCTURE-EXTENSION-NAME") || strings.Contains(failure.Message, "projection") {
			t.Fatalf("supported %v: the refusal is the published path's: %s", supported, failure.Message)
		}
		if engine.Admits(valuesPack{}.bytes(), supported) {
			t.Fatalf("supported %v: Admits has no opt-in and must not admit the pack", supported)
		}
	}
	// Under the RFC 0008 opt-in the name is as reserved as it is without one.
	_, failure := engine.EvaluateWith(valuesPack{}.bytes(), []byte(`{"go": true}`), nil, Options{Command: "test", RFC0008Quantifiers: true})
	if failure == nil || failure.Class != result.ClassPackNotConformant || !strings.Contains(failure.Message, "JPS-STRUCTURE-EXTENSION-NAME") {
		t.Fatalf("refusal = %+v", failure)
	}
}

// One evaluation runs under one draft. The pair is refused as an invocation,
// with no §8.4 class, before anything is decoded, validated or evaluated. The
// bytes handed in as the pack are not read for what they hold, so the refusal
// is the same for a pack, for bytes that are none and for no bytes.
func TestRFC0016TheTwoDraftsAreNotCombined(t *testing.T) {
	options := Options{Command: "test", RFC0008Quantifiers: true, RFC0016OutcomeValues: true}
	for name, pack := range map[string][]byte{"a pack that would evaluate": valuesPack{}.bytes(), "bytes that are no pack": []byte(`{`), "no bytes": nil} {
		_, failure := newTestEngine(t).EvaluateWith(pack, []byte(`{"go": true}`), nil, options)
		if failure == nil || failure.Code != "JPS-INVOCATION-DRAFT-RFC" || failure.Class != "" || failure.Phase != "" || failure.ExitCode != result.ExitInvocation {
			t.Fatalf("%s: refusal = %+v", name, failure)
		}
	}
}

// Everything the draft does not add is still held to full document
// conformance, through the projection, and a required extension the caller does
// not support is still the error it was, at its own place in §8.4's order.
func TestRFC0016TheProjectionIsHeldToCore(t *testing.T) {
	engine := newTestEngine(t)
	undeclared := valuesPack{rules: strings.Replace("["+goRule+"]", `"outcome": "approve"`, `"outcome": "undeclared"`, 1)}
	failure := refused(t, undeclared.bytes(), `{"go": true}`)
	if failure.Class != result.ClassPackNotConformant || failure.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" || !strings.Contains(failure.Message, "Core projection") || !strings.Contains(failure.Message, "/rules/0/outcome") {
		t.Fatalf("refusal = %s/%s: %s", failure.Class, failure.Code, failure.Message)
	}

	// Another reserved name on an outcome is no part of this draft.
	reserved := valuesPack{outcomes: "[" + declaring("approve", exampleDeclaration) + `, {"id": "decline", "label": "An outcome", "extensions": {"org.judgmentpack.other": true}}]`}
	failure = refused(t, reserved.bytes(), `{"go": true}`)
	if failure.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" || !strings.Contains(failure.Message, "/outcomes/1/extensions/org.judgmentpack.other") {
		t.Fatalf("refusal = %s: %s", failure.Code, failure.Message)
	}

	other := valuesPack{
		required: `"` + OutcomeValuesExtension + `", "com.example.review"`,
		more:     `"extensions": {"com.example.review": {"by": "finance"}},`,
	}
	facts := `{"go": true, "proposed": {"refundAmount": "149.50"}}`
	_, failure = engine.EvaluateWith(other.bytes(), []byte(facts), nil, valuesOptions())
	if failure == nil || failure.Class != result.ClassUnsupportedRequiredExtension || !strings.Contains(failure.Message, "com.example.review") || strings.Contains(failure.Message, OutcomeValuesExtension) {
		t.Fatalf("refusal = %+v", failure)
	}
	_, failure = engine.EvaluateWith(other.bytes(), []byte(`{"go": `), nil, valuesOptions())
	if failure == nil || failure.Class != result.ClassMalformedInput {
		t.Fatalf("a malformed input comes before an unsupported required extension: %+v", failure)
	}
	options := valuesOptions()
	options.SupportedExtensions = []string{"com.example.review"}
	evaluated, failure := engine.EvaluateWith(other.bytes(), []byte(facts), nil, options)
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if evaluated.Disposition.Kind != "outcome" || !reflect.DeepEqual(evaluated.Disposition.Value, map[string]any{"currency": "CAD", "refundAmount": "149.50"}) {
		t.Fatalf("disposition = %+v", evaluated.Disposition)
	}
}

// The projection takes out what the draft adds and nothing else, keeps both
// containers, and leaves the document it was given as it was.
func TestRFC0016TheProjection(t *testing.T) {
	pack := valuesPack{
		outcomes: "[" + declaring("approve", exampleDeclaration) + `, {"id": "decline", "label": "An outcome", "extensions": {"com.example.review": 1, "` + OutcomeValuesExtension + `": {"v": {"type": "boolean", "constant": true}}}}, ` + plain("defer") + "]",
		required: `"com.example.first", "` + OutcomeValuesExtension + `", "com.example.review"`,
	}
	decode := func() map[string]any {
		document, failure := carrier.Decode(pack.bytes(), carrier.DefaultLimits())
		if failure != nil {
			t.Fatal(failure.Diagnostic.Message)
		}
		return document.(map[string]any)
	}
	root := decode()
	projected := projectOutcomeValues(root)
	if !reflect.DeepEqual(root, decode()) {
		t.Fatal("the projection changed the document it was given")
	}
	encoded, err := json.Marshal(map[string]any{"metadata": projected["metadata"], "outcomes": projected["outcomes"]})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"metadata":{"requiredExtensions":["com.example.first","com.example.review"]},"outcomes":[{"extensions":{},"id":"approve","label":"An outcome"},{"extensions":{"com.example.review":1},"id":"decline","label":"An outcome"},{"id":"defer","label":"An outcome"}]}`
	if string(encoded) != want {
		t.Fatalf("projection = %s, want %s", encoded, want)
	}
	for _, member := range sortedMembers(root) {
		if member != "metadata" && member != "outcomes" && !reflect.DeepEqual(root[member], projected[member]) {
			t.Errorf("the projection changed %q", member)
		}
	}
	if len(projected) != len(root) {
		t.Errorf("the projection has %d members and the pack %d", len(projected), len(root))
	}
}

// The marker says what the pack uses of the draft, and says of a pack that uses
// none of it that it is a plain pack. Such a pack is evaluated under the opt-in
// to the disposition and the trace it has without it.
func TestRFC0016TheMarker(t *testing.T) {
	engine := newTestEngine(t)
	pack := valuesPack{outcomes: "[" + declaring("later", `{"v": {"type": "boolean", "constant": true}}`) + ", " + declaring("approve", exampleDeclaration) + ", " + plain("decline") + "]"}
	evaluated, failure := engine.EvaluateWith(pack.bytes(), []byte(`{"go": true, "proposed": {"refundAmount": "1"}}`), nil, valuesOptions())
	if failure != nil {
		t.Fatal(failure.Message)
	}
	marker := evaluated.DraftPrototype
	if marker == nil || marker.RFC != "0016" || marker.Status != "draft-rfc-prototype" || marker.PackValidUnderSpecVersion {
		t.Fatalf("marker = %+v", marker)
	}
	if !reflect.DeepEqual(marker.Outcomes, []string{"approve", "later"}) || marker.Operators == nil || len(marker.Operators) != 0 {
		t.Fatalf("marker = %+v", marker)
	}
	for _, says := range []string{"Draft RFC 0016", "NOT valid", "spec validate rejects it", "see CONFORMANCE.md", "no member of §8.3", "copy of that fact"} {
		if !strings.Contains(marker.Note, says) {
			t.Errorf("the note must say %q: %s", says, marker.Note)
		}
	}
	encoded, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(encoded), `{"rfc":"0016","status":"draft-rfc-prototype","operators":[],"outcomes":["approve","later"],"packValidUnderSpecVersion":false,"note":"`) {
		t.Fatalf("marker = %s", encoded)
	}

	plainPack := intakePack(t)
	facts := factsJSON(t, "data-access", "complete", "appropriate", boolPtr(false))
	under, failure := engine.EvaluateWith(plainPack, facts, nil, valuesOptions())
	if failure != nil {
		t.Fatal(failure.Message)
	}
	without, failure := engine.EvaluateWith(plainPack, facts, nil, Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	marker = under.DraftPrototype
	if marker == nil || marker.RFC != "0016" || !marker.PackValidUnderSpecVersion || len(marker.Outcomes) != 0 || !strings.Contains(marker.Note, "remains a plain JPS") || strings.Contains(marker.Note, "NOT valid") {
		t.Fatalf("marker = %+v", marker)
	}
	if encoded, _ := json.Marshal(marker); strings.Contains(string(encoded), `"outcomes"`) {
		t.Fatalf("a pack that declares no value has no outcomes member in its marker: %s", encoded)
	}
	if without.DraftPrototype != nil {
		t.Fatalf("an evaluation without an opt-in carries no marker: %+v", without.DraftPrototype)
	}
	under.DraftPrototype = nil
	if !reflect.DeepEqual(under, without) {
		t.Fatalf("a pack that declares no value must evaluate as it does without the opt-in:\n%+v\n%+v", under, without)
	}
}

// The marker of RFC 0008 is what it was before the outcomes member existed.
func TestRFC0016LeavesTheMarkerOfRFC0008AsItWas(t *testing.T) {
	const predicate = `{"op":"fact","path":"/ok","operator":"equals","value":true}`
	evaluated, failure := newTestEngine(t).EvaluateWith(draftPack(`{"op":"exists","path":"/rows","where":`+predicate+`}`, "ignore"), []byte(`{"rows": [{"ok": true}]}`), nil, draftOptions(0))
	if failure != nil {
		t.Fatal(failure.Message)
	}
	encoded, err := json.Marshal(evaluated.DraftPrototype)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(encoded), `{"rfc":"0008","status":"draft-rfc-prototype","operators":["exists"],"packValidUnderSpecVersion":false,"note":"`) {
		t.Fatalf("marker = %s", encoded)
	}
	canonical, err := evaluated.Disposition.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"held","reasons":[]}` {
		t.Fatalf("disposition = %s", canonical)
	}
}

// The trace names each value of the produced outcome, in the order of the value
// names and after the stages that produced the outcome, and says whether it
// resolved. It never carries a value.
func TestRFC0016TheTraceNamesTheValues(t *testing.T) {
	engine := newTestEngine(t)
	rule := result.TraceEntry{Stage: "rule", ID: "the-rule", Condition: "true", Outcome: "approve"}
	for _, row := range []struct {
		name  string
		facts string
		want  []result.TraceEntry
	}{
		{"every value resolved", `{"go": true, "proposed": {"refundAmount": "149.50"}}`, []result.TraceEntry{
			rule,
			{Stage: "outcome-value", ID: "currency", Condition: "resolved", Outcome: "approve"},
			{Stage: "outcome-value", ID: "refundAmount", Condition: "resolved", Outcome: "approve"},
		}},
		{"one value selected and not of its type", `{"go": true, "proposed": {"refundAmount": 149.50}}`, []result.TraceEntry{
			rule,
			{Stage: "outcome-value", ID: "currency", Condition: "resolved", Outcome: "approve"},
			{Stage: "outcome-value", ID: "refundAmount", Condition: "unresolved", Outcome: "approve"},
		}},
		{"one value did not resolve", `{"go": true}`, []result.TraceEntry{
			rule,
			{Stage: "outcome-value", ID: "currency", Condition: "resolved", Outcome: "approve"},
			{Stage: "outcome-value", ID: "refundAmount", Condition: "unresolved", Outcome: "approve"},
		}},
		{"no outcome produced", `{"go": false}`, []result.TraceEntry{{Stage: "rule", ID: "the-rule", Condition: "false"}}},
	} {
		t.Run(row.name, func(t *testing.T) {
			evaluated, failure := engine.EvaluateWith(valuesPack{}.bytes(), []byte(row.facts), nil, valuesOptions())
			if failure != nil {
				t.Fatal(failure.Message)
			}
			if !reflect.DeepEqual(evaluated.Trace, row.want) {
				t.Fatalf("trace = %+v, want %+v", evaluated.Trace, row.want)
			}
			encoded, err := json.Marshal(evaluated.Trace)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "149.50") || strings.Contains(string(encoded), "CAD") {
				t.Fatalf("the trace must carry no value: %s", encoded)
			}
		})
	}
	// An outcome that declares nothing records nothing, under the opt-in too.
	pack := valuesPack{rules: strings.Replace("["+goRule+"]", `"outcome": "approve"`, `"outcome": "decline"`, 1)}
	evaluated, failure := engine.EvaluateWith(pack.bytes(), []byte(`{"go": true}`), nil, valuesOptions())
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if want := []result.TraceEntry{{Stage: "rule", ID: "the-rule", Condition: "true", Outcome: "decline"}}; !reflect.DeepEqual(evaluated.Trace, want) || evaluated.Disposition.Value != nil {
		t.Fatalf("trace = %+v, value = %v", evaluated.Trace, evaluated.Disposition.Value)
	}
}

// A value that does not resolve is recorded for every value, including one
// that follows another that did not resolve.
func TestRFC0016EveryValueIsTracedAfterOneThatDidNotResolve(t *testing.T) {
	declaration := `{"a": {"type": "string", "fromFact": "/missing"}, "b": {"type": "string", "constant": "kept"}, "c": {"type": "boolean", "fromFact": "/alsoMissing"}}`
	pack := valuesPack{outcomes: "[" + declaring("approve", declaration) + ", " + plain("decline") + "]"}
	evaluated, failure := newTestEngine(t).EvaluateWith(pack.bytes(), []byte(`{"go": true}`), nil, valuesOptions())
	if failure != nil {
		t.Fatal(failure.Message)
	}
	want := []result.TraceEntry{
		{Stage: "rule", ID: "the-rule", Condition: "true", Outcome: "approve"},
		{Stage: "outcome-value", ID: "a", Condition: "unresolved", Outcome: "approve"},
		{Stage: "outcome-value", ID: "b", Condition: "resolved", Outcome: "approve"},
		{Stage: "outcome-value", ID: "c", Condition: "unresolved", Outcome: "approve"},
	}
	if !reflect.DeepEqual(evaluated.Trace, want) || evaluated.Disposition.Kind != "unresolved" || evaluated.Disposition.Value != nil {
		t.Fatalf("trace = %+v, disposition = %+v", evaluated.Trace, evaluated.Disposition)
	}
}

// The resolution step is charged to the evaluation-work limit, and reaching the
// limit in it is the resource-exhaustion error and no disposition. The charge
// is what the step's comment says it is: the search among the outcomes, by
// their number and the bytes of their ids; the declaration, by its whole size;
// each pointer, scanned and resolved; and each value a pointer selected,
// whether or not its type then admits it.
func TestRFC0016TheResolutionStepIsCharged(t *testing.T) {
	const amount = "149.50"
	pack := valuesPack{}
	facts := `{"go": true, "proposed": {"refundAmount": "` + amount + `"}}`
	charged := func(values bool) int {
		packRoot, failure := carrier.Decode(pack.bytes(), carrier.DefaultLimits())
		if failure != nil {
			t.Fatal(failure.Diagnostic.Message)
		}
		factsRoot, failure := carrier.Decode([]byte(facts), carrier.DefaultLimits())
		if failure != nil {
			t.Fatal(failure.Diagnostic.Message)
		}
		eval := &evaluator{outcomeValues: values, budget: DefaultCoreWorkLimit}
		if _, _, _, refusal := resolve(packRoot.(map[string]any), factsRoot, eval); refusal != nil {
			t.Fatal(refusal.Message)
		}
		return eval.charged
	}
	without, with := charged(false), charged(true)
	if with-without != stepOutcomes+stepDeclaration+stepScan+stepResolution+stepSelected {
		t.Fatalf("the step is charged %d units, and its terms come to %d", with-without, stepOutcomes+stepDeclaration+stepScan+stepResolution+stepSelected)
	}

	// A pointer that selects nothing is charged for its resolution, and no
	// value is charged for it, because none is carried.
	resolving := facts
	facts = `{"go": true, "proposed": {}}`
	if nothing := charged(true) - charged(false); nothing != stepOutcomes+stepDeclaration+stepScan+stepResolution {
		t.Fatalf("with nothing selected the step is charged %d units, and its terms come to %d", nothing, stepOutcomes+stepDeclaration+stepScan+stepResolution)
	}
	facts = resolving

	engine := newTestEngine(t)
	options := valuesOptions()
	options.WorkBudget = with
	if _, failure := engine.EvaluateWith(pack.bytes(), []byte(facts), nil, options); failure != nil {
		t.Fatalf("a budget of exactly the charge must hold: %s", failure.Message)
	}
	for _, budget := range []int{with - 1, without + 1, without} {
		options.WorkBudget = budget
		evaluated, failure := engine.EvaluateWith(pack.bytes(), []byte(facts), nil, options)
		if failure == nil || failure.Class != result.ClassResourceExhaustion || failure.Phase != result.PhaseEvaluation || failure.Code != "JPS-RESOURCE-EVALUATION-WORK-LIMIT" {
			t.Fatalf("budget %d: refusal = %+v, disposition = %+v", budget, failure, evaluated.Disposition)
		}
	}

	// An outcome that declares nothing is charged the search for it and
	// nothing else.
	pack = valuesPack{rules: strings.Replace("["+goRule+"]", `"outcome": "approve"`, `"outcome": "decline"`, 1)}
	if without, with := charged(false), charged(true); with-without != stepOutcomes {
		t.Fatalf("an outcome with no declaration is charged %d units by the step, and the search comes to %d", with-without, stepOutcomes)
	}
}

// The draft adds no limit of its own. A declaration is bounded by the carrier
// layer when the pack is admitted, and what it costs to resolve by the work
// limit: values that each select one string of the largest size the carrier
// admits are carried while their sizes fit the limit together, and reach it
// when they do not. The number that fit is a measurement of this pack under the
// default limit, with its short names, its one short pointer and its one rule.
// Longer names or a costlier rule leave room for fewer.
func TestRFC0016ManyLargeValuesReachTheWorkLimit(t *testing.T) {
	largest := strings.Repeat("x", carrier.DefaultMaxStringBytes)
	facts := `{"go": true, "s": "` + largest + `"}`
	most := DefaultCoreWorkLimit / (unitValue + len(largest))
	for _, row := range []struct {
		values   int
		produced bool
	}{
		{most, true},
		{most + 1, false},
	} {
		sources := []string{}
		for index := 0; index < row.values; index++ {
			sources = append(sources, fmt.Sprintf(`"v%d": {"type": "string", "fromFact": "/s"}`, index))
		}
		pack := valuesPack{outcomes: "[" + declaring("approve", "{"+strings.Join(sources, ", ")+"}") + ", " + plain("decline") + "]"}
		evaluated, failure := newTestEngine(t).EvaluateWith(pack.bytes(), []byte(facts), nil, valuesOptions())
		if row.produced {
			if failure != nil || evaluated.Disposition.Kind != "outcome" || len(evaluated.Disposition.Value) != row.values || evaluated.Disposition.Value["v0"] != largest {
				t.Fatalf("%d values: refusal = %+v, kind = %q, values = %d", row.values, failure, evaluated.Disposition.Kind, len(evaluated.Disposition.Value))
			}
			continue
		}
		if failure == nil || failure.Class != result.ClassResourceExhaustion || failure.Phase != result.PhaseEvaluation {
			t.Fatalf("%d values: refusal = %+v", row.values, failure)
		}
	}
	if most != 19 {
		t.Fatalf("the limit holds %d such values of this pack, and the decision record says nineteen", most)
	}
}

// The terms of the step's charge for the pack valuesPack writes by default and
// facts that supply the amount as "149.50", each counted here by hand from the
// documents and not by the function that charges them.
const (
	// The pack declares two outcomes, and the search may read each id.
	stepOutcomes = (1 + len("approve")) + (1 + len("decline"))
	// The declaration is one object of two members. Each member costs its
	// name and its source, and a source is one object whose members each cost
	// their name and their string.
	stepDeclaration = 1 +
		len("refundAmount") + (1 + len("type") + (1 + len("decimal")) + len("fromFact") + (1 + len("/proposed/refundAmount"))) +
		len("currency") + (1 + len("type") + (1 + len("string")) + len("constant") + (1 + len("CAD")))
	// The pointer is scanned once, and resolved in two steps over its tokens.
	stepScan       = 1 + len("/proposed/refundAmount")
	stepResolution = 2 + len("proposed") + len("refundAmount")
	// The value the pointer selects.
	stepSelected = 1 + len("149.50")
)

// The work stops at the first charge the limit does not hold. Each row gives
// the step a limit one unit short of one of its charges and reads what was
// done: which values were traced, whether the pointer was scanned, and what
// had been charged when the step stopped. The values are read in the order of
// their names, the constant first.
//
// What was charged is what tells a step that stopped from one that went on to
// the next charge and was stopped there: a charge that is not held is still
// added, so a step that goes on has charged more. It is the only thing that
// tells the two apart at the search, where going on does nothing else that can
// be seen.
func TestRFC0016TheWorkStopsWhereTheLimitIsReached(t *testing.T) {
	const pointer = "/proposed/refundAmount"
	run := func(budget int, values bool) (*evaluator, []string, *Failure) {
		packRoot, failure := carrier.Decode(valuesPack{}.bytes(), carrier.DefaultLimits())
		if failure != nil {
			t.Fatal(failure.Diagnostic.Message)
		}
		factsRoot, failure := carrier.Decode([]byte(`{"go": true, "proposed": {"refundAmount": "149.50"}}`), carrier.DefaultLimits())
		if failure != nil {
			t.Fatal(failure.Diagnostic.Message)
		}
		eval := &evaluator{outcomeValues: values, budget: budget}
		_, _, trace, refusal := resolve(packRoot.(map[string]any), factsRoot, eval)
		traced := []string{}
		for _, entry := range trace {
			if entry.Stage == traceStageOutcomeValue {
				traced = append(traced, entry.ID)
			}
		}
		return eval, traced, refusal
	}
	core, _, refusal := run(DefaultCoreWorkLimit, false)
	if refusal != nil {
		t.Fatal(refusal.Message)
	}
	before := core.charged
	for _, row := range []struct {
		name string
		// charged is what the step has charged when it stops: every charge up
		// to the one the limit does not hold, that one included.
		charged int
		traced  []string
		scanned bool
		refused bool
	}{
		{"at the search among the outcomes", stepOutcomes, []string{}, false, true},
		{"at the declaration", stepOutcomes + stepDeclaration, []string{}, false, true},
		{"at the pointer's scan", stepOutcomes + stepDeclaration + stepScan, []string{"currency"}, false, true},
		{"at the pointer's resolution", stepOutcomes + stepDeclaration + stepScan + stepResolution, []string{"currency"}, true, true},
		{"at the value selected", stepOutcomes + stepDeclaration + stepScan + stepResolution + stepSelected, []string{"currency"}, true, true},
		{"nowhere", stepOutcomes + stepDeclaration + stepScan + stepResolution + stepSelected, []string{"currency", "refundAmount"}, true, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			budget := before + row.charged
			if row.refused {
				budget--
			}
			eval, traced, refusal := run(budget, true)
			if eval.charged != before+row.charged {
				t.Errorf("charged = %d units by the step, want %d", eval.charged-before, row.charged)
			}
			if row.refused != (refusal != nil) {
				t.Fatalf("refusal = %+v", refusal)
			}
			if refusal != nil && (refusal.Class != result.ClassResourceExhaustion || refusal.Phase != result.PhaseEvaluation) {
				t.Fatalf("refusal = %+v", refusal)
			}
			if !reflect.DeepEqual(traced, row.traced) {
				t.Errorf("values traced = %v, want %v", traced, row.traced)
			}
			if _, scanned := eval.pointers[pointer]; scanned != row.scanned {
				t.Errorf("pointer scanned = %v, want %v", scanned, row.scanned)
			}
		})
	}
}

// What a declared type admits, asked of the function itself. The gate refuses
// a type that is none of the three before resolution can ask about one, so no
// evaluation reaches the last rows, and they are held here.
func TestRFC0016WhatATypeAdmits(t *testing.T) {
	for _, row := range []struct {
		declared string
		value    any
		admitted bool
	}{
		{"string", "", true},
		{"string", "CAD", true},
		{"string", true, false},
		{"string", nil, false},
		{"string", json.Number("1"), false},
		{"string", []any{"CAD"}, false},
		{"decimal", "149.50", true},
		{"decimal", "-0", true},
		{"decimal", "1e3", false},
		{"decimal", "", false},
		{"decimal", json.Number("1"), false},
		{"decimal", nil, false},
		{"boolean", true, true},
		{"boolean", false, true},
		{"boolean", "true", false},
		{"boolean", nil, false},
		{"money", "1", false},
		{"money", true, false},
		{"", "1", false},
		{"String", "1", false},
	} {
		if admittedValue(row.declared, row.value) != row.admitted {
			t.Errorf("type %q, value %#v: admitted = %v, want %v", row.declared, row.value, !row.admitted, row.admitted)
		}
	}
}

// One admitted pack answers under the opt-in and without it, each from its own
// admission: the opt-in is part of what the memo is keyed on.
func TestRFC0016TheAdmissionIsKeyedOnTheOptIn(t *testing.T) {
	engine := newTestEngine(t)
	admitted := engine.AdmitPack(valuesPack{}.bytes())
	facts := []byte(`{"go": true, "proposed": {"refundAmount": "149.50"}}`)
	for round := 0; round < 2; round++ {
		if _, failure := engine.EvaluateAdmitted(admitted, facts, nil, Options{Command: "test"}); failure == nil || failure.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" {
			t.Fatalf("round %d, without the opt-in: %+v", round, failure)
		}
		evaluated, failure := engine.EvaluateAdmitted(admitted, facts, nil, valuesOptions())
		if failure != nil || evaluated.Disposition.Kind != "outcome" {
			t.Fatalf("round %d, under the opt-in: %+v %+v", round, failure, evaluated.Disposition)
		}
		if _, failure := engine.EvaluateAdmitted(admitted, facts, nil, Options{Command: "test", RFC0008Quantifiers: true}); failure == nil || failure.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" {
			t.Fatalf("round %d, under the other opt-in: %+v", round, failure)
		}
	}
	if admissionKey(Options{}) == admissionKey(valuesOptions()) || admissionKey(valuesOptions()) == admissionKey(draftOptions(0)) {
		t.Fatal("the three paths must have three keys")
	}
}

// An expected disposition is held to §8.3 as it is published. The reader of
// expectations refuses the draft's member, under its own sentence, in every
// spelling.
func TestRFC0016AnExpectedDispositionCarriesNoValue(t *testing.T) {
	for _, row := range []struct {
		raw  string
		says string
	}{
		{`{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"approve","reasons":[],"value":{"currency":"CAD"}}`, "draft RFC 0016"},
		{`{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"approve","reasons":[],"value":null}`, "draft RFC 0016"},
		{`{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"approve","reasons":[],"Value":{"currency":"CAD"}}`, `does not know: "Value"`},
		{`{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"approve","reasons":[],"VALUE":{}}`, `does not know: "VALUE"`},
	} {
		if _, err := DecodeDisposition(json.RawMessage(row.raw)); err == nil || !strings.Contains(err.Error(), row.says) {
			t.Errorf("%s: error = %v, want one that says %q", row.raw, err, row.says)
		}
	}
}

// A refusal names the first fault, and the gate finds them all. Each is
// reported once, where it is, in an order that does not depend on the order the
// pack's members were read in: outcomes as the pack lists them, the values of a
// declaration by name, and the rule about the required entry last. A constant is
// judged against its type only where the type is one of the three, so an
// unknown type is one fault and not two.
func TestRFC0016TheGateReportsEachFaultOnceAndInOrder(t *testing.T) {
	declaration := `{
		"c": {"type": "decimal", "constant": "1e3", "fromFact": "/x"},
		"b": {"unit": "CAD", "type": "money", "constant": "1"},
		"A": {"type": "string"}
	}`
	pack := valuesPack{
		outcomes: "[" + plain("decline") + ", " + declaring("approve", declaration) + ", " + declaring("later", `{}`) + "]",
		required: "-",
	}
	document, failure := carrier.Decode(pack.bytes(), carrier.DefaultLimits())
	if failure != nil {
		t.Fatal(failure.Diagnostic.Message)
	}
	got := []string{}
	for _, diagnostic := range rfc0016Diagnostics(document.(map[string]any)) {
		got = append(got, diagnostic.Code+" "+diagnostic.InstancePath)
	}
	first := "/outcomes/1/extensions/" + OutcomeValuesExtension
	want := []string{
		"JPS-EVALUATION-RFC0016-DECLARATION " + first + "/A",
		"JPS-EVALUATION-RFC0016-DECLARATION " + first + "/A",
		"JPS-EVALUATION-RFC0016-DECLARATION " + first + "/b/unit",
		"JPS-EVALUATION-RFC0016-DECLARATION " + first + "/b/type",
		"JPS-EVALUATION-RFC0016-DECLARATION " + first + "/c",
		"JPS-EVALUATION-RFC0016-DECLARATION " + first + "/c/constant",
		"JPS-EVALUATION-RFC0016-DECLARATION /outcomes/2/extensions/" + OutcomeValuesExtension,
		"JPS-EVALUATION-RFC0016-REQUIRED /metadata/requiredExtensions",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostics =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// A pack the gate admits reports nothing.
	admitted, failure := carrier.Decode(valuesPack{}.bytes(), carrier.DefaultLimits())
	if failure != nil {
		t.Fatal(failure.Diagnostic.Message)
	}
	if found := rfc0016Diagnostics(admitted.(map[string]any)); len(found) != 0 {
		t.Fatalf("diagnostics = %+v", found)
	}
}
