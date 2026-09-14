package project

import (
	"slices"

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
// per origin only when the suite's own coverage was (withCoverage).
func matrixProfile(pack map[string]any, matrix Matrix, rows []result.EvaluationCorpusCase, withCoverage bool) *result.MatrixProfile {
	if len(rows) != len(matrix.Cases) {
		return nil
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
		return nil
	}
	slices.Sort(origins)
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
			coverage := result.OriginCoverage{Origin: capRendered(origin)}
			for _, probe := range matrixCoverage(pack, subset) {
				coverage.Probes++
				if probe.Status == result.MatrixProbeCovered {
					coverage.Covered++
				}
			}
			profile.Coverage = append(profile.Coverage, coverage)
		}
	}
	profile.Thresholds = thresholdProfiles(pack, matrix, rows, origins, byOrigin)
	return profile
}

// thresholdProfiles places each origin's rows against each comparison
// boundary the pack draws: the groups the coverage derivation already forms
// (one per distinct pointer and literal value), each row's fact resolved at
// the pointer and compared by the evaluator's own comparison, so a JSON
// number or an absent fact -- what §7.4 cannot compare -- sits on no side.
func thresholdProfiles(pack map[string]any, matrix Matrix, rows []result.EvaluationCorpusCase, origins []string, byOrigin map[string][]int) []result.ThresholdProfile {
	groups := boundaryGroups(comparisonSites(pack))
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
		profile := result.ThresholdProfile{Pointer: capRendered(group.path), Literal: capRendered(group.literal)}
		for _, origin := range origins {
			placed := result.ThresholdOrigin{Origin: capRendered(origin)}
			var nearest [3]any
			var nearestDisagreeing [3]any
			for _, index := range byOrigin[origin] {
				if !decoded[index] {
					continue
				}
				value, resolved := evaluation.ResolvePointer(facts[index], group.path)
				if !resolved {
					continue
				}
				comparison, comparable := evaluation.DecimalCompare(value, group.literal)
				if !comparable {
					continue
				}
				side := comparison + 1 // 0 below, 1 at, 2 above
				disagrees := rows[index].Status != "passed"
				count(&placed, side, disagrees)
				nearest[side] = nearer(nearest[side], value, side)
				if disagrees {
					nearestDisagreeing[side] = nearer(nearestDisagreeing[side], value, side)
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
// the greater of two values below it, the lesser of two above it, and either
// of two at it. Both are decimal strings the evaluator could compare against
// the literal, so it can compare them with each other.
func nearer(current, candidate any, side int) any {
	if current == nil {
		return candidate
	}
	comparison, comparable := evaluation.DecimalCompare(candidate, current)
	if !comparable {
		return current
	}
	if (side == 0 && comparison > 0) || (side == 2 && comparison < 0) {
		return candidate
	}
	return current
}

// renderFact renders a compared fact as the row wrote it -- a decimal string,
// since that is the only value §7.4 compares -- under the boundary text
// budget; "" for none. The spelling is the row's own, so a reader finds the
// case by the value on file, not by a normalisation of it.
func renderFact(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return capRendered(text)
}
