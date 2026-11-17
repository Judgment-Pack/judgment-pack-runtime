// Package emittest is a tiny emitter for tests of the emitter host. A test
// binary becomes it when its first argument is Switch: the test's TestMain
// calls Main before the tests run. A test links its own binary into a
// temporary repository and names it, by a relative path inside that
// repository, as a manifest's command, so no test ever runs a program found
// by name.
package emittest

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
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
//	home-writer PIDFILE
//	                   as orphan, with a child that writes into HOME without
//	                   end; PIDFILE holds the emitter's pid, the child's pid
//	                   and HOME, a line each
//	home-writer-child  write into HOME without end
//
// For the conformance harness's checks:
//
//	tz FILE            answer FILE holding "TZ=" and the TZ variable: a response
//	                   that depends on the time zone
//	reference FILE N text|base64
//	                   answer FILE holding the first N characters of the first
//	                   secret reference in the request (all of it for N 0 or
//	                   past its end), as text or as base64; an empty response
//	                   when the request holds none
//	stray create|change|remove PATH
//	                   create PATH (and its directory), append to it, or remove
//	                   it, relative to the working directory, then answer with
//	                   no files
func Main(args []string) int {
	defer quietCoverage()
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
	case "home-writer":
		self, err := os.Executable()
		if err != nil {
			return 70
		}
		child := exec.Command(self, Switch, "home-writer-child")
		if err := child.Start(); err != nil {
			return 70
		}
		record := fmt.Sprintf("%d\n%d\n%s\n", os.Getpid(), child.Process.Pid, os.Getenv("HOME"))
		if err := os.WriteFile(args[1], []byte(record), 0o600); err != nil {
			return 73
		}
		return answer()
	case "home-writer-child":
		home := os.Getenv("HOME")
		for i := 0; ; i++ {
			dir := filepath.Join(home, "cache", strconv.Itoa(i%16))
			_ = os.MkdirAll(dir, 0o700)
			_ = os.WriteFile(filepath.Join(dir, "counter"), []byte(strconv.Itoa(i)), 0o600)
		}
	case "sleep-child":
		time.Sleep(time.Hour)
		return 0
	case "tz":
		return answer(map[string]any{"path": args[1], "text": "TZ=" + os.Getenv("TZ") + "\n"})
	case "reference":
		n, _ := strconv.Atoi(args[2])
		reference := firstReference(string(request))
		if reference == "" {
			return answer()
		}
		if n > 0 && n < len(reference) {
			reference = reference[:n]
		}
		if args[3] == "base64" {
			return answer(map[string]any{"path": args[1], "base64": base64.StdEncoding.EncodeToString([]byte("<" + reference + ">"))})
		}
		return answer(map[string]any{"path": args[1], "text": "ref " + reference + " end\n"})
	case "stray":
		path := filepath.FromSlash(args[2])
		var err error
		switch args[1] {
		case "create":
			if err = os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
				err = os.WriteFile(path, []byte("stray\n"), 0o644)
			}
		case "change":
			var file *os.File
			if file, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0); err == nil {
				_, err = file.WriteString("changed\n")
				err = errors.Join(err, file.Close())
			}
		case "remove":
			err = os.Remove(path)
		default:
			return 64
		}
		if err != nil {
			return 73
		}
		return answer()
	}
	return 64
}

// firstReference is the first secret reference in a request's text: from
// "secret:" to the closing quote of the JSON string that holds it.
func firstReference(request string) string {
	start := strings.Index(request, `"secret:`)
	if start < 0 {
		return ""
	}
	rest := request[start+1:]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// quietCoverage keeps a test binary built with -cover from writing to
// standard error at exit. Run as the emitter, the binary never reaches the
// end of its tests, so the coverage runtime's exit hook writes its data to
// GOCOVERDIR, which it reads at exit, and warns on standard error when that
// is not set: the host passes the emitter no GOCOVERDIR, and the warning
// would count against the standard-error bounds the tests hold. This points
// GOCOVERDIR at a directory in the run's HOME, which the host (or the
// conformance harness) makes fresh for the run and removes afterwards, and
// is set only once the mode's work is done, so no mode sees it in its
// environment. A binary built without -cover is left as it is.
func quietCoverage() {
	if testing.CoverMode() == "" || os.Getenv("GOCOVERDIR") != "" {
		return
	}
	home := os.Getenv("HOME")
	if home == "" || !filepath.IsAbs(home) {
		return
	}
	dir := filepath.Join(home, "emittest-cover")
	if os.MkdirAll(dir, 0o700) == nil {
		os.Setenv("GOCOVERDIR", dir)
	}
}
