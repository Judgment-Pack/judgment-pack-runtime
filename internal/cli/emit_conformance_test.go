package cli

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit/emittest"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// goldenVectors are the harness's golden vectors: a vector the request mode
// answers into gen/req, whose expected/ the harness wrote with --update, and
// a vector whose options the manifest's schema refuses.
var goldenVectors = filepath.Join("..", "emit", "testdata", "conformance", "vectors")

// conformanceRepo is a temporary repository holding this test binary and a
// manifest that runs it to answer each environment's request into gen/req,
// and a copy of the golden vectors.
func conformanceRepo(t *testing.T) (root, manifestPath, vectors string) {
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
		"emitterManifestVersion": "1.0.0", "id": "https://example.invalid/emitters/cli-conformance", "version": "1.0.0", "owner": "platform",
		"reads":           []any{map[string]any{"kind": "queue", "id": "https://example.invalid/kinds/queue", "versions": "^1.0.0"}},
		"requestVersions": []any{"1.0.0"}, "environments": "each",
		"command": []any{command, emittest.Switch, "request", "gen/req"}, "owns": []any{"gen/**"},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifestPath = filepath.Join(t.TempDir(), "emitter.json")
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	vectors = filepath.Join(t.TempDir(), "vectors")
	err = filepath.WalkDir(goldenVectors, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(goldenVectors, path)
		target := filepath.Join(vectors, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root, manifestPath, vectors
}

func TestEmitConformanceGoldenVectors(t *testing.T) {
	root, manifestPath, vectors := conformanceRepo(t)
	args := []string{"emit", "conformance", "--manifest", manifestPath, "--vectors", vectors, "--root", root}

	code, out, stderr := runTest(t, args, "")
	if code != 0 || stderr != "" || !strings.HasSuffix(out, "}\n") {
		t.Fatalf("golden: %d %q %q", code, out, stderr)
	}
	var report struct {
		HarnessReportVersion string `json:"harnessReportVersion"`
		HarnessLevel         string `json:"harnessLevel"`
		Outcome              string `json:"outcome"`
		Rows                 []struct {
			Vector string `json:"vector"`
			Check  int    `json:"check"`
			Status string `json:"status"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if report.HarnessReportVersion != "1.0.0" || report.HarnessLevel != "1.0" || report.Outcome != "pass" {
		t.Fatalf("report: %s", out)
	}
	statuses := map[string]string{}
	for _, row := range report.Rows {
		statuses[row.Vector+"/"+string(rune('0'+row.Check))] = row.Status
	}
	for key, want := range map[string]string{
		"orders/1": "pass", "orders/2": "pass", "orders/3": "pass", "orders/4": "pass", "orders/5": "pass", "orders/6": "pass",
		"options-refused/1": "pass", "options-refused/2": "skip", "hostile/path-traversal/3": "pass",
	} {
		if statuses[key] != want {
			t.Errorf("%s: %q, want %q", key, statuses[key], want)
		}
	}
	// The report is its own RFC 8785 form.
	var value any
	decoder := json.NewDecoder(strings.NewReader(out))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	if canonical, err := jcs.EncodeDocument(value); err != nil || string(canonical)+"\n" != out {
		t.Fatalf("not canonical: %v", err)
	}
	if strings.Contains(out, "orders-queue") || strings.Contains(out, "retention") {
		t.Fatalf("the report holds a request value: %s", out)
	}

	// A golden edited by hand fails check 1; --update writes it again from
	// the emitter, and the vectors pass again.
	edited := filepath.Join(vectors, "orders", "expected", "gen", "req", "dev", "request.json")
	golden, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(edited, []byte("edited-by-hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ = runTest(t, args, "")
	if code != 1 || !strings.Contains(out, `{"difference":"changed","path":"gen/req/dev/request.json"}`) || strings.Contains(out, "edited-by-hand") {
		t.Fatalf("an edited golden: %d %q", code, out)
	}
	if code, out, _ = runTest(t, append(args, "--update"), ""); code != 0 {
		t.Fatalf("update: %d %q", code, out)
	}
	if data, err := os.ReadFile(edited); err != nil || string(data) != string(golden) {
		t.Fatalf("update wrote: %q %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(vectors, "options-refused", "expected")); !os.IsNotExist(err) {
		t.Fatalf("update wrote an expected-failure vector: %v", err)
	}

	// A level the harness does not know: it could not run.
	if err := os.WriteFile(filepath.Join(vectors, "harness.json"), []byte(`{"harnessLevel":"9.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, _ = runTest(t, args, ""); code != 2 || !strings.Contains(out, `"code":"EMIT-VECTOR"`) || !strings.Contains(out, `"outcome":"not-run"`) {
		t.Fatalf("unknown level: %d %q", code, out)
	}
}

func TestEmitConformanceRefusalsAndInvocation(t *testing.T) {
	root, manifestPath, vectors := conformanceRepo(t)
	refusedManifest := filepath.Join(t.TempDir(), "refused.json")
	if err := os.WriteFile(refusedManifest, []byte(`{"emitterManifestVersion":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		args []string
		code int
		want string
	}{
		"no manifest":        {[]string{"--vectors", vectors, "--root", root}, 3, "JPS-INVOCATION-INPUT"},
		"no vectors":         {[]string{"--manifest", manifestPath, "--root", root}, 3, "JPS-INVOCATION-INPUT"},
		"vectors from stdin": {[]string{"--manifest", manifestPath, "--vectors", "-", "--root", root}, 3, "JPS-INVOCATION-INPUT"},
		"a root that is not": {[]string{"--manifest", manifestPath, "--vectors", vectors, "--root", filepath.Join(root, "missing")}, 3, "JPS-INVOCATION-INPUT"},
		"a zero timeout":     {[]string{"--manifest", manifestPath, "--vectors", vectors, "--root", root, "--timeout", "0s"}, 3, "JPS-INVOCATION-ARGUMENTS"},
		"a refused manifest": {[]string{"--manifest", refusedManifest, "--vectors", vectors, "--root", root}, 2, `"code":"EMIT-MANIFEST"`},
		"a missing manifest": {[]string{"--manifest", filepath.Join(root, "missing.json"), "--vectors", vectors, "--root", root}, 2, `"code":"EMIT-PATH-UNREADABLE"`},
		"missing vectors":    {[]string{"--manifest", manifestPath, "--vectors", filepath.Join(root, "missing"), "--root", root}, 2, `"code":"EMIT-PATH-UNREADABLE"`},
	} {
		t.Run(name, func(t *testing.T) {
			code, out, _ := runTest(t, append([]string{"emit", "conformance"}, c.args...), "")
			if code != c.code || !strings.Contains(out, c.want) {
				t.Fatalf("%d %q", code, out)
			}
		})
	}
}
