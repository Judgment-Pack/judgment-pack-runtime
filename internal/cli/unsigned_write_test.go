package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// auditFailure is an audit write failure's exact output: the operational
// envelope under --format json, and one error line on standard error
// otherwise.
func auditFailure(t *testing.T, format, message string) (string, string) {
	t.Helper()
	if format != "json" {
		return "", "error: " + message + "\n"
	}
	var expected bytes.Buffer
	if err := writeJSON(&expected, result.NewOperationalResult("experimental evaluate", "error", audit.FailureCode, message), false); err != nil {
		t.Fatal(err)
	}
	return expected.String(), ""
}

// A record the write itself could not sign, though the key named signs, is
// reported as such (#354, ADR-0071): with the JPS-AUDIT-UNSIGNED warning
// saying what the write found, the decision otherwise as it would have been,
// when the project does not require signing; and as a JPS-AUDIT-WRITE failure
// with no disposition when it does. A signature that could not be written
// leaves the record written and says so; a trail no lock can be taken on is
// refused before anything is appended, leaving only the file the open made.
func TestARecordTheWriteCouldNotSignIsReportedOrRefused(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	t.Setenv(audit.SigningPublicKeyEnv, "")
	skipWhereKeysCannotSign(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	evaluate := func(t *testing.T, configPath, format string) (int, string, string) {
		t.Helper()
		return runTest(t, []string{"experimental", "evaluate", "--format", format, "--pack-id", "intake", "--config", configPath, "--facts", facts}, "")
	}
	seed, _ := generatedKey(t)
	signer, err := audit.ReadKey(seed)
	if err != nil {
		t.Fatal(err)
	}
	key := `,"signingKey":` + jsonString(t, seed)
	baseline := map[string]string{}
	signs := signingProject(t, key)
	for _, format := range []string{"json", "human"} {
		code, stdout, stderr := evaluate(t, signs, format)
		if code != 0 || stderr != "" || strings.Contains(stdout, project.UnsignedCode) {
			t.Fatalf("a key that signs: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		baseline[format] = stdout
	}
	sidecar := func(configPath string) string {
		return filepath.Join(filepath.Dir(configPath), "audit", audit.SidecarName)
	}
	trail := func(configPath string) string {
		return filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)
	}

	for name, tc := range map[string]struct {
		standIn  func() (restore func())
		warning  string
		refusal  string
		recorded bool
	}{
		"a signature that cannot be written": {
			func() func() { return audit.StandInSidecarWriteForTests(errors.New("the sidecar's disk is full")) },
			"The signature sidecar could not be written, so key " + signer.KeyID() + " signed nothing and the records written are unsigned.",
			"Audit record was written unsigned: the project requires every record to be signed, and the record's signature could not be written to the signature sidecar.",
			true,
		},
		"a trail no lock can be taken on": {
			fssecure.StandInNoLockForTests,
			"No lock can be taken on the audit trail here, so records are written unchained and unsigned.",
			"Audit record could not be written: the project requires every record to be signed, and no lock can be taken on the audit trail here, so no record would be chained or signed.",
			false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			warned := signingProject(t, key)
			required := signingProject(t, key+`,"requireSigning":true`)
			restore := tc.standIn()
			defer restore()

			code, stdout, stderr := evaluate(t, warned, "json")
			if expected := withWarning(t, baseline["json"], tc.warning); code != 0 || stderr != "" || stdout != expected {
				t.Fatalf("warned: exit=%d stderr=%q\n got %q\nwant %q", code, stderr, stdout, expected)
			}
			code, stdout, stderr = evaluate(t, warned, "human")
			if code != 0 || stdout != baseline["human"] || stderr != "warning: "+tc.warning+"\n" {
				t.Fatalf("warned, human: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			if records := auditRecords(t, warned); len(records) != 2 {
				t.Fatalf("both decisions are recorded: %d", len(records))
			}

			for _, format := range []string{"json", "human"} {
				code, stdout, stderr := evaluate(t, required, format)
				wantOut, wantErr := auditFailure(t, format, tc.refusal)
				if code != result.ExitIO || stdout != wantOut || stderr != wantErr {
					t.Fatalf("required (%s): exit=%d\nstdout %q\n  want %q\nstderr %q\n  want %q", format, code, stdout, wantOut, stderr, wantErr)
				}
			}
			if tc.recorded {
				if records := auditRecords(t, required); len(records) != 2 {
					t.Fatalf("each record written stays: %d", len(records))
				}
			} else if written, err := os.ReadFile(trail(required)); err != nil || len(written) != 0 {
				t.Fatalf("nothing is appended: %v %q", err, written)
			}
			for _, configPath := range []string{warned, required} {
				if _, err := os.Stat(sidecar(configPath)); !os.IsNotExist(err) {
					t.Fatalf("nothing is signed: %v", err)
				}
			}
		})
	}
}

// A decision whose key is not the key in force, another key having signed the
// trail's last record, says so in the JPS-AUDIT-UNSIGNED warning (#354).
func TestADecisionWhoseKeyIsNotInForceSaysSo(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	t.Setenv(audit.SigningPublicKeyEnv, "")
	skipWhereKeysCannotSign(t)
	first, _ := generatedKey(t)
	second, _ := generatedKey(t)
	signer, err := audit.ReadKey(second)
	if err != nil {
		t.Fatal(err)
	}
	configPath := signingProject(t, "")
	t.Setenv(audit.SigningKeyEnv, first)
	evaluateOnce(t, configPath)
	t.Setenv(audit.SigningKeyEnv, second)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	want := "Key " + signer.KeyID() + " is not the key in force in the signature sidecar, so it signs nothing and records are written unsigned: rotate to it from the key in force with jpack audit key rotate, or name the key in force."
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--format", "human", "--pack-id", "intake", "--config", configPath, "--facts", facts}, "")
	if code != 0 || stderr != "warning: "+want+"\n" || strings.Contains(stdout, project.UnsignedCode) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if records := auditRecords(t, configPath); len(records) != 2 {
		t.Fatalf("the decision is recorded: %d", len(records))
	}
}

// requireSigning refuses before evaluating: facts the evaluation would refuse
// are not reached, the run reporting JPS-AUDIT-SIGNING-REQUIRED and no
// evaluation error, while the same facts in a project that does not require
// signing are refused by the evaluation.
func TestRequireSigningRefusesBeforeEvaluating(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	t.Setenv(audit.SigningPublicKeyEnv, "")
	malformed := writeDocument(t, "facts.json", `{"request":`)
	evaluate := func(t *testing.T, configPath string) (int, string, string) {
		t.Helper()
		return runTest(t, []string{"experimental", "evaluate", "--format", "json", "--pack-id", "intake", "--config", configPath, "--facts", malformed}, "")
	}
	required := signingProject(t, `,"requireSigning":true`)
	code, stdout, stderr := evaluate(t, required)
	wantOut, _ := signingRefusal(t, "experimental evaluate", "json", "This evaluation was refused because "+required+" requires every decision's record to be signed, and no signing key is named. Name one by the audit member's signingKey or by JPACK_SIGNING_KEY, or declare the run a rehearsal.")
	if code != result.ExitInvalid || stdout != wantOut || stderr != "" || strings.Contains(stdout, "evaluationError") {
		t.Fatalf("refused before evaluating: exit=%d\nstdout %q\n  want %q\nstderr %q", code, stdout, wantOut, stderr)
	}
	noAuditTrail(t, required)
	code, stdout, _ = evaluate(t, signingProject(t, ""))
	if code == 0 || !strings.Contains(stdout, `"evaluationError"`) || strings.Contains(stdout, project.SigningRequiredCode) {
		t.Fatalf("the same facts are refused by the evaluation: exit=%d stdout=%q", code, stdout)
	}
}
