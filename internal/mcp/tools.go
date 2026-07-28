package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/describe"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// toolDefinitions is the tools/list payload. Every tool wraps a read-only core
// operation; all evaluate nothing except experimental_evaluate, the evaluator on
// this runtime's experimental surface (ADR-0007). No description here states a
// conformance claim: the claim is stated, in full and only, in CONFORMANCE.md
// (ADR-0011), and these descriptions reference it.
func toolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "validate",
			"description": "Validate one JPS document for carrier, structural, and semantic conformance. It does not evaluate rules, choose an outcome, or authorize anything.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"document"},
				"properties": map[string]any{
					"document": map[string]any{"type": "string", "description": "The JPS document to validate, as JSON text."},
					"through":  map[string]any{"type": "string", "enum": []string{"carrier", "structural", "semantic"}, "description": "Last validation layer to run; defaults to semantic."},
				},
			},
		},
		{
			"name":        "test_conformance",
			"description": "Run a version-pinned JPS conformance corpus; the bundled corpus by default.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"suite":        map[string]any{"type": "string", "description": "Optional path to a local suite directory or manifest.json; omit for the bundled corpus."},
					"spec_version": map[string]any{"type": "string", "description": "Optional exact JPS version; defaults to the bundled draft."},
				},
			},
		},
		{
			"name":        "get_schema",
			"description": "Return the exact bundled JPS JSON Schema for a specification version, with its digest and byte size.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"spec_version": map[string]any{"type": "string", "description": "Optional exact JPS version; defaults to the bundled draft."},
				},
			},
		},
		{
			"name":        "describe_runtime",
			"description": "Report this runtime's version, the specification versions it supports, and the provenance of its bundled artifacts.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties":           map[string]any{},
			},
		},
		{
			"name":        "list_examples",
			"description": "List the bundled valid JPS example documents: version-pinned conformance fixtures the runtime embeds and digest-locks, offered read-only as starting points for authoring. They are not authored templates. Use get_example to fetch one by name.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties":           map[string]any{},
			},
		},
		{
			"name":        "get_example",
			"description": "Return one bundled valid JPS example document by name, as JSON text, with its digest and byte size. These are version-pinned conformance fixtures, not authored templates; copy one into a document of your own to start authoring, then validate. Call list_examples for the available names.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"name"},
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "The example name, as reported by list_examples (for example, minimal-expense-approval)."},
				},
			},
		},
		{
			"name":        "experimental_evaluate",
			"description": "EXPERIMENTAL SURFACE (ADR-0007): apply the JPS Core §§7-8 resolution model to one conformant pack and one facts document, returning the §8.3 portable disposition (kind, outcomeId, reasons, handoff) and a trace. The disposition is serialized in its RFC 8785 canonical form; a refused evaluation reports its §8.4 error class and no disposition. Only a pack declaring specVersion 0.2.0-draft is evaluated: JPS §11 makes the value exact and requires an unedited 0.1.0-draft pack to be re-declared -- one edit, the specVersion string -- before an implementation claiming this draft evaluates it, so any other version is refused as pack-not-conformant in the preflight phase. This runtime's conformance claim is stated, in full and only, in the repository's CONFORMANCE.md; this description states no claim, and the payload carries a conformanceClaimReference member pointing at that file. Whatever that claim says, it is about this implementation and NOT about the pack you pass, the facts you supply, or whether acting on the returned disposition is correct, permitted, or safe (§3.5). It authorizes nothing, executes nothing, and this surface may change or be removed without compatibility promise.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"pack", "facts"},
				"properties": map[string]any{
					"pack":                 map[string]any{"type": "string", "description": "The JPS document to evaluate, as JSON text. It must have full document conformance; a non-conformant pack is refused."},
					"facts":                map[string]any{"type": "string", "description": "One JSON facts document, as JSON text; fact.path pointers resolve against it."},
					"evidence":             map[string]any{"type": "string", "description": "Optional tri-state evidence availability, as JSON text: an object mapping declared evidence-requirement ids to \"present\", \"absent\", or \"unknown\". An omitted id is unknown. Omit this key entirely to supply no document at all, which makes every declared requirement unknown; a key present with an empty string is a supplied empty document, which is not a JSON text and is refused as malformed-input."},
					"supported_extensions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Extension names this consumer supports."},
				},
			},
		},
	}
}

func (s *Server) callTool(rawParams json.RawMessage) (any, *rpcError) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(rawParams, &params); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "Invalid tools/call parameters."}
	}
	switch params.Name {
	case "validate":
		return s.toolValidate(params.Arguments), nil
	case "test_conformance":
		return s.toolTestConformance(params.Arguments), nil
	case "get_schema":
		return s.toolGetSchema(params.Arguments), nil
	case "describe_runtime":
		return s.toolDescribeRuntime(), nil
	case "list_examples":
		return s.toolListExamples(), nil
	case "get_example":
		return s.toolGetExample(params.Arguments), nil
	case "experimental_evaluate":
		return s.toolExperimentalEvaluate(params.Arguments), nil
	default:
		return nil, &rpcError{Code: codeInvalidParams, Message: "Unknown tool: " + params.Name}
	}
}

func (s *Server) toolValidate(rawArgs json.RawMessage) any {
	var args struct {
		Document string `json:"document"`
		Through  string `json:"through"`
	}
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return toolError(`The "validate" arguments must be an object with a string "document".`)
	}
	if args.Document == "" {
		return toolError(`The "document" argument is required: pass the JPS document as JSON text.`)
	}
	through := args.Through
	if through == "" {
		through = "semantic"
	}
	if through != "carrier" && through != "structural" && through != "semantic" {
		return toolError(`"through" must be carrier, structural, or semantic.`)
	}
	output, operational := s.engine.Validate([]byte(args.Document), validation.Options{Through: through, Limits: carrier.DefaultLimits()})
	if operational != nil {
		return toolError(operational.Message)
	}
	return toolResult(output)
}

func (s *Server) toolTestConformance(rawArgs json.RawMessage) any {
	var args struct {
		Suite       string `json:"suite"`
		SpecVersion string `json:"spec_version"`
	}
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return toolError(`Invalid "test_conformance" arguments.`)
		}
	}
	output, operational := s.runner.Run(args.Suite, args.SpecVersion)
	if operational != nil {
		return toolError(operational.Message)
	}
	return toolResult(output)
}

func (s *Server) toolGetSchema(rawArgs json.RawMessage) any {
	var args struct {
		SpecVersion string `json:"spec_version"`
	}
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return toolError(`Invalid "get_schema" arguments.`)
		}
	}
	version := args.SpecVersion
	if version == "" {
		version = artifacts.DraftVersion
	}
	set, err := artifacts.Load(version)
	if err != nil {
		return toolError("The exact JPS specification version is not bundled with this runtime.")
	}
	schemaBytes, err := set.Schema()
	if err != nil {
		return toolError("The bundled schema is unavailable.")
	}
	meta, err := describe.Schema(set, version, "mcp get_schema", schemaBytes)
	if err != nil {
		return toolError("The bundled schema metadata is invalid.")
	}
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(schemaBytes)}},
		"structuredContent": meta,
		"isError":           false,
	}
}

func (s *Server) toolDescribeRuntime() any {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		return toolError("The bundled artifact metadata is unavailable.")
	}
	return toolResult(describe.Runtime(set, "mcp describe_runtime"))
}

func (s *Server) toolListExamples() any {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		return toolError("The bundled artifact metadata is unavailable.")
	}
	output, err := describe.Examples(set, "mcp list_examples")
	if err != nil {
		return toolError("The bundled examples are unavailable.")
	}
	return toolResult(output)
}

func (s *Server) toolGetExample(rawArgs json.RawMessage) any {
	var args struct {
		Name string `json:"name"`
	}
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return toolError(`The "get_example" arguments must be an object with a string "name".`)
		}
	}
	if args.Name == "" {
		return toolError(`The "name" argument is required; call list_examples for the available names.`)
	}
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		return toolError("The bundled artifact metadata is unavailable.")
	}
	meta, data, err := describe.Example(set, args.Name, "mcp get_example")
	if err != nil {
		var unknown *artifacts.UnknownExampleError
		if errors.As(err, &unknown) {
			return toolError(fmt.Sprintf("No bundled example is named %q; call list_examples for the available names.", args.Name))
		}
		return toolError("The bundled example is unavailable.")
	}
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(data)}},
		"structuredContent": meta,
		"isError":           false,
	}
}

// evaluateCommand names this surface in every payload it produces, exactly as
// the describe package's command strings do.
const evaluateCommand = "mcp experimental_evaluate"

// evaluateArguments is one experimental_evaluate invocation as it arrived on the
// wire. Each document is held as raw JSON rather than as a string because §8.2
// gives an omitted document and a supplied empty one two different meanings — an
// omitted evidence document is the implicit empty object and not an error, while
// empty bytes are not a carrier-conforming JSON text — and a string field
// collapses both to "". Holding the raw value keeps presence separate from value,
// so this surface reaches the preflight with the same distinction the CLI's
// --evidence flag has, and an explicit null stays distinguishable from an absent
// key.
type evaluateArguments struct {
	Pack                json.RawMessage `json:"pack"`
	Facts               json.RawMessage `json:"facts"`
	Evidence            json.RawMessage `json:"evidence"`
	SupportedExtensions []string        `json:"supported_extensions"`
}

// textArgument decodes one document argument, returning its text, whether the key
// was present at all, and an argument-type message when the value is not a JSON
// string. The declared input schema says string, so an explicit null — or a
// number, object, or array — is a bad invocation and never an evaluation input;
// omitting the key is the only form absence takes. A present empty string is a
// value, and is returned as one so the §8.2 preflight classes it.
func textArgument(name string, raw json.RawMessage) (string, bool, string) {
	if len(raw) == 0 {
		return "", false, ""
	}
	argumentError := fmt.Sprintf("The %q argument must be a JSON string; null and every other type are rejected. Omit the key to leave the argument unsupplied.", name)
	// The null literal is rejected by hand: unmarshaling a JSON null into a string
	// is a no-op rather than an error, so decoding alone would silently turn an
	// explicit null into the empty string and, for evidence, into an absence §8.2
	// says only an omitted key expresses.
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", true, argumentError
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", true, argumentError
	}
	return text, true, ""
}

// toolExperimentalEvaluate runs the experimental §§7-8 evaluator (ADR-0007).
// An evaluation failure — a non-conformant pack, a malformed input, an
// undeclared evidence key — is an in-band tool error carrying the §8.4 envelope;
// a produced disposition of any kind is a successful call.
//
// An absent required key is an invocation failure, which §8.4 does not class at
// all: the call never became an evaluation. A key present with an empty string is
// a supplied document, so it enters the §8.2 preflight at that input's own place
// in the order and is classed there — an empty pack is pack-not-conformant, and
// an empty facts or evidence document is malformed-input.
func (s *Server) toolExperimentalEvaluate(rawArgs json.RawMessage) any {
	var args evaluateArguments
	if len(rawArgs) > 0 {
		// Strict decoding honors the declared additionalProperties: false — a
		// misspelled key (say "evidnce") must be an error, not a silently
		// different disposition.
		decoder := json.NewDecoder(bytes.NewReader(rawArgs))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&args); err != nil {
			return toolError(`The "experimental_evaluate" arguments must be an object with string "pack" and "facts", optional string "evidence", and optional "supported_extensions" (an array of strings); unknown keys are rejected.`)
		}
	}
	pack, packPresent, argumentError := textArgument("pack", args.Pack)
	if argumentError != "" {
		return toolError(argumentError)
	}
	facts, factsPresent, argumentError := textArgument("facts", args.Facts)
	if argumentError != "" {
		return toolError(argumentError)
	}
	evidence, evidenceSupplied, argumentError := textArgument("evidence", args.Evidence)
	if argumentError != "" {
		return toolError(argumentError)
	}
	if !packPresent {
		return toolError(`The "pack" argument is required: pass the JPS document as JSON text.`)
	}
	if !factsPresent {
		return toolError(`The "facts" argument is required: pass one JSON facts document as JSON text.`)
	}
	evaluator := evaluation.NewEngine(s.engine)
	output, failure := evaluator.EvaluateWith([]byte(pack), []byte(facts), []byte(evidence), evaluation.Options{
		Command:             evaluateCommand,
		SupportedExtensions: args.SupportedExtensions,
		// The evidence document is supplied exactly when the key was present, empty
		// string included: §8.2's absence is the omitted document, and empty bytes are
		// the malformed-input error the preflight reaches in its own place in the order.
		EvidenceSupplied: evidenceSupplied,
	})
	if failure != nil {
		return evaluationToolError(evaluateCommand, failure)
	}
	return toolResult(output)
}

// evaluationToolError reports one refused evaluation on this surface. The
// structured content is the same shared envelope the CLI writes — the §8.4 class
// and phase, the version of the evaluator contract that assigned them, and this
// runtime's finer JPS-* code beside them as the detail §8.4 admits — so a calling
// model reads the machine-readable identity instead of parsing it back out of
// prose, and the text content stays for a client that reads only text. Neither
// form carries a disposition, ever (§8.4): the envelope type has no such member.
func evaluationToolError(command string, failure *evaluation.Failure) map[string]any {
	status := "error"
	if failure.ExitCode == result.ExitUnsupported {
		status = "unsupported"
	}
	// A refusal §8.4 does not classify — a bad invocation, an internal fault —
	// carries no class, exactly as on the CLI, and is reported as an ordinary
	// operational failure.
	envelope := result.NewOperationalResult(command, status, failure.Code, failure.Message)
	if failure.Class != "" {
		envelope = result.NewEvaluationError(command, status, failure.Class, failure.Phase, failure.Code, failure.Message)
	}
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": evaluationFailureMessage(failure)}},
		"structuredContent": envelope,
		"isError":           true,
	}
}

// evaluationFailureMessage names the JPS §8.4 evaluation-error class and phase
// beside this runtime's finer code, so a calling model reads the same coarse
// identity the CLI reports rather than a message it has to classify itself.
func evaluationFailureMessage(failure *evaluation.Failure) string {
	if failure.Class == "" {
		return failure.Message
	}
	return fmt.Sprintf("%s (evaluation error class: %s; phase: %s; code: %s)", failure.Message, failure.Class, failure.Phase, failure.Code)
}

// toolResult wraps a versioned core payload as an MCP tool result: the payload
// as structured content, and its JSON as text for a client that reads only
// text. A reported "invalid" document is a successful call, not a tool error.
func toolResult(structured any) map[string]any {
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": jsonText(structured)}},
		"structuredContent": structured,
		"isError":           false,
	}
}

// toolError reports a failure to execute the tool itself -- bad arguments or an
// operational failure -- in band so the calling model can react.
func toolError(message string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": message}},
		"isError": true,
	}
}

func jsonText(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}
