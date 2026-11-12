package mcp

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// A standalone contract (no pack) names Core 0.5.0 as it names 0.3.0, base
// only; 0.4.0-draft and 0.4.0 are refused, and the refusal says where the
// draft went.
func TestStandaloneContractAdmitsCore050(t *testing.T) {
	for _, tc := range []struct {
		version, profile string
		refused          bool
	}{{"0.2.0-draft", "", false}, {"0.3.0", "base", false}, {"0.5.0", "base", false}, {"0.4.0-draft", "", true}, {"0.4.0", "", true}} {
		contract, explicit, message := standaloneContract(json.RawMessage(`{"spec_version":"` + tc.version + `"}`))
		if !explicit || (message != "") != tc.refused || (!tc.refused && contract.Profile != tc.profile) {
			t.Errorf("%s: contract %+v message %q", tc.version, contract, message)
		}
		if tc.refused && !strings.Contains(message, "released as Core 0.5.0") {
			t.Errorf("%s: %q", tc.version, message)
		}
	}
	enum := evaluatorSpecProperty()["enum"].([]string)
	if !slices.Equal(enum, []string{"0.2.0-draft", "0.3.0", "0.5.0"}) {
		t.Errorf("spec_version enum = %v", enum)
	}
}
