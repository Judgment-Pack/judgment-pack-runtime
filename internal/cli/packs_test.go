package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// evaluatorPack is the conformant 0.2.0-draft fixture the evaluator's own tests
// use: id, version 0.1.0, three evidence requirements.
func evaluatorPack(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const (
	hardFailFacts   = `{"request":{"type":"data-access","completeness":"complete","appropriateness":"hard-fail","embargoedInformationToUnauthorizedRecipients":false}}`
	presentEvidence = `{"intake-form":"present","sponsor-endorsement":"present"}`
	declineRedirect = `{"kind":"outcome","outcomeId":"decline-redirect","reasons":[],"handoff":{"state":"none"}}`
)

// writeProjectFixture lays out one project and returns its configuration path.
func writeProjectFixture(t *testing.T, config string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath
}

// oneGoodProject is a project whose single pack validates and whose matrix
// passes: the shape the CI one-liner is meant to produce.
func oneGoodProject(t *testing.T) string {
	t.Helper()
	matrix := `{"matrixVersion":"1","cases":[
	  {"id":"hard-fail","facts":` + hardFailFacts + `,"evidenceAvailability":` + presentEvidence + `,"expectedDisposition":` + declineRedirect + `},
	  {"id":"undeclared-key","facts":{"request":{"type":"data-access"}},"evidenceAvailability":{"not-a-requirement":"present"},"expectedErrorClass":"malformed-input","expectedErrorPhase":"preflight"}
	]}`
	return writeProjectFixture(t, `{"configVersion":"1","packs":{"intake":{
	  "path":"packs/intake-0.1.0.pack.json",
	  "matrix":"packs/intake.matrix.json",
	  "description":"Triage an inbound data-access request",
	  "expectedVersion":"0.1.0",
	  "facts":{"/request/type":{"source":"Snowflake ANALYTICS.REQUESTS","hint":"request_kind, lowercased"}},
	  "evidence":{"intake-form":{"source":"SharePoint /Intake"}}
	}}}`, map[string]string{
		"packs/intake-0.1.0.pack.json": evaluatorPack(t),
		"packs/intake.matrix.json":     matrix,
	})
}

// packs list reports the project's decision id beside the pack document's own
// identity, and the two are never conflated.
func TestPacksListReportsBothNames(t *testing.T) {
	configPath := oneGoodProject(t)

	code, stdout, stderr := runTest(t, []string{"packs", "list", "--config", configPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	for _, required := range []string{"intake", "data-request-intake-triage", "0.1.0", "matches", "Triage an inbound"} {
		if !strings.Contains(stdout, required) {
			t.Fatalf("human output must carry %q: %q", required, stdout)
		}
	}

	code, stdout, stderr = runTest(t, []string{"packs", "list", "--config", configPath, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var inventory result.PackInventory
	if err := json.Unmarshal([]byte(stdout), &inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.Command != "packs list" || inventory.Kind != result.ProjectKind || inventory.ConfigVersion != project.ConfigVersion {
		t.Fatalf("inventory = %+v", inventory)
	}
	if len(inventory.Packs) != 1 {
		t.Fatalf("packs = %+v", inventory.Packs)
	}
	entry := inventory.Packs[0]
	if entry.ID != "intake" || entry.PackID != "https://example.invalid/judgment-packs/data-request-intake-triage" || entry.PackVersion != "0.1.0" {
		t.Fatalf("the two names are two members: %+v", entry)
	}
	if !entry.Matrix || entry.ExpectedVersionStatus != result.PackVersionMatches {
		t.Fatalf("entry = %+v", entry)
	}
	if len(entry.EvidenceRequirements) != 3 || entry.EvidenceRequirements[0] != "intake-form" {
		t.Fatalf("the inventory carries the pack's declared evidence ids: %v", entry.EvidenceRequirements)
	}
	if len(entry.Facts) != 1 || entry.Facts[0].Key != "/request/type" || entry.Facts[0].Source == "" {
		t.Fatalf("the hints travel with the inventory: %+v", entry.Facts)
	}

	// The environment variable selects the same configuration the flag does.
	t.Setenv(project.ConfigEnv, configPath)
	code, fromEnv, stderr := runTest(t, []string{"packs", "list", "--format", "json"}, "")
	if code != 0 || stderr != "" || fromEnv != stdout {
		t.Fatalf("%s must select the same configuration --config does: exit=%d stderr=%q", project.ConfigEnv, code, stderr)
	}
}

// A missing configuration is a read failure on the CLI, and it says where the
// runtime looked. The MCP surface answers empty instead, which is asserted there.
func TestPacksCommandsReportAMissingConfiguration(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "jpack.json")
	code, stdout, _ := runTest(t, []string{"packs", "list", "--config", missing, "--format", "json"}, "")
	if code != result.ExitIO {
		t.Fatalf("exit=%d, want the IO class", code)
	}
	assertDiagnosticCode(t, stdout, "JPS-PROJECT-CONFIG-READ")
}

// packs validate reports every check and exits on the verdict: 0 when every
// configured pack passed, 1 when any did not.
func TestPacksValidateReportsEveryCheckAndExitsOnTheVerdict(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"packs", "validate", "--config", oneGoodProject(t), "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, first(stdout, 400))
	}
	var report result.PackValidation
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "valid" || report.Summary.Passed != 1 || report.Summary.Total != 1 {
		t.Fatalf("report = %+v", report.Summary)
	}
	names := []string{}
	for _, check := range report.Packs[0].Checks {
		names = append(names, check.Name)
		if check.Status == result.PackCheckFailed {
			t.Fatalf("no check may fail on a good project: %+v", check)
		}
	}
	want := []string{"path-inside-root", "document-validation", "expected-version", "filename", "hint-keys", "matrix"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("checks = %v, want %v", names, want)
	}

	// A pack that does not validate fails the command, and the detail names the
	// verdict and the first diagnostic rather than only saying "invalid".
	broken := writeProjectFixture(t, `{"configVersion":"1","packs":{"broken":{"path":"packs/broken.json"}}}`,
		map[string]string{"packs/broken.json": `{"specVersion":"0.2.0-draft"}`})
	code, stdout, _ = runTest(t, []string{"packs", "validate", "--config", broken, "--format", "json"}, "")
	if code != result.ExitInvalid {
		t.Fatalf("exit=%d, want the invalid class", code)
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "invalid" || report.Summary.Failed != 1 {
		t.Fatalf("report = %+v", report.Summary)
	}
	if !strings.Contains(report.Packs[0].Checks[1].Detail, "JPS-STRUCTURE-REQUIRED-MEMBER") {
		t.Fatalf("the detail must carry the first diagnostic: %+v", report.Packs[0].Checks[1])
	}

	// --id runs one pack, and an unknown id is refused rather than checking nothing.
	code, _, _ = runTest(t, []string{"packs", "validate", "--config", oneGoodProject(t), "--id", "intake"}, "")
	if code != result.ExitSuccess {
		t.Fatalf("--id on a known pack: exit=%d", code)
	}
	code, stdout, _ = runTest(t, []string{"packs", "validate", "--config", oneGoodProject(t), "--id", "nope", "--format", "json"}, "")
	if code != result.ExitUnsupported {
		t.Fatalf("an unknown --id must be unsupported, got exit=%d", code)
	}
	assertDiagnosticCode(t, stdout, "JPS-PROJECT-UNKNOWN-PACK")
}

// packs test byte-diffs each row's canonical §8.3 disposition. Both directions
// are pinned: a matching matrix exits 0, and one changed expectation exits 1 with
// both byte sequences in the report.
func TestPacksTestByteDiffsDispositionsAndExitsOnAnyMismatch(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"packs", "test", "--config", oneGoodProject(t), "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, first(stdout, 400))
	}
	var run result.PackTest
	if err := json.Unmarshal([]byte(stdout), &run); err != nil {
		t.Fatal(err)
	}
	if run.Status != "passed" || run.Summary.Total != 2 || run.Summary.Passed != 2 {
		t.Fatalf("run = %+v", run.Summary)
	}
	if !run.Experimental || run.ConformanceClaimReference != result.EvaluationClaimReference || run.Label != result.PackMatrixLabel {
		t.Fatalf("the payload must name the surface and point at the claim document: %+v", run)
	}
	if run.Packs[0].Rows[0].Actual == "" || !strings.HasPrefix(run.Packs[0].Rows[0].Actual, `{"handoff":`) {
		t.Fatalf("each row reports the canonical bytes it produced: %+v", run.Packs[0].Rows[0])
	}

	// The human surface leads with the label, not with the pass count.
	code, stdout, stderr = runTest(t, []string{"packs", "test", "--config", oneGoodProject(t)}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	label := strings.Index(stdout, result.PackMatrixLabel)
	count := strings.Index(stdout, "passed: 2/2")
	if label < 0 || count < 0 || label > count {
		t.Fatalf("the label must precede the count: %q", stdout)
	}

	// One byte of one expectation changed is a mismatch and exit 1.
	drifted := `{"cases":[{"id":"hard-fail","facts":` + hardFailFacts + `,"evidenceAvailability":` + presentEvidence +
		`,"expectedDisposition":{"kind":"outcome","outcomeId":"accept-and-fulfill","reasons":[],"handoff":{"state":"none"}}}]}`
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{"intake":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
		map[string]string{"packs/a.json": evaluatorPack(t), "packs/a.matrix.json": drifted})
	code, stdout, _ = runTest(t, []string{"packs", "test", "--config", configPath, "--format", "json"}, "")
	if code != result.ExitInvalid {
		t.Fatalf("a mismatching row must exit 1, got exit=%d", code)
	}
	if err := json.Unmarshal([]byte(stdout), &run); err != nil {
		t.Fatal(err)
	}
	row := run.Packs[0].Rows[0]
	if run.Status != "mismatch" || row.Status != "mismatch" || row.Expected == row.Actual {
		t.Fatalf("the report must carry both byte sequences: %+v", row)
	}
}

// A project whose packs declare no matrix has tested nothing, and packs test
// says so and exits non-zero. The per-pack row alone would not: a CI gate reads
// the exit code, and a green build over zero rows is the failure this prevents.
func TestPacksTestDoesNotReportACleanRunOverZeroRows(t *testing.T) {
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{"intake":{"path":"packs/intake-0.1.0.pack.json"}}}`,
		map[string]string{"packs/intake-0.1.0.pack.json": evaluatorPack(t)})

	code, stdout, stderr := runTest(t, []string{"packs", "test", "--config", configPath, "--format", "json"}, "")
	if code != result.ExitInvalid {
		t.Fatalf("a run that ran no row must exit non-zero, got exit=%d stderr=%q", code, stderr)
	}
	var run result.PackTest
	if err := json.Unmarshal([]byte(stdout), &run); err != nil {
		t.Fatal(err)
	}
	if run.Status != "skipped" || run.Summary.Total != 0 || run.Packs[0].Status != "skipped" {
		t.Fatalf("run = %+v", run)
	}
	code, stdout, _ = runTest(t, []string{"packs", "test", "--config", configPath}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, "skipped: no matrix row ran") {
		t.Fatalf("the human surface must say no row ran: exit=%d %q", code, stdout)
	}

	// A project that configures no pack at all is the same failure reached
	// earlier: the schema requires at least one, so an empty packs object never
	// becomes a run. Both refusals matter and neither replaces the other — the
	// schema stops a configuration nobody can have meant, and the runner's
	// zero-row demotion stops a green result over nothing however the selection
	// came to be empty.
	emptyConfig := writeProjectFixture(t, `{"configVersion":"1","packs":{}}`, nil)
	code, stdout, stderr = runTest(t, []string{"packs", "test", "--config", emptyConfig, "--format", "json"}, "")
	if code == result.ExitSuccess {
		t.Fatalf("a project declaring no packs must not exit 0: stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "JPS-PROJECT-CONFIG-SCHEMA") {
		t.Fatalf("an empty packs object must be refused by the schema: stdout=%q stderr=%q", stdout, stderr)
	}
	// The same configuration is refused identically by the surfaces that read it
	// for anything else, so an empty project is never half-usable.
	if code, _, _ := runTest(t, []string{"packs", "validate", "--config", emptyConfig, "--format", "json"}, ""); code == result.ExitSuccess {
		t.Fatal("packs validate must refuse a project declaring no packs")
	}
	if code, _, _ := runTest(t, []string{"packs", "list", "--config", emptyConfig, "--format", "json"}, ""); code == result.ExitSuccess {
		t.Fatal("packs list must refuse a project declaring no packs")
	}
}

// One byte-limit boundary for every surface, and one reader behind it.
//
// A pack reached by decision id is read through the project's own directory
// handle rather than handed back as a path for the generic reader to open, so
// the oversized case has to be reported by that read and not by the read it
// replaced. It is a §8.2 preflight condition the engine classes — pack-not-
// conformant — and not an unclassified read failure. The MCP surface asserts the
// same thing about the same file; this is the shell half of that pair.
func TestAnOversizedPackReachedByIdIsClassedLikeAnyOtherOversizedPack(t *testing.T) {
	// One byte past the documented hard limit is the whole of what makes it
	// oversized; the content beyond the opening brace never has to be JSON.
	bulk := string(append(append([]byte(`{"pad":"`), bytes.Repeat([]byte("x"), int(carrier.HardMaxBytes))...), []byte(`"}`)...))
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{"huge":{"path":"packs/huge.json"}}}`,
		map[string]string{"packs/huge.json": bulk})
	facts := filepath.Join(filepath.Dir(configPath), "facts.json")
	if err := os.WriteFile(facts, []byte(`{"request":{"type":"data-access"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	packPath := filepath.Join(filepath.Dir(configPath), "packs", "huge.json")
	byID, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "huge", "--config", configPath, "--facts", facts, "--format", "json"}, "")
	if stderr != "" {
		t.Fatalf("exit=%d stderr=%q", byID, stderr)
	}
	byPath, viaPath, _ := runTest(t, []string{"experimental", "evaluate", packPath, "--facts", facts, "--format", "json"}, "")

	// The same file, named two ways, is refused identically. Naming it by id must
	// not change the class, the code, or the exit: the id is a way of finding the
	// pack, not a different contract for reading it.
	if byID != byPath || stdout != viaPath {
		t.Fatalf("by id (exit=%d) and by path (exit=%d) must agree:\n  id:   %s\n  path: %s", byID, byPath, first(stdout, 300), first(viaPath, 300))
	}

	var output struct {
		EvaluationError *struct {
			Class string `json:"class"`
			Phase string `json:"phase"`
		} `json:"evaluationError"`
		Disposition any `json:"disposition"`
	}
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output.EvaluationError == nil || output.EvaluationError.Class != result.ClassPackNotConformant || output.EvaluationError.Phase != "preflight" {
		t.Fatalf("an oversized pack reached by id is the classed preflight condition, not an unclassified read failure: %q", first(stdout, 400))
	}
	if output.Disposition != nil {
		t.Fatalf("an evaluation error carries no disposition: %q", first(stdout, 400))
	}
	assertDiagnosticCode(t, stdout, "JPS-RESOURCE-INPUT-BYTE-LIMIT")
}

// A hint key nothing else ever resolves is checked against the pack document,
// and packs validate fails on one the document does not have.
func TestPacksValidateChecksHintKeysAgainstTheDocument(t *testing.T) {
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{"intake":{
	  "path":"packs/intake-0.1.0.pack.json",
	  "facts":{"/nowhere/at/all":{"source":"Snowflake ANALYTICS.REQUESTS"}},
	  "evidence":{"intake-frm":{"source":"SharePoint /Intake"}}
	}}}`, map[string]string{"packs/intake-0.1.0.pack.json": evaluatorPack(t)})

	code, stdout, _ := runTest(t, []string{"packs", "validate", "--config", configPath, "--format", "json"}, "")
	if code != result.ExitInvalid {
		t.Fatalf("a misspelled hint key must fail the command, got exit=%d", code)
	}
	var report result.PackValidation
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	for _, check := range report.Packs[0].Checks {
		if check.Name != project.CheckHintKeys {
			continue
		}
		if check.Status != result.PackCheckFailed || !strings.Contains(check.Detail, `"intake-frm"`) {
			t.Fatalf("the hint-key check must fail and name the key: %+v", check)
		}
		return
	}
	t.Fatalf("the report has no hint-key check: %+v", report.Packs[0].Checks)
}

// packs schema mirrors spec schema, including its two write guards, and prints
// the exact embedded bytes.
func TestPacksSchemaMirrorsSpecSchema(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"packs", "schema"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, project.SchemaID) {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}

	code, printed, stderr := runTest(t, []string{"packs", "schema", "--write", "-"}, "")
	if code != 0 || stderr != "" || printed != string(project.Schema()) {
		t.Fatalf("--write - must print the exact embedded bytes: exit=%d stderr=%q", code, stderr)
	}

	target := filepath.Join(t.TempDir(), "jpack.schema.json")
	code, stdout, _ = runTest(t, []string{"packs", "schema", "--write", target}, "")
	if code != 0 || !strings.Contains(stdout, "written:") {
		t.Fatalf("exit=%d stdout=%q", code, stdout)
	}
	written, err := os.ReadFile(target)
	if err != nil || string(written) != string(project.Schema()) {
		t.Fatalf("written bytes differ from the embedded schema: %v", err)
	}
	if code, _, _ = runTest(t, []string{"packs", "schema", "--write", target}, ""); code != result.ExitIO {
		t.Fatalf("a re-write must be refused, got exit=%d", code)
	}
	code, stdout, _ = runTest(t, []string{"packs", "schema", "--write", "-", "--format", "json"}, "")
	if code != result.ExitInvocation {
		t.Fatalf("--write - with --format json must be an invocation error, got exit=%d", code)
	}
	assertDiagnosticCode(t, stdout, "JPS-INVOCATION-STDOUT")
}

// experimental evaluate takes the pack one way or the other, never both and
// never neither, and --pack-id resolves through the configuration.
func TestExperimentalEvaluateResolvesAPackByDecisionId(t *testing.T) {
	configPath := oneGoodProject(t)
	facts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(facts, []byte(hardFailFacts), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence := filepath.Join(filepath.Dir(facts), "evidence.json")
	if err := os.WriteFile(evidence, []byte(presentEvidence), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath,
		"--facts", facts, "--evidence", evidence, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, first(stdout, 400))
	}
	var output result.Evaluation
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output.Disposition.Kind != "outcome" || output.Disposition.OutcomeID != "decline-redirect" {
		t.Fatalf("disposition = %+v", output.Disposition)
	}
	if output.PackID != "https://example.invalid/judgment-packs/data-request-intake-triage" || output.PackVersion != "0.1.0" {
		t.Fatalf("the payload echoes the evaluated document's own identity: %+v", output)
	}

	// Naming the same pack by path produces the same disposition and the same echo.
	packPath := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")
	code, byPath, stderr := runTest(t, []string{"experimental", "evaluate", packPath, "--facts", facts, "--evidence", evidence, "--format", "json"}, "")
	if code != 0 || stderr != "" || byPath != stdout {
		t.Fatalf("by-id and by-path must agree byte for byte:\n id=%s\npath=%s", stdout, byPath)
	}

	for name, testCase := range map[string]struct {
		args []string
		code string
	}{
		"both a path and an id": {[]string{"experimental", "evaluate", packPath, "--pack-id", "intake", "--config", configPath, "--facts", facts, "--format", "json"}, "JPS-INVOCATION-PACK-ID"},
		"neither":               {[]string{"experimental", "evaluate", "--facts", facts, "--format", "json"}, "JPS-INVOCATION-PACK-ID"},
		"an unknown id":         {[]string{"experimental", "evaluate", "--pack-id", "nope", "--config", configPath, "--facts", facts, "--format", "json"}, "JPS-PROJECT-UNKNOWN-PACK"},
	} {
		t.Run(name, func(t *testing.T) {
			code, stdout, _ := runTest(t, testCase.args, "")
			if code == result.ExitSuccess {
				t.Fatalf("this invocation must be refused: %q", stdout)
			}
			assertDiagnosticCode(t, stdout, testCase.code)
		})
	}
}

// Every surface that reaches a pack through the configuration applies the whole
// containment rule, including the half a lexical check cannot see. A path that
// leaves the project through a symlinked directory component is refused by each
// of them, and --pack-id — which resolves a path and hands it to the ordinary
// bounded read — is refused exactly as the others are rather than reading the
// out-of-root document and evaluating it.
func TestEverySurfaceRefusesAPathThatEscapesThroughASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the symlinked-directory escape is a POSIX filesystem behavior")
	}
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	// A real, conformant pack outside the project: the refusal is about where the
	// file is, and an evaluation of it would otherwise succeed.
	if err := os.WriteFile(filepath.Join(outside, "secret.pack.json"), []byte(evaluatorPack(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "project")
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "packs", "escape")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	if err := os.WriteFile(configPath, []byte(`{"configVersion":"1","packs":{"escaped":{"path":"packs/escape/secret.pack.json"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	facts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(facts, []byte(hardFailFacts), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, _ := runTest(t, []string{"experimental", "evaluate", "--pack-id", "escaped", "--config", configPath,
		"--facts", facts, "--format", "json"}, "")
	if code == result.ExitSuccess {
		t.Fatalf("an escaping path must never be evaluated: %q", stdout)
	}
	assertDiagnosticCode(t, stdout, "JPS-PROJECT-PACK-PATH")
	if strings.Contains(stdout, "disposition") {
		t.Fatalf("a refused resolution produces no disposition: %q", stdout)
	}

	// packs validate attributes the failure to the containment check itself, and
	// packs list reports the entry with the reason and no invented identity.
	code, stdout, _ = runTest(t, []string{"packs", "validate", "--config", configPath, "--format", "json"}, "")
	if code != result.ExitInvalid {
		t.Fatalf("exit=%d, want the invalid class", code)
	}
	var report result.PackValidation
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	containment := report.Packs[0].Checks[0]
	if containment.Name != project.CheckPathInsideRoot || containment.Status != result.PackCheckFailed {
		t.Fatalf("the containment check is the one that failed: %+v", report.Packs[0].Checks)
	}
	code, stdout, _ = runTest(t, []string{"packs", "list", "--config", configPath}, "")
	if code != result.ExitSuccess || !strings.Contains(stdout, "document unreadable") {
		t.Fatalf("the inventory lists the entry with the reason: exit=%d %q", code, stdout)
	}
	if strings.Contains(stdout, "data-request-intake-triage") {
		t.Fatalf("no surface may report an identity read from outside the project: %q", stdout)
	}
}

// The identity echo is on every evaluation payload, not only the ones that came
// through a project, and it is read off the document rather than from anywhere
// else. A row that produced no disposition carries no echo: a refusal — before
// the pack was admitted or after, as a reached §10 limit is — leaves no
// evaluation to read an identity off.
func TestEveryEvaluationPayloadEchoesTheEvaluatedPacksIdentity(t *testing.T) {
	pack := filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json")
	facts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(facts, []byte(hardFailFacts), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["packId"] != "https://example.invalid/judgment-packs/data-request-intake-triage" || payload["packVersion"] != "0.1.0" {
		t.Fatalf("payload = %v", payload)
	}
	// The members are additive; the protocol version does not move for them.
	if payload["outputVersion"] != result.OutputVersion {
		t.Fatalf("outputVersion = %v, want %q", payload["outputVersion"], result.OutputVersion)
	}

	// The corpus runner echoes the same two members per row that produced a
	// disposition, and neither member on a row that was refused. The two
	// populations are counted separately rather than asserting one number over
	// every row: the second the corpus gains an error-class row, a whole-corpus
	// count would fail for a reason that has nothing to do with the echo.
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate-corpus", "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var corpus result.EvaluationCorpus
	if err := json.Unmarshal([]byte(stdout), &corpus); err != nil {
		t.Fatal(err)
	}
	resolved, refused := 0, 0
	for _, row := range corpus.Cases {
		if row.ActualErrorClass != "" || row.ExpectedErrorClass != "" {
			refused++
			if row.PackID != "" || row.PackVersion != "" {
				t.Fatalf("a row that produced no disposition echoes no identity: %+v", row)
			}
			continue
		}
		resolved++
		if row.PackID == "" || row.PackVersion == "" {
			t.Fatalf("a row that produced a disposition echoes its pack: %+v", row)
		}
	}
	if resolved+refused != len(corpus.Cases) || resolved == 0 {
		t.Fatalf("the corpus must have rows to assert about: %d resolved, %d refused, %d rows", resolved, refused, len(corpus.Cases))
	}
}

// Coverage is reported beside the rows on both surfaces and moves no exit
// code: a passing run with missing probes still exits 0, because a missing
// probe is a fact about what the rows expect, not a failed row (ADR-0014).
func TestPacksTestReportsCoverageWithoutGatingOnIt(t *testing.T) {
	code, stdout, stderr := runTest(t, []string{"packs", "test", "--config", oneGoodProject(t), "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var run result.PackTest
	if err := json.Unmarshal([]byte(stdout), &run); err != nil {
		t.Fatal(err)
	}
	if len(run.Packs[0].Coverage) == 0 {
		t.Fatalf("a loaded matrix carries its coverage: %+v", run.Packs[0])
	}
	missing := 0
	for _, probe := range run.Packs[0].Coverage {
		if probe.Status == result.MatrixProbeMissing {
			missing++
		}
	}
	if missing == 0 {
		t.Fatalf("the fixture matrix does not witness every probe, and the report must say so: %+v", run.Packs[0].Coverage)
	}

	// The human surface prints the exact count and details only missing probes,
	// as it details only mismatching rows: the fixture pack derives seven
	// probes and its matrix witnesses exactly one, and the covered probe's
	// witness line stays out of the human output.
	code, stdout, stderr = runTest(t, []string{"packs", "test", "--config", oneGoodProject(t)}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "coverage: 1/7 derived probes have a row expecting them") {
		t.Fatalf("the human surface must state the exact coverage count: %q", stdout)
	}
	if !strings.Contains(stdout, "No row expects") {
		t.Fatalf("a missing probe gets its detail line: %q", stdout)
	}
	if strings.Contains(stdout, "expects it.") {
		t.Fatalf("a covered probe gets no detail line: %q", stdout)
	}
}
