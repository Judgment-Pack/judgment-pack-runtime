package emit

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestManifestRefusesEachMalformedMember(t *testing.T) {
	cases := []struct {
		name    string
		changes map[string]any
		want    string
	}{
		{"unknown member", map[string]any{"runtime": "wasm"}, "/runtime"},
		{"missing owns", map[string]any{"owns": nil}, "/owns"},
		{"selector", map[string]any{"emitterManifestVersion": "1.1.0"}, "/emitterManifestVersion"},
		{"id not absolute", map[string]any{"id": "emitters/test"}, "/id"},
		{"version with build metadata", map[string]any{"version": "1.0.0+build"}, "/version"},
		{"env name grammar", map[string]any{"env": []any{"lower_case"}}, "/env/0"},
		{"env reserved", map[string]any{"env": []any{"TZ"}}, "/env/0"},
		{"env duplicate", map[string]any{"env": []any{"A_B", "A_B"}}, "/env/1"},
		{"range without operator", map[string]any{"reads": []any{map[string]any{"kind": "queue", "id": "https://example.invalid/kinds/queue", "versions": "1.0.0"}}}, "/reads/0/versions"},
		{"empty range", map[string]any{"reads": []any{map[string]any{"kind": "queue", "id": "https://example.invalid/kinds/queue", "versions": ">=2.0.0 <1.0.0"}}}, "/reads/0/versions"},
		{"range with pre-release", map[string]any{"reads": []any{map[string]any{"kind": "queue", "id": "https://example.invalid/kinds/queue", "versions": "^1.0.0-rc.1"}}}, "/reads/0/versions"},
		{"read kind twice", map[string]any{"reads": []any{
			map[string]any{"kind": "queue", "id": "https://example.invalid/kinds/queue", "versions": "^1.0.0"},
			map[string]any{"kind": "queue", "id": "https://example.invalid/kinds/queue", "versions": "^2.0.0"}}}, "/reads/1/kind"},
		{"command absolute", map[string]any{"command": []any{"/bin/emitter"}}, "/command/0"},
		{"command climbs out", map[string]any{"command": []any{"../bin/emitter"}}, "/command/0"},
		{"command argument climbs out", map[string]any{"command": []any{"bin/emitter", "../../secret"}}, "/command/1"},
		{"command argument absolute", map[string]any{"command": []any{"bin/emitter", "/etc"}}, "/command/1"},
		{"command empty", map[string]any{"command": []any{}}, "/command"},
		{"command program dot", map[string]any{"command": []any{".."}}, "/command/0"},
		{"owns climbs out", map[string]any{"owns": []any{"../gen/**"}}, "/owns/0"},
		{"owns absolute", map[string]any{"owns": []any{"/gen/**"}}, "/owns/0"},
		{"owns partial double star", map[string]any{"owns": []any{"gen/a**"}}, "/owns/0"},
		{"owns backslash", map[string]any{"owns": []any{`gen\**`}}, "/owns/0"},
		{"owns everything", map[string]any{"owns": []any{"gen/**", "**"}}, "/owns/1"},
		// Each of these owns every path, or every top-level file, as "**"
		// does: no owns glob may begin with a segment that holds a "*".
		{"owns everything below any file", map[string]any{"owns": []any{"gen/**", "**/*"}}, "/owns/1"},
		{"owns everything twice", map[string]any{"owns": []any{"gen/**", "**/**"}}, "/owns/1"},
		{"owns everything below any directory", map[string]any{"owns": []any{"gen/**", "*/**"}}, "/owns/1"},
		{"owns every top-level file", map[string]any{"owns": []any{"gen/**", "*"}}, "/owns/1"},
		{"owns a wildcard top-level name", map[string]any{"owns": []any{"gen/**", "g*/a"}}, "/owns/1"},
		{"owns git's directory", map[string]any{"owns": []any{".git/**"}}, "/owns/0"},
		{"owns git's directory in another case", map[string]any{"owns": []any{"sub/.GIT/*"}}, "/owns/0"},
		{"environments with facts contracts", map[string]any{"input": "factsContracts"}, "/environments"},
		{"environments missing", map[string]any{"environments": nil}, "/environments"},
		{"environments value", map[string]any{"environments": "some"}, "/environments"},
		{"options keyword outside the subset", map[string]any{"options": map[string]any{"type": "object", "format": "email"}}, "/options/format"},
		{"deprecated not boolean", map[string]any{"deprecated": "yes"}, "/deprecated"},
		{"request versions empty", map[string]any{"requestVersions": []any{}}, "/requestVersions"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, err := json.Marshal(manifestDoc([]string{"exit", "0"}, c.changes))
			if err != nil {
				t.Fatal(err)
			}
			m, diagnostics := ReadManifest(data)
			if m != nil || !slices.Contains(codes(diagnostics), "EMIT-MANIFEST@"+c.want) {
				t.Fatalf("got %v, want EMIT-MANIFEST at %s", codes(diagnostics), c.want)
			}
		})
	}
	for _, text := range []string{`[]`, `{"emitterManifestVersion":"1.0.0","emitterManifestVersion":"1.0.0"}`, `not json`} {
		if m, diagnostics := ReadManifest([]byte(text)); m != nil || len(diagnostics) == 0 || diagnostics[0].Code != "EMIT-MANIFEST" {
			t.Errorf("%s: got %v", text, codes(diagnostics))
		}
	}
}

func TestManifestAcceptsItsOptionalMembers(t *testing.T) {
	m := mustManifest(t, manifestDoc([]string{"exit", "0"}, map[string]any{
		"description": "Synthetic.", "env": []any{"EMIT_TOKEN_NAME"}, "deprecated": true,
		"command": []any{"./bin/emitter", "--flag=value"},
		"options": map[string]any{"type": "object", "properties": map[string]any{"schemaName": map[string]any{"type": "string"}}},
	}))
	if !m.Deprecated || m.Input != InputResolved || !slices.Equal(m.Env, []string{"EMIT_TOKEN_NAME"}) || m.Command[0] != "./bin/emitter" {
		t.Fatalf("manifest: %+v", m)
	}
	// An owns glob whose first segment is a literal is admitted.
	owns := []any{"gen/**", "db/*.sql", "a/**/b", "README.md"}
	m = mustManifest(t, manifestDoc([]string{"exit", "0"}, map[string]any{"owns": owns}))
	if len(m.Owns) != len(owns) || m.Owns[0].String() != "gen/**" {
		t.Fatalf("owns: %v", m.Owns)
	}
	facts := mustManifest(t, manifestDoc([]string{"exit", "0"}, map[string]any{"input": "factsContracts", "environments": nil}))
	if facts.Input != InputFactsContracts || facts.Environments != "" {
		t.Fatalf("facts contracts manifest: %+v", facts)
	}
}

func TestRangeContains(t *testing.T) {
	cases := []struct {
		versions string
		in, out  []string
	}{
		{"^1.2.0", []string{"1.2.0", "1.2.9", "1.99.0"}, []string{"1.1.9", "2.0.0", "1.3.0-rc.1", "0.9.0"}},
		{"^0.2.3", []string{"0.2.3", "0.2.10"}, []string{"0.3.0", "0.2.2", "1.0.0"}},
		{"^0.0.3", []string{"0.0.3"}, []string{"0.0.4", "0.0.2"}},
		{"~1.2.0", []string{"1.2.0", "1.2.7"}, []string{"1.3.0", "1.1.0"}},
		{"=1.2.0", []string{"1.2.0"}, []string{"1.2.1", "1.2.0-rc.1"}},
		{">=1.2.0 <1.4.0", []string{"1.2.0", "1.3.9"}, []string{"1.4.0", "1.1.99"}},
		{"^99999999999999999999.0.0", []string{"99999999999999999999.5.0"}, []string{"100000000000000000000.0.0", "9999999999999999999.0.0"}},
		{"~1.9.0", []string{"1.9.1"}, []string{"1.10.0"}},
	}
	for _, c := range cases {
		r, err := ParseRange(c.versions)
		if err != nil {
			t.Fatalf("%s: %v", c.versions, err)
		}
		for _, v := range c.in {
			if !r.Contains(v) {
				t.Errorf("%s should contain %s", c.versions, v)
			}
		}
		for _, v := range c.out {
			if r.Contains(v) {
				t.Errorf("%s should not contain %s", c.versions, v)
			}
		}
	}
	for _, bad := range []string{"", "1.2.0", "^1.2", "^01.2.0", ">=1.2.0  <2.0.0", ">=1.2.0<2.0.0", "<2.0.0", ">=1.0.0 <1.0.0", "^1.2.0 ", "x1.2.0"} {
		if _, err := ParseRange(bad); err == nil {
			t.Errorf("%q parsed as a range", bad)
		}
	}
}

func TestGlobMatchBelowAndOverlap(t *testing.T) {
	glob := func(source string) Glob {
		g, err := parseGlob(source)
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		return g
	}
	matches := []struct {
		glob, path string
		want       bool
	}{
		// A trailing "**" matches one or more segments, as in gitignore:
		// gen/** never owns a file named gen. One in the middle matches zero
		// or more: a/**/b matches a/b.
		{"gen/**", "gen/a", true}, {"gen/**", "gen/a/b/c", true}, {"gen/**", "gen", false}, {"gen/**", "other/a", false},
		{"gen/**", "gen/.hidden", true}, {"a/**/b/**", "a/b", false}, {"a/**/b/**", "a/b/c", true}, {"**", "a", true},
		{"gen/*.sql", "gen/roles.sql", true}, {"gen/*.sql", "gen/a/roles.sql", false}, {"gen/*.sql", "gen/.sql", true},
		{"**/x.json", "x.json", true}, {"**/x.json", "a/b/x.json", true}, {"a/**/b", "a/b", true}, {"a/**/b", "a/x/y/b", true},
		{"gen/a*b*c", "gen/abc", true}, {"gen/a*b*c", "gen/axxbyyc", true}, {"gen/a*b*c", "gen/acb", false},
	}
	for _, c := range matches {
		if got := glob(c.glob).Match(c.path); got != c.want {
			t.Errorf("%s matches %s: %v, want %v", c.glob, c.path, got, c.want)
		}
	}
	below := []struct {
		glob, dir string
		want      bool
	}{
		{"gen/**", "", true}, {"gen/**", "gen", true}, {"gen/**", "gen/a/b", true}, {"gen/**", "src", false},
		{"gen/*.sql", "gen", true}, {"gen/*.sql", "gen/sub", false}, {"**/x.json", "deep/er", true}, {"gen/a", "gen/a", false},
	}
	for _, c := range below {
		if got := glob(c.glob).MatchBelow(c.dir); got != c.want {
			t.Errorf("%s below %q: %v, want %v", c.glob, c.dir, got, c.want)
		}
	}
	overlaps := []struct {
		a, b string
		want bool
	}{
		{"gen/**", "gen/db/*.sql", true}, {"gen/a/**", "gen/b/**", false}, {"**", "x", true}, {"*.go", "*_test.go", true},
		{"gen/*.sql", "gen/*.json", false}, {"a*", "*b", true}, {"ab*", "ac*", false}, {"**/a", "b/**", true},
		{"gen/x", "gen/x/**", false}, {"gen/x/a", "gen/x/**", true}, {"gen/x", "gen/x/*", false}, {"gen/**", "gen", false},
		{"gen/**", "gen/**/a", true}, {"a/**/b", "a/b", true}, {"a/**/b", "a/**", true}, {"gen/*/a", "gen/b/*", true}, {"gen/**/z", "gen/*/y", false},
	}
	for _, c := range overlaps {
		if got := glob(c.a).Overlaps(glob(c.b)); got != c.want {
			t.Errorf("%s overlaps %s: %v, want %v", c.a, c.b, got, c.want)
		}
		if got := glob(c.b).Overlaps(glob(c.a)); got != c.want {
			t.Errorf("%s overlaps %s (reversed): %v, want %v", c.b, c.a, got, c.want)
		}
	}
	for _, bad := range []string{"", "/gen", "gen/", "gen//a", "gen/../a", "~/gen", "gen/***", "C:/gen", strings.Repeat("a", MaxPathBytes+1)} {
		if _, err := parseGlob(bad); err == nil {
			t.Errorf("%q parsed as a glob", bad)
		}
	}
}
