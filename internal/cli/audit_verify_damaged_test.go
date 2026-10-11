package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// damagedWithOddBytes ends in a carriage return and bytes that are not UTF-8:
// the member is over the raw bytes, as they are.
const damagedWithOddBytes = "{\"torn\":\r\xff\xfe"

// verify's incompleteLastLine member (#260) gives the line, the count and the
// digest of the bytes after the trail's last newline, and audit repair accepts
// them as they stand.
func TestAuditVerifyIncompleteLastLineMemberFeedsTheRepair(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			configPath, trail := tornProject(t, damagedWithOddBytes)
			code, stdout, stderr := runTest(t, []string{"audit", "verify", "--config", configPath, "--format", "json"}, "")
			var report result.AuditVerification
			if err := json.Unmarshal([]byte(stdout), &report); err != nil || code == 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q %v", code, stdout, stderr, err)
			}
			tail := report.IncompleteLastLine
			if tail == nil {
				t.Fatalf("no incompleteLastLine member: %s", stdout)
			}
			if tail.Line != report.Lines+1 || tail.Bytes != int64(len(damagedWithOddBytes)) || tail.Digest != audit.Digest([]byte(damagedWithOddBytes)) {
				t.Fatalf("member %+v for %d damaged bytes after %d lines", tail, len(damagedWithOddBytes), report.Lines)
			}
			if format == "text" {
				_, human, _ := runTest(t, []string{"audit", "verify", "--config", configPath}, "")
				want := fmt.Sprintf("incomplete last line: line %d, %d bytes after the trail's last newline, %s;", tail.Line, tail.Bytes, tail.Digest)
				if !strings.Contains(human, want) {
					t.Fatalf("the human report lacks %q:\n%s", want, human)
				}
			}
			code, stdout, stderr = runTest(t, []string{"audit", "repair", "--config", configPath, "--format", "json",
				"--damaged-bytes", fmt.Sprint(tail.Bytes), "--damaged-digest", tail.Digest}, "")
			var repaired result.AuditRepair
			if err := json.Unmarshal([]byte(stdout), &repaired); err != nil || code != 0 || repaired.Status != "repaired" ||
				repaired.Discontinuity.Bytes != tail.Bytes || repaired.Discontinuity.Digest != tail.Digest {
				t.Fatalf("the repair refused verify's own values: exit=%d stdout=%q stderr=%q %v", code, stdout, stderr, err)
			}
			if after, err := os.ReadFile(trail); err != nil || !strings.Contains(string(after), damagedWithOddBytes+"\n") {
				t.Fatalf("the damaged bytes are kept: %q %v", after, err)
			}
		})
	}
}

// A trail whose last line is complete has no member, in either format.
func TestAuditVerifyCompleteTrailHasNoIncompleteLastLineMember(t *testing.T) {
	configPath, _ := recordedProject(t, 2)
	_, stdout, _ := runTest(t, []string{"audit", "verify", "--config", configPath, "--format", "json"}, "")
	if strings.Contains(stdout, "incompleteLastLine") {
		t.Fatalf("a complete trail carries the member: %s", stdout)
	}
	_, human, _ := runTest(t, []string{"audit", "verify", "--config", configPath}, "")
	if strings.Contains(human, "incomplete last line") {
		t.Fatalf("a complete trail's human report names an incomplete last line:\n%s", human)
	}
}

// The report for a complete trail is byte-identical to what the code before
// this member produced; the fixtures were taken from it. The trail's path and
// nothing else is normalised.
func TestAuditVerifyCompleteTrailReportIsUnchanged(t *testing.T) {
	trail := filepath.Join(t.TempDir(), "trail.jsonl")
	if err := os.WriteFile(trail, []byte("{\"a\":1}\n{\"b\":2}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for format, golden := range map[string]string{"json": "audit_verify_complete.golden.json", "human": "audit_verify_complete.golden.txt"} {
		want, err := os.ReadFile(filepath.Join("testdata", golden))
		if err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := runTest(t, []string{"audit", "verify", "--trail", trail, "--format", format}, "")
		shown := trail
		if format == "json" {
			// The path appears JSON-escaped (backslashes doubled on Windows).
			encoded, err := json.Marshal(trail)
			if err != nil {
				t.Fatal(err)
			}
			shown = string(encoded[1 : len(encoded)-1])
		}
		got := strings.ReplaceAll(stdout, shown, "trail.jsonl")
		if code != 0 || got != string(want) {
			t.Fatalf("%s: exit=%d stderr=%q\ngot:  %q\nwant: %q", format, code, stderr, got, want)
		}
	}
}
