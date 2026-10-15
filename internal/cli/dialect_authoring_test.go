package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/conformance"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/dialect"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/mcp"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func TestDialectResourcesCLIAndMCP(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"schema", "reference"} {
		code, raw, stderr := runTest(t, []string{"dialect", op}, "")
		if code != 0 || stderr != "" {
			t.Fatal(code, stderr)
		}
		var expected dialect.Report
		if op == "schema" {
			expected, _ = dialect.Schema("test", 2)
		} else {
			expected = dialect.Reference("test")
		}
		if raw != expected.Resource.Text {
			t.Fatal("CLI rewrote resource bytes")
		}
		code, report, stderr := runTest(t, []string{"dialect", op, "--format", "json", "--pretty"}, "")
		if code != 0 || stderr != "" {
			t.Fatal(code, stderr)
		}
		mcpReport, isError := dialectMCPResult(t, engine, "get_dialect_"+op, map[string]any{})
		var cliReport map[string]any
		if err = json.Unmarshal([]byte(report), &cliReport); err != nil {
			t.Fatal(err)
		}
		delete(cliReport, "command")
		delete(mcpReport, "command")
		if isError || !reflect.DeepEqual(cliReport, mcpReport) {
			t.Fatal("CLI/MCP resource mismatch")
		}
	}
	code, stdout, stderr := runTest(t, []string{"dialect", "schema", "--dialect-version", "3", "--format", "json"}, "")
	if code != 2 || stderr != "" || strings.Contains(stdout, `"resource":`) {
		t.Fatal(code, stdout, stderr)
	}
}
func dialectMCPResult(t *testing.T, engine *validation.Engine, name string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	var out, logs bytes.Buffer
	server := mcp.NewServer(engine, conformance.NewRunner(engine))
	if err := server.Serve(bytes.NewReader(append(request, '\n')), &out, &logs); err != nil {
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
	return response.Result.Structured, response.Result.IsError
}
func TestDialectCheckCLIAndMCP(t *testing.T) {
	engine, _ := validation.NewEngine()
	pack, err := os.ReadFile("../artifacts/jps/0.2.0-draft/cases/valid/minimal-expense-approval.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "pack.json")
	if err = os.WriteFile(file, pack, 0600); err != nil {
		t.Fatal(err)
	}
	// Deliberately invalid project configuration must have no effect on these
	// explicit, read-only snapshot operations.
	config := filepath.Join(dir, "jpack.json")
	os.WriteFile(config, []byte("broken"), 0600)
	t.Setenv("JPACK_CONFIG", config)
	for _, raw := range []string{
		`{"version":2,"case":{"facts":[{"target":"/expense","source":"/expense"}],"evidence":[{"requirement":"receipt","source":"/r"},{"requirement":"cost-center","source":"/c"}]}}`,
		`{"version":2,"case":{"facts":[],"evidence":[]}}`,
		`{"version":2,"case":{"facts":[],"evidence":[]},"unmapped":["/expense"],"unmappedEvidence":["receipt","cost-center"]}`,
		`{"version":3}`, `{"version":2,"version":2}`, strings.Repeat(" ", dialect.MaxDocumentBytes+1),
	} {
		code, stdout, stderr := runTest(t, []string{"dialect", "check", "-", "--pack", file, "--format", "json"}, raw)
		if stderr != "" {
			t.Fatal(stderr)
		}
		var cliReport map[string]any
		if err = json.Unmarshal([]byte(stdout), &cliReport); err != nil {
			t.Fatal(err, stdout)
		}
		mcpReport, isError := dialectMCPResult(t, engine, "dialect_check", map[string]any{"document": raw, "pack": string(pack)})
		delete(cliReport, "command")
		delete(mcpReport, "command")
		if isError != (code != 0) || !reflect.DeepEqual(cliReport, mcpReport) {
			t.Fatal("check transport parity", code, isError, stdout, mcpReport)
		}
	}
	code, stdout, stderr := runTest(t, []string{"dialect", "check", "-", "--pack", file}, `{"version":2,"case":{"facts":[],"evidence":[]}}`)
	if code != 1 || stderr != "" || !strings.Contains(stdout, "missing-producer") || !strings.Contains(stdout, "Declared mapping coverage: incomplete") {
		t.Fatal(code, stdout, stderr)
	}
	for _, args := range [][]string{{"dialect", "check", "-"}, {"dialect", "check", "-", "--pack", "-"}} {
		code, _, _ := runTest(t, args, "")
		if code != 3 {
			t.Fatal(args, code)
		}
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(pack, after) {
		t.Fatal("check modified the pack", err)
	}
}
