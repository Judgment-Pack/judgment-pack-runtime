package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// citedSessions are sessions the gateway's token form refuses and the
// dialect companion's §5.2 admits: every one is read as a string and
// recorded exactly as given (ADR-0033, runtime #290).
var citedSessions = map[string]string{
	"empty":                "",
	"spaces":               " s  1 ",
	"colon":                "urn:session:1",
	"quote":                `a"b`,
	"backslash":            `a\b`,
	"control character":    "a\x01\tb\n",
	"non-ASCII":            "séance-日本",
	"line separator":       "a b",
	"dots and slash":       "../x",
	"the bound":            strings.Repeat("s", audit.MaxSessionIDBytes),
	"a surrogate pair":     "a\U0001F600b",
	"leading and trailing": " s ",
}

func citationsOf(t *testing.T, session string) string {
	t.Helper()
	encoded, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	return `[{"sessionId":` + string(encoded) + `,"callIndex":3,"signature":"` + strings.Repeat("ab", 64) + `"}]`
}

func recordedSession(t *testing.T, record map[string]any) string {
	t.Helper()
	cites, ok := record["cites"].([]any)
	if !ok || len(cites) != 1 {
		t.Fatalf("one citation on the record: %v", record["cites"])
	}
	return cites[0].(map[string]any)["sessionId"].(string)
}

// experimental evaluate --cites admits each session and writes it as given.
func TestEvaluateRecordsAnySessionStringAsGiven(t *testing.T) {
	for name, session := range citedSessions {
		t.Run(name, func(t *testing.T) {
			configPath := auditProject(t)
			facts := writeDocument(t, "facts.json", hardFailFacts)
			cites := writeDocument(t, "cites.json", citationsOf(t, session))
			code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
				"--config", configPath, "--facts", facts, "--cites", cites, "--format", "json"}, "")
			if code != 0 || stderr != "" {
				t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
			}
			records := auditRecords(t, configPath)
			if len(records) != 1 || recordedSession(t, records[0]) != session {
				t.Fatalf("the session as given: %v", records)
			}
		})
	}
}

// experimental graph evaluate --cites has its own path: every record of the
// run, node and composite, carries the session as given.
func TestGraphEvaluateRecordsAnySessionStringAsGiven(t *testing.T) {
	for name, session := range citedSessions {
		t.Run(name, func(t *testing.T) {
			configPath, graphPath := graphAuditProject(t)
			inputs := writeGraphInputs(t, graphHappyInputs)
			cites := writeDocument(t, "cites.json", citationsOf(t, session))
			code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
				"--config", configPath, "--inputs", inputs, "--cites", cites, "--format", "json"}, "")
			if code != 0 || stderr != "" {
				t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
			}
			records := auditRecords(t, configPath)
			if len(records) < 2 {
				t.Fatalf("a node record and the composite: %v", records)
			}
			for _, record := range records {
				if recordedSession(t, record) != session {
					t.Fatalf("the session as given on every record: %v", record)
				}
			}
		})
	}
}

// A session over the bound, and one that is not a string, are refused by
// name on both surfaces, and nothing is recorded.
func TestCitationSessionRefusalsNameTheBoundAndTheType(t *testing.T) {
	cases := map[string]struct{ document, message string }{
		"over the bound": {`[{"sessionId":"` + strings.Repeat("s", audit.MaxSessionIDBytes+1) + `","callIndex":1,"signature":"` + strings.Repeat("a", 128) + `"}]`, "sessionId must be at most 1024 bytes in UTF-8"},
		"not a string":   {`[{"sessionId":7,"callIndex":1,"signature":"` + strings.Repeat("a", 128) + `"}]`, "sessionId must be a string"},
		"null":           {`[{"sessionId":null,"callIndex":1,"signature":"` + strings.Repeat("a", 128) + `"}]`, "sessionId must be a string"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			configPath := auditProject(t)
			facts := writeDocument(t, "facts.json", hardFailFacts)
			cites := writeDocument(t, "cites.json", tc.document)
			code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
				"--config", configPath, "--facts", facts, "--cites", cites, "--format", "json"}, "")
			if code != result.ExitInvocation || !strings.Contains(stdout+stderr, "JPS-INVOCATION-CITES") || !strings.Contains(stdout+stderr, tc.message) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			noAuditTrail(t, configPath)

			graphConfig, graphPath := graphAuditProject(t)
			inputs := writeGraphInputs(t, graphHappyInputs)
			code, stdout, stderr = runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
				"--config", graphConfig, "--inputs", inputs, "--cites", cites, "--format", "json"}, "")
			if code != result.ExitInvocation || !strings.Contains(stdout+stderr, "JPS-INVOCATION-CITES") || !strings.Contains(stdout+stderr, tc.message) {
				t.Fatalf("graph: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			noAuditTrail(t, graphConfig)
		})
	}
}
