package cli

import (
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The human summary line names the suite or corpus version the run read, for
// Core 0.5.0 as for 0.3.0, and never leaves it blank.
func TestHumanSummaryLineNamesTheSuite(t *testing.T) {
	for _, tc := range []struct {
		args []string
		line string
	}{
		{[]string{"spec", "test-conformance", "--spec-version", "0.5.0"}, "JPS 0.5.0 · suite 0.5.0 · immutable-git-ref\n"},
		{[]string{"spec", "test-conformance", "--spec-version", "0.3.0"}, "JPS 0.3.0 · suite 0.3.0 · immutable-git-ref\n"},
		{[]string{"experimental", "evaluate-corpus", "--spec-version", "0.5.0"}, "JPS 0.5.0 · corpus 0.5.0 (draft, seed) · immutable-git-ref\n"},
		{[]string{"experimental", "evaluate-corpus", "--spec-version", "0.3.0"}, "JPS 0.3.0 · corpus 0.3.0 (draft, seed) · immutable-git-ref\n"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			code, stdout, stderr := runTest(t, tc.args, "")
			if code != result.ExitSuccess || stderr != "" {
				t.Fatalf("exit=%d stderr=%q", code, stderr)
			}
			if !strings.Contains(stdout, "\n"+tc.line) {
				t.Fatalf("summary line %q missing from:\n%s", tc.line, stdout)
			}
			if strings.Contains(stdout, "suite  ·") || strings.Contains(stdout, "corpus  (") {
				t.Fatalf("blank suite version printed:\n%s", stdout)
			}
		})
	}
}

// The human corpus report of a run under 0.3.0 or 0.5.0 opens by saying that
// the run's specVersion is outside the version scope of the claim in
// CONFORMANCE.md, and does not call its results evidence for that claim.
func TestHumanCorpusReportOutsideTheClaimsVersionScope(t *testing.T) {
	for _, version := range []string{"0.3.0", "0.5.0"} {
		code, stdout, stderr := runTest(t, []string{"experimental", "evaluate-corpus", "--spec-version", version}, "")
		if code != result.ExitSuccess || stderr != "" {
			t.Fatalf("%s: exit=%d stderr=%q", version, code, stderr)
		}
		if !strings.HasPrefix(stdout, "EXPERIMENTAL SURFACE evaluation corpus: corpus results under a specVersion outside the version scope of the claim in CONFORMANCE.md\n") || strings.Contains(stdout, "required evidence") {
			t.Fatalf("%s:\n%s", version, stdout)
		}
	}
}
