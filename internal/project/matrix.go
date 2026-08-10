package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
)

// MatrixVersion is the newest matrixVersion this runtime accepts — the shape a
// matrix written today should declare — on the same single-integer footing as
// ConfigVersion. Older matrices, and the matrices that declare no version at
// all, are still read: see SupportedMatrixVersions.
//
// It moved to "2" for `expectedHandoffTarget` (ADR-0025). A matrix is a **closed
// input**, so VERSIONING.md's rule for those applies rather than the additive
// rule for output: an older reader rejects a document carrying a member it does
// not know instead of ignoring it, so adding a member moves the version whatever
// the addition is — exactly as `graphs` moved configVersion to "2" and `audit`
// moved it to "3". The version gate is what turns "this member is not known"
// into "this matrix needs matrixVersion 2".
const MatrixVersion = "2"

// MatrixVersionDefault is what a matrix declaring no matrixVersion is read as.
// The rows are the specification's own case-carrier shape, and a matrix written
// before any version existed declared nothing; reading silence as the newest
// version would silently admit members that version introduced into a document
// whose author never saw them.
const MatrixVersionDefault = "1"

// SupportedMatrixVersions names every matrixVersion this runtime accepts, newest
// last, so a refusal can say what it would have taken rather than only what was
// wrong.
func SupportedMatrixVersions() []string { return []string{"1", MatrixVersion} }

// matrixRootMembers and matrixRowMembers are the exact member names a matrix
// document may carry, spelled as they must be spelled.
//
// They exist because `encoding/json` matches member names **case-insensitively**
// even under DisallowUnknownFields: `{"Kind":…}`, `{"Facts":…}`, and
// `{"ExpectedHandoffTarget":…}` all decode into the exact members, and a
// document carrying both spellings has one silently overwrite the other in map
// order. A closed shape that admits an alias is not closed, so the member names
// are checked against the carrier-decoded document — where every authored name
// is present verbatim — before anything is decoded into a Go type.
var (
	matrixRootMembers = []string{"matrixVersion", "cases"}
	matrixRowMembers  = []string{
		"id", "origin", "facts", "evidenceAvailability", "supportedExtensions",
		"expectedDisposition", "expectedHandoffTarget", "expectedErrorClass",
		"expectedErrorPhase", "focus", "specSection",
	}
)

// matrixVersionMembers names the members each matrixVersion introduced, so a
// document declaring an older version is refused with the version it would take
// rather than with a generic unknown-member message.
var matrixVersionMembers = map[string]string{"expectedHandoffTarget": "2"}

// Matrix is one pack's instance matrix: the rows a project wrote about its own
// pack.
//
// The rows are evaluation.MatrixCase, which carries the **shared base** the
// bundled evaluation corpus uses — facts, evidenceAvailability,
// supportedExtensions, and exactly one of expectedDisposition or
// expectedErrorClass — and **one project-only extension**,
// expectedHandoffTarget (ADR-0025). That base is not a coincidence to be
// preserved by hand: base rows run through the same comparator the corpus rows
// do, so a project gets the RFC 8785 byte comparison §8.3 defines rather than a
// looser one written for projects.
//
// What the two carriers share is the fields the comparator reads. They are not
// the same document, and the sentence here used to say they were: corpus
// admission additionally requires members a project matrix has no place for —
// pack, origin, supportedExtensions, focus, and specSection are all required by
// the bundled manifest's schema, and a minimal project row declares none of
// them — and that schema closes its case object, so it refuses
// expectedHandoffTarget outright. Lifting a project row into a corpus therefore
// means supplying the corpus-owned members and removing any target assertion;
// what does not have to be rewritten is the expectation, which is the half the
// shared comparator judges.
//
// The target assertion's exclusion is the shape of the thing rather than a
// restriction imposed on it: an escalation target is a fact about the pack that
// declares it, and the corpus's packs are fixtures of the specification rather
// than a project's policy.
type Matrix struct {
	MatrixVersion string                  `json:"matrixVersion,omitempty"`
	Cases         []evaluation.MatrixCase `json:"cases"`
}

// LoadMatrix reads and checks one matrix document through the rooted reader.
//
// "Well-formed" here is the carrier and the shape, not the outcomes: strict JSON
// with no duplicate member names, a closed set of members spelled exactly, a
// declared version this runtime knows, no member a later version introduced, at
// least one row, unique row ids, a facts document per row, exactly one
// expectation per row, and a readable expectedHandoffTarget where a row states
// that optional second assertion. Whether a row's expectation holds is what
// packs test answers, and a matrix that cannot be read as rows has no answer to
// give.
//
// The order of those checks is load-bearing and is not the order they are
// cheapest in. The declared version is settled **first**, off the
// carrier-decoded document, because every later check is a check about a
// particular version's shape: a matrix declaring a version this runtime has
// never heard of must be told so, rather than told that one of its members is
// unknown — which is true, uninformative, and points at the wrong repair.
func (p *Project) LoadMatrix(entry Pack) (Matrix, error) {
	data, err := p.root.Read(entry.Matrix, MaxMatrixBytes)
	if err != nil {
		return Matrix{}, errors.New(ReadFailureMessage(entry.Matrix, err))
	}
	document, carrierFailure := carrier.Decode(data, carrier.DefaultLimits())
	if carrierFailure != nil {
		return Matrix{}, fmt.Errorf("the matrix is not acceptable JSON: %s", display.Sanitize(carrierFailure.Diagnostic.Message))
	}
	root, ok := document.(map[string]any)
	if !ok {
		return Matrix{}, errors.New("the matrix root must be a JSON object with a cases array")
	}
	declared, err := declaredMatrixVersion(root)
	if err != nil {
		return Matrix{}, err
	}
	if err := checkMatrixMembers(root, declared); err != nil {
		return Matrix{}, err
	}
	var matrix Matrix
	// Strict decoding is what closes the shape: a misspelled "expectedDispositon"
	// must be an error, not a row that silently expects nothing. It runs after
	// the member check above, which is the half of "closed" that
	// DisallowUnknownFields does not supply, and it stays because it is also the
	// type check.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&matrix); err != nil {
		return Matrix{}, fmt.Errorf("the matrix has a member this runtime does not know, or a member of the wrong type: %s", display.Sanitize(err.Error()))
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

// declaredMatrixVersion settles which matrixVersion a document is read under,
// before anything version-specific looks at it. An absent member is
// MatrixVersionDefault; a member of the wrong type is refused as such rather
// than coerced; an unknown value names every version this runtime accepts.
func declaredMatrixVersion(root map[string]any) (string, error) {
	value, present := root["matrixVersion"]
	if !present {
		return MatrixVersionDefault, nil
	}
	declared, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("the matrixVersion of the matrix must be a string; this runtime accepts: %s", strings.Join(SupportedMatrixVersions(), ", "))
	}
	if slices.Contains(SupportedMatrixVersions(), declared) {
		return declared, nil
	}
	return "", fmt.Errorf("the matrix declares matrixVersion %q, which this runtime does not support; it accepts: %s", display.Sanitize(declared), strings.Join(SupportedMatrixVersions(), ", "))
}

// checkMatrixMembers holds the carrier-decoded document to the exact member
// names the declared version admits.
//
// It reads the document rather than a decoded struct for one reason: this is
// where an authored member name still exists verbatim. Two defects live here and
// neither is reachable from a Go type — a spelling `encoding/json` case-folds
// into a member this runtime does know, and a member a later matrixVersion
// introduced appearing in a document that declares an earlier one. The second
// is refused with the version it would take, because "expectedHandoffTarget is
// not a member" is a false sentence to print at someone whose only mistake was
// not moving matrixVersion.
func checkMatrixMembers(root map[string]any, declared string) error {
	if err := closedMembers(root, matrixRootMembers, declared, "the matrix"); err != nil {
		return err
	}
	rows, ok := root["cases"].([]any)
	if !ok {
		return nil
	}
	for index, item := range rows {
		row, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("row %d is not a JSON object", index)
		}
		if err := closedMembers(row, matrixRowMembers, declared, fmt.Sprintf("row %d", index)); err != nil {
			return err
		}
	}
	return nil
}

// closedMembers refuses any member of one object that is not exactly one of the
// known names, and any known member the declared version has not introduced yet.
// Members are visited in sorted order so the same document always names the same
// first defect.
func closedMembers(object map[string]any, known []string, declared, subject string) error {
	for _, name := range slices.Sorted(maps.Keys(object)) {
		if !slices.Contains(known, name) {
			for _, candidate := range known {
				if strings.EqualFold(candidate, name) {
					return fmt.Errorf("%s has a member this runtime does not know: %q. The member is spelled %q, and a spelling that differs from it only in case is refused rather than read as it", subject, display.Sanitize(name), candidate)
				}
			}
			return fmt.Errorf("%s has a member this runtime does not know: %q", subject, display.Sanitize(name))
		}
		// Ordered by position in the supported list rather than by the version
		// string, which compares as text and would read "10" as older than "2".
		since, versioned := matrixVersionMembers[name]
		if versioned && slices.Index(SupportedMatrixVersions(), declared) < slices.Index(SupportedMatrixVersions(), since) {
			return fmt.Errorf("%s declares %q, which matrixVersion %s introduced; this matrix is read as matrixVersion %s, so declare matrixVersion %q to use it", subject, name, since, declared, since)
		}
	}
	return nil
}
