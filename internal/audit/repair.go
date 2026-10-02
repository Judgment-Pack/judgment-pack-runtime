package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The refusals of audit repair.
var (
	// ErrNothingToRepair is a trail whose last line is complete: there is no
	// damage at its end for a repair to start a segment after. A broken link
	// elsewhere is not repaired: audit verify reports it, and nothing written
	// after it would make it less broken.
	ErrNothingToRepair = errors.New("the audit trail's last line is complete, so there is nothing to repair")
	// ErrRepairUnchained is a project that does not chain its trail: a repair
	// starts a chained segment, which such a project did not ask for.
	ErrRepairUnchained = errors.New("the project does not chain its audit trail")
	// ErrRepairNeedsLock is a trail no lock can be taken on: a repair reads
	// the trail's end and appends after it, which is safe only while no
	// cooperating writer can append in between.
	ErrRepairNeedsLock = errors.New("no lock can be taken on the audit trail, so it cannot be repaired safely")
	// ErrNoTrail is a project whose trail has not been written yet.
	ErrNoTrail = errors.New("the audit trail does not exist")
)

// discontinuityRecord is the line audit repair appends. Its chain members come
// first and in the order every record's do; it has no evaluatorSpecVersion,
// pack, inputs or disposition, because it records no evaluation.
type discontinuityRecord struct {
	RecordVersion string        `json:"recordVersion"`
	Trail         string        `json:"trail"`
	Sequence      int64         `json:"sequence"`
	Previous      string        `json:"previous"`
	Run           string        `json:"run"`
	At            string        `json:"at"`
	Kind          string        `json:"kind"`
	Surface       string        `json:"surface"`
	Tool          Tool          `json:"tool"`
	Discontinuity discontinuity `json:"discontinuity"`
}

// Repair starts a new segment after a damaged end of the trail (ADR-0047 §1):
// a last line with no newline, which a chaining writer refuses to append after.
//
// Nothing is removed or rewritten. Under the trail's lock, the damaged bytes,
// everything after the last newline, are ended with a newline, so they become
// a line of their own, kept in place, and a discontinuity record follows them.
// Both are appended in one write. The record names the damaged line, its
// length and the digest of its bytes as they were, and its previous links over
// it to what a record in its place would have followed: the last line before
// it when that line is chained, the whole trail before it when it is not. Its
// trail is that record's identity, and its sequence is its line number, so the
// next record a writer chains follows it as it follows any chained line.
//
// It refuses (ErrNothingToRepair) when the last line is complete, which is how
// it refuses when nothing is damaged, and when the damage is longer than any
// line a chained trail holds (ErrOversizedLine). Where no lock can be taken it
// refuses (ErrRepairNeedsLock), and it refuses for a writer that does not chain
// (ErrRepairUnchained).
func (w *Writer) Repair() (result.AuditDiscontinuity, error) {
	if w == nil {
		return result.AuditDiscontinuity{}, ErrNoTrail
	}
	if !w.chain {
		return result.AuditDiscontinuity{}, ErrRepairUnchained
	}
	name := path.Join(w.dir, FileName)
	// The trail must already be there: the locked append below would create
	// it, and a repair that creates the trail it repairs has nothing to repair.
	existing, err := w.root.Open(name)
	if err != nil {
		return result.AuditDiscontinuity{}, errors.Join(ErrNoTrail, err)
	}
	existing.Close()
	var repaired result.AuditDiscontinuity
	err = w.root.AppendLocked(name, func(state fssecure.AppendState) ([]byte, error) {
		appended, discontinuity, err := w.repairLines(state)
		repaired = discontinuity
		return appended, err
	})
	if err != nil {
		return result.AuditDiscontinuity{}, err
	}
	return repaired, nil
}

// repairLines is what a repair appends, decided under the trail's lock: the
// newline that ends the damaged line, then the discontinuity record. Without
// the lock it appends nothing.
func (w *Writer) repairLines(state fssecure.AppendState) ([]byte, result.AuditDiscontinuity, error) {
	if !state.Locked {
		return nil, result.AuditDiscontinuity{}, ErrRepairNeedsLock
	}
	line, discontinuity, err := w.discontinuityAfter(state.Contents, state.Size)
	if err != nil {
		return nil, result.AuditDiscontinuity{}, err
	}
	return append([]byte{'\n'}, line...), discontinuity, nil
}

// discontinuityAfter composes the discontinuity record for a trail of size
// bytes whose last line is incomplete, and the report of it.
func (w *Writer) discontinuityAfter(contents io.ReaderAt, size int64) ([]byte, result.AuditDiscontinuity, error) {
	if size == 0 {
		return nil, result.AuditDiscontinuity{}, ErrNothingToRepair
	}
	var final [1]byte
	if err := readAt(contents, final[:], size-1); err != nil {
		return nil, result.AuditDiscontinuity{}, err
	}
	if final[0] == '\n' {
		return nil, result.AuditDiscontinuity{}, ErrNothingToRepair
	}
	start, err := damageStart(contents, size)
	if err != nil {
		return nil, result.AuditDiscontinuity{}, err
	}
	// What a record in the damaged line's place would have followed: the
	// writer's own reading of the trail before it.
	before, err := readHead(io.NewSectionReader(contents, 0, start), start)
	if err != nil {
		return nil, result.AuditDiscontinuity{}, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.NewSectionReader(contents, start, size-start)); err != nil {
		return nil, result.AuditDiscontinuity{}, err
	}
	damaged := discontinuity{
		Reason: ReasonIncompleteLastLine,
		Line:   before.sequence + 1,
		Bytes:  size - start,
		Digest: "sha256:" + hex.EncodeToString(hash.Sum(nil)),
	}
	current := result.CurrentTool()
	record := discontinuityRecord{
		RecordVersion: RecordVersion,
		Trail:         before.trail,
		Sequence:      damaged.Line + 1,
		Previous:      before.previous,
		Run:           w.run,
		At:            time.Now().UTC().Format(time.RFC3339Nano),
		Kind:          KindDiscontinuity,
		Surface:       "audit repair",
		Tool:          Tool{Name: current.Name, Version: current.Version, Digest: toolDigest()},
		Discontinuity: damaged,
	}
	line, err := encodeJSONLine(record)
	if err != nil {
		return nil, result.AuditDiscontinuity{}, err
	}
	if int64(len(line)-1) > maxLineBytes {
		return nil, result.AuditDiscontinuity{}, ErrRecordTooLarge
	}
	return line, result.AuditDiscontinuity{
		Line:        record.Sequence,
		Reason:      damaged.Reason,
		DamagedLine: damaged.Line,
		Bytes:       damaged.Bytes,
		Digest:      damaged.Digest,
	}, nil
}

// encodeJSONLine encodes one value as a trail line, as encodeLines encodes a
// record: compact, with HTML escaping off, ending its line.
func encodeJSONLine(value any) ([]byte, error) {
	var line bytes.Buffer
	encoder := json.NewEncoder(&line)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return line.Bytes(), nil
}

// damageStart finds where the incomplete last line begins: after the last
// newline, or at the start of the trail. Damage longer than any line a chained
// trail holds is refused, as the writer refuses such a line.
func damageStart(contents io.ReaderAt, size int64) (int64, error) {
	chunk := make([]byte, readChunk)
	for cursor := size; cursor > 0; {
		if size-cursor > maxLineBytes {
			return 0, ErrOversizedLine
		}
		n := min(int64(len(chunk)), cursor)
		if err := readAt(contents, chunk[:n], cursor-n); err != nil {
			return 0, err
		}
		for index := n - 1; index >= 0; index-- {
			if chunk[index] == '\n' {
				start := cursor - n + index + 1
				if size-start > maxLineBytes {
					return 0, ErrOversizedLine
				}
				return start, nil
			}
		}
		cursor -= n
	}
	if size > maxLineBytes {
		return 0, ErrOversizedLine
	}
	return 0, nil
}
