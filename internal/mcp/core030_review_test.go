package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func reviewProfileReport(t *testing.T, call string) {
	t.Helper()
	response := runServer(t, call)[0]["result"].(map[string]any)
	var report result.OperationalError
	decodeStructured(t, response, &report)
	if response["isError"] != true || report.Status != "unsupported" || report.EvaluationError != nil || len(report.Diagnostics) != 1 {
		t.Fatal(response)
	}
	encoded, _ := json.Marshal(report.Diagnostics[0])
	if string(encoded) != `{"code":"profile-unsupported","profile":"extended"}` {
		t.Fatalf("nonexact profile diagnostic: %s", encoded)
	}
}

func TestCore030ReviewMCPProfilePrecedesLawAndMatrixAdmission(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	const extended = `{"specVersion":"0.3.0","profile":"extended"}`
	config := filepath.Join(dir, project.DefaultConfigName)
	if err := os.WriteFile(config, []byte(`{"configVersion":"5","requireReviewed":true,"packs":{"a":{"path":"pack.json","matrix":"missing.matrix"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack.json"), []byte(extended), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, config)
	for _, call := range []string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack": extended, "facts": "{}"}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack_id": "a", "facts": "{}"}),
		toolCall(t, 3, "experimental_test_cases", map[string]any{"pack": extended, "matrix": "{"}),
		toolCall(t, 4, "experimental_test_packs", map[string]any{}),
	} {
		reviewProfileReport(t, call)
	}
	// A complete bounded scan, never the visible unsupported-profile prefix.
	response := runServer(t, toolCall(t, 5, "experimental_test_cases", map[string]any{"pack": strings.TrimSuffix(extended, "}"), "matrix": "{"}))[0]["result"].(map[string]any)
	var report result.OperationalError
	decodeStructured(t, response, &report)
	if response["isError"] != true || report.Diagnostics[0].Code != "JPS-EVALUATION-INPUT-JSON" {
		t.Fatal(response)
	}
}

func TestCore030ReviewMCPLegacySyntaxPrecedesLawFailure(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	config := filepath.Join(dir, project.DefaultConfigName)
	if err := os.WriteFile(config, []byte(`{"configVersion":"5","requireReviewed":true,"packs":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, config)
	response := runServer(t, toolCall(t, 1, "experimental_evaluate", map[string]any{"pack": `{"specVersion":"0.2.0-draft"`, "facts": "{}"}))[0]["result"].(map[string]any)
	var report result.OperationalError
	decodeStructured(t, response, &report)
	if response["isError"] != true || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "JPS-EVALUATION-INPUT-JSON" {
		t.Fatal(response)
	}
}
