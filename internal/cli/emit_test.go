package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit/emittest"
)

// TestMain makes this test binary the emitter of the emit tests when it is
// run as one, linked into a temporary repository; no test runs a program
// found by name.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == emittest.Switch {
		os.Exit(emittest.Main(os.Args[2:]))
	}
	os.Exit(m.Run())
}

// emitRepo is a temporary repository holding this test binary and a
// manifest that runs it to answer each environment's request into gen/req.
func emitRepo(t *testing.T) (root, manifestPath string) {
	t.Helper()
	root = t.TempDir()
	command := "bin/emitter"
	if runtime.GOOS == "windows" {
		command += ".exe"
	}
	if err := emittest.Link(root, command); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(map[string]any{
		"emitterManifestVersion": "1.0.0", "id": "https://example.invalid/emitters/cli-test", "version": "1.0.0", "owner": "platform",
		"reads":           []any{map[string]any{"kind": "queue", "id": "https://example.invalid/kinds/queue", "versions": "^1.0.0"}},
		"requestVersions": []any{"1.0.0"}, "environments": "each",
		"command": []any{command, emittest.Switch, "request", "gen/req"}, "owns": []any{"gen/**"},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifestPath = filepath.Join(root, "tools", "emitter.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, manifestPath
}

func TestEmitWritesThenChecks(t *testing.T) {
	registryPath, kindPath := writeRegistryFiles(t, cliRegistry(t, "14"))
	root, manifestPath := emitRepo(t)
	args := []string{"emit", "--manifest", manifestPath, "--registry", registryPath, "--kinds", kindPath, "--root", root}

	code, out, stderr := runTest(t, args, "")
	if code != 0 || stderr != "" || !strings.Contains(out, `"outcome":"written"`) || !strings.Contains(out, `"gen/req/prod/request.json"`) {
		t.Fatalf("write: %d %q %q", code, out, stderr)
	}
	if code, out, _ = runTest(t, append(args, "--check"), ""); code != 0 || !strings.Contains(out, `"outcome":"identical"`) {
		t.Fatalf("check after write: %d %q", code, out)
	}
	if err := os.WriteFile(filepath.Join(root, "gen", "req", "dev", "request.json"), []byte("edited-by-hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ = runTest(t, append(args, "--check"), "")
	if code != 1 || !strings.Contains(out, `{"path":"gen/req/dev/request.json","difference":"changed"}`) || strings.Contains(out, "edited-by-hand") || strings.Contains(out, "retention") {
		t.Fatalf("check after an edit: %d %q", code, out)
	}
	if text, _ := os.ReadFile(filepath.Join(root, "gen", "req", "dev", "request.json")); string(text) != "edited-by-hand\n" {
		t.Fatalf("check wrote: %q", text)
	}
}

func TestEmitRefusalsAndInvocation(t *testing.T) {
	registryPath, kindPath := writeRegistryFiles(t, cliRegistry(t, "14"))
	root, manifestPath := emitRepo(t)
	dir := t.TempDir()
	bad, empty, oversized, bigOptions := filepath.Join(dir, "bad.json"), filepath.Join(dir, "empty.json"), filepath.Join(dir, "oversized.json"), filepath.Join(dir, "options.json")
	for path, text := range map[string]string{
		bad: `{"emitterManifestVersion":"1.0.0"}`, empty: "",
		oversized:  `{"description":"` + strings.Repeat("x", emit.MaxManifestBytes) + `"}`,
		bigOptions: `{"schemaName":"` + strings.Repeat("x", emit.MaxOptionsBytes) + `"}`,
	} {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inputs := []string{"--registry", registryPath, "--kinds", kindPath, "--root", root}
	for _, row := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"--manifest", bad, "--registry", registryPath, "--kinds", kindPath, "--root", root}, 2, `"code":"EMIT-MANIFEST"`},
		{[]string{"--manifest", filepath.Join(root, "missing.json"), "--registry", registryPath, "--kinds", kindPath, "--root", root}, 2, `"code":"EMIT-PATH-UNREADABLE"`},
		{append([]string{"--manifest", empty}, inputs...), 2, `"code":"EMIT-MANIFEST"`},
		{append([]string{"--manifest", oversized}, inputs...), 2, `"code":"EMIT-MANIFEST"`},
		{append([]string{"--manifest", manifestPath, "--options", bigOptions}, inputs...), 2, `"code":"EMIT-OPTIONS"`},
		{append([]string{"--manifest", manifestPath, "--options", filepath.Join(dir, "absent.json")}, inputs...), 2, `"code":"EMIT-PATH-UNREADABLE"`},
		{append([]string{"--manifest", manifestPath, "--options", empty}, inputs...), 2, `"code":"EMIT-OPTIONS"`},
		{[]string{"--manifest", manifestPath, "--registry", registryPath, "--kinds", kindPath, "--root", root, "--other-manifest", manifestPath}, 2, `"code":"EMIT-OWNS-OVERLAP"`},
		{[]string{"--registry", registryPath, "--kinds", kindPath}, 3, ""},
		{[]string{"--manifest", manifestPath, "--registry", registryPath}, 3, ""},
		{[]string{"--manifest", manifestPath, "--registry", registryPath, "--kinds", kindPath, "--timeout", "0s"}, 3, ""},
		{[]string{"--manifest", manifestPath, "--registry", registryPath, "--kinds", kindPath, "--no-such-flag"}, 3, ""},
		{[]string{"--manifest", manifestPath, "--facts-contract", registryPath, "--root", root}, 3, ""},
		{[]string{"--manifest", manifestPath, "--registry", registryPath, "--kinds", kindPath, "--root", filepath.Join(root, "absent")}, 3, ""},
	} {
		code, out, _ := runTest(t, append([]string{"emit"}, row.args...), "")
		if code != row.code || !strings.Contains(out, row.want) {
			t.Errorf("%v: want exit %d with %s, got %d %q", row.args, row.code, row.want, code, out)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "gen")); !os.IsNotExist(err) {
		t.Fatalf("a refused run wrote: %v", err)
	}
}

// An emitter diagnostic whose location names nothing in the request refuses
// the run, and none of its text reaches standard output or standard error,
// even with the emitter's standard error passed through.
func TestEmitDoesNotCopyALocationTheRequestDoesNotHold(t *testing.T) {
	registryPath, kindPath := writeRegistryFiles(t, cliRegistry(t, "14"))
	root, _ := emitRepo(t)
	const freeText = "/vault-ref=projects~1acme~1secrets~1db-password"
	response := `{"emitResponseVersion":"1.0.0","files":[],"diagnostics":[{"severity":"warning","code":"x-a",` +
		`"location":{"document":"value","entry":{"kind":"queue","id":"not-in-the-request"},"environment":"prod","pointer":"` + freeText + `"}}]}`
	command := "bin/emitter"
	if runtime.GOOS == "windows" {
		command += ".exe"
	}
	manifest, err := json.Marshal(map[string]any{
		"emitterManifestVersion": "1.0.0", "id": "https://example.invalid/emitters/cli-test", "version": "1.0.0", "owner": "platform",
		"reads":           []any{map[string]any{"kind": "queue", "id": "https://example.invalid/kinds/queue", "versions": "^1.0.0"}},
		"requestVersions": []any{"1.0.0"}, "environments": "all",
		"command": []any{command, emittest.Switch, "echo", "fixtures/response.json"}, "owns": []any{"gen/**"},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "tools", "echo.json")
	for path, data := range map[string][]byte{manifestPath: manifest, filepath.Join(root, "fixtures", "response.json"): []byte(response)} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, extra := range [][]string{nil, {"--check"}, {"--emitter-stderr"}} {
		args := append([]string{"emit", "--manifest", manifestPath, "--registry", registryPath, "--kinds", kindPath, "--root", root}, extra...)
		code, out, stderr := runTest(t, args, "")
		if code != 2 || !strings.Contains(out, `"code":"EMIT-RESPONSE"`) || !strings.Contains(out, `"pointer":"/diagnostics/0/location"`) {
			t.Fatalf("%v: %d %q", extra, code, out)
		}
		for _, text := range []string{"vault-ref", "acme", "db-password", "not-in-the-request"} {
			if strings.Contains(out, text) || strings.Contains(stderr, text) {
				t.Fatalf("%v: %q reached the output: %q %q", extra, text, out, stderr)
			}
		}
	}
}
