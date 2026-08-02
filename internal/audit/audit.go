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
	"path"
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
// convention" and "this ran on unreviewed law" are different facts. There is no
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
	Disposition          json.RawMessage        `json:"disposition"`
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
}

// UnderLaw records which law this invocation's records were judged under
// (ADR-0019). It is set on the writer rather than passed to each composer
// because the fact is about the run: one invocation consults the lock once, and
// every record it writes carries the same answer. nil is the value for a
// project that declares no lock, and it is also the zero value, so a surface
// that never consults writes records with no such member.
func (w *Writer) UnderLaw(reviewed *bool) {
	if w == nil {
		return
	}
	w.reviewed = reviewed
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
func EvaluationRecord(evaluated result.Evaluation, inputs Inputs, pack []byte, graph *Graph) (Record, error) {
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
		Disposition:    disposition,
	}), nil
}

// CompositeRecord composes the record one completed graph run's headline
// leaves. It repeats no node's inputs or pack: the node records carry those.
// digest names the graph document's exact bytes, which the composite payload
// does not carry — the caller that read those bytes is the one that knows them.
func CompositeRecord(evaluated result.GraphEvaluation, digest string) (Record, error) {
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
func (w *Writer) Evaluation(evaluated result.Evaluation, inputs Inputs, pack []byte, graph *Graph) error {
	if w == nil {
		return nil
	}
	record, err := EvaluationRecord(evaluated, inputs, pack, graph)
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
