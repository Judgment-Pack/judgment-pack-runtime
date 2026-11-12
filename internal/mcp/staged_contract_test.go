package mcp

import (
	"encoding/json"
	"testing"
)

// A standalone contract (no pack) does not name the staged draft selector,
// which is admitted only with its pack.
func TestStandaloneContractRefusesTheStagedSelector(t *testing.T) {
	for _, tc := range []struct {
		version string
		refused bool
	}{{"0.2.0-draft", false}, {"0.3.0", false}, {"0.4.0-draft", true}} {
		contract, explicit, message := standaloneContract(json.RawMessage(`{"spec_version":"` + tc.version + `"}`))
		if !explicit || (message != "") != tc.refused {
			t.Errorf("%s: contract %+v message %q", tc.version, contract, message)
		}
	}
}
