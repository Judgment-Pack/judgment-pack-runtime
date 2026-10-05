package audit

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// testWitness signs statements for a trail under a key of its own, as a
// witness does: each at its chain's next index, naming the signature before
// it. Its seed is derived from a name and signs nothing real.
type testWitness struct {
	private    ed25519.PrivateKey
	public     ed25519.PublicKey
	statements []*witnessStatement
}

func newTestWitness(name string) *testWitness {
	seed := sha256.Sum256([]byte("a test witness, which signs nothing real: " + name))
	private := ed25519.NewKeyFromSeed(seed[:])
	return &testWitness{private: private, public: private.Public().(ed25519.PublicKey)}
}

// sign fills in a statement's keyId and signature under the witness's key.
func (w *testWitness) sign(statement *witnessStatement) *witnessStatement {
	statement.keyID = KeyID(w.public)
	statement.signature = hex.EncodeToString(ed25519.Sign(w.private, statement.signed()))
	statement.whole = sha256.Sum256([]byte(statement.canonical(true)))
	return statement
}

// next signs a statement of a kind at the chain's next index.
func (w *testWitness) next(kind string, checkpoint result.AuditCheckpoint) *witnessStatement {
	index := len(w.statements)
	statement := &witnessStatement{kind: kind, checkpoint: checkpoint, index: int64(index),
		witnessedAt: fmt.Sprintf("2026-10-05T%02d:%02d:00Z", index/60%24, index%60)}
	if index > 0 {
		statement.prev = w.statements[index-1].signature
	}
	w.statements = append(w.statements, w.sign(statement))
	return statement
}

// file is the statements from index from to index to, exclusive, as a witness
// serves them: one canonical line each.
func (w *testWitness) file(from, to int) []byte {
	var out bytes.Buffer
	for _, statement := range w.statements[from:to] {
		out.WriteString(statement.canonical(true))
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// all is every statement the witness signed, in one file.
func (w *testWitness) all() []byte { return w.file(0, len(w.statements)) }

// supplied is a reading of files under the witness's key.
func (w *testWitness) supplied(files ...[]byte) WitnessSupplied {
	return WitnessSupplied{Keys: [][]byte{w.public}, Statements: files}
}

// witnessTrail is a trail of n chained records of one identity, linked as
// the writer links them; content gives each record a member of its own, so
// two trails can share their first lines and differ after them.
func witnessTrail(identity string, n int, content func(int) string) []byte {
	var out bytes.Buffer
	previous := Digest(nil)
	for sequence := 1; sequence <= n; sequence++ {
		line := fmt.Sprintf(`{"n":%q,"previous":%q,"sequence":%d,"trail":%q}`, content(sequence), previous, sequence, identity)
		out.WriteString(line)
		out.WriteByte('\n')
		previous = Digest([]byte(line))
	}
	return out.Bytes()
}

const (
	witnessedTrail = "5a17e3c09b8d4f2e6a1c3b5d7f9e0a2c"
	anotherTrail   = "0f1e2d3c4b5a69788796a5b4c3d2e1f0"
)

func plainRecords(sequence int) string { return fmt.Sprintf("record %d", sequence) }

// lineCheckpoint is the checkpoint of a trail's line at sequence.
func lineCheckpoint(trail []byte, sequence int64) result.AuditCheckpoint {
	line := splitLines(trail)[sequence-1]
	found, _, _ := readLink(line)
	return result.AuditCheckpoint{CheckpointVersion: result.CheckpointVersion, RecordDigest: Digest(line), Sequence: sequence, Trail: found.trail}
}

// otherRecord is a checkpoint for a sequence naming another record there.
func otherRecord(trail string, sequence int64) result.AuditCheckpoint {
	return result.AuditCheckpoint{CheckpointVersion: result.CheckpointVersion, RecordDigest: Digest([]byte(fmt.Sprintf("another record %d", sequence))), Sequence: sequence, Trail: trail}
}

// witnessVerify verifies a trail held in memory with a witness's statements,
// and a holder's checkpoints when any, refusing the test when the statements
// are refused.
func witnessVerify(t *testing.T, trail []byte, supplied WitnessSupplied, require int64, held ...result.AuditCheckpoint) Report {
	t.Helper()
	input, err := PrepareWitness(supplied)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	report, err := Verify(bytes.NewReader(trail), int64(len(trail)), Options{Held: held, Witness: &WitnessOptions{Input: input, RequireThrough: require}})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// refusalOf is the reason a reading was refused, or "".
func refusalOf(supplied WitnessSupplied) string {
	_, err := PrepareWitness(supplied)
	var refusal *WitnessRefusal
	if errors.As(err, &refusal) {
		return refusal.Reason
	}
	return ""
}

// outcome is what a verification answered, as one short string: its status,
// its findings by name and line, at most ten, and its coverage. Two readings
// that answer alike have the same outcome, whatever the details say.
func outcome(report Report) string {
	chain := report.Chain
	names := []string{}
	for _, finding := range chain.Findings[:min(len(chain.Findings), 10)] {
		names = append(names, fmt.Sprintf("%s@%d", finding.Name, finding.Line))
	}
	return fmt.Sprintf("%s findings=%d%v checkpointed=%s/%d witnessed=%d countersigned=%s/%d", chain.Status, chain.FindingsTotal, names,
		chain.Coverage.Checkpointed.Status, chain.Coverage.Checkpointed.Through, chain.Coverage.Witnessed,
		chain.Coverage.Countersigned.Status, chain.Coverage.Countersigned.Through)
}

// firstDetail is the detail of a report's first finding, or "" when it has
// none, so a test that reads it never fails by a panic.
func firstDetail(report Report) string {
	if len(report.Chain.Findings) == 0 {
		return ""
	}
	return report.Chain.Findings[0].Detail
}

// lowerStatementBound lowers the statement bound for one test.
func lowerStatementBound(t *testing.T, bound int) {
	t.Helper()
	original := witnessStatementBound
	witnessStatementBound = bound
	t.Cleanup(func() { witnessStatementBound = original })
}

// readInSteps reads a witness's statements in steps of at most step new
// statements, under a statement bound lowered to that and the continuation's
// two, each step resuming from the continuation the step before saved. It
// answers every step's report, and stops at the first that saves none.
func readInSteps(t *testing.T, trail []byte, w *testWitness, step int) []Report {
	t.Helper()
	lowerStatementBound(t, step+2)
	reports := []Report{}
	var resume []byte
	for from := 0; from < len(w.statements); {
		to := min(from+step, len(w.statements))
		supplied := w.supplied(w.file(from, to))
		if resume != nil {
			supplied.Resume, supplied.HasResume = resume, true
		}
		report := witnessVerify(t, trail, supplied, 0)
		reports = append(reports, report)
		if report.Continuation == nil {
			break
		}
		resume, from = report.Continuation, to
	}
	return reports
}

// A chain read in steps, each continuing from what the step before saved,
// answers as the whole chain read at once does: the same findings and the
// same coverage (gateway ADR-0013 §6). A conflict at 100 after checkpoints at
// 100 and 200, continued from index 1, passes as it does whole; a conflict
// above the latest checkpoint fails either way; and a long run of conflicts
// after the last checkpoint, which no one step holds, is read through.
func TestAChainReadInStepsAnswersAsAWholeReadingDoes(t *testing.T) {
	cases := []struct {
		name    string
		records int
		build   func(w *testWitness, trail []byte)
		step    int
		want    string
	}{
		{"a conflict at 100 after checkpoints at 100 and 200", 200, func(w *testWitness, trail []byte) {
			w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 100))
			w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 200))
			w.next(WitnessKindConflict, otherRecord(witnessedTrail, 100))
		}, 2, "valid findings=0[] checkpointed=through/200 witnessed=200 countersigned=through/200"},
		{"a conflict above the latest checkpoint", 30, func(w *testWitness, trail []byte) {
			w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 10))
			w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 20))
			w.next(WitnessKindConflict, otherRecord(witnessedTrail, 30))
		}, 2, "invalid findings=1[witness-chain-broken@0] checkpointed=failed/0 witnessed=0 countersigned=failed/0"},
		{"a long run of conflicts after the last checkpoint", 25, func(w *testWitness, trail []byte) {
			w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 25))
			for sequence := int64(1); sequence <= 20; sequence++ {
				w.next(WitnessKindConflict, otherRecord(witnessedTrail, sequence))
			}
		}, 3, "valid findings=0[] checkpointed=through/25 witnessed=25 countersigned=through/25"},
	}
	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			trail := witnessTrail(witnessedTrail, each.records, plainRecords)
			w := newTestWitness(each.name)
			each.build(w, trail)
			whole := witnessVerify(t, trail, w.supplied(w.all()), 0)
			if got := outcome(whole); got != each.want {
				t.Fatalf("whole: %s, want %s", got, each.want)
			}
			steps := readInSteps(t, trail, w, each.step)
			if got := outcome(steps[len(steps)-1]); got != each.want {
				t.Fatalf("in %d steps: %s, want %s", len(steps), got, each.want)
			}
			if len(steps) < 2 || steps[1].Chain.Witness.Began != "continued" {
				t.Fatalf("the chain was not read in steps: %d", len(steps))
			}
			for index, step := range steps[:len(steps)-1] {
				if step.Continuation == nil || step.Chain.FindingsTotal != 0 {
					t.Fatalf("step %d saved no continuation: %s", index, outcome(step))
				}
			}
		})
	}
	// A whole reading of the long run, under the bound a step had, is refused
	// rather than read in part: the steps are what read it.
	lowerStatementBound(t, 5)
	w := newTestWitness("a long run of conflicts after the last checkpoint")
	trail := witnessTrail(witnessedTrail, 25, plainRecords)
	w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 25))
	for sequence := int64(1); sequence <= 20; sequence++ {
		w.next(WitnessKindConflict, otherRecord(witnessedTrail, sequence))
	}
	if reason := refusalOf(w.supplied(w.all())); reason != RefusalStatementsOverBound {
		t.Fatalf("a whole reading over the lowered bound: %q", reason)
	}
}

// continued is a reading that resumes from a continuation of the witness's
// statements: the one whose last is at index last and whose latest checkpoint
// statement is at index latest.
func (w *testWitness) continued(last, latest int, files ...[]byte) WitnessSupplied {
	supplied := w.supplied(files...)
	supplied.Resume, supplied.HasResume = encodeContinuation(w.statements[last], w.statements[latest]), true
	return supplied
}

// withHead adds a head file to a reading.
func withHead(supplied WitnessSupplied, head []byte) WitnessSupplied {
	supplied.Head, supplied.HasHead = head, true
	return supplied
}

// A continuation is held to its own rules (gateway ADR-0013 §6): its two
// statements are checked like the others, its latest checkpoint statement is
// a checkpoint statement at or before its last and the latest one, a head is
// judged against it, nothing at or below its last index is supplied, and its
// checkpoint is held against the trail again.
func TestAContinuationIsHeldToItsOwnRules(t *testing.T) {
	trail := witnessTrail(witnessedTrail, 30, plainRecords)
	w := newTestWitness("continuation")
	w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 5))  // 0
	w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 10)) // 1
	w.next(WitnessKindConflict, otherRecord(witnessedTrail, 8))
	w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 20)) // 3
	w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 25)) // 4
	// Another statement at index 3, signed by the same key: a second chain.
	fork := w.sign(&witnessStatement{kind: WitnessKindCheckpoint, checkpoint: lineCheckpoint(trail, 21), index: 3, prev: w.statements[2].signature, witnessedAt: "2026-10-05T23:59:59Z"})
	forkLine := []byte(fork.canonical(true) + "\n")

	t.Run("a head at its last index that is its last", func(t *testing.T) {
		report := witnessVerify(t, trail, withHead(w.continued(3, 3), w.file(3, 4)), 0)
		section := report.Chain.Witness
		if outcome(report) != "valid findings=0[] checkpointed=through/20 witnessed=20 countersigned=through/20" ||
			section.Reading != "current" || *section.HeadIndex != 3 || *section.HighestIndex != 3 || *section.ContinuedAfter != 3 || section.Began != "continued" {
			t.Fatalf("%s %+v", outcome(report), section)
		}
	})
	t.Run("a head at its last index that differs", func(t *testing.T) {
		report := witnessVerify(t, trail, withHead(w.continued(3, 3), forkLine), 0)
		if got := outcome(report); got != "invalid findings=1[witness-equivocation@0] checkpointed=failed/0 witnessed=0 countersigned=failed/0" {
			t.Fatal(got)
		}
	})
	t.Run("a head below its last index", func(t *testing.T) {
		report := witnessVerify(t, trail, withHead(w.continued(3, 3), w.file(2, 3)), 0)
		if got := outcome(report); got != "invalid findings=1[witness-head-behind@0] checkpointed=failed/0 witnessed=0 countersigned=failed/0" {
			t.Fatal(got)
		}
	})
	t.Run("statements at or below its last index", func(t *testing.T) {
		for _, file := range [][]byte{w.file(3, 5), w.file(2, 3), w.file(0, 1)} {
			if reason := refusalOf(w.continued(3, 3, file)); reason != RefusalStatementBeforeContinuation {
				t.Fatalf("refused %q", reason)
			}
		}
		if reason := refusalOf(w.continued(3, 3, w.file(4, 5))); reason != "" {
			t.Fatalf("the statement after its last: refused %q", reason)
		}
	})
	t.Run("its checkpoint held again after the trail copy changed", func(t *testing.T) {
		rewritten := witnessTrail(witnessedTrail, 30, func(sequence int) string {
			if sequence >= 15 {
				return fmt.Sprintf("rewritten %d", sequence)
			}
			return plainRecords(sequence)
		})
		report := witnessVerify(t, rewritten, w.continued(3, 3), 0)
		if got := outcome(report); got != "invalid findings=1[checkpoint-record-mismatch@20] checkpointed=failed/0 witnessed=0 countersigned=none/0" {
			t.Fatal(got)
		}
		shorter := witnessTrail(witnessedTrail, 12, plainRecords)
		if got := outcome(witnessVerify(t, shorter, w.continued(3, 3), 0)); got != "invalid findings=1[checkpoint-beyond-trail@20] checkpointed=failed/0 witnessed=0 countersigned=none/0" {
			t.Fatal(got)
		}
	})
	t.Run("a conflict last, and its latest checkpoint statement before it", func(t *testing.T) {
		report := witnessVerify(t, trail, w.continued(2, 1, w.file(3, 5)), 0)
		if got := outcome(report); got != "valid findings=0[] checkpointed=through/25 witnessed=25 countersigned=through/25" {
			t.Fatal(got)
		}
	})
	// Each rule of the continuation's own is a witness-chain-broken that says
	// which: the walk after it would find some of them otherwise, by another
	// rule.
	for _, each := range []struct {
		name         string
		last, latest int
		why          string
	}{
		{"its latest checkpoint statement of another kind", 2, 2, "the continuation's latest checkpoint statement is not a checkpoint statement at or before its last"},
		{"its latest checkpoint statement above its last", 2, 3, "the continuation's latest checkpoint statement is not a checkpoint statement at or before its last"},
		{"its last a checkpoint statement, and its latest another", 1, 0, "the continuation's last statement is a checkpoint statement, and its latest checkpoint statement is another"},
		{"its last a conflict above its latest", 2, 0, "the continuation's last statement is a conflict above its latest checkpoint statement's sequence"},
	} {
		t.Run(each.name, func(t *testing.T) {
			report := witnessVerify(t, trail, w.continued(each.last, each.latest), 0)
			if got := outcome(report); got != "invalid findings=1[witness-chain-broken@0] checkpointed=failed/0 witnessed=0 countersigned=failed/0" || firstDetail(report) != each.why {
				t.Fatalf("%s: %q", got, firstDetail(report))
			}
		})
	}
	t.Run("a saved statement whose signature fails, or out of shape", func(t *testing.T) {
		good := encodeContinuation(w.statements[3], w.statements[3])
		for _, broken := range []struct{ continuation, want string }{
			{strings.Replace(string(good), `"witnessedAt":"2026-10-05T00:03:00Z"`, `"witnessedAt":"2026-10-05T00:03:01Z"`, 1), FindingWitnessSignatureInvalid},
			{strings.Replace(string(good), `"witnessVersion":"1"`, `"witnessVersion":"2"`, 1), FindingWitnessMalformed},
			{strings.Replace(string(good), `"continuationVersion":"1"`, `"continuationVersion":"2"`, 1), FindingWitnessMalformed},
			{strings.Replace(string(good), `,"latestCheckpoint":`, `,"latest":`, 1), FindingWitnessMalformed},
		} {
			if broken.continuation == string(good) {
				t.Fatal("the continuation was not changed")
			}
			supplied := w.supplied()
			supplied.Resume, supplied.HasResume = []byte(broken.continuation), true
			report := witnessVerify(t, trail, supplied, 0)
			if got, want := outcome(report), "invalid findings=1["+broken.want+"@0] checkpointed=failed/0 witnessed=0 countersigned=failed/0"; got != want || report.Continuation != nil {
				t.Fatalf("%s: %s", broken.want, got)
			}
		}
	})
	t.Run("a step that fails saves nothing", func(t *testing.T) {
		report := witnessVerify(t, trail, withHead(w.continued(3, 3, w.file(4, 5)), w.file(2, 3)), 0)
		if report.Continuation != nil || report.Chain.FindingsTotal == 0 {
			t.Fatalf("a failing step saved a continuation: %s", outcome(report))
		}
		// A finding of the trail alone, with the statements read cleanly,
		// saves nothing either: a continuation needs no finding at all.
		rewritten := witnessTrail(witnessedTrail, 30, func(sequence int) string {
			if sequence == 30 {
				return "rewritten"
			}
			return plainRecords(sequence)
		})
		lines := splitLines(rewritten)
		lines[29] = []byte(strings.Replace(string(lines[29]), `"sequence":30`, `"sequence":31`, 1))
		report = witnessVerify(t, joinLines(lines), w.continued(3, 3, w.file(4, 5)), 0)
		if report.Continuation != nil || report.Chain.Witness.Status != "read" || report.Chain.FindingsTotal == 0 {
			t.Fatalf("a finding of the trail saved a continuation: %s", outcome(report))
		}
	})
	t.Run("the continuation's two statements are counted", func(t *testing.T) {
		lowerStatementBound(t, 5)
		if reason := refusalOf(w.continued(3, 3, []byte("x\nx\nx\n"))); reason != "" {
			t.Fatalf("at the bound: refused %q", reason)
		}
		if reason := refusalOf(w.continued(3, 3, []byte("x\nx\nx\nx\n"))); reason != RefusalStatementsOverBound {
			t.Fatalf("one past the bound: %q", reason)
		}
	})
}

// A continuation saved at a retirement is the end of its chain: read again
// with nothing new it passes, retired, and any statement after it breaks the
// chain.
func TestAContinuationSavedAtARetirementEndsTheChain(t *testing.T) {
	trail := witnessTrail(witnessedTrail, 10, plainRecords)
	w := newTestWitness("retirement")
	w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 4))
	w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 9))
	w.next(WitnessKindRetirement, lineCheckpoint(trail, 9))
	whole := witnessVerify(t, trail, w.supplied(w.all()), 0)
	if whole.Continuation == nil || !whole.Chain.Witness.Retired {
		t.Fatalf("a retired chain read whole: %s", outcome(whole))
	}
	if !bytes.Equal(whole.Continuation, encodeContinuation(w.statements[2], w.statements[1])) {
		t.Fatalf("the continuation saved: %q", whole.Continuation)
	}
	supplied := w.supplied()
	supplied.Resume, supplied.HasResume = whole.Continuation, true
	again := witnessVerify(t, trail, supplied, 0)
	if outcome(again) != "valid findings=0[] checkpointed=through/9 witnessed=9 countersigned=through/9" || !again.Chain.Witness.Retired || again.Continuation == nil {
		t.Fatalf("resumed with nothing new: %s", outcome(again))
	}
	// A continuation whose last is a retirement repeating another checkpoint
	// than its latest checkpoint statement's names the wrong latest.
	forged := w.supplied()
	forged.Resume, forged.HasResume = encodeContinuation(w.statements[2], w.statements[0]), true
	report := witnessVerify(t, trail, forged, 0)
	if got := outcome(report); got != "invalid findings=1[witness-chain-broken@0] checkpointed=failed/0 witnessed=0 countersigned=failed/0" ||
		firstDetail(report) != "the continuation's last statement is a retirement that does not repeat its latest checkpoint statement's checkpoint" {
		t.Fatalf("a retirement last that does not repeat the latest: %s %q", got, firstDetail(report))
	}
	after := w.sign(&witnessStatement{kind: WitnessKindCheckpoint, checkpoint: lineCheckpoint(trail, 10), index: 3, prev: w.statements[2].signature, witnessedAt: "2026-10-05T01:00:00Z"})
	supplied.Statements = [][]byte{[]byte(after.canonical(true) + "\n")}
	if got := outcome(witnessVerify(t, trail, supplied, 0)); got != "invalid findings=1[witness-chain-broken@0] checkpointed=failed/0 witnessed=0 countersigned=failed/0" {
		t.Fatalf("a statement after a retirement: %s", got)
	}
}

// Every checkpoint statement that verifies is held against the trail copy as
// a held checkpoint is, with the same findings, credited or not: a record
// another than the one witnessed, a trail rewritten from an earlier
// statement's sequence on even when the rewrite's own checkpoint was signed
// later, a trail cut short, and a copy of another trail.
func TestAWitnessedCheckpointIsHeldAgainstTheTrailCopy(t *testing.T) {
	original := witnessTrail(witnessedTrail, 12, plainRecords)
	rewrittenFrom := func(from int) []byte {
		return witnessTrail(witnessedTrail, 12, func(sequence int) string {
			if sequence >= from {
				return fmt.Sprintf("rewritten %d", sequence)
			}
			return plainRecords(sequence)
		})
	}
	t.Run("a rewrite caught by an earlier statement", func(t *testing.T) {
		rewritten := rewrittenFrom(3)
		w := newTestWitness("rewrite")
		w.next(WitnessKindCheckpoint, lineCheckpoint(original, 5))
		w.next(WitnessKindCheckpoint, lineCheckpoint(rewritten, 12))
		report := witnessVerify(t, rewritten, w.supplied(w.all()), 0)
		if got := outcome(report); got != "invalid findings=1[checkpoint-record-mismatch@5] checkpointed=failed/0 witnessed=0 countersigned=none/0" {
			t.Fatal(got)
		}
		if detail := firstDetail(report); detail != "the checkpoint of the witness statement at index 0: the record at the checkpoint's sequence is not the one the checkpoint names" {
			t.Fatal(detail)
		}
		// Whoever presents the chain cannot leave the earlier statement
		// out: a chain that begins late is never read from where it begins.
		if got := outcome(witnessVerify(t, rewritten, w.supplied(w.file(1, 2)), 0)); got != "invalid findings=1[witness-chain-broken@0] checkpointed=failed/0 witnessed=0 countersigned=failed/0" {
			t.Fatal(got)
		}
	})
	t.Run("a trail cut short below the latest", func(t *testing.T) {
		w := newTestWitness("cut short")
		w.next(WitnessKindCheckpoint, lineCheckpoint(original, 5))
		w.next(WitnessKindCheckpoint, lineCheckpoint(original, 12))
		shorter := witnessTrail(witnessedTrail, 9, plainRecords)
		if got := outcome(witnessVerify(t, shorter, w.supplied(w.all()), 0)); got != "invalid findings=1[checkpoint-beyond-trail@12] checkpointed=through/5 witnessed=5 countersigned=through/5" {
			t.Fatal(got)
		}
	})
	t.Run("a record other than the one witnessed, and a chain with a finding", func(t *testing.T) {
		w := newTestWitness("other record")
		w.next(WitnessKindCheckpoint, lineCheckpoint(original, 5))
		w.next(WitnessKindCheckpoint, lineCheckpoint(original, 8))
		w.next(WitnessKindCheckpoint, otherRecord(witnessedTrail, 12))
		if got := outcome(witnessVerify(t, original, w.supplied(w.all()), 0)); got != "invalid findings=1[checkpoint-record-mismatch@12] checkpointed=through/8 witnessed=8 countersigned=through/8" {
			t.Fatal(got)
		}
		// With index 1 left out the chain has a hole and is credited
		// nothing, and the checkpoints that verified are still held.
		supplied := w.supplied(w.file(0, 1), w.file(2, 3))
		if got := outcome(witnessVerify(t, original, supplied, 0)); got != "invalid findings=2[witness-chain-broken@0 checkpoint-record-mismatch@12] checkpointed=failed/0 witnessed=0 countersigned=failed/0" {
			t.Fatal(got)
		}
	})
	t.Run("a copy of another trail", func(t *testing.T) {
		w := newTestWitness("another trail")
		w.next(WitnessKindCheckpoint, lineCheckpoint(original, 5))
		other := witnessTrail(anotherTrail, 12, plainRecords)
		report := witnessVerify(t, other, w.supplied(w.all()), 0)
		if got := outcome(report); got != "invalid findings=1[witness-trail-mismatch@0] checkpointed=failed/0 witnessed=0 countersigned=failed/0" {
			t.Fatal(got)
		}
	})
}

// The report says how far a witness reaches: a credited checkpoint joins the
// held ones, countersigned says how far a witness's signature reaches, a
// chain with a finding is credited nothing, and a required countersigned
// coverage is met or not. The sentences are the record's.
func TestAWitnessReportSaysHowFarTheWitnessReaches(t *testing.T) {
	trail := witnessTrail(witnessedTrail, 10, plainRecords)
	w := newTestWitness("report")
	w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 4))
	w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 8))
	w.next(WitnessKindConflict, otherRecord(witnessedTrail, 4))

	t.Run("credited checkpoints join the held ones", func(t *testing.T) {
		report := witnessVerify(t, trail, w.supplied(w.all()), 8, lineCheckpoint(trail, 3))
		chain := report.Chain
		if outcome(report) != "valid findings=0[] checkpointed=through/8 witnessed=8 countersigned=through/8" ||
			chain.Held.Latest.Sequence != 3 || chain.Held.Status != "matched" || chain.Scope != ScopeCheckpoint ||
			chain.RequiredCountersigned.Status != "met" || chain.Coverage.Unwitnessed != 2 {
			t.Fatalf("%s %+v %+v", outcome(report), chain.Held, chain.RequiredCountersigned)
		}
		section := chain.Witness
		if section.Status != "read" || section.Reading != "historical" || section.HeadIndex != nil || *section.HighestIndex != 2 ||
			*section.LatestCheckpoint != (result.AuditWitnessCheckpoint{Index: 1, Sequence: 8, WitnessedAt: "2026-10-05T00:01:00Z"}) ||
			len(section.Conflicts) != 1 || section.Conflicts[0] != 4 || section.ConflictsTotal != 1 || section.Retired ||
			section.KeysSupplied != 1 || section.StatementsRead != 3 || section.StatementsChecked != 3 || section.Began != "index-0" ||
			section.CountersignedAt != "2026-10-05T00:01:00Z" {
			t.Fatalf("%+v", section)
		}
		wantEstablishes := "Lines 1 to 8 are the lines that existed when a witness under a key supplied signed its statement for checkpoint 8, which it states it did at 2026-10-05T00:01:00Z, if that witness is independent of the trail's operator."
		if chain.Establishes[len(chain.Establishes)-1] != wantEstablishes {
			t.Fatalf("establishes %q", chain.Establishes)
		}
		statements := chain.DoesNotEstablish
		tail := []string{
			"Lines after 8 are covered by no statement of a witness under a key supplied.",
			"That the witness held no statement for this trail after index 2: no head fetched from the witness was supplied, so the chain was read only as far as it was supplied.",
			"Which of two records is the trail's at the sequence of each of the 1 conflict statement(s) read, the first at sequence 4: a submitter the witness allowed for the trail offered there another record than the one the witness held, and a conflict statement says neither which of the two is the trail's nor who that submitter was, beyond the witness's own registration.",
			"Anything against a witness that is not independent of the operator: one that colludes can sign what it is asked, at any time it states, and a second history for another audience; a key supplied is trusted because the verifier chose it.",
			"Who submitted any checkpoint: a statement does not name its submitter, and the witness cannot tell the trail's operator from a holder of the operator's credential.",
			"When any record was made: the time a witness states is its own clock's, for when it held the checkpoint.",
			notAttempts,
		}
		if len(statements) < len(tail) || strings.Join(statements[len(statements)-len(tail):], "\n") != strings.Join(tail, "\n") {
			t.Fatalf("doesNotEstablish ends %q", statements[max(0, len(statements)-len(tail)):])
		}
	})
	t.Run("a current reading", func(t *testing.T) {
		report := witnessVerify(t, trail, withHead(w.supplied(w.file(0, 2)), w.file(2, 3)), 0)
		section := report.Chain.Witness
		if section.Reading != "current" || *section.HeadIndex != 2 ||
			!containsString(report.Chain.DoesNotEstablish, "That the witness's head for this trail is still index 2: the head supplied is as current as the reader's fetch of it, and a signature does not say when it was fetched.") {
			t.Fatalf("%+v %q", section, report.Chain.DoesNotEstablish)
		}
	})
	t.Run("a continued reading", func(t *testing.T) {
		report := witnessVerify(t, trail, w.continued(1, 1, w.file(2, 3)), 0)
		if !containsString(report.Chain.DoesNotEstablish, "Anything about statements up to index 1, which this reading did not read: it continued from a continuation supplied as the reader's own earlier successful reading, which the runtime cannot tell from one someone else wrote, and is as complete as that reading was.") {
			t.Fatalf("%q", report.Chain.DoesNotEstablish)
		}
	})
	t.Run("nothing credited on a finding", func(t *testing.T) {
		report := witnessVerify(t, trail, w.supplied(w.all(), []byte("not a statement\n")), 8)
		chain := report.Chain
		if got := outcome(report); got != "invalid findings=2[witness-malformed@0 countersigned-coverage-missing@8] checkpointed=failed/0 witnessed=0 countersigned=failed/0" {
			t.Fatal(got)
		}
		if chain.Witness.Status != "failed" || chain.Witness.Reading != "" || chain.Witness.LatestCheckpoint != nil || chain.Witness.CountersignedAt != "" ||
			chain.RequiredCountersigned.Status != "unmet" || len(chain.Establishes) != 0 ||
			!containsString(chain.DoesNotEstablish, "That any line is covered by a statement of a witness under a key supplied: no statement that was read is credited with one.") ||
			containsString(chain.DoesNotEstablish, "Lines after 8 are covered by no statement of a witness under a key supplied.") {
			t.Fatalf("%+v %q %q", chain.Witness, chain.Establishes, chain.DoesNotEstablish)
		}
		if chain.DoesNotEstablish[len(chain.DoesNotEstablish)-1] != notAttempts {
			t.Fatal("the sentence on attempts is not last")
		}
	})
	t.Run("an unmet requirement", func(t *testing.T) {
		report := witnessVerify(t, trail, w.supplied(w.all()), 9)
		if got := outcome(report); got != "invalid findings=1[countersigned-coverage-missing@9] checkpointed=through/8 witnessed=8 countersigned=through/8" ||
			report.Chain.RequiredCountersigned.Status != "unmet" || report.Continuation != nil {
			t.Fatal(got)
		}
	})
	t.Run("a requirement of checkpoints met by a witness", func(t *testing.T) {
		input, err := PrepareWitness(w.supplied(w.all()))
		if err != nil {
			t.Fatal(err)
		}
		report, err := Verify(bytes.NewReader(trail), int64(len(trail)), Options{RequireThrough: 8, Witness: &WitnessOptions{Input: input}})
		if err != nil || report.Chain.Required.Status != "met" || report.Chain.FindingsTotal != 0 || report.Chain.Held != nil {
			t.Fatalf("%v %s", err, outcome(report))
		}
	})
	t.Run("no witness key, no witness", func(t *testing.T) {
		chain := verifyBytes(t, trail, nil)
		if chain.Coverage.Countersigned != (result.AuditCoverageState{Status: "not-checked", Detail: "no witness key was supplied"}) || chain.Witness != nil || chain.RequiredCountersigned != nil {
			t.Fatalf("%+v", chain.Coverage.Countersigned)
		}
		for _, statement := range append(chain.Establishes, chain.DoesNotEstablish...) {
			if strings.Contains(statement, "witness") {
				t.Fatalf("a report without a witness says %q", statement)
			}
		}
	})
}

// The statements are counted as the files are split, and the split stops at
// the line past the bound, keeping none after it: a file of 4194304 one-byte
// lines costs no more than the bound does.
func TestStatementLinesAreCountedAsTheyAreSplit(t *testing.T) {
	file := bytes.Repeat([]byte("x\n"), 4194304)
	counted, kept := 0, []witnessLine{}
	if splitStatementLines(file, 1, &counted, MaxWitnessStatements, &kept) {
		t.Fatal("4194304 lines were within the bound")
	}
	if got := fmt.Sprintf("stopped at line %d, kept %d lines", counted, len(kept)); got != "stopped at line 110001, kept 110000 lines" {
		t.Fatal(got)
	}
	// Lines that are empty or hold only spaces, tabs and carriage returns
	// are passed over and not counted; the piece after the last newline is a
	// line.
	counted, kept = 0, nil
	if !splitStatementLines([]byte("a\n\n \t\r\nb\r\n  \nc"), 1, &counted, 3, &kept) || counted != 3 ||
		string(kept[0].text) != "a" || string(kept[1].text) != "b\r" || string(kept[2].text) != "c" || kept[2].line != 6 {
		t.Fatalf("counted %d, kept %d", counted, len(kept))
	}
	counted, kept = 0, nil
	if splitStatementLines([]byte("a\nb\nc\nd"), 1, &counted, 3, &kept) || counted != 4 || len(kept) != 3 {
		t.Fatalf("one past: counted %d, kept %d", counted, len(kept))
	}
	// At the bound, with the continuation's two counted first, in the real
	// bound's terms: 109998 statement lines and a continuation are read, and
	// one more line is refused.
	w := newTestWitness("bound")
	trail := witnessTrail(witnessedTrail, 2, plainRecords)
	w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 2))
	lines := bytes.Repeat([]byte("x\n"), MaxWitnessStatements-2)
	if reason := refusalOf(w.continued(0, 0, lines)); reason != "" {
		t.Fatalf("at the bound: %q", reason)
	}
	if reason := refusalOf(w.continued(0, 0, append(lines, "x\n"...))); reason != RefusalStatementsOverBound {
		t.Fatalf("one past the bound: %q", reason)
	}
}

// The refusals come in their order, before any statement is checked: the
// keys' number, each key by the key rule, the bytes, the statements, and a
// statement at or below the continuation's last.
func TestAReadingIsRefusedInItsOrder(t *testing.T) {
	w := newTestWitness("order")
	trail := witnessTrail(witnessedTrail, 2, plainRecords)
	w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 1))
	w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 2))
	smallOrder := make([]byte, 32)
	tooMany := make([][]byte, MaxWitnessKeys+1)
	for index := range tooMany {
		tooMany[index] = smallOrder
	}
	notCanonical, _ := hex.DecodeString("ed" + strings.Repeat("f", 60) + "7f")
	notAPoint, _ := hex.DecodeString("02" + strings.Repeat("0", 62))
	overBytes := bytes.Repeat([]byte(" "), MaxWitnessBytes+1)
	lowerStatementBound(t, 3)
	for _, each := range []struct {
		name     string
		supplied WitnessSupplied
		want     string
	}{
		{"seventeen keys of small order", WitnessSupplied{Keys: tooMany}, RefusalKeysOverBound},
		{"a key of small order before a good one", WitnessSupplied{Keys: [][]byte{smallOrder, w.public}}, RefusalKeySmallOrder},
		{"a key not canonical", WitnessSupplied{Keys: [][]byte{w.public, notCanonical}}, RefusalKeyNotCanonical},
		{"a key that is no point, before the bytes", WitnessSupplied{Keys: [][]byte{notAPoint}, Statements: [][]byte{overBytes}}, RefusalKeyNotOnCurve},
		{"the bytes before the statements", WitnessSupplied{Keys: [][]byte{w.public}, Statements: [][]byte{overBytes[:MaxWitnessBytes-7], []byte("x\nx\nx\nx\n")}}, RefusalBytesOverBound},
		{"the statements before the continuation", w.continued(1, 1, []byte("x\n"), w.file(0, 1)), RefusalStatementsOverBound},
		{"a statement at the continuation's last", w.continued(1, 1, w.file(1, 2)), RefusalStatementBeforeContinuation},
		{"the head is not held to the continuation", withHead(w.continued(1, 1), w.file(0, 1)), ""},
	} {
		if got := refusalOf(each.supplied); got != each.want {
			t.Errorf("%s: refused %q, want %q", each.name, got, each.want)
		}
	}
}

// A statement is held to each form, on the value read, and a chain to each
// rule at its edge: what passes the form just inside it, and what fails just
// outside.
func TestEachFormAndRuleHoldsAtItsEdge(t *testing.T) {
	trail := witnessTrail(witnessedTrail, 10, plainRecords)
	w := newTestWitness("edges")
	w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 3))
	line := w.statements[0].canonical(true)
	for _, each := range []struct {
		name, from, to string
		form           bool
	}{
		{"the largest index", `"index":0`, `"index":9007199254740990`, true},
		{"an index past it", `"index":0`, `"index":9007199254740991`, false},
		{"an index spelled with a fraction", `"index":0`, `"index":0.0`, false},
		{"a previous signature of no form", `"prevSignature":null`, `"prevSignature":"abc"`, false},
		{"a previous signature that is a number", `"prevSignature":null`, `"prevSignature":5`, false},
		{"a previous signature of its form", `"prevSignature":null`, `"prevSignature":"` + strings.Repeat("ab", 64) + `"`, true},
		{"a time of another form", `"witnessedAt":"2026-10-05T00:00:00Z"`, `"witnessedAt":"2026-10-05 00:00:00Z"`, false},
		{"a time no calendar holds, in its form", `"witnessedAt":"2026-10-05T00:00:00Z"`, `"witnessedAt":"2026-13-45T99:99:99Z"`, true},
		{"a keyId in upper case", `"keyId":"` + w.statements[0].keyID, `"keyId":"` + strings.ToUpper(w.statements[0].keyID), false},
		{"another kind", `"kind":"checkpoint"`, `"kind":"other"`, false},
		{"a checkpoint at sequence 0", `"sequence":3`, `"sequence":0`, false},
		{"a member given twice", `"kind":"checkpoint"`, `"kind":"checkpoint","kind":"checkpoint"`, false},
		{"a member's name escaped", `"kind":"checkpoint"`, `"\u006bind":"checkpoint"`, true},
		{"spaces around it", `{"checkpoint"`, ` { "checkpoint"`, true},
	} {
		changed := strings.Replace(line, each.from, each.to, 1)
		if changed == line {
			t.Fatalf("%s: the statement was not changed", each.name)
		}
		if _, ok := parseWitnessStatement([]byte(changed), "edge"); ok != each.form {
			t.Errorf("%s: of its form %v, want %v", each.name, ok, each.form)
		}
	}

	at := func(build func(w *testWitness), head int) string {
		w := newTestWitness("rules")
		build(w)
		supplied := w.supplied(w.file(0, len(w.statements)))
		if head >= 0 {
			supplied = withHead(w.supplied(w.file(0, len(w.statements)-1)), w.file(head, head+1))
		}
		return outcome(witnessVerify(t, trail, supplied, 0))
	}
	if got := at(func(w *testWitness) {
		w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 6))
		w.next(WitnessKindConflict, otherRecord(witnessedTrail, 6))
	}, -1); got != "valid findings=0[] checkpointed=through/6 witnessed=6 countersigned=through/6" {
		t.Errorf("a conflict at the latest checkpoint's own sequence: %s", got)
	}
	if got := at(func(w *testWitness) {
		w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 6))
		w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 5))
	}, -1); got != "invalid findings=1[witness-chain-broken@0] checkpointed=failed/0 witnessed=0 countersigned=failed/0" {
		t.Errorf("a checkpoint sequence below the latest: %s", got)
	}
	if got := at(func(w *testWitness) {
		w.next(WitnessKindRetirement, lineCheckpoint(trail, 6))
	}, -1); got != "invalid findings=1[witness-chain-broken@0] checkpointed=failed/0 witnessed=0 countersigned=failed/0" {
		t.Errorf("a retirement with no checkpoint statement before it: %s", got)
	}
	twoPast := func(w *testWitness) {
		w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 2))
		w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 4))
		w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 6))
		w.next(WitnessKindCheckpoint, lineCheckpoint(trail, 8))
	}
	// Statements 0 to 2 supplied with the head at 3 joins the chain; with
	// statement 2 left out, the head at 3 is two past the others, unreached.
	if got := at(twoPast, 3); got != "valid findings=0[] checkpointed=through/8 witnessed=8 countersigned=through/8" {
		t.Errorf("a head one past: %s", got)
	}
	w2 := newTestWitness("rules")
	twoPast(w2)
	supplied := withHead(w2.supplied(w2.file(0, 2)), w2.file(3, 4))
	if got := outcome(witnessVerify(t, trail, supplied, 0)); got != "invalid findings=1[witness-head-unreached@0] checkpointed=failed/0 witnessed=0 countersigned=failed/0" {
		t.Errorf("a head two past: %s", got)
	}
}
