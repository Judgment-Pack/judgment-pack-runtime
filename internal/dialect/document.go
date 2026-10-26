// Package dialect implements offline document tools for the pinned dialect
// companion. Document checks are version-scoped; profile and preparation
// verification, project access and decision evaluation stay outside these tools.
package dialect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifactversion"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jpscanon"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	SpecRevision     = "b7cc8906c23d92c63f4e42c4706cc1e21b9d324f"
	SpecURL          = "https://github.com/Judgment-Pack/judgment-pack-spec/blob/" + SpecRevision + "/spec/dialect.md"
	MaxDocumentBytes = 131072
	MaxDepth         = 64
)

// Report is shared by CLI and MCP. Encoded never means schema-valid, prepared,
// executable, or verified. Document is a string so outer transport formatting
// cannot change the bytes whose digest is reported.
type Report struct {
	OutputVersion           string              `json:"outputVersion"`
	Tool                    result.Tool         `json:"tool"`
	Command                 string              `json:"command"`
	ContractVersion         string              `json:"contractVersion"`
	Specification           string              `json:"specification"`
	SpecRevision            string              `json:"specRevision"`
	Maturity                string              `json:"maturity"`
	Status                  string              `json:"status"`
	ValidationScope         string              `json:"validationScope,omitempty"`
	FullDocumentConformance bool                `json:"fullDocumentConformance"`
	FormatVersion           string              `json:"formatVersion,omitempty"`
	ArtifactID              string              `json:"artifactId,omitempty"`
	ArtifactVersion         string              `json:"artifactVersion,omitempty"`
	DialectVersion          int                 `json:"dialectVersion,omitempty"`
	Document                string              `json:"document,omitempty"`
	Digest                  string              `json:"digest,omitempty"`
	StarterTemplate         bool                `json:"starterTemplate,omitempty"`
	Diagnostics             []result.Diagnostic `json:"diagnostics"`
	Resource                *Resource           `json:"resource,omitempty"`
	Compatibility           *Compatibility      `json:"compatibility,omitempty"`
	PackValidation          *result.Validation  `json:"packValidation,omitempty"`
	Capabilities            *Capabilities       `json:"capabilities,omitempty"`
}

type Capabilities struct {
	FormatVersions             []string `json:"formatVersions"`
	ValidationSourceKinds      []string `json:"validationSourceKinds"`
	UnboundOperationValidation bool     `json:"unboundOperationValidation"`
	BindingVerification        bool     `json:"bindingVerification"`
	Operations                 []string `json:"operations"`
	EncodingVersions           []int    `json:"encodingVersions"`
	ValidationVersions         []int    `json:"validationVersions"`
	SchemaVersions             []int    `json:"schemaVersions"`
	PackCheckVersions          []int    `json:"packCheckVersions"`
	PackCheckScope             string   `json:"packCheckScope"`
	ProfileCompatibility       bool     `json:"profileCompatibility"`
	MaxDocumentBytes           int      `json:"maxDocumentBytes"`
	MaxPackBytes               int64    `json:"maxPackBytes"`
	MaxCoverageComparisons     int64    `json:"maxCoverageComparisons"`
	MaxCoverageReportBytes     int      `json:"maxCoverageReportBytes"`
	MaxDepth                   int      `json:"maxDepth"`
	StructuralValidation       bool     `json:"structuralValidation"`
	SemanticValidation         bool     `json:"semanticValidation"`
	PackCompatibility          bool     `json:"packCompatibility"`
	PreparationVerification    bool     `json:"preparationVerification"`
}

func report(command string) Report {
	return Report{OutputVersion: result.OutputVersion, Tool: result.CurrentTool(), Command: command,
		ContractVersion: "1", Specification: SpecURL, SpecRevision: SpecRevision, Maturity: "draft",
		Status: "encoded", Diagnostics: []result.Diagnostic{}}
}

func Contract(command string) Report {
	r := report(command)
	r.Status = "available"
	r.Capabilities = &Capabilities{FormatVersions: []string{"2.0.0", "3.0.0"}, Operations: []string{"contract", "init", "canonicalize", "digest", "validate", "schema", "reference", "check"}, EncodingVersions: []int{2, 3}, ValidationVersions: []int{2, 3}, SchemaVersions: []int{2, 3}, PackCheckVersions: []int{2, 3}, ValidationSourceKinds: []string{"operation", "selected-file", "research"}, UnboundOperationValidation: true, PackCompatibility: true, PackCheckScope: "declared-inventory-coverage", StructuralValidation: true, SemanticValidation: true, MaxDocumentBytes: MaxDocumentBytes, MaxPackBytes: MaxPackBytes, MaxCoverageComparisons: MaxCoverageComparisons, MaxCoverageReportBytes: MaxCoverageReportBytes, MaxDepth: MaxDepth}
	return r
}

func refused(r Report, code, path, message string, exit int) (Report, int) {
	r.Status = "refused"
	r.Document = ""
	r.Digest = ""
	r.Diagnostics = []result.Diagnostic{result.ErrorDiagnostic(code, "dialect", path, message)}
	return r, exit
}

// Init returns bytes only. Saving, choosing domain fields and replacing a
// client's current draft remain client actions (ADR-0006).
func Init(command string, version int) (Report, int) {
	if version != 2 && version != 3 {
		return refused(report(command), "JPS-DIALECT-VERSION", "/version", "Supported dialect encoding versions are 2 and 3.", result.ExitUnsupported)
	}
	document := `{"case":{"evidence":[],"facts":[]},"version":3}`
	if version == 2 {
		document = `{"case":{"evidence":[],"facts":[]},"evidence":null,"facts":null,"provider":"","version":2}`
	}
	r, code := Encode(command, []byte(document))
	if code == result.ExitSuccess {
		r.StarterTemplate = true
	}
	return r, code
}

// Encode checks the carrier, canonical domain, declared encoding version and
// version-3 identity spelling. It refuses forbidden spelling without rewriting it.
// It deliberately does not invent a partial structural validator. Unknown
// members and source kinds are retained verbatim as values, not admitted as
// executable. Document checking is a separate, version-scoped operation.
func Encode(command string, data []byte) (Report, int) {
	return encodeDocument(command, data, MaxDocumentBytes, documentLimits())
}

func documentLimits() carrier.Limits {
	return carrier.Limits{MaxDepth: MaxDepth, MaxNodes: MaxDocumentBytes, MaxStringBytes: MaxDocumentBytes}
}

func encodeDocument(command string, data []byte, maxBytes int, limits carrier.Limits) (Report, int) {
	return encodeDocumentWithIDLimit(command, data, maxBytes, limits, 1024)
}

func encodeDocumentWithIDLimit(command string, data []byte, maxBytes int, limits carrier.Limits, maxIDBytes int) (Report, int) {
	r := report(command)
	if len(data) > maxBytes {
		return refused(r, "JPS-DIALECT-BYTE-LIMIT", "", "The dialect exceeds the document byte limit.", result.ExitInvalid)
	}
	value, failure := carrier.Decode(data, limits)
	if failure != nil {
		r.Status = "refused"
		r.Diagnostics = []result.Diagnostic{failure.Diagnostic}
		return r, result.ExitInvalid
	}
	var out strings.Builder
	if path, ok := jpscanon.Write(&out, value, nil); !ok {
		return refused(r, "JPS-DIALECT-CANONICAL-NUMBER", path, "Canonical dialect numbers must use integer literals within the safe integer range; fractions and exponent notation are refused.", result.ExitInvalid)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return refused(r, "JPS-DIALECT-DOCUMENT", "", "A dialect document must be a JSON object.", result.ExitInvalid)
	}
	if selector, named := object["formatVersion"]; named {
		switch selector {
		case "2.0.0":
			r.DialectVersion = 2
		case "3.0.0":
			r.DialectVersion = 3
		default:
			return refused(r, "JPS-DIALECT-FORMAT", "/formatVersion", "Supported named formats are exactly 2.0.0 and 3.0.0.", result.ExitUnsupported)
		}
		r.FormatVersion = selector.(string)
		id, ok := object["id"].(string)
		if !ok || id == "" || len(id) > maxIDBytes || strings.IndexFunc(id, func(c rune) bool { return c <= 32 || c == 127 }) >= 0 {
			return refused(r, "JPS-DIALECT-ID", "/id", "Provide a non-empty stable identifier within "+strconv.Itoa(maxIDBytes)+" UTF-8 bytes, without ASCII spaces or controls.", result.ExitInvalid)
		}
		version, ok := object["version"].(string)
		if !ok || !artifactGrammar.MatchString(version) {
			return refused(r, "JPS-DIALECT-ARTIFACT-VERSION", "/version", "Provide an exact three-component artifact version without leading zeroes or suffixes.", result.ExitInvalid)
		}
		r.ArtifactID, r.ArtifactVersion = id, version
	} else {
		if _, present := object["id"]; present {
			return refused(r, "JPS-DIALECT-ID", "/id", "Legacy integer formats have no artifact identity.", result.ExitInvalid)
		}
		version, ok := object["version"].(json.Number)
		if !ok {
			return refused(r, "JPS-DIALECT-VERSION", "/version", "A legacy dialect document must declare its integer version.", result.ExitInvalid)
		}
		switch version.String() {
		case "2":
			r.DialectVersion = 2
		case "3":
			r.DialectVersion = 3
		default:
			return refused(r, "JPS-DIALECT-VERSION", "/version", "Supported legacy dialect versions are 2 and 3.", result.ExitUnsupported)
		}
	}
	if r.DialectVersion == 3 || r.FormatVersion != "" {
		if path := version3Spelling(object); path != "" {
			return refused(r, "JPS-DIALECT-V3-SPELLING", path, "Version 3 forbids version-2-only members and requires this optional empty member to be omitted.", result.ExitInvalid)
		}
	}
	r.Document = out.String()
	digest := sha256.Sum256([]byte(r.Document))
	r.Digest = "sha256:" + hex.EncodeToString(digest[:])
	return r, result.ExitSuccess
}

// version3Spelling checks the identity rules of §2, not structural conformance.
// Optional members that affect identity are refused; no bytes are normalized.
func version3Spelling(top map[string]any) string {
	for _, key := range []string{"provider", "facts", "evidence"} {
		if _, present := top[key]; present {
			return "/" + key
		}
	}
	emptyMember := func(value any, prefix string, keys ...string) string {
		object, _ := value.(map[string]any)
		for _, key := range keys {
			empty := false
			switch member := object[key].(type) {
			case []any:
				empty = len(member) == 0
			case map[string]any:
				empty = len(member) == 0
			}
			if empty {
				return prefix + "/" + key
			}
		}
		return ""
	}
	if path := emptyMember(top, "", "sources", "unmapped", "unmappedEvidence"); path != "" {
		return path
	}
	if path := emptyMember(top["case"], "/case", "parameters"); path != "" {
		return path
	}
	if path := emptyMember(top["admits"], "/admits", "facts", "evidence"); path != "" {
		return path
	}
	sources, _ := top["sources"].([]any)
	for i, source := range sources {
		if path := emptyMember(source, "/sources/"+strconv.Itoa(i), "parameters"); path != "" {
			return path
		}
	}
	return ""
}

var artifactGrammar = regexp.MustCompile(artifactversion.Pattern)

// InitArtifact creates a new named document, not a migration of an existing receipt.
func InitArtifact(command, format, id, version string) (Report, int) {
	if !utf8.ValidString(id) || !utf8.ValidString(format) || !utf8.ValidString(version) {
		return refused(report(command), "JPS-DIALECT-IDENTITY", "/id", "Artifact arguments must be valid UTF-8.", result.ExitInvalid)
	}
	raw, _ := json.Marshal(map[string]any{"formatVersion": format, "id": id, "version": version, "case": map[string]any{"facts": []any{}, "evidence": []any{}}})
	r, code := Validate(command, raw)
	if code == result.ExitSuccess {
		r.StarterTemplate = true
	}
	return r, code
}
