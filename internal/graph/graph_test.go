package graph

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "project", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fixtureProject(t *testing.T) *project.Project {
	t.Helper()
	loaded, failure := project.Load(filepath.Join("testdata", "project", "jpack.json"))
	if failure != nil {
		t.Fatal(failure.Message)
	}
	t.Cleanup(func() { loaded.Close() })
	return loaded
}

// writeProject lays out one project in a temporary directory and loads it.
func writeProject(t *testing.T, files map[string]string) *project.Project {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, failure := project.Load(filepath.Join(dir, "jpack.json"))
	if failure != nil {
		t.Fatal(failure.Message)
	}
	t.Cleanup(func() { loaded.Close() })
	return loaded
}

func fixtureDocument(t *testing.T) Document {
	t.Helper()
	document, failure := Load(fixtureBytes(t, "onboarding.graph.json"), "onboarding.graph.json")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	return document
}

func newEngine(t *testing.T) *evaluation.Engine {
	t.Helper()
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	return evaluation.NewEngine(validator)
}

const happyInputs = `{"screening":{"facts":{"screening":{"matches":"0"}},"evidence":{"screening-record":"present"}}}`

func TestLoadAcceptsTheFixture(t *testing.T) {
	document := fixtureDocument(t)
	if document.ID != "vendor-onboarding-flow" || document.Version != "0.1.0" {
		t.Fatalf("unexpected identity: %q %q", document.ID, document.Version)
	}
	if len(document.Nodes) != 2 || len(document.Edges) != 1 || document.Result != "onboarding" {
		t.Fatalf("unexpected shape: %+v", document)
	}
}

func TestLoadRefusals(t *testing.T) {
	base := `{"formatVersion":"1","id":"g","version":"0.1.0","nodes":{"a":{"pack":"p"}},"edges":[],"result":"a"}`
	cases := []struct {
		name string
		data string
		code string
		exit int
	}{
		{"empty", "", "JPS-GRAPH-JSON", result.ExitInvocation},
		{"malformed", "{", "JPS-GRAPH-JSON", result.ExitInvocation},
		{"duplicate member", `{"formatVersion":"1","formatVersion":"1"}`, "JPS-GRAPH-JSON", result.ExitInvocation},
		{"non-object", `[]`, "JPS-GRAPH-SCHEMA", result.ExitInvalid},
		{"no version", `{"id":"g"}`, "JPS-GRAPH-VERSION", result.ExitInvalid},
		{"non-string version", `{"formatVersion":1}`, "JPS-GRAPH-VERSION", result.ExitInvalid},
		{"unsupported version", `{"formatVersion":"2"}`, "JPS-GRAPH-VERSION", result.ExitUnsupported},
		{"unknown member", strings.Replace(base, `"edges"`, `"wires"`, 1), "JPS-GRAPH-SCHEMA", result.ExitInvalid},
		{"edge feeds nothing", `{"formatVersion":"1","id":"g","version":"0.1.0","nodes":{"a":{"pack":"p"},"b":{"pack":"p"}},"edges":[{"from":"a","to":"b"}],"result":"b"}`, "JPS-GRAPH-SCHEMA", result.ExitInvalid},
		{"bad pointer", `{"formatVersion":"1","id":"g","version":"0.1.0","nodes":{"a":{"pack":"p"},"b":{"pack":"p"}},"edges":[{"from":"a","to":"b","fact":"no-slash"}],"result":"b"}`, "JPS-GRAPH-SCHEMA", result.ExitInvalid},
		{"oversized pointer", `{"formatVersion":"1","id":"g","version":"0.1.0","nodes":{"a":{"pack":"p"},"b":{"pack":"p"}},"edges":[{"from":"a","to":"b","fact":"/` + strings.Repeat("x", 1100) + `"}],"result":"b"}`, "JPS-GRAPH-SCHEMA", result.ExitInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, failure := Load([]byte(tc.data), "test.graph.json")
			if failure == nil {
				t.Fatal("the document must be refused")
			}
			if failure.Code != tc.code || failure.ExitCode != tc.exit {
				t.Fatalf("got %s exit=%d (%s), want %s exit=%d", failure.Code, failure.ExitCode, failure.Message, tc.code, tc.exit)
			}
			if failure.Class != "" {
				t.Fatalf("a graph refusal carries no §8.4 class; got %q", failure.Class)
			}
		})
	}
	t.Run("byte limit", func(t *testing.T) {
		_, failure := Load(bytes.Repeat([]byte(" "), int(MaxGraphBytes)+1), "test.graph.json")
		if failure == nil || failure.Code != "JPS-RESOURCE-GRAPH-BYTE-LIMIT" {
			t.Fatalf("got %+v", failure)
		}
	})
}

// TestSemanticFindsEveryDefect mutates the fixture graph one defect at a time
// and asserts each is reported with its own code at its own location.
func TestSemanticFindsEveryDefect(t *testing.T) {
	loaded := fixtureProject(t)
	base := fixtureDocument(t)
	if diagnostics := base.Semantic(loaded); len(diagnostics) != 0 {
		t.Fatalf("the fixture must be clean: %+v", diagnostics)
	}

	edge := base.Edges[0]
	cases := []struct {
		name   string
		mutate func(document *Document)
		code   string
		path   string
	}{
		{"unconfigured pack", func(d *Document) {
			d.Nodes = map[string]Node{"screening": {Pack: "unconfigured-thing"}, "onboarding": d.Nodes["onboarding"]}
		}, "JPS-GRAPH-NODE-PACK", "/nodes/screening"},
		{"unknown from", func(d *Document) {
			d.Edges = append([]Edge{}, edge, Edge{From: "ghost", To: "onboarding", Fact: "/x"})
		}, "JPS-GRAPH-EDGE-ENDPOINT", "/edges/1/from"},
		{"unknown to", func(d *Document) {
			d.Edges = append([]Edge{}, edge, Edge{From: "screening", To: "ghost", Fact: "/x"})
		}, "JPS-GRAPH-EDGE-ENDPOINT", "/edges/1/to"},
		{"self edge", func(d *Document) {
			d.Edges = append([]Edge{}, edge, Edge{From: "screening", To: "screening", Fact: "/x"})
		}, "JPS-GRAPH-EDGE-SELF", "/edges/1"},
		{"duplicate fact", func(d *Document) {
			d.Edges = append([]Edge{}, edge, Edge{From: "screening", To: "onboarding", Fact: edge.Fact})
		}, "JPS-GRAPH-EDGE-DUPLICATE-FACT", "/edges/1/fact"},
		{"duplicate evidence", func(d *Document) {
			d.Edges = append([]Edge{}, edge, Edge{From: "screening", To: "onboarding", Evidence: &EvidenceFeed{ID: edge.Evidence.ID}})
		}, "JPS-GRAPH-EDGE-DUPLICATE-EVIDENCE", "/edges/1/evidence/id"},
		{"overlapping fact pointers", func(d *Document) {
			d.Edges = append([]Edge{}, edge, Edge{From: "screening", To: "onboarding", Fact: edge.Fact + "/deeper"})
		}, "JPS-GRAPH-EDGE-FACT-OVERLAP", "/edges/1/fact"},
		{"overlap by spelling", func(d *Document) {
			// Two spellings, one decoded path: raw equality cannot see it, the
			// decoded comparison must.
			d.Edges = append([]Edge{}, Edge{From: "screening", To: "onboarding", Fact: "/a~0b"}, Edge{From: "screening", To: "onboarding", Fact: "/a~0b/c"})
		}, "JPS-GRAPH-EDGE-FACT-OVERLAP", "/edges/1/fact"},
		{"pointer too deep", func(d *Document) {
			d.Edges = append([]Edge{}, Edge{From: "screening", To: "onboarding", Fact: strings.Repeat("/x", MaxFactPointerDepth+1)})
		}, "JPS-GRAPH-FACT-POINTER-DEPTH", "/edges/0/fact"},
		{"undeclared evidence", func(d *Document) {
			d.Edges = []Edge{{From: "screening", To: "onboarding", Evidence: &EvidenceFeed{ID: "not-a-requirement"}}}
		}, "JPS-GRAPH-EDGE-EVIDENCE-UNDECLARED", "/edges/0/evidence/id"},
		{"unknown result", func(d *Document) {
			d.Result = "ghost"
		}, "JPS-GRAPH-RESULT-NODE", "/result"},
		{"cycle", func(d *Document) {
			d.Edges = append([]Edge{}, edge, Edge{From: "onboarding", To: "screening", Fact: "/loop"})
		}, "JPS-GRAPH-CYCLE", "/edges"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			document := base
			tc.mutate(&document)
			diagnostics := document.Semantic(loaded)
			if len(diagnostics) == 0 {
				t.Fatal("the defect must be reported")
			}
			found := false
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == tc.code && diagnostic.InstancePath == tc.path {
					found = true
				}
			}
			if !found {
				t.Fatalf("want %s at %s, got %+v", tc.code, tc.path, diagnostics)
			}
		})
	}

	t.Run("unreadable pack", func(t *testing.T) {
		broken := writeProject(t, map[string]string{
			"jpack.json": `{"configVersion":"1","packs":{
			  "sanctions-screening":{"path":"sanctions-screening-0.1.0.pack.json"},
			  "vendor-onboarding":{"path":"missing.pack.json"}}}`,
			"sanctions-screening-0.1.0.pack.json": string(fixtureBytes(t, "sanctions-screening-0.1.0.pack.json")),
		})
		diagnostics := base.Semantic(broken)
		if len(diagnostics) != 1 || diagnostics[0].Code != "JPS-GRAPH-NODE-PACK-READ" || diagnostics[0].InstancePath != "/nodes/onboarding" {
			t.Fatalf("got %+v", diagnostics)
		}
	})
}

func TestOrderIsDeterministic(t *testing.T) {
	diamond := Document{
		Nodes: map[string]Node{"gate": {}, "left": {}, "right": {}, "sink": {}},
		Edges: []Edge{
			{From: "gate", To: "left", Fact: "/a"},
			{From: "gate", To: "right", Fact: "/b"},
			{From: "left", To: "sink", Fact: "/c"},
			{From: "right", To: "sink", Fact: "/d"},
		},
	}
	order, cycle := diamond.Order()
	if len(cycle) != 0 {
		t.Fatalf("no cycle exists: %v", cycle)
	}
	want := []string{"gate", "left", "right", "sink"}
	for index, node := range want {
		if order[index] != node {
			t.Fatalf("got %v, want %v", order, want)
		}
	}

	// Independent nodes come out sorted, not in map order.
	independent := Document{Nodes: map[string]Node{"zeta": {}, "alpha": {}, "mid": {}}}
	order, _ = independent.Order()
	if order[0] != "alpha" || order[1] != "mid" || order[2] != "zeta" {
		t.Fatalf("got %v", order)
	}

	looped := Document{
		Nodes: map[string]Node{"a": {}, "b": {}, "free": {}},
		Edges: []Edge{{From: "a", To: "b", Fact: "/x"}, {From: "b", To: "a", Fact: "/y"}},
	}
	order, cycle = looped.Order()
	if len(order) != 1 || order[0] != "free" {
		t.Fatalf("got order %v", order)
	}
	if len(cycle) != 2 || cycle[0] != "a" || cycle[1] != "b" {
		t.Fatalf("got cycle %v", cycle)
	}

	// A node merely blocked behind a cycle is not named as a cycle member: it
	// is not in one, and naming it would send an author hunting a cycle it is
	// not part of.
	blocked := Document{
		Nodes: map[string]Node{"a": {}, "b": {}, "c": {}},
		Edges: []Edge{{From: "a", To: "b", Fact: "/x"}, {From: "b", To: "a", Fact: "/y"}, {From: "b", To: "c", Fact: "/z"}},
	}
	_, cycle = blocked.Order()
	if len(cycle) != 2 || cycle[0] != "a" || cycle[1] != "b" {
		t.Fatalf("the blocked descendant must not be named: %v", cycle)
	}
}

func TestEvaluateComposesTheFixtureGraph(t *testing.T) {
	loaded := fixtureProject(t)
	output, failure := Evaluate(loaded, newEngine(t), fixtureDocument(t), "onboarding.graph.json", []byte(happyInputs), true, Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if output.Status != "evaluated" || !output.Experimental {
		t.Fatalf("unexpected envelope: %+v", output)
	}
	if output.ConformanceClaimReference != result.EvaluationClaimReference || output.Label != result.GraphCompositeLabel || output.Kind != result.ProjectKind {
		t.Fatalf("the labels must be carried: %+v", output)
	}
	if output.FormatVersion != FormatVersion || output.GraphVersion != "0.1.0" {
		t.Fatalf("the format version and the document's own version are two facts and both must be present: %+v", output)
	}
	if output.GraphID != "vendor-onboarding-flow" || output.ResultNode != "onboarding" || output.GraphPath != "onboarding.graph.json" {
		t.Fatalf("unexpected identity: %+v", output)
	}
	if output.Disposition.Kind != "outcome" || output.Disposition.OutcomeID != "approve" {
		t.Fatalf("the composite must echo the result node's outcome: %+v", output.Disposition)
	}
	if len(output.Handoffs) != 0 {
		t.Fatalf("no handoff was requested: %+v", output.Handoffs)
	}
	if len(output.Nodes) != 2 || output.Nodes[0].Node != "screening" || output.Nodes[1].Node != "onboarding" {
		t.Fatalf("unexpected node order: %+v", output.Nodes)
	}
	screening := output.Nodes[0]
	if screening.Disposition.OutcomeID != "clear" || screening.PackID != "https://example.invalid/judgment-packs/sanctions-screening" {
		t.Fatalf("unexpected screening result: %+v", screening)
	}
	onboarding := output.Nodes[1]
	if len(onboarding.FactFeeds) != 1 || !onboarding.FactFeeds[0].Injected || onboarding.FactFeeds[0].Value != "clear" {
		t.Fatalf("the fact feed must record the injection: %+v", onboarding.FactFeeds)
	}
	if len(onboarding.EvidenceFeeds) != 1 || onboarding.EvidenceFeeds[0].State != "present" {
		t.Fatalf("the evidence feed must record present: %+v", onboarding.EvidenceFeeds)
	}
	if output.Artifact == nil {
		t.Fatal("the artifact provenance must be carried")
	}
	canonical, err := output.Disposition.Canonical()
	if err != nil {
		t.Fatalf("the composite headline must stay a legal §8.3 disposition: %v", err)
	}
	if !strings.Contains(string(canonical), `"outcomeId":"approve"`) {
		t.Fatalf("unexpected canonical form: %s", canonical)
	}
}

// TestEvaluateUnresolvedUpstreamPropagates is the partial-failure position
// under test: an upstream that resolved nothing injects no fact and
// contributes unknown evidence, the downstream pack's own declarations decide
// what that means, and both requested handoffs surface in the aggregate.
func TestEvaluateUnresolvedUpstreamPropagates(t *testing.T) {
	loaded := fixtureProject(t)
	inputs := `{"screening":{"facts":{},"evidence":{"screening-record":"present"}}}`
	output, failure := Evaluate(loaded, newEngine(t), fixtureDocument(t), "onboarding.graph.json", []byte(inputs), true, Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if output.Disposition.Kind != "unresolved" || output.Disposition.Reasons[0] != "unknown" {
		t.Fatalf("the composite must echo the unresolved result node: %+v", output.Disposition)
	}
	if len(output.Handoffs) != 2 || output.Handoffs[0].Node != "screening" || output.Handoffs[1].Node != "onboarding" {
		t.Fatalf("both requested handoffs must be aggregated: %+v", output.Handoffs)
	}
	for _, handoff := range output.Handoffs {
		if handoff.Target == nil || handoff.Target.Name != "Compliance" {
			t.Fatalf("the declared target must be echoed: %+v", handoff)
		}
	}
	onboarding := output.Nodes[1]
	if onboarding.FactFeeds[0].Injected || onboarding.FactFeeds[0].Value != "" {
		t.Fatalf("nothing may be injected for a non-outcome upstream: %+v", onboarding.FactFeeds)
	}
	if onboarding.EvidenceFeeds[0].State != "unknown" {
		t.Fatalf("the default onUnresolved contribution is unknown: %+v", onboarding.EvidenceFeeds)
	}
}

// TestEvaluateOnUnresolvedAbsent exercises the declared alternative: an edge
// that states a decision which produced no outcome is itself the missing
// evidence.
func TestEvaluateOnUnresolvedAbsent(t *testing.T) {
	loaded := fixtureProject(t)
	document := fixtureDocument(t)
	document.Edges = []Edge{{From: "screening", To: "onboarding", Evidence: &EvidenceFeed{ID: "screening-outcome", OnUnresolved: "absent"}}}
	inputs := `{"screening":{"facts":{},"evidence":{"screening-record":"present"}}}`
	output, failure := Evaluate(loaded, newEngine(t), document, "inline", []byte(inputs), true, Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	onboarding := output.Nodes[1]
	if onboarding.EvidenceFeeds[0].State != "absent" {
		t.Fatalf("the declared onUnresolved must be applied: %+v", onboarding.EvidenceFeeds)
	}
	if output.Disposition.Kind != "unresolved" || output.Disposition.Reasons[0] != "missing-required-evidence" {
		t.Fatalf("absent required evidence is the §8 step-2 result: %+v", output.Disposition)
	}
}

func TestEvaluateRefusesConflicts(t *testing.T) {
	loaded := fixtureProject(t)
	cases := []struct {
		name   string
		inputs string
		code   string
	}{
		{"fact conflict", `{"screening":{"facts":{"screening":{"matches":"0"}},"evidence":{"screening-record":"present"}},
			"onboarding":{"facts":{"screening":{"status":"precleared"}}}}`, "JPS-GRAPH-FACT-CONFLICT"},
		// The same collisions are refused when the upstream cannot resolve: a
		// guard that waited for an outcome would let the caller's value stand
		// in for the outcome the upstream never produced, at the pointer the
		// edge declares it owns — the exact smuggling the surface exists to
		// remove.
		{"fact conflict under an unresolved upstream", `{"screening":{"facts":{},"evidence":{"screening-record":"present"}},
			"onboarding":{"facts":{"screening":{"status":"clear"}}}}`, "JPS-GRAPH-FACT-CONFLICT"},
		{"traversal conflict under an unresolved upstream", `{"screening":{"facts":{},"evidence":{"screening-record":"present"}},
			"onboarding":{"facts":{"screening":"clear"}}}`, "JPS-GRAPH-FACT-CONFLICT"},
		{"non-object facts with a fact edge", `{"screening":{"facts":{"screening":{"matches":"0"}},"evidence":{"screening-record":"present"}},
			"onboarding":{"facts":[]}}`, "JPS-GRAPH-FACT-CONFLICT"},
		{"evidence conflict", `{"screening":{"facts":{"screening":{"matches":"0"}},"evidence":{"screening-record":"present"}},
			"onboarding":{"evidence":{"screening-outcome":"present"}}}`, "JPS-GRAPH-EVIDENCE-CONFLICT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, failure := Evaluate(loaded, newEngine(t), fixtureDocument(t), "inline", []byte(tc.inputs), true, Options{Command: "test"})
			if failure == nil || failure.Code != tc.code || failure.ExitCode != result.ExitInvocation {
				t.Fatalf("got %+v", failure)
			}
			if !strings.Contains(failure.Message, `"onboarding"`) {
				t.Fatalf("the refusal must name the node: %s", failure.Message)
			}
		})
	}
}

func TestEvaluateRefusesAnInvalidGraph(t *testing.T) {
	loaded := fixtureProject(t)
	document := fixtureDocument(t)
	document.Result = "ghost"
	_, failure := Evaluate(loaded, newEngine(t), document, "inline", nil, false, Options{Command: "test"})
	if failure == nil || failure.Code != "JPS-GRAPH-INVALID" || failure.ExitCode != result.ExitInvalid {
		t.Fatalf("got %+v", failure)
	}
	if !strings.Contains(failure.Message, "JPS-GRAPH-RESULT-NODE") {
		t.Fatalf("the refusal must carry the first diagnostic: %s", failure.Message)
	}
}

// TestEvaluateNodeRefusalNamesTheNode: a node the engine refuses refuses the
// whole run with the §8.4 class intact and the node named, and no composite of
// any kind is reported.
func TestEvaluateNodeRefusalNamesTheNode(t *testing.T) {
	var pack map[string]any
	if err := json.Unmarshal(fixtureBytes(t, "vendor-onboarding-0.1.0.pack.json"), &pack); err != nil {
		t.Fatal(err)
	}
	delete(pack, "title")
	broken, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	loaded := writeProject(t, map[string]string{
		"jpack.json": `{"configVersion":"1","packs":{
		  "sanctions-screening":{"path":"sanctions-screening-0.1.0.pack.json"},
		  "vendor-onboarding":{"path":"vendor-onboarding-0.1.0.pack.json"}}}`,
		"sanctions-screening-0.1.0.pack.json": string(fixtureBytes(t, "sanctions-screening-0.1.0.pack.json")),
		"vendor-onboarding-0.1.0.pack.json":   string(broken),
	})
	_, failure := Evaluate(loaded, newEngine(t), fixtureDocument(t), "inline", []byte(happyInputs), true, Options{Command: "test"})
	if failure == nil {
		t.Fatal("the run must be refused")
	}
	if failure.Class != result.ClassPackNotConformant || failure.Phase != result.PhasePreflight {
		t.Fatalf("the node's §8.4 identity must be intact: %+v", failure)
	}
	if !strings.Contains(failure.Message, `Node "onboarding"`) {
		t.Fatalf("the refusal must name the node: %s", failure.Message)
	}
}

// TestEvaluateLeavesPackDefectsToTheEngine: a pack that is not even JSON is
// the engine's pack-not-conformant refusal with the node named — never an
// unclassed graph refusal — even when edges feed that node evidence, because
// the evaluation gate deliberately reads no pack.
func TestEvaluateLeavesPackDefectsToTheEngine(t *testing.T) {
	loaded := writeProject(t, map[string]string{
		"jpack.json": `{"configVersion":"1","packs":{
		  "sanctions-screening":{"path":"sanctions-screening-0.1.0.pack.json"},
		  "vendor-onboarding":{"path":"vendor-onboarding-0.1.0.pack.json"}}}`,
		"sanctions-screening-0.1.0.pack.json": string(fixtureBytes(t, "sanctions-screening-0.1.0.pack.json")),
		"vendor-onboarding-0.1.0.pack.json":   "{",
	})
	_, failure := Evaluate(loaded, newEngine(t), fixtureDocument(t), "inline", []byte(happyInputs), true, Options{Command: "test"})
	if failure == nil {
		t.Fatal("the run must be refused")
	}
	if failure.Class != result.ClassPackNotConformant || failure.Phase != result.PhasePreflight {
		t.Fatalf("the engine's §8.4 identity must be assigned, not lost to a graph refusal: %+v", failure)
	}
	if !strings.Contains(failure.Message, `Node "onboarding"`) {
		t.Fatalf("the refusal must name the node: %s", failure.Message)
	}
}

// TestPlanReportsUnreadablePack: a plan is printable while one pack is
// missing or mid-edit; the step carries the reason and no identity is
// guessed. Validate is what reports the reference gap.
func TestPlanReportsUnreadablePack(t *testing.T) {
	loaded := writeProject(t, map[string]string{
		"jpack.json": `{"configVersion":"1","packs":{
		  "sanctions-screening":{"path":"sanctions-screening-0.1.0.pack.json"},
		  "vendor-onboarding":{"path":"missing.pack.json"}}}`,
		"sanctions-screening-0.1.0.pack.json": string(fixtureBytes(t, "sanctions-screening-0.1.0.pack.json")),
	})
	output, failure := Plan(loaded, fixtureDocument(t), "inline", "test")
	if failure != nil {
		t.Fatalf("the plan must survive an unreadable pack: %+v", failure)
	}
	step := output.Steps[1]
	if step.Node != "onboarding" || step.Detail == "" || step.PackID != "" || step.PackVersion != "" {
		t.Fatalf("the step must carry the reason and guess nothing: %+v", step)
	}
}

func TestOverInjectionBudget(t *testing.T) {
	if overInjectionBudget(10<<20, 10<<20) {
		t.Fatal("the limit itself is admitted")
	}
	if !overInjectionBudget((10<<20)+1, 10<<20) {
		t.Fatal("one byte past the limit is refused")
	}
}

// TestInjectionBudgetIsClassedInPreflightOrder: a tripped budget stops
// injecting — that is the allocation bound — and reaches the engine as an
// oversized facts input, so it carries §8.4's malformed-input class at the
// facts document's own place in the preflight, and a non-conformant pack
// still outranks it. A resource guard that refused here would let a facts
// condition mask the pack's own admission.
func TestInjectionBudgetIsClassedInPreflightOrder(t *testing.T) {
	loaded := fixtureProject(t)
	options := Options{Command: "test", injectionBudget: 1}
	_, failure := Evaluate(loaded, newEngine(t), fixtureDocument(t), "inline", []byte(happyInputs), true, options)
	if failure == nil {
		t.Fatal("the run must be refused")
	}
	if failure.Class != result.ClassMalformedInput || failure.Phase != result.PhasePreflight {
		t.Fatalf("an over-budget facts input carries the facts document's own class: %+v", failure)
	}
	if !strings.Contains(failure.Message, `Node "onboarding"`) || !strings.Contains(failure.Message, "facts") {
		t.Fatalf("the refusal must name the node and the input: %s", failure.Message)
	}

	// The pack's own admission still comes first: the same tripped budget on a
	// node whose pack is not conformant reports the pack, not the facts.
	var pack map[string]any
	if err := json.Unmarshal(fixtureBytes(t, "vendor-onboarding-0.1.0.pack.json"), &pack); err != nil {
		t.Fatal(err)
	}
	delete(pack, "title")
	broken, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	brokenProject := writeProject(t, map[string]string{
		"jpack.json": `{"configVersion":"1","packs":{
		  "sanctions-screening":{"path":"sanctions-screening-0.1.0.pack.json"},
		  "vendor-onboarding":{"path":"vendor-onboarding-0.1.0.pack.json"}}}`,
		"sanctions-screening-0.1.0.pack.json": string(fixtureBytes(t, "sanctions-screening-0.1.0.pack.json")),
		"vendor-onboarding-0.1.0.pack.json":   string(broken),
	})
	_, failure = Evaluate(brokenProject, newEngine(t), fixtureDocument(t), "inline", []byte(happyInputs), true, options)
	if failure == nil || failure.Class != result.ClassPackNotConformant {
		t.Fatalf("the pack's admission outranks the facts resource condition: %+v", failure)
	}
}

func TestParseInputsRefusals(t *testing.T) {
	document := fixtureDocument(t)
	cases := []struct {
		name string
		data string
		code string
	}{
		{"empty supplied", "", "JPS-GRAPH-INPUTS-JSON"},
		{"malformed", "{", "JPS-GRAPH-INPUTS-JSON"},
		{"non-object", `[]`, "JPS-GRAPH-INPUTS-SHAPE"},
		{"unknown node", `{"ghost":{}}`, "JPS-GRAPH-INPUTS-NODE"},
		{"non-object entry", `{"screening":[]}`, "JPS-GRAPH-INPUTS-SHAPE"},
		{"unknown member", `{"screening":{"fact":{}}}`, "JPS-GRAPH-INPUTS-MEMBER"},
		{"non-object evidence", `{"screening":{"evidence":[]}}`, "JPS-GRAPH-INPUTS-EVIDENCE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, failure := parseInputs([]byte(tc.data), true, document)
			if failure == nil || failure.Code != tc.code {
				t.Fatalf("got %+v", failure)
			}
		})
	}
	t.Run("omitted is empty", func(t *testing.T) {
		perNode, failure := parseInputs(nil, false, document)
		if failure != nil || len(perNode) != 0 {
			t.Fatalf("got %+v %+v", perNode, failure)
		}
	})
	t.Run("byte limit", func(t *testing.T) {
		_, failure := parseInputs(bytes.Repeat([]byte(" "), int(MaxInputsBytes)+1), true, document)
		if failure == nil || failure.Code != "JPS-RESOURCE-GRAPH-INPUTS-BYTE-LIMIT" {
			t.Fatalf("got %+v", failure)
		}
	})
	t.Run("carrier resource limit keeps its code and class", func(t *testing.T) {
		// Inside the byte limit, over the carrier's node limit: a document
		// refused for its size is not a document refused for its syntax.
		huge := `{"screening":{"facts":[` + strings.Repeat("1,", 300_000) + `1]}}`
		_, failure := parseInputs([]byte(huge), true, document)
		if failure == nil || !strings.HasPrefix(failure.Code, "JPS-RESOURCE-") || failure.ExitCode != result.ExitIO {
			t.Fatalf("got %+v", failure)
		}
	})
}

func TestInjectFact(t *testing.T) {
	root := map[string]any{"kept": "value", "nested": map[string]any{"existing": "fact"}}
	if err := injectFact(root, "/nested/added/deep~1key~0", "clear"); err != nil {
		t.Fatal(err)
	}
	nested := root["nested"].(map[string]any)
	added := nested["added"].(map[string]any)
	if added["deep/key~"] != "clear" || nested["existing"] != "fact" || root["kept"] != "value" {
		t.Fatalf("got %+v", root)
	}
	if err := injectFact(root, "/nested/existing", "x"); err == nil || !strings.Contains(err.Error(), "never overwrites") {
		t.Fatalf("an existing value must refuse: %v", err)
	}
	if err := injectFact(root, "/kept/inside", "x"); err == nil || !strings.Contains(err.Error(), "not a JSON object") {
		t.Fatalf("a non-object intermediate must refuse: %v", err)
	}
}

func TestPlanStatesTheFixture(t *testing.T) {
	loaded := fixtureProject(t)
	output, failure := Plan(loaded, fixtureDocument(t), "onboarding.graph.json", "test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if output.Status != "planned" || output.ResultNode != "onboarding" || len(output.Steps) != 2 {
		t.Fatalf("unexpected plan: %+v", output)
	}
	first, second := output.Steps[0], output.Steps[1]
	if first.Order != 1 || first.Node != "screening" || first.PackID == "" || len(first.Feeds) != 0 {
		t.Fatalf("unexpected first step: %+v", first)
	}
	if second.Node != "onboarding" || len(second.Feeds) != 2 {
		t.Fatalf("unexpected second step: %+v", second)
	}
	if second.Feeds[0].Fact != "/screening/status" || second.Feeds[1].Evidence != "screening-outcome" || second.Feeds[1].OnUnresolved != "unknown" {
		t.Fatalf("the feeds must be stated with the default explicit: %+v", second.Feeds)
	}

	invalid := fixtureDocument(t)
	invalid.Result = "ghost"
	if _, planFailure := Plan(loaded, invalid, "inline", "test"); planFailure == nil || planFailure.Code != "JPS-GRAPH-INVALID" {
		t.Fatalf("an invalid graph has no plan: %+v", planFailure)
	}
}

func TestLoadRowsRefusals(t *testing.T) {
	valid := `{"graphMatrixVersion":"1","cases":[{"id":"r","inputs":{},"expectedDisposition":{"kind":"outcome","outcomeId":"x","reasons":[],"handoff":{"state":"none"}}}]}`
	cases := []struct {
		name string
		data string
		code string
	}{
		{"empty", "", "JPS-GRAPH-ROWS-JSON"},
		{"malformed", "{", "JPS-GRAPH-ROWS-JSON"},
		{"unknown member", strings.Replace(valid, `"cases"`, `"rows"`, 1), "JPS-GRAPH-ROWS-SHAPE"},
		{"unsupported version", strings.Replace(valid, `"1"`, `"2"`, 1), "JPS-GRAPH-ROWS-VERSION"},
		{"no rows", `{"cases":[]}`, "JPS-GRAPH-ROWS-EMPTY"},
		{"no id", `{"cases":[{"inputs":{},"expectedErrorClass":"malformed-input"}]}`, "JPS-GRAPH-ROWS-ROW"},
		{"duplicate id", `{"cases":[{"id":"r","inputs":{},"expectedErrorClass":"c"},{"id":"r","inputs":{},"expectedErrorClass":"c"}]}`, "JPS-GRAPH-ROWS-ROW"},
		{"no inputs", `{"cases":[{"id":"r","expectedErrorClass":"c"}]}`, "JPS-GRAPH-ROWS-ROW"},
		{"both expectations", `{"cases":[{"id":"r","inputs":{},"expectedErrorClass":"c","expectedDisposition":{}}]}`, "JPS-GRAPH-ROWS-ROW"},
		{"neither expectation", `{"cases":[{"id":"r","inputs":{}}]}`, "JPS-GRAPH-ROWS-ROW"},
		{"phase without class", `{"cases":[{"id":"r","inputs":{},"expectedDisposition":{},"expectedErrorPhase":"preflight"}]}`, "JPS-GRAPH-ROWS-ROW"},
		{"nodes beside error", `{"cases":[{"id":"r","inputs":{},"expectedErrorClass":"c","expectedNodes":{"n":{}}}]}`, "JPS-GRAPH-ROWS-ROW"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, failure := LoadRows([]byte(tc.data), "rows.json")
			if failure == nil || failure.Code != tc.code {
				t.Fatalf("got %+v", failure)
			}
		})
	}
	if _, failure := LoadRows([]byte(valid), "rows.json"); failure != nil {
		t.Fatalf("the valid document loads: %+v", failure)
	}
}

func TestGraphTestJudgesRowsByteExactly(t *testing.T) {
	loaded := fixtureProject(t)
	rows, failure := LoadRows(fixtureBytes(t, "onboarding.rows.json"), "onboarding.rows.json")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	output, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", rows, Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if output.Status != "passed" || output.Summary.Passed != 3 || output.Summary.Total != 3 {
		t.Fatalf("the fixture rows all pass: %+v", output)
	}
	if output.Label != result.GraphMatrixLabel || !output.Experimental || output.ConformanceClaimReference != result.EvaluationClaimReference {
		t.Fatalf("the labels must be carried: %+v", output)
	}
	first := output.Rows[0]
	if len(first.Nodes) != 1 || first.Nodes[0].Node != "screening" || first.Nodes[0].Status != "passed" || first.Nodes[0].Expected != first.Nodes[0].Actual {
		t.Fatalf("the named node is compared canonically: %+v", first.Nodes)
	}

	// A wrong expectation is a mismatch carrying both canonical forms.
	wrong := rows
	wrong.Cases = []RowCase{{
		ID:                  "wrong",
		Inputs:              rows.Cases[0].Inputs,
		ExpectedDisposition: json.RawMessage(`{"kind":"outcome","outcomeId":"decline","reasons":[],"handoff":{"state":"none"}}`),
	}}
	output, failure = Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", wrong, Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	row := output.Rows[0]
	if output.Status != "mismatch" || row.Status != "mismatch" || !strings.Contains(row.Actual, `"approve"`) || !strings.Contains(row.Expected, `"decline"`) {
		t.Fatalf("a differing headline mismatches with both canonical forms: %+v", row)
	}

	// An illegal expectation is the row's own defect, never compared loosely.
	illegal := rows
	illegal.Cases = []RowCase{{
		ID:                  "illegal",
		Inputs:              rows.Cases[0].Inputs,
		ExpectedDisposition: json.RawMessage(`{"kind":"outcome","outcomeId":"approve","reasons":["unknown"],"handoff":{"state":"none"}}`),
	}}
	output, _ = Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", illegal, Options{Command: "test"})
	if output.Rows[0].Status != "mismatch" || !strings.Contains(output.Rows[0].Detail, "defect in the row") {
		t.Fatalf("an illegal expectation is the row's defect: %+v", output.Rows[0])
	}

	// A row naming a node the graph does not declare mismatches with the name.
	ghost := rows
	ghost.Cases = []RowCase{{
		ID:                  "ghost-node",
		Inputs:              rows.Cases[0].Inputs,
		ExpectedDisposition: json.RawMessage(`{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}`),
		ExpectedNodes:       map[string]json.RawMessage{"ghost": json.RawMessage(`{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}`)},
	}}
	output, _ = Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", ghost, Options{Command: "test"})
	if output.Rows[0].Status != "mismatch" || !strings.Contains(output.Rows[0].Detail, `"ghost"`) {
		t.Fatalf("an undeclared node in expectedNodes mismatches: %+v", output.Rows[0])
	}
}

// A row expecting a refusal passes exactly when the run is refused with that
// class (and phase, when named) — the same discipline a pack matrix's error
// rows have.
func TestGraphTestErrorRows(t *testing.T) {
	var pack map[string]any
	if err := json.Unmarshal(fixtureBytes(t, "vendor-onboarding-0.1.0.pack.json"), &pack); err != nil {
		t.Fatal(err)
	}
	delete(pack, "title")
	broken, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	loaded := writeProject(t, map[string]string{
		"jpack.json": `{"configVersion":"1","packs":{
		  "sanctions-screening":{"path":"sanctions-screening-0.1.0.pack.json"},
		  "vendor-onboarding":{"path":"vendor-onboarding-0.1.0.pack.json"}}}`,
		"sanctions-screening-0.1.0.pack.json": string(fixtureBytes(t, "sanctions-screening-0.1.0.pack.json")),
		"vendor-onboarding-0.1.0.pack.json":   string(broken),
	})
	rows := Rows{Cases: []RowCase{{
		ID:                 "refused",
		Inputs:             json.RawMessage(`{}`),
		ExpectedErrorClass: result.ClassPackNotConformant,
		ExpectedErrorPhase: result.PhasePreflight,
	}}}
	output, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", rows, Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if output.Status != "passed" || output.Rows[0].ActualErrorClass != result.ClassPackNotConformant {
		t.Fatalf("a refused run matches its expected class: %+v", output.Rows[0])
	}
}

// The precondition LoadRows enforces holds for direct callers too, and a
// document whose root is not an object is a shape refusal on the matrix
// loader's own precedent, not an empty-matrix one.
func TestGraphTestPreconditions(t *testing.T) {
	if _, failure := LoadRows([]byte(`null`), "rows.json"); failure == nil || failure.Code != "JPS-GRAPH-ROWS-SHAPE" {
		t.Fatalf("a null root is a shape refusal: %+v", failure)
	}
	loaded := fixtureProject(t)
	if _, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", Rows{}, Options{Command: "test"}); failure == nil || failure.Code != "JPS-GRAPH-ROWS-EMPTY" {
		t.Fatalf("zero rows never yield a clean report: %+v", failure)
	}
}
