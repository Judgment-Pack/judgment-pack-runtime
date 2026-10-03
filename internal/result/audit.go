package result

// CheckpointVersion is the shape of an audit checkpoint (ADR-0047 §1, §2a), a
// single integer as a string on the outputVersion precedent.
const CheckpointVersion = "1"

// AuditCheckpoint names one record of a chained audit trail by what a holder
// needs to check it later: the trail's identity, the record's sequence, and the
// SHA-256 of the record's exact line bytes, without its newline. That digest is
// the one the next record's previous holds and the one a gateway action
// receipt's decision.recordDigest names for the same record, and because a
// chained line carries its own trail and sequence, it commits to both as well
// as to everything the chain links before it.
//
// The members are declared in code-point order and hold only lowercase-hex
// strings and an integer under 2^53, so the compact encoding of this value is
// its own RFC 8785 canonical form: a checkpoint's exact bytes are what a later
// time stamp (#208) can be taken over.
type AuditCheckpoint struct {
	CheckpointVersion string `json:"checkpointVersion"`
	RecordDigest      string `json:"recordDigest"`
	Sequence          int64  `json:"sequence"`
	Trail             string `json:"trail"`
}

// AuditCoverage says how much of a trail each kind of protection reaches.
// The line counts partition the trail's complete lines.
type AuditCoverage struct {
	// LegacyPrefix is the lines before the first chained line, which its
	// previous commits to as one block.
	LegacyPrefix int64 `json:"legacyPrefix"`
	// Chained is the chained lines.
	Chained int64 `json:"chained"`
	// Unchained is the unchained lines after the first chained line that a
	// later chained line commits to, as part of the whole file before it.
	Unchained int64 `json:"unchained"`
	// Uncovered is the lines no chained line commits to: the unchained lines
	// after the last chained line, or every line when none is chained.
	Uncovered int64 `json:"uncovered"`
	// Damaged is the lines a discontinuity names as damaged.
	Damaged int64 `json:"damaged"`
	// Signed is "not-checked" when no public key was supplied, so no
	// signature was checked (ADR-0047 §2b); otherwise "through" the highest
	// sequence whose own record carries a valid signature with no failed
	// check of the chain at or before it, which then covers every line up to
	// it, or "none" when no valid signature covers any.
	Signed AuditCoverageState `json:"signed"`
	// SignedRecords is the chained records with a valid signature of their
	// own, and UnsignedRecords the chained records without one; together
	// they are Chained. Both are zero when no signature was checked.
	SignedRecords   int64 `json:"signedRecords"`
	UnsignedRecords int64 `json:"unsignedRecords"`
	// Checkpointed is "not-supplied" without a held checkpoint, "through" the
	// sequence the held checkpoints cover, and "failed" when they cover none.
	Checkpointed AuditCoverageState `json:"checkpointed"`
	// Witnessed is the chained records a held checkpoint covers, and
	// Unwitnessed the chained records none does: every one after the
	// sequence Checkpointed is through, or every one when it is through none.
	Witnessed   int64 `json:"witnessed"`
	Unwitnessed int64 `json:"unwitnessed"`
	// Stamped is "not-available": time stamps from an RFC 3161 authority are
	// not built yet (runtime #208, part 2), nor the lag between a record's at
	// and the first stamp covering it.
	Stamped AuditCoverageState `json:"stamped"`
}

// AuditCoverageState is one protection's reach.
type AuditCoverageState struct {
	Status  string `json:"status"`
	Through int64  `json:"through,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// AuditSegment is a run of lines between discontinuities, by line number.
type AuditSegment struct {
	FirstLine int64 `json:"firstLine"`
	LastLine  int64 `json:"lastLine"`
}

// AuditDiscontinuity is one discontinuity record a repair appended: the line
// it is on, why, and the damaged line it names, kept in place, with that
// line's length and digest as the record states them.
type AuditDiscontinuity struct {
	Line        int64  `json:"line"`
	Reason      string `json:"reason"`
	DamagedLine int64  `json:"damagedLine"`
	Bytes       int64  `json:"bytes"`
	Digest      string `json:"digest"`
}

// AuditFinding is one check a trail failed: a stable name, the line it is
// about, and what was found, never a record's contents.
type AuditFinding struct {
	Name   string `json:"name"`
	Line   int64  `json:"line"`
	Detail string `json:"detail"`
}

// AuditHeld is what the trail was held to: the checkpoints a holder kept and
// supplied. Every one must match. Latest is the highest that matched with no
// failed check up to it, which is how far the coverage reaches; Status is
// "matched" when every one matched and the coverage reaches the highest
// supplied, and "failed" otherwise.
type AuditHeld struct {
	Supplied int64            `json:"supplied"`
	Matched  int64            `json:"matched"`
	Failed   int64            `json:"failed"`
	Latest   *AuditCheckpoint `json:"latest,omitempty"`
	Status   string           `json:"status"`
}

// AuditRequirement is a coverage the verification was told to require: every
// record up to Through covered by a held checkpoint. Status is "met" or
// "unmet".
type AuditRequirement struct {
	Through int64  `json:"through"`
	Status  string `json:"status"`
}

// AuditSignatures is the signature sidecar as a verification read it
// (ADR-0047 §2b): its lines, those of no shape it reads (a write that did not
// complete among them), the rotations followed, how many public keys and
// revocations were supplied, and the first key and the key in force at the
// end, by keyId.
type AuditSignatures struct {
	Lines        int64  `json:"lines"`
	Unreadable   int64  `json:"unreadable"`
	Rotations    int64  `json:"rotations"`
	KeysSupplied int64  `json:"keysSupplied"`
	Revocations  int64  `json:"revocations"`
	FirstKey     string `json:"firstKey"`
	KeyInForce   string `json:"keyInForce"`
}

// AuditChain is what reading a trail found: its status, the scope of what was
// checked, its coverage and segments, and every finding, with the statements of
// what the result establishes and what it does not. Status is "valid" when
// every check passed and the trail has no discontinuity, "segmented" when every
// check passed and it has at least one, and "invalid" when any check failed.
type AuditChain struct {
	Status          string               `json:"status"`
	Scope           string               `json:"scope"`
	Lines           int64                `json:"lines"`
	Bytes           int64                `json:"bytes"`
	Trail           string               `json:"trail,omitempty"`
	Head            *AuditCheckpoint     `json:"head,omitempty"`
	Coverage        AuditCoverage        `json:"coverage"`
	Segments        []AuditSegment       `json:"segments"`
	SegmentsTotal   int64                `json:"segmentsTotal"`
	Discontinuities []AuditDiscontinuity `json:"discontinuities"`
	// DiscontinuitiesTotal counts every discontinuity; Discontinuities and
	// Segments list the first hundred of each, as Findings does.
	DiscontinuitiesTotal int64             `json:"discontinuitiesTotal"`
	Held                 *AuditHeld        `json:"held,omitempty"`
	Required             *AuditRequirement `json:"required,omitempty"`
	// Signatures is the sidecar as read, present when a public key was
	// supplied, and RequiredSigned a signed coverage the verification was
	// told to require: every record up to Through covered by a valid
	// signature.
	Signatures       *AuditSignatures  `json:"signatures,omitempty"`
	RequiredSigned   *AuditRequirement `json:"requiredSigned,omitempty"`
	Findings         []AuditFinding    `json:"findings"`
	FindingsTotal    int               `json:"findingsTotal"`
	Establishes      []string          `json:"establishes"`
	DoesNotEstablish []string          `json:"doesNotEstablish"`
}

// AuditVerification is jpack audit verify's payload: the chain as read, which
// trail was read, and whether the snapshot was taken between writes.
type AuditVerification struct {
	OutputVersion         string `json:"outputVersion"`
	Tool                  Tool   `json:"tool"`
	Command               string `json:"command"`
	TrailPath             string `json:"trailPath"`
	SnapshotBetweenWrites bool   `json:"snapshotBetweenWrites"`
	AuditChain
}

// AuditCheckpointReport is jpack audit checkpoint's payload: the checkpoint of
// the trail's last chained line, and how many lines after it it does not
// cover.
type AuditCheckpointReport struct {
	OutputVersion  string          `json:"outputVersion"`
	Tool           Tool            `json:"tool"`
	Command        string          `json:"command"`
	Status         string          `json:"status"`
	TrailPath      string          `json:"trailPath"`
	Checkpoint     AuditCheckpoint `json:"checkpoint"`
	UncoveredLines int64           `json:"uncoveredLines"`
}

// AuditCheckpointList is jpack audit checkpoint --since's payload: the
// checkpoints of the chained records after After, in sequence order, as many
// as were asked for, and whether more follow. A deliverer that hands each to a
// holder asks again after the last sequence it received.
type AuditCheckpointList struct {
	OutputVersion string            `json:"outputVersion"`
	Tool          Tool              `json:"tool"`
	Command       string            `json:"command"`
	Status        string            `json:"status"`
	TrailPath     string            `json:"trailPath"`
	After         int64             `json:"after"`
	Checkpoints   []AuditCheckpoint `json:"checkpoints"`
	More          bool              `json:"more"`
}

// AuditRepair is jpack audit repair's payload: the discontinuity record it
// appended.
type AuditRepair struct {
	OutputVersion string             `json:"outputVersion"`
	Tool          Tool               `json:"tool"`
	Command       string             `json:"command"`
	Status        string             `json:"status"`
	TrailPath     string             `json:"trailPath"`
	Discontinuity AuditDiscontinuity `json:"discontinuity"`
}

// AuditKey is jpack audit key generate's and jpack audit key public's
// payload: a signing key's public key and keyId (ADR-0047 §2b), never anything
// of its private half.
type AuditKey struct {
	OutputVersion string `json:"outputVersion"`
	Tool          Tool   `json:"tool"`
	Command       string `json:"command"`
	Status        string `json:"status"`
	PublicKey     string `json:"publicKey"`
	KeyID         string `json:"keyId"`
}

// AuditRotation is jpack audit key rotate's payload: the key-rotation line it
// appended to the signature sidecar, by the trail line it follows, the trail,
// and the two keys by keyId, with the next key's public key.
type AuditRotation struct {
	OutputVersion string `json:"outputVersion"`
	Tool          Tool   `json:"tool"`
	Command       string `json:"command"`
	Status        string `json:"status"`
	TrailPath     string `json:"trailPath"`
	At            int64  `json:"at"`
	Trail         string `json:"trail"`
	From          string `json:"from"`
	Next          string `json:"next"`
	NextPublicKey string `json:"nextPublicKey"`
}
