package conformance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/gitindex"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/gitindex/gitindextest"
)

// refusesNone refuses at no link: every link that leads out is left out.
func refusesNone([]string) (string, bool) { return "", false }

// writeIndex makes dir a repository's working tree whose index holds
// entries, in git's order.
func writeIndex(t *testing.T, dir string, entries ...gitindextest.Entry) {
	t.Helper()
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b gitindextest.Entry) int { return strings.Compare(a.Path, b.Path) })
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "index"), gitindextest.Builder{Version: 2, Entries: sorted}.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// trackedLinks reads the index: a link is tracked when its path, or a path
// below it, is in the index, compared byte for byte and loosely (case,
// normalization, HFS+ ignorables, a backslash, trailing dots and spaces);
// every link below a submodule, or below the directory of an index segment
// that may be a Windows short name, is tracked too.
func TestTrackedLinks(t *testing.T) {
	root := t.TempDir()
	writeIndex(t, root,
		gitindextest.Entry{Path: "a.txt"},
		gitindextest.Entry{Path: "Docs/Guide.md"},
		gitindextest.Entry{Path: "caf\u00e9/x"},
		gitindextest.Entry{Path: "dots./x"},
		gitindextest.Entry{Path: "dir/GENERA~1/a.go"},
		gitindextest.Entry{Path: "sub", Mode: gitindex.ModeGitlink},
		gitindextest.Entry{Path: "venv/python", Mode: gitindex.ModeLink},
		gitindextest.Entry{Path: `win\dir\file`},
		gitindextest.Entry{Path: "zero\u200cwidth/x"},
	)
	want := map[string]bool{
		"a.txt":            true, // the path itself
		"Docs":             true, // a path below it
		"venv":             true, // a link below it
		"venv/python":      true, // a link git tracks
		"docs":             true, // in another case
		"DOCS/GUIDE.MD":    true,
		"cafe\u0301":       true, // decomposed
		"dots":             true, // a trailing dot dropped
		"win":              true, // a backslash read as a separator
		"zerowidth":        true, // an HFS+ ignorable code point
		"dir/generated":    true, // GENERA~1 may be its short name
		"dir/other/deeper": true, // or any name in dir
		"sub/x":            true, // below a submodule
		"sub":              true,
		"a":                false, // "a.txt" is not below it
		"subway":           false,
		"venv2":            false,
		"dirt/x":           false,
		"elsewhere/x":      false,
	}
	links := make([]string, 0, len(want))
	for link := range want {
		links = append(links, link)
	}
	slices.Sort(links)
	tracked, err := trackedLinks(root, links)
	if err != nil {
		t.Fatal(err)
	}
	for i, link := range links {
		if tracked[i] != want[link] {
			t.Errorf("%q: tracked %v, want %v", link, tracked[i], want[link])
		}
	}
	// A short name at the root: every link is tracked.
	writeIndex(t, root, gitindextest.Entry{Path: "VENV~1/python"})
	if tracked, err := trackedLinks(root, []string{"anything/below"}); err != nil || !tracked[0] {
		t.Errorf("a short name at the root: %v %v", tracked, err)
	}
	// An index that cannot be read is an error, never "untracked".
	if err := os.WriteFile(filepath.Join(root, ".git", "index"), []byte("DIRC"), 0o644); err != nil {
		t.Fatal(err)
	}
	if tracked, err := trackedLinks(root, []string{"elsewhere/x"}); !errors.Is(err, gitindex.ErrFormat) || tracked != nil {
		t.Errorf("a corrupt index: %v %v", tracked, err)
	}
	if tracked, err := trackedLinks(t.TempDir(), []string{"elsewhere/x"}); !errors.Is(err, gitindex.ErrNoRepository) || tracked != nil {
		t.Errorf("no .git: %v %v", tracked, err)
	}
}

func TestMayBeShortName(t *testing.T) {
	for segment, want := range map[string]bool{"GENERA~1": true, "A~12.TXT": true, "~1": true, "x~0y": true,
		"backup~": false, "~": false, "a~b": false, "plain": false, "": false} {
		if got := mayBeShortName(segment); got != want {
			t.Errorf("%q: %v, want %v", segment, got, want)
		}
	}
}

// vectorPaths follows the vectors directory link by link: it gives the
// repository paths of the links it followed in the repository and where the
// directory ends there, "." for the root or a directory above it, and "."
// for what it cannot follow.
func TestVectorPathsFollowsTheVectorsDirectory(t *testing.T) {
	root, err := emit.RealRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside, alias := t.TempDir(), t.TempDir()
	write(t, root, map[string]string{"a/b/v/harness.json": "{}"})
	write(t, outside, map[string]string{"v/harness.json": "{}"})
	symlinks(t, root, map[string]string{"ext": outside, "in": "a", "loop": "loop/x",
		"up": "../" + filepath.Base(outside), "back": "a/b/../b"})
	symlinks(t, alias, map[string]string{"into": filepath.Join(root, "a"), "over": "../" + filepath.Base(root) + "/a"})
	// The directory is taken cleaned, as filepath.Abs gives it and the
	// harness opens it; a link's ".." is taken from where the link is.
	cases := map[string]struct {
		dir     string
		through []string
		at      string
	}{
		"outside":                             {filepath.Join(outside, "v"), nil, ""},
		"in the repository":                   {filepath.Join(root, "a", "b", "v"), nil, "a/b/v"},
		"the root":                            {root, nil, "."},
		"above the root":                      {filepath.Dir(root), nil, "."},
		"through a link that leads out":       {filepath.Join(root, "ext", "v"), []string{"ext"}, ""},
		"through a link out by its ..":        {filepath.Join(root, "up", "v"), []string{"up"}, ""},
		"through a link that stays in":        {filepath.Join(root, "in", "b", "v"), []string{"in"}, "a/b/v"},
		"through a link in by its ..":         {filepath.Join(root, "back", "v"), []string{"back"}, "a/b/v"},
		"into the repository from outside":    {filepath.Join(alias, "into", "b", "v"), nil, "a/b/v"},
		"into it from outside by a link's ..": {filepath.Join(alias, "over", "b", "v"), nil, "a/b/v"},
		"a loop":                              {filepath.Join(root, "loop", "v"), nil, "."},
		"a part that cannot be read":          {filepath.Join(root, "missing", "v"), nil, "."},
	}
	for name, c := range cases {
		through, at := vectorPaths(root, c.dir)
		if !slices.Equal(through, c.through) || at != c.at {
			t.Errorf("%s: through %q, at %q; want %q, %q", name, through, at, c.through, c.at)
		}
	}
}

// mayMeet compares a path as given with one found on disk in any case and
// normalization, a segment with "~" as any name, and at, above or below.
func TestMayMeetReadsNamesLoosely(t *testing.T) {
	cases := []struct {
		given, name string
		want        bool
	}{
		{"ext", "ext", true},
		{"EXT", "ext", true},
		{"ext/v", "ext", true},
		{"ext", "ext/v", true},
		{"VECTOR~1/a", "vectors-of-the-emitter", true},
		{"café", "café", true},
		{".", "anything", true},
		{"ext", "other", false},
		{"ext/v", "ext2", false},
		{"a/b", "a/c", false},
	}
	for _, c := range cases {
		if got := mayMeet(c.given, c.name); got != c.want {
			t.Errorf("mayMeet(%q, %q) = %v, want %v", c.given, c.name, got, c.want)
		}
	}
}

// copyTree leaves out the links omit holds, and refuses one that leads out
// and that it does not hold, as a link changed after the check would be.
func TestCopyLeavesOutOnlyTheLinksItIsGiven(t *testing.T) {
	source, outside := t.TempDir(), t.TempDir()
	write(t, source, map[string]string{"a.txt": "a\n"})
	symlinks(t, source, map[string]string{"out": outside, "self": "."})
	target := t.TempDir()
	states, err := copyTree(source, target, map[string]bool{"out": true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := states["out"]; ok || len(states) != 2 {
		t.Fatalf("states: %v", states)
	}
	if _, err := os.Lstat(filepath.Join(target, "out")); !os.IsNotExist(err) {
		t.Fatalf("the link was copied: %v", err)
	}
	if d := copyFailure(second(copyTree(source, t.TempDir(), nil))); d == nil || d.Code != "EMIT-HARNESS-BOUND" || d.Location.Path != "out" {
		t.Fatalf("an outward link not given: %+v", d)
	}
}

func second[A, B any](_ A, b B) B { return b }

// Where the copy falls back to the repository itself, for want of the
// privilege to make a link, a link left out of the copy would be there for
// the emitter: the run is refused at the first, EMIT-HARNESS-BOUND, as before
// links were left out. Without one, the fallback runs as before.
func TestTheFallbackRefusesALinkLeftOutOfTheCopy(t *testing.T) {
	source, outside := t.TempDir(), t.TempDir()
	symlinks(t, source, map[string]string{"self": ".", "z/out": outside, "y-out": outside})
	real := makeLink
	t.Cleanup(func() { makeLink = real })
	makeLink = func(string, string) error { return errCopyLink }
	w, d := newWorkspace(source, refusesNone)
	if w != nil || d == nil || d.Code != "EMIT-HARNESS-BOUND" || d.Location != (emit.Location{Document: emit.DocumentRepository, Path: "y-out"}) {
		t.Fatalf("workspace %+v, diagnostic %+v", w, d)
	}
	for _, link := range []string{"z/out", "y-out"} {
		if err := os.Remove(filepath.Join(source, filepath.FromSlash(link))); err != nil {
			t.Fatal(err)
		}
	}
	w, d = newWorkspace(source, refusesNone)
	if d != nil || w == nil || !w.fallback || w.root != source {
		t.Fatalf("workspace %+v, diagnostic %+v", w, d)
	}
	w.close()
}

// The harness reports at most emit.MaxListedPaths links left out, one at
// each in sorted order, and one more whose message counts the rest.
func TestLinksLeftOutAreListedUnderTheCap(t *testing.T) {
	f := newFixture(t)
	writeIndex(t, f.root, gitindextest.Entry{Path: emitterCommand})
	outside := t.TempDir()
	links := map[string]string{}
	for i := range emit.MaxListedPaths + 2 {
		links[fmt.Sprintf("venv/l%03d", i)] = outside
	}
	symlinks(t, f.root, links)
	m := f.manifest([]string{"stray", "create", "gen/a.txt"}, map[string]any{"environments": "all"})
	f.registryVector("a", nil)
	report := f.run(m, true)
	var listed []string
	var counts []emit.Diagnostic
	for _, d := range report.Diagnostics {
		switch {
		case d.Code != "EMIT-LINK-OMITTED":
			t.Fatalf("diagnostic: %+v", d)
		case d.Location.Path == "":
			counts = append(counts, d)
		default:
			listed = append(listed, d.Location.Path)
		}
	}
	all := make([]string, 0, len(links))
	for link := range links {
		all = append(all, link)
	}
	slices.Sort(all)
	if !slices.Equal(listed, all[:emit.MaxListedPaths]) || len(counts) != 1 || !strings.HasSuffix(counts[0].Message, "; 2 more are not listed.") {
		t.Fatalf("listed %d, counts %+v", len(listed), counts)
	}
}

// A link that leads out of the repository, that git does not track, and
// that no path the run writes or reads could name, is left out of the copy
// and reported as EMIT-LINK-OMITTED at its path: a write at its path lands
// in the copy, a stray write there, and never leaves it. The link stays in
// the repository.
func TestALinkOutOfTheRepositoryIsLeftOut(t *testing.T) {
	outside := t.TempDir()
	cases := map[string]struct {
		links map[string]string
		path  string
	}{
		"absolute":                     {map[string]string{"out": outside}, "out"},
		"absolute, by a .. of its own": {map[string]string{"out": "ROOT/docs/../.."}, "out"},
		"relative":                     {map[string]string{"docs/out": "../.."}, "docs/out"},
		"through a link to the root":   {map[string]string{"r": ".", "s": "r/../x"}, "s"},
		"absolute, through a link":     {map[string]string{"r": ".", "s": "ROOT/r/../x"}, "s"},
		"a loop":                       {map[string]string{"loop": "loop/x"}, "loop"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			write(t, f.root, map[string]string{"docs/a.md": "a\n"})
			writeIndex(t, f.root, gitindextest.Entry{Path: emitterCommand}, gitindextest.Entry{Path: "docs/a.md"})
			links := map[string]string{}
			for link, target := range c.links {
				links[link] = strings.Replace(target, "ROOT", f.root, 1)
			}
			symlinks(t, f.root, links)
			m := f.manifest([]string{"stray", "create", "out/written.txt"}, map[string]any{"environments": "all"})
			f.registryVector("a", nil)
			report := f.run(m, true)
			if report.Outcome != OutcomeFail || len(report.Diagnostics) != 1 {
				t.Fatalf("report: %s", dump(report))
			}
			if d := report.Diagnostics[0]; d.Code != "EMIT-LINK-OMITTED" || d.Severity != emit.SeverityWarning ||
				d.Location != (emit.Location{Document: emit.DocumentRepository, Path: c.path}) || d.Message != emit.NewDiagnostic("EMIT-LINK-OMITTED", d.Location).Message {
				t.Fatalf("diagnostic: %+v", d)
			}
			row := rowOf(t, report, "a", checkStrayWrites)
			if want := []StrayWrite{{"EMIT-STRAY-WRITE", ".", StrayCreated}}; row.Status != StatusFail || !slices.Equal(row.StrayWrites, want) {
				t.Fatalf("check 6: %+v", row)
			}
			if _, err := os.Lstat(filepath.Join(outside, "written.txt")); !os.IsNotExist(err) {
				t.Fatalf("the emitter wrote outside the copy: %v", err)
			}
			for link, target := range links {
				if got, err := os.Readlink(filepath.Join(f.root, filepath.FromSlash(link))); err != nil || got != target {
					t.Fatalf("%s in the repository: %q %v", link, got, err)
				}
			}
		})
	}
}

// A link that leads out of the repository and that git tracks, that a path
// the run writes or reads could name, or whose repository's index cannot be
// read, refuses the run with EMIT-HARNESS-BOUND at its path, before the
// emitter runs.
func TestALinkTheCopyCannotLeaveOutIsRefused(t *testing.T) {
	tracked := []gitindextest.Entry{{Path: emitterCommand}}
	cases := map[string]struct {
		owns    []any
		link    string
		vectors bool
		// index makes the repository's .git; nil writes an index of tracked.
		index func(t *testing.T, root string)
	}{
		"a link git tracks": {link: "venv/python", index: func(t *testing.T, root string) {
			writeIndex(t, root, append(tracked, gitindextest.Entry{Path: "venv/python", Mode: gitindex.ModeLink})...)
		}},
		"a file git tracks below it": {link: "venv", index: func(t *testing.T, root string) {
			writeIndex(t, root, append(tracked, gitindextest.Entry{Path: "venv/lib/site.py"})...)
		}},
		"a link git tracks in another case": {link: "Venv", index: func(t *testing.T, root string) {
			writeIndex(t, root, append(tracked, gitindextest.Entry{Path: "venv"})...)
		}},
		"a link below a submodule": {link: "sub/out", index: func(t *testing.T, root string) {
			writeIndex(t, root, append(tracked, gitindextest.Entry{Path: "sub", Mode: gitindex.ModeGitlink})...)
		}},
		"at an owned directory":        {owns: []any{"gen/**"}, link: "gen"},
		"below it":                     {owns: []any{"gen/**"}, link: "gen/py"},
		"above an owned file":          {owns: []any{"gen/py/a.go"}, link: "gen"},
		"in another case":              {owns: []any{"gen/**"}, link: "Gen"},
		"by a short name the glob has": {owns: []any{"GENERA~1/*.go"}, link: "generated"},
		"on the way to the vectors":    {link: "ext", vectors: true},
		"no .git":                      {link: "out", index: func(*testing.T, string) {}},
		"a .git file, a worktree's": {link: "out", index: func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: elsewhere\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		"no index": {link: "out", index: func(t *testing.T, root string) {
			if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		"a corrupt index": {link: "out", index: func(t *testing.T, root string) {
			writeIndex(t, root, tracked...)
			data, err := os.ReadFile(filepath.Join(root, ".git", "index"))
			if err != nil {
				t.Fatal(err)
			}
			data[len(data)-1] ^= 1
			if err := os.WriteFile(filepath.Join(root, ".git", "index"), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		"a split index": {link: "out", index: func(t *testing.T, root string) {
			b := gitindextest.Builder{Version: 2, Entries: tracked, Extensions: [][]byte{gitindextest.Extension("link", make([]byte, 20))}}
			if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".git", "index"), b.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			if c.index == nil {
				writeIndex(t, f.root, tracked...)
			} else {
				c.index(t, f.root)
			}
			target := t.TempDir()
			symlinks(t, f.root, map[string]string{c.link: target})
			if c.vectors {
				f.vectors = filepath.Join(f.root, c.link, "vectors")
			}
			owns := c.owns
			if owns == nil {
				owns = []any{"gen/**"}
			}
			m := f.manifest([]string{"stray", "create", "ran.txt"}, map[string]any{"environments": "all", "owns": owns})
			f.registryVector("a", nil)
			report := f.run(m, true)
			if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun || len(report.Rows) != 0 || len(report.Diagnostics) != 1 {
				t.Fatalf("report: %s", dump(report))
			}
			if d := report.Diagnostics[0]; d.Code != "EMIT-HARNESS-BOUND" || d.Location != (emit.Location{Document: emit.DocumentRepository, Path: c.link}) {
				t.Fatalf("diagnostic: %+v", d)
			}
			for _, dir := range []string{f.root, target} {
				if _, err := os.Lstat(filepath.Join(dir, "ran.txt")); !os.IsNotExist(err) {
					t.Fatalf("the emitter ran: %v", err)
				}
			}
		})
	}
	// The control: with the index of tracked, the same link at a path no
	// rule names is left out, and the emitter runs.
	f := newFixture(t)
	writeIndex(t, f.root, tracked...)
	symlinks(t, f.root, map[string]string{"out": t.TempDir()})
	m := f.manifest([]string{"stray", "create", "ran.txt"}, map[string]any{"environments": "all"})
	f.registryVector("a", nil)
	if report := f.run(m, true); report.Outcome != OutcomeFail || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "EMIT-LINK-OMITTED" {
		t.Fatalf("control: %s", dump(report))
	}
}
