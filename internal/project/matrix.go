package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
)

// MatrixVersion is the one matrixVersion value this runtime accepts, on the same
// single-integer footing as ConfigVersion. It is optional in a matrix document:
// the rows are the specification's own case-carrier shape, and a matrix that
// declares nothing is read as this version.
const MatrixVersion = "1"

// Matrix is one pack's instance matrix: the rows a project wrote about its own
// pack.
//
// The rows are evaluation.MatrixCase, which is the same case-carrier shape the
// bundled evaluation corpus uses — facts, evidenceAvailability,
// supportedExtensions, and exactly one of expectedDisposition or
// expectedErrorClass. That is not a coincidence to be preserved by hand: the
// rows run through the same comparison the corpus rows do, so a project gets the
// RFC 8785 byte comparison §8.3 defines rather than a looser one written for
// projects, and a row a builder writes can be lifted into a corpus unchanged.
//
// One member is a project's alone: expectedHandoffTarget (ADR-0025), the
// optional assertion about the escalation target §8.3 keeps outside the
// disposition. The corpus manifest's own schema closes its case object, so a
// corpus row cannot state it — which is the shape of the thing rather than a
// restriction imposed on it, because the target a pack configures is a fact
// about that pack and the corpus's packs are fixtures of the specification.
type Matrix struct {
	MatrixVersion string                  `json:"matrixVersion,omitempty"`
	Cases         []evaluation.MatrixCase `json:"cases"`
}

// LoadMatrix reads and checks one matrix document through the rooted reader.
//
// "Well-formed" here is the carrier and the shape, not the outcomes: strict JSON
// with no duplicate member names, a closed set of members, a declared version
// this runtime knows, at least one row, unique row ids, a facts document per
// row, exactly one expectation per row, and a readable expectedHandoffTarget
// where a row states that optional second assertion. Whether a row's expectation
// holds is what packs test answers, and a matrix that cannot be read as rows has
// no answer to give.
func (p *Project) LoadMatrix(entry Pack) (Matrix, error) {
	data, err := p.root.Read(entry.Matrix, MaxMatrixBytes)
	if err != nil {
		return Matrix{}, errors.New(ReadFailureMessage(entry.Matrix, err))
	}
	document, carrierFailure := carrier.Decode(data, carrier.DefaultLimits())
	if carrierFailure != nil {
		return Matrix{}, fmt.Errorf("the matrix is not acceptable JSON: %s", display.Sanitize(carrierFailure.Diagnostic.Message))
	}
	if _, ok := document.(map[string]any); !ok {
		return Matrix{}, errors.New("the matrix root must be a JSON object with a cases array")
	}
	var matrix Matrix
	// Strict decoding is what closes the shape: a misspelled "expectedDispositon"
	// must be an error, not a row that silently expects nothing.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&matrix); err != nil {
		return Matrix{}, fmt.Errorf("the matrix has a member this runtime does not know, or a member of the wrong type: %s", display.Sanitize(err.Error()))
	}
	if matrix.MatrixVersion != "" && matrix.MatrixVersion != MatrixVersion {
		return Matrix{}, fmt.Errorf("the matrix declares matrixVersion %q; this runtime accepts %q", display.Sanitize(matrix.MatrixVersion), MatrixVersion)
	}
	if len(matrix.Cases) == 0 {
		return Matrix{}, errors.New("the matrix declares no rows; an empty matrix tests nothing")
	}
	if len(matrix.Cases) > MaxMatrixCases {
		return Matrix{}, fmt.Errorf("the matrix declares more than the %d supported rows", MaxMatrixCases)
	}
	seen := map[string]bool{}
	for index, row := range matrix.Cases {
		if row.ID == "" {
			return Matrix{}, fmt.Errorf("row %d declares no id; every row is named so a mismatch can be pointed at", index)
		}
		if seen[row.ID] {
			return Matrix{}, fmt.Errorf("row id %q appears more than once", display.Sanitize(row.ID))
		}
		seen[row.ID] = true
		if len(row.Facts) == 0 {
			return Matrix{}, fmt.Errorf("row %q declares no facts document; the evaluator takes one per row", display.Sanitize(row.ID))
		}
		hasDisposition := row.ExpectedDisposition != nil
		hasClass := row.ExpectedErrorClass != ""
		if hasDisposition == hasClass {
			return Matrix{}, fmt.Errorf("row %q must declare exactly one of expectedDisposition and expectedErrorClass: a disposition and an evaluation error are never both produced (§8.4)", display.Sanitize(row.ID))
		}
		if row.ExpectedErrorPhase != "" && !hasClass {
			return Matrix{}, fmt.Errorf("row %q declares an expectedErrorPhase without an expectedErrorClass", display.Sanitize(row.ID))
		}
		// expectedHandoffTarget rides only beside expectedDisposition, on the
		// precedent expectedNodes set on the graph surface: a refused evaluation
		// produces no disposition and no target beside one, so an assertion about
		// the target of a run that never happened is unsatisfiable rather than
		// merely unmet, and a row stating one is refused before it runs.
		if row.ExpectedHandoffTarget != nil && !hasDisposition {
			return Matrix{}, fmt.Errorf("row %q declares an expectedHandoffTarget beside an expected error; a refused evaluation reports no handoff target to compare", display.Sanitize(row.ID))
		}
		// The expectation's own shape is checked here too, through the one decoder
		// the comparator uses: a matrix whose assertion cannot be read is a carrier
		// defect, and packs validate says so before packs test reports it as a
		// mismatching row.
		if row.ExpectedHandoffTarget != nil {
			if _, err := evaluation.DecodeHandoffTarget(row.ExpectedHandoffTarget); err != nil {
				return Matrix{}, fmt.Errorf("row %q declares an expectedHandoffTarget that is neither null nor a {kind, name} object: %s", display.Sanitize(row.ID), display.Sanitize(err.Error()))
			}
		}
	}
	return matrix, nil
}
