package audit

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/timestamp"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/timestamp/tsatest"
)

// writeRecords appends one evaluation record per facts document through a
// chaining writer, each as its own invocation.
func writeRecords(t *testing.T, root *fssecure.Root, facts ...string) {
	t.Helper()
	for _, document := range facts {
		if err := NewWriter(root, "audit", true).Evaluation(evaluated(), Inputs{Facts: []byte(document)}, nil, []byte(`{}`), nil); err != nil {
			t.Fatal(err)
		}
	}
}

// chainedTrail writes a trail of n chained records into a fresh project and
// returns the root and the trail's bytes.
func chainedTrail(t *testing.T, n int) (*fssecure.Root, string, []byte) {
	t.Helper()
	writer, dir := writerAt(t, "audit")
	facts := []string{}
	for index := range n {
		facts = append(facts, fmt.Sprintf(`{"n":%d,"note":"a <b> & c"}`, index))
	}
	writeRecords(t, writer.root, facts...)
	data, err := os.ReadFile(filepath.Join(dir, "audit", FileName))
	if err != nil {
		t.Fatal(err)
	}
	return writer.root, dir, data
}

// verifyBytes verifies a trail held in memory.
func verifyBytes(t *testing.T, data []byte, expect *result.AuditCheckpoint) result.AuditChain {
	t.Helper()
	options := Options{}
	if expect != nil {
		options.Held = []result.AuditCheckpoint{*expect}
	}
	report, err := Verify(bytes.NewReader(data), int64(len(data)), options)
	if err != nil {
		t.Fatal(err)
	}
	return report.Chain
}

// splitLines and joinLines move between a trail's bytes and its lines,
// exactly: every line ends with a newline.
func splitLines(data []byte) [][]byte {
	return bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
}

func joinLines(lines [][]byte) []byte {
	return append(bytes.Join(lines, []byte("\n")), '\n')
}

// findingNames lists a report's findings by name and line.
func findingNames(chain result.AuditChain) []string {
	names := []string{}
	for _, finding := range chain.Findings {
		names = append(names, fmt.Sprintf("%s@%d", finding.Name, finding.Line))
	}
	return names
}

// An intact trail verifies: every line chained, the head is the last record,
// and the report says what that does and does not establish.
func TestAnIntactTrailVerifies(t *testing.T) {
	_, _, data := chainedTrail(t, 4)
	chain := verifyBytes(t, data, nil)
	if chain.Status != "valid" || chain.Scope != ScopeCommittedPrefix || chain.Lines != 4 || chain.Bytes != int64(len(data)) || chain.FindingsTotal != 0 {
		t.Fatalf("chain = %+v", chain)
	}
	if chain.Coverage.Chained != 4 || chain.Coverage.LegacyPrefix != 0 || chain.Coverage.Uncovered != 0 ||
		chain.Coverage.Signed.Status != "not-checked" || chain.Coverage.Checkpointed.Status != "not-supplied" {
		t.Fatalf("coverage = %+v", chain.Coverage)
	}
	lines := splitLines(data)
	link := readChain(t, lines[3])
	if chain.Head == nil || chain.Head.Sequence != 4 || chain.Head.RecordDigest != Digest(lines[3]) || chain.Head.Trail != link.trail || chain.Trail != link.trail {
		t.Fatalf("head = %+v", chain.Head)
	}
	if len(chain.Segments) != 1 || chain.Segments[0] != (result.AuditSegment{FirstLine: 1, LastLine: 4}) {
		t.Fatalf("segments = %+v", chain.Segments)
	}
	wantStatement := "The chain-link checks passed through sequence 4, the last chained record and end of the committed prefix."
	if len(chain.Establishes) != 1 || chain.Establishes[0] != wantStatement ||
		!containsString(chain.DoesNotEstablish, fmt.Sprintf(notLastLine, 4)) || !containsString(chain.DoesNotEstablish, notComplete) || !containsString(chain.DoesNotEstablish, notSignedUnchecked) {
		t.Fatalf("statements = %v / %v", chain.Establishes, chain.DoesNotEstablish)
	}
}

// A chained record commits no unchained suffix after it. Editing such a
// suffix changes neither the checks that passed nor the prefix they describe.
func TestConsistencyStatementExcludesAnUnchainedSuffix(t *testing.T) {
	_, _, prefix := chainedTrail(t, 1)
	tailA := append(append([]byte{}, prefix...), []byte("{\"tail\":1}\n{\"tail\":2}\n")...)
	tailB := bytes.Replace(tailA, []byte(`{"tail":1}`), []byte(`{"tail":9}`), 1)
	want := "The chain-link checks passed through sequence 1, the last chained record and end of the committed prefix; the 2 uncovered line(s) after it are outside that prefix."
	for name, trail := range map[string][]byte{"original": tailA, "edited penultimate line": tailB} {
		t.Run(name, func(t *testing.T) {
			chain := verifyBytes(t, trail, nil)
			if chain.Status != "valid" || chain.Coverage.Chained != 1 || chain.Coverage.Uncovered != 2 ||
				len(chain.Establishes) != 1 || chain.Establishes[0] != want {
				t.Fatalf("coverage=%+v establishes=%q", chain.Coverage, chain.Establishes)
			}
		})
	}
}

// With no chained record there was no chain-link check and no committed
// prefix for the report to imply.
func TestWhollyUnchainedTrailHasNoConsistencyStatement(t *testing.T) {
	trail := []byte("{\"line\":1}\n{\"line\":2}\n")
	chain := verifyBytes(t, trail, nil)
	want := "That the lines form a chained history: no chained record was found, so all 2 line(s) are uncovered."
	if chain.Status != "valid" || chain.Scope != ScopeNoChainedRecord || chain.Coverage.Chained != 0 || chain.Coverage.Uncovered != 2 ||
		len(chain.Establishes) != 0 || !containsString(chain.DoesNotEstablish, want) {
		t.Fatalf("coverage=%+v establishes=%q doesNotEstablish=%q", chain.Coverage, chain.Establishes, chain.DoesNotEstablish)
	}
}

func TestEmptyTrailHasItsOwnNoChainStatement(t *testing.T) {
	chain := verifyBytes(t, nil, nil)
	want := "That the file contains a chained history: the file is empty."
	if chain.Status != "valid" || chain.Scope != ScopeNoChainedRecord || chain.Lines != 0 ||
		len(chain.Establishes) != 0 || !containsString(chain.DoesNotEstablish, want) {
		t.Fatalf("chain=%+v establishes=%q doesNotEstablish=%q", chain, chain.Establishes, chain.DoesNotEstablish)
	}
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestReviewFixedSentencesAreExact(t *testing.T) {
	wants := map[string]string{
		"last chained":         "The last chained line (sequence 6), and any lines rewritten from some point on with their links recomputed, are not authenticated by the chain: only a checkpoint covering them, held independently of the operator, shows they are the ones first written.",
		"no signature after":   "Lines after 2 are not covered by any signature that was checked: a record signed later, or never, is not authenticated by any key.",
		"no signature at all":  "That any line was signed by a key supplied: no signature that was checked covers one.",
		"signature after":      "Lines after 2 are not authenticated as one uninterrupted prefix by any signature that was checked; 1 record after sequence 2 carries a signature that was checked.",
		"signatures after":     "Lines after 2 are not authenticated as one uninterrupted prefix by any signature that was checked; 3 records after sequence 2 carry a signature that was checked.",
		"signature no prefix":  "That any uninterrupted prefix from line 1 is authenticated by a signature that was checked; 1 record carries a signature that was checked.",
		"signatures no prefix": "That any uninterrupted prefix from line 1 is authenticated by a signature that was checked; 3 records carry a signature that was checked.",
		"no stamp after":       "That lines after 2 existed by any time: no trusted stamp covers them.",
		"no stamp at all":      "That any line existed by any time: no trusted stamp covers one.",
		"stamp after":          "Lines after 2 are not shown as one uninterrupted prefix to have existed by any time; 1 trusted stamp after sequence 2 shows its record existed by its time.",
		"stamps after":         "Lines after 2 are not shown as one uninterrupted prefix to have existed by any time; 3 trusted stamps after sequence 2 show their records existed by their times.",
		"stamp no prefix":      "That any uninterrupted prefix from line 1 is shown to have existed by any time; 1 trusted stamp shows its record existed by its time.",
		"stamps no prefix":     "That any uninterrupted prefix from line 1 is shown to have existed by any time; 3 trusted stamps show their records existed by their times.",
		"stamp lag":            "When any record was made: a stamp shows its checkpoint existed by the stamp's time, not how long before, so a record's at stays the operator's word; the lag between each prefix-covered record's at and the first stamp covering it is reported, and judging it is the reader's.",
	}
	got := map[string]string{
		"last chained":         fmt.Sprintf(notLastLine, 6),
		"no signature after":   fmt.Sprintf(notSignedAfter, 2),
		"no signature at all":  notSignedNone,
		"signature after":      signedAfterStatement(2, 1),
		"signatures after":     signedAfterStatement(2, 3),
		"signature no prefix":  signedNoneStatement(1),
		"signatures no prefix": signedNoneStatement(3),
		"no stamp after":       fmt.Sprintf(notStampedAfter, 2),
		"no stamp at all":      notStampedNone,
		"stamp after":          stampedAfterStatement(2, 1),
		"stamps after":         stampedAfterStatement(2, 3),
		"stamp no prefix":      stampedNoneStatement(1),
		"stamps no prefix":     stampedNoneStatement(3),
		"stamp lag":            notBeforeStamp,
	}
	for name, want := range wants {
		if got[name] != want {
			t.Errorf("%s: got %q, want %q", name, got[name], want)
		}
	}
}

// Every kind of tampering is either found by the chain alone or, where the
// chain cannot see it, by a checkpoint of the original head held
// independently: an edited last line, a trail cut short, a suffix rewritten
// with its links recomputed, and a whole trail rewritten under another
// identity are consistent chains, and only the checkpoint tells them apart.
func TestTamperingIsFoundByTheChainOrByACheckpoint(t *testing.T) {
	root, dir, original := chainedTrail(t, 4)
	head := verifyBytes(t, original, nil).Head
	lines := func() [][]byte { return splitLines(append([]byte{}, original...)) }
	edit := func(line []byte) []byte {
		return bytes.Replace(line, []byte(`"note":"a <b> & c"`), []byte(`"note":"a <b> & d"`), 1)
	}
	// rewritten is the original's first keep lines followed by records a
	// chaining writer appends after them: a suffix with its links recomputed.
	rewritten := func(keep int, facts ...string) []byte {
		t.Helper()
		trail := filepath.Join(dir, "audit", FileName)
		if err := os.WriteFile(trail, joinLines(lines()[:keep]), 0o600); err != nil {
			t.Fatal(err)
		}
		writeRecords(t, root, facts...)
		data, err := os.ReadFile(trail)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	otherIdentity := func() []byte {
		t.Helper()
		_, _, data := chainedTrail(t, 4)
		return data
	}
	legacyEdited := func() []byte {
		t.Helper()
		writer, dir := writerAt(t, "audit")
		writer.chain = false
		writeRecords(t, writer.root)
		if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{"legacy":1}`)}, nil, []byte(`{}`), nil); err != nil {
			t.Fatal(err)
		}
		if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{"legacy":2}`)}, nil, []byte(`{}`), nil); err != nil {
			t.Fatal(err)
		}
		writeRecords(t, writer.root, `{"n":0}`, `{"n":1}`)
		data, err := os.ReadFile(filepath.Join(dir, "audit", FileName))
		if err != nil {
			t.Fatal(err)
		}
		if chain := verifyBytes(t, data, nil); chain.Status != "valid" || chain.Coverage.LegacyPrefix != 2 {
			t.Fatalf("the fixture verifies before the edit: %+v", chain)
		}
		edited := splitLines(data)
		edited[0] = bytes.Replace(edited[0], []byte(`"legacy":1`), []byte(`"legacy":9`), 1)
		return joinLines(edited)
	}
	for name, test := range map[string]struct {
		tampered func() []byte
		alone    []string // findings without a checkpoint; none means the chain alone cannot see it
		against  string   // the finding the original head's checkpoint adds; "" when the chain's own findings fail it
	}{
		"a middle line edited": {
			tampered: func() []byte { l := lines(); l[1] = edit(l[1]); return joinLines(l) },
			alone:    []string{"previous-mismatch@3"},
		},
		"the last line edited": {
			tampered: func() []byte { l := lines(); l[3] = edit(l[3]); return joinLines(l) },
			against:  FindingCheckpointRecordMismatch,
		},
		"a middle line deleted": {
			tampered: func() []byte { l := lines(); return joinLines(append(l[:1:1], l[2:]...)) },
			alone:    []string{"sequence-mismatch@2", "previous-mismatch@2", "sequence-mismatch@3"},
			against:  FindingCheckpointBeyondTrail,
		},
		"a line inserted": {
			tampered: func() []byte {
				l := lines()
				return joinLines(append(append(l[:2:2], l[1]), l[2:]...))
			},
			alone:   []string{"sequence-mismatch@3", "previous-mismatch@3", "sequence-mismatch@4", "sequence-mismatch@5"},
			against: FindingCheckpointRecordMismatch,
		},
		"an unchained line inserted": {
			tampered: func() []byte {
				l := lines()
				return joinLines(append(append(l[:2:2], []byte(`{"recordVersion":"1","run":"forged"}`)), l[2:]...))
			},
			alone:   []string{"sequence-mismatch@4", "previous-mismatch@4", "sequence-mismatch@5"},
			against: FindingCheckpointRecordMismatch,
		},
		"two lines reordered": {
			tampered: func() []byte { l := lines(); l[1], l[2] = l[2], l[1]; return joinLines(l) },
			alone:    []string{"sequence-mismatch@2", "previous-mismatch@2", "sequence-mismatch@3", "previous-mismatch@3", "previous-mismatch@4"},
		},
		"cut short": {
			tampered: func() []byte { return joinLines(lines()[:3]) },
			against:  FindingCheckpointBeyondTrail,
		},
		"a suffix rewritten with its links recomputed": {
			tampered: func() []byte { return rewritten(2, `{"n":"other"}`, `{"n":"another"}`) },
			against:  FindingCheckpointRecordMismatch,
		},
		"the whole trail rewritten under another identity": {
			tampered: otherIdentity,
			against:  FindingCheckpointTrailMismatch,
		},
		"one line given another identity": {
			tampered: func() []byte {
				l := lines()
				l[2] = bytes.Replace(l[2], []byte(head.Trail), []byte(strings.Repeat("e", 32)), 1)
				return joinLines(l)
			},
			alone: []string{"trail-mismatch@3", "previous-mismatch@4", "trail-mismatch@4"},
		},
		"the legacy prefix edited": {
			tampered: legacyEdited,
			alone:    []string{"previous-mismatch@3"},
			against:  FindingCheckpointTrailMismatch,
		},
	} {
		t.Run(name, func(t *testing.T) {
			data := test.tampered()
			alone := verifyBytes(t, data, nil)
			names := findingNames(alone)
			if len(test.alone) == 0 {
				if alone.Status != "valid" || alone.FindingsTotal != 0 {
					t.Fatalf("the chain alone cannot see this, and says nothing: %v", names)
				}
			} else if alone.Status != "invalid" || strings.Join(names, " ") != strings.Join(test.alone, " ") {
				t.Fatalf("findings = %v, want %v", names, test.alone)
			}
			if alone.Status == "invalid" && len(alone.Establishes) != 0 {
				t.Fatalf("an invalid trail establishes nothing: %v", alone.Establishes)
			}
			against := verifyBytes(t, data, head)
			if against.Status != "invalid" || against.Held == nil || against.Held.Status != "failed" ||
				against.Coverage.Checkpointed.Status != "failed" || (test.against != "" && !hasFinding(against, test.against)) {
				t.Fatalf("the checkpoint of the original head finds it (%s): %v", test.against, findingNames(against))
			}
		})
	}
	// And the original, against its own head, is through it.
	matched := verifyBytes(t, original, head)
	if matched.Status != "valid" || matched.Scope != ScopeCheckpoint || matched.Held.Status != "matched" ||
		matched.Coverage.Checkpointed != (result.AuditCoverageState{Status: "through", Through: 4}) ||
		!containsString(matched.Establishes, fmt.Sprintf(establishesCheckpoint, 4)) {
		t.Fatalf("the original is through its own checkpoint: %+v", matched)
	}
}

func hasFinding(chain result.AuditChain, name string) bool {
	for _, finding := range chain.Findings {
		if finding.Name == name {
			return true
		}
	}
	return false
}

// A checkpoint is through its sequence only when no line up to it failed a
// check: a break before it fails the checkpoint even when its own line
// matches, and a break after it does not.
func TestACheckpointIsThroughOnlyAnUnbrokenPrefix(t *testing.T) {
	_, _, data := chainedTrail(t, 4)
	lines := splitLines(data)
	third := &result.AuditCheckpoint{CheckpointVersion: "1", Trail: readChain(t, lines[2]).trail, Sequence: 3, RecordDigest: Digest(lines[2])}
	early := append([][]byte{}, lines...)
	early[0] = bytes.Replace(early[0], []byte(`"n":0`), []byte(`"n":9`), 1)
	if chain := verifyBytes(t, joinLines(early), third); chain.Held.Status != "failed" || chain.Coverage.Checkpointed.Status != "failed" {
		t.Fatalf("a break before the checkpoint fails it: %+v", chain.Held)
	}
	late := append([][]byte{}, lines...)
	late[3] = bytes.Replace(late[3], []byte(`"previous":"sha256:`), []byte(`"previous":"sha256:0`), 1)
	late[3] = bytes.Replace(late[3], []byte(`"previous":"sha256:0`), []byte(`"previous":"sha256:`), 1)
	late = append(late, []byte(`{"recordVersion":"1","trail":"`+third.Trail+`","sequence":5,"previous":"`+Digest([]byte("x"))+`","run":"r"}`))
	chain := verifyBytes(t, joinLines(late), third)
	if chain.Status != "invalid" || chain.Held.Status != "matched" || chain.Coverage.Checkpointed.Through != 3 {
		t.Fatalf("a break after the checkpoint leaves it matched, the trail invalid: %+v %v", chain.Held, findingNames(chain))
	}
}

// Unchained lines are counted by what commits to them: before the first
// chained line, one block; between chained lines, the next chained line's
// commitment to the whole trail before it; after the last, nothing.
func TestUnchainedLinesAreCountedByWhatCoversThem(t *testing.T) {
	writer, dir := writerAt(t, "audit")
	write := func(chain bool, count int) {
		t.Helper()
		writer.chain = chain
		for range count {
			if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(false, 2)
	write(true, 2)
	write(false, 1)
	write(true, 1)
	write(false, 2)
	data, err := os.ReadFile(filepath.Join(dir, "audit", FileName))
	if err != nil {
		t.Fatal(err)
	}
	chain := verifyBytes(t, data, nil)
	if chain.Status != "valid" || chain.Coverage.LegacyPrefix != 2 || chain.Coverage.Chained != 3 || chain.Coverage.Unchained != 1 || chain.Coverage.Uncovered != 2 || chain.Head.Sequence != 6 {
		t.Fatalf("coverage = %+v head = %+v findings = %v", chain.Coverage, chain.Head, findingNames(chain))
	}
	none := verifyBytes(t, joinLines(splitLines(data)[:2]), nil)
	if none.Status != "valid" || none.Coverage.Uncovered != 2 || none.Coverage.LegacyPrefix != 0 || none.Head != nil {
		t.Fatalf("a trail with no chained line covers nothing: %+v", none.Coverage)
	}
}

// A last line with no newline is a finding at the line it would have been; a
// line longer than the bound is a finding too, and the line after it is not
// held to a link it cannot be checked against.
func TestDamageIsAFinding(t *testing.T) {
	_, _, data := chainedTrail(t, 2)
	torn := append(append([]byte{}, data...), []byte(`{"recordVersion":"1","ru`)...)
	if chain := verifyBytes(t, torn, nil); chain.Status != "invalid" || strings.Join(findingNames(chain), " ") != "incomplete-last-line@3" || chain.Lines != 2 {
		t.Fatalf("findings = %v", findingNames(chain))
	}
	writer, dir := writerAt(t, "audit")
	writeRecords(t, writer.root, `{"long":"`+strings.Repeat("x", 400)+`"}`, `{}`)
	lines := splitLines(readTrailFile(t, dir))
	lowerLineBound(t, int64(len(lines[1])))
	if chain := verifyBytes(t, readTrailFile(t, dir), nil); strings.Join(findingNames(chain), " ") != "line-too-long@1" {
		t.Fatalf("the line after an unreadable one is not held to a link it cannot be checked against: %v", findingNames(chain))
	}
}

// A report lists at most maxFindings findings and counts them all, so the
// first ones are not buried.
func TestFindingsAreListedUpToABound(t *testing.T) {
	v := newVerifier(Options{})
	for line := range maxFindings + 50 {
		v.record(result.AuditFinding{Name: FindingSequenceMismatch, Line: int64(line + 1)})
	}
	if len(v.findings) != maxFindings || v.findingsTotal != maxFindings+50 || v.findings[0].Line != 1 {
		t.Fatalf("listed %d of %d", len(v.findings), v.findingsTotal)
	}
}

// tornTrail writes n chained records and then a write that did not complete.
func tornTrail(t *testing.T, n int) (*Writer, string) {
	t.Helper()
	writer, dir := writerAt(t, "audit")
	facts := []string{}
	for index := range n {
		facts = append(facts, fmt.Sprintf(`{"n":%d}`, index))
	}
	writeRecords(t, writer.root, facts...)
	appendRaw(t, dir, `{"recordVersion":"1","trail":"`)
	return writer, dir
}

func appendRaw(t *testing.T, dir, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "audit"), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(dir, "audit", FileName), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func readTrailFile(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "audit", FileName))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A repair keeps the damaged bytes in place as a line of their own, follows
// them with a discontinuity record that links over them to the last intact
// line, and the writer chains after it. The trail then verifies as segments,
// never as intact, and a checkpoint made before the damage still verifies the
// segment it covers.
func TestARepairedTrailVerifiesAsSegments(t *testing.T) {
	writer, dir := tornTrail(t, 3)
	before := readTrailFile(t, dir)
	checkpoint := &result.AuditCheckpoint{CheckpointVersion: "1", Trail: readChain(t, splitLines(before)[2]).trail, Sequence: 3, RecordDigest: Digest(splitLines(before)[2])}
	if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); !errors.Is(err, ErrIncompleteLastLine) {
		t.Fatalf("the writer refuses before the repair: %v", err)
	}
	repaired, err := writer.Repair()
	if err != nil {
		t.Fatal(err)
	}
	damage := []byte(`{"recordVersion":"1","trail":"`)
	want := result.AuditDiscontinuity{Line: 5, Reason: ReasonIncompleteLastLine, DamagedLine: 4, Bytes: int64(len(damage)), Digest: Digest(damage)}
	if repaired != want {
		t.Fatalf("repaired = %+v, want %+v", repaired, want)
	}
	after := readTrailFile(t, dir)
	if !bytes.HasPrefix(after, before) {
		t.Fatal("a repair removes and rewrites nothing")
	}
	lines := splitLines(after)
	if string(lines[3]) != string(damage) {
		t.Fatalf("the damaged bytes are kept in place as line 4: %q", lines[3])
	}
	var record map[string]any
	if err := json.Unmarshal(lines[4], &record); err != nil {
		t.Fatal(err)
	}
	link := readChain(t, lines[4])
	if record["kind"] != KindDiscontinuity || record["surface"] != "audit repair" || record["disposition"] != nil || record["pack"] != nil ||
		link.sequence != 5 || link.previous != Digest(lines[2]) || link.trail != checkpoint.Trail {
		t.Fatalf("the discontinuity links over the damage to line 3: %s", lines[4])
	}
	if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatalf("the writer chains after the discontinuity: %v", err)
	}
	after = readTrailFile(t, dir)
	chain := verifyBytes(t, after, checkpoint)
	if chain.Status != "segmented" || chain.FindingsTotal != 0 || chain.Coverage.Damaged != 1 || chain.Coverage.Chained != 5 ||
		len(chain.Discontinuities) != 1 || chain.Discontinuities[0] != want ||
		len(chain.Segments) != 2 || chain.Segments[0] != (result.AuditSegment{FirstLine: 1, LastLine: 3}) || chain.Segments[1] != (result.AuditSegment{FirstLine: 5, LastLine: 6}) ||
		chain.Coverage.Checkpointed.Through != 3 || !containsString(chain.DoesNotEstablish, notSegmented) {
		t.Fatalf("chain = %+v findings = %v", chain, findingNames(chain))
	}
	if _, err := writer.Repair(); !errors.Is(err, ErrNothingToRepair) {
		t.Fatalf("a repaired trail has nothing to repair: %v", err)
	}
	// A checkpoint naming the damaged line names no chained record.
	damagedPoint := &result.AuditCheckpoint{CheckpointVersion: "1", Trail: checkpoint.Trail, Sequence: 4, RecordDigest: Digest(damage)}
	if chain := verifyBytes(t, after, damagedPoint); strings.Join(findingNames(chain), " ") != "checkpoint-not-chained@4" {
		t.Fatalf("findings = %v", findingNames(chain))
	}
}

// A damaged line is the damage its discontinuity names, whatever it holds: a
// torn write that happens to be a whole record of another trail, with the wrong
// sequence and link, is not held to the chain, and the discontinuity still
// follows the last intact line under the trail's own identity.
func TestADamagedLineIsNotHeldToTheChain(t *testing.T) {
	writer, dir := writerAt(t, "audit")
	writeRecords(t, writer.root, `{}`, `{}`)
	appendRaw(t, dir, `{"recordVersion":"1","trail":"`+strings.Repeat("f", 32)+`","sequence":99,"previous":"`+Digest([]byte("x"))+`","run":"r"}`)
	if _, err := writer.Repair(); err != nil {
		t.Fatal(err)
	}
	writeRecords(t, writer.root, `{}`)
	chain := verifyBytes(t, readTrailFile(t, dir), nil)
	if chain.Status != "segmented" || chain.FindingsTotal != 0 || chain.Coverage.Damaged != 1 || chain.Coverage.Chained != 4 {
		t.Fatalf("chain = %+v findings = %v", chain.Coverage, findingNames(chain))
	}
}

// A discontinuity is held to what it names: its damaged line's bytes, and the
// line before it. Edit the damaged line, or move the record, and the trail is
// invalid.
func TestADiscontinuityIsHeldToWhatItNames(t *testing.T) {
	writer, dir := tornTrail(t, 2)
	if _, err := writer.Repair(); err != nil {
		t.Fatal(err)
	}
	lines := splitLines(readTrailFile(t, dir))
	if chain := verifyBytes(t, joinLines(lines), nil); chain.Status != "segmented" {
		t.Fatalf("the repaired fixture is segmented: %v", findingNames(chain))
	}
	edited := append([][]byte{}, lines...)
	edited[2] = append(append([]byte{}, edited[2]...), 'x')
	if chain := verifyBytes(t, joinLines(edited), nil); !hasFinding(chain, FindingDiscontinuityMismatch) {
		t.Fatalf("an edited damaged line: %v", findingNames(chain))
	}
	moved := [][]byte{lines[0], lines[1], lines[2], []byte(`{"recordVersion":"1","run":"between"}`), lines[3]}
	if chain := verifyBytes(t, joinLines(moved), nil); !hasFinding(chain, FindingDiscontinuityMalformed) {
		t.Fatalf("a discontinuity not after the line it names: %v", findingNames(chain))
	}
	for name, member := range map[string]string{
		"an unknown reason":   `{"reason":"other","line":3,"bytes":1,"digest":"` + Digest(nil) + `"}`,
		"a member missing":    `{"reason":"incomplete-last-line","line":3,"bytes":1}`,
		"an extra member":     `{"reason":"incomplete-last-line","line":3,"bytes":1,"digest":"` + Digest(nil) + `","x":1}`,
		"a line of zero":      `{"reason":"incomplete-last-line","line":0,"bytes":1,"digest":"` + Digest(nil) + `"}`,
		"a digest of no form": `{"reason":"incomplete-last-line","line":3,"bytes":1,"digest":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			forged := append([][]byte{}, lines...)
			start := bytes.Index(forged[3], []byte(`"discontinuity":`))
			forged[3] = append(append(append([]byte{}, forged[3][:start]...), []byte(`"discontinuity":`+member)...), '}')
			if chain := verifyBytes(t, joinLines(forged), nil); !hasFinding(chain, FindingDiscontinuityMalformed) {
				t.Fatalf("findings = %v", findingNames(chain))
			}
		})
	}
}

// A repair refuses when nothing is damaged, for a writer that does not chain,
// for damage longer than the bound, without the lock, and for a trail that is
// not there; and a trail that is only damage is repaired into a new one.
func TestRepairRefusesWhatItCannotDoSafely(t *testing.T) {
	_, _, _ = chainedTrail(t, 1)
	writer, dir := writerAt(t, "audit")
	if _, err := writer.Repair(); !errors.Is(err, ErrNoTrail) {
		t.Fatalf("no trail: %v", err)
	}
	writeRecords(t, writer.root, `{}`)
	if _, err := writer.Repair(); !errors.Is(err, ErrNothingToRepair) {
		t.Fatalf("nothing damaged: %v", err)
	}
	unchained := NewWriter(writer.root, "audit", false)
	if _, err := unchained.Repair(); !errors.Is(err, ErrRepairUnchained) {
		t.Fatalf("a writer that does not chain: %v", err)
	}
	before := readTrailFile(t, dir)
	if _, _, err := writer.repairLines(fssecure.AppendState{Locked: false, Size: int64(len(before)), Contents: bytes.NewReader(before)}); !errors.Is(err, ErrRepairNeedsLock) {
		t.Fatalf("without the lock: %v", err)
	}
	// Damage over the bound, after a short line and with nothing before it,
	// under a bound the line before it and the discontinuity record would
	// both be within; and damage within the bound, with nothing before it,
	// whose discontinuity record would not be.
	short, shortDir := writerAt(t, "audit")
	appendRaw(t, shortDir, `{"recordVersion":"1","run":"a"}`+"\n"+strings.Repeat("x", 1000))
	bare, bareDir := writerAt(t, "audit")
	appendRaw(t, bareDir, strings.Repeat("x", 1000))
	small, smallDir := writerAt(t, "audit")
	appendRaw(t, smallDir, strings.Repeat("x", 200))
	unchanged := map[string][]byte{shortDir: readTrailFile(t, shortDir), bareDir: readTrailFile(t, bareDir), smallDir: readTrailFile(t, smallDir)}
	func() {
		original := maxLineBytes
		defer func() { maxLineBytes = original }()
		maxLineBytes = 600
		if _, err := short.Repair(); !errors.Is(err, ErrOversizedLine) {
			t.Fatalf("damage over the bound after a line: %v", err)
		}
		if _, err := bare.Repair(); !errors.Is(err, ErrOversizedLine) {
			t.Fatalf("damage over the bound with nothing before it: %v", err)
		}
		maxLineBytes = 300
		if _, err := small.Repair(); !errors.Is(err, ErrRecordTooLarge) {
			t.Fatalf("a discontinuity record over the bound: %v", err)
		}
	}()
	for refusedDir, before := range unchanged {
		if after := readTrailFile(t, refusedDir); !bytes.Equal(after, before) {
			t.Fatal("a refused repair appends nothing")
		}
	}

	only, onlyDir := writerAt(t, "audit")
	appendRaw(t, onlyDir, `{"recordVersion"`)
	repaired, err := only.Repair()
	if err != nil || repaired.DamagedLine != 1 || repaired.Line != 2 {
		t.Fatalf("a trail that is only damage: %+v %v", repaired, err)
	}
	chain := verifyBytes(t, readTrailFile(t, onlyDir), nil)
	if chain.Status != "segmented" || len(chain.Segments) != 1 || chain.Segments[0] != (result.AuditSegment{FirstLine: 2, LastLine: 2}) {
		t.Fatalf("chain = %+v findings = %v", chain, findingNames(chain))
	}
	link := readChain(t, splitLines(readTrailFile(t, onlyDir))[1])
	if link.previous != Digest(nil) {
		t.Fatalf("with nothing before the damage the record follows the empty trail: %+v", link)
	}
}

// After unchained lines, a repair's record commits to the whole trail before
// the damage and keeps the last chained identity, as the writer would.
func TestARepairAfterUnchainedLinesFollowsTheWritersRule(t *testing.T) {
	writer, dir := writerAt(t, "audit")
	writeRecords(t, writer.root, `{"n":1}`)
	writer.chain = false
	if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	writer.chain = true
	before := readTrailFile(t, dir)
	appendRaw(t, dir, `{"torn`)
	if _, err := writer.Repair(); err != nil {
		t.Fatal(err)
	}
	lines := splitLines(readTrailFile(t, dir))
	link := readChain(t, lines[3])
	if link.previous != Digest(before) || link.trail != readChain(t, lines[0]).trail || link.sequence != 4 {
		t.Fatalf("link = %+v", link)
	}
	if chain := verifyBytes(t, readTrailFile(t, dir), nil); chain.Status != "segmented" || chain.FindingsTotal != 0 {
		t.Fatalf("findings = %v", findingNames(chain))
	}
}

// A checkpoint is written in its canonical form and read back exactly; any
// other shape is refused, and a later version is refused as one.
func TestACheckpointIsCanonicalAndReadStrictly(t *testing.T) {
	checkpoint := result.AuditCheckpoint{CheckpointVersion: "1", Trail: strings.Repeat("ab", 16), Sequence: 42, RecordDigest: Digest([]byte("x"))}
	encoded := EncodeCheckpoint(checkpoint)
	// RFC 8785 for this value: members in code-point order, no whitespace,
	// the integer as its shortest decimal, the strings unescaped. jcs.Encode
	// takes no numbers, so the members other than the sequence are checked
	// through it and the whole against the literal.
	want := `{"checkpointVersion":"1","recordDigest":"` + checkpoint.RecordDigest + `","sequence":42,"trail":"` + checkpoint.Trail + `"}` + "\n"
	if string(encoded) != want {
		t.Fatalf("the checkpoint is its own canonical form:\n%s%s", encoded, want)
	}
	strings_, err := jcs.Encode(map[string]any{"checkpointVersion": "1", "recordDigest": checkpoint.RecordDigest, "trail": checkpoint.Trail})
	if err != nil || !strings.HasPrefix(string(strings_), `{"checkpointVersion":"1","recordDigest":"`+checkpoint.RecordDigest+`"`) {
		t.Fatalf("the members are in canonical order: %s %v", strings_, err)
	}
	if parsed, err := ParseCheckpoint(append([]byte("  "), encoded...)); err != nil || parsed != checkpoint {
		t.Fatalf("round trip: %+v %v", parsed, err)
	}
	digest := Digest([]byte("x"))
	for name, document := range map[string]string{
		"not JSON":             `{"checkpointVersion":"1"`,
		"two texts":            string(encoded) + string(encoded),
		"an array":             `[]`,
		"an unknown member":    `{"checkpointVersion":"1","trail":"` + checkpoint.Trail + `","sequence":42,"recordDigest":"` + digest + `","at":"now"}`,
		"a member twice":       `{"checkpointVersion":"1","trail":"` + checkpoint.Trail + `","trail":"` + checkpoint.Trail + `","sequence":42,"recordDigest":"` + digest + `"}`,
		"a member missing":     `{"checkpointVersion":"1","trail":"` + checkpoint.Trail + `","sequence":42}`,
		"an uppercase trail":   `{"checkpointVersion":"1","trail":"` + strings.ToUpper(checkpoint.Trail) + `","sequence":42,"recordDigest":"` + digest + `"}`,
		"a sequence of zero":   `{"checkpointVersion":"1","trail":"` + checkpoint.Trail + `","sequence":0,"recordDigest":"` + digest + `"}`,
		"a sequence as text":   `{"checkpointVersion":"1","trail":"` + checkpoint.Trail + `","sequence":"42","recordDigest":"` + digest + `"}`,
		"a bare digest":        `{"checkpointVersion":"1","trail":"` + checkpoint.Trail + `","sequence":42,"recordDigest":"` + strings.TrimPrefix(digest, "sha256:") + `"}`,
		"a version as integer": `{"checkpointVersion":1,"trail":"` + checkpoint.Trail + `","sequence":42,"recordDigest":"` + digest + `"}`,
		"too long":             strings.Repeat(" ", MaxCheckpointBytes) + string(encoded),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseCheckpoint([]byte(document)); err == nil || errors.Is(err, ErrCheckpointVersion) {
				t.Fatalf("refused as invalid: %v", err)
			}
		})
	}
	later := `{"checkpointVersion":"2","trail":"` + checkpoint.Trail + `","sequence":42,"recordDigest":"` + digest + `"}`
	if _, err := ParseCheckpoint([]byte(later)); !errors.Is(err, ErrCheckpointVersion) {
		t.Fatalf("a later version: %v", err)
	}
}

// A verification taken while writers append sees only whole writes: its size
// is read under the writer's lock, so every snapshot ends between two writes
// and verifies, never with a write cut in two.
func TestVerifyingWhileWritersAppendSeesOnlyWholeWrites(t *testing.T) {
	_, root := writerAt(t, "audit")
	opened, err := fssecure.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	writeRecords(t, opened, `{}`)
	var group sync.WaitGroup
	stop := make(chan struct{})
	failures := make(chan error, 64)
	for range 3 {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := 0; ; index++ {
				select {
				case <-stop:
					return
				default:
				}
				failures <- appendBatch(opened, index%2 == 1)
			}
		}()
	}
	go func() {
		for err := range failures {
			if err != nil {
				t.Error(err)
			}
		}
	}()
	previous := int64(0)
	for range 40 {
		file, err := os.Open(filepath.Join(root, "audit", FileName))
		if err != nil {
			t.Fatal(err)
		}
		size, locked, err := fssecure.SizeBetweenWrites(file)
		if err != nil {
			t.Fatal(err)
		}
		report, err := Verify(file, size, Options{})
		chain := report.Chain
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if locked && (chain.Status != "valid" || chain.Lines < previous) {
			t.Fatalf("a snapshot between writes verifies: %v", findingNames(chain))
		}
		previous = chain.Lines
	}
	close(stop)
	group.Wait()
	close(failures)
}

// Every report says, once and last among what it does not establish, that the
// trail is silent about evaluations refused, rehearsed or failed before a
// disposition (ADR-0048), whatever was supplied and whatever was found: no
// checkpoint, key or stamp covers a line that was never written, so the
// checkpointed report, the one a counterparty reads, says it as the bare one
// does, and so do a repaired trail's report and a report failing a required
// coverage.
func TestEveryReportSaysTheTrailIsSilentAboutAttempts(t *testing.T) {
	_, _, chained := chainedTrail(t, 3)
	signer := signerOf(t, vectorSeed1)
	_, signedDir := signedTrail(t, signer, 3)
	signed, sidecar := readTrailFile(t, signedDir), readSidecar(t, signedDir)
	tsa := testAuthority(t, tsatest.Options{})
	cleanList, err := tsa.CRL(time.Now().Add(time.Hour), time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(tsa.Root)
	edited := splitLines(chained)
	edited[0] = bytes.Replace(edited[0], []byte(`"n":0`), []byte(`"n":9`), 1)
	unchained, unchainedDir := writerAt(t, "audit")
	if err := NewWriter(unchained.root, "audit", false).Evaluation(evaluated(), Inputs{Facts: []byte(`{"n":0}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	// A torn trail, repaired, with a record chained after the discontinuity.
	torn, tornDir := tornTrail(t, 3)
	if _, err := torn.Repair(); err != nil {
		t.Fatal(err)
	}
	if err := torn.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	// The sidecar without its last line: record 3 is unsigned.
	unsignedLast := joinLines(splitLines(sidecar)[:2])

	signaturesOf := func(lines []byte) *SignatureOptions {
		return &SignatureOptions{Keys: keys(signer), Sidecar: bytes.NewReader(lines), SidecarSize: int64(len(lines)), RequireThrough: 3}
	}
	signatures := func() *SignatureOptions { return signaturesOf(sidecar) }
	stampsThrough := func(trail []byte, through int64, crls ...*x509.RevocationList) *StampOptions {
		stamped := stampOf(t, tsa, checkpointAt(t, trail, through))
		return &StampOptions{
			Verify:         timestamp.VerifyOptions{Roots: roots, CRLs: crls},
			Stamps:         bytes.NewReader(stamped),
			StampsSize:     int64(len(stamped)),
			RequireThrough: 3,
		}
	}
	stamps := func(trail []byte, crls ...*x509.RevocationList) *StampOptions {
		return stampsThrough(trail, 3, crls...)
	}
	// A witness that countersigned records 2 and 3 of the chained trail.
	witness := newTestWitness("every report")
	witness.next(WitnessKindCheckpoint, lineCheckpoint(chained, 2))
	witness.next(WitnessKindCheckpoint, lineCheckpoint(chained, 3))
	witnessed := func(require int64, files ...[]byte) *WitnessOptions {
		input, err := PrepareWitness(witness.supplied(files...))
		if err != nil {
			t.Fatal(err)
		}
		return &WitnessOptions{Input: input, RequireThrough: require}
	}
	cases := []struct {
		name  string
		trail []byte
		opts  Options
		check func(result.AuditChain) bool
	}{
		{"a witness's statements", chained, Options{Witness: witnessed(3, witness.all())}, func(c result.AuditChain) bool {
			return c.Status == "valid" && c.Coverage.Countersigned == (result.AuditCoverageState{Status: "through", Through: 3}) &&
				c.RequiredCountersigned.Status == "met"
		}},
		{"an unmet countersigned requirement", chained, Options{Witness: witnessed(3, witness.file(0, 1))}, func(c result.AuditChain) bool {
			return c.Status == "invalid" && c.RequiredCountersigned != nil && c.RequiredCountersigned.Status == "unmet" &&
				strings.Join(findingNames(c), " ") == "countersigned-coverage-missing@3"
		}},
		{"a witness's statements with a finding", chained, Options{Witness: witnessed(0, witness.file(1, 2))}, func(c result.AuditChain) bool {
			return c.Status == "invalid" && c.Coverage.Countersigned.Status == "failed" &&
				strings.Join(findingNames(c), " ") == "witness-chain-broken@0"
		}},
		{"no inputs", chained, Options{}, func(c result.AuditChain) bool {
			return c.Status == "valid" && c.Coverage.Checkpointed.Status == "not-supplied"
		}},
		{"a held checkpoint", chained, Options{Held: keptCheckpoints(t, chained, 3)}, func(c result.AuditChain) bool {
			return c.Status == "valid" && c.Coverage.Checkpointed == (result.AuditCoverageState{Status: "through", Through: 3})
		}},
		{"a public key", signed, Options{Signatures: signatures()}, func(c result.AuditChain) bool {
			return c.Status == "valid" && c.Coverage.Signed == (result.AuditCoverageState{Status: "through", Through: 3})
		}},
		{"time-stamping roots", chained, Options{Stamps: stamps(chained)}, func(c result.AuditChain) bool {
			return c.Status == "valid" && c.Coverage.Stamped == (result.AuditCoverageState{Status: "through", Through: 3})
		}},
		{"every coverage full", signed, Options{
			Held:           keptCheckpoints(t, signed, 3),
			RequireThrough: 3,
			Signatures:     signatures(),
			Stamps:         stamps(signed, parseList(t, cleanList)),
		}, func(c result.AuditChain) bool {
			return c.Status == "valid" && c.Scope == ScopeCheckpoint &&
				c.Coverage.Checkpointed == (result.AuditCoverageState{Status: "through", Through: 3}) &&
				c.Coverage.Signed == (result.AuditCoverageState{Status: "through", Through: 3}) &&
				c.Coverage.Stamped == (result.AuditCoverageState{Status: "through", Through: 3}) &&
				c.Coverage.Witnessed == 3 && c.Coverage.Unwitnessed == 0 && c.Coverage.UnsignedRecords == 0 &&
				c.Stamps.RevocationChecked == 1 && c.Stamps.RevocationNotChecked == 0 &&
				c.Required.Status == "met" && c.RequiredSigned.Status == "met" && c.RequiredStamped.Status == "met"
		}},
		{"a failed check", joinLines(edited), Options{}, func(c result.AuditChain) bool {
			return c.Status == "invalid" && len(c.Establishes) == 0
		}},
		{"no chained line", readTrailFile(t, unchainedDir), Options{}, func(c result.AuditChain) bool {
			return c.Status == "valid" && c.Coverage.Chained == 0 && c.Coverage.Uncovered == 1
		}},
		{"a repaired trail", readTrailFile(t, tornDir), Options{}, func(c result.AuditChain) bool {
			return c.Status == "segmented" && c.DiscontinuitiesTotal == 1 && c.Coverage.Damaged == 1 && c.FindingsTotal == 0
		}},
		{"an unmet checkpoint requirement", chained, Options{Held: keptCheckpoints(t, chained, 2), RequireThrough: 3}, func(c result.AuditChain) bool {
			return c.Status == "invalid" && c.Required != nil && c.Required.Status == "unmet" &&
				strings.Join(findingNames(c), " ") == "checkpoint-coverage-missing@3"
		}},
		{"an unmet signature requirement", signed, Options{Signatures: signaturesOf(unsignedLast)}, func(c result.AuditChain) bool {
			return c.Status == "invalid" && c.RequiredSigned != nil && c.RequiredSigned.Status == "unmet" &&
				strings.Join(findingNames(c), " ") == "signature-missing@3"
		}},
		{"an unmet stamp requirement", chained, Options{Stamps: stampsThrough(chained, 2)}, func(c result.AuditChain) bool {
			return c.Status == "invalid" && c.RequiredStamped != nil && c.RequiredStamped.Status == "unmet" &&
				strings.Join(findingNames(c), " ") == "stamp-coverage-missing@3"
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			chain := verifyWith(t, c.trail, c.opts).Chain
			if !c.check(chain) {
				t.Fatalf("not the report this case is about: %s %v %+v", chain.Status, findingNames(chain), chain.Coverage)
			}
			count := 0
			for _, statement := range chain.DoesNotEstablish {
				if statement == notAttempts {
					count++
				}
			}
			if count != 1 || chain.DoesNotEstablish[len(chain.DoesNotEstablish)-1] != notAttempts {
				t.Fatalf("the sentence on refused and rehearsed evaluations appears %d time(s), and last: %v; statements: %q",
					count, len(chain.DoesNotEstablish) > 0 && chain.DoesNotEstablish[len(chain.DoesNotEstablish)-1] == notAttempts, chain.DoesNotEstablish)
			}
		})
	}
}
