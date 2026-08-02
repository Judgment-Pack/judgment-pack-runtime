package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// auditProject is one project that asks for a trail: the same pack and matrix
// oneGoodProject declares, under the configVersion the audit member requires.
func auditProject(t *testing.T) string {
	t.Helper()
	matrix := `{"matrixVersion":"1","cases":[
	  {"id":"hard-fail","facts":` + hardFailFacts + `,"evidenceAvailability":` + presentEvidence + `,"expectedDisposition":` + declineRedirect + `}
	]}`
	return writeProjectFixture(t, `{"configVersion":"3","audit":{"dir":"audit"},"packs":{"intake":{
	  "path":"packs/intake-0.1.0.pack.json",
	  "matrix":"packs/intake.matrix.json",
	  "expectedVersion":"0.1.0"
	}}}`, map[string]string{
		"packs/intake-0.1.0.pack.json": evaluatorPack(t),
		"packs/intake.matrix.json":     matrix,
	})
}

// writeDocument writes one input document beside a project and returns its path.
func writeDocument(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// auditRecords reads the trail a project's configuration named, decoding each
// line on its own.
func auditRecords(t *testing.T, configPath string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	records := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("undecodable record %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

// noAuditTrail asserts a project that asked for a trail was written nothing at
// all — not an empty file, which would say a run happened and left no record.
func noAuditTrail(t *testing.T, configPath string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)); err == nil {
		t.Fatalf("this command records nothing: %v", auditRecords(t, configPath))
	}
}

// A project that declares an audit directory gets one record per completed
// evaluation, whether the pack was named by id or by path: the configuration
// declares what is recorded, and how the caller spelled the pack is not part of
// that.
func TestEvaluateRecordsOneEvaluationPerRun(t *testing.T) {
	configPath := auditProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	evidence := writeDocument(t, "evidence.json", presentEvidence)

	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts, "--evidence", evidence, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var evaluation result.Evaluation
	if err := json.Unmarshal([]byte(stdout), &evaluation); err != nil {
		t.Fatal(err)
	}

	records := auditRecords(t, configPath)
	if len(records) != 1 {
		t.Fatalf("one completed evaluation is one record: %v", records)
	}
	record := records[0]
	if record["recordVersion"] != audit.RecordVersion || record["kind"] != audit.KindEvaluation ||
		record["surface"] != "experimental evaluate" || record["at"] == "" {
		t.Fatalf("record = %v", record)
	}
	pack := record["pack"].(map[string]any)
	if pack["id"] != evaluation.PackID || pack["version"] != evaluation.PackVersion ||
		pack["specVersion"] != evaluation.SpecVersion || !strings.HasPrefix(pack["digest"].(string), "sha256:") {
		t.Fatalf("pack = %v, payload = %+v", pack, evaluation)
	}
	inputs := record["inputs"].(map[string]any)
	if inputs["evidenceSupplied"] != true || inputs["facts"] == nil || inputs["evidence"] == nil {
		t.Fatalf("inputs = %v", inputs)
	}
	// The recorded disposition is the one the run reported, in the canonical
	// form §8.3 compares byte for byte — read off the line rather than off a
	// decoded value, because the bytes are what the record is for.
	canonical, err := evaluation.Disposition.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	line, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(line), `"disposition":`+string(canonical)) {
		t.Fatalf("the record must embed the run's own canonical disposition %s: %s", canonical, line)
	}

	// The same project, the same trail, a pack named by path: the second run
	// appends rather than replacing the first.
	packPath := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", packPath,
		"--config", configPath, "--facts", facts, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	records = auditRecords(t, configPath)
	if len(records) != 2 {
		t.Fatalf("a path-named pack in an audited project is recorded too: %v", records)
	}
	// The second run supplied no evidence document, and §8.2 makes that a
	// different evaluation from one supplied an empty document.
	inputs = records[1]["inputs"].(map[string]any)
	if inputs["evidenceSupplied"] != false || inputs["evidence"] != nil {
		t.Fatalf("inputs = %v", inputs)
	}
}

// A refused evaluation produces no disposition at all (§8.4), so it leaves no
// record either: a trail whose lines were sometimes results and sometimes
// failures would not be a trail of what this project decided.
func TestARefusedEvaluationLeavesNoRecord(t *testing.T) {
	configPath := auditProject(t)
	facts := writeDocument(t, "facts.json", `{"request":{"type":"data-access"}}`)
	evidence := writeDocument(t, "evidence.json", `{"not-a-requirement":"present"}`)

	code, _, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts, "--evidence", evidence}, "")
	if code == 0 || stderr == "" {
		t.Fatalf("this evaluation must be refused: exit=%d stderr=%q", code, stderr)
	}
	noAuditTrail(t, configPath)
}

// packs test runs the same evaluator over the same project and records nothing:
// a matrix row is a check on a pack, not a decision anyone took.
func TestTestRunsRecordNothing(t *testing.T) {
	configPath := auditProject(t)
	code, stdout, stderr := runTest(t, []string{"packs", "test", "--config", configPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	noAuditTrail(t, configPath)
}

// A record that cannot be written refuses the run, and the disposition is not
// reported: a project that asked to be told what its packs decided is not
// served by an answer it has no record of.
func TestAFailedRecordRefusesTheRun(t *testing.T) {
	configPath := auditProject(t)
	// Something else is at the declared directory's name, so no record can be
	// written under it. The failure is the operating system's on every platform
	// this runs on, rather than a permission bit Windows does not honor.
	if err := os.WriteFile(filepath.Join(filepath.Dir(configPath), "audit"), []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	facts := writeDocument(t, "facts.json", hardFailFacts)

	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts}, "")
	if code != result.ExitIO {
		t.Fatalf("exit=%d, want the input/output class %d (stderr %q)", code, result.ExitIO, stderr)
	}
	if !strings.Contains(stderr, audit.FailureMessage) {
		t.Fatalf("stderr = %q", stderr)
	}
	if strings.Contains(stdout, "disposition") || strings.Contains(stdout, "outcome") {
		t.Fatalf("no disposition is reported when its record could not be written: %q", stdout)
	}

	// The machine form carries the same code, through the one operational path.
	code, stdout, _ = runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts, "--format", "json"}, "")
	if code != result.ExitIO {
		t.Fatalf("exit=%d, want %d", code, result.ExitIO)
	}
	var refusal struct {
		Status      string `json:"status"`
		Diagnostics []struct {
			Code string `json:"code"`
		} `json:"diagnostics"`
		Disposition any `json:"disposition"`
	}
	if err := json.Unmarshal([]byte(stdout), &refusal); err != nil {
		t.Fatalf("undecodable refusal %q: %v", stdout, err)
	}
	if refusal.Status != "error" || len(refusal.Diagnostics) != 1 ||
		refusal.Diagnostics[0].Code != audit.FailureCode || refusal.Disposition != nil {
		t.Fatalf("refusal = %+v (%q)", refusal, stdout)
	}
}

// A configuration that is there and cannot be read refuses the evaluation, on
// either form of the pack argument. It is a deliberate change of behavior for a
// path-named pack: a project whose configuration is broken may be a project
// that asked to be recorded, and evaluating it unrecorded answers a question
// nobody can afterward show was asked.
func TestABrokenConfigurationRefusesAPathNamedEvaluation(t *testing.T) {
	configPath := writeProjectFixture(t, `{"configVersion":"3","packs":{}}`, map[string]string{
		"pack.json": evaluatorPack(t),
	})
	facts := writeDocument(t, "facts.json", hardFailFacts)

	code, _, stderr := runTest(t, []string{"experimental", "evaluate", filepath.Join(filepath.Dir(configPath), "pack.json"),
		"--config", configPath, "--facts", facts}, "")
	if code != result.ExitInvalid {
		t.Fatalf("exit=%d, want the invalid class %d (stderr %q)", code, result.ExitInvalid, stderr)
	}
	if !strings.Contains(stderr, "jpack.json") {
		t.Fatalf("the refusal names the configuration it could not read: %q", stderr)
	}

	// A project with no configuration at all is unaffected: not using the
	// convention is an ordinary project, and nothing about it is recorded.
	packPath := writeFixture(t, []byte(evaluatorPack(t)))
	code, _, stderr = runTest(t, []string{"experimental", "evaluate", packPath,
		"--config", filepath.Join(t.TempDir(), "absent.json"), "--facts", facts}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
}

// "There is a configuration here" is decided by presence, not by the file being
// a readable regular one. A symlinked jpack.json is demonstrably there and
// demonstrably will not load; reading it as "this project does not use the
// convention" would evaluate unrecorded, exit 0, and leave the project that
// asked to be recorded with nothing.
func TestAConfigurationThatIsThereButUnloadableRefusesRatherThanBeingIgnored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is privileged on Windows")
	}
	configPath := auditProject(t)
	linked := filepath.Join(filepath.Dir(configPath), "jpack.link.json")
	if err := os.Symlink(configPath, linked); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	facts := writeDocument(t, "facts.json", hardFailFacts)
	packPath := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")

	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", packPath,
		"--config", linked, "--facts", facts}, "")
	if code == 0 {
		t.Fatalf("a configuration that is there and will not load must refuse the run: stdout=%q", stdout)
	}
	if !strings.Contains(stderr, "bounded regular file") {
		t.Fatalf("the refusal is the project convention's own: %q", stderr)
	}
	noAuditTrail(t, configPath)
}

// packs validate reports the containment of the one declared path no pack entry
// owns. Without it a project's CI gate passes and every later evaluation fails
// with a message that names neither the member nor the escape.
func TestPacksValidateChecksTheAuditDirectory(t *testing.T) {
	configPath := auditProject(t)
	code, stdout, stderr := runTest(t, []string{"packs", "validate", "--config", configPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	// A directory that is not there yet is contained: the first record makes it.
	if !strings.Contains(stdout, "audit-dir-inside-root: passed") {
		t.Fatalf("the check must be reported: %q", stdout)
	}

	escaping := writeProjectFixture(t, `{"configVersion":"3","audit":{"dir":"../outside"},"packs":{"intake":{
	  "path":"packs/intake-0.1.0.pack.json"
	}}}`, map[string]string{"packs/intake-0.1.0.pack.json": evaluatorPack(t)})
	code, stdout, stderr = runTest(t, []string{"packs", "validate", "--config", escaping, "--format", "json"}, "")
	if code != result.ExitInvalid || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var report result.PackValidation
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "invalid" || len(report.Checks) != 1 ||
		report.Checks[0].Name != "audit-dir-inside-root" || report.Checks[0].Status != result.PackCheckFailed {
		t.Fatalf("report = %+v", report)
	}
	// Every pack still passed: the defect is the configuration's, and the
	// summary counts packs, so the headline must not read as a failed pack.
	if report.Summary.Failed != 0 || report.Summary.Passed != 1 {
		t.Fatalf("summary = %+v", report.Summary)
	}
	code, stdout, _ = runTest(t, []string{"packs", "validate", "--config", escaping}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, "a check on the configuration itself failed") {
		t.Fatalf("human output = %q", stdout)
	}

	// A project that declares no audit member has no such check to report.
	plain := oneGoodProject(t)
	code, stdout, _ = runTest(t, []string{"packs", "validate", "--config", plain}, "")
	if code != 0 || strings.Contains(stdout, "audit-dir-inside-root") {
		t.Fatalf("exit=%d stdout=%q", code, stdout)
	}
}

// The check resolves the declared directory's final component, because that
// component becomes an intermediate one of everything written beneath it. A
// symlinked audit directory pointing out of the project is the escape every
// evaluation would refuse, and the gate has to refuse it too.
func TestPacksValidateResolvesASymlinkedAuditDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory symlink creation is privileged on Windows")
	}
	configPath := auditProject(t)
	root := filepath.Dir(configPath)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "audit")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	code, stdout, stderr := runTest(t, []string{"packs", "validate", "--config", configPath, "--format", "json"}, "")
	if code != result.ExitInvalid || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var report result.PackValidation
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Checks) != 1 || report.Checks[0].Status != result.PackCheckFailed ||
		!strings.Contains(report.Checks[0].Detail, "outside") {
		t.Fatalf("checks = %+v", report.Checks)
	}
	// And the evaluation refuses it too, so the two agree about one project.
	facts := writeDocument(t, "facts.json", hardFailFacts)
	code, _, stderr = runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts}, "")
	if code != result.ExitIO || !strings.Contains(stderr, audit.FailureMessage) {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}

	// An inward symlink is an ordinary project layout and passes both.
	if err := os.Remove(filepath.Join(root, "audit")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "records"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("records", filepath.Join(root, "audit")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	code, stdout, stderr = runTest(t, []string{"packs", "validate", "--config", configPath}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "audit-dir-inside-root: passed") {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
}

// Every argument-shape refusal precedes the filesystem, so a broken
// configuration cannot answer in place of the mistake the caller made. The
// pack-id form is checked too: resolving a decision id is a filesystem
// operation, and it must not outrank a missing required argument either.
func TestArgumentRefusalsPrecedeTheConfiguration(t *testing.T) {
	broken := writeProjectFixture(t, `{"configVersion":"4","packs":{}}`, map[string]string{
		"pack.json": evaluatorPack(t),
	})
	packPath := filepath.Join(filepath.Dir(broken), "pack.json")
	facts := writeDocument(t, "facts.json", hardFailFacts)

	for name, invocation := range map[string]struct {
		args []string
		want string
	}{
		"a missing facts document, pack by path": {
			args: []string{"experimental", "evaluate", packPath, "--config", broken},
			want: "--facts is required",
		},
		"a missing facts document, pack by id": {
			args: []string{"experimental", "evaluate", "--pack-id", "intake", "--config", broken},
			want: "--facts is required",
		},
		"an evidence document on standard input": {
			args: []string{"experimental", "evaluate", packPath, "--config", broken, "--facts", facts, "--evidence", "-"},
			want: "cannot be standard input",
		},
		"a URL input": {
			args: []string{"experimental", "evaluate", "https://example.invalid/p.json", "--config", broken, "--facts", facts},
			want: "URL and remote filesystem inputs are not supported",
		},
	} {
		t.Run(name, func(t *testing.T) {
			code, _, stderr := runTest(t, invocation.args, "")
			if code != result.ExitInvocation {
				t.Fatalf("exit=%d, want the invocation class %d (stderr %q)", code, result.ExitInvocation, stderr)
			}
			if !strings.Contains(stderr, invocation.want) {
				t.Fatalf("stderr = %q, want the argument refusal %q", stderr, invocation.want)
			}
			if strings.Contains(stderr, "configVersion") {
				t.Fatalf("the configuration must not answer in place of the argument mistake: %q", stderr)
			}
		})
	}

	// With the arguments in order, the configuration is reached and refuses. An
	// unsupported version is reported on standard output, as every unsupported
	// refusal is.
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", packPath, "--config", broken, "--facts", facts}, "")
	if code != result.ExitUnsupported || !strings.Contains(stdout, "configVersion") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

// The graph surface records each node and the composite, and its test verb —
// the same evaluator, over the same project, from the same configuration —
// records nothing.
func TestGraphEvaluateRecordsEveryNodeAndTheComposite(t *testing.T) {
	config := strings.Replace(graphFixture(t, "jpack.json"),
		`"configVersion": "2",`, `"configVersion": "3",`+"\n"+`  "audit": {"dir": "audit"},`, 1)
	files := map[string]string{
		"sanctions-screening-0.1.0.pack.json": graphFixture(t, "sanctions-screening-0.1.0.pack.json"),
		"vendor-onboarding-0.1.0.pack.json":   graphFixture(t, "vendor-onboarding-0.1.0.pack.json"),
		"onboarding.graph.json":               graphFixture(t, "onboarding.graph.json"),
		"onboarding.rows.json":                graphFixture(t, "onboarding.rows.json"),
	}
	configPath := writeProjectFixture(t, config, files)
	graphPath := filepath.Join(filepath.Dir(configPath), "onboarding.graph.json")
	inputs := writeGraphInputs(t, graphHappyInputs)

	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
		"--config", configPath, "--inputs", inputs, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var composite result.GraphEvaluation
	if err := json.Unmarshal([]byte(stdout), &composite); err != nil {
		t.Fatal(err)
	}

	records := auditRecords(t, configPath)
	if len(records) != len(composite.Nodes)+1 {
		t.Fatalf("every node plus the composite: %d records for %d nodes", len(records), len(composite.Nodes))
	}
	for index, node := range composite.Nodes {
		record := records[index]
		graph := record["graph"].(map[string]any)
		if record["kind"] != audit.KindEvaluation || record["surface"] != "experimental graph evaluate" {
			t.Fatalf("node record = %v", record)
		}
		if graph["node"] != node.Node || graph["id"] != composite.GraphID || graph["version"] != composite.GraphVersion {
			t.Fatalf("graph = %v, node = %+v", graph, node)
		}
		if record["inputs"].(map[string]any)["facts"] == nil {
			t.Fatalf("a node record carries the document it was evaluated against: %v", record)
		}
	}
	last := records[len(records)-1]
	graph := last["graph"].(map[string]any)
	if last["kind"] != audit.KindGraphComposite || graph["resultNode"] != composite.ResultNode ||
		last["inputs"] != nil || last["pack"] != nil {
		t.Fatalf("composite record = %v", last)
	}
	// The graph's own bytes, digested: a graph's id and version are as mutable
	// as a pack's, and two compositions can carry both unchanged.
	document, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		named := record["graph"].(map[string]any)
		if named["digest"] != audit.Digest(document) || named["formatVersion"] != composite.FormatVersion {
			t.Fatalf("graph provenance = %v, want the document's own digest and format", named)
		}
	}
	// One invocation, one run id, and the composite is what marks it finished.
	run := records[0]["run"]
	for _, record := range records {
		if record["run"] != run {
			t.Fatalf("one run writes one id: %v vs %v", record["run"], run)
		}
	}
	if last["run"] != run {
		t.Fatalf("the composite commits the run its nodes belong to: %v", last["run"])
	}

	// The same graph, the same configuration, run as a matrix: nothing recorded.
	rowsPath := filepath.Join(filepath.Dir(configPath), "onboarding.rows.json")
	code, stdout, stderr = runTest(t, []string{"experimental", "graph", "test", graphPath,
		"--config", configPath, "--rows", rowsPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if grown := auditRecords(t, configPath); len(grown) != len(records) {
		t.Fatalf("a matrix run records nothing: %d records, was %d", len(grown), len(records))
	}
}

// A graph run refused at any node records nothing at all — not the nodes that
// ran before it. A run's records are held until it has a composite, so "a
// refused evaluation records nothing" is true of a composition too, and no line
// in a trail belongs to a run that never finished.
func TestARefusedGraphRunLeavesNoPartialTrail(t *testing.T) {
	config := strings.Replace(graphFixture(t, "jpack.json"),
		`"configVersion": "2",`, `"configVersion": "3",`+"\n"+`  "audit": {"dir": "audit"},`, 1)
	configPath := writeProjectFixture(t, config, map[string]string{
		"sanctions-screening-0.1.0.pack.json": graphFixture(t, "sanctions-screening-0.1.0.pack.json"),
		"vendor-onboarding-0.1.0.pack.json":   graphFixture(t, "vendor-onboarding-0.1.0.pack.json"),
		"onboarding.graph.json":               graphFixture(t, "onboarding.graph.json"),
	})
	graphPath := filepath.Join(filepath.Dir(configPath), "onboarding.graph.json")
	// The first node evaluates; the second is refused for an evidence key its
	// pack does not declare.
	inputs := writeGraphInputs(t, `{"screening":{"facts":{"screening":{"matches":"0"}},"evidence":{"screening-record":"present"}},
	  "onboarding":{"evidence":{"not-a-requirement":"present"}}}`)

	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
		"--config", configPath, "--inputs", inputs}, "")
	if code == 0 {
		t.Fatalf("this run must be refused: stdout=%q", stdout)
	}
	if !strings.Contains(stderr, "onboarding") {
		t.Fatalf("the refusal names the node: %q", stderr)
	}
	noAuditTrail(t, configPath)
}
