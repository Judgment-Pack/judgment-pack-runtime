package cli

import (
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// A staged selector's run carries no suite version, so its human summary line
// names the suite staged instead of leaving a blank; a released selector's line
// is unchanged.
func TestHumanSummaryLineNamesAStagedSuite(t *testing.T) {
	for _, tc := range []struct {
		args []string
		line string
	}{
		{[]string{"spec", "test-conformance", "--spec-version", "0.4.0-draft"}, "JPS 0.4.0-draft · suite staged · immutable-git-ref\n"},
		{[]string{"spec", "test-conformance", "--spec-version", "0.3.0"}, "JPS 0.3.0 · suite 0.3.0 · immutable-git-ref\n"},
		{[]string{"experimental", "evaluate-corpus", "--spec-version", "0.4.0-draft"}, "JPS 0.4.0-draft · corpus staged (staged, staged evaluation rows of an unreleased draft selector; not a corpus and not conformance evidence) · immutable-git-ref\n"},
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
