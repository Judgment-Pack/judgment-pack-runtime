package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func (a *App) operational(command, format string, exitCode int, code, message string) error {
	status := "error"
	if exitCode == result.ExitUnsupported {
		status = "unsupported"
	}
	if format == "json" {
		if err := writeJSON(a.out, result.NewOperationalResult(command, status, code, message)); err != nil {
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
		return writeJSON(a.out, output)
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
		return writeJSON(a.out, output)
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
		return writeJSON(a.out, output)
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

func joinNonEmpty(values ...string) string {
	output := []string{}
	for _, value := range values {
		if value != "" {
			output = append(output, value)
		}
	}
	return strings.Join(output, " ")
}
