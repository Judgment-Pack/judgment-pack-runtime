package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestArtifactVersionTransportParity(t *testing.T) {
	project := filepath.Join(t.TempDir(), "missing-project.json")
	t.Setenv("JPACK_CONFIG", project)
	for _, tc := range []struct {
		base      string
		candidate *string
	}{
		{"0.1.0", nil}, {"9007199254740993.1.0", nil}, {"1.0.0", strptr("0.9.9")}, {"1.0.0", strptr("1.0.0")}, {"1.0.0", strptr("2.0.0")}, {"1.0.0", strptr("")}, {"1.0.0-dev", nil},
	} {
		args := []string{"artifact", "version", "--base", tc.base, "--format", "json"}
		input := map[string]any{"base": tc.base}
		if tc.candidate != nil {
			args = append(args, "--candidate", *tc.candidate)
			input["candidate"] = *tc.candidate
		}
		code, stdout, stderr := runTest(t, args, "")
		if stderr != "" {
			t.Fatal(code, stderr)
		}
		var expected map[string]any
		if err := json.Unmarshal([]byte(stdout), &expected); err != nil {
			t.Fatal(stdout, err)
		}
		request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "get_artifact_version_options", "arguments": input}})
		if err != nil {
			t.Fatal(err)
		}
		mcpCode, mcpOutput, _ := runTest(t, []string{"mcp"}, string(request)+"\n")
		var response struct {
			Result struct {
				Structured map[string]any `json:"structuredContent"`
				IsError    bool           `json:"isError"`
			} `json:"result"`
		}
		if mcpCode != 0 {
			t.Fatal(mcpCode, mcpOutput)
		}
		if err := json.Unmarshal([]byte(mcpOutput), &response); err != nil {
			t.Fatal(err, mcpOutput)
		}
		actual, failed := response.Result.Structured, response.Result.IsError
		delete(actual, "command")
		delete(expected, "command")
		if failed != (code != 0) || !reflect.DeepEqual(actual, expected) {
			t.Fatal("different transports", actual, expected)
		}
	}
	if _, err := os.Stat(project); !os.IsNotExist(err) {
		t.Fatal("project accessed", err)
	}
}
func strptr(v string) *string { return &v }
func TestArtifactVersionCLI(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"artifact", "version", "--base", "0.1.0"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Major: 1.0.0") || !strings.Contains(stdout, "Nothing has been saved") {
		t.Fatal(code, stdout, stderr)
	}
	for _, args := range [][]string{{"artifact", "version"}, {"artifact", "version", "--base", "1.0"}, {"artifact", "version", "--base", "1.0.0", "--write"}, {"artifact", "version", "--base", "1.0.0", "unexpected"}} {
		code, _, _ := runTest(t, args, "")
		if code == 0 {
			t.Fatal("accepted", args)
		}
	}
}
