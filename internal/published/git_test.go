package published

import (
	"bufio"
	"bytes"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/published/gitfixture"
)

// A repository whose HEAD names a branch with no commit yet is refused, with
// nothing read: there is no HEAD to compare a tag with.
func TestAnUnbornHeadIsRefused(t *testing.T) {
	repo := gitfixture.New(t)
	_, failure := Check(defaultOptions(repo.Dir))
	if failure == nil || failure.Code != FailureHead || !strings.Contains(failure.Message, "HEAD does not name a readable commit") {
		t.Fatalf("want %s, got %#v", FailureHead, failure)
	}
	// The same repository with one commit is read.
	repo.Head(repo.Snapshot(map[string]string{"expense.json": packA}))
	if _, failure := Check(defaultOptions(repo.Dir)); failure != nil {
		t.Fatalf("a born HEAD is read: %#v", failure)
	}
}

// A line git writes over the stream's buffer is an error: its first buffer's
// worth is the last thing handed on, and the run fails. Lines within the
// buffer are handed on whole.
func TestAStreamedLineOverTheBoundIsAnError(t *testing.T) {
	repo := gitfixture.New(t)
	g, err := openGit(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	collect := func(content string) ([]string, error) {
		lines := []string{}
		err := g.stream([]string{"cat-file", "blob", repo.Blob(content)}, nil, func(line []byte) error {
			lines = append(lines, string(line))
			return nil
		})
		return lines, err
	}
	within := strings.Repeat("a", 64*1024-1)
	lines, err := collect(within + "\nshort\n")
	if err != nil || len(lines) != 2 || lines[0] != within || lines[1] != "short" {
		t.Fatalf("lines within the bound: %d lines, %v", len(lines), err)
	}
	lines, err = collect(strings.Repeat("a", 64*1024) + "\nshort\n")
	if err == nil || !strings.Contains(err.Error(), "wrote a line longer than 65536 bytes") {
		t.Fatalf("a line over the bound must be an error: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("no line after the long one is handed on: %d lines", len(lines))
	}
}

// Each cat-file record ends with a line feed after its content; a record
// without one is an error, not content.
func TestACatFileRecordWithoutItsLineFeedIsAnError(t *testing.T) {
	answer := func(stream string) (object, []byte, error) {
		o := &objects{writer: bufio.NewWriter(io.Discard), reader: bufio.NewReader(strings.NewReader(stream)), stderr: &boundedBuffer{}}
		return o.read("0123456789012345678901234567890123456789", 1024)
	}
	found, content, err := answer("0123456789012345678901234567890123456789 blob 3\nabc\n")
	if err != nil || found.kind != "blob" || found.size != 3 || !bytes.Equal(content, []byte("abc")) {
		t.Fatalf("a whole record: %#v %q %v", found, content, err)
	}
	for _, stream := range []string{
		"0123456789012345678901234567890123456789 blob 3\nabcd",
		"0123456789012345678901234567890123456789 blob 3\nabc",
	} {
		if _, content, err := answer(stream); err == nil || content != nil {
			t.Fatalf("%q: want an error, got %q", stream, content)
		}
	}
	if _, _, err := answer("0123456789012345678901234567890123456789 blob 3\nabcd"); !strings.Contains(err.Error(), "terminating line feed") {
		t.Fatalf("the error names the line feed: %v", err)
	}
}

// Two layers keep a replace ref from showing other bytes under an object id:
// the --no-replace-objects option and GIT_NO_REPLACE_OBJECTS. Each is shown
// to hold on its own, with the other removed, and the replacement is shown to
// act when both are removed.
func TestEachReplaceObjectLayerHoldsOnItsOwn(t *testing.T) {
	repo := gitfixture.New(t)
	own, replacement := repo.Blob(packB), repo.Blob(packA)
	repo.Git("", "replace", own, replacement)
	g, err := openGit(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	read := func(withOption, withEnvironment bool) string {
		t.Helper()
		options := []string{}
		for _, option := range safetyOptions() {
			if option == "--no-replace-objects" && !withOption {
				continue
			}
			options = append(options, option)
		}
		environment := []string{}
		for _, entry := range g.environment {
			if strings.HasPrefix(entry, "GIT_NO_REPLACE_OBJECTS=") && !withEnvironment {
				continue
			}
			environment = append(environment, entry)
		}
		if withOption != (len(options) == len(safetyOptions())) || withEnvironment != (len(environment) == len(g.environment)) {
			t.Fatal("the layer to remove is not where this test looks for it")
		}
		command := exec.Command(g.program, append(options, "--git-dir="+g.gitDir, "cat-file", "blob", own)...)
		command.Env = environment
		output, err := command.Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(output)
	}
	if got := read(true, false); got != packB {
		t.Fatalf("the option alone: %q", got)
	}
	if got := read(false, true); got != packB {
		t.Fatalf("the environment alone: %q", got)
	}
	if got := read(false, false); got != packA {
		t.Fatalf("with neither layer the replacement acts, or this test shows nothing: %q", got)
	}
}
