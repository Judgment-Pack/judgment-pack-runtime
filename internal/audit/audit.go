// Package audit appends the evaluation records a project asked for (ADR-0018).
//
// It is opt-in and the opt-in is the project's own: a jpack.json declaring an
// audit directory under configVersion "3" has asked to be told what its packs
// decided, and a configuration without that member reaches nothing here. The
// records go into that project's own tree, through the handle internal/project
// already holds open on it, and no surface is ever handed a pathname for
// something else to open (ADR-0012).
//
// What a record deliberately contains is input values: the facts document as
// evaluated, the evidence-availability document, the pack's identity and the
// digest of its exact bytes, and the §8.3 disposition in its canonical form.
// That is not in tension with this runtime's value-free diagnostics — a
// diagnostic goes to an operator who did not ask for the values and may not be
// entitled to them, and a record goes to a directory the project named for
// exactly this purpose. The two are different artifacts with different readers,
// and only the first is sanitized. On unix the trail file is written and kept at
// owner-only permissions; on Windows a Go file mode sets only the read-only
// attribute and does not restrict the DACL, so what may read a record there is
// whatever the containing directory's ACL allows.
//
// Inputs are recorded as JSON *values*, not as source bytes. A document reaches
// the trail through the same encoder every line goes through, and that encoder
// compacts: `{ "x": 1 }` is recorded as `{"x":1}`. Escapes are carried through
// as the caller wrote them, so the text on a line is the source compacted —
// neither the source itself nor a normalization of it, and two callers who sent
// one value spelled two ways leave two differently spelled records. Nothing
// about replay is lost, because evaluation is a function of the value and not of
// its spelling; but a reader who wants the exact bytes a caller sent must keep
// those bytes itself. The pack and graph digests, the executable's digest on
// each record's tool, and on a chained trail each record's previous, which
// names the bytes of the trail's own line before it, are the only places this
// trail speaks about bytes at all.
//
// Three things a record is not. It is not on the deterministic payload path:
// every record carries a wall-clock timestamp, which nothing in an evaluation
// payload does, and no record is an input to anything this runtime computes. It
// is not a result: only a completed evaluation leaves one, so a refused
// evaluation — which has no disposition at all under §8.4 — leaves no record
// either. And on the graph surface the recorded facts are the assembled
// document, after upstream outcomes were injected: what the node was actually
// evaluated against, which is not the same as what the caller supplied.
//
// # Reading a trail
//
// Every record carries a run id: one value per invocation, shared by every
// record that invocation writes, so a single evaluation's record and a whole
// graph run's records are equally attributable to the run that produced them.
// The run id is what makes a graph run's commit visible. A graph run's node
// records are composed as each node completes but held, and appended together
// with the composite in one write, so:
//
//   - node lines whose run id also appears on a "graph-composite" line belong to
//     a run that finished, and that composite is the run's headline;
//   - node lines whose run id has no composite belong to a run that did not
//     finish, and nothing about them should be read as a decision the project
//     took;
//   - a trailing line that is not a complete JSON text is a write that did not
//     complete, and it is the last line or it is not there at all;
//   - a "discontinuity" line records a repair and no decision, and the line
//     before it is the damage it names.
//
// That is what a flat append can honestly promise. A refusal *before* the trail
// is opened writes nothing at all, which is the ordinary case — an escaping
// path, a symlinked trail, a directory that is not one. An I/O failure partway
// through a write cannot be undone by an appender, so the reader rule above,
// not the writer, is what tells a complete run from an abandoned one.
//
// # The chain
//
// A project that keeps a trail has it chained unless its configuration turns
// that off (ADR-0047 §1). A chained record carries three more members, written
// right after recordVersion:
//
//   - trail: the trail's identity, 128 random bits as 32 lowercase hex
//     characters, minted when the trail is first chained and carried by every
//     chained record after;
//   - sequence: the record's line number in the file, counted from 1;
//   - previous: the SHA-256, in the "sha256:" form Digest writes, of the exact
//     bytes of the line before it, without its newline.
//
// The bytes hashed are the bytes in the file, as read. Nothing read from the
// trail is decoded and encoded again, because a record's bytes are what every
// digest of it names: a gateway receipt's decision.recordDigest, a chain link,
// a checkpoint. A record is otherwise exactly what it was before chaining
// existed, member for member and byte for byte, and recordVersion stays "1".
//
// A record follows a chained line by linking to it. A record that follows no
// chained line starts the chain (or starts it again) and commits to everything
// before it at once: its previous is the SHA-256 of the file's whole contents
// up to its own first byte, newlines included, which is the SHA-256 of the file
// as it stood (of the empty string for a new trail), and its sequence continues
// the file's line count. That is how a trail written before chaining, or while
// chaining was off, or by a runtime that does not chain, is covered: as one
// block, never rewritten. A chain started again keeps the identity of the last
// chained line before it, where there is one. Whether a line is chained is read
// from its own members, as JSON reads them (chainedLine), the same way on every
// path: a line that is one JSON object holding a trail of that form, a
// sequence from 1 to 2^53-2 and a previous of that form, each named exactly
// once, is chained; any other line is not. A line longer than maxLineBytes is
// never read and never taken for unchained: a write that must read one is
// refused, ErrOversizedLine, and the writer writes no line that long,
// ErrRecordTooLarge.
//
// The writer holds an exclusive, cooperative lock on the trail
// (fssecure.Root.AppendLocked) across reading the last line, assigning every
// sequence of what it writes (one record, or a graph run's node records and
// composite, the composite last), writing, and syncing the file and its
// directory. It refuses to append after a last line with no newline,
// ErrIncompleteLastLine: a write that did not complete is never chained over.
// Where the platform or the file system offers no lock at all, the records are
// written as they were before chaining, without the three members, because a
// chain written without the lock could link two records to one predecessor; a
// lock that exists but cannot be taken now refuses the append instead.
//
// What the chain establishes, and what it does not. Recomputing each previous
// shows whether the chained lines are consistent with one another: a line
// edited, inserted, deleted or moved anywhere before the last breaks the link
// of the line after it, and the legacy prefix is held as one block. That is
// consistency, not authenticated history. The last line can be edited without
// breaking any link, and a trail cut short, or rewritten from any line on with
// its links recomputed, is as consistent as the real one: only a commitment
// to the trail held by someone other than the operator, covering the lines in
// question, tells them apart (ADR-0047 §2a). Verify checks the links by the
// rules above; it lists the checkpoint of every chained record after a
// sequence, for a deliverer to hand to a holder (Options.List), and holds the
// trail to the checkpoints a holder kept (Options.Held), reporting the records
// none of them covers as unwitnessed. Nothing on the writing path waits for a
// hand-over, and nothing records one: the holder's copy is what counts.
//
// # Repair
//
// Writer.Repair starts a new segment after a last line with no newline. It
// appends and rewrites nothing else: the damaged bytes are ended with a newline
// and kept in place as a line of their own, and a discontinuity record follows
// them, kind "discontinuity", naming that line, its length and its digest. Its
// previous links over the damaged line to what a record in its place would
// have followed, so it is the one record whose previous does not follow the
// line before it, and Verify holds it to that rule. It records no decision and
// carries no pack, inputs or disposition. No discontinuity may name a line that
// is itself a discontinuity, or a line longer than maxLineBytes: Verify reports
// one that does as malformed and excuses nothing, and Repair refuses to write
// one (ErrRepairDiscontinuity, ErrOversizedLine), so a discontinuity's decision
// is final the moment it is read.
package audit

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	// RecordVersion is the shape of one record, a single integer as a string on
	// the outputVersion and configVersion precedent: a reader either knows this
	// shape or does not.
	RecordVersion = "1"
	// FileName is the one file an audit directory holds: one record per line,
	// compact JSON, appended and never rewritten.
	FileName = "evaluations.jsonl"
	// KindEvaluation is one completed single-pack evaluation, including one node
	// of a graph run.
	KindEvaluation = "evaluation"
	// KindGraphComposite is one completed graph run's composite headline. It
	// carries no node's inputs: the node records are where those are, and it is
	// the marker that says its run finished.
	KindGraphComposite = "graph-composite"
	// KindDiscontinuity is the record audit repair appends after a damaged
	// line (ADR-0047 §1). It carries no pack, inputs or disposition: it is not
	// a decision, and a reader that selects records by kind, as every reader of
	// evaluation records does, passes it over.
	KindDiscontinuity = "discontinuity"
	// ReasonIncompleteLastLine is the one reason a discontinuity names today:
	// the line it follows was the trail's last and had no newline.
	ReasonIncompleteLastLine = "incomplete-last-line"
	// FailureCode and FailureMessage are the one refusal a failed append
	// produces, stated here so the three surfaces that write records cannot
	// report the same failure three ways. The message names no value: a caller
	// who cannot write the record is not owed the record's contents.
	FailureCode    = "JPS-AUDIT-WRITE"
	FailureMessage = "Audit record could not be written."
	// incompleteMessage is FailureMessage's form for a trail whose last line is
	// incomplete. It names the trail's state and no value, and it is said
	// because nothing else would tell an operator why every later decision is
	// refused too.
	incompleteMessage = "Audit record could not be written: the audit trail's last line is incomplete, and no record is chained after an incomplete line."
	// oversizedMessage and tooLargeMessage are its forms for a line too long
	// to read whole (maxLineBytes), in the trail or in what would be written.
	oversizedMessage = "Audit record could not be written: a line of the audit trail is longer than 128 MiB, longer than any record this runtime writes, so whether it is chained cannot be read."
	tooLargeMessage  = "Audit record could not be written: the record would be longer than 128 MiB, the longest line a chained audit trail holds."
)

// ErrIncompleteLastLine is the refusal to chain a record after a last line
// with no newline: a write that did not complete. The trail needs repair before
// another chained record can follow it.
var ErrIncompleteLastLine = errors.New("the audit trail's last line is incomplete")

// ErrOversizedLine is the refusal to chain a record when a line the writer
// must read whole to know whether it is chained, the last line or, when the
// chain starts again, any line before it, is longer than maxLineBytes. Such a
// line is never taken for unchained: that would commit to it as part of a
// legacy block, and could start a new identity, over what may be a chained
// record.
var ErrOversizedLine = errors.New("a line of the audit trail is too long to read whole")

// ErrRecordTooLarge is the refusal to write a record whose line would be
// longer than maxLineBytes, which a later writer could not read whole.
var ErrRecordTooLarge = errors.New("the audit record is too long for a chained trail")

// FailureMessageFor is the message a failed append is reported with:
// FailureMessage, or what is wrong with the trail or the record when the
// failure is one the operator must act on. The code is FailureCode either way.
func FailureMessageFor(err error) string {
	switch {
	case errors.Is(err, ErrIncompleteLastLine):
		return incompleteMessage
	case errors.Is(err, ErrOversizedLine):
		return oversizedMessage
	case errors.Is(err, ErrRecordTooLarge):
		return tooLargeMessage
	}
	return FailureMessage
}

// Record is one line of the trail.
//
// Disposition is the §8.3 canonical byte sequence, embedded as it was produced
// rather than re-serialized: the pretty-printing path re-indents inside that
// member, and a record whose disposition is not the canonical form would be a
// record nothing can compare byte for byte.
//
// Tool and Artifact are the provenance an evaluation payload already carries and
// a record would be unreconstructible without: which build produced the record,
// and which bundled specification artifacts it evaluated against. A record's
// Tool also names the executable's bytes, which no payload does (ADR-0043).
// DraftPrototype is carried exactly when the payload carries it, because a
// disposition produced under draft-RFC operators is not a disposition any
// published JPS version defines and a record that dropped the label would say
// otherwise.
//
// Reviewed says which law the decision was judged under (ADR-0019). It is
// present exactly when the project carries a reviewed-set lock: true when every
// document this evaluation applied was one the lock declares and every one of
// them matched, false when any of them was a draft. It is absent — not false —
// for a project that declares no lock, because "this project does not use the
// convention" and "this ran on unreviewed law" are different facts. When it is
// true, ReviewedSet names the revision that made it so. There is no
// "declared but drifted" value, and there cannot be: a deciding surface refuses
// such a run before the evaluator is reached, so nothing composes a record for
// it. The member is additive, which is why recordVersion stays "1" — the same
// rule VERSIONING.md applies to outputVersion, where an added member is
// backward-compatible and a removed or renamed one is the break.
//
// UnknownCauses and TypeMismatches say, on an evaluation record, what the
// evaluation's trace said about its inputs (ADR-0046), because a record keeps
// no trace: every cause an unknown entry named and every equality comparison
// the walk evaluated across JSON types, each distinct one once, in the order
// the trace first names it (traceNotes). A recorded decision whose facts could
// never have matched a comparison then says so where the decision is kept. Each
// names a pointer or an evidence requirement, which are the pack's, and a type
// or a cause, and never a value. Both are omitted when empty, so a record whose
// evaluation crossed no type and left nothing unknown is byte for byte what it
// was before they existed, and a graph composite, which carries no node's
// inputs, carries neither. They are additive, so recordVersion stays "1".
//
// Trail, Sequence and Previous are the chain (ADR-0047 §1; see the package
// doc). They are the writer's to assign, under the trail's lock, and a composer
// leaves them empty: whatever a caller sets is replaced. They are omitted from a
// record that is not chained, so such a record is byte for byte what it was
// before they existed. They are additive, so recordVersion stays "1".
type Record struct {
	RecordVersion        string                 `json:"recordVersion"`
	Trail                string                 `json:"trail,omitempty"`
	Sequence             int64                  `json:"sequence,omitempty"`
	Previous             string                 `json:"previous,omitempty"`
	Run                  string                 `json:"run"`
	At                   string                 `json:"at"`
	Kind                 string                 `json:"kind"`
	Surface              string                 `json:"surface"`
	Tool                 Tool                   `json:"tool"`
	EvaluatorSpecVersion string                 `json:"evaluatorSpecVersion"`
	Pack                 *Pack                  `json:"pack,omitempty"`
	Graph                *Graph                 `json:"graph,omitempty"`
	Inputs               *Inputs                `json:"inputs,omitempty"`
	DraftPrototype       *result.DraftPrototype `json:"draftPrototype,omitempty"`
	Artifact             *result.Artifact       `json:"artifact,omitempty"`
	Reviewed             *bool                  `json:"reviewed,omitempty"`
	ReviewedSet          *ReviewedSet           `json:"reviewedSet,omitempty"`
	Cites                []Citation             `json:"cites,omitempty"`
	UnknownCauses        []result.UnknownCause  `json:"unknownCauses,omitempty"`
	TypeMismatches       []result.TypeMismatch  `json:"typeMismatches,omitempty"`
	Disposition          json.RawMessage        `json:"disposition"`
}

// traceNotes gathers what a trace's entries say about the inputs (ADR-0040):
// every unknown cause and every type mismatch, each distinct one once, in the
// order the trace first names it -- entry by entry in walk order, and within
// an entry in the order the walk met them. Two notes are the same when they
// encode to the same JSON, so the root pointer "" and an unset pointer stay
// different notes, as they are in the trace. The trace is a pure function of
// the evaluation's inputs, so this is too.
func traceNotes(trace []result.TraceEntry) ([]result.UnknownCause, []result.TypeMismatch) {
	var causes []result.UnknownCause
	var mismatches []result.TypeMismatch
	seenCauses, seenMismatches := map[string]bool{}, map[string]bool{}
	for _, entry := range trace {
		for _, cause := range entry.UnknownCauses {
			// These types encode without error: strings, string pointers and a
			// string slice.
			key, _ := json.Marshal(cause)
			if !seenCauses[string(key)] {
				seenCauses[string(key)] = true
				causes = append(causes, cause)
			}
		}
		for _, mismatch := range entry.TypeMismatches {
			key, _ := json.Marshal(mismatch)
			if !seenMismatches[string(key)] {
				seenMismatches[string(key)] = true
				mismatches = append(mismatches, mismatch)
			}
		}
	}
	return causes, mismatches
}

// Tool is the build that wrote a record: the name and version every payload
// carries, and the SHA-256 of the executable that ran (ADR-0043). The digest is
// the third fact of the replay tuple docs/building-with-packs.md names, beside
// the pack's digest and the version, and a version string cannot stand in for
// it: a version names a release, and the digest names the bytes. It is the
// running program's account of itself, so it is evidence of which build ran and
// not proof of it: a modified binary can report any digest it likes. Where the
// executable cannot be read whole, the member is omitted and the record is
// otherwise whole. It is additive, so recordVersion stays "1".
type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest,omitempty"`
}

// toolDigest answers the running executable's digest, or "" when it cannot be
// read whole. It is computed once per process, the first time a record is
// composed, so an invocation that composes no record never reads its own
// executable, and a long-lived server hashes itself once however many records
// it writes. A graph run composes its node records as the nodes complete, so a
// graph run refused after its first node has read the executable though it
// writes nothing. A test replaces it.
var toolDigest = sync.OnceValue(func() string { return digestOf(openRunningExecutable) })

// executableReads counts the times openRunningExecutable has run in this
// process, so a test can hold toolDigest to opening the executable once, and
// not before a record is composed.
var executableReads atomic.Int64

// procSelfExe is the Linux name of the running program's own file. A test
// replaces it with a name that resolves to nothing.
var procSelfExe = "/proc/self/exe"

// openRunningExecutable opens the program that is running.
//
// On Linux that is /proc/self/exe, which names the running file itself: a binary
// replaced on disk after this process started, as an upgrade under a running
// server does, is still the one hashed. If it cannot be opened there is no
// fallback, because the path os.Executable reports may by then name another
// file, and a digest of that file would name bytes that did not run.
//
// Elsewhere it is the path os.Executable reports, read when the first record is
// composed. That is the running program unless the path was replaced or
// retargeted after the process started, in which case the digest names what is
// at the path then. ADR-0043 states this limit.
func openRunningExecutable() (executableFile, error) {
	executableReads.Add(1)
	if runtime.GOOS == "linux" {
		return os.Open(procSelfExe)
	}
	name, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return os.Open(name)
}

// executableFile is what digestOf reads: an open file that can say its size.
type executableFile interface {
	io.ReadCloser
	Stat() (fs.FileInfo, error)
}

// digestOf hashes what open yields, in the form Digest writes, or answers ""
// when it cannot be opened or read to the end, or when the read shows it was
// not one file's bytes: a size or modification time that differs between the
// observations before and after the read, or a number of bytes read that
// differs from the size. That check is as good as those observations and no
// better: a file rewritten during the read and restored to its size and time
// is not seen. On Linux the file is the running image, which the kernel does
// not let a writer open, so the case arises only elsewhere (ADR-0043).
func digestOf(open func() (executableFile, error)) string {
	file, err := open()
	if err != nil {
		return ""
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return ""
	}
	hash := sha256.New()
	read, err := io.Copy(hash, file)
	if err != nil {
		return ""
	}
	after, err := file.Stat()
	if err != nil || read != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return ""
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

// Citation names one gateway receipt the caller said this decision relied
// on (ADR-0033), in the shape the gateway's action receipt gives the same
// member: the session, the receipt's index in it, and the receipt's own
// signature. It is recorded as given. This runtime holds a supplied
// document to the shape below and to nothing else: it does not know
// whether the session exists, whether the signature is one, or whether the
// receipt has anything to do with the facts — those are the gateway
// verifier's findings, made against its own store with the trail beside
// it. The member is additive, so recordVersion stays "1", as it did for
// Reviewed.
type Citation struct {
	SessionID string `json:"sessionId"`
	CallIndex int64  `json:"callIndex"`
	Signature string `json:"signature"`
}

// The gateway's grammar for what a citation names: a session id is a flat
// token (SPEC.md §3a), a signature 128 lowercase hex characters (§1.2a),
// an index an integer the gateway's canonical domain holds (§1.1).
var (
	flatToken    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	signatureHex = regexp.MustCompile(`^[0-9a-f]{128}$`)
)

const maxSafeInteger = 9007199254740991

// MaxCitesBytes bounds a citations document: a citation is under two
// hundred bytes, and a decision that relied on more than a few thousand
// receipts is not one this trail was built for.
const MaxCitesBytes = 1 << 20

// ParseCites holds a citations document to the gateway's shape: a JSON
// array whose every element is an object with exactly sessionId (a flat
// token, [A-Za-z0-9._-]{1,128} and never "." or ".."), callIndex (an
// integer literal from 0 to 2^53-1) and signature (exactly 128 lowercase
// hexadecimal characters) — the structural constraints the gateway's
// SPEC.md §§1.2a and 3a put on an action receipt's citation, so nothing is
// recorded that no conforming receipt could carry. Member names are
// matched exactly and a member twice is refused, since encoding/json would
// fold "SessionId" onto sessionId and keep the last of two — and a citation
// is recorded as given, so what is given must be one thing. A string is
// its value: a JSON escape is read as the character it spells, and a
// value the grammar admits is ASCII, so a lone surrogate or invalid UTF-8
// cannot pass as anything. Nothing is resolved or verified. An empty array
// is no citation, and is returned as nil so that the record omits the
// member.
func ParseCites(document []byte) ([]Citation, error) {
	if len(document) > MaxCitesBytes {
		return nil, fmt.Errorf("the citations document exceeds %d bytes", MaxCitesBytes)
	}
	// An array and nothing else: null would decode into an empty slice
	// and pass for no citation, and null is not what a caller who wrote
	// it meant.
	// Only JSON's own whitespace is passed over on the way to the array;
	// a form feed or a Unicode space is not JSON and is not read past.
	trimmed := bytes.Trim(document, " \t\r\n")
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, errors.New("the citations document is not a JSON array")
	}
	var raw []json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if err := decoder.Decode(&raw); err != nil {
		return nil, errors.New("the citations document is not a JSON array")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("the citations document carries more than one JSON text")
	}
	if len(raw) == 0 {
		return nil, nil
	}
	cites := make([]Citation, 0, len(raw))
	for i, element := range raw {
		members, err := exactObject(element)
		if err != nil {
			return nil, fmt.Errorf("citation %d: %v", i, err)
		}
		for name := range members {
			if name != "sessionId" && name != "callIndex" && name != "signature" {
				return nil, fmt.Errorf("citation %d: unknown member %q", i, name)
			}
		}
		var c Citation
		if err := decodeString(members["sessionId"], &c.SessionID); err != nil || !flatToken.MatchString(c.SessionID) || c.SessionID == "." || c.SessionID == ".." {
			return nil, fmt.Errorf("citation %d: sessionId must be a flat token of 1 to 128 characters from A-Z, a-z, 0-9, dot, underscore and hyphen, and not . or ..", i)
		}
		if err := decodeInteger(members["callIndex"], &c.CallIndex); err != nil || c.CallIndex < 0 || c.CallIndex > maxSafeInteger {
			return nil, fmt.Errorf("citation %d: callIndex must be an integer from 0 to 9007199254740991", i)
		}
		if err := decodeString(members["signature"], &c.Signature); err != nil || !signatureHex.MatchString(c.Signature) {
			return nil, fmt.Errorf("citation %d: signature must be exactly 128 lowercase hexadecimal characters", i)
		}
		cites = append(cites, c)
	}
	return cites, nil
}

// exactObject reads one JSON object into its members by exact name,
// refusing anything that is not an object and any member given twice.
func exactObject(element json.RawMessage) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(element))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("not an object")
	}
	members := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, errors.New("not an object")
		}
		name, ok := token.(string)
		if !ok {
			return nil, errors.New("not an object")
		}
		if _, dup := members[name]; dup {
			return nil, fmt.Errorf("member %q given twice", name)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errors.New("not an object")
		}
		members[name] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("not an object")
	}
	return members, nil
}

// decodeString reads a JSON string and nothing else; an absent member is
// an error.
func decodeString(raw json.RawMessage, into *string) error {
	if len(raw) == 0 || raw[0] != '"' {
		return errors.New("not a string")
	}
	return json.Unmarshal(raw, into)
}

// decodeInteger reads a JSON number that is an integer literal -- no
// fraction, no exponent, no leading zero -- and nothing else. -0 is 0, as
// the gateway's grammar reads it (SPEC.md §1.1).
func decodeInteger(raw json.RawMessage, into *int64) error {
	text := string(bytes.TrimSpace(raw))
	if text == "" || strings.ContainsAny(text, ".eE") {
		return errors.New("not an integer")
	}
	if (len(text) > 1 && text[0] == '0') || (len(text) > 2 && text[0] == '-' && text[1] == '0') {
		return errors.New("not an integer")
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return errors.New("not an integer")
	}
	*into = n
	return nil
}

// ReviewedSet names the revision of the reviewed set that made Reviewed true.
// It is the payload's type (ADR-0044), so a record and the payload beside it
// name a revision in one shape; its members are documented there.
type ReviewedSet = result.ReviewedSet

// Pack is the identity of the document that was evaluated, plus the digest of
// its exact bytes — the one fact no payload carries, and the one that lets a
// reader tell two versions of a pack apart when neither changed its version
// member.
type Pack struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	SpecVersion string `json:"specVersion"`
	Digest      string `json:"digest"`
}

// Graph names the composition a record belongs to: Node on one node's record,
// ResultNode on the composite's. Digest is the graph document's exact bytes, on
// the same reasoning the pack digest exists for — a graph's id and version are
// mutable, and two different compositions can carry both unchanged.
type Graph struct {
	ID            string `json:"id"`
	Version       string `json:"version"`
	FormatVersion string `json:"formatVersion,omitempty"`
	Digest        string `json:"digest,omitempty"`
	Node          string `json:"node,omitempty"`
	ResultNode    string `json:"resultNode,omitempty"`
}

// Inputs are the two documents the evaluation ran against, as they reached the
// engine and as JSON values rather than as source bytes (see the package doc).
// EvidenceSupplied is not redundant with a null Evidence: §8.2 gives an omitted
// document and a supplied empty one two different meanings, and a record that
// collapsed them would not describe the evaluation that happened.
type Inputs struct {
	Facts            json.RawMessage `json:"facts"`
	Evidence         json.RawMessage `json:"evidence"`
	EvidenceSupplied bool            `json:"evidenceSupplied"`
}

// Digest names one document's exact bytes, in the algorithm-prefixed form the
// rest of this runtime writes a digest in.
func Digest(document []byte) string {
	sum := sha256.Sum256(document)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Writer appends records to one project's audit directory.
//
// Its nil value writes nothing and reports no failure, which is what a project
// that declared no audit member asked for: the surfaces then need no branch of
// their own, and "no configuration" cannot be mistaken at a call site for "the
// write failed".
type Writer struct {
	root     *fssecure.Root
	dir      string
	run      string
	chain    bool
	reviewed *bool
	underLaw *ReviewedSet
}

// UnderLaw records which law this invocation's records were judged under
// (ADR-0019). It is set on the writer rather than passed to each composer
// because the fact is about the run: one invocation consults the lock once, and
// every record it writes carries the same answer. nil is the value for a
// project that declares no lock, and it is also the zero value, so a surface
// that never consults writes records with no such member.
func (w *Writer) UnderLaw(reviewed *bool, set *ReviewedSet) {
	if w == nil {
		return
	}
	w.reviewed = reviewed
	w.underLaw = set
}

// NewWriter binds a writer to one project's directory handle. The handle stays
// the project's — the writer neither owns nor closes it — so a record is
// written only while the project it belongs to is open.
//
// One writer is made per invocation of a recording surface and mints the run id
// every record it writes carries. That is why the id is the writer's and not a
// composer's: what it identifies is the invocation, and an invocation has
// exactly one writer.
//
// chain says whether the records are chained (ADR-0047 §1). A project's
// configuration chains unless it says otherwise; a writer that does not chain
// appends exactly as this package did before the chain existed, without the
// lock and without reading the trail.
func NewWriter(root *fssecure.Root, dir string, chain bool) *Writer {
	return &Writer{root: root, dir: dir, run: newRun(), chain: chain}
}

// newRun mints one run id: eight random bytes, hex. It names an invocation and
// nothing else — it is not a sequence number, carries no time, and says nothing
// about the project — so a reader can group a run's lines and tell an
// unfinished run from a finished one without being able to infer anything from
// the value itself.
func newRun() string {
	var raw [8]byte
	// crypto/rand.Read cannot fail on a supported platform: it crashes the
	// program rather than returning entropy it does not have.
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

// EvaluationRecord composes the record one completed single-pack evaluation
// leaves. graph is nil off the graph surface and names the node on it.
//
// Composing and appending are two operations because the graph surface holds
// its records until the whole run has completed. The record is stamped here,
// when the evaluation it describes finished, rather than when the line is
// written — a held record's own time is when its node ran.
func EvaluationRecord(evaluated result.Evaluation, inputs Inputs, cites []Citation, pack []byte, graph *Graph) (Record, error) {
	disposition, err := evaluated.Disposition.Canonical()
	if err != nil {
		return Record{}, err
	}
	// An unsupplied document is a JSON null in the record, never empty bytes:
	// empty bytes are not a JSON text and would make the line undecodable.
	if len(inputs.Evidence) == 0 {
		inputs.Evidence = nil
	}
	causes, mismatches := traceNotes(evaluated.Trace)
	return stamp(Record{
		Kind:                 KindEvaluation,
		Surface:              evaluated.Command,
		EvaluatorSpecVersion: evaluated.EvaluatorSpecVersion,
		Pack: &Pack{
			ID:          evaluated.PackID,
			Version:     evaluated.PackVersion,
			SpecVersion: evaluated.SpecVersion,
			Digest:      Digest(pack),
		},
		Graph:          graph,
		Inputs:         &inputs,
		DraftPrototype: evaluated.DraftPrototype,
		Artifact:       evaluated.Artifact,
		Cites:          cites,
		UnknownCauses:  causes,
		TypeMismatches: mismatches,
		Disposition:    disposition,
	}), nil
}

// CompositeRecord composes the record one completed graph run's headline
// leaves. It repeats no node's inputs or pack: the node records carry those.
// digest names the graph document's exact bytes, which the composite payload
// does not carry — the caller that read those bytes is the one that knows them.
func CompositeRecord(evaluated result.GraphEvaluation, digest string, cites []Citation) (Record, error) {
	disposition, err := evaluated.Disposition.Canonical()
	if err != nil {
		return Record{}, err
	}
	return stamp(Record{
		Kind:                 KindGraphComposite,
		Surface:              evaluated.Command,
		EvaluatorSpecVersion: evaluated.EvaluatorSpecVersion,
		Graph: &Graph{
			ID:            evaluated.GraphID,
			Version:       evaluated.GraphVersion,
			FormatVersion: evaluated.FormatVersion,
			Digest:        digest,
			ResultNode:    evaluated.ResultNode,
		},
		Artifact:    evaluated.Artifact,
		Cites:       cites,
		Disposition: disposition,
	}), nil
}

// stamp fixes a record's shape, its moment, and the build that made it. The
// version is stated by this package rather than by a caller so no surface can
// forget it, and the clock is read here and nowhere an evaluation can see it.
// The resolution is nanoseconds: a graph run writes several records in one
// instant, and records that shared a whole-second timestamp would carry no way
// to order them. The run id is not set here — it belongs to the writer.
func stamp(record Record) Record {
	record.RecordVersion = RecordVersion
	current := result.CurrentTool()
	record.Tool = Tool{Name: current.Name, Version: current.Version, Digest: toolDigest()}
	record.At = time.Now().UTC().Format(time.RFC3339Nano)
	return record
}

// Evaluation composes and appends one completed single-pack evaluation's record.
func (w *Writer) Evaluation(evaluated result.Evaluation, inputs Inputs, cites []Citation, pack []byte, graph *Graph) error {
	if w == nil {
		return nil
	}
	record, err := EvaluationRecord(evaluated, inputs, cites, pack, graph)
	if err != nil {
		return err
	}
	return w.Append(record)
}

// Append writes one record as one line.
func (w *Writer) Append(record Record) error {
	return w.AppendAll([]Record{record})
}

// AppendAll writes every record as one line each, in one open of the trail, all
// of them carrying this writer's run id.
//
// The graph surface hands over a whole run at once: the records exist only in
// memory until the run has a composite, so a run refused before then is never
// opened for, and one failed open loses all of them together rather than
// leaving a run half-recorded. What one open cannot promise is atomicity
// against an I/O failure partway through the write — that is what the run id
// and the composite marker are for, and the package doc states the rule a
// reader applies.
//
// A chaining writer assigns the chain under the trail's lock, in the order the
// records are given, so a graph run's composite, handed over last, is the last
// line of its batch and the batch's sequences are consecutive. Every record is
// encoded before the trail is opened, so a record that cannot be encoded
// refuses the whole batch before anything is created or written.
func (w *Writer) AppendAll(records []Record) error {
	if w == nil || len(records) == 0 {
		return nil
	}
	prepared := make([]Record, 0, len(records))
	for _, record := range records {
		if record.RecordVersion == "" || record.At == "" {
			record = stamp(record)
		}
		record.Run = w.run
		record.Reviewed = w.reviewed
		// The set is named exactly when the record claims a review. A draft was
		// judged under none, and a project with no lock has none to name, so
		// neither carries a member a reader could mistake for provenance.
		record.ReviewedSet = nil
		if w.reviewed != nil && *w.reviewed {
			record.ReviewedSet = w.underLaw
		}
		// The chain is the writer's, assigned under the lock or not at all.
		record.Trail, record.Sequence, record.Previous = "", 0, ""
		prepared = append(prepared, record)
	}
	unchained, err := encodeLines(prepared)
	if err != nil {
		return err
	}
	if err := w.root.MakeDir(w.dir); err != nil {
		return err
	}
	name := path.Join(w.dir, FileName)
	if !w.chain {
		return w.root.Append(name, unchained)
	}
	return w.root.AppendLocked(name, func(state fssecure.AppendState) ([]byte, error) {
		return chainedLines(prepared, unchained, state)
	})
}

// encodeLines encodes records as the trail's lines: compact JSON, one record
// per line, each ending its line.
func encodeLines(records []Record) ([]byte, error) {
	var lines bytes.Buffer
	encoder := json.NewEncoder(&lines)
	// HTML escaping is off so a recorded document reads as the project
	// wrote it rather than as a wall of <. It is a spelling choice and
	// not a semantic one: either form decodes to the same JSON value.
	// Encode writes the newline that ends the line.
	encoder.SetEscapeHTML(false)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			return nil, err
		}
	}
	return lines.Bytes(), nil
}

// chainedLines is what a chaining writer appends, decided under the trail's
// lock: the records chained after what the trail holds, or, where no lock could
// be taken, the unchained lines, since a chain read and written without the
// lock could link two records to one predecessor. Either way no line longer
// than maxLineBytes is written, so no line a chaining writer wrote is one a
// later one cannot read whole.
func chainedLines(records []Record, unchained []byte, state fssecure.AppendState) ([]byte, error) {
	if !state.Locked {
		for _, line := range bytes.SplitAfter(unchained, []byte("\n")) {
			if int64(len(bytes.TrimSuffix(line, []byte("\n")))) > maxLineBytes {
				return nil, ErrRecordTooLarge
			}
		}
		return unchained, nil
	}
	next, err := readHead(state.Contents, state.Size)
	if err != nil {
		return nil, err
	}
	var lines bytes.Buffer
	previous := next.previous
	for index, record := range records {
		record.Trail = next.trail
		record.Sequence = next.sequence + int64(index) + 1
		record.Previous = previous
		line, err := encodeLines([]Record{record})
		if err != nil {
			return nil, err
		}
		if int64(len(line)-1) > maxLineBytes {
			return nil, ErrRecordTooLarge
		}
		// The next record links to these bytes exactly as they will be in the
		// file, without the newline that ends them.
		previous = Digest(line[:len(line)-1])
		lines.Write(line)
	}
	return lines.Bytes(), nil
}

// head is what the next chained record follows: the trail's identity, the
// sequence of the line before it, and the digest that record's previous holds.
type head struct {
	trail    string
	sequence int64
	previous string
}

// The forms a chained line's members take. A line whose members are not of
// them is not chained.
var (
	trailForm    = regexp.MustCompile(`^[0-9a-f]{32}$`)
	previousForm = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// maxLineBytes bounds a line of a chained trail: the longest line the writer
// reads whole to see whether it is chained, and the longest it writes.
//
// An ordinary record is far below it. The facts and evidence documents a
// record carries were each admitted under the carrier's 10 MiB limit
// (unchanged since the first release) and are recorded compacted, never
// longer, and its citations are bounded at MaxCitesBytes. Its disposition comes
// from a pack admitted under the same limit. What no input limit bounds is the
// trace notes: the evaluation work limit bounds how many there are, and a pack
// spells their pointers, so a pack built for it, quantifying under the RFC 0008
// prototype over a collection with long pointers, can make a record longer
// than the bound. The writer refuses such a record rather than write it
// (ErrRecordTooLarge), so the longest line a chaining writer leaves is the
// bound. A line over it in a trail, which a runtime before the chain or a
// writer with chain off could have left, is refused rather than read
// (ErrOversizedLine): moving that trail aside starts a new one. A test lowers
// the bound.
var maxLineBytes int64 = 128 << 20

// readChunk is how much of the trail one read takes while looking for the last
// line's start or hashing the file.
const readChunk = 64 << 10

// readHead reads what the next chained record follows, from the trail's
// contents as the lock found them. Only the last line is read when it is
// chained, which is the ordinary case and costs one short read however long the
// trail is; otherwise the whole file is read once, to commit to it.
func readHead(contents io.ReaderAt, size int64) (head, error) {
	if size == 0 {
		return head{trail: newTrail(), previous: Digest(nil)}, nil
	}
	line, err := lastLine(contents, size)
	if err != nil {
		return head{}, err
	}
	if trail, sequence, ok := chainedLine(line); ok {
		return head{trail: trail, sequence: sequence, previous: Digest(line)}, nil
	}
	digest, lines, trail, err := readPrefix(contents, size)
	if err != nil {
		return head{}, err
	}
	if trail == "" {
		trail = newTrail()
	}
	return head{trail: trail, sequence: lines, previous: digest}, nil
}

// lastLine reads the trail's last line, without its newline, refusing a trail
// whose last byte is not a newline and a last line longer than maxLineBytes,
// which is not read: looking for its start stops once it is past the bound.
func lastLine(contents io.ReaderAt, size int64) ([]byte, error) {
	var final [1]byte
	if err := readAt(contents, final[:], size-1); err != nil {
		return nil, err
	}
	if final[0] != '\n' {
		return nil, ErrIncompleteLastLine
	}
	end := size - 1
	start := int64(0)
	chunk := make([]byte, readChunk)
	for cursor := end; cursor > 0; {
		if end-cursor > maxLineBytes {
			return nil, ErrOversizedLine
		}
		n := min(int64(len(chunk)), cursor)
		if err := readAt(contents, chunk[:n], cursor-n); err != nil {
			return nil, err
		}
		if index := bytes.LastIndexByte(chunk[:n], '\n'); index >= 0 {
			start = cursor - n + int64(index) + 1
			break
		}
		cursor -= n
	}
	if end-start > maxLineBytes {
		return nil, ErrOversizedLine
	}
	line := make([]byte, end-start)
	if err := readAt(contents, line, start); err != nil {
		return nil, err
	}
	return line, nil
}

// readAt reads exactly len(into) bytes at offset.
func readAt(contents io.ReaderAt, into []byte, offset int64) error {
	_, err := io.ReadFull(io.NewSectionReader(contents, offset, int64(len(into))), into)
	return err
}

// readPrefix reads the whole trail once: the SHA-256 of all of it, the number
// of lines, and the identity of the last chained line in it, or "" when there
// is none. The trail's last byte is a newline, which lastLine has already
// checked, so every line is counted.
//
// Every line is read by chainedLine, the one rule readHead reads the last line
// by, and nothing narrower filters what reaches it: a line one path takes for
// chained, the other must too, or a chain started again would take a new
// identity over a chained line written in a spelling the filter did not
// expect. A line longer than maxLineBytes is refused, ErrOversizedLine, for
// lastLine's reason. Reading every line costs one parse per line, and is paid
// only when a chain starts or starts again.
func readPrefix(contents io.ReaderAt, size int64) (string, int64, string, error) {
	hash := sha256.New()
	reader := bufio.NewReaderSize(io.NewSectionReader(contents, 0, size), readChunk)
	var lines int64
	trail := ""
	line := []byte{}
	for {
		piece, err := reader.ReadSlice('\n')
		hash.Write(piece)
		if int64(len(line)+len(piece)) > maxLineBytes+1 {
			return "", 0, "", ErrOversizedLine
		}
		line = append(line, piece...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", 0, "", err
		}
		lines++
		if found, _, ok := chainedLine(line[:len(line)-1]); ok {
			trail = found
		}
		line = line[:0]
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), lines, trail, nil
}

// chainedLine reads a line's chain members: the trail and the sequence, and
// whether it is a chained line at all. The line must be one JSON text, an
// object naming each member exactly once, so a line two readers could read two
// ways is not chained. The sequence stops short of 2^53-1 so the next one is
// still an integer every JSON reader holds exactly.
//
// The rule is about the JSON value, never about the bytes' layout: whitespace
// between tokens and an escape in a member's name are read as JSON reads them.
// That is how a verifier reads a record, by its members, as Runner and the
// gateway read records today; a writer that recognised fewer lines than the
// verifier would start a chain again over a line the verifier holds to be
// chained, and the verifier would report a break the writer made.
func chainedLine(line []byte) (string, int64, bool) {
	found, _, ok := readLink(line)
	return found.trail, found.sequence, ok
}

// link is a chained line's three members.
type link struct {
	trail    string
	sequence int64
	previous string
}

// readLink is chainedLine's rule, with the chain members it read and every
// member of the line, so a reader that needs more of a chained line than its
// trail and sequence parses it once. The members are nil when the line is not
// one JSON object.
func readLink(line []byte) (link, map[string]json.RawMessage, bool) {
	if len(line) == 0 || !json.Valid(line) {
		return link{}, nil, false
	}
	members, err := exactObject(line)
	if err != nil {
		return link{}, nil, false
	}
	var found link
	if decodeString(members["trail"], &found.trail) != nil || !trailForm.MatchString(found.trail) {
		return link{}, members, false
	}
	if decodeInteger(members["sequence"], &found.sequence) != nil || found.sequence < 1 || found.sequence >= maxSafeInteger {
		return link{}, members, false
	}
	if decodeString(members["previous"], &found.previous) != nil || !previousForm.MatchString(found.previous) {
		return link{}, members, false
	}
	return found, members, true
}

// newTrail mints a trail's identity: sixteen random bytes, hex. Like a run id
// it carries no time and says nothing about the project.
func newTrail() string {
	var raw [16]byte
	// crypto/rand.Read cannot fail on a supported platform: it crashes the
	// program rather than returning entropy it does not have.
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}
