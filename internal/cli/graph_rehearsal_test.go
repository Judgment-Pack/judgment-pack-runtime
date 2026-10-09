package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
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
	if !strings.Contains(declared, `"rehearsal":true,`) {
		t.Fatalf("the declared run carries the label: %s", declared)
	}
	if stripped := strings.Replace(declared, `"rehearsal":true,`, "", 1); stripped != undeclared {
		t.Fatalf("the rehearsal must change only its label:\n%s\n%s", declared, undeclared)
	}
	human, plain := run("--rehearsal"), run()
	line := "REHEARSAL: declared not a decision; no audit record was appended and no reviewed set was consulted\n"
	if !strings.Contains(human, line) || strings.Replace(human, line, "", 1) != plain {
		t.Fatalf("the human rendering gains the one line and nothing else:\n%s\n%s", human, plain)
	}
}

// A graph rehearsal consults no reviewed set, for any of what a graph run
// holds to it: the configuration, the declared graph document, and each node's
// pack; and a lock that cannot be read stops the ordinary run and not the
// rehearsal. Each case asserts the refusal first, so it cannot pass while the
// lock catches nothing, and each rehearsal leaves no trail.
func TestGraphRehearsalEvaluatesLawTheLockWouldRefuse(t *testing.T) {
	appendTo := func(name string) func(t *testing.T, dir string) {
		return func(t *testing.T, dir string) {
			path := filepath.Join(dir, name)
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		name  string
		drift func(t *testing.T, dir string)
	}{
		{"configuration drift", appendTo("jpack.json")},
		{"graph document drift", appendTo("onboarding.graph.json")},
		{"upstream node pack drift", appendTo("sanctions-screening-0.1.0.pack.json")},
		{"downstream node pack drift", appendTo("vendor-onboarding-0.1.0.pack.json")},
		{"malformed lock", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "jpack.lock.json"), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"unreadable lock", func(t *testing.T, dir string) {
			lockPath := filepath.Join(dir, "jpack.lock.json")
			if err := os.Remove(lockPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(lockPath, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configPath, graphPath := graphAuditProject(t)
			mustLock(t, configPath)
			tc.drift(t, filepath.Dir(configPath))
			inputs := writeGraphInputs(t, graphHappyInputs)

			code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
				"--config", configPath, "--inputs", inputs}, "")
			if code == 0 {
				t.Fatalf("the undeclared run must be refused: stdout=%q stderr=%q", stdout, stderr)
			}
			code, stdout, stderr = runTest(t, []string{"experimental", "graph", "evaluate", graphPath, "--rehearsal",
				"--config", configPath, "--inputs", inputs, "--format", "json"}, "")
			if code != 0 || stderr != "" || !strings.Contains(stdout, `"rehearsal":true`) {
				t.Fatalf("the declared rehearsal evaluates: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
			}
			noAuditTrail(t, configPath)
		})
	}
}

// Citations are held to their shape on a rehearsal as on any run, and a
// rehearsal records them no more than anything else (ADR-0033).
func TestGraphRehearsalHoldsCitationsToTheirShapeAndRecordsNone(t *testing.T) {
	configPath, graphPath := graphAuditProject(t)
	inputs := writeGraphInputs(t, graphHappyInputs)
	valid := writeDocument(t, "cites.json", `[{"sessionId":"s1","callIndex":0,"signature":"`+strings.Repeat("ab", 64)+`"}]`)
	invalid := writeDocument(t, "bad-cites.json", `[{"sessionId":"s1"}]`)

	code, _, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath, "--rehearsal",
		"--config", configPath, "--inputs", inputs, "--cites", valid}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("well-formed citations on a rehearsal: exit=%d stderr=%q", code, stderr)
	}
	noAuditTrail(t, configPath)
	code, stdout, _ := runTest(t, []string{"experimental", "graph", "evaluate", graphPath, "--rehearsal",
		"--config", configPath, "--inputs", inputs, "--cites", invalid, "--format", "json"}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout, "JPS-INVOCATION-CITES") {
		t.Fatalf("malformed citations are refused on a rehearsal too: exit=%d stdout=%q", code, stdout)
	}
	noAuditTrail(t, configPath)
}

// A refused rehearsal is reported as any refused run is: the node and its
// error, no disposition, and no rehearsal label, since nothing was produced to
// label.
func TestAGraphRehearsalThatIsRefusedCarriesNoLabel(t *testing.T) {
	configPath, graphPath := graphAuditProject(t)
	inputs := writeGraphInputs(t, `{"screening":{"facts":{"screening":{"matches":"0"}},"evidence":{"screening-record":"maybe"}}}`)
	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath, "--rehearsal",
		"--config", configPath, "--inputs", inputs, "--format", "json"}, "")
	if code == 0 || strings.Contains(stdout, `"rehearsal"`) || strings.Contains(stdout, `"disposition"`) {
		t.Fatalf("a refused rehearsal reports the refusal alone: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	noAuditTrail(t, configPath)
}

// A rehearsal in a project whose trail already holds records leaves it byte for
// byte as it found it.
func TestGraphRehearsalLeavesAnExistingTrailUnchanged(t *testing.T) {
	configPath, graphPath := graphAuditProject(t)
	inputs := writeGraphInputs(t, graphHappyInputs)
	if code, _, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
		"--config", configPath, "--inputs", inputs}, ""); code != 0 || stderr != "" {
		t.Fatalf("the recorded run: exit=%d stderr=%q", code, stderr)
	}
	trail := filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)
	before, err := os.ReadFile(trail)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath, "--rehearsal",
		"--config", configPath, "--inputs", inputs}, ""); code != 0 || stderr != "" {
		t.Fatalf("the rehearsal: exit=%d stderr=%q", code, stderr)
	}
	after, err := os.ReadFile(trail)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("a rehearsal appended to an existing trail:\n%s", after)
	}
}

// A refused graph run and a graph rehearsal after a key rotation leave both
// the signed trail and its sidecar exactly as the hand-over left them.
func TestGraphRefusalAndRehearsalAfterRotationLeaveTrailAndSidecarUnchanged(t *testing.T) {
	skipWhereKeysCannotSign(t)
	firstSeed, _ := generatedKey(t)
	nextSeed, _ := generatedKey(t)
	t.Setenv(audit.SigningKeyEnv, firstSeed)
	configPath, graphPath := graphAuditProject(t)
	inputs := writeGraphInputs(t, graphHappyInputs)
	if code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
		"--config", configPath, "--inputs", inputs, "--format", "json"}, ""); code != 0 {
		t.Fatalf("recorded graph: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if code, stdout, stderr := runTest(t, []string{"audit", "key", "rotate", "--format", "json", "--config", configPath, "--next", nextSeed}, ""); code != 0 {
		t.Fatalf("rotation: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	trail := filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)
	sidecar := filepath.Join(filepath.Dir(configPath), "audit", audit.SidecarName)
	beforeTrail, beforeSidecar := readFileBytes(t, trail), readFileBytes(t, sidecar)

	refused := writeGraphInputs(t, `{"screening":{"facts":{"screening":{"matches":"0"}},"evidence":{"screening-record":"maybe"}}}`)
	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
		"--config", configPath, "--inputs", refused, "--format", "json"}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-EVALUATION-EVIDENCE-VALUE"`) || strings.Contains(stdout, `"disposition"`) || stderr != "" {
		t.Fatalf("refusal: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	assertAuditFilesEqual(t, trail, sidecar, beforeTrail, beforeSidecar)

	t.Setenv(audit.SigningKeyEnv, nextSeed)
	code, stdout, stderr = runTest(t, []string{"experimental", "graph", "evaluate", graphPath, "--rehearsal",
		"--config", configPath, "--inputs", inputs, "--format", "json"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"rehearsal":true`) {
		t.Fatalf("rehearsal: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	assertAuditFilesEqual(t, trail, sidecar, beforeTrail, beforeSidecar)
}
