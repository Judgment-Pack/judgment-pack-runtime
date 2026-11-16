//go:build unix

package emit

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// gone says whether pid has ended: no such process, or one that has exited
// and waits only to be reaped.
func gone(pid int) bool {
	if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		return true
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	// The state follows the command name, which is in parentheses.
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	return len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X")
}

func TestEmitKillsTheEmittersProcessGroupAtTheTimeout(t *testing.T) {
	root := testRepo(t)
	start := time.Now()
	report := host(t, root, []string{"sleep", "pids.txt"}, map[string]any{"environments": "all"}, Options{Timeout: 3 * time.Second})
	elapsed := time.Since(start)
	data, err := os.ReadFile(filepath.Join(root, "pids.txt"))
	if err != nil {
		t.Fatalf("the emitter did not record its pids before the limit: %v (%s)", err, reportJSON(t, report))
	}
	var pids []int
	for _, field := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 1 {
			t.Fatalf("pids %q", data)
		}
		pids = append(pids, pid)
	}
	t.Cleanup(func() {
		// Whatever the outcome, nothing this test started outlives it; each is
		// ended by its own pid.
		for _, pid := range pids {
			if !gone(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	if len(pids) != 2 {
		t.Fatalf("pids %q", data)
	}
	if !slices.Equal(codes(report.Diagnostics), []string{"EMIT-PROCESS@/command"}) || report.Exit() != ExitNotDone {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
	if elapsed > 3*time.Second+waitDelay+5*time.Second {
		t.Fatalf("the run took %v", elapsed)
	}
	deadline := time.Now().Add(10 * time.Second)
	for _, pid := range pids {
		for !gone(pid) {
			if time.Now().After(deadline) {
				t.Fatalf("pid %d (of %v) outlived the time limit", pid, pids)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestEmitLeavesOwnedFilesUntouchedWhenStagingFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write into a read-only directory")
	}
	root := testRepo(t)
	writeFiles(t, root, map[string]string{"gen/stale.txt": "stale\n", "gen/kept.txt": "old\n"})
	readOnly := filepath.Join(root, "gen", "ro")
	if err := os.Mkdir(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o755) })
	// gen/ro/sub cannot be made, so gen/ro/sub/new.txt cannot be staged.
	response := responseJSON(t,
		map[string]any{"path": "gen/kept.txt", "text": "new\n"},
		map[string]any{"path": "gen/ro/sub/new.txt", "text": "n\n"},
	)
	report := echoHost(t, root, response, false, nil)
	if report.Exit() != ExitNotDone || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-WRITE@gen/ro/sub/new.txt"}) || len(report.Written) != 0 || len(report.Deleted) != 0 {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
	for path, want := range map[string]string{"gen/stale.txt": "stale\n", "gen/kept.txt": "old\n"} {
		if got, _, ok := onDisk(t, root, path); !ok || got != want {
			t.Errorf("%s holds %q (present %v), want %q", path, got, ok, want)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "gen"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".jpack-emit-") {
			t.Errorf("a staged file was left behind: %s", entry.Name())
		}
	}
}

func TestEmitEndsWhatTheEmitterLeftRunning(t *testing.T) {
	root := testRepo(t)
	start := time.Now()
	report := host(t, root, []string{"orphan", "pid.txt"}, map[string]any{"environments": "all"}, Options{})
	data, err := os.ReadFile(filepath.Join(root, "pid.txt"))
	if err != nil {
		t.Fatalf("the emitter did not record its child: %v (%s)", err, reportJSON(t, report))
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		t.Fatalf("pid %q", data)
	}
	t.Cleanup(func() {
		if !gone(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	if report.Exit() != 0 || len(report.Diagnostics) != 0 {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
	if elapsed := time.Since(start); elapsed > waitDelay+5*time.Second {
		t.Fatalf("the run took %v: it waited on the child", elapsed)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !gone(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("pid %d, started by the emitter, outlived the run", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
