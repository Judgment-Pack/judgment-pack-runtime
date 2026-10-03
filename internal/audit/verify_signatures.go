package audit

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The names of the checks a signature sidecar can fail (ADR-0047 §2b). They
// are stable: a reader may branch on them.
const (
	// FindingSignatureInvalid is a record signature that does not verify
	// under the key in force at its sequence, or names another keyId.
	FindingSignatureInvalid = "signature-invalid"
	// FindingSignatureMissing is a signed coverage the verification was told
	// to require and the sidecar does not give: no valid signature covers
	// the records up to the required sequence.
	FindingSignatureMissing = "signature-missing"
	// FindingSignatureRecordMismatch is a record signature for another
	// record: its record digest is not the digest of the line at its
	// sequence, so the record there is not the one signed.
	FindingSignatureRecordMismatch = "signature-record-mismatch"
	// FindingSignatureNoRecord is a sidecar line naming no record of the
	// trail: a sequence the trail does not reach or where it holds no chained
	// record of the signature's trail, or a rotation after a line the trail
	// does not have.
	FindingSignatureNoRecord = "signature-no-record"
	// FindingSignatureKeyRevoked is a signature or rotation made with a key
	// the verifier was told is revoked from a sequence at or before the one
	// it names.
	FindingSignatureKeyRevoked = "signature-key-revoked"
	// FindingRotationInvalid is a key rotation not signed by the key in
	// force, of another trail than the one it follows, or naming another key
	// than the next one the verifier was given.
	FindingRotationInvalid = "rotation-invalid"
	// FindingSidecarOutOfOrder is a sidecar line behind one already read: a
	// signature for a sequence at or before the last one signed, or a line
	// placed before a rotation it follows.
	FindingSidecarOutOfOrder = "sidecar-out-of-order"
)

// SignatureOptions says what a verification holds the signature sidecar to.
type SignatureOptions struct {
	// Keys are the public keys the verifier trusts, in the order the trail
	// was signed with them: the first is the key in force at the trail's
	// start. When more than one is given, each rotation must name the next
	// one, so a rotation to any other key is refused; with one, a validly
	// signed rotation to any key is followed.
	Keys []ed25519.PublicKey
	// Revoked is the keys the verifier was told not to trust from a sequence
	// on: a signature for that sequence or after it, and a rotation after
	// that line or later, made with such a key, is refused.
	Revoked []Revocation
	// Sidecar is the sidecar's contents, of SidecarSize bytes; nil reads as
	// empty.
	Sidecar     io.ReaderAt
	SidecarSize int64
	// RequireThrough, when above zero, fails the verification unless a valid
	// signature covers every record up to that sequence.
	RequireThrough int64
}

// Revocation is one key the verifier does not trust from a sequence on.
type Revocation struct {
	PublicKey ed25519.PublicKey
	From      int64
}

// MaxRevocationBytes bounds a document of revocations.
const MaxRevocationBytes = 1 << 20

// ParseRevocations reads a document of revocations, one per line: a JSON
// object of exactly from (an integer from 1 to 2^53-2) and publicKey (64
// lowercase hexadecimal characters that CheckPublicKey accepts). Blank lines
// are passed over; the first line of another shape refuses the document,
// naming its line.
func ParseRevocations(document []byte) ([]Revocation, error) {
	if len(document) > MaxRevocationBytes {
		return nil, fmt.Errorf("the revocations exceed %d bytes", MaxRevocationBytes)
	}
	revoked := []Revocation{}
	for number, line := range bytes.Split(document, []byte("\n")) {
		trimmed := bytes.Trim(line, " \t\r")
		if len(trimmed) == 0 {
			continue
		}
		members, err := exactObject(trimmed)
		if err != nil || !json.Valid(trimmed) || len(members) != 2 {
			return nil, fmt.Errorf("line %d: a revocation is a JSON object of exactly from and publicKey", number+1)
		}
		var revocation Revocation
		var public string
		if decodeInteger(members["from"], &revocation.From) != nil || revocation.From < 1 || revocation.From >= maxSafeInteger {
			return nil, fmt.Errorf("line %d: a revocation's from must be an integer from 1 to 9007199254740990", number+1)
		}
		if decodeString(members["publicKey"], &public) != nil || !publicKeyForm.MatchString(public) {
			return nil, fmt.Errorf("line %d: a revocation's publicKey must be 64 lowercase hexadecimal characters", number+1)
		}
		if revocation.PublicKey, err = ParsePublicKey([]byte(public)); err != nil {
			return nil, fmt.Errorf("line %d: a revocation's publicKey is not one: %v", number+1, err)
		}
		revoked = append(revoked, revocation)
	}
	return revoked, nil
}

// signatureChecker reads the sidecar in step with the trail: a line at a time,
// each when the trail line it names is settled, so a verification holds no
// more of either file than one line of each. A record signature's place is
// its sequence, and a rotation's is just after its at, so a sidecar in order
// has every line's place at or after the one before.
type signatureChecker struct {
	options *SignatureOptions
	reader  *bufio.Reader

	ahead     *sidecarItem
	aheadLine int64
	ended     bool

	current  ed25519.PublicKey
	keyIndex int
	// lastPlace is twice the sequence of the last record signature read, or
	// twice the at of the last rotation plus one.
	lastPlace int64
	// lastTrail is the identity of the last chained line settled.
	lastTrail string

	lines      int64
	unreadable int64
	rotations  int64
	signed     int64
	through    int64
}

func newSignatureChecker(options *SignatureOptions) *signatureChecker {
	c := &signatureChecker{options: options}
	if len(options.Keys) > 0 {
		c.current = options.Keys[0]
	}
	if options.Sidecar != nil && options.SidecarSize > 0 {
		c.reader = bufio.NewReaderSize(io.NewSectionReader(options.Sidecar, 0, options.SidecarSize), readChunk)
	}
	return c
}

// place is where a readable sidecar line belongs among the trail's lines.
func (item sidecarItem) place() int64 {
	if item.kind == KindKeyRotation {
		return 2*item.at + 1
	}
	return 2 * item.sequence
}

// names is the trail line a sidecar line is about, for its findings.
func (item sidecarItem) names() int64 {
	if item.kind == KindKeyRotation {
		return item.at
	}
	return item.sequence
}

// peek reads the next sidecar line, once, and keeps it until it is taken.
func (c *signatureChecker) peek() (*sidecarItem, int64, error) {
	if c.ahead != nil || c.ended {
		return c.ahead, c.aheadLine, nil
	}
	if c.reader == nil {
		c.ended = true
		return nil, 0, nil
	}
	line := []byte{}
	overlong := false
	terminated := false
	for {
		piece, err := c.reader.ReadSlice('\n')
		if len(piece) > 0 && piece[len(piece)-1] == '\n' {
			piece, terminated = piece[:len(piece)-1], true
		}
		if !overlong {
			if len(line)+len(piece) > maxSidecarLineBytes {
				overlong, line = true, nil
			} else {
				line = append(line, piece...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, err
		}
		break
	}
	if !terminated && len(line) == 0 && !overlong {
		c.ended = true
		return nil, 0, nil
	}
	c.lines++
	item := sidecarItem{}
	// A line with no newline is a write that did not complete, unreadable
	// whatever it holds, as the writer reads it.
	if terminated && !overlong {
		item = parseSidecarLine(line)
	}
	if !terminated {
		c.ended = true
	}
	c.ahead, c.aheadLine = &item, c.lines
	return c.ahead, c.aheadLine, nil
}

// take consumes the line peek kept.
func (c *signatureChecker) take() { c.ahead = nil }

// settle reads the sidecar lines that belong at or before the trail line just
// settled, and checks each against it: every line up to that line's record
// signature and the rotations after it.
func (c *signatureChecker) settle(v *verifier, settled *seenLine) error {
	if settled.chained && !settled.damaged {
		c.lastTrail = settled.link.trail
	}
	limit := 2*settled.number + 1
	for {
		item, number, err := c.peek()
		if err != nil {
			return err
		}
		if item == nil || (item.readable && item.place() > limit) {
			return nil
		}
		c.take()
		c.check(v, *item, number, settled)
	}
}

// finish reads the sidecar lines left once the trail has ended: each names a
// line the trail does not have.
func (c *signatureChecker) finish(v *verifier) error {
	for {
		item, number, err := c.peek()
		if err != nil {
			return err
		}
		if item == nil {
			return nil
		}
		c.take()
		c.check(v, *item, number, nil)
	}
}

// check holds one sidecar line to its rule. settled is the trail line it
// belongs to, or nil past the trail's end.
func (c *signatureChecker) check(v *verifier, item sidecarItem, number int64, settled *seenLine) {
	if !item.readable {
		c.unreadable++
		return
	}
	finding := result.AuditFinding{Line: item.names()}
	place := item.place()
	if place < c.lastPlace || (item.kind == KindRecordSignature && place == c.lastPlace) {
		v.recordSidecar(result.AuditFinding{
			Name:   FindingSidecarOutOfOrder,
			Line:   item.names(),
			Detail: fmt.Sprintf("sidecar line %d is behind a line before it: a signature for a sequence already passed, or a line placed before a rotation it follows", number),
		})
		return
	}
	c.lastPlace = place
	switch {
	case settled == nil && item.kind == KindRecordSignature:
		finding.Name = FindingSignatureNoRecord
		finding.Detail = fmt.Sprintf("sidecar line %d signs sequence %d, and the trail has %d complete lines", number, item.sequence, v.lines)
	case settled == nil:
		finding.Name = FindingSignatureNoRecord
		finding.Detail = fmt.Sprintf("sidecar line %d rotates the key after line %d, and the trail has %d complete lines", number, item.at, v.lines)
	case item.kind == KindRecordSignature:
		finding.Name, finding.Detail = c.checkRecord(v, item, number, settled)
	default:
		finding.Name, finding.Detail = c.checkRotation(item, number)
	}
	if finding.Name != "" {
		v.recordSidecar(finding)
	}
}

// checkRecord holds a record signature to the line at its sequence and to the
// key in force there, and counts the record signed when it holds.
func (c *signatureChecker) checkRecord(v *verifier, item sidecarItem, number int64, settled *seenLine) (string, string) {
	switch {
	case settled.damaged || !settled.chained || settled.link.trail != item.trail:
		return FindingSignatureNoRecord, fmt.Sprintf("sidecar line %d signs a record of trail %s at sequence %d, and the trail holds no such chained record there", number, item.trail, item.sequence)
	case settled.digest != item.record:
		return FindingSignatureRecordMismatch, fmt.Sprintf("sidecar line %d signs another record at sequence %d than the one there", number, item.sequence)
	case item.keyID != KeyID(c.current) || !ed25519.Verify(c.current, RecordMessage(item.trail, item.sequence, item.record), item.signature):
		return FindingSignatureInvalid, fmt.Sprintf("sidecar line %d does not verify under key %s, the key in force at sequence %d", number, KeyID(c.current), item.sequence)
	case c.revoked(item.sequence):
		return FindingSignatureKeyRevoked, fmt.Sprintf("sidecar line %d is signed with key %s, which is revoked at sequence %d", number, KeyID(c.current), item.sequence)
	}
	c.signed++
	if v.earliestFinding == 0 || v.earliestFinding > item.sequence {
		c.through = item.sequence
	}
	return "", ""
}

// checkRotation holds a rotation to the key in force and to the keys the
// verifier was given, and hands the trail over to the next key when it holds.
// A rotation to a key CheckPublicKey refuses hands nothing over, however it is
// signed: a key of small order would let anyone sign every record after it.
func (c *signatureChecker) checkRotation(item sidecarItem, number int64) (string, string) {
	pinned := len(c.options.Keys) > 1
	refused := CheckPublicKey(item.next)
	switch {
	case c.lastTrail == "" || item.trail != c.lastTrail:
		return FindingRotationInvalid, fmt.Sprintf("sidecar line %d rotates the key of another trail than the one at line %d", number, item.at)
	case item.keyID != KeyID(c.current) || !ed25519.Verify(c.current, RotationMessage(item.trail, item.at, hexKey(item.next)), item.signature):
		return FindingRotationInvalid, fmt.Sprintf("sidecar line %d is a rotation not signed by key %s, the key in force after line %d", number, KeyID(c.current), item.at)
	case c.revoked(item.at):
		return FindingSignatureKeyRevoked, fmt.Sprintf("sidecar line %d is a rotation made with key %s, which is revoked at line %d", number, KeyID(c.current), item.at)
	case refused != nil:
		return FindingRotationInvalid, fmt.Sprintf("sidecar line %d rotates to key %s, which is not one a verifier accepts: %v", number, KeyID(item.next), refused)
	case pinned && (c.keyIndex+1 >= len(c.options.Keys) || !bytes.Equal(item.next, c.options.Keys[c.keyIndex+1])):
		return FindingRotationInvalid, fmt.Sprintf("sidecar line %d rotates to key %s, which is not the next public key supplied", number, KeyID(item.next))
	}
	c.current = item.next
	c.keyIndex++
	c.rotations++
	return "", ""
}

// revoked says whether the key in force is revoked at sequence.
func (c *signatureChecker) revoked(sequence int64) bool {
	for _, revocation := range c.options.Revoked {
		if bytes.Equal(revocation.PublicKey, c.current) && sequence >= revocation.From {
			return true
		}
	}
	return false
}

// coverage reports how far the signatures reach, and the sidecar as read.
func (c *signatureChecker) coverage(v *verifier, chain *result.AuditChain) {
	chain.Coverage.SignedRecords = c.signed
	chain.Coverage.UnsignedRecords = chain.Coverage.Chained - c.signed
	chain.Coverage.Signed = result.AuditCoverageState{Status: "none"}
	if c.through > 0 {
		chain.Coverage.Signed = result.AuditCoverageState{Status: "through", Through: c.through}
	}
	chain.Signatures = &result.AuditSignatures{
		Lines:        c.lines,
		Unreadable:   c.unreadable,
		Rotations:    c.rotations,
		KeysSupplied: int64(len(c.options.Keys)),
		Revocations:  int64(len(c.options.Revoked)),
		FirstKey:     KeyID(c.options.Keys[0]),
		KeyInForce:   KeyID(c.current),
	}
	if required := c.options.RequireThrough; required > 0 {
		chain.RequiredSigned = &result.AuditRequirement{Through: required, Status: "met"}
		if c.through < required {
			chain.RequiredSigned.Status = "unmet"
			v.recordSidecar(result.AuditFinding{
				Name:   FindingSignatureMissing,
				Line:   required,
				Detail: fmt.Sprintf("no valid signature covers the records up to sequence %d: they are unsigned", required),
			})
		}
	}
}

func hexKey(public ed25519.PublicKey) string { return hex.EncodeToString(public) }
