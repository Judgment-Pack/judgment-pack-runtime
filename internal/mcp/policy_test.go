package mcp

import (
	"strings"
	"testing"
)

func TestPolicyToolDiscoveryAndStrictArguments(t *testing.T) {
	tools := runServer(t, message(t, 1, "tools/list", nil))[0]["result"].(map[string]any)["tools"].([]any)
	found := map[string]bool{}
	for _, entry := range tools {
		tool := entry.(map[string]any)
		name := tool["name"].(string)
		if strings.HasPrefix(name, "experimental_policy_") || strings.HasPrefix(name, "experimental_get_policy_") {
			found[name] = true
			if tool["annotations"].(map[string]any)["readOnlyHint"] != true || tool["inputSchema"].(map[string]any)["additionalProperties"] != false {
				t.Fatal(tool)
			}
		}
	}
	for _, op := range []string{"contract", "init", "schema", "canonicalize", "digest", "validate", "check"} {
		if !found[policyToolName(op)] {
			t.Fatal(op, found)
		}
	}
	if len(found) != 7 {
		t.Fatal(found)
	}
	for _, tc := range []struct{ name, args string }{
		{"experimental_get_policy_contract", `null`}, {"experimental_get_policy_contract", `{"path":"secret"}`},
		{"experimental_policy_init", `{}`}, {"experimental_policy_init", `{"kind":"policy","formatVersion":"0.0.2","id":"urn:test","version":null}`},
		{"experimental_get_policy_schema", `{"kind":"policy","formatVersion":"0.0.2","kind":"governing-adoption"}`},
		{"experimental_get_policy_schema", `{"kind":"policy","FormatVersion":"0.0.2"}`},
		{"experimental_policy_validate", `{}`}, {"experimental_policy_validate", `{"document":null}`},
		{"experimental_policy_validate", `{"document":{}}`}, {"experimental_policy_validate", `{"Document":"{}"}`},
		{"experimental_policy_digest", `{"document":"{}","document":"{}"}`},
		{"experimental_policy_canonicalize", `{"document":"{}","path":"secret"}`},
		{"experimental_policy_check", `{"document":"{}"}`},
		{"experimental_policy_check", `{"document":"{}","materials":null}`},
		{"experimental_policy_check", `{"document":"{}","materials":{}}`},
		{"experimental_policy_check", `{"document":"{}","materials":"{}","url":"https://example.invalid"}`},
		{"experimental_policy_check", `[]`},
	} {
		r := runServer(t, rawToolCall(t, 1, tc.name, tc.args))[0]["result"].(map[string]any)
		if r["isError"] != true || r["structuredContent"] != nil {
			t.Fatal(tc, r)
		}
	}
	for _, op := range []string{"validate", "canonicalize", "digest", "check"} {
		args := map[string]any{"document": `{"secret":"must-not-echo"}`}
		if op == "check" {
			args["materials"] = `{"materials":[]}`
		}
		r := runServer(t, toolCall(t, 1, policyToolName(op), args))[0]["result"].(map[string]any)
		if r["isError"] != true || r["structuredContent"] == nil {
			t.Fatal(op, r)
		}
		if strings.Contains(r["content"].([]any)[0].(map[string]any)["text"].(string), "must-not-echo") {
			t.Fatal("plaintext echoed", r)
		}
	}
}

func TestPolicySelectorsAndContract(t *testing.T) {
	r := runServer(t, toolCall(t, 1, "experimental_get_policy_contract", nil))[0]["result"].(map[string]any)
	if r["isError"] == true || r["structuredContent"].(map[string]any)["experimental"] != true {
		t.Fatal(r)
	}
	for _, kind := range []string{"policy", "governing-adoption"} {
		r := runServer(t, toolCall(t, 1, "experimental_policy_init", map[string]any{"kind": kind, "formatVersion": "0.0.2", "id": "urn:test:policy", "version": "1.0.0"}))[0]["result"].(map[string]any)
		if r["isError"] == true || r["structuredContent"].(map[string]any)["status"] != "starter-template" {
			t.Fatal(r)
		}
	}
}

func TestPolicyWellTypedSelectorRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		code string
	}{
		{"experimental_get_policy_schema", map[string]any{"kind": "policy", "formatVersion": "0.0.3"}, "JPS-POLICY-SELECTOR"},
		{"experimental_get_policy_schema", map[string]any{"kind": "unknown", "formatVersion": "0.0.2"}, "JPS-POLICY-SELECTOR"},
		{"experimental_policy_init", map[string]any{"kind": "policy", "formatVersion": "0.0.2", "id": "relative", "version": "1.0.0"}, "JPS-POLICY-ID"},
		{"experimental_policy_init", map[string]any{"kind": "policy", "formatVersion": "0.0.2", "id": "urn:test", "version": "1.0"}, "JPS-POLICY-ARTIFACT-VERSION"},
		{"experimental_policy_init", map[string]any{"kind": "policy", "formatVersion": "0.0.2", "id": "urn:test", "version": ""}, "JPS-POLICY-ARTIFACT-VERSION"},
	} {
		r := runServer(t, toolCall(t, 1, tc.name, tc.args))[0]["result"].(map[string]any)
		report, ok := r["structuredContent"].(map[string]any)
		if r["isError"] != true || !ok || report["status"] != "refused" || report["capabilities"] == nil {
			t.Fatal(tc, r)
		}
		ds := report["diagnostics"].([]any)
		if len(ds) != 1 || ds[0].(map[string]any)["code"] != tc.code {
			t.Fatal(tc, r)
		}
	}
}

func TestPolicyCandidateDiscoveryStrictArguments(t *testing.T) {
	for _, args := range []string{
		`{"allFormats":true,"formatVersion":"1.0.0"}`, `{"allFormats":null}`, `{"allFormats":"true"}`,
		`{"AllFormats":true}`, `{"allFormats":true,"allFormats":true}`, `{"formatVersion":null}`,
		`{"formatVersion":false}`, `{"formatVersion":0}`,
	} {
		reply := runServer(t, rawToolCall(t, 1, "experimental_get_policy_contract", args))[0]["result"].(map[string]any)
		if reply["isError"] != true || reply["structuredContent"] != nil {
			t.Fatal(args, reply)
		}
	}
	for _, args := range []string{`{"document":"{}","formatVersion":null}`, `{"document":"{}","formatVersion":false}`, `{"document":"{}","formatVersion":0}`, `{"document":"{}","FormatVersion":"1.0.0"}`} {
		reply := runServer(t, rawToolCall(t, 1, "experimental_policy_validate", args))[0]["result"].(map[string]any)
		if reply["isError"] != true || reply["structuredContent"] != nil {
			t.Fatal(args, reply)
		}
	}
}
