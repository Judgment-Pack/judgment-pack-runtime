package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
)

const factAdviceTool = "experimental_check_fact_types"
const factAdviceWorkBudget = 100000
const maxFactAdviceExtensions = 256
const maxFactAdviceExtensionBytes = 1024

func factAdviceDefinition() map[string]any {
	return map[string]any{"name": factAdviceTool, "description": "Read-only static fact comparability advice for exact pack and facts JSON. Only Core 0.2.0-draft is supported. Uses Runtime comparison rules without evaluating decisions, requiring test expectations, coercing facts, reading project files or writing audit records. Missing facts are not findings; null, false, zero and decimal strings retain their types. A complete check with findings is not an execution result. Carrier limits apply (10 MiB per document, depth 128, 250000 nodes, 1 MiB strings); each check permits 100000 comparison work units, 100 findings and 256 KiB of serialized findings. A refused or limited check is unavailable, never clean. Experimental.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"pack", "facts"}, "properties": map[string]any{"pack": map[string]any{"type": "string"}, "facts": map[string]any{"type": "string"}, "supported_extensions": map[string]any{"type": "array", "maxItems": maxFactAdviceExtensions, "items": map[string]any{"type": "string", "maxLength": maxFactAdviceExtensionBytes}}}}}
}
func (s *Server) toolFactAdvice(args json.RawMessage) any {
	if message := exactMembers(factAdviceTool, args, "pack", "facts", "supported_extensions"); message != "" {
		return toolError(message)
	}
	var input struct {
		Pack       *string         `json:"pack"`
		Facts      *string         `json:"facts"`
		Extensions json.RawMessage `json:"supported_extensions"`
	}
	if json.Unmarshal(args, &input) != nil || input.Pack == nil || input.Facts == nil {
		return toolError("Supply pack and facts as JSON strings.")
	}
	extensions, message := factAdviceExtensions(input.Extensions)
	if message != "" {
		return toolError(message)
	}
	admitted := evaluation.NewEngine(s.engine).AdmitPack([]byte(*input.Pack))
	findings, failure := admitted.CheckFactTypes([]byte(*input.Facts), evaluation.Options{SupportedExtensions: extensions, WorkBudget: factAdviceWorkBudget})
	report := matrixEnvelope(factAdviceTool)
	report["factCheckVersion"], report["status"], report["findings"] = "1", "complete", findings
	if failure != nil {
		report["status"], report["message"], report["findings"] = "unavailable", factAdviceFailureMessage(failure), []evaluation.FactTypeFinding{}
	}
	return toolResult(report)
}

// Decode entries as pointers: encoding/json otherwise accepts null as "".
func factAdviceExtensions(raw json.RawMessage) ([]string, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	var entries []*string
	if len(raw) > maxFactAdviceExtensions*(6*maxFactAdviceExtensionBytes+3)+2 || json.Unmarshal(raw, &entries) != nil || entries == nil || len(entries) > maxFactAdviceExtensions {
		return nil, "Supply supported_extensions as an array of at most 256 strings, each at most 1024 UTF-8 bytes."
	}
	extensions := make([]string, len(entries))
	for i, entry := range entries {
		if entry == nil || len(*entry) > maxFactAdviceExtensionBytes {
			return nil, "Supply supported_extensions as an array of at most 256 strings, each at most 1024 UTF-8 bytes."
		}
		extensions[i] = *entry
	}
	return extensions, ""
}

// Failure details can quote authored input. Keep unavailability bounded and
// independent of submitted values; the runtime-owned code identifies the gate.
func factAdviceFailureMessage(failure *evaluation.Failure) string {
	return fmt.Sprintf("Fact type advice is unavailable (%s); no complete check was produced.", failure.Code)
}
