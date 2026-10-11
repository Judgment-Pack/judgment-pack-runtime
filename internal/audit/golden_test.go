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

// goldenTrail is a trail the v0.30.0 writer wrote, before records could carry
// a binding (ADR-0050): the four records goldenRecords composes, chained, each
// invocation its own writer.
const goldenTrail = "trail-v0.30.0.jsonl"

// goldenRecords composes, from fixed inputs, the records goldenTrail holds,
// without what the writer and the clock assign: an evaluation with evidence,
// citations and a reviewed set; one with trace notes and nothing optional;
// and a graph run's node and composite.
func goldenRecords(t *testing.T) []Record {
	t.Helper()
	reviewed := true
	first, err := EvaluationRecord(evaluated(), Inputs{
		Facts:            []byte(`{"expense":{"amountUsd":"12.00","note":"a <b> & c"}}`),
		Evidence:         []byte(`{"itemised-receipt":"present"}`),
		EvidenceSupplied: true,
	}, []Citation{{SessionID: "s-2026-09-12-a", CallIndex: 17, Signature: strings.Repeat("a", 128)}}, []byte(`{"id":"expense-approval"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	first.Reviewed = &reviewed
	first.ReviewedSet = goldenReviewedSet()
	noted := evaluated()
	pointer := "/expense/amountUsd"
	noted.Trace = []result.TraceEntry{{
		Stage:          "rule",
		ID:             "r1",
		Condition:      "unknown",
		UnknownCauses:  []result.UnknownCause{{Path: &pointer, Cause: "absent"}},
		TypeMismatches: []result.TypeMismatch{{Path: "/expense/flag", Operator: "equals", FactType: "string", OperandTypes: []string{"boolean"}}},
	}}
	second, err := EvaluationRecord(noted, Inputs{Facts: []byte(`{"n":1}`)}, nil, []byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	nodeEvaluation := evaluated()
	nodeEvaluation.Command = "experimental graph evaluate"
	graphDigest := "sha256:" + strings.Repeat("d", 64)
	node, err := EvaluationRecord(nodeEvaluation, Inputs{Facts: []byte(`{"n":2}`)}, nil, []byte(`{}`), &Graph{ID: "g", Version: "0.1.0", FormatVersion: "1", Digest: graphDigest, Node: "a"})
	if err != nil {
		t.Fatal(err)
	}
	composite, err := CompositeRecord(result.GraphEvaluation{
		Command:              "experimental graph evaluate",
		EvaluatorSpecVersion: result.EvaluatorSpecVersion,
		GraphID:              "g",
		GraphVersion:         "0.1.0",
		FormatVersion:        "1",
		ResultNode:           "a",
		Disposition:          evaluated().Disposition,
	}, graphDigest, nil)
	if err != nil {
		t.Fatal(err)
	}
	return []Record{first, second, node, composite}
}

func goldenReviewedSet() *ReviewedSet {
	return &ReviewedSet{LockDigest: "sha256:" + strings.Repeat("1", 64), LockVersion: "1", ConfigDigest: "sha256:" + strings.Repeat("2", 64)}
}

// A record written without a binding is byte for byte what the v0.30.0 writer
// wrote (ADR-0050). Each record goldenRecords composes today, given only the
// members the writer and the clock assigned when goldenTrail was written --
// run, at, tool, and the chain -- encodes to that trail's line exactly, and
// the trail verifies with no finding: the binding check is silent on records
// that carry none.
func TestARecordWithoutABindingIsTheBytesTheWriterWroteBefore(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", goldenTrail))
	if err != nil {
		t.Fatal(err)
	}
	lines := splitLines(data)
	records := goldenRecords(t)
	if len(lines) != len(records) {
		t.Fatalf("the fixture holds %d lines for %d records", len(lines), len(records))
	}
	for index, line := range lines {
		if bytes.Contains(line, []byte(`"binding"`)) {
			t.Fatalf("line %d of a trail written before the member existed carries it: %s", index+1, line)
		}
		var assigned Record
		if err := json.Unmarshal(line, &assigned); err != nil {
			t.Fatal(err)
		}
		record := records[index]
		record.Run, record.At, record.Tool = assigned.Run, assigned.At, assigned.Tool
		record.Trail, record.Sequence, record.Previous = assigned.Trail, assigned.Sequence, assigned.Previous
		encoded, err := encodeLines([]Record{record})
		if err != nil {
			t.Fatal(err)
		}
		if want := append(append([]byte{}, line...), '\n'); !bytes.Equal(encoded, want) {
			t.Fatalf("record %d is not the bytes the v0.30.0 writer wrote:\n got %s\nwant %s", index+1, encoded, want)
		}
	}
	chain := verifyBytes(t, data, nil)
	if chain.Status != "valid" || chain.FindingsTotal != 0 || chain.Coverage.Chained != int64(len(records)) {
		t.Fatalf("the fixture trail: %s %v %+v", chain.Status, findingNames(chain), chain.Coverage)
	}
}
