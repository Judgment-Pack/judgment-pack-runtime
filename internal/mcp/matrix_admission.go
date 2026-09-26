package mcp

import (
	"encoding/json"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const matrixContractTool = "experimental_get_test_matrix_contract"
const matrixValidationTool = "experimental_validate_test_matrix"

func matrixContractDefinition() map[string]any {
	return map[string]any{"name": matrixContractTool, "description": "Return the supported project test-matrix authoring contract. Read-only, experimental. No project, model, network or evaluator is accessed.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}}
}
func matrixValidationDefinition() map[string]any {
	return map[string]any{"name": matrixValidationTool, "description": "Validate a proposed project test matrix without executing tests. Every admitted row gets an indexed finding using the existing matrix and disposition decoders. Checks representation, not policy, coverage, reachability or agreement with a pack. No files, model, network or audit records are accessed. Experimental.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"matrix"}, "properties": map[string]any{"matrix": map[string]any{"type": "string", "description": "Exact matrix JSON text. Desk sourceMappings and other UI metadata must be kept outside the matrix."}}}}
}
func matrixEnvelope(command string) map[string]any {
	return map[string]any{"outputVersion": result.OutputVersion, "tool": result.CurrentTool(), "command": "mcp " + command, "experimental": true, "contractVersion": "1", "specVersion": expectationSpec}
}
func (s *Server) toolMatrixContract(args json.RawMessage) any {
	if message := exactMembers(matrixContractTool, args); message != "" {
		return toolError(message)
	}
	report := matrixEnvelope(matrixContractTool)
	report["contract"] = project.MatrixContract()
	return toolResult(report)
}
func (s *Server) toolValidateMatrix(args json.RawMessage) any {
	if message := exactMembers(matrixValidationTool, args, "matrix"); message != "" {
		return toolError(message)
	}
	var input struct {
		Matrix *string `json:"matrix"`
	}
	if json.Unmarshal(args, &input) != nil || input.Matrix == nil {
		return toolError("Expected matrix as a JSON string.")
	}
	checked := project.ValidateMatrix([]byte(*input.Matrix))
	report := matrixEnvelope(matrixValidationTool)
	report["status"], report["findings"], report["results"] = checked.Status, checked.Findings, checked.Results
	return toolResult(report)
}
