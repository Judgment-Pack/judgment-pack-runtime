package mcp

import (
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
	for _, index := range []int{3, 4} {
		refused := responses[index]["result"].(map[string]any)
		if refused["isError"] != true || !strings.Contains(toolText(t, refused), `"require_matrix" argument must be a JSON boolean`) {
			t.Fatalf("call %d: a non-boolean require_matrix is refused: %#v", index+1, refused)
		}
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
			t.Fatalf("require_matrix is advertised as an optional boolean on a closed schema: %v", schema)
		}
		return
	}
	t.Fatal("experimental_test_packs is not advertised")
}
