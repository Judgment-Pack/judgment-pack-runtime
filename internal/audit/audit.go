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
// and only the first is sanitized.
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
// A graph run's records are buffered rather than appended node by node, and
// written in one open once the run has a composite. That is what makes "a
// refused run records nothing" true of a composition as well as of a single
// evaluation: a node refused halfway through would otherwise leave the earlier
// nodes' records behind with no composite and nothing marking them as belonging
// to a run that never finished. The cost is that a very large graph's records
// are held in memory until the end, bounded by the same input limits the run
// itself is bounded by.
package audit

import (
	"bytes"
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
	// carries no node's inputs: the node records are where those are.
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
type Record struct {
	RecordVersion        string          `json:"recordVersion"`
	At                   string          `json:"at"`
	Kind                 string          `json:"kind"`
	Surface              string          `json:"surface"`
	EvaluatorSpecVersion string          `json:"evaluatorSpecVersion"`
	Pack                 *Pack           `json:"pack,omitempty"`
	Graph                *Graph          `json:"graph,omitempty"`
	Inputs               *Inputs         `json:"inputs,omitempty"`
	Disposition          json.RawMessage `json:"disposition"`
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
// ResultNode on the composite's.
type Graph struct {
	ID         string `json:"id"`
	Version    string `json:"version"`
	Node       string `json:"node,omitempty"`
	ResultNode string `json:"resultNode,omitempty"`
}

// Inputs are the two documents the evaluation ran against, as they reached the
// engine. EvidenceSupplied is not redundant with a null Evidence: §8.2 gives an
// omitted document and a supplied empty one two different meanings, and a
// record that collapsed them would not describe the evaluation that happened.
type Inputs struct {
	Facts            json.RawMessage `json:"facts"`
	Evidence         json.RawMessage `json:"evidence"`
	EvidenceSupplied bool            `json:"evidenceSupplied"`
}

// Digest names one pack's exact bytes, in the algorithm-prefixed form the rest
// of this runtime writes a digest in.
func Digest(pack []byte) string {
	sum := sha256.Sum256(pack)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Writer appends records to one project's audit directory.
//
// Its nil value writes nothing and reports no failure, which is what a project
// that declared no audit member asked for: the surfaces then need no branch of
// their own, and "no configuration" cannot be mistaken at a call site for "the
// write failed".
type Writer struct {
	root *fssecure.Root
	dir  string
}

// NewWriter binds a writer to one project's directory handle. The handle stays
// the project's — the writer neither owns nor closes it — so a record is
// written only while the project it belongs to is open.
func NewWriter(root *fssecure.Root, dir string) *Writer {
	return &Writer{root: root, dir: dir}
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
		Graph:       graph,
		Inputs:      &inputs,
		Disposition: disposition,
	}), nil
}

// CompositeRecord composes the record one completed graph run's headline
// leaves. It repeats no node's inputs or pack: the node records carry those.
func CompositeRecord(evaluated result.GraphEvaluation) (Record, error) {
	disposition, err := evaluated.Disposition.Canonical()
	if err != nil {
		return Record{}, err
	}
	return stamp(Record{
		Kind:                 KindGraphComposite,
		Surface:              evaluated.Command,
		EvaluatorSpecVersion: evaluated.EvaluatorSpecVersion,
		Graph: &Graph{
			ID:         evaluated.GraphID,
			Version:    evaluated.GraphVersion,
			ResultNode: evaluated.ResultNode,
		},
		Disposition: disposition,
	}), nil
}

// stamp fixes a record's shape and its moment. The version is stated by this
// package rather than by a caller so no surface can forget it, and the clock is
// read here and nowhere an evaluation can see it. The resolution is
// nanoseconds: a graph run writes several records in one instant, and records
// that shared a whole-second timestamp would carry no way to order them.
func stamp(record Record) Record {
	record.RecordVersion = RecordVersion
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

// AppendAll writes every record as one line each, in one open of the trail.
//
// The graph surface hands over a whole run at once, which is what makes a
// refused run leave nothing: the records exist only in memory until the run has
// a composite, and one failed open loses all of them together rather than
// leaving a run half-recorded. Writing them in one open is also what keeps a
// run's records contiguous when two processes append to one trail.
func (w *Writer) AppendAll(records []Record) error {
	if w == nil || len(records) == 0 {
		return nil
	}
	var lines bytes.Buffer
	for _, record := range records {
		if record.RecordVersion == "" || record.At == "" {
			record = stamp(record)
		}
		encoder := json.NewEncoder(&lines)
		// The records hold documents this runtime was handed, not HTML: escaping an
		// angle bracket would make the recorded facts differ from the evaluated
		// ones. Encode writes the newline that ends the line.
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
