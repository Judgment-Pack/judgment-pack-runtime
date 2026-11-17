//go:build unix

package cli

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit/emittest"
)

// TestAnInterruptEndsTheEmitterAndItsTemporaryDirectories: jpack, run as a
// process of its own, is sent SIGINT or SIGTERM while its emitter sleeps.
// The emitter runs in a process group of its own, which the signal does not
// reach, so jpack must end the group itself and remove the emitter's HOME
// and, under `emit conformance`, the harness's repository copy before it
// exits (ADR-0098: nothing the run started outlives it).
func TestAnInterruptEndsTheEmitterAndItsTemporaryDirectories(t *testing.T) {
	for _, tc := range []struct {
		name   string
		signal syscall.Signal
		conf   bool
	}{
		{"emit, SIGINT", syscall.SIGINT, false},
		{"emit, SIGTERM", syscall.SIGTERM, false},
		{"emit conformance, SIGINT", syscall.SIGINT, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The emitter writes its pids relative to its working directory:
			// the root under `emit`, the harness's copy of it under
			// `emit conformance`.
			temp := t.TempDir()
			var args []string
			var pidFiles string
			if tc.conf {
				root, manifestPath, vectors := conformanceRepo(t)
				sleepManifest(t, manifestPath, "pids.txt")
				args = []string{"emit", "conformance", "--manifest", manifestPath, "--vectors", vectors, "--root", root, "--timeout", "10m"}
				pidFiles = filepath.Join(temp, "jpack-harness-repo-*", "pids.txt")
			} else {
				registryPath, kindPath := writeRegistryFiles(t, cliRegistry(t, "14"))
				root, manifestPath := emitRepo(t)
				sleepManifest(t, manifestPath, "pids.txt")
				args = []string{"emit", "--manifest", manifestPath, "--registry", registryPath, "--kinds", kindPath, "--root", root, "--timeout", "10m"}
				pidFiles = filepath.Join(root, "pids.txt")
			}
			cmd := batchHelper(t, 0, args...)
			cmd.Env = append(cmd.Env, "TMPDIR="+temp)
			var output strings.Builder
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			var group int
			t.Cleanup(func() {
				if group > 0 {
					_ = syscall.Kill(-group, syscall.SIGKILL)
				}
				_ = cmd.Process.Kill()
			})
			deadline := time.Now().Add(60 * time.Second)
			for group == 0 {
				if found, _ := filepath.Glob(pidFiles); len(found) == 1 {
					if data, err := os.ReadFile(found[0]); err == nil && strings.HasSuffix(string(data), "\n") {
						group, _ = strconv.Atoi(strings.Fields(string(data))[0])
						break
					}
				}
				select {
				case err := <-done:
					t.Fatalf("jpack exited before the emitter started: %v: %s", err, output.String())
				default:
				}
				if time.Now().After(deadline) {
					t.Fatal("the emitter did not start")
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err := cmd.Process.Signal(tc.signal); err != nil {
				t.Fatal(err)
			}
			var waitErr error
			select {
			case waitErr = <-done:
			case <-time.After(60 * time.Second):
				t.Fatal("jpack did not exit after the signal")
			}
			if tc.conf {
				// The harness stops at the interrupt and says it could not
				// run, rather than failing the emitter on the rows the
				// interrupt broke.
				var exit *exec.ExitError
				if !errors.As(waitErr, &exit) || exit.ExitCode() != 2 || !strings.Contains(output.String(), `"outcome":"not-run"`) ||
					!strings.Contains(output.String(), `"code":"EMIT-INTERRUPTED"`) || strings.Contains(output.String(), `"status":"fail"`) {
					t.Fatalf("the interrupted harness: %v: %s", waitErr, output.String())
				}
			}
			if err := syscall.Kill(-group, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("the emitter's process group outlived jpack (kill -0: %v)", err)
			}
			for _, pattern := range []string{"jpack-emit-home-*", "jpack-harness-repo-*"} {
				if left, _ := filepath.Glob(filepath.Join(temp, pattern)); len(left) > 0 {
					t.Fatalf("left behind: %v", left)
				}
			}
		})
	}
}

// sleepManifest rewrites the manifest at path so that its emitter sleeps,
// after writing its pid and its child's to pidFile.
func sleepManifest(t *testing.T, path, pidFile string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	command := manifest["command"].([]any)
	manifest["command"] = []any{command[0], emittest.Switch, "sleep", pidFile}
	if data, err = json.Marshal(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
