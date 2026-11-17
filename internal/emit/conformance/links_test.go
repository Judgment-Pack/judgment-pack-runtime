package conformance

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
)

// namesNone names no link: every link that leads out is left out.
func namesNone(string) bool { return false }

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
	w, d := newWorkspace(source, namesNone)
	if w != nil || d == nil || d.Code != "EMIT-HARNESS-BOUND" || d.Location != (emit.Location{Document: emit.DocumentRepository, Path: "y-out"}) {
		t.Fatalf("workspace %+v, diagnostic %+v", w, d)
	}
	for _, link := range []string{"z/out", "y-out"} {
		if err := os.Remove(filepath.Join(source, filepath.FromSlash(link))); err != nil {
			t.Fatal(err)
		}
	}
	w, d = newWorkspace(source, namesNone)
	if d != nil || w == nil || !w.fallback || w.root != source {
		t.Fatalf("workspace %+v, diagnostic %+v", w, d)
	}
	w.close()
}

// The harness reports at most emit.MaxListedPaths links left out, one at
// each in sorted order, and one more whose message counts the rest.
func TestLinksLeftOutAreListedUnderTheCap(t *testing.T) {
	f := newFixture(t)
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
