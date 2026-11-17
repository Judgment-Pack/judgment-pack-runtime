package releasecheck

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// settledTree is a repository root with every file the check reads and no
// marker. The word alone, without its parenthesis, is not a marker.
func settledTree() fstest.MapFS {
	return fstest.MapFS{
		"VERSIONING.md":          {Data: []byte("# Versioning\n\nA placeholder, or PLACEHOLDER alone, is prose.\n")},
		"CHANGELOG.md":           {Data: []byte("# Changelog\n")},
		"SECURITY.md":            {Data: []byte("# Security\n")},
		"docs/releasing.md":      {Data: []byte("# Releasing\n")},
		"docs/adr/0001-first.md": {Data: []byte("# First\n")},
		"README.md":              {Data: []byte("<!-- PLACEHOLDER(1.0): a file the check does not read -->\n")},
	}
}

func TestCheckPlaceholdersPassesWithoutAMarker(t *testing.T) {
	if err := CheckPlaceholders(settledTree()); err != nil {
		t.Fatal(err)
	}
}

// A marker in each file the check reads, a nested docs/ file among them,
// refuses the release and is named by its file and line.
func TestCheckPlaceholdersRefusesAMarker(t *testing.T) {
	for _, path := range []string{"VERSIONING.md", "CHANGELOG.md", "SECURITY.md", "docs/releasing.md", "docs/adr/0001-first.md"} {
		t.Run(path, func(t *testing.T) {
			tree := settledTree()
			tree[path] = &fstest.MapFile{Data: []byte("first line\nthe tag `registry-v1.0.0` <!-- PLACEHOLDER(1.0): confirm once the tag exists -->\n")}
			err := CheckPlaceholders(tree)
			if err == nil {
				t.Fatal("a marker must refuse the release")
			}
			if !strings.Contains(err.Error(), path+":2") {
				t.Fatalf("the refusal must name %s:2: %v", path, err)
			}
		})
	}
}

// A run that cannot read what it checks fails rather than passing.
func TestCheckPlaceholdersFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		edit func(fstest.MapFS)
	}{
		{name: "VERSIONING.md missing", edit: func(tree fstest.MapFS) { delete(tree, "VERSIONING.md") }},
		{name: "docs missing", edit: func(tree fstest.MapFS) {
			delete(tree, "docs/releasing.md")
			delete(tree, "docs/adr/0001-first.md")
		}},
		{name: "a link under docs", edit: func(tree fstest.MapFS) {
			tree["docs/link.md"] = &fstest.MapFile{Data: []byte("VERSIONING.md"), Mode: fs.ModeSymlink}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := settledTree()
			test.edit(tree)
			if err := CheckPlaceholders(tree); err == nil {
				t.Fatal("expected the check to fail")
			}
		})
	}
}
