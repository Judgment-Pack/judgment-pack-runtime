package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
)

func matrixReport(t *testing.T, matrix string) map[string]any {
	t.Helper()
	r := runServer(t, toolCall(t, 1, matrixValidationTool, map[string]any{"matrix": matrix}))[0]["result"].(map[string]any)
	if r["isError"] == true {
		t.Fatal(toolText(t, r))
	}
	return r["structuredContent"].(map[string]any)
}
func TestMatrixAdmissionAccountsForEveryCaseAndNamesBadField(t *testing.T) {
	report := matrixReport(t, `{"matrixVersion":"3","cases":[{"id":"valid","facts":{"ok":false},"expectedErrorClass":"malformed-input"},{"id":"named","name":"Human name","facts":{},"expectedErrorClass":"malformed-input"},{"id":"valid","expectedErrorClass":"malformed-input"},{"id":"expectation","facts":{},"expectedDisposition":{"kind":"unresolved","reasons":[],"handoff":{"state":"none"}}}]}`)
	if report["status"] != "invalid" || report["contractVersion"] != "1" || report["outputVersion"] != "2" || report["experimental"] != true {
		t.Fatal(report)
	}
	rows := report["results"].([]any)
	if len(rows) != 4 {
		t.Fatal("lost case", report)
	}
	first := rows[0].(map[string]any)
	if first["status"] != "valid" {
		t.Fatal(first)
	}
	for i, want := range []string{"/cases/1/name", "/cases/2/id", "/cases/3/expectedDisposition"} {
		row := rows[i+1].(map[string]any)
		if row["index"] != float64(i+1) || row["status"] != "invalid" {
			t.Fatal(row)
		}
		findings := row["findings"].([]any)
		if findings[0].(map[string]any)["path"] != want {
			t.Fatal(findings)
		}
	}
	if len(rows[2].(map[string]any)["findings"].([]any)) != 2 {
		t.Fatal("must report missing facts as well as duplicate id")
	}
}
func TestMatrixAdmissionReusesClosedVersionedCarrier(t *testing.T) {
	valid := `{"matrixVersion":"3","cases":[{"id":"negative","facts":null,"evidenceAvailability":42,"expectedErrorClass":"malformed-input"},{"id":"outcome","facts":{},"expectedDisposition":{"kind":"outcome","outcomeId":"accept","reasons":[],"handoff":{"state":"none"}},"expectedHandoffTarget":null}]}`
	if matrixReport(t, valid)["status"] != "valid" {
		t.Fatal("negative evaluator inputs must survive admission")
	}
	for _, bad := range []string{
		`{}`, `{"cases":[]}`, `{"matrixVersion":3,"cases":[]}`,
		`{"matrixVersion":"4","cases":[]}`, `{"matrixVersion":"3","cases":[null]}`,
		`{"cases":[{"id":"x","id":"y","facts":{},"expectedErrorClass":"x"}]}`,
		strings.Replace(valid, `"facts":null`, `"Facts":null`, 1),
		strings.Replace(valid, `"matrixVersion":"3"`, `"matrixVersion":"1"`, 1),
		strings.Replace(valid, `"expectedErrorClass":"malformed-input"`, `"expectedErrorClass":"malformed-input","expectedHandoffTarget":null`, 1),
		strings.Replace(valid, `"facts":null`, `"facts":null,"supportedExtensions":42`, 1),
		strings.Replace(valid, `"facts":null`, `"facts":null,"cites":[{"url":"unverified"}]`, 1),
	} {
		if matrixReport(t, bad)["status"] != "invalid" {
			t.Fatal("accepted", bad)
		}
	}
}
func TestMatrixContractDiscoveryAndArgumentBoundary(t *testing.T) {
	listed := runServer(t, message(t, 1, "tools/list", map[string]any{}))[0]["result"].(map[string]any)["tools"].([]any)
	found := map[string]bool{}
	for _, v := range listed {
		found[v.(map[string]any)["name"].(string)] = true
	}
	if !found[matrixContractTool] || !found[matrixValidationTool] {
		t.Fatal(found)
	}
	expected, _ := json.Marshal(project.MatrixContract())
	// MCP makes arguments optional, so a call that omits the member is the
	// same call as one carrying {} or null.
	for form, call := range map[string]string{
		"omitted": toolCall(t, 2, matrixContractTool, nil),
		"empty":   rawToolCall(t, 2, matrixContractTool, `{}`),
		"null":    rawToolCall(t, 2, matrixContractTool, `null`),
	} {
		r := runServer(t, call)[0]["result"].(map[string]any)
		if r["isError"] == true {
			t.Fatal(form, toolText(t, r))
		}
		actual, _ := json.Marshal(r["structuredContent"].(map[string]any)["contract"])
		if !reflect.DeepEqual(expected, actual) {
			t.Fatal("contract drift", form, string(actual))
		}
	}
	if r := runServer(t, rawToolCall(t, 2, matrixContractTool, `{"matrix":"{}"}`))[0]["result"].(map[string]any); r["isError"] != true {
		t.Fatal("accepted a member the contract tool does not declare")
	}
	for _, args := range []map[string]any{nil, {"Matrix": "{}"}, {"matrix": nil}, {"matrix": "{}", "pack": "{}"}} {
		r := runServer(t, toolCall(t, 3, matrixValidationTool, args))[0]["result"].(map[string]any)
		if r["isError"] != true {
			t.Fatal("accepted unsupported args", args)
		}
	}
}
