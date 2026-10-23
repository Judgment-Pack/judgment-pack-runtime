package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvaluationContributorsReachBothMCPSerializations(t *testing.T) {
	t.Chdir(t.TempDir())
	pack := strings.Replace(core030PublicPack, `"value":false`, `"value":true`, 1)
	report := core030Both(t, toolCall(t, 1, "experimental_evaluate", map[string]any{"pack": pack, "facts": "{}", "rehearsal": true}))
	encoded, err := json.Marshal(report["contributors"])
	const want = `[{"id":"one","kind":"rule"}]`
	if err != nil || string(encoded) != want {
		t.Fatalf("contributors = %s (%v), want %s", encoded, err, want)
	}
	pack = strings.Replace(pack, `"value":true`, `"value":false`, 1)
	report = core030Both(t, toolCall(t, 2, "experimental_evaluate", map[string]any{"pack": pack, "facts": "{}", "rehearsal": true}))
	if _, present := report["contributors"]; present {
		t.Fatal("unresolved evaluation reported contributors")
	}
}
