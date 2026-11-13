package cli

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// failingReader yields its bytes and then fails, as a pipe whose writer
// broke does.
type failingReader struct{ data *strings.Reader }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.data.Len() == 0 {
		return 0, errors.New("the input failed")
	}
	return r.data.Read(p)
}

// failingWriter is output that cannot be written.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("the output failed") }

// An input that fails partway stops the batch with exit 4: the items before
// it are recorded and reported, and the last line names the line the input
// failed in and the run the records carry.
func TestBatchStopsAtAnInputThatFails(t *testing.T) {
	configPath := auditProject(t)
	decided := testBatchItem{facts: hardFailFacts}.line()
	input := &failingReader{data: strings.NewReader(decided + "\n" + decided + "\n" + `{"facts"`)}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json", "--batch", "-"}, input, &stdout, &stderr)
	lines := decodedBatch(t, stdout.String())
	if code != result.ExitIO || len(lines) != 3 || lines[0].ExitCode != 0 || lines[1].ExitCode != 0 ||
		lines[2].Line != 3 || lines[2].ExitCode != result.ExitIO || !strings.Contains(string(lines[2].Result), `"JPS-INPUT-READ"`) {
		t.Fatalf("exit=%d %+v stderr=%q", code, lines, stderr.String())
	}
	records := auditRecords(t, configPath)
	if len(records) != 2 {
		t.Fatalf("the items before the failure are recorded: %d", len(records))
	}
	if lines[2].Run == "" || records[0]["run"] != lines[2].Run || records[1]["run"] != lines[2].Run {
		t.Fatalf("the stop line names the records' run: %q, records %v %v", lines[2].Run, records[0]["run"], records[1]["run"])
	}
}

// Output that cannot be written fails the batch with exit 4. The records
// were appended before their results were written, as a single run's is
// before its payload, so the decisions stay recorded.
func TestBatchFailsWhenItsOutputCannotBeWritten(t *testing.T) {
	configPath := auditProject(t)
	decided := testBatchItem{facts: hardFailFacts}.line()
	var stderr bytes.Buffer
	code := Run([]string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json", "--batch", "-"},
		strings.NewReader(decided+"\n"+decided+"\n"), failingWriter{}, &stderr)
	if code != result.ExitIO {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	records := auditRecords(t, configPath)
	if len(records) != 2 {
		t.Fatalf("records: %d", len(records))
	}
	// Standard error named the run before the first record was appended,
	// so the records no line reported can be found.
	if stderr.String() != fmt.Sprintf(batchRunNote, records[0]["run"]) {
		t.Fatalf("stderr=%q, records' run %v", stderr.String(), records[0]["run"])
	}
}

// An append can fail after writing its records, as a sync or a signature
// write that fails does. The batch then reports each evaluation of that
// chunk as not confirmed, on lines naming the run, and stops. The trail holds
// those records under that run, after the ones the output reported, in input
// order, so a caller can tell which items it holds before running any again.
func TestABatchWhoseAppendFailsNamesTheRunToLookIn(t *testing.T) {
	configPath := auditProject(t)
	defer func(saved int) { batchChunkItems = saved }(batchChunkItems)
	batchChunkItems = 2
	defer func(saved func(*audit.Writer, []audit.Record) error) { batchAppend = saved }(batchAppend)
	var stdout, stderr bytes.Buffer
	appends := 0
	notedFirst := true
	batchAppend = func(writer *audit.Writer, records []audit.Record) error {
		appends++
		// Standard error names the run before any record is appended.
		if stderr.String() != fmt.Sprintf(batchRunNote, writer.Run()) {
			notedFirst = false
		}
		if err := writer.AppendAll(records); err != nil {
			return err
		}
		if appends == 2 {
			return errors.New("the sync failed")
		}
		return nil
	}
	input := ""
	for number := 1; number <= 5; number++ {
		input += testBatchItem{facts: numberedFacts(number)}.line() + "\n"
	}
	code := Run([]string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json", "--batch", "-"}, strings.NewReader(input), &stdout, &stderr)
	lines := decodedBatch(t, stdout.String())
	if code != result.ExitIO || len(lines) != 4 || appends != 2 || !notedFirst {
		t.Fatalf("exit=%d lines=%d appends=%d noted first %v stderr=%q", code, len(lines), appends, notedFirst, stderr.String())
	}
	run := lines[0].Run
	if run == "" || stderr.String() != fmt.Sprintf(batchRunNote, run) {
		t.Fatalf("run %q, stderr=%q", run, stderr.String())
	}
	for index, line := range lines {
		confirmed := index < 2
		if line.Run != run || line.Line != int64(index+1) || (line.ExitCode == 0) != confirmed {
			t.Fatalf("line %d: %+v", index+1, line)
		}
		if confirmed {
			continue
		}
		if line.ExitCode != result.ExitIO || !strings.Contains(string(line.Result), `"`+audit.FailureCode+`"`) ||
			!strings.Contains(string(line.Result), batchUnconfirmed+audit.FailureMessage) {
			t.Fatalf("line %d is not reported as not confirmed: %s", index+1, line.Result)
		}
	}
	items := recordedItems(t, configPath)
	if len(items) != 4 {
		t.Fatalf("the failed chunk's records are in the trail: %+v", items)
	}
	for index, item := range items {
		if item.run != run || item.item != index+1 {
			t.Fatalf("the run's records are the batch's items in order: %+v", items)
		}
	}
}
