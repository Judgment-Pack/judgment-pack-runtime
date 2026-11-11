package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// signingProject is the intake project under configVersion "8", keeping a
// trail, with members added to its audit member.
func signingProject(t *testing.T, members string) string {
	t.Helper()
	return writeProjectFixture(t, `{"configVersion":"8","audit":{"dir":"audit"`+members+`},"packs":{"intake":{"path":"packs/intake-0.1.0.pack.json","expectedVersion":"0.1.0"}}}`,
		map[string]string{"packs/intake-0.1.0.pack.json": evaluatorPack(t)})
}

// jsonString is value as a JSON string.
func jsonString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// withWarning is a payload as writeJSON writes it, with the one
// JPS-AUDIT-UNSIGNED warning added as its last member.
func withWarning(t *testing.T, payload, message string) string {
	t.Helper()
	body, found := strings.CutSuffix(payload, "}\n")
	if !found {
		t.Fatalf("not one JSON object: %q", payload)
	}
	return body + `,"diagnostics":[{"code":"JPS-AUDIT-UNSIGNED","codeStability":"provisional","layer":"audit","severity":"warning","instancePath":"","message":` + jsonString(t, message) + `}]}` + "\n"
}

// unsignedReasons are the ways a named key does not sign, each with the
// warning's exact text: where the key is, which key it is, and whether it is
// the key in force. Each returns the project to evaluate in.
func unsignedReasons(t *testing.T) map[string]func(t *testing.T) (string, string) {
	return map[string]func(t *testing.T) (string, string){
		"where: readable by others": func(t *testing.T) (string, string) {
			seed, _ := generatedKey(t)
			if err := os.Chmod(seed, 0o644); err != nil {
				t.Fatal(err)
			}
			return signingProject(t, `,"signingKey":`+jsonString(t, seed)),
				"The signing key the audit member's signingKey names, " + seed + ", is refused, so records are written unsigned: the signing key can be read or written by its group or by other users."
		},
		"where: through a symbolic link": func(t *testing.T) (string, string) {
			seed, _ := generatedKey(t)
			link := filepath.Join(filepath.Dir(seed), "link")
			if err := os.Symlink(seed, link); err != nil {
				t.Fatal(err)
			}
			t.Setenv(audit.SigningKeyEnv, link)
			return signingProject(t, ""),
				"The signing key JPACK_SIGNING_KEY names, " + link + ", is refused, so records are written unsigned: the signing key's path goes through a symbolic link; name the key by its real path."
		},
		"which: not the expected key": func(t *testing.T) (string, string) {
			seed, _ := generatedKey(t)
			_, other := generatedKey(t)
			t.Setenv(audit.SigningPublicKeyEnv, strings.TrimSpace(readFile(t, other)))
			return signingProject(t, `,"signingKey":`+jsonString(t, seed)),
				"The signing key the audit member's signingKey names is refused, so records are written unsigned: the key at the signing key's path is not the expected key (JPACK_SIGNING_PUBLIC_KEY names the expected key)."
		},
	}
}

// A decision whose named key will not sign its record says why, as a
// JPS-AUDIT-UNSIGNED warning carrying packs validate's reason (#354): in the
// payload under --format json, its last member, and on standard error
// otherwise. The disposition, the payload's every other byte, the human
// report, the exit code and the record are what they are for the same run
// with no key named; the record is written, unsigned. A rehearsal, a project
// naming no key, and a project keeping no trail report nothing.
func TestADecisionWhoseKeyWillNotSignSaysWhy(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	t.Setenv(audit.SigningPublicKeyEnv, "")
	skipWhereKeysCannotSign(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	evaluate := func(t *testing.T, configPath, format string, extra ...string) (int, string, string) {
		t.Helper()
		return runTest(t, append([]string{"experimental", "evaluate", "--format", format, "--pack-id", "intake", "--config", configPath, "--facts", facts}, extra...), "")
	}
	plain := signingProject(t, "")
	baseline := map[string]string{}
	for _, format := range []string{"json", "human"} {
		code, stdout, stderr := evaluate(t, plain, format)
		if code != 0 || stderr != "" {
			t.Fatalf("no key named: exit=%d stderr=%q", code, stderr)
		}
		baseline[format] = stdout
	}
	plainRecord := auditRecords(t, plain)[0]

	for name, reason := range unsignedReasons(t) {
		t.Run(name, func(t *testing.T) {
			configPath, want := reason(t)
			code, stdout, stderr := evaluate(t, configPath, "json")
			if code != 0 || stderr != "" {
				t.Fatalf("the decision is made: exit=%d stderr=%q", code, stderr)
			}
			if expected := withWarning(t, baseline["json"], want); stdout != expected {
				t.Fatalf("payload:\n got %q\nwant %q", stdout, expected)
			}
			code, stdout, stderr = evaluate(t, configPath, "human")
			if code != 0 || stdout != baseline["human"] || stderr != "warning: "+want+"\n" {
				t.Fatalf("human: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			records := auditRecords(t, configPath)
			if len(records) != 2 {
				t.Fatalf("both decisions are recorded: %d", len(records))
			}
			for _, record := range records {
				if !slices.Equal(sortedKeys(record), sortedKeys(plainRecord)) || !jsonEqualValues(t, record["disposition"], plainRecord["disposition"]) {
					t.Fatalf("the record is the one it would have been: %v", record)
				}
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit", audit.SidecarName)); !os.IsNotExist(err) {
				t.Fatalf("nothing is signed: %v", err)
			}
			// A rehearsal records nothing, so nothing is said of a record.
			for _, format := range []string{"json", "human"} {
				code, stdout, stderr := evaluate(t, configPath, format, "--rehearsal")
				if code != 0 || stderr != "" || strings.Contains(stdout, project.UnsignedCode) {
					t.Fatalf("rehearsal (%s): exit=%d stdout=%q stderr=%q", format, code, stdout, stderr)
				}
			}
			if records := auditRecords(t, configPath); len(records) != 2 {
				t.Fatalf("a rehearsal records nothing: %d", len(records))
			}
		})
	}

	// A project keeping no trail records nothing, so a refused key named for
	// it says nothing either.
	seed, _ := generatedKey(t)
	if err := os.Chmod(seed, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(audit.SigningKeyEnv, seed)
	noTrail := writeProjectFixture(t, `{"configVersion":"8","packs":{"intake":{"path":"packs/intake-0.1.0.pack.json"}}}`,
		map[string]string{"packs/intake-0.1.0.pack.json": evaluatorPack(t)})
	for _, format := range []string{"json", "human"} {
		if code, stdout, stderr := evaluate(t, noTrail, format); code != 0 || stderr != "" || strings.Contains(stdout, project.UnsignedCode) {
			t.Fatalf("no trail (%s): exit=%d stdout=%q stderr=%q", format, code, stdout, stderr)
		}
	}
}

func sortedKeys(record map[string]any) []string {
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func jsonEqualValues(t *testing.T, left, right any) bool {
	t.Helper()
	a, err := json.Marshal(left)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(right)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(a, b)
}

// signingRefusal is the refusal's exact output: the operational envelope
// under --format json, and one error line on standard error otherwise.
func signingRefusal(t *testing.T, command, format, message string) (string, string) {
	t.Helper()
	if format != "json" {
		return "", "error: " + message + "\n"
	}
	var expected bytes.Buffer
	if err := writeJSON(&expected, result.NewOperationalResult(command, "error", project.SigningRequiredCode, message), false); err != nil {
		t.Fatal(err)
	}
	return expected.String(), ""
}

// A project whose audit member sets requireSigning refuses a decision whose
// named key will not sign its record, for the warning's reason, and every
// decision while no key is named, before evaluating and with nothing recorded
// (#354). The refusal's bytes are exact in both formats. A rehearsal is not
// refused, a key that signs is not, and turning the member off is an edit the
// reviewed-set lock sees.
func TestAProjectThatRequiresSigningRefusesWhatWouldNotBeSigned(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	t.Setenv(audit.SigningPublicKeyEnv, "")
	skipWhereKeysCannotSign(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	evaluate := func(t *testing.T, configPath, format string, extra ...string) (int, string, string) {
		t.Helper()
		return runTest(t, append([]string{"experimental", "evaluate", "--format", format, "--pack-id", "intake", "--config", configPath, "--facts", facts}, extra...), "")
	}
	lead := func(configPath string) string {
		return "This evaluation was refused because " + configPath + " requires every decision's record to be signed, and "
	}
	refused := func(t *testing.T, configPath, message string) {
		t.Helper()
		for _, format := range []string{"json", "human"} {
			code, stdout, stderr := evaluate(t, configPath, format)
			wantOut, wantErr := signingRefusal(t, "experimental evaluate", format, message)
			if code != result.ExitInvalid || stdout != wantOut || stderr != wantErr {
				t.Fatalf("refusal (%s): exit=%d\nstdout %q\n  want %q\nstderr %q\n  want %q", format, code, stdout, wantOut, stderr, wantErr)
			}
		}
		noAuditTrail(t, configPath)
		for _, format := range []string{"json", "human"} {
			code, stdout, stderr := evaluate(t, configPath, format, "--rehearsal")
			if code != 0 || stderr != "" || strings.Contains(stdout, project.UnsignedCode) {
				t.Fatalf("a rehearsal is not refused (%s): exit=%d stdout=%q stderr=%q", format, code, stdout, stderr)
			}
		}
		noAuditTrail(t, configPath)
	}

	nothingNamed := signingProject(t, `,"requireSigning":true`)
	refused(t, nothingNamed, lead(nothingNamed)+"no signing key is named. Name one by the audit member's signingKey or by JPACK_SIGNING_KEY, or declare the run a rehearsal.")

	seed, _ := generatedKey(t)
	if err := os.Chmod(seed, 0o644); err != nil {
		t.Fatal(err)
	}
	where := signingProject(t, `,"signingKey":`+jsonString(t, seed)+`,"requireSigning":true`)
	refused(t, where, lead(where)+"the signing key the audit member's signingKey names, "+seed+", is refused: the signing key can be read or written by its group or by other users. Make the named key one that signs, or declare the run a rehearsal.")
	if err := os.Chmod(seed, 0o600); err != nil {
		t.Fatal(err)
	}
	_, other := generatedKey(t)
	t.Setenv(audit.SigningPublicKeyEnv, strings.TrimSpace(readFile(t, other)))
	refused(t, where, lead(where)+"the signing key the audit member's signingKey names is refused: the key at the signing key's path is not the expected key (JPACK_SIGNING_PUBLIC_KEY names the expected key). Make the named key one that signs, or declare the run a rehearsal.")
	t.Setenv(audit.SigningPublicKeyEnv, "")

	// The key signs: the decision is made, recorded and signed.
	code, stdout, stderr := evaluate(t, where, "json")
	if code != 0 || stderr != "" || strings.Contains(stdout, project.UnsignedCode) {
		t.Fatalf("a key that signs: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if code, output := verification(t, "--config", where); code != 0 || len(auditRecords(t, where)) != 1 || output.Coverage.UnsignedRecords != 0 {
		t.Fatalf("the decision is recorded: exit=%d %+v", code, output.Coverage)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(where), "audit", audit.SidecarName)); err != nil {
		t.Fatalf("and signed: %v", err)
	}

	// Turning the member off is an edit to reviewed law: a declared run is
	// refused as drift until the project locks again.
	mustLock(t, where)
	body, err := os.ReadFile(where)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(where, []byte(strings.Replace(string(body), `"requireSigning":true`, `"requireSigning":false`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := evaluate(t, where, "human"); code != result.ExitInvalid || !strings.Contains(stderr, "jpack packs lock") {
		t.Fatalf("turning the member off is an amendment the lock sees: exit=%d stderr=%q", code, stderr)
	}
}

// experimental graph evaluate says the same for its run, once, and a project
// that requires signing refuses the run before any node evaluates, recording
// nothing for any node or the composite. A rehearsal is neither warned nor
// refused.
func TestAGraphRunWhoseKeyWillNotSignSaysWhy(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	t.Setenv(audit.SigningPublicKeyEnv, "")
	skipWhereKeysCannotSign(t)
	seed, _ := generatedKey(t)
	if err := os.Chmod(seed, 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := writeGraphInputs(t, graphHappyInputs)
	signingGraph := func(t *testing.T, members string) (string, string) {
		t.Helper()
		config := strings.Replace(graphFixture(t, "jpack.json"),
			`"configVersion": "2",`, `"configVersion": "8",`+"\n"+`  "audit": {"dir": "audit"`+members+`},`, 1)
		configPath := writeProjectFixture(t, config, map[string]string{
			"sanctions-screening-0.1.0.pack.json": graphFixture(t, "sanctions-screening-0.1.0.pack.json"),
			"vendor-onboarding-0.1.0.pack.json":   graphFixture(t, "vendor-onboarding-0.1.0.pack.json"),
			"onboarding.graph.json":               graphFixture(t, "onboarding.graph.json"),
			"onboarding.rows.json":                graphFixture(t, "onboarding.rows.json"),
		})
		return configPath, filepath.Join(filepath.Dir(configPath), "onboarding.graph.json")
	}
	evaluate := func(t *testing.T, configPath, graphPath, format string, extra ...string) (int, string, string) {
		t.Helper()
		return runTest(t, append([]string{"experimental", "graph", "evaluate", graphPath, "--config", configPath, "--inputs", inputs, "--format", format}, extra...), "")
	}
	key := `,"signingKey":` + jsonString(t, seed)
	want := "The signing key the audit member's signingKey names, " + seed + ", is refused, so records are written unsigned: the signing key can be read or written by its group or by other users."

	plainConfig, plainGraph := signingGraph(t, "")
	_, baseline, _ := evaluate(t, plainConfig, plainGraph, "json")
	configPath, graphPath := signingGraph(t, key)
	code, stdout, stderr := evaluate(t, configPath, graphPath, "json")
	if code != 0 || stderr != "" {
		t.Fatalf("the run is made: exit=%d stderr=%q", code, stderr)
	}
	// The payload names paths in its own project, so only those differ from
	// the run with no key named.
	baseline = strings.ReplaceAll(baseline, filepath.Dir(plainConfig), filepath.Dir(configPath))
	var plainRun, warnedRun map[string]any
	if err := json.Unmarshal([]byte(baseline), &plainRun); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(stdout), &warnedRun); err != nil {
		t.Fatal(err)
	}
	if warning := `,"diagnostics":[{"code":"JPS-AUDIT-UNSIGNED","codeStability":"provisional","layer":"audit","severity":"warning","instancePath":"","message":` + jsonString(t, want) + `}]`; strings.Count(stdout, `"diagnostics":`) != 1 || !strings.Contains(stdout, warning) {
		t.Fatalf("the warning, once, exactly:\n%s", stdout)
	}
	delete(warnedRun, "diagnostics")
	delete(warnedRun, "configSha256")
	delete(plainRun, "configSha256")
	if !jsonEqualValues(t, warnedRun, plainRun) {
		t.Fatalf("the run is otherwise the one it would have been:\n%s\n%s", stdout, baseline)
	}
	recorded := len(auditRecords(t, configPath))
	if recorded == 0 || recorded != len(auditRecords(t, plainConfig)) {
		t.Fatalf("every record is written: %d", recorded)
	}
	code, _, stderr = evaluate(t, configPath, graphPath, "human")
	if code != 0 || stderr != "warning: "+want+"\n" {
		t.Fatalf("human: exit=%d stderr=%q", code, stderr)
	}
	for _, format := range []string{"json", "human"} {
		code, stdout, stderr := evaluate(t, configPath, graphPath, format, "--rehearsal")
		if code != 0 || stderr != "" || strings.Contains(stdout, project.UnsignedCode) {
			t.Fatalf("rehearsal (%s): exit=%d stdout=%q stderr=%q", format, code, stdout, stderr)
		}
	}

	required, requiredGraph := signingGraph(t, key+`,"requireSigning":true`)
	message := "This evaluation was refused because " + required + " requires every decision's record to be signed, and the signing key the audit member's signingKey names, " + seed + ", is refused: the signing key can be read or written by its group or by other users. Make the named key one that signs, or declare the run a rehearsal."
	for _, format := range []string{"json", "human"} {
		code, stdout, stderr := evaluate(t, required, requiredGraph, format)
		wantOut, wantErr := signingRefusal(t, "experimental graph evaluate", format, message)
		if code != result.ExitInvalid || stdout != wantOut || stderr != wantErr {
			t.Fatalf("refusal (%s): exit=%d\nstdout %q\n  want %q\nstderr %q\n  want %q", format, code, stdout, wantOut, stderr, wantErr)
		}
	}
	if code, _, stderr := evaluate(t, required, requiredGraph, "json", "--rehearsal"); code != 0 || stderr != "" {
		t.Fatalf("a rehearsal is not refused: exit=%d stderr=%q", code, stderr)
	}
	noAuditTrail(t, required)
}
