package graph

import (
	"errors"
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// This file is the project walk (ADR-0017): the configured graphs of one
// jpack.json, each run through exactly the single-graph surfaces above it.
// Nothing here evaluates or validates differently from the path-named forms —
// TestProject calls Test per entry and ValidateProject applies the same
// document checks — so declaring a graph in the configuration changes where
// its suite is found, never what it means.

// projectFailure converts one project refusal into the evaluation-shaped
// failure every graph surface reports, unchanged.
func projectFailure(failure *project.Failure) *evaluation.Failure {
	return &evaluation.Failure{Code: failure.Code, Message: failure.Message, ExitCode: failure.ExitCode}
}

// TestProject runs every configured graph's matrix — or the one graph id
// selects — each entry exactly one Test of that graph against its declared
// rows. The walk inherits the packs test posture row for row: an entry whose
// graph or rows cannot be loaded is a mismatch, not a silent skip; an entry
// declaring no rows is skipped and says so; and a run in which no row ran at
// all is "skipped" rather than "passed", which the CLI exits non-zero on — a
// green gate over nothing tested is the failure mode this posture exists to
// prevent, and for the graph walk that exit is this record's own
// determination rather than something the packs walk decided (ADR-0017).
func TestProject(loaded *project.Project, engine *evaluation.Engine, id string, options Options) (result.GraphSuite, *evaluation.Failure) {
	selected, selectionFailure := loaded.GraphSelection(id)
	if selectionFailure != nil {
		return result.GraphSuite{}, projectFailure(selectionFailure)
	}
	output := result.GraphSuite{
		OutputVersion:             result.OutputVersion,
		Tool:                      result.CurrentTool(),
		Command:                   options.Command,
		Status:                    "passed",
		Experimental:              true,
		ConformanceClaimReference: result.EvaluationClaimReference,
		Label:                     result.GraphMatrixLabel,
		Kind:                      result.ProjectKind,
		FormatVersion:             FormatVersion,
		EvaluatorSpecVersion:      result.EvaluatorSpecVersion,
		ConfigPath:                loaded.ConfigPath,
		ConfigVersion:             loaded.Config.ConfigVersion,
		Graphs:                    make([]result.GraphSuiteEntry, 0, len(selected)),
	}
	spent := 0
	options.reportSpent = &spent
	for _, graphID := range selected {
		entry, _ := loaded.GraphEntry(graphID)
		report, budgetFailure := testEntry(loaded, engine, graphID, entry, options)
		// Only a budget refusal escapes an entry: everything else that stops a
		// graph is that graph's own in-band mismatch.
		if budgetFailure != nil {
			return result.GraphSuite{}, budgetFailure
		}
		output.Summary.Total += report.Summary.Total
		output.Summary.Passed += report.Summary.Passed
		output.Summary.Mismatched += report.Summary.Mismatched
		if report.Status == "mismatch" {
			output.Status = "mismatch"
		}
		output.Graphs = append(output.Graphs, report)
	}
	if output.Status == "passed" && output.Summary.Total == 0 {
		output.Status = "skipped"
	}
	return output, nil
}

// testEntry runs one configured graph's matrix. Anything that stops the run
// before a row can be judged — an unreadable or invalid graph document, an
// unreadable or malformed rows document — is a mismatch for that entry and
// not a silent skip, exactly as a pack whose matrix will not load has not
// passed.
func testEntry(loaded *project.Project, engine *evaluation.Engine, id string, entry project.Graph, options Options) (result.GraphSuiteEntry, *evaluation.Failure) {
	report := result.GraphSuiteEntry{ID: id, Path: entry.Path, RowsPath: entry.Rows, Status: "passed"}
	mismatch := func(detail string) (result.GraphSuiteEntry, *evaluation.Failure) {
		report.Status = "mismatch"
		report.Detail = detail
		return report, nil
	}
	if entry.Rows == "" {
		report.Status = "skipped"
		report.Detail = "The entry declares no rows, so no row ran for this graph."
		return report, nil
	}
	document, detail := loadEntryDocument(loaded, entry)
	if detail != "" {
		return mismatch(detail)
	}
	report.GraphID, report.GraphVersion = document.ID, document.Version
	rowsBytes, err := loaded.ReadGraphRows(entry, MaxRowsBytes)
	if err != nil {
		if errors.Is(err, fssecure.ErrTooLarge) {
			return mismatch(fmt.Sprintf("The graph matrix %s exceeds the %d-byte limit.", display.Sanitize(entry.Rows), MaxRowsBytes))
		}
		return mismatch(project.ReadFailureMessage(entry.Rows, err))
	}
	rows, loadFailure := LoadRows(rowsBytes, entry.Rows)
	if loadFailure != nil {
		return mismatch(display.Sanitize(loadFailure.Message))
	}
	tested, testFailure := Test(loaded, engine, document, entry.Path, entry.Rows, rows, options)
	if testFailure != nil {
		if testFailure.Code == CodeReportBudget {
			return report, testFailure
		}
		return mismatch(display.Sanitize(testFailure.Message))
	}
	report.Status = tested.Status
	report.Summary = tested.Summary
	report.Rows = tested.Rows
	report.Coverage = tested.Coverage
	return report, nil
}

// ValidateProject checks every configured graph — or the one graph id selects
// — with the same document, rule, and reference checks the path-named
// validate applies, plus the one check only the configuration can ask for:
// that a declared rows path stays inside the project. Zero declared graphs is
// "skipped": nothing was checked, and unlike the test walk this is not a
// failure — a project without graphs has nothing here to be wrong about.
func ValidateProject(loaded *project.Project, id, command string) (result.GraphValidationSuite, *evaluation.Failure) {
	selected, selectionFailure := loaded.GraphSelection(id)
	if selectionFailure != nil {
		return result.GraphValidationSuite{}, projectFailure(selectionFailure)
	}
	output := result.GraphValidationSuite{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        "valid",
		Kind:          result.ProjectKind,
		FormatVersion: FormatVersion,
		ConfigPath:    loaded.ConfigPath,
		ConfigVersion: loaded.Config.ConfigVersion,
		Graphs:        make([]result.GraphValidationEntry, 0, len(selected)),
	}
	for _, graphID := range selected {
		entry, _ := loaded.GraphEntry(graphID)
		report := validateEntry(loaded, graphID, entry)
		output.Summary.Total++
		if report.Status == "valid" {
			output.Summary.Passed++
		} else {
			output.Summary.Failed++
			output.Status = "invalid"
		}
		output.Graphs = append(output.Graphs, report)
	}
	if output.Status == "valid" && output.Summary.Total == 0 {
		output.Status = "skipped"
	}
	return output, nil
}

func validateEntry(loaded *project.Project, id string, entry project.Graph) result.GraphValidationEntry {
	report := result.GraphValidationEntry{ID: id, Path: entry.Path, RowsPath: entry.Rows, Status: "valid", Diagnostics: []result.Diagnostic{}}
	// The rows containment check is independent of the document: an escaped
	// rows path is its own defect, and a graph that will not load must not
	// hide it. Containment only — a rows file that is merely missing is
	// inside the project and not this check's finding; the test walk reports
	// it on the entry when the rows are actually read.
	if entry.Rows != "" {
		if err := loaded.Contains(entry.Rows); errors.Is(err, fssecure.ErrOutsideRoot) {
			report.Diagnostics = append(report.Diagnostics, result.ErrorDiagnostic("JPS-GRAPH-ENTRY-ROWS-PATH", "graph", "",
				fmt.Sprintf("The declared rows path %s must resolve inside the project.", display.Sanitize(entry.Rows))))
		}
	}
	document, detail := loadEntryDocument(loaded, entry)
	if detail != "" {
		report.Status = "invalid"
		report.Diagnostics = append(report.Diagnostics, result.ErrorDiagnostic("JPS-GRAPH-ENTRY-READ", "graph", "", detail))
		return report
	}
	report.GraphID, report.GraphVersion = document.ID, document.Version
	report.Diagnostics = append(report.Diagnostics, document.Semantic(loaded)...)
	if len(report.Diagnostics) > 0 {
		report.Status = "invalid"
	}
	return report
}

// loadEntryDocument reads and loads one configured graph document, returning
// a one-line detail instead of a failure so both walks report it on the entry
// rather than refusing the whole walk — one broken graph must not hide the
// others' results.
func loadEntryDocument(loaded *project.Project, entry project.Graph) (Document, string) {
	data, err := loaded.ReadGraph(entry, MaxGraphBytes)
	if err != nil {
		if errors.Is(err, fssecure.ErrTooLarge) {
			return Document{}, fmt.Sprintf("The graph document %s exceeds the %d-byte limit.", display.Sanitize(entry.Path), MaxGraphBytes)
		}
		return Document{}, project.ReadFailureMessage(entry.Path, err)
	}
	document, loadFailure := Load(data, entry.Path)
	if loadFailure != nil {
		return Document{}, display.Sanitize(loadFailure.Message)
	}
	return document, ""
}

// CodeReportBudget marks the one failure an entry may raise that is NOT that
// entry's in-band mismatch: the whole run is refused.
const CodeReportBudget = "JPS-GRAPH-REPORT-BUDGET"

// reportBudgetFailure refuses a run whose report passed the caller's budget. It
// names the graph, how many rows had been judged when it tripped, and both
// numbers, because the caller has to decide whether to select one graph or move
// to the streaming surface.
func reportBudgetFailure(graphID string, rowsJudged int, spent, budget int) *evaluation.Failure {
	return &evaluation.Failure{
		Code: CodeReportBudget,
		Message: fmt.Sprintf(
			"The report reached %d bytes after %d row(s) of graph %q, over this surface's %d-byte budget; the remaining rows were not evaluated, and a truncated suite report would under-report silently.",
			spent, rowsJudged, graphID, budget),
	}
}

// rowEncodingFailure refuses a run whose own row report will not encode.
func rowEncodingFailure(rowID string) *evaluation.Failure {
	return &evaluation.Failure{
		Code:    "JPS-GRAPH-REPORT-ENCODE",
		Message: fmt.Sprintf("The report for row %q could not be encoded.", rowID),
	}
}
