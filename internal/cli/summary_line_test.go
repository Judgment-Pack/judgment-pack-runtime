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
