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

// The opt-in changes the matrix-less entries and nothing else: the entry of a
// pack with a matrix, its rows and its coverage report included, is byte for
// byte the same in both modes, and the failing entry's detail names the
// requirement, not only the missing matrix.
func TestRequireMatrixChangesOnlyTheMatrixLessEntries(t *testing.T) {
	configPath := twoPackProject(t)
	entries := func(extra ...string) map[string]json.RawMessage {
		t.Helper()
		_, stdout, _ := runTest(t, append([]string{"packs", "test", "--config", configPath, "--format", "json"}, extra...), "")
		var report struct {
			Packs []json.RawMessage `json:"packs"`
		}
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatal(err)
		}
		byID := map[string]json.RawMessage{}
		for _, raw := range report.Packs {
			var entry struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &entry); err != nil {
				t.Fatal(err)
			}
			byID[entry.ID] = raw
		}
		return byID
	}
	plain, strict := entries(), entries("--require-matrix")
	if string(plain["intake"]) != string(strict["intake"]) || !strings.Contains(string(strict["intake"]), `"coverage"`) {
		t.Fatalf("the tested pack's entry, coverage included, is the same in both modes:\n%s\n%s", plain["intake"], strict["intake"])
	}
	if !strings.Contains(string(strict["untested"]), "this run requires every selected pack to have one") {
		t.Fatalf("the failing entry names the requirement: %s", strict["untested"])
	}
}

// Under the opt-in: a project whose only pack has no matrix fails; a project
// with no packs is still skipped, since nothing was selected to require
// anything of; a pack whose matrix cannot be read is a mismatch in both modes;
// and selecting the matrix-less pack by --id fails it. The raw payload names
// the member exactly, and omits it without the opt-in.
func TestRequireMatrixAcrossProjectShapes(t *testing.T) {
	onlyUntested := writeProjectFixture(t, `{"configVersion":"1","packs":{"untested":{"path":"packs/untested-0.1.0.pack.json"}}}`,
		map[string]string{"packs/untested-0.1.0.pack.json": evaluatorPack(t)})
	noPacks := writeProjectFixture(t, `{"configVersion":"1","packs":{}}`, map[string]string{})
	unreadable := writeProjectFixture(t, `{"configVersion":"1","packs":{"broken":{"path":"packs/broken-0.1.0.pack.json","matrix":"packs/broken.matrix.json"}}}`,
		map[string]string{"packs/broken-0.1.0.pack.json": evaluatorPack(t), "packs/broken.matrix.json": `{`})
	for _, tc := range []struct {
		name, config string
		extra        []string
		code         int
		status       string
	}{
		{"only a matrix-less pack", onlyUntested, nil, result.ExitInvalid, "mismatch"},
		{"no packs", noPacks, nil, result.ExitInvalid, "skipped"},
		{"an unreadable matrix", unreadable, nil, result.ExitInvalid, "mismatch"},
		{"the matrix-less pack selected", twoPackProject(t), []string{"--id", "untested"}, result.ExitInvalid, "mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"packs", "test", "--config", tc.config, "--format", "json", "--require-matrix"}, tc.extra...)
			code, stdout, _ := runTest(t, args, "")
			if code != tc.code || !strings.Contains(stdout, `"status":"`+tc.status+`"`) || !strings.Contains(stdout, `"requireMatrix":true`) {
				t.Fatalf("exit=%d, want %d with status %s and the member: %s", code, tc.code, tc.status, stdout)
			}
		})
	}
	code, stdout, _ := runTest(t, []string{"packs", "test", "--config", onlyUntested, "--format", "json"}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, `"status":"skipped"`) || strings.Contains(stdout, "requireMatrix") {
		t.Fatalf("without the opt-in the same project is skipped and carries no member: exit=%d %s", code, stdout)
	}
}
