package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// A synthetic registry: one kind, two environments, one overlay.
const cliKind = `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/queue","version":"1.0.0","kind":"queue","owner":"platform",
"schema":{"type":"object","required":["retention"],"additionalProperties":false,"properties":{"retention":{"type":"integer","minimum":1,"maximum":30}}},
"overlayable":["/retention"]}`

func cliRegistry(t *testing.T, retention string) string {
	t.Helper()
	var kind any
	decoder := json.NewDecoder(strings.NewReader(cliKind))
	decoder.UseNumber()
	if err := decoder.Decode(&kind); err != nil {
		t.Fatal(err)
	}
	digest, err := registry.Digest(kind)
	if err != nil {
		t.Fatal(err)
	}
	return `{"registryFormatVersion":"1.0.0","id":"https://example.invalid/registries/q","version":"1.0.0",
"environments":["dev","prod"],"owners":["platform"],
"kinds":[{"kind":"queue","id":"https://example.invalid/kinds/queue","version":"1.0.0","digest":"` + digest + `"}],
"entries":[{"kind":"queue","id":"orders","owner":"platform","value":{"retention":3}}],
"overlays":[{"environment":"prod","patches":[{"kind":"queue","id":"orders","set":{"/retention":` + retention + `}}]}]}`
}

func writeRegistryFiles(t *testing.T, registryText string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	registryPath, kindPath := filepath.Join(dir, "registry.json"), filepath.Join(dir, "queue.json")
	for path, text := range map[string]string{registryPath: registryText, kindPath: cliKind} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return registryPath, kindPath
}

func TestRegistryValidateAndResolve(t *testing.T) {
	registryPath, kindPath := writeRegistryFiles(t, cliRegistry(t, "14"))
	code, out, stderr := runTest(t, []string{"registry", "validate", registryPath, "--kind", kindPath}, "")
	if code != 0 || stderr != "" || !strings.HasPrefix(out, `{"reportFormatVersion":"1.0.0","valid":true,`) || !strings.Contains(out, `"diagnostics":[]`) {
		t.Fatalf("validate: %d %q %q", code, out, stderr)
	}
	code, out, stderr = runTest(t, []string{"registry", "resolve", registryPath, "--kind", kindPath, "--environment", "prod"}, "")
	if code != 0 || stderr != "" || !strings.HasSuffix(out, "\n") || strings.Count(out, "\n") != 1 || !strings.Contains(out, `"value":{"retention":14}`) {
		t.Fatalf("resolve: %d %q %q", code, out, stderr)
	}
	// The registry from standard input gives the same bytes.
	again, stdinOut, _ := runTest(t, []string{"registry", "resolve", "-", "--kind", kindPath, "--environment", "prod"}, cliRegistry(t, "14"))
	if again != 0 || stdinOut != out {
		t.Fatalf("stdin resolve differs: %d %q", again, stdinOut)
	}
}

func TestRegistryInvalidAndInvocation(t *testing.T) {
	registryPath, kindPath := writeRegistryFiles(t, cliRegistry(t, "90"))
	for _, verb := range [][]string{{"validate"}, {"resolve", "--environment", "dev"}} {
		args := append([]string{"registry", verb[0], registryPath, "--kind", kindPath}, verb[1:]...)
		code, out, _ := runTest(t, args, "")
		if code != 1 || !strings.Contains(out, `"valid":false`) || !strings.Contains(out, `"REG-VALUE-RANGE"`) || strings.Contains(out, ":90") {
			t.Fatalf("%s: want the value-free report and exit 1, got %d %q", verb[0], code, out)
		}
	}
	validPath, _ := writeRegistryFiles(t, cliRegistry(t, "14"))
	for _, row := range []struct {
		args []string
		code int
	}{
		{[]string{"resolve", validPath, "--kind", kindPath}, 3},
		{[]string{"resolve", validPath, "--kind", kindPath, "--environment", "qa"}, 3},
		{[]string{"validate", "-", "--kind", "-"}, 3},
		{[]string{"validate", "https://example.invalid/registry.json", "--kind", kindPath}, 3},
		{[]string{"validate", filepath.Join(t.TempDir(), "missing.json"), "--kind", kindPath}, 4},
	} {
		code, _, _ := runTest(t, append([]string{"registry"}, row.args...), "")
		if code != row.code {
			t.Fatalf("%v: want exit %d, got %d", row.args, row.code, code)
		}
	}
}

func TestRegistryOversizeIsAFinding(t *testing.T) {
	_, kindPath := writeRegistryFiles(t, "{}")
	code, out, _ := runTest(t, []string{"registry", "validate", "-", "--kind", kindPath}, strings.Repeat(" ", registry.MaxRegistryBytes+1))
	if code != 1 || !strings.Contains(out, `"REG-DOC-SIZE"`) || !strings.Contains(out, `"document":"registry"`) {
		t.Fatalf("want REG-DOC-SIZE and exit 1, got %d %q", code, out)
	}
}

// An oversize kind file is one finding, and the rest of the registry is still
// validated: its pin is reported missing beside it.
func TestRegistryOversizeKindIsOneFindingAmongOthers(t *testing.T) {
	registryPath, kindPath := writeRegistryFiles(t, cliRegistry(t, "14"))
	if err := os.WriteFile(kindPath, []byte(strings.Repeat(" ", registry.MaxKindBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runTest(t, []string{"registry", "validate", registryPath, "--kind", kindPath}, "")
	if code != 1 || !strings.Contains(out, `"REG-DOC-SIZE"`) || !strings.Contains(out, `"document":"kind"`) || !strings.Contains(out, `"REG-PIN-MISSING"`) {
		t.Fatalf("want REG-DOC-SIZE and REG-PIN-MISSING with exit 1, got %d %q", code, out)
	}
}
