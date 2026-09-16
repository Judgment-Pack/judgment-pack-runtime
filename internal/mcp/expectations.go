package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const expectationTool = "experimental_validate_expectations"
const maxExpectations = 256
const maxExpectationBytes = 16 * 1024
const expectationSpec = "0.2.0-draft"

func expectationToolDefinition() map[string]any {
	return map[string]any{
		"name":        expectationTool,
		"description": "EXPERIMENTAL SURFACE (ADR-0035): check proposed exact JPS dispositions before admitting authoring test cases. This surface may change or be removed without compatibility promise. This read-only operation uses the same strict disposition decoder as the runtime's matrix comparator. It checks representation and disposition-local constraints, not whether a pack produces the expectation, whether the policy is correct, or whether a declared outcome or handoff agrees with a particular pack. A valid finding is therefore necessary, not sufficient: some valid dispositions are reachable by no pack at all, and an outcome id it admits may name no declared outcome. Reason and trigger sets are normalized rather than refused — duplicates and order carry no meaning in a §8.3 set — so the canonical text of a valid finding, not the text submitted, is what this runtime compared. Every input has one indexed valid/invalid result, and an invalid finding carries the code and the rule that refused it; none are silently dropped. A JPS-EXPECTATION-LIMIT finding means the input was not admitted, not that Core prohibits its meaning, so branch on the code rather than the status. No pack, project, evaluator, audit record, source, credential or network is accessed. Only JPS 0.2.0-draft is supported. Each expectation is JSON text, limited to 16 KiB, depth 16, 1024 nodes and 8 KiB per string; a call carries 1–256 expectations.",
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

func (s *Server) toolValidateExpectations(rawArgs json.RawMessage) any {
	if message := exactMembers(expectationTool, rawArgs, "spec_version", "expectations"); message != "" {
		return toolError(message)
	}
	const notABatch = "Expected spec_version and an array of expectation JSON strings."
	// The batch is held as raw bytes, not as a slice of elements: decoding the
	// elements here would build every one of them -- a maximal frame carries
	// millions of short ones -- before the count that refuses the call is
	// known, so the refusal would cost more than the work it refuses. Whether
	// the member is an array at all is still settled here, so a batch of the
	// wrong shape is refused where it always was, ahead of the version.
	var args struct {
		SpecVersion  string          `json:"spec_version"`
		Expectations json.RawMessage `json:"expectations"`
	}
	if err := json.Unmarshal(rawArgs, &args); err != nil || !isBatchArray(args.Expectations) {
		return toolError(notABatch)
	}
	if args.SpecVersion != expectationSpec {
		return toolError("Expectation validation supports exactly spec_version 0.2.0-draft.")
	}
	count, counted := countBatch(args.Expectations)
	if !counted {
		return toolError(notABatch)
	}
	if count == 0 || count > maxExpectations {
		return toolError("An expectation validation call must carry 1–256 expectations.")
	}
	// Within the count bound the batch is small enough to hold, so its elements
	// are read the way every other tool reads its arguments.
	var elements []json.RawMessage
	if err := json.Unmarshal(args.Expectations, &elements); err != nil {
		return toolError(notABatch)
	}
	// Reject malformed arguments as a whole, before reporting any disposition.
	texts := make([]string, len(elements))
	for i, raw := range elements {
		if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &texts[i]) != nil {
			return toolError(fmt.Sprintf("expectations[%d] must be a JSON string.", i))
		}
	}
	results := make([]result.ExpectationFinding, len(texts))
	for i, text := range texts {
		row := result.ExpectationFinding{Index: i, Status: "invalid", Code: "JPS-EXPECTATION-INVALID"}
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
		results[i] = row
	}
	return toolResult(result.NewExpectationReport("mcp "+expectationTool, expectationSpec, results))
}

// isBatchArray reports whether the expectations member is an array, or is
// absent or null -- both of which are the empty batch the count bound refuses,
// as they were when a typed slice read them.
func isBatchArray(batch json.RawMessage) bool {
	text := bytes.TrimSpace(batch)
	return len(text) == 0 || text[0] == '[' || bytes.Equal(text, []byte("null"))
}

// countBatch counts the elements of the batch, keeping none of them: one
// buffer is reused for every element, so the count costs the bytes of the
// largest element and not of the array. It stops at the first element past the
// bound, because nothing beyond the count is decided for a call that is
// refused for its count. The bool is false only for bytes that are not an
// array of complete values -- unreachable through the server, which has
// already held the whole message to being valid JSON.
func countBatch(batch json.RawMessage) (int, bool) {
	text := bytes.TrimSpace(batch)
	if len(text) == 0 || bytes.Equal(text, []byte("null")) {
		return 0, true
	}
	decoder := json.NewDecoder(bytes.NewReader(text))
	if token, err := decoder.Token(); err != nil || token != json.Delim('[') {
		return 0, false
	}
	count := 0
	element := make(json.RawMessage, 0, 64)
	for decoder.More() {
		count++
		if count > maxExpectations {
			return count, true
		}
		element = element[:0]
		if err := decoder.Decode(&element); err != nil {
			return 0, false
		}
	}
	return count, true
}
