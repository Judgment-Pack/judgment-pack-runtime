//go:build unix

package emit

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sleepPids waits for the sleep emitter's pid file in root and returns the
// pids it holds, each ended when the test ends if it is still running.
func sleepPids(t *testing.T, root string) []int {
	t.Helper()
	path := filepath.Join(root, "pids.txt")
	deadline := time.Now().Add(20 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && strings.HasSuffix(string(data), "\n") {
			var pids []int
			for _, field := range strings.Fields(string(data)) {
				if pid, err := strconv.Atoi(field); err == nil && pid > 1 {
					pids = append(pids, pid)
				}
			}
			t.Cleanup(func() {
				for _, pid := range pids {
					if !gone(pid) {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
				}
			})
			return pids
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// An interrupt while the emitter runs ends it, and the run is refused with
// EMIT-INTERRUPTED, not EMIT-PROCESS: the emitter neither failed nor ran out
// of time. Of two environments, the second is not run, and the refusal is
// reported once, at /command.
func TestAnInterruptDuringTheRunIsNotAProcessFailure(t *testing.T) {
	root := testRepo(t)
	// The emitter writes its pid and its child's, relative to the root.
	m := mustManifest(t, manifestDoc([]string{"sleep", "pids.txt"}, map[string]any{"environments": "each"}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	found := make(chan []int, 1)
	go func() {
		pids := sleepPids(t, root)
		cancel() // as an interrupt to jpack does
		found <- pids
	}()
	report := Emit(ctx, m, nil, Inputs{Registry: testRegistry(t)}, Options{Root: root, Timeout: time.Minute})
	if pids := <-found; len(pids) != 2 {
		t.Fatalf("pids %v, report: %s", pids, reportJSON(t, report))
	}
	if report.Outcome == OutcomeWritten || len(report.Runs) != 1 || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-INTERRUPTED@/command"}) {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
}

// An emitter killed at its time limit is EMIT-PROCESS though an interrupt
// came before the outcome was decided: the limit ended it.
func TestAnInterruptAfterTheTimeLimitIsStillAProcessFailure(t *testing.T) {
	root := testRepo(t)
	ctx, runs := interruptAfterWait(t, false)
	m := mustManifest(t, manifestDoc([]string{"sleep", "pids.txt"}, map[string]any{"environments": "all"}))
	report := Emit(ctx, m, nil, Inputs{Registry: testRegistry(t)}, Options{Root: root, Timeout: 3 * time.Second})
	if pids := sleepPids(t, root); len(pids) != 2 {
		t.Fatalf("pids %v, report: %s", pids, reportJSON(t, report))
	}
	if *runs != 1 || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-PROCESS@/command"}) {
		t.Fatalf("runs %d, report: %s", *runs, reportJSON(t, report))
	}
}
