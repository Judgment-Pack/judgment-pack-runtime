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
