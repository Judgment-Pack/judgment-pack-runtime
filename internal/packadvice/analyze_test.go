package packadvice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func engine(t *testing.T) *evaluation.Engine {
	t.Helper()
	v, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	return evaluation.NewEngine(v)
}
func literal(b bool) map[string]any { return map[string]any{"op": "literal", "value": b} }
func fact(op string, value any) map[string]any {
	return map[string]any{"op": "fact", "path": "/x", "operator": op, "value": value}
}
func rule(id, outcome string, when any) map[string]any {
	return map[string]any{"id": id, "description": "Synthetic rule.", "when": when, "outcome": outcome, "onUnknown": "ignore"}
}
func exception(id, effect string, when any) map[string]any {
	return map[string]any{"id": id, "description": "Synthetic exception.", "when": when, "effect": effect, "onUnknown": "ignore"}
}
func pack(rules ...map[string]any) map[string]any {
	rs := []any{}
	for _, r := range rules {
		rs = append(rs, r)
	}
	return map[string]any{"specVersion": "0.3.0", "id": "https://example.com/advice", "version": "0.1.0", "title": "Synthetic advice pack", "decision": map[string]any{"intent": "Exercise diagnostics.", "question": "Which outcome?"}, "outcomes": []any{map[string]any{"id": "a", "label": "A"}, map[string]any{"id": "b", "label": "B"}}, "rules": rs}
}
func bytesOf(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
func input(facts any) map[string]any {
	return map[string]any{"facts": facts, "evidence": map[string]any{}}
}
func domainOf(inputs ...any) []byte {
	return bytesOf(map[string]any{"kind": "finite-cases", "inputs": inputs})
}
func effects(report map[string]any) []string {
	out := []string{}
	for _, raw := range report["findings"].([]any) {
		out = append(out, raw.(map[string]any)["effect"].(string))
	}
	slices.Sort(out)
	return out
}
func validateReport(t *testing.T, r map[string]any) {
	t.Helper()
	c := jsonschema.NewCompiler()
	schema, err := c.Compile("testdata/report.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var plain any
	decoder := json.NewDecoder(bytes.NewReader(bytesOf(r)))
	decoder.UseNumber()
	if err := decoder.Decode(&plain); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(plain); err != nil {
		t.Fatal(err, string(bytesOf(r)))
	}
	if !bounded(r) {
		t.Fatal("output bounds")
	}
}
func analyze(t *testing.T, p map[string]any, d []byte, o Options) map[string]any {
	t.Helper()
	r, f := Analyze(engine(t), bytesOf(p), d, o)
	if f != nil {
		t.Fatal(f, string(bytesOf(p)))
	}
	validateReport(t, r)
	return r
}
func has(t *testing.T, r map[string]any, effect string, want bool) {
	t.Helper()
	if slices.Contains(effects(r), effect) != want {
		t.Fatalf("%s=%v: %v", effect, want, effects(r))
	}
}

func TestPackAdviceCompatibleRedundancyAndConflictTwins(t *testing.T) {
	d := domainOf(input(map[string]any{"x": "70"}), input(map[string]any{"x": "40"}))
	p := pack(rule("low", "a", fact("greater-than-or-equal", "40")), rule("high", "a", fact("greater-than-or-equal", "70")))
	r := analyze(t, p, d, DefaultOptions())
	has(t, r, "compatible-agreement", true)
	has(t, r, "outcome-redundancy", true)
	for _, raw := range r["findings"].([]any) {
		f := raw.(map[string]any)
		if f["effect"] == "outcome-redundancy" {
			if f["sources"].([]any)[0].(map[string]any)["id"] != "high" || f["proof"].(map[string]any)["method"] != "exhaustive-domain" {
				t.Fatal(f)
			}
		}
	}
	p["rules"].([]any)[1].(map[string]any)["outcome"] = "b"
	r = analyze(t, p, d, DefaultOptions())
	has(t, r, "compatible-agreement", false)
	has(t, r, "outcome-redundancy", false)
	// True candidate agreement remains valid while a third outcome conflicts.
	p["rules"] = append(p["rules"].([]any), rule("also-low", "a", literal(true)))
	r = analyze(t, p, d, DefaultOptions())
	has(t, r, "compatible-agreement", true)
	ev, failure := engine(t).EvaluateWith(bytesOf(p), bytesOf(map[string]any{"x": "70"}), nil, evaluation.Options{SpecVersion: "0.3.0"})
	if failure != nil || ev.Disposition.Kind != "unresolved" || !slices.Contains(ev.Disposition.Reasons, "conflict") {
		t.Fatal(ev, failure)
	}
	// A sole contribution in a later case defeats universal redundancy.
	p = pack(rule("r", "a", literal(true)), rule("s", "a", fact("equals", true)))
	r = analyze(t, p, domainOf(input(map[string]any{"x": true}), input(map[string]any{"x": false})), DefaultOptions())
	count := 0
	for _, raw := range r["findings"].([]any) {
		if raw.(map[string]any)["effect"] == "outcome-redundancy" {
			count++
		}
	}
	if count != 1 {
		t.Fatal(r)
	}
}
func TestPackAdviceCoverageAndUnknownTwins(t *testing.T) {
	p := pack(rule("r", "a", literal(false)))
	d := domainOf(input(map[string]any{}))
	r := analyze(t, p, d, DefaultOptions())
	has(t, r, "no-match-reachable", true)
	has(t, r, "condition-never-true", true)
	has(t, r, "never-contributes", true)
	p["fallbackOutcome"] = "b"
	r = analyze(t, p, d, DefaultOptions())
	has(t, r, "fallback-reachable", true)
	has(t, r, "no-match-reachable", false)
	o := DefaultOptions()
	o.IntentionalFallback = true
	r = analyze(t, p, d, o)
	for _, raw := range r["findings"].([]any) {
		f := raw.(map[string]any)
		if f["effect"] == "fallback-reachable" && f["severity"] != "info" {
			t.Fatal(f)
		}
	}
	p = pack(rule("r", "a", fact("equals", nil)))
	p["fallbackOutcome"] = "b"
	p["rules"].([]any)[0].(map[string]any)["onUnknown"] = "escalate"
	r = analyze(t, p, d, o)
	has(t, r, "unknown-blocks-resolution", true)
	has(t, r, "fallback-reachable", false)
	r = analyze(t, p, domainOf(input(map[string]any{"x": nil})), o)
	has(t, r, "unknown-blocks-resolution", false)
	has(t, r, "never-contributes", false)
	p["rules"].([]any)[0].(map[string]any)["onUnknown"] = "ignore"
	r = analyze(t, p, d, o)
	has(t, r, "unknown-blocks-resolution", false)
	has(t, r, "fallback-reachable", true)
}
func TestPackAdviceGatesDoNotInventConditionTruth(t *testing.T) {
	p := pack(rule("r", "a", literal(true)))
	e := exception("s", "suppress-rule", literal(true))
	e["targetRule"] = "r"
	p["exceptions"] = []any{e}
	p["fallbackOutcome"] = "b"
	r := analyze(t, p, domainOf(input(nil)), DefaultOptions())
	has(t, r, "suppression", true)
	has(t, r, "condition-never-true", false)
	has(t, r, "never-contributes", true)
	if r["complete"] != false {
		t.Fatal(r)
	}
	e["when"] = literal(false)
	r = analyze(t, p, domainOf(input(nil)), DefaultOptions())
	has(t, r, "suppression", false)
	has(t, r, "never-contributes", false)
	if r["complete"] != true {
		t.Fatal(r)
	}
	e = exception("force", "force-outcome", literal(true))
	e["outcome"] = "a"
	p["exceptions"] = []any{e}
	r = analyze(t, p, domainOf(input(nil)), DefaultOptions())
	has(t, r, "force-bypasses-rules", true)
	has(t, r, "fallback-reachable", false)
	e2 := exception("force-other", "force-outcome", literal(true))
	e2["outcome"] = "b"
	p["exceptions"] = []any{e, e2}
	r = analyze(t, p, domainOf(input(nil)), DefaultOptions())
	has(t, r, "forced-conflict", true)
	has(t, r, "force-bypasses-rules", false)
	e2["outcome"] = "a"
	r = analyze(t, p, domainOf(input(nil)), DefaultOptions())
	has(t, r, "forced-conflict", false)
	has(t, r, "force-bypasses-rules", true)
	p["exceptions"] = []any{exception("review", "escalate", literal(true))}
	r = analyze(t, p, domainOf(input(nil)), DefaultOptions())
	has(t, r, "direct-escalation", true)
	p["applicability"] = literal(false)
	r = analyze(t, p, domainOf(input(nil)), DefaultOptions())
	has(t, r, "direct-escalation", false)
	has(t, r, "never-contributes", true)
	p["applicability"] = fact("equals", true)
	r = analyze(t, p, domainOf(input(nil)), DefaultOptions())
	has(t, r, "unknown-blocks-resolution", true)
	// One actual true condition disproves never-true even if another is skipped.
	delete(p, "applicability")
	e = exception("s", "suppress-rule", fact("equals", true))
	e["targetRule"] = "r"
	p["exceptions"] = []any{e}
	r = analyze(t, p, domainOf(input(map[string]any{"x": true}), input(map[string]any{"x": false})), DefaultOptions())
	if r["complete"] != true {
		t.Fatal(r)
	}
	has(t, r, "condition-never-true", false)
}
func TestPackAdviceUnknownEvidenceAndExceptionTwins(t *testing.T) {
	p := pack(rule("r", "a", literal(true)))
	p["evidenceRequirements"] = []any{map[string]any{"id": "e", "description": "Required synthetic evidence.", "required": true}}
	d := domainOf(input(nil))
	r := analyze(t, p, d, DefaultOptions())
	has(t, r, "unknown-blocks-resolution", true)
	has(t, r, "never-contributes", true)
	i := input(nil)
	i["evidence"] = map[string]any{"e": "present"}
	r = analyze(t, p, domainOf(i), DefaultOptions())
	has(t, r, "unknown-blocks-resolution", false)
	has(t, r, "never-contributes", false)
	i["evidence"] = map[string]any{"e": "absent"}
	r = analyze(t, p, domainOf(i), DefaultOptions())
	has(t, r, "unknown-blocks-resolution", false)
	delete(p, "evidenceRequirements")
	e := exception("review", "escalate", fact("equals", true))
	e["onUnknown"] = "escalate"
	p["exceptions"] = []any{e}
	r = analyze(t, p, d, DefaultOptions())
	has(t, r, "unknown-blocks-resolution", true)
	has(t, r, "direct-escalation", false)
	e["onUnknown"] = "ignore"
	r = analyze(t, p, d, DefaultOptions())
	has(t, r, "unknown-blocks-resolution", false)
}
func TestPackAdviceExactBoundaryMissingNullAndIncomparable(t *testing.T) {
	for _, row := range []struct {
		facts       any
		condition   any
		contributes bool
	}{
		{map[string]any{"x": "7.0"}, fact("greater-than-or-equal", "7"), true},
		{map[string]any{"x": "6.999999999999999999999999999999999999"}, fact("greater-than-or-equal", "7"), false},
		{map[string]any{"x": json.Number("7")}, fact("greater-than-or-equal", "7"), false},
		{map[string]any{"x": nil}, fact("equals", nil), true},
		{map[string]any{}, fact("equals", nil), false},
		{map[string]any{"x": json.Number("9007199254740993")}, fact("equals", json.Number("9007199254740992")), false},
		{map[string]any{"x": "7.0"}, fact("equals", "7"), false},
	} {
		r := analyze(t, pack(rule("r", "a", row.condition)), domainOf(input(row.facts)), DefaultOptions())
		has(t, r, "never-contributes", !row.contributes)
	}
}
func TestPackAdviceDigestEchoAndOrderIndependence(t *testing.T) {
	p := pack(rule("r", "a", literal(true)), rule("s", "a", literal(true)))
	d := []byte(` {"kind":"finite-cases", "inputs":[{"facts":{"n":9007199254740993,"z":1e10000},"evidence":{}}]} `)
	r := analyze(t, p, d, DefaultOptions())
	if r["domainDigest"] != digest(d) || r["packDigest"] != digest(bytesOf(p)) {
		t.Fatal(r)
	}
	echo := r["domain"].(map[string]any)["inputs"].([]any)[0].(map[string]any)["facts"].(map[string]any)
	if echo["n"] != json.Number("9007199254740993") || echo["z"] != json.Number("1e10000") {
		t.Fatal(echo)
	}
	q := pack(p["rules"].([]any)[1].(map[string]any), p["rules"].([]any)[0].(map[string]any))
	other := analyze(t, q, d, DefaultOptions())
	if !reflect.DeepEqual(effects(r), effects(other)) {
		t.Fatal(r, other)
	}
	r2 := analyze(t, p, []byte(strings.TrimSpace(string(d))), DefaultOptions())
	if r["domainDigest"] == r2["domainDigest"] {
		t.Fatal("hashed reserialization")
	}
}
func TestPackAdviceRefusals(t *testing.T) {
	p := bytesOf(pack(rule("r", "a", literal(true))))
	valid := domainOf(input(nil))
	e := engine(t)
	for _, row := range []struct {
		domain []byte
		code   string
	}{
		{nil, "domain-invalid"}, {[]byte(`{}`), "domain-invalid"}, {[]byte(`{"kind":"finite-cases","inputs":[]}`), "domain-invalid"},
		{[]byte(`{"kind":"finite-cases","kind":"finite-cases","inputs":[]}`), "domain-invalid"},
		{[]byte(`{"kind":"finite-cases","inputs":[{"facts":null}]}`), "domain-invalid"},
		{[]byte(`{"kind":"finite-cases","inputs":[{"facts":"\ud800","evidence":{}}]}`), "domain-invalid"},
		{[]byte(`{"kind":"finite-cases","inputs":[{"facts":null,"evidence":{"undeclared":"present"}}]}`), "domain-invalid"},
		{[]byte(strings.Repeat(" ", MaxBytes+1)), "domain-limit"},
		{[]byte(`{"kind":"finite-cases","inputs":[{"facts":` + strings.Repeat("1", 4097) + `,"evidence":{}}]}`), "domain-limit"},
		{domainOf(input(strings.Repeat("x", 65537))), "domain-limit"},
	} {
		r, f := Analyze(e, p, row.domain, DefaultOptions())
		if r != nil || f == nil || f.Code != row.code {
			t.Fatal(row.code, r, f)
		}
		if len(bytesOf(f)) > 512 {
			t.Fatal(f)
		}
	}
	for _, bad := range [][]byte{nil, []byte(`{}`), []byte(strings.Replace(string(p), "0.3.0", "0.4.0", 1)), []byte(strings.Replace(string(p), `"specVersion"`, `"profile":"unknown","specVersion"`, 1))} {
		r, f := Analyze(e, bad, valid, DefaultOptions())
		if r != nil || f == nil || f.Code != "pack-unsupported" {
			t.Fatal(r, f)
		}
	}
	over := pack()
	rs := []any{}
	for i := 0; i < 257; i++ {
		rs = append(rs, rule(fmt.Sprint("r", i), "a", literal(true)))
	}
	over["rules"] = rs
	r, f := Analyze(e, bytesOf(over), valid, DefaultOptions())
	if r != nil || f == nil || f.Code != "pack-unsupported" {
		t.Fatal(r, f)
	}
}
func TestPackAdviceWorkAndOutputExhaustion(t *testing.T) {
	p := pack(rule("r", "a", literal(false)))
	d := domainOf(input(nil))
	o := DefaultOptions()
	o.MaxWork = 1
	r := analyze(t, p, d, o)
	has(t, r, "analysis-incomplete", true)
	has(t, r, "never-contributes", false)
	if r["usage"].(map[string]any)["work"] != 1 || r["usage"].(map[string]any)["findingsTruncated"] != false {
		t.Fatal(r)
	}
	o = DefaultOptions()
	o.MaxFindings = 1
	r = analyze(t, p, d, o)
	if len(r["findings"].([]any)) != 1 || r["usage"].(map[string]any)["findingsTruncated"] != true {
		t.Fatal(r)
	}
	// A large witness fits once but repeated copies overflow the report ceiling.
	big := map[string]any{}
	for i := 0; i < 12; i++ {
		big[fmt.Sprint(i)] = strings.Repeat("x", 60000)
	}
	p = pack(rule("r", "a", literal(true)), rule("s", "a", literal(true)), rule("t", "a", literal(true)))
	r = analyze(t, p, domainOf(input(big)), DefaultOptions())
	has(t, r, "analysis-incomplete", true)
	if r["usage"].(map[string]any)["findingsTruncated"] != true {
		t.Fatal(r)
	}
	// The domain fits admission at depth 32 but cannot fit one level deeper in a report.
	nested := any(nil)
	for i := 0; i < 29; i++ {
		nested = []any{nested}
	}
	_, _, f := domain(domainOf(input(nested)))
	if f != nil {
		t.Fatal(f)
	}
	report, refusal := Analyze(engine(t), bytesOf(p), domainOf(input(nested)), DefaultOptions())
	if report != nil || refusal == nil || refusal.Code != "analysis-limit" {
		t.Fatal(report, refusal)
	}
}

func TestPackAdviceSchemaOraclePinned(t *testing.T) {
	b, err := os.ReadFile("testdata/report.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "RFC 0027") {
		t.Fatal("missing proposal identity")
	}
}

func TestPackAdviceAdmissionBoundaries(t *testing.T) {
	valid := domainOf(input(nil))
	padded := append(append([]byte{}, valid...), []byte(strings.Repeat(" ", MaxBytes-len(valid)))...)
	if _, _, f := domain(padded); f != nil {
		t.Fatal("exact byte ceiling", f)
	}
	if _, _, f := domain(append(padded, ' ')); f == nil || f.Code != "domain-limit" {
		t.Fatal(f)
	}
	for _, n := range []int{64, 65} {
		inputs := make([]any, n)
		for i := range inputs {
			inputs[i] = input(nil)
		}
		_, _, f := domain(domainOf(inputs...))
		if (f != nil) != (n == 65) {
			t.Fatal(n, f)
		}
	}
	for _, n := range []int{256, 257} {
		i := input(nil)
		e := map[string]any{}
		for k := 0; k < n; k++ {
			e[fmt.Sprint(k)] = "unknown"
		}
		i["evidence"] = e
		_, _, f := domain(domainOf(i))
		if (f != nil) != (n == 257) {
			t.Fatal(n, f)
		}
	}
	for _, n := range []int{16378, 16379} {
		values := make([]any, n)
		_, _, f := domain(domainOf(input(values)))
		if (f != nil) != (n == 16379) {
			t.Fatal(n, f)
		}
	}
	for _, n := range []int{65536, 65537} {
		_, _, f := domain(domainOf(input(strings.Repeat("x", n))))
		if (f != nil) != (n == 65537) {
			t.Fatal(n, f)
		}
	}
	for _, n := range []int{4096, 4097} {
		number := json.Number("1e" + strings.Repeat("9", n-1))
		_, _, f := domain(domainOf(input(number)))
		if (f != nil) != (n == 4097) {
			t.Fatal(n, f)
		}
	}
	for _, n := range []int{29, 30} {
		v := any(nil)
		for k := 0; k < n; k++ {
			v = []any{v}
		}
		_, _, f := domain(domainOf(input(v)))
		if (f != nil) != (n == 30) {
			t.Fatal(n, f)
		}
	}
	// IDs exceed the report's source representability capability, while the
	// underlying Core document remains conformant.
	p := pack(rule(strings.Repeat("r", 257), "a", literal(true)))
	e := engine(t)
	if !e.AdmitPack(bytesOf(p)).AdmitsWith(evaluation.Options{SpecVersion: "0.3.0"}) {
		t.Fatal("Core id validity changed")
	}
	r, f := Analyze(e, bytesOf(p), valid, DefaultOptions())
	if r != nil || f == nil || f.Code != "pack-unsupported" {
		t.Fatal(r, f)
	}
	p = pack(rule(strings.Repeat("r", 256), "a", literal(true)))
	analyze(t, p, valid, DefaultOptions())
	// Requirement count is bounded by pack admission, not by the rule/exception cap.
	req := []any{}
	for n := 0; n < 257; n++ {
		req = append(req, map[string]any{"id": fmt.Sprint("e", n), "description": "Optional.", "required": false})
	}
	p["evidenceRequirements"] = req
	analyze(t, p, valid, DefaultOptions())
}
func TestPackAdviceEvaluatorFailureRefusesWholeReport(t *testing.T) {
	// The real evaluator charges every in-candidate read of a 60 KB selected
	// value. A later admitted input exceeds its independent 20 MiB work budget.
	candidates := make([]any, 400)
	p := pack(rule("r", "a", fact("in", candidates)))
	d := domainOf(input(map[string]any{"x": "tiny"}), input(map[string]any{"x": strings.Repeat("x", 60000)}))
	r, f := Analyze(engine(t), bytesOf(p), d, DefaultOptions())
	if r != nil || f == nil || f.Code != "analysis-limit" {
		t.Fatal(r, f)
	}
}
func TestPackAdviceGateUnknownReasonDoesNotInventEvidenceBlock(t *testing.T) {
	p := pack(rule("r", "a", literal(true)))
	p["evidenceRequirements"] = []any{map[string]any{"id": "missing", "description": "Required.", "required": true}, map[string]any{"id": "unknown", "description": "Required.", "required": true}}
	e := exception("review", "escalate", fact("equals", true))
	e["onUnknown"] = "escalate"
	p["exceptions"] = []any{e}
	i := input(nil)
	i["evidence"] = map[string]any{"missing": "absent"}
	r := analyze(t, p, domainOf(i), DefaultOptions())
	for _, raw := range r["findings"].([]any) {
		f := raw.(map[string]any)
		if f["effect"] == "unknown-blocks-resolution" {
			for _, raw := range f["sources"].([]any) {
				if raw.(map[string]any)["id"] == "unknown" {
					t.Fatal("absent evidence takes precedence over evidence unknown", f)
				}
			}
		}
	}
}
func TestPackAdviceRawDomainBytesDifferFromReportEncoding(t *testing.T) {
	// UTF-8 admission counts the retained input, not a serialization with HTML
	// escapes. The echo can still prevent any bounded optional report.
	d := []byte(`{"kind":"finite-cases","inputs":[{"facts":{"a":"` + strings.Repeat("<", 60000) + `","b":"` + strings.Repeat("<", 60000) + `","c":"` + strings.Repeat("<", 60000) + `"},"evidence":{}}]}`)
	if _, _, f := domain(d); f != nil {
		t.Fatal(f)
	}
	r, f := Analyze(engine(t), bytesOf(pack(rule("r", "a", literal(true)))), d, DefaultOptions())
	if r != nil || f == nil || f.Code != "analysis-limit" {
		t.Fatal(r, f)
	}
}
func TestPackAdviceChargeBeforeEveryLogicalPredicate(t *testing.T) {
	// For two false rules and two false exceptions, the checks still cost:
	// evaluation 1, rule contribution/condition 4, directed redundancy pairs 2,
	// agreement pair 1, coverage 1, exception effects 2, exception pair 1,
	// applicability unknown 1, named-entity unknown 4 = 17.
	p := pack(rule("r", "a", literal(false)), rule("s", "a", literal(false)))
	e := exception("e", "force-outcome", literal(false))
	e["outcome"] = "a"
	f := exception("f", "force-outcome", literal(false))
	f["outcome"] = "b"
	p["exceptions"] = []any{e, f}
	d := domainOf(input(nil))
	o := DefaultOptions()
	o.MaxWork = 17
	r := analyze(t, p, d, o)
	if r["usage"].(map[string]any)["work"] != 17 || r["complete"] != true {
		t.Fatal(r)
	}
	o.MaxWork = 16
	r = analyze(t, p, d, o)
	has(t, r, "analysis-incomplete", true)
	has(t, r, "never-contributes", false)
	if r["usage"].(map[string]any)["work"] != 16 {
		t.Fatal(r)
	}
}
func TestPackAdviceForceGroupsAndRuleCeiling(t *testing.T) {
	p := pack()
	rules := []any{}
	for n := 0; n < 256; n++ {
		rules = append(rules, rule(fmt.Sprint("r", n), "a", literal(true)))
	}
	p["rules"] = rules
	e := exception("force", "force-outcome", literal(true))
	e["outcome"] = "a"
	p["exceptions"] = []any{e}
	r := analyze(t, p, domainOf(input(nil)), DefaultOptions())
	groups := 0
	total := 0
	for _, raw := range r["findings"].([]any) {
		f := raw.(map[string]any)
		if f["effect"] == "force-bypasses-rules" {
			groups++
			src := f["sources"].([]any)
			if len(src) > 256 {
				t.Fatal(src)
			}
			total += len(src) - 1
		}
	}
	if groups != 2 || total != 256 {
		t.Fatal(groups, total)
	}
	if r["usage"].(map[string]any)["work"] != 98693 {
		t.Fatal("cached false pair predicates must still pay", r["usage"])
	}
}
func TestPackAdviceUnsupportedRequiredExtensionAndExceptionOrder(t *testing.T) {
	p := pack(rule("r", "a", literal(false)))
	p["metadata"] = map[string]any{"requiredExtensions": []any{"com.example.custom"}}
	p["extensions"] = map[string]any{"com.example.custom": map[string]any{}}
	r, f := Analyze(engine(t), bytesOf(p), domainOf(input(nil)), DefaultOptions())
	if r != nil || f == nil || f.Code != "pack-unsupported" {
		t.Fatal(r, f)
	}
	delete(p, "metadata")
	delete(p, "extensions")
	e := exception("e", "force-outcome", literal(true))
	e["outcome"] = "a"
	other := exception("f", "force-outcome", literal(true))
	other["outcome"] = "b"
	p["exceptions"] = []any{e, other}
	first := analyze(t, p, domainOf(input(nil)), DefaultOptions())
	p["exceptions"] = []any{other, e}
	second := analyze(t, p, domainOf(input(nil)), DefaultOptions())
	if !reflect.DeepEqual(effects(first), effects(second)) {
		t.Fatal(first, second)
	}
}
