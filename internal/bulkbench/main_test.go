package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func fixture(t *testing.T) ([]input, evaluate) {
	t.Helper()
	var rows []input
	if err := json.Unmarshal(inputs, &rows); err != nil {
		t.Fatal(err)
	}
	var shape struct {
		Outcomes []json.RawMessage
		Rules    []json.RawMessage
	}
	if err := json.Unmarshal(pack, &shape); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 12 || len(shape.Rules) != 12 || len(shape.Outcomes) != 4 {
		t.Fatal("fixture must exercise twelve rules and four outcomes")
	}
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	engine := evaluation.NewEngine(validator)
	admitted := engine.AdmitPack(pack)
	eval := func(facts []byte) (result.Evaluation, *evaluation.Failure) {
		return engine.EvaluateAdmitted(admitted, facts, nil, evaluation.Options{Command: "bulkbench"})
	}
	// Force the lazy conformance admission before sharing it between workers.
	evaluated, failure := eval(rows[0].Facts)
	if err := check(rows[0], evaluated, failure); err != nil {
		t.Fatal(err)
	}
	return rows, eval
}

func TestHandwrittenFixtureAndParallelRemainder(t *testing.T) {
	rows, eval := fixture(t)
	// Two full cycles plus five rows: this literal oracle is independent of the
	// evaluator and catches a dropped tail or duplicate worker assignment.
	want := tally{Completed: 29, Counts: map[string]int{"proceed": 9, "clarify": 8, "decline": 6, "specialist": 6}, ExpectedCounts: map[string]int{"proceed": 9, "clarify": 8, "decline": 6, "specialist": 6}}
	for _, workers := range []int{1, 4, 17, 29} {
		got, err := run(rows, 29, workers, eval)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("workers=%d: got %+v; want %+v", workers, got, want)
		}
	}
}

func TestSemanticMismatchAndRefusalAbort(t *testing.T) {
	rows, eval := fixture(t)
	for _, workers := range []int{1, 4} {
		wrong := append([]input(nil), rows...)
		wrong[2].Outcome = "decline"
		if _, err := run(wrong, 29, workers, eval); err == nil {
			t.Fatal("semantic mismatch must fail")
		}
		malformed := append([]input(nil), rows...)
		malformed[2].Facts = []byte(`{`)
		if _, err := run(malformed, 29, workers, eval); err == nil {
			t.Fatal("refusal must fail")
		}
	}
}

func TestInvalidBoundsDoNotEvaluate(t *testing.T) {
	eval := func([]byte) (result.Evaluation, *evaluation.Failure) {
		t.Fatal("invalid request evaluated")
		return result.Evaluation{}, nil
	}
	for _, bounds := range [][2]int{{0, 1}, {1, 0}, {1, 2}, {1025, 1025}} {
		if _, err := run([]input{{}}, bounds[0], bounds[1], eval); err == nil {
			t.Fatalf("accepted %v", bounds)
		}
	}
	if _, err := run(nil, 1, 1, eval); err == nil {
		t.Fatal("accepted no inputs")
	}
}

func TestInputIdentityIsStableAndSynthetic(t *testing.T) {
	rows, _ := fixture(t)
	seen := map[string]bool{}
	for _, row := range rows {
		if !bytes.Contains(row.Facts, []byte(`"synthetic":true`)) {
			t.Fatalf("non-synthetic input: %s", row.Facts)
		}
		if seen[string(row.Facts)] {
			t.Fatal("duplicate input leaves a rule unexercised")
		}
		seen[string(row.Facts)] = true
	}
}
