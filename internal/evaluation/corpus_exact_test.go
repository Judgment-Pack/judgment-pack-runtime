package evaluation

import (
	"encoding/json"
	"strings"
	"testing"
)

// The one disposition gate holds member names to their exact §8.3 spellings,
// at the disposition and inside its handoff member: encoding/json case-folds,
// so "Kind" or "State" would otherwise bind and canonicalize as the members
// they are not — including beside the canonical spelling, where the fold
// silently overwrites it.
func TestDecodeDispositionHoldsMemberSpelling(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  string
		want string
	}{
		"a case-folded kind": {
			raw:  `{"Kind":"outcome","outcomeId":"a","reasons":[],"handoff":{"state":"none"}}`,
			want: `spelled "kind"`,
		},
		"a case-folded outcomeId": {
			raw:  `{"kind":"outcome","OutcomeID":"a","reasons":[],"handoff":{"state":"none"}}`,
			want: `spelled "outcomeId"`,
		},
		"a case-folded handoff state": {
			raw:  `{"kind":"outcome","outcomeId":"a","reasons":[],"handoff":{"State":"none"}}`,
			want: `spelled "state"`,
		},
		"an alias beside the canonical spelling": {
			raw:  `{"kind":"outcome","Kind":"unresolved","outcomeId":"a","reasons":[],"handoff":{"state":"none"}}`,
			want: `spelled "kind"`,
		},
		"a wholly unknown member": {
			raw:  `{"kind":"outcome","outcomeId":"a","reasons":[],"handoff":{"state":"none"},"wholly":1}`,
			want: `does not know: "wholly"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeDisposition(json.RawMessage(tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
	// The exactly spelled disposition still decodes.
	exact := `{"kind":"outcome","outcomeId":"a","reasons":[],"handoff":{"state":"none"}}`
	if _, err := DecodeDisposition(json.RawMessage(exact)); err != nil {
		t.Fatalf("the exact spelling decodes: %v", err)
	}
}

// The gate is the gate for every reader, including one whose bytes never went
// through a carrier. Decode reads the first value and stops, and the raw pass
// needs the whole text to parse as one object, so without an explicit refusal
// trailing data would skip every rule this function states — the spellings
// above and §8.3's presence rules alike.
func TestDecodeDispositionRefusesMoreThanOneValue(t *testing.T) {
	valid := `{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none"}}`
	if _, err := DecodeDisposition(json.RawMessage(valid)); err != nil {
		t.Fatalf("the control is a legal disposition: %v", err)
	}
	for name, raw := range map[string]string{
		"an object after the disposition":     valid + " {}",
		"a number after the disposition":      valid + " 0",
		"a defect the trailing value hides":   `{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none","triggeredBy":[]}} {}`,
		"a spelling the trailing value hides": `{"Kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none"}} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeDisposition(json.RawMessage(raw)); err == nil {
				t.Fatal("more than one value is not one disposition")
			}
		})
	}
}

// §8.3's presence rules are about the raw text, which typed decoding cannot
// recover: absent, null and empty are three different things in the positions
// below, and each is refused for what it is. The MCP admission tool reaches
// these through its own fixtures; these hold the shared gate itself, which the
// matrix, graph and coverage readers call directly.
func TestDecodeDispositionHoldsPresenceAndDirectEscalation(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  string
		want string
	}{
		"a missing reasons member": {
			raw:  `{"kind":"outcome","outcomeId":"a","handoff":{"state":"none"}}`,
			want: "reasons must be present and non-null",
		},
		"a null reasons member": {
			raw:  `{"kind":"outcome","outcomeId":"a","reasons":null,"handoff":{"state":"none"}}`,
			want: "reasons must be present and non-null",
		},
		"an empty trigger set beside state none": {
			raw:  `{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none","triggeredBy":[]}}`,
			want: "triggeredBy must be present if and only if state is requested",
		},
		"an outcome id on a kind that admits none": {
			raw:  `{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none"},"outcomeId":""}`,
			want: "outcomeId must be present if and only if kind is outcome",
		},
		// §8.1: the request a retained exception-escalation reason makes does not
		// depend on the pack's escalation object, so neither shape below is
		// producible by any pack, and neither is a legal disposition.
		"a direct escalation with no requested handoff": {
			raw:  `{"kind":"unresolved","reasons":["exception-escalation"],"handoff":{"state":"none"}}`,
			want: "direct request",
		},
		"a direct escalation outside its own triggers": {
			raw:  `{"kind":"unresolved","reasons":["exception-escalation","unknown"],"handoff":{"state":"requested","triggeredBy":["unknown"]}}`,
			want: "direct request",
		},
		// A defect in kind or state is named there, not reported as a defect in
		// the member whose presence that value governs.
		"a case-variant kind beside an outcome id": {
			raw:  `{"kind":"Outcome","outcomeId":"a","reasons":[],"handoff":{"state":"none"}}`,
			want: `kind must be "outcome", "not-applicable", or "unresolved"`,
		},
		"a case-variant state beside a trigger set": {
			raw:  `{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"Requested","triggeredBy":["unknown"]}}`,
			want: `handoff.state must be "requested" or "none"`,
		},
		"a disposition that is not an object at all": {
			raw:  `null`,
			want: "must be a JSON object",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeDisposition(json.RawMessage(tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
	// The producible shapes stay decodable: this gate must not refuse a
	// disposition the engine can emit.
	for name, raw := range map[string]string{
		"a direct escalation request":              `{"kind":"unresolved","reasons":["exception-escalation"],"handoff":{"state":"requested","triggeredBy":["exception-escalation"]}}`,
		"a direct escalation beside a trigger":     `{"kind":"unresolved","reasons":["conflict","exception-escalation"],"handoff":{"state":"requested","triggeredBy":["conflict","exception-escalation"]}}`,
		"a not-applicable result with a handoff":   `{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`,
		"an unresolved result with two reasons":    `{"kind":"unresolved","reasons":["missing-required-evidence","unknown"],"handoff":{"state":"none"}}`,
		"an outcome with an empty retained reason": `{"kind":"outcome","outcomeId":"a","reasons":[],"handoff":{"state":"none"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeDisposition(json.RawMessage(raw)); err != nil {
				t.Fatalf("a producible disposition is decodable: %v", err)
			}
		})
	}
}
