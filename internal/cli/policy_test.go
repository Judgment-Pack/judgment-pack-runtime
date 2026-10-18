package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/conformance"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/mcp"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/policy"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func policyFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "policy", "prototype", "examples", "refunds", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func policyMaterialsFixture(t *testing.T, adoption bool) string {
	t.Helper()
	names := []string{"narrative.txt", "source.txt"}
	if adoption {
		names = append(names, "interpretation.txt", "consumer-pack.json", "policy.json")
	}
	var fixture struct {
		Materials []struct {
			Path    string `json:"path"`
			Content any    `json:"content"`
		} `json:"materials"`
	}
	if err := json.Unmarshal([]byte(policyFixture(t, "fixture.json")), &fixture); err != nil {
		t.Fatal(err)
	}
	materials := []any{}
	for _, name := range names {
		for _, item := range fixture.Materials {
			if item.Path == name {
				materials = append(materials, map[string]any{"content": item.Content, "dataBase64": base64.StdEncoding.EncodeToString([]byte(policyFixture(t, name)))})
			}
		}
	}
	raw, err := json.Marshal(map[string]any{"materials": materials})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestPolicyCLIAndMCPParity(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	materials := policyMaterialsFixture(t, true)
	materialsPath := filepath.Join(t.TempDir(), "materials.json")
	if err := os.WriteFile(materialsPath, []byte(materials), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		operation, document, materials string
		exit                           int
	}{
		{"canonicalize", policyFixture(t, "policy.json"), "", 0}, {"digest", policyFixture(t, "adoption.json"), "", 0},
		{"validate", policyFixture(t, "policy.json"), "", 0}, {"check", policyFixture(t, "adoption.json"), materials, 0},
		{"check", policyFixture(t, "adoption.json"), `{"materials":[]}`, 1},
		{"validate", `{"kind":"policy","secret":"must-not-echo"}`, "", 2},
		{"canonicalize", `{"kind":"policy","kind":"policy"}`, "", 1},
		{"digest", strings.Repeat(" ", policy.MaxDocumentBytes+1), "", 2},
	} {
		t.Run(tc.operation+"/"+tc.document[:min(20, len(tc.document))], func(t *testing.T) {
			args := []string{"experimental", "policy", tc.operation, "-", "--format", "json"}
			input := map[string]any{"document": tc.document}
			if tc.operation == "check" {
				if err := os.WriteFile(materialsPath, []byte(tc.materials), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--materials", materialsPath)
				input["materials"] = tc.materials
			}
			code, stdout, stderr := runTest(t, args, tc.document)
			if stderr != "" || code != tc.exit {
				t.Fatal(code, stdout, stderr)
			}
			var cli map[string]any
			if err := json.Unmarshal([]byte(stdout), &cli); err != nil {
				t.Fatal(err, stdout)
			}
			request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "experimental_policy_" + tc.operation, "arguments": input}})
			if err != nil {
				t.Fatal(err)
			}
			var out, logs bytes.Buffer
			if err := mcp.NewServer(engine, conformance.NewRunner(engine)).Serve(bytes.NewReader(append(request, '\n')), &out, &logs); err != nil {
				t.Fatal(err)
			}
			var response struct {
				Result struct {
					Structured map[string]any `json:"structuredContent"`
					IsError    bool           `json:"isError"`
				} `json:"result"`
			}
			if err := json.Unmarshal(out.Bytes(), &response); err != nil {
				t.Fatal(err, out.String())
			}
			if response.Result.IsError != (code != 0) {
				t.Fatal(code, out.String())
			}
			delete(cli, "command")
			delete(response.Result.Structured, "command")
			if !reflect.DeepEqual(cli, response.Result.Structured) {
				t.Fatalf("CLI/MCP mismatch\n%v\n%v", cli, response.Result.Structured)
			}
			if strings.Contains(stdout, "must-not-echo") || strings.Contains(logs.String(), "must-not-echo") || strings.Contains(stdout, "dataBase64") || strings.Contains(stdout, base64.StdEncoding.EncodeToString([]byte(policyFixture(t, "source.txt")))) {
				t.Fatal("document plaintext echoed")
			}
		})
	}
}

func TestPolicyCLIArtifactsAndBoundedReads(t *testing.T) {
	code, starter, stderr := runTest(t, []string{"experimental", "policy", "init", "--kind", "policy", "--format-version", "0.0.2", "--id", "urn:test:policy", "--version", "1.2.3"}, "")
	if code != 0 || !json.Valid([]byte(starter)) || !strings.Contains(stderr, "missing and unchecked") {
		t.Fatal(code, starter, stderr)
	}

	for _, kind := range []string{"policy", "governing-adoption"} {
		code, raw, stderr := runTest(t, []string{"experimental", "policy", "init", "--kind", kind, "--format-version", "0.0.2", "--id", "urn:test:policy", "--version", "1.2.3", "--format", "json"}, "")
		var report policy.Report
		if code != 0 || stderr != "" || json.Unmarshal([]byte(raw), &report) != nil || report.Status != "starter-template" {
			t.Fatal(code, raw, stderr)
		}
		code, raw, stderr = runTest(t, []string{"experimental", "policy", "schema", "--kind", kind, "--format-version", "0.0.2"}, "")
		if code != 0 || stderr != "" || !json.Valid([]byte(raw)) {
			t.Fatal(code, raw, stderr)
		}
	}
	for _, tc := range []struct {
		args  []string
		input string
		exit  int
	}{
		{[]string{"init", "--format", "json"}, "", 3},
		{[]string{"schema", "--kind", "policy", "--format-version", "0.0.3", "--format", "json"}, "", 2},
		{[]string{"check", "-", "--materials", "-", "--format", "json"}, "{}", 3},
		{[]string{"check", "-", "--format", "json"}, "{}", 3},
		{[]string{"validate", t.TempDir(), "--format", "json"}, "", 4},
		{[]string{"validate", "https://example.invalid/private", "--format", "json"}, "", 4},
		{[]string{"digest", "-", "--format", "json"}, strings.Repeat(" ", policy.MaxDocumentBytes+1), 2},
		{[]string{"validate", "-", "--unknown", "--format", "json"}, "", 3},
	} {
		code, raw, stderr := runTest(t, append([]string{"experimental", "policy"}, tc.args...), tc.input)
		if code != tc.exit || stderr != "" || !strings.Contains(raw, `"command":"experimental policy `) {
			t.Fatal(tc.args, code, raw, stderr)
		}
	}
	code, raw, stderr := runTest(t, []string{"experimental", "policy", "validate", "-"}, policyFixture(t, "policy.json"))
	if code != 0 || stderr != "" || !strings.Contains(raw, "Retained materials were not checked") {
		t.Fatal(code, raw, stderr)
	}
	code, raw, stderr = runTest(t, []string{"experimental", "policy", "validate", "-"}, `{"secret":"must-not-echo"}`)
	if code == 0 || raw != "" || strings.Contains(stderr, "must-not-echo") || !strings.Contains(stderr, "JPS-POLICY-") {
		t.Fatal(code, raw, stderr)
	}
}

func TestPolicyCLIAndMCPSelectorParity(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		operation, kind, format, id, version string
		exit                                 int
	}{
		{"schema", "policy", "0.0.2", "", "", 0},
		{"schema", "policy", "0.0.3", "", "", 2},
		{"schema", "unknown", "0.0.2", "", "", 2},
		{"schema", "policy", "", "", "", 2},
		{"init", "policy", "0.0.2", "urn:test:policy", "1.0.0", 0},
		{"init", "policy", "0.0.2", "relative", "1.0.0", 1},
		{"init", "policy", "0.0.2", "urn:test:policy", "1.0", 1},
		{"init", "policy", "0.0.2", "urn:test:policy", "", 1},
	} {
		args := []string{"experimental", "policy", tc.operation, "--kind", tc.kind, "--format-version", tc.format, "--format", "json"}
		input := map[string]any{"kind": tc.kind, "formatVersion": tc.format}
		name := "experimental_get_policy_schema"
		if tc.operation == "init" {
			args = append(args, "--id", tc.id, "--version", tc.version)
			input["id"], input["version"] = tc.id, tc.version
			name = "experimental_policy_init"
		}
		code, stdout, stderr := runTest(t, args, "")
		if code != tc.exit || stderr != "" {
			t.Fatal(tc, code, stdout, stderr)
		}
		var cli map[string]any
		if err := json.Unmarshal([]byte(stdout), &cli); err != nil {
			t.Fatal(err, stdout)
		}
		request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": input}})
		if err != nil {
			t.Fatal(err)
		}
		var out, logs bytes.Buffer
		if err := mcp.NewServer(engine, conformance.NewRunner(engine)).Serve(bytes.NewReader(append(request, '\n')), &out, &logs); err != nil {
			t.Fatal(err)
		}
		var response struct {
			Result struct {
				Structured map[string]any `json:"structuredContent"`
				IsError    bool           `json:"isError"`
			} `json:"result"`
		}
		if err := json.Unmarshal(out.Bytes(), &response); err != nil {
			t.Fatal(err, out.String())
		}
		if response.Result.IsError != (code != 0) {
			t.Fatal(tc, out.String())
		}
		delete(cli, "command")
		delete(response.Result.Structured, "command")
		if !reflect.DeepEqual(cli, response.Result.Structured) {
			t.Fatalf("%v CLI/MCP mismatch\n%v\n%v", tc, cli, response.Result.Structured)
		}
	}
}

func TestPolicyCheckMaterialStdinAndWireBound(t *testing.T) {
	file := filepath.Join(t.TempDir(), "policy.json")
	document := policyFixture(t, "policy.json")
	if err := os.WriteFile(file, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"experimental", "policy", "check", file, "--materials", "-"}
	code, stdout, stderr := runTest(t, args, policyMaterialsFixture(t, false))
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Full Core semantics and evaluation were not checked") {
		t.Fatal(code, stdout, stderr)
	}
	args = append(args, "--format", "json")
	code, stdout, stderr = runTest(t, args, strings.Repeat(" ", policy.MaxMaterialsWireBytes+1))
	if code != 2 || stderr != "" || !strings.Contains(stdout, "JPS-POLICY-MATERIALS-BOUND") || len(stdout) > 8192 {
		t.Fatal(code, stdout, stderr)
	}
	after, err := os.ReadFile(file)
	if err != nil || string(after) != document {
		t.Fatal("check modified input", err)
	}
}
