package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// twoPackProject declares one pack with a passing matrix and one with none.
func twoPackProject(t *testing.T) string {
	t.Helper()
	matrix := `{"matrixVersion":"1","cases":[
	  {"id":"hard-fail","facts":` + hardFailFacts + `,"evidenceAvailability":` + presentEvidence + `,"expectedDisposition":` + declineRedirect + `}
	]}`
	return writeProjectFixture(t, `{"configVersion":"1","packs":{
	  "intake":{"path":"packs/intake-0.1.0.pack.json","matrix":"packs/intake.matrix.json"},
	  "untested":{"path":"packs/untested-0.1.0.pack.json"}
	}}`, map[string]string{
		"packs/intake-0.1.0.pack.json":   evaluatorPack(t),
		"packs/untested-0.1.0.pack.json": evaluatorPack(t),
		"packs/intake.matrix.json":       matrix,
	})
}

// --require-matrix is an opt-in (ADR-0042): without it a declared pack with no
// matrix is skipped and the run passes on the pack that has one, as before;
// with it that pack fails the run, the exit code is 1, and the payload says the
// run required matrices. Selecting only the tested pack passes either way.
func TestRequireMatrixFailsARunWithAnUntestedPack(t *testing.T) {
	configPath := twoPackProject(t)
	run := func(extra ...string) (int, result.PackTest, string) {
		t.Helper()
		args := append([]string{"packs", "test", "--config", configPath, "--format", "json"}, extra...)
		code, stdout, stderr := runTest(t, args, "")
		if stderr != "" {
			t.Fatalf("%v: stderr=%q", extra, stderr)
		}
		var report result.PackTest
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatal(err)
		}
		return code, report, stdout
	}
	entry := func(report result.PackTest, id string) result.PackTestEntry {
		for _, pack := range report.Packs {
			if pack.ID == id {
				return pack
			}
		}
		t.Fatalf("no entry %q in %+v", id, report.Packs)
		return result.PackTestEntry{}
	}

	code, report, stdout := run()
	if code != 0 || report.Status != "passed" || entry(report, "untested").Status != "skipped" || strings.Contains(stdout, "requireMatrix") {
		t.Fatalf("the default skips the untested pack and passes: exit=%d %+v", code, report)
	}

	code, report, _ = run("--require-matrix")
	untested := entry(report, "untested")
	if code != result.ExitInvalid || report.Status != "mismatch" || !report.RequireMatrix ||
		untested.Status != "mismatch" || !strings.Contains(untested.Detail, "declares no matrix") ||
		entry(report, "intake").Status != "passed" {
		t.Fatalf("the opt-in fails the run on the untested pack alone: exit=%d %+v", code, report)
	}

	code, report, _ = run("--require-matrix", "--id", "intake")
	if code != 0 || report.Status != "passed" || !report.RequireMatrix {
		t.Fatalf("a selection that holds only a tested pack passes under the opt-in: exit=%d %+v", code, report)
	}
}
