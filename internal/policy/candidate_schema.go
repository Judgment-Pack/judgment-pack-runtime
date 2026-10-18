package policy

import (
	"embed"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed candidate
var candidate embed.FS

const candidateCommonID = "urn:jps:proposal:policy-companion:1.0.0:common"

var candidateSchemasOnce sync.Once
var candidateSchemas map[string]*jsonschema.Schema
var candidateSchemasError error

func schemaSource(version string) (embed.FS, string, string, string) {
	if version == CandidateVersion {
		return candidate, candidateFormat.directory, candidateFormat.commonURI, candidateFormat.revision
	}
	return prototype, prototypeFormat.directory, prototypeFormat.commonURI, prototypeFormat.revision
}

func loadCandidateSchemas() (map[string]*jsonschema.Schema, error) {
	candidateSchemasOnce.Do(func() {
		resources := map[string][]byte{}
		for _, name := range []string{"common", "policy", "governing-adoption"} {
			raw, err := candidate.ReadFile("candidate/" + name + ".schema.json")
			if err != nil {
				candidateSchemasError = err
				return
			}
			resources["urn:jps:proposal:policy-companion:1.0.0:"+name] = raw
		}
		candidateSchemas = map[string]*jsonschema.Schema{}
		for name, id := range map[string]string{"common": candidateCommonID, "uri": candidateCommonID + "#/$defs/uri", "policy": "urn:jps:proposal:policy-companion:1.0.0:policy", "governing-adoption": "urn:jps:proposal:policy-companion:1.0.0:governing-adoption"} {
			s, err := validation.CompileSchemas(resources, id)
			if err != nil {
				candidateSchemasError = err
				return
			}
			candidateSchemas[name] = s
		}
	})
	return candidateSchemas, candidateSchemasError
}

func validURIFormat(id, version string) bool {
	if version == ProfileVersion {
		return validURI(id)
	}
	s, err := loadCandidateSchemas()
	return err == nil && s["uri"].Validate(id) == nil
}

func candidateSchema(name string, value any) (*issue, int) {
	if name != "common" && name != "policy" && name != "governing-adoption" {
		return candidateIssue("invalid-document"), result.ExitInvalid
	}
	schemas, err := loadCandidateSchemas()
	if err != nil {
		return problem("JPS-POLICY-ARTIFACT", "", "Pinned candidate schemas could not be compiled."), result.ExitIO
	}
	if err := schemas[name].Validate(value); err != nil {
		if schemaBound(err) {
			return candidateIssue("input-over-bound"), result.ExitUnsupported
		}
		return candidateIssue("invalid-document"), result.ExitInvalid
	}
	return nil, 0
}

func candidateLocalCode(doc map[string]any, failure *issue) string {
	if doc["kind"] == "policy" {
		if failure.code == "JPS-POLICY-DUPLICATE-ID" && len(failure.path) >= len("/sourceProvenance") && failure.path[:len("/sourceProvenance")] == "/sourceProvenance" {
			return "invalid-document"
		}
		return "invalid-anchor"
	}
	return "invalid-coverage"
}
