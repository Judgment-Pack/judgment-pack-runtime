package audit

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/timestamp"
)

// Time stamps (ADR-0047 §2a, C2). A checkpoint is stamped by an RFC 3161
// time-stamping authority over its digest, CheckpointDigest: the SHA-256 of
// the checkpoint's canonical form (EncodeCheckpoint, without its newline),
// which commits to its trail, its sequence and its record's exact bytes, and
// through the chain to every line before it. The token is kept beside the
// trail, in StampsName, one line per stamp, in its RFC 8785 canonical form:
//
//	{"checkpoint":{…},"stampVersion":"1","token":"<base64 of the token's DER>"}
//
// Stamping is its own act (jpack audit stamp), never part of a decision: the
// decision is appended first, and stamped after, when something asks.
const (
	// StampsName is the stamps file, beside the trail.
	StampsName = "stamps.jsonl"
	// StampVersion is the shape of a stamps line.
	StampVersion = "1"
	// MaxStampLineBytes bounds a stamps line, written or read whole. A token
	// carrying a certificate chain is a few kilobytes; the bound holds the
	// largest reply timestamp.Ask reads, MaxReplyBytes, in base64 with its
	// checkpoint, so every token Ask returns can be kept and read back.
	MaxStampLineBytes = 2 << 20
	// MaxStampsBytes bounds the stamps file a verification reads.
	MaxStampsBytes = 64 << 20
)

// The names of the checks a stamp can fail. They are stable: a reader may
// branch on them.
const (
	// FindingStampMalformed is a stamps line whose token is not a time-stamp
	// token of the shape RFC 3161 gives it.
	FindingStampMalformed = "stamp-malformed"
	// FindingStampSignatureInvalid is a token whose signature, or signed
	// attributes, do not hold.
	FindingStampSignatureInvalid = "stamp-signature-invalid"
	// FindingStampImprintMismatch is a token over another digest than the
	// checkpoint it is kept with.
	FindingStampImprintMismatch = "stamp-imprint-mismatch"
	// FindingStampUntrusted is a token whose certificate does not chain to a
	// supplied root at the time it states, expired or not yet valid then
	// among them.
	FindingStampUntrusted = "stamp-untrusted"
	// FindingStampUsageInvalid is a token whose certificate is not for
	// time-stamping alone, by a critical extended key usage.
	FindingStampUsageInvalid = "stamp-usage-invalid"
	// FindingStampPolicyMismatch is a token under a policy other than the
	// ones supplied.
	FindingStampPolicyMismatch = "stamp-policy-mismatch"
	// FindingStampRevoked is a token whose certificate a supplied revocation
	// list shows revoked as of its time.
	FindingStampRevoked = "stamp-revoked"
	// FindingStampCheckpointMismatch is a trusted stamp of a checkpoint the
	// trail does not hold: a sequence it does not reach, a line that is not a
	// chained record of that trail, or another record there. The authority
	// attests that checkpoint existed, so the trail was rewritten since.
	FindingStampCheckpointMismatch = "stamp-checkpoint-mismatch"
	// FindingStampCoverageMissing is a stamped coverage the verification was
	// told to require and the stamps do not give.
	FindingStampCoverageMissing = "stamp-coverage-missing"
)

// CheckpointDigest is what a stamp stamps: the SHA-256 of the checkpoint's
// canonical form, without the newline that ends its line. It is also the key
// a retry is idempotent by.
func CheckpointDigest(checkpoint result.AuditCheckpoint) []byte {
	sum := sha256.Sum256(bytes.TrimSuffix(EncodeCheckpoint(checkpoint), []byte("\n")))
	return sum[:]
}

// stampLine is a stamps line, its members in code-point order.
type stampLine struct {
	Checkpoint   result.AuditCheckpoint `json:"checkpoint"`
	StampVersion string                 `json:"stampVersion"`
	Token        string                 `json:"token"`
}

// encodeStampLine is a stamps line, newline included.
func encodeStampLine(checkpoint result.AuditCheckpoint, token []byte) []byte {
	encoded, _ := json.Marshal(stampLine{Checkpoint: checkpoint, StampVersion: StampVersion, Token: base64.StdEncoding.EncodeToString(token)})
	return append(encoded, '\n')
}

// parsedStamp is a stamps line as read: readable when it is of its shape, with
// its checkpoint and its token's bytes.
type parsedStamp struct {
	readable   bool
	checkpoint result.AuditCheckpoint
	token      []byte
}

// parseStampLine reads a stamps line, without its newline: one JSON object of
// exactly checkpoint (a checkpoint, by ParseCheckpoint's rule), stampVersion
// ("1") and token (standard base64). Anything else is unreadable.
func parseStampLine(line []byte) parsedStamp {
	if len(line) == 0 || len(line) > MaxStampLineBytes || !json.Valid(line) {
		return parsedStamp{}
	}
	members, err := exactObject(line)
	if err != nil || len(members) != 3 {
		return parsedStamp{}
	}
	var version, encoded string
	if decodeString(members["stampVersion"], &version) != nil || version != StampVersion || decodeString(members["token"], &encoded) != nil {
		return parsedStamp{}
	}
	checkpoint, err := ParseCheckpoint(members["checkpoint"])
	if err != nil {
		return parsedStamp{}
	}
	token, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(token) == 0 {
		return parsedStamp{}
	}
	return parsedStamp{readable: true, checkpoint: checkpoint, token: token}
}

// stampsPath is the stamps file's name beneath the project's root.
func (w *Writer) stampsPath() string { return path.Join(w.dir, StampsName) }

// tornStampLine ends a stamps line a failed write left without its newline,
// so it stays a line of its own that can never be read as a stamp, as a sidecar
// line is ended (tornLineEnd).
const tornStampLine = tornLineEnd

// holdsStamp says whether stamps already hold a stamp of checkpoint: a
// readable line for it whose token parses, signature and all, over the
// checkpoint's digest. Nothing about trust is known here; a verifier decides
// that with its roots.
func holdsStamp(contents io.Reader, checkpoint result.AuditCheckpoint) (bool, error) {
	want := CheckpointDigest(checkpoint)
	reader := bufio.NewReaderSize(contents, readChunk)
	for {
		line, terminated, err := readBoundedLine(reader, MaxStampLineBytes)
		if err != nil {
			return false, err
		}
		if line == nil {
			return false, nil
		}
		if !terminated {
			continue
		}
		parsed := parseStampLine(line)
		if !parsed.readable || parsed.checkpoint != checkpoint {
			continue
		}
		if token, err := timestamp.Parse(parsed.token); err == nil && token.HashAlgorithm.Equal(timestamp.OIDSHA256) && bytes.Equal(token.HashedMessage, want) {
			return true, nil
		}
	}
}

// readBoundedLine reads one line, without its newline, and whether a newline
// ended it. A line longer than limit is read through and answered empty, so
// it is unreadable. At the end of the input it answers nil.
func readBoundedLine(reader *bufio.Reader, limit int) ([]byte, bool, error) {
	line := []byte{}
	overlong := false
	read := false
	for {
		piece, err := reader.ReadSlice('\n')
		read = read || len(piece) > 0
		terminated := len(piece) > 0 && piece[len(piece)-1] == '\n'
		if terminated {
			piece = piece[:len(piece)-1]
		}
		if !overlong {
			if len(line)+len(piece) > limit {
				overlong, line = true, []byte{}
			} else {
				line = append(line, piece...)
			}
		}
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if !read {
				return nil, false, nil
			}
			return line, false, nil
		case err != nil:
			return nil, false, err
		}
		return line, true, nil
	}
}

// Stamped says whether the stamps file already holds a stamp of checkpoint, so
// a retry asks the authority nothing.
func (w *Writer) Stamped(checkpoint result.AuditCheckpoint) (bool, error) {
	if w == nil {
		return false, ErrNoTrail
	}
	file, err := w.root.Open(w.stampsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	return holdsStamp(io.LimitReader(file, MaxStampsBytes), checkpoint)
}

// ErrStampTooLarge is a token whose stamps line would be longer than
// MaxStampLineBytes, which a reader passes over as unreadable; it is refused
// before the stamps file is opened.
var ErrStampTooLarge = errors.New("the time-stamp token is too large to keep as one stamps line")

// RecordStamp keeps a token for checkpoint in the stamps file, under the
// stamps file's own lock, never the trail's: a decision never waits on a
// stamp. It is idempotent by the checkpoint's digest: when the file already
// holds a stamp of that checkpoint, which a concurrent stamp may have written
// meanwhile, nothing is appended, and the answer is false.
func (w *Writer) RecordStamp(checkpoint result.AuditCheckpoint, token []byte) (bool, error) {
	if w == nil {
		return false, ErrNoTrail
	}
	if len(encodeStampLine(checkpoint, token)) > MaxStampLineBytes {
		return false, ErrStampTooLarge
	}
	appended := false
	err := w.root.AppendLocked(w.stampsPath(), func(state fssecure.AppendState) ([]byte, error) {
		held, err := holdsStamp(io.NewSectionReader(state.Contents, 0, min(state.Size, MaxStampsBytes)), checkpoint)
		if err != nil {
			return nil, err
		}
		if held {
			return nil, nil
		}
		line := encodeStampLine(checkpoint, token)
		if state.Size > 0 {
			var final [1]byte
			if err := readAt(state.Contents, final[:], state.Size-1); err != nil {
				return nil, err
			}
			if final[0] != '\n' {
				line = append([]byte(tornStampLine), line...)
			}
		}
		appended = true
		return line, nil
	})
	if err != nil {
		return false, err
	}
	return appended, nil
}

// StampOptions says what a verification holds the stamps to: the trust a
// token is verified under, the stamps file's contents, and the coverage to
// require.
type StampOptions struct {
	Verify timestamp.VerifyOptions
	// Stamps is the stamps file's contents, of StampsSize bytes; nil reads as
	// empty.
	Stamps     io.ReaderAt
	StampsSize int64
	// RequireThrough, when above zero, fails the verification unless a
	// trusted stamp covers every record up to that sequence.
	RequireThrough int64
}

// candidateStamp is a stamp whose token holds under the verifier's trust; it
// counts once its checkpoint matches the trail.
type candidateStamp struct {
	checkpoint result.AuditCheckpoint
	existedBy  time.Time
	checked    bool
	matches    bool
}

// lagInterval gathers the records between two stamped sequences: every one
// of them is first covered by the same stamp, so the earliest and latest at
// among them give the interval's longest and shortest lag.
type lagInterval struct {
	count                  int64
	earliest, latest       time.Time
	earliestSeq, latestSeq int64
	// unreadable counts the interval's records whose at could not be read.
	unreadable int64
}

// stampChecker holds the stamps a verification read, and the lags it gathers
// as the trail is read.
type stampChecker struct {
	options    *StampOptions
	lines      int64
	unreadable int64
	candidates []candidateStamp
	// sequences is the candidates' sequences, sorted and distinct; the
	// records up to each one and after the one before share a lagInterval.
	sequences []int64
	intervals []lagInterval
	findings  []result.AuditFinding
	// limit is the earliest line any check of the trail failed at, or 0, as
	// it stood once the trail and the held checkpoints were checked: a stamp
	// lends its time only to records the chain links to it, so only a stamp
	// before the limit counts, and only for the records up to it.
	limit int64
}

// newStampChecker reads every stamps line and checks each token, before the
// trail is read: a token's checks need nothing of the trail.
func newStampChecker(options *StampOptions) (*stampChecker, error) {
	c := &stampChecker{options: options}
	if options.Stamps == nil || options.StampsSize <= 0 {
		return c, nil
	}
	reader := bufio.NewReaderSize(io.NewSectionReader(options.Stamps, 0, min(options.StampsSize, MaxStampsBytes)), readChunk)
	for number := int64(1); ; number++ {
		line, terminated, err := readBoundedLine(reader, MaxStampLineBytes)
		if err != nil {
			return nil, err
		}
		if line == nil {
			break
		}
		c.lines++
		parsed := parseStampLine(line)
		if !terminated || !parsed.readable {
			c.unreadable++
			continue
		}
		c.check(parsed, number)
	}
	seen := map[int64]bool{}
	for _, candidate := range c.candidates {
		if !seen[candidate.checkpoint.Sequence] {
			seen[candidate.checkpoint.Sequence] = true
			c.sequences = append(c.sequences, candidate.checkpoint.Sequence)
		}
	}
	sort.Slice(c.sequences, func(i, j int) bool { return c.sequences[i] < c.sequences[j] })
	c.intervals = make([]lagInterval, len(c.sequences))
	return c, nil
}

// check holds one stamp's token to the verifier's trust, and keeps it as a
// candidate when it holds.
func (c *stampChecker) check(parsed parsedStamp, number int64) {
	finding := result.AuditFinding{Line: parsed.checkpoint.Sequence}
	token, err := timestamp.Parse(parsed.token)
	switch {
	case errors.Is(err, timestamp.ErrSignature):
		finding.Name, finding.Detail = FindingStampSignatureInvalid, fmt.Sprintf("stamps line %d: %v", number, err)
	case err != nil:
		finding.Name, finding.Detail = FindingStampMalformed, fmt.Sprintf("stamps line %d: %v", number, err)
	case !token.HashAlgorithm.Equal(timestamp.OIDSHA256) || !bytes.Equal(token.HashedMessage, CheckpointDigest(parsed.checkpoint)):
		finding.Name, finding.Detail = FindingStampImprintMismatch, fmt.Sprintf("stamps line %d: the token stamps another digest than the checkpoint's", number)
	}
	if finding.Name != "" {
		c.findings = append(c.findings, finding)
		return
	}
	revocation, err := token.Verify(c.options.Verify)
	switch {
	case errors.Is(err, timestamp.ErrUsage):
		finding.Name = FindingStampUsageInvalid
	case errors.Is(err, timestamp.ErrPolicy):
		finding.Name = FindingStampPolicyMismatch
	case errors.Is(err, timestamp.ErrRevoked):
		finding.Name = FindingStampRevoked
	case err != nil:
		finding.Name = FindingStampUntrusted
	}
	if finding.Name != "" {
		finding.Detail = fmt.Sprintf("stamps line %d: %v", number, err)
		c.findings = append(c.findings, finding)
		return
	}
	c.candidates = append(c.candidates, candidateStamp{
		checkpoint: parsed.checkpoint,
		existedBy:  token.ExistedBy(),
		checked:    revocation == timestamp.RevocationChecked,
	})
}

// wanted is the sequences whose lines the trail reading must keep, to match
// the candidates' checkpoints against them.
func (c *stampChecker) wanted() []int64 { return c.sequences }

// settle gathers one settled chained record's at into the interval of the
// first stamped sequence at or after it.
func (c *stampChecker) settle(sequence int64, at time.Time, atRead bool) {
	index := sort.Search(len(c.sequences), func(i int) bool { return c.sequences[i] >= sequence })
	if index == len(c.sequences) {
		return
	}
	interval := &c.intervals[index]
	if !atRead {
		interval.unreadable++
		return
	}
	if interval.count == 0 || at.Before(interval.earliest) {
		interval.earliest, interval.earliestSeq = at, sequence
	}
	if interval.count == 0 || at.After(interval.latest) {
		interval.latest, interval.latestSeq = at, sequence
	}
	interval.count++
}

// match holds every candidate's checkpoint to the trail as read, recording a
// mismatch as a finding of the trail: a trusted authority attests that
// checkpoint existed.
func (c *stampChecker) match(v *verifier) {
	for index := range c.candidates {
		candidate := &c.candidates[index]
		expect := candidate.checkpoint
		named := v.heldLines[expect.Sequence]
		detail := ""
		switch {
		case expect.Sequence > v.lines || named == nil:
			detail = fmt.Sprintf("a trusted stamp names sequence %d, and the trail has %d complete lines", expect.Sequence, v.lines)
		case named.damaged || !named.chained:
			detail = "a trusted stamp names a line that is not a chained record"
		case named.link.trail != expect.Trail:
			detail = "a trusted stamp names a record of another trail"
		case named.digest != expect.RecordDigest:
			detail = "a trusted stamp names another record than the one at its sequence"
		}
		if detail != "" {
			v.record(result.AuditFinding{Name: FindingStampCheckpointMismatch, Line: expect.Sequence, Detail: detail})
			continue
		}
		candidate.matches = true
	}
}

// lends says whether a stamp lends its time to the records up to its
// checkpoint: it is trusted, its checkpoint matches the trail, and no check of
// the trail failed at or before its sequence, so the chain links every one of
// those records to the checkpoint it stamps.
func (c *stampChecker) lends(candidate candidateStamp) bool {
	return candidate.matches && (c.limit == 0 || candidate.checkpoint.Sequence < c.limit)
}

// coverage reports how far the trusted stamps reach, their lags, and the
// requirement.
func (c *stampChecker) coverage(v *verifier, chain *result.AuditChain) {
	for _, finding := range c.findings {
		v.recordSidecar(finding)
	}
	summary := &result.AuditStamps{Lines: c.lines, Unreadable: c.unreadable}
	// cover[k] is the earliest time a trusted, matching stamp at or after
	// sequences[k] attests: when the records of interval k were first
	// covered.
	cover := make([]time.Time, len(c.sequences))
	for index := len(c.sequences) - 1; index >= 0; index-- {
		if index+1 < len(c.sequences) {
			cover[index] = cover[index+1]
		}
		for _, candidate := range c.candidates {
			if c.lends(candidate) && candidate.checkpoint.Sequence == c.sequences[index] && (cover[index].IsZero() || candidate.existedBy.Before(cover[index])) {
				cover[index] = candidate.existedBy
			}
		}
	}
	var through int64
	for _, candidate := range c.candidates {
		if !candidate.matches {
			continue
		}
		summary.Trusted++
		if candidate.checked {
			summary.RevocationChecked++
		} else {
			summary.RevocationNotChecked++
		}
		if sequence := candidate.checkpoint.Sequence; sequence > through && c.lends(candidate) {
			through = sequence
		}
	}
	lag := &result.AuditStampLag{}
	var longest, shortest time.Duration
	for index, interval := range c.intervals {
		if cover[index].IsZero() {
			continue
		}
		lag.AtUnreadable += interval.unreadable
		if interval.count == 0 {
			continue
		}
		most, least := cover[index].Sub(interval.earliest), cover[index].Sub(interval.latest)
		if lag.Records == 0 || most > longest {
			longest, lag.MaxSequence = most, interval.earliestSeq
		}
		if lag.Records == 0 || least < shortest {
			shortest, lag.MinSequence = least, interval.latestSeq
		}
		lag.Records += interval.count
	}
	lag.MaxSeconds, lag.MinSeconds = longest.Seconds(), shortest.Seconds()
	// A record whose at is after the time the authority attests it existed
	// by: the operator's clock ran ahead of the authority's, or at is false.
	lag.AtAfterStamp = lag.Records > 0 && shortest < 0
	summary.Lag = lag
	chain.Stamps = summary
	chain.Coverage.Stamped = result.AuditCoverageState{Status: "none"}
	if through > 0 {
		chain.Coverage.Stamped = result.AuditCoverageState{Status: "through", Through: through}
		index := sort.Search(len(c.sequences), func(i int) bool { return c.sequences[i] >= through })
		summary.CoveredBy = cover[index].UTC().Format(time.RFC3339Nano)
	}
	if required := c.options.RequireThrough; required > 0 {
		chain.RequiredStamped = &result.AuditRequirement{Through: required, Status: "met"}
		if through < required {
			chain.RequiredStamped.Status = "unmet"
			v.recordSidecar(result.AuditFinding{
				Name:   FindingStampCoverageMissing,
				Line:   required,
				Detail: fmt.Sprintf("no trusted stamp covers the records up to sequence %d: they are not shown to have existed by any time", required),
			})
		}
	}
}
