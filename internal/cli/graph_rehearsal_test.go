package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// graphAuditProject is the graph fixture under configVersion 3 with an audit
// directory declared, and the path of its graph document.
func graphAuditProject(t *testing.T) (string, string) {
	t.Helper()
	config := strings.Replace(graphFixture(t, "jpack.json"),
		`"configVersion": "2",`, `"configVersion": "3",`+"\n"+`  "audit": {"dir": "audit"},`, 1)
	files := map[string]string{
		"sanctions-screening-0.1.0.pack.json": graphFixture(t, "sanctions-screening-0.1.0.pack.json"),
		"vendor-onboarding-0.1.0.pack.json":   graphFixture(t, "vendor-onboarding-0.1.0.pack.json"),
		"onboarding.graph.json":               graphFixture(t, "onboarding.graph.json"),
		"onboarding.rows.json":                graphFixture(t, "onboarding.rows.json"),
	}
	configPath := writeProjectFixture(t, config, files)
	return configPath, filepath.Join(filepath.Dir(configPath), "onboarding.graph.json")
}

// A declared graph rehearsal writes no record, for any node or the composite,
// in a project that asked for a trail, and its payload says what it is; the
// same run without the declaration writes every record and carries no label
// (ADR-0041, on ADR-0028's precedent).
func TestGraphRehearsalWritesNoRecordAndLabelsThePayload(t *testing.T) {
	configPath, graphPath := graphAuditProject(t)
	inputs := writeGraphInputs(t, graphHappyInputs)

	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath, "--rehearsal",
		"--config", configPath, "--inputs", inputs, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var rehearsed result.GraphEvaluation
	if err := json.Unmarshal([]byte(stdout), &rehearsed); err != nil {
		t.Fatal(err)
	}
	if !rehearsed.Rehearsal || rehearsed.Disposition.Kind == "" || len(rehearsed.Nodes) == 0 {
		t.Fatalf("a graph rehearsal is a labeled, complete composite: %+v", rehearsed)
	}
	noAuditTrail(t, configPath)

	code, stdout, stderr = runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
		"--config", configPath, "--inputs", inputs, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if strings.Contains(stdout, `"rehearsal"`) {
		t.Fatalf("an undeclared run carries no rehearsal member: %q", stdout)
	}
	if records := auditRecords(t, configPath); len(records) != len(rehearsed.Nodes)+1 {
		t.Fatalf("the undeclared run records every node and the composite, the rehearsal nothing: %d records", len(records))
	}
}

// A rehearsal changes exactly one thing in the payload, the label, and one
// line of the human rendering. Deleting only the label from the declared run's
// bytes must leave the undeclared run's bytes.
func TestGraphRehearsalChangesOnlyTheLabel(t *testing.T) {
	configPath, graphPath := graphAuditProject(t)
	inputs := writeGraphInputs(t, graphHappyInputs)
	run := func(extra ...string) string {
		args := append([]string{"experimental", "graph", "evaluate", graphPath, "--config", configPath, "--inputs", inputs}, extra...)
		code, stdout, stderr := runTest(t, args, "")
		if code != 0 || stderr != "" {
			t.Fatalf("%v: exit=%d stderr=%q", extra, code, stderr)
		}
		return stdout
	}
	declared, undeclared := run("--rehearsal", "--format", "json"), run("--format", "json")
	if stripped := strings.Replace(declared, `"rehearsal":true,`, "", 1); stripped != undeclared {
		t.Fatalf("the rehearsal must change only its label:\n%s\n%s", declared, undeclared)
	}
	human, plain := run("--rehearsal"), run()
	line := "REHEARSAL: declared not a decision; no audit record was appended and no reviewed set was consulted\n"
	if !strings.Contains(human, line) || strings.Replace(human, line, "", 1) != plain {
		t.Fatalf("the human rendering gains the one line and nothing else:\n%s\n%s", human, plain)
	}
}

// A graph rehearsal consults no reviewed set: under law the lock refuses, the
// undeclared run is refused and the declared one evaluates.
func TestGraphRehearsalEvaluatesLawTheLockWouldRefuse(t *testing.T) {
	configPath, graphPath := graphAuditProject(t)
	mustLock(t, configPath)
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	inputs := writeGraphInputs(t, graphHappyInputs)

	code, _, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
		"--config", configPath, "--inputs", inputs}, "")
	if code != result.ExitInvalid || !strings.Contains(stderr, "jpack packs lock") {
		t.Fatalf("the undeclared run must be refused under drifted law: exit=%d stderr=%q", code, stderr)
	}
	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath, "--rehearsal",
		"--config", configPath, "--inputs", inputs, "--format", "json"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"rehearsal":true`) {
		t.Fatalf("the declared rehearsal evaluates under drifted law: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	noAuditTrail(t, configPath)
}
