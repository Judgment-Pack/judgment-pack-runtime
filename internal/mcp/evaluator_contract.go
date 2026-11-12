package mcp

import (
	"encoding/json"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func evaluatorSpecProperty() map[string]any {
	return map[string]any{"type": "string", "enum": []string{expectationSpec, "0.3.0", "0.5.0"}, "description": "Optional exact Core disposition contract; defaults to 0.2.0-draft without a pack. With a pack it must match the pack's selected version. Core 0.3.0 and 0.5.0 support base only."}
}

func standaloneContract(raw json.RawMessage) (result.EvaluatorContract, bool, string) {
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil {
		return result.EvaluatorContract{}, false, "Expected an arguments object."
	}
	version, supplied := members["spec_version"]
	if !supplied {
		return result.LegacyEvaluatorContract(), false, ""
	}
	var selected *string
	if json.Unmarshal(version, &selected) != nil || selected == nil {
		return result.EvaluatorContract{}, true, "Supply spec_version as an exact supported Core version string."
	}
	contract := result.EvaluatorContract{SpecVersion: *selected}
	if *selected == "" {
		return contract, true, "Supply spec_version as an exact supported Core version string."
	}
	// Core 0.5.0's disposition and evaluator rules are 0.3.0's (a 0.5.0 pack
	// is evaluated as its evaluation form, a Core 0.3.0 pack), so it names a
	// standalone contract as 0.3.0 does.
	if err := contract.Validate(); err != nil {
		return contract, true, result.WithRetiredSelectorNote("Supported spec_version values are 0.2.0-draft, 0.3.0 and 0.5.0.", *selected)
	}
	if contract.Core030Rules() {
		contract.Profile = "base"
	}
	return contract, true, ""
}

func matrixEnvelopeWith(command string, contract result.EvaluatorContract) map[string]any {
	report := matrixEnvelope(command)
	report["specVersion"] = contract.Version()
	return report
}
