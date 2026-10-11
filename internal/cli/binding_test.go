package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// bindingDigest is a well-formed binding digest.
var bindingDigest = "sha256:" + strings.Repeat("0123456789abcdef", 4)

// bindingShape is the sentence a binding document not of its shape is
// refused with, before the reason.
const bindingShape = `The --binding document must be one JSON object of exactly {"kind": string, "digest": string}: `

// A well-formed binding lands on the evaluation's record exactly as given
// (ADR-0050), compact and kind first whatever the file's order and spacing;
// an evaluation without one records none; and audit verify, which holds a
// recorded binding to its shape only, says nothing about a well-formed one.
func TestEvaluateRecordsTheBindingAsGiven(t *testing.T) {
	configPath := auditProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	binding := writeDocument(t, "binding.json", "{ \"digest\": \""+bindingDigest+"\",\n  \"kind\": \"judgment-pack-runner/preparation/1\" }\n")

	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts, "--binding", binding, "--format", "json"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"disposition"`) {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	code, _, stderr = runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("two records: %q", data)
	}
	member := `"binding":{"kind":"judgment-pack-runner/preparation/1","digest":"` + bindingDigest + `"}`
	if strings.Count(lines[0], member) != 1 || strings.Count(lines[0], `"binding"`) != 1 {
		t.Fatalf("the binding as given: %s", lines[0])
	}
	if strings.Contains(lines[1], `"binding"`) {
		t.Fatalf("an evaluation without a binding records none: %s", lines[1])
	}
	verified, output := verification(t, "--config", configPath)
	if verified != 0 || output.Status != "valid" || output.FindingsTotal != 0 {
		t.Fatalf("a well-formed binding is not a finding: exit=%d %+v", verified, output.Findings)
	}
	if help := helpOf(t, "experimental", "evaluate"); !strings.Contains(help, "--binding") || !strings.Contains(help, "never interprets, verifies or recomputes the digest") {
		t.Fatalf("the help names the flag and what is not done with it: %q", help)
	}
}

// helpOf is a command's help text.
func helpOf(t *testing.T, args ...string) string {
	t.Helper()
	code, stdout, stderr := runTest(t, append(args, "--help"), "")
	if code != 0 {
		t.Fatalf("help: exit=%d stderr=%q", code, stderr)
	}
	return stdout
}

// A binding the runtime will not record is refused before anything is
// evaluated, as a malformed --cites is: exit 3, JPS-INVOCATION-BINDING, a
// sentence of its own naming no value of the document, in both formats, and
// nothing written -- not a record, not the trail, not its directory. A
// rehearsal records nothing, so a binding on one is refused too, before the
// document is read.
func TestEvaluateRefusesABindingItWillNotRecord(t *testing.T) {
	facts := writeDocument(t, "facts.json", hardFailFacts)
	valid := `{"kind":"k","digest":"` + bindingDigest + `"}`
	document := func(body string) func(*testing.T) string {
		return func(t *testing.T) string { return writeDocument(t, "binding.json", body) }
	}
	path := func(value string) func(*testing.T) string {
		return func(*testing.T) string { return value }
	}
	for _, tc := range []struct {
		name      string
		binding   func(*testing.T) string
		rehearsal bool
		want      string
	}{
		{"not an object", document(`[` + valid + `]`), false, bindingShape + "it is not one JSON object with each member named once."},
		{"not JSON", document(`{"kind":`), false, bindingShape + "it is not one JSON object with each member named once."},
		{"a member twice", document(`{"kind":"k","kind":"k","digest":"` + bindingDigest + `"}`), false, bindingShape + "it is not one JSON object with each member named once."},
		{"an extra member", document(`{"kind":"k","digest":"` + bindingDigest + `","note":"secret-value"}`), false, bindingShape + "it does not have exactly the two members kind and digest."},
		{"a missing member", document(`{"kind":"k"}`), false, bindingShape + "it does not have exactly the two members kind and digest."},
		{"a kind outside the grammar", document(`{"kind":"Secret Value","digest":"` + bindingDigest + `"}`), false, bindingShape + "kind must be a string of 1 to 128 characters from a-z, 0-9, dot, underscore, slash and hyphen."},
		{"a digest of another form", document(`{"kind":"k","digest":"sha256:SECRET"}`), false, bindingShape + "digest must be a string of sha256: and 64 lowercase hexadecimal characters."},
		{"a file over 1 KiB", document(valid + strings.Repeat(" ", audit.MaxBindingBytes+1-len(valid))), false, bindingShape + "it is longer than 1024 bytes."},
		{"standard input", path("-"), false, "--binding takes a file path, not -: the binding document is not read from standard input."},
		{"a URL", path("https://example.invalid/binding.json"), false, "URL and remote filesystem inputs are not supported for --binding; use a local file."},
		{"an absent file", func(t *testing.T) string { return filepath.Join(t.TempDir(), "absent.json") }, false, "The --binding document could not be read as one bounded regular file."},
		{"a rehearsal", document(valid), true, bindingRehearsalMessage},
		{"a rehearsal, before the document is read", func(t *testing.T) string { return filepath.Join(t.TempDir(), "absent.json") }, true, bindingRehearsalMessage},
	} {
		for _, format := range []string{"json", "human"} {
			t.Run(tc.name+" "+format, func(t *testing.T) {
				configPath := auditProject(t)
				args := []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath,
					"--facts", facts, "--binding", tc.binding(t), "--format", format}
				if tc.rehearsal {
					args = append(args, "--rehearsal")
				}
				code, stdout, stderr := runTest(t, args, "")
				if code != result.ExitInvocation {
					t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
				}
				if format == "json" {
					var output result.OperationalError
					if err := json.Unmarshal([]byte(stdout), &output); err != nil {
						t.Fatalf("undecodable %q: %v", stdout, err)
					}
					if output.Status != "error" || len(output.Diagnostics) != 1 || output.Diagnostics[0].Code != "JPS-INVOCATION-BINDING" ||
						output.Diagnostics[0].Message != tc.want || stderr != "" {
						t.Fatalf("output = %+v, stderr=%q", output, stderr)
					}
				} else if stdout != "" || stderr != "error: "+tc.want+"\n" {
					t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
				}
				if strings.Contains(stdout+stderr, "secret-value") || strings.Contains(stdout+stderr, "Secret Value") || strings.Contains(stdout+stderr, "SECRET") {
					t.Fatalf("the refusal repeats the document: %q %q", stdout, stderr)
				}
				if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit")); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("a refused invocation creates nothing: %v", err)
				}
			})
		}
	}
	// The refusal precedes the configuration, as the citations' does.
	broken := filepath.Join(t.TempDir(), "jpack.json")
	if err := os.WriteFile(broken, []byte(`{"configVersion":"3","audit":{},"packs":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", broken, "--facts", facts, "--binding", "-", "--format", "json"}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout+stderr, "JPS-INVOCATION-BINDING") {
		t.Fatalf("the binding refusal precedes the configuration: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

// audit verify reports a recorded binding that is not of its shape as
// binding-malformed at its line, in both formats, and fails the verification.
func TestAuditVerifyReportsAMalformedBinding(t *testing.T) {
	configPath := auditProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	binding := writeDocument(t, "binding.json", `{"kind":"k","digest":"`+bindingDigest+`"}`)
	for range 2 {
		if code, _, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
			"--config", configPath, "--facts", facts, "--binding", binding, "--format", "json"}, ""); code != 0 {
			t.Fatalf("exit=%d stderr=%q", code, stderr)
		}
	}
	trail := filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)
	rewriteLine(t, trail, 1, func(line []byte) []byte {
		return []byte(strings.Replace(string(line), `"kind":"k"`, `"kind":"K"`, 1))
	})
	code, output := verification(t, "--config", configPath)
	if code != 1 || output.Status != "invalid" || output.FindingsTotal != 1 || output.Findings[0].Name != audit.FindingBindingMalformed || output.Findings[0].Line != 2 {
		t.Fatalf("exit=%d %+v", code, output.Findings)
	}
	code, stdout, _ := runTest(t, []string{"audit", "verify", "--config", configPath}, "")
	if code != 1 || !strings.Contains(stdout, "INVALID: 1 failed check(s)\n") || !strings.Contains(stdout, "- binding-malformed (line 2): the record's binding is not one JSON object") {
		t.Fatalf("human output: exit=%d %q", code, stdout)
	}
}
