package evaluation

import (
	"encoding/json"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// Core §2.2 admits an optional minus, a zero or nonzero-led integer, and
// an optional fraction with at least one digit. These handwritten boundaries
// exercise both the pinned schema's operand gate and the evaluator's parser.
func TestOrderedDecimalOperandGrammar(t *testing.T) {
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	operators := []struct {
		name string
		// Verdicts for a fact below, equal to, or above the operand.
		want [3]tri
	}{
		{"greater-than", [3]tri{triFalse, triFalse, triTrue}},
		{"greater-than-or-equal", [3]tri{triFalse, triTrue, triTrue}},
		{"less-than", [3]tri{triTrue, triFalse, triFalse}},
		{"less-than-or-equal", [3]tri{triTrue, triTrue, triFalse}},
	}
	cases := []struct {
		name    string
		operand any
		fact    string
		order   int // handwritten mathematical comparison: fact vs operand
		valid   bool
	}{
		{"zero", "0", "1", 1, true},
		{"nonzero-digit", "7", "6", -1, true},
		{"integer-digits-include-zero", "107", "108", 1, true},
		{"optional-minus", "-7", "-8", -1, true},
		{"negative-fraction", "-0.5", "0", 1, true},
		{"negative-zero", "-0", "0", 0, true},
		{"fraction-one-digit", "5.1", "5.2", 1, true},
		{"fraction-leading-zero", "0.01", "0.1", 1, true},
		{"equal-different-scales", "0.10", "0.1", 0, true},
		{"equal-integer-and-fraction", "5.000", "5", 0, true},
		{"fact-has-more-fractional-digits", "0.1", "0.10000000000000000001", 1, true},
		{"operand-has-more-fractional-digits", "0.10000000000000000001", "0.1", -1, true},
		{"leading-plus", "+1", "0", 0, false},
		{"repeated-minus", "--1", "0", 0, false},
		{"minus-without-integer", "-", "0", 0, false},
		{"leading-zeros", "007", "0", 0, false},
		{"zero-before-zero", "00", "0", 0, false},
		{"negative-leading-zero", "-01", "0", 0, false},
		{"fraction-after-leading-zero", "01.5", "0", 0, false},
		{"bare-fraction", ".5", "0", 0, false},
		{"negative-bare-fraction", "-.5", "0", 0, false},
		{"trailing-point", "5.", "0", 0, false},
		{"second-point", "5.1.2", "0", 0, false},
		{"lowercase-exponent", "1e3", "0", 0, false},
		{"uppercase-signed-exponent", "1E+3", "0", 0, false},
		{"leading-space", " 5", "0", 0, false},
		{"trailing-space", "5 ", "0", 0, false},
		{"internal-space", "5 .1", "0", 0, false},
		{"leading-tab", "\t5", "0", 0, false},
		{"trailing-line-feed", "5\n", "0", 0, false},
		{"empty", "", "0", 0, false},
		{"non-ASCII-digit", "١", "0", 0, false},
		{"JSON-number-is-not-a-decimal-string", json.Number("5"), "0", 0, false},
	}
	for _, operator := range operators {
		t.Run(operator.name, func(t *testing.T) {
			for _, row := range cases {
				t.Run(row.name, func(t *testing.T) {
					condition := map[string]any{"op": "fact", "path": "/amount", "operator": operator.name, "value": row.operand}
					encoded, err := json.Marshal(condition)
					if err != nil {
						t.Fatal(err)
					}
					// draftPack contains only Core condition nodes here; no draft opt-in.
					checked, failure := validator.Validate(draftPack(string(encoded), "escalate"), validation.Options{})
					if failure != nil {
						t.Fatal(failure)
					}
					if row.valid {
						if checked.Status != "valid" || len(checked.Diagnostics) != 0 {
							t.Fatalf("valid decimal operand refused: %+v", checked)
						}
						if got := evalCondition(condition, map[string]any{"amount": row.fact}, nil); got != operator.want[row.order+1] {
							t.Fatalf("%q %s %q = %v, want %v", row.fact, operator.name, row.operand, got, operator.want[row.order+1])
						}
						return
					}
					if checked.Status != "invalid" || len(checked.Diagnostics) != 1 || checked.Diagnostics[0].Code != "JPS-STRUCTURE-DECIMAL-OPERAND" || checked.Diagnostics[0].InstancePath != "/rules/0/when/value" {
						t.Fatalf("invalid operand diagnostic = %+v, want JPS-STRUCTURE-DECIMAL-OPERAND at /rules/0/when/value", checked.Diagnostics)
					}
					if got := evalCondition(condition, map[string]any{"amount": row.fact}, nil); got != triUnknown {
						t.Fatalf("invalid operand %v yielded %v, want unknown", row.operand, got)
					}
					// A facts document has no decimal schema: the same grammar must
					// still make malformed facts incomparable during evaluation.
					condition["value"] = "0"
					if got := evalCondition(condition, map[string]any{"amount": row.operand}, nil); got != triUnknown {
						t.Fatalf("invalid fact %v yielded %v, want unknown", row.operand, got)
					}
				})
			}
		})
	}
}
