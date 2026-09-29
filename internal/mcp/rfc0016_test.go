package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The draft RFC 0016 opt-in is the command line's alone (ADR-0039), as the
// draft RFC 0008 opt-in is (ADR-0009): a prototype grammar is not something an
// agent should reach through a tool description. The tool refuses a pack that
// carries a value declaration as it did before the draft was prototyped, takes
// no argument that would admit one, and says nothing of the draft in what it
// lists.
func TestExperimentalEvaluateHasNoOutcomeValuesOptIn(t *testing.T) {
	pack, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "rfc0016", "refund-pass-through.json"))
	if err != nil {
		t.Fatal(err)
	}
	facts := `{"customer":{"goodStanding":true},"proposed":{"refundAmount":"149.50"}}`
	calls := []string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack": string(pack), "facts": facts}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack": string(pack), "facts": facts, "supported_extensions": []string{"org.judgmentpack.outcome-values"}}),
	}
	spellings := []string{"rfc0016_outcome_values", "rfc0016OutcomeValues", "rfc0016-outcome-values", "outcome_values", "rfc0008_quantifiers"}
	for index, spelling := range spellings {
		calls = append(calls, toolCall(t, 3+index, "experimental_evaluate", map[string]any{"pack": string(pack), "facts": facts, spelling: true}))
	}
	calls = append(calls, message(t, 3+len(spellings), "tools/list", map[string]any{}))
	responses := runServer(t, strings.Join(calls, ""))

	for index := 0; index < 2; index++ {
		refused := responses[index]["result"].(map[string]any)
		assertEvaluationError(t, refused, "pack-not-conformant", "preflight")
		if text := toolText(t, refused); !strings.Contains(text, "JPS-STRUCTURE-EXTENSION-NAME") || strings.Contains(text, "projection") {
			t.Fatalf("call %d: the refusal is the published path's: %q", index+1, text)
		}
	}
	for index, spelling := range spellings {
		refused := responses[2+index]["result"].(map[string]any)
		want := `The "experimental_evaluate" arguments carry an unknown member "` + spelling + `"; the accepted members are pack and pack_id and facts and evidence and supported_extensions and rehearsal and cites, spelled exactly.`
		if refused["isError"] != true || toolText(t, refused) != want {
			t.Fatalf("%s: %#v", spelling, refused)
		}
	}
	listed, err := json.Marshal(responses[2+len(spellings)]["result"])
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"0016", "outcome-values", "outcome values", "outcomeValues", "outcome_values"} {
		if strings.Contains(string(listed), word) {
			t.Errorf("the tool listing must say nothing of the draft, and holds %q", word)
		}
	}
	if !strings.Contains(string(listed), `"experimental_evaluate"`) {
		t.Fatalf("the listing must be the tools': %s", listed)
	}
}
