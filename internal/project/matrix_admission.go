package project

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
)

// MatrixContract describes the authoring carrier; DecodeMatrix remains the
// authority. Facts/evidence are evaluator inputs and may deliberately be invalid
// in a case expecting an evaluation error.
func MatrixContract() map[string]any {
	return map[string]any{
		"contractVersion": "1", "matrixVersion": MatrixVersion,
		"supportedVersions": SupportedMatrixVersions(), "defaultVersion": MatrixVersionDefault,
		"rootMembers": slices.Clone(matrixRootMembers), "rowMembers": slices.Clone(matrixRowMembers),
		"introducedIn": maps.Clone(matrixVersionMembers), "requiredRowMembers": []string{"id", "facts"},
		"exactlyOne": []string{"expectedDisposition", "expectedErrorClass"},
		"memberTypes": map[string]string{
			"id": "nonempty unique string", "facts": "any JSON value; preserve nested structure and omitted values",
			"origin": "string", "focus": "string: case name and rationale", "specSection": "string",
			"evidenceAvailability": "JSON evaluator input; normally an object mapping evidence IDs to present, absent or unknown",
			"supportedExtensions":  "array of strings", "expectedErrorClass": "nonempty string", "expectedErrorPhase": "string; only with expectedErrorClass",
			"expectedHandoffTarget": "null or {kind, name}; only with expectedDisposition",
			"cites":                 "array of {sessionId, callIndex, signature} gateway receipt citations; never invent receipts",
			"expectedDisposition":   "complete JPS 0.2.0-draft disposition; validated with the runtime disposition decoder",
		},
		"outcomeExample": map[string]any{"kind": "outcome", "outcomeId": "declared-outcome", "reasons": []string{}, "handoff": map[string]string{"state": "none"}},
		"limits":         map[string]any{"bytes": MaxMatrixBytes, "cases": MaxMatrixCases},
		"scope":          "Representation and disposition-local constraints only. Does not evaluate a pack, prove coverage, validate policy, or establish reachability. Unknown members are refused. Desk metadata does not belong in matrix rows.",
	}
}

type MatrixFinding struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}
type MatrixAdmissionRow struct {
	Index    int             `json:"index"`
	ID       string          `json:"id,omitempty"`
	Status   string          `json:"status"`
	Findings []MatrixFinding `json:"findings"`
}
type MatrixAdmission struct {
	Status   string               `json:"status"`
	Findings []MatrixFinding      `json:"findings"`
	Results  []MatrixAdmissionRow `json:"results"`
}

// ValidateMatrix accounts for every admitted row, without a project or evaluator.
// Per-row carrier checks reuse DecodeMatrix, never a second permissive decoder.
func ValidateMatrix(data []byte) MatrixAdmission {
	report := MatrixAdmission{Status: "invalid", Findings: []MatrixFinding{}, Results: []MatrixAdmissionRow{}}
	fail := func(code, path, message string) MatrixAdmission {
		report.Findings = append(report.Findings, MatrixFinding{code, path, message})
		return report
	}
	if int64(len(data)) > MaxMatrixBytes {
		return fail("MATRIX-LIMIT", "", "Matrix exceeds the input limit.")
	}
	value, failure := carrier.Decode(data, carrier.DefaultLimits())
	if failure != nil {
		code := "MATRIX-JSON"
		if failure.Resource {
			code = "MATRIX-LIMIT"
		}
		return fail(code, "", failure.Error())
	}
	root, ok := value.(map[string]any)
	if !ok {
		return fail("MATRIX-SHAPE", "", "Matrix must be an object.")
	}
	version, err := declaredMatrixVersion(root)
	if err != nil {
		return fail("MATRIX-VERSION", "/matrixVersion", err.Error())
	}
	for _, key := range slices.Sorted(maps.Keys(root)) {
		if !slices.Contains(matrixRootMembers, key) {
			return fail("MATRIX-MEMBER", "/"+pointerMember(key), fmt.Sprintf("Unsupported matrix member %q.", key))
		}
	}
	var raw struct {
		Cases []json.RawMessage `json:"cases"`
	}
	if err := json.Unmarshal(data, &raw); err != nil || len(raw.Cases) == 0 {
		return fail("MATRIX-SHAPE", "/cases", "Supply a nonempty cases array.")
	}
	if len(raw.Cases) > MaxMatrixCases {
		return fail("MATRIX-LIMIT", "/cases", "Matrix exceeds the supported case count.")
	}
	seen := map[string]bool{}
	report.Status = "valid"
	for index, bytes := range raw.Cases {
		path := fmt.Sprintf("/cases/%d", index)
		row := MatrixAdmissionRow{Index: index, Status: "valid", Findings: []MatrixFinding{}}
		add := func(code, suffix, message string) {
			row.Findings = append(row.Findings, MatrixFinding{code, path + suffix, message})
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(bytes, &object) != nil || object == nil {
			add("MATRIX-SHAPE", "", "Case must be an object.")
		} else {
			_ = json.Unmarshal(object["id"], &row.ID)
			for _, key := range slices.Sorted(maps.Keys(object)) {
				if !slices.Contains(matrixRowMembers, key) {
					add("MATRIX-MEMBER", "/"+pointerMember(key), fmt.Sprintf("Unsupported case member %q. Put the case name and rationale in focus.", key))
				}
			}
			if row.ID == "" {
				add("MATRIX-ID", "/id", "Every case needs a nonempty string id.")
			} else if seen[row.ID] {
				add("MATRIX-ID", "/id", "Case id is duplicated: "+row.ID)
			}
			seen[row.ID] = true
			if _, ok := object["facts"]; !ok {
				add("MATRIX-FACTS", "/facts", "Every case needs a facts value.")
			}
			if len(row.Findings) == 0 {
				single, _ := json.Marshal(map[string]any{"matrixVersion": version, "cases": []json.RawMessage{bytes}})
				admitted, err := DecodeMatrix(single)
				if err != nil {
					add("MATRIX-ROW", "", err.Error())
				} else if disposition := admitted.Cases[0].ExpectedDisposition; disposition != nil {
					if _, err := evaluation.DecodeDisposition(disposition); err != nil {
						add("MATRIX-EXPECTATION", "/expectedDisposition", err.Error())
					}
				}
			}
		}
		if len(row.Findings) > 0 {
			row.Status = "invalid"
			report.Status = "invalid"
		}
		report.Results = append(report.Results, row)
	}
	return report
}
func pointerMember(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
