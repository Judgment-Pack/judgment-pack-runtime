package dialect

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// These bytes are embedded; serving a resource never fetches a mutable URL.
// The spec is the checksum-pinned upstream text used by the fixture tests.
//
//go:embed testdata/spec-dialect.md
var referenceText string

//go:embed version2.schema.json
var version2Schema string

// Both resources share the same copy, rule, parameter and template definitions.
var version3Schema = version3AuthoringSchema()

// Resource describes its own bytes, not a dialect or preparation digest.
// A runtime-derived schema is an editing aid, never a substitute for Validate.
type Resource struct {
	MediaType string `json:"mediaType"`
	Origin    string `json:"origin"`
	Bytes     int    `json:"bytes"`
	SHA256    string `json:"sha256"`
	Text      string `json:"text"`
}

func resource(text, mediaType, origin string) *Resource {
	sum := sha256.Sum256([]byte(text))
	return &Resource{MediaType: mediaType, Origin: origin, Bytes: len(text), SHA256: hex.EncodeToString(sum[:]), Text: text}
}

func Reference(command string) Report {
	r := report(command)
	r.Status = "available"
	r.Resource = resource(referenceText, "text/markdown", "pinned-specification")
	return r
}

func Schema(command string, version int) (Report, int) {
	r := report(command)
	if version != 2 && version != 3 {
		return refused(r, "JPS-DIALECT-SCHEMA-VERSION", "/version", "The Runtime-derived authoring schema supports versions 2 and 3.", result.ExitUnsupported)
	}
	r.Status = "available"
	r.DialectVersion = version
	r.ValidationScope = "version-" + strconv.Itoa(version) + "-structure-aid"
	schema := version2Schema
	if version == 3 {
		schema = version3Schema
	}
	r.Resource = resource(schema, "application/schema+json", "runtime-derived")
	return r, result.ExitSuccess
}

// ArtifactSchema is an authoring aid selected by an exact named format.
func ArtifactSchema(command, format string) (Report, int) {
	version := 2
	if format == "3.0.0" {
		version = 3
	} else if format != "2.0.0" {
		return refused(report(command), "JPS-DIALECT-FORMAT", "/formatVersion", "Unsupported named format.", result.ExitUnsupported)
	}
	r, code := Schema(command, version)
	var schema map[string]any
	_ = json.Unmarshal([]byte(r.Resource.Text), &schema)
	applyStrictOmissions(schema)
	schema["$id"] = "urn:judgmentpack:runtime:dialect:" + format + ":authoring-schema:1"
	schema["title"] = "Named Dialect " + format + " — Runtime authoring schema"
	props := schema["properties"].(map[string]any)
	for _, key := range []string{"provider", "facts", "evidence"} {
		delete(props, key)
	}
	props["formatVersion"] = map[string]any{"const": format}
	props["id"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 1024, "pattern": `^[^\u0000-\u0020\u007f]+$(?![\s\S])`}
	props["version"] = map[string]any{"type": "string", "pattern": artifactGrammar.String() + `(?![\s\S])`}
	schema["required"] = []string{"formatVersion", "id", "version", "case"}
	// Identity omission rules and UTF-8 byte bounds are enforced by Validate.
	encoded, _ := json.MarshalIndent(schema, "", "  ")
	r.Resource = resource(string(encoded)+"\n", "application/schema+json", "runtime-derived")
	r.FormatVersion = format
	return r, code
}
