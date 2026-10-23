package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEvaluationContributorsReachCLIJSONAndAudit(t *testing.T) {
	t.Chdir(t.TempDir())
	pack := strings.Replace(core030CLIPack, `"value":false`, `"value":true`, 1)
	config := writeProjectFixture(t, `{"configVersion":"3","audit":{"dir":"audit"},"packs":{"routes":{"path":"pack.json"}}}`, map[string]string{"pack.json": pack})
	facts := writeDocument(t, "facts.json", "{}")
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "routes", "--config", config, "--facts", facts, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("evaluation: %d %s %s", code, stdout, stderr)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatal(err)
	}
	const want = `[{"kind":"rule","id":"one"}]`
	var compact bytes.Buffer
	if err := json.Compact(&compact, payload["contributors"]); err != nil {
		t.Fatal(err)
	}
	if compact.String() != want {
		t.Fatalf("CLI contributors = %s, want %s", compact.String(), want)
	}
	records := auditRecords(t, config)
	if len(records) != 1 {
		t.Fatalf("records = %d", len(records))
	}
	encoded, err := json.Marshal(records[0]["contributors"])
	if err != nil || string(encoded) != `[{"id":"one","kind":"rule"}]` {
		t.Fatalf("audit contributors = %s (%v)", encoded, err)
	}
}
