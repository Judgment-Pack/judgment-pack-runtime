package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// vocabularyPack is one of the Spec's annotation-vocabulary rows, vendored
// under internal/vocabulary.
func vocabularyPack(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "vocabulary", "testdata", "1.0.0", "packs", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// vocabularyProject declares one pack per named row, each under its row name.
func vocabularyProject(t *testing.T, rows ...string) string {
	t.Helper()
	sort.Strings(rows)
	entries := []string{}
	files := map[string]string{}
	for _, row := range rows {
		entries = append(entries, `"`+row+`":{"path":"packs/`+row+`.json"}`)
		files["packs/"+row+".json"] = vocabularyPack(t, row)
	}
	return writeProjectFixture(t, `{"configVersion":"1","packs":{`+strings.Join(entries, ",")+`}}`, files)
}

func validateVocabulary(t *testing.T, config string, extra ...string) (int, result.PackValidation, map[string]result.PackValidationEntry, string) {
	t.Helper()
	code, stdout, stderr := runTest(t, append([]string{"packs", "validate", "--config", config, "--format", "json"}, extra...), "")
	if stderr != "" {
		t.Fatalf("stderr=%q", stderr)
	}
	var report result.PackValidation
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	byID := map[string]result.PackValidationEntry{}
	for _, entry := range report.Packs {
		byID[entry.ID] = entry
	}
	return code, report, byID, stdout
}

func vocabularyCheck(t *testing.T, entry result.PackValidationEntry) result.PackCheck {
	t.Helper()
	for _, check := range entry.Checks {
		if check.Name == "annotation-vocabulary" {
			return check
		}
	}
	t.Fatalf("%s has no annotation-vocabulary check: %+v", entry.ID, entry.Checks)
	return result.PackCheck{}
}

// A defined value passes; a value that is not a level, in any case or as any
// list, fails the pack and the run, with one finding per refused outcome
// naming the outcome and the value; the key on a rule or the root is not
// read. The document check is Core's and passes throughout (ADR-0088).
func TestPacksValidateChecksTheSeverityVocabulary(t *testing.T) {
	code, report, byID, stdout := validateVocabulary(t, vocabularyProject(t, "each-level", "rule-position", "root-position", "unknown-value", "wrong-case", "list-one"))
	if code != result.ExitInvalid || report.Status != "invalid" || report.Summary.Failed != 3 || report.Summary.Passed != 3 {
		t.Fatalf("a refusal fails the run: exit=%d %s", code, stdout)
	}
	for _, id := range []string{"each-level", "rule-position", "root-position", "unknown-value", "wrong-case", "list-one"} {
		entry := byID[id]
		if entry.Checks[1].Name != "document-validation" || entry.Checks[1].Status != result.PackCheckPassed {
			t.Fatalf("%s: Core admits every one of these packs: %+v", id, entry)
		}
		if entry.Vocabulary == nil || entry.Vocabulary.Vocabulary != "annotation-vocabulary" || entry.Vocabulary.Version != "1.0.0" {
			t.Fatalf("%s: the entry states the vocabulary version applied: %+v", id, entry.Vocabulary)
		}
		if last := entry.Checks[len(entry.Checks)-1]; last.Name != "annotation-vocabulary" || !strings.Contains(last.Detail, "annotation vocabulary 1.0.0") {
			t.Fatalf("%s: the vocabulary check ends the checks and names its version: %+v", id, entry.Checks)
		}
	}
	for _, id := range []string{"each-level", "rule-position", "root-position"} {
		entry := byID[id]
		if entry.Status != "valid" || vocabularyCheck(t, entry).Status != result.PackCheckPassed || entry.Vocabulary.Result != "pass" || len(entry.Vocabulary.Findings) != 0 {
			t.Fatalf("%s passes: %+v", id, entry)
		}
	}
	for id, want := range map[string][]string{
		"unknown-value": {`"refer"`, `"hihg"`},
		"wrong-case":    {`"note"`, `"High"`, `"decline"`, `"CRITICAL"`},
		"list-one":      {`"refer"`, `["high"]`},
	} {
		entry := byID[id]
		check := vocabularyCheck(t, entry)
		if entry.Status != "invalid" || check.Status != result.PackCheckFailed || entry.Vocabulary.Result != "refused" {
			t.Fatalf("%s is refused: %+v", id, entry)
		}
		messages := ""
		for _, finding := range entry.Vocabulary.Findings {
			if finding.Code != "JPS-VOCABULARY-SEVERITY-VALUE" || finding.Layer != "vocabulary" || !strings.HasSuffix(finding.InstancePath, "/extensions/org.judgmentpack.annotations/severity") {
				t.Fatalf("%s: finding %+v", id, finding)
			}
			messages += finding.Message
		}
		for _, part := range want {
			if !strings.Contains(messages, part) {
				t.Fatalf("%s: the findings do not name %s: %s", id, part, messages)
			}
		}
	}
	if got := len(byID["wrong-case"].Vocabulary.Findings); got != 2 || !strings.Contains(vocabularyCheck(t, byID["wrong-case"]).Detail, "2 outcomes state") {
		t.Fatalf("two refused outcomes give two findings: %d %+v", got, byID["wrong-case"])
	}

	code, human, _ := runTest(t, []string{"packs", "validate", "--config", vocabularyProject(t, "unknown-value")}, "")
	if code != result.ExitInvalid || !strings.Contains(human, "  annotation-vocabulary: failed — Checked against annotation vocabulary 1.0.0: 1 outcome states") ||
		!strings.Contains(human, `  annotation-vocabulary 1.0.0: error JPS-VOCABULARY-SEVERITY-VALUE /outcomes/2/extensions/org.judgmentpack.annotations/severity: Outcome "refer" states the severity "hihg"`) {
		t.Fatalf("the human surface prints the check and the finding: exit=%d\n%s", code, human)
	}
}

// A pack the vocabulary does not apply to is skipped with the reason: another
// selector, Core's refusal with its layer, and a profile this runtime does
// not read. Core's refusal is reported by the document check alone, with no
// finding of the vocabulary. A required extension this runtime does not
// support does not stop the check.
func TestPacksValidateSaysWhyTheVocabularyWasNotApplied(t *testing.T) {
	legacy, err := os.ReadFile(filepath.Join("..", "artifacts", "jps", "0.3.0", "cases", "valid", "minimal-literal.json"))
	if err != nil {
		t.Fatal(err)
	}
	config := writeProjectFixture(t, `{"configVersion":"1","packs":{
	  "legacy":{"path":"packs/legacy.json"},
	  "extended":{"path":"packs/extended.json"},
	  "empty-string":{"path":"packs/empty-string.json"},
	  "unsupported":{"path":"packs/unsupported.json"}
	}}`, map[string]string{
		"packs/legacy.json":       string(legacy),
		"packs/extended.json":     vocabularyPack(t, "profile-extended"),
		"packs/empty-string.json": vocabularyPack(t, "core-empty-string"),
		"packs/unsupported.json":  vocabularyPack(t, "unsupported-extension"),
	})
	code, _, byID, stdout := validateVocabulary(t, config)
	if code != result.ExitInvalid {
		t.Fatalf("exit=%d %s", code, stdout)
	}
	for id, want := range map[string]struct{ reason, layer, detail string }{
		"legacy":       {"spec-version", "", "(spec-version)"},
		"extended":     {"profile-unsupported", "", "(profile-unsupported)"},
		"empty-string": {"core-refused", "structural", "Core refused the pack at the structural layer"},
	} {
		entry := byID[id]
		check := vocabularyCheck(t, entry)
		if check.Status != result.PackCheckSkipped || !strings.Contains(check.Detail, want.detail) || !strings.Contains(check.Detail, "annotation vocabulary 1.0.0") {
			t.Fatalf("%s: the skip says why: %+v", id, check)
		}
		if entry.Vocabulary == nil || entry.Vocabulary.Result != "not-checked" || entry.Vocabulary.Reason != want.reason || entry.Vocabulary.CoreLayer != want.layer ||
			len(entry.Vocabulary.Findings) != 0 || len(entry.Vocabulary.Advisories) != 0 {
			t.Fatalf("%s: %+v", id, entry.Vocabulary)
		}
	}
	if byID["legacy"].Status != "valid" {
		t.Fatalf("a 0.3.0 pack is not failed by a vocabulary that does not apply to it: %+v", byID["legacy"])
	}
	refused := byID["empty-string"]
	if refused.Checks[1].Status != result.PackCheckFailed || !strings.Contains(refused.Checks[1].Detail, "JPS-STRUCTURE-MIN-LENGTH") || strings.Contains(refused.Checks[1].Detail, "VOCABULARY") {
		t.Fatalf("Core's refusal is Core's diagnostic only: %+v", refused)
	}
	unsupported := byID["unsupported"]
	if unsupported.Checks[1].Status != result.PackCheckFailed || vocabularyCheck(t, unsupported).Status != result.PackCheckFailed ||
		unsupported.Vocabulary.Result != "refused" || len(unsupported.Vocabulary.Findings) != 1 {
		t.Fatalf("an unsupported required extension does not stop the check: %+v", unsupported)
	}
}

// The identifier advisory is carried in the vocabulary's report, moves no
// status by default, and fails the pack under --fail-on-advisory, as
// spec validate's advice does (ADR-0081); the entry's own advisories member
// stays spec validate's.
func TestPacksValidateCarriesTheIdentifierAdvisory(t *testing.T) {
	// The rows' rule no-receipt overlaps the others, which spec validate
	// advises on; without it, the only advice is the vocabulary's.
	config := writeProjectFixture(t, `{"configVersion":"1","packs":{
	  "identifier-disagrees":{"path":"packs/disagrees.json"},
	  "identifier-agrees":{"path":"packs/agrees.json"}
	}}`, map[string]string{
		"packs/disagrees.json": withoutRule(t, vocabularyPack(t, "identifier-disagrees"), "no-receipt"),
		"packs/agrees.json":    withoutRule(t, vocabularyPack(t, "identifier-agrees"), "no-receipt"),
	})
	code, report, byID, stdout := validateVocabulary(t, config)
	advised := byID["identifier-disagrees"]
	if code != result.ExitSuccess || report.Status != "valid" || advised.Status != "valid" || advised.Advisories != nil ||
		len(advised.Vocabulary.Advisories) != 1 || advised.Vocabulary.Advisories[0].Code != "JPS-VOCABULARY-SEVERITY-IDENTIFIER" ||
		advised.Vocabulary.Advisories[0].InstancePath != "/outcomes/2/id" || advised.Vocabulary.Advisories[0].Severity != "warning" {
		t.Fatalf("the advisory moves no status: exit=%d %s", code, stdout)
	}
	if len(byID["identifier-agrees"].Vocabulary.Advisories) != 0 {
		t.Fatalf("no advisory where the two agree: %s", stdout)
	}
	code, human, _ := runTest(t, []string{"packs", "validate", "--config", config}, "")
	if code != result.ExitSuccess || strings.Count(human, "  advice: warning JPS-VOCABULARY-SEVERITY-IDENTIFIER /outcomes/2/id: ") != 1 {
		t.Fatalf("the human surface prints the advice: exit=%d\n%s", code, human)
	}

	code, report, byID, stdout = validateVocabulary(t, config, "--fail-on-advisory")
	advised = byID["identifier-disagrees"]
	last := advised.Checks[len(advised.Checks)-1]
	if code != result.ExitInvalid || report.Status != "invalid" || advised.Status != "invalid" || last.Name != "advisories" ||
		last.Status != result.PackCheckFailed || last.Detail != "The pack draws 1 advisory, and this run fails on any." {
		t.Fatalf("the advisory fails the pack under the option: exit=%d %s", code, stdout)
	}
	if agrees := byID["identifier-agrees"]; agrees.Status != "valid" || agrees.Checks[len(agrees.Checks)-1].Status != result.PackCheckPassed {
		t.Fatalf("a pack with no advice passes the option: %+v", agrees)
	}

	// A pack whose only Core finding is a required extension this runtime
	// does not support fails its document check, yet the vocabulary is
	// applied to it (§6.2 step 4) and its advice is computed: under the
	// option the advisories check fails on that advice, and is not skipped
	// as for a pack whose advice was never computed.
	unsupported := strings.ReplaceAll(strings.Replace(vocabularyPack(t, "unsupported-extension"), `"hihg"`, `"low"`, 1), `"refer"`, `"refer-high"`)
	config = writeProjectFixture(t, `{"configVersion":"1","packs":{"unsupported-extension":{"path":"packs/unsupported.json"}}}`,
		map[string]string{"packs/unsupported.json": withoutRule(t, unsupported, "no-receipt")})
	code, _, byID, stdout = validateVocabulary(t, config, "--fail-on-advisory")
	entry := byID["unsupported-extension"]
	last = entry.Checks[len(entry.Checks)-1]
	if code != result.ExitInvalid || entry.Vocabulary.Result != "pass" || len(entry.Vocabulary.Advisories) != 1 ||
		entry.Vocabulary.Advisories[0].InstancePath != "/outcomes/2/id" ||
		last.Name != "advisories" || last.Status != result.PackCheckFailed || last.Detail != "The pack draws 1 advisory, and this run fails on any." {
		t.Fatalf("the advice on a pack with an unsupported required extension: exit=%d %s", code, stdout)
	}
}

// withoutRule is a pack with one rule removed.
func withoutRule(t *testing.T, pack, id string) string {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal([]byte(pack), &document); err != nil {
		t.Fatal(err)
	}
	rules := []any{}
	for _, rule := range document["rules"].([]any) {
		if rule.(map[string]any)["id"] != id {
			rules = append(rules, rule)
		}
	}
	if len(rules) == len(document["rules"].([]any)) {
		t.Fatalf("no rule %s", id)
	}
	document["rules"] = rules
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// spec validate is unchanged by the vocabulary: Core 0.5.0 admits any
// annotation value, and its report for a pack whose severity is not a level
// is the bytes it gives for the same pack with a level or with no key.
func TestSpecValidateDoesNotApplyTheVocabulary(t *testing.T) {
	refused := vocabularyPack(t, "unknown-value")
	defined := strings.Replace(refused, `"severity": "hihg"`, `"severity": "high"`, 1)
	absent := strings.Replace(refused, `,
      "extensions": {
        "org.judgmentpack.annotations": {
          "severity": "hihg"
        }
      }`, "", 1)
	if defined == refused || absent == refused || strings.Contains(absent, "hihg") {
		t.Fatal("the fixtures did not change")
	}
	var first string
	for _, pack := range []string{refused, defined, absent} {
		for _, format := range []string{"json", "human"} {
			code, stdout, stderr := runTest(t, []string{"spec", "validate", "-", "--format", format}, pack)
			if code != result.ExitSuccess || stderr != "" || strings.Contains(stdout, "VOCABULARY") || strings.Contains(stdout, "vocabulary") {
				t.Fatalf("exit=%d stderr=%q %s", code, stderr, stdout)
			}
			if format == "json" {
				if first == "" {
					first = stdout
				} else if stdout != first {
					t.Fatalf("spec validate differs with the severity:\n%s\n%s", first, stdout)
				}
			}
		}
	}
}
