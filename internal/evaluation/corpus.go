package evaluation

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// The evaluation corpus of JPS §3.4.1, run against this runtime's experimental
// evaluator. Running it produces results and nothing else: §3.4.1 defines
// exactly one form of evaluator-conformance claim, this runtime makes none, and
// every payload here says so (result.EvaluationCorpusLabel). A passing run is
// evidence a claim would require, not a claim, and not a decision to make one.

// corpusManifest is the case carrier of the bundled evaluation corpus. facts,
// evidenceAvailability, and expectedDisposition are kept as raw bytes: the
// evaluator takes its inputs as documents, and re-encoding them here would put a
// second serializer between the corpus and the evaluation.
type corpusManifest struct {
	SuiteVersion string       `json:"suiteVersion"`
	SpecVersion  string       `json:"specVersion"`
	Status       string       `json:"status"`
	Label        string       `json:"label"`
	Cases        []corpusCase `json:"cases"`
}

type corpusCase struct {
	ID                   string          `json:"id"`
	Origin               string          `json:"origin"`
	Pack                 string          `json:"pack"`
	Facts                json.RawMessage `json:"facts"`
	EvidenceAvailability json.RawMessage `json:"evidenceAvailability"`
	SupportedExtensions  []string        `json:"supportedExtensions"`
	ExpectedDisposition  json.RawMessage `json:"expectedDisposition"`
	ExpectedErrorClass   string          `json:"expectedErrorClass"`
	ExpectedErrorPhase   string          `json:"expectedErrorPhase"`
	Focus                string          `json:"focus"`
	SpecSection          string          `json:"specSection"`
}

// RunCorpus runs the evaluation corpus bundled for one exact specification
// version through this evaluator and reports every row's result.
//
// A row carrying an expected disposition passes when the disposition this
// evaluator produces canonicalizes, under RFC 8785, to the same bytes as the
// row's — the byte agreement §8.3 requires of two implementations, applied to
// one implementation and one pinned expectation. A row carrying an expected
// §8.4 error class instead passes when the evaluation is refused with that
// class, and with that phase when the row names one.
//
// A failing row decides nothing by itself: §3.4 makes a divergence as likely to
// be a defect in the row as in the implementation, and neither this runtime nor
// this function adjudicates that question.
func (e *Engine) RunCorpus(specVersion, command string) (result.EvaluationCorpus, *Failure) {
	if specVersion == "" {
		specVersion = artifacts.EvaluatorDraftVersion
	}
	set, err := artifacts.Load(specVersion)
	if err != nil {
		return result.EvaluationCorpus{}, &Failure{
			Code:     "JPS-CAPABILITY-SPEC-VERSION",
			Message:  "The exact JPS specification version is not bundled with this runtime.",
			ExitCode: result.ExitUnsupported,
		}
	}
	if !set.HasEvaluationCorpus() {
		return result.EvaluationCorpus{}, &Failure{
			Code:     "JPS-CAPABILITY-EVALUATION-CORPUS",
			Message:  fmt.Sprintf("JPS %s publishes no evaluation corpus; its Core defines no evaluator conformance class.", specVersion),
			ExitCode: result.ExitUnsupported,
		}
	}
	manifest, failure := loadCorpusManifest(set, specVersion)
	if failure != nil {
		return result.EvaluationCorpus{}, failure
	}

	output := result.EvaluationCorpus{
		OutputVersion:    result.OutputVersion,
		Tool:             result.CurrentTool(),
		Command:          command,
		Status:           "passed",
		Experimental:     true,
		ConformanceClaim: result.EvaluationClaim,
		Label:            result.EvaluationCorpusLabel,
		SpecVersion:      manifest.SpecVersion,
		SuiteVersion:     manifest.SuiteVersion,
		CorpusStatus:     manifest.Status,
		CorpusLabel:      manifest.Label,
		Provenance:       set.Lock().Source.Kind,
		Cases:            []result.EvaluationCorpusCase{},
	}
	for _, item := range manifest.Cases {
		caseResult := e.runCorpusCase(set, item)
		output.Summary.Total++
		if caseResult.Status == "passed" {
			output.Summary.Passed++
		} else {
			output.Summary.Mismatched++
			output.Status = "mismatch"
		}
		output.Cases = append(output.Cases, caseResult)
	}
	return output, nil
}

// loadCorpusManifest reads the bundled corpus index and holds it to its own
// bundled schema before a single row runs, so a carrier defect is never mistaken
// for an evaluation result.
func loadCorpusManifest(set *artifacts.Set, specVersion string) (corpusManifest, *Failure) {
	manifestBytes, err := set.EvaluationManifest()
	if err != nil {
		return corpusManifest{}, internalCorpusFailure("Bundled evaluation-corpus metadata is unavailable.")
	}
	schemaBytes, err := set.EvaluationManifestSchema()
	if err != nil {
		return corpusManifest{}, internalCorpusFailure("Bundled evaluation-corpus schema is unavailable.")
	}
	document, carrierFailure := carrier.Decode(manifestBytes, carrier.DefaultLimits())
	if carrierFailure != nil {
		return corpusManifest{}, internalCorpusFailure("Bundled evaluation-corpus metadata is not valid strict JSON.")
	}
	compiled, err := validation.CompileSchema(schemaBytes, "urn:judgmentpack:jps:"+specVersion+":evaluation-manifest")
	if err != nil {
		return corpusManifest{}, internalCorpusFailure("Bundled evaluation-corpus schema could not be compiled.")
	}
	if err := compiled.Validate(document); err != nil {
		return corpusManifest{}, internalCorpusFailure("Bundled evaluation-corpus metadata does not satisfy its schema.")
	}
	var manifest corpusManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return corpusManifest{}, internalCorpusFailure("Bundled evaluation-corpus metadata could not be decoded.")
	}
	if manifest.SpecVersion != specVersion || len(manifest.Cases) == 0 {
		return corpusManifest{}, internalCorpusFailure("Bundled evaluation corpus does not target its own specification version.")
	}
	return manifest, nil
}

// runCorpusCase runs one row. The pack is a bundled, digest-locked fixture; the
// facts and evidence inputs are the row's own bytes, unaltered.
func (e *Engine) runCorpusCase(set *artifacts.Set, item corpusCase) result.EvaluationCorpusCase {
	outcome := result.EvaluationCorpusCase{
		ID:                 item.ID,
		Origin:             item.Origin,
		SpecSection:        item.SpecSection,
		Status:             "passed",
		ExpectedErrorClass: item.ExpectedErrorClass,
		ExpectedErrorPhase: item.ExpectedErrorPhase,
	}
	pack, err := set.EvaluationPack(item.Pack)
	if err != nil {
		return corpusMismatch(outcome, "The row's pack fixture is not bundled.")
	}
	if item.ExpectedDisposition != nil {
		expected, err := canonicalDisposition(item.ExpectedDisposition)
		if err != nil {
			return corpusMismatch(outcome, "The row's expected disposition is not a canonicalizable §8.3 disposition, which is a defect in the bundled carrier and not a result: "+err.Error())
		}
		outcome.Expected = expected
	}

	evaluated, failure := e.Evaluate(pack, item.Facts, item.EvidenceAvailability, item.SupportedExtensions, "experimental evaluate-corpus")
	if failure != nil {
		outcome.ActualErrorClass, outcome.ActualErrorPhase = failure.Class, failure.Phase
		if item.ExpectedErrorClass == "" {
			return corpusMismatch(outcome, "The evaluation was refused where a disposition was expected: "+failure.Code+": "+failure.Message)
		}
		if failure.Class != item.ExpectedErrorClass {
			return corpusMismatch(outcome, "The evaluation error class differs: "+failure.Code+": "+failure.Message)
		}
		if item.ExpectedErrorPhase != "" && failure.Phase != item.ExpectedErrorPhase {
			return corpusMismatch(outcome, "The evaluation error phase differs: "+failure.Code+": "+failure.Message)
		}
		return outcome
	}
	actual, err := evaluated.Disposition.Canonical()
	if err != nil {
		return corpusMismatch(outcome, "The disposition could not be canonicalized: "+err.Error())
	}
	outcome.Actual = string(actual)
	if item.ExpectedErrorClass != "" {
		return corpusMismatch(outcome, "A disposition was produced where an evaluation error was expected.")
	}
	if outcome.Actual != outcome.Expected {
		return corpusMismatch(outcome, "The canonical disposition bytes differ.")
	}
	return outcome
}

func corpusMismatch(outcome result.EvaluationCorpusCase, detail string) result.EvaluationCorpusCase {
	outcome.Status = "mismatch"
	outcome.Detail = detail
	return outcome
}

// canonicalDisposition canonicalizes one row's expected disposition through the
// same type, the same set normalization, and the same encoder the actual side
// uses, so the two sides of the comparison are produced identically.
//
// Comparing is then disposition equality as §8.3 defines it rather than raw byte
// equality of what the manifest happens to store: reasons and
// handoff.triggeredBy are sets, and "serialized order is never a difference in
// the disposition". Every bundled row stores them sorted, so this changes no
// current result; a future row that did not would be reported as the equal
// disposition it is instead of as a mismatch. Canonical() also holds the
// expectation to §8.3's three presence rules, so a carrier defect the manifest
// schema cannot catch is named as one.
func canonicalDisposition(raw json.RawMessage) (string, error) {
	var disposition result.Disposition
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&disposition); err != nil {
		return "", err
	}
	encoded, err := disposition.Canonical()
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func internalCorpusFailure(message string) *Failure {
	return &Failure{Code: "JPS-ARTIFACT-EVALUATION-CORPUS", Message: message, ExitCode: result.ExitInternal}
}
