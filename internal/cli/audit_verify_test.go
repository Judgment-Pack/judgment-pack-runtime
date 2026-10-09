package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

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
