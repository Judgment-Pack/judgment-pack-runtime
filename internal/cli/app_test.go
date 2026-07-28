package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/describe"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
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

// spec examples lists the embedded valid fixtures and prints one by name; the
// bytes it writes must round-trip through validate, since they are the exact
// conformant fixture. An unknown name is an unsupported result, mirroring
// spec schema for a version this CLI does not bundle.
func TestExamplesCommandListsPrintsAndValidates(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"spec", "examples", "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var catalog map[string]any
	if err := json.Unmarshal([]byte(stdout), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog["kind"] != "version-pinned-conformance-fixture" {
		t.Fatalf("catalog must label its kind as a fixture: %#v", catalog["kind"])
	}
	if len(catalog["examples"].([]any)) == 0 {
		t.Fatal("the example catalog must not be empty")
	}

	// Print one example to stdout and pipe it back through validate.
	code, document, stderr := runTest(t, []string{"spec", "examples", "minimal-literal", "--write", "-"}, "")
	if code != 0 || stderr != "" || !strings.Contains(document, `"specVersion"`) {
		t.Fatalf("exit=%d stderr=%q output prefix=%q", code, stderr, first(document, 80))
	}
	code, _, stderr = runTest(t, []string{"spec", "validate", "-", "--quiet"}, document)
	if code != 0 || stderr != "" {
		t.Fatalf("a printed example must be valid: exit=%d stderr=%q", code, stderr)
	}

	// An unknown name is unsupported (exit 2), not an internal error.
	code, stdout, stderr = runTest(t, []string{"spec", "examples", "no-such-example", "--format", "json"}, "")
	if code != 2 || stderr != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output["status"] != "unsupported" {
		t.Fatalf("unknown example status = %v, want unsupported", output["status"])
	}

	// No drift: the CLI single-example JSON must be the exact payload the shared
	// describe seam produces, so the CLI cannot diverge from the MCP surface
	// (which is pinned to the same seam in the mcp package's tests).
	code, single, stderr := runTest(t, []string{"spec", "examples", "minimal-literal", "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := describe.Example(set, "minimal-literal", "spec examples")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(single), &got); err != nil {
		t.Fatal(err)
	}
	if want := jsonRoundTrip(t, meta); !reflect.DeepEqual(got, want) {
		t.Fatalf("CLI JSON drifted from describe.Example:\n got=%v\nwant=%v", got, want)
	}
}

// spec examples --write to a file must write the exact fixture bytes and refuse
// to overwrite an existing file (O_EXCL), and the two write-specific invocation
// guards must reject their misuse — none of which the stdout path exercises.
func TestExamplesWriteToFileAndInvocationGuards(t *testing.T) {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	want, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "seed.json")

	// Write to a new file: exit 0, bytes identical to the embedded fixture.
	code, stdout, stderr := runTest(t, []string{"spec", "examples", "minimal-literal", "--write", target}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "written:") {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, want) {
		t.Fatal("written bytes differ from the embedded fixture")
	}

	// Writing again to the same path is refused, and the existing file is intact.
	code, _, _ = runTest(t, []string{"spec", "examples", "minimal-literal", "--write", target}, "")
	if code != result.ExitIO {
		t.Fatalf("re-write to an existing path must fail with the IO exit class, got exit=%d", code)
	}
	if after, err := os.ReadFile(target); err != nil || !bytes.Equal(after, want) {
		t.Fatal("a refused overwrite must leave the existing file unchanged")
	}

	// --write with no example name is an invocation error.
	code, stdout, _ = runTest(t, []string{"spec", "examples", "--write", filepath.Join(dir, "x.json"), "--format", "json"}, "")
	if code != result.ExitInvocation {
		t.Fatalf("--write without a name must be an invocation error, got exit=%d", code)
	}
	assertDiagnosticCode(t, stdout, "JPS-INVOCATION-OUTPUT")

	// --write - cannot be combined with --format json.
	code, stdout, _ = runTest(t, []string{"spec", "examples", "minimal-literal", "--write", "-", "--format", "json"}, "")
	if code != result.ExitInvocation {
		t.Fatalf("--write - with --format json must be an invocation error, got exit=%d", code)
	}
	assertDiagnosticCode(t, stdout, "JPS-INVOCATION-STDOUT")
}

func assertDiagnosticCode(t *testing.T, jsonOutput, wantCode string) {
	t.Helper()
	var env struct {
		Diagnostics []struct {
			Code string `json:"code"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &env); err != nil {
		t.Fatalf("output is not a JSON envelope: %v (%q)", err, jsonOutput)
	}
	if len(env.Diagnostics) == 0 || env.Diagnostics[0].Code != wantCode {
		t.Fatalf("diagnostic code = %+v, want %q", env.Diagnostics, wantCode)
	}
}

// jsonRoundTrip marshals a value and decodes it back into a generic map, so a
// typed payload and a decoded JSON payload can be compared with reflect.DeepEqual.
func jsonRoundTrip(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
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

func TestMCPSubcommandRespondsToInitialize(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n"
	code, stdout, stderr := runTest(t, []string{"mcp"}, input)
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &response); err != nil {
		t.Fatalf("undecodable response %q: %v", stdout, err)
	}
	result, ok := response["result"].(map[string]any)
	if !ok || result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("unexpected initialize response: %#v", response)
	}
}

// The experimental evaluate command produces a labeled disposition and exits 0
// for any produced disposition; its invocation guards use the standard codes.
func TestExperimentalEvaluateCommand(t *testing.T) {
	pack := filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json")
	dir := t.TempDir()
	facts := filepath.Join(dir, "facts.json")
	if err := os.WriteFile(facts, []byte(`{"request":{"type":"data-access","completeness":"complete","appropriateness":"hard-fail","embargoedInformationToUnauthorizedRecipients":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence := filepath.Join(dir, "evidence.json")
	if err := os.WriteFile(evidence, []byte(`{"intake-form":"present","sponsor-endorsement":"present"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--evidence", evidence, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output["experimental"] != true || output["conformanceClaim"] != "none" {
		t.Fatalf("evaluation output must be labeled experimental with no claim: %v", output)
	}
	disposition := output["disposition"].(map[string]any)
	if disposition["kind"] != "outcome" || disposition["outcomeId"] != "decline-redirect" {
		t.Fatalf("disposition = %v", disposition)
	}

	// --facts is required.
	code, stdout, _ = runTest(t, []string{"experimental", "evaluate", pack, "--format", "json"}, "")
	if code != result.ExitInvocation {
		t.Fatalf("missing --facts must be an invocation error, got exit=%d", code)
	}
	assertDiagnosticCode(t, stdout, "JPS-INVOCATION-FACTS")
}

// The draft RFC 0008 grammar is reachable only through the opt-in flag on
// experimental evaluate. spec validate is untouched and still rejects a pack
// using the operators; without the flag the evaluator refuses it for the same
// reason; with the flag it evaluates, and the output says in band that the pack
// is not valid under the published specification.
func TestExperimentalEvaluateRFC0008QuantifierFlag(t *testing.T) {
	pack := filepath.Join("..", "evaluation", "testdata", "rfc0008", "airline-cancellation-quantifier.json")
	facts := filepath.Join(t.TempDir(), "facts.json")
	document := `{"reservation":{"segments":[{"cancelledByAirline":false},{"cancelledByAirline":true}]}}`
	if err := os.WriteFile(facts, []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}

	// spec validate is untouched: the pack is structurally non-conforming.
	code, stdout, stderr := runTest(t, []string{"spec", "validate", pack, "--format", "json"}, "")
	if code != result.ExitInvalid || stderr != "" {
		t.Fatalf("spec validate must still reject a draft-operator pack: exit=%d stderr=%q", code, stderr)
	}
	assertDiagnosticCode(t, stdout, "JPS-STRUCTURE-CONDITION-SHAPE")

	// Without the flag the evaluator refuses it exactly as it does today.
	code, stdout, _ = runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--format", "json"}, "")
	if code != result.ExitInvalid {
		t.Fatalf("without the flag the pack must be refused, got exit=%d", code)
	}
	assertDiagnosticCode(t, stdout, "JPS-EVALUATION-PACK-NOT-CONFORMANT")

	// With the flag it evaluates, and the marker travels with the result.
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0008-quantifiers", "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var output struct {
		SpecVersion    string `json:"specVersion"`
		DraftPrototype *struct {
			RFC                       string   `json:"rfc"`
			Status                    string   `json:"status"`
			Operators                 []string `json:"operators"`
			PackValidUnderSpecVersion bool     `json:"packValidUnderSpecVersion"`
			Note                      string   `json:"note"`
		} `json:"draftPrototype"`
		Disposition struct {
			Kind      string `json:"kind"`
			OutcomeID string `json:"outcomeId"`
		} `json:"disposition"`
	}
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output.Disposition.Kind != "outcome" || output.Disposition.OutcomeID != "free-cancellation" {
		t.Fatalf("disposition = %+v", output.Disposition)
	}
	if output.DraftPrototype == nil {
		t.Fatal("a draft-grammar evaluation must carry the prototype marker")
	}
	if output.DraftPrototype.RFC != "0008" || output.DraftPrototype.Status != "draft-rfc-prototype" {
		t.Fatalf("marker = %+v", output.DraftPrototype)
	}
	if !reflect.DeepEqual(output.DraftPrototype.Operators, []string{"exists"}) {
		t.Fatalf("marker must name the operators used: %v", output.DraftPrototype.Operators)
	}
	if output.DraftPrototype.PackValidUnderSpecVersion || !strings.Contains(output.DraftPrototype.Note, "NOT valid") {
		t.Fatalf("the marker must deny validity under %s: %+v", output.SpecVersion, output.DraftPrototype)
	}

	// The human surface carries the same warning, above the disposition.
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0008-quantifiers"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	marker := strings.Index(stdout, "DRAFT-RFC PROTOTYPE")
	disposition := strings.Index(stdout, "disposition:")
	if marker < 0 || disposition < 0 || marker > disposition {
		t.Fatalf("the prototype marker must precede the disposition: %q", stdout)
	}
	if !strings.Contains(stdout, "NOT valid under JPS 0.1.0-draft") {
		t.Fatalf("the human marker must name the specification version it is not valid under: %q", stdout)
	}

	// The flag on a pack that uses no draft operator changes nothing about it,
	// and the human marker says so rather than accusing a valid pack. The JSON
	// marker of the same run reports packValidUnderSpecVersion: true, so the two
	// surfaces must not contradict each other.
	corePack := filepath.Join("..", "evaluation", "testdata", "rfc0008", "airline-cancellation-prepared.json")
	coreFacts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(coreFacts, []byte(`{"reservation":{"anySegmentCancelledByAirline":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", corePack, "--facts", coreFacts, "--rfc0008-quantifiers"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "DRAFT-RFC PROTOTYPE") || strings.Contains(stdout, "NOT valid") {
		t.Fatalf("a pack using no draft operator must not be called invalid: %q", stdout)
	}
	if !strings.Contains(stdout, "uses no draft operator and remains a plain JPS 0.1.0-draft pack") {
		t.Fatalf("the human marker must say the pack is unchanged by the flag: %q", stdout)
	}
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", corePack, "--facts", coreFacts, "--rfc0008-quantifiers", "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	output.DraftPrototype = nil
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output.DraftPrototype == nil || !output.DraftPrototype.PackValidUnderSpecVersion || len(output.DraftPrototype.Operators) != 0 {
		t.Fatalf("the JSON marker must agree with the human one: %+v", output.DraftPrototype)
	}
}
