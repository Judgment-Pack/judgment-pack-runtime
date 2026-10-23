package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const core030CLIPack = `{"specVersion":"0.3.0","id":"https://example.com/packs/routes","version":"1.0.0","title":"Routes","decision":{"intent":"Review","question":"Review?"},"outcomes":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],"rules":[{"id":"one","description":"One","when":{"op":"literal","value":false},"outcome":"yes","onUnknown":"ignore"}],"escalation":{"triggers":["no-match"],"routes":[{"target":{"kind":"queue","name":"Z"}},{"target":{"kind":"system","name":"A"}},{"target":{"kind":"queue","name":"Z"}}]}}`
const core030CLIExpected = `{"handoff":{"routes":[{"kind":"queue","name":"Z"},{"kind":"system","name":"A"},{"kind":"queue","name":"Z"}],"state":"requested","triggeredBy":["no-match"]},"kind":"unresolved","reasons":["no-match"]}`

func TestCore030CLIRoutesAndBoundedHumanRendering(t *testing.T) {
	t.Chdir(t.TempDir())
	pack := writeDocument(t, "pack.json", core030CLIPack)
	facts := writeDocument(t, "facts.json", "{}")
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("evaluation: %d %s %s", code, stdout, stderr)
	}
	var typed result.Evaluation
	if err := json.Unmarshal([]byte(stdout), &typed); err != nil {
		t.Fatal(err)
	}
	canonical, err := typed.Disposition.CanonicalWith(result.Core030EvaluatorContract())
	if err != nil || string(canonical) != core030CLIExpected || typed.EvaluatorSpecVersion != "0.3.0" || typed.HandoffTarget != nil {
		t.Fatalf("typed: %s %v %+v", canonical, err, typed)
	}
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts}, "")
	if code != 0 || stderr != "" || strings.Count(stdout, "handoff route[") != 3 || strings.Contains(stdout, "no declared destination") {
		t.Fatalf("human: %d %s %s", code, stdout, stderr)
	}
	large := strings.ReplaceAll(core030CLIPack, `"name":"Z"`, `"name":"`+strings.Repeat("z", 4096)+`"`)
	pack = writeDocument(t, "large.json", large)
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts}, "")
	if code != 0 || stderr != "" || len(stdout) > 4096 {
		t.Fatalf("unbounded human: %d bytes=%d %s", code, len(stdout), stderr)
	}
}

func TestCore030CLIFailureContractAndExactProfile(t *testing.T) {
	t.Chdir(t.TempDir())
	facts := writeDocument(t, "facts.json", "{")
	for _, profile := range []string{"base", "extended"} {
		pack := strings.Replace(core030CLIPack, `"specVersion":"0.3.0"`, `"specVersion":"0.3.0","profile":"`+profile+`"`, 1)
		path := writeDocument(t, "pack.json", pack)
		code, stdout, _ := runTest(t, []string{"experimental", "evaluate", path, "--facts", facts, "--format", "json"}, "")
		var report map[string]any
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatal(err, stdout)
		}
		if profile == "extended" {
			if code != result.ExitUnsupported || report["status"] != "unsupported" {
				t.Fatal(code, report)
			}
			diagnostic := report["diagnostics"].([]any)[0].(map[string]any)
			if len(diagnostic) != 2 || diagnostic["code"] != "profile-unsupported" || diagnostic["profile"] != "extended" || report["evaluationError"] != nil {
				t.Fatal(report)
			}
		} else if code == 0 || report["evaluationError"].(map[string]any)["evaluatorSpecVersion"] != "0.3.0" {
			t.Fatal(code, report)
		}
	}
}

func TestCore030CLIPackBytesRemainTheAuditIdentity(t *testing.T) {
	t.Chdir(t.TempDir())
	pack := " \n" + strings.Replace(core030CLIPack, `"specVersion":"0.3.0"`, `"specVersion":"0.3.0","profile":"base"`, 1) + "\n"
	config := writeProjectFixture(t, `{"configVersion":"3","audit":{"dir":"audit"},"packs":{"routes":{"path":"pack.json"}}}`, map[string]string{"pack.json": pack})
	facts := writeDocument(t, "facts.json", "{}")
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "routes", "--config", config, "--facts", facts, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("evaluation: %d %s %s", code, stdout, stderr)
	}
	records := auditRecords(t, config)
	if len(records) != 1 || records[0]["pack"].(map[string]any)["digest"] != audit.Digest([]byte(pack)) || records[0]["evaluatorSpecVersion"] != "0.3.0" {
		t.Fatal(records)
	}
	var typed result.Disposition
	encoded, _ := json.Marshal(records[0]["disposition"])
	if err := json.Unmarshal(encoded, &typed); err != nil {
		t.Fatal(err)
	}
	canonical, err := typed.CanonicalWith(result.Core030EvaluatorContract())
	if err != nil || string(canonical) != core030CLIExpected {
		t.Fatalf("audit route bytes: %s %v", canonical, err)
	}
}

func TestCore030CLICompareSelectionRefusals(t *testing.T) {
	t.Chdir(t.TempDir())
	old := writeDocument(t, "old.json", core030CLIPack)
	extended := writeDocument(t, "extended.json", strings.Replace(core030CLIPack, `"specVersion":"0.3.0"`, `"specVersion":"0.3.0","profile":"extended"`, 1))
	inputs := writeDocument(t, "cases.json", `{"cases":[{"id":"x","facts":{},"expectedErrorClass":"unused"}]}`)
	code, stdout, _ := runTest(t, []string{"experimental", "compare", old, extended, "--inputs", inputs, "--format", "json"}, "")
	var report result.OperationalError
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err, stdout)
	}
	if code != result.ExitUnsupported || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "profile-unsupported" || report.Diagnostics[0].Profile != "extended" || report.EvaluationError != nil {
		t.Fatal(code, report)
	}
	legacy := writeDocument(t, "legacy.json", strings.Replace(core030CLIPack, `"specVersion":"0.3.0"`, `"specVersion":"0.2.0-draft"`, 1))
	code, stdout, _ = runTest(t, []string{"experimental", "compare", old, legacy, "--inputs", inputs, "--format", "json"}, "")
	if code != result.ExitUnsupported || !strings.Contains(stdout, "JPS-COMPARE-MIXED-CONTRACT") {
		t.Fatal(code, stdout)
	}
}

func TestCore030CLIFailureContextFallback(t *testing.T) {
	var output bytes.Buffer
	app := &App{out: &output}
	failure := &evaluation.Failure{SpecVersion: "0.3.0", Class: result.ClassMalformedInput, Phase: result.PhasePreflight, Code: "JPS-TEST", ExitCode: result.ExitInvalid}
	_ = app.evaluationFailure("test", "json", failure)
	var report result.OperationalError
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.EvaluationError.EvaluatorSpecVersion != "0.3.0" {
		t.Fatal(report)
	}
	output.Reset()
	failure.SpecVersion = "unsupported"
	_ = app.evaluationFailure("test", "json", failure)
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.EvaluationError.EvaluatorSpecVersion != result.EvaluatorSpecVersion {
		t.Fatal("unsupported context became a contract", report)
	}
}

func TestCore030CLIConformanceShowsUncheckedAdvisoryCases(t *testing.T) {
	var text bytes.Buffer
	app := &App{out: &text}
	suite := result.Suite{Status: "valid", SpecVersion: "0.3.0", Summary: result.SuiteSummary{Total: 156, Passed: 151, Skipped: 5}}
	for index := 0; index < 5; index++ {
		suite.Cases = append(suite.Cases, result.Case{ID: fmt.Sprintf("advice-%d", index), Status: "skipped", ActualStatus: "not-checked", SkipReason: "Optional expectedAdvisories assertions are not implemented."})
	}
	if err := app.renderSuite("human", suite); err != nil {
		t.Fatal(err)
	}
	rendered := text.String()
	if !strings.Contains(rendered, "151/151 checked conformance cases matched expectations; 5 skipped (not checked)") || strings.Count(rendered, "[not checked]: Optional expectedAdvisories assertions are not implemented.") != 5 {
		t.Fatal(rendered)
	}
	text.Reset()
	suite.Summary = result.SuiteSummary{Total: 20, Passed: 20}
	suite.Cases = nil
	if err := app.renderSuite("human", suite); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text.String(), "passed: 20/20 conformance cases matched expectations\n") || strings.Contains(text.String(), "skipped") {
		t.Fatal("legacy renderer changed", text.String())
	}
}

func TestCore030CLIExamplesNameTheSelectedEvaluator(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"spec", "examples", "--spec-version", "0.3.0", "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatal(code, stdout, stderr)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if report["specVersion"] != "0.3.0" || report["evaluatorSpecVersion"] != "0.3.0" {
		t.Fatal(report)
	}
	code, stdout, stderr = runTest(t, []string{"spec", "examples", "--spec-version", "0.3.0"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "the evaluator admits 0.3.0: a pack made from this set needs no re-declaration") {
		t.Fatal(code, stdout, stderr)
	}
}
