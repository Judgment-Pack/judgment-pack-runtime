package cli

import (
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The human output says in words what ADR-0040's members record: one bracket
// per cause and per cross-type comparison, after the rest of the trace line,
// and one line naming the evidence step 2 found wanting.
func TestTheHumanTraceSaysWhatLeftAnEntryUnknown(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry result.TraceEntry
		want  string
	}{
		{"absent fact", result.TraceEntry{Stage: "rule", ID: "r", Condition: "unknown", OnUnknown: "escalate",
			UnknownCauses: []result.UnknownCause{{Path: "/expense/amount", Cause: "absent"}}},
			"trace: rule r: unknown onUnknown=escalate [fact /expense/amount absent]"},
		{"value not comparable", result.TraceEntry{Stage: "rule", ID: "r", Condition: "unknown", OnUnknown: "ignore",
			UnknownCauses: []result.UnknownCause{{Path: "/refund/amount", Cause: "not-comparable", FactType: "number"}}},
			"trace: rule r: unknown onUnknown=ignore [fact /refund/amount is number, not a decimal string]"},
		{"evidence unknown", result.TraceEntry{Stage: "exception", ID: "x", Condition: "unknown", OnUnknown: "ignore",
			UnknownCauses: []result.UnknownCause{{EvidenceRequirement: "receipt", Cause: "unknown"}}},
			"trace: exception x: unknown onUnknown=ignore [evidence receipt unknown]"},
		{"inside a collection", result.TraceEntry{Stage: "applicability", Condition: "unknown",
			UnknownCauses: []result.UnknownCause{{Path: "/sku", Within: "/lines", Cause: "absent"}, {Path: "/other", Cause: "not-an-array", FactType: "string"}}},
			"trace: applicability: unknown [fact /sku in each of /lines absent] [fact /other is string, not an array]"},
		{"cross-type equality", result.TraceEntry{Stage: "exception", ID: "x", Condition: "false",
			TypeMismatches: []result.TypeMismatch{{Path: "/expense/flag", Operator: "equals", FactType: "string", OperandTypes: []string{"boolean"}}}},
			"trace: exception x: false [equals fact /expense/flag: fact is string, operand is boolean]"},
		{"neither member", result.TraceEntry{Stage: "rule", ID: "r", Condition: "true", Outcome: "a"},
			"trace: rule r: true outcome=a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := traceLine(tc.entry); got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
	if got, want := unmetEvidenceLine([]result.UnmetEvidence{{Requirement: "receipt", State: "absent"}, {Requirement: "cost-center", State: "unknown"}}),
		"unmet evidence: receipt absent, cost-center unknown"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := unmetEvidenceLine(nil); got != "" {
		t.Fatalf("nothing unmet prints nothing: %q", got)
	}
}
