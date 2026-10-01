// Package compare evaluates one set of inputs under two versions of a pack and
// reports the inputs they decide differently (ADR-0045).
//
// It is a rehearsal by construction: it takes two documents and one inputs
// document, opens no project, writes nothing and consults no reviewed set. A
// difference is information about the two documents, not a verdict on either,
// and nothing here reads an expectation.
package compare

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Input is one set of evaluation inputs: an id to report it by, a facts
// document, the evidence availability when one is stated, and the supported
// extensions a matrix row declares.
type Input struct {
	ID                  string
	Facts               json.RawMessage
	Evidence            json.RawMessage
	SupportedExtensions []string
}

// MaxReportBytes bounds what one comparison retains: the differences it lists,
// both sides of each, charged as the compact JSON the CLI writes them in (no
// HTML escaping, no indentation). A difference repeats two dispositions and two
// handoff targets whose strings a pack may make large, and a matrix may carry
// ten thousand rows, so the differences are charged as they are built and the
// run is refused rather than truncated past this, as a matrix is at its own
// limit. The rendered output adds the packs' identities and the framing, and
// --pretty or the human rendering add their own layout: it is bounded by this
// and proportional to it, not equal to it.
const MaxReportBytes = project.MaxMatrixBytes

// ErrReportTooLarge is the one failure Run returns: the differences would not
// fit MaxReportBytes.
var ErrReportTooLarge = errors.New("the comparison's report would exceed its byte limit")

// Side is one of the two packs as the caller read it: the path it names, its
// bytes, and whether the bounded read stopped at the limit.
type Side struct {
	Path      string
	Pack      []byte
	Oversized bool
}

// ReadInputs reads an inputs document: a candidates document from packs
// suggest when its root declares candidatesVersion, and a matrix otherwise,
// each held to its own closed shape. A matrix row's expectation is read by the
// matrix loader, as every row's is, and never used.
func ReadInputs(data []byte) (string, []Input, error) {
	document, carrierFailure := carrier.Decode(data, carrier.DefaultLimits())
	if carrierFailure == nil {
		if root, ok := document.(map[string]any); ok {
			if _, candidates := root["candidatesVersion"]; candidates {
				decoded, err := project.DecodeCandidates(data)
				if err != nil {
					return "", nil, err
				}
				inputs := make([]Input, 0, len(decoded.Candidates))
				for _, candidate := range decoded.Candidates {
					inputs = append(inputs, Input{ID: candidate.ID, Facts: candidate.Facts, Evidence: candidate.EvidenceAvailability})
				}
				return "candidates", inputs, nil
			}
		}
	}
	matrix, err := project.DecodeMatrix(data)
	if err != nil {
		return "", nil, errors.New("the inputs document is neither a matrix nor a candidates document: " + err.Error())
	}
	inputs := make([]Input, 0, len(matrix.Cases))
	for _, row := range matrix.Cases {
		inputs = append(inputs, Input{ID: row.ID, Facts: row.Facts, Evidence: row.EvidenceAvailability, SupportedExtensions: row.SupportedExtensions})
	}
	return "matrix", inputs, nil
}

// outcome is one pack's result for one input.
type outcome struct {
	evaluated *result.Evaluation
	canonical []byte
	refused   *result.ComparedRefusal
}

// Run evaluates every input under both packs and reports the differences.
// Each evaluation is the one packs test makes for a row: the input's facts and
// evidence availability, and its supported extensions joined by the caller's.
// A pack is admitted once per side, as packs test admits it once per matrix.
func Run(engine *evaluation.Engine, oldPack, newPack Side, kind string, inputs []Input, supported []string, command string) (result.PackComparison, error) {
	comparison := result.PackComparison{
		OutputVersion:             result.OutputVersion,
		Tool:                      result.CurrentTool(),
		Command:                   command,
		Status:                    "valid",
		Experimental:              true,
		Rehearsal:                 true,
		ConformanceClaimReference: result.EvaluationClaimReference,
		Label:                     result.ComparisonLabel,
		EvaluatorSpecVersion:      result.EvaluatorSpecVersion,
		Old:                       result.ComparedPack{Path: oldPack.Path, Digest: digest(oldPack)},
		New:                       result.ComparedPack{Path: newPack.Path, Digest: digest(newPack)},
		Inputs:                    result.ComparedInputs{Kind: kind, Count: len(inputs)},
		Differences:               []result.InputDifference{},
	}
	oldAdmitted, newAdmitted := engine.AdmitPack(oldPack.Pack), engine.AdmitPack(newPack.Pack)
	charged := int64(0)
	for _, input := range inputs {
		extensions := slices.Clone(input.SupportedExtensions)
		for _, name := range supported {
			if !slices.Contains(extensions, name) {
				extensions = append(extensions, name)
			}
		}
		before := evaluate(engine, oldAdmitted, oldPack, input, extensions, command, &comparison.Old)
		after := evaluate(engine, newAdmitted, newPack, input, extensions, command, &comparison.New)
		changed := differences(before, after)
		if len(changed) == 0 {
			comparison.Inputs.Same++
			continue
		}
		comparison.Inputs.Different++
		difference := result.InputDifference{ID: input.ID, Changed: changed, Old: side(before), New: side(after)}
		var encoded bytes.Buffer
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(difference); err != nil {
			return result.PackComparison{}, err
		}
		if charged += int64(encoded.Len()); charged > MaxReportBytes {
			return result.PackComparison{}, ErrReportTooLarge
		}
		comparison.Differences = append(comparison.Differences, difference)
	}
	return comparison, nil
}

// digest names one pack's exact bytes, or nothing when the bounded read
// stopped at the limit and the bytes in hand are not the document's.
func digest(pack Side) string {
	if pack.Oversized {
		return ""
	}
	return audit.Digest(pack.Pack)
}

// evaluate runs one input under one pack and, the first time an evaluation of
// it succeeds, records the pack's own id and version on its report.
func evaluate(engine *evaluation.Engine, admitted *evaluation.AdmittedPack, pack Side, input Input, extensions []string, command string, named *result.ComparedPack) outcome {
	options := evaluation.Options{Command: command, SupportedExtensions: extensions}
	if pack.Oversized {
		options.OversizedInputs = []string{"pack"}
	}
	evaluated, failure := engine.EvaluateAdmitted(admitted, input.Facts, input.Evidence, options)
	if failure != nil {
		return outcome{refused: &result.ComparedRefusal{Class: failure.Class, Phase: failure.Phase, Code: failure.Code}}
	}
	if named.PackID == "" && named.PackVersion == "" {
		named.PackID, named.PackVersion = evaluated.PackID, evaluated.PackVersion
	}
	canonical, err := evaluated.Disposition.Canonical()
	if err != nil {
		return outcome{refused: &result.ComparedRefusal{Code: "JPS-COMPARE-CANONICAL"}}
	}
	return outcome{evaluated: &evaluated, canonical: canonical}
}

// differences names what differs between two results, or nothing when they
// are the same. Two refusals are the same when class, phase and code are; a
// refusal and a disposition always differ; two dispositions differ when their
// §8.3 canonical bytes or their handoff targets do.
func differences(before, after outcome) []string {
	if before.refused != nil || after.refused != nil {
		if before.refused != nil && after.refused != nil && *before.refused == *after.refused {
			return nil
		}
		return []string{"refusal"}
	}
	sameTarget := reflect.DeepEqual(before.evaluated.HandoffTarget, after.evaluated.HandoffTarget)
	if bytes.Equal(before.canonical, after.canonical) && sameTarget {
		return nil
	}
	was, now := before.evaluated.Disposition, after.evaluated.Disposition
	changed := []string{}
	if was.Kind != now.Kind {
		changed = append(changed, "kind")
	}
	if was.OutcomeID != now.OutcomeID {
		changed = append(changed, "outcomeId")
	}
	if !slices.Equal(was.Reasons, now.Reasons) {
		changed = append(changed, "reasons")
	}
	if !reflect.DeepEqual(was.Handoff, now.Handoff) {
		changed = append(changed, "handoff")
	}
	if !reflect.DeepEqual(was.Value, now.Value) {
		changed = append(changed, "value")
	}
	if !sameTarget {
		changed = append(changed, "handoffTarget")
	}
	if len(changed) == 0 {
		// The canonical bytes differ in a way no named member shows. The bytes
		// decide, so the input is still reported, under the one name that is
		// true of it.
		changed = append(changed, "disposition")
	}
	return changed
}

// side is one result as the report states it.
func side(got outcome) result.ComparedSide {
	if got.refused != nil {
		return result.ComparedSide{EvaluationError: got.refused}
	}
	disposition := got.evaluated.Disposition
	return result.ComparedSide{Disposition: &disposition, HandoffTarget: got.evaluated.HandoffTarget}
}
