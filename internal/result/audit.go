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
	// Signed is "not-available": detached signatures are not built yet.
	Signed AuditCoverageState `json:"signed"`
	// Checkpointed is "not-supplied" without a checkpoint, "through" when the
	// one supplied matched, and "failed" when it did not.
	Checkpointed AuditCoverageState `json:"checkpointed"`
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

// AuditExpectation is the checkpoint a verification was asked to hold the
// trail to, and whether it held.
type AuditExpectation struct {
	Checkpoint AuditCheckpoint `json:"checkpoint"`
	Status     string          `json:"status"`
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
	Expect               *AuditExpectation `json:"expect,omitempty"`
	Findings             []AuditFinding    `json:"findings"`
	FindingsTotal        int               `json:"findingsTotal"`
	Establishes          []string          `json:"establishes"`
	DoesNotEstablish     []string          `json:"doesNotEstablish"`
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
