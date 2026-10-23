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
const draftSchemaPrefix = "https://judgmentpack.org/schema/policy/1.0.0/"
const draftCommonID = draftSchemaPrefix + "common.schema.json"

var candidateSchemasOnce sync.Once
var candidateSchemas map[string]*jsonschema.Schema
var candidateSchemasError error

func schemaSource(version string) (embed.FS, string, string, string) {
	if version == CandidateVersion {
		return official, candidateFormat.directory, candidateFormat.commonURI, candidateFormat.revision
	}
	return prototype, prototypeFormat.directory, prototypeFormat.commonURI, prototypeFormat.revision
}

func loadCandidateSchemas() (map[string]*jsonschema.Schema, error) {
	candidateSchemasOnce.Do(func() {
		if err := VerifyOfficialArtifacts(); err != nil {
			candidateSchemasError = err
			return
		}
		resources := map[string][]byte{}
		for _, name := range []string{"common", "policy", "governing-adoption"} {
			raw, err := official.ReadFile(candidateFormat.directory + "/" + name + ".schema.json")
			if err != nil {
				candidateSchemasError = err
				return
			}
			resources[draftSchemaPrefix+name+".schema.json"] = raw
		}
		candidateSchemas = map[string]*jsonschema.Schema{}
		for name, id := range map[string]string{"common": draftCommonID, "uri": draftCommonID + "#/$defs/uri", "policy": draftSchemaPrefix + "policy.schema.json", "governing-adoption": draftSchemaPrefix + "governing-adoption.schema.json"} {
			s, err := compileCandidateSchemas(resources, id)
			if err != nil {
				candidateSchemasError = err
				return
			}
			candidateSchemas[name] = s
		}
	})
	return candidateSchemas, candidateSchemasError
}

func compileCandidateSchemas(resources map[string][]byte, id string) (*jsonschema.Schema, error) {
	return validation.CompileSchemas(resources, id)
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
		return problem("JPS-POLICY-ARTIFACT", "", "Pinned official draft schemas could not be verified or compiled."), result.ExitIO
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
