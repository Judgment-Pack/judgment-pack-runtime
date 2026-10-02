package audit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func text(value string) *string { return &value }

// notedEvaluation is evaluated() with a trace whose entries noted what ADR-0040
// records: causes on the unknown entries, mismatches wherever the walk compared
// across types, repeats across entries, the root pointer "" beside an unset
// one, and a within beside its absence.
func notedEvaluation() result.Evaluation {
	evaluation := evaluated()
	investigation := result.TypeMismatch{Path: "/expense/activeInvestigation", Operator: "equals", FactType: "string", OperandTypes: []string{"boolean"}}
	evaluation.Trace = []result.TraceEntry{
		{Stage: "applicability", Condition: "true"},
		{Stage: "exception", ID: "active-investigation", Condition: "false", TypeMismatches: []result.TypeMismatch{investigation}},
		{Stage: "rule", ID: "large-expense", Condition: "unknown", OnUnknown: "escalate",
			UnknownCauses: []result.UnknownCause{{Path: text("/expense/amount"), Cause: "not-comparable", FactType: "number"}},
			TypeMismatches: []result.TypeMismatch{
				investigation,
				{Path: "/flag", Within: text(""), Operator: "in", FactType: "null", OperandTypes: []string{"string", "boolean"}},
			}},
		{Stage: "rule", ID: "ordinary-expense", Condition: "unknown", OnUnknown: "escalate",
			UnknownCauses: []result.UnknownCause{
				{Path: text(""), Cause: "absent"},
				{EvidenceRequirement: "receipt", Cause: "unknown"},
				{Path: text("/expense/amount"), Cause: "not-comparable", FactType: "number"},
				{Cause: "unsupported"},
				{Path: text(""), Cause: "unsupported"},
			},
			TypeMismatches: []result.TypeMismatch{{Path: "/flag", Operator: "in", FactType: "null", OperandTypes: []string{"string", "boolean"}}}},
	}
	return evaluation
}

// A record says what its evaluation's trace said about the inputs (ADR-0046):
// every cause and every type mismatch, each distinct one once, in the order the
// trace first names it, in the trace's own shape -- a pointer or a
// requirement, and a type or a cause -- and never a value. The root pointer
// "" is kept apart from an unset one, and a within from its absence, because
// the trace keeps them apart.
func TestARecordSaysWhatTheTraceSaidAboutItsInputs(t *testing.T) {
	record, err := EvaluationRecord(notedEvaluation(), Inputs{Facts: []byte(`{"expense":{"amount":120,"activeInvestigation":"SECRET-VALUE"}}`)}, nil, []byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	const causes = `"unknownCauses":[` +
		`{"path":"/expense/amount","cause":"not-comparable","factType":"number"},` +
		`{"path":"","cause":"absent"},` +
		`{"evidenceRequirement":"receipt","cause":"unknown"},` +
		`{"cause":"unsupported"},` +
		`{"path":"","cause":"unsupported"}]`
	const mismatches = `"typeMismatches":[` +
		`{"path":"/expense/activeInvestigation","operator":"equals","factType":"string","operandTypes":["boolean"]},` +
		`{"path":"/flag","within":"","operator":"in","factType":"null","operandTypes":["string","boolean"]},` +
		`{"path":"/flag","operator":"in","factType":"null","operandTypes":["string","boolean"]}]`
	if !bytes.Contains(line, []byte(causes+","+mismatches+`,"disposition":`)) {
		t.Fatalf("the record carries the trace's notes, deduplicated in trace order, before the disposition:\n%s", line)
	}
	notes := line[bytes.Index(line, []byte(`"unknownCauses"`)):bytes.Index(line, []byte(`"disposition"`))]
	if bytes.Contains(notes, []byte("SECRET-VALUE")) || bytes.Contains(notes, []byte("120")) {
		t.Fatalf("the notes name no value: %s", notes)
	}

	// The same through the writer, which is how every surface appends.
	writer, root := writerAt(t, "audit")
	if err := writer.Evaluation(notedEvaluation(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(root, "audit", FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(written, []byte(causes+","+mismatches+`,"disposition":`)) {
		t.Fatalf("the appended line carries the notes: %s", written)
	}
}

// Each member is there when its notes are, and only then: a trace with
// mismatches and no unknown carries typeMismatches alone, one with an unknown
// and no mismatch carries unknownCauses alone.
func TestEachNoteMemberIsPresentExactlyWhenTheTraceHasOne(t *testing.T) {
	onlyMismatch := evaluated()
	onlyMismatch.Trace = []result.TraceEntry{{Stage: "rule", ID: "r", Condition: "false",
		TypeMismatches: []result.TypeMismatch{{Path: "/a", Operator: "equals", FactType: "number", OperandTypes: []string{"boolean"}}}}}
	onlyCause := evaluated()
	onlyCause.Trace = []result.TraceEntry{{Stage: "rule", ID: "r", Condition: "unknown", OnUnknown: "ignore",
		UnknownCauses: []result.UnknownCause{{Path: text("/a"), Cause: "absent"}}}}
	for _, tc := range []struct {
		name                 string
		evaluation           result.Evaluation
		causes, mismatches   bool
		causeKey, matchedKey string
	}{
		{"a mismatch alone", onlyMismatch, false, true, "", `"typeMismatches":[{"path":"/a","operator":"equals","factType":"number","operandTypes":["boolean"]}]`},
		{"a cause alone", onlyCause, true, false, `"unknownCauses":[{"path":"/a","cause":"absent"}]`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record, err := EvaluationRecord(tc.evaluation, Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil)
			if err != nil {
				t.Fatal(err)
			}
			line, _ := json.Marshal(record)
			if bytes.Contains(line, []byte(`"unknownCauses"`)) != tc.causes || bytes.Contains(line, []byte(`"typeMismatches"`)) != tc.mismatches {
				t.Fatalf("members present: %s", line)
			}
			for _, want := range []string{tc.causeKey, tc.matchedKey} {
				if want != "" && !bytes.Contains(line, []byte(want)) {
					t.Fatalf("want %s in %s", want, line)
				}
			}
		})
	}
}

// recordBefore is Record exactly as it was before ADR-0046: the same members,
// the same tags, the same order. A record without notes must encode to the
// same bytes through it.
type recordBefore struct {
	RecordVersion        string                 `json:"recordVersion"`
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
	Disposition          json.RawMessage        `json:"disposition"`
}

func encodeLine(t *testing.T, value any) []byte {
	t.Helper()
	var line bytes.Buffer
	encoder := json.NewEncoder(&line)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		t.Fatal(err)
	}
	return line.Bytes()
}

// A record whose trace noted nothing -- an evaluation, with an empty trace or
// a trace of entries that crossed no type and left nothing unknown, and a
// graph composite, which carries no node's inputs -- is byte for byte the line
// it was before the members existed: the line the writer appends equals the
// encoding of the same values through the record's former shape.
func TestARecordWithoutNotesIsByteForByteWhatItWas(t *testing.T) {
	plain := evaluated()
	plain.Trace = []result.TraceEntry{
		{Stage: "applicability", Condition: "true"},
		{Stage: "rule", ID: "r", Condition: "true", Outcome: "auto-approve"},
		{Stage: "rule", ID: "s", Condition: "not-evaluated", Suppressed: true},
	}
	reviewed := true
	evaluation, err := EvaluationRecord(plain, Inputs{Facts: []byte(`{"a":"<b>"}`), Evidence: []byte(`{"e":"present"}`), EvidenceSupplied: true},
		[]Citation{{SessionID: "s", CallIndex: 1, Signature: strings.Repeat("a", 128)}}, []byte(`{}`), &Graph{ID: "g", Version: "1", Node: "n"})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := EvaluationRecord(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	composite, err := CompositeRecord(result.GraphEvaluation{
		Command: "experimental graph evaluate", EvaluatorSpecVersion: result.EvaluatorSpecVersion,
		GraphID: "g", GraphVersion: "1", FormatVersion: "1", ResultNode: "n", Disposition: evaluated().Disposition,
		Nodes: []result.GraphNodeEvaluation{{Node: "n", Trace: notedEvaluation().Trace}},
	}, "sha256:"+strings.Repeat("b", 64), nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, record := range map[string]Record{"an evaluation": evaluation, "an empty trace": empty, "a composite": composite} {
		t.Run(name, func(t *testing.T) {
			writer, root := writerAt(t, "audit")
			// The chain's three members are the chain tests' to hold to the
			// former bytes (chain_test.go); this one holds the notes.
			writer.chain = false
			writer.UnderLaw(&reviewed, &ReviewedSet{LockDigest: "sha256:" + strings.Repeat("c", 64), LockVersion: "1", ConfigDigest: "sha256:" + strings.Repeat("d", 64)})
			if err := writer.Append(record); err != nil {
				t.Fatal(err)
			}
			written, err := os.ReadFile(filepath.Join(root, "audit", FileName))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(written, []byte(`"unknownCauses"`)) || bytes.Contains(written, []byte(`"typeMismatches"`)) {
				t.Fatalf("a record with no notes carries neither member: %s", written)
			}
			var stamped Record
			if err := json.Unmarshal(written, &stamped); err != nil {
				t.Fatal(err)
			}
			before := encodeLine(t, recordBefore{
				RecordVersion: stamped.RecordVersion, Run: stamped.Run, At: stamped.At, Kind: stamped.Kind, Surface: stamped.Surface,
				Tool: stamped.Tool, EvaluatorSpecVersion: stamped.EvaluatorSpecVersion, Pack: stamped.Pack, Graph: stamped.Graph,
				Inputs: stamped.Inputs, DraftPrototype: stamped.DraftPrototype, Artifact: stamped.Artifact, Reviewed: stamped.Reviewed,
				ReviewedSet: stamped.ReviewedSet, Cites: stamped.Cites, Disposition: stamped.Disposition,
			})
			if !bytes.Equal(written, before) {
				t.Fatalf("the line changed:\n%s\n%s", written, before)
			}
		})
	}
}
