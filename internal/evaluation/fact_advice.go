package evaluation

import (
	"encoding/json"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Advisory limits apply to reporting, independently of the comparison work
// budget and the input carrier limits. A limit refuses the whole check.
const MaxFactTypeFindings = 100
const MaxFactTypeFindingBytes = 256 * 1024

// FactTypeFinding is a static advisory, never an evaluation or a policy verdict.
// Path is relative to the facts document, or to an element of Within.
// Values are not copied into diagnostics.
type FactTypeFinding struct {
	Code          string   `json:"code"`
	Path          string   `json:"path"`
	Within        *string  `json:"within,omitempty"`
	Operator      string   `json:"operator"`
	FactType      string   `json:"factType"`
	ExpectedTypes []string `json:"expectedTypes"`
	Message       string   `json:"message"`
}

// CheckFactTypes reuses the same bounded comparability walk as the optional
// project guard, without evaluating conditions, applying the guard, or coercing
// facts. Missing facts are not findings; deliberate wrong-type cases stay legal.
func (a *AdmittedPack) CheckFactTypes(facts []byte, options Options) ([]FactTypeFinding, *Failure) {
	if failure := oneDraft(options); failure != nil {
		return nil, failure
	}
	if a.oversized != nil {
		return nil, copyFailure(a.oversized)
	}
	if failure := byteLimit("pack", a.pack, result.ClassPackNotConformant, options.oversized("pack")); failure != nil {
		return nil, failure
	}
	admission := a.admission(options)
	if admission.failure != nil {
		return nil, copyFailure(admission.failure)
	}
	if admission.unsupported != nil {
		return nil, copyFailure(admission.unsupported)
	}
	if failure := byteLimit("facts", facts, result.ClassMalformedInput, options.oversized("facts")); failure != nil {
		return nil, failure
	}
	document, failure := decodeInput(facts, "facts", result.ClassMalformedInput)
	if failure != nil {
		return nil, failure
	}
	found, exceeded := incomparableFacts(admission.packRoot, document, options)
	if exceeded || len(found) > MaxFactTypeFindings {
		return nil, factTypeLimit()
	}
	findings := make([]FactTypeFinding, 0, len(found))
	reportBytes := 2 // []
	for _, item := range found {
		expected := append([]string{}, item.matches...)
		if item.ordered {
			expected = []string{"decimal-string"}
		}
		finding := FactTypeFinding{Code: "FACT-NOT-COMPARABLE", Path: item.path, Within: item.within, Operator: item.operator, FactType: item.factType, ExpectedTypes: expected, Message: item.describe()}
		encoded, _ := json.Marshal(finding)
		reportBytes += len(encoded) + 1
		if reportBytes > MaxFactTypeFindingBytes {
			return nil, factTypeLimit()
		}
		findings = append(findings, finding)
	}
	return findings, nil
}

func factTypeLimit() *Failure {
	return &Failure{Code: "FACT-CHECK-LIMIT", Message: "Fact type checks exceeded the advisory work or report limit; no complete check is available.", ExitCode: result.ExitInvalid}
}
