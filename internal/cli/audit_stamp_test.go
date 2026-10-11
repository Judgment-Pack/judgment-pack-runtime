package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/timestamp/tsatest"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

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
// again stamps the checkpoint at the trail's head then.
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
		if data, err := os.ReadFile(stamps); !os.IsNotExist(err) && (err != nil || len(data) != 0) {
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

// A token that cannot be kept leaves the trail and its decisions untouched,
// but a retry stamps whichever checkpoint is at the trail's head then.
func TestAuditStampReportsTheCheckpointAWriteFailureRetryWillStamp(t *testing.T) {
	for _, format := range []string{"json", "human"} {
		t.Run(format, func(t *testing.T) {
			tsa, err := tsatest.New(tsatest.Options{})
			if err != nil {
				t.Fatal(err)
			}
			originalTransport := http.DefaultClient.Transport
			http.DefaultClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				response := httptest.NewRecorder()
				tsa.ServeHTTP(response, request)
				return response.Result(), nil
			})
			t.Cleanup(func() { http.DefaultClient.Transport = originalTransport })
			configPath, trail := recordedProject(t, 1)
			stamps := filepath.Join(filepath.Dir(trail), audit.StampsName)
			if err := os.WriteFile(stamps, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(stamps)
			if err != nil {
				t.Fatal(err)
			}
			beforeStampAppend = func() error { return errors.New("forced stamp append failure") }
			t.Cleanup(func() { beforeStampAppend = nil })

			code, stdout, stderr := runTest(t, []string{"audit", "stamp", "--format", format, "--config", configPath, "--tsa", "https://tsa.invalid/stamp"}, "")
			const want = "The token could not be kept in the stamps file; the trail and the decisions in it are as they were, and asking again stamps the checkpoint at the trail's head at that time."
			if code != result.ExitIO || tsa.Requests != 1 {
				t.Fatalf("append failure: exit=%d requests=%d stdout=%q stderr=%q", code, tsa.Requests, stdout, stderr)
			}
			if format == "json" {
				var output result.OperationalError
				if err := json.Unmarshal([]byte(stdout), &output); err != nil {
					t.Fatal(err)
				}
				if len(output.Diagnostics) != 1 || output.Diagnostics[0].Code != "JPS-AUDIT-STAMPS-WRITE" || output.Diagnostics[0].Message != want {
					t.Fatalf("diagnostic: %+v", output)
				}
			} else if stdout != "" || stderr != "error: "+want+"\n" {
				t.Fatalf("human: stdout=%q stderr=%q", stdout, stderr)
			}
			after, err := os.ReadFile(stamps)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("stamps changed: before=%q after=%q err=%v", before, after, err)
			}
			beforeStampAppend = nil
			if retryCode, _, all := stamping(t, "--config", configPath, "--tsa", "https://tsa.invalid/stamp"); retryCode != 0 || tsa.Requests != 2 {
				t.Fatalf("discarded token was not requested again: exit=%d requests=%d output=%q", retryCode, tsa.Requests, all)
			}
		})
	}
}

func TestAuditStampRefusesAnUnwritableStampsFileBeforeAuthority(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not what a Windows file mode means")
	}
	configPath, trail := recordedProject(t, 1)
	stamps := filepath.Join(filepath.Dir(trail), audit.StampsName)
	if err := os.WriteFile(stamps, nil, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stamps, 0o600) })
	originalTransport := http.DefaultClient.Transport
	requests := 0
	http.DefaultClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("request must not be made")
	})
	t.Cleanup(func() { http.DefaultClient.Transport = originalTransport })

	code, _, all := stamping(t, "--config", configPath, "--tsa", "https://tsa.invalid/stamp")
	var output result.OperationalError
	if err := json.Unmarshal([]byte(all), &output); err != nil {
		t.Fatal(err)
	}
	const want = "The stamps file could not be held open; no authority was asked."
	if code != result.ExitIO || requests != 0 || len(output.Diagnostics) != 1 || output.Diagnostics[0].Code != "JPS-AUDIT-STAMPS-WRITE" || output.Diagnostics[0].Message != want {
		t.Fatalf("unwritable stamps file: exit=%d requests=%d output=%+v", code, requests, output)
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

// An oversized stamps file is an invalid verification in both output formats,
// even when no stamping requirement was supplied. This test needs no server.
func TestAuditVerifyReportsOversizedStamps(t *testing.T) {
	_, trail := recordedProject(t, 1)
	tsa, err := tsatest.New(tsatest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	roots := writeDocument(t, "roots.pem", string(tsa.RootPEM()))
	file, err := os.Create(filepath.Join(filepath.Dir(trail), audit.StampsName))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(audit.MaxStampsBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	code, got := verification(t, "--trail", trail, "--tsa-roots", roots)
	if code != result.ExitInvalid || got.Status != "invalid" || len(got.Findings) != 1 || got.Findings[0].Name != "stamp-file-too-large" || got.Coverage.Stamped.Through != 0 {
		t.Fatalf("oversized stamps JSON: exit=%d %+v", code, got)
	}
	code, stdout, stderr := runTest(t, []string{"audit", "verify", "--trail", trail, "--tsa-roots", roots}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, "INVALID") || !strings.Contains(stdout, "stamp-file-too-large") || !strings.Contains(stdout, "no stamps were checked") ||
		!strings.Contains(stdout, "stamps file: not read because it exceeds 67108864 bytes") || strings.Contains(stdout, "stamps file: 0 line(s), 0 unreadable") {
		t.Fatalf("oversized stamps human: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

// A stamps file with no room for the largest accepted line is refused before
// any HTTP request. A file already beyond the verification bound gets the same
// dedicated operational code and is described as too large, not unreadable.
func TestAuditStampRefusesAFullStampsFileBeforeAsking(t *testing.T) {
	configPath, trail := recordedProject(t, 1)
	name := filepath.Join(filepath.Dir(trail), audit.StampsName)
	file, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(audit.MaxStampsBytes - audit.MaxStampLineBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	originalTransport := http.DefaultClient.Transport
	requests := 0
	http.DefaultClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("request must not be made")
	})
	t.Cleanup(func() { http.DefaultClient.Transport = originalTransport })

	code, _, all := stamping(t, "--config", configPath, "--tsa", "https://tsa.invalid/stamp")
	if code != result.ExitIO || requests != 0 || !strings.Contains(all, `"JPS-AUDIT-STAMPS-TOO-LARGE"`) || !strings.Contains(all, "no authority was asked") {
		t.Fatalf("full file: exit=%d requests=%d output=%q", code, requests, all)
	}
	if err := os.Truncate(name, audit.MaxStampsBytes+1); err != nil {
		t.Fatal(err)
	}
	code, _, all = stamping(t, "--config", configPath, "--tsa", "https://tsa.invalid/stamp")
	if code != result.ExitIO || requests != 0 || !strings.Contains(all, `"JPS-AUDIT-STAMPS-TOO-LARGE"`) || !strings.Contains(all, "larger than its 67108864-byte limit") || strings.Contains(all, "could not be read to see") {
		t.Fatalf("oversized file: exit=%d requests=%d output=%q", code, requests, all)
	}
}

// A token obtained for a moved trail belongs to neither a fresh trail nor the
// retired stamps file. Both output formats report the same refusal.
func TestAuditStampRefusesAMovedTrail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot rename these open files")
	}
	for _, format := range []string{"json", "human"} {
		t.Run(format, func(t *testing.T) {
			address, roots := stampAuthorityTransport(t)
			configPath, trail := recordedProject(t, 1)
			if code, _, all := stamping(t, "--config", configPath, "--tsa", address); code != 0 {
				t.Fatal(all)
			}
			evaluateOnce(t, configPath)
			stamps := filepath.Join(filepath.Dir(trail), audit.StampsName)
			before, err := os.ReadFile(stamps)
			if err != nil {
				t.Fatal(err)
			}
			retired := t.TempDir()
			oldTrail := filepath.Join(retired, filepath.Base(trail))
			oldStamps := filepath.Join(retired, audit.StampsName)
			beforeStampAppend = func() error {
				if err := os.Rename(trail, oldTrail); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(stamps, oldStamps); err != nil {
					t.Fatal(err)
				}
				evaluateOnce(t, configPath)
				return nil
			}
			t.Cleanup(func() { beforeStampAppend = nil })
			code, stdout, stderr := runTest(t, []string{"audit", "stamp", "--format", format, "--config", configPath, "--tsa", address}, "")
			const message = "The trail at the path is not the one whose checkpoint was stamped; nothing was written, and the token was discarded."
			if code != result.ExitIO {
				t.Errorf("exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			if format == "json" {
				var got result.OperationalError
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatal(err)
				}
				if len(got.Diagnostics) != 1 || got.Diagnostics[0].Code != "JPS-AUDIT-STAMP-TRAIL-MOVED" || got.Diagnostics[0].Message != message {
					t.Errorf("diagnostic: %s", stdout)
				}
			} else if stdout != "" || stderr != "error: "+message+"\n" {
				t.Errorf("human: stdout=%q stderr=%q", stdout, stderr)
			}
			if data, err := os.ReadFile(stamps); !os.IsNotExist(err) && (err != nil || len(data) != 0) {
				t.Errorf("new stamps: %q %v", data, err)
			}
			if data, err := os.ReadFile(oldStamps); err != nil || !bytes.Equal(data, before) {
				t.Errorf("old stamps changed: %v", err)
			}
			if code, got := verification(t, "--trail", trail, "--tsa-roots", roots); code != 0 || got.Status != "valid" || len(got.Findings) != 0 || got.Coverage.Stamped.Status != "none" {
				t.Errorf("fresh trail: exit=%d %+v", code, got)
			}
			if code, got := verification(t, "--trail", oldTrail, "--tsa-roots", roots); code != 0 || got.Status != "valid" || len(got.Findings) != 0 || got.Coverage.Stamped.Through != 1 || got.Stamps.Trusted != 1 {
				t.Errorf("retired trail: exit=%d %+v", code, got)
			}
		})
	}
}

// Ordinary decisions may append while the authority is asked: the snapshot's
// checkpoint remains part of that same trail, with no trail lock retained.
func TestAuditStampAllowsAnAppendToTheSameTrail(t *testing.T) {
	address, roots := stampAuthorityTransport(t)
	configPath, _ := recordedProject(t, 1)
	beforeStampAppend = func() error {
		evaluateOnce(t, configPath)
		return nil
	}
	t.Cleanup(func() { beforeStampAppend = nil })
	if code, got, all := stamping(t, "--config", configPath, "--tsa", address); code != 0 || got.Checkpoint.Sequence != 1 {
		t.Fatalf("stamp: %d %s", code, all)
	}
	if code, got := verification(t, "--config", configPath, "--tsa-roots", roots); code != 0 || got.Coverage.Stamped.Through != 1 {
		t.Fatalf("verify: %d %+v", code, got)
	}
}

// stampAuthorityTransport exercises the RFC 3161 exchange without a socket.
func stampAuthorityTransport(t *testing.T) (string, string) {
	t.Helper()
	tsa, err := tsatest.New(tsatest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	original := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		reply := httptest.NewRecorder()
		tsa.ServeHTTP(reply, request)
		return reply.Result(), nil
	})
	t.Cleanup(func() { http.DefaultClient.Transport = original })
	return "https://tsa.invalid/stamp", writeDocument(t, "roots.pem", string(tsa.RootPEM()))
}

// stampRefusal runs audit stamp in one format and holds it to one refusal.
func stampRefusal(t *testing.T, format, configPath, address, code, message string) {
	t.Helper()
	exit, stdout, stderr := runTest(t, []string{"audit", "stamp", "--format", format, "--config", configPath, "--tsa", address}, "")
	if exit != result.ExitIO {
		t.Errorf("exit=%d stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if format == "json" {
		var got result.OperationalError
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("%v: %s", err, stdout)
		}
		if got.Status != "error" || len(got.Diagnostics) != 1 || got.Diagnostics[0].Code != code || got.Diagnostics[0].Message != message {
			t.Errorf("diagnostic: %s", stdout)
		}
	} else if stdout != "" || stderr != "error: "+message+"\n" {
		t.Errorf("human: stdout=%q stderr=%q", stdout, stderr)
	}
}

const stampsMovedMessage = "The stamps file was moved, removed, linked elsewhere or truncated during the request; nothing was written, and the token was discarded."

// A stamps file removed, or linked elsewhere, while the authority is asked is
// refused: the token is not appended to a file the name no longer names alone.
func TestAuditStampRefusesAStampsFileChangedDuringTheRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot remove these open files, and the link count is not read there")
	}
	for _, format := range []string{"json", "human"} {
		t.Run("removed/"+format, func(t *testing.T) {
			address, roots := stampAuthorityTransport(t)
			configPath, trail := recordedProject(t, 1)
			stamps := filepath.Join(filepath.Dir(trail), audit.StampsName)
			beforeStampAppend = func() error { return os.Remove(stamps) }
			t.Cleanup(func() { beforeStampAppend = nil })
			stampRefusal(t, format, configPath, address, "JPS-AUDIT-STAMPS-MOVED", stampsMovedMessage)
			if _, err := os.Stat(stamps); !os.IsNotExist(err) {
				t.Errorf("a stamps file appeared: %v", err)
			}
			if code, got := verification(t, "--config", configPath, "--tsa-roots", roots); code != 0 || got.Status != "valid" || got.Coverage.Stamped.Status != "none" {
				t.Errorf("verify: exit=%d %+v", code, got)
			}
		})
		t.Run("linked/"+format, func(t *testing.T) {
			address, _ := stampAuthorityTransport(t)
			configPath, trail := recordedProject(t, 1)
			stamps := filepath.Join(filepath.Dir(trail), audit.StampsName)
			other := filepath.Join(t.TempDir(), "elsewhere")
			beforeStampAppend = func() error { return os.Link(stamps, other) }
			t.Cleanup(func() { beforeStampAppend = nil })
			stampRefusal(t, format, configPath, address, "JPS-AUDIT-STAMPS-MOVED", stampsMovedMessage)
			if data, err := os.ReadFile(other); err != nil || len(data) != 0 {
				t.Errorf("linked file: %q %v", data, err)
			}
		})
	}
}

// A trail truncated in place and written again keeps its file identity. One
// written back shorter than the snapshot is refused by its size; one written
// back as long or longer, by the record at the checkpoint's sequence read
// again. The stamps file is truncated beside it, as a copying rotation does.
func TestAuditStampRefusesATrailTruncatedInPlace(t *testing.T) {
	for _, rewrite := range []struct {
		name      string
		decisions int
	}{{"shorter", 1}, {"as-long", 3}} {
		for _, format := range []string{"json", "human"} {
			t.Run(rewrite.name+"/"+format, func(t *testing.T) {
				address, roots := stampAuthorityTransport(t)
				configPath, trail := recordedProject(t, 1)
				if code, _, all := stamping(t, "--config", configPath, "--tsa", address); code != 0 {
					t.Fatal(all)
				}
				evaluateOnce(t, configPath)
				before, err := os.Stat(trail)
				if err != nil {
					t.Fatal(err)
				}
				stamps := filepath.Join(filepath.Dir(trail), audit.StampsName)
				beforeStampAppend = func() error {
					for _, each := range []string{trail, stamps} {
						if err := os.Truncate(each, 0); err != nil {
							return err
						}
					}
					for range rewrite.decisions {
						evaluateOnce(t, configPath)
					}
					return nil
				}
				t.Cleanup(func() { beforeStampAppend = nil })
				stampRefusal(t, format, configPath, address, "JPS-AUDIT-STAMP-TRAIL-MOVED", "The trail at the path is not the one whose checkpoint was stamped; nothing was written, and the token was discarded.")
				after, err := os.Stat(trail)
				if err != nil || !os.SameFile(before, after) || (rewrite.decisions > 1) != (after.Size() >= before.Size()) {
					t.Fatalf("the rewrite is not the case under test: %v %v", after, err)
				}
				if data, err := os.ReadFile(stamps); err != nil || len(data) != 0 {
					t.Errorf("stamps written: %q %v", data, err)
				}
				if code, got := verification(t, "--config", configPath, "--tsa-roots", roots); code != 0 || got.Status != "valid" || len(got.Findings) != 0 || got.Coverage.Stamped.Status != "none" {
					t.Errorf("verify: exit=%d %+v", code, got)
				}
			})
		}
	}
}

// A trail found shorter than the snapshot is refused even when what was cut
// lies after the checkpoint's record, which then still reads as before: a
// trail is only ever appended to.
func TestAuditStampRefusesATrailThatShrankAfterItsCheckpoint(t *testing.T) {
	address, _ := stampAuthorityTransport(t)
	configPath, trail := recordedProject(t, 1)
	const unchained = "{\"note\":\"an unchained line after the checkpoint\"}\n"
	file, err := os.OpenFile(trail, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(unchained); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(trail)
	if err != nil {
		t.Fatal(err)
	}
	stamps := filepath.Join(filepath.Dir(trail), audit.StampsName)
	beforeStampAppend = func() error { return os.Truncate(trail, info.Size()-int64(len(unchained))) }
	t.Cleanup(func() { beforeStampAppend = nil })
	stampRefusal(t, "json", configPath, address, "JPS-AUDIT-STAMP-TRAIL-MOVED", "The trail at the path is not the one whose checkpoint was stamped; nothing was written, and the token was discarded.")
	if data, err := os.ReadFile(stamps); err != nil || len(data) != 0 {
		t.Errorf("stamps written: %q %v", data, err)
	}
}

// A trail that cannot be opened again under the stamps lock is not called
// another trail: it could not be checked, and nothing is written.
func TestAuditStampSaysAnUncheckableTrailCouldNotBeChecked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not what a Windows file mode means")
	}
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not bind the superuser")
	}
	address, _ := stampAuthorityTransport(t)
	configPath, trail := recordedProject(t, 1)
	stamps := filepath.Join(filepath.Dir(trail), audit.StampsName)
	beforeStampAppend = func() error { return os.Chmod(trail, 0) }
	t.Cleanup(func() {
		beforeStampAppend = nil
		_ = os.Chmod(trail, 0o600)
	})
	stampRefusal(t, "json", configPath, address, "JPS-AUDIT-STAMP-TRAIL-UNCHECKED", "The trail at the path could not be checked; nothing was written, and the token was discarded.")
	if data, err := os.ReadFile(stamps); err != nil || len(data) != 0 {
		t.Errorf("stamps written: %q %v", data, err)
	}
}

// Whether a checkpoint is stamped already is answered without opening the
// stamps file for writing, so a re-run on a read-only stamps file still
// answers it and asks nothing.
func TestAuditStampAnswersAlreadyStampedFromAReadOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not what a Windows file mode means")
	}
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not bind the superuser")
	}
	address, _ := stampAuthorityTransport(t)
	configPath, trail := recordedProject(t, 1)
	if code, _, all := stamping(t, "--config", configPath, "--tsa", address); code != 0 {
		t.Fatal(all)
	}
	stamps := filepath.Join(filepath.Dir(trail), audit.StampsName)
	if err := os.Chmod(stamps, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stamps, 0o600) })
	requests := 0
	inner := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return inner.RoundTrip(request)
	})
	code, got, all := stamping(t, "--config", configPath, "--tsa", address)
	if code != 0 || got.Status != "already-stamped" || requests != 0 {
		t.Fatalf("re-run: exit=%d requests=%d output=%s", code, requests, all)
	}
}

// A redirect answer is refused, never followed: the digest goes to the
// authority named and nothing else. The transport counts requests per host and
// would stamp anything sent to the host the redirect names.
func TestAuditStampRefusesARedirectWithoutFollowingIt(t *testing.T) {
	const target = "http://elsewhere.invalid/collect"
	for _, status := range []int{301, 302, 303, 304, 307, 308} {
		for _, format := range []string{"json", "human"} {
			t.Run(strconv.Itoa(status)+"/"+format, func(t *testing.T) {
				address, roots := stampAuthorityTransport(t)
				stamper := http.DefaultClient.Transport
				perHost := map[string]int{}
				http.DefaultClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
					perHost[request.URL.Host]++
					if request.URL.Host == "tsa.invalid" {
						header := http.Header{"Location": []string{target}}
						return &http.Response{StatusCode: status, Header: header, Body: http.NoBody, Request: request}, nil
					}
					return stamper.RoundTrip(request)
				})
				configPath, trail := recordedProject(t, 1)
				message := "the time-stamping authority answered with a redirect, status " + strconv.Itoa(status) + "; the request was not followed and nothing was stamped. Configure the address the authority answers at. The trail and the decisions in it are as they were."
				exit, stdout, stderr := runTest(t, []string{"audit", "stamp", "--format", format, "--config", configPath, "--tsa", address}, "")
				if exit != result.ExitIO {
					t.Errorf("exit=%d stdout=%s stderr=%s", exit, stdout, stderr)
				}
				if format == "json" {
					var got result.OperationalError
					if err := json.Unmarshal([]byte(stdout), &got); err != nil {
						t.Fatalf("%v: %s", err, stdout)
					}
					if len(got.Diagnostics) != 1 || got.Diagnostics[0].Code != "JPS-AUDIT-STAMP-REDIRECT" || got.Diagnostics[0].Message != message {
						t.Errorf("diagnostic: %s", stdout)
					}
				} else if stdout != "" || stderr != "error: "+message+"\n" {
					t.Errorf("human: stdout=%q stderr=%q", stdout, stderr)
				}
				for _, text := range []string{stdout, stderr} {
					if strings.Contains(text, "elsewhere") || strings.Contains(text, "collect") || strings.Contains(text, target) {
						t.Errorf("the redirect target is in the output: %q", text)
					}
				}
				if perHost["tsa.invalid"] != 1 || len(perHost) != 1 {
					t.Errorf("requests per host: %v", perHost)
				}
				stamps := filepath.Join(filepath.Dir(trail), audit.StampsName)
				if data, err := os.ReadFile(stamps); err != nil && !os.IsNotExist(err) || len(data) != 0 {
					t.Errorf("stamps written: %q %v", data, err)
				}
				if code, got := verification(t, "--config", configPath, "--tsa-roots", roots); code != 0 || got.Coverage.Stamped.Status != "none" {
					t.Errorf("verify: exit=%d %+v", code, got)
				}
			})
		}
	}
}

// A redirect without a Location is still a refusal, and a 2xx other than 200
// is still an unreachable authority, as before.
func TestAuditStampRedirectWithoutLocationAndOtherStatuses(t *testing.T) {
	cases := []struct {
		status  int
		code    string
		message string
	}{
		{302, "JPS-AUDIT-STAMP-REDIRECT", "the time-stamping authority answered with a redirect, status 302; the request was not followed and nothing was stamped. Configure the address the authority answers at. The trail and the decisions in it are as they were."},
		{204, "JPS-AUDIT-STAMP-UNREACHABLE", "the time-stamping authority could not be asked, or did not answer: it answered HTTP 204. Nothing was written, and the trail and the decisions in it are as they were; asking again stamps the checkpoint at the trail's head at that time."},
	}
	for _, c := range cases {
		t.Run(strconv.Itoa(c.status), func(t *testing.T) {
			address, _ := stampAuthorityTransport(t)
			http.DefaultClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: c.status, Header: http.Header{}, Body: http.NoBody, Request: request}, nil
			})
			configPath, trail := recordedProject(t, 1)
			stampRefusal(t, "json", configPath, address, c.code, c.message)
			stamps := filepath.Join(filepath.Dir(trail), audit.StampsName)
			if data, err := os.ReadFile(stamps); err != nil && !os.IsNotExist(err) || len(data) != 0 {
				t.Errorf("stamps written: %q %v", data, err)
			}
		})
	}
}
