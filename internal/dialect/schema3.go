package dialect

import "encoding/json"

// The authoring aid is self-contained and derived from the version-2 aid so
// shared forms cannot drift. Its identity is distinct; version-2 bytes stay fixed.
func version3AuthoringSchema() string {
	var schema map[string]any
	if err := json.Unmarshal([]byte(version2Schema), &schema); err != nil {
		panic(err) // Invalid embedded source is a build defect, never user input.
	}
	schema["$id"] = "urn:judgmentpack:runtime:dialect:3:authoring-schema:1"
	schema["title"] = "Dialect version 3 — Runtime authoring schema"
	schema["$comment"] = "Runtime-derived editing aid from Spec commit " + SpecRevision + "; not a normative schema. Supports operation and research (bound or unbound), and selected-file. Use dialect validate for strict carrier, spelling, scope, reference and aggregate checks. Does not bind sources or check profiles, preparations, capability availability or execution readiness."
	properties := schema["properties"].(map[string]any)
	properties["version"] = map[string]any{"const": 3}
	for _, name := range []string{"provider", "facts", "evidence"} {
		delete(properties, name)
	}
	applyStrictOmissions(schema)
	defs := schema["$defs"].(map[string]any)
	source := defs["source"].(map[string]any)
	forms := source["oneOf"].([]any)
	// Clone the bound operation rather than weakening it into optional pins.
	encoded, err := json.Marshal(forms[0])
	if err != nil {
		panic(err)
	}
	var unbound map[string]any
	if err = json.Unmarshal(encoded, &unbound); err != nil {
		panic(err)
	}
	p := unbound["properties"].(map[string]any)
	delete(p, "profile")
	delete(p, "profileDigest")
	for _, name := range []string{"platform", "operation", "record"} {
		p[name] = map[string]any{"type": "string", "minLength": 1}
	}
	p["shape"] = map[string]any{"enum": []string{"airbyte", "mcp", "http"}}
	unbound["required"] = []string{"name", "kind", "read", "platform", "operation", "shape", "record", "maxAge", "arguments"}
	source["oneOf"] = append(append(forms, unbound), researchAuthoringSchema(defs))
	encoded, err = json.MarshalIndent(schema, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(encoded) + "\n"
}

func applyStrictOmissions(schema map[string]any) {
	properties := schema["properties"].(map[string]any)
	for _, name := range []string{"sources", "unmapped", "unmappedEvidence"} {
		properties[name].(map[string]any)["minItems"] = 1
	}
	defs := schema["$defs"].(map[string]any)
	caseProperties := defs["case"].(map[string]any)["properties"].(map[string]any)
	caseProperties["parameters"].(map[string]any)["minProperties"] = 1
	admissions := defs["admits"].(map[string]any)["properties"].(map[string]any)
	for _, name := range []string{"facts", "evidence"} {
		admissions[name].(map[string]any)["minProperties"] = 1
	}
	source := defs["source"].(map[string]any)
	forms := source["oneOf"].([]any)
	for _, form := range forms {
		p := form.(map[string]any)["properties"].(map[string]any)
		p["parameters"].(map[string]any)["minProperties"] = 1
	}

}

func researchAuthoringSchema(defs map[string]any) map[string]any {
	text := func(max int) map[string]any {
		return map[string]any{"type": "string", "minLength": 1, "maxLength": max}
	}
	profile := map[string]any{"type": "object", "properties": map[string]any{"profile": text(131072), "profileDigest": map[string]any{"type": "string", "pattern": digestPattern.String() + `(?![\s\S])`}}, "required": []string{"profile", "profileDigest"}, "additionalProperties": false}
	field := map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string", "minLength": 1, "maxLength": 64, "not": map[string]any{"const": "case"}}, "type": map[string]any{"enum": []string{"integer", "decimal", "boolean", "text", "text-list", "range"}}, "required": map[string]any{"type": "boolean"}, "description": text(2048)}, "required": []string{"name", "type", "required", "description"}, "additionalProperties": false}
	parameters := defs["source"].(map[string]any)["oneOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["parameters"]
	reconciliation := map[string]any{"type": "object", "properties": map[string]any{"sources": map[string]any{"type": "array", "minItems": 1, "maxItems": 15, "uniqueItems": true, "items": text(131072)}, "rule": map[string]any{"$ref": "#/$defs/rule"}}, "required": []string{"sources", "rule"}, "additionalProperties": false}
	return map[string]any{"type": "object", "properties": map[string]any{
		"name": map[string]any{"type": "string", "minLength": 1, "not": map[string]any{"const": "case"}}, "kind": map[string]any{"const": "research"}, "parameters": parameters,
		"read":        map[string]any{"oneOf": []any{map[string]any{"$ref": "#/$defs/read"}, reconciliation}},
		"maxAge":      map[string]any{"type": "integer", "minimum": 1, "maximum": 86400},
		"question":    map[string]any{"oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "object", "properties": map[string]any{"$param": text(131072)}, "required": []string{"$param"}, "additionalProperties": false}, map[string]any{"type": "object", "properties": map[string]any{"$text": map[string]any{"type": "string"}}, "required": []string{"$text"}, "additionalProperties": false}}},
		"constraints": map[string]any{"type": "object", "additionalProperties": map[string]any{"$ref": "#/$defs/canonicalValue"}}, "model": text(256),
		"evidence": map[string]any{"type": "array", "items": text(4096)}, "fields": map[string]any{"type": "array", "minItems": 1, "maxItems": 64, "items": field},
		"capabilities": map[string]any{"enum": [][]string{{"answer"}, {"answer", "search"}, {"answer", "page"}, {"answer", "search", "page"}}},
		"profile":      text(131072), "profileDigest": map[string]any{"type": "string", "pattern": digestPattern.String() + `(?![\s\S])`},
		"webProfiles": map[string]any{"type": "object", "properties": map[string]any{"search": profile, "page": profile}, "additionalProperties": false},
	}, "required": []string{"name", "kind", "read", "maxAge", "question", "constraints", "model", "evidence", "fields", "capabilities"}, "additionalProperties": false,
		"dependentRequired": map[string]any{"profile": []string{"profileDigest", "webProfiles"}, "profileDigest": []string{"profile", "webProfiles"}, "webProfiles": []string{"profile", "profileDigest"}}}
}
