package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// stripFinalNewline leaves the trail's last line without its newline: a write
// that did not complete, as a crash leaves it.
func stripFinalNewline(t *testing.T, dir string) {
	t.Helper()
	trail := filepath.Join(dir, "audit", FileName)
	data, err := os.ReadFile(trail)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatal("the trail already ends without a newline")
	}
	if err := os.WriteFile(trail, data[:len(data)-1], 0o600); err != nil {
		t.Fatal(err)
	}
}

// forgedRepair appends what a repair would append after a trail whose last
// line is incomplete, without repair's refusal of a torn discontinuity: the
// newline that ends the damaged line, and a discontinuity naming it and
// linking over it as the writer's own rules give. It is how a test builds the
// trail a repair of a repair would have left.
func forgedRepair(t *testing.T, data []byte) []byte {
	t.Helper()
	start := int64(bytes.LastIndexByte(data, '\n') + 1)
	damage := data[start:]
	before, err := readHead(bytes.NewReader(data[:start]), start)
	if err != nil {
		t.Fatal(err)
	}
	line, err := encodeJSONLine(discontinuityRecord{
		RecordVersion: RecordVersion,
		Trail:         before.trail,
		Sequence:      before.sequence + 2,
		Previous:      before.previous,
		Run:           "forged0000000000",
		At:            time.Now().UTC().Format(time.RFC3339Nano),
		Kind:          KindDiscontinuity,
		Surface:       "audit repair",
		Tool:          Tool{Name: "jpack", Version: "test"},
		Discontinuity: discontinuity{Reason: ReasonIncompleteLastLine, Line: before.sequence + 1, Bytes: int64(len(damage)), Digest: Digest(damage)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(append(append([]byte{}, data...), '\n'), line...)
}

// A discontinuity is never named damaged. The reviewer's reproduction: write
// A and B, strip B's newline and repair (D1), strip D1's newline. Repair now
// refuses to repair a repair, and the trail a repair of D1 would have left
// (D2, naming D1) does not verify: D2 is malformed and suppresses nothing,
// so D1's own link is checked, and an edit to A, checkpointed through D2, is
// found. The same holds three levels deep and with records after the forged
// repair.
func TestARepairDoesNotRepairARepair(t *testing.T) {
	writer, dir := writerAt(t, "audit")
	writeRecords(t, writer.root, `{"a":1}`, `{"b":2}`)
	stripFinalNewline(t, dir)
	if _, err := writer.Repair(); err != nil {
		t.Fatal(err)
	}
	stripFinalNewline(t, dir)
	torn := readTrailFile(t, dir)
	if _, err := writer.Repair(); !errors.Is(err, ErrRepairDiscontinuity) {
		t.Fatalf("a torn discontinuity is not repaired: %v", err)
	}
	if after := readTrailFile(t, dir); !bytes.Equal(after, torn) {
		t.Fatal("a refused repair appends nothing")
	}

	twice := forgedRepair(t, torn)
	lines := splitLines(twice)
	checkpoint := &result.AuditCheckpoint{CheckpointVersion: "1", Trail: readChain(t, lines[3]).trail, Sequence: 4, RecordDigest: Digest(lines[3])}
	if chain := verifyBytes(t, twice, nil); chain.Status != "invalid" || !hasFinding(chain, FindingDiscontinuityMalformed) || chain.DiscontinuitiesTotal != 1 {
		t.Fatalf("a discontinuity naming a discontinuity never verifies: %v", findingNames(chain))
	}
	edited := append([][]byte{}, lines...)
	edited[0] = bytes.Replace(edited[0], []byte(`"a":1`), []byte(`"a":9`), 1)
	chain := verifyBytes(t, joinLines(edited), checkpoint)
	if chain.Status != "invalid" || chain.Expect.Status != "failed" || chain.Coverage.Checkpointed.Status != "failed" ||
		strings.Join(findingNames(chain), " ") != "previous-mismatch@3 discontinuity-malformed@4 previous-mismatch@4" {
		t.Fatalf("an edit to A behind a damaged discontinuity is found: %v %+v", findingNames(chain), chain.Coverage.Checkpointed)
	}

	// Three levels deep.
	thrice := forgedRepair(t, twice[:len(twice)-1])
	if chain := verifyBytes(t, thrice, nil); chain.Status != "invalid" || !hasFinding(chain, FindingDiscontinuityMalformed) {
		t.Fatalf("three levels: %v", findingNames(chain))
	}
	edited = splitLines(thrice)
	edited[0] = bytes.Replace(edited[0], []byte(`"a":1`), []byte(`"a":9`), 1)
	deep := splitLines(thrice)
	deepPoint := &result.AuditCheckpoint{CheckpointVersion: "1", Trail: checkpoint.Trail, Sequence: int64(len(deep)), RecordDigest: Digest(deep[len(deep)-1])}
	if chain := verifyBytes(t, joinLines(edited), deepPoint); chain.Expect.Status != "failed" || !hasFinding(chain, FindingPreviousMismatch) {
		t.Fatalf("three levels, A edited: %v", findingNames(chain))
	}

	// A damaged discontinuity mid-trail, with records chained after it.
	trail := filepath.Join(dir, "audit", FileName)
	if err := os.WriteFile(trail, twice, 0o600); err != nil {
		t.Fatal(err)
	}
	writeRecords(t, writer.root, `{"c":3}`)
	middle := readTrailFile(t, dir)
	if chain := verifyBytes(t, middle, nil); chain.Status != "invalid" || !hasFinding(chain, FindingDiscontinuityMalformed) || chain.Lines != 5 {
		t.Fatalf("mid-trail: %v", findingNames(chain))
	}
}

// repeated is a file made up as it is read: count copies of fill, then tail.
// It lets a test hold a line at the real bound without holding the line.
type repeated struct {
	fill  byte
	count int64
	tail  []byte
}

func (r repeated) size() int64 { return r.count + int64(len(r.tail)) }

func (r repeated) ReadAt(into []byte, offset int64) (int, error) {
	n := 0
	for ; n < len(into) && offset+int64(n) < r.size(); n++ {
		at := offset + int64(n)
		if at < r.count {
			into[n] = r.fill
		} else {
			into[n] = r.tail[at-r.count]
		}
	}
	if n < len(into) {
		return n, io.EOF
	}
	return n, nil
}

// digestOfFill is the SHA-256 of count copies of fill, in Digest's form.
func digestOfFill(fill byte, count int64) string {
	hash := sha256.New()
	chunk := bytes.Repeat([]byte{fill}, 1<<20)
	for remaining := count; remaining > 0; {
		n := min(remaining, int64(len(chunk)))
		hash.Write(chunk[:n])
		remaining -= n
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

// discontinuityNaming is a discontinuity line at line 2 naming line 1 as
// damaged, with the stated length and digest, following the empty trail.
func discontinuityNaming(t *testing.T, length int64, digest string) []byte {
	t.Helper()
	line, err := encodeJSONLine(discontinuityRecord{
		RecordVersion: RecordVersion, Trail: strings.Repeat("ab", 16), Sequence: 2, Previous: Digest(nil),
		Run: "forged0000000000", At: "2026-10-02T00:00:00Z", Kind: KindDiscontinuity, Surface: "audit repair",
		Tool:          Tool{Name: "jpack", Version: "test"},
		Discontinuity: discontinuity{Reason: ReasonIncompleteLastLine, Line: 1, Bytes: length, Digest: digest},
	})
	if err != nil {
		t.Fatal(err)
	}
	return line
}

// At the real bound: a first line one byte over it is refused as too long
// whether or not a discontinuity follows naming it with the right length and
// digest, since no discontinuity may excuse a line the bound refuses; a first
// line exactly at the bound is read, and a discontinuity may name it.
func TestALineOverTheBoundIsRefusedEvenWhenNamedDamaged(t *testing.T) {
	over := maxLineBytes + 1
	overDigest := digestOfFill('x', over)
	alone := repeated{fill: 'x', count: over, tail: []byte("\n")}
	chain, err := Verify(alone, alone.size(), nil)
	if err != nil || strings.Join(findingNames(chain), " ") != "line-too-long@1" {
		t.Fatalf("alone: %v %v", findingNames(chain), err)
	}
	named := repeated{fill: 'x', count: over, tail: append([]byte("\n"), discontinuityNaming(t, over, overDigest)...)}
	chain, err = Verify(named, named.size(), nil)
	if err != nil || chain.Status != "invalid" || strings.Join(findingNames(chain), " ") != "line-too-long@1 discontinuity-malformed@2" || chain.DiscontinuitiesTotal != 0 {
		t.Fatalf("named damaged: %v %v", findingNames(chain), err)
	}
	at := maxLineBytes
	atBound := repeated{fill: 'x', count: at, tail: append([]byte("\n"), discontinuityNaming(t, at, digestOfFill('x', at))...)}
	chain, err = Verify(atBound, atBound.size(), nil)
	if err != nil || chain.Status != "segmented" || chain.FindingsTotal != 0 {
		t.Fatalf("at the bound: %v %v", findingNames(chain), err)
	}
	// A discontinuity claiming more bytes than the bound is malformed even
	// when the line it names is short.
	claim := discontinuityNaming(t, 1_000_000, Digest([]byte("short")))
	lowerLineBound(t, int64(len(claim)))
	claimed := append([]byte("short\n"), claim...)
	if chain := verifyBytes(t, claimed, nil); !hasFinding(chain, FindingDiscontinuityMalformed) || hasFinding(chain, FindingDiscontinuityMismatch) || hasFinding(chain, FindingLineTooLong) {
		t.Fatalf("a claim over the bound: %v", findingNames(chain))
	}
}

// A trail of many repairs lists a hundred discontinuities and a hundred
// segments, counts them all, and says how many it did not list.
func TestManyDiscontinuitiesAreListedUpToABound(t *testing.T) {
	writer, _ := writerAt(t, "audit")
	data := []byte{}
	const repairs = 300
	for index := range repairs {
		data = append(data, []byte("torn "+strings.Repeat("x", index%7))...)
		line, _, err := writer.discontinuityAfter(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		data = append(append(data, '\n'), line...)
	}
	chain := verifyBytes(t, data, nil)
	if chain.Status != "segmented" || chain.FindingsTotal != 0 || chain.DiscontinuitiesTotal != repairs || len(chain.Discontinuities) != maxListed ||
		chain.SegmentsTotal != repairs || len(chain.Segments) != maxListed || chain.Coverage.Damaged != repairs {
		t.Fatalf("listed %d of %d discontinuities, %d of %d segments, findings %v",
			len(chain.Discontinuities), chain.DiscontinuitiesTotal, len(chain.Segments), chain.SegmentsTotal, findingNames(chain))
	}
	if chain.Discontinuities[0].Line != 2 || chain.Segments[0] != (result.AuditSegment{FirstLine: 2, LastLine: 2}) {
		t.Fatalf("the first are listed: %+v %+v", chain.Discontinuities[0], chain.Segments[0])
	}
}

// A line's digest is over every byte of it, a carriage return included: a
// trail whose line endings were converted to CRLF, as a checkout can convert
// them, is other bytes and does not verify, and its last line no longer has
// the digest a checkpoint holds.
func TestLineEndingsAreBytesToo(t *testing.T) {
	_, _, data := chainedTrail(t, 3)
	head := verifyBytes(t, data, nil).Head
	converted := bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
	if chain := verifyBytes(t, converted, nil); strings.Join(findingNames(chain), " ") != "previous-mismatch@2 previous-mismatch@3" {
		t.Fatalf("findings = %v", findingNames(chain))
	}
	if chain := verifyBytes(t, converted, head); !hasFinding(chain, FindingCheckpointRecordMismatch) {
		t.Fatalf("against the checkpoint: %v", findingNames(chain))
	}
}

// A checkpoint's sequence is an integer literal: a fraction or an exponent
// that spells the same number is refused, as is a leading zero.
func TestACheckpointSequenceIsAnIntegerLiteral(t *testing.T) {
	trail := strings.Repeat("ab", 16)
	digest := Digest([]byte("x"))
	for _, spelled := range []string{"42.0", "4.2e1", "42e0", "42E0", "042", "-42"} {
		document := `{"checkpointVersion":"1","trail":"` + trail + `","sequence":` + spelled + `,"recordDigest":"` + digest + `"}`
		if _, err := ParseCheckpoint([]byte(document)); err == nil {
			t.Fatalf("sequence %s is refused", spelled)
		}
	}
}
