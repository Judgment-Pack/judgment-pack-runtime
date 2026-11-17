package emit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RunOptions are the conditions of one emitter run.
type RunOptions struct {
	// Root is the repository root, the run's working directory. It must be
	// an absolute path with no symbolic link in it (see RealRoot).
	Root string
	// Timeout is the wall-clock limit; zero is DefaultTimeout.
	Timeout time.Duration
	// Environ is the host's environment, where PATH and the manifest's env
	// names are looked up; nil is os.Environ(). Nothing else of it reaches
	// the emitter.
	Environ []string
	// Override replaces a variable of the emitter's environment after it is
	// formed. It is for the conformance harness's determinism check (HOME,
	// LANG, TZ, SOURCE_DATE_EPOCH; RFC 0036 §5), never for the host.
	Override map[string]string
	// Stderr, when not nil, receives the first MaxStderrBytes of the
	// emitter's standard error after it exits (--emitter-stderr). Otherwise
	// standard error is counted and discarded.
	Stderr io.Writer
}

// RunResult is the outcome of one run: the validated response, or the
// diagnostics that refuse it.
type RunResult struct {
	// Response is the validated response, nil when the run failed.
	Response *Response
	// ResponseBytes are the bytes of a validated response, as read.
	ResponseBytes []byte
	// StderrBytes counts what the emitter wrote to standard error.
	StderrBytes int64
	// EmitterDiagnostics are those of the response, when it could be read.
	EmitterDiagnostics []EmitterDiagnostic
	// Diagnostics are the host's: EMIT-ENV-UNSET warnings, and EMIT-PROCESS
	// or EMIT-RESPONSE when the run failed.
	Diagnostics []Diagnostic
	// Started says whether the emitter's process was started. A run that
	// fails before it (command[0] not found, no HOME) is EMIT-PROCESS too;
	// the conformance harness counts only started runs as the emitter's.
	Started bool
}

// waitDelay bounds how long a run waits, once the emitter has exited or been
// killed, for standard output and standard error to close.
var waitDelay = 2 * time.Second

// Run runs one emitter on one request and validates its response: the run
// step of `jpack emit`, which the conformance harness reuses. The emitter
// gets an environment of only PATH (its absolute directories), HOME (a fresh empty directory, removed
// afterwards), LANG=C.UTF-8 and the manifest's env names that are set; the
// repository root as its working directory; and the request on standard
// input. It runs in a process group of its own, which is killed when the time
// limit passes or standard output passes MaxResponseBytes, and once the
// emitter has exited. A process the emitter starts in a session of its own
// (setsid) escapes the group; the emitter is bounded by its environment, its
// time and its output, and is not sandboxed. No value of an env variable is
// reported, only the names of those that were unset.
func Run(ctx context.Context, m *Manifest, requestVersion string, request []byte, opts RunOptions) RunResult {
	var result RunResult
	processFailure := func(location Location) RunResult {
		result.Diagnostics = append(result.Diagnostics, diagnostic("EMIT-PROCESS", location))
		return result
	}
	environ := opts.Environ
	if environ == nil {
		environ = os.Environ()
	}
	lookup := func(name string) (string, bool) {
		// The last setting wins, as a process reading its environment sees;
		// Windows names variables without regard to case ("Path").
		value, found := "", false
		for _, item := range environ {
			if key, v, ok := strings.Cut(item, "="); ok && (key == name || runtime.GOOS == "windows" && strings.EqualFold(key, name)) {
				value, found = v, true
			}
		}
		return value, found
	}
	program, ok := resolveProgram(m.Command[0], opts.Root, lookup)
	if !ok {
		return processFailure(Location{Document: DocumentManifest, Pointer: "/command/0"})
	}
	home, err := os.MkdirTemp("", "jpack-emit-home-")
	if err != nil {
		return processFailure(Location{Document: DocumentManifest, Pointer: "/command"})
	}
	defer os.RemoveAll(home)

	env := map[string]string{"HOME": home, "LANG": "C.UTF-8"}
	order := []string{"PATH", "HOME", "LANG"}
	if path, ok := lookup("PATH"); ok {
		// Only absolute directories, as command[0] is looked up: an empty or
		// relative entry would have the emitter's own lookups search the
		// repository.
		var dirs []string
		for _, dir := range filepath.SplitList(path) {
			if dir != "" && filepath.IsAbs(dir) {
				dirs = append(dirs, dir)
			}
		}
		env["PATH"] = strings.Join(dirs, string(os.PathListSeparator))
	}
	for i, name := range m.Env {
		value, ok := lookup(name)
		if !ok {
			result.Diagnostics = append(result.Diagnostics, diagnostic("EMIT-ENV-UNSET", Location{Document: DocumentManifest, Pointer: "/env/" + strconv.Itoa(i), Variable: name}))
			continue
		}
		env[name] = value
		order = append(order, name)
	}
	overrides := make([]string, 0, len(opts.Override))
	for name := range opts.Override {
		overrides = append(overrides, name)
	}
	slices.Sort(overrides)
	for _, name := range overrides {
		if _, listed := env[name]; !listed {
			order = append(order, name)
		}
		env[name] = opts.Override[name]
	}
	var childEnv []string
	for _, name := range order {
		if value, ok := env[name]; ok {
			childEnv = append(childEnv, name+"="+value)
		}
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout := &capture{limit: maxResponseBytes, overflow: cancel}
	stderr := &capture{limit: maxStderrBytes}
	cmd := exec.CommandContext(runCtx, program, m.Command[1:]...)
	cmd.Args[0] = m.Command[0]
	cmd.Dir = opts.Root
	cmd.Env = childEnv
	cmd.Stdin = bytes.NewReader(request)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = waitDelay
	ownProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd.Process) }
	if err = cmd.Start(); err == nil {
		result.Started = true
		err = cmd.Wait()
	}
	// Whatever the emitter started and left running is in its process group,
	// which is ended once the emitter has exited, whether or not it still
	// holds the emitter's output open. A child that leaves the group (setsid)
	// is out of reach.
	endProcessGroup(cmd.Process)
	result.StderrBytes = stderr.total
	if opts.Stderr != nil {
		_, _ = opts.Stderr.Write(stderr.kept())
	}
	switch {
	case stdout.overflowed():
		result.Diagnostics = append(result.Diagnostics, diagnostic("EMIT-RESPONSE", Location{Document: DocumentResponse}))
		return result
	case err != nil || runCtx.Err() != nil:
		return processFailure(Location{Document: DocumentManifest, Pointer: "/command"})
	}
	data := stdout.kept()
	response, findings := ParseResponse(data, requestVersion, request, m)
	if response != nil {
		result.EmitterDiagnostics = response.Diagnostics
	}
	if len(findings) > 0 {
		result.Diagnostics = append(result.Diagnostics, findings...)
		return result
	}
	result.Response = response
	result.ResponseBytes = data
	return result
}

// resolveProgram finds command's first item: a name with no "/" on the host's
// PATH, absolute directories only, as the emitter is given it; or a path
// relative to the repository root that, with its links resolved, stays inside
// it. It never resolves a name against the working directory.
func resolveProgram(name, root string, lookup func(string) (string, bool)) (string, bool) {
	if !strings.Contains(name, "/") {
		// Windows has no executable bit: a program there is a regular file
		// whose name ends in one of PATHEXT's extensions, which may be left
		// off the name.
		names := []string{name}
		if runtime.GOOS == "windows" {
			extensions, ok := lookup("PATHEXT")
			if !ok {
				extensions = ".COM;.EXE;.BAT;.CMD"
			}
			names = names[:0]
			for _, extension := range filepath.SplitList(extensions) {
				if extension == "" {
					continue
				}
				if strings.HasSuffix(strings.ToLower(name), strings.ToLower(extension)) {
					names = append(names, name)
				}
				names = append(names, name+extension)
			}
		}
		path, _ := lookup("PATH")
		for _, dir := range filepath.SplitList(path) {
			if dir == "" || !filepath.IsAbs(dir) {
				continue
			}
			for _, candidate := range names {
				candidate = filepath.Join(dir, candidate)
				if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && (runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0) {
					return candidate, true
				}
			}
		}
		return "", false
	}
	candidate := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(name, "./")))
	real, err := filepath.EvalSymlinks(candidate)
	if err != nil || !within(root, real) {
		return "", false
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return real, true
}

// within says whether path is root or below it; both are cleaned absolute
// paths.
func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// RealRoot returns the absolute repository root with its symbolic links
// resolved, the form RunOptions.Root and the host take.
func RealRoot(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("emit: the repository root is not a directory")
	}
	return real, nil
}

// capture keeps the first limit bytes written to it and counts the rest.
// Past the limit it calls overflow once, when set.
type capture struct {
	mu       sync.Mutex
	limit    int
	buffer   []byte
	total    int64
	overflow func()
	over     bool
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total += int64(len(p))
	if room := c.limit - len(c.buffer); room > 0 {
		c.buffer = append(c.buffer, p[:min(room, len(p))]...)
	}
	if c.total > int64(c.limit) && !c.over {
		c.over = true
		if c.overflow != nil {
			c.overflow()
		}
	}
	return len(p), nil
}

func (c *capture) kept() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buffer
}

func (c *capture) overflowed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.over
}
