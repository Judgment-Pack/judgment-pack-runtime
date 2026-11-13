package published

import "testing"

func TestGlobs(t *testing.T) {
	for _, row := range []struct {
		pattern, name string
		match         bool
	}{
		{"**/*.json", "a.json", true},
		{"**/*.json", "a/b/c.json", true},
		{"**/*.json", "a/b/c.json5", false},
		{"*.json", "a/b.json", false},
		{"packs/*.json", "packs/a.json", true},
		{"packs/*.json", "packs/x/a.json", false},
		{"packs/**", "packs/x/a.json", true},
		{"a/**/b/*.json", "a/b/c.json", true},
		{"a/**/b/*.json", "a/x/y/b/c.json", true},
		{"v*", "v1.0.0", true},
		{"v*", "release/v1", false},
		{"**", "release/v1", true},
		{"*.json", "new\nline.json", true},
		{"*.json", "-x.json", true},
	} {
		glob, err := CompileGlob(row.pattern)
		if err != nil {
			t.Fatalf("%q: %v", row.pattern, err)
		}
		if got := glob.Match(row.name); got != row.match {
			t.Errorf("%q against %q: got %v", row.pattern, row.name, got)
		}
	}
	for _, bad := range []string{"", "a//b", "a/**b", "[", "a/[b"} {
		if _, err := CompileGlob(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestSelectionPrunesOnlyWhatCannotMatch(t *testing.T) {
	include, _ := CompileGlob("packs/**/*.json")
	exclude, _ := CompileGlob("packs/testdata/**")
	selection := Selection{Include: []Glob{include}, Exclude: []Glob{exclude}}
	for dir, want := range map[string]bool{"packs": true, "packs/a": true, "src": false, "packs/testdata": false, "packs/testdata/x": false} {
		if got := selection.descends(dir); got != want {
			t.Errorf("descends(%q) = %v", dir, got)
		}
	}
	if !selection.selects("packs/a/b.json") || selection.selects("packs/testdata/b.json") || selection.selects("src/b.json") {
		t.Error("selects disagrees with the patterns")
	}
}
