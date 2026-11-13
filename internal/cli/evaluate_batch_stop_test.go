package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

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
// failed in.
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
	if records := auditRecords(t, configPath); len(records) != 2 {
		t.Fatalf("the items before the failure are recorded: %d", len(records))
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
	if records := auditRecords(t, configPath); len(records) != 2 {
		t.Fatalf("records: %d", len(records))
	}
}
