package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func TestRulesOverlapExpectedAdviceIsCheckedAndRuleIDsAreDistinct(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	root := writeLocalSuiteVersion(t, artifacts.CoreVersion)
	path := filepath.Join(root, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	cases := manifest["cases"].([]any)
	var advice map[string]any
	for _, raw := range cases {
		c := raw.(map[string]any)
		if c["id"] == "advisory-rules-overlap-exact" {
			advice = c["expectedAdvisories"].([]any)[0].(map[string]any)
		}
	}
	advice["overlap"].(map[string]any)["from"] = "71"
	encoded, _ := json.Marshal(manifest)
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	report, failure := NewRunner(engine).Run(root, artifacts.CoreVersion)
	if failure != nil || report.Status != "mismatch" || report.Advisories.Mismatched != 1 || report.Summary.Mismatched != 1 {
		t.Fatalf("wrong expected region passed: %+v %v", report, failure)
	}
	identities := advice["rules"].([]any)
	identities[1].(map[string]any)["id"] = identities[0].(map[string]any)["id"]
	encoded, _ = json.Marshal(manifest)
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	_, failure = NewRunner(engine).Run(root, artifacts.CoreVersion)
	if failure == nil || failure.Code != "JPS-SUITE-MANIFEST" || failure.Message != "An overlap advisory requires two distinct rule ids." {
		t.Fatalf("duplicate rule id accepted: %v", failure)
	}
}

func TestRulesOverlapSuiteByteBoundRetainsWholeDiagnostics(t *testing.T) {
	d := result.ErrorDiagnostic("JPS-RULES-OVERLAP", "advisory", "/rules/0/when", "complete rewrite")
	d.OverlapAdvice = &result.OverlapAdvice{Certainty: "exact", SuggestedRewrite: &result.OverlapRewrite{RuleID: "a", When: map[string]any{"op": "literal", "value": false}}}
	encoded, _ := json.Marshal(d)
	remaining := len(encoded)
	got, truncated := retainDiagnosticBytes([]result.Diagnostic{d, d}, &remaining)
	if len(got) != 1 || !truncated || remaining != 0 || got[0].SuggestedRewrite == nil {
		t.Fatalf("partial/oversized diagnostic: %+v %v %d", got, truncated, remaining)
	}
}

func TestRulesOverlapAdvisorySubsetDistinguishesNullFromMissing(t *testing.T) {
	want := map[string]any{"value": nil}
	if advisorySubset(want, map[string]any{}) || !advisorySubset(want, map[string]any{"value": nil}) {
		t.Fatal("null and missing expectation members collapsed")
	}
}
