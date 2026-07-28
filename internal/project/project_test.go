package project

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// packFixture is the evaluator package's own conformant 0.2.0-draft pack, which
// declares id, version 0.1.0, and three evidence requirements. Reusing it keeps
// these tests about the convention rather than about a pack written for them.
func packFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// writeProject lays out one project directory and returns its configuration
// path. Every test builds the project it needs rather than sharing one, so a
// test that mutates a file cannot reach another.
func writeProject(t *testing.T, config string, files map[string]string) string {
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
	configPath := filepath.Join(root, DefaultConfigName)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath
}

func mustLoad(t *testing.T, configPath string) *Project {
	t.Helper()
	loaded, failure := Load(configPath)
	if failure != nil {
		t.Fatalf("load: %s: %s", failure.Code, failure.Message)
	}
	return loaded
}

func newValidator(t *testing.T) *validation.Engine {
	t.Helper()
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

// The schema is closed and the accepted shape is exactly the documented one. Each
// rejected row is a different way to get the shape wrong, and each is refused
// with a code a caller can branch on rather than with a generic parse failure.
func TestConfigSchemaAcceptsTheDocumentedShapeAndRejectsEverythingElse(t *testing.T) {
	accepted := `{
	  "configVersion": "1",
	  "packs": {
	    "expense-approval": {
	      "path": "packs/expense-approval-0.1.0.pack.json",
	      "matrix": "packs/expense-approval.matrix.json",
	      "description": "Approve or decline one expense",
	      "expectedVersion": "0.1.0",
	      "facts": {"/expense/amount": {"source": "Snowflake FINANCE.EXPENSES", "hint": "amount_usd as a decimal string"}},
	      "evidence": {"receipt": {"source": "SharePoint /Receipts"}}
	    },
	    "minimal": {"path": "packs/minimal.json"}
	  }
	}`
	configPath := writeProject(t, accepted, nil)
	loaded := mustLoad(t, configPath)
	if len(loaded.IDs) != 2 || loaded.IDs[0] != "expense-approval" || loaded.IDs[1] != "minimal" {
		t.Fatalf("ids must be sorted and complete: %v", loaded.IDs)
	}
	entry, _ := loaded.Entry("expense-approval")
	if entry.Facts["/expense/amount"].Source != "Snowflake FINANCE.EXPENSES" || entry.Evidence["receipt"].Source != "SharePoint /Receipts" {
		t.Fatalf("hints must survive the round trip: %+v", entry)
	}
	if loaded.Root != filepath.Dir(configPath) {
		t.Fatalf("the root is the configuration's own directory: %q", loaded.Root)
	}

	rejected := map[string]struct {
		config string
		code   string
	}{
		"no configVersion":                     {`{"packs":{}}`, "JPS-PROJECT-CONFIG-VERSION"},
		"a later configVersion":                {`{"configVersion":"2","packs":{}}`, "JPS-PROJECT-CONFIG-VERSION"},
		"a semver configVersion":               {`{"configVersion":"1.0.0","packs":{}}`, "JPS-PROJECT-CONFIG-VERSION"},
		"a numeric configVersion":              {`{"configVersion":1,"packs":{}}`, "JPS-PROJECT-CONFIG-VERSION"},
		"no packs member":                      {`{"configVersion":"1"}`, "JPS-PROJECT-CONFIG-SCHEMA"},
		"an unknown root member":               {`{"configVersion":"1","packs":{},"targets":{}}`, "JPS-PROJECT-CONFIG-SCHEMA"},
		"an unknown pack member":               {`{"configVersion":"1","packs":{"a":{"path":"a.json","template":"x"}}}`, "JPS-PROJECT-CONFIG-SCHEMA"},
		"a pack with no path":                  {`{"configVersion":"1","packs":{"a":{"description":"x"}}}`, "JPS-PROJECT-CONFIG-SCHEMA"},
		"a decision id that is not a local id": {`{"configVersion":"1","packs":{"Expense Approval":{"path":"a.json"}}}`, "JPS-PROJECT-CONFIG-SCHEMA"},
		"a non-semver expectedVersion":         {`{"configVersion":"1","packs":{"a":{"path":"a.json","expectedVersion":"v1"}}}`, "JPS-PROJECT-CONFIG-SCHEMA"},
		"a fact hint keyed by a non-pointer":   {`{"configVersion":"1","packs":{"a":{"path":"a.json","facts":{"amount":{"source":"x"}}}}}`, "JPS-PROJECT-CONFIG-SCHEMA"},
		"an empty hint object":                 {`{"configVersion":"1","packs":{"a":{"path":"a.json","evidence":{"receipt":{}}}}}`, "JPS-PROJECT-CONFIG-SCHEMA"},
		"a duplicate member name":              {"{\"configVersion\":\"1\",\"packs\":{},\"packs\":{}}", "JPS-PROJECT-CONFIG-JSON"},
		"a non-object root":                    {`["configVersion"]`, "JPS-PROJECT-CONFIG-SCHEMA"},
		"unparsable JSON":                      {`{`, "JPS-PROJECT-CONFIG-JSON"},
	}
	for name, testCase := range rejected {
		t.Run(name, func(t *testing.T) {
			_, failure := Load(writeProject(t, testCase.config, nil))
			if failure == nil {
				t.Fatal("this configuration must be refused")
			}
			if failure.Code != testCase.code {
				t.Fatalf("code = %q (%s), want %q", failure.Code, failure.Message, testCase.code)
			}
		})
	}

	// The version refusal names what would have been accepted; a message that only
	// said "no" would leave a caller guessing.
	_, failure := Load(writeProject(t, `{"configVersion":"2","packs":{}}`, nil))
	if failure.ExitCode != result.ExitUnsupported || !strings.Contains(failure.Message, "It accepts: "+ConfigVersion+".") {
		t.Fatalf("the refusal must be unsupported and name this runtime's versions: exit=%d %q", failure.ExitCode, failure.Message)
	}
}

// A missing configuration is a read failure a caller can act on, and it names
// both ways of pointing at one.
func TestLoadReportsAMissingConfigurationWithTheWayToSupplyOne(t *testing.T) {
	_, failure := Load(filepath.Join(t.TempDir(), "absent.json"))
	if failure == nil || failure.Code != "JPS-PROJECT-CONFIG-READ" {
		t.Fatalf("failure = %+v", failure)
	}
	for _, required := range []string{"--config", ConfigEnv, DefaultConfigName} {
		if !strings.Contains(failure.Message, required) {
			t.Fatalf("the message must name %q: %q", required, failure.Message)
		}
	}
}

// Locate is the one resolution order every surface uses.
func TestLocatePrefersTheExplicitChoiceThenTheEnvironment(t *testing.T) {
	t.Setenv(ConfigEnv, "/from/env.json")
	if got := Locate("/explicit.json"); got != "/explicit.json" {
		t.Fatalf("explicit wins: %q", got)
	}
	if got := Locate("  "); got != "/from/env.json" {
		t.Fatalf("the environment is next: %q", got)
	}
	t.Setenv(ConfigEnv, "")
	if got := Locate(""); got != DefaultConfigName {
		t.Fatalf("the default is last: %q", got)
	}
}

// A configured path that leaves the project is refused twice over: once when the
// configuration is validated, before anything is read, and again at every read.
// Both halves are asserted, because a check that only ran at read time would let
// packs validate report a clean project that no surface can actually use.
func TestAnEscapingPathIsRefusedAtValidateTimeAndAtReadTime(t *testing.T) {
	pack := packFixture(t)
	for name, declared := range map[string]string{
		"a parent traversal": "../outside.pack.json",
		"an absolute path":   "/etc/passwd",
	} {
		t.Run(name, func(t *testing.T) {
			configPath := writeProject(t,
				`{"configVersion":"1","packs":{"escaping":{"path":`+quote(declared)+`}}}`,
				map[string]string{"packs/real.pack.json": string(pack)})
			// The file the escaping path points at is real, so the refusal is about
			// where it is and never about whether it exists.
			if err := os.WriteFile(filepath.Join(filepath.Dir(filepath.Dir(configPath)), "outside.pack.json"), pack, 0o600); err != nil {
				t.Fatal(err)
			}
			loaded := mustLoad(t, configPath)

			report, failure := loaded.Validate(newValidator(t), "", "packs validate")
			if failure != nil {
				t.Fatalf("validate: %s", failure.Message)
			}
			if report.Status != "invalid" || len(report.Packs) != 1 {
				t.Fatalf("report = %+v", report)
			}
			check := report.Packs[0].Checks[0]
			if check.Name != CheckPathInsideRoot || check.Status != result.PackCheckFailed {
				t.Fatalf("the first check must fail on containment: %+v", report.Packs[0].Checks)
			}
			for _, later := range report.Packs[0].Checks[1:] {
				if later.Status != result.PackCheckSkipped {
					t.Fatalf("every later check reads that file, so it must be skipped: %+v", later)
				}
			}

			// Read time refuses it too, on every path that reads a pack.
			entry, _ := loaded.Entry("escaping")
			if _, err := loaded.ReadPack(entry); err == nil {
				t.Fatal("the read must be refused as well")
			}
			if _, _, docFailure := loaded.Document("escaping", "mcp get_pack"); docFailure == nil {
				t.Fatal("serving the document must be refused as well")
			}
			inventory := loaded.Inventory("packs list")
			if inventory.Packs[0].PackID != "" || inventory.Packs[0].Detail == "" {
				t.Fatalf("an unreadable pack is listed with the reason and no invented identity: %+v", inventory.Packs[0])
			}
		})
	}
}

// A path that leaves the project through a symlinked directory component is the
// escape a lexical check cannot see, and the named check has to report it as the
// containment failure it is. A "path-inside-root: passed" row above a read that
// was refused for leaving the root would be a named check asserting the opposite
// of what happened, which is the defect the per-check report exists to prevent.
func TestASymlinkedComponentEscapeFailsTheContainmentCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the symlinked-directory escape is a POSIX filesystem behavior")
	}
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	// The file the escaping path reaches is a perfectly good pack, so the refusal
	// is about where it is and never about what it contains.
	if err := os.WriteFile(filepath.Join(outside, "secret.pack.json"), packFixture(t), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "project")
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "packs", "escape")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	configPath := filepath.Join(root, DefaultConfigName)
	if err := os.WriteFile(configPath, []byte(`{"configVersion":"1","packs":{"escaped":{"path":"packs/escape/secret.pack.json"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := mustLoad(t, configPath)

	report, failure := loaded.Validate(newValidator(t), "", "packs validate")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	containment := report.Packs[0].Checks[0]
	if containment.Name != CheckPathInsideRoot || containment.Status != result.PackCheckFailed {
		t.Fatalf("the containment check must fail, not the one after it: %+v", report.Packs[0].Checks)
	}
	if !strings.Contains(containment.Detail, "outside the configuration's own directory") {
		t.Fatalf("the detail must say what was refused: %q", containment.Detail)
	}
	for _, later := range report.Packs[0].Checks[1:] {
		if later.Status != result.PackCheckSkipped {
			t.Fatalf("every later check reads that file, so it must be skipped: %+v", later)
		}
	}

	entry, _ := loaded.Entry("escaped")
	if _, err := loaded.ReadPack(entry); !errors.Is(err, fssecure.ErrOutsideRoot) {
		t.Fatalf("the read must be refused as outside the root: %v", err)
	}
	// Asking the containment question without reading is the same refusal, and it
	// is asked of the same directory handle the read is bounded by: a surface that
	// wants the verdict rather than the bytes gets all the containment a read gets.
	if err := loaded.Contains(entry.Path); !errors.Is(err, fssecure.ErrOutsideRoot) {
		t.Fatalf("the containment check must be refused as outside the root: %v", err)
	}
	if _, _, docFailure := loaded.Document("escaped", "mcp get_pack"); docFailure == nil {
		t.Fatal("serving the document must be refused as well")
	}
}

// A hint key is the one cross-reference nothing else resolves: the runtime never
// reads a source, so a misspelled key reaches an agent as an authoritative
// instruction unless this check catches it.
func TestHintKeysAreCheckedAgainstThePackDocument(t *testing.T) {
	pack := string(packFixture(t))
	configPath := writeProject(t, `{"configVersion":"1","packs":{
	  "good":{"path":"packs/a.json",
	    "facts":{"/request/type":{"source":"Snowflake ANALYTICS.REQUESTS"}},
	    "evidence":{"intake-form":{"source":"SharePoint /Intake"}}},
	  "subtree":{"path":"packs/a.json","facts":{"/request":{"source":"Snowflake ANALYTICS.REQUESTS"}}},
	  "misspelled":{"path":"packs/a.json",
	    "facts":{"/nowhere/at/all":{"source":"Snowflake ANALYTICS.REQUESTS"}},
	    "evidence":{"intake-frm":{"source":"SharePoint /Intake"}}},
	  "unhinted":{"path":"packs/a.json"}
	}}`, map[string]string{"packs/a.json": pack})
	report, failure := mustLoad(t, configPath).Validate(newValidator(t), "", "packs validate")
	if failure != nil {
		t.Fatal(failure.Message)
	}

	if got := checkNamed(t, report, "good", CheckHintKeys); got.Status != result.PackCheckPassed {
		t.Fatalf("keys the document has must pass: %+v", got)
	}
	// A hint at an ancestor of a pointer some condition reads says something true
	// about the pack: the agent has to gather that subtree.
	if got := checkNamed(t, report, "subtree", CheckHintKeys); got.Status != result.PackCheckPassed {
		t.Fatalf("an ancestor of a read pointer must pass: %+v", got)
	}
	if got := checkNamed(t, report, "unhinted", CheckHintKeys); got.Status != result.PackCheckSkipped {
		t.Fatalf("an entry with no hints skips the check rather than passing it: %+v", got)
	}
	misspelled := checkNamed(t, report, "misspelled", CheckHintKeys)
	if misspelled.Status != result.PackCheckFailed {
		t.Fatalf("a key the document does not have must fail: %+v", misspelled)
	}
	for _, required := range []string{`"intake-frm"`, `"/nowhere/at/all"`} {
		if !strings.Contains(misspelled.Detail, required) {
			t.Fatalf("the detail must name %s: %q", required, misspelled.Detail)
		}
	}
	if report.Status != "invalid" || report.Summary.Failed != 1 {
		t.Fatalf("exactly the pack with the bad keys fails: %+v", report.Summary)
	}
}

// The pin is a reference, never a truth: a difference is reported as one, and
// the pack document's own version is what the pack is.
func TestExpectedVersionDriftIsReportedAgainstTheDocument(t *testing.T) {
	pack := packFixture(t)
	configPath := writeProject(t, `{"configVersion":"1","packs":{
	  "pinned-right":{"path":"packs/a.json","expectedVersion":"0.1.0"},
	  "pinned-wrong":{"path":"packs/b.json","expectedVersion":"0.2.0"},
	  "unpinned":{"path":"packs/c.json"}
	}}`, map[string]string{
		"packs/a.json": string(pack),
		"packs/b.json": string(pack),
		"packs/c.json": string(pack),
	})
	loaded := mustLoad(t, configPath)

	wantStatus := map[string]string{
		"pinned-right": result.PackVersionMatches,
		"pinned-wrong": result.PackVersionDiffers,
		"unpinned":     result.PackVersionUnset,
	}
	for _, summary := range loaded.Inventory("packs list").Packs {
		if summary.ExpectedVersionStatus != wantStatus[summary.ID] {
			t.Fatalf("%s: status = %q, want %q", summary.ID, summary.ExpectedVersionStatus, wantStatus[summary.ID])
		}
		if summary.PackVersion != "0.1.0" {
			t.Fatalf("%s: the reported version is the document's: %q", summary.ID, summary.PackVersion)
		}
	}

	report, failure := loaded.Validate(newValidator(t), "", "packs validate")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if report.Status != "invalid" || report.Summary.Failed != 1 {
		t.Fatalf("exactly the drifting pack fails: %+v", report.Summary)
	}
	drifted := checkNamed(t, report, "pinned-wrong", CheckExpectedVer)
	if drifted.Status != result.PackCheckFailed || !strings.Contains(drifted.Detail, `"0.2.0"`) || !strings.Contains(drifted.Detail, `"0.1.0"`) {
		t.Fatalf("the detail must name both values: %+v", drifted)
	}
	if got := checkNamed(t, report, "unpinned", CheckExpectedVer); got.Status != result.PackCheckSkipped {
		t.Fatalf("an unpinned entry skips the check rather than passing it: %+v", got)
	}
}

// The filename convention is optional and cross-checked when followed: matching,
// mismatching on either half, and outside the pattern entirely.
func TestFilenameCrossCheckIsOptionalAndBindingWhenFollowed(t *testing.T) {
	pack := string(packFixture(t))
	configPath := writeProject(t, `{"configVersion":"1","packs":{
	  "intake":{"path":"packs/intake-0.1.0.pack.json"},
	  "triage":{"path":"packs/intake-0.1.0.pack.json"},
	  "review":{"path":"packs/review-9.9.9.pack.json"},
	  "freeform":{"path":"packs/whatever.json"}
	}}`, map[string]string{
		"packs/intake-0.1.0.pack.json": pack,
		"packs/review-9.9.9.pack.json": pack,
		"packs/whatever.json":          pack,
	})
	report, failure := mustLoad(t, configPath).Validate(newValidator(t), "", "packs validate")
	if failure != nil {
		t.Fatal(failure.Message)
	}

	if got := checkNamed(t, report, "intake", CheckFilename); got.Status != result.PackCheckPassed {
		t.Fatalf("a filename agreeing with both must pass: %+v", got)
	}
	mismatchedID := checkNamed(t, report, "triage", CheckFilename)
	if mismatchedID.Status != result.PackCheckFailed || !strings.Contains(mismatchedID.Detail, `"triage"`) {
		t.Fatalf("a filename naming another decision must fail: %+v", mismatchedID)
	}
	mismatchedVersion := checkNamed(t, report, "review", CheckFilename)
	if mismatchedVersion.Status != result.PackCheckFailed || !strings.Contains(mismatchedVersion.Detail, `"9.9.9"`) {
		t.Fatalf("a filename naming another version must fail: %+v", mismatchedVersion)
	}
	outside := checkNamed(t, report, "freeform", CheckFilename)
	if outside.Status != result.PackCheckSkipped {
		t.Fatalf("a filename outside the convention is not a defect: %+v", outside)
	}
	if report.Status != "invalid" || report.Summary.Failed != 2 {
		t.Fatalf("exactly the two cross-check failures fail: %+v", report.Summary)
	}
}

// A matrix has to load as rows before packs test can run one, and every way of
// getting the carrier wrong is named rather than silently tolerated.
func TestMatrixWellFormednessIsCheckedBeforeAnyRowRuns(t *testing.T) {
	pack := string(packFixture(t))
	for name, matrix := range map[string]string{
		"an empty case list":          `{"cases":[]}`,
		"a row with no id":            `{"cases":[{"facts":{},"expectedErrorClass":"malformed-input"}]}`,
		"a duplicate row id":          `{"cases":[{"id":"a","facts":{},"expectedErrorClass":"malformed-input"},{"id":"a","facts":{},"expectedErrorClass":"malformed-input"}]}`,
		"a row with no facts":         `{"cases":[{"id":"a","expectedErrorClass":"malformed-input"}]}`,
		"a row with no expectation":   `{"cases":[{"id":"a","facts":{}}]}`,
		"a row with two expectations": `{"cases":[{"id":"a","facts":{},"expectedErrorClass":"malformed-input","expectedDisposition":{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none"}}}]}`,
		"a misspelled row member":     `{"cases":[{"id":"a","facts":{},"expectedDispositon":{}}]}`,
		"an unknown matrixVersion":    `{"matrixVersion":"2","cases":[{"id":"a","facts":{},"expectedErrorClass":"malformed-input"}]}`,
		"a phase without a class":     `{"cases":[{"id":"a","facts":{},"expectedDisposition":{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none"}},"expectedErrorPhase":"preflight"}]}`,
		"a non-object root":           `[]`,
		"unparsable JSON":             `{`,
	} {
		t.Run(name, func(t *testing.T) {
			configPath := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
				map[string]string{"packs/a.json": pack, "packs/a.matrix.json": matrix})
			loaded := mustLoad(t, configPath)
			report, failure := loaded.Validate(newValidator(t), "", "packs validate")
			if failure != nil {
				t.Fatal(failure.Message)
			}
			if got := checkNamed(t, report, "a", CheckMatrix); got.Status != result.PackCheckFailed {
				t.Fatalf("this matrix must be refused: %+v", got)
			}
			// The same defect stops packs test rather than being reported as a clean run.
			run, testFailure := loaded.Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
			if testFailure != nil {
				t.Fatal(testFailure.Message)
			}
			if run.Status != "mismatch" || run.Packs[0].Status != "mismatch" {
				t.Fatalf("a matrix that will not load has not passed: %+v", run)
			}
		})
	}

	// An absent matrix is skipped and never counted as passed, and a run over zero
	// rows is not a clean run: the top-level status is what a CI gate reads, so it
	// is the one that has to say nothing was tested.
	configPath := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json"}}}`,
		map[string]string{"packs/a.json": pack})
	run, failure := mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if run.Packs[0].Status != "skipped" || run.Summary.Total != 0 {
		t.Fatalf("a pack with no matrix is skipped: %+v", run)
	}
	if run.Status != "skipped" {
		t.Fatalf("a run in which no row ran must not report passed: %+v", run)
	}

	// One pack with rows carries the run: the demotion is about a run that tested
	// nothing, not about a project that also owns a matrix-less pack.
	mixed := `{"cases":[{"id":"a","facts":{"request":{"type":"data-access"}},"evidenceAvailability":{"not-a-requirement":"present"},"expectedErrorClass":"malformed-input"}]}`
	configPath = writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json"},"b":{"path":"packs/a.json","matrix":"packs/b.matrix.json"}}}`,
		map[string]string{"packs/a.json": pack, "packs/b.matrix.json": mixed})
	run, failure = mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if run.Status != "passed" || run.Summary.Total != 1 {
		t.Fatalf("a run with a row that passed is a pass: %+v", run)
	}
}

// The demotion is about rows, not about how many packs were selected.
//
// The schema refuses an empty packs object, so this state cannot be reached
// through a configuration — which is exactly why it is asserted here, against
// the runner directly. A guard that additionally required a non-empty selection
// would report a clean run over zero rows for a project that configures nothing,
// and it would do so silently the moment anything else produced an empty
// selection. Two independent refusals, and this is the one that does not depend
// on the schema being right.
func TestAnEmptySelectionIsNotACleanRun(t *testing.T) {
	empty := &Project{
		ConfigPath: "jpack.json",
		Config:     Config{ConfigVersion: ConfigVersion, Packs: map[string]Pack{}},
		IDs:        []string{},
	}
	run, failure := empty.Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if run.Status != "skipped" {
		t.Fatalf("a run over no packs at all must not report passed: %+v", run)
	}
	if run.Summary.Total != 0 || len(run.Packs) != 0 {
		t.Fatalf("nothing ran, and the report must say exactly that: %+v", run)
	}
}

// Serving a document that does not decode says so. The bytes are the project's
// and are handed over unaltered, but the payload cannot call them valid and
// report an empty identity with no explanation — list_packs reports the same
// file the same way, and two surfaces reading one file must agree about it.
func TestServingAnUndecodableDocumentSaysSo(t *testing.T) {
	configPath := writeProject(t, `{"configVersion":"1","packs":{"broken":{"path":"packs/broken.json"}}}`,
		map[string]string{"packs/broken.json": `{ this is not json`})
	loaded := mustLoad(t, configPath)

	document, data, failure := loaded.Document("broken", "mcp get_pack")
	if failure != nil {
		t.Fatalf("the bytes were read, so they are served: %s", failure.Message)
	}
	if string(data) != `{ this is not json` {
		t.Fatalf("the project's own bytes are served unaltered: %q", data)
	}
	if document.Status != "undecodable" || document.Detail == "" {
		t.Fatalf("a document that did not decode is not reported valid: %+v", document)
	}
	if document.PackID != "" || document.PackVersion != "" || document.SpecVersion != "" {
		t.Fatalf("no identity is invented for a document that did not decode: %+v", document)
	}
	// The inventory says the same thing about the same file.
	if detail := loaded.Inventory("packs list").Packs[0].Detail; detail != document.Detail {
		t.Fatalf("the two surfaces must report one file identically: %q vs %q", detail, document.Detail)
	}

	// A document that decodes but declares no id is a different condition: it is
	// served as valid, with the identity members it actually states.
	configPath = writeProject(t, `{"configVersion":"1","packs":{"empty":{"path":"packs/empty.json"}}}`,
		map[string]string{"packs/empty.json": `{}`})
	document, _, failure = mustLoad(t, configPath).Document("empty", "mcp get_pack")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if document.Status != "valid" || document.Detail != "" || document.PackID != "" {
		t.Fatalf("a decodable document declaring no id is not a read failure: %+v", document)
	}
}

// A row is judged by the byte comparison §8.3 defines, and by the §8.4 class for
// a row that expects a refusal. Both the passing and the failing side are pinned,
// because a comparison that never fails is not a comparison.
func TestMatrixRowsAreComparedByCanonicalBytesAndErrorClass(t *testing.T) {
	pack := string(packFixture(t))
	facts := `{"request":{"type":"data-access","completeness":"complete","appropriateness":"hard-fail","embargoedInformationToUnauthorizedRecipients":false}}`
	evidence := `{"intake-form":"present","sponsor-endorsement":"present"}`
	declineRedirect := `{"kind":"outcome","outcomeId":"decline-redirect","reasons":[],"handoff":{"state":"none"}}`

	passing := `{"matrixVersion":"1","cases":[
	  {"id":"hard-fail","facts":` + facts + `,"evidenceAvailability":` + evidence + `,"expectedDisposition":` + declineRedirect + `},
	  {"id":"undeclared-key","facts":{"request":{"type":"data-access"}},"evidenceAvailability":{"not-a-requirement":"present"},"expectedErrorClass":"malformed-input","expectedErrorPhase":"preflight"}
	]}`
	configPath := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
		map[string]string{"packs/a.json": pack, "packs/a.matrix.json": passing})
	run, failure := mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if run.Status != "passed" || run.Summary.Total != 2 || run.Summary.Passed != 2 {
		t.Fatalf("run = %+v rows=%+v", run.Summary, run.Packs[0].Rows)
	}
	if run.Packs[0].PackID == "" || run.Packs[0].PackVersion != "0.1.0" {
		t.Fatalf("the entry echoes the document's own identity: %+v", run.Packs[0])
	}
	if run.Packs[0].Rows[0].PackID != run.Packs[0].PackID {
		t.Fatalf("a row that produced a disposition echoes the pack it ran against: %+v", run.Packs[0].Rows[0])
	}
	// A row that produced no disposition echoes no identity: there was no
	// evaluation to read one off.
	if run.Packs[0].Rows[1].PackID != "" {
		t.Fatalf("a refused row has no pack identity to echo: %+v", run.Packs[0].Rows[1])
	}
	if run.Label != result.PackMatrixLabel || run.ConformanceClaimReference != result.EvaluationClaimReference || !run.Experimental {
		t.Fatalf("the payload must label the run and point at the claim document: %+v", run)
	}

	// One byte of the expectation changed is a mismatch, and the report carries
	// both canonical byte sequences so a builder can see the difference.
	failing := strings.Replace(passing, "decline-redirect", "accept-and-fulfill", 1)
	configPath = writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
		map[string]string{"packs/a.json": pack, "packs/a.matrix.json": failing})
	run, failure = mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if run.Status != "mismatch" || run.Summary.Mismatched != 1 || run.Summary.Passed != 1 {
		t.Fatalf("run = %+v", run.Summary)
	}
	mismatched := run.Packs[0].Rows[0]
	if mismatched.Status != "mismatch" || mismatched.Expected == mismatched.Actual || mismatched.Expected == "" || mismatched.Actual == "" {
		t.Fatalf("the row must report both canonical byte sequences: %+v", mismatched)
	}
}

// An unknown decision id is refused rather than reported as zero results, on
// every surface that takes one. A CI step that silently checks nothing because of
// a typo is the failure this prevents.
func TestAnUnknownDecisionIdIsRefusedAndListsTheKnownOnes(t *testing.T) {
	configPath := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json"}}}`,
		map[string]string{"packs/a.json": string(packFixture(t))})
	loaded := mustLoad(t, configPath)
	for name, run := range map[string]func() *Failure{
		"validate": func() *Failure { _, f := loaded.Validate(newValidator(t), "nope", "packs validate"); return f },
		"test": func() *Failure {
			_, f := loaded.Test(evaluation.NewEngine(newValidator(t)), "nope", "packs test")
			return f
		},
		"document": func() *Failure { _, _, f := loaded.Document("nope", "mcp get_pack"); return f },
	} {
		t.Run(name, func(t *testing.T) {
			failure := run()
			if failure == nil || failure.Code != "JPS-PROJECT-UNKNOWN-PACK" {
				t.Fatalf("failure = %+v", failure)
			}
			if !strings.Contains(failure.Message, "Configured ids: a.") {
				t.Fatalf("the refusal must list what is configured: %q", failure.Message)
			}
		})
	}
}

// The embedded schema is a real, compilable schema and describes the version it
// claims to; packs schema prints these exact bytes.
func TestTheEmbeddedSchemaIsCoherent(t *testing.T) {
	var document map[string]any
	if err := json.Unmarshal(Schema(), &document); err != nil {
		t.Fatalf("the embedded schema must be JSON: %v", err)
	}
	if document["$id"] != SchemaID {
		t.Fatalf("$id = %v, want %q", document["$id"], SchemaID)
	}
	if _, err := validation.CompileSchema(Schema(), SchemaID); err != nil {
		t.Fatalf("the embedded schema must compile: %v", err)
	}
	described := SchemaDescription("packs schema")
	if described.Bytes != len(Schema()) || described.ConfigVersion != ConfigVersion || described.Kind != result.ProjectKind {
		t.Fatalf("described = %+v", described)
	}
}

// checkNamed pulls one named check out of a validation report.
func checkNamed(t *testing.T, report result.PackValidation, id, check string) result.PackCheck {
	t.Helper()
	for _, pack := range report.Packs {
		if pack.ID != id {
			continue
		}
		for _, item := range pack.Checks {
			if item.Name == check {
				return item
			}
		}
		t.Fatalf("%s has no %q check: %+v", id, check, pack.Checks)
	}
	t.Fatalf("no pack %q in the report", id)
	return result.PackCheck{}
}

func quote(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
