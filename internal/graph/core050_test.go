package graph

import (
	"os"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Graphs do not read Core 0.5.0: a node pack declaring it is refused at
// contract selection, before any node runs, with the surface refusal code and
// a message naming the surface and the version.
func TestCore050NodePackIsRefused(t *testing.T) {
	raw, err := os.ReadFile("../artifacts/jps/0.3.0/cases/valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, failure := selectNodeContract(raw, nil); failure != nil {
		t.Fatalf("released pack refused: %+v", failure)
	}
	pack, err := os.ReadFile("../artifacts/jps/0.5.0/cases/valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	_, failure := selectNodeContract(pack, nil)
	if failure == nil || failure.Code != "JPS-SURFACE-SPEC-VERSION-UNSUPPORTED" || failure.ExitCode != result.ExitUnsupported || failure.Message != `Graph evaluation does not read packs declaring specVersion "0.5.0"; it reads Core 0.2.0-draft and 0.3.0 packs.` {
		t.Fatalf("0.5.0 node pack: %+v", failure)
	}
}

// core050GraphProject lays out a project whose graph has a Core 0.3.0 node
// "a" and a Core 0.5.0 node "b", with declared rows that are never read.
// first replaces node a's pack; "" leaves it out of the tree, declared but
// unreadable.
func core050GraphProject(t *testing.T, first string) (*project.Project, Document) {
	t.Helper()
	files := map[string]string{
		"jpack.json": `{"configVersion":"2","packs":{"p":{"path":"p.json"},"q":{"path":"q.json"}},"graphs":{"routes":{"path":"graph.json","rows":"missing.rows.json"}}}`,
		"q.json":     coreGraphPack("0.5.0", graphRoutes, ""),
		"graph.json": `{"formatVersion":"1","id":"routes","version":"1.0.0","nodes":{"a":{"pack":"p"},"b":{"pack":"q"}},"edges":[],"result":"b"}`,
	}
	if first != "" {
		files["p.json"] = first
	}
	loaded := writeProject(t, files)
	doc, failure := Load([]byte(files["graph.json"]), "graph.json")
	if failure != nil {
		t.Fatal(failure)
	}
	return loaded, doc
}

// Every graph surface that reads node packs refuses a graph with a Core 0.5.0
// node whole, before anything else it would read or report: malformed
// inputs, an empty matrix, a rows file that does not exist, and a node whose
// pack cannot be read, named before it, all yield to it. Each message names
// the node, the pack and the surface, by hand.
func TestEveryGraphSurfaceRefusesACore050Node(t *testing.T) {
	const reads = ` does not read packs declaring specVersion "0.5.0"; it reads Core 0.2.0-draft and 0.3.0 packs.`
	for _, first := range []struct{ name, pack string }{
		{"beside a Core 0.3.0 node", coreGraphPack("0.3.0", graphRoutes, "")},
		{"after a node whose pack cannot be read", ""},
	} {
		t.Run(first.name, func(t *testing.T) {
			loaded, doc := core050GraphProject(t, first.pack)
			surfaces := []struct {
				surface string
				run     func() *evaluation.Failure
			}{
				{"Graph evaluation", func() *evaluation.Failure {
					_, failure := Evaluate(loaded, nil, doc, "graph.json", []byte(`{"a":`), true, Options{})
					return failure
				}},
				{"Graph testing", func() *evaluation.Failure {
					_, failure := Test(loaded, nil, doc, "graph.json", "rows.json", Rows{}, Options{})
					return failure
				}},
				{"Graph testing", func() *evaluation.Failure {
					_, failure := TestProject(loaded, nil, "", Options{})
					return failure
				}},
				{"Graph planning", func() *evaluation.Failure {
					_, failure := Plan(loaded, doc, "graph.json", "experimental graph explain")
					return failure
				}},
				{"Graph validation", func() *evaluation.Failure {
					_, failure := ValidateProject(loaded, "", "experimental graph validate")
					return failure
				}},
			}
			for index, surface := range surfaces {
				failure := surface.run()
				want := `Node "b" (pack "q"): ` + surface.surface + reads
				if failure == nil || failure.Code != "JPS-SURFACE-SPEC-VERSION-UNSUPPORTED" || failure.ExitCode != result.ExitUnsupported || failure.Message != want {
					t.Errorf("surface %d (%s): %+v", index, surface.surface, failure)
				}
			}
		})
	}
}

// A graph whose nodes are all released packs is not refused, and the scan
// reads nothing it reports.
func TestAReleasedGraphIsNotRefusedAsCore050(t *testing.T) {
	base := coreGraphPack("0.3.0", graphRoutes, "")
	loaded, doc := coreGraphProject(t, base, base)
	if refusal := Core050NodeRefusal(loaded, doc, "Graph evaluation"); refusal != nil {
		t.Fatalf("released graph refused: %+v", refusal)
	}
}
