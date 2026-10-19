package policy

import (
	"embed"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

//go:embed prototype
var prototype embed.FS

const commonID = "urn:jps:prototype:policy-companion:0.0.2:common"
const wireID = "urn:judgmentpack:runtime:experimental-policy:materials:1"

var schemasOnce sync.Once
var schemas map[string]*jsonschema.Schema
var schemasError error

const wireSchema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["materials"],"properties":{"materials":{"type":"array","minItems":0,"maxItems":16,"items":{"type":"object","additionalProperties":false,"required":["content","dataBase64"],"properties":{"content":{"$ref":"urn:jps:prototype:policy-companion:0.0.2:common#/$defs/retainedContent"},"dataBase64":{"type":"string","maxLength":5592408}}}}}}`

func loadSchemas() (map[string]*jsonschema.Schema, error) {
	schemasOnce.Do(func() {
		resources := map[string][]byte{wireID: []byte(wireSchema)}
		for _, name := range []string{"common", "policy", "governing-adoption"} {
			raw, err := prototype.ReadFile("prototype/" + name + ".schema.json")
			if err != nil {
				schemasError = err
				return
			}
			resources["urn:jps:prototype:policy-companion:0.0.2:"+name] = raw
		}
		schemas = map[string]*jsonschema.Schema{}
		for name, id := range map[string]string{"common": commonID, "uri": commonID + "#/$defs/uri", "policy": "urn:jps:prototype:policy-companion:0.0.2:policy", "governing-adoption": "urn:jps:prototype:policy-companion:0.0.2:governing-adoption", "materials": wireID} {
			s, err := validation.CompileSchemas(resources, id)
			if err != nil {
				schemasError = err
				return
			}
			schemas[name] = s
		}
	})
	return schemas, schemasError
}
func validURI(id string) bool {
	s, err := loadSchemas()
	return err == nil && s["uri"].Validate(id) == nil
}

// Schema returns a self-contained editing copy assembled from exact pinned
// resources. Vendored source schemas and their provenance hashes stay unchanged.
func Schema(command, kindName, version string) (Report, int) {
	r := report(command)
	if version == CandidateVersion {
		r = candidateReport(command)
		if err := VerifyOfficialArtifacts(); err != nil {
			return refused(r, "JPS-POLICY-ARTIFACT", "", "The pinned official draft resources failed verification.", result.ExitIO)
		}
	}
	if code, path, message := selector(kindName, version); code != 0 {
		return refused(r, "JPS-POLICY-SELECTOR", path, message, code)
	}
	source, directory, commonURI, revision := schemaSource(version)
	raw, err := source.ReadFile(directory + "/" + kindName + ".schema.json")
	if err != nil {
		return refused(r, "JPS-POLICY-ARTIFACT", "", "The pinned prototype schema is unavailable.", result.ExitIO)
	}
	common, _ := source.ReadFile(directory + "/common.schema.json")
	var doc, shared map[string]any
	if json.Unmarshal(raw, &doc) != nil || json.Unmarshal(common, &shared) != nil {
		return refused(r, "JPS-POLICY-ARTIFACT", "", "The pinned prototype schema is invalid.", result.ExitIO)
	}
	delete(shared, "$id")
	delete(shared, "$schema")
	delete(shared, "$ref")
	rewriteRefs(shared, "#/$defs/", "#/$defs/common/$defs/")
	rewriteRefs(shared, commonURI+"#", "#/$defs/common")
	rewriteRefs(doc, commonURI+"#", "#/$defs/common")
	defs, _ := doc["$defs"].(map[string]any)
	if defs == nil {
		defs = map[string]any{}
		doc["$defs"] = defs
	}
	defs["common"] = shared
	doc["$id"] = "urn:judgmentpack:runtime:experimental-policy:" + version + ":" + kindName + ":authoring-schema:1"
	doc["$comment"] = "Runtime self-contained editing copy of non-normative RFC 0023 prototype at Spec commit " + revision + ". Use experimental policy validate/check; schema acceptance grants no approval, execution authority or conformance."
	if version == CandidateVersion {
		doc["$id"] = "urn:judgmentpack:runtime:experimental-policy:" + DraftRelease + ":" + kindName + ":authoring-schema:1"
		doc["$comment"] = "Runtime self-contained editing copy of " + draftSchemaPrefix + kindName + ".schema.json from " + DraftRelease + " at Spec commit " + revision + ". The source resource retains its immutable HTTPS identity; this derived copy has a Runtime URN. Schema acceptance establishes no authority or conformance."
	}
	text, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return refused(r, "JPS-POLICY-ARTIFACT", "", "The editing schema could not be assembled.", result.ExitIO)
	}
	r.Status = "available"
	r.Kind = kindName
	r.Resource = &Resource{Name: kindName + ".schema.json", MediaType: "application/schema+json", Text: string(text) + "\n"}
	if version == CandidateVersion {
		r.Resource.SourceID = draftSchemaPrefix + kindName + ".schema.json"
	}
	return r, result.ExitSuccess
}
func rewriteRefs(value any, from, to string) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "$ref" {
				if ref, ok := child.(string); ok && strings.HasPrefix(ref, from) {
					v[key] = to + strings.TrimPrefix(ref, from)
				}
			} else {
				rewriteRefs(child, from, to)
			}
		}
	case []any:
		for _, child := range v {
			rewriteRefs(child, from, to)
		}
	}
}

func validateDocument(command string, data []byte) (Report, map[string]any, int) {
	return validateDocumentFormat(command, data, "")
}
func validateDocumentFormat(command string, data []byte, format string) (Report, map[string]any, int) {
	r, doc, code := parseFormat(command, data, format)
	if code != 0 {
		return r, nil, code
	}
	if r.FormatVersion == CandidateVersion {
		if failure, c := candidateSchema(r.Kind, doc); failure != nil {
			if c == result.ExitIO {
				rr, code := refused(r, failure.code, failure.path, failure.message, c)
				return rr, nil, code
			}
			rr, code := candidateRefused(r, candidateCode(failure, c))
			return rr, nil, code
		}
		if failure := localReferences(doc); failure != nil {
			rr, code := candidateRefused(r, candidateLocalCode(doc, failure))
			return rr, nil, code
		}
		r.Status, r.ValidationScope, r.Document = "document-valid", r.Capabilities.ValidationScope, ""
		return r, doc, 0
	}
	if failure := referenceSelectors(doc); failure != nil {
		r, code := refused(r, failure.code, failure.path, failure.message, result.ExitUnsupported)
		return r, nil, code
	}
	compiled, err := loadSchemas()
	if err != nil {
		r, c := refused(r, "JPS-POLICY-ARTIFACT", "", "Pinned prototype schemas could not be compiled.", result.ExitIO)
		return r, nil, c
	}
	if err := compiled[r.Kind].Validate(doc); err != nil {
		r.Status = "refused"
		r.Document = ""
		r.Diagnostics, r.DiagnosticsTruncated = schemaDiagnostics(err)
		code := result.ExitInvalid
		if schemaBound(err) {
			code = result.ExitUnsupported
		}
		return r, nil, code
	}
	if issue := localReferences(doc); issue != nil {
		r, c := refused(r, issue.code, issue.path, issue.message, result.ExitInvalid)
		return r, nil, c
	}
	r.Status = "document-valid"
	r.ValidationScope = r.Capabilities.ValidationScope
	r.Document = ""
	return r, doc, result.ExitSuccess
}
func Validate(command string, data []byte) (Report, int) {
	r, _, code := validateDocument(command, data)
	return r, code
}

func ValidateFormat(command string, data []byte, format string) (Report, int) {
	r, _, code := validateDocumentFormat(command, data, format)
	return r, code
}

func referenceSelectors(doc map[string]any) *issue {
	if doc["kind"] != "governing-adoption" {
		return nil
	}
	check := func(value any, expected, path string) *issue {
		reference := object(value)
		if kind, ok := reference["kind"].(string); ok && kind != expected {
			return problem("JPS-POLICY-REFERENCE-KIND", path+"/kind", "This artifact-reference kind is outside the supported prototype slice.")
		}
		identity := object(reference["canonicalIdentity"])
		if profile, ok := identity["profile"].(string); ok && profile != "jps-canon" {
			return problem("JPS-POLICY-CANONICAL-PROFILE", path+"/canonicalIdentity/profile", "The exact supported canonical identity profile is jps-canon.")
		}
		return nil
	}
	if err := check(doc["consumer"], "pack", "/consumer"); err != nil {
		return err
	}
	for i, value := range array(doc["governingPolicies"]) {
		if err := check(object(value)["policy"], "policy", indexPath("/governingPolicies", i)+"/policy"); err != nil {
			return err
		}
	}
	return nil
}

func schemaDiagnostics(err error) ([]result.Diagnostic, bool) {
	var root *jsonschema.ValidationError
	if !errors.As(err, &root) {
		return []result.Diagnostic{result.ErrorDiagnostic("JPS-POLICY-SCHEMA", "policy", "", "Closed prototype schema validation failed.")}, false
	}
	items := []result.Diagnostic{}
	truncated := false
	var walk func(*jsonschema.ValidationError)
	add := func(code string, path []string, message string) {
		item := result.ErrorDiagnostic(code, "policy", carrier.Pointer(path), message)
		index := sort.Search(len(items), func(i int) bool { return !diagnosticLess(items[i], item) })
		if index < len(items) && items[index] == item {
			return
		}
		// Keep the same sorted, unique first 100 regardless of the schema
		// dependency's map traversal order. Output storage stays bounded while
		// every cause in the input-bounded validation tree is considered.
		if len(items) == validation.MaxDiagnostics {
			truncated = true
			if index == len(items) {
				return
			}
			copy(items[index+1:], items[index:len(items)-1])
			items[index] = item
			return
		}
		items = append(items, result.Diagnostic{})
		copy(items[index+1:], items[index:len(items)-1])
		items[index] = item
	}
	walk = func(e *jsonschema.ValidationError) {
		switch k := e.ErrorKind.(type) {
		case *kind.Required:
			for _, name := range k.Missing {
				add("JPS-POLICY-REQUIRED", append(append([]string(nil), e.InstanceLocation...), name), "Required prototype member is missing.")
			}
			return
		case *kind.AdditionalProperties:
			for range k.Properties {
				add("JPS-POLICY-UNKNOWN-MEMBER", e.InstanceLocation, "Member is not allowed by this closed prototype schema.")
			}
			return
		}
		if len(e.Causes) == 0 {
			add("JPS-POLICY-SCHEMA", e.InstanceLocation, "Value does not satisfy the closed prototype schema.")
			return
		}
		for _, cause := range e.Causes {
			walk(cause)
		}
	}
	walk(root)
	if len(items) == 0 {
		add("JPS-POLICY-SCHEMA", nil, "Value does not satisfy the closed prototype schema.")
	}
	return items, truncated
}

func diagnosticLess(a, b result.Diagnostic) bool {
	if a.InstancePath != b.InstancePath {
		return a.InstancePath < b.InstancePath
	}
	if a.Code != b.Code {
		return a.Code < b.Code
	}
	return a.Message < b.Message
}

// Upper collection, string and byte bounds describe prototype input support.
func schemaBound(err error) bool {
	var validationError *jsonschema.ValidationError
	if !errors.As(err, &validationError) {
		return false
	}
	var walk func(*jsonschema.ValidationError) bool
	walk = func(e *jsonschema.ValidationError) bool {
		switch e.ErrorKind.(type) {
		case *kind.MaxItems, *kind.MaxLength, *kind.Maximum:
			return true
		}
		for _, child := range e.Causes {
			if walk(child) {
				return true
			}
		}
		return false
	}
	return walk(validationError)
}
