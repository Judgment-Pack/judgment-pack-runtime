package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func writeJSON(writer io.Writer, value any, pretty bool) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(value)
}

func (a *App) writeJSON(value any) error {
	return writeJSON(a.out, value, a.pretty)
}

func (a *App) operational(command, format string, exitCode int, code, message string) error {
	status := "error"
	if exitCode == result.ExitUnsupported {
		status = "unsupported"
	}
	if format == "json" {
		if err := a.writeJSON(result.NewOperationalResult(command, status, code, message)); err != nil {
			return &handledExit{code: result.ExitIO}
		}
	} else if status == "unsupported" {
		fmt.Fprintln(a.out, "unsupported:", display.Sanitize(message))
	} else {
		fmt.Fprintln(a.errOut, "error:", display.Sanitize(message))
	}
	return &handledExit{code: exitCode}
}

// evaluationFailure reports one refused evaluation. When the refusal is an
// evaluation error of JPS Core §8.4 it names the class and the phase in band —
// as the evaluationError member in JSON, on its own line in human output — with
// this runtime's finer diagnostic code beside them as the detail §8.4 admits. A
// refusal §8.4 does not classify is reported as any other operational error.
// No disposition accompanies either.
func (a *App) evaluationFailure(command, format string, failure *evaluation.Failure) error {
	if failure.Class == "" {
		return a.operational(command, format, failure.ExitCode, failure.Code, failure.Message)
	}
	status := "error"
	if failure.ExitCode == result.ExitUnsupported {
		status = "unsupported"
	}
	if format == "json" {
		if err := a.writeJSON(result.NewEvaluationError(command, status, failure.Class, failure.Phase, failure.Code, failure.Message)); err != nil {
			return &handledExit{code: result.ExitIO}
		}
		return &handledExit{code: failure.ExitCode}
	}
	stream := a.errOut
	if status == "unsupported" {
		stream = a.out
	}
	fmt.Fprintf(stream, "%s: %s\n", status, display.Sanitize(failure.Message))
	fmt.Fprintf(stream, "evaluation error: %s (%s phase; %s)\n", failure.Class, failure.Phase, failure.Code)
	return &handledExit{code: failure.ExitCode}
}

func (a *App) renderValidation(format string, output result.Validation) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	if output.Status == "valid" {
		if output.ValidationScope.FullDocumentConformance {
			fmt.Fprintf(a.out, "valid: JPS document conformance passed (%s)\n", display.Sanitize(output.SpecVersion))
		} else {
			fmt.Fprintf(a.out, "valid through %s (partial validation)\n", display.Sanitize(output.ValidationScope.RequestedThrough))
		}
		if output.Artifact != nil {
			fmt.Fprintf(a.out, "artifacts: %s · sha256 %s\n", display.Sanitize(output.Artifact.Provenance), output.Artifact.BundleDigest)
		}
		return nil
	}
	fmt.Fprintf(a.out, "%s: JPS document conformance was not established\n", output.Status)
	for _, diagnostic := range output.Diagnostics {
		location := diagnostic.InstancePath
		if location == "" {
			location = "<root>"
		}
		fmt.Fprintf(a.out, "%s %s: %s\n", display.Sanitize(diagnostic.Code), display.Sanitize(location), display.Sanitize(diagnostic.Message))
	}
	return nil
}

func (a *App) renderSuite(format string, output result.Suite) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	if output.Status == "valid" {
		fmt.Fprintf(a.out, "passed: %d/%d conformance cases matched expectations\n", output.Summary.Passed, output.Summary.Total)
	} else {
		fmt.Fprintf(a.out, "mismatch: %d/%d conformance cases did not match expectations\n", output.Summary.Mismatched, output.Summary.Total)
		for _, item := range output.Cases {
			if item.Status == "mismatch" {
				fmt.Fprintf(a.out, "- %s: expected %s, got %s\n", display.Sanitize(item.ID), display.Sanitize(item.ExpectedStatus), display.Sanitize(item.ActualStatus))
			}
		}
	}
	fmt.Fprintf(a.out, "JPS %s · suite %s · %s\n", display.Sanitize(output.SpecVersion), display.Sanitize(output.SuiteVersion), display.Sanitize(output.Provenance))
	fmt.Fprintf(a.out, "corpus: %s:%s\n", output.CorpusDigestAlgorithm, output.CorpusDigest)
	if output.DiagnosticsTruncated {
		fmt.Fprintln(a.out, "diagnostics: truncated at the suite output limit")
	}
	return nil
}

func (a *App) renderSchema(format string, output result.Schema) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintf(a.out, "JPS schema %s\n", display.Sanitize(output.SpecVersion))
	fmt.Fprintf(a.out, "id: %s\n", display.Sanitize(output.SchemaID))
	fmt.Fprintf(a.out, "sha256: %s\n", output.SHA256)
	fmt.Fprintf(a.out, "bytes: %d\n", output.Bytes)
	fmt.Fprintf(a.out, "artifacts: %s\n", display.Sanitize(output.Provenance))
	if output.WrittenTo != "" {
		fmt.Fprintf(a.out, "written: %s\n", display.Sanitize(output.WrittenTo))
	}
	return nil
}

func (a *App) renderExamples(format string, output result.Examples) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintf(a.out, "JPS examples %s (%s)\n", display.Sanitize(output.SpecVersion), display.Sanitize(output.Provenance))
	fmt.Fprintln(a.out, "version-pinned conformance fixtures, not authored templates")
	for _, example := range output.Examples {
		fmt.Fprintf(a.out, "- %s: %s [%s]\n", display.Sanitize(example.Name), display.Sanitize(example.Focus), display.Sanitize(example.SpecSection))
	}
	return nil
}

func (a *App) renderExample(format string, output result.Example) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintf(a.out, "JPS example %s\n", display.Sanitize(output.Name))
	fmt.Fprintf(a.out, "focus: %s\n", display.Sanitize(output.Focus))
	fmt.Fprintf(a.out, "spec: %s\n", display.Sanitize(output.SpecSection))
	fmt.Fprintf(a.out, "sha256: %s\n", output.SHA256)
	fmt.Fprintf(a.out, "bytes: %d\n", output.Bytes)
	fmt.Fprintf(a.out, "kind: %s\n", display.Sanitize(output.Kind))
	fmt.Fprintf(a.out, "artifacts: %s\n", display.Sanitize(output.Provenance))
	if output.WrittenTo != "" {
		fmt.Fprintf(a.out, "written: %s\n", display.Sanitize(output.WrittenTo))
	}
	return nil
}

// renderEvaluationCorpus reports one evaluation-corpus run. The label leads the
// human output for the same reason it is carried in the JSON: a reader who sees
// only the pass count must not read the claim into it. Corpus results are the
// claim's required evidence and are not exhaustive evidence of it (§3.4.1), and
// the label says where the claim itself is.
func (a *App) renderEvaluationCorpus(format string, output result.EvaluationCorpus) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintf(a.out, "EXPERIMENTAL SURFACE evaluation corpus: %s\n", output.Label)
	if output.Status == "passed" {
		fmt.Fprintf(a.out, "passed: %d/%d corpus rows matched their expectation\n", output.Summary.Passed, output.Summary.Total)
	} else {
		fmt.Fprintf(a.out, "mismatch: %d/%d corpus rows did not match their expectation\n", output.Summary.Mismatched, output.Summary.Total)
	}
	for _, item := range output.Cases {
		if item.Status != "mismatch" {
			continue
		}
		fmt.Fprintf(a.out, "- %s [%s]: %s\n", display.Sanitize(item.ID), display.Sanitize(item.SpecSection), display.Sanitize(item.Detail))
		if item.Expected != "" || item.Actual != "" {
			fmt.Fprintf(a.out, "  expected: %s\n", display.Sanitize(item.Expected))
			fmt.Fprintf(a.out, "  actual:   %s\n", display.Sanitize(item.Actual))
		}
		if item.ExpectedErrorClass != "" || item.ActualErrorClass != "" {
			fmt.Fprintf(a.out, "  expected class: %s / actual class: %s\n", display.Sanitize(item.ExpectedErrorClass), display.Sanitize(item.ActualErrorClass))
		}
	}
	fmt.Fprintf(a.out, "JPS %s · corpus %s (%s, %s) · %s\n",
		display.Sanitize(output.SpecVersion), display.Sanitize(output.SuiteVersion),
		display.Sanitize(output.CorpusStatus), display.Sanitize(output.CorpusLabel), display.Sanitize(output.Provenance))
	fmt.Fprintln(a.out, "A mismatching row decides nothing by itself: a divergence is as likely to be a defect in the row as in this implementation.")
	return nil
}

// renderPackInventory reports one project's resolved inventory. The two names a
// row carries are printed as two names: the project's decision id leads the
// line, and the pack document's own id and version follow it in parentheses, so
// no reader takes the short id for the pack's identity.
func (a *App) renderPackInventory(format string, output result.PackInventory) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	if output.Status == "none" {
		fmt.Fprintln(a.out, display.Sanitize(output.Note))
		return nil
	}
	fmt.Fprintf(a.out, "%s (configVersion %s) · %s\n", display.Sanitize(output.ConfigPath), display.Sanitize(output.ConfigVersion), display.Sanitize(output.Kind))
	if len(output.Packs) == 0 {
		fmt.Fprintln(a.out, "no packs declared")
		return nil
	}
	for _, pack := range output.Packs {
		// Three different things leave the identity empty and they call for three
		// different fixes, so the label is keyed off the detail rather than off the
		// missing id: a file that could not be obtained carries one, and a document
		// that was read and simply states no id does not. Sending a reader to look
		// for a missing file that is sitting right there is the confusion the
		// three-way read-failure message exists to prevent.
		identity := "no id declared"
		switch {
		case pack.PackID != "":
			identity = display.Sanitize(pack.PackID) + " " + display.Sanitize(pack.PackVersion)
		case pack.Detail != "":
			identity = "document unreadable"
		}
		fmt.Fprintf(a.out, "- %s (%s): %s\n", display.Sanitize(pack.ID), identity, display.Sanitize(pack.Path))
		if pack.Description != "" {
			fmt.Fprintf(a.out, "  %s\n", display.Sanitize(pack.Description))
		}
		fmt.Fprintf(a.out, "  matrix: %s · expectedVersion: %s\n", matrixLabel(pack), expectedVersionLabel(pack))
		if pack.Detail != "" {
			fmt.Fprintf(a.out, "  detail: %s\n", display.Sanitize(pack.Detail))
		}
	}
	return nil
}

func matrixLabel(pack result.PackSummary) string {
	if pack.MatrixPath == "" {
		return "none"
	}
	return display.Sanitize(pack.MatrixPath)
}

func expectedVersionLabel(pack result.PackSummary) string {
	if pack.ExpectedVersion == "" {
		return display.Sanitize(pack.ExpectedVersionStatus)
	}
	return display.Sanitize(pack.ExpectedVersion) + " (" + display.Sanitize(pack.ExpectedVersionStatus) + ")"
}

// renderPackValidation reports every check, passed and skipped ones included. A
// report that printed only failures would let a project believe a check ran when
// the configuration never asked for it.
func (a *App) renderPackValidation(format string, output result.PackValidation) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	switch {
	case output.Status == "valid":
		fmt.Fprintf(a.out, "valid: %d/%d configured packs passed every check\n", output.Summary.Passed, output.Summary.Total)
	case output.Summary.Failed > 0:
		fmt.Fprintf(a.out, "invalid: %d/%d configured packs failed a check\n", output.Summary.Failed, output.Summary.Total)
	default:
		// Every pack passed and something about the configuration itself did
		// not. Reporting "0/1 configured packs failed a check" under an invalid
		// headline would send a reader looking through the packs for a defect
		// that is not in any of them.
		fmt.Fprintln(a.out, "invalid: every configured pack passed, and a check on the configuration itself failed")
	}
	if len(output.Checks) > 0 {
		fmt.Fprintln(a.out, "- the configuration itself:")
		for _, check := range output.Checks {
			line := fmt.Sprintf("  %s: %s", display.Sanitize(check.Name), display.Sanitize(check.Status))
			if check.Detail != "" {
				line += " — " + display.Sanitize(check.Detail)
			}
			fmt.Fprintln(a.out, line)
		}
	}
	for _, pack := range output.Packs {
		fmt.Fprintf(a.out, "- %s [%s]: %s\n", display.Sanitize(pack.ID), display.Sanitize(pack.Status), display.Sanitize(pack.Path))
		for _, check := range pack.Checks {
			line := fmt.Sprintf("  %s: %s", display.Sanitize(check.Name), display.Sanitize(check.Status))
			if check.Detail != "" {
				line += " — " + display.Sanitize(check.Detail)
			}
			fmt.Fprintln(a.out, line)
		}
	}
	fmt.Fprintf(a.out, "%s (configVersion %s) · %s\n", display.Sanitize(output.ConfigPath), display.Sanitize(output.ConfigVersion), display.Sanitize(output.Kind))
	return nil
}

// renderPackProducersLint reports every producer check, skipped ones
// included, on renderPackValidation's own reasoning: a report that printed
// only failures would let a project believe a check ran when it never could.
func (a *App) renderPackProducersLint(format string, output result.PackProducersLint) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	switch output.Status {
	case "passed":
		fmt.Fprintf(a.out, "passed: %d/%d declared packs lint clean against the %s (%d skipped)\n", output.Summary.Passed, output.Summary.Total, display.Sanitize(output.ProducersSource), output.Summary.Skipped)
	case "failed":
		fmt.Fprintf(a.out, "failed: %d/%d declared packs and the %s disagree — the checks below say in which direction\n", output.Summary.Failed, output.Summary.Total, display.Sanitize(output.ProducersSource))
	default:
		fmt.Fprintln(a.out, "skipped: nothing was checkable — a green lint over zero checks would say a project was linted when nothing was")
	}
	if len(output.Checks) > 0 {
		fmt.Fprintln(a.out, "- the manifest against every selected pack:")
		for _, check := range output.Checks {
			line := fmt.Sprintf("  %s: %s", display.Sanitize(check.Name), display.Sanitize(check.Status))
			if check.Detail != "" {
				line += " — " + display.Sanitize(check.Detail)
			}
			fmt.Fprintln(a.out, line)
		}
	}
	for _, pack := range output.Packs {
		fmt.Fprintf(a.out, "- %s [%s]: %s\n", display.Sanitize(pack.ID), display.Sanitize(pack.Status), display.Sanitize(pack.Path))
		for _, check := range pack.Checks {
			line := fmt.Sprintf("  %s: %s", display.Sanitize(check.Name), display.Sanitize(check.Status))
			if check.Detail != "" {
				line += " — " + display.Sanitize(check.Detail)
			}
			fmt.Fprintln(a.out, line)
		}
	}
	fmt.Fprintf(a.out, "%s (configVersion %s) · %s\n", display.Sanitize(output.ConfigPath), display.Sanitize(output.ConfigVersion), display.Sanitize(output.Kind))
	return nil
}

// renderPackLock reports the reviewed set one packs lock run declared. It
// prints every entry with its digest: the file itself is the artifact a
// reviewer reads, and the terminal output is what says which documents just
// entered the set.
func (a *App) renderPackLock(format string, output result.PackLock) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintf(a.out, "locked: %d declared document(s) are now this project's reviewed set\n", output.Summary.Total)
	fmt.Fprintf(a.out, "- configuration: %s\n", display.Sanitize(output.ConfigDigest))
	for _, entry := range output.Entries {
		fmt.Fprintf(a.out, "- %s %s: %s (%s)\n", display.Sanitize(entry.Kind), display.Sanitize(entry.ID),
			display.Sanitize(entry.Digest), display.Sanitize(entry.Path))
	}
	if output.WrittenTo != "" {
		fmt.Fprintf(a.out, "written: %s\n", display.Sanitize(output.WrittenTo))
	}
	fmt.Fprintf(a.out, "%s (lockVersion %s) · %s\n", display.Sanitize(output.ConfigPath), display.Sanitize(output.LockVersion), display.Sanitize(output.Kind))
	return nil
}

// renderPackLockVerification reports every difference from the reviewed set, or
// says plainly that there is none. A verification that printed nothing on
// success would leave a reader unsure whether it ran.
func (a *App) renderPackLockVerification(format string, output result.PackLockVerification) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	if output.Status == "valid" {
		fmt.Fprintf(a.out, "verified: %d/%d declared document(s) match the reviewed set, and so does the configuration\n",
			output.Summary.Passed, output.Summary.Total)
	} else {
		fmt.Fprintf(a.out, "invalid: %d difference(s) from the reviewed set (%d/%d declared document(s) failed",
			len(output.Findings), output.Summary.Failed, output.Summary.Total)
		if output.StaleEntries > 0 {
			fmt.Fprintf(a.out, ", %d locked entry/entries the configuration no longer declares", output.StaleEntries)
		}
		fmt.Fprintln(a.out, ")")
	}
	for _, finding := range output.Findings {
		subject := display.Sanitize(finding.Path)
		if finding.ID != "" {
			subject = fmt.Sprintf("%s (%s)", display.Sanitize(finding.ID), display.Sanitize(finding.Path))
		}
		line := fmt.Sprintf("- %s: %s", display.Sanitize(finding.Name), subject)
		if finding.Detail != "" {
			line += " — " + display.Sanitize(finding.Detail)
		}
		fmt.Fprintln(a.out, line)
	}
	if output.Status != "valid" {
		fmt.Fprintln(a.out, "Run jpack packs lock to declare the amendment, or restore the reviewed bytes.")
	}
	fmt.Fprintf(a.out, "%s (lockVersion %s) · %s\n", display.Sanitize(output.LockPath), display.Sanitize(output.LockVersion), display.Sanitize(output.Kind))
	return nil
}

// renderPackTest reports one project matrix run. The label leads the output for
// the same reason the corpus label does: a reader who sees only a pass count
// must not read anything about this runtime's conformance into it.
func (a *App) renderPackTest(format string, output result.PackTest) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintf(a.out, "EXPERIMENTAL SURFACE project matrix: %s\n", output.Label)
	switch output.Status {
	case "passed":
		fmt.Fprintf(a.out, "passed: %d/%d matrix rows matched their expectation\n", output.Summary.Passed, output.Summary.Total)
	case "skipped":
		// No row ran, which is reported as its own outcome rather than as a pass over
		// zero rows: nothing here was tested, and the exit code says so. The wording
		// covers both ways of running nothing — packs that declare no matrix, and no
		// selected pack at all — because a sentence naming only the first would be
		// false for a project that configures none.
		fmt.Fprintln(a.out, "skipped: no matrix row ran, because no selected pack declares a matrix (or no pack was selected)")
	default:
		fmt.Fprintf(a.out, "mismatch: %d/%d matrix rows did not match their expectation\n", output.Summary.Mismatched, output.Summary.Total)
	}
	for _, pack := range output.Packs {
		fmt.Fprintf(a.out, "- %s [%s]: %d/%d\n", display.Sanitize(pack.ID), display.Sanitize(pack.Status), pack.Summary.Passed, pack.Summary.Total)
		if pack.Detail != "" {
			fmt.Fprintf(a.out, "  detail: %s\n", display.Sanitize(pack.Detail))
		}
		for _, row := range pack.Rows {
			if row.Status != "mismatch" {
				continue
			}
			fmt.Fprintf(a.out, "  %s: %s\n", display.Sanitize(row.ID), display.Sanitize(row.Detail))
			if row.Expected != "" || row.Actual != "" {
				fmt.Fprintf(a.out, "    expected: %s\n", display.Sanitize(row.Expected))
				fmt.Fprintf(a.out, "    actual:   %s\n", display.Sanitize(row.Actual))
			}
			if row.ExpectedErrorClass != "" || row.ActualErrorClass != "" {
				fmt.Fprintf(a.out, "    expected class: %s / actual class: %s\n", display.Sanitize(row.ExpectedErrorClass), display.Sanitize(row.ActualErrorClass))
			}
		}
		a.renderCoverage("  ", pack.Coverage)
	}
	fmt.Fprintf(a.out, "%s (configVersion %s) · %s\n", display.Sanitize(output.ConfigPath), display.Sanitize(output.ConfigVersion), display.Sanitize(output.Kind))
	fmt.Fprintln(a.out, "A mismatching row is a statement about the pack and the row, not about this runtime.")
	return nil
}

// renderCoverage prints one coverage block: the count line always states that
// probes were derived; only missing probes get a detail line, as only
// mismatching rows do. Coverage moves no status and no exit code (ADR-0014;
// ADR-0016 for graph rows) — the exit code is rows-only on every surface that
// calls this.
func (a *App) renderCoverage(indent string, probes []result.MatrixProbe) {
	if len(probes) == 0 {
		return
	}
	covered := 0
	for _, probe := range probes {
		if probe.Status == result.MatrixProbeCovered {
			covered++
		}
	}
	// "witnessed by a row" rather than "have a row expecting them": a boundary
	// probe is witnessed by what a row's facts state, not by what it expects
	// (ADR-0023), and one count line covers both families.
	fmt.Fprintf(a.out, "%scoverage: %d/%d derived probes are witnessed by a row\n", indent, covered, len(probes))
	for _, probe := range probes {
		if probe.Status != result.MatrixProbeMissing {
			continue
		}
		fmt.Fprintf(a.out, "%s  %s: %s\n", indent, display.Sanitize(probe.Probe), display.Sanitize(probe.Detail))
	}
}

func (a *App) renderConfigSchema(format string, output result.ConfigSchema) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintf(a.out, "jpack.json schema (configVersion %s)\n", display.Sanitize(output.ConfigVersion))
	fmt.Fprintf(a.out, "id: %s\n", display.Sanitize(output.SchemaID))
	fmt.Fprintf(a.out, "sha256: %s\n", output.SHA256)
	fmt.Fprintf(a.out, "bytes: %d\n", output.Bytes)
	fmt.Fprintf(a.out, "kind: %s\n", display.Sanitize(output.Kind))
	if output.WrittenTo != "" {
		fmt.Fprintf(a.out, "written: %s\n", display.Sanitize(output.WrittenTo))
	}
	return nil
}

func (a *App) renderEvaluation(format string, output result.Evaluation) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintln(a.out, "EXPERIMENTAL SURFACE evaluation (claim and scope: CONFORMANCE.md; this result authorizes nothing)")
	if prototype := output.DraftPrototype; prototype != nil {
		// The two wordings mirror the JSON marker's own: a pack that used no
		// draft operator is a plain pack the published validator accepts, and
		// saying otherwise would contradict the same run's JSON output.
		if prototype.PackValidUnderSpecVersion {
			fmt.Fprintf(a.out, "DRAFT-RFC PROTOTYPE: RFC %s grammar enabled; this pack uses no draft operator and remains a plain JPS %s pack\n",
				display.Sanitize(prototype.RFC), display.Sanitize(output.SpecVersion))
		} else {
			fmt.Fprintf(a.out, "DRAFT-RFC PROTOTYPE: RFC %s operators %s; this pack is NOT valid under JPS %s and spec validate rejects it\n",
				display.Sanitize(prototype.RFC), display.Sanitize(operatorList(prototype.Operators)), display.Sanitize(output.SpecVersion))
		}
	}
	switch output.Disposition.Kind {
	case "outcome":
		fmt.Fprintf(a.out, "disposition: outcome %s\n", display.Sanitize(output.Disposition.OutcomeID))
	default:
		fmt.Fprintf(a.out, "disposition: %s (%s)\n", display.Sanitize(output.Disposition.Kind), display.Sanitize(strings.Join(output.Disposition.Reasons, ", ")))
	}
	if output.Disposition.Handoff.State == "requested" {
		triggers := display.Sanitize(strings.Join(output.Disposition.Handoff.TriggeredBy, ", "))
		if target := output.HandoffTarget; target != nil {
			fmt.Fprintf(a.out, "handoff: requested -> %s %q (triggered by %s)\n", display.Sanitize(target.Kind), display.Sanitize(target.Name), triggers)
		} else {
			fmt.Fprintf(a.out, "handoff: requested (no declared destination; triggered by %s)\n", triggers)
		}
	}
	for _, entry := range output.Trace {
		fmt.Fprintln(a.out, traceLine(entry))
	}
	if output.Artifact != nil {
		fmt.Fprintf(a.out, "artifacts: %s · sha256 %s\n", display.Sanitize(output.Artifact.Provenance), output.Artifact.BundleDigest)
	}
	return nil
}

// renderGraphValidation reports one graph document's checks. Every diagnostic
// is printed, on the spec validate precedent: a reader fixing a graph wants
// the whole list, not a conversation.
func (a *App) renderGraphValidation(format string, output result.GraphValidation) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	if output.Status == "valid" {
		fmt.Fprintf(a.out, "valid: graph %s %s and its references check out\n", display.Sanitize(output.GraphID), display.Sanitize(output.GraphVersion))
	} else {
		fmt.Fprintf(a.out, "invalid: %d problem(s) in graph %s\n", len(output.Diagnostics), display.Sanitize(output.GraphPath))
		for _, diagnostic := range output.Diagnostics {
			location := diagnostic.InstancePath
			if location == "" {
				location = "<root>"
			}
			fmt.Fprintf(a.out, "%s %s: %s\n", display.Sanitize(diagnostic.Code), display.Sanitize(location), display.Sanitize(diagnostic.Message))
		}
	}
	fmt.Fprintf(a.out, "%s · %s · %s\n", display.Sanitize(output.GraphPath), display.Sanitize(output.ConfigPath), display.Sanitize(output.Kind))
	return nil
}

// renderGraphEvaluation reports one graph run. The label leads for the same
// reason every experimental label does, the composite headline echoes the
// result node, and every requested handoff is printed before the per-node
// detail so an escalation upstream of the result cannot be scrolled past.
func (a *App) renderGraphEvaluation(format string, output result.GraphEvaluation) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintf(a.out, "EXPERIMENTAL SURFACE graph evaluation: %s\n", output.Label)
	fmt.Fprintf(a.out, "graph: %s %s · result node: %s\n", display.Sanitize(output.GraphID), display.Sanitize(output.GraphVersion), display.Sanitize(output.ResultNode))
	a.printDisposition("disposition", output.Disposition, output.HandoffTarget)
	for _, handoff := range output.Handoffs {
		target := "no declared destination"
		if handoff.Target != nil {
			target = fmt.Sprintf("%s %q", display.Sanitize(handoff.Target.Kind), display.Sanitize(handoff.Target.Name))
		}
		fmt.Fprintf(a.out, "handoff[%s]: requested -> %s (triggered by %s)\n", display.Sanitize(handoff.Node), target, display.Sanitize(strings.Join(handoff.TriggeredBy, ", ")))
	}
	for _, node := range output.Nodes {
		fmt.Fprintf(a.out, "- node %s (pack %s = %s %s): %s\n",
			display.Sanitize(node.Node), display.Sanitize(node.Pack),
			display.Sanitize(node.PackID), display.Sanitize(node.PackVersion),
			dispositionSummary(node.Disposition))
		for _, feed := range node.FactFeeds {
			note := "not injected (the upstream disposition is not an outcome)"
			if feed.Injected {
				note = "injected " + display.Sanitize(feed.Value)
			}
			fmt.Fprintf(a.out, "  fact %s <- %s: %s\n", display.Sanitize(feed.Pointer), display.Sanitize(feed.From), note)
		}
		for _, feed := range node.EvidenceFeeds {
			fmt.Fprintf(a.out, "  evidence %s <- %s: %s\n", display.Sanitize(feed.Requirement), display.Sanitize(feed.From), display.Sanitize(feed.State))
		}
		for _, entry := range node.Trace {
			fmt.Fprintf(a.out, "  %s\n", traceLine(entry))
		}
	}
	if output.Artifact != nil {
		fmt.Fprintf(a.out, "artifacts: %s · sha256 %s\n", display.Sanitize(output.Artifact.Provenance), output.Artifact.BundleDigest)
	}
	return nil
}

// renderGraphPlan reports one graph's evaluation plan. The closing line says
// what a plan is not, because a printed order can read like a run to someone
// scrolling past.
func (a *App) renderGraphPlan(format string, output result.GraphPlan) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintf(a.out, "graph plan: %s %s · result node: %s\n", display.Sanitize(output.GraphID), display.Sanitize(output.GraphVersion), display.Sanitize(output.ResultNode))
	for _, step := range output.Steps {
		// Three different things leave the identity empty, exactly as in the
		// project inventory: a document that could not be obtained carries a
		// detail, and one that was read and simply declares no id does not.
		identity := "no id declared"
		switch {
		case step.PackID != "":
			identity = display.Sanitize(step.PackID) + " " + display.Sanitize(step.PackVersion)
		case step.Detail != "":
			identity = "document unreadable"
		}
		fmt.Fprintf(a.out, "%d. %s (pack %s = %s): %s\n", step.Order, display.Sanitize(step.Node), display.Sanitize(step.Pack), identity, display.Sanitize(step.Path))
		if step.Detail != "" {
			fmt.Fprintf(a.out, "   detail: %s\n", display.Sanitize(step.Detail))
		}
		for _, feed := range step.Feeds {
			if feed.Fact != "" {
				fmt.Fprintf(a.out, "   fact %s <- %s\n", display.Sanitize(feed.Fact), display.Sanitize(feed.From))
			}
			if feed.Evidence != "" {
				fmt.Fprintf(a.out, "   evidence %s <- %s (onUnresolved: %s)\n", display.Sanitize(feed.Evidence), display.Sanitize(feed.From), display.Sanitize(feed.OnUnresolved))
			}
		}
	}
	fmt.Fprintf(a.out, "%s · %s · %s\n", display.Sanitize(output.GraphPath), display.Sanitize(output.ConfigPath), display.Sanitize(output.Kind))
	fmt.Fprintln(a.out, "nothing was evaluated: no condition was interpreted and no disposition exists")
	return nil
}

func (a *App) renderGraphSchema(format string, output result.GraphSchema) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintf(a.out, "graph schema (formatVersion %s)\n", display.Sanitize(output.FormatVersion))
	fmt.Fprintf(a.out, "id: %s\n", display.Sanitize(output.SchemaID))
	fmt.Fprintf(a.out, "sha256: %s\n", output.SHA256)
	fmt.Fprintf(a.out, "bytes: %d\n", output.Bytes)
	fmt.Fprintf(a.out, "kind: %s\n", display.Sanitize(output.Kind))
	if output.WrittenTo != "" {
		fmt.Fprintf(a.out, "written: %s\n", display.Sanitize(output.WrittenTo))
	}
	return nil
}

// renderGraphTest reports one graph matrix run. The label leads for the same
// reason every matrix label does: a reader who sees only a pass count must
// not read anything about conformance into it.
func (a *App) renderGraphTest(format string, output result.GraphTest) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintf(a.out, "EXPERIMENTAL SURFACE graph matrix: %s\n", output.Label)
	if output.Status == "passed" {
		fmt.Fprintf(a.out, "passed: %d/%d graph rows matched their expectation\n", output.Summary.Passed, output.Summary.Total)
	} else {
		fmt.Fprintf(a.out, "mismatch: %d/%d graph rows did not match their expectation\n", output.Summary.Mismatched, output.Summary.Total)
	}
	for _, row := range output.Rows {
		if row.Status != "mismatch" {
			continue
		}
		fmt.Fprintf(a.out, "- %s: %s\n", display.Sanitize(row.ID), display.Sanitize(row.Detail))
		if row.Expected != "" || row.Actual != "" {
			fmt.Fprintf(a.out, "  expected: %s\n", display.Sanitize(row.Expected))
			fmt.Fprintf(a.out, "  actual:   %s\n", display.Sanitize(row.Actual))
		}
		if row.ExpectedErrorClass != "" || row.ActualErrorClass != "" {
			fmt.Fprintf(a.out, "  expected class: %s / actual class: %s\n", display.Sanitize(row.ExpectedErrorClass), display.Sanitize(row.ActualErrorClass))
		}
		for _, node := range row.Nodes {
			if node.Status != "mismatch" {
				continue
			}
			fmt.Fprintf(a.out, "  node %s expected: %s\n", display.Sanitize(node.Node), display.Sanitize(node.Expected))
			fmt.Fprintf(a.out, "  node %s actual:   %s\n", display.Sanitize(node.Node), display.Sanitize(node.Actual))
		}
	}
	a.renderCoverage("", output.Coverage)
	fmt.Fprintf(a.out, "graph: %s %s · %s · %s\n", display.Sanitize(output.GraphID), display.Sanitize(output.GraphVersion), display.Sanitize(output.RowsPath), display.Sanitize(output.Kind))
	fmt.Fprintln(a.out, "A mismatching row is a statement about the graph and the row, not about this runtime.")
	return nil
}

// renderGraphSuite reports the project graph-matrix walk, on renderPackTest's
// shape: the label leads, entries carry their own counts and details, and only
// mismatching rows and missing probes get lines.
func (a *App) renderGraphSuite(format string, output result.GraphSuite) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintf(a.out, "EXPERIMENTAL SURFACE graph matrix: %s\n", output.Label)
	switch output.Status {
	case "passed":
		fmt.Fprintf(a.out, "passed: %d/%d graph rows matched their expectation\n", output.Summary.Passed, output.Summary.Total)
	case "skipped":
		// No row ran, and the exit code says so: a green gate over nothing
		// tested is not a pass (ADR-0017). The wording covers both ways of
		// running nothing — no graph declares rows, and no graph declared at
		// all.
		fmt.Fprintln(a.out, "skipped: no graph row ran, because no selected graph declares rows (or no graph is configured)")
	default:
		fmt.Fprintf(a.out, "mismatch: %d/%d graph rows did not match their expectation\n", output.Summary.Mismatched, output.Summary.Total)
	}
	for _, entry := range output.Graphs {
		fmt.Fprintf(a.out, "- %s [%s]: %d/%d\n", display.Sanitize(entry.ID), display.Sanitize(entry.Status), entry.Summary.Passed, entry.Summary.Total)
		if entry.Detail != "" {
			fmt.Fprintf(a.out, "  detail: %s\n", display.Sanitize(entry.Detail))
		}
		for _, row := range entry.Rows {
			if row.Status != "mismatch" {
				continue
			}
			fmt.Fprintf(a.out, "  %s: %s\n", display.Sanitize(row.ID), display.Sanitize(row.Detail))
			if row.Expected != "" || row.Actual != "" {
				fmt.Fprintf(a.out, "    expected: %s\n", display.Sanitize(row.Expected))
				fmt.Fprintf(a.out, "    actual:   %s\n", display.Sanitize(row.Actual))
			}
			if row.ExpectedErrorClass != "" || row.ActualErrorClass != "" {
				fmt.Fprintf(a.out, "    expected class: %s / actual class: %s\n", display.Sanitize(row.ExpectedErrorClass), display.Sanitize(row.ActualErrorClass))
			}
		}
		a.renderCoverage("  ", entry.Coverage)
	}
	fmt.Fprintf(a.out, "%s (configVersion %s) · %s\n", display.Sanitize(output.ConfigPath), display.Sanitize(output.ConfigVersion), display.Sanitize(output.Kind))
	fmt.Fprintln(a.out, "A mismatching row is a statement about the graph and the row, not about this runtime.")
	return nil
}

// renderGraphValidationSuite reports the project graph-validation walk, on
// renderPackValidation's shape.
func (a *App) renderGraphValidationSuite(format string, output result.GraphValidationSuite) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	switch output.Status {
	case "valid":
		fmt.Fprintf(a.out, "valid: %d/%d configured graphs passed every check\n", output.Summary.Passed, output.Summary.Total)
	case "skipped":
		fmt.Fprintln(a.out, "skipped: the configuration declares no graphs, so nothing was checked")
	default:
		fmt.Fprintf(a.out, "invalid: %d/%d configured graphs failed a check\n", output.Summary.Failed, output.Summary.Total)
	}
	for _, entry := range output.Graphs {
		fmt.Fprintf(a.out, "- %s [%s]: %s\n", display.Sanitize(entry.ID), display.Sanitize(entry.Status), display.Sanitize(entry.Path))
		for _, diagnostic := range entry.Diagnostics {
			location := diagnostic.InstancePath
			if location == "" {
				location = "<root>"
			}
			fmt.Fprintf(a.out, "  %s at %s: %s\n", display.Sanitize(diagnostic.Code), display.Sanitize(location), display.Sanitize(diagnostic.Message))
		}
	}
	fmt.Fprintf(a.out, "%s (configVersion %s) · %s\n", display.Sanitize(output.ConfigPath), display.Sanitize(output.ConfigVersion), display.Sanitize(output.Kind))
	return nil
}

// printDisposition prints one disposition and its handoff, exactly as the
// standalone evaluation renderer formats them.
func (a *App) printDisposition(label string, disposition result.Disposition, target *result.HandoffTarget) {
	fmt.Fprintf(a.out, "%s: %s\n", label, dispositionSummary(disposition))
	if disposition.Handoff.State == "requested" {
		triggers := display.Sanitize(strings.Join(disposition.Handoff.TriggeredBy, ", "))
		if target != nil {
			fmt.Fprintf(a.out, "handoff: requested -> %s %q (triggered by %s)\n", display.Sanitize(target.Kind), display.Sanitize(target.Name), triggers)
		} else {
			fmt.Fprintf(a.out, "handoff: requested (no declared destination; triggered by %s)\n", triggers)
		}
	}
}

// dispositionSummary is one disposition on one line: the outcome id for an
// outcome, and the kind with its retained reasons for anything else.
func dispositionSummary(disposition result.Disposition) string {
	if disposition.Kind == "outcome" {
		return "outcome " + display.Sanitize(disposition.OutcomeID)
	}
	return fmt.Sprintf("%s (%s)", display.Sanitize(disposition.Kind), display.Sanitize(strings.Join(disposition.Reasons, ", ")))
}

// traceLine formats one trace entry. Both surfaces that print a trace -- the
// standalone evaluation renderer and the per-node graph renderer, which indents
// what it returns -- render through this one function, so one trace reads the
// same way wherever it is shown.
func traceLine(entry result.TraceEntry) string {
	note := entry.Condition
	if entry.Suppressed {
		note = "suppressed"
	}
	if entry.Skipped {
		note = "skipped (forced outcome)"
	}
	detail := ""
	if entry.Effect != "" {
		detail = " effect=" + entry.Effect
	}
	if entry.Outcome != "" {
		detail += " outcome=" + entry.Outcome
	}
	if entry.OnUnknown != "" {
		detail += " onUnknown=" + entry.OnUnknown
	}
	// An applicability entry is unnamed, and prints as its stage alone rather
	// than as a stage trailed by the gap where an authored id would have been.
	stage := display.Sanitize(entry.Stage)
	if entry.ID != "" {
		stage += " " + display.Sanitize(entry.ID)
	}
	return fmt.Sprintf("trace: %s: %s%s", stage, display.Sanitize(note), display.Sanitize(detail))
}

// operatorList names the draft operators a pack actually used. The empty case
// never reaches the marker line — a pack that used none gets the other wording
// above — but it is spelled out rather than left to render as an empty list.
func operatorList(operators []string) string {
	if len(operators) == 0 {
		return "(none used)"
	}
	return strings.Join(operators, ", ")
}

func joinNonEmpty(values ...string) string {
	output := []string{}
	for _, value := range values {
		if value != "" {
			output = append(output, value)
		}
	}
	return strings.Join(output, " ")
}
