package mcp

import "testing"

// get_schema and the example tools refuse the retired selectors as any
// version they do not bundle, and say, as the pack and suite refusals do,
// where the staged draft went. A version that was never a selector gets the
// plain refusal.
func TestSchemaAndExampleToolsSayWhereTheStagedDraftWent(t *testing.T) {
	const plain = "The exact JPS specification version is not bundled with this runtime."
	const draft = plain + " The staged draft selector 0.4.0-draft was never released: its text was released as Core 0.5.0, and a pack written to it moves up by changing its specVersion to \"0.5.0\" (Core \u00a711)."
	const point = plain + " No specVersion 0.4.0 exists: the staged draft selector 0.4.0-draft was released as Core 0.5.0 (Core \u00a711)."
	for _, tool := range []string{"get_schema", "list_examples", "get_example"} {
		for version, want := range map[string]string{"0.4.0-draft": draft, "0.4.0": point, "9.9.9": plain} {
			t.Run(tool+" "+version, func(t *testing.T) {
				arguments := map[string]any{"spec_version": version}
				if tool == "get_example" {
					arguments["name"] = "minimal"
				}
				outcome := runServer(t, toolCall(t, 1, tool, arguments))[0]["result"].(map[string]any)
				if outcome["isError"] != true || toolText(t, outcome) != want {
					t.Fatalf("outcome %#v", outcome)
				}
			})
		}
	}
}
