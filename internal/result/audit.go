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
	// Stamped is "not-checked" when no time-stamping roots were supplied, so
	// no stamp was checked (ADR-0047 §2a, C2); otherwise "through" the
	// highest sequence a trusted stamp's checkpoint names, with no failed
	// check of the trail at or before it, or "none".
	Stamped AuditCoverageState `json:"stamped"`
	// Countersigned says how far a witness's signature reaches (gateway
	// ADR-0013): "not-checked" when no witness key was supplied; "failed"
	// when the witness's statements had a finding, so none is credited;
	// otherwise "through" the highest sequence a credited checkpoint
	// statement names whose checkpoint matched the trail with no failed check
	// at or before it, or "none". A credited statement's checkpoint also
	// counts toward Checkpointed and Witnessed, as a held one does.
	Countersigned AuditCoverageState `json:"countersigned"`
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
// about, and what was found, never a record's contents. A finding about a
// witness's statements is about no line of the trail, and its line is 0.
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

// AuditStamps is the stamps file as a verification read it: its lines, those
// of no shape it reads, the trusted stamps (their token holds under the roots
// supplied and their checkpoint matches the trail), how many of those had
// their certificates' revocation status checked against a supplied list and
// how many did not, the time the stamped coverage's records existed by, and
// the lag between records' at and the first trusted stamp covering them.
type AuditStamps struct {
	Lines                int64          `json:"lines"`
	Unreadable           int64          `json:"unreadable"`
	Trusted              int64          `json:"trusted"`
	RevocationChecked    int64          `json:"revocationChecked"`
	RevocationNotChecked int64          `json:"revocationNotChecked"`
	CoveredBy            string         `json:"coveredBy,omitempty"`
	Lag                  *AuditStampLag `json:"lag"`
}

// AuditStampLag is the lag between each covered record's at, the operator's
// word, and the time the first trusted stamp covering it attests the record
// existed by: over Records records, the longest (and its record's sequence)
// and the shortest (and its). AtAfterStamp says some record's at is later
// than that time, which the operator's word cannot be if the authority's is
// right. AtUnreadable counts covered records whose at could not be read.
type AuditStampLag struct {
	Records      int64   `json:"records"`
	MaxSeconds   float64 `json:"maxSeconds"`
	MaxSequence  int64   `json:"maxSequence,omitempty"`
	MinSeconds   float64 `json:"minSeconds"`
	MinSequence  int64   `json:"minSequence,omitempty"`
	AtAfterStamp bool    `json:"atAfterStamp"`
	AtUnreadable int64   `json:"atUnreadable"`
}

// AuditWitness is a witness's statements as a verification read them (gateway
// ADR-0013): how many keys were supplied, how many statement lines were read
// (the head's and a continuation's two included) and how many distinct
// statements were checked, each once; where the reading began, "index-0" or
// "continued" after ContinuedAfter, a continuation's last index; and Status,
// "read" when the statements had no finding and "failed" when they had one,
// in which case none is credited and nothing below is reported. A reading is
// "current" when a head fetched from the witness was supplied, as of that
// fetch, and "historical" when none was, ending at the highest statement
// supplied. HeadIndex is the head's index, or null; HighestIndex the last
// statement read; LatestCheckpoint the latest checkpoint statement at or
// before it; Conflicts the sequences of the conflict statements read, in
// index order, the first hundred of ConflictsTotal; Retired whether the
// chain's last statement is a retirement. CountersignedAt is the time the
// witness states for the statement the countersigned coverage reaches, and
// ContinuationSaved whether this verification saved a continuation.
type AuditWitness struct {
	KeysSupplied      int64                   `json:"keysSupplied"`
	StatementsRead    int64                   `json:"statementsRead"`
	StatementsChecked int64                   `json:"statementsChecked"`
	Began             string                  `json:"began"`
	ContinuedAfter    *int64                  `json:"continuedAfter,omitempty"`
	Status            string                  `json:"status"`
	Reading           string                  `json:"reading,omitempty"`
	HeadIndex         *int64                  `json:"headIndex"`
	HighestIndex      *int64                  `json:"highestIndex,omitempty"`
	LatestCheckpoint  *AuditWitnessCheckpoint `json:"latestCheckpoint,omitempty"`
	Conflicts         []int64                 `json:"conflicts"`
	ConflictsTotal    int64                   `json:"conflictsTotal"`
	Retired           bool                    `json:"retired"`
	CountersignedAt   string                  `json:"countersignedAt,omitempty"`
	ContinuationSaved bool                    `json:"continuationSaved"`
}

// AuditWitnessCheckpoint is a checkpoint statement by its place in the
// witness's chain, the sequence its checkpoint names, and the time the
// witness states it signed it at, its own clock's.
type AuditWitnessCheckpoint struct {
	Index       int64  `json:"index"`
	Sequence    int64  `json:"sequence"`
	WitnessedAt string `json:"witnessedAt"`
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
	Signatures     *AuditSignatures  `json:"signatures,omitempty"`
	RequiredSigned *AuditRequirement `json:"requiredSigned,omitempty"`
	// Stamps is the stamps file as read, present when time-stamping roots
	// were supplied, and RequiredStamped a stamped coverage the verification
	// was told to require.
	Stamps          *AuditStamps      `json:"stamps,omitempty"`
	RequiredStamped *AuditRequirement `json:"requiredStamped,omitempty"`
	// Witness is a witness's statements as read, present when a witness key
	// was supplied, and RequiredCountersigned a countersigned coverage the
	// verification was told to require.
	Witness               *AuditWitness     `json:"witness,omitempty"`
	RequiredCountersigned *AuditRequirement `json:"requiredCountersigned,omitempty"`
	Findings              []AuditFinding    `json:"findings"`
	FindingsTotal         int               `json:"findingsTotal"`
	Establishes           []string          `json:"establishes"`
	DoesNotEstablish      []string          `json:"doesNotEstablish"`
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

// AuditStamp is jpack audit stamp's payload: the checkpoint stamped, whether
// this run stamped it or found it stamped already, and, for a stamp this run
// kept, the time the authority states, the time the checkpoint existed by
// (that time plus its stated accuracy) and the policy it stamped under.
type AuditStamp struct {
	OutputVersion string          `json:"outputVersion"`
	Tool          Tool            `json:"tool"`
	Command       string          `json:"command"`
	Status        string          `json:"status"`
	TrailPath     string          `json:"trailPath"`
	Checkpoint    AuditCheckpoint `json:"checkpoint"`
	StampedAt     string          `json:"stampedAt,omitempty"`
	ExistedBy     string          `json:"existedBy,omitempty"`
	Policy        string          `json:"policy,omitempty"`
}
