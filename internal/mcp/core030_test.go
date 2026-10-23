package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Hand-authored public vectors; these are not rows of the named corpus.
const core030PublicPack = `{"specVersion":"0.3.0","id":"https://example.com/packs/routes","version":"1.0.0","title":"Routes","decision":{"intent":"Review","question":"Review?"},"outcomes":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],"rules":[{"id":"one","description":"One","when":{"op":"literal","value":false},"outcome":"yes","onUnknown":"ignore"}],"escalation":{"triggers":["no-match"],"routes":[{"target":{"kind":"queue","name":"Z<&"}},{"target":{"kind":"system","name":"A"}},{"target":{"kind":"queue","name":"Z<&"}}]}}`
const core030PublicExpected = `{"handoff":{"routes":[{"kind":"queue","name":"Z<&"},{"kind":"system","name":"A"},{"kind":"queue","name":"Z<&"}],"state":"requested","triggeredBy":["no-match"]},"kind":"unresolved","reasons":["no-match"]}`
const core030PublicMatrix = `{"matrixVersion":"3","cases":[{"id":"routes","facts":{},"expectedDisposition":` + core030PublicExpected + `}]}`

func core030Both(t *testing.T, call string) map[string]any {
	t.Helper()
	response := runServer(t, call)[0]["result"].(map[string]any)
	structured, ok := response["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("no structured report: %+v", response)
	}
	var text map[string]any
	if err := json.Unmarshal([]byte(toolText(t, response)), &text); err != nil || !reflect.DeepEqual(structured, text) {
		t.Fatalf("serialization mismatch: %v, %+v", err, response)
	}
	return structured
}

func TestCore030PublicRoutedEvaluationBothSerializations(t *testing.T) {
	t.Chdir(t.TempDir())
	report := core030Both(t, toolCall(t, 1, "experimental_evaluate", map[string]any{"pack": core030PublicPack, "facts": "{}", "rehearsal": true}))
	if report["evaluatorSpecVersion"] != "0.3.0" || report["specVersion"] != "0.3.0" {
		t.Fatal(report)
	}
	if _, present := report["handoffTarget"]; present {
		t.Fatal("invented singular target", report)
	}
	encoded, _ := json.Marshal(report)
	var typed result.Evaluation
	if err := json.Unmarshal(encoded, &typed); err != nil {
		t.Fatal(err)
	}
	canonical, err := typed.Disposition.CanonicalWith(result.Core030EvaluatorContract())
	if err != nil || string(canonical) != core030PublicExpected {
		t.Fatalf("typed route output: %s, %v", canonical, err)
	}
	report = core030Both(t, toolCall(t, 2, "experimental_test_cases", map[string]any{"pack": core030PublicPack, "matrix": core030PublicMatrix}))
	if report["status"] != "passed" || report["evaluatorSpecVersion"] != "0.3.0" {
		t.Fatal(report)
	}
}

func TestCore030PublicStandaloneContractSelection(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, version := range []string{"", expectationSpec, "0.3.0"} {
		args := map[string]any{"expectations": []string{core030PublicExpected}}
		if version != "" {
			args["spec_version"] = version
		}
		report := core030Both(t, toolCall(t, 1, expectationTool, args))
		finding := report["results"].([]any)[0].(map[string]any)
		want := "invalid"
		if version == "0.3.0" {
			want = "valid"
		}
		if finding["status"] != want {
			t.Fatal(version, report)
		}
		if want == "valid" && finding["canonical"] != core030PublicExpected {
			t.Fatal(report)
		}
		args = map[string]any{"matrix": core030PublicMatrix}
		if version != "" {
			args["spec_version"] = version
		}
		report = core030Both(t, toolCall(t, 2, matrixValidationTool, args))
		if report["status"] != want {
			t.Fatal(version, report)
		}
	}
	report := core030Both(t, toolCall(t, 3, matrixContractTool, map[string]any{"spec_version": "0.3.0"}))
	if report["specVersion"] != "0.3.0" || !strings.Contains(report["contract"].(map[string]any)["memberTypes"].(map[string]any)["expectedDisposition"].(string), "0.3.0") {
		t.Fatal(report)
	}
	report = core030Both(t, toolCall(t, 4, matrixValidationTool, map[string]any{"pack": core030PublicPack, "matrix": core030PublicMatrix}))
	if report["specVersion"] != "0.3.0" || report["status"] != "valid" {
		t.Fatal(report)
	}
	for _, version := range []any{expectationSpec, nil, "", 42} {
		response := runServer(t, toolCall(t, 5, matrixValidationTool, map[string]any{"spec_version": version, "pack": core030PublicPack, "matrix": core030PublicMatrix}))[0]["result"].(map[string]any)
		if response["isError"] != true {
			t.Fatal("explicit conflict/invalid version accepted", response)
		}
	}
}

func TestCore030PublicProfileRefusalAndErrorContract(t *testing.T) {
	t.Chdir(t.TempDir())
	extended := strings.Replace(core030PublicPack, `"specVersion":"0.3.0"`, `"specVersion":"0.3.0","profile":"extended","invalid":true`, 1)
	calls := []string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack": extended, "facts": "{", "evidence": "{"}),
		toolCall(t, 2, factAdviceTool, map[string]any{"pack": extended, "facts": "{"}),
		toolCall(t, 3, matrixValidationTool, map[string]any{"pack": extended, "matrix": core030PublicMatrix}),
		toolCall(t, 4, "experimental_test_cases", map[string]any{"pack": extended, "matrix": core030PublicMatrix}),
	}
	for _, call := range calls {
		response := runServer(t, call)[0]["result"].(map[string]any)
		report := response["structuredContent"].(map[string]any)
		want := []any{map[string]any{"code": "profile-unsupported", "profile": "extended"}}
		if response["isError"] != true || report["status"] != "unsupported" || !reflect.DeepEqual(report["diagnostics"], want) {
			t.Fatal(report)
		}
		if _, present := report["evaluationError"]; present {
			t.Fatal("invented profile error class", report)
		}
	}
	response := runServer(t, toolCall(t, 5, "experimental_evaluate", map[string]any{"pack": core030PublicPack, "facts": "{"}))[0]["result"].(map[string]any)
	report := response["structuredContent"].(map[string]any)
	if report["evaluationError"].(map[string]any)["evaluatorSpecVersion"] != "0.3.0" {
		t.Fatal(report)
	}
}

func TestCore030PublicAdviceRemainsStaticAndCapabilitiesStrict(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "jpack.json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	report := core030Both(t, toolCall(t, 1, factAdviceTool, map[string]any{"pack": core030PublicPack, "facts": "{}"}))
	if report["status"] != "complete" || report["specVersion"] != "0.3.0" || report["factCheckVersion"] != "1" {
		t.Fatal(report)
	}
	for _, capabilities := range []any{nil, []any{nil}} {
		response := runServer(t, toolCall(t, 2, factAdviceTool, map[string]any{"pack": core030PublicPack, "facts": "{}", "supported_extensions": capabilities}))[0]["result"].(map[string]any)
		if response["isError"] != true {
			t.Fatal("invalid capabilities normalized", response)
		}
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatal("advice accessed project or wrote files", files, err)
	}
}

func TestCore030PublicFailureContextFallback(t *testing.T) {
	failure := &evaluation.Failure{SpecVersion: "0.3.0", Class: result.ClassMalformedInput, Phase: result.PhasePreflight, Code: "JPS-TEST", ExitCode: result.ExitInvalid}
	response := evaluationToolError("test", failure)
	report := response["structuredContent"].(result.OperationalError)
	if report.EvaluationError.EvaluatorSpecVersion != "0.3.0" {
		t.Fatal(report)
	}
	failure.SpecVersion = "unsupported"
	response = evaluationToolError("test", failure)
	report = response["structuredContent"].(result.OperationalError)
	if report.EvaluationError.EvaluatorSpecVersion != result.EvaluatorSpecVersion {
		t.Fatal("unsupported context became a contract", report)
	}
}
