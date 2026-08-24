package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// comparisons flattens every node comparison the walk reported, so assertions
// range over exactly what a consumer receives.
func comparisons(t *testing.T, entry result.GraphSuiteEntry) []result.GraphTestNode {
	t.Helper()
	flattened := []result.GraphTestNode{}
	for _, row := range entry.Rows {
		flattened = append(flattened, row.Nodes...)
	}
	return flattened
}

// Asked, every compared node's comparison carries the evaluation's own trace
// (ADR-0031): present on the wire, [] at minimum, exactly ADR-0027's
// never-omitted rule carried over. Not asked, the member is absent
// everywhere — the previous payload, byte for byte.
func TestIncludeTracesCarriesEachComparedNodesTrace(t *testing.T) {
	loaded := fixtureProject(t)
	with, failure := TestProject(loaded, newEngine(t), "", Options{Command: "test", IncludeTraces: true})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	compared := comparisons(t, with.Graphs[0])
	if len(compared) != 3 {
		t.Fatalf("the fixture rows name three node comparisons: %+v", compared)
	}
	for _, comparison := range compared {
		if !strings.Contains(rawJSON(t, comparison), `"trace":[`) {
			t.Fatalf("a compared node carries its trace on the wire: %s", rawJSON(t, comparison))
		}
	}

	without, failure := TestProject(loaded, newEngine(t), "", Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if strings.Contains(rawJSON(t, without.Graphs[0]), `"trace"`) {
		t.Fatalf("not asked, no trace member anywhere: %s", rawJSON(t, without.Graphs[0]))
	}
}

// A mismatching comparison carries the trace most of all: "why" is what the
// ask is for, and the walk attaches it the moment the evaluation is in hand,
// before any comparison can end the row.
func TestMismatchedNodeComparisonStillCarriesTheTrace(t *testing.T) {
	files := fixtureFiles(t)
	files["onboarding.rows.json"] = strings.Replace(files["onboarding.rows.json"],
		`"outcomeId": "clear"`, `"outcomeId": "not-clear"`, 1)
	loaded := writeProject(t, files)
	tested, failure := TestProject(loaded, newEngine(t), "", Options{Command: "test", IncludeTraces: true})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	for _, row := range tested.Graphs[0].Rows {
		if row.ID != "clear-approves" {
			continue
		}
		if len(row.Nodes) != 1 || row.Nodes[0].Status != "mismatch" ||
			!strings.Contains(rawJSON(t, row.Nodes[0]), `"trace":[`) {
			t.Fatalf("the mismatching comparison keeps its trace: %s", rawJSON(t, row.Nodes[0]))
		}
		return
	}
	t.Fatal("the clear-approves row was not reported")
}

// A node the graph does not declare was never evaluated: its comparison is
// already a mismatch whose detail says why, and it has no trace to carry even
// when the run was asked.
func TestUnknownNodeComparisonCarriesNoTrace(t *testing.T) {
	files := fixtureFiles(t)
	files["onboarding.rows.json"] = strings.Replace(files["onboarding.rows.json"],
		`"expectedNodes": {`,
		`"expectedNodes": { "aaa-ghost": { "kind": "outcome", "outcomeId": "x", "reasons": [], "handoff": { "state": "none" } },`, 1)
	loaded := writeProject(t, files)
	tested, failure := TestProject(loaded, newEngine(t), "", Options{Command: "test", IncludeTraces: true})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	for _, row := range tested.Graphs[0].Rows {
		if row.ID != "clear-approves" {
			continue
		}
		if len(row.Nodes) != 1 || row.Nodes[0].Node != "aaa-ghost" || row.Nodes[0].Status != "mismatch" ||
			strings.Contains(rawJSON(t, row.Nodes[0]), `"trace"`) {
			t.Fatalf("an undeclared node's comparison has no trace: %s", rawJSON(t, row.Nodes[0]))
		}
		return
	}
	t.Fatal("the clear-approves row was not reported")
}

// fixtureFiles reads the whole fixture project into a map a test can perturb
// one file of.
func fixtureFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, name := range []string{"jpack.json", "onboarding.graph.json", "onboarding.rows.json", "sanctions-screening-0.1.0.pack.json", "vendor-onboarding-0.1.0.pack.json"} {
		data, err := os.ReadFile(filepath.Join("testdata", "project", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(data)
	}
	return files
}
