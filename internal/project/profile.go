package project

import (
	"fmt"
	"math/big"
	"slices"
	"sync/atomic"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The history profile of ADR-0034.
//
// A matrix whose rows were transcribed from past decisions carries, per row,
// where it came from (origin) and what was recorded (the expectation). This
// file reads the run packs test already made against those origins: which
// history agrees, which of the pack's reachable behaviors each history ever
// exercised, and where each history's cases sit against each line the pack
// draws. It derives nothing that the run and the coverage derivation did not
// already compute, it evaluates nothing a second time, and it decides nothing:
// a disagreement is reported with the origin it came from and the boundary it
// sits against, and what it means is the policy owner's to say.

// matrixProfile builds the profile for one pack's rows, or nil when no row
// declares an origin -- absence of the marker is not a claim. rows are the
// run's results in the matrix's own order, one per case; coverage is derived
// per origin only when the suite's own coverage was (withCoverage). The
// entries the profile would retain are counted before any is, and a profile
// beyond the budget is refused rather than built.
func matrixProfile(pack map[string]any, matrix Matrix, rows []result.EvaluationCorpusCase, withCoverage bool, budget int) (*result.MatrixProfile, *Failure) {
	return profileFor(derivePackProbes(pack, Reach{}), boundaryGroups(comparisonSites(pack)), matrix, rows, withCoverage, budget)
}

// profileFor builds the profile over a derivation made once -- the pack's
// probe set and its boundary groups -- so that many origins cost one reading
// of the pack, and each origin's coverage is a count of predicates over that
// set with nothing rendered.
func profileFor(set packProbeSet, groups []boundaryGroup, matrix Matrix, rows []result.EvaluationCorpusCase, withCoverage bool, budget int) (*result.MatrixProfile, *Failure) {
	if len(rows) != len(matrix.Cases) {
		return nil, nil
	}
	origins := []string{}
	byOrigin := map[string][]int{}
	for index, row := range matrix.Cases {
		if row.Origin == "" {
			continue
		}
		if _, seen := byOrigin[row.Origin]; !seen {
			origins = append(origins, row.Origin)
		}
		byOrigin[row.Origin] = append(byOrigin[row.Origin], index)
	}
	if len(origins) == 0 {
		return nil, nil
	}
	slices.Sort(origins)
	profiled := 0
	for _, origin := range origins {
		profiled += len(byOrigin[origin])
	}
	probes := 0
	if withCoverage {
		probes = set.count() + len(groups)
	}
	work := profileWork(profiled, len(groups), probes)
	if work > budget {
		return nil, &Failure{
			Code: "JPS-RESOURCE-MATRIX-PROFILE",
			Message: fmt.Sprintf("The history profile of this pack would cost %d units of work -- %d rows with an origin against %d comparison boundaries and %d coverage probes -- beyond the %d this runtime spends on one pack. Nothing is truncated and no partial profile is written, because a profile cut short looks exactly like a complete one. The budget is per pack: give fewer rows an origin, or split the pack's comparisons across packs.",
				work, profiled, len(groups), probes, budget),
			ExitCode: result.ExitIO,
		}
	}
	profile := &result.MatrixProfile{}
	for _, origin := range origins {
		agreement := result.OriginAgreement{Origin: capRendered(origin)}
		for _, index := range byOrigin[origin] {
			agreement.Rows++
			if rows[index].Status == "passed" {
				agreement.Passed++
			} else {
				agreement.Mismatched++
			}
		}
		profile.Agreement = append(profile.Agreement, agreement)
	}
	if withCoverage {
		for _, origin := range origins {
			subset := Matrix{MatrixVersion: matrix.MatrixVersion}
			for _, index := range byOrigin[origin] {
				subset.Cases = append(subset.Cases, matrix.Cases[index])
			}
			// The same probes the suite's coverage reports, witnessed by
			// this origin's rows alone: predicates over the derivation
			// made once, nothing rendered.
			profile.Coverage = append(profile.Coverage, result.OriginCoverage{
				Origin:  capRendered(origin),
				Covered: set.covered(matrixWitnesses(subset)) + boundaryCovered(groups, subset),
				Probes:  probes,
			})
		}
	}
	profile.Thresholds = thresholdProfiles(matrix, rows, origins, byOrigin, groups)
	return profile, nil
}

// profileWork is what a profile costs, per row with an origin: its
// agreement, one witnessing of every coverage probe (an outcome or reason
// probe reads the row's expectation; a boundary probe resolves and compares
// its fact), and one placement per boundary (a resolution and a comparison,
// and at most one more comparison for the nearest). Counted in this
// arithmetic before anything is built or witnessed, so the product of two
// bounded inputs is judged rather than spent. Retention is a lesser count:
// origins are at most rows, and every retained string is capped.
func profileWork(rows, groups, probes int) int {
	return rows + rows*probes + rows*groups
}

// thresholdProfiles places each origin's rows against each comparison
// boundary the pack draws: the groups the coverage derivation already forms
// (one per distinct pointer and literal value), each row's fact resolved at
// the pointer and compared by the evaluator's own comparison, so a JSON
// number or an absent fact -- what §7.4 cannot compare -- sits on no side.
// Every decimal is read once: the literal once per group, a row's fact once
// per group, and the nearest value on a side is kept as the number it was
// read into beside the spelling it was written in, so no retained spelling
// is read again for the next row.
func thresholdProfiles(matrix Matrix, rows []result.EvaluationCorpusCase, origins []string, byOrigin map[string][]int, groups []boundaryGroup) []result.ThresholdProfile {
	if len(groups) == 0 {
		return nil
	}
	// Each row's facts are decoded once, whatever the number of boundaries.
	facts := make([]any, len(matrix.Cases))
	decoded := make([]bool, len(matrix.Cases))
	for index, row := range matrix.Cases {
		if row.Origin == "" {
			continue
		}
		if document, failure := carrier.Decode(row.Facts, carrier.DefaultLimits()); failure == nil {
			facts[index], decoded[index] = document, true
		}
	}
	profiles := make([]result.ThresholdProfile, 0, len(groups))
	for _, group := range groups {
		literal, ok := parseDecimal(group.literal)
		if !ok {
			continue // a boundary the derivation admitted is a decimal; defensively, one that is not places nothing
		}
		profile := result.ThresholdProfile{Pointer: capRendered(group.path), Literal: capRendered(group.literal)}
		for _, origin := range origins {
			placed := result.ThresholdOrigin{Origin: capRendered(origin)}
			var nearest, nearestDisagreeing [3]*nearestValue
			for _, index := range byOrigin[origin] {
				if !decoded[index] {
					continue
				}
				value, resolved := evaluation.ResolvePointer(facts[index], group.path)
				if !resolved {
					continue
				}
				number, ok := parseDecimal(value)
				if !ok {
					continue
				}
				side := number.Cmp(literal) + 1 // 0 below, 1 at, 2 above
				disagrees := rows[index].Status != "passed"
				count(&placed, side, disagrees)
				if side == 1 {
					// At the literal every value is the literal; the nearest
					// is any of them, and no comparison chooses among them.
					if nearest[1] == nil {
						nearest[1] = &nearestValue{number: number, spelling: value}
					}
					if disagrees && nearestDisagreeing[1] == nil {
						nearestDisagreeing[1] = &nearestValue{number: number, spelling: value}
					}
					continue
				}
				candidate := &nearestValue{number: number, spelling: value}
				nearest[side] = nearer(nearest[side], candidate, side)
				if disagrees {
					nearestDisagreeing[side] = nearer(nearestDisagreeing[side], candidate, side)
				}
			}
			for side := range 3 {
				at := sideOf(&placed, side)
				at.Nearest = renderFact(nearest[side])
				at.NearestDisagreeing = renderFact(nearestDisagreeing[side])
			}
			profile.Origins = append(profile.Origins, placed)
		}
		profiles = append(profiles, profile)
	}
	return profiles
}

// nearestValue is a compared fact as the number it was read into and the
// spelling it was written in: compared by the one, rendered as the other.
type nearestValue struct {
	number   *big.Rat
	spelling any
}

// decimalParses counts the decimals the profile reads; a test holds a
// profile to one reading per fact per boundary and one per literal.
var decimalParses atomic.Int64

// parseDecimal reads one value through the evaluator's grammar (§7.4), once.
func parseDecimal(value any) (*big.Rat, bool) {
	decimalParses.Add(1)
	return evaluation.DecimalValue(value)
}

// sideOf addresses one side of a placement: 0 below, 1 at, 2 above.
func sideOf(placed *result.ThresholdOrigin, side int) *result.ThresholdSide {
	switch side {
	case 0:
		return &placed.Below
	case 1:
		return &placed.At
	default:
		return &placed.Above
	}
}

func count(placed *result.ThresholdOrigin, side int, disagrees bool) {
	at := sideOf(placed, side)
	at.Rows++
	if disagrees {
		at.Disagreeing++
	}
}

// nearer keeps whichever of two values on one side is closer to the literal:
// the greater of two values below it, the lesser of two above it -- compared
// as the numbers they were read into, so a retained spelling is never read
// again.
func nearer(current, candidate *nearestValue, side int) *nearestValue {
	if current == nil {
		return candidate
	}
	comparison := candidate.number.Cmp(current.number)
	if (side == 0 && comparison > 0) || (side == 2 && comparison < 0) {
		return candidate
	}
	return current
}

// renderFact renders a compared fact as the row wrote it -- a decimal string,
// since that is the only value §7.4 compares -- under the boundary text
// budget; "" for none. The spelling is the row's own, so a reader finds the
// case by the value on file, not by a normalisation of it.
func renderFact(value *nearestValue) string {
	if value == nil {
		return ""
	}
	text, ok := value.spelling.(string)
	if !ok {
		return ""
	}
	return capRendered(text)
}
