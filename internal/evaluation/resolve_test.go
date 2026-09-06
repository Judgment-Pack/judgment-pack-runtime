package evaluation

import (
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// TestTwoRulesAgreeingOnOneOutcomeAreNotAConflict holds §8's distinctness
// clause at the resolver: outcomes that agree are one candidate, and only
// distinct ones are a conflict. Two rules naming the same outcome are the
// ordinary way an author writes two independent reasons for the same
// destination, and the runtime already ships a pack shaped that way.

func TestTwoRulesAgreeingOnOneOutcomeAreNotAConflict(t *testing.T) {
	pack := map[string]any{
		"applicability": literalCondition(true),
		"outcomes": []any{
			map[string]any{"id": "a"},
			map[string]any{"id": "b"},
		},
		"rules": []any{
			map[string]any{"id": "r1", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
			map[string]any{"id": "r2", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
		},
	}

	disposition, _, _, failure := resolve(pack, map[string]any{}, coreEvaluator())
	if failure != nil {
		t.Fatalf("resolution failed: %+v", failure)
	}
	if disposition.Kind != "outcome" {
		t.Fatalf("two rules agreeing on one outcome produce that outcome, not a conflict: %+v", disposition)
	}
	if disposition.OutcomeID != "a" {
		t.Fatalf("the outcome must be a: %+v", disposition)
	}
	if len(disposition.Reasons) > 0 {
		t.Fatalf("an outcome result retains no reasons: %+v", disposition)
	}
}

// TestOneRuleTrueAndOneRuleFalseProducesTheOneTrueOutcome verifies the
// complementary case: one true rule and one false rule with the same outcome
// still produce that outcome, not a conflict.

func TestOneRuleTrueAndOneRuleFalseProducesTheOneTrueOutcome(t *testing.T) {
	pack := map[string]any{
		"applicability": literalCondition(true),
		"outcomes": []any{map[string]any{"id": "a"}},
		"rules": []any{
			map[string]any{"id": "r1", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
			map[string]any{"id": "r2", "when": literalCondition(false), "outcome": "a", "onUnknown": "ignore"},
		},
	}

	disposition, _, _, failure := resolve(pack, map[string]any{}, coreEvaluator())
	if failure != nil {
		t.Fatalf("resolution failed: %+v", failure)
	}
	if disposition.Kind != "outcome" || disposition.OutcomeID != "a" {
		t.Fatalf("one true and one false rule with the same outcome produce that outcome: %+v", disposition)
	}
}

// TestTwoRulesWithDistinctOutcomesIsAConflict verifies the complementary
// case: two true rules with different outcomes are a conflict, not an
// outcome.

func TestTwoRulesWithDistinctOutcomesIsAConflict(t *testing.T) {
	pack := map[string]any{
		"applicability": literalCondition(true),
		"outcomes": []any{
			map[string]any{"id": "a"},
			map[string]any{"id": "b"},
		},
		"rules": []any{
			map[string]any{"id": "r1", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
			map[string]any{"id": "r2", "when": literalCondition(true), "outcome": "b", "onUnknown": "ignore"},
		},
	}

	disposition, _, _, failure := resolve(pack, map[string]any{}, coreEvaluator())
	if failure != nil {
		t.Fatalf("resolution failed: %+v", failure)
	}
	if disposition.Kind != "unresolved" {
		t.Fatalf("two true rules with distinct outcomes are a conflict: %+v", disposition)
	}
	if len(disposition.Reasons) != 1 || disposition.Reasons[0] != ReasonConflict {
		t.Fatalf("the conflict reason must be retained: %+v", disposition)
	}
}

// TestThreeRulesAgreeingOnOneOutcomeProducesOneOutcome verifies that the
// agreement clause generalises: three or more true rules naming the same
// outcome are still one candidate, not a conflict.

func TestThreeRulesAgreeingOnOneOutcomeProducesOneOutcome(t *testing.T) {
	pack := map[string]any{
		"applicability": literalCondition(true),
		"outcomes": []any{map[string]any{"id": "a"}},
		"rules": []any{
			map[string]any{"id": "r1", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
			map[string]any{"id": "r2", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
			map[string]any{"id": "r3", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
		},
	}

	disposition, _, _, failure := resolve(pack, map[string]any{}, coreEvaluator())
	if failure != nil {
		t.Fatalf("resolution failed: %+v", failure)
	}
	if disposition.Kind != "outcome" || disposition.OutcomeID != "a" {
		t.Fatalf("three true rules agreeing on one outcome produce that outcome: %+v", disposition)
	}
}

// TestAgreeingOutcomesPreserveTraceEntries verifies that each agreeing rule
// still gets its own trace entry — the trace records what happened, not just
// the final disposition.

func TestAgreeingOutcomesPreserveTraceEntries(t *testing.T) {
	pack := map[string]any{
		"applicability": literalCondition(true),
		"outcomes": []any{map[string]any{"id": "a"}},
		"rules": []any{
			map[string]any{"id": "r1", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
			map[string]any{"id": "r2", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
		},
	}

	disposition, _, trace, failure := resolve(pack, map[string]any{}, coreEvaluator())
	if failure != nil {
		t.Fatalf("resolution failed: %+v", failure)
	}
	if disposition.Kind != "outcome" {
		t.Fatalf("agreement should produce an outcome: %+v", disposition)
	}

	// Both rules should appear in the trace with outcome "a"
	ruleEntries := 0
	for _, entry := range trace {
		if entry.Stage == "rule" {
			ruleEntries++
			if entry.Outcome != "a" {
				t.Fatalf("each agreeing rule should record its outcome in the trace: %+v", entry)
			}
		}
	}
	if ruleEntries != 2 {
		t.Fatalf("both agreeing rules must appear in the trace, got %d entries", ruleEntries)
	}
}

// TestDistinctnessClauseIsHeldUnderForcedOutcomes verifies that the
// distinctness clause still applies when forced outcomes are present: a
// forced outcome that agrees with a rule's outcome does not create a
// conflict.

func TestDistinctnessClauseIsHeldUnderForcedOutcomes(t *testing.T) {
	pack := map[string]any{
		"applicability": literalCondition(true),
		"outcomes": []any{map[string]any{"id": "a"}},
		"exceptions": []any{
			map[string]any{"id": "x1", "when": literalCondition(true), "effect": "force-outcome", "outcome": "a", "onUnknown": "ignore"},
		},
		"rules": []any{
			map[string]any{"id": "r1", "when": literalCondition(true), "outcome": "a", "onUnknown": "ignore"},
		},
	}

	disposition, _, _, failure := resolve(pack, map[string]any{}, coreEvaluator())
	if failure != nil {
		t.Fatalf("resolution failed: %+v", failure)
	}
	if disposition.Kind != "outcome" || disposition.OutcomeID != "a" {
		t.Fatalf("a forced outcome and a rule agreeing on the same outcome produce that outcome: %+v", disposition)
	}
}

var _ = result.Disposition{} // prevent unused import in builds
