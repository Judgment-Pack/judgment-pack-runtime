package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/dialect"
)

func dialectDefinition(operation string) map[string]any {
	name := "dialect_" + operation
	if operation == "contract" || operation == "schema" || operation == "reference" {
		name = "get_dialect_" + operation
	}
	descriptions := map[string]string{
		"schema":       "Return the Runtime-derived version-2 JSON authoring schema with byte identity. An editing aid: strict carrier and semantic checks still require dialect_validate. Offline; no files read or written.",
		"reference":    "Return the exact embedded pinned draft dialect reference, including unimplemented forms. Inspect capabilities separately; reference text is not an implementation support claim. Offline; no files read or written.",
		"check":        "Validate a version-2 dialect and supplied pack, then compare declared targets with Runtime's over-approximated pack inventory. Distinguishes mapped, partial, acknowledged and missing coverage. Does not check value types, actual values, installed capabilities, profiles or preparations. Both inputs are JSON text, never paths. No execution or persistence.",
		"validate":     "Validate version-2 dialect document forms, limits and internal references. Profile compatibility, source capabilities, pack compatibility and preparation verification are not checked. Returns document-valid only for this explicit scope. No files are read or written.",
		"contract":     "Describe the pinned dialect contract and implemented operations. Reports which checks are unavailable. Offline; reads no project and writes nothing.",
		"init":         "Return a generic dialect starter as JSON text. The client saves it. No project, acquisition, model, preparation or evaluation is accessed.",
		"canonicalize": "Return canonical dialect bytes and their digest. Checks JSON, integer encoding, declared version and version-3 identity spelling; full structural/semantic validation and execution readiness are not checked. No files are read or written.",
		"digest":       "Return canonical dialect bytes and their digest. Encoding success is not document conformance or preparation verification. No files are read or written.",
	}
	properties := map[string]any{}
	schema := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	if operation == "init" || operation == "schema" {
		versions := []int{2, 3}
		if operation == "schema" {
			versions = []int{2}
		}
		properties["version"] = map[string]any{"type": "integer", "enum": versions, "description": "Dialect version; defaults to 2."}
	}
	if operation == "canonicalize" || operation == "digest" || operation == "validate" || operation == "check" {
		properties["document"] = map[string]any{"type": "string", "description": fmt.Sprintf("Exact dialect JSON text, never a path. Maximum %d UTF-8 bytes; the byte bound is enforced after transport decoding.", dialect.MaxDocumentBytes), "maxLength": dialect.MaxDocumentBytes}
		schema["required"] = []string{"document"}
	}
	if operation == "check" {
		properties["pack"] = map[string]any{"type": "string", "description": fmt.Sprintf("Exact pack JSON text, never a path. Maximum %d UTF-8 bytes; the byte bound is enforced after transport decoding.", dialect.MaxPackBytes), "maxLength": dialect.MaxPackBytes}
		schema["required"] = []string{"document", "pack"}
	}
	return map[string]any{"name": name, "description": descriptions[operation], "inputSchema": schema, "annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}
}
func (s *Server) toolDialect(name string, args json.RawMessage) any {
	command := "mcp " + name
	if name == "get_dialect_contract" || name == "get_dialect_reference" {
		if m := noArguments(name, args); m != "" {
			return toolError(m)
		}
		if name == "get_dialect_reference" {
			return toolResult(dialect.Reference(command))
		}
		return toolResult(dialect.Contract(command))
	}
	var report dialect.Report
	var code int
	if name == "dialect_init" || name == "get_dialect_schema" {
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
		if name == "get_dialect_schema" {
			report, code = dialect.Schema(command, version)
		} else {
			report, code = dialect.Init(command, version)
		}
	} else {
		members := []string{"document"}
		if name == "dialect_check" {
			members = append(members, "pack")
		}
		if m := exactMembers(name, args, members...); m != "" {
			return toolError(m)
		}
		var input struct {
			Document *string `json:"document"`
			Pack     *string `json:"pack"`
		}
		if json.Unmarshal(args, &input) != nil || input.Document == nil {
			return toolError("Expected document as exact JSON text in a string.")
		}
		if name == "dialect_check" {
			if input.Pack == nil {
				return toolError("Expected pack as exact JSON text in a string.")
			}
			report, code = dialect.Check(command, []byte(*input.Document), []byte(*input.Pack), s.engine)
		} else if name == "dialect_validate" {
			report, code = dialect.Validate(command, []byte(*input.Document))
		} else {
			report, code = dialect.Encode(command, []byte(*input.Document))
		}
	}
	reply := toolResult(report)
	if code != 0 {
		reply["isError"] = true
	}
	return reply
}
