package project

import (
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// strengthDocument is a hand-written document that reaches every door the
// derivation opens: applicability, a not under an all, a repeated member, an
// exception that forces an outcome, two bounds at one pointer authored at
// different precisions, and a referenced and an unreferenced value set. It is
// only derived from here, never admitted, so it carries just the members the
// derivation reads.
const strengthDocument = `{
  "applicability":{"op":"fact","path":"/kind","operator":"equals","value":"expense"},
  "outcomes":[{"id":"a"},{"id":"b"},{"id":"c"}],
  "rules":[{"id":"r","when":{"op":"all","conditions":[
      {"op":"fact","path":"/amount","operator":"less-than","value":"10.5"},
      {"op":"not","condition":{"op":"fact","path":"/tier","operator":"in","valueSet":"tiers"}},
      {"op":"literal","value":true},
      {"op":"literal","value":true}]},
    "outcome":"a","onUnknown":"ignore"}],
  "exceptions":[{"id":"x","when":{"op":"fact","path":"/amount","operator":"greater-than","value":"10.25"},"effect":"force-outcome","outcome":"b","onUnknown":"escalate"},
                {"id":"y","when":{"op":"literal","value":false},"effect":"suppress-rule","onUnknown":"ignore"}],
  "fallbackOutcome":"c",
  "valueSets":[{"id":"tiers","entries":[{"value":"gold"},{"value":"gold","description":"again"},{"value":"silver"}]},
               {"id":"unread","entries":[{"value":"z"}]}]
}`

func decodeStrengthDocument(t *testing.T) map[string]any {
	t.Helper()
	decoded, failure := carrier.Decode([]byte(strengthDocument), carrier.DefaultLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	return decoded.(map[string]any)
}

// Every kind is derived where the document states it, and nowhere else: the
// repeated literal member and the repeated value-set entry are not derived,
// a suppress-rule exception has no outcome to swap, membership has no
// reversal, and an unreferenced set derives nothing. The bound steps at the
// finest precision the pack authors a value in at that pointer.
func TestDeriveVariantsCoversEveryKindAtItsLocation(t *testing.T) {
	variants, over := deriveVariants(decodeStrengthDocument(t), true, DefaultMaxVariants)
	if over {
		t.Fatal("the ceiling was reached")
	}
	var ids []string
	for _, variant := range variants {
		ids = append(ids, variant.report.ID)
	}
	want := []string{
		"applicability:operator-reversed:not-equals",
		"exceptions/x/onUnknown:on-unknown-changed:ignore",
		"exceptions/x/outcome:outcome-swapped:a",
		"exceptions/x/outcome:outcome-swapped:c",
		"exceptions/x/when:bound-moved:10.24",
		"exceptions/x/when:bound-moved:10.26",
		"exceptions/x/when:operator-reversed:greater-than-or-equal",
		"exceptions/x/when:operator-reversed:less-than-or-equal",
		"exceptions/y/onUnknown:on-unknown-changed:escalate",
		"fallbackOutcome:outcome-swapped:a",
		"fallbackOutcome:outcome-swapped:b",
		"rules/r/onUnknown:on-unknown-changed:escalate",
		"rules/r/outcome:outcome-swapped:b",
		"rules/r/outcome:outcome-swapped:c",
		"rules/r/when/conditions/0:bound-moved:10.4",
		"rules/r/when/conditions/0:bound-moved:10.6",
		"rules/r/when/conditions/0:condition-dropped",
		"rules/r/when/conditions/0:operator-reversed:greater-than-or-equal",
		"rules/r/when/conditions/0:operator-reversed:less-than-or-equal",
		"rules/r/when/conditions/1:condition-dropped",
		`valueSets/tiers/entries:value-set-entry-removed:"silver"`,
	}
	if !slices.Equal(ids, want) {
		t.Fatalf("ids:\n got %q\nwant %q", ids, want)
	}
	// Without value sets -- a contract before Core 0.5.0 -- no removal is
	// derived even from a document that carries the member.
	before050, _ := deriveVariants(decodeStrengthDocument(t), false, DefaultMaxVariants)
	for _, variant := range before050 {
		if variant.report.Kind == result.VariantValueSetEntryRemoved {
			t.Fatalf("a value-set removal outside Core 0.5.0: %s", variant.report.ID)
		}
	}
	if len(before050) != len(want)-1 {
		t.Fatalf("%d variants without value sets, want %d", len(before050), len(want)-1)
	}
}

// Building a variant copies the path it changes and nothing the caller
// holds: the authored document reads the same after every variant is built,
// and each variant differs from it.
func TestVariantsNeverChangeTheAuthoredDocument(t *testing.T) {
	document := decodeStrengthDocument(t)
	before := canonicalText(document)
	variants, _ := deriveVariants(document, true, DefaultMaxVariants)
	for _, variant := range variants {
		changed, ok := rewriteAt(document, variant.path, variant.edit)
		if !ok {
			t.Fatalf("%s does not apply", variant.report.ID)
		}
		if canonicalText(changed) == before {
			t.Fatalf("%s changed nothing", variant.report.ID)
		}
	}
	if canonicalText(document) != before {
		t.Fatal("building the variants changed the authored document")
	}
}

// The ceiling stops the derivation one past the limit and reports it, so
// the caller refuses rather than running a part of the variants.
func TestDeriveVariantsStopsPastTheCeiling(t *testing.T) {
	kept, over := deriveVariants(decodeStrengthDocument(t), true, 21)
	if over || len(kept) != 21 {
		t.Fatalf("at the limit: over=%v kept=%d", over, len(kept))
	}
	kept, over = deriveVariants(decodeStrengthDocument(t), true, 20)
	if !over || len(kept) != 20 {
		t.Fatalf("past the limit: over=%v kept=%d", over, len(kept))
	}
	if failure := CheckStrength(&StrengthOptions{MaxVariants: 0}); failure == nil || failure.Code != "JPS-INVOCATION-MAX-VARIANTS" {
		t.Fatalf("a zero ceiling: %v", failure)
	}
}

// A moved bound's hint names the one value at which the two comparisons
// disagree, which is the bound itself on one side and the moved bound on the
// other, for every ordered operator.
func TestBoundHintNamesTheValueThatDisagrees(t *testing.T) {
	for _, tc := range []struct {
		operator, moved, at string
	}{
		{"greater-than-or-equal", "5001", "5000"}, {"greater-than-or-equal", "4999", "4999"},
		{"greater-than", "5001", "5001"}, {"greater-than", "4999", "5000"},
		{"less-than-or-equal", "5001", "5001"}, {"less-than-or-equal", "4999", "5000"},
		{"less-than", "5001", "5000"}, {"less-than", "4999", "4999"},
	} {
		document := map[string]any{"rules": []any{map[string]any{"id": "r", "when": map[string]any{"op": "fact", "path": "/v", "operator": tc.operator, "value": "5000"}}}}
		variants, _ := deriveVariants(document, false, DefaultMaxVariants)
		found := false
		for _, variant := range variants {
			if variant.report.ID == "rules/r/when:bound-moved:"+tc.moved {
				found = true
				if !strings.HasPrefix(variant.report.Hint, "a row that places /v at "+tc.at+" ") {
					t.Fatalf("%s to %s: hint %q, want the value %s", tc.operator, tc.moved, variant.report.Hint, tc.at)
				}
			}
		}
		if !found {
			t.Fatalf("%s: no bound moved to %s", tc.operator, tc.moved)
		}
	}
}
