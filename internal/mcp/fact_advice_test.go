package mcp

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestFactAdviceNeedsNoExpectationAndWritesNothing(t *testing.T) {
	pack, trail := comparableServerProject(t, `"configVersion":"5","requireComparableFacts":true`)
	for _, row := range []struct {
		facts    string
		findings int
	}{{`{"amount":{"value":0}}`, 1}, {`{"amount":{"value":"0"}}`, 0}, {`{"amount":{"value":false}}`, 1}, {`{"amount":{"value":null}}`, 1}, {`{}`, 0}} {
		r := runServer(t, toolCall(t, 1, factAdviceTool, map[string]any{"pack": pack, "facts": row.facts}))[0]["result"].(map[string]any)
		if r["isError"] == true {
			t.Fatal(r)
		}
		var report struct {
			Status, FactCheckVersion string
			Findings                 []any
		}
		decodeStructured(t, r, &report)
		if report.Status != "complete" || report.FactCheckVersion != "1" || len(report.Findings) != row.findings {
			t.Fatal(row, report)
		}
	}
	if _, e := os.Stat(trail); !os.IsNotExist(e) {
		t.Fatal("fact check wrote an audit record", e)
	}
	for _, args := range []map[string]any{{"pack": pack, "facts": "{"}, {"pack": "{}", "facts": "{}"}} {
		r := runServer(t, toolCall(t, 1, factAdviceTool, args))[0]["result"].(map[string]any)
		var report struct{ Status, Message string }
		decodeStructured(t, r, &report)
		if report.Status != "unavailable" || report.Message == "" {
			t.Fatal("invalid input looked clean", report)
		}
	}
	for _, args := range []map[string]any{{"pack": pack}, {"pack": pack, "facts": nil}, {"pack": pack, "facts": "{}", "expectedDisposition": "invented"}} {
		r := runServer(t, toolCall(t, 1, factAdviceTool, args))[0]["result"].(map[string]any)
		if r["isError"] != true {
			t.Fatal("unsupported arguments accepted", r)
		}
	}
}

func TestFactAdviceStrictArgumentsAndEnvelope(t *testing.T) {
	pack, _ := comparableServerProject(t, `"configVersion":"5"`)
	for _, arguments := range []string{
		`null`, `[]`, `{}`, `{"pack":"{}","Facts":"{}"}`,
		`{"pack":"{}","facts":"{}","supported_extensions":null}`,
		`{"pack":"{}","facts":"{}","supported_extensions":[null]}`,
		`{"pack":"{}","facts":"{}","supported_extensions":[1]}`,
		`{"pack":"{}","facts":"{}","supported_extensions":{}}`,
		`{"pack":"{}","facts":"{}","expectedDisposition":{}}`,
		`{"pack":"{}","pack":"{}","facts":"{}"}`,
	} {
		r := runServer(t, rawToolCall(t, 1, factAdviceTool, arguments))[0]
		if result, ok := r["result"].(map[string]any); ok && result["isError"] != true {
			t.Fatal(arguments, r)
		}
		if r["result"] == nil && r["error"] == nil {
			t.Fatal(arguments, r)
		}
	}
	for _, extensions := range []any{make([]string, 257), []string{strings.Repeat("x", 1025)}, []string{strings.Repeat("é", 513)}} {
		r := runServer(t, toolCall(t, 1, factAdviceTool, map[string]any{"pack": pack, "facts": "{}", "supported_extensions": extensions}))[0]["result"].(map[string]any)
		if r["isError"] != true {
			t.Fatal("accepted oversized extensions", r)
		}
	}
	r := runServer(t, toolCall(t, 1, factAdviceTool, map[string]any{"pack": pack, "facts": `{"amount":{"value":0}}`, "supported_extensions": []string{}}))[0]["result"].(map[string]any)
	report := r["structuredContent"].(map[string]any)
	for key, want := range map[string]any{"outputVersion": "2", "contractVersion": "1", "specVersion": "0.2.0-draft", "factCheckVersion": "1", "experimental": true, "command": "mcp " + factAdviceTool, "status": "complete"} {
		if report[key] != want {
			t.Fatal(key, report)
		}
	}
	for _, key := range []string{"disposition", "evaluationError", "expectedDisposition", "trace", "factChecks"} {
		if _, ok := report[key]; ok {
			t.Fatal("unexpected evaluation member", key)
		}
	}
	finding := report["findings"].([]any)[0].(map[string]any)
	want := map[string]any{"code": "FACT-NOT-COMPARABLE", "path": "/amount/value", "operator": "greater-than", "factType": "number", "expectedTypes": []any{"decimal-string"}, "message": `"/amount/value" is a number, and greater-than can match only a decimal string`}
	if !reflect.DeepEqual(finding, want) {
		t.Fatalf("%#v", finding)
	}
	var textReport map[string]any
	if err := json.Unmarshal([]byte(toolText(t, r)), &textReport); err != nil || !reflect.DeepEqual(report, textReport) {
		t.Fatal("text/structured report differ", err)
	}
}

func TestFactAdviceUnavailableForMalformedLimitedOrUnsupportedInputs(t *testing.T) {
	pack, _ := comparableServerProject(t, `"configVersion":"5"`)
	extended := strings.TrimSuffix(strings.TrimSpace(pack), "}") + `,"extensions":{"com.example.test":{}},"metadata":{"requiredExtensions":["com.example.test"]}}`
	for _, args := range []map[string]any{
		{"pack": pack, "facts": ""},
		{"pack": pack, "facts": `{"amount":{},"amount":{}}`},
		{"pack": pack, "facts": `{"amount":{"value":"` + strings.Repeat("9", factAdviceWorkBudget) + `"}}`},
		{"pack": strings.Replace(pack, "0.2.0-draft", "0.3.0", 1), "facts": "{}"},
		{"pack": extended, "facts": "{}"},
	} {
		r := runServer(t, toolCall(t, 1, factAdviceTool, args))[0]["result"].(map[string]any)
		report := r["structuredContent"].(map[string]any)
		if report["status"] != "unavailable" || report["message"] == "" || len(report["findings"].([]any)) != 0 {
			t.Fatal(report)
		}
	}
	r := runServer(t, toolCall(t, 1, factAdviceTool, map[string]any{"pack": extended, "facts": "null", "supported_extensions": []string{"com.example.test"}}))[0]["result"].(map[string]any)
	if r["structuredContent"].(map[string]any)["status"] != "complete" {
		t.Fatal(r)
	}
}
