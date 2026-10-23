package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMatrixFactAdviceIsOptionalReadOnlyAndDoesNotRejectNegativeTests(t *testing.T) {
	pack, trail := comparableServerProject(t, `"configVersion":"5","requireComparableFacts":true`)
	matrix := `{"matrixVersion":"3","cases":[{"id":"number","facts":{"amount":{"value":42}},"expectedDisposition":{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none"}}},{"id":"good","facts":{"amount":{"value":"42"}},"expectedErrorClass":"malformed-input"},{"id":"null","facts":{"amount":{"value":null}},"expectedErrorClass":"malformed-input"},{"id":"absent","facts":{},"expectedErrorClass":"malformed-input"}]}`
	response := runServer(t, toolCall(t, 1, matrixValidationTool, map[string]any{"matrix": matrix, "pack": pack}))[0]["result"].(map[string]any)
	if response["isError"] == true {
		t.Fatal(response)
	}
	var report struct {
		Status           string
		FactCheckVersion string
		FactChecks       []struct {
			Index      int
			ID, Status string
			Findings   []struct {
				Path, FactType string
				ExpectedTypes  []string
			}
		}
	}
	decodeStructured(t, response, &report)
	if report.Status != "valid" || report.FactCheckVersion != "1" || len(report.FactChecks) != 4 {
		t.Fatalf("%+v", report)
	}
	for i, row := range report.FactChecks {
		if row.Index != i || row.Status != "complete" || row.ID != []string{"number", "good", "null", "absent"}[i] {
			t.Fatal(row)
		}
	}
	if f := report.FactChecks[0].Findings; len(f) != 1 || f[0].Path != "/amount/value" || f[0].FactType != "number" || f[0].ExpectedTypes[0] != "decimal-string" {
		t.Fatal(f)
	}
	if len(report.FactChecks[1].Findings) != 0 || len(report.FactChecks[2].Findings) != 1 || len(report.FactChecks[3].Findings) != 0 {
		t.Fatal(report.FactChecks)
	}
	if _, err := os.Stat(trail); !os.IsNotExist(err) {
		t.Fatal("advisories wrote an audit record", err)
	}
	// Even a broken project configuration is irrelevant: only supplied bytes are read.
	if err := os.WriteFile(filepath.Join(filepath.Dir(filepath.Dir(trail)), "jpack.json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	r := runServer(t, toolCall(t, 2, matrixValidationTool, map[string]any{"matrix": matrix, "pack": pack}))[0]["result"].(map[string]any)
	if r["isError"] == true {
		t.Fatal(r)
	}
	legacy := matrixReport(t, matrix)
	if _, ok := legacy["factChecks"]; ok {
		t.Fatal("changed legacy validation")
	}
	// Malformed pack cannot produce a clean fact check.
	r = runServer(t, toolCall(t, 3, matrixValidationTool, map[string]any{"matrix": matrix, "pack": "{}"}))[0]["result"].(map[string]any)
	decodeStructured(t, r, &report)
	for _, row := range report.FactChecks {
		if row.Status != "unavailable" {
			t.Fatal(row)
		}
	}
}

func TestMatrixFactAdvicePreservesAdmissionAndUnavailableOrdering(t *testing.T) {
	pack, _ := comparableServerProject(t, `"configVersion":"5"`)
	matrix := `{"matrixVersion":"3","cases":[{"id":"missing-expectation","facts":{"amount":{"value":0}}}]}`
	legacy := matrixReport(t, matrix)
	r := runServer(t, toolCall(t, 1, matrixValidationTool, map[string]any{"matrix": matrix, "pack": pack}))[0]["result"].(map[string]any)["structuredContent"].(map[string]any)
	if !reflect.DeepEqual(legacy, r) {
		t.Fatal("pack must not change malformed matrix admission", legacy, r)
	}
	// Deliberately malformed evaluator inputs remain structurally legal rows.
	matrix = `{"matrixVersion":"3","cases":[{"id":"false","facts":false,"expectedErrorClass":"malformed-input"},{"id":"zero","facts":0,"expectedErrorClass":"malformed-input"},{"id":"null","facts":null,"expectedErrorClass":"malformed-input"}]}`
	r = runServer(t, toolCall(t, 1, matrixValidationTool, map[string]any{"matrix": matrix, "pack": "{}"}))[0]["result"].(map[string]any)["structuredContent"].(map[string]any)
	if r["status"] != "valid" || r["factCheckVersion"] != "1" {
		t.Fatal(r)
	}
	for i, row := range r["factChecks"].([]any) {
		got := row.(map[string]any)
		if got["index"] != float64(i) || got["id"] != []string{"false", "zero", "null"}[i] || got["status"] != "unavailable" || got["message"] == "" || len(got["findings"].([]any)) != 0 {
			t.Fatal(got)
		}
	}
}

func TestMatrixFactAdviceBatchLimits(t *testing.T) {
	pack, _ := comparableServerProject(t, `"configVersion":"5"`)
	for _, tc := range []struct {
		name       string
		count      int
		extensions bool
	}{
		{"case-count", maxFactAdviceCases + 1, false},
		{"capability-sets", maxFactAdviceCapabilitySets + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := []any{}
			for i := 0; i < tc.count; i++ {
				row := map[string]any{"id": fmt.Sprintf("case-%d", i), "facts": map[string]any{}, "expectedErrorClass": "malformed-input"}
				if tc.extensions {
					row["supportedExtensions"] = []string{fmt.Sprintf("com.example.cap-%d", i)}
				}
				rows = append(rows, row)
			}
			matrix, _ := json.Marshal(map[string]any{"matrixVersion": "3", "cases": rows})
			if matrixReport(t, string(matrix))["status"] != "valid" {
				t.Fatal("changed matrix admission")
			}
			r := runServer(t, toolCall(t, 1, matrixValidationTool, map[string]any{"matrix": string(matrix), "pack": pack}))[0]["result"].(map[string]any)
			if r["isError"] != true {
				t.Fatal("limited advisory looked complete", r)
			}
		})
	}
}

func TestMatrixFactAdviceWorkLimitIsPerCaseAndFindingsRemainOrdered(t *testing.T) {
	pack, _ := comparableServerProject(t, `"configVersion":"5"`)
	matrix := `{"matrixVersion":"3","cases":[{"id":"limited","facts":{"amount":{"value":"` + strings.Repeat("9", factAdviceWorkBudget) + `"}},"expectedErrorClass":"resource-exhaustion"},{"id":"good","facts":{"amount":{"value":"0"}},"expectedErrorClass":"malformed-input"}]}`
	r := runServer(t, toolCall(t, 1, matrixValidationTool, map[string]any{"matrix": matrix, "pack": pack}))[0]["result"].(map[string]any)["structuredContent"].(map[string]any)
	rows := r["factChecks"].([]any)
	if r["status"] != "valid" || len(rows) != 2 || rows[0].(map[string]any)["status"] != "unavailable" || rows[1].(map[string]any)["status"] != "complete" {
		t.Fatal(r)
	}
}

func TestMatrixFactAdviceAggregateReportLimits(t *testing.T) {
	base, _ := comparableServerProject(t, `"configVersion":"5"`)
	for _, tc := range []struct {
		name, prefix      string
		conditions, cases int
	}{
		{"finding-count", "p", 100, 11},
		{"serialized-bytes", strings.Repeat("<", 5000), 1, 150},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var pack map[string]any
			if err := json.Unmarshal([]byte(base), &pack); err != nil {
				t.Fatal(err)
			}
			conditions := []any{}
			facts := map[string]any{}
			for i := 0; i < tc.conditions; i++ {
				key := fmt.Sprintf("%s%d", tc.prefix, i)
				conditions = append(conditions, map[string]any{"op": "fact", "path": "/" + key, "operator": "equals", "value": true})
				facts[key] = 0
			}
			pack["rules"] = []any{map[string]any{"id": "check", "description": "Check fact types.", "when": map[string]any{"op": "all", "conditions": conditions}, "outcome": "violation", "onUnknown": "escalate"}}
			packBytes, _ := json.Marshal(pack)
			rows := []any{}
			for i := 0; i < tc.cases; i++ {
				rows = append(rows, map[string]any{"id": fmt.Sprintf("row-%d", i), "facts": facts, "expectedErrorClass": "malformed-input"})
			}
			matrix, _ := json.Marshal(map[string]any{"matrixVersion": "3", "cases": rows})
			r := runServer(t, toolCall(t, 1, matrixValidationTool, map[string]any{"matrix": string(matrix), "pack": string(packBytes)}))[0]["result"].(map[string]any)
			if r["isError"] != true || !strings.Contains(toolText(t, r), "report") {
				t.Fatal("aggregate limit did not refuse", r)
			}
		})
	}
}

func TestMatrixFactAdviceCountsCapabilitySetsByMeaning(t *testing.T) {
	pack, _ := comparableServerProject(t, `"configVersion":"5"`)
	rows := []any{
		map[string]any{"id": "omitted", "facts": map[string]any{}, "expectedErrorClass": "malformed-input"},
		map[string]any{"id": "empty", "facts": map[string]any{}, "supportedExtensions": []string{}, "expectedErrorClass": "malformed-input"},
	}
	for i := 0; i < maxFactAdviceCapabilitySets-1; i++ {
		rows = append(rows, map[string]any{"id": fmt.Sprintf("set-%d", i), "facts": map[string]any{}, "supportedExtensions": []string{fmt.Sprintf("com.example.cap-%d", i)}, "expectedErrorClass": "malformed-input"})
	}
	matrix, _ := json.Marshal(map[string]any{"matrixVersion": "3", "cases": rows})
	r := runServer(t, toolCall(t, 1, matrixValidationTool, map[string]any{"matrix": string(matrix), "pack": pack}))[0]["result"].(map[string]any)
	if r["isError"] == true || len(r["structuredContent"].(map[string]any)["factChecks"].([]any)) != len(rows) {
		t.Fatal("equivalent empty capability sets counted twice", r)
	}
}

func TestMatrixFactAdviceKeepsRawNullCapabilitiesUnavailable(t *testing.T) {
	pack, _ := comparableServerProject(t, `"configVersion":"5"`)
	// Matrix-only admission historically accepts these representations. Advice
	// must inspect the raw members before []string decoding can normalize them.
	matrix := `{"matrixVersion":"3","cases":[{"id":"null-array","facts":{"amount":{"value":0}},"supportedExtensions":null,"expectedErrorClass":"malformed-input"},{"id":"null-entry","facts":{"amount":{"value":false}},"supportedExtensions":[null],"expectedErrorClass":"malformed-input"},{"id":"omitted","facts":{"amount":{"value":0}},"expectedErrorClass":"malformed-input"},{"id":"empty","facts":{"amount":{"value":"0"}},"supportedExtensions":[],"expectedErrorClass":"malformed-input"}]}`
	legacy := matrixReport(t, matrix)
	if legacy["status"] != "valid" || legacy["factChecks"] != nil || legacy["factCheckVersion"] != nil {
		t.Fatal("changed legacy admission or shape", legacy)
	}
	wantResults := []any{}
	for index, id := range []string{"null-array", "null-entry", "omitted", "empty"} {
		wantResults = append(wantResults, map[string]any{"index": float64(index), "id": id, "status": "valid", "findings": []any{}})
	}
	if !reflect.DeepEqual(legacy["results"], wantResults) {
		t.Fatal("legacy structural results changed", legacy["results"])
	}
	response := runServer(t, toolCall(t, 1, matrixValidationTool, map[string]any{"matrix": matrix, "pack": pack}))[0]["result"].(map[string]any)
	if response["isError"] == true {
		t.Fatal(response)
	}
	report := response["structuredContent"].(map[string]any)
	if report["status"] != legacy["status"] || report["factCheckVersion"] != "1" || !reflect.DeepEqual(report["results"], legacy["results"]) {
		t.Fatal("advice changed structural admission", report)
	}
	rows := report["factChecks"].([]any)
	if len(rows) != 4 {
		t.Fatal("lost advisory rows", rows)
	}
	for index, id := range []string{"null-array", "null-entry", "omitted", "empty"} {
		row := rows[index].(map[string]any)
		if row["index"] != float64(index) || row["id"] != id {
			t.Fatal("lost advisory order", row)
		}
		if index < 2 {
			if row["status"] != "unavailable" || row["message"] == nil || row["message"] == "" || len(row["findings"].([]any)) != 0 {
				t.Error("null capabilities silently normalized into a completed check", row)
			}
		} else if row["status"] != "complete" || len(row["findings"].([]any)) != 3-index {
			t.Error("omitted and empty capabilities must preserve fact advice", row)
		}
	}
	if after := matrixReport(t, matrix); !reflect.DeepEqual(legacy, after) {
		t.Fatal("advice changed later matrix-only behavior", after)
	}
}
