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
	return map[string]any{"name": matrixContractTool, "description": "Return the supported project test-matrix authoring contract. Optional spec_version selects Core 0.2.0-draft (default) or 0.3.0 base without changing matrix carrier versions 1-3. Read-only, experimental. No project, model, network or evaluator is accessed.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"spec_version": evaluatorSpecProperty()}}}
}
func matrixValidationDefinition() map[string]any {
	return map[string]any{"name": matrixValidationTool, "description": "Validate a proposed project test matrix without executing tests. Every admitted row gets an indexed finding using the existing matrix and disposition decoders. Optional spec_version selects the exact expectation contract, default 0.2.0-draft; a supplied pack selects its own exact contract, and a conflicting explicit version is refused. Route presence never selects a version. Checks representation. Optional pack text adds ordered per-case static fact-type advisories under the exact selected Core 0.2.0-draft or 0.3.0 base contract; these do not change admission or establish policy, coverage, reachability or expected results. A valid matrix with pack permits at most 256 cases and 16 distinct supported-extension sets (each at most 256 strings of 1024 UTF-8 bytes), 100000 comparison work units per case, 100 findings and 256 KiB of serialized findings per case, and 1024 findings and 4 MiB of serialized fact-check rows in total. An exceeded batch bound is a tool error, never a clean check; per-case admission/work/report failure is unavailable. No files, model, network or audit records are accessed. Experimental.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"matrix"}, "properties": map[string]any{"spec_version": evaluatorSpecProperty(), "pack": map[string]any{"type": "string", "description": "Optional exact pack JSON text for static fact-type advisories. No evaluation is performed."}, "matrix": map[string]any{"type": "string", "description": "Exact matrix JSON text. Desk sourceMappings and other UI metadata must be kept outside the matrix."}}}}
}
func matrixEnvelope(command string) map[string]any {
	return map[string]any{"outputVersion": result.OutputVersion, "tool": result.CurrentTool(), "command": "mcp " + command, "experimental": true, "contractVersion": "1", "specVersion": expectationSpec}
}
func (s *Server) toolMatrixContract(args json.RawMessage) any {
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage(`{}`)
	}
	if message := exactMembers(matrixContractTool, args, "spec_version"); message != "" {
		return toolError(message)
	}
	contract, _, message := standaloneContract(args)
	if message != "" {
		return toolError(message)
	}
	report := matrixEnvelopeWith(matrixContractTool, contract)
	report["contract"] = project.MatrixContractWith(contract)
	return toolResult(report)
}
func (s *Server) toolValidateMatrix(args json.RawMessage) any {
	if message := exactMembers(matrixValidationTool, args, "matrix", "pack", "spec_version"); message != "" {
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
	contract, explicit, message := standaloneContract(args)
	if message != "" {
		return toolError(message)
	}
	if input.Pack != nil {
		selected, failure := evaluation.SelectContract([]byte(*input.Pack))
		if failure != nil && failure.Code == "profile-unsupported" {
			return evaluationToolError("mcp "+matrixValidationTool, failure)
		}
		if failure == nil && explicit && contract.Version() != selected.Version() {
			return toolError("spec_version conflicts with the exact supplied pack contract.")
		}
		if failure == nil {
			contract = selected
		}
	}
	checked := project.ValidateMatrixWith([]byte(*input.Matrix), contract)
	report := matrixEnvelopeWith(matrixValidationTool, contract)
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
		// Preserve raw capability types for advice. The legacy matrix decoder
		// accepts null slices and null entries through []string decoding; keep
		// that structural admission, but never advise using normalized values.
		var rawMatrix struct {
			Cases []struct {
				Extensions json.RawMessage `json:"supportedExtensions"`
			} `json:"cases"`
		}
		if json.Unmarshal([]byte(*input.Matrix), &rawMatrix) != nil || len(rawMatrix.Cases) != len(matrix.Cases) {
			return toolError("Fact type advice could not inspect the supplied matrix capabilities.")
		}
		supportedExtensions := make([][]string, len(matrix.Cases))
		capabilityMessages := make([]string, len(matrix.Cases))
		capabilities := map[string]bool{}
		for index, item := range matrix.Cases {
			if len(item.SupportedExtensions) > maxFactAdviceExtensions {
				return toolError("Fact type advice permits at most 256 supported extensions per case.")
			}
			for _, extension := range item.SupportedExtensions {
				if len(extension) > maxFactAdviceExtensionBytes {
					return toolError("Fact type advice permits at most 1024 UTF-8 bytes per supported extension.")
				}
			}
			extensions, message := factAdviceExtensions(rawMatrix.Cases[index].Extensions)
			if message != "" {
				capabilityMessages[index] = "Fact type advice is unavailable: supportedExtensions must be an array of at most 256 strings, each at most 1024 UTF-8 bytes."
				continue
			}
			supportedExtensions[index] = extensions
			names := slices.Clone(extensions)
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
			findings := []evaluation.FactTypeFinding{}
			var failure *evaluation.Failure
			if capabilityMessages[index] == "" {
				findings, failure = admitted.CheckFactTypes(item.Facts, evaluation.Options{SpecVersion: contract.Version(), SupportedExtensions: supportedExtensions[index], WorkBudget: factAdviceWorkBudget})
			}
			row := map[string]any{"index": index, "id": item.ID, "status": "complete", "findings": findings}
			total += len(findings)
			if total > maxMatrixFactFindings {
				return toolError("Fact type findings exceed the report limit. Check a smaller group of cases.")
			}
			if capabilityMessages[index] != "" {
				row["status"], row["message"] = "unavailable", capabilityMessages[index]
			} else if failure != nil {
				setFactAdviceFailure(row, failure)
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
