package mcp

import (
	"encoding/json"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/packadvice"
)

const packAdviceTool = "experimental_analyze_pack"

func packAdviceDefinition() map[string]any {
	return map[string]any{"name": packAdviceTool, "description": "Read-only optional Draft RFC 0027 pack-design advisories over an explicit finite-cases domain. Exact supplied pack/domain JSON strings bind byte digests. Evaluates Core 0.3.0 base once per domain input with separate evaluator limits; does not append audit records or change admission, verdicts, strict validation or publication gates. All six advisory families run within declared work/output limits, with incomplete results explicit. Domain/output: 1 MiB, depth 32, 16384 values, 65536 UTF-8 bytes/string, 4096 digits/number. Domain: 1..64 complete {facts,evidence} inputs, <=256 evidence entries each. Analyzer admits <=256 rules/exceptions and source IDs <=256 characters; max_work 1..100000, max_findings 1..256. Universal claims cover only the supplied domain. Refusals carry status unavailable and an analyzer explanation code, with no report or Core error class. Experimental. This runtime's conformance claim and its version scope are stated, in full and only, in CONFORMANCE.md.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"pack", "domain"}, "properties": map[string]any{"pack": map[string]any{"type": "string"}, "domain": map[string]any{"type": "string"}, "intentional_fallback": map[string]any{"type": "boolean", "default": false}, "max_work": map[string]any{"type": "integer", "minimum": 1, "maximum": 100000, "default": 100000}, "max_findings": map[string]any{"type": "integer", "minimum": 1, "maximum": 256, "default": 256}}}}
}
func (s *Server) toolPackAdvice(raw json.RawMessage) any {
	if message := exactMembers(packAdviceTool, raw, "pack", "domain", "intentional_fallback", "max_work", "max_findings"); message != "" {
		return toolError(message)
	}
	var input struct {
		Pack        *string `json:"pack"`
		Domain      *string `json:"domain"`
		Intentional *bool   `json:"intentional_fallback"`
		Work        *int    `json:"max_work"`
		Findings    *int    `json:"max_findings"`
	}
	if json.Unmarshal(raw, &input) != nil || input.Pack == nil || input.Domain == nil {
		return toolError("Supply pack and domain as exact JSON strings, with optional boolean intentional_fallback and integer max_work/max_findings.")
	}
	// Explicit null is not an omitted optional argument.
	var members map[string]json.RawMessage
	_ = json.Unmarshal(raw, &members)
	for _, key := range []string{"intentional_fallback", "max_work", "max_findings"} {
		if string(members[key]) == "null" {
			return toolError("Optional analyzer arguments cannot be null.")
		}
	}
	options := packadvice.DefaultOptions()
	if input.Intentional != nil {
		options.IntentionalFallback = *input.Intentional
	}
	if input.Work != nil {
		options.MaxWork = *input.Work
	}
	if input.Findings != nil {
		options.MaxFindings = *input.Findings
	}
	report, refusal := packadvice.Analyze(evaluation.NewEngine(s.engine), []byte(*input.Pack), []byte(*input.Domain), options)
	if refusal != nil {
		return toolResult(refusal)
	}
	return toolResult(report)
}
