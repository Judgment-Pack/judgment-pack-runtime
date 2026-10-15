// Package dialect implements offline document tools for the pinned dialect
// companion. It does not prepare inputs, validate a dialect's full schema,
// consult a project, or evaluate decisions.
package dialect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	SpecRevision     = "9fb1142546e79ab4d03f744415afc098a81efe3e"
	SpecURL          = "https://github.com/Judgment-Pack/judgment-pack-spec/blob/" + SpecRevision + "/spec/dialect.md"
	MaxDocumentBytes = 131072
	MaxDepth         = 64
	maxInteger       = int64(9007199254740991)
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
	FullDocumentConformance bool                `json:"fullDocumentConformance"`
	DialectVersion          int                 `json:"dialectVersion,omitempty"`
	Document                string              `json:"document,omitempty"`
	Digest                  string              `json:"digest,omitempty"`
	StarterTemplate         bool                `json:"starterTemplate,omitempty"`
	Diagnostics             []result.Diagnostic `json:"diagnostics"`
	Capabilities            *Capabilities       `json:"capabilities,omitempty"`
}

type Capabilities struct {
	Operations              []string `json:"operations"`
	EncodingVersions        []int    `json:"encodingVersions"`
	MaxDocumentBytes        int      `json:"maxDocumentBytes"`
	MaxDepth                int      `json:"maxDepth"`
	StructuralValidation    bool     `json:"structuralValidation"`
	SemanticValidation      bool     `json:"semanticValidation"`
	PackCompatibility       bool     `json:"packCompatibility"`
	PreparationVerification bool     `json:"preparationVerification"`
}

func report(command string) Report {
	return Report{OutputVersion: result.OutputVersion, Tool: result.CurrentTool(), Command: command,
		ContractVersion: "1", Specification: SpecURL, SpecRevision: SpecRevision, Maturity: "draft",
		Status: "encoded", Diagnostics: []result.Diagnostic{}}
}

func Contract(command string) Report {
	r := report(command)
	r.Status = "available"
	r.Capabilities = &Capabilities{Operations: []string{"contract", "init", "canonicalize", "digest"}, EncodingVersions: []int{2, 3}, MaxDocumentBytes: MaxDocumentBytes, MaxDepth: MaxDepth}
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
// executable. Full schema/semantic checking is a separate future operation.
func Encode(command string, data []byte) (Report, int) {
	r := report(command)
	if len(data) > MaxDocumentBytes {
		return refused(r, "JPS-DIALECT-BYTE-LIMIT", "", "The dialect exceeds the document byte limit.", result.ExitInvalid)
	}
	value, failure := carrier.Decode(data, carrier.Limits{MaxDepth: MaxDepth, MaxNodes: MaxDocumentBytes, MaxStringBytes: MaxDocumentBytes})
	if failure != nil {
		r.Status = "refused"
		r.Diagnostics = []result.Diagnostic{failure.Diagnostic}
		return r, result.ExitInvalid
	}
	var out strings.Builder
	if path, ok := canonical(&out, value, nil); !ok {
		return refused(r, "JPS-DIALECT-CANONICAL-NUMBER", path, "Canonical dialect numbers must use integer literals within the safe integer range; fractions and exponent notation are refused.", result.ExitInvalid)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return refused(r, "JPS-DIALECT-DOCUMENT", "", "A dialect document must be a JSON object.", result.ExitInvalid)
	}
	version, ok := object["version"].(json.Number)
	if !ok {
		return refused(r, "JPS-DIALECT-VERSION", "/version", "A dialect document must declare its integer version.", result.ExitInvalid)
	}
	switch version.String() {
	case "2":
		r.DialectVersion = 2
	case "3":
		r.DialectVersion = 3
	default:
		return refused(r, "JPS-DIALECT-VERSION", "/version", "Supported dialect encoding versions are 2 and 3.", result.ExitUnsupported)
	}
	if r.DialectVersion == 3 {
		if path := version3Spelling(object); path != "" {
			return refused(r, "JPS-DIALECT-V3-SPELLING", path, "Version 3 forbids version-2-only members and requires this optional empty member to be omitted.", result.ExitInvalid)
		}
	}
	r.Document = out.String()
	digest := sha256.Sum256([]byte(r.Document))
	r.Digest = "sha256:" + hex.EncodeToString(digest[:])
	return r, result.ExitSuccess
}

// UTF-8 lexical order equals Unicode code-point order for the well-formed
// strings admitted by carrier.Decode. This is not disposition JCS (UTF-16
// ordering) and not preparation canonicalization (which preserves -0).
func canonical(out *strings.Builder, value any, path []string) (string, bool) {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(v))
	case string:
		writeString(out, v)
	case json.Number:
		literal := v.String()
		if strings.ContainsAny(literal, ".eE") {
			return carrier.Pointer(path), false
		}
		n, err := strconv.ParseInt(literal, 10, 64)
		if err != nil || n < -maxInteger || n > maxInteger {
			return carrier.Pointer(path), false
		}
		out.WriteString(strconv.FormatInt(n, 10))
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if p, ok := canonical(out, item, child(path, strconv.Itoa(i))); !ok {
				return p, false
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			writeString(out, key)
			out.WriteByte(':')
			if p, ok := canonical(out, v[key], child(path, key)); !ok {
				return p, false
			}
		}
		out.WriteByte('}')
	default:
		panic("carrier returned a value outside JSON")
	}
	return "", true
}
func child(path []string, key string) []string {
	out := make([]string, len(path)+1)
	copy(out, path)
	out[len(path)] = key
	return out
}
func writeString(out *strings.Builder, value string) {
	const hexDigits = "0123456789abcdef"
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 {
				out.WriteString(`\u00`)
				out.WriteByte(hexDigits[r>>4])
				out.WriteByte(hexDigits[r&15])
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
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
