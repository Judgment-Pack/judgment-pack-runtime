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
	// The emitter here is a Go test binary, whose runtime installs its own
	// SIGPIPE handler at start whatever it inherited, so the line above
	// cannot tell. What can: jpack ran in this process, and a disposition
	// this process holds ignored is what an exec'd emitter inherits.
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range strings.Split(string(status), "\n") {
		if hex, ok := strings.CutPrefix(field, "SigIgn:"); ok {
			own, err := strconv.ParseUint(strings.TrimSpace(hex), 16, 64)
			if err != nil || own&(1<<(sigpipe-1)) != 0 {
				t.Fatalf("jpack holds SIGPIPE ignored, which an emitter inherits (SigIgn %s): %v", strings.TrimSpace(hex), err)
			}
			return
		}
	}
	t.Fatal("no SigIgn line in /proc/self/status")
}
