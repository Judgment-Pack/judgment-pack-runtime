package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/policy"
)

func policyToolName(operation string) string {
	if operation == "contract" || operation == "schema" {
		return "experimental_get_policy_" + operation
	}
	return "experimental_policy_" + operation
}

func policyDefinition(operation string) map[string]any {
	descriptions := map[string]string{
		"contract":     "Discover the pinned nonnormative RFC 0023 Policy prototype 0.0.2: exact kinds, capabilities and limits. Experimental; no project reads, persistence, evaluation or authority.",
		"init":         "Return a nonnormative Policy prototype starter template for an explicitly selected kind, format and identity. Retained materials are missing and unchecked. The client saves it; no publication, approval or governing-adoption authority is established.",
		"schema":       "Return a self-contained Runtime editing schema derived from pinned nonnormative prototype resources for an explicit kind and exact format 0.0.2. Use validate for strict document and local-reference checks. Offline; no project reads or writes.",
		"canonicalize": "Return canonical Policy prototype document bytes and separate canonical/raw byte digests. Encoding success is not document validation, retained-material verification or authority. Experimental; no files read or written.",
		"digest":       "Return the canonical Policy prototype digest and separate raw-byte identity. Encoding success is not validation or authority. Experimental; no files read or written.",
		"validate":     "Check exact Policy prototype document JSON for strict forms, schema and local references. Does not check retained materials, full Core semantics, evaluation, approval or governing-adoption authority. Experimental; no files read or written.",
		"check":        fmt.Sprintf("Check the exact supplied Policy prototype document and retained-material closure, raw byte identities, structural Core packs and specific references. Does not run full Core semantic validation, evaluate, fetch sources, publish, approve or establish governing-adoption authority. Both arguments are exact JSON text strings, never paths or URLs. Materials use a private Runtime byte transport, not a portable release envelope. The unchanged %d-byte stdio request limit includes its delimiter and JSON/base64 overhead, so not every bundle under the retained-byte limit fits MCP.", maxMessageBytes),
	}
	properties := map[string]any{}
	schema := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	if operation == "init" || operation == "schema" {
		properties["kind"] = map[string]any{"type": "string", "enum": []string{"policy", "governing-adoption"}}
		properties["formatVersion"] = map[string]any{"type": "string", "enum": []string{policy.PrototypeVersion}, "description": "Exact nonnormative prototype version; no stable Policy format is selected by default."}
		schema["required"] = []string{"kind", "formatVersion"}
		if operation == "init" {
			properties["id"] = map[string]any{"type": "string", "description": "Explicit absolute artifact URI identifier."}
			properties["version"] = map[string]any{"type": "string", "description": "Explicit exact three-component artifact version, separate from prototype format."}
			schema["required"] = []string{"kind", "formatVersion", "id", "version"}
		}
	}
	if operation == "canonicalize" || operation == "digest" || operation == "validate" || operation == "check" {
		properties["document"] = map[string]any{"type": "string", "maxLength": policy.MaxDocumentBytes, "description": fmt.Sprintf("Exact document JSON text, never a path. Maximum %d UTF-8 bytes after transport decoding.", policy.MaxDocumentBytes)}
		schema["required"] = []string{"document"}
	}
	if operation == "check" {
		properties["materials"] = map[string]any{"type": "string", "maxLength": policy.MaxMaterialsWireBytes, "description": fmt.Sprintf("Exact retained-material JSON text: {materials:[{content:{bytesDigest,byteLength,mediaType},dataBase64}]}. No paths/URLs. Maximum decoded JSON text %d bytes; stdio's %d-byte line limit can be reached first after JSON/base64 overhead.", policy.MaxMaterialsWireBytes, maxMessageBytes)}
		schema["required"] = []string{"document", "materials"}
	}
	return map[string]any{"name": policyToolName(operation), "description": descriptions[operation], "inputSchema": schema, "annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}
}

func (s *Server) toolPolicy(name string, args json.RawMessage) any {
	command := "mcp " + name
	if bytes.Equal(bytes.TrimSpace(args), []byte("null")) {
		return toolError("Expected an arguments object; null is refused.")
	}
	if name == policyToolName("contract") {
		if m := noArguments(name, args); m != "" {
			return toolError("Policy contract accepts an empty arguments object only.")
		}
		return toolResult(policy.Contract(command))
	}
	var report policy.Report
	var code int
	if name == policyToolName("init") || name == policyToolName("schema") {
		members := []string{"kind", "formatVersion"}
		if name == policyToolName("init") {
			members = append(members, "id", "version")
		}
		if m := exactMembers(name, args, members...); m != "" {
			return toolError("Expected exact Policy selector members, each given once.")
		}
		var input struct {
			Kind          *string `json:"kind"`
			FormatVersion *string `json:"formatVersion"`
			ID            *string `json:"id"`
			Version       *string `json:"version"`
		}
		if json.Unmarshal(args, &input) != nil || input.Kind == nil || input.FormatVersion == nil {
			return toolError("Expected explicit kind and formatVersion strings.")
		}
		if name == policyToolName("schema") {
			report, code = policy.Schema(command, *input.Kind, *input.FormatVersion)
		} else {
			if input.ID == nil || input.Version == nil {
				return toolError("Expected explicit id and version strings.")
			}
			report, code = policy.Init(command, *input.Kind, *input.FormatVersion, *input.ID, *input.Version)
		}
	} else {
		members := []string{"document"}
		if name == policyToolName("check") {
			members = append(members, "materials")
		}
		if m := exactMembers(name, args, members...); m != "" {
			return toolError("Expected document and, for check, materials as exact JSON text strings only.")
		}
		var input struct {
			Document  *string `json:"document"`
			Materials *string `json:"materials"`
		}
		if json.Unmarshal(args, &input) != nil || input.Document == nil {
			return toolError("Expected document as exact JSON text in a string.")
		}
		switch name {
		case "experimental_policy_canonicalize":
			report, code = policy.Canonicalize(command, []byte(*input.Document))
		case "experimental_policy_digest":
			report, code = policy.Digest(command, []byte(*input.Document))
		case "experimental_policy_validate":
			report, code = policy.Validate(command, []byte(*input.Document))
		case "experimental_policy_check":
			if input.Materials == nil {
				return toolError("Expected materials as exact retained-material JSON text in a string.")
			}
			report, code = policy.Check(command, []byte(*input.Document), []byte(*input.Materials), s.engine)
		}
	}
	reply := toolResult(report)
	if code != 0 {
		reply["isError"] = true
	}
	return reply
}
