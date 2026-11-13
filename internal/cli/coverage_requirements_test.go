package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// atThresholdRow is the row that witnesses the threshold pack's boundary.
const atThresholdRow = `,
	  {"id":"at-threshold","facts":{"expense":{"amount":"5000"}},"expectedDisposition":{"kind":"outcome","outcomeId":"auto-approve","reasons":[],"handoff":{"state":"none"}}}`

func runPackTest(t *testing.T, config string, extra ...string) (int, result.PackTest, string) {
	t.Helper()
	args := append([]string{"packs", "test", "--config", config, "--format", "json"}, extra...)
	code, stdout, stderr := runTest(t, args, "")
	if stderr != "" {
		t.Fatalf("%v: stderr=%q", extra, stderr)
	}
	var report result.PackTest
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("%v: %v: %s", extra, err, stdout)
	}
	return code, report, stdout
}

// --min-coverage fails a pack whose witnessed share of derived probes is
// below the level and passes one at or above it (ADR-0080). The fixture's
// rows witness 2 of its 4 probes, 50 percent: 49 and 50 pass, 51 fails. The
// entry names the requirement and its detail states the count; the payload
// carries the level asked for.
func TestMinCoverageFailsBelowTheLevelOnly(t *testing.T) {
	config := greenSuiteWithUnwitnessedBoundary(t, "")
	for _, tc := range []struct {
		level  string
		code   int
		status string
	}{
		{"49", result.ExitSuccess, "passed"},
		{"50", result.ExitSuccess, "passed"},
		{"51", result.ExitInvalid, "mismatch"},
		{"100", result.ExitInvalid, "mismatch"},
		{"0", result.ExitSuccess, "passed"},
	} {
		code, report, stdout := runPackTest(t, config, "--min-coverage", tc.level)
		entry := report.Packs[0]
		if code != tc.code || report.Status != tc.status || entry.Status != tc.status ||
			report.MinCoverage == nil || !strings.Contains(stdout, `"minCoverage":`+tc.level) || strings.Contains(stdout, "requireBoundaries") {
			t.Fatalf("level %s: exit=%d %s", tc.level, code, stdout)
		}
		if tc.status == "mismatch" {
			if !slices.Equal(entry.UnmetRequirements, []string{result.RequirementMinCoverage}) ||
				!strings.Contains(entry.Detail, "at least "+tc.level+"% of the derived probes") || !strings.Contains(entry.Detail, "2/4 are") {
				t.Fatalf("level %s: the entry names the requirement and the count: %+v", tc.level, entry)
			}
			if entry.Summary.Mismatched != 0 || report.Summary.Passed != 2 {
				t.Fatalf("level %s: no row changed: %+v", tc.level, report.Summary)
			}
		} else if entry.UnmetRequirements != nil || entry.Detail != "" {
			t.Fatalf("level %s: a met level adds nothing to the entry: %+v", tc.level, entry)
		}
	}
	// A third witnessed probe moves the fixture to 75 percent, above 51.
	code, report, _ := runPackTest(t, greenSuiteWithUnwitnessedBoundary(t, atThresholdRow), "--min-coverage", "51")
	if code != result.ExitSuccess || report.Status != "passed" {
		t.Fatalf("75 percent meets 51: exit=%d %+v", code, report)
	}
}

// --require-boundaries fails a pack with a derived boundary probe no row
// witnesses and passes once a row sits at the literal (ADR-0080).
func TestRequireBoundariesFailsOnAnUnwitnessedBoundaryOnly(t *testing.T) {
	code, report, stdout := runPackTest(t, greenSuiteWithUnwitnessedBoundary(t, ""), "--require-boundaries")
	entry := report.Packs[0]
	if code != result.ExitInvalid || report.Status != "mismatch" || entry.Status != "mismatch" || !report.RequireBoundaries ||
		!slices.Equal(entry.UnmetRequirements, []string{result.RequirementBoundaries}) ||
		!strings.Contains(entry.Detail, "1 of 1 is not") || !strings.Contains(stdout, `"requireBoundaries":true`) || strings.Contains(stdout, "minCoverage") {
		t.Fatalf("an unwitnessed boundary fails the run: exit=%d %s", code, stdout)
	}
	code, report, _ = runPackTest(t, greenSuiteWithUnwitnessedBoundary(t, atThresholdRow), "--require-boundaries")
	if code != result.ExitSuccess || report.Status != "passed" || report.Packs[0].UnmetRequirements != nil {
		t.Fatalf("a witnessed boundary passes: exit=%d %+v", code, report)
	}
	// Disposition probes are left to --min-coverage: the witnessed fixture
	// still misses one of its four probes and passes this requirement.
	if missing := len(report.Packs[0].Coverage) - 3; missing != 1 {
		t.Fatalf("the fixture still misses one disposition probe: %+v", report.Packs[0].Coverage)
	}
	// A pack that compares nothing in order derives no boundary and meets it.
	code, report, _ = runPackTest(t, oneGoodProject(t), "--require-boundaries")
	if code != result.ExitSuccess || report.Status != "passed" {
		t.Fatalf("a pack with no boundary meets the requirement: exit=%d %+v", code, report)
	}
}

// Both options at once name both requirements, in the fixed order, and the
// human surface states what was required and why the pack failed.
func TestCoverageRequirementsTogether(t *testing.T) {
	config := greenSuiteWithUnwitnessedBoundary(t, "")
	_, report, _ := runPackTest(t, config, "--require-boundaries", "--min-coverage", "90")
	if !slices.Equal(report.Packs[0].UnmetRequirements, []string{result.RequirementMinCoverage, result.RequirementBoundaries}) {
		t.Fatalf("both requirements are named in order: %+v", report.Packs[0])
	}
	code, stdout, stderr := runTest(t, []string{"packs", "test", "--config", config, "--min-coverage", "90", "--require-boundaries"}, "")
	if code != result.ExitInvalid || stderr != "" ||
		!strings.Contains(stdout, "required: at least 90% of each tested pack's derived probes witnessed by a row") ||
		!strings.Contains(stdout, "required: every derived boundary probe of each tested pack witnessed by a row") ||
		!strings.Contains(stdout, "detail: This run requires at least 90%") ||
		!strings.Contains(stdout, ", and 1 pack did not meet a coverage requirement this run asked for\n") ||
		!strings.Contains(stdout, "[mismatch]: 2/2 (unmet: min-coverage, require-boundaries)\n") {
		t.Fatalf("exit=%d stderr=%q\n%s", code, stderr, stdout)
	}
}

// Without either option the payload and the exit code are what they were:
// the same bytes as before the options existed, no member of either, and
// exit 0 for a green suite with unwitnessed probes. A pack with no matrix
// is untouched by both options.
func TestCoverageRequirementsAbsentChangeNothing(t *testing.T) {
	config := greenSuiteWithUnwitnessedBoundary(t, "")
	code, stdout, stderr := runTest(t, []string{"packs", "test", "--config", config, "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" || strings.Contains(stdout, "minCoverage") || strings.Contains(stdout, "requireBoundaries") || strings.Contains(stdout, "unmetRequirements") {
		t.Fatalf("exit=%d %s", code, stdout)
	}
	code, human, _ := runTest(t, []string{"packs", "test", "--config", config}, "")
	if code != result.ExitSuccess || strings.Contains(human, "required:") || strings.Contains(human, "unmet") || strings.Contains(human, "coverage requirement") {
		t.Fatalf("exit=%d %s", code, human)
	}
	// A mismatch the coverage options did not cause prints its headline as
	// before: --require-matrix fails the pack with no matrix.
	code, human, _ = runTest(t, []string{"packs", "test", "--config", twoPackProject(t), "--require-matrix"}, "")
	if code != result.ExitInvalid || !strings.Contains(human, "mismatch: 0/") || strings.Contains(human, "coverage requirement") || strings.Contains(human, "unmet") {
		t.Fatalf("exit=%d %s", code, human)
	}
	code, report, _ := runPackTest(t, twoPackProject(t), "--min-coverage", "0", "--require-boundaries")
	for _, entry := range report.Packs {
		if entry.ID == "untested" && (entry.Status != "skipped" || entry.UnmetRequirements != nil) {
			t.Fatalf("a pack with no matrix is untouched: %+v", entry)
		}
	}
	if code != result.ExitSuccess {
		t.Fatalf("exit=%d %+v", code, report)
	}
}

// A pack whose coverage was not derived -- one the evaluator refuses -- has
// no share to measure, so it does not meet any level, 0 included, and its
// boundaries, when it has any, were not checked.
func TestCoverageRequirementsOnAnUnmeasuredPack(t *testing.T) {
	pack := `{"specVersion":"0.1.0-draft","id":"https://example.invalid/judgment-packs/stale","version":"0.1.0"}`
	matrix := `{"matrixVersion":"1","cases":[{"id":"refused","facts":{},"expectedErrorClass":"pack-not-conformant","expectedErrorPhase":"preflight"}]}`
	config := writeProjectFixture(t, `{"configVersion":"1","packs":{"stale":{"path":"packs/stale.json","matrix":"packs/stale.matrix.json"}}}`,
		map[string]string{"packs/stale.json": pack, "packs/stale.matrix.json": matrix})
	code, report, _ := runPackTest(t, config)
	if code != result.ExitSuccess || report.Packs[0].Coverage != nil {
		t.Fatalf("the fixture passes with no coverage: exit=%d %+v", code, report)
	}
	code, report, _ = runPackTest(t, config, "--min-coverage", "0")
	if code != result.ExitInvalid || !slices.Equal(report.Packs[0].UnmetRequirements, []string{result.RequirementMinCoverage}) ||
		!strings.Contains(report.Packs[0].Detail, "no coverage was derived") {
		t.Fatalf("an unmeasured pack does not meet a level: exit=%d %+v", code, report)
	}
}

// A level outside 0 to 100 is refused before any project is read.
func TestMinCoverageOutOfRangeIsRefused(t *testing.T) {
	for _, level := range []string{"-1", "101"} {
		code, stdout, _ := runTest(t, []string{"packs", "test", "--config", "does-not-exist.json", "--format", "json", "--min-coverage", level}, "")
		if code != result.ExitInvocation || !strings.Contains(stdout, "JPS-INVOCATION-MIN-COVERAGE") {
			t.Fatalf("level %s: exit=%d %s", level, code, stdout)
		}
	}
}
