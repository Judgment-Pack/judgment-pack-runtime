package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/timestamp/tsatest"
)

// testAuthorityServer is a time-stamping authority on a local test server,
// and its root in a PEM file; no test asks a real authority.
func testAuthorityServer(t *testing.T) (*tsatest.Authority, *httptest.Server, string) {
	t.Helper()
	tsa, err := tsatest.New(tsatest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(tsa)
	t.Cleanup(server.Close)
	return tsa, server, writeDocument(t, "roots.pem", string(tsa.RootPEM()))
}

func stamping(t *testing.T, args ...string) (int, result.AuditStamp, string) {
	t.Helper()
	code, stdout, stderr := runTest(t, append([]string{"audit", "stamp", "--format", "json"}, args...), "")
	var output result.AuditStamp
	_ = json.Unmarshal([]byte(stdout), &output)
	return code, output, stdout + stderr
}

// audit stamp has the authority stamp the current checkpoint, keeps the token
// beside the trail, and asks nothing again for a checkpoint stamped already;
// audit verify --tsa-roots then reports the records stamped.
func TestAuditStampStampsTheCurrentCheckpointOnce(t *testing.T) {
	tsa, server, roots := testAuthorityServer(t)
	configPath, trail := recordedProject(t, 2)
	code, output, all := stamping(t, "--config", configPath, "--tsa", server.URL)
	if code != 0 || output.Status != "stamped" || output.Checkpoint.Sequence != 2 || output.StampedAt == "" || output.ExistedBy == "" || tsa.Requests != 1 {
		t.Fatalf("stamp: exit=%d %+v %s", code, output, all)
	}
	if strings.Contains(all, server.URL) {
		t.Fatal("the output does not name the authority's address")
	}
	stamps := filepath.Join(filepath.Dir(trail), audit.StampsName)
	data, err := os.ReadFile(stamps)
	if err != nil || len(bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))) != 1 {
		t.Fatalf("one stamp kept: %q %v", data, err)
	}
	code, output, _ = stamping(t, "--config", configPath, "--tsa", server.URL)
	if code != 0 || output.Status != "already-stamped" || tsa.Requests != 1 {
		t.Fatalf("a retry: exit=%d %+v requests=%d", code, output, tsa.Requests)
	}
	code, verified := verification(t, "--config", configPath, "--tsa-roots", roots, "--require-stamped-through", "2")
	if code != 0 || verified.Coverage.Stamped != (result.AuditCoverageState{Status: "through", Through: 2}) || verified.Stamps.Trusted != 1 ||
		verified.Stamps.Lag.Records != 2 || verified.RequiredStamped.Status != "met" {
		t.Fatalf("verify: exit=%d %+v %+v", code, verified.Coverage, verified.Stamps)
	}
	if code, named := verification(t, "--trail", trail, "--tsa-roots", roots); code != 0 || named.Stamps.Trusted != 1 {
		t.Fatalf("--trail reads the stamps beside it: exit=%d %+v", code, named.Stamps)
	}
	code, stdout, _ := runTest(t, []string{"audit", "verify", "--config", configPath, "--tsa-roots", roots}, "")
	if code != 0 || !strings.Contains(stdout, "stamped: through sequence 2") || !strings.Contains(stdout, "lag from at to the first trusted stamp: 2 record(s)") ||
		!strings.Contains(stdout, "NOT ESTABLISHED: When any record was made") || !strings.Contains(stdout, "ESTABLISHED: Lines 1 to 2 existed by") {
		t.Fatalf("human verify: %q", stdout)
	}
	// A new record is not covered until stamped again.
	evaluateOnce(t, configPath)
	code, verified = verification(t, "--config", configPath, "--tsa-roots", roots, "--require-stamped-through", "3")
	if code != result.ExitInvalid || verified.Coverage.Stamped.Through != 2 || verified.Findings[0].Name != audit.FindingStampCoverageMissing ||
		verified.RequiredStamped == nil || verified.RequiredStamped.Status != "unmet" || !endsWithAttempts(verified.DoesNotEstablish) {
		t.Fatalf("an unstamped record: exit=%d %+v %+v %q", code, verified.Coverage, verified.Findings, verified.DoesNotEstablish)
	}
	if code, output, _ = stamping(t, "--config", configPath, "--tsa", server.URL); code != 0 || output.Status != "stamped" || output.Checkpoint.Sequence != 3 {
		t.Fatalf("stamping the new checkpoint: exit=%d %+v", code, output)
	}
	// Roots of another authority trust none of it.
	_, _, otherRoots := testAuthorityServer(t)
	code, verified = verification(t, "--config", configPath, "--tsa-roots", otherRoots)
	if code != result.ExitInvalid || verified.Coverage.Stamped.Status != "none" || verified.Findings[0].Name != audit.FindingStampUntrusted {
		t.Fatalf("another root: exit=%d %+v", code, verified.Findings)
	}
	code, verified = verification(t, "--config", configPath, "--tsa-roots", roots, "--tsa-policy", "1.2.3.4")
	if code != result.ExitInvalid || verified.Findings[0].Name != audit.FindingStampPolicyMismatch {
		t.Fatalf("another policy: exit=%d %+v", code, verified.Findings)
	}
	if code, verified = verification(t, "--config", configPath, "--tsa-roots", roots, "--tsa-policy", tsatest.Policy.String()); code != 0 {
		t.Fatalf("its policy: exit=%d %+v", code, verified.Findings)
	}
	crl, err := tsa.CRL(time.Now().Add(time.Hour), time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if code, verified = verification(t, "--config", configPath, "--tsa-roots", roots, "--tsa-crls", writeDocument(t, "tsa.crl", string(crl))); code != 0 || verified.Stamps.RevocationChecked != 2 {
		t.Fatalf("a clean revocation list: exit=%d %+v", code, verified.Stamps)
	}
}

// The configuration names the authority under configVersion "6"; the flag
// replaces it.
func TestAuditStampReadsTheAuthorityFromTheConfiguration(t *testing.T) {
	_, server, _ := testAuthorityServer(t)
	configPath, _ := recordedProject(t, 1)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config := strings.Replace(string(data), `"configVersion":"3","audit":{"dir":"audit"}`, `"configVersion":"6","audit":{"dir":"audit","timestampAuthority":`+quote(server.URL)+`}`, 1)
	if config == string(data) {
		t.Fatal("the configuration was not rewritten")
	}
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, output, all := stamping(t, "--config", configPath); code != 0 || output.Status != "stamped" {
		t.Fatalf("the configured authority: exit=%d %s", code, all)
	}
}

func quote(text string) string {
	encoded, _ := json.Marshal(text)
	return string(encoded)
}

// An authority that cannot stamp leaves the trail and every decision in it as
// they were: nothing is written, the decisions go on recording, and asking
// again stamps the same checkpoint.
func TestAStampThatFailsLeavesTheTrailAndTheDecisionsUntouched(t *testing.T) {
	tsa, server, _ := testAuthorityServer(t)
	configPath, trail := recordedProject(t, 1)
	before, err := os.ReadFile(trail)
	if err != nil {
		t.Fatal(err)
	}
	stamps := filepath.Join(filepath.Dir(trail), audit.StampsName)
	cases := []struct {
		name  string
		setup func()
		code  string
	}{
		{"an HTTP failure", func() { tsa.Fail = http.StatusServiceUnavailable }, "JPS-AUDIT-STAMP-UNREACHABLE"},
		{"a rejection", func() { tsa.Status = 2 }, "JPS-AUDIT-STAMP-REJECTED"},
		{"a reply for another digest", func() { tsa.WrongDigest = true }, "JPS-AUDIT-STAMP-INVALID"},
	}
	for _, c := range cases {
		tsa.Fail, tsa.Status, tsa.WrongDigest = 0, 0, false
		c.setup()
		code, _, all := stamping(t, "--config", configPath, "--tsa", server.URL)
		if code != result.ExitIO || !strings.Contains(all, `"`+c.code+`"`) || !strings.Contains(all, "trail and the decisions in it are as they were") {
			t.Fatalf("%s: exit=%d %s", c.name, code, all)
		}
		if after, err := os.ReadFile(trail); err != nil || !bytes.Equal(before, after) {
			t.Fatalf("%s: the trail changed", c.name)
		}
		if _, err := os.Stat(stamps); !os.IsNotExist(err) {
			t.Fatalf("%s: a stamps file was written: %v", c.name, err)
		}
	}
	closed := httptest.NewServer(tsa)
	address := closed.URL
	closed.Close()
	if code, _, all := stamping(t, "--config", configPath, "--tsa", address); code != result.ExitIO || !strings.Contains(all, `"JPS-AUDIT-STAMP-UNREACHABLE"`) {
		t.Fatalf("nothing listening: exit=%d %s", code, all)
	}
	evaluateOnce(t, configPath)
	tsa.Fail, tsa.Status, tsa.WrongDigest = 0, 0, false
	if code, output, all := stamping(t, "--config", configPath, "--tsa", server.URL); code != 0 || output.Status != "stamped" || output.Checkpoint.Sequence != 2 {
		t.Fatalf("asking again: exit=%d %s", code, all)
	}
}

// What audit stamp refuses, and the flags verify reads stamps with.
func TestAuditStampAndItsFlagsRefuseWhatTheyCannotDo(t *testing.T) {
	_, server, roots := testAuthorityServer(t)
	configPath, trail := recordedProject(t, 2)
	for _, c := range []struct {
		args []string
		code string
	}{
		{[]string{"--config", configPath}, "JPS-AUDIT-STAMP-NO-AUTHORITY"},
		{[]string{"--config", configPath, "--tsa", "ftp://example.invalid/tsa"}, "JPS-INVOCATION-AUDIT-STAMP"},
		{[]string{"--config", configPath, "--tsa", server.URL, "--timeout", "0s"}, "JPS-INVOCATION-AUDIT-STAMP"},
	} {
		if code, _, all := stamping(t, c.args...); code != result.ExitInvocation || !strings.Contains(all, `"`+c.code+`"`) {
			t.Fatalf("%v: exit=%d %s", c.args, code, all)
		}
	}
	for _, args := range [][]string{
		{"--require-stamped-through", "1"},
		{"--tsa-policy", "1.2.3"},
		{"--tsa-crls", "list.crl"},
		{"--stamps", "stamps.jsonl"},
	} {
		code, stdout, _ := runTest(t, append([]string{"audit", "verify", "--format", "json", "--config", configPath}, args...), "")
		if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-INVOCATION-AUDIT-STAMPS"`) {
			t.Fatalf("%v: exit=%d %q", args, code, stdout)
		}
	}
	for _, c := range []struct {
		args []string
		code string
	}{
		{[]string{"--tsa-roots", writeDocument(t, "empty.pem", "no certificate here")}, "JPS-AUDIT-TSA-ROOTS-INVALID"},
		{[]string{"--tsa-roots", roots, "--tsa-policy", "not.an.oid"}, "JPS-INVOCATION-AUDIT-STAMPS"},
		{[]string{"--tsa-roots", roots, "--tsa-crls", writeDocument(t, "bad.crl", "not a list")}, "JPS-AUDIT-TSA-CRLS-INVALID"},
	} {
		code, stdout, _ := runTest(t, append([]string{"audit", "verify", "--format", "json", "--config", configPath}, c.args...), "")
		if code != result.ExitInvocation || !strings.Contains(stdout, `"`+c.code+`"`) {
			t.Fatalf("%v: exit=%d %q", c.args, code, stdout)
		}
	}
	// A trail that fails a check is not stamped.
	rewriteLine(t, trail, 0, func(line []byte) []byte { return append(line, ' ') })
	if code, _, all := stamping(t, "--config", configPath, "--tsa", server.URL); code != result.ExitInvalid || !strings.Contains(all, `"JPS-AUDIT-STAMP-REFUSED"`) {
		t.Fatalf("an invalid trail: exit=%d %s", code, all)
	}
	// With no roots, nothing is checked.
	if code, verified := verification(t, "--config", configPath); verified.Coverage.Stamped.Status != "not-checked" || code != result.ExitInvalid {
		t.Fatalf("no roots: exit=%d %+v", code, verified.Coverage)
	}
}

// A project whose trail is not chained has no checkpoint to stamp.
func TestAuditStampRefusesAnUnchainedTrail(t *testing.T) {
	_, server, _ := testAuthorityServer(t)
	configPath, _ := recordedProject(t, 1)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config := strings.Replace(string(data), `"configVersion":"3","audit":{"dir":"audit"}`, `"configVersion":"6","audit":{"dir":"audit","chain":false}`, 1)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, all := stamping(t, "--config", configPath, "--tsa", server.URL); code != result.ExitInvocation || !strings.Contains(all, `"JPS-AUDIT-STAMP-UNCHAINED"`) {
		t.Fatalf("an unchained trail: exit=%d %s", code, all)
	}
}
