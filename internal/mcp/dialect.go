package mcp

import (
	"bytes"
	"encoding/json"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/dialect"
)

func dialectDefinition(operation string) map[string]any {
	name := "dialect_" + operation
	if operation == "contract" {
		name = "get_dialect_contract"
	}
	descriptions := map[string]string{
		"contract":     "Describe the pinned dialect contract and implemented operations. Reports which checks are unavailable. Offline; reads no project and writes nothing.",
		"init":         "Return a generic dialect starter as JSON text. The client saves it. No project, acquisition, model, preparation or evaluation is accessed.",
		"canonicalize": "Return canonical dialect bytes and their digest. Checks JSON, integer encoding, declared version and version-3 identity spelling; full structural/semantic validation and execution readiness are not checked. No files are read or written.",
		"digest":       "Return canonical dialect bytes and their digest. Encoding success is not document conformance or preparation verification. No files are read or written.",
	}
	properties := map[string]any{}
	schema := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	if operation == "init" {
		properties["version"] = map[string]any{"type": "integer", "enum": []int{2, 3}, "description": "Dialect encoding version; defaults to 2."}
	}
	if operation == "canonicalize" || operation == "digest" {
		properties["document"] = map[string]any{"type": "string", "description": "Exact dialect JSON text, never a path. Maximum 131072 UTF-8 bytes; the byte bound is enforced after transport decoding.", "maxLength": dialect.MaxDocumentBytes}
		schema["required"] = []string{"document"}
	}
	return map[string]any{"name": name, "description": descriptions[operation], "inputSchema": schema, "annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}
}
func (s *Server) toolDialect(name string, args json.RawMessage) any {
	command := "mcp " + name
	if name == "get_dialect_contract" {
		if m := noArguments(name, args); m != "" {
			return toolError(m)
		}
		return toolResult(dialect.Contract(command))
	}
	var report dialect.Report
	var code int
	if name == "dialect_init" {
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		if bytes.Equal(bytes.TrimSpace(args), []byte("null")) {
			return toolError("Expected an arguments object.")
		}

		if m := exactMembers(name, args, "version"); m != "" {
			return toolError(m)
		}
		var input struct {
			Version json.RawMessage `json:"version"`
		}
		if len(args) > 0 && json.Unmarshal(args, &input) != nil {
			return toolError("Expected an object with an optional integer version.")
		}
		version := 2
		if len(input.Version) > 0 && (string(input.Version) == "null" || json.Unmarshal(input.Version, &version) != nil) {
			return toolError("Dialect version must be an integer.")
		}
		report, code = dialect.Init(command, version)
	} else {
		if m := exactMembers(name, args, "document"); m != "" {
			return toolError(m)
		}
		var input struct {
			Document *string `json:"document"`
		}
		if json.Unmarshal(args, &input) != nil || input.Document == nil {
			return toolError("Expected document as exact JSON text in a string.")
		}
		report, code = dialect.Encode(command, []byte(*input.Document))
	}
	reply := toolResult(report)
	if code != 0 {
		reply["isError"] = true
	}
	return reply
}
