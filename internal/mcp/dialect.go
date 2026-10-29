package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/dialect"
	"strings"
)

func dialectDefinition(operation string) map[string]any {
	name := "dialect_" + operation
	if operation == "contract" || operation == "schema" || operation == "reference" {
		name = "get_dialect_" + operation
	}
	descriptions := map[string]string{
		"schema":       "Return the Runtime-derived legacy version-2/version-3 or named 2.0.0/3.0.0 JSON authoring schema with byte identity. An editing aid: strict carrier and semantic checks still require dialect_validate. Offline; no files read or written.",
		"reference":    "Return the exact embedded pinned draft dialect reference, including unimplemented forms. Inspect capabilities separately; reference text is not an implementation support claim. Offline; no files read or written.",
		"check":        "Validate a supported legacy or named dialect and supplied pack, then compare declared targets with Runtime's over-approximated pack inventory. Distinguishes mapped, partial, acknowledged and missing coverage. Does not check value types, actual values, installed capabilities, profiles or preparations. Both inputs are JSON text, never paths. No execution or persistence.",
		"validate":     "Validate legacy version-2/version-3 and named 2.0.0/3.0.0 dialect document forms, limits and internal references for operation and selected-file sources, plus version-3 research sources. Version-3 operation and research declarations may be unbound; no binding is verified. Research checks cover typed fields, capabilities and reconciliation reads. HTTP, SQL and web source kinds are explicitly unsupported. Profile compatibility, source capabilities, pack compatibility and preparation verification are not checked. Returns document-valid only for this explicit scope. No files are read or written.",
		"contract":     "Describe the pinned dialect contract and implemented operations. Reports which checks are unavailable. Offline; reads no project and writes nothing.",
		"init":         "Return a generic dialect starter as JSON text. The client saves it. No project, acquisition, model, preparation or evaluation is accessed.",
		"canonicalize": "Return canonical dialect bytes and their digest. Checks JSON, integer encoding, declared version and version-3 identity spelling; full structural/semantic validation and execution readiness are not checked. No files are read or written.",
		"digest":       "Return canonical dialect bytes and their digest. Encoding success is not document conformance or preparation verification. No files are read or written.",
	}
	properties := map[string]any{}
	schema := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	if operation == "init" || operation == "schema" {
		versions := []int{2, 3}
		properties["formatVersion"] = map[string]any{"type": "string", "enum": []string{"2.0.0", "3.0.0"}}
		if operation == "init" {
			properties["id"] = map[string]any{"type": "string"}
			properties["artifactVersion"] = map[string]any{"type": "string"}
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
	if operation == "contract" || operation == "canonicalize" || operation == "digest" || operation == "validate" || operation == "check" {
		dialectDraftSchema(operation, schema, properties)
		descriptions[operation] += fmt.Sprintf(" Explicit compatibility draftContract/draftProfile selects the installed leaf document/frame reader; no preparation, authenticity or execution claim. The %d-byte stdio request limit includes JSON escaping and delimiter; decoded byte limits do not guarantee transport admission.", maxMessageBytes)
	}
	return map[string]any{"name": name, "description": descriptions[operation], "inputSchema": schema}
}
func (s *Server) toolDialect(name string, args json.RawMessage) any {
	command := "mcp " + name
	if name == "get_dialect_contract" || name == "dialect_canonicalize" || name == "dialect_digest" || name == "dialect_validate" || name == "dialect_check" {
		var fields map[string]json.RawMessage
		if json.Unmarshal(args, &fields) == nil {
			_, selector := fields["draftContract"]
			_, profile := fields["draftProfile"]
			_, frame := fields["retainedFrame"]
			if selector || profile || frame {
				return s.toolRetainedColumnDraft(name, args)
			}
		}
	}
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

		members := []string{"version", "formatVersion"}
		if name == "dialect_init" {
			members = append(members, "id", "artifactVersion")
		}
		if m := exactMembers(name, args, members...); m != "" {
			return toolError(m)
		}
		var input struct {
			Version         json.RawMessage `json:"version"`
			FormatVersion   *string         `json:"formatVersion"`
			ID              string          `json:"id"`
			ArtifactVersion string          `json:"artifactVersion"`
		}
		var rawFields map[string]json.RawMessage
		if json.Unmarshal(args, &rawFields) != nil {
			return toolError("Expected an arguments object.")
		}
		for _, field := range []string{"formatVersion", "id", "artifactVersion"} {
			if value, exists := rawFields[field]; exists && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return toolError("Named format arguments must be strings, not null.")
			}
		}
		if len(args) > 0 && json.Unmarshal(args, &input) != nil {
			return toolError("Expected an object with an optional integer version.")
		}
		if input.FormatVersion != nil {
			if len(input.Version) > 0 {
				return toolError("Choose a legacy version or named format, not both.")
			}
			if name == "get_dialect_schema" {
				report, code = dialect.ArtifactSchema(command, *input.FormatVersion)
			} else {
				report, code = dialect.InitArtifact(command, *input.FormatVersion, input.ID, input.ArtifactVersion)
			}
			reply := toolResult(report)
			if code != 0 {
				reply["isError"] = true
			}
			return reply
		}
		if _, hasID := rawFields["id"]; hasID || rawFields["artifactVersion"] != nil {
			return toolError("Artifact identity requires formatVersion.")
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

// Draft branches keep the ordinary bound conditional on absence of all selectors.
// maxLength is a transport editing aid; UTF-8 byte ceilings are enforced by readers.
func dialectDraftSchema(operation string, schema, properties map[string]any) {
	properties["draftContract"] = map[string]any{"type": "string", "enum": []string{dialect.RetainedColumnDraftSelector}}
	properties["draftProfile"] = map[string]any{"type": "string", "enum": []string{"base", "extended"}}
	selectorMembers := []any{map[string]any{"required": []string{"draftContract"}}, map[string]any{"required": []string{"draftProfile"}}}
	if operation == "validate" {
		properties["retainedFrame"] = map[string]any{"type": "boolean", "description": "With exact installed leaf selection, check document text as a standalone retained frame; no conversion or provenance verification."}
		selectorMembers = append(selectorMembers, map[string]any{"required": []string{"retainedFrame"}})
	}
	legacy := map[string]any{"not": map[string]any{"anyOf": selectorMembers}}
	if operation != "contract" {
		document := properties["document"].(map[string]any)
		delete(document, "maxLength")
		document["description"] = "Exact JSON text, never a path. The selected legacy/leaf branch bounds decoded UTF-8 bytes; stdio escaping and delimiter count against the separate request envelope."
		legacy["properties"] = map[string]any{"document": map[string]any{"maxLength": dialect.MaxDocumentBytes}}
	}
	branches := []any{legacy}
	for _, profile := range []string{"base", "extended"} {
		docBound, frameBound := dialect.RetainedColumnDraftDocumentBytes, dialect.RetainedColumnDraftFrameBytes
		if profile == "extended" {
			docBound, frameBound = dialect.RetainedColumnDraftExtendedDocumentBytes, dialect.RetainedColumnDraftExtendedFrameBytes
		}
		variants := 1
		if operation == "validate" {
			variants = 2
		}
		for variant := 0; variant < variants; variant++ {
			props := map[string]any{"draftContract": map[string]any{"const": dialect.RetainedColumnDraftSelector}, "draftProfile": map[string]any{"const": profile}}
			required := []string{"draftContract", "draftProfile"}
			bound := docBound
			if operation == "validate" {
				props["retainedFrame"] = map[string]any{"const": variant == 1}
				if variant == 1 {
					bound = frameBound
					required = append(required, "retainedFrame")
				}
			}
			if operation != "contract" {
				props["document"] = map[string]any{"maxLength": bound}
			}
			branches = append(branches, map[string]any{"required": required, "properties": props})
		}
	}
	schema["oneOf"] = branches
}

func (s *Server) toolRetainedColumnDraft(name string, args json.RawMessage) any {
	command := "mcp " + name
	members := []string{"draftContract", "draftProfile"}
	if name != "get_dialect_contract" {
		members = append(members, "document")
	}
	if name == "dialect_check" {
		members = append(members, "pack")
	}
	if name == "dialect_validate" {
		members = append(members, "retainedFrame")
	}
	if message := exactMembers(name, args, members...); message != "" {
		return toolError(message)
	}
	var input struct {
		DraftContract *string `json:"draftContract"`
		DraftProfile  *string `json:"draftProfile"`
		RetainedFrame *bool   `json:"retainedFrame"`
		Document      *string `json:"document"`
		Pack          *string `json:"pack"`
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(args, &fields) != nil || json.Unmarshal(args, &input) != nil {
		return toolError("Expected exact installed leaf selectors and JSON text arguments.")
	}
	for _, key := range []string{"draftContract", "draftProfile", "retainedFrame"} {
		if value, present := fields[key]; present && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return toolError("Leaf selection members cannot be null.")
		}
	}
	selector, profile := "", ""
	if input.DraftContract != nil {
		selector = *input.DraftContract
	}
	if input.DraftProfile != nil {
		profile = *input.DraftProfile
	}
	selected, selection, code := dialect.SelectRetainedColumnDraft(command, selector, profile)
	var output any = selected
	if code == 0 {
		if name == "get_dialect_contract" {
			selected, code = dialect.ContractRetainedColumnDraft(command, selection)
			output = selected
		} else {
			if input.Document == nil {
				return toolError("Expected document as exact JSON text in a string.")
			}
			switch name {
			case "dialect_check":
				if input.Pack == nil {
					return toolError("Expected pack as exact JSON text in a string.")
				}
				selected, code = dialect.CheckRetainedColumnDraft(command, []byte(*input.Document), []byte(*input.Pack), s.engine, selection)
				output = selected
			case "dialect_validate":
				if input.RetainedFrame != nil && *input.RetainedFrame {
					frame, frameCode := dialect.ValidateRetainedColumnDraftFrame(command, []byte(*input.Document), selection)
					output, code = frame, frameCode
				} else {
					selected, code = dialect.ValidateRetainedColumnDraft(command, []byte(*input.Document), selection)
					output = selected
				}
			default:
				selected, code = dialect.EncodeRetainedColumnDraft(command, []byte(*input.Document), selection)
				output = selected
			}
		}
	}
	// The outer MCP encoder already emits literal HTML. Apply the same policy
	// to this selected Draft report's text representation: a second optional
	// HTML escape pass can make a valid Base frame's dual response exceed the
	// stdio client envelope. JSON quoting, controls and UTF-8 content remain
	// exact, and the structured report and retained-byte digest are unchanged.
	var text bytes.Buffer
	encoder := json.NewEncoder(&text)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(output); err != nil {
		return toolError("The selected leaf report could not be encoded.")
	}
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": strings.TrimSuffix(text.String(), "\n")}},
		"structuredContent": output,
		"isError":           code != 0,
	}
}
