package evaluation

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The rows below pin ADR-0026's trace contract byte for byte, at the resolver,
// against the exact serialized record: entry order is walk order (applicability
// when authored, then every exception in document order, then every rule in
// document order), each entry's member set is the smallest one that states what
// happened, and two identical resolutions serialize identically. A golden
// string rather than per-member assertions is the point — an accidental
// reordering, a renamed member, or a member emitted empty changes the bytes,
// and the bytes are what a caller retains and what a replay of an audit
// record's stored inputs must reproduce. Fixture ids are deliberately in
// non-lexical document order throughout, so a sort by id cannot masquerade as
// document order.

// traceJSON serializes a trace exactly as the evaluation payload does.
func traceJSON(t *testing.T, trace []result.TraceEntry) string {
	t.Helper()
	encoded, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("marshaling the trace: %v", err)
	}
	return string(encoded)
}

// unknownCondition is one authored condition that evaluates to unknown: a fact
// pointer that resolves against nothing. §7.4 makes an unresolved pointer
// unknown, which is the ordinary way a real pack goes unknown — not a malformed
// node degrading, but an authored question the facts document cannot answer.
func unknownCondition() map[string]any {
	return map[string]any{"op": "fact", "path": "/missing", "operator": "equals", "value": "x"}
}

func literalCondition(value bool) map[string]any {
	return map[string]any{"op": "literal", "value": value}
}

// One walk through every stage: an authored applicability, an unknown-ignored
// exception beside a false one, and a false, a true, and an unknown-ignored
// rule. The unknown exception's suppression never happens — only a true
// exception suppresses — so its target rule is evaluated like any other, while
// the unknown itself stays visible with the onUnknown that let resolution
// ignore it (§8: ignore never erases an unknown from a trace). Two resolutions
// of the same inputs produce identical bytes: the trace is a pure function of
// the admitted documents, tri-state evidence, and the effective options under
// this evaluator release.
func TestTraceIsADeterministicOrderedRecord(t *testing.T) {
	pack := map[string]any{
		"applicability": literalCondition(true),
		"outcomes":      []any{map[string]any{"id": "a"}},
		"exceptions": []any{
			map[string]any{"id": "x2", "when": unknownCondition(), "effect": "suppress-rule", "targetRule": "r1", "onUnknown": "ignore"},
			map[string]any{"id": "x1", "when": literalCondition(false), "effect": "escalate", "onUnknown": "ignore"},
		},
		"rules": []any{
			map[string]any{"id": "r3", "when": literalCondition(false), "outcome": "a", "onUnknown": "ignore"},
			map[string]any{"id": "r1", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
			map[string]any{"id": "r2", "when": unknownCondition(), "outcome": "a", "onUnknown": "ignore"},
		},
	}
	want := `[{"stage":"applicability","condition":"true"},` +
		`{"stage":"exception","id":"x2","condition":"unknown","onUnknown":"ignore"},` +
		`{"stage":"exception","id":"x1","condition":"false"},` +
		`{"stage":"rule","id":"r3","condition":"false"},` +
		`{"stage":"rule","id":"r1","condition":"true","outcome":"a"},` +
		`{"stage":"rule","id":"r2","condition":"unknown","onUnknown":"ignore"}]`

	disposition, _, first, failure := resolve(pack, map[string]any{}, coreEvaluator())
	if failure != nil {
		t.Fatalf("resolution failed: %+v", failure)
	}
	if disposition.Kind != "outcome" || disposition.OutcomeID != "a" {
		t.Fatalf("one true rule and no blocking reason produce its outcome: %+v", disposition)
	}
	if got := traceJSON(t, first); got != want {
		t.Fatalf("trace = %s\nwant   %s", got, want)
	}
	_, _, second, _ := resolve(pack, map[string]any{}, coreEvaluator())
	if traceJSON(t, second) != traceJSON(t, first) {
		t.Fatalf("two resolutions of the same inputs must serialize identically:\n%s\n%s",
			traceJSON(t, first), traceJSON(t, second))
	}
}

// A forced outcome produces without evaluating normal rules (§8 step 6), and
// every authored rule still appears — not-evaluated and skipped, one entry
// each. This is this evaluator's answer to the Core's open question on trace
// minimums (whether a true rule a forced outcome skipped must be surfaced):
// surfaced, unevaluated, and labeled with why. No applicability was authored,
// so the record opens at the exception that decided the walk.
func TestForcedOutcomeSkipsEveryRuleVisibly(t *testing.T) {
	pack := map[string]any{
		"outcomes": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}},
		"exceptions": []any{
			map[string]any{"id": "force", "when": literalCondition(true), "effect": "force-outcome", "outcome": "b", "onUnknown": "ignore"},
		},
		"rules": []any{
			map[string]any{"id": "r2", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
			map[string]any{"id": "r1", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
		},
	}
	want := `[{"stage":"exception","id":"force","condition":"true","effect":"force-outcome","outcome":"b"},` +
		`{"stage":"rule","id":"r2","condition":"not-evaluated","skipped":true},` +
		`{"stage":"rule","id":"r1","condition":"not-evaluated","skipped":true}]`

	disposition, _, trace, failure := resolve(pack, map[string]any{}, coreEvaluator())
	if failure != nil {
		t.Fatalf("resolution failed: %+v", failure)
	}
	if disposition.Kind != "outcome" || disposition.OutcomeID != "b" {
		t.Fatalf("one compatible forced outcome produces it: %+v", disposition)
	}
	if got := traceJSON(t, trace); got != want {
		t.Fatalf("trace = %s\nwant   %s", got, want)
	}
}

// Skipping takes precedence over suppression. A true suppression and a
// compatible forced outcome can coexist (§8 step 4 calls them compatible), and
// then step 6 ends the walk before suppression filtering runs: every rule —
// the suppression's target included — is skipped, because suppressed labels
// belong to the evaluated walk that never happened. This is the overlap
// ADR-0026 clause 4 states, pinned so neither label ever leaks into the
// other's territory.
func TestForcedOutcomeOverridesSuppressionLabels(t *testing.T) {
	pack := map[string]any{
		"outcomes": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}},
		"exceptions": []any{
			map[string]any{"id": "shield", "when": literalCondition(true), "effect": "suppress-rule", "targetRule": "r1", "onUnknown": "ignore"},
			map[string]any{"id": "force", "when": literalCondition(true), "effect": "force-outcome", "outcome": "b", "onUnknown": "ignore"},
		},
		"rules": []any{
			map[string]any{"id": "r1", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
		},
	}
	want := `[{"stage":"exception","id":"shield","condition":"true","effect":"suppress-rule"},` +
		`{"stage":"exception","id":"force","condition":"true","effect":"force-outcome","outcome":"b"},` +
		`{"stage":"rule","id":"r1","condition":"not-evaluated","skipped":true}]`

	disposition, _, trace, failure := resolve(pack, map[string]any{}, coreEvaluator())
	if failure != nil {
		t.Fatalf("resolution failed: %+v", failure)
	}
	if disposition.Kind != "outcome" || disposition.OutcomeID != "b" {
		t.Fatalf("suppression is compatible with a forced outcome, which produces: %+v", disposition)
	}
	if got := traceJSON(t, trace); got != want {
		t.Fatalf("trace = %s\nwant   %s", got, want)
	}
}

// A suppressed rule is the other not-evaluated shape, and the two never blur:
// suppressed says a true exception removed this rule from the walk, skipped
// says a forced outcome ended the walk before it. The unsuppressed rule beside
// it is evaluated normally, so one record carries both shapes side by side.
func TestSuppressedAndEvaluatedRulesShareOneRecord(t *testing.T) {
	pack := map[string]any{
		"outcomes": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}},
		"exceptions": []any{
			map[string]any{"id": "x", "when": literalCondition(true), "effect": "suppress-rule", "targetRule": "r2", "onUnknown": "ignore"},
		},
		"rules": []any{
			map[string]any{"id": "r2", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
			map[string]any{"id": "r1", "when": literalCondition(false), "outcome": "a", "onUnknown": "ignore"},
		},
		"fallbackOutcome": "b",
	}
	want := `[{"stage":"exception","id":"x","condition":"true","effect":"suppress-rule"},` +
		`{"stage":"rule","id":"r2","condition":"not-evaluated","suppressed":true},` +
		`{"stage":"rule","id":"r1","condition":"false"}]`

	disposition, _, trace, failure := resolve(pack, map[string]any{}, coreEvaluator())
	if failure != nil {
		t.Fatalf("resolution failed: %+v", failure)
	}
	if disposition.Kind != "outcome" || disposition.OutcomeID != "b" {
		t.Fatalf("with the only true rule suppressed the fallback applies: %+v", disposition)
	}
	if got := traceJSON(t, trace); got != want {
		t.Fatalf("trace = %s\nwant   %s", got, want)
	}
}

// A blocking exception never hides the ones after it: §8 step 5 returns only
// after every exception was inspected, so the record carries the complete
// exception stage even when its first entry already decided the walk. The
// rules below it are never reached and never traced — the "possibly partial"
// of a successful early return, complete for the path §8 reached.
func TestBlockedWalkStillRecordsEveryException(t *testing.T) {
	pack := map[string]any{
		"outcomes": []any{map[string]any{"id": "a"}},
		"exceptions": []any{
			map[string]any{"id": "x2", "when": unknownCondition(), "effect": "escalate", "onUnknown": "escalate"},
			map[string]any{"id": "x1", "when": literalCondition(false), "effect": "escalate", "onUnknown": "ignore"},
		},
		"rules": []any{
			map[string]any{"id": "r1", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
		},
	}
	want := `[{"stage":"exception","id":"x2","condition":"unknown","onUnknown":"escalate"},` +
		`{"stage":"exception","id":"x1","condition":"false"}]`

	disposition, _, trace, failure := resolve(pack, map[string]any{}, coreEvaluator())
	if failure != nil {
		t.Fatalf("resolution failed: %+v", failure)
	}
	if disposition.Kind != "unresolved" || !reflect.DeepEqual(disposition.Reasons, []string{"unknown"}) {
		t.Fatalf("an unknown escalating exception blocks resolution: %+v", disposition)
	}
	if got := traceJSON(t, trace); got != want {
		t.Fatalf("trace = %s\nwant   %s", got, want)
	}
}

// Evidence inspection leaves no trace entry in any of its three states. §8
// step 2 inspects tri-state presence without evaluating any condition, so
// there is no evaluation to record, and inventing an entry would trace
// something that never ran; its findings surface as disposition reasons
// instead. The one authored stage before the walk stopped is the whole record,
// and the reason beside it proves the inspection happened — false wins the
// reason over unknown, exactly as step 2 orders them.
func TestEvidenceInspectionIsNeverTraced(t *testing.T) {
	pack := map[string]any{
		"applicability": literalCondition(true),
		"outcomes":      []any{map[string]any{"id": "a"}},
		"evidenceRequirements": []any{
			map[string]any{"id": "e-yes", "required": true},
			map[string]any{"id": "e-no", "required": true},
			map[string]any{"id": "e-maybe", "required": true},
		},
		"rules": []any{
			map[string]any{"id": "r1", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
		},
	}
	evaluator := &evaluator{
		evidence: map[string]tri{"e-yes": triTrue, "e-no": triFalse, "e-maybe": triUnknown},
		budget:   DefaultCoreWorkLimit,
	}
	disposition, _, trace, failure := resolve(pack, map[string]any{}, evaluator)
	if failure != nil {
		t.Fatalf("resolution failed: %+v", failure)
	}
	if disposition.Kind != "unresolved" || !reflect.DeepEqual(disposition.Reasons, []string{"missing-required-evidence"}) {
		t.Fatalf("a false required presence blocks with its own reason, unknown beside it notwithstanding: %+v", disposition)
	}
	if got, want := traceJSON(t, trace), `[{"stage":"applicability","condition":"true"}]`; got != want {
		t.Fatalf("inspection records nothing and the blocked walk evaluates no rule: trace = %s, want %s", got, want)
	}
}

// The envelope-level floor: a successful evaluation whose walk recorded
// nothing still carries "trace":[] — present, empty, never null and never
// omitted. The pack below authors no applicability and no exceptions, and its
// unknown required evidence blocks the walk before any rule, so the record is
// empty while the payload is a normal unresolved evaluation. This is the
// serialization half of ADR-0026 clause 1, which no resolver-level golden can
// see.
func TestSuccessfulEnvelopeCarriesTheEmptyTraceFloor(t *testing.T) {
	engine := newTestEngine(t)
	pack := []byte(`{
  "specVersion": "0.2.0-draft",
  "id": "https://example.invalid/judgment-packs/empty-trace-floor",
  "version": "0.1.0",
  "title": "Synthetic empty-trace-floor row",
  "description": "Invented content for specification testing; it authorizes nothing.",
  "decision": {
    "intent": "Exercise the envelope's empty-trace serialization floor.",
    "question": "Was any stage recorded?"
  },
  "outcomes": [
    {"id": "held", "label": "Held"},
    {"id": "did-not-hold", "label": "Did not hold"}
  ],
  "evidenceRequirements": [
    {"id": "the-requirement", "description": "Required and never supplied.", "required": true}
  ],
  "rules": [
    {
      "id": "the-rule",
      "description": "Never reached: the unknown requirement blocks the walk first.",
      "when": {"op": "literal", "value": true},
      "outcome": "held",
      "onUnknown": "ignore"
    }
  ],
  "fallbackOutcome": "did-not-hold"
}`)

	output, failure := engine.Evaluate(pack, []byte(`{}`), nil, nil, "test")
	if failure != nil {
		t.Fatalf("evaluation failed: %s: %s", failure.Code, failure.Message)
	}
	if output.Disposition.Kind != "unresolved" || !reflect.DeepEqual(output.Disposition.Reasons, []string{"unknown"}) {
		t.Fatalf("unknown required evidence blocks resolution: %+v", output.Disposition)
	}
	envelope, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshaling the envelope: %v", err)
	}
	if !strings.Contains(string(envelope), `"trace":[]`) {
		t.Fatalf(`a successful payload carries "trace":[] even when the walk recorded nothing: %s`, envelope)
	}
}

// A refused evaluation reports no trace at all. §8.4 reports an error and no
// partial disposition, and the payload holds the two to the same standard: the
// engine returns the zero evaluation beside the failure, so whatever prefix of
// a record the interrupted walk had built never escapes. The false exception
// below exists to make that prefix non-empty — it is evaluated and traced
// before the expensive rule interrupts the walk — and the asserted class and
// phase pin the refusal to §10's limit reached during evaluation, so a
// preflight refusal cannot satisfy this row with an empty prefix.
func TestRefusedEvaluationReportsNoTrace(t *testing.T) {
	engine := newTestEngine(t)
	facts := membersFacts(200_000)
	const collectionUnits = 200_001
	outside := DefaultCoreWorkLimit/collectionUnits + 1

	pack := []byte(fmt.Sprintf(`{
  "specVersion": "0.2.0-draft",
  "id": "https://example.invalid/judgment-packs/refused-trace",
  "version": "0.1.0",
  "title": "Synthetic refused-evaluation trace row",
  "description": "Invented content for specification testing; it authorizes nothing.",
  "decision": {
    "intent": "Exercise the no-leaked-record rule of a refused evaluation.",
    "question": "Was the evaluation affordable?"
  },
  "outcomes": [
    {"id": "held", "label": "Held"},
    {"id": "did-not-hold", "label": "Did not hold"}
  ],
  "exceptions": [
    {
      "id": "noted",
      "description": "Evaluated and false, so one trace entry exists before the walk is interrupted.",
      "when": {"op": "literal", "value": false},
      "effect": "suppress-rule",
      "targetRule": "the-rule",
      "onUnknown": "ignore"
    }
  ],
  "rules": [
    {
      "id": "the-rule",
      "description": "The one rule expensive enough to exhaust the limit.",
      "when": %s,
      "outcome": "held",
      "onUnknown": "ignore"
    }
  ],
  "fallbackOutcome": "did-not-hold"
}`, inCondition(outside)))

	output, failure := engine.Evaluate(pack, facts, nil, nil, "test")
	if failure == nil {
		t.Fatalf("the documented limit must refuse this evaluation, got %+v", output.Disposition)
	}
	if failure.Class != result.ClassResourceExhaustion || failure.Phase != result.PhaseEvaluation {
		t.Fatalf("the refusal is §10's limit reached during evaluation, got class %q phase %q",
			failure.Class, failure.Phase)
	}
	if !reflect.DeepEqual(output, result.Evaluation{}) {
		t.Fatalf("a refused evaluation reports the zero payload, not a partial one: %+v", output)
	}
}
