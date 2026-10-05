package cli

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// cliWitness signs statements as a checkpoint witness does. The bytes are
// spelled out here from the gateway's SPEC.md §8.3, not built by the audit
// package, so a change to the bytes the runtime reads fails a test that names
// them. Its seed signs nothing real.
type cliWitness struct {
	private    ed25519.PrivateKey
	public     ed25519.PublicKey
	lines      []string
	signatures []string
}

func newCLIWitness(name string) *cliWitness {
	seed := sha256.Sum256([]byte("a test witness, which signs nothing real: " + name))
	private := ed25519.NewKeyFromSeed(seed[:])
	return &cliWitness{private: private, public: private.Public().(ed25519.PublicKey)}
}

// witnessedAt is the time the test witness states for the statement at an
// index.
func witnessedAt(index int) string { return fmt.Sprintf("2026-10-05T12:%02d:00Z", index) }

// sign signs a statement of a kind at the chain's next index, for a
// checkpoint as audit checkpoint prints it, without its newline.
func (w *cliWitness) sign(kind, checkpoint string) {
	index := len(w.lines)
	previous := "null"
	if index > 0 {
		previous = `"` + w.signatures[index-1] + `"`
	}
	head := fmt.Sprintf(`{"checkpoint":%s,"index":%d,"keyId":"%s","kind":"%s","prevSignature":%s`, checkpoint, index, audit.KeyID(w.public), kind, previous)
	tail := fmt.Sprintf(`"witnessVersion":"1","witnessedAt":"%s"}`, witnessedAt(index))
	signature := hex.EncodeToString(ed25519.Sign(w.private, []byte("judgment-pack-gateway/witness/1:"+head+","+tail)))
	w.lines = append(w.lines, head+`,"signature":"`+signature+`",`+tail)
	w.signatures = append(w.signatures, signature)
}

// file writes the statements from index from to index to, exclusive, one
// line each, and returns its path.
func (w *cliWitness) file(t *testing.T, name string, from, to int) string {
	t.Helper()
	return writeDocument(t, name, strings.Join(w.lines[from:to], "\n")+"\n")
}

// keyFile writes the witness's public key, and returns its path.
func (w *cliWitness) keyFile(t *testing.T) string {
	t.Helper()
	return writeDocument(t, "witness.pub", hex.EncodeToString(w.public)+"\n")
}

// trailCheckpoints is every checkpoint of a project's trail, one canonical
// line each, as audit checkpoint --since 0 prints them.
func trailCheckpoints(t *testing.T, configPath string) []string {
	t.Helper()
	code, stdout, stderr := runTest(t, []string{"audit", "checkpoint", "--config", configPath, "--since", "0"}, "")
	if code != 0 {
		t.Fatalf("exit=%d %q", code, stderr)
	}
	return strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
}

// The fixed sentences of a witness's statements (gateway ADR-0013 §6), spelled
// out here so a change to the text a reader mirrors verbatim fails a test that
// names it.
const (
	notAgainstWitnessSentence = "Anything against a witness that is not independent of the operator: one that colludes can sign what it is asked, at any time it states, and a second history for another audience; a key supplied is trusted because the verifier chose it."
	notSubmitterSentence      = "Who submitted any checkpoint: a statement does not name its submitter, and the witness cannot tell the trail's operator from a holder of the operator's credential."
	notWitnessTimeSentence    = "When any record was made: the time a witness states is its own clock's, for when it held the checkpoint."
)

// endsWith reports whether a list ends with the items of tail, in order.
func endsWith(list []string, tail ...string) bool {
	return len(list) >= len(tail) && strings.Join(list[len(list)-len(tail):], "\n") == strings.Join(tail, "\n")
}

// audit verify reads a witness's chain for the trail under the key the reader
// trusts, to the head it fetched, credits it, and saves a continuation; the
// next reading goes on from that continuation with only what the witness
// signed since. A step that fails leaves the continuation as it was.
func TestAuditVerifyReadsAWitnessesChainAndContinuesIt(t *testing.T) {
	configPath, _ := recordedProject(t, 2)
	checkpoints := trailCheckpoints(t, configPath)
	w := newCLIWitness("chain")
	w.sign("checkpoint", checkpoints[0])
	w.sign("checkpoint", checkpoints[1])
	key := w.keyFile(t)
	statements := w.file(t, "statements.jsonl", 0, 2)
	head := w.file(t, "head.jsonl", 1, 2)
	saved := filepath.Join(t.TempDir(), "continuation.json")

	code, output := verification(t, "--config", configPath, "--witness-key", key, "--witness", statements, "--witness-head", head,
		"--witness-save", saved, "--require-countersigned-through", "2")
	section := output.Witness
	if code != 0 || output.Status != "valid" || output.Coverage.Countersigned != (result.AuditCoverageState{Status: "through", Through: 2}) ||
		output.Coverage.Checkpointed.Through != 2 || output.Coverage.Witnessed != 2 || output.RequiredCountersigned.Status != "met" ||
		section == nil || section.Status != "read" || section.Reading != "current" || *section.HeadIndex != 1 || *section.HighestIndex != 1 ||
		section.StatementsRead != 3 || section.StatementsChecked != 2 || section.KeysSupplied != 1 || section.Began != "index-0" || !section.ContinuationSaved {
		t.Fatalf("exit=%d %+v %+v %+v", code, output.Coverage, section, output.Findings)
	}
	if want := "Lines 1 to 2 are the lines that existed when a witness under a key supplied signed its statement for checkpoint 2, which it states it did at " + witnessedAt(1) + ", if that witness is independent of the trail's operator."; output.Establishes[len(output.Establishes)-1] != want {
		t.Fatalf("establishes %q", output.Establishes)
	}
	if !endsWith(output.DoesNotEstablish,
		"Lines after 2 are covered by no statement of a witness under a key supplied.",
		"That the witness's head for this trail is still index 1: the head supplied is as current as the reader's fetch of it, and a signature does not say when it was fetched.",
		notAgainstWitnessSentence, notSubmitterSentence, notWitnessTimeSentence, attemptsSentence) {
		t.Fatalf("doesNotEstablish %q", output.DoesNotEstablish)
	}
	continuation, err := os.ReadFile(saved)
	if err != nil || string(continuation) != `{"continuationVersion":"1","last":`+w.lines[1]+`,"latestCheckpoint":`+w.lines[1]+"}\n" {
		t.Fatalf("the continuation saved: %q %v", continuation, err)
	}

	code, human, _ := runTest(t, []string{"audit", "verify", "--config", configPath, "--witness-key", key, "--witness", statements,
		"--require-countersigned-through", "2"}, "")
	for _, line := range []string{
		"consistent, and witnessed through the checkpoint a witness statement countersigns at sequence 2: 2 line(s)",
		"witness: 2 statement line(s) read, 2 statement(s) checked, 1 key(s) supplied; read from index 0",
		"witness reading: historical, ending at the highest index supplied, index 1",
		"latest checkpoint statement: index 1, sequence 2, witnessed at " + witnessedAt(1) + " by the witness's clock",
		"countersigned: through sequence 2",
		"required: every record through sequence 2 countersigned: met",
		"NOT ESTABLISHED: That the witness held no statement for this trail after index 1: no head fetched from the witness was supplied, so the chain was read only as far as it was supplied.",
	} {
		if code != 0 || !strings.Contains(human, line+"\n") {
			t.Fatalf("human output, a historical reading, lacks %q: exit=%d %q", line, code, human)
		}
	}
	if !humanEndsWithAttempts(human) {
		t.Fatalf("human output does not end with the sentence on attempts: %q", human)
	}

	// A third record, and the witness signs its checkpoint and then a
	// conflict over the first: the reader goes on from its continuation with
	// those two alone, and the head it fetched.
	facts := writeDocument(t, "facts.json", hardFailFacts)
	if code, _, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--facts", facts}, ""); code != 0 {
		t.Fatalf("exit=%d %q", code, stderr)
	}
	checkpoints = trailCheckpoints(t, configPath)
	first, err := audit.ParseCheckpoint([]byte(checkpoints[0]))
	if err != nil {
		t.Fatal(err)
	}
	w.sign("checkpoint", checkpoints[2])
	w.sign("conflict", fmt.Sprintf(`{"checkpointVersion":"1","recordDigest":"sha256:%s","sequence":1,"trail":"%s"}`, strings.Repeat("0", 64), first.Trail))
	newer := w.file(t, "newer.jsonl", 2, 4)
	newerHead := w.file(t, "newer-head.jsonl", 3, 4)
	code, output = verification(t, "--config", configPath, "--witness-key", key, "--witness-resume", saved, "--witness", newer,
		"--witness-head", newerHead, "--witness-save", saved, "--require-countersigned-through", "3")
	section = output.Witness
	if code != 0 || output.Coverage.Countersigned.Through != 3 || section.Began != "continued" || *section.ContinuedAfter != 1 ||
		*section.HeadIndex != 3 || section.ConflictsTotal != 1 || section.Conflicts[0] != 1 || section.StatementsRead != 5 || !section.ContinuationSaved {
		t.Fatalf("resumed: exit=%d %+v %+v %+v", code, output.Coverage, section, output.Findings)
	}
	if !endsWith(output.DoesNotEstablish,
		"Lines after 3 are covered by no statement of a witness under a key supplied.",
		"That the witness's head for this trail is still index 3: the head supplied is as current as the reader's fetch of it, and a signature does not say when it was fetched.",
		"Anything about statements up to index 1, which this reading did not read: it continued from a continuation supplied as the reader's own earlier successful reading, which the runtime cannot tell from one someone else wrote, and is as complete as that reading was.",
		"Which of two records is the trail's at the sequence of each of the 1 conflict statement(s) read, the first at sequence 1: a submitter the witness allowed for the trail offered there another record than the one the witness held, and a conflict statement says neither which of the two is the trail's nor who that submitter was, beyond the witness's own registration.",
		notAgainstWitnessSentence, notSubmitterSentence, notWitnessTimeSentence, attemptsSentence) {
		t.Fatalf("doesNotEstablish %q", output.DoesNotEstablish)
	}
	continuation, err = os.ReadFile(saved)
	if err != nil || string(continuation) != `{"continuationVersion":"1","last":`+w.lines[3]+`,"latestCheckpoint":`+w.lines[2]+"}\n" {
		t.Fatalf("the continuation saved: %q %v", continuation, err)
	}

	// A step that fails saves nothing: the continuation stays as it was.
	for _, failing := range [][]string{
		{"--witness-head", w.file(t, "stale-head.jsonl", 1, 2)},
		{"--require-countersigned-through", "4"},
		{"--witness", w.file(t, "again.jsonl", 3, 4)},
	} {
		args := append([]string{"audit", "verify", "--format", "json", "--config", configPath, "--witness-key", key, "--witness-resume", saved, "--witness-save", saved}, failing...)
		code, stdout, _ := runTest(t, args, "")
		after, err := os.ReadFile(saved)
		if code == 0 || err != nil || string(after) != string(continuation) {
			t.Fatalf("%v: exit=%d, the continuation changed: %q", failing, code, stdout)
		}
	}
	code, output = verification(t, "--config", configPath, "--witness-key", key, "--witness-resume", saved, "--witness-head", w.file(t, "stale.jsonl", 1, 2))
	if code != result.ExitInvalid || len(output.Findings) == 0 || output.Findings[0].Name != audit.FindingWitnessHeadBehind || output.Coverage.Countersigned.Status != "failed" ||
		!endsWith(output.DoesNotEstablish, "That any line is covered by a statement of a witness under a key supplied: no statement that was read is credited with one.",
			notAgainstWitnessSentence, notSubmitterSentence, notWitnessTimeSentence, attemptsSentence) {
		t.Fatalf("a stale head: exit=%d %+v %q", code, output.Findings, output.DoesNotEstablish)
	}
	code, output = verification(t, "--config", configPath, "--witness-key", key, "--witness-resume", saved, "--require-countersigned-through", "4")
	if code != result.ExitInvalid || output.RequiredCountersigned.Status != "unmet" || len(output.Findings) != 1 ||
		output.Findings[0] != (result.AuditFinding{Name: audit.FindingCountersignedCoverageMissing, Line: 4, Detail: "no credited witness statement covers the records up to sequence 4: they are not countersigned"}) {
		t.Fatalf("an unmet requirement: exit=%d %+v", code, output.Findings)
	}
}

// The witness flags need a key to read statements by, and a reading over a
// bound, or with a key the key rule refuses, is refused before any statement
// is checked; a refused reading saves nothing.
func TestAuditVerifyWitnessFlagsAreChecked(t *testing.T) {
	configPath, _ := recordedProject(t, 1)
	checkpoints := trailCheckpoints(t, configPath)
	w := newCLIWitness("flags")
	w.sign("checkpoint", checkpoints[0])
	key := w.keyFile(t)
	statement := w.file(t, "statement.jsonl", 0, 1)
	run := func(args ...string) (int, string) {
		t.Helper()
		code, stdout, _ := runTest(t, append([]string{"audit", "verify", "--format", "json", "--config", configPath}, args...), "")
		return code, stdout
	}
	for _, args := range [][]string{
		{"--witness", statement},
		{"--witness-head", statement},
		{"--witness-resume", statement},
		{"--witness-save", filepath.Join(t.TempDir(), "saved")},
		{"--require-countersigned-through", "1"},
	} {
		if code, stdout := run(args...); code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-INVOCATION-AUDIT-WITNESS"`) {
			t.Fatalf("%v: exit=%d %q", args, code, stdout)
		}
	}
	if code, stdout := run("--witness-key", key, "--require-countersigned-through", "-1"); code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-INVOCATION-AUDIT-REQUIRE"`) {
		t.Fatalf("a negative requirement: exit=%d %q", code, stdout)
	}
	// Seventeen keys are refused before any is read: none of these exists.
	keys := []string{}
	for index := range audit.MaxWitnessKeys + 1 {
		keys = append(keys, "--witness-key", filepath.Join(t.TempDir(), fmt.Sprintf("absent-%d.pub", index)))
	}
	if code, stdout := run(keys...); code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-AUDIT-WITNESS-REFUSED"`) || !strings.Contains(stdout, "keys-over-bound") {
		t.Fatalf("seventeen keys: exit=%d %q", code, stdout)
	}
	if code, stdout := run(keys[2:]...); code != result.ExitIO || !strings.Contains(stdout, `"JPS-AUDIT-WITNESS-KEY-READ"`) {
		t.Fatalf("sixteen keys are read: exit=%d %q", code, stdout)
	}
	for _, refused := range []struct{ key, reason string }{
		{strings.Repeat("0", 64), "(key-small-order)"},
		{"ed" + strings.Repeat("f", 60) + "7f", "(key-not-canonical)"},
		{"02" + strings.Repeat("0", 62), "(key-not-on-curve)"},
		{"not a key", "is not one: a public key is 64 hexadecimal characters"},
	} {
		code, stdout := run("--witness-key", writeDocument(t, "refused.pub", refused.key+"\n"), "--witness", statement)
		if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-AUDIT-WITNESS-KEY-INVALID"`) || !strings.Contains(stdout, refused.reason) {
			t.Fatalf("%s: exit=%d %q", refused.key, code, stdout)
		}
	}
	// The files' sizes are bounded together before any is read: a sparse
	// file of the bound and a head of one byte are over it.
	sparse := filepath.Join(t.TempDir(), "sparse.jsonl")
	if err := os.WriteFile(sparse, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(sparse, audit.MaxWitnessBytes); err != nil {
		t.Fatal(err)
	}
	saved := filepath.Join(t.TempDir(), "saved.json")
	if code, stdout := run("--witness-key", key, "--witness", sparse, "--witness-head", writeDocument(t, "byte.jsonl", "x"), "--witness-save", saved); code != result.ExitInvocation ||
		!strings.Contains(stdout, `"JPS-AUDIT-WITNESS-REFUSED"`) || !strings.Contains(stdout, "(bytes-over-bound): the witness files hold 67108865 bytes together, more than 67108864.") {
		t.Fatalf("over the byte bound, by the files' sizes: exit=%d %q", code, stdout)
	}
	if _, err := os.Stat(saved); !os.IsNotExist(err) {
		t.Fatalf("a refused reading saved a continuation: %v", err)
	}
	many := writeDocument(t, "many.jsonl", strings.Repeat("x\n", audit.MaxWitnessStatements+1))
	if code, stdout := run("--witness-key", key, "--witness", many); code != result.ExitInvocation || !strings.Contains(stdout, "statements-over-bound") {
		t.Fatalf("over the statement bound: exit=%d %q", code, stdout)
	}
	code, output := verification(t, "--config", configPath, "--witness-key", key, "--witness", statement, "--witness-save", saved)
	if code != 0 || !output.Witness.ContinuationSaved {
		t.Fatalf("exit=%d %+v", code, output.Witness)
	}
	if code, stdout := run("--witness-key", key, "--witness-resume", saved, "--witness", statement); code != result.ExitInvocation || !strings.Contains(stdout, "statement-before-continuation") {
		t.Fatalf("a statement at the continuation's last: exit=%d %q", code, stdout)
	}
	for _, args := range [][]string{{"--witness", "-"}, {"--witness-save", "-"}, {"--witness-head", "https://witness.example/head"}} {
		if code, stdout := run(append([]string{"--witness-key", key}, args...)...); code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-INVOCATION-INPUT"`) {
			t.Fatalf("%v: exit=%d %q", args, code, stdout)
		}
	}
	if code, stdout := run("--witness-key", key, "--witness", filepath.Join(t.TempDir(), "absent.jsonl")); code != result.ExitIO || !strings.Contains(stdout, `"JPS-AUDIT-WITNESS-READ"`) {
		t.Fatalf("an absent statements file: exit=%d %q", code, stdout)
	}
	// A statement of another trail: the identity is the trail's own.
	other := newCLIWitness("other trail")
	first, err := audit.ParseCheckpoint([]byte(checkpoints[0]))
	if err != nil {
		t.Fatal(err)
	}
	another := strings.Repeat("f", 32)
	if first.Trail == another {
		another = strings.Repeat("e", 32)
	}
	other.sign("checkpoint", fmt.Sprintf(`{"checkpointVersion":"1","recordDigest":"%s","sequence":1,"trail":"%s"}`, first.RecordDigest, another))
	code, output = verification(t, "--config", configPath, "--witness-key", other.keyFile(t), "--witness", other.file(t, "other.jsonl", 0, 1))
	if code != result.ExitInvalid || len(output.Findings) == 0 || output.Findings[0].Name != audit.FindingWitnessTrailMismatch || output.Coverage.Countersigned.Status != "failed" {
		t.Fatalf("another trail: exit=%d %+v", code, output.Findings)
	}
}
