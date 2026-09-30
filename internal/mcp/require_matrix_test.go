package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// require_matrix is experimental_test_packs' form of --require-matrix
// (ADR-0042): a strict boolean, advertised as one, that turns a declared pack
// with no matrix from skipped into a failing entry.
func TestExperimentalTestPacksRequireMatrix(t *testing.T) {
	matrixProjectFixture(t, `{"configVersion":"1","packs":{
	  "intake":{"path":"packs/intake-0.1.0.pack.json","matrix":"packs/intake.matrix.json"},
	  "untested":{"path":"packs/intake-0.1.0.pack.json"}
	}}`, passingMatrix)
	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_test_packs", map[string]any{}),
		toolCall(t, 2, "experimental_test_packs", map[string]any{"require_matrix": true}),
		toolCall(t, 3, "experimental_test_packs", map[string]any{"require_matrix": false}),
		toolCall(t, 4, "experimental_test_packs", map[string]any{"require_matrix": "yes"}),
		toolCall(t, 5, "experimental_test_packs", map[string]any{"require_matrix": nil}),
		toolCall(t, 6, "experimental_test_packs", map[string]any{"require_matrix": 1}),
		toolCall(t, 7, "experimental_test_packs", map[string]any{"require_matrix": map[string]any{}}),
		toolCall(t, 8, "experimental_test_packs", map[string]any{"require_matrix": []any{true}}),
		toolCall(t, 9, "experimental_test_packs", map[string]any{"REQUIRE_MATRIX": true}),
		toolCall(t, 10, "experimental_test_packs", map[string]any{"requireMatrix": true}),
		toolCall(t, 11, "experimental_test_packs", map[string]any{"require_matrix": true, "pack_id": "untested"}),
	}, ""))
	status := func(index int) (result.PackTest, map[string]string) {
		outcome := responses[index]["result"].(map[string]any)
		if outcome["isError"] != false {
			t.Fatalf("call %d must succeed: %#v", index+1, outcome)
		}
		var report result.PackTest
		decodeStructured(t, outcome, &report)
		entries := map[string]string{}
		for _, pack := range report.Packs {
			entries[pack.ID] = pack.Status
		}
		return report, entries
	}
	for _, index := range []int{0, 2} {
		report, entries := status(index)
		if report.Status != "passed" || report.RequireMatrix || entries["untested"] != "skipped" {
			t.Fatalf("call %d, without the opt-in, skips the untested pack: %+v", index+1, report)
		}
	}
	report, entries := status(1)
	if report.Status != "mismatch" || !report.RequireMatrix || entries["untested"] != "mismatch" || entries["intake"] != "passed" {
		t.Fatalf("the opt-in fails the run on the untested pack alone: %+v", report)
	}
	for _, index := range []int{3, 4, 5, 6, 7} {
		refused := responses[index]["result"].(map[string]any)
		if refused["isError"] != true || !strings.Contains(toolText(t, refused), `"require_matrix" argument must be a JSON boolean`) {
			t.Fatalf("call %d: a non-boolean require_matrix is refused: %#v", index+1, refused)
		}
	}
	for _, index := range []int{8, 9} {
		refused := responses[index]["result"].(map[string]any)
		if refused["isError"] != true || !strings.Contains(toolText(t, refused), "require_matrix") {
			t.Fatalf("call %d: a misspelling of require_matrix is refused: %#v", index+1, refused)
		}
	}
	selected, entries := status(10)
	if selected.Status != "mismatch" || len(entries) != 1 || entries["untested"] != "mismatch" {
		t.Fatalf("selecting the matrix-less pack under the opt-in fails it: %+v", selected)
	}
	// The raw payload names the member exactly, and omits it without the opt-in.
	raw := func(index int) string {
		return string(mustJSON(t, responses[index]["result"].(map[string]any)["structuredContent"]))
	}
	if !strings.Contains(raw(1), `"requireMatrix":true`) || strings.Contains(raw(0), "requireMatrix") || strings.Contains(raw(2), "requireMatrix") {
		t.Fatalf("requireMatrix is present exactly when asked:\n%s\n%s", raw(1), raw(0))
	}

	// The argument is advertised as the handler holds it.
	listed := runServer(t, message(t, 1, "tools/list", map[string]any{}))[0]["result"].(map[string]any)["tools"].([]any)
	for _, entry := range listed {
		tool := entry.(map[string]any)
		if tool["name"] != "experimental_test_packs" {
			continue
		}
		schema := tool["inputSchema"].(map[string]any)
		property, ok := schema["properties"].(map[string]any)["require_matrix"].(map[string]any)
		if !ok || property["type"] != "boolean" || schema["additionalProperties"] != false {
			t.Fatalf("require_matrix is advertised as a boolean on a closed schema: %v", schema)
		}
		if required, _ := schema["required"].([]any); len(required) != 0 {
			t.Fatalf("require_matrix is optional, and nothing is required: %v", schema["required"])
		}
		return
	}
	t.Fatal("experimental_test_packs is not advertised")
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// The null-arguments refusal describes the tool's arguments as they are.
func TestExperimentalTestPacksNullArgumentsNameBothMembers(t *testing.T) {
	matrixProjectFixture(t, matrixConfig, passingMatrix)
	refused := runServer(t, message(t, 1, "tools/call", map[string]any{"name": "experimental_test_packs", "arguments": nil}))[0]["result"].(map[string]any)
	if refused["isError"] != true || !strings.Contains(toolText(t, refused), `an optional string "pack_id" and an optional boolean "require_matrix"`) {
		t.Fatalf("the refusal names both members: %#v", refused)
	}
}

// An oversized report is refused with the command that streams the same
// report: the same selection and the same rule, so a strict run is not
// redirected to one judged under the default.
func TestAnOversizedStrictReportNamesTheSameCommand(t *testing.T) {
	matrixProjectFixture(t, `{"configVersion":"1","packs":{
	  "intake":{"path":"packs/intake-0.1.0.pack.json","matrix":"packs/intake.matrix.json"},
	  "untested":{"path":"packs/intake-0.1.0.pack.json"}
	}}`, passingMatrix)
	bound := maxMatrixResultBytes
	maxMatrixResultBytes = 1024
	defer func() { maxMatrixResultBytes = bound }()
	for _, tc := range []struct {
		arguments map[string]any
		command   string
	}{
		{map[string]any{"require_matrix": true}, "Run jpack packs test --format json --require-matrix, which"},
		{map[string]any{"require_matrix": true, "pack_id": "intake"}, "Run jpack packs test --format json --id intake --require-matrix, which"},
		{map[string]any{}, "Run jpack packs test --format json, which"},
	} {
		refused := runServer(t, toolCall(t, 1, "experimental_test_packs", tc.arguments))[0]["result"].(map[string]any)
		if refused["isError"] != true || !strings.Contains(toolText(t, refused), tc.command) {
			t.Fatalf("%v: the refusal names %q: %q", tc.arguments, tc.command, toolText(t, refused))
		}
	}
}
