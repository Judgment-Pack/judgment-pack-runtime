package emit

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// An interrupt while the emitter runs ends it, and the run is refused with
// EMIT-INTERRUPTED, not EMIT-PROCESS: the emitter neither failed nor ran out
// of time.
func TestAnInterruptDuringTheRunIsNotAProcessFailure(t *testing.T) {
	root := testRepo(t)
	// The emitter writes its pid and its child's, relative to the root.
	pids := filepath.Join(root, "pids.txt")
	m := mustManifest(t, manifestDoc([]string{"sleep", "pids.txt"}, map[string]any{"environments": "all"}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if data, err := os.ReadFile(pids); err == nil && strings.HasSuffix(string(data), "\n") {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancel() // as an interrupt to jpack does
	}()
	report := Emit(ctx, m, nil, Inputs{Registry: testRegistry(t)}, Options{Root: root, Timeout: time.Minute})
	if report.Outcome == OutcomeWritten || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-INTERRUPTED@/command"}) {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
}
