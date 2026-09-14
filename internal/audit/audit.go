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
// those bytes itself, and the pack and graph digests are the only places this
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
//     complete, and it is the last line or it is not there at all.
//
// That is what a flat append can honestly promise. A refusal *before* the trail
// is opened writes nothing at all, which is the ordinary case — an escaping
// path, a symlinked trail, a directory that is not one. An I/O failure partway
// through a write cannot be undone by an appender, so the reader rule above,
// not the writer, is what tells a complete run from an abandoned one.
package audit

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
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
	// FailureCode and FailureMessage are the one refusal a failed append
	// produces, stated here so the three surfaces that write records cannot
	// report the same failure three ways. The message names no value: a caller
	// who cannot write the record is not owed the record's contents.
	FailureCode    = "JPS-AUDIT-WRITE"
	FailureMessage = "Audit record could not be written."
)

// Record is one line of the trail.
//
// Disposition is the §8.3 canonical byte sequence, embedded as it was produced
// rather than re-serialized: the pretty-printing path re-indents inside that
// member, and a record whose disposition is not the canonical form would be a
// record nothing can compare byte for byte.
//
// Tool and Artifact are the provenance an evaluation payload already carries and
// a record would be unreconstructible without: which build produced the record,
// and which bundled specification artifacts it evaluated against.
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
type Record struct {
	RecordVersion        string                 `json:"recordVersion"`
	Run                  string                 `json:"run"`
	At                   string                 `json:"at"`
	Kind                 string                 `json:"kind"`
	Surface              string                 `json:"surface"`
	Tool                 result.Tool            `json:"tool"`
	EvaluatorSpecVersion string                 `json:"evaluatorSpecVersion"`
	Pack                 *Pack                  `json:"pack,omitempty"`
	Graph                *Graph                 `json:"graph,omitempty"`
	Inputs               *Inputs                `json:"inputs,omitempty"`
	DraftPrototype       *result.DraftPrototype `json:"draftPrototype,omitempty"`
	Artifact             *result.Artifact       `json:"artifact,omitempty"`
	Reviewed             *bool                  `json:"reviewed,omitempty"`
	ReviewedSet          *ReviewedSet           `json:"reviewedSet,omitempty"`
	Cites                []Citation             `json:"cites,omitempty"`
	Disposition          json.RawMessage        `json:"disposition"`
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

// ReviewedSet names the revision of the reviewed set that made Reviewed true:
// the digest of the exact lock bytes the checks used, the shape those bytes
// declared, and the configuration digest that was compared.
//
// It is here because the lock is replaced in place. Without it a reader holding
// a record and a lock file cannot tell whether that lock is the one the decision
// was judged under, and the Boolean would be a claim nothing outside the run can
// re-derive — which is the opposite of what a trail is for. It is present
// exactly when Reviewed is true: a draft was judged under no reviewed set, and a
// project with no lock has none to name.
type ReviewedSet struct {
	LockDigest   string `json:"lockDigest"`
	LockVersion  string `json:"lockVersion"`
	ConfigDigest string `json:"configDigest"`
}

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
func NewWriter(root *fssecure.Root, dir string) *Writer {
	return &Writer{root: root, dir: dir, run: newRun()}
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
	record.Tool = result.CurrentTool()
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
func (w *Writer) AppendAll(records []Record) error {
	if w == nil || len(records) == 0 {
		return nil
	}
	var lines bytes.Buffer
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
		encoder := json.NewEncoder(&lines)
		// HTML escaping is off so a recorded document reads as the project
		// wrote it rather than as a wall of <. It is a spelling choice and
		// not a semantic one: either form decodes to the same JSON value.
		// Encode writes the newline that ends the line.
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(record); err != nil {
			return err
		}
	}
	if err := w.root.MakeDir(w.dir); err != nil {
		return err
	}
	return w.root.Append(path.Join(w.dir, FileName), lines.Bytes())
}
