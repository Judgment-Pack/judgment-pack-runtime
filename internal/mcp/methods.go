package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

const methodVersion = "1"
const methodResourcePrefix = "judgment-pack://methods/1/"
const maxMethodBytes = 64 << 10
const terminalPrerequisite = "This method's optional terminal operations require separately admitted host terminal tools. Its derived profile covers only the declared Runtime MCP tools. A host must refuse unresolved external prerequisites; this skill grants no terminal or other tool."

// Ordered complete MCP dependencies of the unchanged, empty-argument prompt templates.
var methodTools = map[string][]string{
	"author_pack":         {"get_schema", "list_examples", "get_example", "validate", "experimental_evaluate"},
	"fix_pack":            {"validate", "experimental_test_packs"},
	"test_pack":           {"experimental_evaluate", "experimental_test_packs"},
	"present_pack":        {"get_pack", "experimental_evaluate"},
	"explain_disposition": {"experimental_evaluate", "get_pack"},
	"author_graph":        {"get_pack", "experimental_test_graphs", "experimental_list_graphs", "experimental_get_graph"},
}
var methodOrder = []string{"author_pack", "fix_pack", "test_pack", "present_pack", "explain_disposition", "author_graph"}
var profileOperationOrder = []string{"discover", "read", "answer", "check", "transform", "evaluate", "propose", "act", "run", "own-state", "ask"}

// methodBundle matches Agent skillDigest exactly; profiles are recomputed, never authored.
func methodBundle(name, description, instructions string, tools []string, resources []any, admitted map[string]toolTaxonomy) (map[string]any, map[string]any, error) {
	dependencies := make([]any, 0, len(tools))
	operations := map[string]bool{}
	effect := "pure"
	for _, name := range tools {
		t, ok := admitted[name]
		if !ok || !t.valid() {
			return nil, nil, fmt.Errorf("method references an unadmitted Runtime tool")
		}
		dependencies = append(dependencies, map[string]any{"name": name, "taxonomy": t.metadata()})
		operations[t.Operation] = true
		if t.Effect == "act" || (t.Effect == "read" && effect == "pure") {
			effect = t.Effect
		}
	}
	ops := []string{}
	for _, op := range profileOperationOrder {
		if operations[op] {
			ops = append(ops, op)
		}
	}
	bundle := map[string]any{"kind": "method", "name": name, "description": description, "instructions": instructions, "tools": tools, "resources": resources}
	if len(tools) > 0 {
		bundle["dependencies"] = dependencies
	}
	return bundle, map[string]any{"effect": effect, "operations": ops}, nil
}
func methodDigest(bundle map[string]any) (string, error) {
	canonical, err := jcs.Encode(bundle)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func publishedMethods(definitions []map[string]any, declarations map[string]toolTaxonomy) ([]map[string]any, error) {
	// Bind dependencies only to tools that the real served list successfully classified.
	tools, err := classifiedTools(definitions, declarations)
	if err != nil {
		return nil, err
	}
	admitted := map[string]toolTaxonomy{}
	for _, tool := range tools {
		name := tool["name"].(string)
		admitted[name] = declarations[name]
	}
	prompts := map[string]promptDefinition{}
	for _, prompt := range promptDefinitions() {
		prompts[prompt.name] = prompt
	}
	out := make([]map[string]any, 0, len(methodOrder))
	for _, name := range methodOrder {
		prompt, ok := prompts[name]
		if !ok {
			return nil, fmt.Errorf("method prompt is unavailable")
		}
		deps, ok := methodTools[name]
		if !ok || len(deps) == 0 {
			return nil, fmt.Errorf("method dependency declaration is unavailable")
		}
		resources := []any{}
		if name == "author_graph" {
			resources = append(resources, map[string]any{"name": "external-terminal-prerequisite", "text": terminalPrerequisite})
		}
		bundle, profile, err := methodBundle(name, prompt.description, prompt.build(map[string]string{}), deps, resources, admitted)
		if err != nil {
			return nil, err
		}
		digest, err := methodDigest(bundle)
		if err != nil {
			return nil, err
		}
		definition := map[string]any{}
		for key, value := range bundle {
			definition[key] = value
		}
		definition["version"] = methodVersion
		definition["publisher"] = "judgment-pack-runtime"
		definition["digest"] = digest
		definition["profile"] = profile
		definition["uri"] = methodResourcePrefix + name + "/" + strings.TrimPrefix(digest, "sha256:")
		encoded, err := json.Marshal(definition)
		if err != nil || len(encoded) > maxMethodBytes {
			return nil, fmt.Errorf("method definition exceeds publication bounds")
		}
		out = append(out, definition)
	}
	return out, nil
}
func methodResources(raw json.RawMessage, read bool) (map[string]any, *rpcError) {
	allowed := []string{"_meta"}
	if read {
		allowed = append(allowed, "uri")
	}
	if len(raw) > 0 {
		trimmed := strings.TrimSpace(string(raw))
		if !strings.HasPrefix(trimmed, "{") || membersOnce(raw, allowed) != "" || exactMembers("method resources", raw, allowed...) != "" {
			return nil, &rpcError{Code: codeInvalidParams, Message: "Expected exact method resource parameters; list takes no cursor and read requires uri."}
		}
	}
	var params struct {
		URI string `json:"uri"`
	}
	if read && (json.Unmarshal(raw, &params) != nil || params.URI == "" || len(params.URI) > 256) {
		return nil, &rpcError{Code: codeInvalidParams, Message: "Expected one exact method resource URI."}
	}
	methods, err := publishedMethods(toolDefinitions(), toolDeclarations)
	if err != nil {
		return nil, &rpcError{Code: codeInternal, Message: "Method resources are unavailable."}
	}
	resources := make([]map[string]any, 0, len(methods))
	for _, method := range methods {
		if read {
			if method["uri"] != params.URI {
				continue
			}
			encoded, _ := json.Marshal(method)
			return map[string]any{"contents": []map[string]any{{"uri": params.URI, "mimeType": "application/json", "text": string(encoded)}}}, nil
		}
		resources = append(resources, map[string]any{"uri": method["uri"], "name": method["name"], "description": method["description"], "mimeType": "application/json"})
	}
	if read {
		return nil, &rpcError{Code: codeInvalidParams, Message: "Unknown method resource URI."}
	}
	return map[string]any{"resources": resources}, nil
}
