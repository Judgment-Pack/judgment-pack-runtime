package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// MaxCheckpointBytes bounds a checkpoint document: its four members take under
// two hundred bytes.
const MaxCheckpointBytes = 4096

// ErrCheckpointVersion is a checkpoint of a checkpointVersion this runtime
// does not read.
var ErrCheckpointVersion = errors.New("the checkpoint's checkpointVersion is not one this runtime reads")

// ParseCheckpoint holds a checkpoint document to its shape: one JSON object of
// exactly checkpointVersion ("1"), trail (32 lowercase hex characters),
// sequence (an integer from 1 to 2^53-2) and recordDigest ("sha256:" and 64
// lowercase hex characters), each named once and nothing else. JSON's own
// whitespace around the object is allowed, so a document saved with a trailing
// newline reads. A checkpointVersion other than "1" is ErrCheckpointVersion.
func ParseCheckpoint(document []byte) (result.AuditCheckpoint, error) {
	if len(document) > MaxCheckpointBytes {
		return result.AuditCheckpoint{}, fmt.Errorf("the checkpoint exceeds %d bytes", MaxCheckpointBytes)
	}
	trimmed := bytes.Trim(document, " \t\r\n")
	if !json.Valid(trimmed) {
		return result.AuditCheckpoint{}, errors.New("the checkpoint is not one JSON text")
	}
	members, err := exactObject(trimmed)
	if err != nil {
		return result.AuditCheckpoint{}, fmt.Errorf("the checkpoint is not a JSON object of distinct members: %v", err)
	}
	for name := range members {
		switch name {
		case "checkpointVersion", "trail", "sequence", "recordDigest":
		default:
			return result.AuditCheckpoint{}, fmt.Errorf("the checkpoint has a member %q it does not define", name)
		}
	}
	var checkpoint result.AuditCheckpoint
	if decodeString(members["checkpointVersion"], &checkpoint.CheckpointVersion) != nil {
		return result.AuditCheckpoint{}, errors.New("the checkpoint's checkpointVersion must be a string")
	}
	if checkpoint.CheckpointVersion != result.CheckpointVersion {
		return result.AuditCheckpoint{}, ErrCheckpointVersion
	}
	if decodeString(members["trail"], &checkpoint.Trail) != nil || !trailForm.MatchString(checkpoint.Trail) {
		return result.AuditCheckpoint{}, errors.New("the checkpoint's trail must be 32 lowercase hexadecimal characters")
	}
	if decodeInteger(members["sequence"], &checkpoint.Sequence) != nil || checkpoint.Sequence < 1 || checkpoint.Sequence >= maxSafeInteger {
		return result.AuditCheckpoint{}, errors.New("the checkpoint's sequence must be an integer from 1 to 9007199254740990")
	}
	if decodeString(members["recordDigest"], &checkpoint.RecordDigest) != nil || !previousForm.MatchString(checkpoint.RecordDigest) {
		return result.AuditCheckpoint{}, errors.New("the checkpoint's recordDigest must be sha256: and 64 lowercase hexadecimal characters")
	}
	return checkpoint, nil
}

// EncodeCheckpoint writes a checkpoint as one compact JSON line, its members in
// code-point order: the RFC 8785 canonical form, followed by a newline.
func EncodeCheckpoint(checkpoint result.AuditCheckpoint) []byte {
	// The members are strings of hexadecimal and fixed text and an integer,
	// so encoding cannot fail and no escape is needed.
	encoded, _ := json.Marshal(checkpoint)
	return append(encoded, '\n')
}
