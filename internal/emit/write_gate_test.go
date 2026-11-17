package emit

import (
	"slices"
	"testing"
)

// A run whose write is refused at its start (an interrupt came) is refused
// with EMIT-PROCESS at /command, and writes nothing.
func TestARunInterruptedBeforeItsWriteWritesNothing(t *testing.T) {
	root := testRepo(t)
	asked := false
	report := host(t, root, []string{"env", "gen/env.txt"}, map[string]any{"environments": "all"}, Options{BeginWrite: func() bool { asked = true; return false }})
	if !asked || report.Outcome == OutcomeWritten || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-PROCESS@/command"}) {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
	if _, _, ok := onDisk(t, root, "gen/env.txt"); ok {
		t.Fatal("the interrupted run wrote a file")
	}
	report = host(t, root, []string{"env", "gen/env.txt"}, map[string]any{"environments": "all"}, Options{BeginWrite: func() bool { return true }})
	if report.Outcome != OutcomeWritten {
		t.Fatalf("a run whose write may start: %s", reportJSON(t, report))
	}
}
