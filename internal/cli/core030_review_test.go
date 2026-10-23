package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func reviewProfileRefusal(t *testing.T, code int, text string) {
	t.Helper()
	var report result.OperationalError
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		t.Fatal(err, text)
	}
	if code != result.ExitUnsupported || report.Status != "unsupported" || report.EvaluationError != nil || len(report.Diagnostics) != 1 {
		t.Fatal(code, report)
	}
	encoded, _ := json.Marshal(report.Diagnostics[0])
	if string(encoded) != `{"code":"profile-unsupported","profile":"extended"}` {
		t.Fatalf("nonexact profile diagnostic: %s", encoded)
	}
}

type reviewCountingReader struct{ reads int }

func (r *reviewCountingReader) Read(p []byte) (int, error) { r.reads++; return 0, io.EOF }

func TestCore030ReviewCLIProfilePrecedesFactsEvidenceAndLaw(t *testing.T) {
	t.Chdir(t.TempDir())
	const extended = `{"specVersion":"0.3.0","profile":"extended"}`
	missing := filepath.Join(t.TempDir(), "missing")
	code, text, _ := runTest(t, []string{"experimental", "evaluate", "-", "--facts", missing, "--format", "json"}, extended)
	reviewProfileRefusal(t, code, text)
	pack := writeDocument(t, "pack.json", extended)
	facts := writeDocument(t, "facts.json", "{}")
	code, text, _ = runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--evidence", missing, "--format", "json"}, "")
	reviewProfileRefusal(t, code, text)
	reader := &reviewCountingReader{}
	var stdout, stderr bytes.Buffer
	code = Run([]string{"experimental", "evaluate", pack, "--facts", "-", "--format", "json"}, reader, &stdout, &stderr)
	reviewProfileRefusal(t, code, stdout.String())
	if reader.reads != 0 {
		t.Fatal("facts stdin consumed before profile refusal")
	}
	config := writeProjectFixture(t, `{"configVersion":"5","requireReviewed":true,"packs":{"a":{"path":"pack.json"}}}`, map[string]string{"pack.json": extended})
	code, text, _ = runTest(t, []string{"experimental", "evaluate", "--pack-id", "a", "--config", config, "--facts", facts, "--format", "json"}, "")
	reviewProfileRefusal(t, code, text)
	code, text, _ = runTest(t, []string{"experimental", "evaluate", "-", "--facts", missing, "--format", "json"}, strings.TrimSuffix(extended, "}"))
	if code == 0 || !strings.Contains(text, "JPS-EVALUATION-INPUT-JSON") || strings.Contains(text, "profile-unsupported") {
		t.Fatal("accepted malformed pack prefix", code, text)
	}
}

func TestCore030ReviewCLICompareNegotiatesBeforeOtherAcquisition(t *testing.T) {
	t.Chdir(t.TempDir())
	extended := writeDocument(t, "pack.json", `{"specVersion":"0.3.0","profile":"extended"}`)
	base := writeDocument(t, "base.json", core030CLIPack)
	missing := filepath.Join(t.TempDir(), "missing")
	for _, sides := range [][2]string{{extended, missing}, {base, extended}} {
		code, text, _ := runTest(t, []string{"experimental", "compare", sides[0], sides[1], "--inputs", missing, "--format", "json"}, "")
		reviewProfileRefusal(t, code, text)
	}
}

func TestCore030ReviewCLIComparePreservesMalformedPackRows(t *testing.T) {
	t.Chdir(t.TempDir())
	good := writeDocument(t, "good.json", expensePack("5000", "Reviewer"))
	malformed := writeDocument(t, "malformed.json", `{"specVersion":"0.2.0-draft"`)
	inputs := writeDocument(t, "cases.json", `{"cases":[{"id":"x","facts":{},"expectedErrorClass":"unused"}]}`)
	code, text, stderr := runTest(t, []string{"experimental", "compare", good, malformed, "--inputs", inputs, "--format", "json"}, "")
	var comparison result.PackComparison
	if err := json.Unmarshal([]byte(text), &comparison); err != nil {
		t.Fatal(err, text)
	}
	if code != 0 || stderr != "" || len(comparison.Differences) != 1 || comparison.Differences[0].New.EvaluationError == nil || comparison.Differences[0].New.EvaluationError.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" {
		t.Fatal(code, text, stderr)
	}
}

func TestCore030ReviewCLIGraphNegotiatesBeforeInputsRowsAndLaw(t *testing.T) {
	t.Chdir(t.TempDir())
	config := writeProjectFixture(t, `{"configVersion":"5","requireReviewed":true,"packs":{"a":{"path":"pack.json"}}}`, map[string]string{"pack.json": `{"specVersion":"0.3.0","profile":"extended"}`, "graph.json": `{"formatVersion":"1","id":"review","version":"1.0.0","nodes":{"a":{"pack":"a"}},"edges":[],"result":"a"}`})
	graph := filepath.Join(filepath.Dir(config), "graph.json")
	missing := filepath.Join(t.TempDir(), "missing")
	for _, args := range [][]string{
		{"experimental", "graph", "evaluate", graph, "--config", config, "--inputs", missing, "--format", "json"},
		{"experimental", "graph", "test", graph, "--config", config, "--rows", missing, "--format", "json"},
	} {
		code, text, _ := runTest(t, args, "")
		reviewProfileRefusal(t, code, text)
	}
}

// Root parsing is deliberately first even when a malformed pack was intended
// for the legacy contract: an incomplete text cannot establish a profile.
func TestCore030ReviewCLILegacySyntaxPrecedesLaterReadFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	missing := filepath.Join(t.TempDir(), "missing")
	code, text, _ := runTest(t, []string{"experimental", "evaluate", "-", "--facts", missing, "--format", "json"}, `{"specVersion":"0.2.0-draft"`)
	if code == 0 || !strings.Contains(text, "JPS-EVALUATION-INPUT-JSON") || strings.Contains(text, "JPS-INPUT-READ") {
		t.Fatal(code, text)
	}
}
