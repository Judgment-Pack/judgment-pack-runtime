package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// The CLI fixture of registry_test.go, in YAML.
const cliKindYAML = `kindFormatVersion: "1.0.0"
id: https://example.invalid/kinds/queue
version: 1.0.0
kind: queue
owner: platform
schema:
  type: object
  required: [retention]
  additionalProperties: false
  properties:
    retention: {type: integer, minimum: 1, maximum: 30}
overlayable: [/retention]
`

func cliRegistryYAML(t *testing.T) string {
	t.Helper()
	registryJSON := cliRegistry(t, "14")
	digest := registryJSON[strings.Index(registryJSON, "sha256:"):][:71]
	return `registryFormatVersion: "1.0.0"
id: https://example.invalid/registries/q
version: 1.0.0
environments: [dev, prod]
owners: [platform]
kinds:
- {kind: queue, id: "https://example.invalid/kinds/queue", version: 1.0.0, digest: "` + digest + `"}
entries:
- kind: queue
  id: orders
  owner: platform
  value: {retention: 3}
overlays:
- environment: prod
  patches:
  - {kind: queue, id: orders, set: {/retention: 14}}
`
}

func writeText(t *testing.T, dir, name, text string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRegistryImportThenValidate(t *testing.T) {
	dir := t.TempDir()
	kindYAML := writeText(t, dir, "queue.yaml", cliKindYAML)
	code, kindJSON, stderr := runTest(t, []string{"registry", "import", kindYAML}, "")
	if code != 0 || stderr != "" || !strings.HasSuffix(kindJSON, "}\n") || strings.Count(kindJSON, "\n") != 1 {
		t.Fatalf("import kind: %d %q %q", code, kindJSON, stderr)
	}
	kindPath := writeText(t, dir, "queue.json", kindJSON)
	registryPath := filepath.Join(dir, "registry.json")
	code, out, stderr := runTest(t, []string{"registry", "import", "-", "--output", registryPath}, cliRegistryYAML(t))
	if code != 0 || out != "" || stderr != "" {
		t.Fatalf("import registry: %d %q %q", code, out, stderr)
	}
	code, out, _ = runTest(t, []string{"registry", "validate", registryPath, "--kind", kindPath}, "")
	if code != 0 || !strings.Contains(out, `"valid":true`) || !strings.Contains(out, `"diagnostics":[]`) {
		t.Fatalf("validate the imported registry: %d %q", code, out)
	}
	code, out, _ = runTest(t, []string{"registry", "resolve", registryPath, "--kind", kindPath, "--environment", "prod"}, "")
	if code != 0 || !strings.Contains(out, `"value":{"retention":14}`) {
		t.Fatalf("resolve the imported registry: %d %q", code, out)
	}
}

func TestRegistryImportRefusalWritesNothing(t *testing.T) {
	dir := t.TempDir()
	refused := writeText(t, dir, "refused.yaml", "kindFormatVersion: \"1.0.0\"\nowner: on\nsecret: &a example-password-value\n")
	existing := writeText(t, dir, "existing.json", "kept\n")
	absent := filepath.Join(dir, "absent.json")
	for _, output := range []string{existing, absent} {
		code, out, _ := runTest(t, []string{"registry", "import", refused, "--output", output}, "")
		if code != 1 || !strings.Contains(out, `"valid":false`) || !strings.Contains(out, `"code":"REG-YAML-SCALAR","severity":"error","location":{"document":"yaml","pointer":"","line":2,"column":8}`) ||
			!strings.Contains(out, `"REG-YAML-ANCHOR"`) || strings.Contains(out, "example-password-value") {
			t.Fatalf("want the value-free report and exit 1, got %d %q", code, out)
		}
	}
	if data, err := os.ReadFile(existing); err != nil || string(data) != "kept\n" {
		t.Fatalf("the existing output changed: %q %v", data, err)
	}
	if _, err := os.Lstat(absent); !os.IsNotExist(err) {
		t.Fatalf("a refused import created its output: %v", err)
	}
}

func TestRegistryImportInvocationAndIO(t *testing.T) {
	dir := t.TempDir()
	valid := writeText(t, dir, "queue.yaml", cliKindYAML)
	target := writeText(t, dir, "target.json", "kept\n")
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		args []string
		code int
	}{
		{[]string{"https://example.invalid/registry.yaml"}, 3},
		{[]string{valid, "--output", "https://example.invalid/out.json"}, 3},
		{[]string{filepath.Join(dir, "missing.yaml")}, 4},
		{[]string{valid, "--output", filepath.Join(dir, "no-such-directory", "out.json")}, 4},
		{[]string{valid, "--output", link}, 4},
	} {
		code, _, _ := runTest(t, append([]string{"registry", "import"}, row.args...), "")
		if code != row.code {
			t.Fatalf("%v: want exit %d, got %d", row.args, row.code, code)
		}
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "kept\n" {
		t.Fatalf("written through a symbolic link: %q %v", data, err)
	}
	code, out, _ := runTest(t, []string{"registry", "import", "-"}, strings.Repeat("#", registry.MaxImportBytes+1))
	if code != 1 || !strings.Contains(out, `"REG-DOC-SIZE"`) || !strings.Contains(out, `"document":"yaml"`) {
		t.Fatalf("oversize: want REG-DOC-SIZE and exit 1, got %d %q", code, out)
	}
}
