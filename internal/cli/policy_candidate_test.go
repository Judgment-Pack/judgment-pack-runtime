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

func candidateCLIBytes(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "policy", "candidate", "examples", "refunds", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func candidateCLIWire(t *testing.T) string {
	t.Helper()
	var manifest struct {
		Materials []struct {
			Path    string `json:"path"`
			Content any    `json:"content"`
		} `json:"materials"`
	}
	if err := json.Unmarshal([]byte(candidateCLIBytes(t, "fixture.json")), &manifest); err != nil {
		t.Fatal(err)
	}
	entries := []any{}
	for _, entry := range manifest.Materials {
		entries = append(entries, map[string]any{"content": entry.Content, "dataBase64": base64.StdEncoding.EncodeToString([]byte(candidateCLIBytes(t, entry.Path)))})
	}
	raw, err := json.Marshal(map[string]any{"materials": entries})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func candidateMCPReply(t *testing.T, name string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
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
		t.Fatal(err)
	}
	return response.Result.Structured, response.Result.IsError
}

func TestPolicyCandidateCLIAndMCPParity(t *testing.T) {
	materialPath := filepath.Join(t.TempDir(), "materials.json")
	if err := os.WriteFile(materialPath, []byte(candidateCLIWire(t)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		operation, document, refusal string
		exit                         int
	}{
		{"canonicalize", candidateCLIBytes(t, "policy.json"), "", 0},
		{"digest", candidateCLIBytes(t, "adoption.json"), "", 0},
		{"validate", candidateCLIBytes(t, "policy.json"), "", 0},
		{"check", candidateCLIBytes(t, "adoption.json"), "", 0},
		{"check", `{}`, "invalid-selector", 1},
		{"check", `{"formatVersion":"7.0.0","n":1.5}`, "unsupported-format", 2},
		{"check", `{"formatVersion":"1.0.0","n":1.5}`, "invalid-document", 1},
		{"check", strings.Repeat("[", 33), "input-over-bound", 2},
	} {
		t.Run(tc.operation+"/"+tc.refusal, func(t *testing.T) {
			args := []string{"experimental", "policy", tc.operation, "-", "--format", "json", "--format-version", policy.CandidateVersion}
			input := map[string]any{"document": tc.document, "formatVersion": policy.CandidateVersion}
			if tc.operation == "check" {
				args = append(args, "--materials", materialPath)
				input["materials"] = candidateCLIWire(t)
			}
			exit, stdout, stderr := runTest(t, args, tc.document)
			if exit != tc.exit || stderr != "" {
				t.Fatalf("exit %d stdout %s stderr %s", exit, stdout, stderr)
			}
			var actual map[string]any
			if err := json.Unmarshal([]byte(stdout), &actual); err != nil {
				t.Fatal(err)
			}
			other, isError := candidateMCPReply(t, "experimental_policy_"+tc.operation, input)
			if isError != (exit != 0) {
				t.Fatal("MCP error status")
			}
			delete(actual, "command")
			delete(other, "command")
			if !reflect.DeepEqual(actual, other) {
				t.Fatalf("candidate CLI/MCP mismatch\n%v\n%v", actual, other)
			}
			if actual["specRevision"] != policy.CandidateRevision || actual["maturity"] != "proposed-draft" {
				t.Fatal("candidate provenance")
			}
			if tc.operation == "check" {
				result := actual["candidateResult"].(map[string]any)
				if tc.refusal != "" && result["code"] != tc.refusal {
					t.Fatal(result)
				}
			} else if actual["candidateResult"] != nil {
				t.Fatal("narrower operation claimed full check")
			}
		})
	}
	for _, operation := range []string{"validate", "check"} {
		args := []string{"experimental", "policy", operation, "-"}
		if operation == "check" {
			args = append(args, "--materials", materialPath)
		}
		exit, stdout, stderr := runTest(t, args, candidateCLIBytes(t, "policy.json"))
		if exit != 0 || stderr != "" || !strings.Contains(stdout, "Proposed-draft candidate") {
			t.Fatalf("candidate human output %d %s %s", exit, stdout, stderr)
		}
	}
}

func TestPolicyExplicitContractDiscoveryParity(t *testing.T) {
	for _, tc := range []struct {
		flags    []string
		input    map[string]any
		registry bool
	}{
		{nil, map[string]any{}, false},
		{[]string{"--all-formats"}, map[string]any{"allFormats": true}, true},
		{[]string{"--format-version", policy.CandidateVersion}, map[string]any{"formatVersion": policy.CandidateVersion}, false},
	} {
		args := append([]string{"experimental", "policy", "contract", "--format", "json"}, tc.flags...)
		exit, stdout, stderr := runTest(t, args, "")
		if exit != 0 || stderr != "" {
			t.Fatal(exit, stdout, stderr)
		}
		var actual map[string]any
		if err := json.Unmarshal([]byte(stdout), &actual); err != nil {
			t.Fatal(err)
		}
		other, isError := candidateMCPReply(t, "experimental_get_policy_contract", tc.input)
		if isError {
			t.Fatal(other)
		}
		delete(actual, "command")
		delete(other, "command")
		if tc.registry {
			for _, result := range []map[string]any{actual, other} {
				for _, format := range result["formats"].([]any) {
					delete(format.(map[string]any), "command")
				}
			}
		}
		if !reflect.DeepEqual(actual, other) {
			t.Fatalf("discovery mismatch %v %v", actual, other)
		}
		if len(tc.flags) == 0 && (actual["formatVersion"] != nil || actual["candidateCapabilities"] != nil || actual["registryVersion"] != nil || actual["specRevision"] != policy.SpecRevision) {
			t.Fatal("legacy contract changed", actual)
		}
	}
	for _, flags := range [][]string{{"--all-formats", "--format-version", "1.0.0"}, {"--format-version", ""}} {
		exit, _, _ := runTest(t, append([]string{"experimental", "policy", "contract"}, flags...), "")
		if exit != 3 {
			t.Fatal("selector conflict accepted")
		}
	}
}
