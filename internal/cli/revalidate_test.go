package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/baseline"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func revalidation(t *testing.T, stdout string) result.Revalidation {
	t.Helper()
	var output result.Revalidation
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("not a revalidation report: %v (%q)", err, stdout)
	}
	return output
}

func findingKinds(output result.Revalidation) []string {
	kinds := []string{}
	for _, finding := range output.Findings {
		kinds = append(kinds, finding.Kind)
	}
	return kinds
}

func takeBaseline(t *testing.T, configPath string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "baseline.json")
	code, stdout, stderr := runTest(t, []string{"experimental", "revalidate", "--config", configPath, "--write", target}, "")
	if code != result.ExitSuccess {
		t.Fatalf("--write exit = %d: %s%s", code, stdout, stderr)
	}
	return target
}

// The round trip a pin move makes: take a baseline, then check this runtime
// against it. Nothing changed, so nothing differs, and the bytes are said to
// be the bytes the rows ran over.
func TestRevalidateTakesABaselineAndFindsNothingChanged(t *testing.T) {
	configPath := oneGoodProject(t)
	target := takeBaseline(t, configPath)
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	read, err := baseline.Decode(written)
	if err != nil {
		t.Fatalf("the written baseline does not read back: %v", err)
	}
	pack, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	matrix, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "packs", "intake.matrix.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := read.Packs[0]; got.PackDigest != audit.Digest(pack) || got.MatrixDigest != audit.Digest(matrix) || len(got.Rows) != 2 {
		t.Fatalf("the baseline does not hold the digests of the bytes on disk: %+v", got)
	}
	code, stdout, stderr := runTest(t, []string{"experimental", "revalidate", "--config", configPath, "--baseline", target, "--format", "json"}, "")
	if code != result.ExitSuccess {
		t.Fatalf("exit = %d: %s%s", code, stdout, stderr)
	}
	output := revalidation(t, stdout)
	if output.Status != "same" || output.Summary.RowsSame != 2 || output.Packs[0].Bytes != "same" || len(output.Findings) != 0 {
		t.Fatalf("output = %+v", output)
	}
	code, stdout, _ = runTest(t, []string{"experimental", "revalidate", "--config", configPath, "--baseline", target}, "")
	if code != result.ExitSuccess || !strings.Contains(stdout, "SAME: 1 pack(s), 2 row(s) compared") || !strings.Contains(stdout, "CONFORMANCE.md") {
		t.Fatalf("human exit = %d: %s", code, stdout)
	}
}

// What the pinned runtime gave and this one does not is reported row by row,
// with the exit code a CI gate reads.
func TestRevalidateReportsEachDifference(t *testing.T) {
	configPath := oneGoodProject(t)
	target := takeBaseline(t, configPath)
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(t *testing.T, root string, baseline string) string
		kinds  []string
	}{
		{"a changed disposition", func(t *testing.T, _ string, document string) string {
			return mustReplace(t, document, `\"outcomeId\":\"decline-redirect\"`, `\"outcomeId\":\"proceed\"`)
		}, []string{"row-differs"}},
		{"a row only in the baseline", func(t *testing.T, _ string, document string) string {
			return mustReplace(t, document, `"id": "hard-fail"`, `"id": "retired"`)
		}, []string{"row-only-in-baseline", "row-only-in-candidate"}},
		{"a pack whose bytes changed", func(t *testing.T, root string, document string) string {
			appendTo(t, filepath.Join(root, "packs", "intake-0.1.0.pack.json"), "\n")
			return document
		}, []string{"pack-bytes-differ"}},
		{"a matrix whose bytes changed", func(t *testing.T, root string, document string) string {
			appendTo(t, filepath.Join(root, "packs", "intake.matrix.json"), "\n")
			return document
		}, []string{"matrix-bytes-differ"}},
		{"a baseline from another specVersion", func(t *testing.T, _ string, document string) string {
			return mustReplace(t, document, `"specVersion": "0.2.0-draft"`, `"specVersion": "0.3.0"`)
		}, []string{"spec-version-differs"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Each case runs over a fresh copy of the project, so a change
			// to one file is seen by that case alone.
			config := oneGoodProject(t)
			changed := test.change(t, filepath.Dir(config), string(written))
			path := filepath.Join(t.TempDir(), "baseline.json")
			if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
				t.Fatal(err)
			}
			code, stdout, stderr := runTest(t, []string{"experimental", "revalidate", "--config", config, "--baseline", path, "--format", "json"}, "")
			if code != result.ExitInvalid {
				t.Fatalf("exit = %d, want %d: %s%s", code, result.ExitInvalid, stdout, stderr)
			}
			output := revalidation(t, stdout)
			if output.Status != "different" || strings.Join(findingKinds(output), ",") != strings.Join(test.kinds, ",") {
				t.Fatalf("findings = %v, want %v", findingKinds(output), test.kinds)
			}
		})
	}
}

// A row only the candidate has: the project's matrix grew a row the
// baseline never saw.
func TestRevalidateReportsARowOnlyInTheCandidate(t *testing.T) {
	configPath := oneGoodProject(t)
	target := takeBaseline(t, configPath)
	matrixPath := filepath.Join(filepath.Dir(configPath), "packs", "intake.matrix.json")
	matrix, err := os.ReadFile(matrixPath)
	if err != nil {
		t.Fatal(err)
	}
	grown := mustReplace(t, string(matrix), `"cases":[`, `"cases":[
	  {"id":"added","facts":`+hardFailFacts+`,"evidenceAvailability":`+presentEvidence+`,"expectedDisposition":`+declineRedirect+`},`)
	if err := os.WriteFile(matrixPath, []byte(grown), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runTest(t, []string{"experimental", "revalidate", "--config", configPath, "--baseline", target, "--format", "json"}, "")
	output := revalidation(t, stdout)
	if code != result.ExitInvalid || strings.Join(findingKinds(output), ",") != "matrix-bytes-differ,row-only-in-candidate" {
		t.Fatalf("exit = %d, findings = %v", code, findingKinds(output))
	}
	if output.Findings[1].Row != "added" || output.Summary.RowsSame != 2 {
		t.Fatalf("output = %+v", output)
	}
}

// A packs test --format json report is a baseline too: that is what a
// released runtime older than this command writes. Its rows are compared,
// and its bytes are said not to have been.
func TestRevalidateReadsAPacksTestReport(t *testing.T) {
	configPath := oneGoodProject(t)
	for name, report := range map[string]func() string{
		// The report this runtime's own packs test writes.
		"this runtime's packs test": func() string {
			code, stdout, _ := runTest(t, []string{"packs", "test", "--config", configPath, "--format", "json"}, "")
			if code != result.ExitSuccess {
				t.Fatalf("packs test exit = %d", code)
			}
			path := filepath.Join(t.TempDir(), "report.json")
			if err := os.WriteFile(path, []byte(stdout), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		},
		// The hand-written fixture in the shape v0.45.0 writes.
		"a v0.45.0 report": func() string {
			return filepath.Join("..", "baseline", "testdata", "packs-test-v0.45.0.json")
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := report()
			code, stdout, stderr := runTest(t, []string{"experimental", "revalidate", "--config", configPath, "--baseline", path, "--format", "json"}, "")
			if code != result.ExitSuccess {
				t.Fatalf("exit = %d: %s%s", code, stdout, stderr)
			}
			output := revalidation(t, stdout)
			if output.Status != "same" || output.Baselines[0].Form != baseline.FormReport || output.Packs[0].Bytes != "not-recorded" ||
				output.Summary.BytesNotRecorded != 1 || output.Summary.BytesCompared != 0 || output.Summary.RowsSame != 2 {
				t.Fatalf("output = %+v", output)
			}
			_, human, _ := runTest(t, []string{"experimental", "revalidate", "--config", configPath, "--baseline", path}, "")
			if !strings.Contains(human, "BYTES NOT COMPARED: 1 pack(s)") || !strings.Contains(human, "bytes not-recorded") {
				t.Fatalf("the human report must say the bytes were not compared: %s", human)
			}
		})
	}
}

// One packs test run refuses packs of two Core versions, so an older pin's
// reports are taken one pack at a time; a baseline document takes the whole
// project at once, each pack under its own contract.
func TestRevalidateRunsAProjectOfMixedContracts(t *testing.T) {
	core030, err := os.ReadFile(filepath.Join("..", "artifacts", "jps", "0.3.0", "evaluation", "packs", "decimal-threshold-fee.json"))
	if err != nil {
		t.Fatal(err)
	}
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{
	  "expense":{"path":"packs/expense.pack.json","matrix":"packs/expense.matrix.json"},
	  "fee":{"path":"packs/fee.pack.json","matrix":"packs/fee.matrix.json"}}}`, map[string]string{
		"packs/expense.pack.json":   thresholdPack,
		"packs/expense.matrix.json": `{"matrixVersion":"1","cases":[{"id":"under","facts":{"expense":{"amount":"10"}},"expectedDisposition":{"kind":"outcome","outcomeId":"auto-approve","reasons":[],"handoff":{"state":"none"}}}]}`,
		"packs/fee.pack.json":       string(core030),
		"packs/fee.matrix.json":     `{"matrixVersion":"1","cases":[{"id":"empty","facts":{},"expectedErrorClass":"malformed-input"}]}`,
	})
	if code, stdout, _ := runTest(t, []string{"packs", "test", "--config", configPath, "--format", "json"}, ""); code != result.ExitUnsupported || !strings.Contains(stdout, "JPS-MATRIX-MIXED-CONTRACT") {
		t.Fatalf("packs test is expected to refuse this project in one run: %d %s", code, stdout)
	}
	target := takeBaseline(t, configPath)
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	read, err := baseline.Decode(written)
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Packs) != 2 || read.Packs[0].SpecVersion != "0.2.0-draft" || read.Packs[1].SpecVersion != "0.3.0" {
		t.Fatalf("packs = %+v", read.Packs)
	}
	code, stdout, stderr := runTest(t, []string{"experimental", "revalidate", "--config", configPath, "--baseline", target}, "")
	if code != result.ExitSuccess {
		t.Fatalf("exit = %d: %s%s", code, stdout, stderr)
	}
	// The same project's reports taken one pack at a time, as an older pin
	// would, are read together.
	reports := []string{"experimental", "revalidate", "--config", configPath}
	for _, id := range []string{"expense", "fee"} {
		_, report, _ := runTest(t, []string{"packs", "test", "--config", configPath, "--id", id, "--format", "json"}, "")
		path := filepath.Join(t.TempDir(), id+".json")
		if err := os.WriteFile(path, []byte(report), 0o600); err != nil {
			t.Fatal(err)
		}
		reports = append(reports, "--baseline", path)
	}
	if code, stdout, stderr := runTest(t, reports, ""); code != result.ExitSuccess {
		t.Fatalf("exit = %d: %s%s", code, stdout, stderr)
	}
}

// --write - puts the baseline alone on stdout and the report on stderr.
func TestRevalidateWritesTheBaselineToStdout(t *testing.T) {
	configPath := oneGoodProject(t)
	code, stdout, stderr := runTest(t, []string{"experimental", "revalidate", "--config", configPath, "--write", "-"}, "")
	if code != result.ExitSuccess {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	if _, err := baseline.Decode([]byte(stdout)); err != nil {
		t.Fatalf("stdout is not exactly a baseline: %v", err)
	}
	if !strings.Contains(stderr, "BASELINE: 1 pack(s), 2 row(s)") || !strings.Contains(stderr, "written to standard output") {
		t.Fatalf("stderr = %s", stderr)
	}
}

// A row that does not match its expectation is in the baseline as given:
// the baseline is what the runtime gave, and packs test is what judges it.
func TestRevalidateRecordsAMismatchedRowAsGiven(t *testing.T) {
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{"intake":{"path":"packs/intake.pack.json","matrix":"packs/intake.matrix.json"}}}`, map[string]string{
		"packs/intake.pack.json":   evaluatorPack(t),
		"packs/intake.matrix.json": `{"matrixVersion":"1","cases":[{"id":"wrong","facts":` + hardFailFacts + `,"evidenceAvailability":` + presentEvidence + `,"expectedDisposition":{"kind":"outcome","outcomeId":"proceed","reasons":[],"handoff":{"state":"none"}}}]}`,
	})
	target := filepath.Join(t.TempDir(), "baseline.json")
	code, stdout, _ := runTest(t, []string{"experimental", "revalidate", "--config", configPath, "--write", target, "--format", "json"}, "")
	var taken result.BaselineTaken
	if err := json.Unmarshal([]byte(stdout), &taken); err != nil || code != result.ExitSuccess {
		t.Fatalf("exit = %d: %s", code, stdout)
	}
	if taken.Packs[0].Unexpected != 1 || taken.WrittenTo != target {
		t.Fatalf("taken = %+v", taken)
	}
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), `"status": "mismatch"`) || !strings.Contains(string(written), `\"outcomeId\":\"decline-redirect\"`) {
		t.Fatalf("the baseline must hold what was given: %s", written)
	}
}

// The take refuses rather than writes a baseline that leaves a pack out, and
// refuses every destination it must not write.
func TestRevalidateWriteRefusals(t *testing.T) {
	good := oneGoodProject(t)
	brokenMatrix := writeProjectFixture(t, `{"configVersion":"1","packs":{"intake":{"path":"packs/intake.pack.json","matrix":"packs/intake.matrix.json"}}}`, map[string]string{
		"packs/intake.pack.json":   evaluatorPack(t),
		"packs/intake.matrix.json": `{"matrixVersion":"1","cases":[]}`,
	})
	noMatrix := writeProjectFixture(t, `{"configVersion":"1","packs":{"intake":{"path":"packs/intake.pack.json"}}}`, map[string]string{
		"packs/intake.pack.json": evaluatorPack(t),
	})
	existing := filepath.Join(t.TempDir(), "existing.json")
	if err := os.WriteFile(existing, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		args   []string
		exit   int
		code   string
		target string
	}{
		{"a matrix that does not load", []string{"--config", brokenMatrix}, result.ExitInvalid, "JPS-BASELINE-PACK-NOT-RUN", "fresh"},
		{"a project with no matrix", []string{"--config", noMatrix}, result.ExitInvalid, "JPS-BASELINE-EMPTY", "fresh"},
		{"a declared destination", []string{"--config", good, "--write", filepath.Join(filepath.Dir(good), "packs", "intake.matrix.json")}, result.ExitInvalid, "JPS-BASELINE-PATH", ""},
		{"an existing destination", []string{"--config", good, "--write", existing}, result.ExitIO, "JPS-BASELINE-WRITE", ""},
		{"stdout twice", []string{"--config", good, "--write", "-"}, result.ExitInvocation, "JPS-INVOCATION-STDOUT", ""},
		{"no mode", []string{"--config", good}, result.ExitInvocation, "JPS-INVOCATION-REVALIDATE-MODE", ""},
		{"both modes", []string{"--config", good, "--write", "-", "--baseline", existing}, result.ExitInvocation, "JPS-INVOCATION-REVALIDATE-MODE", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"experimental", "revalidate", "--format", "json"}, test.args...)
			fresh := ""
			if test.target == "fresh" {
				fresh = filepath.Join(t.TempDir(), "baseline.json")
				args = append(args, "--write", fresh)
			}
			code, stdout, _ := runTest(t, args, "")
			if code != test.exit {
				t.Fatalf("exit = %d, want %d: %s", code, test.exit, stdout)
			}
			assertDiagnosticCode(t, stdout, test.code)
			if fresh != "" {
				if _, err := os.Stat(fresh); !os.IsNotExist(err) {
					t.Fatalf("a refused take wrote %s", fresh)
				}
			}
		})
	}
}

// Each kind of unusable baseline is refused before a row runs, with its own
// exit code: 2 for a version this runtime does not read, 3 for a malformed
// or oversized one, 4 for one that cannot be read.
func TestRevalidateBaselineRefusals(t *testing.T) {
	configPath := oneGoodProject(t)
	target := takeBaseline(t, configPath)
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	write := func(t *testing.T, body string) string {
		path := filepath.Join(t.TempDir(), "baseline.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, test := range []struct {
		name  string
		body  func(t *testing.T) string
		args  func(t *testing.T, path string) []string
		stdin string
		exit  int
		code  string
		says  string
	}{
		{name: "malformed", body: func(*testing.T) string { return `{"baselineVersion":"1",` }, exit: result.ExitInvocation, code: "JPS-BASELINE-INVALID"},
		{name: "a duplicate member", body: func(t *testing.T) string {
			return mustReplace(t, string(written), `"baselineVersion": "1",`, `"baselineVersion": "1", "baselineVersion": "1",`)
		}, exit: result.ExitInvocation, code: "JPS-BASELINE-INVALID"},
		{name: "an unknown member", body: func(t *testing.T) string {
			return mustReplace(t, string(written), `"id": "hard-fail",`, `"id": "hard-fail", "detail": "x",`)
		}, exit: result.ExitInvocation, code: "JPS-BASELINE-INVALID", says: `member \"detail\" in row 0`},
		{name: "a later format version", body: func(t *testing.T) string {
			return mustReplace(t, string(written), `"baselineVersion": "1"`, `"baselineVersion": "2"`)
		}, exit: result.ExitUnsupported, code: "JPS-BASELINE-VERSION"},
		{name: "a pack in two baselines", body: func(*testing.T) string { return string(written) },
			args: func(_ *testing.T, path string) []string { return []string{"--baseline", path, "--baseline", target} },
			exit: result.ExitInvocation, code: "JPS-BASELINE-INVALID", says: "more than one baseline"},
		{name: "unreadable", args: func(t *testing.T, _ string) []string {
			return []string{"--baseline", filepath.Join(t.TempDir(), "missing.json")}
		}, exit: result.ExitIO, code: "JPS-BASELINE-READ"},
		{name: "oversized", args: func(*testing.T, string) []string { return []string{"--baseline", "-"} },
			stdin: strings.Repeat(" ", int(baseline.MaxBytes)+1), exit: result.ExitInvocation, code: "JPS-BASELINE-INVALID", says: "bytes together"},
		{name: "oversized together", args: func(*testing.T, string) []string { return []string{"--baseline", target, "--baseline", "-"} },
			stdin: strings.Repeat(" ", int(baseline.MaxBytes)-len(written)+1), exit: result.ExitInvocation, code: "JPS-BASELINE-INVALID", says: "bytes together"},
		{name: "a remote path", args: func(*testing.T, string) []string {
			return []string{"--baseline", "https://example.invalid/baseline.json"}
		},
			exit: result.ExitInvocation, code: "JPS-INVOCATION-INPUT"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := ""
			if test.body != nil {
				path = write(t, test.body(t))
			}
			args := []string{"--baseline", path}
			if test.args != nil {
				args = test.args(t, path)
			}
			code, stdout, _ := runTest(t, append([]string{"experimental", "revalidate", "--config", configPath, "--format", "json"}, args...), test.stdin)
			if code != test.exit {
				t.Fatalf("exit = %d, want %d: %s", code, test.exit, first(stdout, 400))
			}
			assertDiagnosticCode(t, stdout, test.code)
			if test.says != "" && !strings.Contains(stdout, test.says) {
				t.Fatalf("the refusal must say %q: %s", test.says, first(stdout, 400))
			}
		})
	}
}

func mustReplace(t *testing.T, document, old, new string) string {
	t.Helper()
	if !strings.Contains(document, old) {
		t.Fatalf("fixture has no %q", old)
	}
	return strings.Replace(document, old, new, 1)
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		t.Fatal(err)
	}
}
