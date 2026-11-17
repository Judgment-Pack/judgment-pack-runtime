//go:build unix

package emit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit/emittest"
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

// helperState runs this test binary as the emitter in mode and returns its
// wait status, after SIGKILL when kill is set.
func helperState(t *testing.T, kill bool, mode ...string) *os.ProcessState {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, append([]string{emittest.Switch}, mode...)...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if kill {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
	return cmd.ProcessState
}

// What ended the emitter decides whether the run was interrupted: the wait
// status and the first cause that ended the run's context, not the kill
// alone. exec can send the kill once the emitter has exited by itself (here,
// 3) and before Wait has noticed; that emitter is EMIT-PROCESS. A cause that
// comes after the time limit or the output bound does not replace it.
func TestTheInterruptIsDecidedByTheWaitStatusAndTheFirstCause(t *testing.T) {
	killed := helperState(t, true, "sleep-child")
	exited := helperState(t, false, "exit", "3")
	if !endedByKill(killed) || endedByKill(exited) {
		t.Fatalf("killed %v, exited %v", killed, exited)
	}
	interruptedRun := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		run, _, stop := runContext(ctx, time.Minute)
		t.Cleanup(stop)
		cancel()
		return run
	}
	limitedRun := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		run, _, stop := runContext(ctx, time.Nanosecond)
		t.Cleanup(stop)
		<-run.Done()
		cancel()
		return run
	}
	boundedRun := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		run, overflow, stop := runContext(ctx, time.Minute)
		t.Cleanup(stop)
		overflow()
		cancel()
		return run
	}
	cases := map[string]struct {
		killSent bool
		state    *os.ProcessState
		run      context.Context
		want     bool
	}{
		"killed by the interrupt":         {true, killed, interruptedRun(), true},
		"exited before the kill was sent": {true, exited, interruptedRun(), false},
		"killed at the time limit":        {true, killed, limitedRun(), false},
		"killed at the output bound":      {true, killed, boundedRun(), false},
		"no kill sent":                    {false, killed, interruptedRun(), false},
		"no wait status":                  {true, nil, interruptedRun(), false},
	}
	for name, c := range cases {
		if got := interrupted(c.killSent, c.state, c.run); got != c.want {
			t.Errorf("%s: interrupted %v, want %v", name, got, c.want)
		}
	}
}
