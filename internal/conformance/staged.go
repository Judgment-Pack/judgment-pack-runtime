package conformance

import (
	"encoding/json"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// The staged draft selector's document cases (EXPERIMENTAL). They are in no
// corpus: the bundled manifest has a "staged" status and no suiteVersion, and
// each case is written against the released case schema unchanged
// (manifest.schema.json#/$defs/case), whose Core references resolve against
// the released Core 0.3.0 schema the cases were staged after. Running them is
// implementation experience; it is not conformance evidence and no claim
// cites it.

// stagedEnvelope is the part of the staged manifest the runner holds to its
// stated shape before a case runs.
type stagedEnvelope struct {
	Status         string            `json:"status"`
	StagedAfter    string            `json:"stagedAfter"`
	SpecVersion    string            `json:"specVersion"`
	CaseSchema     string            `json:"caseSchema"`
	ReaderProfiles []string          `json:"readerProfiles"`
	Cases          []json.RawMessage `json:"cases"`
}

func stagedSource(version string) (suiteSource, *validation.OperationalFailure) {
	source, failure := bundledSource(version)
	if failure != nil {
		return suiteSource{}, failure
	}
	set, err := artifacts.Load(version)
	if err != nil || !set.Staged() {
		return suiteSource{}, internalFailure("JPS-ARTIFACT-INTEGRITY", "The staged bundle does not state its stage.")
	}
	var envelope stagedEnvelope
	if err := json.Unmarshal(source.manifestBytes, &envelope); err != nil {
		return suiteSource{}, internalFailure("JPS-ARTIFACT-MANIFEST", "Bundled staged conformance metadata is invalid.")
	}
	released, err := artifacts.Load(envelope.StagedAfter)
	if err != nil || artifacts.IsStaged(envelope.StagedAfter) {
		return suiteSource{}, internalFailure("JPS-ARTIFACT-MANIFEST", "The staged cases name no bundled released version they were staged after.")
	}
	coreSchema, err := released.Schema()
	if err != nil {
		return suiteSource{}, internalFailure("JPS-ARTIFACT-SCHEMA", "Bundled Core schema is unavailable.")
	}
	source.staged = true
	source.coreSchemaBytes = coreSchema
	return source, nil
}

// validateStagedManifest holds the staged envelope to its stated shape and
// every case to the released case schema, then applies the same case rules
// the released runner applies.
func validateStagedManifest(manifestBytes, schemaBytes, coreSchemaBytes []byte) (Manifest, *validation.OperationalFailure) {
	value, carrierFailure := carrier.Decode(manifestBytes, carrier.DefaultLimits())
	if carrierFailure != nil {
		return Manifest{}, internalFailure("JPS-SUITE-MANIFEST", "Staged suite manifest is not valid strict JSON.")
	}
	var envelope stagedEnvelope
	if err := json.Unmarshal(manifestBytes, &envelope); err != nil {
		return Manifest{}, internalFailure("JPS-SUITE-MANIFEST", "Staged suite manifest could not be decoded.")
	}
	if envelope.Status != "staged" || !artifacts.IsStaged(envelope.SpecVersion) || envelope.CaseSchema != "../manifest.schema.json#/$defs/case" || len(envelope.ReaderProfiles) == 0 || len(envelope.Cases) == 0 {
		return Manifest{}, internalFailure("JPS-SUITE-MANIFEST", "Staged suite manifest does not have the staged shape.")
	}
	if _, has := value.(map[string]any)["suiteVersion"]; has {
		return Manifest{}, internalFailure("JPS-SUITE-MANIFEST", "A staged suite manifest carries no suiteVersion.")
	}
	resources := map[string][]byte{"urn:judgmentpack:jps:conformance-manifest": schemaBytes}
	var coreSchema struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(coreSchemaBytes, &coreSchema); err != nil || coreSchema.ID == "" {
		return Manifest{}, internalFailure("JPS-SUITE-MANIFEST-SCHEMA", "Bundled Core schema identity is unavailable.")
	}
	resources[coreSchema.ID] = coreSchemaBytes
	compiled, err := validation.CompileSchemas(resources, "urn:judgmentpack:jps:conformance-manifest#/$defs/case")
	if err != nil {
		return Manifest{}, internalFailure("JPS-SUITE-MANIFEST-SCHEMA", "The released case schema could not be compiled.")
	}
	for _, raw := range envelope.Cases {
		item, failure := carrier.Decode(raw, carrier.DefaultLimits())
		if failure != nil || compiled.Validate(item) != nil {
			return Manifest{}, internalFailure("JPS-SUITE-MANIFEST", "A staged case does not satisfy the released case schema.")
		}
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return Manifest{}, internalFailure("JPS-SUITE-MANIFEST", "Staged suite manifest could not be decoded.")
	}
	if failure := checkCases(manifest); failure != nil {
		return Manifest{}, failure
	}
	return manifest, nil
}

// declaredProfile reads the fixture's root profile declaration without
// validating it: "" when the fixture declares none or cannot be scanned.
func declaredProfile(fixture []byte) string {
	root, failure := carrier.ScanRootDeclarations(fixture, carrier.DefaultLimits())
	if failure != nil {
		return ""
	}
	profile, _ := root["profile"].(string)
	return profile
}

// skippedProfileCase reports a staged case written for a reader profile this
// reader does not implement. It passes as skipped only when the reader
// refused the fixture's profile; reading it any other way is a mismatch.
func skippedProfileCase(id, expected, profile string, actual result.Validation) result.Case {
	caseResult := result.Case{
		ID:                id,
		ExpectedStatus:    expected,
		ActualStatus:      actual.Status,
		Status:            "skipped",
		SkipReason:        "the fixture declares the " + profile + " profile; this reader implements base only, and refused it as profile-unsupported",
		ActualDiagnostics: actual.Diagnostics,
	}
	if caseResult.ActualDiagnostics == nil {
		caseResult.ActualDiagnostics = []result.Diagnostic{}
	}
	refused := actual.Status == "unsupported" && len(actual.Diagnostics) == 1 && actual.Diagnostics[0].Code == "profile-unsupported"
	if !refused {
		caseResult.Status = "mismatch"
		caseResult.SkipReason = ""
	}
	return caseResult
}
