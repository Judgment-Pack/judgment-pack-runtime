package conformance

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Frozen Core 0.3.0 expectations point at a rule object. Runtime diagnostics
// point at its when member. This adapter is local to advisory expectations;
// normative diagnostic pointers keep literal equality.
func advisoryExpectationsMatch(raw json.RawMessage, actual []result.Diagnostic, strict bool) bool {
	var expected []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&expected) != nil || len(expected) != len(actual) {
		return false
	}
	used := make([]bool, len(actual))
	for _, want := range expected {
		want["instancePath"] = want["path"].(string) + "/when"
		delete(want, "path")
		mode := "validate"
		if strict {
			mode = "strict"
		}
		want["severity"] = want["severity"].(map[string]any)[mode]
		found := false
		for i, d := range actual {
			if !used[i] {
				encoded, _ := json.Marshal(d)
				var got map[string]any
				decoder := json.NewDecoder(bytes.NewReader(encoded))
				decoder.UseNumber()
				_ = decoder.Decode(&got)
				if advisorySubset(want, got) {
					used[i] = true
					found = true
					break
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func advisorySubset(want, got any) bool {
	if members, ok := want.(map[string]any); ok {
		actual, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range members {
			actualValue, exists := actual[key]
			if !exists || !advisorySubset(value, actualValue) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(want, got)
}
func splitAdvice(actual []result.Diagnostic, strict bool) (normative, advice []result.Diagnostic) {
	for _, d := range actual {
		if d.Code == "JPS-RULES-OVERLAP" {
			if strict && d.Certainty == "exact" {
				d.Severity = "error"
			}
			advice = append(advice, d)
		} else {
			normative = append(normative, d)
		}
	}
	return
}

func retainDiagnosticBytes(diagnostics []result.Diagnostic, remaining *int) ([]result.Diagnostic, bool) {
	retained := 0
	for _, d := range diagnostics {
		encoded, _ := json.Marshal(d)
		if len(encoded) > *remaining {
			break
		}
		*remaining -= len(encoded)
		retained++
	}
	return diagnostics[:retained], retained < len(diagnostics)
}
