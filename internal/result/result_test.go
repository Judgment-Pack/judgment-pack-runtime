package result

import (
	"encoding/json"
	"testing"
)

// The values below are the machine-facing contract: consumers branch on the
// exit status and on outputVersion. Changing any of them is a compatibility
// break that must be a deliberate, documented decision rather than a side
// effect of an unrelated edit, so they are pinned by literal here.

func TestOutputVersionIsPinned(t *testing.T) {
	if OutputVersion != "1" {
		t.Fatalf("outputVersion is part of the machine contract: got %q, want %q", OutputVersion, "1")
	}
	if CLIName != "judgment-pack" {
		t.Fatalf("tool.name is part of the machine contract: got %q", CLIName)
	}
}

func TestExitClassesArePinned(t *testing.T) {
	for _, pinned := range []struct {
		name string
		got  int
		want int
	}{
		{"ExitSuccess", ExitSuccess, 0},
		{"ExitInvalid", ExitInvalid, 1},
		{"ExitUnsupported", ExitUnsupported, 2},
		{"ExitInvocation", ExitInvocation, 3},
		{"ExitIO", ExitIO, 4},
		{"ExitInternal", ExitInternal, 5},
	} {
		if pinned.got != pinned.want {
			t.Errorf("%s is part of the machine contract: got %d, want %d", pinned.name, pinned.got, pinned.want)
		}
	}
}

// Every payload carries the same four identifying members. A rename of any of
// them is invisible to Go's type checker but breaks every consumer, so assert
// the emitted JSON rather than the struct.
func TestEveryPayloadCarriesTheCommonEnvelope(t *testing.T) {
	for _, payload := range []struct {
		name  string
		value any
	}{
		{"Validation", Validation{OutputVersion: OutputVersion, Tool: CurrentTool(), Command: "spec validate", Status: "valid"}},
		{"Suite", Suite{OutputVersion: OutputVersion, Tool: CurrentTool(), Command: "spec test-conformance", Status: "valid"}},
		{"Schema", Schema{OutputVersion: OutputVersion, Tool: CurrentTool(), Command: "spec schema", Status: "valid"}},
		{"Version", Version{OutputVersion: OutputVersion, Tool: CurrentTool(), Command: "version", Status: "valid"}},
	} {
		encoded, err := json.Marshal(payload.value)
		if err != nil {
			t.Fatalf("%s: %v", payload.name, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("%s: %v", payload.name, err)
		}
		for _, member := range []string{"outputVersion", "tool", "command", "status"} {
			if _, present := decoded[member]; !present {
				t.Errorf("%s payload is missing the %q member: %s", payload.name, member, encoded)
			}
		}
		if decoded["outputVersion"] != "1" {
			t.Errorf("%s payload reports outputVersion %v, want \"1\"", payload.name, decoded["outputVersion"])
		}
	}
}
