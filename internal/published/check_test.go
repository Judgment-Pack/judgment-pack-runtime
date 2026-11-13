package published

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/published/gitfixture"
)

const (
	packA    = `{"specVersion":"0.5.0","id":"expense","version":"1.0.0","outcomes":[{"id":"approve"}]}`
	packB    = `{"specVersion":"0.5.0","id":"expense","version":"1.0.0","outcomes":[{"id":"deny"}]}`
	packNext = `{"specVersion":"0.5.0","id":"expense","version":"1.1.0","outcomes":[{"id":"deny"}]}`
	dialectA = `{"formatVersion":"3.0.0","id":"intake","version":"1.0.0","case":{"facts":[],"evidence":[]}}`
	dialectB = `{"formatVersion":"3.0.0","id":"intake","version":"1.0.0","case":{"facts":[{"name":"x"}],"evidence":[]}}`
)

func digestOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func defaultOptions(dir string) Options {
	include, _ := CompileGlob(DefaultInclude)
	return Options{Repository: dir, Selection: Selection{Include: []Glob{include}}}
}

func run(t *testing.T, options Options) *Report {
	t.Helper()
	report, failure := Check(options)
	if failure != nil {
		t.Fatalf("the check did not run: %s: %s", failure.Code, failure.Message)
	}
	return report
}

// history builds tags in order: each map is one tagged commit, the child of
// the one before; head, when not nil, is one more commit HEAD names.
func history(t *testing.T, tags []string, snapshots []map[string]string, head map[string]string) *gitfixture.Repo {
	t.Helper()
	repo := gitfixture.New(t)
	parent := ""
	for index, files := range snapshots {
		var commit string
		if parent == "" {
			commit = repo.Snapshot(files)
		} else {
			commit = repo.Snapshot(files, parent)
		}
		if index%2 == 0 {
			repo.AnnotatedTag(tags[index], commit)
		} else {
			repo.Tag(tags[index], commit)
		}
		parent = commit
	}
	if head != nil {
		parent = repo.Snapshot(head, parent)
	}
	repo.Head(parent)
	return repo
}

func onlyFinding(t *testing.T, report *Report) Finding {
	t.Helper()
	if report.Status != StatusInvalid || report.FindingCount != 1 || len(report.Findings) != 1 {
		t.Fatalf("want exactly one finding, got status %s and %#v", report.Status, report.Findings)
	}
	return report.Findings[0]
}

func TestAnUnchangedIdentityAcrossTagsPasses(t *testing.T) {
	repo := history(t, []string{"v1.0.0", "v1.1.0"}, []map[string]string{
		{"packs/expense.json": packA, "README": "one"},
		{"packs/expense.json": packA, "README": "two"},
	}, map[string]string{"packs/expense.json": packA, "README": "three"})
	report := run(t, defaultOptions(repo.Dir))
	if report.Status != StatusValid || report.FindingCount != 0 || report.Identities != 1 || report.TagsRead != 2 || report.TagsTotal != 2 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestBytesChangedUnderOneIdentityInALaterTag(t *testing.T) {
	repo := history(t, []string{"v1.0.0", "v1.1.0"}, []map[string]string{
		{"packs/expense.json": packA},
		{"packs/expense.json": packB},
	}, nil)
	finding := onlyFinding(t, run(t, defaultOptions(repo.Dir)))
	want := Finding{Code: CodeChanged, Identity: Identity{Kind: KindPack, ID: "expense", Version: "1.0.0"},
		Published: Location{Source: SourceTag, Ref: "refs/tags/v1.0.0", Path: "packs/expense.json", Digest: digestOf(packA)}}
	if finding.Code != want.Code || finding.Identity != want.Identity || finding.Published != want.Published || finding.Found == nil ||
		*finding.Found != (Location{Source: SourceTag, Ref: "refs/tags/v1.1.0", Path: "packs/expense.json", Digest: digestOf(packB)}) {
		t.Fatalf("unexpected finding: %#v found %#v", finding, finding.Found)
	}
}

func TestAChangeOnlyAtHeadNamesHead(t *testing.T) {
	repo := history(t, []string{"v1.0.0"}, []map[string]string{{"packs/expense.json": packA}},
		map[string]string{"packs/expense.json": packB})
	finding := onlyFinding(t, run(t, defaultOptions(repo.Dir)))
	if finding.Found == nil || finding.Found.Ref != HeadRef || finding.Found.Source != SourceHead || finding.Found.Digest != digestOf(packB) || finding.Published.Ref != "refs/tags/v1.0.0" {
		t.Fatalf("unexpected finding: %#v found %#v", finding, finding.Found)
	}
}

func TestAWhitespaceOnlyChangeIsAFinding(t *testing.T) {
	repo := history(t, []string{"v1"}, []map[string]string{{"expense.json": packA}},
		map[string]string{"expense.json": packA + "\n"})
	finding := onlyFinding(t, run(t, defaultOptions(repo.Dir)))
	if finding.Code != CodeChanged || finding.Found.Digest != digestOf(packA+"\n") {
		t.Fatalf("unexpected finding: %#v", finding)
	}
}

func TestAMovedFileWithTheSameBytesPasses(t *testing.T) {
	repo := history(t, []string{"v1", "v2"}, []map[string]string{
		{"packs/expense.json": packA},
		{"published/1.0.0/expense.json": packA},
	}, map[string]string{"elsewhere.json": packA})
	if report := run(t, defaultOptions(repo.Dir)); report.Status != StatusValid || report.Identities != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestAVersionBumpedWithTheChangePasses(t *testing.T) {
	repo := history(t, []string{"v1", "v2"}, []map[string]string{
		{"packs/expense.json": packA},
		{"packs/expense.json": packNext},
	}, nil)
	if report := run(t, defaultOptions(repo.Dir)); report.Status != StatusValid || report.Identities != 2 {
		t.Fatalf("unexpected report: %#v", report)
	}
	// HEAD never publishes: a version bumped only at HEAD is no published
	// identity and enters no manifest until a tag holds it.
	unreleased := history(t, []string{"v1"}, []map[string]string{{"packs/expense.json": packA}}, map[string]string{"packs/expense.json": packNext})
	report := run(t, defaultOptions(unreleased.Dir))
	if report.Status != StatusValid || report.Identities != 1 || len(report.Manifest.Documents) != 1 || report.Manifest.Documents[0].Version != "1.0.0" {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestADialectDocumentIsCheckedLikeAPack(t *testing.T) {
	moved := history(t, []string{"v1", "v2"}, []map[string]string{
		{"dialects/intake.json": dialectA},
		{"intake.json": dialectA},
	}, nil)
	if report := run(t, defaultOptions(moved.Dir)); report.Status != StatusValid || report.Identities != 1 || report.Documents != 1 {
		t.Fatalf("a moved dialect with the same bytes must pass: %#v", report)
	}
	changed := history(t, []string{"v1", "v2"}, []map[string]string{
		{"dialects/intake.json": dialectA},
		{"dialects/intake.json": dialectB},
	}, nil)
	finding := onlyFinding(t, run(t, defaultOptions(changed.Dir)))
	if finding.Kind != KindDialect || finding.ID != "intake" || finding.Published.Digest != digestOf(dialectA) || finding.Found.Digest != digestOf(dialectB) {
		t.Fatalf("unexpected finding: %#v", finding)
	}
}

// A pack and a dialect document are two identities even with one id and
// version: the kind is part of what is published.
func TestThePackAndTheDialectOfOneIdAreTwoIdentities(t *testing.T) {
	dialect := strings.Replace(dialectA, `"intake"`, `"expense"`, 1)
	repo := history(t, []string{"v1"}, []map[string]string{{"a.json": packA, "b.json": dialect}}, nil)
	if report := run(t, defaultOptions(repo.Dir)); report.Status != StatusValid || report.Identities != 2 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestTwoDocumentsWithOneIdentityInOneTagAreAFinding(t *testing.T) {
	repo := history(t, []string{"v1"}, []map[string]string{{"a/expense.json": packA, "b/expense.json": packB}}, nil)
	finding := onlyFinding(t, run(t, defaultOptions(repo.Dir)))
	if finding.Code != CodeDuplicate || finding.Published.Ref != "refs/tags/v1" || finding.Found.Ref != "refs/tags/v1" ||
		finding.Published.Path != "a/expense.json" || finding.Found.Path != "b/expense.json" {
		t.Fatalf("unexpected finding: %#v found %#v", finding, finding.Found)
	}
}

func TestAnUnparseableDocumentIsReportedAndHidesNoFinding(t *testing.T) {
	repo := history(t, []string{"v1", "v2"}, []map[string]string{
		{"packs/expense.json": packA, "packs/broken.json": `{"specVersion": "0.5.0", "id": `},
		{"packs/expense.json": packB},
	}, nil)
	report := run(t, defaultOptions(repo.Dir))
	onlyFinding(t, report)
	if report.UnreadableCount != 1 || report.Unreadable[0].Ref != "refs/tags/v1" || report.Unreadable[0].Path != "packs/broken.json" ||
		!strings.Contains(report.Unreadable[0].Reason, "not acceptable JSON") || report.Unreadable[0].Digest != digestOf(`{"specVersion": "0.5.0", "id": `) {
		t.Fatalf("the unreadable document must be reported at its tag: %#v", report.Unreadable)
	}

	alone := history(t, []string{"v1"}, []map[string]string{{"packs/broken.json": "{"}}, nil)
	if report := run(t, defaultOptions(alone.Dir)); report.Status != StatusIncomplete || report.UnreadableCount != 1 {
		t.Fatalf("an unreadable document alone leaves the result incomplete, never valid: %#v", report)
	}
}

func TestAPackWithoutAReadableIdentityIsUnreadable(t *testing.T) {
	repo := history(t, []string{"v1"}, []map[string]string{{"p.json": `{"specVersion":"0.5.0","id":7,"version":"1.0.0"}`, "jpack.json": `{"configVersion":"8","packs":{}}`}}, nil)
	report := run(t, defaultOptions(repo.Dir))
	if report.Status != StatusIncomplete || report.UnreadableCount != 1 || report.OtherFiles != 1 || !strings.Contains(report.Unreadable[0].Reason, "identity cannot be read") {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestOptionLikeTagNamesAndOddPathsAreData(t *testing.T) {
	repo := gitfixture.New(t)
	first := repo.Commit(repo.TreeOf(
		gitfixture.Entry{Mode: "100644", Kind: "blob", ID: repo.Blob(packA), Name: "-x.json"},
		gitfixture.Entry{Mode: "100644", Kind: "blob", ID: repo.Blob(dialectA), Name: "new\nline.json"},
	))
	second := repo.Commit(repo.TreeOf(
		gitfixture.Entry{Mode: "100644", Kind: "blob", ID: repo.Blob(packB), Name: "-x.json"},
		gitfixture.Entry{Mode: "100644", Kind: "blob", ID: repo.Blob(dialectB), Name: "new\nline.json"},
	), first)
	repo.Tag("--upload-pack=touch${IFS}upload-marker", first)
	repo.Tag("-x", second)
	repo.Head(second)
	report := run(t, defaultOptions(repo.Dir))
	if report.Status != StatusInvalid || report.FindingCount != 2 {
		t.Fatalf("unexpected report: %#v", report)
	}
	for _, finding := range report.Findings {
		if finding.Published.Ref != "refs/tags/--upload-pack=touch${IFS}upload-marker" || finding.Found.Ref != "refs/tags/-x" {
			t.Fatalf("tag names must be reported as given: %#v %#v", finding.Published, finding.Found)
		}
	}
	paths := []string{report.Findings[0].Found.Path, report.Findings[1].Found.Path}
	sort.Strings(paths)
	if paths[0] != "-x.json" || paths[1] != "new\nline.json" {
		t.Fatalf("paths must be reported as given: %q", paths)
	}
	if _, err := os.Stat(filepath.Join(repo.Dir, "upload-marker")); err == nil {
		t.Fatal("a tag name was run as an option")
	}
}

// git reads a tree entry's mode by its file-type bits, not its spelling: a
// directory written 040000 or 40755 is a directory to git as 40000 is, so it
// is entered, and its spelling, which git never writes, is reported.
func TestADirectoryIsEnteredHoweverItsModeIsWritten(t *testing.T) {
	for _, row := range []struct {
		mode      string
		canonical bool
	}{{"40000", true}, {"040000", false}, {"40755", false}} {
		t.Run(row.mode, func(t *testing.T) {
			repo := gitfixture.New(t)
			commit := func(pack string, parents ...string) string {
				below := repo.Tree(map[string]string{"expense.json": pack})
				return repo.Commit(repo.RawTree(gitfixture.Entry{Mode: row.mode, ID: below, Name: "packs"}), parents...)
			}
			first := commit(packA)
			second := commit(packB, first)
			repo.Tag("v1", first)
			repo.Head(first)
			report := run(t, defaultOptions(repo.Dir))
			if report.Identities != 1 || report.Documents != 1 {
				t.Fatalf("the directory must be entered: %#v", report)
			}
			wantStatus, wantUnread := StatusValid, 0
			if !row.canonical {
				wantStatus, wantUnread = StatusIncomplete, 1
			}
			if report.Status != wantStatus || report.UnreadableCount != wantUnread {
				t.Fatalf("want %s with %d unreadable, got %#v", wantStatus, wantUnread, report)
			}
			if !row.canonical && (report.Unreadable[0].Path != "packs" || !strings.Contains(report.Unreadable[0].Reason, "written "+row.mode+", which git does not write")) {
				t.Fatalf("unexpected unreadable: %#v", report.Unreadable)
			}

			repo.Tag("v2", second)
			repo.Head(second)
			finding := onlyFinding(t, run(t, defaultOptions(repo.Dir)))
			if finding.Code != CodeChanged || finding.Published.Path != "packs/expense.json" || finding.Found.Ref != "refs/tags/v2" || finding.Found.Digest != digestOf(packB) {
				t.Fatalf("a change below the directory must be found: %#v found %#v", finding, finding.Found)
			}
		})
	}
}

// Every other entry the selection reaches is read as git reads it or
// reported: a file whose mode is written otherwise is read and reported, a
// symbolic link at a selected path, a submodule and a mode git reads as one
// are reported, and a tree with a mode that is not octal is unreadable whole.
func TestEveryEntryTheSelectionReachesIsReadOrReported(t *testing.T) {
	repo := gitfixture.New(t)
	blob := repo.Blob(packA)
	root := repo.RawTree(
		gitfixture.Entry{Mode: "120000", ID: repo.Blob("not selected"), Name: "README"},
		gitfixture.Entry{Mode: "0100644", ID: blob, Name: "a.json"},
		gitfixture.Entry{Mode: "120000", ID: repo.Blob("a.json"), Name: "link.json"},
		gitfixture.Entry{Mode: "160000", ID: strings.Repeat("cd", 20), Name: "module"},
		gitfixture.Entry{Mode: "60644", ID: blob, Name: "odd.json"},
	)
	commit := repo.Commit(root)
	repo.Tag("v1", commit)
	repo.Head(commit)
	report := run(t, defaultOptions(repo.Dir))
	if report.Status != StatusIncomplete || report.Identities != 1 || report.Documents != 1 {
		t.Fatalf("a.json must be read and the run incomplete: %#v", report)
	}
	want := map[string]string{
		"a.json":    "written 0100644, which git does not write; it was read as git reads it, as a regular file",
		"link.json": "symbolic link",
		"module":    "it is a submodule",
		"odd.json":  "mode 60644 names no file, directory, link or submodule",
	}
	if report.UnreadableCount != len(want) {
		t.Fatalf("want %d unreadable, got %#v", len(want), report.Unreadable)
	}
	for _, unreadable := range report.Unreadable {
		if reason, ok := want[unreadable.Path]; !ok || !strings.Contains(unreadable.Reason, reason) || unreadable.Ref != "refs/tags/v1" {
			t.Fatalf("unexpected unreadable: %#v", unreadable)
		}
	}
	// A submodule the selection excludes is not reached.
	options := defaultOptions(repo.Dir)
	exclude, _ := CompileGlob("module/**")
	options.Selection.Exclude = []Glob{exclude}
	if report := run(t, options); report.UnreadableCount != len(want)-1 {
		t.Fatalf("an excluded submodule is not reported: %#v", report.Unreadable)
	}

	malformed := gitfixture.New(t)
	commit = malformed.Commit(malformed.RawTree(gitfixture.Entry{Mode: "1o0644", ID: malformed.Blob(packA), Name: "a.json"}))
	malformed.Tag("v1", commit)
	malformed.Head(commit)
	report = run(t, defaultOptions(malformed.Dir))
	if report.Status != StatusIncomplete || report.UnreadableCount != 1 || report.Unreadable[0].Path != "." || !strings.Contains(report.Unreadable[0].Reason, "not octal digits") {
		t.Fatalf("a tree git cannot parse is unreadable: %#v", report)
	}
}

// planted writes a script that creates marker when run, and returns its path.
// It reads nothing and fails at once, so a git that ran it (an ssh command
// for a fetch among them) stops instead of waiting on it.
func planted(t *testing.T, dir, name, marker string) string {
	t.Helper()
	script := filepath.Join(dir, name)
	body := "#!/bin/sh\n: > '" + filepath.ToSlash(marker) + "'\nexit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(script)
}

func TestARepositorysConfigurationRunsNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the planted scripts are POSIX shell scripts")
	}
	repo := history(t, []string{"v1", "v2"}, []map[string]string{{"expense.json": packA}, {"expense.json": packB}}, nil)
	repo.Git("", "reset", "-q", "--hard")
	scripts := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	hooks := filepath.Join(scripts, "hooks")
	if err := os.Mkdir(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, hook := range []string{"pre-commit", "post-checkout", "reference-transaction", "post-index-change", "pre-auto-gc", "post-rewrite"} {
		planted(t, hooks, hook, marker)
	}
	command := planted(t, scripts, "command", marker)
	for _, setting := range [][2]string{
		{"core.hooksPath", filepath.ToSlash(hooks)},
		{"core.fsmonitor", command},
		{"core.pager", command},
		{"pager.cat-file", command},
		{"pager.for-each-ref", command},
		{"pager.rev-list", command},
		{"pager.rev-parse", command},
		{"core.sshCommand", command},
		{"diff.external", command},
		{"core.alternateRefsCommand", command},
		{"credential.helper", "!" + command},
	} {
		repo.Git("", "config", setting[0], setting[1])
	}
	// The plant works: a command that consults the fsmonitor runs it.
	repo.Git("", "status", "--short")
	if _, err := os.Stat(marker); err != nil {
		t.Skip("this git did not run the planted fsmonitor, so the plant cannot be shown to work here")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	report := run(t, defaultOptions(repo.Dir))
	onlyFinding(t, report)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the check ran a program the repository's configuration names")
	}
}

func TestAPartialCloneIsRefusedAndNothingIsFetched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the planted script is a POSIX shell script")
	}
	repo := gitfixture.New(t)
	missing := strings.Repeat("ab", 20)
	commit := repo.Commit(repo.TreeOf(gitfixture.Entry{Mode: "100644", Kind: "blob", ID: missing, Name: "expense.json"}))
	repo.Tag("v1", commit)
	repo.Head(commit)
	marker := filepath.Join(t.TempDir(), "fetched")
	command := planted(t, t.TempDir(), "ssh", marker)
	repo.Git("", "config", "remote.origin.url", "ssh://example.invalid/repository")
	repo.Git("", "config", "remote.origin.promisor", "true")
	repo.Git("", "config", "core.sshCommand", command)
	_, failure := Check(defaultOptions(repo.Dir))
	if failure == nil || failure.Code != FailurePartial {
		t.Fatalf("a promisor remote must be refused: %#v", failure)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("reading the repository started a fetch")
	}

	// Below the refusal, the reader itself starts no fetch for a blob the
	// repository lacks: the lazy fetch is turned off, and every transport
	// is refused besides.
	g, err := openGit(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := g.openObjects()
	if err != nil {
		t.Fatal(err)
	}
	defer objects.close()
	found, _, err := objects.read(missing, MaxObjectBytes)
	if err != nil || !found.missing {
		t.Fatalf("a blob the repository lacks is missing: %#v %v", found, err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("reading a missing blob started a fetch")
	}
}

// withoutVariables returns environment without the variables named.
func withoutVariables(environment []string, names ...string) []string {
	kept := []string{}
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		drop := false
		for _, unwanted := range names {
			drop = drop || name == unwanted
		}
		if !drop {
			kept = append(kept, entry)
		}
	}
	return kept
}

// gitAtLeast reports whether the git the tests run is at least major.minor.
func gitAtLeast(t *testing.T, repo *gitfixture.Repo, major, minor int) bool {
	t.Helper()
	text := strings.TrimPrefix(repo.Git("", "--version"), "git version ")
	fields := strings.SplitN(text, ".", 3)
	if len(fields) < 2 {
		t.Fatalf("cannot read the git version %q", text)
	}
	gotMajor, errMajor := strconv.Atoi(fields[0])
	gotMinor, errMinor := strconv.Atoi(strings.TrimFunc(fields[1], func(r rune) bool { return r < '0' || r > '9' }))
	if errMajor != nil || errMinor != nil {
		t.Fatalf("cannot read the git version %q", text)
	}
	return gotMajor > major || gotMajor == major && gotMinor >= minor
}

// A repository's own protocol.<name>.allow overrides the -c protocol.allow=never
// every invocation carries, so the environment is what refuses a fetch below
// the partial-clone refusal: GIT_ALLOW_PROTOCOL on every git, and, from git
// 2.45, GIT_NO_LAZY_FETCH. Each is shown to hold alone, after the fetch is
// shown to start with both removed.
func TestTheEnvironmentRefusesAFetchTheRepositoryAllows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the planted script is a POSIX shell script")
	}
	repo := gitfixture.New(t)
	missing := strings.Repeat("ab", 20)
	marker := filepath.Join(t.TempDir(), "fetched")
	command := planted(t, t.TempDir(), "ssh", marker)
	for _, setting := range [][2]string{
		{"remote.origin.url", "ssh://example.invalid/repository"},
		{"remote.origin.promisor", "true"},
		{"protocol.ssh.allow", "always"},
		{"core.sshCommand", command},
	} {
		repo.Git("", "config", setting[0], setting[1])
	}
	fetches := func(environment []string) bool {
		t.Helper()
		g, err := openGit(repo.Dir)
		if err != nil {
			t.Fatal(err)
		}
		g.environment = environment
		objects, err := g.openObjects()
		if err != nil {
			t.Fatal(err)
		}
		// The blob is missing either way; a git before 2.26 or so stops
		// when the fetch fails, so only the marker is asked.
		_, _, _ = objects.read(missing, MaxObjectBytes)
		objects.close()
		_, err = os.Stat(marker)
		_ = os.Remove(marker)
		return err == nil
	}
	full := childEnvironment(os.Environ())
	if !fetches(withoutVariables(full, "GIT_ALLOW_PROTOCOL", "GIT_NO_LAZY_FETCH")) {
		t.Skip("this git started no fetch with both refusals removed, so neither can be shown to work here")
	}
	if fetches(withoutVariables(full, "GIT_NO_LAZY_FETCH")) {
		t.Fatal("GIT_ALLOW_PROTOCOL alone must refuse a transport the repository's configuration allows")
	}
	if fetches(withoutVariables(full, "GIT_ALLOW_PROTOCOL")) && gitAtLeast(t, repo, 2, 45) {
		t.Fatal("from git 2.45, GIT_NO_LAZY_FETCH alone must stop the fetch")
	}
	if fetches(full) {
		t.Fatal("the environment every invocation gets must start no fetch")
	}
}

// The one transport GIT_ALLOW_PROTOCOL allows has a name no built-in
// transport has, and that no alias can have before git 2.54, whose names are
// letters, digits and hyphens: with a name like "none", a repository whose
// remote has vcs = none would have git run the repository's alias
// remote-none, where GIT_NO_LAZY_FETCH is not honoured.
func TestTheTransportGitIsAllowedHasNoNameARepositoryCanGive(t *testing.T) {
	allowed := ""
	for _, entry := range childEnvironment(nil) {
		if value, ok := strings.CutPrefix(entry, "GIT_ALLOW_PROTOCOL="); ok {
			allowed = value
		}
	}
	if allowed == "" || strings.Contains(allowed, ":") {
		t.Fatalf("GIT_ALLOW_PROTOCOL must name exactly one transport: %q", allowed)
	}
	if strings.Trim(allowed, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-") == "" {
		t.Fatalf("the transport GIT_ALLOW_PROTOCOL allows has a name an alias can have: %q", allowed)
	}
	for _, builtin := range []string{"file", "git", "ssh", "http", "https", "ftp", "ftps", "ext", "fd"} {
		if allowed == builtin {
			t.Fatalf("GIT_ALLOW_PROTOCOL allows the built-in transport %s", builtin)
		}
	}
}

// Each setting that makes a remote a promisor to git is refused before
// anything is read: a true promisor setting, a partialclonefilter setting
// alone, and extensions.partialclone. A promisor setting that is false makes
// none. Each row is then shown to make a plain git fetch, or not, as it says;
// that comes last because the fetch writes settings of its own.
func TestEveryPromisorSettingIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the planted script is a POSIX shell script")
	}
	for _, row := range []struct {
		key, value string
		refused    bool
	}{
		{"remote.origin.promisor", "true", true},
		{"remote.origin.partialclonefilter", "blob:none", true},
		{"extensions.partialclone", "origin", true},
		{"remote.origin.promisor", "false", false},
	} {
		t.Run(row.key+"="+row.value, func(t *testing.T) {
			repo := gitfixture.New(t)
			missing := strings.Repeat("ab", 20)
			commit := repo.Commit(repo.TreeOf(gitfixture.Entry{Mode: "100644", Kind: "blob", ID: missing, Name: "expense.json"}))
			repo.Tag("v1", commit)
			repo.Head(commit)
			marker := filepath.Join(t.TempDir(), "fetched")
			repo.Git("", "config", "remote.origin.url", "ssh://example.invalid/repository")
			repo.Git("", "config", "core.sshCommand", planted(t, t.TempDir(), "ssh", marker))
			repo.Git("", "config", row.key, row.value)
			report, failure := Check(defaultOptions(repo.Dir))
			if row.refused && (failure == nil || failure.Code != FailurePartial || !strings.Contains(failure.Message, row.key)) {
				t.Fatalf("a promisor must be refused: %#v", failure)
			}
			if !row.refused && (failure != nil || report.Status != StatusIncomplete || !strings.Contains(report.Unreadable[0].Reason, "not in this repository")) {
				t.Fatalf("no promisor: the repository is read and the blob it lacks is unreadable: %#v %#v", failure, report)
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("the check started a fetch")
			}
			_ = repo.Try("cat-file", "-e", missing)
			if _, err := os.Stat(marker); (err == nil) != row.refused {
				t.Skipf("to this git the setting does not make what the row says (fetched: %v)", err == nil)
			}
		})
	}
}

func TestReplaceRefsDoNotSubstituteBytes(t *testing.T) {
	repo := history(t, []string{"v1", "v2"}, []map[string]string{{"expense.json": packA}, {"expense.json": packB}}, nil)
	repo.Git("", "replace", repo.Blob(packB), repo.Blob(packA))
	finding := onlyFinding(t, run(t, defaultOptions(repo.Dir)))
	if finding.Found.Digest != digestOf(packB) {
		t.Fatalf("the replaced blob's own bytes must be read: %#v", finding.Found)
	}
}

func TestAnInheritedGitEnvironmentDoesNotRedirectTheRead(t *testing.T) {
	repo := history(t, []string{"v1", "v2"}, []map[string]string{{"expense.json": packA}, {"expense.json": packB}}, nil)
	other := history(t, []string{"v1"}, []map[string]string{{"expense.json": packA}}, nil)
	t.Setenv("GIT_DIR", other.GitDir())
	t.Setenv("GIT_OBJECT_DIRECTORY", filepath.Join(other.GitDir(), "objects"))
	t.Setenv("GIT_REPLACE_REF_BASE", "refs/other/")
	finding := onlyFinding(t, run(t, defaultOptions(repo.Dir)))
	if finding.Found.Ref != "refs/tags/v2" {
		t.Fatalf("unexpected finding: %#v", finding)
	}
}

func TestAShallowCloneIsStatedAndNeverClean(t *testing.T) {
	source := history(t, []string{"v1", "v2"}, []map[string]string{{"expense.json": packA}, {"expense.json": packA, "two.json": packNext}}, nil)
	clone := gitfixture.New(t)
	clone.Git("", "-c", "protocol.file.allow=always", "fetch", "-q", "--update-head-ok", "--depth", "1", "file://"+filepath.ToSlash(source.Dir), "+refs/heads/main:refs/heads/main")
	clone.Git("", "update-ref", "refs/tags/v2", clone.Git("", "rev-parse", "refs/heads/main"))
	_, failure := Check(defaultOptions(clone.Dir))
	if failure == nil || failure.Code != FailureShallow {
		t.Fatalf("a shallow clone without a manifest must be refused: %#v", failure)
	}
	full := run(t, defaultOptions(source.Dir))
	options := defaultOptions(clone.Dir)
	options.Manifest = &full.Manifest
	report := run(t, options)
	if !report.Shallow || report.Status != StatusValid || len(report.NotHeld) != 0 {
		t.Fatalf("a shallow clone checked against its manifest is stated as shallow: %#v", report)
	}
	// An entry nothing held answers is not a vanished identity in a shallow
	// clone, which cannot tell vanished from not fetched: it is listed.
	extra := full.Manifest
	extra.Documents = append(append([]ManifestEntry(nil), full.Manifest.Documents...), ManifestEntry{Kind: KindPack, ID: "old", Version: "0.1.0", Digest: digestOf("old"), FirstTag: "v0"})
	options.Manifest = &extra
	report = run(t, options)
	if !report.Shallow || report.Status != StatusValid || len(report.NotHeld) != 1 || report.NotHeld[0].ID != "old" {
		t.Fatalf("unexpected report: %#v", report)
	}
	// A manifest is never written from a shallow clone, with or without one
	// given: it could pin bytes a tag it lacks published otherwise.
	options.Writing = true
	if _, failure := Check(options); failure == nil || failure.Code != FailureShallow {
		t.Fatalf("writing a manifest from a shallow clone must be refused: %#v", failure)
	}
}

func TestTheManifestRoundTripsAndCatchesTamperingAndVanishing(t *testing.T) {
	repo := history(t, []string{"v1", "v2"}, []map[string]string{
		{"packs/expense.json": packA, "dialects/intake.json": dialectA},
		{"packs/expense.json": packA, "packs/next.json": packNext, "dialects/intake.json": dialectA},
	}, nil)
	first := run(t, defaultOptions(repo.Dir))
	encoded := EncodeManifest(first.Manifest)
	if string(encoded) != string(EncodeManifest(run(t, defaultOptions(repo.Dir)).Manifest)) {
		t.Fatal("the manifest's bytes must be deterministic")
	}
	want := "{\n  \"manifestVersion\": \"1\",\n  \"documents\": [\n" +
		"    {\"kind\": \"dialect\", \"id\": \"intake\", \"version\": \"1.0.0\", \"digest\": \"" + digestOf(dialectA) + "\", \"firstTag\": \"v1\"},\n" +
		"    {\"kind\": \"pack\", \"id\": \"expense\", \"version\": \"1.0.0\", \"digest\": \"" + digestOf(packA) + "\", \"firstTag\": \"v1\"},\n" +
		"    {\"kind\": \"pack\", \"id\": \"expense\", \"version\": \"1.1.0\", \"digest\": \"" + digestOf(packNext) + "\", \"firstTag\": \"v2\"}\n" +
		"  ]\n}\n"
	if string(encoded) != want {
		t.Fatalf("manifest bytes:\n%s\nwant:\n%s", encoded, want)
	}
	manifest, err := ReadManifest(encoded)
	if err != nil {
		t.Fatal(err)
	}
	options := defaultOptions(repo.Dir)
	options.Manifest = manifest
	if report := run(t, options); report.Status != StatusValid || len(report.New) != 0 || string(EncodeManifest(report.Manifest)) != want {
		t.Fatalf("the round trip must pass and rewrite the same manifest: %#v", report)
	}

	tampered := *manifest
	tampered.Documents = append([]ManifestEntry(nil), manifest.Documents...)
	tampered.Documents[1].Digest = digestOf(packB)
	options.Manifest = &tampered
	finding := onlyFinding(t, run(t, options))
	if finding.Code != CodeDiffersFromManifest || finding.Published.Source != SourceManifest || finding.Published.Digest != digestOf(packB) || finding.Found.Digest != digestOf(packA) || finding.Found.Ref != "refs/tags/v1" {
		t.Fatalf("unexpected finding: %#v found %#v", finding, finding.Found)
	}

	vanished := *manifest
	vanished.Documents = append(append([]ManifestEntry(nil), manifest.Documents...), ManifestEntry{Kind: KindPack, ID: "retired", Version: "0.1.0", Digest: digestOf("gone"), FirstTag: "v0"})
	options.Manifest = &vanished
	finding = onlyFinding(t, run(t, options))
	if finding.Code != CodeVanished || finding.ID != "retired" || finding.Found != nil || finding.Published.Ref != "refs/tags/v0" {
		t.Fatalf("unexpected finding: %#v", finding)
	}

	// A pinned identity HEAD still holds but no tag does (its tag was
	// deleted) has vanished from every tag.
	retagged := history(t, []string{"v2"}, []map[string]string{{"packs/expense.json": packA, "dialects/intake.json": dialectA}}, map[string]string{"packs/expense.json": packA, "packs/next.json": packNext, "dialects/intake.json": dialectA})
	headOnly := defaultOptions(retagged.Dir)
	headOnly.Manifest = manifest
	finding = onlyFinding(t, run(t, headOnly))
	if finding.Code != CodeVanished || finding.Version != "1.1.0" || finding.Published.Ref != "refs/tags/v2" {
		t.Fatalf("an identity only HEAD holds does not answer its pin: %#v", finding)
	}

	partial := *manifest
	partial.Documents = manifest.Documents[:2]
	options.Manifest = &partial
	report := run(t, options)
	if report.Status != StatusValid || len(report.New) != 1 || report.New[0].Version != "1.1.0" || len(report.Manifest.Documents) != 3 {
		t.Fatalf("a published identity the manifest does not pin is new, not a failure: %#v", report)
	}
	options.Strict = true
	finding = onlyFinding(t, run(t, options))
	if finding.Code != CodeNotInManifest || finding.Version != "1.1.0" || finding.Published.Ref != "refs/tags/v2" {
		t.Fatalf("under strict a new identity is a finding: %#v", finding)
	}
}

func TestTheManifestReaderRefusesWhatItWouldNotWrite(t *testing.T) {
	entry := `{"kind": "pack", "id": "expense", "version": "1.0.0", "digest": "` + digestOf(packA) + `", "firstTag": "v1"}`
	if _, err := ReadManifest([]byte(`{"manifestVersion": "1", "documents": [` + entry + `]}`)); err != nil {
		t.Fatalf("a well-formed manifest must be read: %v", err)
	}
	for name, document := range map[string]string{
		"unknown member":    `{"manifestVersion": "1", "documents": [], "note": "x"}`,
		"other case":        `{"ManifestVersion": "1", "documents": []}`,
		"other version":     `{"manifestVersion": "2", "documents": []}`,
		"no documents":      `{"manifestVersion": "1"}`,
		"duplicate member":  `{"manifestVersion": "1", "manifestVersion": "1", "documents": []}`,
		"unknown kind":      `{"manifestVersion": "1", "documents": [` + strings.Replace(entry, `"pack"`, `"graph"`, 1) + `]}`,
		"short digest":      `{"manifestVersion": "1", "documents": [` + strings.Replace(entry, digestOf(packA), "sha256:abc", 1) + `]}`,
		"upper-case digest": `{"manifestVersion": "1", "documents": [` + strings.Replace(entry, digestOf(packA), strings.ToUpper(digestOf(packA)), 1) + `]}`,
		"empty first tag":   `{"manifestVersion": "1", "documents": [` + strings.Replace(entry, `"v1"`, `""`, 1) + `]}`,
		"entry member":      `{"manifestVersion": "1", "documents": [` + strings.Replace(entry, `}`, `, "path": "p"}`, 1) + `]}`,
		"twice":             `{"manifestVersion": "1", "documents": [` + entry + `, ` + entry + `]}`,
	} {
		_, err := ReadManifest([]byte(document))
		if err == nil {
			t.Errorf("%s: the manifest must be refused", name)
		}
		if errors.Is(err, ErrManifestVersion) != (name == "other version") {
			t.Errorf("%s: only another manifestVersion is a version refusal: %v", name, err)
		}
	}
	if _, err := ReadManifest([]byte(`{"manifestVersion": 1, "documents": []}`)); err == nil || errors.Is(err, ErrManifestVersion) {
		t.Errorf("a manifestVersion that is not a string is damage, not another version: %v", err)
	}
}

// tree lists every file under dir, .git included, with the SHA-256 of its
// bytes, and every directory and link.
func listing(t *testing.T, dir string) map[string]string {
	t.Helper()
	found := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(dir, path)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.Type().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			found[relative] = hex.EncodeToString(sum[:]) + " " + info.Mode().String()
		default:
			found[relative] = info.Mode().String()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestNothingIsWrittenIntoTheRepository(t *testing.T) {
	repo := history(t, []string{"v1", "v2"}, []map[string]string{{"expense.json": packA}, {"expense.json": packB, "next.json": packNext}},
		map[string]string{"expense.json": packB})
	// A working tree and an index, so a command that refreshed the index
	// would have something to write.
	repo.Git("", "reset", "-q", "--hard")
	before := listing(t, repo.Dir)
	run(t, defaultOptions(repo.Dir))
	after := listing(t, repo.Dir)
	if len(before) != len(after) {
		t.Fatalf("the repository's files changed: %d before, %d after", len(before), len(after))
	}
	for path, digest := range before {
		if after[path] != digest {
			t.Fatalf("%s changed: %s, then %s", path, digest, after[path])
		}
	}
}

func TestEachBlobIsReadOnceHoweverOftenItIsHeld(t *testing.T) {
	repo := history(t, []string{"v1", "v2", "v3"}, []map[string]string{
		{"a.json": packA, "b.json": packA},
		{"a.json": packA, "c/d.json": packA, "x": "1"},
		{"e.json": packA},
	}, map[string]string{"f.json": packA, "g.json": "[]", "h.json": "[]"})
	report := run(t, defaultOptions(repo.Dir))
	if report.Status != StatusValid || report.Documents != 1 || report.OtherFiles != 1 {
		t.Fatalf("one distinct document and one distinct other file must be read once each: %#v", report)
	}
}

func TestADocumentOverTheReadLimitIsUnreadable(t *testing.T) {
	large := `{"specVersion":"0.5.0","id":"big","version":"1.0.0","pad":"` + strings.Repeat("x", int(MaxObjectBytes)) + `"}`
	repo := history(t, []string{"v1"}, []map[string]string{{"big.json": large}}, nil)
	report := run(t, defaultOptions(repo.Dir))
	if report.Status != StatusIncomplete || report.UnreadableCount != 1 || !strings.Contains(report.Unreadable[0].Reason, "larger than") {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestTagsAreOrderedByTheCommitGraphNotByName(t *testing.T) {
	repo := gitfixture.New(t)
	first := repo.Snapshot(map[string]string{"expense.json": packA})
	second := repo.Snapshot(map[string]string{"expense.json": packB}, first)
	repo.Tag("z-first", first)
	repo.Tag("a-second", second)
	repo.Head(second)
	finding := onlyFinding(t, run(t, defaultOptions(repo.Dir)))
	if finding.Published.Ref != "refs/tags/z-first" || finding.Found.Ref != "refs/tags/a-second" {
		t.Fatalf("the ancestor's tag published the identity: %#v found %#v", finding.Published, finding.Found)
	}
}

func TestTagPatternsAndTagsThatNameNoCommit(t *testing.T) {
	repo := gitfixture.New(t)
	first := repo.Snapshot(map[string]string{"expense.json": packA})
	second := repo.Snapshot(map[string]string{"expense.json": packB}, first)
	repo.Tag("v1", first)
	repo.Tag("experiment", second)
	repo.Tag("v-blob", repo.Blob("not a commit"))
	repo.Head(first)
	options := defaultOptions(repo.Dir)
	release, _ := CompileGlob("v*")
	options.Tags = []Glob{release}
	report := run(t, options)
	if report.Status != StatusIncomplete || report.TagsTotal != 3 || report.TagsRead != 1 || len(report.UnreadTags) != 1 ||
		report.UnreadTags[0].Ref != "refs/tags/v-blob" || !strings.Contains(report.UnreadTags[0].Reason, "not a commit") {
		t.Fatalf("unexpected report: %#v", report)
	}
	options.Tags = nil
	if report := run(t, options); report.FindingCount != 1 {
		t.Fatalf("every tag counts when no pattern is given: %#v", report)
	}
}

func TestIncludeAndExcludeSelectThePathsRead(t *testing.T) {
	repo := history(t, []string{"v1", "v2"}, []map[string]string{
		{"packs/expense.json": packA, "testdata/expense.json": packB},
		{"packs/expense.json": packA, "testdata/expense.json": packB},
	}, nil)
	if report := run(t, defaultOptions(repo.Dir)); report.FindingCount != 1 {
		t.Fatalf("by default every .json file is read: %#v", report)
	}
	options := defaultOptions(repo.Dir)
	exclude, _ := CompileGlob("testdata/**")
	options.Selection.Exclude = []Glob{exclude}
	if report := run(t, options); report.Status != StatusValid {
		t.Fatalf("an excluded directory is not read: %#v", report)
	}
}

func TestMissingGitIsAnOperationalFailure(t *testing.T) {
	saved := gitProgram
	gitProgram = "jpack-test-no-such-program"
	defer func() { gitProgram = saved }()
	if _, failure := Check(defaultOptions(t.TempDir())); failure == nil || failure.Code != FailureGit {
		t.Fatalf("want %s, got %#v", FailureGit, failure)
	}
}

func TestADirectoryOutsideAnyRepositoryIsRefused(t *testing.T) {
	gitfixture.Require(t)
	_, failure := Check(defaultOptions(t.TempDir()))
	if failure == nil || failure.Code != FailureRepository {
		t.Fatalf("want %s, got %#v", FailureRepository, failure)
	}
}
