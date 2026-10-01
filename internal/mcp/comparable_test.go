package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	comparableGood   = `{"amount":{"exceedsCap":false,"value":"10"},"request":{"exempt":false,"outsideWindow":false,"region":"domestic","verified":true}}`
	comparableString = `{"amount":{"exceedsCap":"true","value":"10"},"request":{"exempt":false,"outsideWindow":false,"region":"domestic","verified":true}}`
	comparableNull   = `{"amount":{"exceedsCap":null,"value":"10"},"request":{"exempt":false,"outsideWindow":false,"region":"domestic","verified":true}}`
	comparableAbsent = `{"amount":{"value":"10"},"request":{"exempt":false,"outsideWindow":false,"region":"domestic","verified":true}}`
)

// comparableServerProject lays out a project around the comparable-facts
// fixture under the given configuration head, with an audit trail and a matrix
// whose row probes the string flag, points JPACK_CONFIG at it, and returns the
// pack's bytes and the trail's path.
func comparableServerProject(t *testing.T, head string) (string, string) {
	t.Helper()
	root := t.TempDir()
	pack, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "comparable-facts.json"))
	if err != nil {
		t.Fatal(err)
	}
	matrix := `{"matrixVersion":"1","cases":[{"id":"string-flag","facts":` + comparableString +
		`,"expectedDisposition":{"kind":"outcome","outcomeId":"permitted","reasons":[],"handoff":{"state":"none"}}}]}`
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{"packs/detector.json": pack, "packs/detector.matrix.json": []byte(matrix),
		project.DefaultConfigName: []byte(`{` + head + `,"audit":{"dir":"audit"},"packs":{"detector":{"path":"packs/detector.json","matrix":"packs/detector.matrix.json"}}}`)} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(project.ConfigEnv, filepath.Join(root, project.DefaultConfigName))
	return string(pack), filepath.Join(root, "audit", audit.FileName)
}

// comparableRefusal requires one call's result to be the comparable-facts
// refusal, naming want, with no disposition and no §8.4 class.
func comparableRefusal(t *testing.T, outcome map[string]any, want string) {
	t.Helper()
	if outcome["isError"] != true {
		t.Fatalf("the call must be refused: %#v", outcome)
	}
	var envelope struct {
		Diagnostics []struct {
			Code string `json:"code"`
		} `json:"diagnostics"`
		EvaluationError any `json:"evaluationError"`
		Disposition     any `json:"disposition"`
	}
	decodeStructured(t, outcome, &envelope)
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != evaluation.ComparableFactsCode || envelope.Disposition != nil ||
		envelope.EvaluationError != nil || !strings.Contains(toolText(t, outcome), want) {
		t.Fatalf("refusal = %+v %q, want %s naming %q", envelope, toolText(t, outcome), evaluation.ComparableFactsCode, want)
	}
}

// experimental_evaluate in a project that sets requireComparableFacts refuses
// a fact no comparison reading it can match, as the CLI does (ADR-0046): a
// pack by id or as text, a decision or a rehearsal, a string or null where a
// boolean is compared. Facts of the right types and an absent fact are
// evaluated, and only those calls are recorded. experimental_test_packs, whose
// row probes the string flag on purpose, is not refused.
func TestExperimentalEvaluateRefusesAFactNoComparisonCanMatch(t *testing.T) {
	pack, trail := comparableServerProject(t, `"configVersion":"5","requireComparableFacts":true`)
	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "detector", "facts": comparableString}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack": pack, "facts": comparableNull}),
		toolCall(t, 3, "experimental_evaluate", map[string]any{"pack_id": "detector", "facts": comparableString, "rehearsal": true}),
		toolCall(t, 4, "experimental_evaluate", map[string]any{"pack_id": "detector", "facts": comparableGood}),
		toolCall(t, 5, "experimental_evaluate", map[string]any{"pack_id": "detector", "facts": comparableAbsent}),
		toolCall(t, 6, "experimental_test_packs", map[string]any{}),
	}, ""))
	comparableRefusal(t, responses[0]["result"].(map[string]any), `"/amount/exceedsCap" is a string, and equals can match only a boolean`)
	comparableRefusal(t, responses[1]["result"].(map[string]any), `"/amount/exceedsCap" is null, and equals can match only a boolean`)
	comparableRefusal(t, responses[2]["result"].(map[string]any), `"/amount/exceedsCap" is a string, and equals can match only a boolean`)
	var good, absent result.Evaluation
	decodeStructured(t, responses[3]["result"].(map[string]any), &good)
	decodeStructured(t, responses[4]["result"].(map[string]any), &absent)
	if good.Disposition.OutcomeID != "permitted" || absent.Disposition.Kind != "unresolved" {
		t.Fatalf("the right types and an absent fact are evaluated: %+v %+v", good.Disposition, absent.Disposition)
	}
	tested := responses[5]["result"].(map[string]any)
	if tested["isError"] != false || !strings.Contains(toolText(t, tested), `"string-flag"`) || strings.Contains(toolText(t, tested), evaluation.ComparableFactsCode) {
		t.Fatalf("experimental_test_packs runs the probing row: %#v", tested)
	}
	data, err := os.ReadFile(trail)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(data)), "\n"); len(lines) != 2 {
		t.Fatalf("only the two evaluated decisions are recorded: %q", data)
	}
}

// Without the member a "5" project evaluates the string flag to the permitted
// answer Core gives it, and the call's record names the comparison that
// crossed types.
func TestWithoutTheMemberExperimentalEvaluateRecordsTheMismatch(t *testing.T) {
	_, trail := comparableServerProject(t, `"configVersion":"5"`)
	responses := runServer(t, toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "detector", "facts": comparableString}))
	var evaluated result.Evaluation
	decodeStructured(t, responses[0]["result"].(map[string]any), &evaluated)
	if evaluated.Disposition.OutcomeID != "permitted" {
		t.Fatalf("the string flag falls through to the fallback: %+v", evaluated.Disposition)
	}
	data, err := os.ReadFile(trail)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	mismatches, _ := json.Marshal(record["typeMismatches"])
	if string(mismatches) != `[{"factType":"string","operandTypes":["boolean"],"operator":"equals","path":"/amount/exceedsCap"}]` {
		t.Fatalf("the record names the comparison that crossed types: %s", mismatches)
	}
}
