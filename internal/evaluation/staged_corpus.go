package evaluation

import (
	"encoding/json"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// The staged draft selector's evaluation rows (EXPERIMENTAL). They are in no
// corpus: the bundled file has a "staged" status and no suiteVersion, and each
// row is written against the released evaluation case schema unchanged
// (manifest.schema.json#/$defs/case). A held row is one the released case
// schema cannot state; it is reported as held, with the reason the staged file
// gives, and never run. Running the rows is implementation experience; it is
// not conformance evidence and no claim cites it.

// stagedRowsCaseSchema is the case schema the staged rows name, relative to
// their own file; the bundle carries that schema as evaluation/manifest.schema.json.
const stagedRowsCaseSchema = "../../manifest.schema.json#/$defs/case"

type stagedRows struct {
	Status      string            `json:"status"`
	StagedAfter string            `json:"stagedAfter"`
	SpecVersion string            `json:"specVersion"`
	CaseSchema  string            `json:"caseSchema"`
	Cases       []json.RawMessage `json:"cases"`
	HeldCases   []stagedHeldRow   `json:"heldCases"`
}

type stagedHeldRow struct {
	Case        json.RawMessage `json:"case"`
	HeldBecause string          `json:"heldBecause"`
}

// runStagedRows runs a staged bundle's evaluation rows under its own selector.
func (e *Engine) runStagedRows(set *artifacts.Set, command string) (result.EvaluationCorpus, *Failure) {
	rows, cases, held, failure := loadStagedRows(set)
	if failure != nil {
		return result.EvaluationCorpus{}, failure
	}
	output := result.EvaluationCorpus{
		OutputVersion:             result.OutputVersion,
		Tool:                      result.CurrentTool(),
		Command:                   command,
		Status:                    "passed",
		Experimental:              true,
		ConformanceClaimReference: result.EvaluationClaimReference,
		Label:                     result.EvaluationCorpusLabel,
		SpecVersion:               rows.SpecVersion,
		CorpusStatus:              rows.Status,
		CorpusLabel:               "staged evaluation rows of an unreleased draft selector; not a corpus and not conformance evidence",
		Provenance:                set.Lock().Source.Kind,
		Cases:                     []result.EvaluationCorpusCase{},
	}
	admissions := map[string]*AdmittedPack{}
	for _, item := range cases {
		caseResult := e.runCorpusCase(set, admissions, item)
		output.Summary.Total++
		if caseResult.Status == "passed" {
			output.Summary.Passed++
		} else {
			output.Summary.Mismatched++
			output.Status = "mismatch"
		}
		output.Cases = append(output.Cases, caseResult)
	}
	for index, item := range held {
		output.Summary.Held++
		output.Cases = append(output.Cases, result.EvaluationCorpusCase{
			ID:          item.ID,
			Origin:      item.Origin,
			SpecSection: item.SpecSection,
			Status:      "held",
			Detail:      rows.HeldCases[index].HeldBecause,
		})
	}
	return output, nil
}

// loadStagedRows holds the staged file to its stated shape and every row, run
// or held, to the released case schema before a single row runs, so a carrier
// defect is never mistaken for an evaluation result.
func loadStagedRows(set *artifacts.Set) (stagedRows, []corpusCase, []corpusCase, *Failure) {
	version := set.Lock().SpecVersion
	data, err := set.StagedEvaluationRows()
	if err != nil {
		return stagedRows{}, nil, nil, internalCorpusFailure("Bundled staged evaluation rows are unavailable.")
	}
	schemaBytes, err := set.EvaluationManifestSchema()
	if err != nil {
		return stagedRows{}, nil, nil, internalCorpusFailure("Bundled evaluation case schema is unavailable.")
	}
	document, carrierFailure := carrier.Decode(data, carrier.DefaultLimits())
	if carrierFailure != nil {
		return stagedRows{}, nil, nil, internalCorpusFailure("Bundled staged evaluation rows are not valid strict JSON.")
	}
	var rows stagedRows
	if err := json.Unmarshal(data, &rows); err != nil {
		return stagedRows{}, nil, nil, internalCorpusFailure("Bundled staged evaluation rows could not be decoded.")
	}
	if _, has := document.(map[string]any)["suiteVersion"]; has || rows.Status != "staged" || rows.SpecVersion != version || rows.CaseSchema != stagedRowsCaseSchema || len(rows.Cases) == 0 {
		return stagedRows{}, nil, nil, internalCorpusFailure("Bundled staged evaluation rows do not have the staged shape.")
	}
	const schemaID = "urn:judgmentpack:jps:evaluation-manifest"
	compiled, err := validation.CompileSchemas(map[string][]byte{schemaID: schemaBytes}, schemaID+"#/$defs/case")
	if err != nil {
		return stagedRows{}, nil, nil, internalCorpusFailure("Bundled evaluation case schema could not be compiled.")
	}
	decode := func(raw json.RawMessage) (corpusCase, bool) {
		value, failure := carrier.Decode(raw, carrier.DefaultLimits())
		if failure != nil || compiled.Validate(value) != nil {
			return corpusCase{}, false
		}
		var item corpusCase
		return item, json.Unmarshal(raw, &item) == nil
	}
	cases := make([]corpusCase, 0, len(rows.Cases))
	for _, raw := range rows.Cases {
		item, ok := decode(raw)
		if !ok {
			return stagedRows{}, nil, nil, internalCorpusFailure("A staged evaluation row does not satisfy the released case schema.")
		}
		cases = append(cases, item)
	}
	held := make([]corpusCase, 0, len(rows.HeldCases))
	for _, entry := range rows.HeldCases {
		var item corpusCase
		if entry.HeldBecause == "" || json.Unmarshal(entry.Case, &item) != nil || item.ID == "" {
			return stagedRows{}, nil, nil, internalCorpusFailure("A held staged evaluation row does not state its case and reason.")
		}
		held = append(held, item)
	}
	return rows, cases, held, nil
}
