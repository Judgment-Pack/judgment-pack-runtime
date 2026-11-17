//go:build linux

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit/emittest"
)

// jpack catches SIGPIPE rather than ignoring it, so an emitter it starts
// begins with SIGPIPE at its default disposition: ignored dispositions are
// inherited across exec, caught ones are not.
func TestTheEmitterDoesNotInheritSIGPIPEIgnored(t *testing.T) {
	registryPath, kindPath := writeRegistryFiles(t, cliRegistry(t, "14"))
	root, manifestPath := emitRepo(t)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	command := manifest["command"].([]any)
	manifest["command"] = []any{command[0], emittest.Switch, "sigign", "gen/sigign.txt"}
	manifest["environments"] = "all"
	if data, err = json.Marshal(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := runTest(t, []string{"emit", "--manifest", manifestPath, "--registry", registryPath, "--kinds", kindPath, "--root", root}, "")
	if code != 0 {
		t.Fatalf("emit: %d %s %s", code, out, stderr)
	}
	line, err := os.ReadFile(filepath.Join(root, "gen", "sigign.txt"))
	if err != nil {
		t.Fatal(err)
	}
	mask, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(string(line), "SigIgn:")), 16, 64)
	if err != nil {
		t.Fatalf("%q: %v", line, err)
	}
	const sigpipe = 13
	if mask&(1<<(sigpipe-1)) != 0 {
		t.Fatalf("the emitter started with SIGPIPE ignored (SigIgn %s)", strings.TrimSpace(string(line)))
	}
}
