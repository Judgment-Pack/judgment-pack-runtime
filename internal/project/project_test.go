package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
	// Windows refuses to remove a directory somebody holds open, so an unclosed
	// handle fails t.TempDir's cleanup there; Unix merely leaks the descriptor.
	t.Cleanup(func() { _ = loaded.Close() })
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
		"a later configVersion":                {`{"configVersion":"4","packs":{}}`, "JPS-PROJECT-CONFIG-VERSION"},
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
	_, failure := Load(writeProject(t, `{"configVersion":"4","packs":{}}`, nil))
	if failure.ExitCode != result.ExitUnsupported || !strings.Contains(failure.Message, "It accepts: "+strings.Join(SupportedConfigVersions(), ", ")+".") {
		t.Fatalf("the refusal must be unsupported and name this runtime's versions: exit=%d %q", failure.ExitCode, failure.Message)
	}
	// A version above what this runtime reads says which side to change: the
	// runtime. Left ambiguous, a reader edits the declaration down — observed
	// live, with an agent — and silently discards what it declared.
	if !strings.Contains(failure.Message, "upgrade the runtime") ||
		!strings.Contains(failure.Message, "Do not edit the declaration") {
		t.Fatalf("a newer declaration must steer toward upgrading the runtime: %q", failure.Message)
	}
	// An unparseable declaration says nothing about which side is behind, so it
	// earns the version list and no steer.
	_, failure = Load(writeProject(t, `{"configVersion":"next","packs":{}}`, nil))
	if failure.Code != "JPS-PROJECT-CONFIG-VERSION" || strings.Contains(failure.Message, "upgrade the runtime") {
		t.Fatalf("an unparseable declaration must not claim the runtime is behind: %q", failure.Message)
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

// The inventory reports the pointers the conditions read — from every position
// a condition can sit in, deduplicated, by the shape-keyed walk rather than the
// operators this runtime happens to know (ADR-0020). Listing is not validating:
// the document below is not a conformant pack, and the walk reports what it
// carries, verbatim.
func TestTheInventoryReportsEveryPointerTheConditionsRead(t *testing.T) {
	document := `{
	  "id": "https://example.invalid/judgment-packs/walked", "version": "0.1.0",
	  "applicability": {"op": "fact", "path": "/request/type"},
	  "rules": [{"id": "r1", "when": {"all": [
	    {"op": "fact", "path": "/request/containsPersonalData"},
	    {"any": [{"op": "fact", "path": "/request/type"}]}
	  ]}}],
	  "exceptions": [{"id": "x1", "when": {"op": "future-shape", "path": "/request/urgency"}}],
	  "metadata": {"op": "root-read", "path": ""},
	  "notes": [{"op": "non-string-is-skipped", "path": 7},
	            {"op": "reported", "path": "not-a-pointer"}]
	}`
	configPath := writeProject(t, `{"configVersion":"1","packs":{"walked":{"path":"packs/walked.json"}}}`,
		map[string]string{"packs/walked.json": document})
	summary := mustLoad(t, configPath).Inventory("packs list").Packs[0]

	// /request/type is read twice and reported once; the exception's unknown
	// operator is still a condition by shape; the empty string is the RFC 6901
	// root pointer — a condition carrying it reads the whole facts document —
	// and the string that is not a pointer is reported verbatim rather than
	// silently dropped.
	want := []string{"", "/request/containsPersonalData", "/request/type", "/request/urgency", "not-a-pointer"}
	if !slices.Equal(summary.ConsultedFactPaths, want) {
		t.Fatalf("consultedFactPaths = %v, want %v", summary.ConsultedFactPaths, want)
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
		"an unknown matrixVersion":    `{"matrixVersion":"9","cases":[{"id":"a","facts":{},"expectedErrorClass":"malformed-input"}]}`,
		"a phase without a class":     `{"cases":[{"id":"a","facts":{},"expectedDisposition":{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none"}},"expectedErrorPhase":"preflight"}]}`,
		"a non-object root":           `[]`,
		"unparsable JSON":             `{`,
		// ADR-0025: the optional second assertion is held to its shape before any
		// row runs, and it rides only beside a disposition.
		"a handoff target beside an error class": `{"matrixVersion":"2","cases":[{"id":"a","facts":{},"expectedErrorClass":"malformed-input","expectedHandoffTarget":{"kind":"human-role","name":"Intake reviewer"}}]}`,
		// The literal null is the same refusal for the same reason: a refused
		// evaluation reports no target at all, which is not the statement "this
		// evaluation reported none". Both spellings of the assertion are refused,
		// or the companionship rule would have a hole in exactly the state the
		// unavailable rendering exists to keep distinct.
		"a null handoff target beside an error class": `{"matrixVersion":"2","cases":[{"id":"a","facts":{},"expectedErrorClass":"malformed-input","expectedHandoffTarget":null}]}`,
		"a handoff target that is not an object":      `{"matrixVersion":"2","cases":[{"id":"a","facts":{},"expectedDisposition":{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none"}},"expectedHandoffTarget":"Intake reviewer"}]}`,
		"a handoff target missing a member":           `{"matrixVersion":"2","cases":[{"id":"a","facts":{},"expectedDisposition":{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none"}},"expectedHandoffTarget":{"kind":"human-role"}}]}`,
		"a handoff target with an empty member":       `{"matrixVersion":"2","cases":[{"id":"a","facts":{},"expectedDisposition":{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none"}},"expectedHandoffTarget":{"kind":"human-role","name":""}}]}`,
		"a handoff target with an unknown member":     `{"matrixVersion":"2","cases":[{"id":"a","facts":{},"expectedDisposition":{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none"}},"expectedHandoffTarget":{"kind":"human-role","name":"Intake reviewer","queue":"triage"}}]}`,
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
	row := loaded.Inventory("packs list").Packs[0]
	if row.Detail != document.Detail {
		t.Fatalf("the two surfaces must report one file identically: %q vs %q", row.Detail, document.Detail)
	}
	if row.ConsultedFactPaths == nil || len(row.ConsultedFactPaths) != 0 {
		t.Fatalf("a document that did not decode carries [], never null: %v", row.ConsultedFactPaths)
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
	// A row that asserts nothing about the handoff target reports nothing about
	// it: the members are absent, so the payload is what it was before the
	// assertion existed (ADR-0025).
	for _, row := range run.Packs[0].Rows {
		if row.ExpectedHandoffTarget != "" || row.ActualHandoffTarget != "" {
			t.Fatalf("a row that asks nothing about the target must report nothing about it: %+v", row)
		}
	}
}

// The escalation target lives outside the disposition (§8.3), so no comparison
// of dispositions can see a change to it. This is Study 013's holdout cell h02,
// registered adversarially by that study's cross-vendor reviewer: a pack
// mutation reaching only escalation.target.name leaves kind, outcomeId,
// reasons, handoff.state, and handoff.triggeredBy identical, and a matrix that
// compares only dispositions stays green over the corrupted pack.
//
// The first half of this test demonstrates the gap rather than asserting about
// it; the second half is the row member that closes it (ADR-0025).
func TestATargetOnlyMutationIsInvisibleToEveryDisposition(t *testing.T) {
	pack := string(packFixture(t))
	corrupted := strings.Replace(pack, `"Intake reviewer"`, `"Disclosure office"`, 1)
	if corrupted == pack {
		t.Fatal("the fixture must declare the escalation target this test corrupts")
	}
	notApplicable := `{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`
	config := `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`

	// Every row of this matrix passes against either pack: the corruption is
	// outside every byte the rows compare.
	dispositionOnly := `{"matrixVersion":"1","cases":[
	  {"id":"out-of-scope","facts":{"request":{"type":"unrelated"}},"expectedDisposition":` + notApplicable + `}
	]}`
	for name, document := range map[string]string{"the correct pack": pack, "the corrupted pack": corrupted} {
		configPath := writeProject(t, config, map[string]string{"packs/a.json": document, "packs/a.matrix.json": dispositionOnly})
		run, failure := mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
		if failure != nil {
			t.Fatal(failure.Message)
		}
		if run.Status != "passed" {
			t.Fatalf("%s: a disposition-only matrix cannot see a target-only mutation: %+v", name, run.Packs[0].Rows)
		}
	}

	// The same row, asserting the target the correct pack declares, separates
	// the two packs.
	asserting := `{"matrixVersion":"2","cases":[
	  {"id":"out-of-scope","facts":{"request":{"type":"unrelated"}},"expectedDisposition":` + notApplicable + `,
	   "expectedHandoffTarget":{"kind":"human-role","name":"Intake reviewer"}}
	]}`
	configPath := writeProject(t, config, map[string]string{"packs/a.json": pack, "packs/a.matrix.json": asserting})
	run, failure := mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if run.Status != "passed" {
		t.Fatalf("the correct pack still passes: %+v", run.Packs[0].Rows)
	}
	configPath = writeProject(t, config, map[string]string{"packs/a.json": corrupted, "packs/a.matrix.json": asserting})
	run, failure = mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if run.Status != "mismatch" || run.Summary.Mismatched != 1 {
		t.Fatalf("the corrupted target must fail the row that asserts it: %+v", run.Packs[0].Rows)
	}
	row := run.Packs[0].Rows[0]
	if row.Expected != row.Actual {
		t.Fatalf("the disposition is unchanged by the mutation, which is the whole point: %+v", row)
	}
	if row.ExpectedHandoffTarget != `{"kind":"human-role","name":"Intake reviewer"}` ||
		row.ActualHandoffTarget != `{"kind":"human-role","name":"Disclosure office"}` {
		t.Fatalf("the row must report both targets: %+v", row)
	}
	if !strings.Contains(row.Detail, "both name a target") {
		t.Fatalf("the detail must say which side names a destination: %q", row.Detail)
	}
}

// The assertion has three states — absent, an object, and null — and the row's
// status follows all three (ADR-0025). Absent is covered where the canonical
// comparison is; these are the other two, against a pack whose escalation
// target is reported for some inputs and not for others.
func TestExpectedHandoffTargetAssertsThePresenceOfATargetAsWellAsItsIdentity(t *testing.T) {
	pack := string(packFixture(t))
	// Out of scope: not-applicable is a declared trigger, so the target is
	// reported. Hard fail: an outcome requests no handoff, so none is.
	escalates := `{"request":{"type":"unrelated"}}`
	outcome := `{"request":{"type":"data-access","completeness":"complete","appropriateness":"hard-fail","embargoedInformationToUnauthorizedRecipients":false}}`
	evidence := `{"intake-form":"present","sponsor-endorsement":"present"}`
	notApplicable := `{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`
	declineRedirect := `{"kind":"outcome","outcomeId":"decline-redirect","reasons":[],"handoff":{"state":"none"}}`
	target := `{"kind":"human-role","name":"Intake reviewer"}`

	for name, expectation := range map[string]struct {
		row    string
		passes bool
		detail string
	}{
		"the target as declared": {
			row:    `{"id":"r","facts":` + escalates + `,"expectedDisposition":` + notApplicable + `,"expectedHandoffTarget":` + target + `}`,
			passes: true,
		},
		"a mistyped name": {
			row:    `{"id":"r","facts":` + escalates + `,"expectedDisposition":` + notApplicable + `,"expectedHandoffTarget":{"kind":"human-role","name":"Intake reviewers"}}`,
			detail: "both name a target",
		},
		"a mistyped kind": {
			row:    `{"id":"r","facts":` + escalates + `,"expectedDisposition":` + notApplicable + `,"expectedHandoffTarget":{"kind":"queue","name":"Intake reviewer"}}`,
			detail: "both name a target",
		},
		"null where no target is reported": {
			row:    `{"id":"r","facts":` + outcome + `,"evidenceAvailability":` + evidence + `,"expectedDisposition":` + declineRedirect + `,"expectedHandoffTarget":null}`,
			passes: true,
		},
		"null where a target is reported": {
			row:    `{"id":"r","facts":` + escalates + `,"expectedDisposition":` + notApplicable + `,"expectedHandoffTarget":null}`,
			detail: "the row expects no target and the evaluation reports one",
		},
		"a target where none is reported": {
			row:    `{"id":"r","facts":` + outcome + `,"evidenceAvailability":` + evidence + `,"expectedDisposition":` + declineRedirect + `,"expectedHandoffTarget":` + target + `}`,
			detail: "the row expects a target and the evaluation reports none",
		},
	} {
		t.Run(name, func(t *testing.T) {
			configPath := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
				map[string]string{"packs/a.json": pack, "packs/a.matrix.json": `{"matrixVersion":"2","cases":[` + expectation.row + `]}`})
			run, failure := mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
			if failure != nil {
				t.Fatal(failure.Message)
			}
			row := run.Packs[0].Rows[0]
			if expectation.passes {
				if run.Status != "passed" || row.Status != "passed" {
					t.Fatalf("this row must pass: %+v", row)
				}
				return
			}
			// A mismatching target moves the row, the pack entry, and the run,
			// exactly as a mismatching disposition does: this is an expectation and
			// not a coverage line.
			if run.Status != "mismatch" || run.Packs[0].Status != "mismatch" || row.Status != "mismatch" {
				t.Fatalf("this row must gate: %+v", row)
			}
			// The disposition matched, so the row failed on the member the
			// disposition cannot carry.
			if row.Expected != row.Actual || row.Expected == "" {
				t.Fatalf("the disposition is not what differs here: %+v", row)
			}
			if !strings.Contains(row.Detail, expectation.detail) {
				t.Fatalf("detail = %q, want it to contain %q", row.Detail, expectation.detail)
			}
			if row.ExpectedHandoffTarget == "" || row.ActualHandoffTarget == "" || row.ExpectedHandoffTarget == row.ActualHandoffTarget {
				t.Fatalf("both renderings are reported and they differ: %+v", row)
			}
		})
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

// Coverage probes are derived from the pack document's own declarations, and
// each exists only where the declarations make its behavior reachable: a probe
// for a pack that cannot produce the behavior would demand a row no facts can
// produce. Witnessing is by expectation only — an error row expects no
// disposition and witnesses nothing.
func TestMatrixCoverageDerivesProbesFromDeclarations(t *testing.T) {
	probeNames := func(probes []result.MatrixProbe) []string {
		names := make([]string, 0, len(probes))
		for _, probe := range probes {
			names = append(names, probe.Probe)
		}
		return names
	}

	// The full derivation, in deterministic order: outcomes as declared, then
	// the reachable reason classes.
	pack := map[string]any{
		"applicability": map[string]any{"op": "equals", "path": "/t", "value": "x"},
		"evidenceRequirements": []any{
			map[string]any{"id": "receipt", "required": true},
			map[string]any{"id": "approval", "required": false},
		},
		"outcomes": []any{map[string]any{"id": "allow"}, map[string]any{"id": "deny"}},
		"rules": []any{
			map[string]any{"id": "r1", "outcome": "allow", "onUnknown": "ignore"},
			map[string]any{"id": "r2", "outcome": "deny", "onUnknown": "escalate"},
		},
		"exceptions":      []any{map[string]any{"id": "e1", "effect": "force-outcome", "outcome": "deny"}},
		"fallbackOutcome": "allow",
	}
	probes := matrixCoverage(pack, Matrix{})
	want := []string{"outcome:allow", "outcome:deny", "not-applicable", "missing-required-evidence", "unknown", "conflict"}
	if got := probeNames(probes); !slices.Equal(got, want) {
		t.Fatalf("probes = %v, want %v", got, want)
	}
	for _, probe := range probes {
		if probe.Status != result.MatrixProbeMissing || probe.Detail == "" {
			t.Fatalf("an empty matrix witnesses nothing, and every missing probe says why: %+v", probe)
		}
	}

	// A pack that declares none of the reachable behaviors derives none of the
	// probes — and no fallbackOutcome is what makes no-match reachable.
	minimal := map[string]any{
		"outcomes": []any{map[string]any{"id": "allow"}},
		"rules": []any{
			map[string]any{"id": "r1", "outcome": "allow"},
			map[string]any{"id": "r2", "outcome": "allow"},
		},
	}
	if got := probeNames(matrixCoverage(minimal, Matrix{})); !slices.Equal(got, []string{"outcome:allow", "no-match"}) {
		t.Fatalf("probes = %v", got)
	}

	// A direct escalation is the one exception effect an expectation can
	// witness, and two force-outcome exceptions naming different outcomes make
	// a conflict constructible even when every rule agrees.
	exceptional := map[string]any{
		"outcomes": []any{map[string]any{"id": "allow"}},
		"rules":    []any{map[string]any{"id": "r1", "outcome": "allow"}},
		"exceptions": []any{
			map[string]any{"id": "e1", "effect": "escalate"},
			map[string]any{"id": "e2", "effect": "force-outcome", "outcome": "allow"},
			map[string]any{"id": "e3", "effect": "force-outcome", "outcome": "deny"},
		},
		"fallbackOutcome": "allow",
	}
	if got := probeNames(matrixCoverage(exceptional, Matrix{})); !slices.Equal(got, []string{"outcome:allow", "conflict", "exception-escalation"}) {
		t.Fatalf("probes = %v", got)
	}

	// A declared outcome nothing references cannot be produced under §8, so
	// no probe is derived for it: the probe would be permanently missing, and
	// a row written to cover it would mismatch forever. Semantic validation
	// checks only that named outcomes are declared, not the reverse.
	unreferenced := map[string]any{
		"outcomes": []any{map[string]any{"id": "allow"}, map[string]any{"id": "orphan"}},
		"rules":    []any{map[string]any{"id": "r1", "outcome": "allow"}},
	}
	if got := probeNames(matrixCoverage(unreferenced, Matrix{})); !slices.Equal(got, []string{"outcome:allow", "no-match"}) {
		t.Fatalf("an unreferenced outcome derives no probe: %v", got)
	}

	// A pack that did not decode derives nothing at all.
	if matrixCoverage(nil, Matrix{}) != nil {
		t.Fatal("no pack, no probes")
	}
}

// A witness passes the same strict §8.3 gate the row comparator applies: an
// expectation that is legal JSON but not a legal disposition mismatches by
// construction, and a looser decode would report a probe covered by a row
// that can never hold.
func TestMatrixCoverageRefusesIllegalWitnesses(t *testing.T) {
	pack := map[string]any{
		"outcomes":        []any{map[string]any{"id": "allow"}},
		"rules":           []any{map[string]any{"id": "r1", "outcome": "allow"}},
		"fallbackOutcome": "allow",
	}
	illegal := Matrix{Cases: []evaluation.MatrixCase{{
		ID:                  "outcome-with-reasons",
		ExpectedDisposition: json.RawMessage(`{"kind":"outcome","outcomeId":"allow","reasons":["unknown"],"handoff":{"state":"none"}}`),
	}}}
	for _, probe := range matrixCoverage(pack, illegal) {
		if probe.Status != result.MatrixProbeMissing {
			t.Fatalf("an illegal expectation witnesses nothing: %+v", probe)
		}
	}
	legal := Matrix{Cases: []evaluation.MatrixCase{{
		ID:                  "plain-allow",
		ExpectedDisposition: json.RawMessage(`{"kind":"outcome","outcomeId":"allow","reasons":[],"handoff":{"state":"none"}}`),
	}}}
	probes := matrixCoverage(pack, legal)
	if len(probes) != 1 || probes[0].Probe != "outcome:allow" || probes[0].Status != result.MatrixProbeCovered {
		t.Fatalf("the same row with a legal disposition witnesses: %+v", probes)
	}
}

// A probe is witnessed by what a row expects, not by what it produced: the
// first witnessing row is named, a reason witnesses exactly its own probe, an
// error row witnesses nothing, and a row whose expectation does not decode is
// dropped here because it fails its own comparison anyway.
func TestMatrixCoverageIsWitnessedByExpectationsOnly(t *testing.T) {
	pack := map[string]any{
		"evidenceRequirements": []any{map[string]any{"id": "receipt", "required": true}},
		"outcomes":             []any{map[string]any{"id": "allow"}},
		"rules":                []any{map[string]any{"id": "r1", "outcome": "allow", "onUnknown": "escalate"}},
		"fallbackOutcome":      "allow",
	}
	matrix := Matrix{Cases: []evaluation.MatrixCase{
		{ID: "errors", Facts: json.RawMessage(`{}`), ExpectedErrorClass: "malformed-input"},
		{ID: "undecodable", Facts: json.RawMessage(`{}`), ExpectedDisposition: json.RawMessage(`[`)},
		{ID: "first-allow", Facts: json.RawMessage(`{}`), ExpectedDisposition: json.RawMessage(`{"kind":"outcome","outcomeId":"allow","reasons":[],"handoff":{"state":"none"}}`)},
		{ID: "second-allow", Facts: json.RawMessage(`{}`), ExpectedDisposition: json.RawMessage(`{"kind":"outcome","outcomeId":"allow","reasons":[],"handoff":{"state":"none"}}`)},
		{ID: "no-receipt", Facts: json.RawMessage(`{}`), ExpectedDisposition: json.RawMessage(`{"kind":"unresolved","reasons":["missing-required-evidence"],"handoff":{"state":"requested","triggeredBy":["missing-required-evidence"]}}`)},
	}}
	probes := matrixCoverage(pack, matrix)
	byName := map[string]result.MatrixProbe{}
	for _, probe := range probes {
		byName[probe.Probe] = probe
	}
	if got := byName["outcome:allow"]; got.Status != result.MatrixProbeCovered || !strings.Contains(got.Detail, `"first-allow"`) {
		t.Fatalf("the first witnessing row is named: %+v", got)
	}
	if got := byName["missing-required-evidence"]; got.Status != result.MatrixProbeCovered {
		t.Fatalf("a reason witnesses its probe: %+v", got)
	}
	if got := byName["unknown"]; got.Status != result.MatrixProbeMissing {
		t.Fatalf("a reason witnesses only its own probe: %+v", got)
	}
}

// Coverage rides beside the rows: it is derived whenever the matrix loaded —
// mismatched rows included, because their expectations exist whether or not
// they held — and never moves a status. A pack with no matrix has no rows to
// read and carries no coverage.
func TestMatrixCoverageIsReportedBesideRowsAndMovesNoStatus(t *testing.T) {
	pack := string(packFixture(t))
	facts := `{"request":{"type":"data-access","completeness":"complete","appropriateness":"hard-fail","embargoedInformationToUnauthorizedRecipients":false}}`
	evidence := `{"intake-form":"present","sponsor-endorsement":"present"}`
	matrix := `{"matrixVersion":"1","cases":[
	  {"id":"hard-fail","facts":` + facts + `,"evidenceAvailability":` + evidence + `,"expectedDisposition":{"kind":"outcome","outcomeId":"decline-redirect","reasons":[],"handoff":{"state":"none"}}}
	]}`
	configPath := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"},"bare":{"path":"packs/a.json"}}}`,
		map[string]string{"packs/a.json": pack, "packs/a.matrix.json": matrix})
	run, failure := mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}

	// The fixture declares three outcomes, applicability, required evidence,
	// escalating rules with three distinct outcomes, and a fallback: seven
	// probes, of which this one-row matrix witnesses exactly one.
	entry := run.Packs[0]
	if entry.Status != "passed" || len(entry.Coverage) != 7 {
		t.Fatalf("entry = %s with %d probes: %+v", entry.Status, len(entry.Coverage), entry.Coverage)
	}
	covered := 0
	for _, probe := range entry.Coverage {
		if probe.Status == result.MatrixProbeCovered {
			covered++
			if probe.Probe != "outcome:decline-redirect" {
				t.Fatalf("only the expected outcome is witnessed: %+v", probe)
			}
		}
	}
	if covered != 1 {
		t.Fatalf("covered = %d: %+v", covered, entry.Coverage)
	}
	// Missing probes moved nothing: the entry passed, the run passed, and the
	// matrix-less pack is skipped with no coverage to read.
	if run.Status != "passed" {
		t.Fatalf("coverage must not gate: %+v", run.Summary)
	}
	if bare := run.Packs[1]; bare.Status != "skipped" || bare.Coverage != nil {
		t.Fatalf("no matrix, no coverage: %+v", bare)
	}

	// A mismatching entry still reports what its matrix fails to probe.
	drifted := strings.Replace(matrix, "decline-redirect", "proceed", 1)
	configPath = writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
		map[string]string{"packs/a.json": pack, "packs/a.matrix.json": drifted})
	run, failure = mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if run.Packs[0].Status != "mismatch" || len(run.Packs[0].Coverage) != 7 {
		t.Fatalf("a mismatching entry keeps its coverage: %+v", run.Packs[0])
	}
}

// Every witness matcher is pinned by a matrix that covers every probe: a
// mis-wired matcher — the wrong kind, the wrong reason string — flips its probe
// to missing and fails here. The pack derives all six reason-class probes plus
// its outcomes, and a pack whose declarations derive no probe at all carries
// nil coverage rather than an empty list, so the payload member is absent, not
// empty.
func TestMatrixCoverageWitnessesEveryProbeClass(t *testing.T) {
	pack := map[string]any{
		"applicability":        map[string]any{"op": "equals", "path": "/t", "value": "x"},
		"evidenceRequirements": []any{map[string]any{"id": "receipt", "required": true}},
		"outcomes":             []any{map[string]any{"id": "allow"}, map[string]any{"id": "deny"}},
		"rules": []any{
			map[string]any{"id": "r1", "outcome": "allow", "onUnknown": "escalate"},
			map[string]any{"id": "r2", "outcome": "deny"},
		},
		"exceptions": []any{map[string]any{"id": "e1", "effect": "escalate"}},
	}
	unresolved := func(reason string) json.RawMessage {
		return json.RawMessage(`{"kind":"unresolved","reasons":[` + quote(reason) + `],"handoff":{"state":"none"}}`)
	}
	matrix := Matrix{Cases: []evaluation.MatrixCase{
		{ID: "w-allow", Facts: json.RawMessage(`{}`), ExpectedDisposition: json.RawMessage(`{"kind":"outcome","outcomeId":"allow","reasons":[],"handoff":{"state":"none"}}`)},
		{ID: "w-deny", Facts: json.RawMessage(`{}`), ExpectedDisposition: json.RawMessage(`{"kind":"outcome","outcomeId":"deny","reasons":[],"handoff":{"state":"none"}}`)},
		{ID: "w-na", Facts: json.RawMessage(`{}`), ExpectedDisposition: json.RawMessage(`{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"none"}}`)},
		{ID: "w-evidence", Facts: json.RawMessage(`{}`), ExpectedDisposition: unresolved("missing-required-evidence")},
		{ID: "w-unknown", Facts: json.RawMessage(`{}`), ExpectedDisposition: unresolved("unknown")},
		{ID: "w-conflict", Facts: json.RawMessage(`{}`), ExpectedDisposition: unresolved("conflict")},
		{ID: "w-escalation", Facts: json.RawMessage(`{}`), ExpectedDisposition: unresolved("exception-escalation")},
		{ID: "w-no-match", Facts: json.RawMessage(`{}`), ExpectedDisposition: unresolved("no-match")},
	}}
	probes := matrixCoverage(pack, matrix)
	if len(probes) != 8 {
		t.Fatalf("all eight probes derive: %+v", probes)
	}
	for _, probe := range probes {
		if probe.Status != result.MatrixProbeCovered {
			t.Fatalf("every probe class must be witnessed by its matcher: %+v", probe)
		}
	}

	// No derivable probe, no coverage member: nil, not empty.
	if probes := matrixCoverage(map[string]any{"fallbackOutcome": "x"}, matrix); probes != nil {
		t.Fatalf("a pack deriving no probe carries nil coverage: %+v", probes)
	}
}

// The exported derivation is the pack derivation: PackProbes over witnesses
// built the way matrixCoverage builds them yields the disposition probes byte
// for byte, so the refactor that opened the seam for the graph surface
// (ADR-0016) is provably behavior-preserving and the zero Reach narrows
// nothing. The boundary family sits outside that entry point on purpose
// (ADR-0023): it needs a row's facts, which a composed surface may inject
// through an edge rather than state, so PackProbes derives none of it and
// matrixCoverage is exactly PackProbes plus boundaryProbes.
func TestPackProbesMatchesMatrixCoverage(t *testing.T) {
	pack := `{"specVersion":"x","outcomes":[{"id":"a","description":"d"},{"id":"b","description":"d"},{"id":"ghost","description":"d"}],
	  "applicability":{"op":"fact","path":"/x","operator":"exists"},
	  "evidenceRequirements":[{"id":"r","description":"d","required":true}],
	  "rules":[{"id":"one","outcome":"a","onUnknown":"escalate"},
	    {"id":"two","outcome":"b","onUnknown":"escalate","when":{"op":"fact","path":"/amount","operator":"greater-than-or-equal","value":"10"}}]}`
	root := PackRoot([]byte(pack))
	if root == nil {
		t.Fatal("the inline pack must decode")
	}
	matrix := Matrix{Cases: []evaluation.MatrixCase{
		{ID: "good", Facts: json.RawMessage(`{"amount":"10"}`), ExpectedDisposition: json.RawMessage(`{"kind":"outcome","outcomeId":"a","reasons":[],"handoff":{"state":"none"}}`)},
		{ID: "illegal", ExpectedDisposition: json.RawMessage(`{"kind":"outcome","outcomeId":"b","reasons":["unknown"],"handoff":{"state":"none"}}`)},
		{ID: "error-row"},
	}}
	direct := matrixCoverage(root, matrix)
	witnesses := []ProbeWitness{}
	for _, row := range matrix.Cases {
		if len(row.ExpectedDisposition) == 0 {
			continue
		}
		if witness, ok := DecodeWitness(`Row "`+row.ID+`"`, row.ExpectedDisposition); ok {
			witnesses = append(witnesses, witness)
		}
	}
	exported := PackProbes(root, witnesses, Reach{})
	for _, probe := range exported {
		if strings.HasPrefix(probe.Probe, "boundary:") {
			t.Fatalf("the shared derivation derives no boundary probe: %+v", exported)
		}
	}
	if !slices.Equal(direct, append(exported, boundaryProbes(root, matrix)...)) {
		t.Fatalf("the two derivations diverged:\n%+v\n%+v", direct, exported)
	}
	// The ghost outcome is declared but nothing references it: no probe.
	for _, probe := range exported {
		if probe.Probe == "outcome:ghost" {
			t.Fatalf("an unreferenced outcome derives no probe: %+v", exported)
		}
	}
}

// The boundary family: one probe per distinct fact pointer and decimal value a
// pack's conditions compare, witnessed by a row whose own facts place the value
// exactly there. This is the contributed case — a rule whose description says
// "5000 or more" and whose operator says strictly greater — made visible by the
// one row that tells the two encodings apart.
func TestMatrixCoverageDerivesBoundaryProbesFromOrderedComparisons(t *testing.T) {
	pack := map[string]any{
		"outcomes": []any{map[string]any{"id": "review"}},
		"rules": []any{map[string]any{
			"id": "expense-threshold", "outcome": "review",
			"description": "5000 or more spend requires review",
			"when":        map[string]any{"op": "fact", "path": "/expense/amount", "operator": "greater-than", "value": "5000"},
		}},
		"fallbackOutcome": "review",
	}
	outcome := json.RawMessage(`{"kind":"outcome","outcomeId":"review","reasons":[],"handoff":{"state":"none"}}`)
	row := func(id, facts string) evaluation.MatrixCase {
		return evaluation.MatrixCase{ID: id, Facts: json.RawMessage(facts), ExpectedDisposition: outcome}
	}

	// Rows on either side of the threshold say nothing about which side of it
	// the comparison falls on.
	blind := Matrix{Cases: []evaluation.MatrixCase{
		row("under", `{"expense":{"amount":"4999"}}`),
		row("over", `{"expense":{"amount":"5001"}}`),
	}}
	probe := findProbe(t, matrixCoverage(pack, blind), "boundary:/expense/amount:5000")
	if probe.Status != result.MatrixProbeMissing {
		t.Fatalf("neither side of a threshold witnesses it: %+v", probe)
	}
	for _, want := range []string{`"/expense/amount"`, "5000", `rule "expense-threshold" (greater-than)`, "policy text is the arbiter"} {
		if !strings.Contains(probe.Detail, want) {
			t.Fatalf("the missing detail must name %q: %q", want, probe.Detail)
		}
	}

	// The row at exactly the literal witnesses it, and names itself.
	at := Matrix{Cases: append(slices.Clone(blind.Cases), row("at-threshold", `{"expense":{"amount":"5000"}}`))}
	probe = findProbe(t, matrixCoverage(pack, at), "boundary:/expense/amount:5000")
	if probe.Status != result.MatrixProbeCovered || !strings.Contains(probe.Detail, `"at-threshold"`) {
		t.Fatalf("a row at the literal witnesses it and is named: %+v", probe)
	}

	// Equality is the evaluator's own: "5000.0" is the same value, and a second
	// site spelling the literal the other way is the same boundary, not a
	// second one.
	twoSpellings := map[string]any{
		"outcomes": []any{map[string]any{"id": "review"}},
		"rules": []any{
			map[string]any{"id": "a", "outcome": "review", "when": map[string]any{"op": "fact", "path": "/expense/amount", "operator": "greater-than", "value": "5000"}},
			map[string]any{"id": "b", "outcome": "review", "when": map[string]any{"op": "fact", "path": "/expense/amount", "operator": "less-than-or-equal", "value": "5000.0"}},
		},
		"fallbackOutcome": "review",
	}
	decimal := Matrix{Cases: []evaluation.MatrixCase{row("scaled", `{"expense":{"amount":"5000.0"}}`)}}
	boundaries := boundaryNames(matrixCoverage(twoSpellings, decimal))
	if !slices.Equal(boundaries, []string{"boundary:/expense/amount:5000"}) {
		t.Fatalf("one pointer and one value is one probe, however it is spelled: %v", boundaries)
	}
	probe = findProbe(t, matrixCoverage(twoSpellings, decimal), "boundary:/expense/amount:5000")
	if probe.Status != result.MatrixProbeCovered {
		t.Fatalf(`"5000.0" is the value "5000": %+v`, probe)
	}

	// Non-witnesses, each for its own reason: §7.4 cannot compare a JSON number
	// or a non-decimal string at all, an unresolvable pointer places nothing,
	// an error row expects no disposition, an expectation that does not decode
	// mismatches forever, and facts that do not decode are no facts.
	for _, blindRow := range []evaluation.MatrixCase{
		row("json-number", `{"expense":{"amount":5000}}`),
		row("grouped", `{"expense":{"amount":"5,000"}}`),
		row("elsewhere", `{"expense":{"total":"5000"}}`),
		{ID: "error-class", Facts: json.RawMessage(`{"expense":{"amount":"5000"}}`), ExpectedErrorClass: "malformed-input"},
		{ID: "undecodable-expectation", Facts: json.RawMessage(`{"expense":{"amount":"5000"}}`), ExpectedDisposition: json.RawMessage(`[`)},
		{ID: "undecodable-facts", Facts: json.RawMessage(`{"expense":`), ExpectedDisposition: outcome},
	} {
		probe = findProbe(t, matrixCoverage(pack, Matrix{Cases: []evaluation.MatrixCase{blindRow}}), "boundary:/expense/amount:5000")
		if probe.Status != result.MatrixProbeMissing {
			t.Fatalf("row %q must witness nothing: %+v", blindRow.ID, probe)
		}
	}

	// A pack whose conditions compare nothing in order derives no boundary
	// probe, and the disposition family is untouched.
	plain := map[string]any{
		"outcomes":        []any{map[string]any{"id": "review"}},
		"rules":           []any{map[string]any{"id": "a", "outcome": "review", "when": map[string]any{"op": "fact", "path": "/expense/amount", "operator": "equals", "value": "5000"}}},
		"fallbackOutcome": "review",
	}
	if got := boundaryNames(matrixCoverage(plain, at)); len(got) != 0 {
		t.Fatalf("only ordered comparisons have a boundary: %v", got)
	}
}

// The walk is structure-keyed: it visits applicability, every rule's when and
// every exception's when, and descends only through all, any, and not. It never
// reads a condition-shaped object carried as data, because an over-reported
// probe is a demand for a row no facts could ever satisfy (ADR-0023).
func TestBoundaryProbesWalkOnlyDeclaredConditions(t *testing.T) {
	pack := map[string]any{
		"applicability": map[string]any{"op": "not", "condition": map[string]any{
			"op": "fact", "path": "/scope/tier", "operator": "less-than", "value": "2",
		}},
		"outcomes": []any{map[string]any{"id": "review"}},
		"rules": []any{map[string]any{"id": "nested", "outcome": "review", "when": map[string]any{
			"op": "all", "conditions": []any{
				map[string]any{"op": "any", "conditions": []any{
					map[string]any{"op": "fact", "path": "/expense/amount", "operator": "greater-than-or-equal", "value": "5000"},
				}},
				// A condition-shaped object inside a value literal is data the
				// evaluator compares, not a comparison it performs.
				map[string]any{"op": "fact", "path": "/expense/shape", "operator": "equals", "value": map[string]any{
					"op": "fact", "path": "/decoy/one", "operator": "greater-than", "value": "1",
				}},
			},
		}},
		},
		"exceptions": []any{map[string]any{"id": "vip", "effect": "escalate", "when": map[string]any{
			"op": "fact", "path": "/vendor/tenure", "operator": "greater-than", "value": "10",
		}}},
		// An extension slot is not a door the walk opens.
		"extensions": map[string]any{"example.invalid/x": map[string]any{
			"op": "fact", "path": "/decoy/two", "operator": "less-than", "value": "3",
		}},
		"fallbackOutcome": "review",
	}
	want := []string{
		"boundary:/scope/tier:2",
		"boundary:/expense/amount:5000",
		"boundary:/vendor/tenure:10",
	}
	if got := boundaryNames(matrixCoverage(pack, Matrix{})); !slices.Equal(got, want) {
		t.Fatalf("walk order is applicability, rules, exceptions, depth first: %v, want %v", got, want)
	}
}

// The Study 010 shape: one threshold compared at three sites with two
// operators, and a second threshold below it. Identity is per pointer and
// value, so the three sites are one probe — one row settles all of them — and
// the row at 70 leaves the 40 boundary honestly missing.
func TestBoundaryProbesMergeSitesSharingAPointerAndValue(t *testing.T) {
	fact := func(operator, value string) map[string]any {
		return map[string]any{"op": "fact", "path": "/vendor/riskScore", "operator": operator, "value": value}
	}
	pack := map[string]any{
		"outcomes": []any{map[string]any{"id": "review"}, map[string]any{"id": "clear"}},
		"rules": []any{
			map[string]any{"id": "review-high-risk", "outcome": "review", "when": fact("greater-than-or-equal", "70")},
			map[string]any{"id": "review-personal-data-midband", "outcome": "review", "when": map[string]any{
				"op": "all", "conditions": []any{
					fact("less-than", "70"),
					map[string]any{"op": "fact", "path": "/vendor/handlesPersonalData", "operator": "equals", "value": true},
				},
			}},
			map[string]any{"id": "clear-low-risk", "outcome": "clear", "when": map[string]any{
				"op": "all", "conditions": []any{
					fact("less-than", "40"),
					map[string]any{"op": "not", "condition": fact("greater-than-or-equal", "70")},
				},
			}},
		},
	}
	want := []string{"boundary:/vendor/riskScore:70", "boundary:/vendor/riskScore:40"}
	probes := matrixCoverage(pack, Matrix{Cases: []evaluation.MatrixCase{{
		ID:                  "at-seventy",
		Facts:               json.RawMessage(`{"vendor":{"riskScore":"70","handlesPersonalData":false}}`),
		ExpectedDisposition: json.RawMessage(`{"kind":"outcome","outcomeId":"review","reasons":[],"handoff":{"state":"none"}}`),
	}}})
	if got := boundaryNames(probes); !slices.Equal(got, want) {
		t.Fatalf("three sites at one value are one probe: %v, want %v", got, want)
	}
	if got := findProbe(t, probes, "boundary:/vendor/riskScore:70"); got.Status != result.MatrixProbeCovered {
		t.Fatalf("one row at 70 settles every site comparing 70: %+v", got)
	}
	// The merged probe still names each site and its operator, because the
	// operator is the half that can disagree with the prose.
	missing := findProbe(t, probes, "boundary:/vendor/riskScore:40")
	if missing.Status != result.MatrixProbeMissing || !strings.Contains(missing.Detail, `rule "clear-low-risk" (less-than)`) {
		t.Fatalf("the 40 boundary is unwitnessed and names its site: %+v", missing)
	}
}

// Witness eligibility is per stage, read off §8's evaluation order, and §8 has
// three stages where a pack's own condition is read: applicability (step 1),
// every exception's when (step 3), and the normal rules that survive step 5
// (steps 6-7). A row's facts at the literal are only half of the test.
//
// Applicability is exercised by any row whose expectation decodes —
// applicability runs first, and a not-applicable disposition is what that
// comparison produced. An exception's when is exercised by every row except a
// not-applicable one: §8 records missing evidence at step 2 without returning
// and halts at step 5 "after all exception effects have been inspected". A
// normal rule's when additionally excludes every expectation that proves that
// step-5 halt — a retained missing-required-evidence, whatever else it is
// retained beside, and a retained exception-escalation.
func TestBoundaryWitnessEligibilityFollowsTheEvaluationOrder(t *testing.T) {
	pack := map[string]any{
		"outcomes":             []any{map[string]any{"id": "review"}},
		"evidenceRequirements": []any{map[string]any{"id": "invoice", "description": "d", "required": true}},
		"applicability": map[string]any{
			"op": "fact", "path": "/scope/tier", "operator": "greater-than-or-equal", "value": "5000",
		},
		"exceptions": []any{map[string]any{"id": "vip", "effect": "escalate", "when": map[string]any{
			"op": "fact", "path": "/vendor/tenure", "operator": "greater-than", "value": "5000",
		}}},
		"rules": []any{map[string]any{"id": "threshold", "outcome": "review", "when": map[string]any{
			"op": "fact", "path": "/expense/amount", "operator": "greater-than", "value": "5000",
		}}},
		"fallbackOutcome": "review",
	}
	// All three pointers carry exactly the compared literal in every row below,
	// so the facts never decide the difference: only the expectation does.
	facts := json.RawMessage(`{"scope":{"tier":"5000"},"vendor":{"tenure":"5000"},"expense":{"amount":"5000"}}`)
	for _, testCase := range []struct {
		id            string
		expectation   string
		exceptionsRan bool
		rulesRan      bool
	}{
		// Step 1 halts here; the applicability comparison is what produced it,
		// and nothing after step 1 was evaluated.
		{"not-applicable", `{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"none"}}`, false, false},
		// The same halt spelled as an unresolved carrying the not-applicable
		// reason: the disposition grammar pins the reason set only for the
		// kind, so this decodes — and it must mean the same step-1 halt, or a
		// row expecting it would witness exceptions that were never evaluated.
		{"not-applicable-as-reason", `{"kind":"unresolved","reasons":["not-applicable"],"handoff":{"state":"none"}}`, false, false},
		// Step 2 records the reason and returns nothing: every exception was
		// still evaluated, and the halt this reason causes is step 5's, before
		// the normal rules.
		{"missing-evidence", `{"kind":"unresolved","reasons":["missing-required-evidence"],"handoff":{"state":"none"}}`, true, false},
		// A retained missing-required-evidence proves the step-5 halt however
		// many other reasons it is retained beside.
		{"missing-and-unknown", `{"kind":"unresolved","reasons":["missing-required-evidence","unknown"],"handoff":{"state":"none"}}`, true, false},
		// A true escalate exception is one of step 5's named halting states, so
		// its reason proves the normal rules were skipped too — and it proves
		// the exceptions themselves were all evaluated.
		{"exception-escalation", `{"kind":"unresolved","reasons":["exception-escalation"],"handoff":{"state":"requested","triggeredBy":["exception-escalation"]}}`, true, false},
		// Reason unknown is admitted at both later stages: §8 reaches it at an
		// escalating rule as well as at applicability, at evidence, and at an
		// escalating exception, and no derivation over declarations can tell
		// those apart.
		{"unknown", `{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none"}}`, true, true},
		// An outcome is produced by a rule or the fallback, so rules ran.
		{"outcome", `{"kind":"outcome","outcomeId":"review","reasons":[],"handoff":{"state":"none"}}`, true, true},
	} {
		probes := matrixCoverage(pack, Matrix{Cases: []evaluation.MatrixCase{{
			ID: testCase.id, Facts: facts, ExpectedDisposition: json.RawMessage(testCase.expectation),
		}}})
		applicability := findProbe(t, probes, "boundary:/scope/tier:5000")
		if applicability.Status != result.MatrixProbeCovered {
			t.Fatalf("row %q exercised the applicability comparison whatever it expects: %+v", testCase.id, applicability)
		}
		for _, staged := range []struct {
			name    string
			probe   string
			reached bool
		}{
			{"exception", "boundary:/vendor/tenure:5000", testCase.exceptionsRan},
			{"rule", "boundary:/expense/amount:5000", testCase.rulesRan},
		} {
			probe := findProbe(t, probes, staged.probe)
			want := result.MatrixProbeMissing
			if staged.reached {
				want = result.MatrixProbeCovered
			}
			if probe.Status != want {
				t.Fatalf("row %q at the %s's literal: %+v, want %s", testCase.id, staged.name, probe, want)
			}
		}
	}
}

// One pointer and one value compared at two stages is one probe, and covering
// it takes a witness for each stage. Grouping must not mask: an
// applicability-only witness settles the applicability copy of the comparison
// and says nothing about the rule copy, so the merged probe stays missing —
// and its sentence names the site still unprobed, not both sites — until a row
// that could have reached the rule places the value there too.
func TestBoundaryCoverageIsPerStageSoGroupingCannotMask(t *testing.T) {
	pack := map[string]any{
		"outcomes":      []any{map[string]any{"id": "review"}},
		"applicability": map[string]any{"op": "fact", "path": "/expense/amount", "operator": "greater-than-or-equal", "value": "5000"},
		"rules": []any{map[string]any{"id": "threshold", "outcome": "review", "when": map[string]any{
			"op": "fact", "path": "/expense/amount", "operator": "greater-than", "value": "5000.0",
		}}},
		"fallbackOutcome": "review",
	}
	facts := json.RawMessage(`{"expense":{"amount":"5000"}}`)
	notApplicable := evaluation.MatrixCase{
		ID: "not-applicable", Facts: facts,
		ExpectedDisposition: json.RawMessage(`{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"none"}}`),
	}
	reachesTheRule := evaluation.MatrixCase{
		ID: "at-threshold", Facts: facts,
		ExpectedDisposition: json.RawMessage(`{"kind":"outcome","outcomeId":"review","reasons":[],"handoff":{"state":"none"}}`),
	}

	masked := matrixCoverage(pack, Matrix{Cases: []evaluation.MatrixCase{notApplicable}})
	if got := boundaryNames(masked); !slices.Equal(got, []string{"boundary:/expense/amount:5000"}) {
		t.Fatalf("the two sites are one boundary: %v", got)
	}
	probe := findProbe(t, masked, "boundary:/expense/amount:5000")
	if probe.Status != result.MatrixProbeMissing {
		t.Fatalf("an applicability-only witness cannot settle the rule site: %+v", probe)
	}
	if !strings.Contains(probe.Detail, `rule "threshold" (greater-than)`) || strings.Contains(probe.Detail, "applicability") {
		t.Fatalf("the sentence names the unwitnessed site and only that one: %q", probe.Detail)
	}

	// Both stages witnessed, by two different rows, is covered — and the
	// sentence names both rows, because neither one answers for the other.
	covered := findProbe(t, matrixCoverage(pack, Matrix{Cases: []evaluation.MatrixCase{notApplicable, reachesTheRule}}),
		"boundary:/expense/amount:5000")
	if covered.Status != result.MatrixProbeCovered {
		t.Fatalf("a witness at every stage covers the boundary: %+v", covered)
	}
	for _, want := range []string{`"not-applicable"`, `"at-threshold"`} {
		if !strings.Contains(covered.Detail, want) {
			t.Fatalf("the covered detail names %s: %q", want, covered.Detail)
		}
	}

	// One row eligible for the strictest stage is eligible for every earlier
	// one, so it answers for the whole boundary by itself.
	alone := findProbe(t, matrixCoverage(pack, Matrix{Cases: []evaluation.MatrixCase{reachesTheRule}}),
		"boundary:/expense/amount:5000")
	if alone.Status != result.MatrixProbeCovered || strings.Contains(alone.Detail, "one for each stage") {
		t.Fatalf("one rule-eligible row settles both stages: %+v", alone)
	}
}

// One rule may compare the same pointer against the same value twice — once
// plainly and once under a not — and the missing sentence names that member
// once, because the second mention tells a reader nothing the first did not.
func TestBoundaryMissingDetailNamesEachSiteOnce(t *testing.T) {
	fact := map[string]any{"op": "fact", "path": "/x", "operator": "greater-than", "value": "5"}
	pack := map[string]any{
		"outcomes": []any{map[string]any{"id": "review"}},
		"rules": []any{map[string]any{"id": "twice", "outcome": "review", "when": map[string]any{
			"op": "all", "conditions": []any{fact, map[string]any{"op": "not", "condition": fact}},
		}}},
		"fallbackOutcome": "review",
	}
	probe := findProbe(t, matrixCoverage(pack, Matrix{}), "boundary:/x:5")
	if got := strings.Count(probe.Detail, `rule "twice" (greater-than)`); got != 1 {
		t.Fatalf("a repeated site is named once, got %d: %q", got, probe.Detail)
	}
}

// The site list itself is capped: one boundary compared at many declarations
// names the first six and counts the rest, so the sentence cannot grow with
// the pack.
func TestBoundaryMissingDetailCapsTheSiteList(t *testing.T) {
	rules := make([]any, 0, 8)
	for index := range 8 {
		rules = append(rules, map[string]any{
			"id": fmt.Sprintf("rule-%d", index), "outcome": "review",
			"when": map[string]any{"op": "fact", "path": "/x", "operator": "greater-than", "value": "5"},
		})
	}
	pack := map[string]any{"outcomes": []any{map[string]any{"id": "review"}}, "rules": rules, "fallbackOutcome": "review"}
	probe := findProbe(t, matrixCoverage(pack, Matrix{}), "boundary:/x:5")
	if !strings.Contains(probe.Detail, `rule "rule-5" (greater-than)`) || strings.Contains(probe.Detail, `rule "rule-6"`) {
		t.Fatalf("the first six sites are named: %q", probe.Detail)
	}
	if !strings.Contains(probe.Detail, "(and 2 more)") {
		t.Fatalf("the rest are counted: %q", probe.Detail)
	}
}

// Pointers, literals, and declaration ids are authored strings the carrier
// bounds only at a megabyte each, and this family repeats them in a probe name
// and in a sentence. Every one of them is rendered under a fixed budget, and
// the overflow is replaced by a digest of the authored bytes, so two strings
// that agree for the whole budget still name two different probes — and the
// same pack names them the same way every time.
func TestBoundaryRenderingIsCappedAndStillDistinct(t *testing.T) {
	long := func(suffix string) string { return "/" + strings.Repeat("a", 4096) + suffix }
	digits := strings.Repeat("9", 4096)
	pack := map[string]any{
		"outcomes": []any{map[string]any{"id": "review"}},
		"rules": []any{
			map[string]any{"id": strings.Repeat("z", 4096), "outcome": "review", "when": map[string]any{
				"op": "fact", "path": long("one"), "operator": "greater-than", "value": digits + "1",
			}},
			map[string]any{"id": "short", "outcome": "review", "when": map[string]any{
				"op": "fact", "path": long("two"), "operator": "less-than", "value": digits + "2",
			}},
		},
		"fallbackOutcome": "review",
	}
	probes := matrixCoverage(pack, Matrix{})
	names := boundaryNames(probes)
	if len(names) != 2 || names[0] == names[1] {
		t.Fatalf("two compared pairs are two distinct probes: %v", names)
	}
	if !slices.Equal(names, boundaryNames(matrixCoverage(pack, Matrix{}))) {
		t.Fatalf("the same pack derives the same names: %v", names)
	}
	for _, probe := range probes {
		if !strings.HasPrefix(probe.Probe, "boundary:") {
			continue
		}
		// "boundary:" plus a capped pointer, a colon, and a capped literal.
		if want := len("boundary:") + 2*boundaryTextBudget + 1; len(probe.Probe) > want {
			t.Fatalf("probe name is %d bytes, over the %d-byte budget: %q", len(probe.Probe), want, probe.Probe)
		}
		if len(probe.Detail) > 1024 {
			t.Fatalf("missing sentence is %d bytes: %q", len(probe.Detail), probe.Detail)
		}
	}
	// The capped rendering is the prefix, an ellipsis, and a digest of the
	// authored bytes — not a truncation that would collide.
	first, second := capRendered(long("one")), capRendered(long("two"))
	if first == second || !strings.Contains(first, "…") || len(first) > boundaryTextBudget {
		t.Fatalf("capped renderings must stay distinct and bounded: %q, %q", first, second)
	}
	if short := "/vendor/riskScore"; capRendered(short) != short {
		t.Fatalf("text inside the budget is rendered whole: %q", capRendered(short))
	}
}

func findProbe(t *testing.T, probes []result.MatrixProbe, name string) result.MatrixProbe {
	t.Helper()
	for _, probe := range probes {
		if probe.Probe == name {
			return probe
		}
	}
	t.Fatalf("no probe named %q in %+v", name, probes)
	return result.MatrixProbe{}
}

func boundaryNames(probes []result.MatrixProbe) []string {
	var names []string
	for _, probe := range probes {
		if strings.HasPrefix(probe.Probe, "boundary:") {
			names = append(names, probe.Probe)
		}
	}
	return names
}

// Reach narrows exactly the two evidence doors. A required requirement that
// can only be present or unknown cannot be missing; one that can only be
// present or absent keeps missing-required-evidence, and reason unknown then
// survives only through its other doors.
func TestReachNarrowsTheEvidenceDoors(t *testing.T) {
	// One required requirement, no applicability, no escalating onUnknown: the
	// unknown reason's only door is the evidence side.
	pack := `{"outcomes":[{"id":"a","description":"d"}],
	  "evidenceRequirements":[{"id":"r","description":"d","required":true}],
	  "rules":[{"id":"one","outcome":"a","onUnknown":"ignore"}],"fallbackOutcome":"a"}`
	root := PackRoot([]byte(pack))

	unnarrowed := ReachableReasons(root, Reach{})
	if !slices.Contains(unnarrowed, evaluation.ReasonMissingEvidence) || !slices.Contains(unnarrowed, evaluation.ReasonUnknown) {
		t.Fatalf("the zero Reach narrows nothing: %v", unnarrowed)
	}
	neverAbsent := ReachableReasons(root, Reach{EvidenceStates: func(string) []string { return []string{"present", "unknown"} }})
	if slices.Contains(neverAbsent, evaluation.ReasonMissingEvidence) {
		t.Fatalf("a requirement that cannot be absent cannot be missing: %v", neverAbsent)
	}
	if !slices.Contains(neverAbsent, evaluation.ReasonUnknown) {
		t.Fatalf("unknown stays reachable through the evidence door: %v", neverAbsent)
	}
	neverUnknown := ReachableReasons(root, Reach{EvidenceStates: func(string) []string { return []string{"present", "absent"} }})
	if !slices.Contains(neverUnknown, evaluation.ReasonMissingEvidence) {
		t.Fatalf("absence reachable keeps the missing probe: %v", neverUnknown)
	}
	if slices.Contains(neverUnknown, evaluation.ReasonUnknown) {
		t.Fatalf("with the evidence door closed and no other door, unknown is unreachable: %v", neverUnknown)
	}

	// An escalating rule reopens unknown regardless of the narrowing.
	escalating := PackRoot([]byte(`{"outcomes":[{"id":"a","description":"d"}],
	  "evidenceRequirements":[{"id":"r","description":"d","required":true}],
	  "rules":[{"id":"one","outcome":"a","onUnknown":"escalate"}],"fallbackOutcome":"a"}`))
	reopened := ReachableReasons(escalating, Reach{EvidenceStates: func(string) []string { return []string{"present", "absent"} }})
	if !slices.Contains(reopened, evaluation.ReasonUnknown) {
		t.Fatalf("the rule door is untouched by evidence narrowing: %v", reopened)
	}
}

// configVersion "2" is the shape with graphs; "1" the shape without, still
// read. The version gate lives in the schema's own bytes: graphs declared
// under "1" name the exact member to change, a "2" without graphs is legal,
// and the graph entries get the same closed-shape treatment pack entries get.
func TestConfigVersionTwoDeclaresGraphs(t *testing.T) {
	_, failure := Load(writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"a.json"}},"graphs":{"g":{"path":"g.json"}}}`, nil))
	if failure == nil || failure.Code != "JPS-PROJECT-CONFIG-SCHEMA" || !strings.Contains(failure.Message, "'2'") {
		t.Fatalf("graphs under configVersion 1 name the member to change: %+v", failure)
	}

	loaded, failure := Load(writeProject(t, `{"configVersion":"2","packs":{"a":{"path":"a.json"}},
	  "graphs":{"zeta":{"path":"z.json"},"alpha":{"path":"a.graph.json","rows":"a.rows.json","description":"d"}}}`, nil))
	if failure != nil {
		t.Fatal(failure.Message)
	}
	defer loaded.Close()
	if !slices.Equal(loaded.GraphIDs, []string{"alpha", "zeta"}) {
		t.Fatalf("graph ids are sorted: %v", loaded.GraphIDs)
	}
	entry, ok := loaded.GraphEntry("alpha")
	if !ok || entry.Path != "a.graph.json" || entry.Rows != "a.rows.json" {
		t.Fatalf("entry = %+v", entry)
	}
	if failure := loaded.UnknownGraphFailure("ghost"); failure.Code != "JPS-PROJECT-UNKNOWN-GRAPH" ||
		!strings.Contains(failure.Message, "alpha, zeta") || failure.ExitCode != result.ExitUnsupported {
		t.Fatalf("the refusal lists configured graph ids: %+v", failure)
	}

	if loaded, failure := Load(writeProject(t, `{"configVersion":"2","packs":{"a":{"path":"a.json"}}}`, nil)); failure != nil {
		t.Fatalf("a 2 without graphs is legal: %+v", failure)
	} else {
		loaded.Close()
	}

	rejected := map[string]string{
		"an empty graphs object":     `{"configVersion":"2","packs":{"a":{"path":"a.json"}},"graphs":{}}`,
		"a graph without a path":     `{"configVersion":"2","packs":{"a":{"path":"a.json"}},"graphs":{"g":{"rows":"r.json"}}}`,
		"a graph key outside the id": `{"configVersion":"2","packs":{"a":{"path":"a.json"}},"graphs":{"Bad_Key":{"path":"g.json"}}}`,
		"an unknown graph member":    `{"configVersion":"2","packs":{"a":{"path":"a.json"}},"graphs":{"g":{"path":"g.json","expectedVersion":"0.1.0"}}}`,
	}
	for name, config := range rejected {
		t.Run(name, func(t *testing.T) {
			if _, failure := Load(writeProject(t, config, nil)); failure == nil || failure.Code != "JPS-PROJECT-CONFIG-SCHEMA" {
				t.Fatalf("this configuration must be refused by the schema: %+v", failure)
			}
		})
	}

	// The schema payload names the newest shape and every shape read, so it
	// cannot imply an earlier one stopped being accepted.
	description := SchemaDescription("packs schema")
	if description.ConfigVersion != "3" || !slices.Equal(description.SupportedConfigVersions, []string{"1", "2", "3"}) ||
		description.SchemaID != "urn:judgmentpack:runtime:jpack-config:3" {
		t.Fatalf("schema description = %+v", description)
	}
}

// configVersion "3" is the shape that may ask for an audit trail; "2" and "1",
// the shapes without it, are still read. The version gate lives in the
// schema's own bytes and nowhere else: audit declared under an earlier version
// names the exact member to change, a "3" without audit is legal, a "3" may
// still declare graphs, and the audit entry gets the closed-shape treatment
// every other entry gets.
func TestConfigVersionThreeDeclaresTheAuditDirectory(t *testing.T) {
	for name, config := range map[string]string{
		"under configVersion 1": `{"configVersion":"1","packs":{"a":{"path":"a.json"}},"audit":{"dir":"audit"}}`,
		"under configVersion 2": `{"configVersion":"2","packs":{"a":{"path":"a.json"}},"audit":{"dir":"audit"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			// The gate is a schema refusal, not a version refusal: the declared
			// version is one this runtime reads, and what it does not read is
			// this member under that version. That is the diagnostic the gate
			// exists to produce, and it names the value to change.
			_, failure := Load(writeProject(t, config, nil))
			if failure == nil || failure.Code != "JPS-PROJECT-CONFIG-SCHEMA" || !strings.Contains(failure.Message, "'3'") {
				t.Fatalf("audit under an earlier configVersion names the member to change: %+v", failure)
			}
			if failure.ExitCode != result.ExitInvalid {
				t.Fatalf("exit = %d, want the invalid class", failure.ExitCode)
			}
		})
	}

	loaded, failure := Load(writeProject(t, `{"configVersion":"3","packs":{"a":{"path":"a.json"}},
	  "audit":{"dir":"records/evaluations"},"graphs":{"g":{"path":"g.json"}}}`, nil))
	if failure != nil {
		t.Fatal(failure.Message)
	}
	defer loaded.Close()
	if loaded.Config.Audit == nil || loaded.Config.Audit.Dir != "records/evaluations" {
		t.Fatalf("audit = %+v", loaded.Config.Audit)
	}
	if !slices.Equal(loaded.GraphIDs, []string{"g"}) {
		t.Fatalf("a 3 may still declare graphs: %v", loaded.GraphIDs)
	}
	if loaded.AuditWriter() == nil {
		t.Fatal("a declared audit directory produces a writer")
	}

	// Absence is distinguishable from declaration with defaults, which is the
	// whole reason the member is a pointer, and a project that declares no
	// audit member has nothing to write with.
	if plain, failure := Load(writeProject(t, `{"configVersion":"3","packs":{"a":{"path":"a.json"}}}`, nil)); failure != nil {
		t.Fatalf("a 3 without audit is legal: %+v", failure)
	} else {
		defer plain.Close()
		if plain.Config.Audit != nil || plain.AuditWriter() != nil {
			t.Fatalf("audit = %+v", plain.Config.Audit)
		}
	}

	rejected := map[string]string{
		"an audit entry with no dir":     `{"configVersion":"3","packs":{"a":{"path":"a.json"}},"audit":{}}`,
		"an unknown audit member":        `{"configVersion":"3","packs":{"a":{"path":"a.json"}},"audit":{"dir":"audit","rotate":"daily"}}`,
		"an audit dir that is not text":  `{"configVersion":"3","packs":{"a":{"path":"a.json"}},"audit":{"dir":["audit"]}}`,
		"an empty audit dir":             `{"configVersion":"3","packs":{"a":{"path":"a.json"}},"audit":{"dir":""}}`,
		"an audit member that is a path": `{"configVersion":"3","packs":{"a":{"path":"a.json"}},"audit":"audit"}`,
	}
	for name, config := range rejected {
		t.Run(name, func(t *testing.T) {
			if _, failure := Load(writeProject(t, config, nil)); failure == nil || failure.Code != "JPS-PROJECT-CONFIG-SCHEMA" {
				t.Fatalf("this configuration must be refused by the schema: %+v", failure)
			}
		})
	}
}

// A matrix is a closed input, so the member that ADR-0025 adds moves
// matrixVersion to "2" — VERSIONING.md's rule for closed inputs, not its
// additive rule for output.
//
// The grid below is the whole contract: a matrix declaring nothing, or "1", is
// read as the shape it was written for and refuses the new member by name; a
// matrix declaring "2" admits all three assertion states. The version is
// settled before anything version-specific decodes, so a document declaring a
// version this runtime has never heard of is told that, rather than told that
// one of its members is unknown — true, uninformative, and pointing at the
// wrong repair.
func TestTheHandoffTargetAssertionRequiresMatrixVersionTwo(t *testing.T) {
	pack := string(packFixture(t))
	notApplicable := `{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`
	target := `{"kind":"human-role","name":"Intake reviewer"}`

	for _, version := range []struct {
		declared string
		prefix   string
		admits   bool
	}{
		{declared: "omitted", prefix: `{`, admits: false},
		{declared: "1", prefix: `{"matrixVersion":"1",`, admits: false},
		{declared: "2", prefix: `{"matrixVersion":"2",`, admits: true},
	} {
		for _, assertion := range []struct {
			name   string
			member string
			states bool
		}{
			{name: "no assertion", member: "", states: false},
			{name: "null", member: `,"expectedHandoffTarget":null`, states: true},
			{name: "a target", member: `,"expectedHandoffTarget":` + target, states: true},
		} {
			t.Run("matrixVersion "+version.declared+" with "+assertion.name, func(t *testing.T) {
				matrix := version.prefix + `"cases":[{"id":"out-of-scope","facts":{"request":{"type":"unrelated"}},"expectedDisposition":` +
					notApplicable + assertion.member + `}]}`
				configPath := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
					map[string]string{"packs/a.json": pack, "packs/a.matrix.json": matrix})
				loaded := mustLoad(t, configPath)
				_, err := loaded.LoadMatrix(loaded.Config.Packs["a"])
				if !assertion.states || version.admits {
					if err != nil {
						t.Fatalf("this matrix is well formed: %v", err)
					}
					return
				}
				if err == nil {
					t.Fatal("a v1 matrix must not admit a v2 member")
				}
				// The refusal names the version that would take it, because
				// "expectedHandoffTarget is not a member" is a false sentence to
				// print at someone whose only mistake was not moving matrixVersion.
				for _, want := range []string{"expectedHandoffTarget", "matrixVersion 2", `"2"`} {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("the refusal must contain %q: %v", want, err)
					}
				}
			})
		}
	}

	// The version is settled first. A matrix declaring a version this runtime
	// does not know is told exactly that, even when it also carries a member
	// that version would have introduced — which is what "preflight before
	// version-specific decoding" buys.
	for name, matrix := range map[string]string{
		"a future version alone":                            `{"matrixVersion":"9","cases":[{"id":"a","facts":{},"expectedErrorClass":"malformed-input"}]}`,
		"a future version with a member it would introduce": `{"matrixVersion":"9","cases":[{"id":"a","facts":{},"expectedDisposition":` + notApplicable + `,"expectedHandoffTarget":` + target + `,"somethingFromNine":1}]}`,
		"a future version with a null assertion":            `{"matrixVersion":"9","cases":[{"id":"a","facts":{},"expectedDisposition":` + notApplicable + `,"expectedHandoffTarget":null}]}`,
		"a version of the wrong type":                       `{"matrixVersion":2,"cases":[{"id":"a","facts":{},"expectedErrorClass":"malformed-input"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			configPath := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
				map[string]string{"packs/a.json": pack, "packs/a.matrix.json": matrix})
			loaded := mustLoad(t, configPath)
			_, err := loaded.LoadMatrix(loaded.Config.Packs["a"])
			if err == nil || !strings.Contains(err.Error(), "matrixVersion") {
				t.Fatalf("the version is what this refusal is about: %v", err)
			}
			if strings.Contains(err.Error(), "member this runtime does not know") {
				t.Fatalf("the version must be settled before the members are: %v", err)
			}
			if !strings.Contains(err.Error(), "1, 2") {
				t.Fatalf("the refusal must say what it would take: %v", err)
			}
		})
	}
}

// A member spelled in another case is refused rather than read as the member it
// case-folds onto (ADR-0025).
//
// encoding/json matches member names case-insensitively even under
// DisallowUnknownFields, so `{"Facts":…}` and `{"ExpectedHandoffTarget":…}`
// decode into the exact members and a document carrying both spellings has one
// silently overwrite the other. A closed shape that admits an alias is not
// closed, so the names are checked against the carrier-decoded document, where
// every authored name is still present verbatim.
func TestAMemberSpelledInAnotherCaseIsRefusedRatherThanRead(t *testing.T) {
	pack := string(packFixture(t))
	notApplicable := `{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`
	for name, matrix := range map[string]string{
		"a capitalized row member": `{"matrixVersion":"2","cases":[{"id":"a","Facts":{},"expectedErrorClass":"malformed-input"}]}`,
		"a capitalized assertion":  `{"matrixVersion":"2","cases":[{"id":"a","facts":{},"expectedDisposition":` + notApplicable + `,"ExpectedHandoffTarget":{"kind":"queue","name":"Ops"}}]}`,
		"an alias beside the member": `{"matrixVersion":"2","cases":[{"id":"a","facts":{},"expectedDisposition":` + notApplicable +
			`,"expectedHandoffTarget":{"kind":"human-role","name":"Intake reviewer"},"ExpectedHandoffTarget":{"kind":"queue","name":"Ops"}}]}`,
		"a capitalized root member": `{"MatrixVersion":"2","cases":[{"id":"a","facts":{},"expectedErrorClass":"malformed-input"}]}`,
		"a capitalized cases array": `{"matrixVersion":"2","Cases":[{"id":"a","facts":{},"expectedErrorClass":"malformed-input"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			configPath := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
				map[string]string{"packs/a.json": pack, "packs/a.matrix.json": matrix})
			loaded := mustLoad(t, configPath)
			_, err := loaded.LoadMatrix(loaded.Config.Packs["a"])
			if err == nil {
				t.Fatal("an alias must be refused, not read as the member it folds onto")
			}
			if !strings.Contains(err.Error(), "only in case") {
				t.Fatalf("the refusal must say what it saw: %v", err)
			}
		})
	}
}

// The renderings one run retains are bounded in aggregate, not only per row
// (ADR-0025).
//
// Per-row the budget is result.HandoffTargetBudget; what this bounds is the
// accumulation, which is the product a pack's authored target and a matrix's
// row count make. Crossing it refuses the run rather than truncating it: a
// report cut short looks exactly like a complete one, and a resource limit is a
// statement about neither the pack nor a row, so it is a failure and not a
// mismatch.
func TestTheHandoffTargetReportIsBoundedInAggregate(t *testing.T) {
	pack := string(packFixture(t))
	notApplicable := `{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`
	rows := make([]string, 0, 64)
	for index := range 64 {
		rows = append(rows, fmt.Sprintf(`{"id":"row-%d","facts":{"request":{"type":"unrelated"}},"expectedDisposition":%s,"expectedHandoffTarget":{"kind":"human-role","name":"Intake reviewer"}}`, index, notApplicable))
	}
	matrix := `{"matrixVersion":"2","cases":[` + strings.Join(rows, ",") + `]}`
	configPath := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
		map[string]string{"packs/a.json": pack, "packs/a.matrix.json": matrix})

	// Under the shipped budget the same run is a plain pass, so the refusal
	// below is the budget firing and not the rows being wrong.
	run, failure := mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if run.Status != "passed" || run.Summary.Total != 64 {
		t.Fatalf("run = %+v", run.Summary)
	}

	loaded := mustLoad(t, configPath)
	loaded.handoffTargetReportBudget = 500
	run, failure = loaded.Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure == nil {
		t.Fatalf("the budget must refuse this run: %+v", run.Summary)
	}
	if failure.Code != "JPS-RESOURCE-MATRIX-HANDOFF-TARGETS" || failure.ExitCode != result.ExitIO {
		t.Fatalf("failure = %+v", failure)
	}
	for _, want := range []string{"500-byte budget", "row-", "Nothing is truncated"} {
		if !strings.Contains(failure.Message, want) {
			t.Fatalf("the refusal must contain %q: %q", want, failure.Message)
		}
	}
	// Nothing partial is returned beside the refusal.
	if len(run.Packs) != 0 || run.Summary.Total != 0 {
		t.Fatalf("a refused run writes no report: %+v", run)
	}
	// A run whose rows assert nothing is charged nothing, however many rows it
	// has: the budget is about the members this record added.
	silent := strings.ReplaceAll(matrix, `,"expectedHandoffTarget":{"kind":"human-role","name":"Intake reviewer"}`, "")
	configPath = writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
		map[string]string{"packs/a.json": pack, "packs/a.matrix.json": silent})
	quiet := mustLoad(t, configPath)
	quiet.handoffTargetReportBudget = 1
	if _, failure := quiet.Test(evaluation.NewEngine(newValidator(t)), "", "packs test"); failure != nil {
		t.Fatalf("a suite that asserts nothing spends nothing: %s", failure.Message)
	}
}

// The remaining shapes an asserting row can take, each pinned once (ADR-0025).
//
// Every case here holds the disposition constant or checks it separately, so
// what each one measures is the second comparison and nothing else.
func TestTheHandoffTargetComparisonAcrossTheShapesARowCanTake(t *testing.T) {
	pack := string(packFixture(t))
	escalates := `{"request":{"type":"unrelated"}}`
	notApplicable := `{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`
	target := `{"kind":"human-role","name":"Intake reviewer"}`
	config := `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`
	run := func(t *testing.T, document, matrix string) result.PackTest {
		t.Helper()
		configPath := writeProject(t, config, map[string]string{"packs/a.json": document, "packs/a.matrix.json": matrix})
		report, failure := mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
		if failure != nil {
			t.Fatal(failure.Message)
		}
		return report
	}

	// A passing row reports the exact renderings, not merely equal ones.
	t.Run("a passing row reports both renderings verbatim", func(t *testing.T) {
		report := run(t, pack, `{"matrixVersion":"2","cases":[{"id":"r","facts":`+escalates+`,"expectedDisposition":`+notApplicable+`,"expectedHandoffTarget":`+target+`}]}`)
		row := report.Packs[0].Rows[0]
		if row.Status != "passed" || row.ExpectedHandoffTarget != target || row.ActualHandoffTarget != target {
			t.Fatalf("row = %+v", row)
		}
	})
	t.Run("a passing null row reports the literal on both sides", func(t *testing.T) {
		outcome := `{"request":{"type":"data-access","completeness":"complete","appropriateness":"hard-fail","embargoedInformationToUnauthorizedRecipients":false}}`
		declineRedirect := `{"kind":"outcome","outcomeId":"decline-redirect","reasons":[],"handoff":{"state":"none"}}`
		report := run(t, pack, `{"matrixVersion":"2","cases":[{"id":"r","facts":`+outcome+`,"evidenceAvailability":{"intake-form":"present","sponsor-endorsement":"present"},"expectedDisposition":`+declineRedirect+`,"expectedHandoffTarget":null}]}`)
		row := report.Packs[0].Rows[0]
		if row.Status != "passed" || row.ExpectedHandoffTarget != "null" || row.ActualHandoffTarget != "null" {
			t.Fatalf("row = %+v", row)
		}
	})

	// An unresolved/requested disposition asserting the object target it actually
	// reaches: the escalating path, passing, with both sides naming a
	// destination. The not-applicable case above reaches the target through §8's
	// step 1; this reaches it through a rule that escalated on an unknown, which
	// is the path a real suite exercises most.
	t.Run("an unresolved requested disposition with an object target", func(t *testing.T) {
		unknown := `{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"requested","triggeredBy":["unknown"]}}`
		facts := `{"request":{"type":"data-access","completeness":"complete","appropriateness":"pass"}}`
		matrix := `{"matrixVersion":"2","cases":[{"id":"r","facts":` + facts +
			`,"evidenceAvailability":{"intake-form":"present","sponsor-endorsement":"present"},"expectedDisposition":` + unknown +
			`,"expectedHandoffTarget":` + target + `}]}`
		report := run(t, pack, matrix)
		row := report.Packs[0].Rows[0]
		if report.Status != "passed" || row.Status != "passed" {
			t.Fatalf("row = %+v", row)
		}
		if row.ExpectedHandoffTarget != target || row.ActualHandoffTarget != target {
			t.Fatalf("both sides name the destination: %+v", row)
		}
		// The same row against a pack whose target name differs fails, so the
		// pass above is the comparison agreeing rather than the comparison not
		// running.
		corrupted := strings.Replace(pack, `"Intake reviewer"`, `"Disclosure office"`, 1)
		row = run(t, corrupted, matrix).Packs[0].Rows[0]
		if row.Status != "mismatch" || row.Expected != row.Actual {
			t.Fatalf("the disposition is unchanged and the target is not: %+v", row)
		}
	})

	// A requested handoff with no configured destination is a real §8.1 state:
	// a direct exception escalation on a pack that declares no escalation
	// object. Its assertion is null, and null is what it reports.
	t.Run("a requested handoff with no configured destination", func(t *testing.T) {
		escalate, err := os.ReadFile(filepath.Join("..", "artifacts", "jps", "0.2.0-draft", "cases", "valid", "exception-escalate.json"))
		if err != nil {
			t.Fatal(err)
		}
		requested := `{"kind":"unresolved","reasons":["exception-escalation"],"handoff":{"state":"requested","triggeredBy":["exception-escalation"]}}`
		matrix := `{"matrixVersion":"2","cases":[{"id":"r","facts":{},"expectedDisposition":` + requested + `,"expectedHandoffTarget":null}]}`
		report := run(t, string(escalate), matrix)
		row := report.Packs[0].Rows[0]
		if row.Status != "passed" || row.ActualHandoffTarget != "null" {
			t.Fatalf("a requested handoff can have no destination: %+v", row)
		}
		// And a row asserting a destination for it fails, naming the direction.
		report = run(t, string(escalate), strings.Replace(matrix, `"expectedHandoffTarget":null`, `"expectedHandoffTarget":`+target, 1))
		row = report.Packs[0].Rows[0]
		if row.Status != "mismatch" || !strings.Contains(row.Detail, "the row expects a target and the evaluation reports none") {
			t.Fatalf("row = %+v", row)
		}
	})

	// A kind outside §8.1's enumeration is a mismatch and not a refusal: the
	// vocabulary belongs to the pack schema, which has already held the pack to
	// it, and this runtime does not keep a second copy of the list.
	t.Run("an unknown kind mismatches rather than being refused", func(t *testing.T) {
		report := run(t, pack, `{"matrixVersion":"2","cases":[{"id":"r","facts":`+escalates+`,"expectedDisposition":`+notApplicable+`,"expectedHandoffTarget":{"kind":"wizard","name":"Intake reviewer"}}]}`)
		row := report.Packs[0].Rows[0]
		if row.Status != "mismatch" || !strings.Contains(row.Detail, "both name a target") {
			t.Fatalf("row = %+v", row)
		}
		if row.ExpectedHandoffTarget != `{"kind":"wizard","name":"Intake reviewer"}` {
			t.Fatalf("the row's own words are reported back: %+v", row)
		}
	})

	// Both comparisons can fail at once. The detail names the disposition,
	// because that is the first difference, and the payload still carries every
	// side of both — which is what the two members are for.
	t.Run("a row that fails both comparisons pins both", func(t *testing.T) {
		wrong := `{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"requested","triggeredBy":["unknown"]}}`
		report := run(t, pack, `{"matrixVersion":"2","cases":[{"id":"r","facts":`+escalates+`,"expectedDisposition":`+wrong+`,"expectedHandoffTarget":{"kind":"queue","name":"Ops"}}]}`)
		row := report.Packs[0].Rows[0]
		if row.Status != "mismatch" || row.Detail != "The canonical disposition bytes differ." {
			t.Fatalf("the first difference is the disposition: %+v", row)
		}
		if row.Expected == row.Actual || row.ExpectedHandoffTarget == row.ActualHandoffTarget {
			t.Fatalf("both sides of both comparisons are reported: %+v", row)
		}
		if row.ActualHandoffTarget != target {
			t.Fatalf("the produced target is reported even where the disposition failed: %+v", row)
		}
	})

	// An asserting row whose evaluation is refused reports "unavailable" and
	// never null: no evaluation happened, which is not the same fact as an
	// evaluation reporting no target. The pair still appears together.
	t.Run("an unexpected refusal reports the target as unavailable", func(t *testing.T) {
		report := run(t, pack, `{"matrixVersion":"2","cases":[{"id":"r","facts":{"request":{"type":"data-access"}},"evidenceAvailability":{"not-a-requirement":"present"},"expectedDisposition":`+notApplicable+`,"expectedHandoffTarget":`+target+`}]}`)
		row := report.Packs[0].Rows[0]
		if row.Status != "mismatch" {
			t.Fatalf("row = %+v", row)
		}
		if row.ExpectedHandoffTarget != target || row.ActualHandoffTarget != result.HandoffTargetUnavailable {
			t.Fatalf("the pair appears together, and the actual side is unavailable: %+v", row)
		}
		if row.ActualHandoffTarget == "null" {
			t.Fatal("a refused evaluation reported no target; it did not report none")
		}
	})
}

// A suite that asserts nothing serializes nothing new, in the payload and on
// the human surface (ADR-0025).
//
// Checking that the Go fields are empty would still pass if the members started
// being written, so this checks the bytes a consumer actually reads.
func TestASuiteThatAssertsNoTargetSerializesNoTargetMembers(t *testing.T) {
	pack := string(packFixture(t))
	facts := `{"request":{"type":"data-access","completeness":"complete","appropriateness":"hard-fail","embargoedInformationToUnauthorizedRecipients":false}}`
	declineRedirect := `{"kind":"outcome","outcomeId":"decline-redirect","reasons":[],"handoff":{"state":"none"}}`
	matrix := `{"matrixVersion":"1","cases":[
	  {"id":"hard-fail","facts":` + facts + `,"evidenceAvailability":{"intake-form":"present","sponsor-endorsement":"present"},"expectedDisposition":` + declineRedirect + `},
	  {"id":"escalates","facts":{"request":{"type":"unrelated"}},"expectedDisposition":{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}}
	]}`
	configPath := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
		map[string]string{"packs/a.json": pack, "packs/a.matrix.json": matrix})
	run, failure := mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if run.Status != "passed" || run.Summary.Total != 2 {
		t.Fatalf("run = %+v", run.Summary)
	}
	// The second row's evaluation does report a target; not asking about it is
	// what keeps it out of the payload.
	encoded, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{"expectedHandoffTarget", "actualHandoffTarget", "Intake reviewer"} {
		if strings.Contains(string(encoded), member) {
			t.Fatalf("a suite that asserts nothing must not report %q: %s", member, encoded)
		}
	}
}

// The target comparison is exact string equality over the decoded kind and
// name, so Unicode behaves the way §8.3's byte comparison does and nothing
// normalizes anything (ADR-0025). The renderings this test also reads are the
// report's, not the verdict's.
func TestTheHandoffTargetComparisonIsExactOverUnicode(t *testing.T) {
	pack := string(packFixture(t))
	// The fixture's target name, respelled with an escape for one of its own
	// characters. It is the same string, so it must compare equal to the row's
	// plain literal: an escape and the character it names decode to one value,
	// and the comparison reads decoded values.
	escaped := strings.Replace(pack, `"name": "Intake reviewer"`, `"name": "Intake \u0072eviewer"`, 1)
	if escaped == pack {
		t.Fatal("the fixture must declare the target this test respells")
	}
	notApplicable := `{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`
	matrix := `{"matrixVersion":"2","cases":[{"id":"r","facts":{"request":{"type":"unrelated"}},"expectedDisposition":` + notApplicable +
		`,"expectedHandoffTarget":{"kind":"human-role","name":"Intake reviewer"}}]}`
	configPath := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
		map[string]string{"packs/a.json": escaped, "packs/a.matrix.json": matrix})
	run, failure := mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if run.Status != "passed" {
		t.Fatalf("an escape and its literal are one string: %+v", run.Packs[0].Rows)
	}

	// A pack whose target name carries a lone surrogate escape is refused at the
	// carrier, before any comparison can be reached: RFC 8785 §3.2.2.2 makes it
	// invalid rather than replaceable, and repairing it to U+FFFD would let two
	// different documents compare equal.
	lone := strings.Replace(pack, `"name": "Intake reviewer"`, `"name": "Intake \ud800reviewer"`, 1)
	configPath = writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
		map[string]string{"packs/a.json": lone, "packs/a.matrix.json": matrix})
	run, failure = mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if run.Packs[0].Status != "mismatch" || !strings.Contains(run.Packs[0].Detail, "unpaired surrogate") {
		t.Fatalf("a lone surrogate in a pack is refused: %+v", run.Packs[0])
	}
	// And the same in a matrix, on the other side of the same comparison.
	loneRow := strings.Replace(matrix, `"name":"Intake reviewer"`, `"name":"Intake \udc00reviewer"`, 1)
	configPath = writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`,
		map[string]string{"packs/a.json": pack, "packs/a.matrix.json": loneRow})
	run, failure = mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if run.Packs[0].Status != "mismatch" || !strings.Contains(run.Packs[0].Detail, "unpaired surrogate") {
		t.Fatalf("a lone surrogate in a matrix is refused: %+v", run.Packs[0])
	}
}

// The shape the amplification blocker was really about, end to end: a target at
// the carrier's own maximum string length, asserted by many rows (ADR-0025).
//
// Every input here is one this runtime accepts — §2.1 admits a megabyte-long
// authored string, and MaxMatrixCases admits ten thousand rows — which is the
// point: the defence cannot be "nobody would write that". Three things are
// pinned. The per-row rendering stays inside its budget, so no row retains a
// megabyte. The aggregate budget sees the accumulation and refuses when it is
// crossed. And the work is bounded too, not only the retained bytes: the pack's
// target is rendered once for the run rather than once per row, which is what
// keeps a valid matrix from forcing gigabytes of repeated canonicalizing and
// hashing that no retained-bytes budget could ever notice.
func TestACarrierMaximumTargetAcrossManyRowsIsBoundedInWorkAndInBytes(t *testing.T) {
	const rows = 2000
	name := strings.Repeat("q", 1<<20)
	pack := strings.Replace(string(packFixture(t)), `"name": "Intake reviewer"`, `"name": "`+name+`"`, 1)
	notApplicable := `{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`
	cases := make([]string, 0, rows)
	for index := range rows {
		// Every row asserts null against a pack that reports a megabyte-long
		// target: the assertion is four bytes and the answer is a megabyte, which
		// is the asymmetry that made the uncapped version a defect.
		cases = append(cases, fmt.Sprintf(`{"id":"row-%d","facts":{"request":{"type":"unrelated"}},"expectedDisposition":%s,"expectedHandoffTarget":null}`, index, notApplicable))
	}
	matrix := `{"matrixVersion":"2","cases":[` + strings.Join(cases, ",") + `]}`
	config := `{"configVersion":"1","packs":{"a":{"path":"packs/a.json","matrix":"packs/a.matrix.json"}}}`
	configPath := writeProject(t, config, map[string]string{"packs/a.json": pack, "packs/a.matrix.json": matrix})

	run, failure := mustLoad(t, configPath).Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	// Every row mismatches — each expects no target and the pack configures one —
	// which is the correct verdict and not what this test is about.
	if run.Summary.Total != rows || run.Summary.Mismatched != rows {
		t.Fatalf("run = %+v", run.Summary)
	}
	retained := 0
	for _, row := range run.Packs[0].Rows {
		if len(row.ActualHandoffTarget) > result.HandoffTargetBudget {
			t.Fatalf("no row may retain more than the budget: %d bytes", len(row.ActualHandoffTarget))
		}
		if !strings.Contains(row.ActualHandoffTarget, "…") {
			t.Fatalf("a target past the budget is reported as capped: %q", row.ActualHandoffTarget)
		}
		if row.ExpectedHandoffTarget != "null" {
			t.Fatalf("the row's own assertion is reported as it was written: %+v", row)
		}
		retained += len(row.ExpectedHandoffTarget) + len(row.ActualHandoffTarget)
	}
	// The whole run's target renderings are kilobytes, against a pack whose one
	// target is a megabyte: two thousand uncapped renderings would have been two
	// gigabytes.
	if retained > MaxHandoffTargetReportBytes {
		t.Fatalf("the aggregate must stay inside its budget: %d bytes", retained)
	}
	if retained > rows*(result.HandoffTargetBudget+len("null")) {
		t.Fatalf("retained = %d, which is past what the per-row budget allows", retained)
	}

	// The same run under a budget smaller than it needs is refused whole, and
	// writes nothing.
	bounded := mustLoad(t, configPath)
	bounded.handoffTargetReportBudget = 4096
	partial, failure := bounded.Test(evaluation.NewEngine(newValidator(t)), "", "packs test")
	if failure == nil || failure.Code != "JPS-RESOURCE-MATRIX-HANDOFF-TARGETS" {
		t.Fatalf("the aggregate budget must refuse this run: %+v", failure)
	}
	if len(partial.Packs) != 0 {
		t.Fatalf("a refused run writes no report: %+v", partial)
	}
}
