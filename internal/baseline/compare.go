package baseline

import (
	"fmt"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The kinds of finding a comparison reports. A pack's findings are reported
// in this order, then its rows: the baseline's rows in the baseline's order,
// then the rows only the candidate has, in the candidate's order.
const (
	FindingPackOnlyInBaseline  = "pack-only-in-baseline"
	FindingPackOnlyInCandidate = "pack-only-in-candidate"
	FindingPackNotRun          = "pack-not-run"
	FindingPackBytes           = "pack-bytes-differ"
	FindingMatrixBytes         = "matrix-bytes-differ"
	FindingPackIdentity        = "pack-identity-differs"
	FindingSpecVersion         = "spec-version-differs"
	FindingRowDiffers          = "row-differs"
	FindingRowOnlyInBaseline   = "row-only-in-baseline"
	FindingRowOnlyInCandidate  = "row-only-in-candidate"
)

// Comparison is what Compare finds: everything a revalidation report states
// beside its header.
type Comparison struct {
	Status    string
	Baselines []result.RevalidationSource
	Summary   result.RevalidationSummary
	Packs     []result.RevalidatedPack
	Findings  []result.RevalidationFinding
}

// Compare holds what this runtime gave for a project's matrices against one
// or more baselines. The baselines are merged by pack id, and a pack that two
// of them hold is refused rather than chosen between. The status is "same"
// exactly when there is no finding; a baseline always holds at least one pack
// with at least one row, so "same" always follows at least one compared row.
func Compare(sources []Source, runs []project.PackRun, noMatrix []string) (Comparison, error) {
	comparison := Comparison{Status: "same", Baselines: make([]result.RevalidationSource, 0, len(sources)), Packs: []result.RevalidatedPack{}, Findings: []result.RevalidationFinding{}}
	held := map[string]Pack{}
	for index, source := range sources {
		ids := make([]string, 0, len(source.Packs))
		for _, pack := range source.Packs {
			if _, twice := held[pack.ID]; twice {
				return Comparison{}, fmt.Errorf("the pack %s is in more than one baseline (the second is baseline %d), and a comparison does not choose between them", shown(pack.ID), index+1)
			}
			held[pack.ID] = pack
			ids = append(ids, pack.ID)
		}
		comparison.Baselines = append(comparison.Baselines, result.RevalidationSource{Form: source.Form, Version: source.Version, Tool: source.Tool, Packs: ids})
	}
	candidates := map[string]project.PackRun{}
	for _, run := range runs {
		candidates[run.ID] = run
	}
	withoutMatrix := map[string]bool{}
	for _, id := range noMatrix {
		withoutMatrix[id] = true
	}
	ids := make([]string, 0, len(held)+len(candidates))
	for id := range held {
		ids = append(ids, id)
	}
	for id := range candidates {
		if _, both := held[id]; !both {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		base, inBaseline := held[id]
		run, inCandidate := candidates[id]
		compared, findings, err := comparePack(id, base, inBaseline, run, inCandidate, withoutMatrix[id])
		if err != nil {
			return Comparison{}, err
		}
		comparison.Summary.Packs++
		if compared.Status == "same" {
			comparison.Summary.PacksSame++
		}
		comparison.Summary.RowsCompared += compared.Rows.Compared
		comparison.Summary.RowsSame += compared.Rows.Same
		switch compared.Bytes {
		case "same", "different":
			comparison.Summary.BytesCompared++
		case "not-recorded":
			comparison.Summary.BytesNotRecorded++
		}
		comparison.Packs = append(comparison.Packs, compared)
		comparison.Findings = append(comparison.Findings, findings...)
	}
	comparison.Summary.Findings = len(comparison.Findings)
	if len(comparison.Findings) > 0 {
		comparison.Status = "different"
	}
	return comparison, nil
}

func comparePack(id string, base Pack, inBaseline bool, run project.PackRun, inCandidate, declaredWithoutMatrix bool) (result.RevalidatedPack, []result.RevalidationFinding, error) {
	compared := result.RevalidatedPack{ID: id, Status: "same", Bytes: "not-compared"}
	if inBaseline {
		compared.Baseline = &result.RevalidatedPackSide{PackID: base.PackID, PackVersion: base.PackVersion, SpecVersion: base.SpecVersion, PackDigest: base.PackDigest, MatrixDigest: base.MatrixDigest, Rows: len(base.Rows)}
	}
	if inCandidate && run.Ran {
		compared.Candidate = &result.RevalidatedPackSide{PackID: run.PackID, PackVersion: run.PackVersion, SpecVersion: run.SpecVersion, PackDigest: run.PackDigest, MatrixDigest: run.MatrixDigest, Rows: len(run.Rows)}
	}
	findings := []result.RevalidationFinding{}
	add := func(finding result.RevalidationFinding) {
		finding.Pack = id
		findings = append(findings, finding)
	}
	switch {
	case !inCandidate:
		detail := "This configuration does not declare the pack."
		if declaredWithoutMatrix {
			detail = "This configuration declares the pack with no matrix, so no row of it ran."
		}
		add(result.RevalidationFinding{Kind: FindingPackOnlyInBaseline, Detail: detail})
	case !inBaseline:
		finding := result.RevalidationFinding{Kind: FindingPackOnlyInCandidate, Detail: "No baseline holds the pack."}
		if !run.Ran {
			finding.Detail = "No baseline holds the pack, and its matrix did not run: " + run.Detail
		}
		add(finding)
	case !run.Ran:
		add(result.RevalidationFinding{Kind: FindingPackNotRun, Detail: "The pack's matrix did not run under this runtime: " + run.Detail})
	default:
		if base.PackDigest == "" {
			compared.Bytes = "not-recorded"
		} else {
			compared.Bytes = "same"
			if base.PackDigest != run.PackDigest {
				compared.Bytes = "different"
				add(result.RevalidationFinding{Kind: FindingPackBytes, Baseline: base.PackDigest, Candidate: run.PackDigest})
			}
			if base.MatrixDigest != run.MatrixDigest {
				compared.Bytes = "different"
				add(result.RevalidationFinding{Kind: FindingMatrixBytes, Baseline: base.MatrixDigest, Candidate: run.MatrixDigest})
			}
		}
		identity := []string{}
		if base.PackID != run.PackID {
			identity = append(identity, "packId")
		}
		if base.PackVersion != run.PackVersion {
			identity = append(identity, "packVersion")
		}
		if len(identity) > 0 {
			add(result.RevalidationFinding{Kind: FindingPackIdentity, Members: identity, Baseline: base.PackID + " " + base.PackVersion, Candidate: run.PackID + " " + run.PackVersion})
		}
		if base.SpecVersion != run.SpecVersion {
			add(result.RevalidationFinding{Kind: FindingSpecVersion, Baseline: base.SpecVersion, Candidate: run.SpecVersion})
		}
		rows, counts, err := compareRows(base.Rows, run.Rows)
		if err != nil {
			return result.RevalidatedPack{}, nil, err
		}
		compared.Rows = counts
		for _, finding := range rows {
			add(finding)
		}
	}
	if len(findings) > 0 {
		compared.Status = "different"
	}
	return compared, findings, nil
}

// compareRows compares one pack's rows by id: a row the same exactly when its
// canonical form is the same bytes on both sides.
func compareRows(base []Row, candidateRows []result.EvaluationCorpusCase) ([]result.RevalidationFinding, result.RevalidatedRowCounts, error) {
	counts := result.RevalidatedRowCounts{}
	findings := []result.RevalidationFinding{}
	candidate := make(map[string]Row, len(candidateRows))
	for _, row := range candidateRows {
		candidate[row.ID] = RowFrom(row)
	}
	inBaseline := make(map[string]bool, len(base))
	for _, row := range base {
		inBaseline[row.ID] = true
		was, err := row.Canonical()
		if err != nil {
			return nil, counts, err
		}
		other, found := candidate[row.ID]
		if !found {
			findings = append(findings, result.RevalidationFinding{Kind: FindingRowOnlyInBaseline, Row: row.ID, Baseline: was})
			continue
		}
		now, err := other.Canonical()
		if err != nil {
			return nil, counts, err
		}
		counts.Compared++
		if was == now {
			counts.Same++
			continue
		}
		findings = append(findings, result.RevalidationFinding{Kind: FindingRowDiffers, Row: row.ID, Members: differingMembers(row, other), Baseline: was, Candidate: now})
	}
	for _, row := range candidateRows {
		if inBaseline[row.ID] {
			continue
		}
		now, err := RowFrom(row).Canonical()
		if err != nil {
			return nil, counts, err
		}
		findings = append(findings, result.RevalidationFinding{Kind: FindingRowOnlyInCandidate, Row: row.ID, Candidate: now})
	}
	return findings, counts, nil
}

// differingMembers names the members two rows of one id differ in, in the
// order the canonical form writes them.
func differingMembers(was, now Row) []string {
	differing := []string{}
	for _, member := range []struct {
		name     string
		was, now string
	}{
		{"disposition", was.Disposition, now.Disposition},
		{"errorClass", was.ErrorClass, now.ErrorClass},
		{"errorPhase", was.ErrorPhase, now.ErrorPhase},
		{"handoffTarget", was.HandoffTarget, now.HandoffTarget},
		{"status", was.Status, now.Status},
	} {
		if member.was != member.now {
			differing = append(differing, member.name)
		}
	}
	return differing
}
