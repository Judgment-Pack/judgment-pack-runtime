package emit

import "testing"

// MayOwnAtOrBelow is the host's EMIT-LINK rule for a link at a path (an owns
// glob matches the path or may match one below it), with names read loosely
// too: in any case, Unicode normalization or HFS+ ignorable code point, with
// a segment's trailing dots and spaces dropped and a backslash read as a
// separator, as Windows reads names, and a glob's segment holding "~" as any
// name, which Windows can read as a short name.
func TestMayOwnAtOrBelowReadsNamesLoosely(t *testing.T) {
	cases := []struct {
		owns []string
		path string
		want bool
	}{
		{[]string{"gen/**"}, "gen", true},
		{[]string{"gen/**"}, "gen/py/bin", true},
		{[]string{"gen/py/a.go"}, "gen", true},
		{[]string{"gen/py/a.go"}, "gen/py/a.go", true},
		{[]string{"gen/**"}, "Gen", true},
		{[]string{"gen/**"}, "GEN/x", true},
		{[]string{"GENERA~1/*.go"}, "generated", true},
		{[]string{"GENERA~1/*.go"}, "generated/a.go", true},
		{[]string{"docs/straße.md"}, "docs/STRASSE.md", true},
		{[]string{"docs/café.md"}, "docs/café.md", true},
		{[]string{"ılk/**"}, "ILK", true},
		{[]string{"gen‌/**"}, "gen", true},
		{[]string{"out/**"}, "out.", true},
		{[]string{"out/**"}, "out ./x", true},
		{[]string{"out/a.txt"}, "out. ", true},
		{[]string{"out/a.txt"}, `out\a.txt`, true},
		{[]string{"out/**"}, "out.d", false},
		{[]string{"gen/**"}, "other", false},
		{[]string{"gen/**"}, "ge", false},
		{[]string{"gen/*.go"}, "gen/a.go/x", false},
		{[]string{"GENERA~1/*.go"}, "x/generated", false},
		{[]string{"docs/**", "gen/**"}, "venv", false},
	}
	for _, c := range cases {
		m := &Manifest{}
		for _, source := range c.owns {
			glob, err := parseGlob(source)
			if err != nil {
				t.Fatalf("%q: %v", source, err)
			}
			m.Owns = append(m.Owns, glob)
		}
		if got := m.MayOwnAtOrBelow(c.path); got != c.want {
			t.Errorf("owns %q, %q: %v, want %v", c.owns, c.path, got, c.want)
		}
	}
}

// NameKey reads names as the loosest file system may: case, normalization
// and HFS+ ignorables folded, a backslash as a separator, and each
// segment's trailing dots and spaces dropped, so that a name made only of
// them has an empty key.
func TestNameKey(t *testing.T) {
	for path, want := range map[string]string{
		"venv.":     "VENV",
		"venv. /x ": "VENV/X",
		`a\b.`:      "A/B",
		"...":       "",
		"a/. ./b":   "A//B",
		"a.b":       "A.B",
		".hidden":   ".HIDDEN",
		"café":      "CAFÉ",
		"zero‌":     "ZERO",
	} {
		if got := NameKey(path); got != want {
			t.Errorf("NameKey(%q) = %q, want %q", path, got, want)
		}
	}
}
