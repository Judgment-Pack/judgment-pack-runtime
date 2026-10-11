package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/timestamp/tsatest"
)

type lockAwareBuffer struct {
	bytes.Buffer
	locked         *bool
	wroteWhileHeld bool
}

func (b *lockAwareBuffer) Write(p []byte) (int, error) {
	b.wroteWhileHeld = b.wroteWhileHeld || *b.locked
	return b.Buffer.Write(p)
}

// recordedProject is a project that keeps a chained trail with n records.
func recordedProject(t *testing.T, n int) (string, string) {
	t.Helper()
	configPath := auditProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	for range n {
		if code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--facts", facts}, ""); code != 0 {
			t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
		}
	}
	return configPath, filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)
}

// attemptsSentence is the fixed sentence every audit verify report ends its
// doesNotEstablish list with (ADR-0048), spelled out here rather than read
// from the audit package, so a change to the text a reader mirrors verbatim
// fails a test that names it.
const attemptsSentence = "Whether any evaluation was refused at the gate, rehearsed, or failed before a disposition: the trail records decisions, not attempts, so its silence is not evidence that none were (ADR-0048)."

// endsWithAttempts reports whether a report's doesNotEstablish list holds the
// sentence on refused and rehearsed evaluations once, as its last item.
func endsWithAttempts(statements []string) bool {
	count := 0
	for _, statement := range statements {
		if statement == attemptsSentence {
			count++
		}
	}
	return count == 1 && statements[len(statements)-1] == attemptsSentence
}

// humanEndsWithAttempts is endsWithAttempts for the human report: the
// sentence is on one "NOT ESTABLISHED: " line, and that line is the last of
// them. The trail's path and the snapshot note may follow it.
func humanEndsWithAttempts(stdout string) bool {
	statements := []string{}
	for _, line := range strings.Split(stdout, "\n") {
		if statement, found := strings.CutPrefix(line, "NOT ESTABLISHED: "); found {
			statements = append(statements, statement)
		}
	}
	return len(statements) > 0 && endsWithAttempts(statements)
}

// verification runs audit verify with JSON output and decodes it.
func verification(t *testing.T, args ...string) (int, result.AuditVerification) {
	t.Helper()
	code, stdout, stderr := runTest(t, append([]string{"audit", "verify", "--format", "json"}, args...), "")
	var output result.AuditVerification
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("undecodable output %q (stderr %q): %v", stdout, stderr, err)
	}
	return code, output
}

// rewriteLine replaces one line of a trail file, by index.
func rewriteLine(t *testing.T, trail string, index int, edit func([]byte) []byte) {
	t.Helper()
	data, err := os.ReadFile(trail)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
	lines[index] = edit(lines[index])
	if err := os.WriteFile(trail, append(bytes.Join(lines, []byte("\n")), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// audit verify reads the project's trail, or one named by --trail, and exits by
// what it found: 0 when every check passed, 1 when any failed. Without a
// checkpoint it says it checked one supplied chain, and what that does not
// establish.
func TestAuditVerifyExitsByTheChain(t *testing.T) {
	configPath, trail := recordedProject(t, 3)
	code, output := verification(t, "--config", configPath)
	if code != 0 || output.OutputVersion != result.OutputVersion || output.Command != "audit verify" || output.Status != "valid" ||
		output.Scope != audit.ScopeCommittedPrefix || output.Lines != 3 || output.Coverage.Chained != 3 || output.TrailPath == "" {
		t.Fatalf("exit=%d output=%+v", code, output)
	}
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		if !output.SnapshotBetweenWrites {
			t.Fatal("on this platform the snapshot is taken between writes")
		}
	}
	if code, named := verification(t, "--trail", trail); code != 0 || named.Head == nil || *named.Head != *output.Head {
		t.Fatalf("--trail reads the same trail: exit=%d %+v", code, named.Head)
	}
	if !endsWithAttempts(output.DoesNotEstablish) {
		t.Fatalf("doesNotEstablish: %q", output.DoesNotEstablish)
	}
	code, stdout, _ := runTest(t, []string{"audit", "verify", "--config", configPath}, "")
	if code != 0 || !strings.HasPrefix(stdout, "consistent through sequence 3; 0 uncovered line(s) after it\n") || !strings.Contains(stdout, "NOT ESTABLISHED: The last chained line (sequence 3), and any lines rewritten") ||
		!strings.Contains(stdout, "signed: not checked (no --public-key)") || !humanEndsWithAttempts(stdout) {
		t.Fatalf("human output: %q", stdout)
	}

	rewriteLine(t, trail, 0, func(line []byte) []byte {
		return bytes.Replace(line, []byte(`"evidenceSupplied":false`), []byte(`"evidenceSupplied":true`), 1)
	})
	code, output = verification(t, "--config", configPath)
	if code != result.ExitInvalid || output.Status != "invalid" || len(output.Findings) != 1 ||
		output.Findings[0] != (result.AuditFinding{Name: audit.FindingPreviousMismatch, Line: 2, Detail: "previous is not the digest of the line before it"}) {
		t.Fatalf("an edited first line: exit=%d %+v", code, output.Findings)
	}
}

func TestAuditVerifyHelpDistinguishesPrefixAndDirectEvidence(t *testing.T) {
	help := (&App{}).auditVerifyCommand().Long
	for _, want := range []string{
		"unchained lines after it are uncovered and outside that prefix, and a file with no chained record has no chained history",
		"signedRecords counts individual records whose own signatures hold even after an earlier chain break",
		"stamps.trusted counts trusted stamps whose individual record checkpoints match even after an earlier chain break",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("audit verify help does not contain %q", want)
		}
	}
}

func TestAuditVerifyHeadlinesNameCommittedAndUncoveredLines(t *testing.T) {
	unchained := filepath.Join(t.TempDir(), "unchained.jsonl")
	if err := os.WriteFile(unchained, []byte("{\"line\":1}\n{\"line\":2}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest(t, []string{"audit", "verify", "--trail", unchained}, "")
	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "no chained record: 2 line(s), all uncovered\n") {
		t.Fatalf("unchained: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if code, output := verification(t, "--trail", unchained); code != 0 || output.Scope != audit.ScopeNoChainedRecord {
		t.Fatalf("unchained JSON: exit=%d scope=%q", code, output.Scope)
	}

	empty := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runTest(t, []string{"audit", "verify", "--trail", empty}, "")
	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "no chained record: 0 line(s), all uncovered\n") ||
		!strings.Contains(stdout, "NOT ESTABLISHED: That the file contains a chained history: the file is empty.\n") {
		t.Fatalf("empty: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}

	_, trail := recordedProject(t, 1)
	file, err := os.OpenFile(trail, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{\"tail\":1}\n{\"tail\":2}\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runTest(t, []string{"audit", "verify", "--trail", trail}, "")
	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "consistent through sequence 1; 2 uncovered line(s) after it\n") {
		t.Fatalf("tail: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
}

// audit checkpoint prints the canonical checkpoint document of the last
// chained record, which verify --expect reads back: the trail matches it until
// its last record is edited or it is cut short, which the chain alone does not
// see.
func TestAuditCheckpointIsWhatVerifyExpects(t *testing.T) {
	configPath, trail := recordedProject(t, 2)
	code, stdout, stderr := runTest(t, []string{"audit", "checkpoint", "--config", configPath}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	checkpoint, err := audit.ParseCheckpoint([]byte(stdout))
	if err != nil || string(audit.EncodeCheckpoint(checkpoint)) != stdout || checkpoint.Sequence != 2 {
		t.Fatalf("the human output is the checkpoint document alone: %q %v", stdout, err)
	}
	code, stdout, _ = runTest(t, []string{"audit", "checkpoint", "--config", configPath, "--format", "json"}, "")
	var report result.AuditCheckpointReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || code != 0 || report.Checkpoint != checkpoint || report.Status != "checkpointed" || report.OutputVersion != result.OutputVersion {
		t.Fatalf("exit=%d report=%+v %v", code, report, err)
	}
	held := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := os.WriteFile(held, audit.EncodeCheckpoint(checkpoint), 0o600); err != nil {
		t.Fatal(err)
	}
	code, output := verification(t, "--config", configPath, "--expect", held)
	if code != 0 || output.Scope != audit.ScopeCheckpoint || output.Held.Status != "matched" || output.Coverage.Checkpointed.Through != 2 {
		t.Fatalf("exit=%d %+v", code, output.Held)
	}
	// The checkpointed report, the one a counterparty reads, says too that the
	// trail is silent about refused and rehearsed evaluations.
	if !endsWithAttempts(output.DoesNotEstablish) {
		t.Fatalf("doesNotEstablish with --expect: %q", output.DoesNotEstablish)
	}
	if code, stdout, _ := runTest(t, []string{"audit", "verify", "--config", configPath, "--expect", held}, ""); code != 0 ||
		!humanEndsWithAttempts(stdout) {
		t.Fatalf("human output with --expect: %q", stdout)
	}

	original, err := os.ReadFile(trail)
	if err != nil {
		t.Fatal(err)
	}
	rewriteLine(t, trail, 1, func(line []byte) []byte {
		return bytes.Replace(line, []byte(`"evidenceSupplied":false`), []byte(`"evidenceSupplied":true`), 1)
	})
	if code, alone := verification(t, "--config", configPath); code != 0 || alone.Status != "valid" {
		t.Fatalf("the chain alone cannot see an edited last line: exit=%d %+v", code, alone.Findings)
	}
	if code, against := verification(t, "--config", configPath, "--expect", held); code != result.ExitInvalid || against.Findings[0].Name != audit.FindingCheckpointRecordMismatch {
		t.Fatalf("the checkpoint does: exit=%d %+v", code, against.Findings)
	}
	shortened := bytes.SplitAfter(original, []byte("\n"))[0]
	if err := os.WriteFile(trail, shortened, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, against := verification(t, "--config", configPath, "--expect", held); code != result.ExitInvalid || against.Findings[0].Name != audit.FindingCheckpointBeyondTrail {
		t.Fatalf("a trail cut short: exit=%d %+v", code, against.Findings)
	}

	// Lines after the last chained record are not covered, and a note says so.
	unchained := writeProjectFixture(t, `{"configVersion":"6","audit":{"dir":"audit","chain":false},"packs":{"intake":{"path":"packs/intake-0.1.0.pack.json"}}}`,
		map[string]string{"packs/intake-0.1.0.pack.json": evaluatorPack(t)})
	if err := os.MkdirAll(filepath.Join(filepath.Dir(unchained), "audit"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(unchained), "audit", audit.FileName), original, 0o600); err != nil {
		t.Fatal(err)
	}
	facts := writeDocument(t, "facts.json", hardFailFacts)
	if code, _, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", unchained, "--facts", facts}, ""); code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	code, stdout, stderr = runTest(t, []string{"audit", "checkpoint", "--config", unchained}, "")
	if code != 0 || stdout != string(audit.EncodeCheckpoint(checkpoint)) || !strings.Contains(stderr, "the 1 line(s) after sequence 2 are not chained") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

// A trail that fails a check, or has no chained record, is given no
// checkpoint.
func TestAuditCheckpointRefusesWhatItCannotStandBehind(t *testing.T) {
	configPath, trail := recordedProject(t, 2)
	rewriteLine(t, trail, 0, func(line []byte) []byte {
		return bytes.Replace(line, []byte(`"evidenceSupplied":false`), []byte(`"evidenceSupplied":true`), 1)
	})
	code, stdout, stderr := runTest(t, []string{"audit", "checkpoint", "--config", configPath}, "")
	if code != result.ExitInvalid || stdout != "" || !strings.Contains(stderr, "previous-mismatch at line 2") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	legacy := filepath.Join(t.TempDir(), "legacy.jsonl")
	if err := os.WriteFile(legacy, []byte(`{"recordVersion":"1","run":"r"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ = runTest(t, []string{"audit", "checkpoint", "--trail", legacy, "--format", "json"}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, `"JPS-AUDIT-CHECKPOINT-NONE"`) {
		t.Fatalf("exit=%d stdout=%q", code, stdout)
	}
}

// A torn write stops the writer; audit repair starts a new segment after it,
// the writer goes on, and verify reports segments, exit 0, never an intact
// history. A second repair has nothing to repair.
func TestAuditRepairStartsANewSegment(t *testing.T) {
	configPath, trail := recordedProject(t, 2)
	file, err := os.OpenFile(trail, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"recordVersion":"1","tr`); err != nil {
		t.Fatal(err)
	}
	file.Close()
	facts := writeDocument(t, "facts.json", hardFailFacts)
	evaluate := func() int {
		code, _, _ := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--facts", facts}, "")
		return code
	}
	if code := evaluate(); code != result.ExitIO {
		t.Fatalf("the writer refuses after a torn line: exit=%d", code)
	}
	code, stdout, _ := runTest(t, []string{"audit", "repair", "--config", configPath, "--format", "json"}, "")
	var repaired result.AuditRepair
	if err := json.Unmarshal([]byte(stdout), &repaired); err != nil || code != 0 || repaired.Status != "repaired" ||
		repaired.Discontinuity.DamagedLine != 3 || repaired.Discontinuity.Line != 4 || repaired.Discontinuity.Reason != audit.ReasonIncompleteLastLine {
		t.Fatalf("exit=%d %+v %v", code, repaired, err)
	}
	if code := evaluate(); code != 0 {
		t.Fatalf("the writer chains after the discontinuity: exit=%d", code)
	}
	file, err = os.OpenFile(trail, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{\"tail\":true}\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	code, output := verification(t, "--config", configPath)
	if code != 0 || output.Status != "segmented" || len(output.Segments) != 2 || output.Coverage.Damaged != 1 || len(output.Discontinuities) != 1 ||
		!endsWithAttempts(output.DoesNotEstablish) {
		t.Fatalf("exit=%d %+v", code, output)
	}
	code, human, _ := runTest(t, []string{"audit", "verify", "--config", configPath}, "")
	wantHeadline := "SEGMENTED: 1 discontinuity record(s); the history is not intact across them; chain-link checks passed through sequence 5, with 1 uncovered line(s) after it\n"
	if code != 0 || !strings.HasPrefix(human, wantHeadline) ||
		!humanEndsWithAttempts(human) {
		t.Fatalf("human output: %q", human)
	}
	code, stdout, _ = runTest(t, []string{"audit", "repair", "--config", configPath, "--format", "json"}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, `"JPS-AUDIT-REPAIR-NOTHING"`) {
		t.Fatalf("exit=%d %q", code, stdout)
	}
}

// The commands refuse what they cannot act on, each under its own code: two
// trails at once, standard input, a checkpoint that is not one or is of a later
// version, a project with no trail or no record yet, and a repair of a trail
// the project does not chain.
func TestAuditCommandsRefuseWhatTheyCannotActOn(t *testing.T) {
	configPath, trail := recordedProject(t, 1)
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"checkpointVersion":"1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	later := filepath.Join(t.TempDir(), "later.json")
	if err := os.WriteFile(later, []byte(`{"checkpointVersion":"2","trail":"`+strings.Repeat("a", 32)+`","sequence":1,"recordDigest":"`+audit.Digest(nil)+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	noTrail := writeProjectFixture(t, `{"configVersion":"3","packs":{}}`, nil)
	notYet := writeProjectFixture(t, `{"configVersion":"3","audit":{"dir":"audit"},"packs":{}}`, nil)
	unchained := writeProjectFixture(t, `{"configVersion":"6","audit":{"dir":"audit","chain":false},"packs":{}}`, nil)
	for name, test := range map[string]struct {
		args []string
		exit int
		code string
	}{
		"two trails":               {[]string{"audit", "verify", "--trail", trail, "--config", configPath}, result.ExitInvocation, "JPS-INVOCATION-AUDIT-TRAIL"},
		"standard input":           {[]string{"audit", "verify", "--trail", "-"}, result.ExitInvocation, "JPS-INVOCATION-STDIN"},
		"a missing trail file":     {[]string{"audit", "verify", "--trail", filepath.Join(t.TempDir(), "absent.jsonl")}, result.ExitIO, "JPS-AUDIT-TRAIL-READ"},
		"a malformed checkpoint":   {[]string{"audit", "verify", "--config", configPath, "--expect", bad}, result.ExitInvocation, "JPS-AUDIT-CHECKPOINT-INVALID"},
		"a later checkpoint":       {[]string{"audit", "verify", "--config", configPath, "--expect", later}, result.ExitUnsupported, "JPS-AUDIT-CHECKPOINT-VERSION"},
		"a missing checkpoint":     {[]string{"audit", "verify", "--config", configPath, "--expect", filepath.Join(t.TempDir(), "absent.json")}, result.ExitIO, "JPS-AUDIT-CHECKPOINT-READ"},
		"a project with no trail":  {[]string{"audit", "verify", "--config", noTrail}, result.ExitInvocation, "JPS-AUDIT-NOT-DECLARED"},
		"no record yet":            {[]string{"audit", "checkpoint", "--config", notYet}, result.ExitIO, "JPS-AUDIT-TRAIL-READ"},
		"repair with no trail":     {[]string{"audit", "repair", "--config", noTrail}, result.ExitInvocation, "JPS-AUDIT-NOT-DECLARED"},
		"repair before any record": {[]string{"audit", "repair", "--config", notYet}, result.ExitIO, "JPS-AUDIT-TRAIL-READ"},
		"repair of an unchained":   {[]string{"audit", "repair", "--config", unchained}, result.ExitInvocation, "JPS-AUDIT-REPAIR-UNCHAINED"},
	} {
		t.Run(name, func(t *testing.T) {
			code, stdout, _ := runTest(t, append(test.args, "--format", "json"), "")
			if code != test.exit || !strings.Contains(stdout, `"`+test.code+`"`) {
				t.Fatalf("exit=%d stdout=%q, want %d %s", code, stdout, test.exit, test.code)
			}
		})
	}
}

// Snapshot open failures keep the path-specific diagnostics from before
// snapshot acquisition moved into fssecure. In particular, a new project's
// absent trail is not described as a concurrency failure.
func TestAuditOpenFailuresNameTheirPaths(t *testing.T) {
	notYet := writeProjectFixture(t, `{"configVersion":"3","audit":{"dir":"audit"},"packs":{}}`, nil)
	projectTrail := filepath.Join(filepath.Dir(notYet), "audit", audit.FileName)
	code, _, stderr := runTest(t, []string{"audit", "checkpoint", "--config", notYet}, "")
	want := "error: The project's trail " + projectTrail + " does not exist yet: no record has been written.\n"
	if code != result.ExitIO || stderr != want {
		t.Fatalf("fresh project: exit=%d stderr=%q, want %q", code, stderr, want)
	}

	_, trail := recordedProject(t, 1)
	missingTrail := filepath.Join(t.TempDir(), "missing-trail.jsonl")
	if code, _, stderr := runTest(t, []string{"audit", "checkpoint", "--trail", missingTrail}, ""); code != result.ExitIO || !strings.Contains(stderr, "The trail "+missingTrail+" could not be opened as one regular file.") {
		t.Fatalf("missing trail: exit=%d stderr=%q", code, stderr)
	}
	nonRegularTrail := t.TempDir()
	if code, _, stderr := runTest(t, []string{"audit", "checkpoint", "--trail", nonRegularTrail}, ""); code != result.ExitIO || !strings.Contains(stderr, "The trail "+nonRegularTrail+" could not be opened as one regular file.") {
		t.Fatalf("non-regular trail: exit=%d stderr=%q", code, stderr)
	}
	_, public := generatedKey(t)
	missingSignatures := filepath.Join(t.TempDir(), "missing-signatures.jsonl")
	if code, _, stderr := runTest(t, []string{"audit", "verify", "--trail", trail, "--public-key", public, "--signatures", missingSignatures}, ""); code != result.ExitIO || !strings.Contains(stderr, "The signature sidecar "+missingSignatures+" could not be opened as one regular file.") {
		t.Fatalf("missing signatures: exit=%d stderr=%q", code, stderr)
	}
	tsa, err := tsatest.New(tsatest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	roots := writeDocument(t, "roots.pem", string(tsa.RootPEM()))
	missingStamps := filepath.Join(t.TempDir(), "missing-stamps.jsonl")
	if code, _, stderr := runTest(t, []string{"audit", "verify", "--trail", trail, "--tsa-roots", roots, "--stamps", missingStamps}, ""); code != result.ExitIO || !strings.Contains(stderr, "The stamps file "+missingStamps+" could not be opened as one regular file.") {
		t.Fatalf("missing stamps: exit=%d stderr=%q", code, stderr)
	}
}

// A companion opener returns its typed failure through OpenSnapshot. Rendering
// the diagnostic must wait until OpenSnapshot has released the trail lock.
func TestAuditCompanionOpenFailureIsRenderedAfterTheLock(t *testing.T) {
	_, trail := recordedProject(t, 1)
	_, public := generatedKey(t)
	missing := filepath.Join(t.TempDir(), "missing-signatures.jsonl")
	original := openAuditSnapshot
	t.Cleanup(func() { openAuditSnapshot = original })
	locked := false
	openAuditSnapshot = func(_ func() (*os.File, error), companions ...func() (*os.File, error)) ([]*os.File, []int64, bool, error) {
		locked = true
		_, err := companions[0]()
		locked = false
		return nil, nil, false, err
	}
	var stdout bytes.Buffer
	stderr := &lockAwareBuffer{locked: &locked}
	code := Run([]string{"audit", "verify", "--trail", trail, "--public-key", public, "--signatures", missing}, strings.NewReader(""), &stdout, stderr)
	if code != result.ExitIO || stderr.wroteWhileHeld || !strings.Contains(stderr.String(), missing) {
		t.Fatalf("exit=%d wrote while held=%v stderr=%q", code, stderr.wroteWhileHeld, stderr.String())
	}
}

// A repair whose own write did not complete is not repaired: the command says
// so under its own code and appends nothing.
func TestAuditRepairDoesNotRepairARepair(t *testing.T) {
	configPath, trail := recordedProject(t, 1)
	strip := func() {
		t.Helper()
		data, err := os.ReadFile(trail)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(trail, bytes.TrimSuffix(data, []byte("\n")), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	strip()
	if code, stdout, _ := runTest(t, []string{"audit", "repair", "--config", configPath, "--format", "json"}, ""); code != 0 {
		t.Fatalf("the first repair: exit=%d %q", code, stdout)
	}
	strip()
	before, err := os.ReadFile(trail)
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runTest(t, []string{"audit", "repair", "--config", configPath, "--format", "json"}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, `"JPS-AUDIT-REPAIR-DISCONTINUITY"`) {
		t.Fatalf("exit=%d %q", code, stdout)
	}
	if after, err := os.ReadFile(trail); err != nil || !bytes.Equal(after, before) {
		t.Fatal("a refused repair appends nothing")
	}
}

// shownDamage is the damaged end a caller checked and showed before asking for
// a repair (#230): eight bytes after the trail's last newline.
const shownDamage = `{"torn":`

// tornProject is a project with one record whose trail then ends in damage.
func tornProject(t *testing.T, damage string) (string, string) {
	t.Helper()
	configPath, trail := recordedProject(t, 1)
	file, err := os.OpenFile(trail, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(damage); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return configPath, trail
}

// projectTree is every directory and file under a project's directory, files
// with their bytes, so a test can say a refusal changed and created nothing.
func projectTree(t *testing.T, configPath string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(filepath.Dir(configPath), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			tree[path+string(filepath.Separator)] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		tree[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// A repair bound to the damaged bytes the caller showed (#230) repairs as one
// without the binding does: the same report, and one newline and one record
// appended after the same bytes.
func TestAuditRepairBoundToTheShownBytesRepairsAsWithout(t *testing.T) {
	digest := audit.Digest([]byte(shownDamage))
	repair := func(binding ...string) result.AuditRepair {
		t.Helper()
		configPath, trail := tornProject(t, shownDamage)
		before, err := os.ReadFile(trail)
		if err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := runTest(t, append([]string{"audit", "repair", "--config", configPath, "--format", "json"}, binding...), "")
		var repaired result.AuditRepair
		if err := json.Unmarshal([]byte(stdout), &repaired); err != nil || code != 0 || repaired.Status != "repaired" {
			t.Fatalf("exit=%d stdout=%q stderr=%q %v", code, stdout, stderr, err)
		}
		after, err := os.ReadFile(trail)
		if err != nil {
			t.Fatal(err)
		}
		appended, found := bytes.CutPrefix(after, append(before, '\n'))
		if !found || bytes.Count(appended, []byte("\n")) != 1 || !bytes.HasSuffix(appended, []byte("\n")) || !bytes.Contains(appended, []byte(`"kind":"discontinuity"`)) {
			t.Fatalf("a repair keeps the damaged bytes and appends one record: %q", after)
		}
		return repaired
	}
	unbound := repair()
	bound := repair("--damaged-bytes", fmt.Sprint(len(shownDamage)), "--damaged-digest", digest)
	if bound.Discontinuity != unbound.Discontinuity || bound.Discontinuity.Bytes != int64(len(shownDamage)) || bound.Discontinuity.Digest != digest {
		t.Fatalf("bound %+v, unbound %+v", bound.Discontinuity, unbound.Discontinuity)
	}
}

// Where the trail's damaged end is not the bytes the flags name, one more byte
// appended after the caller's check, one fewer, or none because the line was
// finished, the repair is refused in both formats under its own code, names
// the length and digest it found, and leaves the project as it was. Bound to
// what it found, where it found damaged bytes, it then repairs.
func TestAuditRepairRefusesDamageOtherThanShown(t *testing.T) {
	shownDigest := audit.Digest([]byte(shownDamage))
	for name, damage := range map[string]string{
		"a byte appended after the check": shownDamage + "x",
		"a damaged end that shrank":       shownDamage[:len(shownDamage)-1],
		"the line finished":               "",
	} {
		t.Run(name, func(t *testing.T) {
			configPath, _ := tornProject(t, damage)
			found := audit.Digest([]byte(damage))
			want := fmt.Sprintf("The damaged bytes at the trail's end are now %d bytes with digest %s, not the %d bytes with digest %s that --damaged-bytes and --damaged-digest name, so nothing was repaired and the trail is as it was; check it again with jpack audit verify and repair what it shows.",
				len(damage), found, len(shownDamage), shownDigest)
			before := projectTree(t, configPath)
			args := []string{"audit", "repair", "--config", configPath, "--damaged-bytes", fmt.Sprint(len(shownDamage)), "--damaged-digest", shownDigest}
			code, stdout, _ := runTest(t, append(args, "--format", "json"), "")
			var refused result.OperationalError
			if err := json.Unmarshal([]byte(stdout), &refused); err != nil || code != result.ExitInvalid || refused.Status != "error" || len(refused.Diagnostics) != 1 ||
				refused.Diagnostics[0].Code != "JPS-AUDIT-REPAIR-TAIL-CHANGED" || refused.Diagnostics[0].Message != want {
				t.Fatalf("json: exit=%d stdout=%q %v", code, stdout, err)
			}
			code, stdout, stderr := runTest(t, args, "")
			if code != result.ExitInvalid || stdout != "" || stderr != "error: "+want+"\n" {
				t.Fatalf("human: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			if after := projectTree(t, configPath); !reflect.DeepEqual(after, before) {
				t.Fatalf("a refused repair changed the project:\nbefore %q\nafter  %q", before, after)
			}
			if damage == "" {
				return
			}
			code, stdout, _ = runTest(t, []string{"audit", "repair", "--config", configPath, "--format", "json", "--damaged-bytes", fmt.Sprint(len(damage)), "--damaged-digest", found}, "")
			var repaired result.AuditRepair
			if err := json.Unmarshal([]byte(stdout), &repaired); err != nil || code != 0 || repaired.Discontinuity.Bytes != int64(len(damage)) || repaired.Discontinuity.Digest != found {
				t.Fatalf("bound to what was found: exit=%d stdout=%q %v", code, stdout, err)
			}
		})
	}
}

// A read of the damaged bytes that fails under the lock is the repair's write
// failure, JPS-AUDIT-WRITE, exit 4, never TAIL-CHANGED. The binding here is
// deliberately not the trail's bytes, so a check run on bytes that were never
// read would refuse as TAIL-CHANGED instead.
func TestAuditRepairThatCannotReadTheDamageSaysSo(t *testing.T) {
	configPath, _ := tornProject(t, shownDamage)
	restore := audit.StandInDamageReadForTests(errors.New("unreadable"))
	t.Cleanup(restore)
	before := projectTree(t, configPath)
	other := `{"evil":`
	code, stdout, _ := runTest(t, []string{"audit", "repair", "--config", configPath, "--format", "json",
		"--damaged-bytes", fmt.Sprint(len(other)), "--damaged-digest", audit.Digest([]byte(other))}, "")
	var refused result.OperationalError
	if err := json.Unmarshal([]byte(stdout), &refused); err != nil || code != result.ExitIO || len(refused.Diagnostics) != 1 ||
		refused.Diagnostics[0].Code != "JPS-AUDIT-WRITE" || strings.Contains(stdout, "TAIL-CHANGED") {
		t.Fatalf("exit=%d stdout=%q %v", code, stdout, err)
	}
	if after := projectTree(t, configPath); !reflect.DeepEqual(after, before) {
		t.Fatalf("a refused repair changed the project:\nbefore %q\nafter  %q", before, after)
	}
}

// The binding's flags are refused, each with its own sentence, before anything
// is opened: a configuration that does not exist and a project with no trail
// yet are never the reason given.
func TestAuditRepairRefusesAMalformedBindingFirst(t *testing.T) {
	digest := audit.Digest([]byte(shownDamage))
	const (
		pair  = "--damaged-bytes and --damaged-digest are given together or not at all: the repair is bound to the damaged bytes by their length and their digest both."
		count = "--damaged-bytes must be a whole number from 1: how many bytes follow the trail's last newline, as jpack audit verify reports them."
		form  = "--damaged-digest must be sha256: and 64 lowercase hexadecimal characters: the SHA-256 of exactly the bytes after the trail's last newline."
	)
	missing := filepath.Join(t.TempDir(), "absent", "jpack.json")
	notYet := writeProjectFixture(t, `{"configVersion":"3","audit":{"dir":"audit"},"packs":{}}`, nil)
	for name, test := range map[string]struct {
		flags []string
		want  string
	}{
		"bytes alone":                 {[]string{"--damaged-bytes=8"}, pair},
		"a digest alone":              {[]string{"--damaged-digest=" + digest}, pair},
		"a negative count":            {[]string{"--damaged-bytes=-8", "--damaged-digest=" + digest}, count},
		"a count of none":             {[]string{"--damaged-bytes=0", "--damaged-digest=" + digest}, count},
		"not a whole number":          {[]string{"--damaged-bytes=8.0", "--damaged-digest=" + digest}, count},
		"not a number":                {[]string{"--damaged-bytes=eight", "--damaged-digest=" + digest}, count},
		"a digest of no form":         {[]string{"--damaged-bytes=8", "--damaged-digest=sha256:xyz"}, form},
		"an uppercase digest":         {[]string{"--damaged-bytes=8", "--damaged-digest=sha256:" + strings.ToUpper(strings.TrimPrefix(digest, "sha256:"))}, form},
		"a digest without its prefix": {[]string{"--damaged-bytes=8", "--damaged-digest=" + strings.TrimPrefix(digest, "sha256:")}, form},
	} {
		t.Run(name, func(t *testing.T) {
			for _, configPath := range []string{missing, notYet} {
				code, stdout, _ := runTest(t, append([]string{"audit", "repair", "--config", configPath, "--format", "json"}, test.flags...), "")
				var refused result.OperationalError
				if err := json.Unmarshal([]byte(stdout), &refused); err != nil || code != result.ExitInvocation || len(refused.Diagnostics) != 1 ||
					refused.Diagnostics[0].Code != "JPS-INVOCATION-AUDIT-DAMAGED" || refused.Diagnostics[0].Message != test.want {
					t.Fatalf("exit=%d stdout=%q %v", code, stdout, err)
				}
			}
		})
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(notYet), "audit")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a refused binding created the audit directory: %v", err)
	}
}

// Human output lists what the payload lists and says how many discontinuities
// and segments it did not.
func TestAuditVerifySaysWhatItDidNotList(t *testing.T) {
	app := &App{}
	var out bytes.Buffer
	app.out = &out
	output := result.AuditVerification{TrailPath: "trail.jsonl", SnapshotBetweenWrites: true}
	output.Status = "segmented"
	output.DiscontinuitiesTotal, output.SegmentsTotal = 150, 151
	for index := range 100 {
		line := int64(2*index + 2)
		output.Discontinuities = append(output.Discontinuities, result.AuditDiscontinuity{Line: line, DamagedLine: line - 1, Reason: audit.ReasonIncompleteLastLine})
		output.Segments = append(output.Segments, result.AuditSegment{FirstLine: line, LastLine: line})
	}
	if err := app.renderAuditVerification("human", output); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.HasPrefix(text, "SEGMENTED: 150 discontinuity record(s)") || !strings.Contains(text, "segment: and 51 more, not listed") ||
		!strings.Contains(text, "discontinuity: and 50 more, not listed") {
		t.Fatalf("human output: %q", text)
	}
}

// The first signed write creates the sidecar after the reader opens the trail
// but before it takes the lock. Both project and explicit paths must capture
// that completed transaction, including its newly created companion.
func TestAuditSnapshotIncludesFirstSidecarCreation(t *testing.T) {
	skipWhereKeysCannotSign(t)
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			t.Setenv(audit.SigningKeyEnv, "")
			config, trail := recordedProject(t, 1)
			seed, public := generatedKey(t)
			t.Setenv(audit.SigningKeyEnv, seed)
			original := openAuditSnapshot
			t.Cleanup(func() { openAuditSnapshot = original })
			wrote := false
			openAuditSnapshot = func(open func() (*os.File, error), companions ...func() (*os.File, error)) ([]*os.File, []int64, bool, error) {
				return original(func() (*os.File, error) {
					file, err := open()
					if err == nil && !wrote {
						wrote = true
						evaluateOnce(t, config)
					}
					return file, err
				}, companions...)
			}
			args := []string{"--config", config, "--public-key", public, "--require-signed-through", "2"}
			if explicit {
				args[0], args[1] = "--trail", trail
			}
			code, got := verification(t, args...)
			if !wrote || code != 0 || got.Lines != 2 || got.Signatures == nil || got.Signatures.Lines != 1 || got.Coverage.SignedRecords != 1 || got.RequiredSigned.Status != "met" {
				t.Fatalf("first creation snapshot: exit=%d %+v", code, got)
			}
			if runtime.GOOS != "windows" && !got.SnapshotBetweenWrites {
				t.Fatal("snapshot should hold the shared lock")
			}
			openAuditSnapshot = original
			_, fresh := verification(t, args...)
			if fresh.Lines != got.Lines || fresh.Coverage != got.Coverage {
				t.Fatalf("fresh snapshot differs: %+v %+v", got, fresh)
			}
		})
	}
}

// openTrail returns the sizes captured with the identity-checked snapshot.
// Moving the trail aside before it is read must not cause a second, unchecked
// size acquisition from the old inode.
func TestAuditCheckpointUsesTheOpenedSnapshotSizes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("moving a trail aside while its snapshot is held is not exercised because a held file cannot be renamed over on Windows")
	}
	_, trail := recordedProject(t, 2)
	longer, err := os.ReadFile(trail)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.SplitAfter(longer, []byte("\n"))
	secondLine := lines[1]
	if err := os.WriteFile(trail, lines[0], 0o600); err != nil {
		t.Fatal(err)
	}
	aside := trail + ".aside"
	original := openAuditSnapshot
	t.Cleanup(func() { openAuditSnapshot = original })
	moved := false
	openAuditSnapshot = func(open func() (*os.File, error), companions ...func() (*os.File, error)) ([]*os.File, []int64, bool, error) {
		files, sizes, locked, err := original(open, companions...)
		if err == nil && !moved {
			closeFiles := func() {
				for _, file := range files {
					if file != nil {
						file.Close()
					}
				}
			}
			moved = true
			if err := os.Rename(trail, aside); err != nil {
				closeFiles()
				return nil, nil, false, err
			}
			file, err := os.OpenFile(aside, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				closeFiles()
				return nil, nil, false, err
			}
			if _, err := file.Write(secondLine); err != nil {
				file.Close()
				closeFiles()
				return nil, nil, false, err
			}
			if err := file.Close(); err != nil {
				closeFiles()
				return nil, nil, false, err
			}
		}
		return files, sizes, locked, err
	}
	code, stdout, stderr := runTest(t, []string{"audit", "checkpoint", "--trail", trail}, "")
	if !moved || code != 0 || !strings.Contains(stdout, `"sequence":1`) {
		t.Fatalf("moved snapshot: moved=%v exit=%d stdout=%q stderr=%q", moved, code, stdout, stderr)
	}
}
