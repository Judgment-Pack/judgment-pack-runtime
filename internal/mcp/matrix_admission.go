package mcp

import (
	"encoding/json"
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const matrixContractTool = "experimental_get_test_matrix_contract"
const matrixValidationTool = "experimental_validate_test_matrix"
const maxFactAdviceCases = 256
const maxFactAdviceCapabilitySets = 16
const maxMatrixFactFindings = 1024
const maxMatrixFactBytes = 4 << 20

func matrixContractDefinition() map[string]any {
	return map[string]any{"name": matrixContractTool, "description": "Return the supported project test-matrix authoring contract. Read-only, experimental. No project, model, network or evaluator is accessed.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}}
}
func matrixValidationDefinition() map[string]any {
	return map[string]any{"name": matrixValidationTool, "description": "Validate a proposed project test matrix without executing tests. Every admitted row gets an indexed finding using the existing matrix and disposition decoders. Checks representation. Optional pack text adds ordered per-case static fact-type advisories for Core 0.2.0-draft; these do not change admission or establish policy, coverage, reachability or expected results. A valid matrix with pack permits at most 256 cases and 16 distinct supported-extension sets (each at most 256 strings of 1024 UTF-8 bytes), 100000 comparison work units per case, 100 findings and 256 KiB of serialized findings per case, and 1024 findings and 4 MiB of serialized fact-check rows in total. An exceeded batch bound is a tool error, never a clean check; per-case admission/work/report failure is unavailable. No files, model, network or audit records are accessed. Experimental.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"matrix"}, "properties": map[string]any{"pack": map[string]any{"type": "string", "description": "Optional exact pack JSON text for static fact-type advisories. No evaluation is performed."}, "matrix": map[string]any{"type": "string", "description": "Exact matrix JSON text. Desk sourceMappings and other UI metadata must be kept outside the matrix."}}}}
}
func matrixEnvelope(command string) map[string]any {
	return map[string]any{"outputVersion": result.OutputVersion, "tool": result.CurrentTool(), "command": "mcp " + command, "experimental": true, "contractVersion": "1", "specVersion": expectationSpec}
}
func (s *Server) toolMatrixContract(args json.RawMessage) any {
	if message := noArguments(matrixContractTool, args); message != "" {
		return toolError(message)
	}
	report := matrixEnvelope(matrixContractTool)
	report["contract"] = project.MatrixContract()
	return toolResult(report)
}
func (s *Server) toolValidateMatrix(args json.RawMessage) any {
	if message := exactMembers(matrixValidationTool, args, "matrix", "pack"); message != "" {
		return toolError(message)
	}
	var input struct {
		Matrix *string `json:"matrix"`
		Pack   *string `json:"pack"`
	}
	if json.Unmarshal(args, &input) != nil || input.Matrix == nil {
		return toolError("Expected matrix as a JSON string.")
	}
	var members map[string]json.RawMessage
	_ = json.Unmarshal(args, &members)
	if _, present := members["pack"]; present && input.Pack == nil {
		return toolError("Expected pack as a JSON string when supplied.")
	}
	checked := project.ValidateMatrix([]byte(*input.Matrix))
	report := matrixEnvelope(matrixValidationTool)
	report["status"], report["findings"], report["results"] = checked.Status, checked.Findings, checked.Results
	if input.Pack != nil && checked.Status == "valid" {
		matrix, err := project.DecodeMatrix([]byte(*input.Matrix))
		if err != nil {
			return toolError(err.Error())
		}
		// Bound validation amplification before any pack admission: different
		// capability sets otherwise each validate and retain another pack root.
		if len(matrix.Cases) > maxFactAdviceCases {
			return toolError("Fact type advice permits at most 256 cases. Check a smaller group of cases.")
		}
		capabilities := map[string]bool{}
		for _, item := range matrix.Cases {
			if len(item.SupportedExtensions) > maxFactAdviceExtensions {
				return toolError("Fact type advice permits at most 256 supported extensions per case.")
			}
			for _, extension := range item.SupportedExtensions {
				if len(extension) > maxFactAdviceExtensionBytes {
					return toolError("Fact type advice permits at most 1024 UTF-8 bytes per supported extension.")
				}
			}
			names := slices.Clone(item.SupportedExtensions)
			if names == nil {
				names = []string{}
			}
			slices.Sort(names)
			key, _ := json.Marshal(slices.Compact(names))
			capabilities[string(key)] = true
			if len(capabilities) > maxFactAdviceCapabilitySets {
				return toolError("Fact type advice permits at most 16 distinct supported-extension sets.")
			}
		}
		admitted := evaluation.NewEngine(s.engine).AdmitPack([]byte(*input.Pack))
		rows := make([]map[string]any, 0, len(matrix.Cases))
		total, reportBytes := 0, 2
		for index, item := range matrix.Cases {
			findings, failure := admitted.CheckFactTypes(item.Facts, evaluation.Options{SupportedExtensions: item.SupportedExtensions, WorkBudget: factAdviceWorkBudget})
			row := map[string]any{"index": index, "id": item.ID, "status": "complete", "findings": findings}
			total += len(findings)
			if total > maxMatrixFactFindings {
				return toolError("Fact type findings exceed the report limit. Check a smaller group of cases.")
			}
			if failure != nil {
				row["status"], row["message"], row["findings"] = "unavailable", factAdviceFailureMessage(failure), []evaluation.FactTypeFinding{}
			}
			encoded, _ := json.Marshal(row)
			reportBytes += len(encoded) + 1
			if reportBytes > maxMatrixFactBytes {
				return toolError("Fact type findings exceed the report byte limit. Check a smaller group of cases.")
			}
			rows = append(rows, row)
		}
		report["factCheckVersion"], report["factChecks"] = "1", rows
	}
	return toolResult(report)
}
