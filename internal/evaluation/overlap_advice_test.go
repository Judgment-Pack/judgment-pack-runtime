package evaluation

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// The independent evaluator checks exact witnesses from hand-authored cases;
// no evaluation code or disposition contract is changed by this advice.
func TestRulesOverlapExactWitnessesAgreeWithEvaluator(t *testing.T) {
	data, err := os.ReadFile("../validation/testdata/rules-overlap-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	cases, failure := carrier.Decode(data, carrier.DefaultLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	base, err := os.ReadFile("../validation/testdata/rules-overlap-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range cases.([]any) {
		c := raw.(map[string]any)
		if c["certainty"] != "exact" {
			continue
		}
		t.Run(c["name"].(string), func(t *testing.T) {
			root, _ := carrier.Decode(base, carrier.DefaultLimits())
			pack := root.(map[string]any)
			rules := pack["rules"].([]any)
			rules[0].(map[string]any)["when"] = c["a"]
			rules[1].(map[string]any)["when"] = c["b"]
			encoded, _ := json.Marshal(pack)
			report, operational := validator.Validate(encoded, validation.Options{})
			if operational != nil || report.Status != "valid" || len(report.Diagnostics) != 1 || report.Diagnostics[0].Certainty != "exact" {
				t.Fatalf("advice: %+v %v", report, operational)
			}
			facts, failure := carrier.Decode(report.Diagnostics[0].WitnessFacts, carrier.DefaultLimits())
			if failure != nil {
				t.Fatal(failure)
			}
			for _, key := range []string{"a", "b"} {
				if actual := evalCondition(c[key], facts, nil); actual != triTrue {
					t.Fatalf("%s at witness %s = %v", key, report.Diagnostics[0].WitnessFacts, actual)
				}
			}
		})
	}
}

func TestRulesOverlapRewritePreservesUnknownAndEvaluation(t *testing.T) {
	data, err := os.ReadFile("../validation/testdata/rules-overlap-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(validator)
	before, failure := engine.EvaluateWith(data, []byte(`{"score":"90"}`), nil, Options{SpecVersion: "0.3.0"})
	if failure != nil || before.Disposition.Kind != "unresolved" || len(before.Disposition.Reasons) != 1 || before.Disposition.Reasons[0] != "conflict" {
		t.Fatalf("original disposition changed: %+v %v", before.Disposition, failure)
	}
	report, _ := validator.Validate(data, validation.Options{})
	root, _ := carrier.Decode(data, carrier.DefaultLimits())
	pack := root.(map[string]any)
	rules := pack["rules"].([]any)
	rules[0].(map[string]any)["when"] = report.Diagnostics[0].SuggestedRewrite.When
	rewritten, _ := json.Marshal(pack)
	after, failure := engine.EvaluateWith(rewritten, []byte(`{"score":"90"}`), nil, Options{SpecVersion: "0.3.0"})
	if failure != nil || after.Disposition.Kind != "outcome" || after.Disposition.OutcomeID != "high" {
		t.Fatalf("rewrite: %+v %v", after.Disposition, failure)
	}
	rules[0].(map[string]any)["when"] = map[string]any{"op": "fact", "path": "/score", "operator": "greater-than-or-equal", "value": "40"}
	rules[1].(map[string]any)["when"] = map[string]any{"op": "fact", "path": "/priority", "operator": "greater-than-or-equal", "value": "70"}
	cross, _ := json.Marshal(pack)
	report, _ = validator.Validate(cross, validation.Options{})
	rules[0].(map[string]any)["when"] = report.Diagnostics[0].SuggestedRewrite.When
	for _, facts := range []map[string]any{{"score": "50"}, {"score": "50", "priority": nil}, {"score": "50", "priority": true}} {
		if got := evalCondition(rules[0].(map[string]any)["when"], facts, nil); got != triUnknown {
			t.Fatalf("cross-path rewrite silently lowered missing/nondecimal facts: %v", got)
		}
	}
	rewritten, _ = json.Marshal(pack)
	after, failure = engine.EvaluateWith(rewritten, []byte(`{"score":"50"}`), nil, Options{SpecVersion: "0.3.0"})
	if failure != nil || after.Disposition.Kind != "unresolved" {
		t.Fatalf("missing higher fact: %+v %v", after.Disposition, failure)
	}
}
