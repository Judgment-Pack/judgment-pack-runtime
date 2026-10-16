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
	schema["$comment"] = "Runtime-derived editing aid from Spec commit " + SpecRevision + "; not a normative schema. Supports operation (bound or unbound) and selected-file only. Use dialect validate for strict carrier, spelling, scope, reference and aggregate checks. Does not bind sources or check profiles, preparations, capability availability or execution readiness."
	properties := schema["properties"].(map[string]any)
	properties["version"] = map[string]any{"const": 3}
	for _, name := range []string{"provider", "facts", "evidence"} {
		delete(properties, name)
	}
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
	source["oneOf"] = append(forms, unbound)
	encoded, err = json.MarshalIndent(schema, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(encoded) + "\n"
}
