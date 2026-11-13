package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Each path-named graph command refuses a graph with a Core 0.5.0 node before
// it reads anything else it would report on: graph evaluate before its inputs
// file and graph test before its rows file, both of which do not exist here,
// and graph validate and graph explain before checking or planning the graph.
// The message names the node, the pack and the surface.
func TestGraphCommandsRefuseACore050Node(t *testing.T) {
	const pack = `{"specVersion":"%s","id":"https://example.com/packs/graph","version":"1.0.0","title":"Graph","decision":{"intent":"Review","question":"Review?"},"outcomes":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],"rules":[{"id":"one","description":"One","when":{"op":"literal","value":false},"outcome":"yes","onUnknown":"ignore"}]}`
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{"p":{"path":"p.json"},"q":{"path":"q.json"}}}`, map[string]string{
		"p.json":     strings.Replace(pack, "%s", "0.3.0", 1),
		"q.json":     strings.Replace(pack, "%s", "0.5.0", 1),
		"graph.json": `{"formatVersion":"1","id":"routes","version":"1.0.0","nodes":{"a":{"pack":"p"},"b":{"pack":"q"}},"edges":[],"result":"b"}`,
	})
	root := filepath.Dir(configPath)
	graphPath := filepath.Join(root, "graph.json")
	missing := filepath.Join(root, "missing.json")
	const reads = ` does not read packs declaring specVersion "0.5.0"; it reads Core 0.2.0-draft and 0.3.0 packs. Meanwhile, experimental evaluate and packs test read a Core 0.5.0 pack on its own, as its evaluation form; CONFORMANCE.md states what graphs wait for.`
	for _, command := range []struct {
		args    []string
		surface string
	}{
		{[]string{"experimental", "graph", "validate", graphPath, "--config", configPath, "--format", "json"}, "Graph validation"},
		{[]string{"experimental", "graph", "explain", graphPath, "--config", configPath, "--format", "json"}, "Graph planning"},
		{[]string{"experimental", "graph", "test", graphPath, "--config", configPath, "--rows", missing, "--format", "json"}, "Graph testing"},
		{[]string{"experimental", "graph", "evaluate", graphPath, "--config", configPath, "--inputs", missing, "--format", "json"}, "Graph evaluation"},
	} {
		t.Run(command.args[2], func(t *testing.T) {
			code, stdout, stderr := runTest(t, command.args, "")
			var refusal struct {
				Status      string
				Diagnostics []struct{ Code, Message string }
			}
			if err := json.Unmarshal([]byte(stdout), &refusal); err != nil || code != result.ExitUnsupported || stderr != "" {
				t.Fatalf("exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
			}
			want := `Node "b" (pack "q"): ` + command.surface + reads
			if refusal.Status != "unsupported" || len(refusal.Diagnostics) != 1 || refusal.Diagnostics[0].Code != "JPS-SURFACE-SPEC-VERSION-UNSUPPORTED" || refusal.Diagnostics[0].Message != want {
				t.Fatalf("refusal %+v", refusal)
			}
		})
	}
}
