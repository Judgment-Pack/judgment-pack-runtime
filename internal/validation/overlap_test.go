package validation

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
)

func TestRulesOverlapHandAuthoredCases(t *testing.T) {
	data, err := os.ReadFile("testdata/rules-overlap-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	value, failure := carrier.Decode(data, carrier.DefaultLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	for _, raw := range value.([]any) {
		c := raw.(map[string]any)
		t.Run(c["name"].(string), func(t *testing.T) {
			root := map[string]any{"rules": []any{map[string]any{"id": "lower", "outcome": "low", "when": c["a"]}, map[string]any{"id": "higher", "outcome": "high", "when": c["b"]}}}
			got, truncated := ruleOverlapDiagnostics(root)
			want := c["certainty"].(string)
			if truncated {
				t.Fatal("unexpected truncation")
			}
			if want == "" {
				if len(got) != 0 {
					t.Fatalf("expected disjoint: %+v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Certainty != want || got[0].Severity != "warning" || got[0].InstancePath != "/rules/0/when" || got[0].Effect != "Core §8 records conflict; the disposition is unresolved." {
				t.Fatalf("want %s: %+v", want, got)
			}
			if got[0].SuggestedRewrite == nil || len(got[0].Rules) != 2 {
				t.Fatalf("incomplete diagnostic: %+v", got[0])
			}
			if want == "exact" {
				if len(got[0].WitnessFacts) == 0 {
					t.Fatal("exact result needs a witness")
				}
				// The production evaluator independently checks these witnesses in its
				// own package; here retain strict carrier identity, including null.
				if _, failure := carrier.Decode(got[0].WitnessFacts, carrier.DefaultLimits()); failure != nil {
					t.Fatal(failure)
				}
			} else if len(got[0].WitnessFacts) != 0 || got[0].Overlap != nil {
				t.Fatal("uncertainty has an exact region")
			}
		})
	}
}

func TestRulesOverlapValidationAndSameOutcome(t *testing.T) {
	data, err := os.ReadFile("testdata/rules-overlap-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, through := range []string{"carrier", "structural", "semantic"} {
		got, failure := engine.Validate(data, Options{Through: through})
		if failure != nil || got.Status != "valid" {
			t.Fatalf("%s: %+v %v", through, got, failure)
		}
		want := 0
		if through == "semantic" {
			want = 1
		}
		if len(got.Diagnostics) != want {
			t.Fatalf("%s: %+v", through, got.Diagnostics)
		}
	}
	same := strings.Replace(string(data), `"outcome": "high"`, `"outcome": "medium"`, 1)
	got, failure := engine.Validate([]byte(same), Options{})
	if failure != nil || got.Status != "valid" || len(got.Diagnostics) != 0 {
		t.Fatalf("same outcome: %+v %v", got, failure)
	}
	encoded, _ := json.Marshal(got)
	if !strings.Contains(string(encoded), `"diagnostics":[]`) {
		t.Fatalf("empty diagnostic array changed: %s", encoded)
	}

	for _, version := range []string{"0.1.0-draft", "0.2.0-draft"} {
		legacy := strings.Replace(string(data), `"0.3.0"`, `"`+version+`"`, 1)
		got, failure := engine.Validate([]byte(legacy), Options{})
		if failure != nil || got.Status != "valid" || len(got.Diagnostics) != 0 {
			t.Fatalf("legacy changed: %+v %v", got, failure)
		}
	}
}

func TestRulesOverlapBoundedReasoningAndOutput(t *testing.T) {
	fact := map[string]any{"op": "fact", "path": "/x", "operator": "equals", "value": "a"}
	anyNode := map[string]any{"op": "any", "conditions": []any{fact, fact}}
	conditions := []any{}
	for i := 0; i < 9; i++ {
		conditions = append(conditions, anyNode)
	}
	root := map[string]any{"rules": []any{map[string]any{"id": "a", "outcome": "a", "when": map[string]any{"op": "all", "conditions": conditions}}, map[string]any{"id": "b", "outcome": "b", "when": fact}}}
	got, truncated := ruleOverlapDiagnostics(root)
	if truncated || len(got) != 1 || got[0].Certainty != "not-excluded" {
		t.Fatalf("bounded DNF must not claim exact: %+v %v", got, truncated)
	}
	budget := &overlapBudget{0}
	possible, exact, _, _ := overlapPair(overlapFormula{}, overlapFormula{}, budget)
	if !possible || exact {
		t.Fatal("exhausted analysis must retain uncertainty")
	}
	rules := []any{}
	for i := 0; i < 16; i++ {
		rules = append(rules, map[string]any{"id": strings.Repeat("a", i+1), "outcome": strings.Repeat("a", i+1), "when": fact})
	}
	root["rules"] = rules
	got, truncated = ruleOverlapDiagnostics(root)
	if !truncated || len(got) != MaxDiagnostics {
		t.Fatalf("diagnostic bound: %d %v", len(got), truncated)
	}
}

func TestRulesOverlapOutputByteBoundAndFiniteRegion(t *testing.T) {
	fact := map[string]any{"op": "fact", "path": "/x", "operator": "equals", "value": "a"}
	root := map[string]any{"rules": []any{map[string]any{"id": "a", "outcome": "a", "when": fact}, map[string]any{"id": "b", "outcome": "b", "when": fact}}}
	got, truncated := ruleOverlapDiagnostics(root)
	if truncated || len(got) != 1 || got[0].Overlap == nil || len(got[0].Overlap.SharedValues) != 1 || got[0].Overlap.SharedValues[0] != "a" {
		t.Fatalf("finite region: %+v", got)
	}
	// Long labels exercise the report bound without generating the fixture or
	// bypassing a reasoning limit with a large comparison operand.
	root["rules"].([]any)[0].(map[string]any)["id"] = strings.Repeat("a", overlapOutputLimit)
	got, truncated = ruleOverlapDiagnostics(root)
	if !truncated || len(got) != 0 {
		t.Fatalf("oversized diagnostic emitted: %d %v", len(got), truncated)
	}
}

func TestRulesOverlapNumericRewriteSelectsLowerBand(t *testing.T) {
	low := map[string]any{"id": "low", "when": map[string]any{"op": "fact", "path": "/x", "operator": "greater-than-or-equal", "value": "40"}}
	high := map[string]any{"id": "high", "when": map[string]any{"op": "fact", "path": "/x", "operator": "greater-than", "value": "70"}}
	rewrite := exclusiveRewrite(high, low)
	encoded, _ := json.Marshal(rewrite)
	if rewrite.RuleID != "low" || !strings.Contains(string(encoded), `"operator":"less-than-or-equal"`) {
		t.Fatalf("wrong lower band: %s", encoded)
	}
}

func TestRulesOverlapChargesGeneratedDecimalSpellingBytes(t *testing.T) {
	terms := overlapClause{{node: map[string]any{"op": "fact", "path": "/x", "operator": "greater-than", "value": "0.000001"}, truth: true}, {node: map[string]any{"op": "fact", "path": "/x", "operator": "less-than", "value": "1"}, truth: true}}
	// The operands fit; generated decimal point/interval spellings exhaust the
	// remaining bytes. No partial partition may claim exclusion or exactness.
	_, complete := overlapValues(terms, &overlapBudget{35})
	if complete {
		t.Fatal("generated decimal bytes bypassed the analysis budget")
	}
}

// Under the staged draft selector the advice reads the evaluation form, so a
// rewrite built there inlines a set's values. A pair where either condition as
// written names a value set gets no rewrite; any other pair gets one as before.
func TestRulesOverlapStagedValueSetReferenceOffersNoRewrite(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	pack := func(inWhen, eqWhen, valueSets string) []byte {
		return []byte(`{
  "specVersion": "0.4.0-draft",
  "id": "https://example.com/packs/staged-overlap",
  "version": "1.0.0",
  "title": "Overlapping tiers",
  "decision": {"intent": "Assign a tier", "question": "Which tier applies?"},
  "outcomes": [{"id": "standard", "label": "Standard"}, {"id": "premium", "label": "Premium"}],
  "rules": [
    {"id": "r-in", "description": "Listed tiers", "outcome": "standard", "onUnknown": "escalate", "when": ` + inWhen + `},
    {"id": "r-eq", "description": "Gold tier", "outcome": "premium", "onUnknown": "escalate", "when": ` + eqWhen + `}
  ]` + valueSets + `
}`)
	}
	const tiers = `,
  "valueSets": [{"id": "tiers", "entries": [{"value": "gold"}, {"value": "silver"}]}]`
	const inReference = `{"op": "fact", "path": "/tier", "operator": "in", "valueSet": "tiers"}`
	const inInline = `{"op": "fact", "path": "/tier", "operator": "in", "value": ["gold", "silver"]}`
	const equalsGold = `{"op": "fact", "path": "/tier", "operator": "equals", "value": "gold"}`
	const head = `Rules "r-in" (standard) and "r-eq" (premium): overlap exact. Core §8 records conflict; the disposition is unresolved. `
	const noRewrite = "No exclusive rewrite is suggested because a condition names a value set."
	const rewrite = `{"ruleId":"r-in","when":{"conditions":[{"op":"fact","operator":"in","path":"/tier","value":["gold","silver"]},{"condition":{"op":"fact","operator":"equals","path":"/tier","value":"gold"},"op":"not"}],"op":"all"}}`

	for _, tc := range []struct {
		name, message, rewrite, unavailable string
		pack                                []byte
	}{
		{"reference in the first rule", head + noRewrite, "", noRewrite, pack(inReference, equalsGold, tiers)},
		{"reference nested in the second rule", head + noRewrite, "", noRewrite, pack(equalsGold, `{"op": "any", "conditions": [`+inReference+`]}`, tiers)},
		{"no reference", head + "Suggested exclusive rewrite: " + rewrite, rewrite, "", pack(inInline, equalsGold, "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, failure := engine.Validate(tc.pack, Options{})
			if failure != nil || got.Status != "valid" || len(got.Diagnostics) != 1 {
				t.Fatalf("%+v %v", got, failure)
			}
			d := got.Diagnostics[0]
			if d.Code != "JPS-RULES-OVERLAP" || d.Layer != "advisory" || d.Severity != "warning" || d.InstancePath != "/rules/0/when" || d.Certainty != "exact" {
				t.Fatalf("advisory changed: %+v", d)
			}
			if d.Message != tc.message {
				t.Fatalf("message:\n got %s\nwant %s", d.Message, tc.message)
			}
			if d.RewriteUnavailable != tc.unavailable {
				t.Fatalf("rewriteUnavailable: %q", d.RewriteUnavailable)
			}
			if tc.rewrite == "" {
				if d.SuggestedRewrite != nil {
					t.Fatalf("rewrite offered over a value-set reference: %+v", d.SuggestedRewrite)
				}
				return
			}
			encoded, _ := json.Marshal(d.SuggestedRewrite)
			if string(encoded) != tc.rewrite {
				t.Fatalf("rewrite: %s", encoded)
			}
		})
	}
}
