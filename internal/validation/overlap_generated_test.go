package validation

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
)

func overlapBoundaryPack(t *testing.T, a, b any) (map[string]any, *Engine) {
	t.Helper()
	data, err := os.ReadFile("testdata/rules-overlap-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	value, failure := carrier.Decode(data, carrier.DefaultLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	root := value.(map[string]any)
	rules := root["rules"].([]any)
	rules[0].(map[string]any)["when"] = a
	rules[1].(map[string]any)["when"] = b
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	return root, engine
}

func TestRulesOverlapRewriteBoundaryReportsUnavailable(t *testing.T) {
	// Hand-authored review counterexample: both conditions are true, and the
	// admitted second condition is only four containers below the carrier ceiling.
	literal := map[string]any{"op": "literal", "value": true}
	var deep any = literal
	for i := 0; i < 124; i++ {
		deep = map[string]any{"op": "not", "condition": deep}
	}
	root, engine := overlapBoundaryPack(t, literal, deep)
	encoded, _ := json.Marshal(root)
	report, failure := engine.Validate(encoded, Options{})
	if failure != nil || report.Status != "valid" || len(report.Diagnostics) != 1 {
		t.Fatalf("boundary source: %+v %v", report, failure)
	}
	d := report.Diagnostics[0]
	if d.Certainty != "exact" || d.SuggestedRewrite != nil || d.RewriteUnavailable != overlapRewriteUnavailable || !strings.Contains(d.Message, "Exclusive rewrite unavailable:") {
		t.Fatalf("unusable replacement advertised: %+v", d)
	}
	if _, failure := carrier.Decode(d.WitnessFacts, carrier.DefaultLimits()); failure != nil {
		t.Fatal(failure)
	}
	after, _ := json.Marshal(root)
	if string(after) != string(encoded) {
		t.Fatal("rewrite preflight mutated the admitted pack")
	}
}

func TestRulesOverlapRewriteUsesCompletePackAndApplicableLimits(t *testing.T) {
	literal := map[string]any{"op": "literal", "value": true}
	// This small carrier has exactly twelve value nodes and a maximum container
	// depth of three. The generated all/conditions/not wrapper adds eight nodes.
	a := map[string]any{"id": "a", "when": literal}
	b := map[string]any{"id": "b", "when": literal}
	root := map[string]any{"rules": []any{a, b}}
	candidate := exclusiveRewrite(a, b)
	for _, name := range []string{"depth", "nodes", "string", "work"} {
		t.Run(name, func(t *testing.T) {
			limits := carrier.DefaultLimits()
			budget := &overlapBudget{overlapWorkLimit}
			switch name {
			case "depth":
				limits.MaxDepth = 4
			case "nodes":
				limits.MaxNodes = 12
			case "string":
				limits.MaxStringBytes = 7
			case "work":
				budget.remaining = 0
			}
			original, _ := json.Marshal(root)
			if _, failure := carrier.Decode(original, limits); failure != nil {
				t.Fatalf("source is not admitted: %v", failure)
			}
			if got := boundedExclusiveRewrite(root, candidate, limits, budget); got != nil {
				t.Fatalf("replacement bypassed %s bound", name)
			}
		})
	}
	// At sufficient limits the complete generated pack, not just its condition,
	// must be admitted and retain the ordinary pasteable replacement.
	limits := carrier.DefaultLimits()
	got := boundedExclusiveRewrite(root, candidate, limits, &overlapBudget{overlapWorkLimit})
	if got == nil {
		t.Fatal("ordinary replacement unavailable")
	}
	a["when"] = got.When
	encoded, _ := json.Marshal(root)
	if _, failure := carrier.Decode(encoded, limits); failure != nil {
		t.Fatal(failure)
	}
}

func TestRulesOverlapWitnessBoundaryDowngradesWithoutInvalidFacts(t *testing.T) {
	// The pack condition is shallow; the pointer would require 129 fact objects.
	fact := map[string]any{"op": "fact", "path": strings.Repeat("/x", 129), "operator": "equals", "value": "yes"}
	root, engine := overlapBoundaryPack(t, fact, map[string]any{"op": "literal", "value": true})
	encoded, _ := json.Marshal(root)
	report, failure := engine.Validate(encoded, Options{})
	if failure != nil || report.Status != "valid" || len(report.Diagnostics) != 1 {
		t.Fatalf("boundary source: %+v %v", report, failure)
	}
	d := report.Diagnostics[0]
	if d.Certainty != "not-excluded" || len(d.WitnessFacts) != 0 || d.Overlap != nil {
		t.Fatalf("inadmissible exact witness: %+v", d)
	}
	if d.SuggestedRewrite == nil {
		t.Fatal("shallow replacement should still fit")
	}
}

func TestRulesOverlapWitnessPreflightAllocatesNothingBeyondDepth(t *testing.T) {
	path := strings.Repeat("/x", 129)
	allocations := testing.AllocsPerRun(1, func() {
		if facts, ok := boundedOverlapFacts(path, "yes", carrier.DefaultLimits(), &overlapBudget{overlapWorkLimit}); ok || len(facts) > 0 {
			t.Fatal("over-depth pointer generated a witness")
		}
	})
	if allocations != 0 {
		t.Fatalf("pointer allocation preceded depth preflight: %v allocations", allocations)
	}
}

func TestRulesOverlapLongAdmittedPointerDoesNotAllocateFacts(t *testing.T) {
	// The source path itself is at the default string limit. Check a generous
	// work budget as well, so the depth/node guard must precede token allocation.
	path := strings.Repeat("/x", carrier.DefaultMaxStringBytes/2)
	for _, work := range []int{overlapWorkLimit, 2 * overlapWorkLimit} {
		allocations := testing.AllocsPerRun(1, func() {
			if facts, ok := boundedOverlapFacts(path, "yes", carrier.DefaultLimits(), &overlapBudget{work}); ok || len(facts) > 0 {
				t.Fatal("long pointer generated a witness")
			}
		})
		if allocations != 0 {
			t.Fatalf("long pointer allocated facts/tokens: %v allocations", allocations)
		}
	}
	root, engine := overlapBoundaryPack(t, map[string]any{"op": "fact", "path": path, "operator": "equals", "value": "yes"}, map[string]any{"op": "literal", "value": true})
	encoded, _ := json.Marshal(root)
	report, failure := engine.Validate(encoded, Options{})
	if failure != nil || report.Status != "valid" || len(report.Diagnostics) != 1 {
		t.Fatalf("long pointer source: %+v %v", report, failure)
	}
	d := report.Diagnostics[0]
	if d.Certainty != "not-excluded" || len(d.WitnessFacts) != 0 || d.SuggestedRewrite != nil || d.RewriteUnavailable == "" {
		t.Fatalf("generation limits bypassed: %+v", d)
	}
}

func TestRulesOverlapWitnessChecksLeafAndCustomLimits(t *testing.T) {
	limits := carrier.DefaultLimits()
	// Exactly 128 fact objects and one scalar are admissible at the default
	// depth; an object leaf adds another container and cannot fit.
	path := strings.Repeat("/x", 128)
	facts, ok := boundedOverlapFacts(path, "yes", limits, &overlapBudget{overlapWorkLimit})
	if !ok {
		t.Fatal("at-depth scalar witness refused")
	}
	if _, failure := carrier.Decode(facts, limits); failure != nil {
		t.Fatal(failure)
	}
	if _, ok := boundedOverlapFacts(path, map[string]any{}, limits, &overlapBudget{overlapWorkLimit}); ok {
		t.Fatal("container leaf exceeded depth")
	}
	for _, name := range []string{"depth", "nodes", "string", "work"} {
		t.Run(name, func(t *testing.T) {
			limits := carrier.DefaultLimits()
			budget := &overlapBudget{overlapWorkLimit}
			switch name {
			case "depth":
				limits.MaxDepth = 2
			case "nodes":
				limits.MaxNodes = 3
			case "string":
				limits.MaxStringBytes = 2
			case "work":
				budget.remaining = 1
			}
			if _, ok := boundedOverlapFacts("/x/x/x", "yes", limits, budget); ok {
				t.Fatalf("witness bypassed %s limit", name)
			}
		})
	}
}

func TestRulesOverlapGeneratedShapeCountsEscapedBytesAndHardLimit(t *testing.T) {
	// Every encoding/json escape family is included; member names and values
	// share the same string limit and encoded-byte accounting.
	value := map[string]any{"\"\\\x00\b\f\n\r\t<>&\u2028\u2029é": []any{true, false, nil, json.Number("1e9999"), "\"\\\x01<>&\u2028\u2029é"}}
	shape := overlapShape{limits: carrier.DefaultLimits(), budget: &overlapBudget{overlapWorkLimit}}
	if !shape.value(value, 0) {
		t.Fatal("bounded value refused")
	}
	encoded, _ := json.Marshal(value)
	if shape.bytes != int64(len(encoded)) {
		t.Fatalf("encoded byte count %d != %d", shape.bytes, len(encoded))
	}
	shape = overlapShape{limits: carrier.DefaultLimits(), budget: &overlapBudget{overlapWorkLimit}, bytes: carrier.HardMaxBytes - 2}
	if shape.value("x", 0) {
		t.Fatal("generated bytes exceeded the hard carrier limit")
	}
}

func TestRulesOverlapWitnessBoundaryRetainsSupportedExactRegion(t *testing.T) {
	path := strings.Repeat("/x", 129)
	fact := map[string]any{"op": "fact", "path": path, "operator": "equals", "value": "yes"}
	root, engine := overlapBoundaryPack(t, fact, fact)
	encoded, _ := json.Marshal(root)
	report, failure := engine.Validate(encoded, Options{})
	if failure != nil || report.Status != "valid" || len(report.Diagnostics) != 1 {
		t.Fatalf("source: %+v %v", report, failure)
	}
	d := report.Diagnostics[0]
	if d.Certainty != "exact" || len(d.WitnessFacts) != 0 || d.Overlap == nil || d.Overlap.Path != path || len(d.Overlap.SharedValues) != 1 || d.Overlap.SharedValues[0] != "yes" {
		t.Fatalf("supported region lost or invalid witness emitted: %+v", d)
	}
}

func TestRulesOverlapGenerationReceivesValidationCarrierLimits(t *testing.T) {
	root, engine := overlapBoundaryPack(t, map[string]any{"op": "fact", "path": "/x/x/x/x/x", "operator": "equals", "value": "yes"}, map[string]any{"op": "literal", "value": true})
	encoded, _ := json.Marshal(root)
	normal, failure := engine.Validate(encoded, Options{})
	if failure != nil || normal.Status != "valid" || normal.Diagnostics[0].Certainty != "exact" || normal.Diagnostics[0].SuggestedRewrite == nil {
		t.Fatalf("normal advice: %+v %v", normal, failure)
	}
	limits := carrier.DefaultLimits()
	limits.MaxDepth = 4
	limited, failure := engine.Validate(encoded, Options{Limits: limits})
	if failure != nil || limited.Status != "valid" || len(limited.Diagnostics) != 1 {
		t.Fatalf("limited source: %+v %v", limited, failure)
	}
	d := limited.Diagnostics[0]
	if d.Certainty != "not-excluded" || len(d.WitnessFacts) != 0 || d.SuggestedRewrite != nil || d.RewriteUnavailable == "" {
		t.Fatalf("configured limits ignored: %+v", d)
	}
}
