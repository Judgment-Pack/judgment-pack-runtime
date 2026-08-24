package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/graph"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// graphFixture reads one file of the graph package's fixture project, so the
// CLI is tested against the same demo-seam pair the engine-level tests use.
func graphFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "graph", "testdata", "project", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// graphProject lays out the fixture project plus the graph document and any
// extra files, returning the configuration path and the graph path.
func graphProject(t *testing.T, graphDocument string, extra map[string]string) (string, string) {
	t.Helper()
	files := map[string]string{
		"sanctions-screening-0.1.0.pack.json": graphFixture(t, "sanctions-screening-0.1.0.pack.json"),
		"vendor-onboarding-0.1.0.pack.json":   graphFixture(t, "vendor-onboarding-0.1.0.pack.json"),
		"onboarding.graph.json":               graphDocument,
	}
	for name, body := range extra {
		files[name] = body
	}
	configPath := writeProjectFixture(t, graphFixture(t, "jpack.json"), files)
	return configPath, filepath.Join(filepath.Dir(configPath), "onboarding.graph.json")
}

const graphHappyInputs = `{"screening":{"facts":{"screening":{"matches":"0"}},"evidence":{"screening-record":"present"}}}`

func writeGraphInputs(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "inputs.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGraphValidateCommand(t *testing.T) {
	configPath, graphPath := graphProject(t, graphFixture(t, "onboarding.graph.json"), nil)
	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "validate", graphPath, "--config", configPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "valid: graph vendor-onboarding-flow 0.1.0") {
		t.Fatalf("unexpected human output: %q", stdout)
	}

	// A cycle is found, reported as a diagnostic, and exits 1.
	looped := strings.Replace(graphFixture(t, "onboarding.graph.json"),
		`"edges": [`,
		`"edges": [
    {"from": "onboarding", "to": "screening", "fact": "/loop"},`, 1)
	configPath, graphPath = graphProject(t, looped, nil)
	code, stdout, stderr = runTest(t, []string{"experimental", "graph", "validate", graphPath, "--config", configPath, "--format", "json"}, "")
	if code != result.ExitInvalid || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var validation result.GraphValidation
	if err := json.Unmarshal([]byte(stdout), &validation); err != nil {
		t.Fatal(err)
	}
	if validation.Status != "invalid" || validation.Kind != result.ProjectKind {
		t.Fatalf("unexpected payload: %+v", validation)
	}
	found := false
	for _, diagnostic := range validation.Diagnostics {
		if diagnostic.Code == "JPS-GRAPH-CYCLE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the cycle must be a diagnostic: %+v", validation.Diagnostics)
	}
}

func TestGraphEvaluateCommandComposesAndReportsJSON(t *testing.T) {
	configPath, graphPath := graphProject(t, graphFixture(t, "onboarding.graph.json"), nil)
	inputs := writeGraphInputs(t, graphHappyInputs)
	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath, "--inputs", inputs, "--config", configPath, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var output result.GraphEvaluation
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output.OutputVersion != result.OutputVersion || !output.Experimental || output.Status != "evaluated" {
		t.Fatalf("unexpected envelope: %+v", output)
	}
	if output.ConformanceClaimReference != result.EvaluationClaimReference || !strings.Contains(output.Label, "graph composite") {
		t.Fatalf("the labels must be carried: %+v", output)
	}
	if output.EvaluatorSpecVersion != result.EvaluatorSpecVersion || output.ResultNode != "onboarding" {
		t.Fatalf("unexpected identity: %+v", output)
	}
	if output.FormatVersion != graph.FormatVersion || output.GraphVersion != "0.1.0" {
		t.Fatalf("the format version and the document's own version are two members: %+v", output)
	}
	if output.Disposition.Kind != "outcome" || output.Disposition.OutcomeID != "approve" {
		t.Fatalf("unexpected composite disposition: %+v", output.Disposition)
	}
	if len(output.Nodes) != 2 || len(output.Handoffs) != 0 {
		t.Fatalf("unexpected composition: %+v", output)
	}

	// The escalation path: an unresolvable upstream propagates, and both
	// requested handoffs are aggregated in the composite.
	inputs = writeGraphInputs(t, `{"screening":{"facts":{},"evidence":{"screening-record":"present"}}}`)
	code, stdout, stderr = runTest(t, []string{"experimental", "graph", "evaluate", graphPath, "--inputs", inputs, "--config", configPath, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output.Disposition.Kind != "unresolved" || len(output.Handoffs) != 2 {
		t.Fatalf("the escalations must surface: %+v", output)
	}
}

func TestGraphEvaluateReportsTheNodeErrorClass(t *testing.T) {
	var pack map[string]any
	if err := json.Unmarshal([]byte(graphFixture(t, "vendor-onboarding-0.1.0.pack.json")), &pack); err != nil {
		t.Fatal(err)
	}
	delete(pack, "title")
	broken, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	configPath, graphPath := graphProject(t, graphFixture(t, "onboarding.graph.json"), map[string]string{
		"vendor-onboarding-0.1.0.pack.json": string(broken),
	})
	inputs := writeGraphInputs(t, graphHappyInputs)
	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath, "--inputs", inputs, "--config", configPath, "--format", "json"}, "")
	if code != result.ExitInvalid || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var envelope result.OperationalError
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.EvaluationError == nil || envelope.EvaluationError.Class != result.ClassPackNotConformant || envelope.EvaluationError.Phase != result.PhasePreflight {
		t.Fatalf("the node's §8.4 identity must be in band: %+v", envelope)
	}
	if !strings.Contains(envelope.Diagnostics[0].Message, `Node "onboarding"`) {
		t.Fatalf("the refusal must name the node: %+v", envelope.Diagnostics)
	}
}

func TestGraphExplainCommand(t *testing.T) {
	configPath, graphPath := graphProject(t, graphFixture(t, "onboarding.graph.json"), nil)
	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "explain", graphPath, "--config", configPath, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var plan result.GraphPlan
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Status != "planned" || len(plan.Steps) != 2 || plan.Steps[1].Node != "onboarding" || len(plan.Steps[1].Feeds) != 2 {
		t.Fatalf("unexpected plan: %+v", plan)
	}

	code, stdout, stderr = runTest(t, []string{"experimental", "graph", "explain", graphPath, "--config", configPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "graph plan:") || !strings.Contains(stdout, "nothing was evaluated") {
		t.Fatalf("unexpected human output: %q", stdout)
	}

	// A pack that read and decoded fine but declares no id is not "document
	// unreadable": three different things leave the identity empty, and the
	// plan keeps them apart exactly as the project inventory does.
	var pack map[string]any
	if err := json.Unmarshal([]byte(graphFixture(t, "vendor-onboarding-0.1.0.pack.json")), &pack); err != nil {
		t.Fatal(err)
	}
	delete(pack, "id")
	anonymous, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	configPath, graphPath = graphProject(t, graphFixture(t, "onboarding.graph.json"), map[string]string{
		"vendor-onboarding-0.1.0.pack.json": string(anonymous),
	})
	code, stdout, stderr = runTest(t, []string{"experimental", "graph", "explain", graphPath, "--config", configPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "(pack vendor-onboarding = no id declared)") || strings.Contains(stdout, "document unreadable") {
		t.Fatalf("a decodable pack without an id must not be called unreadable: %q", stdout)
	}
}

func TestGraphSchemaCommand(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "schema"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "graph schema (formatVersion 1)") || !strings.Contains(stdout, graph.SchemaID) {
		t.Fatalf("unexpected output: %q", stdout)
	}

	code, stdout, stderr = runTest(t, []string{"experimental", "graph", "schema", "--write", "-"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if stdout != string(graph.Schema()) {
		t.Fatalf("--write - must print the exact schema bytes")
	}

	code, _, _ = runTest(t, []string{"experimental", "graph", "schema", "--write", "-", "--format", "json"}, "")
	if code != result.ExitInvocation {
		t.Fatalf("--write - with --format json must be refused: exit=%d", code)
	}
}

func TestGraphInvocationGuards(t *testing.T) {
	configPath, _ := graphProject(t, graphFixture(t, "onboarding.graph.json"), nil)
	code, stdout, _ := runTest(t, []string{"experimental", "graph", "evaluate", "-", "--inputs", "-", "--config", configPath, "--format", "json"}, "")
	if code != result.ExitInvocation {
		t.Fatalf("two stdin inputs must be refused: exit=%d", code)
	}
	var envelope result.OperationalError
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Diagnostics[0].Code != "JPS-INVOCATION-STDIN" {
		t.Fatalf("unexpected code: %+v", envelope.Diagnostics)
	}
	if envelope.Command != "experimental graph evaluate" {
		t.Fatalf("the command label must name the graph verb: %+v", envelope)
	}

	// The graph document itself can arrive on standard input.
	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "validate", "-", "--config", configPath}, graphFixture(t, "onboarding.graph.json"))
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
}

func TestGraphTestCommand(t *testing.T) {
	configPath, graphPath := graphProject(t, graphFixture(t, "onboarding.graph.json"), map[string]string{
		"onboarding.rows.json": graphFixture(t, "onboarding.rows.json"),
	})
	rowsPath := filepath.Join(filepath.Dir(configPath), "onboarding.rows.json")
	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "test", graphPath, "--rows", rowsPath, "--config", configPath, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var output result.GraphTest
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != "passed" || output.Summary.Passed != 3 || !output.Experimental {
		t.Fatalf("unexpected payload: %+v", output)
	}
	if output.ConformanceClaimReference != result.EvaluationClaimReference || !strings.Contains(output.Label, "graph matrix") {
		t.Fatalf("the labels must be carried: %+v", output)
	}
	if len(output.Coverage) != 13 {
		t.Fatalf("the fixture derives thirteen probes: %+v", output.Coverage)
	}

	// The human surface prints the coverage block, and missing probes move
	// nothing: the run above already exited 0 with probes missing, and the
	// exact count line is pinned so the informs-never-gates block is a
	// deliberate edit, never drift (ADR-0016).
	code, stdout, stderr = runTest(t, []string{"experimental", "graph", "test", graphPath, "--rows", rowsPath, "--config", configPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("missing probes must not gate: exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "coverage: 7/13 derived probes are witnessed by a row") {
		t.Fatalf("the human surface carries the count line: %q", stdout)
	}
	if !strings.Contains(stdout, "node:screening:missing-required-evidence:") {
		t.Fatalf("a missing probe gets its detail line: %q", stdout)
	}

	// A mismatching matrix exits 1 and the human surface details it.
	broken := strings.Replace(graphFixture(t, "onboarding.rows.json"), `"outcomeId": "approve"`, `"outcomeId": "decline"`, 1)
	brokenPath := filepath.Join(t.TempDir(), "broken.rows.json")
	if err := os.WriteFile(brokenPath, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runTest(t, []string{"experimental", "graph", "test", graphPath, "--rows", brokenPath, "--config", configPath}, "")
	if code != result.ExitInvalid || stderr != "" {
		t.Fatalf("a mismatch exits 1: exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "mismatch: 1/3") || !strings.Contains(stdout, "expected:") {
		t.Fatalf("the human surface details the mismatch: %q", stdout)
	}

	// --rows is required, and dual stdin is refused.
	code, _, _ = runTest(t, []string{"experimental", "graph", "test", graphPath, "--config", configPath}, "")
	if code != result.ExitInvocation {
		t.Fatalf("--rows is required: exit=%d", code)
	}
	code, _, _ = runTest(t, []string{"experimental", "graph", "test", "-", "--rows", "-", "--config", configPath}, "")
	if code != result.ExitInvocation {
		t.Fatalf("dual stdin is refused: exit=%d", code)
	}
}

// With no argument the two verbs walk the project's declared graphs
// (ADR-0017): the fixture's one graph runs its declared rows — coverage
// included — and validates; --id selects; the flag combinations that mix the
// two forms are invocation errors; and a walk that ran nothing exits 1 for
// test while validate reports it skipped at exit 0.
func TestGraphWalkCommands(t *testing.T) {
	configPath, _ := graphProject(t, graphFixture(t, "onboarding.graph.json"), map[string]string{
		"onboarding.rows.json": graphFixture(t, "onboarding.rows.json"),
	})
	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "test", "--config", configPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "passed: 3/3 graph rows matched their expectation") ||
		!strings.Contains(stdout, "- onboarding [passed]: 3/3") ||
		!strings.Contains(stdout, "  coverage: 7/13 derived probes are witnessed by a row") {
		t.Fatalf("the walk is the single-graph run relocated, coverage included: %q", stdout)
	}
	code, stdout, _ = runTest(t, []string{"experimental", "graph", "test", "--config", configPath, "--format", "json"}, "")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	var suite result.GraphSuite
	if err := json.Unmarshal([]byte(stdout), &suite); err != nil {
		t.Fatal(err)
	}
	if suite.Status != "passed" || suite.ConfigVersion != "2" || len(suite.Graphs) != 1 || len(suite.Graphs[0].Coverage) != 13 {
		t.Fatalf("suite = %+v", suite)
	}

	code, stdout, _ = runTest(t, []string{"experimental", "graph", "validate", "--config", configPath}, "")
	if code != 0 || !strings.Contains(stdout, "valid: 1/1 configured graphs passed every check") {
		t.Fatalf("the validate walk: exit=%d %q", code, stdout)
	}

	// --id selects one configured graph, and an unknown id lists what exists.
	code, _, _ = runTest(t, []string{"experimental", "graph", "test", "--id", "onboarding", "--config", configPath}, "")
	if code != 0 {
		t.Fatalf("--id selects the declared graph: exit=%d", code)
	}
	code, stdout, _ = runTest(t, []string{"experimental", "graph", "test", "--id", "ghost", "--config", configPath}, "")
	if code != result.ExitUnsupported || !strings.Contains(stdout, "onboarding") {
		t.Fatalf("an unknown id refusal lists configured ids: exit=%d %q", code, stdout)
	}

	// Mixing the two forms is an invocation error, stated for the flag used.
	graphPath := filepath.Join(filepath.Dir(configPath), "onboarding.graph.json")
	code, _, _ = runTest(t, []string{"experimental", "graph", "test", graphPath, "--id", "onboarding", "--config", configPath}, "")
	if code != result.ExitInvocation {
		t.Fatalf("--id with a path is an invocation error: exit=%d", code)
	}
	code, _, _ = runTest(t, []string{"experimental", "graph", "validate", graphPath, "--id", "onboarding", "--config", configPath}, "")
	if code != result.ExitInvocation {
		t.Fatalf("--id with a path is an invocation error on validate too: exit=%d", code)
	}
	code, _, _ = runTest(t, []string{"experimental", "graph", "test", "--rows", "x.json", "--config", configPath}, "")
	if code != result.ExitInvocation {
		t.Fatalf("--rows in the walk form is an invocation error: exit=%d", code)
	}

	// A project declaring no graphs: the test walk ran nothing and exits 1;
	// the validate walk checked nothing and that is not a failure.
	bare := writeProjectFixture(t, `{"configVersion":"1","packs":{"sanctions-screening":{"path":"sanctions-screening-0.1.0.pack.json"}}}`,
		map[string]string{"sanctions-screening-0.1.0.pack.json": graphFixture(t, "sanctions-screening-0.1.0.pack.json")})
	code, stdout, _ = runTest(t, []string{"experimental", "graph", "test", "--config", bare}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, "skipped: no graph row ran") {
		t.Fatalf("a green gate over nothing tested is not a pass: exit=%d %q", code, stdout)
	}
	code, stdout, _ = runTest(t, []string{"experimental", "graph", "validate", "--config", bare}, "")
	if code != 0 || !strings.Contains(stdout, "skipped: the configuration declares no graphs") {
		t.Fatalf("validating nothing is an answer, not a failure: exit=%d %q", code, stdout)
	}
}

// The graph inventory on the CLI, the twin of experimental_list_graphs: one
// function builds both payloads, so the two surfaces cannot disagree
// (ADR-0029). Listing reads each document's identity and does not validate it.
func TestGraphListInventory(t *testing.T) {
	configPath, _ := graphProject(t, graphFixture(t, "onboarding.graph.json"), map[string]string{
		"onboarding.rows.json": graphFixture(t, "onboarding.rows.json"),
	})

	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "list",
		"--config", configPath, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var inventory result.GraphInventory
	if err := json.Unmarshal([]byte(stdout), &inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.Status != "resolved" || !inventory.Experimental || inventory.Command != "experimental graph list" {
		t.Fatalf("inventory = %+v", inventory)
	}
	if len(inventory.Graphs) != 1 {
		t.Fatalf("graphs = %+v", inventory.Graphs)
	}
	row := inventory.Graphs[0]
	if row.ID != "onboarding" || row.GraphID != "vendor-onboarding-flow" || row.GraphVersion != "0.1.0" ||
		row.ResultNode != "onboarding" || row.NodeCount == nil || *row.NodeCount != 2 ||
		row.EdgeCount == nil || *row.EdgeCount != 1 || !row.RowsDeclared {
		t.Fatalf("row = %+v", row)
	}

	// The human rendering names the experimental surface and the two-name rule.
	code, human, stderr := runTest(t, []string{"experimental", "graph", "list", "--config", configPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(human, "EXPERIMENTAL SURFACE") || !strings.Contains(human, "onboarding (vendor-onboarding-flow 0.1.0)") {
		t.Fatalf("human = %q", human)
	}
	if !strings.Contains(human, "nodes: 2 · edges: 1") {
		t.Fatalf("present counts render as numbers: %q", human)
	}

	// A count that cannot be taken renders as unknown, never as zero: the
	// document below is decodable with nodes and edges in the wrong shapes.
	futureConfig, _ := graphProject(t, `{"formatVersion":"future","id":"t","version":"1","nodes":[],"edges":{},"result":7}`, map[string]string{
		"onboarding.rows.json": graphFixture(t, "onboarding.rows.json"),
	})
	code, human, stderr = runTest(t, []string{"experimental", "graph", "list", "--config", futureConfig}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(human, "nodes: unknown · edges: unknown") {
		t.Fatalf("an untakeable count renders unknown, never zero: %q", human)
	}

	// Correctly shaped and empty renders the honest zero, distinct from
	// unknown above.
	hollowConfig, _ := graphProject(t, `{"formatVersion":"1","id":"hollow","version":"0.0.1","nodes":{},"edges":[],"result":"none"}`, map[string]string{
		"onboarding.rows.json": graphFixture(t, "onboarding.rows.json"),
	})
	code, human, stderr = runTest(t, []string{"experimental", "graph", "list", "--config", hollowConfig}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(human, "nodes: 0 · edges: 0") {
		t.Fatalf("zero is an honest count: %q", human)
	}

	// A bad invocation names this exact command in the JSON envelope: the
	// invocation-error roster learned the list verb with the verb itself.
	code, stdout, _ = runTest(t, []string{"experimental", "graph", "list", "extra", "--format", "json"}, "")
	if code == 0 {
		t.Fatal("an extra operand is a bad invocation")
	}
	if !strings.Contains(stdout, `"command":"experimental graph list"`) {
		t.Fatalf("the envelope names the command exactly: %q", stdout)
	}
}
