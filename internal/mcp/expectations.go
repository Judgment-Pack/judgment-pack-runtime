package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
)

const expectationTool = "experimental_validate_expectations"
const maxExpectations = 256
const maxExpectationBytes = 16 * 1024
const expectationSpec = "0.2.0-draft"

func expectationToolDefinition() map[string]any {
	return map[string]any{
		"name":        expectationTool,
		"description": "Check proposed exact JPS dispositions before admitting authoring test cases (ADR-0035). This read-only operation uses the same strict disposition decoder as the runtime's matrix comparator. It checks representation and disposition-local constraints, not whether a pack produces the expectation, whether the policy is correct, or whether a declared outcome or handoff agrees with a particular pack. Every input has one indexed valid/invalid result. Invalid expectations remain reported; none are silently dropped. No pack, project, evaluator, audit record, source, credential or network is accessed. Only JPS 0.2.0-draft is supported. Each expectation is JSON text, limited to 16 KiB, depth 16, 1024 nodes and 8 KiB per string; a call carries 1–256 expectations.",
		"inputSchema": map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"spec_version", "expectations"},
			"properties": map[string]any{
				"spec_version": map[string]any{"type": "string", "enum": []string{expectationSpec}},
				"expectations": map[string]any{"type": "array", "minItems": 1, "maxItems": maxExpectations,
					"items": map[string]any{"type": "string", "description": "One complete expected §8.3 disposition as JSON text; byte and structural limits are enforced before disposition decoding."}},
			},
		},
	}
}

type expectationResult struct {
	Index     int    `json:"index"`
	Status    string `json:"status"`
	Canonical string `json:"canonical,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
}

func (s *Server) toolValidateExpectations(rawArgs json.RawMessage) any {
	if message := exactMembers(expectationTool, rawArgs, "spec_version", "expectations"); message != "" {
		return toolError(message)
	}
	var args struct {
		SpecVersion  string            `json:"spec_version"`
		Expectations []json.RawMessage `json:"expectations"`
	}
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return toolError("Expected spec_version and an array of expectation JSON strings.")
	}
	if args.SpecVersion != expectationSpec {
		return toolError("Expectation validation supports exactly spec_version 0.2.0-draft.")
	}
	if len(args.Expectations) == 0 || len(args.Expectations) > maxExpectations {
		return toolError("An expectation validation call must carry 1–256 expectations.")
	}
	// Reject malformed arguments as a whole, before reporting any disposition.
	texts := make([]string, len(args.Expectations))
	for i, raw := range args.Expectations {
		if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &texts[i]) != nil {
			return toolError(fmt.Sprintf("expectations[%d] must be a JSON string.", i))
		}
	}
	status := "valid"
	results := make([]expectationResult, len(texts))
	for i, text := range texts {
		row := expectationResult{Index: i, Status: "invalid", Code: "JPS-EXPECTATION-INVALID"}
		switch {
		case len(text) > maxExpectationBytes:
			row.Code, row.Message = "JPS-EXPECTATION-LIMIT", "The expectation exceeds the 16 KiB limit."
		default:
			_, failure := carrier.Decode([]byte(text), carrier.Limits{MaxDepth: 16, MaxNodes: 1024, MaxStringBytes: 8192})
			if failure != nil {
				row.Message = failure.Error()
				if failure.Resource {
					row.Code = "JPS-EXPECTATION-LIMIT"
				}
			} else if disposition, err := evaluation.DecodeDisposition(json.RawMessage(text)); err != nil {
				row.Message = err.Error()
			} else {
				canonical, _ := disposition.Canonical() // DecodeDisposition already applied this gate.
				row.Status, row.Canonical, row.Code = "valid", string(canonical), ""
			}
		}
		if row.Status != "valid" {
			status = "invalid"
		}
		results[i] = row
	}
	return toolResult(map[string]any{"status": status, "specVersion": expectationSpec, "results": results})
}
