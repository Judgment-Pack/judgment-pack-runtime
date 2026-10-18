package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDuplicateUnknownArgumentNamesAreNeverEchoed(t *testing.T) {
	secret := "private-source-prose-and-retained-base64-marker"
	quoted, err := json.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	args := `{` + string(quoted) + `:null,` + string(quoted) + `:null}`
	// The shared guard must protect every registered tool before dispatch,
	// including tools which would otherwise access a project or evaluator.
	for _, definition := range toolDefinitions() {
		name := definition["name"].(string)
		t.Run(name, func(t *testing.T) {
			lines := serveLines(t, rawToolCall(t, 1, name, args))
			if len(lines) != 1 || len(lines[0]) > 1024 || strings.Contains(lines[0], secret) {
				t.Fatal("duplicate refusal disclosed rejected content or exceeded its bound")
			}
			var response struct {
				Result struct {
					IsError bool `json:"isError"`
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"result"`
			}
			if err := json.Unmarshal([]byte(lines[0]), &response); err != nil {
				t.Fatal(err)
			}
			if !response.Result.IsError || len(response.Result.Content) != 1 || !strings.Contains(response.Result.Content[0].Text, "carries a repeated member") {
				t.Fatal(lines[0])
			}
		})
	}
}

func TestLargeDuplicateNamesProduceBoundedRefusalsThroughToolsCall(t *testing.T) {
	secret := "private-retained-material-do-not-echo-"
	key := secret + strings.Repeat("c2VjcmV0LXJldGFpbmVkLWJ5dGVz", 32768)
	quoted, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	member := string(quoted) + `:null,` + string(quoted) + `:null`
	for _, name := range []string{"experimental_policy_check", "validate", "get_schema", key} {
		line := rawToolCall(t, 1, name, `{`+member+`}`)
		if len(line) >= maxMessageBytes {
			t.Fatal("test must reach the argument guard within the unchanged request bound")
		}
		lines := serveLines(t, line)
		if len(lines) != 1 || len(lines[0]) > 1024 || strings.Contains(lines[0], secret) || strings.Contains(lines[0], "c2VjcmV0LXJldGFpbmVkLWJ5dGVz") {
			t.Fatal("large duplicate name or unregistered tool name was echoed")
		}
		var response struct {
			Result struct {
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(lines[0]), &response); err != nil || !response.Result.IsError {
			t.Fatal(err, lines[0])
		}
	}
	for _, line := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"experimental_policy_check","arguments":{}},` + member + `}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"experimental_policy_check","arguments":{},` + member + `}}`,
	} {
		lines := serveLines(t, line+"\n")
		if len(lines) != 1 || len(lines[0]) > 1024 || strings.Contains(lines[0], secret) {
			t.Fatal("envelope refusal disclosed rejected content or exceeded its bound")
		}
		var response struct {
			Error *rpcError `json:"error"`
		}
		if err := json.Unmarshal([]byte(lines[0]), &response); err != nil || response.Error == nil {
			t.Fatal(err, lines[0])
		}
	}
}

func TestKnownDuplicateAndCaseArgumentRefusalsRemainActionable(t *testing.T) {
	for _, tc := range []struct{ name, args, want, absent string }{
		{"validate", `{"document":"{}","document":"[]"}`, `the member "document" twice`, ""},
		{"experimental_policy_check", `{"materials":"{}","materials":"[]"}`, `the member "materials" twice`, ""},
		{"validate", `{"DOCUMENT":"{}"}`, `use "document" spelled exactly`, "DOCUMENT"},
		{"experimental_policy_check", `{"MATERIALS":"{}"}`, `use "materials" spelled exactly`, "MATERIALS"},
	} {
		response := runServer(t, rawToolCall(t, 1, tc.name, tc.args))[0]["result"].(map[string]any)
		message := toolText(t, response)
		if response["isError"] != true || !strings.Contains(message, tc.want) || tc.absent != "" && strings.Contains(message, tc.absent) {
			t.Fatal(tc, message)
		}
	}
}
