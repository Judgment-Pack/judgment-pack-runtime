package project

import (
	"fmt"

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
// for projects. What the two carriers share is the fields that comparator reads,
// which is what makes it one comparison — not the shape of a document: corpus
// admission requires members a project row need not declare and refuses the one
// a project row may add (ADR-0012's 2026-08-09 correction, ADR-0025).
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
// of ADR-0014 and ADR-0023: the probe classes the pack's own declarations
// derive, and which of them some row states — a disposition probe through what
// a row expects, a comparison boundary through what a row's facts place at the
// literal. Coverage informs and never gates — it moves no status and no exit
// code.
//
// Beside that, an entry whose matrix loaded carries the origins its rows
// declare, counted (ADR-0024). It is a measurement of how a suite's inputs were
// supplied, not a judgement of them: the member that decides a row is its
// expectation, and an expectation is authored whatever supplied its facts. Like
// coverage it moves no status and no exit code, and unlike coverage there is
// nothing here to satisfy.
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
	// The aggregate handoff-target budget is one counter for the whole run, not
	// one per pack: what it bounds is what the report retains, and a report is
	// one document however many packs contributed to it (ADR-0025).
	spent := 0
	for _, packID := range selected {
		entry, failure := p.testPack(evaluator, packID, p.Config.Packs[packID], command, &spent)
		if failure != nil {
			return result.PackTest{}, failure
		}
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
//
// spent carries the run's aggregate handoff-target budget across packs. Crossing
// it is the one thing here that is a *failure* rather than a mismatch: a
// mismatch is a statement about a pack and a row, and "this report does not fit"
// is a statement about neither.
func (p *Project) testPack(evaluator *evaluation.Engine, id string, entry Pack, command string, spent *int) (result.PackTestEntry, *Failure) {
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
		return report, nil
	}
	found, pack, err := p.readIdentity(entry)
	if err != nil {
		// A pack that cannot be read or decoded runs no row, and reporting that as a
		// skip would let a broken pack pass a CI gate. Every row would be refused in
		// any case; saying so once is clearer than saying it per row.
		report.Status = "mismatch"
		report.Detail = ReadFailureMessage(entry.Path, err)
		return report, nil
	}
	report.PackID, report.PackVersion = found.ID, found.Version
	matrix, err := p.LoadMatrix(entry)
	if err != nil {
		report.Status = "mismatch"
		report.Detail = display.Sanitize(err.Error())
		return report, nil
	}
	// The origins the rows declare are counted before any of them runs, because
	// the count is about the row documents and not about what running them
	// produced. It moves no status: the member that decides a row is its
	// expectation, and an expectation is authored whatever supplied the facts
	// (ADR-0024).
	report.Origins = MatrixOrigins(matrix)
	admitted := evaluator.AdmitPack(pack)
	// The pack's declared escalation target is rendered here, once, before any
	// row runs — and only when some row asks about one (ADR-0025). This is the
	// place that owns the row loop and the place a pack is loaded, so "once per
	// pack per run" is a property of where the call sits rather than of a cache
	// that has to decide whether to answer. §8.1 gives a pack one escalation
	// target and the rendering is a function of the pack's bytes alone, so
	// nothing below this line can need a second one.
	//
	// The rows run sequentially, one after another in this loop. Nothing here is
	// shared *concurrently*: the rendering is read by every row and written by
	// none, and the aggregate budget below is deliberately carried across the
	// successive packs of one run — it bounds a report, and a report is one
	// document however many packs contributed to it.
	var declaredTarget evaluation.HandoffTargetRendering
	if assertsHandoffTarget(matrix) {
		declaredTarget = evaluator.PackHandoffTarget(pack)
	}
	for _, row := range matrix.Cases {
		outcome := evaluator.RunCaseAdmitted(admitted, row, declaredTarget, command)
		// Charged as the row's result is composed, before it is retained, so the
		// refusal fires instead of the report being built and then rejected. Only
		// the two target renderings are charged: they are the members whose size a
		// pack's own authored string decides, and the budget's number was derived
		// from exactly them (ADR-0025).
		*spent += len(outcome.ExpectedHandoffTarget) + len(outcome.ActualHandoffTarget)
		if *spent > p.handoffBudget() {
			return report, &Failure{
				Code: "JPS-RESOURCE-MATRIX-HANDOFF-TARGETS",
				Message: fmt.Sprintf("The handoff-target renderings of this run exceed the %d-byte budget this runtime retains for them, reached at row %q of pack %q. Each rendering is already bounded at %d bytes, so crossing this is a row count rather than one long target: assert expectedHandoffTarget on the rows that probe an escalation path rather than on every row, or select one pack at a time. Nothing is truncated and no partial report is written, because a report cut short looks exactly like a complete one.",
					p.handoffBudget(), display.Sanitize(row.ID), display.Sanitize(id), result.HandoffTargetBudget),
				ExitCode: result.ExitIO,
			}
		}
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
	if admitsForSomeRow(admitted, matrix) {
		report.Coverage = matrixCoverage(PackRoot(pack), matrix)
	}
	return report, nil
}

// assertsHandoffTarget reports whether any row of one matrix declares the
// optional target assertion. A suite that asks nothing about the escalation
// target renders nothing and decodes nothing extra: absent stays absent in the
// work a run does, not only in the payload it writes (ADR-0025).
func assertsHandoffTarget(matrix Matrix) bool {
	for _, row := range matrix.Cases {
		if row.ExpectedHandoffTarget != nil {
			return true
		}
	}
	return false
}

// admitsForSomeRow reports whether the pack is admitted under the empty
// capability set or under at least one row's declared supportedExtensions —
// the sets rows actually evaluate with, read from the same admissions the
// rows already computed (issue #78: probing coverage must not re-validate
// the pack per row).
func admitsForSomeRow(admitted *evaluation.AdmittedPack, matrix Matrix) bool {
	if admitted.Admits(nil) {
		return true
	}
	for _, row := range matrix.Cases {
		if len(row.SupportedExtensions) > 0 && admitted.Admits(row.SupportedExtensions) {
			return true
		}
	}
	return false
}
