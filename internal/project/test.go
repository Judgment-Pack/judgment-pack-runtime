package project

import (
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Test runs every configured pack's instance matrix through the experimental
// evaluator and reports every row.
//
// A row is judged exactly as a bundled evaluation-corpus row is, by the same
// code: the RFC 8785 canonical disposition compared byte for byte against the
// row's, or the §8.4 error class and phase the row expects. A project therefore
// gets the byte agreement §8.3 defines rather than a looser comparison invented
// for projects, and a row a builder writes is the same artifact a corpus row is.
//
// What this reports is what a project's own rows did. It is not the
// specification's corpus and says nothing about the evaluator's conformance,
// which is stated, in full and only, in CONFORMANCE.md.
//
// A pack with no declared matrix is reported as skipped rather than passed, and
// a run in which no row ran at all is reported "skipped" rather than "passed" —
// which the CLI exits non-zero on. A test command that reported a clean run over
// zero rows would hand a project a green CI gate for a suite that tested
// nothing, and the per-pack rows saying "skipped" underneath it would not undo
// the top line anyone reads.
//
// Beside its rows, every entry whose matrix loaded carries the coverage report
// of ADR-0014: the probe classes the pack's own declarations derive, and which
// of them some row's expectation witnesses. Coverage informs and never gates —
// it moves no status and no exit code.
func (p *Project) Test(evaluator *evaluation.Engine, id, command string) (result.PackTest, *Failure) {
	selected, failure := p.selection(id)
	if failure != nil {
		return result.PackTest{}, failure
	}
	output := result.PackTest{
		OutputVersion:             result.OutputVersion,
		Tool:                      result.CurrentTool(),
		Command:                   command,
		Status:                    "passed",
		Experimental:              true,
		EvaluatorSpecVersion:      result.EvaluatorSpecVersion,
		ConformanceClaimReference: result.EvaluationClaimReference,
		Label:                     result.PackMatrixLabel,
		Kind:                      result.ProjectKind,
		ConfigPath:                p.ConfigPath,
		ConfigVersion:             p.Config.ConfigVersion,
		Packs:                     make([]result.PackTestEntry, 0, len(selected)),
	}
	for _, packID := range selected {
		entry := p.testPack(evaluator, packID, p.Config.Packs[packID], command)
		output.Summary.Total += entry.Summary.Total
		output.Summary.Passed += entry.Summary.Passed
		output.Summary.Mismatched += entry.Summary.Mismatched
		if entry.Status == "mismatch" {
			output.Status = "mismatch"
		}
		output.Packs = append(output.Packs, entry)
	}
	// Zero rows is not a pass, however few packs were selected. An empty packs
	// object is the case a selection-size guard would let through, and it is the
	// worst one: a project that configures nothing would report a clean run over
	// nothing at all. What makes a pass a pass is that a row ran and matched, so
	// the condition is about rows and nothing else. A mismatch already found
	// stands: a pack whose matrix would not load has failed, which is a stronger
	// statement than "nothing ran".
	if output.Status == "passed" && output.Summary.Total == 0 {
		output.Status = "skipped"
	}
	return output, nil
}

// testPack runs one pack's matrix. Anything that stops the run before a row can
// be judged — an unreadable pack, an unreadable or malformed matrix — is a
// mismatch for that pack and not a silent skip: a matrix that will not load has
// not passed.
func (p *Project) testPack(evaluator *evaluation.Engine, id string, entry Pack, command string) result.PackTestEntry {
	report := result.PackTestEntry{
		ID:         id,
		Path:       entry.Path,
		MatrixPath: entry.Matrix,
		Status:     "passed",
		Rows:       []result.EvaluationCorpusCase{},
	}
	if entry.Matrix == "" {
		report.Status = "skipped"
		report.Detail = "The entry declares no matrix, so no row ran for this pack."
		return report
	}
	found, pack, err := p.readIdentity(entry)
	if err != nil {
		// A pack that cannot be read or decoded runs no row, and reporting that as a
		// skip would let a broken pack pass a CI gate. Every row would be refused in
		// any case; saying so once is clearer than saying it per row.
		report.Status = "mismatch"
		report.Detail = ReadFailureMessage(entry.Path, err)
		return report
	}
	report.PackID, report.PackVersion = found.ID, found.Version
	matrix, err := p.LoadMatrix(entry)
	if err != nil {
		report.Status = "mismatch"
		report.Detail = display.Sanitize(err.Error())
		return report
	}
	for _, row := range matrix.Cases {
		outcome := evaluator.RunCase(pack, row, command)
		report.Summary.Total++
		if outcome.Status == "passed" {
			report.Summary.Passed++
		} else {
			report.Summary.Mismatched++
			report.Status = "mismatch"
		}
		report.Rows = append(report.Rows, outcome)
	}
	// Coverage reads the rows' expectations, which exist whether or not they
	// held, so mismatched rows are covered rows too and a mismatching entry
	// still reports what its matrix fails to probe. It is derived only when
	// the evaluator admits the pack for the empty capability set or for some
	// row's declared one: a pack no row's capabilities can get past the
	// preflight never reaches §8, so a derivation over its declarations would
	// describe behavior nothing can reach — and its matrix, rightly full of
	// error rows, would be told it misses probes it can never cover. A row
	// that supports a required extension reaches §8, and its coverage is not
	// forfeited to a stricter set than any row uses.
	if admitsForSomeRow(evaluator, pack, matrix) {
		report.Coverage = matrixCoverage(PackRoot(pack), matrix)
	}
	return report
}

// admitsForSomeRow reports whether the evaluator admits the pack under the
// empty capability set or under at least one row's declared
// supportedExtensions — the sets rows actually evaluate with.
func admitsForSomeRow(evaluator *evaluation.Engine, pack []byte, matrix Matrix) bool {
	if evaluator.Admits(pack, nil) {
		return true
	}
	for _, row := range matrix.Cases {
		if len(row.SupportedExtensions) > 0 && evaluator.Admits(pack, row.SupportedExtensions) {
			return true
		}
	}
	return false
}
