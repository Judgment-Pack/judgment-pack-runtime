package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// batchItem is one test item: its facts and evidence as the exact bytes a
// single run reads from a file, and its id.
type testBatchItem struct {
	id       string
	facts    string
	evidence string
}

// line is the item as a batch line.
func (item testBatchItem) line() string {
	members := []string{}
	if item.id != "" {
		members = append(members, `"id":`+strconv.Quote(item.id))
	}
	members = append(members, `"facts":`+item.facts)
	if item.evidence != "" {
		members = append(members, `"evidence":`+item.evidence)
	}
	return "{" + strings.Join(members, ",") + "}"
}

// refusedFacts is a facts document the engine refuses: a member twice.
const refusedFacts = `{"request":{},"request":{}}`

func batchLines(items []testBatchItem) string {
	lines := []string{}
	for _, item := range items {
		lines = append(lines, item.line())
	}
	return strings.Join(lines, "\n") + "\n"
}

// decodedBatch is a batch's output, one decoded line each, with each line's
// result kept as the bytes it carried.
func decodedBatch(t *testing.T, stdout string) []batchLine {
	t.Helper()
	lines := []batchLine{}
	for _, text := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
		if text == "" {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.DisallowUnknownFields()
		var line batchLine
		if err := decoder.Decode(&line); err != nil {
			t.Fatalf("undecodable batch line %q: %v", text, err)
		}
		lines = append(lines, line)
	}
	return lines
}

// strippedRecord is a record without the members a run, its moment and the
// chain assign, which two runs recording the same evaluation differ in.
func strippedRecord(record map[string]any) map[string]any {
	stripped := map[string]any{}
	for name, value := range record {
		switch name {
		case "run", "at", "trail", "sequence", "previous":
		default:
			stripped[name] = value
		}
	}
	return stripped
}

// The items the identity test runs: dispositions, every kind of refusal the
// engine gives a facts or an evidence document, and the facts byte limit.
func identityItems() []testBatchItem {
	return []testBatchItem{
		{id: "decided", facts: hardFailFacts, evidence: presentEvidence},
		{facts: hardFailFacts},
		{id: "partial", facts: `{"request":{"type":"data-access"}}`},
		{id: "duplicate member", facts: refusedFacts},
		{facts: `"not an object"`},
		{facts: `[]`},
		{facts: `null`},
		{id: "escaped", facts: `{"request":{"type":"data\u002daccess","completeness":"complete"}}`},
		{facts: hardFailFacts, evidence: `{"no-such-requirement":"present"}`},
		{facts: hardFailFacts, evidence: `{"intake-form":"maybe"}`},
		{facts: hardFailFacts, evidence: `[]`},
		{id: "over the limit", facts: `"` + strings.Repeat("a", int(carrier.HardMaxBytes)) + `"`},
	}
}

// A batch's result for each item is exactly what a single experimental
// evaluate --format json writes for that item, with the code it exits with,
// in input order and with the id echoed; the trail holds one record per
// evaluation, each the record the single run leaves but for the run, the
// moment and the chain, and the batch's records share one run id. This holds
// for a project keeping a trail, one signing it, one whose named key will not
// sign (the warning each payload carries), and a rehearsal, which records
// nothing.
func TestBatchResultsAreSingleEvaluateResults(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	t.Setenv(audit.SigningPublicKeyEnv, "")
	items := identityItems()
	batch := writeDocument(t, "batch.jsonl", batchLines(items))
	files := make([][2]string, len(items))
	for index, item := range items {
		files[index][0] = writeDocument(t, "facts.json", item.facts)
		if item.evidence != "" {
			files[index][1] = writeDocument(t, "evidence.json", item.evidence)
		}
	}
	cases := map[string]struct {
		project   func(t *testing.T) string
		extra     []string
		recording bool
		warns     bool
		reviewed  bool
	}{
		"a trail": {project: func(t *testing.T) string { return auditProject(t) }, recording: true},
		"a signed trail": {project: func(t *testing.T) string {
			skipWhereKeysCannotSign(t)
			seed, _ := generatedKey(t)
			t.Setenv(audit.SigningKeyEnv, seed)
			return signingProject(t, "")
		}, recording: true},
		"a key that will not sign": {project: func(t *testing.T) string {
			skipWhereKeysCannotSign(t)
			seed, _ := generatedKey(t)
			_, other := generatedKey(t)
			t.Setenv(audit.SigningPublicKeyEnv, strings.TrimSpace(readFile(t, other)))
			return signingProject(t, `,"signingKey":`+jsonString(t, seed))
		}, recording: true, warns: true},
		"a reviewed set": {project: func(t *testing.T) string {
			configPath := auditProject(t)
			if code, stdout, stderr := runTest(t, []string{"packs", "lock", "--config", configPath}, ""); code != 0 {
				t.Fatalf("lock: exit=%d %q %q", code, stdout, stderr)
			}
			return configPath
		}, recording: true, reviewed: true},
		"a rehearsal": {project: func(t *testing.T) string { return auditProject(t) }, extra: []string{"--rehearsal"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			batchProject, singleProject := c.project(t), c.project(t)
			// The two projects declare the same pack under the same
			// configuration, so the pack digest and every payload agree.
			args := func(configPath string) []string {
				return append([]string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json"}, c.extra...)
			}
			code, stdout, stderr := runTest(t, append(args(batchProject), "--batch", batch), "")
			if code != result.ExitInvalid || stderr != "" {
				t.Fatalf("batch: exit=%d stderr=%q", code, stderr)
			}
			if strings.Contains(stdout, `"JPS-AUDIT-UNSIGNED"`) != c.warns {
				t.Fatalf("the unsigned warning: want %v", c.warns)
			}
			if strings.Contains(stdout, `"reviewed":true,"reviewedSet":`) != c.reviewed {
				t.Fatalf("the reviewed set on the payload: want %v", c.reviewed)
			}
			lines := decodedBatch(t, stdout)
			if len(lines) != len(items) {
				t.Fatalf("%d lines for %d items", len(lines), len(items))
			}
			recorded := 0
			for index, item := range items {
				single := append(args(singleProject), "--facts", files[index][0])
				if files[index][1] != "" {
					single = append(single, "--evidence", files[index][1])
				}
				wantCode, want, wantErr := runTest(t, single, "")
				line := lines[index]
				if wantErr != "" || line.Line != int64(index+1) || line.ExitCode != wantCode || string(line.Result)+"\n" != want {
					t.Fatalf("item %d: line=%d exit=%d want %d\n got %s\nwant %s(stderr %q)", index+1, line.Line, line.ExitCode, wantCode, first(string(line.Result), 600), first(want, 600), wantErr)
				}
				if (item.id == "") != (line.ID == nil) || (line.ID != nil && *line.ID != item.id) {
					t.Fatalf("item %d: id %v, want %q", index+1, line.ID, item.id)
				}
				if wantCode == 0 {
					recorded++
				}
			}
			if !c.recording {
				noAuditTrail(t, batchProject)
				noAuditTrail(t, singleProject)
				return
			}
			batchRecords, singleRecords := auditRecords(t, batchProject), auditRecords(t, singleProject)
			if recorded == 0 || len(batchRecords) != recorded || len(singleRecords) != recorded {
				t.Fatalf("records: batch %d, single %d, want %d", len(batchRecords), len(singleRecords), recorded)
			}
			runs := map[any]bool{}
			for index := range batchRecords {
				runs[batchRecords[index]["run"]] = true
				got, _ := json.Marshal(strippedRecord(batchRecords[index]))
				want, _ := json.Marshal(strippedRecord(singleRecords[index]))
				if !bytes.Equal(got, want) {
					t.Fatalf("record %d:\n got %s\nwant %s", index+1, first(string(got), 800), first(string(want), 800))
				}
			}
			if len(runs) != 1 {
				t.Fatalf("a batch's records share one run id: %v", runs)
			}
			for _, configPath := range []string{batchProject, singleProject} {
				if code, report := verification(t, "--config", configPath); code != 0 || report.Status != "valid" {
					t.Fatalf("audit verify: exit=%d %+v", code, report.Findings)
				}
			}
		})
	}
}

// The batch's exit code: 0 when every item produced a disposition, an empty
// batch included; 1 when the input was read to its end and an item was
// refused; and the stopping line's code when the batch stopped, the item
// ceiling's 4 here, after the items before it are recorded and reported.
func TestBatchExitCodes(t *testing.T) {
	run := func(t *testing.T, configPath, input string, extra ...string) (int, []batchLine, string) {
		t.Helper()
		code, stdout, stderr := runTest(t, append([]string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json", "--batch", "-"}, extra...), input)
		return code, decodedBatch(t, stdout), stderr
	}
	decided := testBatchItem{facts: hardFailFacts, evidence: presentEvidence}
	t.Run("every item decided", func(t *testing.T) {
		configPath := auditProject(t)
		code, lines, stderr := run(t, configPath, batchLines([]testBatchItem{decided, decided, decided}))
		if code != result.ExitSuccess || stderr != "" || len(lines) != 3 || len(auditRecords(t, configPath)) != 3 {
			t.Fatalf("exit=%d lines=%d stderr=%q", code, len(lines), stderr)
		}
	})
	t.Run("an empty batch", func(t *testing.T) {
		configPath := auditProject(t)
		code, lines, stderr := run(t, configPath, "")
		if code != result.ExitSuccess || stderr != "" || len(lines) != 0 {
			t.Fatalf("exit=%d lines=%d stderr=%q", code, len(lines), stderr)
		}
		noAuditTrail(t, configPath)
	})
	t.Run("a refused item", func(t *testing.T) {
		configPath := auditProject(t)
		code, lines, _ := run(t, configPath, batchLines([]testBatchItem{decided, {facts: refusedFacts}, decided}))
		if code != result.ExitInvalid || len(lines) != 3 || lines[1].ExitCode == 0 || lines[2].ExitCode != 0 || len(auditRecords(t, configPath)) != 2 {
			t.Fatalf("exit=%d %+v", code, lines)
		}
	})
	t.Run("a final line without a newline", func(t *testing.T) {
		configPath := auditProject(t)
		code, lines, _ := run(t, configPath, decided.line()+"\n"+decided.line())
		if code != result.ExitSuccess || len(lines) != 2 || len(auditRecords(t, configPath)) != 2 {
			t.Fatalf("exit=%d %+v", code, lines)
		}
	})
	t.Run("more items than --max-items", func(t *testing.T) {
		configPath := auditProject(t)
		code, lines, _ := run(t, configPath, batchLines([]testBatchItem{decided, decided, decided, decided}), "--max-items", "2")
		if code != result.ExitIO || len(lines) != 3 || lines[0].ExitCode != 0 || lines[1].ExitCode != 0 ||
			lines[2].Line != 3 || lines[2].ExitCode != result.ExitIO || !strings.Contains(string(lines[2].Result), `"JPS-RESOURCE-BATCH-ITEMS"`) {
			t.Fatalf("exit=%d %+v", code, lines)
		}
		if records := auditRecords(t, configPath); len(records) != 2 {
			t.Fatalf("the items before the ceiling are recorded: %d", len(records))
		}
		code, lines, _ = run(t, configPath, batchLines([]testBatchItem{decided, decided}), "--max-items", "2")
		if code != result.ExitSuccess || len(lines) != 2 {
			t.Fatalf("exactly --max-items: exit=%d %+v", code, lines)
		}
	})
}

// Each line not of an item's shape is refused on its own, as an invocation,
// naming nothing from the line, and the items after it are evaluated.
func TestBatchItemShapeIsHeld(t *testing.T) {
	configPath := auditProject(t)
	decided := testBatchItem{facts: hardFailFacts}.line()
	refused := []string{
		``,
		`   `,
		`[]`,
		`"facts"`,
		`{}`,
		`{"id":"x"}`,
		`{"Facts":{}}`,
		`{"facts":{},"facts":{}}`,
		`{"facts":{},"other":1}`,
		`{"facts":{},"id":1}`,
		`{"facts":{},"id":null}`,
		`{"facts":{},"id":""}`,
		`{"facts":{},"id":"` + strings.Repeat("i", MaxBatchIDBytes+1) + `"}`,
		"{\"facts\":{},\"id\":\"\xff\"}",
		`{"facts":{}} {"facts":{}}`,
		`{"facts":{}}x`,
		`{"facts":{}`,
		`{"facts":NaN}`,
		`{"facts":{},"evidence":{},"evidence":{}}`,
	}
	input := ""
	for _, line := range refused {
		input += line + "\n" + decided + "\n"
	}
	code, stdout, _ := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json", "--batch", "-"}, input)
	lines := decodedBatch(t, stdout)
	if code != result.ExitInvalid || len(lines) != 2*len(refused) {
		t.Fatalf("exit=%d lines=%d", code, len(lines))
	}
	for index, line := range refused {
		got, next := lines[2*index], lines[2*index+1]
		var envelope result.OperationalError
		if err := json.Unmarshal(got.Result, &envelope); err != nil {
			t.Fatal(err)
		}
		if got.ExitCode != result.ExitInvocation || got.ID != nil || len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != batchItemCode ||
			(len(line) > 8 && strings.Contains(envelope.Diagnostics[0].Message, line)) || next.ExitCode != 0 {
			t.Fatalf("line %q: %+v %s, then %+v", line, got, got.Result, next)
		}
	}
	// The id at its bound is taken.
	code, stdout, _ = runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json", "--batch", "-"},
		`{"facts":{},"id":"`+strings.Repeat("i", MaxBatchIDBytes)+`"}`+"\n")
	if lines := decodedBatch(t, stdout); code != result.ExitSuccess || len(lines) != 1 || lines[0].ID == nil || len(*lines[0].ID) != MaxBatchIDBytes {
		t.Fatalf("an id at the bound: exit=%d %s", code, stdout)
	}
}

// generatedLine is a reader of one line of n bytes, {"facts":"aaa…"} and a
// newline, made as it is read rather than held.
func generatedLine(n int64) io.Reader {
	head, tail := `{"facts":"`, `"}`
	body := n - int64(len(head)+len(tail))
	return io.MultiReader(strings.NewReader(head), io.LimitReader(repeatedByte('a'), body), strings.NewReader(tail+"\n"))
}

type repeatedByte byte

func (b repeatedByte) Read(p []byte) (int, error) {
	for index := range p {
		p[index] = byte(b)
	}
	return len(p), nil
}

// A line at the bound is read and its facts are the engine's to refuse at
// their byte limit; a line one byte longer is refused unread, for its own
// line, and the batch goes on.
func TestBatchLineIsBounded(t *testing.T) {
	configPath := auditProject(t)
	decided := testBatchItem{facts: hardFailFacts}.line() + "\n"
	input := io.MultiReader(strings.NewReader(decided), generatedLine(maxBatchLineBytes), generatedLine(maxBatchLineBytes+1), strings.NewReader(decided))
	var stdout, stderr bytes.Buffer
	code := Run([]string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json", "--batch", "-"}, input, &stdout, &stderr)
	lines := decodedBatch(t, stdout.String())
	if code != result.ExitInvalid || len(lines) != 4 {
		t.Fatalf("exit=%d lines=%d stderr=%q", code, len(lines), stderr.String())
	}
	if lines[0].ExitCode != 0 || lines[3].ExitCode != 0 ||
		lines[1].ExitCode != result.ExitIO || !strings.Contains(string(lines[1].Result), `"JPS-RESOURCE-INPUT-BYTE-LIMIT"`) ||
		lines[2].ExitCode != result.ExitIO || !strings.Contains(string(lines[2].Result), `"`+batchLineCode+`"`) {
		t.Fatalf("lines: %d %s / %d %s", lines[1].ExitCode, first(string(lines[1].Result), 300), lines[2].ExitCode, first(string(lines[2].Result), 300))
	}
}

// Every invocation a batch does not take is refused before the filesystem
// is consulted, and nothing is recorded; an unreadable batch input is
// refused as an unreadable facts document is.
func TestBatchInvocationIsHeld(t *testing.T) {
	configPath := auditProject(t)
	batch := writeDocument(t, "batch.jsonl", testBatchItem{facts: hardFailFacts}.line()+"\n")
	facts := writeDocument(t, "facts.json", hardFailFacts)
	evidence := writeDocument(t, "evidence.json", presentEvidence)
	cites := writeDocument(t, "cites.json", `[]`)
	binding := writeDocument(t, "binding.json", `{"kind":"x","digest":"sha256:`+strings.Repeat("0", 64)+`"}`)
	base := []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath}
	for _, c := range []struct {
		args []string
		exit int
		code string
	}{
		{[]string{"--batch", batch, "--facts", facts}, result.ExitInvocation, batchInvocationCode},
		{[]string{"--batch", batch, "--evidence", evidence}, result.ExitInvocation, batchInvocationCode},
		{[]string{"--batch", batch, "--cites", cites}, result.ExitInvocation, batchInvocationCode},
		{[]string{"--batch", batch, "--binding", binding}, result.ExitInvocation, batchInvocationCode},
		{[]string{"--pretty", "--batch", batch}, result.ExitInvocation, batchInvocationCode},
		{[]string{"--batch", batch, "--max-items", "0"}, result.ExitInvocation, maxItemsCode},
		{[]string{"--facts", facts, "--max-items", "5"}, result.ExitInvocation, maxItemsCode},
		{[]string{"--batch", "https://example.invalid/batch.jsonl"}, result.ExitInvocation, "JPS-INVOCATION-INPUT"},
		{[]string{"--batch", filepath.Join(t.TempDir(), "absent.jsonl")}, result.ExitIO, "JPS-INPUT-READ"},
	} {
		code, stdout, stderr := runTest(t, append(append(append([]string{}, base...), "--format", "json"), c.args...), "")
		if code != c.exit || stderr != "" || !strings.Contains(stdout, `"`+c.code+`"`) {
			t.Fatalf("%v: exit=%d stdout=%q stderr=%q", c.args, code, stdout, stderr)
		}
	}
	// Human output is not JSON Lines, and is refused under its own format.
	for _, format := range [][]string{{"--format", "human"}, {}} {
		code, stdout, stderr := runTest(t, append(append(append([]string{}, base...), format...), "--batch", batch), "")
		if code != result.ExitInvocation || stdout != "" || stderr != "error: --batch writes one JSON line per item and requires --format json.\n" {
			t.Fatalf("%v: exit=%d stdout=%q stderr=%q", format, code, stdout, stderr)
		}
	}
	code, stdout, _ := runTest(t, []string{"experimental", "evaluate", "-", "--config", configPath, "--format", "json", "--batch", "-"}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-INVOCATION-STDIN"`) {
		t.Fatalf("both from standard input: exit=%d %q", code, stdout)
	}
	noAuditTrail(t, configPath)
}

// A pack the engine refuses is refused once, before any item is read, with
// the envelope and the exit code a single run gives it, and nothing is
// recorded.
func TestBatchRefusesAPackOnce(t *testing.T) {
	configPath := auditProject(t)
	pack := writeDocument(t, "pack.json", `{"specVersion":"0.2.0-draft"}`)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	wantCode, want, _ := runTest(t, []string{"experimental", "evaluate", pack, "--config", configPath, "--format", "json", "--facts", facts}, "")
	code, stdout, _ := runTest(t, []string{"experimental", "evaluate", pack, "--config", configPath, "--format", "json", "--batch", "-"}, batchLines([]testBatchItem{{facts: hardFailFacts}, {facts: `[]`}}))
	if wantCode == 0 || code != wantCode || stdout != want {
		t.Fatalf("exit=%d want %d\n got %q\nwant %q", code, wantCode, stdout, want)
	}
	noAuditTrail(t, configPath)
}

// No result is written before its record is durable. A trail whose last line
// is incomplete takes no append; every evaluation in the chunk is then
// reported with the refusal a single run gives on that trail, an item
// refused before it keeps its own refusal, the batch stops after the chunk,
// and the trail is as it was.
func TestBatchReportsNothingItCouldNotRecord(t *testing.T) {
	decided := testBatchItem{facts: hardFailFacts, evidence: presentEvidence}
	items := []testBatchItem{decided, {facts: refusedFacts}, decided, decided, decided}
	for _, chunk := range []int{256, 2} {
		t.Run(fmt.Sprintf("chunks of %d", chunk), func(t *testing.T) {
			defer func(saved int) { batchChunkItems = saved }(batchChunkItems)
			batchChunkItems = chunk
			configPath, trail := tornProject(t, shownDamage)
			before := readFile(t, trail)
			facts := writeDocument(t, "facts.json", decided.facts)
			evidence := writeDocument(t, "evidence.json", decided.evidence)
			wantCode, want, _ := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json", "--facts", facts, "--evidence", evidence}, "")
			if wantCode != result.ExitIO || !strings.Contains(want, `"`+audit.FailureCode+`"`) {
				t.Fatalf("a single run on the torn trail: exit=%d %q", wantCode, want)
			}
			code, stdout, _ := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json", "--batch", "-"}, batchLines(items))
			lines := decodedBatch(t, stdout)
			reported := min(chunk, len(items))
			if code != result.ExitIO || len(lines) != reported {
				t.Fatalf("exit=%d lines=%d, want %d", code, len(lines), reported)
			}
			for index, line := range lines {
				if index == 1 {
					if line.ExitCode == 0 || strings.Contains(string(line.Result), audit.FailureCode) {
						t.Fatalf("the refused item keeps its refusal: %+v %s", line, line.Result)
					}
					continue
				}
				if line.ExitCode != wantCode || string(line.Result)+"\n" != want {
					t.Fatalf("item %d: exit=%d %s", index+1, line.ExitCode, first(string(line.Result), 300))
				}
			}
			if readFile(t, trail) != before {
				t.Fatal("the trail is as it was")
			}
		})
	}
}

// batchHelperEnv names the arguments a helper process runs the CLI with.
const batchHelperEnv = "JPACK_TEST_BATCH_HELPER"

// TestBatchHelperProcess is not a test: it is the CLI, run in a process of
// its own by the tests below, with a chunk of the size they name.
func TestBatchHelperProcess(t *testing.T) {
	encoded := os.Getenv(batchHelperEnv)
	if encoded == "" {
		t.Skip("run only as a helper process")
	}
	var helper struct {
		Args  []string
		Chunk int
	}
	if err := json.Unmarshal([]byte(encoded), &helper); err != nil {
		os.Exit(99)
	}
	if helper.Chunk > 0 {
		batchChunkItems = helper.Chunk
	}
	os.Exit(Run(helper.Args, os.Stdin, os.Stdout, os.Stderr))
}

// batchHelper is a helper process running the CLI with args.
func batchHelper(t *testing.T, chunk int, args ...string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Skipf("this platform cannot name the running executable: %v", err)
	}
	encoded, err := json.Marshal(map[string]any{"Args": args, "Chunk": chunk})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestBatchHelperProcess$")
	command.Env = append(os.Environ(), batchHelperEnv+"="+string(encoded))
	return command
}

// numberedFacts is the decided facts with a member naming the item, which a
// record's inputs carry, so a trail says which items it holds.
func numberedFacts(number int) string {
	return strings.Replace(hardFailFacts, `{"request":`, `{"item":`+strconv.Itoa(number)+`,"request":`, 1)
}

// recordedItems is the item number of each evaluation record in a trail, in
// trail order, with its run id and sequence.
func recordedItems(t *testing.T, configPath string) []struct {
	item     int
	run      string
	sequence int64
} {
	t.Helper()
	out := []struct {
		item     int
		run      string
		sequence int64
	}{}
	for _, record := range auditRecords(t, configPath) {
		facts, _ := record["inputs"].(map[string]any)["facts"].(map[string]any)
		number, _ := facts["item"].(float64)
		sequence, _ := record["sequence"].(float64)
		out = append(out, struct {
			item     int
			run      string
			sequence int64
		}{int(number), record["run"].(string), int64(sequence)})
	}
	return out
}

// A batch ended while it waits for input, after some chunks were recorded
// and one more item was read, leaves a trail that verifies and holds exactly
// the items reported, the prefix of the batch, in order; the item read and
// not yet recorded is neither recorded nor reported.
func TestABatchEndedLeavesAPrefix(t *testing.T) {
	configPath := auditProject(t)
	command := batchHelper(t, 2, "experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json", "--batch", "-")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	ended := false
	defer func() {
		if !ended {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	reader := bufio.NewReader(stdout)
	reported := 0
	for number := 1; number <= 5; number++ {
		if _, err := io.WriteString(stdin, testBatchItem{facts: numberedFacts(number)}.line()+"\n"); err != nil {
			t.Fatal(err)
		}
		if number%2 == 0 {
			for range 2 {
				if _, err := reader.ReadString('\n'); err != nil {
					t.Fatalf("a recorded chunk is reported: %v", err)
				}
				reported++
			}
		}
	}
	// Item 5 is read and held: its chunk is not full and the input has not
	// ended. Give the process the time to read it, then end it.
	time.Sleep(200 * time.Millisecond)
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	ended = true
	rest, _ := io.ReadAll(reader)
	if reported != 4 || len(rest) != 0 {
		t.Fatalf("reported %d, then %q", reported, rest)
	}
	items := recordedItems(t, configPath)
	if len(items) != 4 {
		t.Fatalf("records %+v", items)
	}
	for index, item := range items {
		if item.item != index+1 {
			t.Fatalf("the trail holds the batch's prefix in order: %+v", items)
		}
	}
	if code, report := verification(t, "--config", configPath); code != 0 || report.Status != "valid" {
		t.Fatalf("audit verify: exit=%d %+v", code, report.Findings)
	}
}

// Two batches and single runs writing one signed trail at once interleave
// whole chunks: the trail verifies with every record signed, each batch's
// records are its items in order, and the records of each of its chunks are
// consecutive in the trail, each chunk having been appended under one hold
// of the lock.
func TestBatchesAndSingleRunsShareATrail(t *testing.T) {
	skipWhereKeysCannotSign(t)
	seed, public := generatedKey(t)
	t.Setenv(audit.SigningKeyEnv, seed)
	t.Setenv(audit.SigningPublicKeyEnv, "")
	configPath := signingProject(t, "")
	const chunk, perBatch, singles = 4, 40, 8
	batches := []string{}
	for offset := range 2 {
		items := []testBatchItem{}
		for number := 1; number <= perBatch; number++ {
			items = append(items, testBatchItem{facts: numberedFacts(1000*(offset+1) + number)})
		}
		batches = append(batches, writeDocument(t, "batch.jsonl", batchLines(items)))
	}
	facts := writeDocument(t, "facts.json", numberedFacts(1))
	commands := []*exec.Cmd{}
	outputs := []*bytes.Buffer{}
	for _, batch := range batches {
		commands = append(commands, batchHelper(t, chunk, "experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json", "--batch", batch))
	}
	for range singles {
		commands = append(commands, batchHelper(t, 0, "experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json", "--facts", facts))
	}
	var wait sync.WaitGroup
	failures := make([]error, len(commands))
	for index, command := range commands {
		output := &bytes.Buffer{}
		outputs = append(outputs, output)
		command.Stdout = output
		wait.Add(1)
		go func() {
			defer wait.Done()
			failures[index] = command.Run()
		}()
	}
	wait.Wait()
	for index, err := range failures {
		if err != nil {
			t.Fatalf("writer %d: %v %q", index, err, outputs[index].String())
		}
	}
	for index := range batches {
		lines := decodedBatch(t, outputs[index].String())
		if len(lines) != perBatch {
			t.Fatalf("batch %d reported %d lines", index, len(lines))
		}
	}
	code, report := verification(t, "--config", configPath, "--public-key", public, "--require-signed-through", strconv.Itoa(2*perBatch+singles))
	if code != 0 || report.Status != "valid" || report.Coverage.SignedRecords != 2*perBatch+singles || report.Coverage.UnsignedRecords != 0 {
		t.Fatalf("audit verify: exit=%d %+v %+v", code, report.Coverage, report.Findings)
	}
	byRun := map[string][]int{}
	sequences := map[string][]int64{}
	for _, item := range recordedItems(t, configPath) {
		if item.item >= 1000 {
			byRun[item.run] = append(byRun[item.run], item.item)
			sequences[item.run] = append(sequences[item.run], item.sequence)
		}
	}
	if len(byRun) != 2 {
		t.Fatalf("two batches, two run ids: %d", len(byRun))
	}
	for run, numbers := range byRun {
		base := numbers[0] - 1
		for index, number := range numbers {
			if number != base+index+1 {
				t.Fatalf("run %s holds its items in order: %v", run, numbers)
			}
			if index%chunk != 0 && sequences[run][index] != sequences[run][index-1]+1 {
				t.Fatalf("run %s: a chunk's records are consecutive: %v", run, sequences[run])
			}
		}
	}
}

// The failure envelopes a single run writes are what they were before the
// batch shared their composition: one evaluation error under each format, and
// the code each exits with.
func TestSingleEvaluateRefusalsAreUnchanged(t *testing.T) {
	pack := writeDocument(t, "pack.json", evaluatorPack(t))
	t.Chdir(t.TempDir())
	facts := writeDocument(t, "facts.json", `{"a":1,"a":2}`)
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--format", "json"}, "")
	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil || code != result.ExitInvocation && code != result.ExitInvalid || stderr != "" {
		t.Fatalf("json: exit=%d %q %q", code, stdout, stderr)
	}
	diagnostic := envelope["diagnostics"].([]any)[0].(map[string]any)
	evaluationError := envelope["evaluationError"].(map[string]any)
	if envelope["status"] != "error" || envelope["command"] != "experimental evaluate" || evaluationError["class"] != "malformed-input" ||
		evaluationError["phase"] != "preflight" || evaluationError["evaluatorSpecVersion"] != "0.2.0-draft" || diagnostic["code"] == "" {
		t.Fatalf("json envelope: %s", stdout)
	}
	humanCode, humanOut, humanErr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts}, "")
	want := fmt.Sprintf("error: %s\nevaluation error: malformed-input (preflight phase; %s)\n", diagnostic["message"], diagnostic["code"])
	if humanCode != code || humanOut != "" || humanErr != want {
		t.Fatalf("human: exit=%d %q %q, want %q", humanCode, humanOut, humanErr, want)
	}
	extended := writeDocument(t, "extended.json", strings.Replace(core030CLIPack, `"specVersion":"0.3.0"`, `"specVersion":"0.3.0","profile":"extended"`, 1))
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", extended, "--facts", facts}, "")
	if code != result.ExitUnsupported || stdout != "unsupported: profile-unsupported (extended)\n" || stderr != "" {
		t.Fatalf("human profile: exit=%d %q %q", code, stdout, stderr)
	}
}
