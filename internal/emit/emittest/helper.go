// Package emittest is a tiny emitter for tests of the emitter host. A test
// binary becomes it when its first argument is Switch: the test's TestMain
// calls Main before the tests run. A test links its own binary into a
// temporary repository and names it, by a relative path inside that
// repository, as a manifest's command, so no test ever runs a program found
// by name.
package emittest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Switch is the first argument that makes a test binary the emitter.
const Switch = "jpack-emit-test-helper"

// Link puts the running test binary at dir/name, hard-linked where it can
// be and copied where not, and returns name: a command a manifest can give.
func Link(dir, name string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	target := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if os.Link(self, target) == nil {
		return nil
	}
	in, err := os.Open(self)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Main runs one mode of the emitter, named by args[0], and returns its exit
// code. The working directory is the repository root.
//
//	echo FILE          write FILE's bytes as the response
//	echo-env DIR       write DIR/<environment>.json's bytes as the response
//	request DIR        answer DIR/<environment>/request.json (or DIR/request.json)
//	                   holding the request as text
//	env FILE           answer FILE holding the environment, one VAR=value a line, sorted
//	cwd FILE           answer FILE holding the working directory
//	exit N             exit N with nothing written
//	stderr N           write N bytes to standard error, then an empty response
//	flood              write to standard output without end
//	sleep PIDFILE      write its pid and a child's pid to PIDFILE, then sleep
//	sleep-child        sleep
//	orphan PIDFILE     start a child with its stdio on the null device, write
//	                   the child's pid to PIDFILE, answer and exit
func Main(args []string) int {
	if len(args) == 0 {
		return 64
	}
	request, _ := io.ReadAll(os.Stdin)
	answer := func(files ...map[string]any) int {
		if files == nil {
			files = []map[string]any{}
		}
		data, err := json.Marshal(map[string]any{"emitResponseVersion": "1.0.0", "files": files, "diagnostics": []any{}})
		if err != nil {
			return 70
		}
		_, err = os.Stdout.Write(data)
		if err != nil {
			return 74
		}
		return 0
	}
	switch args[0] {
	case "echo":
		data, err := os.ReadFile(args[1])
		if err != nil {
			return 66
		}
		os.Stdout.Write(data)
		return 0
	case "echo-env":
		var parsed struct {
			Resolved []struct {
				Environment string `json:"environment"`
			} `json:"resolved"`
		}
		if json.Unmarshal(request, &parsed) != nil || len(parsed.Resolved) != 1 {
			return 65
		}
		data, err := os.ReadFile(args[1] + "/" + parsed.Resolved[0].Environment + ".json")
		if err != nil {
			return 66
		}
		os.Stdout.Write(data)
		return 0
	case "request":
		var parsed struct {
			Resolved []struct {
				Environment string `json:"environment"`
			} `json:"resolved"`
		}
		path := args[1] + "/request.json"
		if json.Unmarshal(request, &parsed) == nil && len(parsed.Resolved) == 1 {
			path = args[1] + "/" + parsed.Resolved[0].Environment + "/request.json"
		}
		return answer(map[string]any{"path": path, "text": string(request) + "\n"})
	case "env":
		environ := os.Environ()
		slices.Sort(environ)
		return answer(map[string]any{"path": args[1], "text": strings.Join(environ, "\n") + "\n"})
	case "cwd":
		dir, _ := os.Getwd()
		return answer(map[string]any{"path": args[1], "text": dir + "\n"})
	case "exit":
		code, _ := strconv.Atoi(args[1])
		return code
	case "stderr":
		n, _ := strconv.Atoi(args[1])
		os.Stderr.Write([]byte(strings.Repeat("e", n)))
		return answer()
	case "flood":
		chunk := []byte(strings.Repeat("x", 1<<16))
		for {
			if _, err := os.Stdout.Write(chunk); err != nil {
				return 74
			}
		}
	case "sleep":
		self, err := os.Executable()
		if err != nil {
			return 70
		}
		child := exec.Command(self, Switch, "sleep-child")
		if err := child.Start(); err != nil {
			return 70
		}
		pids := fmt.Sprintf("%d %d\n", os.Getpid(), child.Process.Pid)
		if err := os.WriteFile(args[1], []byte(pids), 0o600); err != nil {
			return 73
		}
		time.Sleep(time.Hour)
		return 0
	case "orphan":
		self, err := os.Executable()
		if err != nil {
			return 70
		}
		// Stdin, Stdout and Stderr left nil are the null device: the child
		// holds nothing of the emitter's open, so the run does not wait on it.
		child := exec.Command(self, Switch, "sleep-child")
		if err := child.Start(); err != nil {
			return 70
		}
		if err := os.WriteFile(args[1], []byte(strconv.Itoa(child.Process.Pid)+"\n"), 0o600); err != nil {
			return 73
		}
		return answer()
	case "sleep-child":
		time.Sleep(time.Hour)
		return 0
	}
	return 64
}
