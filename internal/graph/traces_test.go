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

// The trace on a comparison is the evaluation's own, untransformed: byte for
// byte what the graph evaluate composite reports for the same node under the
// same inputs. An empty or reshaped substitute is not a trace of this run.
func TestComparedNodeTraceIsTheEvaluationsOwn(t *testing.T) {
	loaded := fixtureProject(t)
	graphBytes, err := os.ReadFile(filepath.Join("testdata", "project", "onboarding.graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	document, loadFailure := Load(graphBytes, "onboarding.graph.json")
	if loadFailure != nil {
		t.Fatal(loadFailure.Message)
	}
	inputs := []byte(`{"screening":{"facts":{"screening":{"matches":"0"}},"evidence":{"screening-record":"present"}}}`)
	engine := newEngine(t)
	evaluated, failure := Evaluate(loaded, engine, document, "onboarding.graph.json", inputs, true, Options{Command: "evaluate"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	want := ""
	for _, node := range evaluated.Nodes {
		if node.Node == "screening" {
			if len(node.Trace) == 0 {
				t.Fatalf("the fixture screening evaluation carries a nonempty trace: %+v", node)
			}
			want = rawJSON(t, node.Trace)
		}
	}

	tested, failure := TestProject(loaded, engine, "", Options{Command: "test", IncludeTraces: true})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	found := false
	for _, row := range tested.Graphs[0].Rows {
		if row.ID != "clear-approves" {
			continue
		}
		if len(row.Nodes) != 1 || row.Nodes[0].Trace == nil || rawJSON(t, *row.Nodes[0].Trace) != want {
			t.Fatalf("the comparison carries the evaluation's own trace:\n got  %s\n want %s", rawJSON(t, row.Nodes[0].Trace), want)
		}
		found = true
	}
	if !found {
		t.Fatal("the clear-approves row was not reported")
	}

	// A row comparing two nodes carries two different traces, each the right
	// node's own — the same equality, per node, in one row.
	unresolvedInputs := []byte(`{"screening":{"facts":{},"evidence":{"screening-record":"present"}}}`)
	unresolvedRun, failure := Evaluate(loaded, engine, document, "onboarding.graph.json", unresolvedInputs, true, Options{Command: "evaluate"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	wantByNode := map[string]string{}
	for _, node := range unresolvedRun.Nodes {
		wantByNode[node.Node] = rawJSON(t, node.Trace)
	}
	for _, row := range tested.Graphs[0].Rows {
		if row.ID != "unresolved-screening-escalates" {
			continue
		}
		if len(row.Nodes) != 2 {
			t.Fatalf("the row names two nodes: %+v", row.Nodes)
		}
		for _, comparison := range row.Nodes {
			if comparison.Trace == nil || rawJSON(t, *comparison.Trace) != wantByNode[comparison.Node] {
				t.Fatalf("node %q carries its own evaluation's trace", comparison.Node)
			}
		}
		if rawJSON(t, *row.Nodes[0].Trace) == rawJSON(t, *row.Nodes[1].Trace) {
			t.Fatal("two different nodes' traces must differ in this fixture")
		}
		return
	}
	t.Fatal("the unresolved-screening-escalates row was not reported")
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

// Traces are charged by the report budget as retained bytes, on both paths: a
// budget the bare run fits exactly is a budget the traced run overruns. The
// bare spend equals the full envelope's own bytes (the reconcile makes it so),
// which pins the boundary without a magic number — and makes this test fail if
// a change ever excludes traces from the metering or defers them past it.
func TestTracesAreChargedByTheReportBudget(t *testing.T) {
	loaded := fixtureProject(t)
	engine := newEngine(t)
	graphBytes, err := os.ReadFile(filepath.Join("testdata", "project", "onboarding.graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	document, loadFailure := Load(graphBytes, "onboarding.graph.json")
	if loadFailure != nil {
		t.Fatal(loadFailure.Message)
	}
	rowsBytes, err := os.ReadFile(filepath.Join("testdata", "project", "onboarding.rows.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, rowsFailure := LoadRows(rowsBytes, "onboarding.rows.json")
	if rowsFailure != nil {
		t.Fatal(rowsFailure.Message)
	}

	// Direct path: the bare spend is exactly the envelope's bytes.
	bare, failure := Test(loaded, engine, document, "onboarding.graph.json", "onboarding.rows.json", rows, Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	exact := len(rawJSON(t, bare))
	if _, failure = Test(loaded, engine, document, "onboarding.graph.json", "onboarding.rows.json", rows, Options{Command: "test", ReportBudget: exact}); failure != nil {
		t.Fatalf("the bare run fits its own bytes exactly: %s", failure.Message)
	}
	_, failure = Test(loaded, engine, document, "onboarding.graph.json", "onboarding.rows.json", rows, Options{Command: "test", ReportBudget: exact, IncludeTraces: true})
	if failure == nil || failure.Code != CodeReportBudget {
		t.Fatalf("the traced run overruns the bare boundary, by the budget refusal: %+v", failure)
	}

	// Suite path: the bare spend is exactly the entry's bytes.
	bareSuite, failure := TestProject(loaded, engine, "", Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	entryExact := len(rawJSON(t, bareSuite.Graphs[0]))
	if _, failure = TestProject(loaded, engine, "", Options{Command: "test", ReportBudget: entryExact}); failure != nil {
		t.Fatalf("the bare walk fits its entry's bytes exactly: %s", failure.Message)
	}
	_, failure = TestProject(loaded, engine, "", Options{Command: "test", ReportBudget: entryExact, IncludeTraces: true})
	if failure == nil || failure.Code != CodeReportBudget {
		t.Fatalf("the traced walk overruns the bare boundary, by the budget refusal: %+v", failure)
	}
}

// A row whose composite headline mismatches ends before any node comparison
// exists: expectedNodes or not, asked or not, it reports no comparisons and so
// no traces — ADR-0031's stated limit, pinned.
func TestHeadlineMismatchReportsNoComparisonsAndNoTraces(t *testing.T) {
	files := fixtureFiles(t)
	files["onboarding.rows.json"] = strings.Replace(files["onboarding.rows.json"],
		`"expectedDisposition": { "kind": "outcome", "outcomeId": "approve", "reasons": [], "handoff": { "state": "none" } }`,
		`"expectedDisposition": { "kind": "outcome", "outcomeId": "decline", "reasons": [], "handoff": { "state": "none" } }`, 1)
	loaded := writeProject(t, files)
	tested, failure := TestProject(loaded, newEngine(t), "", Options{Command: "test", IncludeTraces: true})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	for _, row := range tested.Graphs[0].Rows {
		if row.ID != "clear-approves" {
			continue
		}
		if row.Status != "mismatch" || len(row.Nodes) != 0 || strings.Contains(rawJSON(t, row), `"trace"`) {
			t.Fatalf("a headline mismatch ends the row before comparisons: %s", rawJSON(t, row))
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
