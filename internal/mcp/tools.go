package mcp

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/describe"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// toolDefinitions is the tools/list payload. Every tool wraps a read-only core
// operation and evaluates nothing.
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
