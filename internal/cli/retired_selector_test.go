package cli

import (
	"encoding/json"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// spec schema and spec examples refuse the retired selectors as any version
// they do not bundle, and say, as the pack and suite refusals do, where the
// staged draft went. A version that was never a selector gets the plain
// refusal.
func TestSpecSchemaAndExamplesSayWhereTheStagedDraftWent(t *testing.T) {
	const plain = "The exact JPS specification version is not bundled with this CLI."
	const draft = plain + " The staged draft selector 0.4.0-draft was never released: its text was released as Core 0.5.0, and a pack written to it moves up by changing its specVersion to \"0.5.0\" (Core \u00a711)."
	const point = plain + " No specVersion 0.4.0 exists: the staged draft selector 0.4.0-draft was released as Core 0.5.0 (Core \u00a711)."
	for _, tc := range []struct {
		name, want string
		args       []string
	}{
		{"schema draft", draft, []string{"spec", "schema", "0.4.0-draft", "--format", "json"}},
		{"schema point", point, []string{"spec", "schema", "0.4.0", "--format", "json"}},
		{"schema other", plain, []string{"spec", "schema", "9.9.9", "--format", "json"}},
		{"examples draft", draft, []string{"spec", "examples", "--spec-version", "0.4.0-draft", "--format", "json"}},
		{"examples point", point, []string{"spec", "examples", "--spec-version", "0.4.0", "--format", "json"}},
		{"examples other", plain, []string{"spec", "examples", "--spec-version", "9.9.9", "--format", "json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runTest(t, tc.args, "")
			var refusal struct {
				Status      string
				Diagnostics []struct{ Code, Message string }
			}
			if err := json.Unmarshal([]byte(stdout), &refusal); err != nil || code != result.ExitUnsupported || stderr != "" {
				t.Fatalf("exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
			}
			if refusal.Status != "unsupported" || len(refusal.Diagnostics) != 1 || refusal.Diagnostics[0].Code != "JPS-CAPABILITY-SPEC-VERSION" || refusal.Diagnostics[0].Message != tc.want {
				t.Fatalf("refusal %+v", refusal)
			}
		})
	}
}
