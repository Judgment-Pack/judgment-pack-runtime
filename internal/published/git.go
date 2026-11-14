package published

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// gitProgram is the name looked up on PATH. A test replaces it with a name
// that resolves to nothing to exercise the missing-program refusal.
var gitProgram = "git"

// safetyOptions are given to every git process this package starts, before
// the subcommand. The repository being read is not trusted: its own
// configuration can name programs (a hook path, an fsmonitor command, a
// pager, an ssh command a lazy fetch would run) and can substitute objects
// (replace refs). None of the read-only plumbing this package runs executes
// a hook, the fsmonitor or a pager, so the first three are defence in depth,
// and so is protocol.allow=never, which the repository's own
// protocol.<name>.allow overrides; what stops a fetch is the partial-clone
// refusal and, below it, the environment (childEnvironment).
// --no-replace-objects is what keeps a replace ref from showing other bytes
// under an object id.
//
// A grafts file and a commit-graph file are local files, never cloned, and
// either can change the ancestry git reports, and so which tag published an
// identity first: the grafts file is pointed at the null device
// (childEnvironment) and the commit-graph file is not read.
//
// --no-optional-locks keeps git from taking the optional index lock, so a
// read never writes; --literal-pathspecs makes any pathspec literal, although
// no path or tag name is ever passed as an argument: tags are named by object
// id, and trees are walked from object ids read over standard input.
func safetyOptions() []string {
	return []string{
		"--no-pager",
		"--no-replace-objects",
		"--no-optional-locks",
		"--literal-pathspecs",
		"-c", "core.fsmonitor=false",
		"-c", "core.commitGraph=false",
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "protocol.allow=never",
		"-c", "credential.helper=",
		"-c", "gc.auto=0",
		"-c", "maintenance.auto=false",
	}
}

// childEnvironment is the environment every git process gets: the caller's,
// with every GIT_* variable removed, so an inherited GIT_DIR, object
// directory, replace-ref base, configuration parameter or ssh command cannot
// redirect or extend what is read, and with the variables that refuse a lazy
// fetch, a transport, a credential prompt, the optional locks and the grafts
// file added. HOME
// and PATH are kept, so the user's own global and system configuration
// (safe.directory among it, which git reads from no other scope) still
// applies; a setting there that could start a program is overridden by a
// safety option or by this environment.
//
// Two of these stop a fetch, each for the versions of git it covers. From
// git 2.45, GIT_NO_LAZY_FETCH means no missing object is fetched at all.
// Before it, GIT_ALLOW_PROTOCOL does: it overrides every protocol.allow and
// protocol.<name>.allow in every configuration, so no transport is allowed
// but one named "_". No built-in transport has that name; a remote helper
// could be given it (remote.<name>.vcs), but git looks the helper up on PATH,
// and an alias, which a repository could define, cannot have that name
// before git 2.54. An empty or a word-like name would not do: git would run
// the alias "remote-none" a repository defines for a remote whose vcs is
// "none".
func childEnvironment(parent []string) []string {
	environment := make([]string, 0, len(parent)+6)
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment,
		"GIT_ALLOW_PROTOCOL=_",
		"GIT_NO_LAZY_FETCH=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_NO_REPLACE_OBJECTS=1",
		"GIT_GRAFT_FILE="+os.DevNull,
	)
}

// git is one repository opened for reading: the resolved program, the
// repository's own git directory, named explicitly to every process after
// discovery, and the environment above.
type git struct {
	program     string
	gitDir      string
	environment []string
}

// ErrNoGit is returned when no git program is found on PATH.
var ErrNoGit = errors.New("git was not found on PATH")

// openGit resolves the git program and the git directory of the repository
// at or above dir. exec.LookPath never returns a program found through a
// relative PATH entry (exec.ErrDot), so a git placed in the repository being
// read is not the one that runs.
func openGit(dir string) (*git, error) {
	program, err := exec.LookPath(gitProgram)
	if err != nil {
		return nil, ErrNoGit
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("the repository directory could not be resolved: %w", err)
	}
	g := &git{program: program, environment: childEnvironment(os.Environ())}
	// -C is given an absolute path, which never begins with a dash.
	output, err := g.run(append([]string{"-C", absolute}, "rev-parse", "--absolute-git-dir"), nil)
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a git repository this git can read: %w", absolute, err)
	}
	gitDir := strings.TrimSuffix(string(output), "\n")
	gitDir = strings.TrimSuffix(gitDir, "\r")
	if gitDir == "" || !filepath.IsAbs(gitDir) {
		return nil, fmt.Errorf("git reported no absolute git directory for %s", absolute)
	}
	g.gitDir = gitDir
	return g, nil
}

// command builds one git invocation: the safety options, the explicit git
// directory, then the subcommand and its fixed arguments.
func (g *git) command(arguments []string) *exec.Cmd {
	full := safetyOptions()
	if g.gitDir != "" {
		full = append(full, "--git-dir="+g.gitDir)
	}
	full = append(full, arguments...)
	command := exec.Command(g.program, full...)
	command.Env = g.environment
	return command
}

// maxMessageBytes bounds what is kept of a git process's standard error.
const maxMessageBytes = 4096

// boundedBuffer keeps the first maxMessageBytes written to it. It holds its
// buffer as a field, not embedded: an embedded bytes.Buffer would lend it
// ReadFrom, which io.Copy (and so exec.Cmd) prefers to Write, and the bound
// would never apply.
type boundedBuffer struct{ data bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if room := maxMessageBytes - b.data.Len(); room > 0 {
		if len(p) > room {
			b.data.Write(p[:room])
		} else {
			b.data.Write(p)
		}
	}
	return len(p), nil
}

func (b *boundedBuffer) Len() int       { return b.data.Len() }
func (b *boundedBuffer) String() string { return b.data.String() }

// maxOutputBytes bounds what is kept of the standard output of a git
// process whose output is read whole rather than streamed: a git directory
// (rev-parse) and the partial-clone settings (config). A process that
// writes more is a failure, never read in part.
const maxOutputBytes = 64 * 1024

// limitedBuffer keeps at most limit bytes and records whether more were
// written; the rest is dropped, so the process is never blocked. Like
// boundedBuffer, it holds its buffer as a field so that Write is the only
// way in.
type limitedBuffer struct {
	data  bytes.Buffer
	limit int
	over  bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	room := b.limit - b.data.Len()
	if len(p) > room {
		b.over = true
		if room > 0 {
			b.data.Write(p[:room])
		}
		return len(p), nil
	}
	b.data.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte  { return b.data.Bytes() }
func (b *limitedBuffer) String() string { return b.data.String() }

// gitError is a git process that failed, with the first line it wrote.
type gitError struct {
	subcommand string
	message    string
}

func (e *gitError) Error() string {
	if e.message == "" {
		return "git " + e.subcommand + " failed"
	}
	return "git " + e.subcommand + " failed: " + e.message
}

func newGitError(subcommand string, stderr *boundedBuffer, err error) error {
	line, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
	if line == "" && err != nil {
		line = err.Error()
	}
	return &gitError{subcommand: subcommand, message: strings.TrimSpace(line)}
}

// run runs one git process to completion and returns its standard output,
// which must be at most maxOutputBytes: more is a failure.
func (g *git) run(arguments []string, stdin io.Reader) ([]byte, error) {
	command := g.command(arguments)
	command.Stdin = stdin
	stdout := limitedBuffer{limit: maxOutputBytes}
	var stderr boundedBuffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, newGitError(subcommandOf(arguments), &stderr, err)
	}
	if stdout.over {
		return nil, fmt.Errorf("git %s wrote more than %d bytes", subcommandOf(arguments), maxOutputBytes)
	}
	return stdout.Bytes(), nil
}

func subcommandOf(arguments []string) string {
	for index := 0; index < len(arguments); index++ {
		if arguments[index] == "-C" {
			index++
			continue
		}
		if !strings.HasPrefix(arguments[index], "-") {
			return arguments[index]
		}
	}
	return "command"
}

// stream runs one git process and hands each line of its standard output to
// each, without holding the output.
func (g *git) stream(arguments []string, stdin io.Reader, each func(line []byte) error) error {
	command := g.command(arguments)
	command.Stdin = stdin
	var stderr boundedBuffer
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return newGitError(subcommandOf(arguments), &stderr, err)
	}
	reader := bufio.NewReaderSize(stdout, 64*1024)
	var callbackErr error
	for {
		line, readErr := reader.ReadSlice('\n')
		if len(line) > 0 && callbackErr == nil {
			callbackErr = each(bytes.TrimSuffix(line, []byte("\n")))
		}
		if readErr == bufio.ErrBufferFull {
			// A line longer than the buffer is not an object id; the
			// remainder is read and dropped by the next iterations.
			if callbackErr == nil {
				callbackErr = fmt.Errorf("git %s wrote a line longer than %d bytes", subcommandOf(arguments), reader.Size())
			}
			continue
		}
		if readErr != nil {
			break
		}
	}
	waitErr := command.Wait()
	if callbackErr != nil {
		return callbackErr
	}
	if waitErr != nil {
		return newGitError(subcommandOf(arguments), &stderr, waitErr)
	}
	return nil
}

// objects is one long-running "git cat-file --batch": object names (always
// a full hexadecimal object id, optionally followed by a fixed peel suffix)
// go in on standard input, one per line, and each comes back as a header and
// its content. One process serves every read of a run.
type objects struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	writer  *bufio.Writer
	reader  *bufio.Reader
	stderr  *boundedBuffer
	closed  bool
}

func (g *git) openObjects() (*objects, error) {
	command := g.command([]string{"cat-file", "--batch"})
	stderr := &boundedBuffer{}
	command.Stderr = stderr
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, newGitError("cat-file", stderr, err)
	}
	return &objects{command: command, stdin: stdin, writer: bufio.NewWriter(stdin), reader: bufio.NewReaderSize(stdout, 64*1024), stderr: stderr}, nil
}

// object is one answer of the batch: its id, type and size, or missing.
type object struct {
	id      string
	kind    string
	size    int64
	missing bool
}

// errObjectTooLarge is an object whose content was over the caller's limit:
// it was read past and dropped, never held.
var errObjectTooLarge = errors.New("object larger than the limit")

// read asks for one object and returns its header and, when its size is
// within limit, its content. Content over the limit is consumed from the
// pipe in bounded pieces and dropped.
func (o *objects) read(name string, limit int64) (object, []byte, error) {
	if strings.ContainsAny(name, "\n\r") {
		return object{}, nil, errors.New("an object name cannot hold a line break")
	}
	if _, err := o.writer.WriteString(name + "\n"); err != nil {
		return object{}, nil, o.failure(err)
	}
	if err := o.writer.Flush(); err != nil {
		return object{}, nil, o.failure(err)
	}
	header, err := o.reader.ReadString('\n')
	if err != nil {
		return object{}, nil, o.failure(err)
	}
	header = strings.TrimSuffix(header, "\n")
	fields := strings.Split(header, " ")
	if len(fields) == 2 && (fields[1] == "missing" || fields[1] == "ambiguous") {
		return object{missing: true}, nil, nil
	}
	if len(fields) != 3 {
		return object{}, nil, fmt.Errorf("git cat-file answered %q", header)
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 {
		return object{}, nil, fmt.Errorf("git cat-file answered %q", header)
	}
	found := object{id: fields[0], kind: fields[1], size: size}
	if size > limit {
		if _, err := io.CopyN(io.Discard, o.reader, size+1); err != nil {
			return object{}, nil, o.failure(err)
		}
		return found, nil, errObjectTooLarge
	}
	content := make([]byte, size+1)
	if _, err := io.ReadFull(o.reader, content); err != nil {
		return object{}, nil, o.failure(err)
	}
	if content[size] != '\n' {
		return object{}, nil, errors.New("git cat-file wrote an object without its terminating line feed")
	}
	return found, content[:size], nil
}

func (o *objects) failure(err error) error {
	if message := strings.TrimSpace(o.stderr.String()); message != "" {
		line, _, _ := strings.Cut(message, "\n")
		return &gitError{subcommand: "cat-file", message: line}
	}
	return fmt.Errorf("git cat-file stopped: %w", err)
}

// close ends the batch: standard input is closed, which ends the process,
// and it is waited for; a process that does not end is ended by its id.
func (o *objects) close() {
	if o == nil || o.closed {
		return
	}
	o.closed = true
	_ = o.stdin.Close()
	_, _ = io.Copy(io.Discard, o.reader)
	if err := o.command.Wait(); err != nil && o.command.ProcessState == nil && o.command.Process != nil {
		_ = o.command.Process.Kill()
		_ = o.command.Wait()
	}
}
