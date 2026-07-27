package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
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

func (a *App) renderEvaluation(format string, output result.Evaluation) error {
	if format == "json" {
		return a.writeJSON(output)
	}
	fmt.Fprintln(a.out, "EXPERIMENTAL evaluation (no conformance claim; JPS 0.1.0-draft defines no evaluator conformance)")
	switch output.Disposition.Kind {
	case "outcome":
		fmt.Fprintf(a.out, "disposition: outcome %s\n", display.Sanitize(output.Disposition.OutcomeID))
	default:
		fmt.Fprintf(a.out, "disposition: %s (%s)\n", display.Sanitize(output.Disposition.Kind), display.Sanitize(strings.Join(output.Disposition.Reasons, ", ")))
	}
	if output.Disposition.Handoff.State == "requested" {
		if target := output.Disposition.Handoff.Target; target != nil {
			fmt.Fprintf(a.out, "handoff: requested -> %s %q\n", display.Sanitize(target.Kind), display.Sanitize(target.Name))
		} else {
			fmt.Fprintln(a.out, "handoff: requested (no declared destination)")
		}
	}
	for _, entry := range output.Trace {
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
		fmt.Fprintf(a.out, "trace: %s %s: %s%s\n", display.Sanitize(entry.Stage), display.Sanitize(entry.ID), display.Sanitize(note), display.Sanitize(detail))
	}
	if output.Artifact != nil {
		fmt.Fprintf(a.out, "artifacts: %s · sha256 %s\n", display.Sanitize(output.Artifact.Provenance), output.Artifact.BundleDigest)
	}
	return nil
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
