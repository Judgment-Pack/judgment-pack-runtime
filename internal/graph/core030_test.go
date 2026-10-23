package graph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const graphRoutes = `{"triggers":["no-match"],"routes":[{"target":{"kind":"queue","name":"Z"}},{"target":{"kind":"system","name":"A"}},{"target":{"kind":"queue","name":"Z"}}]}`
const graphCanonical = `{"handoff":{"routes":[{"kind":"queue","name":"Z"},{"kind":"system","name":"A"},{"kind":"queue","name":"Z"}],"state":"requested","triggeredBy":["no-match"]},"kind":"unresolved","reasons":["no-match"]}`

func coreGraphPack(version, escalation, extra string) string {
	return `{"specVersion":"` + version + `","id":"https://example.com/packs/graph","version":"1.0.0","title":"Graph routes","decision":{"intent":"Review","question":"Review?"},"outcomes":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],"rules":[{"id":"one","description":"One","when":{"op":"literal","value":false},"outcome":"yes","onUnknown":"ignore"}],"escalation":` + escalation + extra + `}`
}

func coreGraphProject(t *testing.T, first, second string) (*project.Project, Document) {
	t.Helper()
	loaded := writeProject(t, map[string]string{
		"jpack.json": `{"configVersion":"1","packs":{"p":{"path":"p.json"},"q":{"path":"q.json"}}}`,
		"p.json":     first, "q.json": second,
	})
	doc, failure := Load([]byte(`{"formatVersion":"1","id":"routes","version":"1.0.0","nodes":{"a":{"pack":"p"},"b":{"pack":"q"}},"edges":[],"result":"b"}`), "graph.json")
	if failure != nil {
		t.Fatal(failure)
	}
	return loaded, doc
}

func coreGraphAudit(t *testing.T) (*audit.Writer, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := fssecure.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return audit.NewWriter(root, "audit", true), dir
}

func TestCore030GraphRoutesRowsAndProvenance(t *testing.T) {
	pack := coreGraphPack("0.3.0", graphRoutes, "")
	loaded, doc := coreGraphProject(t, pack, pack)
	engine := newEngine(t)
	got, failure := Evaluate(loaded, engine, doc, "graph.json", nil, false, Options{})
	if failure != nil {
		t.Fatal(failure)
	}
	if got.EvaluatorSpecVersion != "0.3.0" || got.HandoffTarget != nil || len(got.Handoffs) != 6 {
		t.Fatalf("graph: %+v", got)
	}
	for i, want := range []string{"Z", "A", "Z", "Z", "A", "Z"} {
		if got.Handoffs[i].Target.Name != want || got.Handoffs[i].Node != []string{"a", "a", "a", "b", "b", "b"}[i] {
			t.Fatalf("route order: %+v", got.Handoffs)
		}
	}
	bundle, err := artifacts.Load("0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.Artifact == nil || got.Artifact.SpecVersion != "0.3.0" || got.Artifact.BundleDigest != bundle.Lock().BundleDigest.Value {
		t.Fatalf("artifact: %+v", got.Artifact)
	}
	for _, node := range got.Nodes {
		canonical, err := node.Disposition.Canonical()
		if err != nil || string(canonical) != graphCanonical || node.SpecVersion != "0.3.0" || node.PackSHA256 != strings.TrimPrefix(audit.Digest([]byte(pack)), "sha256:") {
			t.Fatalf("node: %+v %s %v", node, canonical, err)
		}
	}
	rows := Rows{Cases: []RowCase{{ID: "r", Inputs: json.RawMessage(`{}`), ExpectedDisposition: json.RawMessage(graphCanonical), ExpectedNodes: map[string]json.RawMessage{"a": json.RawMessage(graphCanonical)}}}}
	tested, failure := Test(loaded, engine, doc, "graph.json", "rows.json", rows, Options{})
	if failure != nil || tested.Status != "passed" || tested.EvaluatorSpecVersion != "0.3.0" || len(tested.Rows[0].Nodes) != 1 {
		t.Fatalf("row: %+v %+v", tested, failure)
	}
	if tested.Rows[0].Expected != graphCanonical || tested.Rows[0].Actual != graphCanonical {
		t.Fatalf("canonical comparison: %+v", tested.Rows[0])
	}
	foundWitness := false
	for _, probe := range tested.Coverage {
		if strings.HasPrefix(probe.Probe, "node:a:") && probe.Status == result.MatrixProbeCovered {
			foundWitness = true
		}
	}
	if !foundWitness {
		t.Fatalf("Core 0.3 expectation did not witness coverage: %+v", tested.Coverage)
	}
}

func TestCore030GraphRefusesMixedContractsWithoutExecutionOrAudit(t *testing.T) {
	legacy := coreGraphPack(result.EvaluatorSpecVersion, `{"triggers":["no-match"],"target":{"kind":"queue","name":"Review"}}`, "")
	core := coreGraphPack("0.3.0", graphRoutes, "")
	loaded, doc := coreGraphProject(t, legacy, core)
	writer, dir := coreGraphAudit(t)
	calls := 0
	got, failure := Evaluate(loaded, newEngine(t), doc, "graph.json", nil, false, Options{Audit: writer, LawCheck: func(string, []byte) *evaluation.Failure { calls++; return nil }})
	if failure == nil || failure.Code != "JPS-GRAPH-MIXED-CONTRACT" || got.EvaluatorSpecVersion != "" || calls != 0 {
		t.Fatalf("mixed graph: %+v %+v calls=%d", got, failure, calls)
	}
	if _, err := os.Stat(filepath.Join(dir, "audit", audit.FileName)); !os.IsNotExist(err) {
		t.Fatalf("refusal wrote audit: %v", err)
	}
	_, failure = Test(loaded, newEngine(t), doc, "graph.json", "rows.json", Rows{Cases: []RowCase{{ID: "r", Inputs: json.RawMessage(`{}`), ExpectedDisposition: json.RawMessage(graphCanonical)}}}, Options{})
	if failure == nil || failure.Code != "JPS-GRAPH-MIXED-CONTRACT" {
		t.Fatalf("mixed test: %+v", failure)
	}
}

func TestCore030GraphProfileRefusalHasNoNodeDetail(t *testing.T) {
	base := coreGraphPack("0.3.0", graphRoutes, "")
	extended := coreGraphPack("0.3.0", graphRoutes, `,"profile":"extended"`)
	loaded, doc := coreGraphProject(t, base, extended)
	_, failure := Evaluate(loaded, newEngine(t), doc, "graph.json", nil, false, Options{})
	if failure == nil || failure.Code != "profile-unsupported" || failure.Profile != "extended" || failure.Message != "" || failure.Class != "" {
		t.Fatalf("profile refusal: %+v", failure)
	}
	_, failure = Test(loaded, newEngine(t), doc, "graph.json", "rows.json", Rows{Cases: []RowCase{{ID: "r", Inputs: json.RawMessage(`{}`), ExpectedDisposition: json.RawMessage(graphCanonical)}}}, Options{})
	if failure == nil || failure.Code != "profile-unsupported" || failure.Profile != "extended" || failure.Message != "" {
		t.Fatalf("test profile refusal: %+v", failure)
	}
}

func TestCore030GraphDirectFilteredNoneAndSingularAssertions(t *testing.T) {
	const want = `{"handoff":{"routes":[],"state":"none"},"kind":"unresolved","reasons":["exception-escalation"]}`
	pack := coreGraphPack("0.3.0", `{"triggers":["unknown"],"routes":[{"target":{"kind":"queue","name":"Review"},"triggers":["unknown"]}]}`, `,"exceptions":[{"id":"direct","description":"Direct","when":{"op":"literal","value":true},"effect":"escalate","onUnknown":"ignore"}]`)
	loaded, doc := coreGraphProject(t, pack, pack)
	got, failure := Evaluate(loaded, newEngine(t), doc, "graph.json", nil, false, Options{})
	if failure != nil || len(got.Handoffs) != 0 || got.HandoffTarget != nil {
		t.Fatalf("filtered graph: %+v %+v", got, failure)
	}
	canonical, err := got.Disposition.Canonical()
	if err != nil || string(canonical) != want {
		t.Fatalf("filtered canonical: %s %v", canonical, err)
	}
	for _, node := range []bool{false, true} {
		row := RowCase{ID: "r", Inputs: json.RawMessage(`{}`), ExpectedDisposition: json.RawMessage(want)}
		if node {
			row.ExpectedNodes = map[string]json.RawMessage{"a": json.RawMessage(want)}
			row.ExpectedNodeHandoffTargets = map[string]json.RawMessage{"a": json.RawMessage(`null`)}
		} else {
			row.ExpectedHandoffTarget = json.RawMessage(`null`)
		}
		tested, failure := Test(loaded, newEngine(t), doc, "graph.json", "rows.json", Rows{Cases: []RowCase{row}}, Options{})
		if failure != nil || tested.Status != "mismatch" || !strings.Contains(tested.Rows[0].Detail, "incompatible") {
			t.Fatalf("singular routed assertion: %+v %+v", tested, failure)
		}
	}
}

func TestCore030GraphPortableNamesAreExactAndIndependentOfDisplay(t *testing.T) {
	routes := strings.Replace(graphRoutes, `"name":"Z"`, `"name":"Z\u001b[31m"`, 1)
	pack := coreGraphPack("0.3.0", routes, "")
	loaded, doc := coreGraphProject(t, pack, pack)
	got, failure := Evaluate(loaded, newEngine(t), doc, "graph.json", nil, false, Options{})
	if failure != nil {
		t.Fatal(failure)
	}
	if got.Disposition.Handoff.Routes[0].Name != "Z\x1b[31m" || strings.Contains(got.Handoffs[0].Target.Name, "\x1b") {
		t.Fatalf("portable/display names: %+v %+v", got.Disposition.Handoff.Routes, got.Handoffs)
	}
	if !reflect.DeepEqual(got.Nodes[1].Disposition.Handoff.Routes, got.Disposition.Handoff.Routes) {
		t.Fatal("headline changed portable routes")
	}
}

func TestCore030GraphRechecksTheExactEvaluationReadBeforeAudit(t *testing.T) {
	pack := coreGraphPack("0.3.0", graphRoutes, "")
	loaded, doc := coreGraphProject(t, pack, pack)
	writer, dir := coreGraphAudit(t)
	changed := false
	_, failure := Evaluate(loaded, newEngine(t), doc, "graph.json", nil, false, Options{Audit: writer, LawCheck: func(_ string, _ []byte) *evaluation.Failure {
		if !changed {
			changed = true
			legacy := coreGraphPack(result.EvaluatorSpecVersion, `{"triggers":["no-match"],"target":{"kind":"queue","name":"Review"}}`, "")
			if err := os.WriteFile(filepath.Join(filepath.Dir(loaded.ConfigPath), "q.json"), []byte(legacy), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}})
	if failure == nil || failure.Code != "JPS-GRAPH-MIXED-CONTRACT" {
		t.Fatalf("changed contract was evaluated: %+v", failure)
	}
	if _, err := os.Stat(filepath.Join(dir, "audit", audit.FileName)); !os.IsNotExist(err) {
		t.Fatalf("partial run wrote audit: %v", err)
	}
}

func TestCore030GraphBoundsRouteAmplificationWithoutAudit(t *testing.T) {
	// Each pack remains below the carrier byte/string bounds; portable routes
	// duplicated across nodes, headline and handoffs exceed the report bound.
	route := `{"target":{"kind":"queue","name":"` + strings.Repeat("r", 500000) + `"}}`
	routes := make([]string, 8)
	for i := range routes {
		routes[i] = route
	}
	pack := coreGraphPack("0.3.0", `{"triggers":["no-match"],"routes":[`+strings.Join(routes, ",")+`]}`, "")
	loaded, doc := coreGraphProject(t, pack, pack)
	writer, dir := coreGraphAudit(t)
	got, failure := Evaluate(loaded, newEngine(t), doc, "graph.json", nil, false, Options{Audit: writer})
	if failure == nil || failure.Code != "JPS-GRAPH-REPORT-BUDGET" || len(got.Nodes) != 0 {
		t.Fatalf("route amplification: nodes=%d failure=%+v", len(got.Nodes), failure)
	}
	if _, err := os.Stat(filepath.Join(dir, "audit", audit.FileName)); !os.IsNotExist(err) {
		t.Fatalf("oversized run wrote audit: %v", err)
	}
}

func TestCore030GraphSuiteStatesUniformContractAndRejectsMixedEntries(t *testing.T) {
	const doc = `{"formatVersion":"1","id":"routes","version":"1.0.0","nodes":{"a":{"pack":"p"}},"edges":[],"result":"a"}`
	const other = `{"formatVersion":"1","id":"other","version":"1.0.0","nodes":{"a":{"pack":"q"}},"edges":[],"result":"a"}`
	files := map[string]string{
		"jpack.json": `{"configVersion":"2","packs":{"p":{"path":"p.json"},"q":{"path":"q.json"}},"graphs":{"first":{"path":"first.graph.json","rows":"rows.json"},"second":{"path":"second.graph.json","rows":"rows.json"}}}`,
		"p.json":     coreGraphPack("0.3.0", graphRoutes, ""), "q.json": coreGraphPack("0.3.0", graphRoutes, ""),
		"first.graph.json": doc, "second.graph.json": other,
		"rows.json": `{"cases":[{"id":"r","inputs":{},"expectedDisposition":` + graphCanonical + `}]}`,
	}
	loaded := writeProject(t, files)
	got, failure := TestProject(loaded, newEngine(t), "", Options{})
	if failure != nil || got.Status != "passed" || got.EvaluatorSpecVersion != "0.3.0" || len(got.Graphs) != 2 {
		t.Fatalf("Core 0.3 suite: %+v %+v", got, failure)
	}
	for _, entry := range got.Graphs {
		if entry.EvaluatorSpecVersion != "0.3.0" {
			t.Fatalf("entry contract: %+v", entry)
		}
	}
	files["q.json"] = coreGraphPack(result.EvaluatorSpecVersion, `{"triggers":["no-match"],"target":{"kind":"queue","name":"Review"}}`, "")
	mixed := writeProject(t, files)
	got, failure = TestProject(mixed, nil, "", Options{})
	if failure == nil || failure.Code != "JPS-GRAPH-MIXED-CONTRACT" || len(got.Graphs) != 0 {
		t.Fatalf("mixed suite executed: %+v %+v", got, failure)
	}
}

func TestLegacyGraphExpectationsDoNotInferCore030FromRoutes(t *testing.T) {
	pack := coreGraphPack(result.EvaluatorSpecVersion, `{"triggers":["no-match"],"target":{"kind":"queue","name":"Review"}}`, "")
	loaded, doc := coreGraphProject(t, pack, pack)
	rows := Rows{Cases: []RowCase{{ID: "r", Inputs: json.RawMessage(`{}`), ExpectedDisposition: json.RawMessage(graphCanonical)}}}
	got, failure := Test(loaded, newEngine(t), doc, "graph.json", "rows.json", rows, Options{})
	if failure != nil || got.Status != "mismatch" || got.EvaluatorSpecVersion != result.EvaluatorSpecVersion || !strings.Contains(got.Rows[0].Detail, "defect in the row") {
		t.Fatalf("route inference: %+v %+v", got, failure)
	}
}

func TestCore030GraphClassedRefusalCarriesSelectedContract(t *testing.T) {
	pack := coreGraphPack("0.3.0", graphRoutes, "")
	broken := strings.Replace(pack, `"title":"Graph routes",`, "", 1)
	loaded, doc := coreGraphProject(t, pack, broken)
	_, failure := Evaluate(loaded, newEngine(t), doc, "graph.json", nil, false, Options{})
	if failure == nil || failure.Class != result.ClassPackNotConformant || failure.SpecVersion != "0.3.0" || !strings.Contains(failure.Message, `Node "b"`) {
		t.Fatalf("selected refusal contract: %+v", failure)
	}
}
