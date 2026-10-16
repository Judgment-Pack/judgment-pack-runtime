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

func TestDialectCLIAndMCPParity(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"version":2,"case":{"facts":[],"evidence":[]}}`, `{"version":3,"x":-0}`, `{"version":3,"sources":[]}`, `{"version":3,"provider":""}`, `{"version":3,"case":{"parameters":{}}}`, `{"version":3,"admits":{"facts":{}}}`, `{"version":2,"n":1.5}`, `{"version":2,"version":3}`, `{"version":4}`, strings.Repeat(" ", dialect.MaxDocumentBytes+1)} {
		for _, operation := range []string{"canonicalize", "digest", "validate"} {
			code, stdout, stderr := runTest(t, []string{"dialect", operation, "-", "--format", "json"}, raw)
			if stderr != "" {
				t.Fatal(stderr)
			}
			var cli map[string]any
			if err = json.Unmarshal([]byte(stdout), &cli); err != nil {
				t.Fatal(stdout, err)
			}
			request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "dialect_" + operation, "arguments": map[string]any{"document": raw}}})
			var out, logs bytes.Buffer
			server := mcp.NewServer(engine, conformance.NewRunner(engine))
			if err = server.Serve(bytes.NewReader(append(request, '\n')), &out, &logs); err != nil {
				t.Fatal(err)
			}
			var response struct {
				Result struct {
					Structured map[string]any `json:"structuredContent"`
					IsError    bool           `json:"isError"`
				} `json:"result"`
			}
			if err = json.Unmarshal(out.Bytes(), &response); err != nil {
				t.Fatal(err, out.String())
			}
			if response.Result.IsError != (code != 0) {
				t.Fatalf("CLI exit %d MCP isError %v", code, response.Result.IsError)
			}
			delete(cli, "command")
			delete(response.Result.Structured, "command")
			if !reflect.DeepEqual(cli, response.Result.Structured) {
				t.Fatalf("CLI/MCP mismatch\n%v\n%v", cli, response.Result.Structured)
			}
		}
	}
}
func TestDialectCLIArtifactsAndRefusals(t *testing.T) {
	code, raw, stderr := runTest(t, []string{"dialect", "init"}, "")
	if code != 0 || stderr != "" || strings.HasSuffix(raw, "\n") {
		t.Fatal(code, raw, stderr)
	}
	expected, _ := dialect.Init("test", 2)
	if raw != expected.Document {
		t.Fatal(raw)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "dialect.json")
	if err := os.WriteFile(file, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	code, digest, stderr := runTest(t, []string{"dialect", "digest", file}, "")
	if code != 0 || digest != expected.Digest+"\n" || stderr != "" {
		t.Fatal(code, digest, stderr)
	}
	after, _ := os.ReadFile(file)
	if string(after) != raw {
		t.Fatal("modified input")
	}
	code, encoded, stderr := runTest(t, []string{"dialect", "canonicalize", file, "--pretty"}, "")
	if code != 0 || encoded != raw || stderr != "" {
		t.Fatal("pretty changed canonical bytes", code, encoded, stderr)
	}
	for _, tc := range []struct {
		args  []string
		input string
		exit  int
	}{
		{[]string{"dialect", "init", "--dialect-version", "4", "--format", "json"}, "", 2},
		{[]string{"dialect", "digest", "-"}, `{"version":2,"x":1.0}`, 1},
		{[]string{"dialect", "digest", dir}, "", 4},
		{[]string{"dialect", "digest", "https://example.com/x"}, "", 4},
		{[]string{"dialect", "digest", "-", "--format", "json", "--unknown"}, "", 3},
	} {
		exit, stdout, stderr := runTest(t, tc.args, tc.input)
		if exit != tc.exit {
			t.Fatal(tc.args, exit, stdout, stderr)
		}
		if strings.Contains(strings.Join(tc.args, " "), "--format json") && !strings.Contains(stdout, `"command":"dialect `) {
			t.Fatal("error has wrong command", stdout)
		}
	}
	code, stdout, _ := runTest(t, []string{"dialect", "contract", "--format", "json"}, "")
	if code != 0 || !strings.Contains(stdout, `"validationVersions":[2,3]`) {
		t.Fatal(stdout)
	}
}

func TestDialectValidationHumanAndUnsupported(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"dialect", "validate", "-"}, `{"version":2,"case":{"facts":[],"evidence":[]}}`)
	if code != 0 || stderr != "" || stdout != "Version-2 document checks passed. Profiles, preparations and pack compatibility were not checked.\n" {
		t.Fatal(code, stdout, stderr)
	}
	code, stdout, stderr = runTest(t, []string{"dialect", "validate", "-"}, `{"version":2,"case":{"facts":[],"evidence":[]},"secret":"do-not-echo"}`)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "JPS-DIALECT-FORM") || strings.Contains(stderr, "do-not-echo") {
		t.Fatal(code, stdout, stderr)
	}
	code, stdout, stderr = runTest(t, []string{"dialect", "validate", "-", "--format", "json"}, `{"version":3,"case":{"facts":[],"evidence":[]},"sources":[{"name":"research","kind":"research"}]}`)
	if code != 2 || stderr != "" || !strings.Contains(stdout, "JPS-DIALECT-SOURCE-UNSUPPORTED") || strings.Contains(stdout, `"digest":`) {
		t.Fatal(code, stdout, stderr)
	}
}

func TestDialectVersionFlagUsesExactSpelling(t *testing.T) {
	for _, value := range []string{"0x3", "03", "0b11", "+3", "3e0", "3.0", " 3", "3 ", ""} {
		code, raw, _ := runTest(t, []string{"dialect", "init", "--dialect-version", value, "--format", "json"}, "")
		if code == 0 || strings.Contains(raw, `"document":`) || strings.Contains(raw, `"digest":`) {
			t.Fatalf("accepted %q: %d %s", value, code, raw)
		}
	}
}
