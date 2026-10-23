package mcp

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRulesOverlapMCPValidateBothSerializations(t *testing.T) {
	data, err := os.ReadFile("../validation/testdata/rules-overlap-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, cross := range []bool{false, true} {
		document := string(data)
		certainty := "exact"
		if cross {
			document = strings.Replace(document, `"path": "/score"`, `"path": "/priority"`, 1)
			certainty = "not-excluded"
		}
		report := core030Both(t, toolCall(t, 1, "validate", map[string]any{"document": document}))
		if report["status"] != "valid" {
			t.Fatal(report)
		}
		diagnostics := report["diagnostics"].([]any)
		if len(diagnostics) != 1 {
			t.Fatal(report)
		}
		d := diagnostics[0].(map[string]any)
		if d["code"] != "JPS-RULES-OVERLAP" || d["severity"] != "warning" || d["certainty"] != certainty || d["instancePath"] != "/rules/0/when" || d["suggestedRewrite"] == nil {
			t.Fatal(d)
		}
	}
	report := core030Both(t, toolCall(t, 1, "test_conformance", map[string]any{"spec_version": "0.3.0"}))
	if report["status"] != "valid" || report["advisories"].(map[string]any)["passed"] != float64(5) {
		t.Fatal(report)
	}
}

func TestRulesOverlapMCPReportsBoundedRewriteUnavailable(t *testing.T) {
	data, err := os.ReadFile("../validation/testdata/rules-overlap-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	var pack map[string]any
	if err := json.Unmarshal(data, &pack); err != nil {
		t.Fatal(err)
	}
	literal := map[string]any{"op": "literal", "value": true}
	var deep any = literal
	for i := 0; i < 124; i++ {
		deep = map[string]any{"op": "not", "condition": deep}
	}
	rules := pack["rules"].([]any)
	rules[0].(map[string]any)["when"] = literal
	rules[1].(map[string]any)["when"] = deep
	encoded, _ := json.Marshal(pack)
	report := core030Both(t, toolCall(t, 1, "validate", map[string]any{"document": string(encoded)}))
	if report["status"] != "valid" {
		t.Fatal(report)
	}
	d := report["diagnostics"].([]any)[0].(map[string]any)
	if d["certainty"] != "exact" || d["rewriteUnavailable"] == nil || d["suggestedRewrite"] != nil {
		t.Fatalf("unusable MCP replacement: %+v", d)
	}
}
