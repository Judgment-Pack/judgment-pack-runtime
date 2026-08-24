package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// A declared rehearsal writes no record in a project that asked for a trail,
// and its payload says what it is; the same run without the declaration writes
// the record and carries no label. The label is present exactly when declared
// (ADR-0028): a decoding accident or a default can never manufacture either
// side.
func TestRehearsalWritesNoRecordAndLabelsThePayload(t *testing.T) {
	configPath := auditProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	evidence := writeDocument(t, "evidence.json", presentEvidence)

	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--rehearsal",
		"--config", configPath, "--facts", facts, "--evidence", evidence, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var rehearsed result.Evaluation
	if err := json.Unmarshal([]byte(stdout), &rehearsed); err != nil {
		t.Fatal(err)
	}
	if !rehearsed.Rehearsal || rehearsed.Disposition.Kind == "" {
		t.Fatalf("a rehearsal is a labeled, complete evaluation: %+v", rehearsed)
	}
	noAuditTrail(t, configPath)

	// The undeclared run is the ordinary recorded call, and its payload carries
	// no rehearsal member at all — absent, not false — so a reader of the bytes
	// sees the label exactly where a declaration was made.
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts, "--evidence", evidence, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if strings.Contains(stdout, `"rehearsal"`) {
		t.Fatalf("an undeclared run carries no rehearsal member: %q", stdout)
	}
	records := auditRecords(t, configPath)
	if len(records) != 1 {
		t.Fatalf("one undeclared evaluation is one record, the rehearsal none: %v", records)
	}
}

// The human rendering says the same thing the payload's member says, in one
// line under the experimental headline, and says nothing when nothing was
// declared.
func TestRehearsalIsNamedInHumanOutput(t *testing.T) {
	configPath := auditProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	evidence := writeDocument(t, "evidence.json", presentEvidence)

	code, stdout, _ := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--rehearsal",
		"--config", configPath, "--facts", facts, "--evidence", evidence}, "")
	if code != 0 || !strings.Contains(stdout, "REHEARSAL: declared not a decision") {
		t.Fatalf("exit=%d, the human rendering names the declaration: %q", code, stdout)
	}
	noAuditTrail(t, configPath)
}

// A rehearsal consults no reviewed set: law the lock would refuse to decide
// under still rehearses, because the run is declared, in band, to decide
// nothing — the standing a matrix row has always had (ADR-0021). The refusal
// on the undeclared run is asserted first, so this row cannot pass while the
// lock catches nothing.
func TestRehearsalEvaluatesLawTheLockWouldRefuse(t *testing.T) {
	configPath := auditProject(t)
	mustLock(t, configPath)
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	facts := writeDocument(t, "facts.json", hardFailFacts)

	code, _, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts}, "")
	if code != result.ExitInvalid || !strings.Contains(stderr, "jpack packs lock") {
		t.Fatalf("the undeclared run must be refused under drifted law: exit=%d stderr=%q", code, stderr)
	}

	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--rehearsal",
		"--config", configPath, "--facts", facts, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("the declared rehearsal evaluates: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var rehearsed result.Evaluation
	if err := json.Unmarshal([]byte(stdout), &rehearsed); err != nil {
		t.Fatal(err)
	}
	if !rehearsed.Rehearsal || rehearsed.Disposition.Kind == "" {
		t.Fatalf("a rehearsal under drifted law is still a labeled, complete evaluation: %+v", rehearsed)
	}
	noAuditTrail(t, configPath)
}
