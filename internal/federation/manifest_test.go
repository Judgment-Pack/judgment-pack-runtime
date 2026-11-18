package federation

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// A manifest's sources, as a test writes them.
func standardSource(name string, kinds ...string) map[string]any {
	s := map[string]any{"name": name, "role": RoleStandard, "repository": "https://example.invalid/scm/" + name, "registry": "registry.json"}
	if len(kinds) > 0 {
		s["kinds"] = kinds
	}
	return s
}

func repositorySource(name string) map[string]any {
	return map[string]any{"name": name, "role": RoleRepository, "repository": "https://example.invalid/scm/" + name, "registry": "registry.json"}
}

func testManifest(environments []string, sources ...map[string]any) map[string]any {
	return map[string]any{
		"federationFormatVersion": FederationFormatVersion,
		"registry":                map[string]any{"id": "https://example.invalid/registries/assembled", "version": "1.0.0"},
		"environments":            environments,
		"sources":                 sources,
	}
}

func encode(t testing.TB, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// found lists diagnostics as "CODE document source pointer".
func found(ds []Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, strings.Join(strings.Fields(fmt.Sprintf("%s %s %s %s", d.Code, d.Location.Document, d.Location.Source, d.Location.Pointer)), " "))
	}
	return out
}

func readTestManifest(t testing.TB, data []byte) (*manifest, []string) {
	t.Helper()
	collected := &collector{}
	m := readManifest(data, collected)
	return m, found(collected.kept)
}

func expectFound(t *testing.T, got []string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if !slices.Equal(got, want) {
		t.Errorf("diagnostics\n got %q\nwant %q", got, want)
	}
}

func TestManifestRefusesDuplicateMembers(t *testing.T) {
	for _, c := range []struct{ data, want string }{
		{`{"federationFormatVersion":"1.1.0","federationFormatVersion":"1.1.0"}`, "REG-DOC-DUPLICATE federation /federationFormatVersion"},
		{`{"federationFormatVersion":"1.1.0","registry":{"id":"https://example.invalid/r","id":"https://example.invalid/s","version":"1.0.0"},"environments":["dev"],"sources":[]}`, "REG-DOC-DUPLICATE federation /registry/id"},
	} {
		m, got := readTestManifest(t, []byte(c.data))
		if m != nil {
			t.Errorf("%s: a manifest with a repeated member was admitted", c.data)
		}
		expectFound(t, got, c.want)
	}
}

func TestManifestSizeBound(t *testing.T) {
	data := encode(t, testManifest([]string{"dev"}, standardSource("standard")))
	at := append(data, strings.Repeat(" ", MaxManifestBytes-len(data))...)
	if _, got := readTestManifest(t, at); len(got) != 0 {
		t.Errorf("a manifest at its bound: %q", got)
	}
	_, got := readTestManifest(t, append(at, ' '))
	expectFound(t, got, "REG-DOC-SIZE federation")
}

func TestManifestSourceCount(t *testing.T) {
	sources := []map[string]any{standardSource("s000")}
	for i := 1; i < MaxSources; i++ {
		sources = append(sources, repositorySource(fmt.Sprintf("s%03d", i)))
	}
	_, got := readTestManifest(t, encode(t, testManifest([]string{"dev"}, sources...)))
	expectFound(t, got)
	sources = append(sources, repositorySource("s999"))
	_, got = readTestManifest(t, encode(t, testManifest([]string{"dev"}, sources...)))
	expectFound(t, got, "FED-MANIFEST federation /sources")
}

func TestManifestPathCount(t *testing.T) {
	paths := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("kinds/k%02d.json", i)
		}
		return out
	}
	// 64 sources of 64 paths each: a registry and 63 kinds for each
	// repository, 64 kinds for the standard without a registry.
	standard := standardSource("s00", paths(MaxSourceKinds)...)
	delete(standard, "registry")
	sources := []map[string]any{standard}
	for i := 1; i < 64; i++ {
		s := repositorySource(fmt.Sprintf("s%02d", i))
		s["kinds"] = paths(MaxSourceKinds - 1)
		sources = append(sources, s)
	}
	_, got := readTestManifest(t, encode(t, testManifest([]string{"dev"}, sources...)))
	expectFound(t, got)
	sources = append(sources, repositorySource("s99"))
	_, got = readTestManifest(t, encode(t, testManifest([]string{"dev"}, sources...)))
	expectFound(t, got, "FED-MANIFEST federation /sources")
}

func TestManifestKindsPerSource(t *testing.T) {
	kinds := make([]string, MaxSourceKinds)
	for i := range kinds {
		kinds[i] = fmt.Sprintf("kinds/k%02d.json", i)
	}
	_, got := readTestManifest(t, encode(t, testManifest([]string{"dev"}, standardSource("standard", kinds...))))
	expectFound(t, got)
	_, got = readTestManifest(t, encode(t, testManifest([]string{"dev"}, standardSource("standard", append(kinds, "kinds/extra.json")...))))
	expectFound(t, got, "FED-MANIFEST federation /sources/0/kinds")
}

func TestManifestEnvironmentCount(t *testing.T) {
	environments := make([]string, MaxEnvironments)
	for i := range environments {
		environments[i] = fmt.Sprintf("e%02d", i)
	}
	_, got := readTestManifest(t, encode(t, testManifest(environments, standardSource("standard"))))
	expectFound(t, got)
	_, got = readTestManifest(t, encode(t, testManifest(append(environments, "e99"), standardSource("standard"))))
	expectFound(t, got, "FED-MANIFEST federation /environments")
}

// A repository serves a non-empty subset of the manifest's environments, in
// the manifest's order; the standard serves them all and states none.
func TestManifestServedEnvironments(t *testing.T) {
	environments := []string{"dev", "stage", "prod"}
	for _, c := range []struct {
		served []any
		want   []string
	}{
		{[]any{"dev", "prod"}, nil},
		{[]any{"stage"}, nil},
		{[]any{"dev", "stage", "prod"}, nil},
		{[]any{"prod", "dev"}, []string{"FED-MANIFEST federation /sources/0/environments"}},
		{[]any{"dev", "qa"}, []string{"FED-MANIFEST federation /sources/0/environments"}},
		{[]any{"dev", "dev"}, []string{"FED-MANIFEST federation /sources/0/environments"}},
		{[]any{}, []string{"FED-MANIFEST federation /sources/0/environments"}},
		{[]any{"dev", 1}, []string{"FED-MANIFEST federation /sources/0/environments"}},
	} {
		repository := repositorySource("billing")
		repository["environments"] = c.served
		m, got := readTestManifest(t, encode(t, testManifest(environments, repository, standardSource("standard"))))
		expectFound(t, got, c.want...)
		if c.want == nil {
			if served := m.sources[0].serves; !slices.Equal(anyStrings(served), c.served) {
				t.Errorf("%v: serves %v", c.served, served)
			}
		}
	}
	standard := standardSource("standard")
	standard["environments"] = []string{"dev"}
	_, got := readTestManifest(t, encode(t, testManifest(environments, standard)))
	expectFound(t, got, "FED-MANIFEST federation /sources/0/environments")
}

func TestManifestPaths(t *testing.T) {
	for _, c := range []struct {
		kinds []string
		want  []string
	}{
		{[]string{"kinds/a.json", "kinds/b.json"}, nil},
		// A control character, C1 as well as C0, is no path character.
		{[]string{"kinds/a\u0085.json"}, []string{"FED-MANIFEST federation /sources/0/kinds/0"}},
		{[]string{"kinds/a\x7f.json"}, []string{"FED-MANIFEST federation /sources/0/kinds/0"}},
		// Two paths of one source that fold to one name.
		{[]string{"kinds/a.json", "Kinds/A.json"}, []string{"FED-MANIFEST federation /sources/0/kinds/1"}},
		{[]string{"kinds/a.json", "kinds/a.json"}, []string{"FED-MANIFEST federation /sources/0/kinds/1"}},
		{[]string{"registry.json"}, []string{"FED-MANIFEST federation /sources/0/kinds/0"}},
		{[]string{"/kinds/a.json"}, []string{"FED-MANIFEST federation /sources/0/kinds/0"}},
		{[]string{"kinds/../a.json"}, []string{"FED-MANIFEST federation /sources/0/kinds/0"}},
		{[]string{strings.Repeat("a", MaxPathBytes)}, nil},
		{[]string{strings.Repeat("a", MaxPathBytes+1)}, []string{"FED-MANIFEST federation /sources/0/kinds/0"}},
	} {
		_, got := readTestManifest(t, encode(t, testManifest([]string{"dev"}, standardSource("standard", c.kinds...))))
		expectFound(t, got, c.want...)
	}
}

func TestManifestRepositoryIdentity(t *testing.T) {
	for _, c := range []struct {
		repository any
		ok         bool
	}{
		{"https://example.invalid/scm/billing", true},
		{"https://example.invalid/scm/a@b", true},
		{"ssh://git@example.invalid/scm/billing", false},
		{"https://example.invalid/scm/billing?ref=main", false},
		{"https://example.invalid/scm/billing#main", false},
		{"scm/billing", false},
		{"https://example.invalid/" + strings.Repeat("a", MaxRepositoryURI), false},
		{7, false},
	} {
		if got := validRepository(c.repository); got != c.ok {
			t.Errorf("%v: %v, want %v", c.repository, got, c.ok)
		}
	}
}

func TestManifestSourceNames(t *testing.T) {
	_, got := readTestManifest(t, encode(t, testManifest([]string{"dev"}, standardSource("standard"), repositorySource("billing"))))
	expectFound(t, got, "FED-MANIFEST federation /sources/1/name")
	_, got = readTestManifest(t, encode(t, testManifest([]string{"dev"}, repositorySource("billing"), repositorySource("billing"))))
	expectFound(t, got, "FED-MANIFEST federation /sources", "FED-MANIFEST federation /sources/1/name")
	two := standardSource("two")
	_, got = readTestManifest(t, encode(t, testManifest([]string{"dev"}, standardSource("one"), two)))
	expectFound(t, got, "FED-MANIFEST federation /sources/1/role")
}
