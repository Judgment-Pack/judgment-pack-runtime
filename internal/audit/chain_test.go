package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// trailLines reads a trail as the bytes it holds: each line exactly, without
// its newline. Nothing is decoded and encoded again on the way, because the
// chain is over these bytes and a re-encoding would be other bytes.
func trailLines(t *testing.T, root, dir string) (whole []byte, lines [][]byte) {
	t.Helper()
	whole, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dir), FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(whole, []byte("\n")) {
		t.Fatalf("every record ends its own line: %q", whole)
	}
	return whole, bytes.Split(bytes.TrimSuffix(whole, []byte("\n")), []byte("\n"))
}

// chainOf is one line's chain members, read by exact name.
type chainOf struct {
	trail    string
	sequence int64
	previous string
	present  bool
}

func readChain(t *testing.T, line []byte) chainOf {
	t.Helper()
	members, err := exactObject(line)
	if err != nil {
		t.Fatalf("undecodable record %s: %v", line, err)
	}
	_, hasTrail := members["trail"]
	_, hasSequence := members["sequence"]
	_, hasPrevious := members["previous"]
	if !hasTrail && !hasSequence && !hasPrevious {
		return chainOf{}
	}
	var link chainOf
	link.present = true
	if decodeString(members["trail"], &link.trail) != nil || decodeInteger(members["sequence"], &link.sequence) != nil ||
		decodeString(members["previous"], &link.previous) != nil {
		t.Fatalf("a chained record carries all three members, each of its type: %s", line)
	}
	return link
}

var trailIdentity = regexp.MustCompile(`^[0-9a-f]{32}$`)

// checkChain holds a whole trail to the chain's rule, line by line, from the
// first line: every line chained, its sequence its line number, one identity
// throughout, and its previous the SHA-256 of the line before it exactly as it
// is in the file (of the empty string for the first). It is the reader's side
// of the writer's contract, written out so a test can apply it to whatever the
// writers left.
func checkChain(t *testing.T, lines [][]byte) string {
	t.Helper()
	trail := ""
	for index, line := range lines {
		link := readChain(t, line)
		if !link.present {
			t.Fatalf("line %d is not chained: %s", index+1, line)
		}
		if link.sequence != int64(index+1) {
			t.Fatalf("line %d carries sequence %d", index+1, link.sequence)
		}
		if index == 0 {
			trail = link.trail
			if !trailIdentity.MatchString(trail) {
				t.Fatalf("the trail's identity is 128 bits in lowercase hex: %q", trail)
			}
			if link.previous != Digest(nil) {
				t.Fatalf("a new trail's first record commits to the empty string: %q", link.previous)
			}
			continue
		}
		if link.trail != trail {
			t.Fatalf("line %d names trail %q, the trail is %q", index+1, link.trail, trail)
		}
		if link.previous != Digest(lines[index-1]) {
			t.Fatalf("line %d's previous %q is not the digest of line %d's bytes", index+1, link.previous, index)
		}
	}
	return trail
}

// A trail chained from its first record: the first commits to the empty
// string, and a record written by a later invocation, a second writer, links
// to the first line's bytes as they are in the file and keeps its identity.
func TestANewTrailIsChainedFromTheEmptyString(t *testing.T) {
	first, root := writerAt(t, "audit")
	if err := first.Evaluation(evaluated(), Inputs{Facts: []byte(`{"a":1}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	opened, err := fssecure.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	second := NewWriter(opened, "audit", true)
	if err := second.Evaluation(evaluated(), Inputs{Facts: []byte(`{"a":2}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	_, lines := trailLines(t, root, "audit")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	checkChain(t, lines)
	// The members sit right after recordVersion, in this order.
	if !regexp.MustCompile(`^\{"recordVersion":"1","trail":"[0-9a-f]{32}","sequence":1,"previous":"sha256:[0-9a-f]{64}","run":`).Match(lines[0]) {
		t.Fatalf("the chain's members follow recordVersion: %s", lines[0])
	}
}

// The byte round trip ADR-0047 asks of every component that keeps a record. A
// record holding &, < and >, characters outside ASCII and a fact spelled 1.0 is
// the record it was before chaining existed, byte for byte, with the three
// members inserted after recordVersion and nothing else changed: no escape
// added, no character re-spelled, no number re-written. And the next record's
// previous is the digest of those bytes exactly as they are in the file, which
// a decoded and re-encoded copy does not reproduce.
func TestAChainedRecordIsTheRecordItWasWithThreeMembers(t *testing.T) {
	facts := []byte(`{"note":"a <b> & c > d","name":"Zoë 日本 \u00e9","amount":1.0,"limit":[1.0,2.50]}`)
	record, err := EvaluationRecord(evaluated(), Inputs{
		Facts:            facts,
		Evidence:         []byte(`{"itemised-receipt":"present"}`),
		EvidenceSupplied: true,
	}, nil, []byte(`{"id":"expense-approval"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	chained, chainedRoot := writerAt(t, "audit")
	plain, plainRoot := writerAt(t, "audit")
	plain.chain = false
	plain.run = chained.run
	if err := chained.Append(record); err != nil {
		t.Fatal(err)
	}
	if err := plain.Append(record); err != nil {
		t.Fatal(err)
	}
	_, chainedLines := trailLines(t, chainedRoot, "audit")
	_, plainLines := trailLines(t, plainRoot, "audit")
	line, before := chainedLines[0], plainLines[0]

	// The unchained line is what this runtime always wrote: the documents
	// compacted and otherwise as the caller spelled them.
	for _, spelled := range []string{`"note":"a <b> & c > d"`, `"name":"Zoë 日本 \u00e9"`, `"amount":1.0`, `"limit":[1.0,2.50]`} {
		if !bytes.Contains(before, []byte(spelled)) {
			t.Fatalf("the record must keep %s as spelled: %s", spelled, before)
		}
	}
	link := readChain(t, line)
	head := fmt.Sprintf(`{"recordVersion":"1","trail":%q,"sequence":1,"previous":%q,`, link.trail, link.previous)
	const recordVersionHead = `{"recordVersion":"1",`
	if !bytes.HasPrefix(before, []byte(recordVersionHead)) || !bytes.Equal(line, append([]byte(head), before[len(recordVersionHead):]...)) {
		t.Fatalf("the chained line must be the unchained one with the three members inserted:\n%s\n%s", line, before)
	}

	// The next record links to the first line's bytes as read from the file.
	if err := chained.Append(record); err != nil {
		t.Fatal(err)
	}
	_, chainedLines = trailLines(t, chainedRoot, "audit")
	next := readChain(t, chainedLines[1])
	if next.previous != Digest(chainedLines[0]) {
		t.Fatalf("previous %q is not the digest of the first line's bytes", next.previous)
	}
	// A decoded and re-encoded copy is other bytes with another digest: the
	// default encoder escapes &, < and >, and a number decoded as a float64
	// loses the spelling 1.0. So the link above was taken over the bytes.
	var decoded map[string]any
	if err := json.Unmarshal(chainedLines[0], &decoded); err != nil {
		t.Fatal(err)
	}
	reencoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(reencoded, chainedLines[0]) || Digest(reencoded) == next.previous {
		t.Fatalf("a re-encoding must not reproduce the line, or this test shows nothing: %s", reencoded)
	}
	checkChain(t, chainedLines)
}

// A graph run's records take consecutive sequences in the order handed over,
// the composite last, each linked to the line before it in the batch exactly as
// written -- and the first to whatever the trail already held.
func TestABatchTakesConsecutiveSequencesWithItsCompositeLast(t *testing.T) {
	writer, root := writerAt(t, "audit")
	if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	batch := []Record{}
	for _, node := range []string{"detect", "decide"} {
		record, err := EvaluationRecord(evaluated(), Inputs{Facts: []byte(`{"node":"` + node + `"}`)}, nil, []byte(`{}`), &Graph{ID: "g", Version: "1", Node: node})
		if err != nil {
			t.Fatal(err)
		}
		batch = append(batch, record)
	}
	composite, err := CompositeRecord(result.GraphEvaluation{
		Command: "experimental graph evaluate", EvaluatorSpecVersion: result.EvaluatorSpecVersion,
		GraphID: "g", GraphVersion: "1", ResultNode: "decide", Disposition: evaluated().Disposition,
	}, "sha256:"+strings.Repeat("b", 64), nil)
	if err != nil {
		t.Fatal(err)
	}
	// A composer's chain members are not the writer's, and are replaced.
	composite.Trail, composite.Sequence, composite.Previous = strings.Repeat("0", 32), 99, Digest([]byte("forged"))
	if err := writer.AppendAll(append(batch, composite)); err != nil {
		t.Fatal(err)
	}
	_, lines := trailLines(t, root, "audit")
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want 4", len(lines))
	}
	checkChain(t, lines)
	records := decodeLines(t, root, "audit")
	if records[3]["kind"] != KindGraphComposite || records[1]["kind"] != KindEvaluation || records[2]["kind"] != KindEvaluation {
		t.Fatalf("the composite is the batch's last line: %v", records)
	}
}

// A composer's chain members are not the writer's: a writer that does not
// chain writes none of them, whatever the record it was handed carried.
func TestAComposersChainMembersNeverReachAnUnchainedLine(t *testing.T) {
	writer, root := writerAt(t, "audit")
	writer.chain = false
	record, err := EvaluationRecord(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	record.Trail, record.Sequence, record.Previous = strings.Repeat("0", 32), 7, Digest([]byte("forged"))
	if err := writer.Append(record); err != nil {
		t.Fatal(err)
	}
	_, lines := trailLines(t, root, "audit")
	if readChain(t, lines[0]).present {
		t.Fatalf("an unchained line carries no chain member: %s", lines[0])
	}
}

// A trail written before chaining, two lines here, is never rewritten: the
// first chained record follows it with sequence 3 and commits to the whole of
// it at once -- the SHA-256 of the file as it stood, newlines included -- under
// a new identity, and the record after that links to the line before it.
func TestALegacyPrefixIsCommittedAsOneBlock(t *testing.T) {
	writer, root := writerAt(t, "audit")
	writer.chain = false
	for range 2 {
		if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
			t.Fatal(err)
		}
	}
	legacy, legacyLines := trailLines(t, root, "audit")
	for _, line := range legacyLines {
		if readChain(t, line).present {
			t.Fatalf("a writer that does not chain writes no chain member: %s", line)
		}
	}
	writer.chain = true
	for range 2 {
		if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
			t.Fatal(err)
		}
	}
	whole, lines := trailLines(t, root, "audit")
	if !bytes.HasPrefix(whole, legacy) {
		t.Fatal("the legacy prefix must be kept byte for byte")
	}
	first, second := readChain(t, lines[2]), readChain(t, lines[3])
	if first.sequence != 3 || first.previous != Digest(legacy) || !trailIdentity.MatchString(first.trail) {
		t.Fatalf("the first chained record commits to the whole prefix: %+v, want sequence 3 and %s", first, Digest(legacy))
	}
	if second.sequence != 4 || second.previous != Digest(lines[2]) || second.trail != first.trail {
		t.Fatalf("the next record links to the line before it: %+v", second)
	}
}

// A chain that was turned off and on again starts again over the whole file,
// under the identity it had: the unchained lines in between are covered with
// everything before them, and the sequence is still the line number.
func TestAChainStartedAgainKeepsItsIdentity(t *testing.T) {
	writer, root := writerAt(t, "audit")
	write := func() {
		t.Helper()
		if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
			t.Fatal(err)
		}
	}
	write()
	write()
	writer.chain = false
	write()
	before, _ := trailLines(t, root, "audit")
	writer.chain = true
	write()
	_, lines := trailLines(t, root, "audit")
	original, again := readChain(t, lines[0]), readChain(t, lines[3])
	if again.sequence != 4 || again.previous != Digest(before) || again.trail != original.trail {
		t.Fatalf("a chain started again commits to the whole file under its identity: %+v, want %s %s", again, original.trail, Digest(before))
	}
}

// A last line with no newline is a write that did not complete, and nothing is
// chained over it: the append is refused, the trail is left byte for byte as
// it was, and the refusal says why in the one message every surface gives. A
// writer that does not chain appends as it always did, after it.
func TestNoRecordIsChainedAfterAnIncompleteLastLine(t *testing.T) {
	writer, root := writerAt(t, "audit")
	if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	trail := filepath.Join(root, "audit", FileName)
	torn, err := os.OpenFile(trail, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := torn.WriteString(`{"recordVersion":"1","run":"`); err != nil {
		t.Fatal(err)
	}
	torn.Close()
	before, err := os.ReadFile(trail)
	if err != nil {
		t.Fatal(err)
	}
	err = writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil)
	if !errors.Is(err, ErrIncompleteLastLine) {
		t.Fatalf("err = %v, want ErrIncompleteLastLine", err)
	}
	if FailureMessageFor(err) != incompleteMessage || FailureMessageFor(errors.New("disk full")) != FailureMessage {
		t.Fatalf("the message names the incomplete line, and only for it: %q", FailureMessageFor(err))
	}
	after, err := os.ReadFile(trail)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("a refused append leaves the trail as it was: %v", err)
	}
	writer.chain = false
	if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatalf("a writer that does not chain appends as it always did: %v", err)
	}
}

// Whether a line is chained is read from its own members, by exact name, each
// once, each of its form, in a line that is one JSON object. A record that
// follows a line that is not chained commits to the whole file instead of
// linking to it.
func TestOnlyAWellFormedChainedLineIsFollowedAsOne(t *testing.T) {
	trail := strings.Repeat("ab", 16)
	previous := Digest([]byte("x"))
	line := func(members string) []byte {
		return []byte(`{"recordVersion":"1",` + members + `,"run":"r"}`)
	}
	good := `"trail":"` + trail + `","sequence":7,"previous":"` + previous + `"`
	if found, sequence, ok := chainedLine(line(good)); !ok || found != trail || sequence != 7 {
		t.Fatalf("a well-formed chained line is one: %q %d %v", found, sequence, ok)
	}
	for name, candidate := range map[string][]byte{
		"no trail":             line(`"sequence":7,"previous":"` + previous + `"`),
		"no sequence":          line(`"trail":"` + trail + `","previous":"` + previous + `"`),
		"no previous":          line(`"trail":"` + trail + `","sequence":7`),
		"a trail given twice":  line(good + `,"trail":"` + strings.Repeat("cd", 16) + `"`),
		"a trail in uppercase": line(`"trail":"` + strings.ToUpper(trail) + `","sequence":7,"previous":"` + previous + `"`),
		"a short trail":        line(`"trail":"` + trail[:31] + `","sequence":7,"previous":"` + previous + `"`),
		"a sequence of zero":   line(`"trail":"` + trail + `","sequence":0,"previous":"` + previous + `"`),
		"a sequence of 1.0":    line(`"trail":"` + trail + `","sequence":7.0,"previous":"` + previous + `"`),
		"a sequence as text":   line(`"trail":"` + trail + `","sequence":"7","previous":"` + previous + `"`),
		"a sequence at 2^53-1": line(`"trail":"` + trail + `","sequence":` + strconv.FormatInt(maxSafeInteger, 10) + `,"previous":"` + previous + `"`),
		"a bare previous":      line(`"trail":"` + trail + `","sequence":7,"previous":"` + strings.TrimPrefix(previous, "sha256:") + `"`),
		"trailing content":     append(line(good), []byte(` {}`)...),
		"not an object":        []byte(`["` + trail + `"]`),
		"empty":                {},
	} {
		if _, _, ok := chainedLine(candidate); ok {
			t.Fatalf("%s is not a chained line: %s", name, candidate)
		}
	}

	// Through the writer: a last line that only looks chained is committed to
	// with everything before it.
	writer, root := writerAt(t, "audit")
	if err := os.MkdirAll(filepath.Join(root, "audit"), 0o700); err != nil {
		t.Fatal(err)
	}
	prefix := append(line(`"trail":"`+trail+`","sequence":0,"previous":"`+previous+`"`), '\n')
	if err := os.WriteFile(filepath.Join(root, "audit", FileName), prefix, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	_, lines := trailLines(t, root, "audit")
	link := readChain(t, lines[1])
	if link.sequence != 2 || link.previous != Digest(prefix) || link.trail == trail {
		t.Fatalf("a record after a line that is not chained commits to the whole file: %+v", link)
	}
}

// Where no lock can be taken the writer does not chain: what it appends is the
// unchained lines, exactly, and the trail is not read.
func TestWithoutALockTheRecordsAreNotChained(t *testing.T) {
	record, err := EvaluationRecord(evaluated(), Inputs{Facts: []byte(`{"a":"<&>"}`)}, nil, []byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	record.RecordVersion = RecordVersion
	unchained, err := encodeLines([]Record{record})
	if err != nil {
		t.Fatal(err)
	}
	written, err := chainedLines([]Record{record}, unchained, fssecure.AppendState{Locked: false, Size: 1, Contents: failingReaderAt{}})
	if err != nil || !bytes.Equal(written, unchained) {
		t.Fatalf("without the lock the lines are the unchained ones: %s, %v", written, err)
	}
	// With it, the same records are chained, and the contents are read.
	if _, err := chainedLines([]Record{record}, unchained, fssecure.AppendState{Locked: true, Size: 1, Contents: failingReaderAt{}}); err == nil {
		t.Fatal("under the lock the trail is read")
	}
}

type failingReaderAt struct{}

func (failingReaderAt) ReadAt([]byte, int64) (int, error) { return 0, errors.New("read failed") }

// A long last line is found by reading back from the end in pieces: a record
// longer than one read, after a short one, links the next record to its whole
// bytes and to nothing before them.
func TestALongLastLineIsReadWhole(t *testing.T) {
	writer, root := writerAt(t, "audit")
	long := `{"note":"` + strings.Repeat("x", 3*readChunk+17) + `"}`
	for _, facts := range []string{`{}`, long, long} {
		if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(facts)}, nil, []byte(`{}`), nil); err != nil {
			t.Fatal(err)
		}
	}
	_, lines := trailLines(t, root, "audit")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	checkChain(t, lines)
}

// lowerLineBound sets maxLineBytes for one test, so a test can hold the bound
// with lines of kilobytes rather than of a hundred megabytes.
func lowerLineBound(t *testing.T, bound int64) {
	t.Helper()
	original := maxLineBytes
	maxLineBytes = bound
	t.Cleanup(func() { maxLineBytes = original })
}

// A last line longer than the bound is refused, not read whole and not taken
// for unchained. A line of exactly the bound is still read, and looking for
// the start of a much longer line stops once it is past the bound.
func TestALineLongerThanTheBoundIsRefusedNotReadWhole(t *testing.T) {
	lowerLineBound(t, 4*readChunk)
	over := syntheticLine{size: maxLineBytes + 2, read: new(int64)}
	if line, err := lastLine(over, over.size); !errors.Is(err, ErrOversizedLine) || line != nil {
		t.Fatalf("a line over the bound is refused: %d bytes, %v", len(line), err)
	}
	at := syntheticLine{size: maxLineBytes + 1, read: new(int64)}
	if line, err := lastLine(at, at.size); err != nil || int64(len(line)) != maxLineBytes {
		t.Fatalf("a line at the bound is read: %d bytes, %v", len(line), err)
	}
	if next, err := readHead(over, over.size); !errors.Is(err, ErrOversizedLine) {
		t.Fatalf("what follows such a line is refused, not committed to as a legacy block: %+v, %v", next, err)
	}
	far := syntheticLine{size: 64 * maxLineBytes, read: new(int64)}
	if line, err := lastLine(far, far.size); !errors.Is(err, ErrOversizedLine) || line != nil {
		t.Fatalf("a line over the bound is refused: %d bytes, %v", len(line), err)
	}
	if *far.read > maxLineBytes+2*readChunk {
		t.Fatalf("the search read %d bytes of a line it was never going to read", *far.read)
	}
	// And the refusal is the head's own: the line is not read a second time,
	// from the start of the file, on the way to the same answer.
	again := syntheticLine{size: 64 * maxLineBytes, read: new(int64)}
	if _, err := readHead(again, again.size); !errors.Is(err, ErrOversizedLine) {
		t.Fatalf("an over-bound last line refuses the append: %v", err)
	}
	if *again.read > maxLineBytes+2*readChunk {
		t.Fatalf("refusing the last line read %d bytes", *again.read)
	}
}

// A valid chained record longer than the bound is never reinterpreted: as the
// last line, or as a line the chain must read when it starts again, it refuses
// the append, under the message every surface gives, and the trail is left as
// it was. A chained line of exactly the bound is followed as one.
func TestAValidChainedLineOverTheBoundIsRefusedNotReinterpreted(t *testing.T) {
	big := `{"note":"` + strings.Repeat("x", 6000) + `"}`
	write := func(writer *Writer, facts string) error {
		return writer.Evaluation(evaluated(), Inputs{Facts: []byte(facts)}, nil, []byte(`{}`), nil)
	}

	// As the last line.
	writer, root := writerAt(t, "audit")
	if err := write(writer, big); err != nil {
		t.Fatal(err)
	}
	before, lines := trailLines(t, root, "audit")
	if !readChain(t, lines[0]).present {
		t.Fatal("the fixture's line is chained")
	}
	func() {
		lowerLineBound(t, int64(len(lines[0])-1))
		err := write(writer, `{}`)
		if !errors.Is(err, ErrOversizedLine) || FailureMessageFor(err) != oversizedMessage {
			t.Fatalf("a chained last line over the bound refuses the append: %v", err)
		}
		if after, _ := trailLines(t, root, "audit"); !bytes.Equal(before, after) {
			t.Fatal("a refused append leaves the trail as it was")
		}
		maxLineBytes = int64(len(lines[0]))
		if err := write(writer, `{}`); err != nil {
			t.Fatalf("a chained line of exactly the bound is followed: %v", err)
		}
		_, lines = trailLines(t, root, "audit")
		checkChain(t, lines)
	}()

	// As a line the chain must read when it starts again.
	writer, root = writerAt(t, "audit")
	if err := write(writer, big); err != nil {
		t.Fatal(err)
	}
	writer.chain = false
	if err := write(writer, `{}`); err != nil {
		t.Fatal(err)
	}
	writer.chain = true
	before, lines = trailLines(t, root, "audit")
	lowerLineBound(t, int64(len(lines[0])-1))
	if err := write(writer, `{}`); !errors.Is(err, ErrOversizedLine) {
		t.Fatalf("a chained line over the bound before an unchained one refuses the append: %v", err)
	}
	if after, _ := trailLines(t, root, "audit"); !bytes.Equal(before, after) {
		t.Fatal("a refused append leaves the trail as it was")
	}
}

// A chaining writer never writes a line over the bound, whether it holds the
// lock or not, so no line it leaves is one a later writer must refuse.
func TestARecordOverTheBoundIsNotWritten(t *testing.T) {
	big := `{"note":"` + strings.Repeat("x", 6000) + `"}`
	writer, root := writerAt(t, "audit")
	if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	before, _ := trailLines(t, root, "audit")
	lowerLineBound(t, 4096)
	err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(big)}, nil, []byte(`{}`), nil)
	if !errors.Is(err, ErrRecordTooLarge) || FailureMessageFor(err) != tooLargeMessage {
		t.Fatalf("a record over the bound is refused: %v", err)
	}
	if after, _ := trailLines(t, root, "audit"); !bytes.Equal(before, after) {
		t.Fatal("nothing is appended")
	}
	record, err := EvaluationRecord(evaluated(), Inputs{Facts: []byte(big)}, nil, []byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	unchained, err := encodeLines([]Record{record})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chainedLines([]Record{record}, unchained, fssecure.AppendState{Locked: false}); !errors.Is(err, ErrRecordTooLarge) {
		t.Fatalf("without the lock too: %v", err)
	}
}

// A chained line is read one way on both paths, whatever its spelling: as the
// last line it is linked to, and as a line before an unchained one it gives
// the chain started again its identity. Whitespace around the colon and an
// escape in the member's name are JSON, and are read as JSON reads them.
func TestEverySpellingOfAChainedLineIsReadOneWay(t *testing.T) {
	trail := strings.Repeat("cd", 16)
	previous := Digest([]byte("x"))
	for name, line := range map[string]string{
		"compact":          `{"recordVersion":"1","trail":"` + trail + `","sequence":1,"previous":"` + previous + `","run":"r"}`,
		"space before ':'": `{"recordVersion":"1","trail" : "` + trail + `", "sequence" : 1, "previous" : "` + previous + `","run":"r"}`,
		"escaped name":     `{"recordVersion":"1","tr\u0061il":"` + trail + `","sequence":1,"previous":"` + previous + `","run":"r"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if found, _, ok := chainedLine([]byte(line)); !ok || found != trail {
				t.Fatalf("the line is chained: %s", line)
			}
			seed := func(t *testing.T, contents string) (*Writer, string) {
				t.Helper()
				writer, root := writerAt(t, "audit")
				if err := os.MkdirAll(filepath.Join(root, "audit"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "audit", FileName), []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
				return writer, root
			}
			// As the last line.
			writer, root := seed(t, line+"\n")
			if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
				t.Fatal(err)
			}
			_, lines := trailLines(t, root, "audit")
			if link := readChain(t, lines[1]); link.trail != trail || link.sequence != 2 || link.previous != Digest([]byte(line)) {
				t.Fatalf("the record links to the line: %+v", link)
			}
			// Before an unchained line.
			unchained := `{"recordVersion":"1","run":"s"}`
			writer, root = seed(t, line+"\n"+unchained+"\n")
			if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
				t.Fatal(err)
			}
			_, lines = trailLines(t, root, "audit")
			if link := readChain(t, lines[2]); link.trail != trail || link.sequence != 3 || link.previous != Digest([]byte(line+"\n"+unchained+"\n")) {
				t.Fatalf("the chain started again keeps the line's identity: %+v", link)
			}
		})
	}
}

// syntheticLine is a file of one line, size bytes long with its newline, made
// up as it is read so a test need not hold it. read counts the bytes handed out.
type syntheticLine struct {
	size int64
	read *int64
}

func (s syntheticLine) ReadAt(into []byte, offset int64) (int, error) {
	if offset >= s.size {
		return 0, io.EOF
	}
	n := 0
	for ; n < len(into) && offset+int64(n) < s.size; n++ {
		into[n] = 'x'
		if offset+int64(n) == s.size-1 {
			into[n] = '\n'
		}
	}
	*s.read += int64(n)
	if n < len(into) {
		return n, io.EOF
	}
	return n, nil
}

func (s syntheticLine) digest() string {
	hash := sha256.New()
	if _, err := io.Copy(hash, io.NewSectionReader(s, 0, s.size)); err != nil {
		return ""
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

// Writers racing on one trail never share or skip a sequence, and every link
// holds: each writer opens its own handle, and the lock is what orders them.
// Batches of one and of three records alternate, and each batch's lines are
// adjacent.
func TestWritersRacingInOneProcessKeepTheChain(t *testing.T) {
	_, root := writerAt(t, "audit")
	const writers, batches = 4, 24
	var group sync.WaitGroup
	failures := make(chan error, writers*batches)
	for range writers {
		group.Add(1)
		go func() {
			defer group.Done()
			opened, err := fssecure.OpenRoot(root)
			if err != nil {
				failures <- err
				return
			}
			defer opened.Close()
			for batch := range batches {
				failures <- appendBatch(opened, batch%2 == 1)
			}
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	_, lines := trailLines(t, root, "audit")
	if len(lines) != writers*batches*2 {
		t.Fatalf("lines = %d, want %d", len(lines), writers*batches*2)
	}
	checkChain(t, lines)
	checkBatchesAdjacent(t, lines)
}

// appendBatch is one invocation's write: one record, or a graph run's two node
// records and its composite.
func appendBatch(root *fssecure.Root, graph bool) error {
	writer := NewWriter(root, "audit", true)
	single, err := EvaluationRecord(evaluated(), Inputs{Facts: []byte(`{"a":"<&>"}`)}, nil, []byte(`{}`), nil)
	if err != nil {
		return err
	}
	if !graph {
		return writer.Append(single)
	}
	composite, err := CompositeRecord(result.GraphEvaluation{
		Command: "experimental graph evaluate", EvaluatorSpecVersion: result.EvaluatorSpecVersion,
		GraphID: "g", GraphVersion: "1", ResultNode: "n", Disposition: evaluated().Disposition,
	}, "sha256:"+strings.Repeat("b", 64), nil)
	if err != nil {
		return err
	}
	return writer.AppendAll([]Record{single, single, composite})
}

// checkBatchesAdjacent holds each run's lines to being adjacent, a graph run's
// ending with its composite.
func checkBatchesAdjacent(t *testing.T, lines [][]byte) {
	t.Helper()
	seen := map[string]bool{}
	for index := 0; index < len(lines); {
		var first map[string]any
		if err := json.Unmarshal(lines[index], &first); err != nil {
			t.Fatal(err)
		}
		run := first["run"].(string)
		if seen[run] {
			t.Fatalf("run %s's lines are not adjacent (line %d)", run, index+1)
		}
		seen[run] = true
		end := index + 1
		for end < len(lines) {
			var next map[string]any
			if err := json.Unmarshal(lines[end], &next); err != nil {
				t.Fatal(err)
			}
			if next["run"] != run {
				break
			}
			end++
		}
		if size := end - index; size != 1 && size != 3 {
			t.Fatalf("run %s has %d lines", run, size)
		}
		if end-index == 3 {
			var last map[string]any
			if err := json.Unmarshal(lines[end-1], &last); err != nil || last["kind"] != KindGraphComposite {
				t.Fatalf("run %s's composite is its last line", run)
			}
		}
		index = end
	}
}

// raceHelperDir names the project directory a helper process writes into, and
// raceHelperRun marks the process as that helper. Both are set only by
// TestWritersRacingInSeveralProcessesKeepTheChain.
const (
	raceHelperDir = "JPACK_AUDIT_RACE_HELPER_DIR"
	raceHelperRun = "JPACK_AUDIT_RACE_HELPER_RUN"
)

// The same race across processes: copies of this test binary, started at
// once, each append batches to one trail, and the trail is one chain.
func TestWritersRacingInSeveralProcessesKeepTheChain(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("this test binary cannot be named: %v", err)
	}
	root := t.TempDir()
	const processes = 4
	commands := make([]*exec.Cmd, 0, processes)
	outputs := make([]*bytes.Buffer, 0, processes)
	for range processes {
		command := exec.Command(self, "-test.run=^TestRaceHelperProcess$", "-test.count=1")
		command.Env = append(os.Environ(), raceHelperDir+"="+root, raceHelperRun+"=1")
		output := &bytes.Buffer{}
		command.Stdout, command.Stderr = output, output
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, command)
		outputs = append(outputs, output)
	}
	for index, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatalf("helper process %d: %v\n%s", index, err, outputs[index])
		}
		if !strings.Contains(outputs[index].String(), "race helper wrote 20 batches") {
			t.Fatalf("helper process %d did not write: %s", index, outputs[index])
		}
	}
	_, lines := trailLines(t, root, "audit")
	if len(lines) != processes*20*2 {
		t.Fatalf("lines = %d, want %d", len(lines), processes*20*2)
	}
	checkChain(t, lines)
	checkBatchesAdjacent(t, lines)
}

// TestRaceHelperProcess is the process
// TestWritersRacingInSeveralProcessesKeepTheChain starts. It writes only when
// both markers are set, and only into the directory it is given.
func TestRaceHelperProcess(t *testing.T) {
	dir := os.Getenv(raceHelperDir)
	if dir == "" || os.Getenv(raceHelperRun) != "1" {
		t.Skip("run by TestWritersRacingInSeveralProcessesKeepTheChain")
	}
	opened, err := fssecure.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	for batch := range 20 {
		if err := appendBatch(opened, batch%2 == 1); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println("race helper wrote 20 batches")
}
