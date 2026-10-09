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
	"sort"
	"time"

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
	// FindingCheckpointCoverageMissing is a coverage the verification was told
	// to require and the held checkpoints do not give: records up to the
	// required sequence that no held checkpoint covers are unwitnessed.
	FindingCheckpointCoverageMissing = "checkpoint-coverage-missing"
)

// The scopes a verification reports, which say what was checked.
const (
	// ScopeCommittedPrefix is a verification with no checkpoint and at least
	// one chained record: the committed prefix of one supplied chain.
	ScopeCommittedPrefix = "committed-prefix"
	// ScopeNoChainedRecord is a verification with no checkpoint and no chained
	// record, so every complete line is uncovered.
	ScopeNoChainedRecord = "no-chained-record"
	// ScopeCheckpoint is a verification held to one or more checkpoints a
	// holder kept and supplied with it.
	ScopeCheckpoint = "checkpoint"
)

// maxFindings bounds the findings a report lists; FindingsTotal counts them all.
// A line deleted near the start of a long trail fails the sequence of every
// line after it, and listing each would bury the first.
const maxFindings = 100

// The statements a report makes about what its result does and does not
// establish. They are fixed sentences, so a reader can compare them.
const (
	establishesConsistency           = "The chain-link checks passed through sequence %d, the last chained record and end of the committed prefix."
	establishesConsistencyBeforeTail = "The chain-link checks passed through sequence %d, the last chained record and end of the committed prefix; the %d uncovered line(s) after it are outside that prefix."
	notChained                       = "That the lines form a chained history: no chained record was found, so all %d line(s) are uncovered."
	notChainedEmpty                  = "That the file contains a chained history: the file is empty."
	establishesCheckpoint            = "Lines 1 to %d are the lines that existed when the checkpoint was made, if the checkpoint was held independently of the trail's operator."
	notLastLine                      = "The last chained line (sequence %d), and any lines rewritten from some point on with their links recomputed, are not authenticated by the chain: only a checkpoint covering them, held independently of the operator, shows they are the ones first written."
	notLastLineAfter                 = "Lines after %d, the checkpoint's sequence, are not authenticated by the chain: only a later checkpoint covering them, held independently of the operator, shows they are the ones first written."
	notComplete                      = "That the trail is complete: a trail cut short is as consistent as the whole one, and nothing here says which decisions were never written to it."
	notCompleteAfter                 = "That the trail is complete after line %d: lines removed from its end since the checkpoint was made are not missed."
	notTime                          = "That any record's at is true: it is the operator's clock."
	notSignedUnchecked               = "Who wrote any record: no public key was supplied, so no signature was checked."
	establishesSigned                = "Lines 1 to %d are as they stood when the signature on record %[1]d was made: altering any of them since takes one of the signing keys from the first public key supplied to the one in force at that record."
	notSignedAfter                   = "Lines after %d are not covered by any signature that was checked: a record signed later, or never, is not authenticated by any key."
	notSignedNone                    = "That any line was signed by a key supplied: no signature that was checked covers one."
	notAgainstOperator               = "Anything against the operator, who holds the signing key: a record the operator altered and signed again, or a trail the operator rewrote from some point on and signed, verifies like the one first written; only a checkpoint held by someone else shows the difference."
	notAfterTheft                    = "Anything after a signing key was copied or stolen: whoever holds it can sign altered records, or a rotation to a key of their own, and only the verifier's own trust configuration refuses what they sign, by revoking that key from the sequence it was taken at and by naming the keys the trail rotates through."
	notSegmented                     = "That the history is intact across a discontinuity: a repair keeps the damaged line in place and links over it, so what the damaged line held is not part of any segment."
	notHeldAll                       = "That the holder kept every checkpoint handed to it, or that none later than those supplied exists: the coverage reaches only the checkpoints supplied here."
	notStampedUnchecked              = "When any checkpoint existed: no time-stamping roots were supplied, so no stamp was checked."
	establishesStamped               = "Lines 1 to %d existed by %s, as a time-stamping authority under a root supplied attests: the time it states, plus the accuracy it states."
	notStampedAfter                  = "That lines after %d existed by any time: no trusted stamp covers them."
	notStampedNone                   = "That any line existed by any time: no trusted stamp covers one."
	notBeforeStamp                   = "When any record was made: a stamp shows its checkpoint existed by the stamp's time, not how long before, so a record's at stays the operator's word; the lag between each prefix-covered record's at and the first stamp covering it is reported, and judging it is the reader's."
	notAgainstAuthority              = "Anything against a time-stamping authority that is not independent of the operator: one that colludes can stamp what it is asked, when it is asked; a root supplied is trusted because the verifier chose it."
	notRevocationChecked             = "That no time-stamping certificate was revoked as of its stamp's time, for %d trusted stamp(s): no revocation list supplied speaks for that time, so their status was not checked."
	// The sentences of a witness's statements (gateway ADR-0013 §6), the first
	// eight as the record states them. notCountersignedNone and
	// notWitnessConflict are this runtime's, for what the record decides
	// without giving its words: a reading that credits no line, and a
	// conflict statement, which fails nothing and is reported with a fixed
	// sentence, from what the record's table says one does not establish.
	establishesCountersigned = "Lines 1 to %d are the lines that existed when a witness under a key supplied signed its statement for checkpoint %[1]d, which it states it did at %s, if that witness is independent of the trail's operator."
	notCountersignedAfter    = "Lines after %d are covered by no statement of a witness under a key supplied."
	notWitnessCurrent        = "That the witness's head for this trail is still index %d: the head supplied is as current as the reader's fetch of it, and a signature does not say when it was fetched."
	notWitnessHistorical     = "That the witness held no statement for this trail after index %d: no head fetched from the witness was supplied, so the chain was read only as far as it was supplied."
	notWitnessContinued      = "Anything about statements up to index %d, which this reading did not read: it continued from a continuation supplied as the reader's own earlier successful reading, which the runtime cannot tell from one someone else wrote, and is as complete as that reading was."
	notAgainstWitness        = "Anything against a witness that is not independent of the operator: one that colludes can sign what it is asked, at any time it states, and a second history for another audience; a key supplied is trusted because the verifier chose it."
	notWitnessSubmitter      = "Who submitted any checkpoint: a statement does not name its submitter, and the witness cannot tell the trail's operator from a holder of the operator's credential."
	notWitnessTime           = "When any record was made: the time a witness states is its own clock's, for when it held the checkpoint."
	notCountersignedNone     = "That any line is covered by a statement of a witness under a key supplied: no statement that was read is credited with one."
	notWitnessConflict       = "Which of two records is the trail's at the sequence of each of the %d conflict statement(s) read, the first at sequence %d: a submitter the witness allowed for the trail offered there another record than the one the witness held, and a conflict statement says neither which of the two is the trail's nor who that submitter was, beyond the witness's own registration."
	// notAttempts is in every report, whatever was supplied and whatever was
	// found, and last: a checkpoint, a key or a stamp covers the lines a
	// trail holds, and no line is written for a refusal, a rehearsal or an
	// evaluation that failed (ADR-0048).
	notAttempts = "Whether any evaluation was refused at the gate, rehearsed, or failed before a disposition: the trail records decisions, not attempts, so its silence is not evidence that none were (ADR-0048)."
)

func signedAfterStatement(through, count int64) string {
	if count == 1 {
		return fmt.Sprintf("Lines after %d are not authenticated as one uninterrupted prefix by any signature that was checked; 1 record after sequence %d carries a signature that was checked.", through, through)
	}
	return fmt.Sprintf("Lines after %d are not authenticated as one uninterrupted prefix by any signature that was checked; %d records after sequence %d carry a signature that was checked.", through, count, through)
}

func signedNoneStatement(count int64) string {
	if count == 1 {
		return "That any uninterrupted prefix from line 1 is authenticated by a signature that was checked; 1 record carries a signature that was checked."
	}
	return fmt.Sprintf("That any uninterrupted prefix from line 1 is authenticated by a signature that was checked; %d records carry a signature that was checked.", count)
}

func stampedAfterStatement(through, count int64) string {
	if count == 1 {
		return fmt.Sprintf("Lines after %d are not shown as one uninterrupted prefix to have existed by any time; 1 trusted stamp after sequence %d shows its record existed by its time.", through, through)
	}
	return fmt.Sprintf("Lines after %d are not shown as one uninterrupted prefix to have existed by any time; %d trusted stamps after sequence %d show their records existed by their times.", through, count, through)
}

func stampedNoneStatement(count int64) string {
	if count == 1 {
		return "That any uninterrupted prefix from line 1 is shown to have existed by any time; 1 trusted stamp shows its record existed by its time."
	}
	return fmt.Sprintf("That any uninterrupted prefix from line 1 is shown to have existed by any time; %d trusted stamps show their records existed by their times.", count)
}

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
// With held checkpoints the trail is also held to each of them: the line at
// a checkpoint's sequence must be a chained record of its trail whose exact
// bytes have its digest. A trail shorter than that sequence fails, as does one
// with another identity or another record there. The records the highest
// matching checkpoint covers, with no failed check up to it, are witnessed,
// and the chained records after it are unwitnessed.
//
// The error is for a trail that could not be read; every check the trail
// fails is a finding in the report.
func Verify(contents io.ReaderAt, size int64, options Options) (Report, error) {
	v := newVerifier(options)
	if options.Stamps != nil && options.Stamps.Verify.Roots != nil {
		stamps, err := newStampChecker(options.Stamps)
		if err != nil {
			return Report{}, errors.Join(ErrStampsRead, err)
		}
		v.stamps = stamps
		for _, sequence := range stamps.wanted() {
			if _, kept := v.heldLines[sequence]; !kept {
				v.heldLines[sequence] = nil
			}
		}
	}
	reader := bufio.NewReaderSize(io.NewSectionReader(contents, 0, size), readChunk)
	for {
		ended, err := v.readLine(reader)
		if err != nil {
			return Report{}, err
		}
		if ended {
			break
		}
	}
	if v.signatures != nil {
		if v.sidecarErr == nil {
			v.sidecarErr = v.signatures.finish(v)
		}
		if v.sidecarErr != nil {
			return Report{}, errors.Join(ErrSidecarRead, v.sidecarErr)
		}
	}
	report := Report{Chain: v.report(size), Listed: v.listed, More: v.more}
	if read := v.witnessRead; read != nil && read.clean && read.last != nil && read.latest != nil && report.Chain.FindingsTotal == 0 {
		report.Continuation = encodeContinuation(read.last, read.latest)
	}
	return report, nil
}

// ErrSidecarRead is a signature sidecar that could not be read to its end.
var ErrSidecarRead = errors.New("the signature sidecar could not be read")

// ErrStampsRead is a stamps file that could not be read to its end.
var ErrStampsRead = errors.New("the stamps file could not be read")

// Options says what Verify holds a trail to and what it lists.
type Options struct {
	// Held is the checkpoints a holder kept. Every one must match the trail.
	Held []result.AuditCheckpoint
	// RequireThrough, when above zero, fails the verification unless a held
	// checkpoint covers every record up to that sequence.
	RequireThrough int64
	// List asks for the checkpoints of the chained records after ListAfter,
	// in sequence order, at most ListLimit of them.
	List      bool
	ListAfter int64
	ListLimit int
	// Signatures, when it supplies at least one public key, checks the
	// signature sidecar in step with the trail (ADR-0047 §2b).
	Signatures *SignatureOptions
	// Stamps, when it supplies roots, checks the stamps file's tokens and
	// holds their checkpoints to the trail (ADR-0047 §2a, C2).
	Stamps *StampOptions
	// Witness, when it is given, reads a witness's statements and holds the
	// trail to the checkpoint of every one that verifies (gateway ADR-0013).
	Witness *WitnessOptions
}

// WitnessOptions is a witness's statements, prepared within the bounds of
// one reading by PrepareWitness, and the countersigned coverage the
// verification is told to require, when above zero.
type WitnessOptions struct {
	Input          *WitnessInput
	RequireThrough int64
}

// Report is what Verify found: the chain, and the checkpoints it listed, with
// whether more chained records followed the last one listed. Continuation is
// what a later reading of the witness's statements may resume from: present
// only when statements were read and the verification has no finding at all.
type Report struct {
	Chain        result.AuditChain
	Listed       []result.AuditCheckpoint
	More         bool
	Continuation []byte
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
	// at is a chained line's at, the operator's word, when it could be read.
	at     time.Time
	atRead bool
}

// The rules a chained line's previous is held to, named in a finding.
const (
	ruleLine          = "the digest of the line before it"
	rulePrefix        = "the digest of the whole trail before it"
	ruleDiscontinuity = "what a record in the damaged line's place would have followed"
)

type verifier struct {
	options Options
	whole   hash.Hash

	lines     int64
	previous  *seenLine // the line before the one being read
	lastTrail string    // the identity of the last chained line read

	// heldLines is the line at each held checkpoint's sequence, as read, and
	// chainedAt the chained records up to and including each such line.
	heldLines map[int64]*seenLine
	chainedAt map[int64]int64
	head      *seenLine

	listed []result.AuditCheckpoint
	more   bool

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
	// earliestFinding is the lowest line any check of the trail or of a held
	// checkpoint failed at, or 0: a checkpoint is matched through its
	// sequence, and a signature covers the lines up to its own, only when no
	// line up to it failed such a check. A finding about the sidecar does
	// not move it, since a sidecar line says nothing about whether the trail
	// is consistent.
	earliestFinding int64

	// signatures reads the sidecar in step with the trail, when a public key
	// was supplied, and sidecarErr is the first failure to read it.
	signatures *signatureChecker
	sidecarErr error
	// stamps holds the stamps a verification read, when roots were
	// supplied, and gathers the lags of the records they cover.
	stamps *stampChecker
	// witnessRead is what a reading of a witness's statements found, once
	// the trail's identity is known, and countersigned the credited
	// checkpoint statement the countersigned coverage reaches, or nil.
	witnessRead   *witnessReading
	countersigned *witnessStatement
}

func newVerifier(options Options) *verifier {
	v := &verifier{options: options, whole: sha256.New(), segmentStart: 1,
		heldLines: map[int64]*seenLine{}, chainedAt: map[int64]int64{}}
	for _, held := range options.Held {
		v.heldLines[held.Sequence] = nil
	}
	if options.Witness != nil && options.Witness.Input != nil {
		for _, sequence := range options.Witness.Input.checkpointSequences() {
			v.heldLines[sequence] = nil
		}
	}
	if options.Signatures != nil && len(options.Signatures.Keys) > 0 {
		v.signatures = newSignatureChecker(options.Signatures)
	}
	return v
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
	if _, wanted := v.heldLines[seen.number]; wanted {
		v.heldLines[seen.number] = seen
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
	var at string
	if decodeString(members["at"], &at) == nil {
		parsed, err := time.Parse(time.RFC3339Nano, at)
		seen.at, seen.atRead = parsed, err == nil
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
// it is counted, and then the sidecar lines that belong with it are read and
// checked. It is called once for each line, after the line that follows it is
// checked, or at the end of the trail.
func (v *verifier) commit() {
	settled := v.previous
	if settled == nil {
		return
	}
	v.settle(settled)
	if v.signatures != nil && v.sidecarErr == nil {
		v.sidecarErr = v.signatures.settle(v, settled)
	}
}

// settle reports a settled line's findings and counts it.
func (v *verifier) settle(settled *seenLine) {
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
	if v.stamps != nil {
		v.stamps.settle(settled.number, settled.at, settled.atRead)
	}
	if v.firstChained {
		v.coverage.Unchained += v.pendingUnchained
	} else {
		v.coverage.LegacyPrefix += v.pendingUnchained
		v.firstChained = true
	}
	v.pendingUnchained = 0
	v.head = settled
	if _, wanted := v.heldLines[settled.number]; wanted {
		v.chainedAt[settled.number] = v.coverage.Chained
	}
	if v.options.List && settled.number > v.options.ListAfter {
		if len(v.listed) < v.options.ListLimit {
			v.listed = append(v.listed, checkpointOf(settled))
		} else {
			v.more = true
		}
	}
}

// checkpointOf is the checkpoint of a chained line.
func checkpointOf(line *seenLine) result.AuditCheckpoint {
	return result.AuditCheckpoint{
		CheckpointVersion: result.CheckpointVersion,
		RecordDigest:      line.digest,
		Sequence:          line.number,
		Trail:             line.link.trail,
	}
}

func (v *verifier) record(finding result.AuditFinding) {
	if v.earliestFinding == 0 || finding.Line < v.earliestFinding {
		v.earliestFinding = finding.Line
	}
	v.recordSidecar(finding)
}

// recordSidecar reports a finding about the signature sidecar, which does not
// move earliestFinding.
func (v *verifier) recordSidecar(finding result.AuditFinding) {
	v.findingsTotal++
	if len(v.findings) < maxFindings {
		v.findings = append(v.findings, finding)
	}
}

// report composes what the reading found.
func (v *verifier) report(size int64) result.AuditChain {
	chain := result.AuditChain{
		Scope:    ScopeNoChainedRecord,
		Lines:    v.lines,
		Bytes:    size,
		Coverage: v.coverage,
		Findings: v.findings,
	}
	chain.Coverage.Uncovered = v.pendingUnchained
	chain.Coverage.Signed = result.AuditCoverageState{Status: "not-checked", Detail: "no public key was supplied"}
	chain.Coverage.Checkpointed = result.AuditCoverageState{Status: "not-supplied"}
	chain.Coverage.Stamped = result.AuditCoverageState{Status: "not-checked", Detail: "no time-stamping roots were supplied"}
	chain.Coverage.Countersigned = result.AuditCoverageState{Status: "not-checked", Detail: "no witness key was supplied"}
	chain.Coverage.Unwitnessed = chain.Coverage.Chained
	if v.head != nil {
		chain.Scope = ScopeCommittedPrefix
		chain.Trail = v.head.link.trail
		head := checkpointOf(v.head)
		chain.Head = &head
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
	// A trusted stamp of a checkpoint the trail no longer holds is a finding
	// of the trail, recorded before any coverage is decided.
	if v.stamps != nil {
		v.stamps.match(v)
	}
	// A witness's statements are read against the identity the trail's
	// chained records carry. Their findings are about the statements, not
	// the trail's lines, so they move no line's coverage: they void the
	// witness's credit alone.
	if v.options.Witness != nil && v.options.Witness.Input != nil {
		v.witnessRead = v.options.Witness.Input.read(chain.Trail)
		for _, finding := range v.witnessRead.findings {
			v.recordSidecar(finding)
		}
	}
	if len(v.options.Held) > 0 || v.witnessRead != nil {
		v.checkHeld(&chain)
	}
	if v.stamps != nil {
		// The limit is taken before any requirement is checked: an unmet
		// requirement is not a break in the chain.
		v.stamps.limit = v.earliestFinding
	}
	if v.options.RequireThrough > 0 {
		v.checkRequirement(&chain)
	}
	if v.witnessRead != nil {
		v.witnessSection(&chain)
	}
	if v.signatures != nil {
		v.signatures.coverage(v, &chain)
	}
	if v.stamps != nil {
		v.stamps.coverage(v, &chain)
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
	var signedAfter, stampedAfter int64
	if v.signatures != nil {
		signedAfter = v.signatures.afterThrough()
	}
	if v.stamps != nil {
		stampedAfter = v.stamps.afterThrough(chain.Coverage.Stamped.Through)
	}
	chain.Establishes, chain.DoesNotEstablish = statements(chain, signedAfter, stampedAfter)
	return chain
}

// checkHeld holds the trail to every checkpoint a holder kept, and to the
// checkpoint of every checkpoint statement of a witness that verified: the
// line at each one's sequence must be there, chained, of its trail, and have
// its digest, and each that does not is a finding, whether or not the
// witness's statements are credited. The coverage then reaches the highest
// checkpoint that matched with no failed check at or before its sequence,
// among the holder's and, when the witness's statements had no finding, the
// witness's: those records are witnessed, and the chained records after it
// are not. The countersigned coverage is the same, among the witness's alone.
// The checkpoints are checked in sequence order, so the findings come out in
// one order whatever order the holder supplied them in.
func (v *verifier) checkHeld(chain *result.AuditChain) {
	held := append([]result.AuditCheckpoint{}, v.options.Held...)
	sort.SliceStable(held, func(i, j int) bool { return held[i].Sequence < held[j].Sequence })
	chain.Scope = ScopeCheckpoint
	summary := &result.AuditHeld{Supplied: int64(len(held)), Status: "failed"}
	matched := []result.AuditCheckpoint{}
	for _, expect := range held {
		if name, detail := v.holdCheckpoint(expect); name != "" {
			summary.Failed++
			v.record(result.AuditFinding{Name: name, Line: expect.Sequence, Detail: detail})
			continue
		}
		summary.Matched++
		matched = append(matched, expect)
	}
	credited := []*witnessStatement{}
	if read := v.witnessRead; read != nil {
		statements := append([]*witnessStatement{}, read.verified...)
		sort.SliceStable(statements, func(i, j int) bool {
			return statements[i].checkpoint.Sequence < statements[j].checkpoint.Sequence
		})
		for _, statement := range statements {
			if name, detail := v.holdCheckpoint(statement.checkpoint); name != "" {
				v.record(result.AuditFinding{Name: name, Line: statement.checkpoint.Sequence, Detail: fmt.Sprintf("the checkpoint of the witness statement at index %d: %s", statement.index, detail)})
				continue
			}
			if read.clean {
				credited = append(credited, statement)
			}
		}
	}
	// Every finding is recorded before the coverage is decided: a checkpoint
	// that failed voids the coverage of every checkpoint at or after it.
	covers := func(sequence int64) bool { return v.earliestFinding == 0 || v.earliestFinding > sequence }
	var through *result.AuditCheckpoint
	for index := range matched {
		if covers(matched[index].Sequence) {
			through = &matched[index]
		}
	}
	if len(held) > 0 {
		if through != nil {
			latest := *through
			summary.Latest = &latest
			if summary.Failed == 0 && through.Sequence == held[len(held)-1].Sequence {
				summary.Status = "matched"
			}
		}
		chain.Held = summary
	}
	for _, statement := range credited {
		if covers(statement.checkpoint.Sequence) {
			v.countersigned = statement
		}
	}
	reach := int64(0)
	if through != nil {
		reach = through.Sequence
	}
	if v.countersigned != nil && v.countersigned.checkpoint.Sequence > reach {
		reach = v.countersigned.checkpoint.Sequence
	}
	chain.Coverage.Checkpointed = result.AuditCoverageState{Status: "failed"}
	if reach > 0 {
		chain.Coverage.Checkpointed = result.AuditCoverageState{Status: "through", Through: reach}
		chain.Coverage.Witnessed = v.chainedAt[reach]
		chain.Coverage.Unwitnessed = chain.Coverage.Chained - chain.Coverage.Witnessed
	}
}

// holdCheckpoint holds the trail to one checkpoint: the line at its sequence
// must be there, a chained record of its trail with its digest. It answers
// the finding's name and detail, or "" when the checkpoint matches.
func (v *verifier) holdCheckpoint(expect result.AuditCheckpoint) (string, string) {
	named := v.heldLines[expect.Sequence]
	switch {
	case expect.Sequence > v.lines || named == nil:
		return FindingCheckpointBeyondTrail, fmt.Sprintf("the trail has %d complete lines, fewer than the checkpoint's sequence", v.lines)
	case named.damaged || !named.chained:
		return FindingCheckpointNotChained, "the line at the checkpoint's sequence is not a chained record"
	case named.link.trail != expect.Trail:
		return FindingCheckpointTrailMismatch, "the record at the checkpoint's sequence is of another trail"
	case named.digest != expect.RecordDigest:
		return FindingCheckpointRecordMismatch, "the record at the checkpoint's sequence is not the one the checkpoint names"
	}
	return "", ""
}

// witnessSection reports a witness's statements as read: the countersigned
// coverage checkHeld found, the section, and the countersigned coverage the
// verification was told to require.
func (v *verifier) witnessSection(chain *result.AuditChain) {
	read, input := v.witnessRead, v.options.Witness.Input
	section := &result.AuditWitness{
		KeysSupplied:      int64(input.keyCount),
		StatementsRead:    int64(read.read),
		StatementsChecked: int64(read.checked),
		Began:             "index-0",
		Status:            "failed",
		Conflicts:         []int64{},
	}
	if input.resumed {
		section.Began = "continued"
		if input.last != nil {
			after := input.last.index
			section.ContinuedAfter = &after
		}
	}
	chain.Coverage.Countersigned = result.AuditCoverageState{Status: "failed"}
	if read.clean {
		section.Status = "read"
		section.Reading = "historical"
		if read.head != nil {
			section.Reading = "current"
			index := read.head.index
			section.HeadIndex = &index
		}
		if read.last != nil {
			highest := read.last.index
			section.HighestIndex = &highest
		}
		if read.latest != nil {
			section.LatestCheckpoint = &result.AuditWitnessCheckpoint{Index: read.latest.index, Sequence: read.latest.checkpoint.Sequence, WitnessedAt: read.latest.witnessedAt}
		}
		section.Conflicts = append(section.Conflicts, read.conflicts[:min(len(read.conflicts), maxListed)]...)
		section.ConflictsTotal = int64(len(read.conflicts))
		section.Retired = read.retired
		chain.Coverage.Countersigned = result.AuditCoverageState{Status: "none"}
		if v.countersigned != nil {
			chain.Coverage.Countersigned = result.AuditCoverageState{Status: "through", Through: v.countersigned.checkpoint.Sequence}
			section.CountersignedAt = v.countersigned.witnessedAt
		}
	}
	chain.Witness = section
	if required := v.options.Witness.RequireThrough; required > 0 {
		chain.RequiredCountersigned = &result.AuditRequirement{Through: required, Status: "met"}
		if chain.Coverage.Countersigned.Status != "through" || chain.Coverage.Countersigned.Through < required {
			chain.RequiredCountersigned.Status = "unmet"
			v.recordSidecar(result.AuditFinding{
				Name:   FindingCountersignedCoverageMissing,
				Line:   required,
				Detail: fmt.Sprintf("no credited witness statement covers the records up to sequence %d: they are not countersigned", required),
			})
		}
	}
}

// checkRequirement fails a verification whose held checkpoints do not cover
// every record up to the sequence it was told to require.
func (v *verifier) checkRequirement(chain *result.AuditChain) {
	required := v.options.RequireThrough
	chain.Required = &result.AuditRequirement{Through: required, Status: "met"}
	if chain.Coverage.Checkpointed.Status == "through" && chain.Coverage.Checkpointed.Through >= required {
		return
	}
	chain.Required.Status = "unmet"
	v.record(result.AuditFinding{
		Name:   FindingCheckpointCoverageMissing,
		Line:   required,
		Detail: fmt.Sprintf("no held checkpoint covers the records up to sequence %d: they are unwitnessed", required),
	})
}

// statements says what a report's result establishes and what it does not, in
// the fixed sentences above. Nothing is established about a trail that failed a
// check beyond the findings themselves.
func statements(chain result.AuditChain, signedAfter, stampedAfter int64) ([]string, []string) {
	establishes := []string{}
	if chain.Status != "invalid" && chain.Coverage.Chained > 0 {
		if chain.Coverage.Uncovered > 0 {
			establishes = append(establishes, fmt.Sprintf(establishesConsistencyBeforeTail, chain.Head.Sequence, chain.Coverage.Uncovered))
		} else {
			establishes = append(establishes, fmt.Sprintf(establishesConsistency, chain.Head.Sequence))
		}
	}
	notEstablished := []string{}
	if chain.Coverage.Chained == 0 {
		if chain.Lines == 0 {
			notEstablished = append(notEstablished, notChainedEmpty)
		} else {
			notEstablished = append(notEstablished, fmt.Sprintf(notChained, chain.Coverage.Uncovered))
		}
	}
	if chain.Coverage.Checkpointed.Status == "through" {
		through := chain.Coverage.Checkpointed.Through
		establishes = append(establishes, fmt.Sprintf(establishesCheckpoint, through))
		notEstablished = append(notEstablished, fmt.Sprintf(notLastLineAfter, through), fmt.Sprintf(notCompleteAfter, through))
	} else {
		if chain.Head != nil {
			notEstablished = append(notEstablished, fmt.Sprintf(notLastLine, chain.Head.Sequence))
		}
		notEstablished = append(notEstablished, notComplete)
	}
	if chain.DiscontinuitiesTotal > 0 {
		notEstablished = append(notEstablished, notSegmented)
	}
	if chain.Held != nil {
		notEstablished = append(notEstablished, notHeldAll)
	}
	notEstablished = append(notEstablished, notTime)
	switch chain.Coverage.Signed.Status {
	case "through":
		through := chain.Coverage.Signed.Through
		establishes = append(establishes, fmt.Sprintf(establishesSigned, through))
		if signedAfter > 0 {
			notEstablished = append(notEstablished, signedAfterStatement(through, signedAfter))
		} else {
			notEstablished = append(notEstablished, fmt.Sprintf(notSignedAfter, through))
		}
		notEstablished = append(notEstablished, notAgainstOperator, notAfterTheft)
	case "none":
		if signedAfter > 0 {
			notEstablished = append(notEstablished, signedNoneStatement(signedAfter))
		} else {
			notEstablished = append(notEstablished, notSignedNone)
		}
		notEstablished = append(notEstablished, notAgainstOperator, notAfterTheft)
	default:
		notEstablished = append(notEstablished, notSignedUnchecked)
	}
	switch chain.Coverage.Stamped.Status {
	case "through":
		through := chain.Coverage.Stamped.Through
		establishes = append(establishes, fmt.Sprintf(establishesStamped, through, chain.Stamps.CoveredBy))
		if stampedAfter > 0 {
			notEstablished = append(notEstablished, stampedAfterStatement(through, stampedAfter))
		} else {
			notEstablished = append(notEstablished, fmt.Sprintf(notStampedAfter, through))
		}
	case "none":
		if stampedAfter > 0 {
			notEstablished = append(notEstablished, stampedNoneStatement(stampedAfter))
		} else {
			notEstablished = append(notEstablished, notStampedNone)
		}
	default:
		notEstablished = append(notEstablished, notStampedUnchecked)
	}
	if chain.Stamps != nil {
		notEstablished = append(notEstablished, notBeforeStamp, notAgainstAuthority)
		if chain.Stamps.RevocationNotChecked > 0 {
			notEstablished = append(notEstablished, fmt.Sprintf(notRevocationChecked, chain.Stamps.RevocationNotChecked))
		}
	}
	if witness := chain.Witness; witness != nil {
		if chain.Coverage.Countersigned.Status == "through" {
			through := chain.Coverage.Countersigned.Through
			establishes = append(establishes, fmt.Sprintf(establishesCountersigned, through, witness.CountersignedAt))
			notEstablished = append(notEstablished, fmt.Sprintf(notCountersignedAfter, through))
		} else {
			notEstablished = append(notEstablished, notCountersignedNone)
		}
		if witness.Status == "read" {
			switch {
			case witness.Reading == "current" && witness.HeadIndex != nil:
				notEstablished = append(notEstablished, fmt.Sprintf(notWitnessCurrent, *witness.HeadIndex))
			case witness.HighestIndex != nil:
				notEstablished = append(notEstablished, fmt.Sprintf(notWitnessHistorical, *witness.HighestIndex))
			}
			if witness.ContinuedAfter != nil {
				notEstablished = append(notEstablished, fmt.Sprintf(notWitnessContinued, *witness.ContinuedAfter))
			}
			if witness.ConflictsTotal > 0 && len(witness.Conflicts) > 0 {
				notEstablished = append(notEstablished, fmt.Sprintf(notWitnessConflict, witness.ConflictsTotal, witness.Conflicts[0]))
			}
		}
		notEstablished = append(notEstablished, notAgainstWitness, notWitnessSubmitter, notWitnessTime)
	}
	notEstablished = append(notEstablished, notAttempts)
	return establishes, notEstablished
}
