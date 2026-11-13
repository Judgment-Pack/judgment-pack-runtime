// Package gitfixture builds small git repositories for tests, from git's
// plumbing alone: blobs, trees and commits are written into the object store
// directly, so a path no file system admits (a line feed in a name, a name
// beginning with a dash) is as easy to build as any other, and nothing
// depends on a working tree. Authors, dates and configuration are fixed, so
// the same calls give the same object ids. Only tests import it.
package gitfixture

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Repo is one fixture repository.
type Repo struct {
	t   testing.TB
	Dir string
	env []string
}

// Require skips the test when no git program is installed.
func Require(t testing.TB) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; this test builds a git repository with it")
	}
}

// New creates an empty repository whose HEAD names refs/heads/main.
func New(t testing.TB) *Repo {
	t.Helper()
	Require(t)
	home := t.TempDir()
	environment := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(name), "GIT_") || name == "HOME" || name == "XDG_CONFIG_HOME" {
			continue
		}
		environment = append(environment, entry)
	}
	environment = append(environment,
		"HOME="+home,
		"XDG_CONFIG_HOME="+home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+filepath.Join(home, "gitconfig"),
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
	r := &Repo{t: t, Dir: t.TempDir(), env: environment}
	r.Git("", "-c", "init.defaultBranch=main", "init", "-q")
	r.Git("", "config", "commit.gpgSign", "false")
	r.Git("", "config", "tag.gpgSign", "false")
	return r
}

// Git runs one git command in the repository and returns its standard output
// without the final line feed; a failure fails the test.
func (r *Repo) Git(stdin string, arguments ...string) string {
	r.t.Helper()
	command := exec.Command("git", append([]string{"-C", r.Dir}, arguments...)...)
	command.Env = r.env
	command.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		r.t.Fatalf("git %q: %v: %s", arguments, err, stderr.String())
	}
	return strings.TrimSuffix(stdout.String(), "\n")
}

// Blob writes one blob and returns its id.
func (r *Repo) Blob(content string) string {
	r.t.Helper()
	return r.Git(content, "hash-object", "-w", "--stdin")
}

// Tree writes the tree holding files, each a slash-separated path and its
// content, and returns its id.
func (r *Repo) Tree(files map[string]string) string {
	r.t.Helper()
	type child struct {
		mode, kind, id string
	}
	children := map[string]child{}
	nested := map[string]map[string]string{}
	for name, content := range files {
		head, rest, deeper := strings.Cut(name, "/")
		if deeper {
			if nested[head] == nil {
				nested[head] = map[string]string{}
			}
			nested[head][rest] = content
			continue
		}
		children[head] = child{mode: "100644", kind: "blob", id: r.Blob(content)}
	}
	for name, below := range nested {
		children[name] = child{mode: "040000", kind: "tree", id: r.Tree(below)}
	}
	names := make([]string, 0, len(children))
	for name := range children {
		names = append(names, name)
	}
	sort.Strings(names)
	var input strings.Builder
	for _, name := range names {
		c := children[name]
		input.WriteString(c.mode + " " + c.kind + " " + c.id + "\t" + name + "\x00")
	}
	return r.Git(input.String(), "mktree", "-z")
}

// Entry is one raw tree entry for TreeOf.
type Entry struct {
	Mode, Kind, ID, Name string
}

// TreeOf writes one tree from raw entries, which may name objects the
// repository does not hold.
func (r *Repo) TreeOf(entries ...Entry) string {
	r.t.Helper()
	var input strings.Builder
	for _, e := range entries {
		input.WriteString(e.Mode + " " + e.Kind + " " + e.ID + "\t" + e.Name + "\x00")
	}
	return r.Git(input.String(), "mktree", "-z", "--missing")
}

// Commit writes a commit of tree with the given parents and returns its id.
func (r *Repo) Commit(tree string, parents ...string) string {
	r.t.Helper()
	arguments := []string{"commit-tree", tree, "-m", "fixture"}
	for _, parent := range parents {
		arguments = append(arguments, "-p", parent)
	}
	return r.Git("", arguments...)
}

// Snapshot writes a commit holding files, with the given parents.
func (r *Repo) Snapshot(files map[string]string, parents ...string) string {
	r.t.Helper()
	return r.Commit(r.Tree(files), parents...)
}

// Tag writes a lightweight tag. update-ref takes the full ref name, so a
// name that begins with a dash is data here.
func (r *Repo) Tag(name, object string) {
	r.t.Helper()
	r.Git("", "update-ref", "refs/tags/"+name, object)
}

// AnnotatedTag writes an annotated tag object for commit and its ref.
func (r *Repo) AnnotatedTag(name, object string) {
	r.t.Helper()
	tag := r.Git("object "+object+"\ntype commit\ntag "+name+"\ntagger fixture <fixture@example.invalid> 1767225600 +0000\n\nfixture\n", "mktag")
	r.Git("", "update-ref", "refs/tags/"+name, tag)
}

// Head points refs/heads/main, which HEAD names, at commit.
func (r *Repo) Head(commit string) {
	r.t.Helper()
	r.Git("", "update-ref", "refs/heads/main", commit)
}

// GitDir is the repository's .git directory.
func (r *Repo) GitDir() string { return filepath.Join(r.Dir, ".git") }
