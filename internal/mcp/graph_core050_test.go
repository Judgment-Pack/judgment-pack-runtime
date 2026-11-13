package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
)

// experimental_test_graphs refuses a declared graph with a Core 0.5.0 node
// whole, before it reads the graph's rows, which do not exist here, as the
// CLI walk does; the message names the node, the pack and the surface.
func TestExperimentalTestGraphsRefusesACore050Node(t *testing.T) {
	const pack = `{"specVersion":"%s","id":"https://example.com/packs/graph","version":"1.0.0","title":"Graph","decision":{"intent":"Review","question":"Review?"},"outcomes":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],"rules":[{"id":"one","description":"One","when":{"op":"literal","value":false},"outcome":"yes","onUnknown":"ignore"}]}`
	root := t.TempDir()
	for name, body := range map[string]string{
		project.DefaultConfigName: `{"configVersion":"2","packs":{"p":{"path":"p.json"},"q":{"path":"q.json"}},"graphs":{"routes":{"path":"graph.json","rows":"missing.rows.json"}}}`,
		"p.json":                  strings.Replace(pack, "%s", "0.3.0", 1),
		"q.json":                  strings.Replace(pack, "%s", "0.5.0", 1),
		"graph.json":              `{"formatVersion":"1","id":"routes","version":"1.0.0","nodes":{"a":{"pack":"p"},"b":{"pack":"q"}},"edges":[],"result":"b"}`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(project.ConfigEnv, filepath.Join(root, project.DefaultConfigName))
	outcome := runServer(t, toolCall(t, 1, "experimental_test_graphs", nil))[0]["result"].(map[string]any)
	const want = `Node "b" (pack "q"): Graph testing does not read packs declaring specVersion "0.5.0"; it reads Core 0.2.0-draft and 0.3.0 packs. Meanwhile, experimental evaluate and packs test read a Core 0.5.0 pack on its own, as its evaluation form; CONFORMANCE.md states what graphs wait for.`
	if outcome["isError"] != true || toolText(t, outcome) != want {
		t.Fatalf("outcome %#v", outcome)
	}
}
