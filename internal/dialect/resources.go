package dialect

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// These bytes are embedded; serving a resource never fetches a mutable URL.
// The spec is the checksum-pinned upstream text used by the fixture tests.
//
//go:embed testdata/spec-dialect.md
var referenceText string

//go:embed version2.schema.json
var version2Schema string

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
	if version != 2 {
		return refused(r, "JPS-DIALECT-SCHEMA-VERSION", "/version", "The Runtime-derived authoring schema supports version 2 only.", result.ExitUnsupported)
	}
	r.Status = "available"
	r.DialectVersion = version
	r.ValidationScope = "version-2-structure-aid"
	r.Resource = resource(version2Schema, "application/schema+json", "runtime-derived")
	return r, result.ExitSuccess
}
