package audit

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// verifyWith runs Verify over a trail held in memory with the options given.
func verifyWith(t *testing.T, data []byte, options Options) Report {
	t.Helper()
	report, err := Verify(bytes.NewReader(data), int64(len(data)), options)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// listed is the sequences of the checkpoints a report listed.
func listed(report Report) []int64 {
	sequences := []int64{}
	for _, checkpoint := range report.Listed {
		sequences = append(sequences, checkpoint.Sequence)
	}
	return sequences
}

func sameSequences(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// Every chained record after a sequence has its checkpoint listed, in order,
// up to a limit, with whether more follow; a damaged line has none, and a
// discontinuity has one like any chained record. Each is a function of its
// record's bytes, so listing again gives the same bytes: a deliverer's retry
// hands over the same line.
func TestCheckpointsAreListedAfterASequence(t *testing.T) {
	writer, dir := tornTrail(t, 3)
	if _, err := writer.Repair(); err != nil {
		t.Fatal(err)
	}
	writeRecords(t, writer.root, `{"n":4}`)
	data := readTrailFile(t, dir)
	lines := splitLines(data)
	all := verifyWith(t, data, Options{List: true, ListAfter: 0, ListLimit: 10})
	if !sameSequences(listed(all), []int64{1, 2, 3, 5, 6}) || all.More {
		t.Fatalf("listed %v more=%v", listed(all), all.More)
	}
	for _, checkpoint := range all.Listed {
		line := lines[checkpoint.Sequence-1]
		if checkpoint.RecordDigest != Digest(line) || checkpoint.Trail != readChain(t, line).trail || checkpoint.CheckpointVersion != result.CheckpointVersion {
			t.Fatalf("checkpoint %+v is not of line %d", checkpoint, checkpoint.Sequence)
		}
	}
	if head := all.Chain.Head; head == nil || *head != all.Listed[len(all.Listed)-1] {
		t.Fatalf("the last listed is the head: %+v", head)
	}
	page := verifyWith(t, data, Options{List: true, ListAfter: 1, ListLimit: 2})
	if !sameSequences(listed(page), []int64{2, 3}) || !page.More {
		t.Fatalf("a page: %v more=%v", listed(page), page.More)
	}
	next := verifyWith(t, data, Options{List: true, ListAfter: 3, ListLimit: 2})
	if !sameSequences(listed(next), []int64{5, 6}) || next.More {
		t.Fatalf("the next page: %v more=%v", listed(next), next.More)
	}
	if after := verifyWith(t, data, Options{List: true, ListAfter: 6, ListLimit: 2}); len(after.Listed) != 0 || after.More {
		t.Fatalf("nothing after the head: %v", listed(after))
	}
	again := verifyWith(t, data, Options{List: true, ListAfter: 0, ListLimit: 10})
	for index := range all.Listed {
		if !bytes.Equal(EncodeCheckpoint(all.Listed[index]), EncodeCheckpoint(again.Listed[index])) {
			t.Fatal("listing again gives the same bytes")
		}
	}
}

// keptCheckpoints is the checkpoints a holder kept of the records at the
// sequences given.
func keptCheckpoints(t *testing.T, data []byte, sequences ...int64) []result.AuditCheckpoint {
	t.Helper()
	all := verifyWith(t, data, Options{List: true, ListLimit: 1 << 20})
	kept := []result.AuditCheckpoint{}
	for _, wanted := range sequences {
		for _, checkpoint := range all.Listed {
			if checkpoint.Sequence == wanted {
				kept = append(kept, checkpoint)
			}
		}
	}
	if len(kept) != len(sequences) {
		t.Fatalf("kept %d of %v", len(kept), sequences)
	}
	return kept
}

// The checkpoints a holder kept witness the records up to the highest of
// them, in whatever order they are supplied; the chained records after it are
// unwitnessed; and a required coverage is met only up to there.
func TestHeldCheckpointsWitnessTheRecordsTheyCover(t *testing.T) {
	_, _, data := chainedTrail(t, 6)
	kept := keptCheckpoints(t, data, 4, 2)
	report := verifyWith(t, data, Options{Held: kept, RequireThrough: 4})
	chain := report.Chain
	if chain.Status != "valid" || chain.Scope != ScopeCheckpoint || chain.Held == nil ||
		chain.Held.Supplied != 2 || chain.Held.Matched != 2 || chain.Held.Failed != 0 || chain.Held.Status != "matched" ||
		chain.Held.Latest == nil || chain.Held.Latest.Sequence != 4 ||
		chain.Coverage.Checkpointed != (result.AuditCoverageState{Status: "through", Through: 4}) ||
		chain.Coverage.Witnessed != 4 || chain.Coverage.Unwitnessed != 2 ||
		chain.Required == nil || *chain.Required != (result.AuditRequirement{Through: 4, Status: "met"}) {
		t.Fatalf("chain = %+v held = %+v required = %+v", chain.Coverage, chain.Held, chain.Required)
	}
	if !containsString(chain.DoesNotEstablish, notHeldAll) || !containsString(chain.DoesNotEstablish, notStamped) ||
		chain.Coverage.Stamped.Status != "not-available" {
		t.Fatalf("statements = %v", chain.DoesNotEstablish)
	}
	beyond := verifyWith(t, data, Options{Held: kept, RequireThrough: 5}).Chain
	if beyond.Status != "invalid" || strings.Join(findingNames(beyond), " ") != "checkpoint-coverage-missing@5" || beyond.Required.Status != "unmet" {
		t.Fatalf("a requirement past the coverage: %v", findingNames(beyond))
	}
	none := verifyWith(t, data, Options{RequireThrough: 1}).Chain
	if none.Status != "invalid" || none.Coverage.Unwitnessed != 6 || none.Coverage.Witnessed != 0 || !hasFinding(none, FindingCheckpointCoverageMissing) {
		t.Fatalf("no held checkpoint witnesses nothing: %+v %v", none.Coverage, findingNames(none))
	}
	if alone := verifyWith(t, data, Options{}).Chain; alone.Coverage.Unwitnessed != 6 || alone.Required != nil || alone.Held != nil {
		t.Fatalf("without held checkpoints every record is unwitnessed: %+v", alone.Coverage)
	}
	// Every held checkpoint matches its record, but a break between them
	// stops the coverage at the one before it, and the held checkpoints are
	// not reported as matched.
	lines := splitLines(data)
	lines[2] = bytes.Replace(lines[2], []byte(`"note":"a <b> & c"`), []byte(`"note":"a <b> & d"`), 1)
	broken := verifyWith(t, joinLines(lines), Options{Held: keptCheckpoints(t, data, 2, 5)}).Chain
	if broken.Held.Matched != 2 || broken.Held.Failed != 0 || broken.Held.Status != "failed" || broken.Coverage.Checkpointed.Through != 2 || broken.Coverage.Witnessed != 2 {
		t.Fatalf("a break between held checkpoints: %+v %+v %v", broken.Held, broken.Coverage, findingNames(broken))
	}
}

// A suffix rewritten with its links recomputed is a consistent chain, which
// only a held checkpoint covering it finds; the records before the rewrite
// stay witnessed by the checkpoints that cover them alone, and a holder that
// kept only checkpoints before the rewrite sees nothing wrong, since its
// coverage ends there. A holder given both the original and the rewritten
// checkpoint of one sequence holds proof of the rewrite.
func TestARewrittenSuffixIsCaughtOnlyAgainstAHeldCheckpoint(t *testing.T) {
	root, dir, original := chainedTrail(t, 4)
	kept := keptCheckpoints(t, original, 1, 2, 3, 4)
	trail := filepath.Join(dir, "audit", FileName)
	if err := os.WriteFile(trail, joinLines(splitLines(original)[:2]), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRecords(t, root, `{"n":"other"}`, `{"n":"another"}`)
	rewritten := readTrailFile(t, dir)
	if alone := verifyWith(t, rewritten, Options{}).Chain; alone.Status != "valid" {
		t.Fatalf("the chain alone cannot see the rewrite: %v", findingNames(alone))
	}
	held := verifyWith(t, rewritten, Options{Held: kept}).Chain
	if held.Status != "invalid" || strings.Join(findingNames(held), " ") != "checkpoint-record-mismatch@3 checkpoint-record-mismatch@4" ||
		held.Held.Matched != 2 || held.Held.Failed != 2 || held.Held.Status != "failed" ||
		held.Coverage.Checkpointed.Through != 2 || held.Coverage.Witnessed != 2 || held.Coverage.Unwitnessed != 2 {
		t.Fatalf("held: %v %+v %+v", findingNames(held), held.Held, held.Coverage)
	}
	early := verifyWith(t, rewritten, Options{Held: kept[:2]}).Chain
	if early.Status != "valid" || early.Coverage.Checkpointed.Through != 2 || early.Coverage.Unwitnessed != 2 {
		t.Fatalf("checkpoints before the rewrite see nothing after them: %+v", early.Coverage)
	}
	reissued := keptCheckpoints(t, rewritten, 3)
	if reissued[0].RecordDigest == kept[2].RecordDigest {
		t.Fatal("the rewrite gives sequence 3 another checkpoint")
	}
	for name, data := range map[string][]byte{"the rewritten trail": rewritten, "the original": original} {
		both := verifyWith(t, data, Options{Held: []result.AuditCheckpoint{kept[2], reissued[0]}}).Chain
		if both.Status != "invalid" || both.Held.Failed != 1 || !hasFinding(both, FindingCheckpointRecordMismatch) {
			t.Fatalf("%s against two checkpoints of one sequence: %v", name, findingNames(both))
		}
	}
}

// A holder's file holds its checkpoints one per line; blank lines are passed
// over, a line that is not a checkpoint refuses the file and is named, a later
// version is refused as one, and a file of none is refused.
func TestHeldCheckpointsAreReadOnePerLine(t *testing.T) {
	_, _, data := chainedTrail(t, 3)
	kept := keptCheckpoints(t, data, 1, 2, 3)
	document := append(append(append(EncodeCheckpoint(kept[0]), '\n'), EncodeCheckpoint(kept[1])...), EncodeCheckpoint(kept[2])...)
	parsed, err := ParseCheckpoints(document)
	if err != nil || len(parsed) != 3 || parsed[0] != kept[0] || parsed[2] != kept[2] {
		t.Fatalf("parsed %d: %v", len(parsed), err)
	}
	broken := append(append([]byte{}, document...), []byte("{}\n")...)
	if _, err := ParseCheckpoints(broken); err == nil || !strings.Contains(err.Error(), "line 5") {
		t.Fatalf("a line that is not a checkpoint: %v", err)
	}
	later := bytes.Replace(document, []byte(`"checkpointVersion":"1"`), []byte(`"checkpointVersion":"2"`), 1)
	if _, err := ParseCheckpoints(later); !errors.Is(err, ErrCheckpointVersion) {
		t.Fatalf("a later version: %v", err)
	}
	if _, err := ParseCheckpoints([]byte("\n  \n")); err == nil {
		t.Fatal("a file of no checkpoint is refused")
	}
	// Blank lines a thousand bytes wide, then the checkpoints: every line is
	// readable, so only the size refuses it.
	blank := append(bytes.Repeat([]byte(" "), 1000), '\n')
	over := append(bytes.Repeat(blank, MaxHeldBytes/len(blank)+1), document...)
	if _, err := ParseCheckpoints(over); err == nil || !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("a file over the bound is refused for its size: %v", err)
	}
}
