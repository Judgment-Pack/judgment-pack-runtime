package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/lock"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// evaluateJSON runs experimental evaluate with --format json and decodes the
// payload, failing the test on any refusal.
func evaluateJSON(t *testing.T, args ...string) result.Evaluation {
	t.Helper()
	code, stdout, stderr := runTest(t, append([]string{"experimental", "evaluate", "--format", "json"}, args...), "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var evaluation result.Evaluation
	if err := json.Unmarshal([]byte(stdout), &evaluation); err != nil {
		t.Fatal(err)
	}
	return evaluation
}

// The payload says which law the decision was judged under, as the record
// does (ADR-0044): nothing in a project with no lock, true with the lock's
// revision for declared law that matched, false for a draft, and nothing for a
// rehearsal, which consulted no reviewed set. The human output says the same
// in one line.
func TestThePayloadSaysWhichLawTheDecisionWasJudgedUnder(t *testing.T) {
	configPath := auditProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	packPath := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")

	unlocked := evaluateJSON(t, "--pack-id", "intake", "--config", configPath, "--facts", facts)
	if unlocked.Reviewed != nil || unlocked.ReviewedSet != nil {
		t.Fatalf("a project with no lock says nothing about one: %v %v", unlocked.Reviewed, unlocked.ReviewedSet)
	}
	_, human, _ := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--facts", facts}, "")
	if strings.Contains(human, "reviewed set:") {
		t.Fatalf("no line for a project with no lock: %q", human)
	}

	mustLock(t, configPath)
	lockBytes, err := os.ReadFile(lockPath(configPath))
	if err != nil {
		t.Fatal(err)
	}
	declared := evaluateJSON(t, "--pack-id", "intake", "--config", configPath, "--facts", facts)
	if declared.Reviewed == nil || !*declared.Reviewed || declared.ReviewedSet == nil || declared.ReviewedSet.LockDigest != lock.Digest(lockBytes) {
		t.Fatalf("declared law that matched: reviewed=%v set=%+v", declared.Reviewed, declared.ReviewedSet)
	}
	records := auditRecords(t, configPath)
	recorded := records[len(records)-1]["reviewedSet"].(map[string]any)
	if recorded["lockDigest"] != declared.ReviewedSet.LockDigest || recorded["configDigest"] != declared.ReviewedSet.ConfigDigest {
		t.Fatalf("the payload names the revision the record names: %v vs %+v", recorded, declared.ReviewedSet)
	}
	_, human, _ = runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--facts", facts}, "")
	if !strings.Contains(human, "reviewed set: applied") || !strings.Contains(human, lock.Digest(lockBytes)) {
		t.Fatalf("the human output says the law was reviewed, and under which lock: %q", human)
	}

	draft := evaluateJSON(t, packPath, "--config", configPath, "--facts", facts)
	if draft.Reviewed == nil || *draft.Reviewed || draft.ReviewedSet != nil {
		t.Fatalf("a draft: reviewed=%v set=%+v", draft.Reviewed, draft.ReviewedSet)
	}
	_, human, _ = runTest(t, []string{"experimental", "evaluate", packPath, "--config", configPath, "--facts", facts}, "")
	if !strings.Contains(human, "reviewed set: NOT applied") {
		t.Fatalf("the human output says a draft was evaluated: %q", human)
	}

	rehearsed := evaluateJSON(t, "--pack-id", "intake", "--rehearsal", "--config", configPath, "--facts", facts)
	if !rehearsed.Rehearsal || rehearsed.Reviewed != nil || rehearsed.ReviewedSet != nil {
		t.Fatalf("a rehearsal consulted no reviewed set and says nothing about one: %+v", rehearsed)
	}
}

// requireReviewedProject is auditProject under configVersion "4" with
// requireReviewed set.
func requireReviewedProject(t *testing.T) string {
	t.Helper()
	configPath := auditProject(t)
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	required := strings.Replace(string(body), `"configVersion":"3",`, `"configVersion":"4","requireReviewed":true,`, 1)
	if required == string(body) {
		t.Fatal("the fixture's configuration changed shape; update this helper")
	}
	if err := os.WriteFile(configPath, []byte(required), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath
}

// refusedForReview runs one evaluation and requires the review refusal: its
// exit class, its code, the steer, and no disposition.
func refusedForReview(t *testing.T, want string, args ...string) {
	t.Helper()
	code, stdout, stderr := runTest(t, append([]string{"experimental", "evaluate", "--format", "json"}, args...), "")
	if code != result.ExitInvalid {
		t.Fatalf("exit=%d, want %d (stdout %q stderr %q)", code, result.ExitInvalid, stdout, stderr)
	}
	var envelope struct {
		Diagnostics []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"diagnostics"`
		Disposition any `json:"disposition"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("undecodable refusal %q: %v", stdout, err)
	}
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != lock.ReviewRequiredCode || envelope.Disposition != nil ||
		!strings.Contains(envelope.Diagnostics[0].Message, want) || !strings.Contains(envelope.Diagnostics[0].Message, "declare the run a rehearsal") {
		t.Fatalf("refusal = %+v, want %s naming %q", envelope, lock.ReviewRequiredCode, want)
	}
}

// A project that sets requireReviewed refuses every deciding run that does not
// apply its reviewed set, before any evaluation and without a record: every
// run while it has no lock, and a draft however it is named once it has one.
// Declared law that matched is evaluated, a rehearsal is not a decision and is
// not refused, and turning the member off is an amendment the lock sees.
func TestAProjectThatRequiresReviewRefusesWhatItDidNotReview(t *testing.T) {
	configPath := requireReviewedProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	packPath := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")
	trail := filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)

	refusedForReview(t, "there is no reviewed-set lock", "--pack-id", "intake", "--config", configPath, "--facts", facts)
	refusedForReview(t, "there is no reviewed-set lock", packPath, "--config", configPath, "--facts", facts)
	if _, err := os.Stat(trail); err == nil {
		t.Fatal("a refused run leaves no record")
	}

	mustLock(t, configPath)
	declared := evaluateJSON(t, "--pack-id", "intake", "--config", configPath, "--facts", facts)
	if declared.Reviewed == nil || !*declared.Reviewed {
		t.Fatalf("declared law that matched is evaluated: %+v", declared)
	}
	refusedForReview(t, "this run applies a draft", packPath, "--config", configPath, "--facts", facts)
	if records := auditRecords(t, configPath); len(records) != 1 {
		t.Fatalf("only the reviewed run is recorded: %v", records)
	}

	rehearsed := evaluateJSON(t, packPath, "--rehearsal", "--config", configPath, "--facts", facts)
	if !rehearsed.Rehearsal {
		t.Fatalf("a rehearsal is not refused: %+v", rehearsed)
	}
	if records := auditRecords(t, configPath); len(records) != 1 {
		t.Fatalf("a rehearsal records nothing: %v", records)
	}

	// Turning the member off is an edit to reviewed law: a declared run is
	// refused as drift until the project re-locks, and a draft is a draft again.
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(strings.Replace(string(body), `"requireReviewed":true`, `"requireReviewed":false`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--facts", facts}, "")
	if code != result.ExitInvalid || !strings.Contains(stderr, "jpack packs lock") {
		t.Fatalf("turning the member off is an amendment the lock sees: exit=%d stderr=%q", code, stderr)
	}
	draft := evaluateJSON(t, packPath, "--config", configPath, "--facts", facts)
	if draft.Reviewed == nil || *draft.Reviewed {
		t.Fatalf("with the member off a draft is evaluated as one: %+v", draft)
	}
}

// The graph surface applies the same rule to the run: a declared graph over
// reviewed packs is evaluated and its composite says so, and a graph document
// the configuration does not declare is a draft the project refuses.
func TestAProjectThatRequiresReviewRefusesAnUndeclaredGraph(t *testing.T) {
	config := strings.Replace(graphFixture(t, "jpack.json"),
		`"configVersion": "2",`, `"configVersion": "4",`+"\n"+`  "requireReviewed": true,`, 1)
	configPath := writeProjectFixture(t, config, map[string]string{
		"sanctions-screening-0.1.0.pack.json": graphFixture(t, "sanctions-screening-0.1.0.pack.json"),
		"vendor-onboarding-0.1.0.pack.json":   graphFixture(t, "vendor-onboarding-0.1.0.pack.json"),
		"onboarding.graph.json":               graphFixture(t, "onboarding.graph.json"),
		"onboarding.rows.json":                graphFixture(t, "onboarding.rows.json"),
	})
	mustLock(t, configPath)
	graphPath := filepath.Join(filepath.Dir(configPath), "onboarding.graph.json")
	inputs := writeGraphInputs(t, graphHappyInputs)

	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
		"--config", configPath, "--inputs", inputs, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var composite result.GraphEvaluation
	if err := json.Unmarshal([]byte(stdout), &composite); err != nil {
		t.Fatal(err)
	}
	if composite.Reviewed == nil || !*composite.Reviewed || composite.ReviewedSet == nil {
		t.Fatalf("a declared graph over reviewed packs says so: reviewed=%v set=%+v", composite.Reviewed, composite.ReviewedSet)
	}

	body, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	undeclared := filepath.Join(filepath.Dir(configPath), "scratch.graph.json")
	if err := os.WriteFile(undeclared, body, 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runTest(t, []string{"experimental", "graph", "evaluate", undeclared,
		"--config", configPath, "--inputs", inputs}, "")
	if code != result.ExitInvalid || !strings.Contains(stderr, "this run applies a draft") || strings.Contains(stdout, "disposition") {
		t.Fatalf("an undeclared graph document is refused: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runTest(t, []string{"experimental", "graph", "evaluate", undeclared,
		"--config", configPath, "--inputs", inputs, "--rehearsal"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "REHEARSAL") {
		t.Fatalf("a rehearsal of it is not refused: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

// A graph run over an undeclared graph document in a locked project that does
// not require review is a draft, evaluated with the lock open: its composite
// says reviewed false and names no reviewed set, because it was judged under
// none.
func TestAnUndeclaredGraphNamesNoReviewedSet(t *testing.T) {
	config := strings.Replace(graphFixture(t, "jpack.json"), `"configVersion": "2",`, `"configVersion": "3",`, 1)
	configPath := writeProjectFixture(t, config, map[string]string{
		"sanctions-screening-0.1.0.pack.json": graphFixture(t, "sanctions-screening-0.1.0.pack.json"),
		"vendor-onboarding-0.1.0.pack.json":   graphFixture(t, "vendor-onboarding-0.1.0.pack.json"),
		"onboarding.graph.json":               graphFixture(t, "onboarding.graph.json"),
		"onboarding.rows.json":                graphFixture(t, "onboarding.rows.json"),
	})
	mustLock(t, configPath)
	body, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "onboarding.graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	undeclared := filepath.Join(filepath.Dir(configPath), "scratch.graph.json")
	if err := os.WriteFile(undeclared, body, 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", undeclared,
		"--config", configPath, "--inputs", writeGraphInputs(t, graphHappyInputs), "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var composite result.GraphEvaluation
	if err := json.Unmarshal([]byte(stdout), &composite); err != nil {
		t.Fatal(err)
	}
	if composite.Reviewed == nil || *composite.Reviewed || composite.ReviewedSet != nil {
		t.Fatalf("an undeclared graph is a draft: reviewed=%v set=%+v", composite.Reviewed, composite.ReviewedSet)
	}
}
