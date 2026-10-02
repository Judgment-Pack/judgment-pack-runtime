package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The names of the checks a trail can fail (ADR-0047 §1). They are stable: a
// reader may branch on them.
const (
	// FindingIncompleteLastLine is bytes after the last newline: a write that
	// did not complete, which audit repair starts a new segment after.
	FindingIncompleteLastLine = "incomplete-last-line"
	// FindingLineTooLong is a line longer than maxLineBytes, which is not read
	// whole and so cannot be classified.
	FindingLineTooLong = "line-too-long"
	// FindingSequenceMismatch is a chained line whose sequence is not its line
	// number.
	FindingSequenceMismatch = "sequence-mismatch"
	// FindingPreviousMismatch is a chained line whose previous is not what the
	// lines before it make it: the digest of the line before when that line is
	// chained, of the whole file before it when it is not.
	FindingPreviousMismatch = "previous-mismatch"
	// FindingTrailMismatch is a chained line whose trail is not the identity
	// of the last chained line before it.
	FindingTrailMismatch = "trail-mismatch"
	// FindingDiscontinuityMalformed is a discontinuity record whose
	// discontinuity member is not of its shape, or names a line other than the
	// one before it.
	FindingDiscontinuityMalformed = "discontinuity-malformed"
	// FindingDiscontinuityMismatch is a discontinuity record whose damaged
	// line does not have the length or the digest the record states.
	FindingDiscontinuityMismatch = "discontinuity-mismatch"
	// FindingCheckpointBeyondTrail is a checkpoint naming a sequence the
	// trail does not reach: a trail shorter than the one checkpointed.
	FindingCheckpointBeyondTrail = "checkpoint-beyond-trail"
	// FindingCheckpointNotChained is a checkpoint naming a line that is not a
	// chained record.
	FindingCheckpointNotChained = "checkpoint-not-chained"
	// FindingCheckpointTrailMismatch is a checkpoint naming another trail.
	FindingCheckpointTrailMismatch = "checkpoint-trail-mismatch"
	// FindingCheckpointRecordMismatch is a checkpoint whose record digest is
	// not the digest of the line at its sequence: another record is there.
	FindingCheckpointRecordMismatch = "checkpoint-record-mismatch"
)

// The scopes a verification reports, which say what was checked.
const (
	// ScopeOneSuppliedChain is a verification with no checkpoint: the
	// integrity of one supplied chain, and nothing about whether it is the
	// project's whole or authentic trail.
	ScopeOneSuppliedChain = "one-supplied-chain"
	// ScopeCheckpoint is a verification held to a checkpoint supplied with it.
	ScopeCheckpoint = "checkpoint"
)

// maxFindings bounds the findings a report lists; FindingsTotal counts them all.
// A line deleted near the start of a long trail fails the sequence of every
// line after it, and listing each would bury the first.
const maxFindings = 100

// The statements a report makes about what its result does and does not
// establish. They are fixed sentences, so a reader can compare them.
const (
	establishesConsistency = "The chained lines are consistent with one another: no line before the last was edited, inserted, deleted or moved without breaking a link, and the lines before the first chained line are the block its previous commits to."
	establishesCheckpoint  = "Lines 1 to %d are the lines that existed when the checkpoint was made, if the checkpoint was held independently of the trail's operator."
	notLastLine            = "The last line, and any lines rewritten from some point on with their links recomputed, are not authenticated by the chain: only a checkpoint covering them, held independently of the operator, shows they are the ones first written."
	notLastLineAfter       = "Lines after %d, the checkpoint's sequence, are not authenticated by the chain: only a later checkpoint covering them, held independently of the operator, shows they are the ones first written."
	notComplete            = "That the trail is complete: a trail cut short is as consistent as the whole one, and nothing here says which decisions were never written to it."
	notCompleteAfter       = "That the trail is complete after line %d: lines removed from its end since the checkpoint was made are not missed."
	notTime                = "That any record's at is true: it is the operator's clock."
	notSigned              = "Who wrote any record: signatures are not available yet (runtime #209)."
	notSegmented           = "That the history is intact across a discontinuity: a repair keeps the damaged line in place and links over it, so what the damaged line held is not part of any segment."
)

// Verify reads a trail of size bytes, line by line and over its exact bytes,
// and checks every trail, sequence and previous from the first chained record
// on, by the rules the writer chains by (see the package doc): a chained line's
// sequence is its line number; it carries the identity of the last chained
// line before it; and its previous is the digest of the line before it when
// that line is chained, and of the whole file before it when it is not. Lines
// are recognised by chainedLine's rule, and no line is ever decoded and encoded
// again: each digest is taken over the bytes read.
//
// A discontinuity record (audit repair) is the one record whose previous does
// not follow the line before it: that line is the damage it names, kept in
// place, and the record links over it to what a record in the damaged line's
// place would have followed. Its damaged line is held to the length and digest
// the record states and is otherwise not read, and the report's segments are
// split there.
//
// With expect, the trail is also held to a checkpoint: the line at its
// sequence must be a chained record of its trail whose exact bytes have its
// digest. A trail shorter than that sequence fails, as does one with another
// identity or another record there.
//
// The error is for a trail that could not be read; every check the trail
// fails is a finding in the report.
func Verify(contents io.ReaderAt, size int64, expect *result.AuditCheckpoint) (result.AuditChain, error) {
	v := newVerifier(expect)
	reader := bufio.NewReaderSize(io.NewSectionReader(contents, 0, size), readChunk)
	for {
		ended, err := v.readLine(reader)
		if err != nil {
			return result.AuditChain{}, err
		}
		if ended {
			break
		}
	}
	return v.report(size), nil
}

// seenLine is what the verifier keeps of one line: enough to link the next
// line to it, to check a discontinuity naming it, and to check a checkpoint
// naming it. Its bytes are not kept.
type seenLine struct {
	number    int64
	length    int64
	digest    string
	oversized bool
	chained   bool
	link      link
	// before is what a record in this line's place would follow, and
	// beforeKnown says whether that could be read: it cannot after a line too
	// long to classify. beforeRule says which rule gave it.
	before      head
	beforeKnown bool
	beforeRule  string
	// findings are this line's own, held until the next line is read, since a
	// discontinuity there can name this line as damaged.
	findings []result.AuditFinding
	damaged  bool
	// discontinuityKind says the line is a JSON object whose kind is
	// discontinuity, whatever else it holds: no discontinuity may name such a
	// line as damaged.
	discontinuityKind bool
}

// The rules a chained line's previous is held to, named in a finding.
const (
	ruleLine          = "the digest of the line before it"
	rulePrefix        = "the digest of the whole trail before it"
	ruleDiscontinuity = "what a record in the damaged line's place would have followed"
)

type verifier struct {
	expect *result.AuditCheckpoint
	whole  hash.Hash

	lines     int64
	previous  *seenLine // the line before the one being read
	lastTrail string    // the identity of the last chained line read

	checkpointed *seenLine
	head         *seenLine

	pendingUnchained int64
	firstChained     bool
	coverage         result.AuditCoverage
	findings         []result.AuditFinding
	findingsTotal    int
	// The discontinuities and the segments they split the trail into are
	// listed up to maxListed each and counted in full, so a trail of many
	// repairs costs a verification the same memory as a trail of few.
	discontinuities      []result.AuditDiscontinuity
	discontinuitiesTotal int64
	segments             []result.AuditSegment
	segmentsTotal        int64
	segmentStart         int64
	// earliestFinding is the lowest line any finding is about, or 0: a
	// checkpoint is matched through its sequence only when no line up to it
	// failed a check.
	earliestFinding int64
}

func newVerifier(expect *result.AuditCheckpoint) *verifier {
	return &verifier{expect: expect, whole: sha256.New(), segmentStart: 1}
}

// maxListed bounds the discontinuities and the segments a report lists, as
// maxFindings bounds its findings; their totals are counted in full.
const maxListed = 100

// readLine reads one line and checks it, reporting whether the trail ended.
func (v *verifier) readLine(reader *bufio.Reader) (bool, error) {
	prefix := "sha256:" + hex.EncodeToString(v.whole.Sum(nil))
	lineHash := sha256.New()
	line := []byte{}
	var length int64
	oversized := false
	terminated := false
	for {
		piece, err := reader.ReadSlice('\n')
		v.whole.Write(piece)
		body := piece
		if len(body) > 0 && body[len(body)-1] == '\n' {
			body = body[:len(body)-1]
			terminated = true
		}
		lineHash.Write(body)
		length += int64(len(body))
		if !oversized {
			if length > maxLineBytes {
				oversized, line = true, nil
			} else {
				line = append(line, body...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		break
	}
	if !terminated {
		// Bytes after the last newline are a write that did not complete;
		// none is an ordinary end. Either way the trail ends here, and the
		// last complete line is settled: nothing after it can name it.
		v.commit()
		if length > 0 {
			v.record(result.AuditFinding{
				Name:   FindingIncompleteLastLine,
				Line:   v.lines + 1,
				Detail: fmt.Sprintf("the trail ends in %d bytes with no newline: a write that did not complete", length),
			})
		}
		return true, nil
	}
	v.lines++
	seen := &seenLine{
		number:    v.lines,
		length:    length,
		digest:    "sha256:" + hex.EncodeToString(lineHash.Sum(nil)),
		oversized: oversized,
	}
	seen.before, seen.beforeKnown, seen.beforeRule = v.headBefore(prefix)
	v.check(seen, line)
	v.commit()
	v.previous = seen
	if v.expect != nil && seen.number == v.expect.Sequence {
		v.checkpointed = seen
	}
	return false, nil
}

// headBefore is what a record in the line being read would follow: the line
// before it when that line is chained, the whole trail before it when it is
// not (prefix), and nothing knowable after a line too long to classify. It is
// readHead's rule, applied at every line.
func (v *verifier) headBefore(prefix string) (head, bool, string) {
	before := v.previous
	switch {
	case before != nil && before.oversized:
		return head{}, false, ""
	case before != nil && before.chained:
		return head{trail: before.link.trail, previous: before.digest}, true, ruleLine
	default:
		return head{trail: v.lastTrail, previous: prefix}, true, rulePrefix
	}
}

// check classifies one line and makes its checks.
func (v *verifier) check(seen *seenLine, line []byte) {
	if seen.oversized {
		seen.findings = append(seen.findings, result.AuditFinding{
			Name:   FindingLineTooLong,
			Line:   seen.number,
			Detail: fmt.Sprintf("the line is %d bytes, longer than any line a chained trail holds, so whether it is chained cannot be read", seen.length),
		})
		return
	}
	found, members, ok := readLink(line)
	var kind string
	seen.discontinuityKind = members != nil && decodeString(members["kind"], &kind) == nil && kind == KindDiscontinuity
	if !ok {
		return
	}
	seen.chained, seen.link = true, found
	expected, known, rule := seen.before, seen.beforeKnown, seen.beforeRule
	if parsed, isDiscontinuity := discontinuityOf(members); isDiscontinuity {
		expected, known, rule = v.checkDiscontinuity(seen, parsed)
	}
	if found.sequence != seen.number {
		seen.findings = append(seen.findings, result.AuditFinding{
			Name:   FindingSequenceMismatch,
			Line:   seen.number,
			Detail: fmt.Sprintf("the record's sequence is %d", found.sequence),
		})
	}
	if known {
		if found.previous != expected.previous {
			seen.findings = append(seen.findings, result.AuditFinding{
				Name:   FindingPreviousMismatch,
				Line:   seen.number,
				Detail: "previous is not " + rule,
			})
		}
		if expected.trail != "" && found.trail != expected.trail {
			seen.findings = append(seen.findings, result.AuditFinding{
				Name:   FindingTrailMismatch,
				Line:   seen.number,
				Detail: "the record names another trail than the last chained line before it",
			})
		}
	}
	v.lastTrail = found.trail
}

// discontinuity is a discontinuity record's own member: why the record was
// written, the damaged line it names, and that line's length and digest.
type discontinuity struct {
	Reason string `json:"reason"`
	Line   int64  `json:"line"`
	Bytes  int64  `json:"bytes"`
	Digest string `json:"digest"`
}

// parsedDiscontinuity is a discontinuity member as read: valid when it is of
// its shape.
type parsedDiscontinuity struct {
	valid bool
	value discontinuity
}

// discontinuityOf reads a chained line's kind: true when it is a
// discontinuity, with its discontinuity member as read.
func discontinuityOf(members map[string]json.RawMessage) (parsedDiscontinuity, bool) {
	var kind string
	if decodeString(members["kind"], &kind) != nil || kind != KindDiscontinuity {
		return parsedDiscontinuity{}, false
	}
	inner, err := exactObject(members["discontinuity"])
	if err != nil || len(inner) != 4 {
		return parsedDiscontinuity{}, true
	}
	var d discontinuity
	if decodeString(inner["reason"], &d.Reason) != nil || d.Reason != ReasonIncompleteLastLine ||
		decodeInteger(inner["line"], &d.Line) != nil || d.Line < 1 || d.Line >= maxSafeInteger ||
		decodeInteger(inner["bytes"], &d.Bytes) != nil || d.Bytes < 0 || d.Bytes >= maxSafeInteger ||
		decodeString(inner["digest"], &d.Digest) != nil || !previousForm.MatchString(d.Digest) {
		return parsedDiscontinuity{}, true
	}
	return parsedDiscontinuity{valid: true, value: d}, true
}

// checkDiscontinuity holds a discontinuity record to its rule: it names the
// line before it, that line has the length and digest it states, and it
// follows what a record in that line's place would have followed. The damaged
// line is marked so, and commit then reports none of its own findings, since
// it is the damage the record names. It answers what the record's previous and
// trail are held to.
//
// Two lines can never be named damaged, and a discontinuity naming either is
// malformed and suppresses nothing, so its own link is held to the ordinary
// rule:
//
//   - a line longer than maxLineBytes, which repair refuses and the bound says
//     is refused rather than read, so its line-too-long finding stands;
//   - a line that is itself a discontinuity, which repair refuses too. A
//     discontinuity decides which line is not held to the chain; if a later
//     one could name it damaged, the first one's decision would stand on a
//     line nothing vouches for, and the line it excused, and everything that
//     line binds, would be checked by nothing. Since no discontinuity can be
//     named damaged, each one's decision is final the moment it is read.
func (v *verifier) checkDiscontinuity(seen *seenLine, parsed parsedDiscontinuity) (head, bool, string) {
	damaged := v.previous
	malformed := ""
	switch {
	case !parsed.valid || damaged == nil || parsed.value.Line != damaged.number:
		malformed = "the discontinuity member is not of its shape, or does not name the line before it"
	case damaged.oversized || parsed.value.Bytes > maxLineBytes:
		malformed = "the discontinuity names damage longer than any line a chained trail holds, which is refused rather than repaired"
	case damaged.discontinuityKind:
		malformed = "the discontinuity names another discontinuity as damaged, which no repair writes: a discontinuity's own line is never excused"
	}
	if malformed != "" {
		seen.findings = append(seen.findings, result.AuditFinding{Name: FindingDiscontinuityMalformed, Line: seen.number, Detail: malformed})
		return seen.before, seen.beforeKnown, seen.beforeRule
	}
	damaged.damaged = true
	if damaged.length != parsed.value.Bytes || damaged.digest != parsed.value.Digest {
		seen.findings = append(seen.findings, result.AuditFinding{
			Name:   FindingDiscontinuityMismatch,
			Line:   seen.number,
			Detail: fmt.Sprintf("line %d is not the %d bytes with the digest the discontinuity states", damaged.number, parsed.value.Bytes),
		})
	}
	v.discontinuitiesTotal++
	if len(v.discontinuities) < maxListed {
		v.discontinuities = append(v.discontinuities, result.AuditDiscontinuity{
			Line:        seen.number,
			Reason:      parsed.value.Reason,
			DamagedLine: damaged.number,
			Bytes:       parsed.value.Bytes,
			Digest:      parsed.value.Digest,
		})
	}
	v.closeSegment(damaged.number - 1)
	v.segmentStart = seen.number
	return damaged.before, damaged.beforeKnown, ruleDiscontinuity
}

// closeSegment ends the segment that started at segmentStart at last, when
// it holds any line, listing it when there is room.
func (v *verifier) closeSegment(last int64) {
	if last < v.segmentStart {
		return
	}
	v.segmentsTotal++
	if len(v.segments) < maxListed {
		v.segments = append(v.segments, result.AuditSegment{FirstLine: v.segmentStart, LastLine: last})
	}
}

// commit settles the line before the one just read: nothing after it can now
// withdraw its findings or name it as damaged, so its findings are reported and
// it is counted. It is called once for each line, after the line that follows
// it is checked, or at the end of the trail.
func (v *verifier) commit() {
	settled := v.previous
	if settled == nil {
		return
	}
	if settled.damaged {
		v.coverage.Damaged++
		return
	}
	for _, finding := range settled.findings {
		v.record(finding)
	}
	if !settled.chained {
		v.pendingUnchained++
		return
	}
	v.coverage.Chained++
	if v.firstChained {
		v.coverage.Unchained += v.pendingUnchained
	} else {
		v.coverage.LegacyPrefix += v.pendingUnchained
		v.firstChained = true
	}
	v.pendingUnchained = 0
	v.head = settled
}

func (v *verifier) record(finding result.AuditFinding) {
	if v.earliestFinding == 0 || finding.Line < v.earliestFinding {
		v.earliestFinding = finding.Line
	}
	v.findingsTotal++
	if len(v.findings) < maxFindings {
		v.findings = append(v.findings, finding)
	}
}

// report composes what the reading found.
func (v *verifier) report(size int64) result.AuditChain {
	chain := result.AuditChain{
		Scope:    ScopeOneSuppliedChain,
		Lines:    v.lines,
		Bytes:    size,
		Coverage: v.coverage,
		Findings: v.findings,
	}
	chain.Coverage.Uncovered = v.pendingUnchained
	chain.Coverage.Signed = result.AuditCoverageState{Status: "not-available", Detail: "detached signatures are runtime #209"}
	chain.Coverage.Checkpointed = result.AuditCoverageState{Status: "not-supplied"}
	if v.head != nil {
		chain.Trail = v.head.link.trail
		chain.Head = &result.AuditCheckpoint{
			CheckpointVersion: result.CheckpointVersion,
			RecordDigest:      v.head.digest,
			Sequence:          v.head.number,
			Trail:             v.head.link.trail,
		}
	}
	v.closeSegment(v.lines)
	chain.Segments, chain.SegmentsTotal = v.segments, v.segmentsTotal
	if chain.Segments == nil {
		chain.Segments = []result.AuditSegment{}
	}
	chain.Discontinuities, chain.DiscontinuitiesTotal = v.discontinuities, v.discontinuitiesTotal
	if chain.Discontinuities == nil {
		chain.Discontinuities = []result.AuditDiscontinuity{}
	}
	if v.expect != nil {
		v.checkCheckpoint(&chain)
	}
	chain.Findings = v.findings
	if chain.Findings == nil {
		chain.Findings = []result.AuditFinding{}
	}
	chain.FindingsTotal = v.findingsTotal
	switch {
	case v.findingsTotal > 0:
		chain.Status = "invalid"
	case v.discontinuitiesTotal > 0:
		chain.Status = "segmented"
	default:
		chain.Status = "valid"
	}
	chain.Establishes, chain.DoesNotEstablish = statements(chain)
	return chain
}

// checkCheckpoint holds the trail to the checkpoint it was given: the line at
// its sequence must be there, chained, of its trail, and have its digest. It
// is through that sequence only when it matched and no check of any line up to
// it failed.
func (v *verifier) checkCheckpoint(chain *result.AuditChain) {
	expect := *v.expect
	chain.Scope = ScopeCheckpoint
	chain.Expect = &result.AuditExpectation{Checkpoint: expect, Status: "failed"}
	chain.Coverage.Checkpointed = result.AuditCoverageState{Status: "failed"}
	before := v.findingsTotal
	named := v.checkpointed
	switch {
	case expect.Sequence > v.lines || named == nil:
		v.record(result.AuditFinding{
			Name:   FindingCheckpointBeyondTrail,
			Line:   expect.Sequence,
			Detail: fmt.Sprintf("the trail has %d complete lines, fewer than the checkpoint's sequence", v.lines),
		})
	case named.damaged || !named.chained:
		v.record(result.AuditFinding{
			Name:   FindingCheckpointNotChained,
			Line:   expect.Sequence,
			Detail: "the line at the checkpoint's sequence is not a chained record",
		})
	case named.link.trail != expect.Trail:
		v.record(result.AuditFinding{
			Name:   FindingCheckpointTrailMismatch,
			Line:   expect.Sequence,
			Detail: "the record at the checkpoint's sequence is of another trail",
		})
	case named.digest != expect.RecordDigest:
		v.record(result.AuditFinding{
			Name:   FindingCheckpointRecordMismatch,
			Line:   expect.Sequence,
			Detail: "the record at the checkpoint's sequence is not the one the checkpoint names",
		})
	}
	if v.findingsTotal != before || (v.earliestFinding > 0 && v.earliestFinding <= expect.Sequence) {
		return
	}
	chain.Expect.Status = "matched"
	chain.Coverage.Checkpointed = result.AuditCoverageState{Status: "through", Through: expect.Sequence}
}

// statements says what a report's result establishes and what it does not, in
// the fixed sentences above. Nothing is established about a trail that failed a
// check beyond the findings themselves.
func statements(chain result.AuditChain) ([]string, []string) {
	establishes := []string{}
	if chain.Status != "invalid" {
		establishes = append(establishes, establishesConsistency)
	}
	notEstablished := []string{}
	if chain.Coverage.Checkpointed.Status == "through" {
		through := chain.Coverage.Checkpointed.Through
		establishes = append(establishes, fmt.Sprintf(establishesCheckpoint, through))
		notEstablished = append(notEstablished, fmt.Sprintf(notLastLineAfter, through), fmt.Sprintf(notCompleteAfter, through))
	} else {
		notEstablished = append(notEstablished, notLastLine, notComplete)
	}
	if chain.DiscontinuitiesTotal > 0 {
		notEstablished = append(notEstablished, notSegmented)
	}
	notEstablished = append(notEstablished, notTime, notSigned)
	return establishes, notEstablished
}
