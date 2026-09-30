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
	sameAsRecord(t, unlocked.Reviewed, unlocked.ReviewedSet, lastRecord(t, configPath))
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
	sameAsRecord(t, declared.Reviewed, declared.ReviewedSet, lastRecord(t, configPath))
	_, human, _ = runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--facts", facts}, "")
	if !strings.Contains(human, "reviewed set: applied") || !strings.Contains(human, lock.Digest(lockBytes)) {
		t.Fatalf("the human output says the law was reviewed, and under which lock: %q", human)
	}

	draft := evaluateJSON(t, packPath, "--config", configPath, "--facts", facts)
	if draft.Reviewed == nil || *draft.Reviewed || draft.ReviewedSet != nil {
		t.Fatalf("a draft: reviewed=%v set=%+v", draft.Reviewed, draft.ReviewedSet)
	}
	sameAsRecord(t, draft.Reviewed, draft.ReviewedSet, lastRecord(t, configPath))
	_, human, _ = runTest(t, []string{"experimental", "evaluate", packPath, "--config", configPath, "--facts", facts}, "")
	if !strings.Contains(human, "reviewed set: NOT applied") {
		t.Fatalf("the human output says a draft was evaluated: %q", human)
	}

	before := len(auditRecords(t, configPath))
	rehearsed := evaluateJSON(t, "--pack-id", "intake", "--rehearsal", "--config", configPath, "--facts", facts)
	if !rehearsed.Rehearsal || rehearsed.Reviewed != nil || rehearsed.ReviewedSet != nil {
		t.Fatalf("a rehearsal consulted no reviewed set and says nothing about one: %+v", rehearsed)
	}
	if after := len(auditRecords(t, configPath)); after != before {
		t.Fatalf("a rehearsal leaves no record: %d then %d", before, after)
	}
}

// lastRecord is the newest line of a project's trail.
func lastRecord(t *testing.T, configPath string) map[string]any {
	t.Helper()
	records := auditRecords(t, configPath)
	return records[len(records)-1]
}

// sameAsRecord holds a payload's reviewed and reviewedSet to the record the
// same run wrote, member by member: present in both or neither, and equal.
func sameAsRecord(t *testing.T, reviewed *bool, set *result.ReviewedSet, record map[string]any) {
	t.Helper()
	sameReviewedAsRecord(t, reviewed, set, record)
}

// sameReviewedAsRecord compares on the record's own keys, so a member present
// as null, or of another type, is not mistaken for an absent one: reviewed
// present exactly when the payload's is, and a Boolean equal to it; reviewedSet
// present exactly when the payload's is, and an object of exactly its three
// members, each equal.
func sameReviewedAsRecord(t *testing.T, reviewed *bool, set *result.ReviewedSet, record map[string]any) {
	t.Helper()
	recordedReviewed, hasReviewed := record["reviewed"]
	if hasReviewed != (reviewed != nil) {
		t.Fatalf("payload reviewed %v, record %v (present %v)", reviewed, recordedReviewed, hasReviewed)
	}
	if reviewed != nil {
		if flag, isBool := recordedReviewed.(bool); !isBool || flag != *reviewed {
			t.Fatalf("payload reviewed %v, record %#v", *reviewed, recordedReviewed)
		}
	}
	rawSet, hasSet := record["reviewedSet"]
	if hasSet != (set != nil) {
		t.Fatalf("payload reviewedSet %+v, record %#v (present %v)", set, rawSet, hasSet)
	}
	if set == nil {
		return
	}
	recordedSet, isObject := rawSet.(map[string]any)
	if !isObject || len(recordedSet) != 3 || recordedSet["lockDigest"] != set.LockDigest ||
		recordedSet["lockVersion"] != set.LockVersion || recordedSet["configDigest"] != set.ConfigDigest {
		t.Fatalf("payload reviewedSet %+v, record %#v", set, rawSet)
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
		`"configVersion": "2",`, `"configVersion": "4",`+"\n"+`  "requireReviewed": true,`+"\n"+`  "audit": {"dir": "audit"},`, 1)
	configPath := writeProjectFixture(t, config, map[string]string{
		"sanctions-screening-0.1.0.pack.json": graphFixture(t, "sanctions-screening-0.1.0.pack.json"),
		"vendor-onboarding-0.1.0.pack.json":   graphFixture(t, "vendor-onboarding-0.1.0.pack.json"),
		"onboarding.graph.json":               graphFixture(t, "onboarding.graph.json"),
		"onboarding.rows.json":                graphFixture(t, "onboarding.rows.json"),
	})
	graphPath := filepath.Join(filepath.Dir(configPath), "onboarding.graph.json")
	inputs := writeGraphInputs(t, graphHappyInputs)

	// With no lock, even the declared graph is refused: there is no reviewed
	// set to apply.
	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
		"--config", configPath, "--inputs", inputs}, "")
	if code != result.ExitInvalid || !strings.Contains(stderr, "there is no reviewed-set lock") || strings.Contains(stdout, "disposition") {
		t.Fatalf("a graph run with no lock is refused: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	mustLock(t, configPath)
	code, stdout, stderr = runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
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
	// The composite's record is the run's last line.
	sameAsRecord(t, composite.Reviewed, composite.ReviewedSet, lastRecord(t, configPath))

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

// A pack named by decision id that is over the byte limit is declared law whose
// bytes never arrived, not a draft: in a project that requires review and has
// a lock, the evaluator's own byte-limit refusal answers; with no lock, the
// requirement refuses it for having none.
func TestAnOversizedDeclaredPackIsNotCalledADraft(t *testing.T) {
	configPath := requireReviewedProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	packPath := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")
	body, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatal(err)
	}
	oversized := append(append([]byte{}, body[:len(body)-1]...), []byte(`,"x-padding":"`+strings.Repeat("a", 11<<20)+`"}`)...)

	if err := os.WriteFile(packPath, oversized, 0o600); err != nil {
		t.Fatal(err)
	}
	refusedForReview(t, "there is no reviewed-set lock", "--pack-id", "intake", "--config", configPath, "--facts", facts)

	if err := os.WriteFile(packPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	mustLock(t, configPath)
	if err := os.WriteFile(packPath, oversized, 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--format", "json", "--pack-id", "intake", "--config", configPath, "--facts", facts}, "")
	var refused struct {
		EvaluationError struct {
			Class string `json:"class"`
			Phase string `json:"phase"`
		} `json:"evaluationError"`
		Diagnostics []struct {
			Code string `json:"code"`
		} `json:"diagnostics"`
		Disposition any `json:"disposition"`
	}
	if err := json.Unmarshal([]byte(stdout), &refused); err != nil {
		t.Fatalf("undecodable refusal %.300q: %v", stdout, err)
	}
	if code != result.ExitIO || refused.EvaluationError.Class != "pack-not-conformant" || refused.EvaluationError.Phase != "preflight" ||
		len(refused.Diagnostics) != 1 || refused.Diagnostics[0].Code != "JPS-RESOURCE-INPUT-BYTE-LIMIT" || refused.Disposition != nil {
		t.Fatalf("the byte limit answers, not the requirement: exit=%d refusal=%+v stderr=%q", code, refused, stderr)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)); err == nil {
		t.Fatal("a refused evaluation leaves no record")
	}
}
