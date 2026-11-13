package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// overlapProject declares the bundled exact-overlap advisory case of each
// Core version that has the advice, and a pack that draws none.
func overlapProject(t *testing.T) (string, map[string]string) {
	t.Helper()
	packs := map[string]string{}
	for _, version := range []string{"0.3.0", "0.5.0"} {
		data, err := os.ReadFile(filepath.Join("..", "artifacts", "jps", version, "cases", "advisory", "rules-overlap-exact.json"))
		if err != nil {
			t.Fatal(err)
		}
		packs[version] = string(data)
	}
	config := writeProjectFixture(t, `{"configVersion":"1","packs":{
	  "core-three":{"path":"packs/core-three.json"},
	  "core-five":{"path":"packs/core-five.json"},
	  "plain":{"path":"packs/plain.json"}
	}}`, map[string]string{
		"packs/core-three.json": packs["0.3.0"],
		"packs/core-five.json":  packs["0.5.0"],
		"packs/plain.json":      evaluatorPack(t),
	})
	return config, packs
}

// packs validate carries, per pack, the advice spec validate reports for the
// same bytes, member for member, and the advice moves no status and no exit
// code (ADR-0081). A pack that draws no advice carries no member, and the
// human surface prints the advice under its pack.
func TestPacksValidateReportsRuleOverlapAdvice(t *testing.T) {
	config, packs := overlapProject(t)
	code, stdout, stderr := runTest(t, []string{"packs", "validate", "--config", config, "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("advice moves no exit code: exit=%d stderr=%q %s", code, stderr, stdout)
	}
	var report struct {
		Status string `json:"status"`
		Packs  []struct {
			ID         string            `json:"id"`
			Status     string            `json:"status"`
			Advisories []json.RawMessage `json:"advisories"`
		} `json:"packs"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "valid" {
		t.Fatalf("advice moves no status: %s", stdout)
	}
	byID := map[string][]json.RawMessage{}
	for _, entry := range report.Packs {
		if entry.Status != "valid" {
			t.Fatalf("%s: advice moves no entry status: %s", entry.ID, stdout)
		}
		byID[entry.ID] = entry.Advisories
	}
	for id, version := range map[string]string{"core-three": "0.3.0", "core-five": "0.5.0"} {
		_, specOut, _ := runTest(t, []string{"spec", "validate", "-", "--format", "json"}, packs[version])
		var validated struct {
			Diagnostics []json.RawMessage `json:"diagnostics"`
		}
		if err := json.Unmarshal([]byte(specOut), &validated); err != nil {
			t.Fatal(err)
		}
		if len(validated.Diagnostics) == 0 || len(byID[id]) != len(validated.Diagnostics) {
			t.Fatalf("%s: the advice is spec validate's: %d against %d\n%s", id, len(byID[id]), len(validated.Diagnostics), specOut)
		}
		for index := range validated.Diagnostics {
			if string(byID[id][index]) != string(validated.Diagnostics[index]) || !strings.Contains(string(byID[id][index]), `"code":"JPS-RULES-OVERLAP"`) {
				t.Fatalf("%s: advice %d differs from spec validate's:\n%s\n%s", id, index, byID[id][index], validated.Diagnostics[index])
			}
		}
	}
	if byID["plain"] != nil || strings.Count(stdout, `"advisories"`) != 2 {
		t.Fatalf("a pack that draws no advice carries no member: %s", stdout)
	}

	code, human, _ := runTest(t, []string{"packs", "validate", "--config", config}, "")
	if code != result.ExitSuccess || strings.Count(human, "  advice: warning JPS-RULES-OVERLAP /rules/0/when: ") != 2 {
		t.Fatalf("the human surface prints the advice per pack: exit=%d\n%s", code, human)
	}
}

// --fail-on-advisory appends the advisories check to every selected pack:
// failed for a pack that draws advice, which fails the run, passed for one
// that draws none, and skipped for one whose document check did not pass.
// Without it there is no such check and no member (ADR-0081).
func TestFailOnAdvisoryFailsAPackThatDrawsAdvice(t *testing.T) {
	config, _ := overlapProject(t)
	code, stdout, stderr := runTest(t, []string{"packs", "validate", "--config", config, "--format", "json", "--fail-on-advisory"}, "")
	var report result.PackValidation
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if code != result.ExitInvalid || stderr != "" || report.Status != "invalid" || !report.FailOnAdvisory ||
		report.Summary.Failed != 2 || report.Summary.Passed != 1 || !strings.Contains(stdout, `"failOnAdvisory":true`) {
		t.Fatalf("advice fails the run under the option: exit=%d stderr=%q %s", code, stderr, stdout)
	}
	for _, entry := range report.Packs {
		last := entry.Checks[len(entry.Checks)-1]
		want, status := result.PackCheckFailed, "invalid"
		if entry.ID == "plain" {
			want, status = result.PackCheckPassed, "valid"
		}
		if last.Name != "advisories" || last.Status != want || entry.Status != status {
			t.Fatalf("%s: the advisories check ends the checks: %+v", entry.ID, entry)
		}
	}

	// A pack whose document check fails gets the check skipped, not passed.
	broken := writeProjectFixture(t, `{"configVersion":"1","packs":{"broken":{"path":"packs/broken.json"}}}`,
		map[string]string{"packs/broken.json": `{"specVersion":"0.3.0"}`})
	_, stdout, _ = runTest(t, []string{"packs", "validate", "--config", broken, "--format", "json", "--fail-on-advisory"}, "")
	report = result.PackValidation{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	checks := report.Packs[0].Checks
	if last := checks[len(checks)-1]; last.Name != "advisories" || last.Status != result.PackCheckSkipped {
		t.Fatalf("an invalid document's advice check is skipped: %+v", checks)
	}

	code, stdout, _ = runTest(t, []string{"packs", "validate", "--config", config, "--format", "json"}, "")
	if code != result.ExitSuccess || strings.Contains(stdout, `"advisories","status"`) || strings.Contains(stdout, `"name":"advisories"`) || strings.Contains(stdout, "failOnAdvisory") {
		t.Fatalf("without the option there is no advisories check: exit=%d %s", code, stdout)
	}
}
