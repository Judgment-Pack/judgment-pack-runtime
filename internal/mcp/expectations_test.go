package mcp

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func expectationCall(t *testing.T, arguments any) map[string]any {
	t.Helper()
	response := runServer(t, message(t, 1, "tools/call", map[string]any{"name": expectationTool, "arguments": arguments}))[0]
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected tool response: %#v", response)
	}
	return result
}

func TestExpectationAdmissionContract(t *testing.T) {
	data, err := os.ReadFile("testdata/expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name  string
		Text  string
		Valid bool
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	texts := make([]string, len(fixtures))
	for i, fixture := range fixtures {
		texts[i] = fixture.Text
	}
	result := expectationCall(t, map[string]any{"spec_version": expectationSpec, "expectations": texts})
	if result["isError"] == true {
		t.Fatalf("invalid assertions are indexed findings: %#v", result)
	}
	report := result["structuredContent"].(map[string]any)
	if report["status"] != "invalid" || report["specVersion"] != expectationSpec {
		t.Fatal(report)
	}
	rows := report["results"].([]any)
	if len(rows) != len(fixtures) {
		t.Fatalf("a case was lost: %d", len(rows))
	}
	for i, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			row := rows[i].(map[string]any)
			if row["index"] != float64(i) || (row["status"] == "valid") != fixture.Valid {
				t.Fatal(row)
			}
			if fixture.Valid {
				canonical, ok := row["canonical"].(string)
				if !ok || !json.Valid([]byte(canonical)) {
					t.Fatalf("valid expectation has no canonical form: %#v", row)
				}
			} else {
				message, ok := row["message"].(string)
				if !ok || message == "" || row["code"] != "JPS-EXPECTATION-INVALID" {
					t.Fatal(row)
				}
			}
		})
	}
	// Asking only about valid inputs is a valid report, independent of any project.
	only := expectationCall(t, map[string]any{"spec_version": expectationSpec, "expectations": texts[:5]})
	if only["structuredContent"].(map[string]any)["status"] != "valid" {
		t.Fatal(only)
	}
}

func TestExpectationAdmissionArgumentsAndLimits(t *testing.T) {
	for _, args := range []map[string]any{
		{}, {"spec_version": "0.1.0-draft", "expectations": []string{"{}"}},
		{"spec_version": expectationSpec, "expectations": []string{}},
		{"spec_version": expectationSpec, "expectations": make([]string, maxExpectations+1)},
		{"spec_version": expectationSpec, "expectations": []any{nil}},
		{"spec_version": expectationSpec, "expectations": []any{map[string]any{}}},
		{"spec_version": expectationSpec, "expectations": []string{"{}"}, "pack": "any"},
	} {
		if result := expectationCall(t, args); result["isError"] != true {
			t.Fatalf("bad arguments accepted: %#v", result)
		}
	}
	for _, text := range []string{strings.Repeat(" ", maxExpectationBytes+1), `"` + strings.Repeat("x", 8193) + `"`, strings.Repeat("[", 18) + "0" + strings.Repeat("]", 18), "[" + strings.Repeat("0,", 1024) + "0]"} {
		result := expectationCall(t, map[string]any{"spec_version": expectationSpec, "expectations": []string{text}})
		row := result["structuredContent"].(map[string]any)["results"].([]any)[0].(map[string]any)
		if row["code"] != "JPS-EXPECTATION-LIMIT" {
			t.Fatal(row)
		}
	}
}
