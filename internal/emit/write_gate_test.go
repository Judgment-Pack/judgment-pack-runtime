package emit

import (
	"context"
	"slices"
	"testing"
	"time"
)

// A run whose write is refused at its start (an interrupt came) is refused
// with EMIT-INTERRUPTED at /command, and writes nothing, though its emitter
// exited 0.
func TestARunInterruptedBeforeItsWriteWritesNothing(t *testing.T) {
	root := testRepo(t)
	asked := false
	report := host(t, root, []string{"env", "gen/env.txt"}, map[string]any{"environments": "all"}, Options{BeginWrite: func() bool { asked = true; return false }})
	if !asked || report.Outcome == OutcomeWritten || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-INTERRUPTED@/command"}) {
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

// An emitter that exits non-zero is still EMIT-PROCESS, whatever the gate
// would say: the run is refused before its write is asked for.
func TestANonZeroExitIsStillAProcessFailure(t *testing.T) {
	root := testRepo(t)
	report := host(t, root, []string{"exit", "3"}, map[string]any{"environments": "all"}, Options{BeginWrite: func() bool { t.Error("the write was asked for"); return false }})
	if !slices.Equal(codes(report.Diagnostics), []string{"EMIT-PROCESS@/command"}) {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
}

// interruptAfterWait has the next runs' afterWait cancel ctx, as an
// interrupt to jpack that comes once the emitter has ended; the first run only
// when first is set.
func interruptAfterWait(t *testing.T, first bool) (context.Context, *int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	runs := 0
	saved := afterWait
	afterWait = func() {
		runs++
		if !first || runs == 1 {
			cancel()
		}
	}
	t.Cleanup(func() { afterWait = saved })
	return ctx, &runs
}

// What ended the emitter decides the code: one that exited non-zero is
// EMIT-PROCESS though an interrupt came before the outcome was decided.
func TestAnInterruptAfterANonZeroExitIsStillAProcessFailure(t *testing.T) {
	root := testRepo(t)
	ctx, runs := interruptAfterWait(t, false)
	m := mustManifest(t, manifestDoc([]string{"exit", "3"}, map[string]any{"environments": "all"}))
	report := Emit(ctx, m, nil, Inputs{Registry: testRegistry(t)}, Options{Root: root, Timeout: time.Minute})
	if *runs != 1 || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-PROCESS@/command"}) {
		t.Fatalf("runs %d, report: %s", *runs, reportJSON(t, report))
	}
}

// An emitter that exited 0 with a valid response before the interrupt keeps
// its response: the run is not refused for an interrupt that came after it.
// The interrupt then stops the next environment's run from starting, and the
// refusal is reported once.
func TestAnInterruptAfterACleanExitKeepsTheResponse(t *testing.T) {
	root := testRepo(t)
	ctx, runs := interruptAfterWait(t, true)
	m := mustManifest(t, manifestDoc([]string{"request", "gen/req"}, map[string]any{"environments": "each"}))
	report := Emit(ctx, m, nil, Inputs{Registry: testRegistry(t)}, Options{Root: root, Timeout: time.Minute})
	if *runs != 1 || len(report.Runs) != 1 || report.Runs[0].ResponseDigest == "" || report.Outcome == OutcomeWritten || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-INTERRUPTED@/command"}) {
		t.Fatalf("runs %d, report: %s", *runs, reportJSON(t, report))
	}
	if _, _, ok := onDisk(t, root, "gen/req/dev/request.json"); ok {
		t.Fatal("the interrupted run wrote a file")
	}
}

// In check mode, an interrupt that comes once the last emitter has exited 0
// with a valid response, before the scan and the comparison end, refuses the
// run with EMIT-INTERRUPTED, as the write mode's check before its write does;
// the comparison is not reported as identical or different (#475).
func TestACheckInterruptedAfterTheLastRunIsRefused(t *testing.T) {
	root := testRepo(t)
	ctx, runs := interruptAfterWait(t, true)
	m := mustManifest(t, manifestDoc([]string{"env", "gen/env.txt"}, map[string]any{"environments": "all"}))
	report := Emit(ctx, m, nil, Inputs{Registry: testRegistry(t)}, Options{Root: root, Timeout: time.Minute, Check: true})
	if *runs != 1 || report.Outcome != OutcomeNotChecked || len(report.Differences) != 0 || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-INTERRUPTED@/command"}) {
		t.Fatalf("runs %d, report: %s", *runs, reportJSON(t, report))
	}
	// The control: not interrupted, the same check reports the difference.
	report = host(t, root, []string{"env", "gen/env.txt"}, map[string]any{"environments": "all"}, Options{Check: true})
	if report.Outcome != OutcomeDifferent || len(report.Diagnostics) != 0 {
		t.Fatalf("not interrupted: %s", reportJSON(t, report))
	}
}
