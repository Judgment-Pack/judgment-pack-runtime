package project

import (
	"fmt"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// MaxMatrixDispositionReportBytes bounds exact expected and actual canonical
// disposition text retained across a Core 0.3 test run. Route names are not
// truncated, and exceeding the budget refuses the whole report.
const MaxMatrixDispositionReportBytes = 10 << 20

type matrixRun struct {
	targets      int
	dispositions int
	contract     result.EvaluatorContract
	// options are the caller's; the coverage requirements in them are held
	// per pack, after that pack's rows ran (ADR-0080).
	options TestOptions
}

func (p *Project) dispositionBudget() int {
	if p.dispositionReportBudget > 0 {
		return p.dispositionReportBudget
	}
	return MaxMatrixDispositionReportBytes
}

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
// literal. By default coverage informs and never gates — it moves no status
// and no exit code. Only a requirement the caller asks for in TestOptions
// makes it gate (ADR-0080).
//
// Beside that, an entry whose matrix loaded carries the origins its rows
// declare, counted (ADR-0024). It is a measurement of how a suite's inputs were
// supplied, not a judgement of them: the member that decides a row is its
// expectation, and an expectation is authored whatever supplied its facts. Like
// coverage it moves no status and no exit code, and unlike coverage there is
// nothing here to satisfy.
func (p *Project) Test(evaluator *evaluation.Engine, id, command string) (result.PackTest, *Failure) {
	return p.TestWith(evaluator, id, command, TestOptions{})
}

// TestOptions are a packs test run's caller-chosen options.
type TestOptions struct {
	// RequireMatrix makes a declared pack with no matrix fail the run instead
	// of being skipped (ADR-0042). It is the caller's opt-in, per run: the
	// default stays skipped, because a project may declare a pack before it
	// has written its rows. It gates nothing derived: a missing matrix is a
	// declared pack that nothing checks, not a probe the pack's content
	// derived. Derived coverage gates a run only through MinCoverage and
	// RequireBoundaries (ADR-0080).
	RequireMatrix bool
	// MinCoverage, when not nil, is a percentage from 0 to 100: each pack
	// whose matrix ran must have at least that share of its derived probes
	// witnessed by a row, or it fails the run (ADR-0080). A pack for which no
	// coverage was derived has no share to measure and does not meet it. A
	// pack with no matrix is untouched; RequireMatrix is what fails one.
	MinCoverage *int
	// RequireBoundaries makes each pack whose matrix ran fail the run when a
	// boundary probe its comparisons derive is witnessed by no row
	// (ADR-0080). A pack that compares nothing in order derives none and
	// meets it.
	RequireBoundaries bool
	// Strength, when not nil, also runs the EXPERIMENTAL test-strength
	// variants of ADR-0084 for each pack whose matrix ran: changes derived
	// from the pack's own declarations, each run against that pack's rows.
	// By default it informs and moves no status; FailOnSurvivor makes a
	// pack with a surviving variant, or with none run, fail the run. A pack
	// with no matrix is untouched, as RequireMatrix leaves it.
	Strength *StrengthOptions
}

// CheckMinCoverage refuses a required coverage outside 0 to 100, and accepts
// nil, which asks for none.
func CheckMinCoverage(level *int) *Failure {
	if level != nil && (*level < 0 || *level > 100) {
		return &Failure{Code: "JPS-INVOCATION-MIN-COVERAGE", Message: "--min-coverage must be a whole percentage from 0 to 100.", ExitCode: result.ExitInvocation}
	}
	return nil
}

// TestWith is Test with options.
func (p *Project) TestWith(evaluator *evaluation.Engine, id, command string, options TestOptions) (result.PackTest, *Failure) {
	if failure := CheckMinCoverage(options.MinCoverage); failure != nil {
		return result.PackTest{}, failure
	}
	if failure := CheckStrength(options.Strength); failure != nil {
		return result.PackTest{}, failure
	}
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
		RequireMatrix:             options.RequireMatrix,
		Packs:                     make([]result.PackTestEntry, 0, len(selected)),
		MinCoverage:               options.MinCoverage,
		RequireBoundaries:         options.RequireBoundaries,
	}
	if options.Strength != nil {
		output.Strength = &result.StrengthRun{Label: result.StrengthLabel, MaxVariants: options.Strength.MaxVariants, FailOnSurvivor: options.Strength.FailOnSurvivor}
	}
	// The aggregate handoff-target budget is one counter for the whole run, not
	// one per pack: what it bounds is what the report retains, and a report is
	// one document however many packs contributed to it (ADR-0025).
	// Negotiate every participating pack before any matrix acquisition or row.
	// Like graph preflight, this retains no pack buffers; each execution read
	// must still agree with the selected contract. Packs without matrices skip.
	run := &matrixRun{options: options}
	for _, packID := range selected {
		entry := p.Config.Packs[packID]
		if entry.Matrix == "" {
			continue
		}
		pack, err := p.ReadPack(entry)
		if err != nil {
			// Existing per-pack mismatch behavior handles unreadable packs.
			continue
		}
		if _, failure := run.selectPackContract(pack); failure != nil {
			return result.PackTest{}, failure
		}
	}
	for _, packID := range selected {
		entry, failure := p.testPack(evaluator, packID, p.Config.Packs[packID], command, run)
		if failure != nil {
			return result.PackTest{}, failure
		}
		if options.RequireMatrix && p.Config.Packs[packID].Matrix == "" {
			entry.Status = "mismatch"
			entry.Detail = "The entry declares no matrix, and this run requires every selected pack to have one, so the pack has not passed."
		}
		if output.Strength != nil {
			// A pack that never reached its rows -- no matrix, or a pack or
			// matrix that would not load -- ran no variant either, and says so.
			if entry.Strength == nil {
				detail := "The pack's own matrix did not run, so no variant ran."
				if p.Config.Packs[packID].Matrix == "" {
					detail = "The entry declares no matrix, so no variant ran."
				}
				entry.Strength = skippedStrength(detail)
			}
			output.Strength.Summary.Derived += entry.Strength.Counts.Derived
			output.Strength.Summary.Noticed += entry.Strength.Counts.Noticed
			output.Strength.Summary.Survived += entry.Strength.Counts.Survived
			output.Strength.Summary.NotDerivable += entry.Strength.Counts.NotDerivable
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
	if run.contract.SpecVersion != "" {
		output.EvaluatorSpecVersion = run.contract.Version()
	}
	return output, nil
}

// selectPackContract checks only root declarations, never a matrix or row.
// Malformed legacy packs retain their existing per-row or per-pack refusals.
func (run *matrixRun) selectPackContract(pack []byte) (result.EvaluatorContract, *Failure) {
	contract, selectionFailure := evaluation.SelectContract(pack)
	if selectionFailure != nil {
		if selectionFailure.Code == "profile-unsupported" {
			return contract, &Failure{Code: selectionFailure.Code, Profile: selectionFailure.Profile, ExitCode: selectionFailure.ExitCode}
		}
		return contract, nil
	}
	if run.contract.SpecVersion != "" && run.contract.Version() != contract.Version() {
		return contract, &Failure{Code: "JPS-MATRIX-MIXED-CONTRACT", Message: "A test run requires one exact evaluator contract; select packs of one Core version.", ExitCode: result.ExitUnsupported}
	}
	run.contract = contract
	return contract, nil
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
func (p *Project) testPack(evaluator *evaluation.Engine, id string, entry Pack, command string, run *matrixRun) (result.PackTestEntry, *Failure) {
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
	pack, err := p.ReadPack(entry)
	var contract result.EvaluatorContract
	if err == nil {
		var failure *Failure
		contract, failure = run.selectPackContract(pack)
		if failure != nil {
			return report, failure
		}
	}
	var found identity
	if err == nil {
		found, err = identityFrom(pack)
	}
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
	return p.testLoadedPack(evaluator, id, report, pack, matrix, command, run, contract)
}

func (p *Project) testLoadedPack(evaluator *evaluation.Engine, id string, report result.PackTestEntry, pack []byte, matrix Matrix, command string, run *matrixRun, contract result.EvaluatorContract) (result.PackTestEntry, *Failure) {
	// The origins the rows declare are counted before any of them runs, because
	// the count is about the row documents and not about what running them
	// produced. It moves no status: the member that decides a row is its
	// expectation, and an expectation is authored whatever supplied the facts
	// (ADR-0024).
	report.Origins = MatrixOrigins(matrix)
	matrix.DispositionContract = contract
	admitted := evaluator.AdmitPack(pack)
	// The pack's declared escalation target is rendered here, once, before any
	// row runs — and only when some row asks about one (ADR-0025). This is the
	// place that owns the row loop and the place a pack is loaded, so "once per
	// pack per run" is a property of where the call sits rather than of a cache
	// that has to decide whether to answer. Target-only packs have one escalation
	// target and the rendering is a function of the pack's bytes alone, so
	// nothing below this line can need a second one.
	//
	// The rows run sequentially, one after another in this loop. Nothing here is
	// shared *concurrently*: the rendering is read by every row and written by
	// none, and the aggregate budget below is deliberately carried across the
	// successive packs of one run — it bounds a report, and a report is one
	// document however many packs contributed to it.
	root := PackRoot(pack)
	escalation, _ := root["escalation"].(map[string]any)
	_, routed := escalation["routes"]
	var declaredTarget evaluation.HandoffTargetRendering
	if assertsHandoffTarget(matrix) && !routed {
		declaredTarget = evaluator.PackHandoffTarget(pack)
	}
	for _, row := range matrix.Cases {
		outcome := judgeRow(evaluator, admitted, row, declaredTarget, command, contract, routed)
		if contract.Core030Rules() {
			run.dispositions += len(outcome.Expected) + len(outcome.Actual)
			if run.dispositions > p.dispositionBudget() {
				return report, &Failure{Code: "JPS-RESOURCE-MATRIX-DISPOSITIONS", Message: "The exact canonical dispositions exceed the Core 0.3 test report byte budget; select fewer cases or packs. No partial report is returned.", ExitCode: result.ExitIO}
			}
		}
		// The row's citations, as the values it declared (ADR-0034): parsed
		// by the grammar LoadMatrix held them to, so what the report carries
		// is what the gateway would resolve -- session, index, signature --
		// and never a re-spelling of the row's bytes.
		outcome.Cites = rowCitations(row)
		// Charged as the row's result is composed, before it is retained, so the
		// refusal fires instead of the report being built and then rejected. Only
		// the two target renderings are charged: they are the members whose size a
		// pack's own authored string decides, and the budget's number was derived
		// from exactly them (ADR-0025).
		run.targets += len(outcome.ExpectedHandoffTarget) + len(outcome.ActualHandoffTarget)
		if run.targets > p.handoffBudget() {
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
	// The pack's probes and boundaries are derived once here and read by
	// the suite's coverage and by the profile's every origin alike: a pack
	// is read one time however many origins its matrix declares.
	set, groups := derivePackProbes(root, Reach{}), boundaryGroups(comparisonSites(root))
	withCoverage := admitsForSomeRowWith(admitted, matrix, contract)
	var boundaries []result.MatrixProbe
	if withCoverage {
		// coverageFor, with the boundary probes kept apart for the
		// boundary requirement: the same probes in the same order.
		boundaries = boundaryProbesFor(groups, matrix)
		report.Coverage = append(set.probes(matrixWitnesses(matrix)), boundaries...)
	}
	run.holdToRequirements(&report, withCoverage, len(groups), boundaries)
	// The profile reads what this run already produced -- the rows' statuses,
	// the same derivation -- against the origins the rows declare, and moves
	// no status (ADR-0034). Coverage by origin is derived exactly when the
	// suite's own coverage was.
	profile, failure := profileFor(set, groups, matrix, report.Rows, withCoverage, p.profileBudget())
	if failure != nil {
		return report, failure
	}
	report.Profile = profile
	// The test-strength run reads what the rows above already established --
	// whether every one held, and whether any reaches §8 -- and runs only when
	// the caller asked for it, after everything a run without it reports
	// (ADR-0084).
	if run.options.Strength != nil {
		strength, failure := strengthOf(evaluator, strengthTarget{
			id: id, pack: pack, matrix: matrix, contract: contract, command: command,
			rows: report.Summary.Total, mismatched: report.Summary.Mismatched, admitted: withCoverage,
		}, *run.options.Strength)
		if failure != nil {
			return report, failure
		}
		report.Strength = strength
		holdToSurvivors(&report, *run.options.Strength)
	}
	return report, nil
}

// judgeRow judges one matrix row against an admitted pack: the corpus
// comparison, then the one Core 0.3 refusal a project row adds -- an
// expectedHandoffTarget against a routed escalation is a mismatch (ADR-0025).
// The pack's own rows and every test-strength variant (ADR-0084) are judged
// by this one function, so a variant is noticed exactly when packs test would
// have reported the row it ran as a mismatch.
func judgeRow(evaluator *evaluation.Engine, admitted *evaluation.AdmittedPack, row evaluation.MatrixCase, declaredTarget evaluation.HandoffTargetRendering, command string, contract result.EvaluatorContract, routed bool) result.EvaluationCorpusCase {
	outcome := evaluator.RunCaseAdmittedWith(admitted, row, declaredTarget, command, evaluation.Options{SpecVersion: contract.Version()})
	if contract.Core030Rules() && row.ExpectedHandoffTarget != nil && routed {
		outcome.Status = "mismatch"
		outcome.Detail = "expectedHandoffTarget is incompatible with a routed escalation; assert the complete disposition handoff.routes instead."
	}
	return outcome
}

// plural is one when n is 1 and many otherwise.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// holdToRequirements fails one entry whose matrix ran on each coverage
// requirement the caller asked for and the entry does not meet (ADR-0080).
// measured is whether coverage was derived for the pack; groups is how many
// boundaries its comparisons derive, and boundaries their probes when
// measured. An unmet requirement makes the entry a mismatch, names the
// requirement in UnmetRequirements and says why in Detail; a met one, or one
// not asked for, changes nothing.
func (run *matrixRun) holdToRequirements(report *result.PackTestEntry, measured bool, groups int, boundaries []result.MatrixProbe) {
	var unmet, details []string
	if level := run.options.MinCoverage; level != nil {
		covered, total := coveredIn(report.Coverage), len(report.Coverage)
		switch {
		case !measured || total == 0:
			unmet = append(unmet, result.RequirementMinCoverage)
			details = append(details, fmt.Sprintf("This run requires at least %d%% of the derived probes to be witnessed by a row, and no coverage was derived for this pack, so the level is not met.", *level))
		case covered*100 < *level*total:
			unmet = append(unmet, result.RequirementMinCoverage)
			details = append(details, fmt.Sprintf("This run requires at least %d%% of the derived probes to be witnessed by a row, and %d/%d are, so the level is not met.", *level, covered, total))
		}
	}
	if run.options.RequireBoundaries && groups > 0 {
		missing := len(boundaries) - coveredIn(boundaries)
		switch {
		case !measured:
			unmet = append(unmet, result.RequirementBoundaries)
			details = append(details, fmt.Sprintf("This run requires every derived boundary probe to be witnessed by a row, and no coverage was derived for this pack, so its %d %s not checked.", groups, plural(groups, "boundary was", "boundaries were")))
		case missing > 0:
			unmet = append(unmet, result.RequirementBoundaries)
			details = append(details, fmt.Sprintf("This run requires every derived boundary probe to be witnessed by a row, and %d of %d %s not.", missing, len(boundaries), plural(missing, "is", "are")))
		}
	}
	if len(unmet) == 0 {
		return
	}
	report.Status = "mismatch"
	report.UnmetRequirements = unmet
	if report.Detail != "" {
		details = append([]string{report.Detail}, details...)
	}
	report.Detail = strings.Join(details, " ")
}

// rowCitations is a row's citations as values, or nil for a row that cites
// nothing (an absent member, or an empty array, which is the same claim). The
// parse cannot fail here -- LoadMatrix refused any row whose citations it
// could not read -- and a row that somehow reaches this with unreadable ones
// reports none rather than a guess.
func rowCitations(row evaluation.MatrixCase) []result.Citation {
	if row.Cites == nil {
		return nil
	}
	parsed, err := audit.ParseCites(row.Cites)
	if err != nil || len(parsed) == 0 {
		return nil
	}
	cites := make([]result.Citation, 0, len(parsed))
	for _, c := range parsed {
		cites = append(cites, result.Citation{SessionID: c.SessionID, CallIndex: c.CallIndex, Signature: c.Signature})
	}
	return cites
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
	return admitsForSomeRowWith(admitted, matrix, result.LegacyEvaluatorContract())
}

func admitsForSomeRowWith(admitted *evaluation.AdmittedPack, matrix Matrix, contract result.EvaluatorContract) bool {
	if admitted.AdmitsWith(evaluation.Options{SpecVersion: contract.Version()}) {
		return true
	}
	for _, row := range matrix.Cases {
		if len(row.SupportedExtensions) > 0 && admitted.AdmitsWith(evaluation.Options{SpecVersion: contract.Version(), SupportedExtensions: row.SupportedExtensions}) {
			return true
		}
	}
	return false
}

// TestSnapshot runs exact supplied bytes without file reads, registration or audit writes.
// It shares matrix admission, canonical comparisons, target budgets and coverage with Project.Test.
func TestSnapshot(evaluator *evaluation.Engine, pack, cases []byte, command string) (result.PackTest, error) {
	run := &matrixRun{}
	contract, failure := run.selectPackContract(pack)
	if failure != nil {
		return result.PackTest{}, failure
	}
	found, err := identityFrom(pack)
	if err != nil {
		return result.PackTest{}, err
	}
	matrix, err := DecodeMatrix(cases)
	if err != nil {
		return result.PackTest{}, err
	}
	p := &Project{}
	report, failure := p.testLoadedPack(evaluator, found.ID, result.PackTestEntry{ID: found.ID, PackID: found.ID, PackVersion: found.Version, Status: "passed", Rows: []result.EvaluationCorpusCase{}}, pack, matrix, command, run, contract)
	if failure != nil {
		return result.PackTest{}, failure
	}
	return result.PackTest{OutputVersion: result.OutputVersion, Tool: result.CurrentTool(), Command: command, Status: report.Status, Experimental: true, EvaluatorSpecVersion: run.contract.Version(), ConformanceClaimReference: result.EvaluationClaimReference, Label: result.PackMatrixLabel, Kind: result.ProjectKind, Summary: report.Summary, Packs: []result.PackTestEntry{report}}, nil
}
