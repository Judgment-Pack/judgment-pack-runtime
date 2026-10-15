package mcp

import "testing"

func TestArtifactVersionToolArgumentsAndDiscovery(t *testing.T) {
	tools := runServer(t, message(t, 1, "tools/list", nil))[0]["result"].(map[string]any)["tools"].([]any)
	found := false
	for _, item := range tools {
		tool := item.(map[string]any)
		if tool["name"] != "get_artifact_version_options" {
			continue
		}
		found = true
		a := tool["annotations"].(map[string]any)
		if a["readOnlyHint"] != true || a["openWorldHint"] != false || a["destructiveHint"] != false {
			t.Fatal(a)
		}
	}
	if !found {
		t.Fatal("missing tool")
	}
	for _, args := range []string{`{}`, `null`, `[]`, `{"base":null}`, `{"base":2}`, `{"Base":"1.0.0"}`, `{"base":"1.0.0","base":"2.0.0"}`, `{"base":"1.0.0","candidate":null}`, `{"base":"1.0.0","candidate":2}`, `{"base":"1.0.0","save":true}`, `{"base":"1.0.0","candidate":"0.1"}`} {
		r := runServer(t, rawToolCall(t, 1, "get_artifact_version_options", args))[0]["result"].(map[string]any)
		if r["isError"] != true {
			t.Fatal("accepted", args, r)
		}
	}
	r := runServer(t, rawToolCall(t, 1, "get_artifact_version_options", `{"base":"0.1.0","candidate":"0.2.0"}`))[0]["result"].(map[string]any)
	if r["isError"] == true {
		t.Fatal(r)
	}
	report := r["structuredContent"].(map[string]any)
	if report["comparison"] != "greater" || report["compatibilityChecked"] != false {
		t.Fatal(report)
	}
}
