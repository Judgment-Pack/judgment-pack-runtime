package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func text(value string) *string { return &value }

// The human output says in words what ADR-0040's members record: one bracket
// per cause and per cross-type comparison, after the rest of the trace line,
// and one line naming the evidence step 2 found wanting. A cause inside a
// draft aggregate names its collection as a scope, not as every element, and
// the root pointer "" is named rather than printed as nothing.
func TestTheHumanTraceSaysWhatLeftAnEntryUnknown(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry result.TraceEntry
		want  string
	}{
		{"absent fact", result.TraceEntry{Stage: "rule", ID: "r", Condition: "unknown", OnUnknown: "escalate",
			UnknownCauses: []result.UnknownCause{{Path: text("/expense/amount"), Cause: "absent"}}},
			"trace: rule r: unknown onUnknown=escalate [fact /expense/amount absent]"},
		{"value not comparable", result.TraceEntry{Stage: "rule", ID: "r", Condition: "unknown", OnUnknown: "ignore",
			UnknownCauses: []result.UnknownCause{{Path: text("/refund/amount"), Cause: "not-comparable", FactType: "number"}}},
			"trace: rule r: unknown onUnknown=ignore [fact /refund/amount is number, not a decimal string]"},
		{"evidence unknown", result.TraceEntry{Stage: "exception", ID: "x", Condition: "unknown", OnUnknown: "ignore",
			UnknownCauses: []result.UnknownCause{{EvidenceRequirement: "receipt", Cause: "unknown"}}},
			"trace: exception x: unknown onUnknown=ignore [evidence receipt unknown]"},
		{"inside a collection", result.TraceEntry{Stage: "applicability", Condition: "unknown",
			UnknownCauses: []result.UnknownCause{{Path: text("/sku"), Within: text("/lines"), Cause: "absent"}, {Path: text("/other"), Cause: "not-an-array", FactType: "string"}}},
			"trace: applicability: unknown [fact /sku within collection /lines absent] [fact /other is string, not an array]"},
		{"the root pointer", result.TraceEntry{Stage: "rule", ID: "r", Condition: "unknown", OnUnknown: "ignore",
			UnknownCauses: []result.UnknownCause{{Path: text(""), Cause: "not-comparable", FactType: "number"}, {Path: text("/sku"), Within: text(""), Cause: "absent"}}},
			`trace: rule r: unknown onUnknown=ignore [fact "" (the root) is number, not a decimal string] [fact /sku within collection "" (the root) absent]`},
		{"a node that names nothing", result.TraceEntry{Stage: "rule", ID: "r", Condition: "unknown", OnUnknown: "ignore",
			UnknownCauses: []result.UnknownCause{{Cause: "unsupported"}}},
			"trace: rule r: unknown onUnknown=ignore [unsupported]"},
		{"cross-type equality", result.TraceEntry{Stage: "exception", ID: "x", Condition: "false",
			TypeMismatches: []result.TypeMismatch{{Path: "/expense/flag", Operator: "equals", FactType: "string", OperandTypes: []string{"boolean"}}}},
			"trace: exception x: false [equals fact /expense/flag: fact is string, operand is boolean]"},
		{"cross-type in within a collection", result.TraceEntry{Stage: "rule", ID: "r", Condition: "false",
			TypeMismatches: []result.TypeMismatch{{Path: "/sku", Within: text("/lines"), Operator: "in", FactType: "number", OperandTypes: []string{"string", "boolean"}}}},
			"trace: rule r: false [in fact /sku within collection /lines: fact is number, operand is string or boolean]"},
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

// The evaluation command prints the unmet-evidence line between the handoff and
// the trace, from a real evaluation rather than a hand-built payload.
func TestTheEvaluateCommandPrintsUnmetEvidence(t *testing.T) {
	t.Setenv("JPACK_CONFIG", filepath.Join(t.TempDir(), "none.json"))
	pack := writeDocument(t, "pack.json", `{
	  "specVersion": "0.2.0-draft", "id": "https://example.invalid/p", "version": "0.1.0", "title": "P",
	  "decision": {"intent": "i", "question": "q"},
	  "outcomes": [{"id": "a", "label": "A"}, {"id": "b", "label": "B"}],
	  "evidenceRequirements": [{"id": "receipt", "description": "r", "required": true}, {"id": "cost-center", "description": "c", "required": true}],
	  "rules": [{"id": "r1", "description": "d", "when": {"op": "literal", "value": true}, "outcome": "a", "onUnknown": "escalate"}]
	}`)
	facts := writeDocument(t, "facts.json", `{}`)
	evidence := writeDocument(t, "evidence.json", `{"receipt":"absent"}`)
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--evidence", evidence, "--rehearsal"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "\nunmet evidence: receipt absent, cost-center unknown\n") {
		t.Fatalf("the human output names the unmet evidence: %s", stdout)
	}
}
