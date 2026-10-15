package mcp

import (
	"strings"
	"testing"
)

func TestDialectToolDiscoveryAndStrictArguments(t *testing.T) {
	tools := runServer(t, message(t, 1, "tools/list", nil))[0]["result"].(map[string]any)["tools"].([]any)
	found := 0
	for _, item := range tools {
		tool := item.(map[string]any)
		name := tool["name"].(string)
		if strings.HasPrefix(name, "dialect_") || strings.HasPrefix(name, "get_dialect_") {
			found++
			if tool["annotations"].(map[string]any)["readOnlyHint"] != true {
				t.Fatal(tool)
			}
		}
	}
	if found != 8 {
		t.Fatal(found)
	}
	for _, tc := range []struct{ name, args string }{
		{"dialect_init", `{"version":null}`}, {"dialect_init", `{"version":2.0}`}, {"dialect_init", `{"version":2,"version":3}`}, {"dialect_init", `{"Version":2}`}, {"dialect_init", `[]`}, {"dialect_init", `null`},
		{"dialect_digest", `{}`}, {"dialect_digest", `{"document":null}`}, {"dialect_digest", `{"document":{}}`}, {"dialect_digest", `{"document":"{}","path":"secret"}`},
		{"dialect_digest", `{"Document":"{}"}`}, {"dialect_digest", `{"document":"{}","document":"{}"}`},
		{"get_dialect_contract", `{"version":2}`},
		{"get_dialect_schema", `{"version":null}`}, {"get_dialect_schema", `{"version":2.0}`}, {"get_dialect_schema", `{"Version":2}`}, {"get_dialect_schema", `{"version":2,"version":3}`},
		{"get_dialect_reference", `{"path":"secret"}`},
		{"dialect_check", `{"document":"{}"}`}, {"dialect_check", `{"document":"{}","pack":null}`}, {"dialect_check", `{"document":"{}","pack":"{}","pack":"{}"}`},
		{"dialect_check", `{"document":"{}","Pack":"{}"}`}, {"dialect_check", `{"document":"{}","pack":"{}","pack_id":"secret"}`},
		{"dialect_validate", `{}`}, {"dialect_validate", `{"Document":"{}"}`}, {"dialect_validate", `{"document":"{}","document":"{}"}`}, {"dialect_validate", `{"document":null}`},
	} {
		r := runServer(t, rawToolCall(t, 1, tc.name, tc.args))[0]["result"].(map[string]any)
		if r["isError"] != true {
			t.Fatalf("accepted %s %s: %v", tc.name, tc.args, r)
		}
	}
	for _, args := range []string{"", `{}`, `{"version":3}`} {
		request := toolCall(t, 1, "dialect_init", nil)
		if args != "" {
			request = rawToolCall(t, 1, "dialect_init", args)
		}
		r := runServer(t, request)[0]["result"].(map[string]any)
		if r["isError"] == true {
			t.Fatal(args, r)
		}
		report := r["structuredContent"].(map[string]any)
		if report["fullDocumentConformance"] != false || report["status"] != "encoded" {
			t.Fatal(report)
		}
	}
}
