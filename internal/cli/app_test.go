package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
)

func TestHelpUsesSpecNamespace(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"--help"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "spec") || strings.Contains(stdout, "jps validate") {
		t.Fatalf("unexpected help: %s", stdout)
	}
}

func TestValidateJSONContractAndExitCodes(t *testing.T) {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	validPath := writeFixture(t, valid)
	code, stdout, stderr := runTest(t, []string{"spec", "validate", validPath, "--format", "json"}, "")
	if code != 0 || stderr != "" || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var validResult map[string]any
	if err := json.Unmarshal([]byte(stdout), &validResult); err != nil {
		t.Fatal(err)
	}
	if validResult["command"] != "spec validate" || validResult["status"] != "valid" {
		t.Fatalf("unexpected result: %#v", validResult)
	}

	invalid, err := set.Case("structural/missing-decision.json")
	if err != nil {
		t.Fatal(err)
	}
	invalidPath := writeFixture(t, invalid)
	code, stdout, stderr = runTest(t, []string{"spec", "validate", invalidPath, "--format", "json"}, "")
	if code != 1 || stderr != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var invalidResult map[string]any
	if err := json.Unmarshal([]byte(stdout), &invalidResult); err != nil {
		t.Fatal(err)
	}
	if invalidResult["status"] != "invalid" {
		t.Fatalf("unexpected result: %#v", invalidResult)
	}
}

func TestValidateUnknownVersionIsUnsupported(t *testing.T) {
	document := `{"specVersion":"9.9.9"}`
	code, stdout, stderr := runTest(t, []string{"spec", "validate", "-", "--format", "json"}, document)
	if code != 2 || stderr != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output["status"] != "unsupported" || output["command"] != "spec validate" {
		t.Fatalf("unexpected result: %#v", output)
	}
}

func TestConformanceAndSchemaCommands(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"spec", "test-conformance", "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var suite map[string]any
	if err := json.Unmarshal([]byte(stdout), &suite); err != nil {
		t.Fatal(err)
	}
	if suite["command"] != "spec test-conformance" || suite["status"] != "valid" {
		t.Fatalf("unexpected suite result: %#v", suite)
	}

	code, stdout, stderr = runTest(t, []string{"spec", "schema", artifacts.DraftVersion, "--write", "-"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"$schema"`) {
		t.Fatalf("exit=%d stderr=%q output prefix=%q", code, stderr, first(stdout, 80))
	}
}

func TestJSONOptionErrorIsOneMachineResult(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"spec", "validate", "-", "--format", "json", "--quiet"}, `{}`)
	if code != 3 || stderr != "" || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output["status"] != "error" {
		t.Fatalf("unexpected result: %#v", output)
	}
}

func TestJSONParseAndArityErrorsUseMachineEnvelope(t *testing.T) {
	for name, args := range map[string][]string{
		"missing argument": {"spec", "validate", "--format", "json"},
		"unknown flag":     {"spec", "validate", "--bogus", "--format", "json", "-"},
		"extra argument":   {"spec", "test-conformance", "one", "two", "--format=json"},
	} {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := runTest(t, args, "{}")
			if code != 3 || stderr != "" || strings.Count(stdout, "\n") != 1 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			var output map[string]any
			if err := json.Unmarshal([]byte(stdout), &output); err != nil {
				t.Fatal(err)
			}
			if output["status"] != "error" {
				t.Fatalf("unexpected result: %#v", output)
			}
		})
	}
}

func TestUnsupportedCommandResultsHaveUnsupportedStatus(t *testing.T) {
	for name, args := range map[string][]string{
		"schema":      {"spec", "schema", "9.9.9", "--format", "json"},
		"conformance": {"spec", "test-conformance", "--spec-version", "9.9.9", "--format", "json"},
	} {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := runTest(t, args, "")
			if code != 2 || stderr != "" {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			var output map[string]any
			if err := json.Unmarshal([]byte(stdout), &output); err != nil {
				t.Fatal(err)
			}
			if output["status"] != "unsupported" {
				t.Fatalf("unexpected result: %#v", output)
			}
		})
	}

	code, stdout, stderr := runTest(t, []string{"spec", "schema", "9.9.9"}, "")
	if code != 2 || stderr != "" || !strings.HasPrefix(stdout, "unsupported:") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestConformanceHelpNamesExactDefaultVersion(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"spec", "test-conformance", "--help"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, artifacts.DraftVersion) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestJPSNamespaceDoesNotExist(t *testing.T) {
	code, _, stderr := runTest(t, []string{"jps", "validate", "-"}, `{}`)
	if code != 3 || !strings.Contains(stderr, "unknown command") {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
}

func runTest(t *testing.T, args []string, stdin string) (int, string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(args, strings.NewReader(stdin), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func writeFixture(t *testing.T, data []byte) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "pack.json")
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return target
}

func first(value string, count int) string {
	if len(value) <= count {
		return value
	}
	return value[:count]
}

func TestPrettyFlagIndentsJSONOutput(t *testing.T) {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	validPath := writeFixture(t, valid)
	code, stdout, stderr := runTest(t, []string{"spec", "validate", validPath, "--format", "json", "--pretty"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if strings.Count(stdout, "\n") < 2 {
		t.Fatalf("pretty JSON should span multiple lines: %q", stdout)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output["status"] != "valid" {
		t.Fatalf("unexpected result: %#v", output)
	}
}

func TestInputSizeAndReadErrorsUseDistinctCodes(t *testing.T) {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	validPath := writeFixture(t, valid)

	// A byte limit below the document size reports a dedicated resource code,
	// distinct from a generic unreadable-input failure.
	code, stdout, stderr := runTest(t, []string{"spec", "validate", validPath, "--max-bytes", "10", "--format", "json"}, "")
	if code != 4 || stderr != "" {
		t.Fatalf("oversized: exit=%d stderr=%q", code, stderr)
	}
	if got := diagnosticCode(t, stdout); got != "JPS-RESOURCE-INPUT-BYTE-LIMIT" {
		t.Fatalf("oversized input should report the byte-limit code, got %q", got)
	}

	// A genuinely unreadable input keeps the generic read code.
	missing := filepath.Join(t.TempDir(), "absent.json")
	code, stdout, stderr = runTest(t, []string{"spec", "validate", missing, "--format", "json"}, "")
	if code != 4 || stderr != "" {
		t.Fatalf("missing: exit=%d stderr=%q", code, stderr)
	}
	if got := diagnosticCode(t, stdout); got != "JPS-INPUT-READ" {
		t.Fatalf("missing input should report the generic read code, got %q", got)
	}
}

func diagnosticCode(t *testing.T, jsonOutput string) string {
	t.Helper()
	var output struct {
		Diagnostics []struct {
			Code string `json:"code"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &output); err != nil {
		t.Fatalf("decode: %v (output %q)", err, jsonOutput)
	}
	if len(output.Diagnostics) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %d: %q", len(output.Diagnostics), jsonOutput)
	}
	return output.Diagnostics[0].Code
}
