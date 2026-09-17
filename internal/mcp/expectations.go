package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"

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
		"description": "EXPERIMENTAL SURFACE (ADR-0035): check proposed exact JPS dispositions before admitting authoring test cases. This surface may change or be removed without compatibility promise. This read-only operation uses the same strict disposition decoder as the runtime's matrix comparator. It checks representation and disposition-local constraints, not whether a pack produces the expectation, whether the policy is correct, or whether a declared outcome or handoff agrees with a particular pack. Pack-independent reachability is checked after that grammar gate and only when the grammar gate passes: a JPS-EXPECTATION-UNREACHABLE finding means §8's step order or §5's local-identifier grammar puts the disposition beyond every conforming pack — an unresolved result retaining not-applicable, no-match beside another reason, or an outcomeId outside ^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$ — so the expectation and not the candidate is what must change; an input that is both malformed and unreachable is reported for its grammar defect, under JPS-EXPECTATION-INVALID. A valid finding is therefore necessary, not sufficient: pack-dependent reachability is not checked, so an outcome id it admits may name no declared outcome of your pack and a handoff may be one your pack does not configure. Reason and trigger sets are normalized rather than refused — duplicates and order carry no meaning in a §8.3 set — so the canonical text of a valid finding, not the text submitted, is what this runtime compared. Every input has one indexed valid/invalid result, and an invalid finding carries the code and the rule that refused it; none are silently dropped. A JPS-EXPECTATION-LIMIT finding means the input was not admitted, not that Core prohibits its meaning, so branch on the code rather than the status. No pack, project, evaluator, audit record, source, credential or network is accessed. Only JPS 0.2.0-draft is supported. Each expectation is JSON text, limited to 16 KiB, depth 16, 1024 nodes and 8 KiB per string; a call carries 1–256 expectations.",
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
			} else if unreachable := unreachableExpectation(disposition); unreachable != "" {
				// After the §8.3 grammar gate, never before it. A shape that is
				// not a disposition at all has no reachability to speak of, and
				// an author cannot act on the second diagnosis before the first,
				// so a text that is both malformed and unreachable keeps the
				// code that means "not a disposition".
				//
				// The row keeps the empty Canonical it was built with. A valid
				// and an invalid finding carry disjoint members (ADR-0035,
				// result.ExpectationFinding), and the canonical text is what a
				// client stores and compares — the one thing it must not do with
				// an expectation no pack can produce.
				row.Code, row.Message = "JPS-EXPECTATION-UNREACHABLE", unreachable
			} else {
				canonical, _ := disposition.Canonical() // DecodeDisposition already applied this gate.
				row.Status, row.Canonical, row.Code = "valid", string(canonical), ""
			}
		}
		results[i] = row
	}
	return toolResult(result.NewExpectationReport("mcp "+expectationTool, expectationSpec, results))
}

// localIdentifier is §5's local-object-identifier grammar, quoted from the
// section itself: "Local object identifiers are non-empty ASCII strings matching
// `^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`."
//
// It is a fourth copy of that literal rather than a reuse of an existing one,
// deliberately. internal/validation holds the same characters as a lookup key
// that maps a JSON Schema `pattern` to a diagnostic code, not as a matcher, so
// sharing it would couple this gate to that table's job; internal/project's
// compiled copy is unexported in a package the MCP server does not and should
// not import for one regexp. The grammar is a normative constant of §5, not an
// implementation detail that can drift: every place that enforces it in this
// repository already states it, and the specification's own expected-disposition
// schema states it once more, at $defs/localId, which $defs/disposition names
// for outcomeId.
var localIdentifier = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// unreachableOutcomeIDRule is class 3's message, with the offending identifier
// quoted into it by %q. It is a named constant because the size of that echo is
// a documented bound (ADR-0036, Security and privacy) and the bound is derived
// from this text rather than asserted beside it: the sentence is 166 bytes with
// the verb removed, the largest identifier the carrier admits expands to 32,770
// bytes under %q -- 8,192 decoded bytes of U+007F, four bytes each -- so the
// decoded message reaches 32,936 bytes and the serialized finding 41,212, both
// measured by TestUnreachableOutcomeIDEchoIsBoundedAtItsWorstCase.
const unreachableOutcomeIDRule = `§5: "Local object identifiers are non-empty ASCII strings matching ^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$", so %q is a string no conforming pack can declare as an outcome id.`

// unreachableExpectation names the rule that puts a legal §8.3 disposition
// beyond every conforming pack, or returns "" when none does (ADR-0036).
//
// Every rule here is pack-independent: it holds for all packs, so it can be
// decided from the disposition alone, which is all this tool ever sees.
// Pack-dependent reachability — whether this pack declares that outcome, whether
// it configures that handoff — is not decided here and is not decidable here.
// Two of the three are §8's step order rather than §8.3's grammar, which is why
// the §8.3 decoder admits them and this check, running after it, does not.
//
// The classes are tested in §8's own order, then §5; the first two overlap only
// for an unresolved result retaining both not-applicable and no-match, which is
// ruled out at step 1 and reported there. That precedence is a fixture of its
// own ("unresolved retaining both not-applicable and no-match is reported at
// step 1"), because the two arms differ only in which repair they instruct and
// swapping them changes no status and no code.
func unreachableExpectation(disposition result.Disposition) string {
	reasons := make(map[string]bool, len(disposition.Reasons))
	for _, reason := range disposition.Reasons {
		reasons[reason] = true
	}
	switch {
	case disposition.Kind == "unresolved" && reasons["not-applicable"]:
		return `§8 step 1: "If applicability is false, produce a terminal not-applicable result carrying reason not-applicable and do not evaluate exceptions or rules" is the only step that records that reason, and it reports the halt under kind "not-applicable", so no evaluation produces an unresolved result retaining that reason. Expect kind "not-applicable" instead.`
	case reasons["no-match"] && len(reasons) > 1:
		return `§8 step 10: "If no fallback is present, produce unresolved with reason no-match" is the only step that records "no-match", and every step that records another reason returns before it — step 5 "produce unresolved after all exception effects have been inspected, and do not evaluate normal rules", step 8 "Produce unresolved whenever either reason is present" — so no evaluation produces "no-match" beside another reason.`
	case disposition.Kind == "outcome" && !localIdentifier.MatchString(disposition.OutcomeID):
		return fmt.Sprintf(unreachableOutcomeIDRule, disposition.OutcomeID)
	}
	return ""
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
