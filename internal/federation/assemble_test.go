package federation

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

func testdataFile(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "1.1.0", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// twoSources is the inputs of the row assemble-two-sources, an assembly
// with no error.
func twoSources(t testing.TB) Input {
	t.Helper()
	files := func(paths map[string]string) map[string]registry.Document {
		out := map[string]registry.Document{}
		for path, file := range paths {
			out[path] = registry.Document{Data: testdataFile(t, file)}
		}
		return out
	}
	return Input{
		Manifest: registry.Document{Data: testdataFile(t, "federation/two-sources.json")},
		Sources: map[string]SourceInput{
			"billing": {Commit: "acd14c7a6c04c1dd6dc6c2d66d487a28667c0ad6", Files: files(map[string]string{
				"registry/bounds.json": "sources/billing/bounds.json", "registry/registry.json": "sources/billing/registry.json",
			})},
			"standard": {Commit: "d294fcce0cc88587843099d85dd805aeef1b09a63b0db1dd3e4dc62a343c1db5", Files: files(map[string]string{
				"bounds.json": "sources/platform/bounds.json", "kinds/database.json": "kinds/database.json",
				"kinds/db-role.json": "kinds/db-role.json", "registry.json": "sources/platform/registry.json",
			})},
		},
	}
}

func assemble(t testing.TB, in Input) Assembly {
	t.Helper()
	out, err := Assemble(in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// padded is a JSON document with white space after it up to n bytes.
func padded(t testing.TB, data []byte, n int) []byte {
	t.Helper()
	if len(data) > n {
		t.Fatalf("a document of %d bytes is over %d already", len(data), n)
	}
	return append(bytes.Clone(data), bytes.Repeat([]byte(" "), n-len(data))...)
}

// Each document a source supplies is admitted at its bound and refused one
// byte over it, located in the source; a kind declaration's refusal names
// no kindDocument, since its bytes past the bound are not its digest's.
func TestSourceDocumentBounds(t *testing.T) {
	for _, c := range []struct {
		source, path string
		bound        int
		want         string
	}{
		{"billing", "registry/registry.json", MaxRegistryBytes, "REG-DOC-SIZE registry billing"},
		{"billing", "registry/bounds.json", MaxBoundsBytes, "REG-DOC-SIZE bounds billing"},
		{"standard", "kinds/db-role.json", MaxKindBytes, "REG-DOC-SIZE kind standard"},
	} {
		in := twoSources(t)
		document := in.Sources[c.source].Files[c.path]
		in.Sources[c.source].Files[c.path] = registry.Document{Data: padded(t, document.Data, c.bound)}
		if out := assemble(t, in); !out.Report.Valid {
			t.Errorf("%s at its bound: %q", c.path, found(out.Report.Diagnostics))
		}
		for _, over := range []registry.Document{
			{Data: padded(t, document.Data, c.bound+1)},
			{Data: document.Data[:1], Oversize: true},
		} {
			in.Sources[c.source].Files[c.path] = over
			out := assemble(t, in)
			expectFound(t, found(out.Report.Diagnostics), c.want)
			if out.Report.Valid || out.Registry != nil || out.Index != nil {
				t.Errorf("%s over its bound: valid %v", c.path, out.Report.Valid)
			}
			for _, d := range out.Report.Diagnostics {
				if d.Location.KindDocument != "" {
					t.Errorf("%s over its bound names a kindDocument", c.path)
				}
			}
		}
	}
}

func TestManifestDocumentBound(t *testing.T) {
	in := twoSources(t)
	data := in.Manifest.Data
	in.Manifest = registry.Document{Data: padded(t, data, MaxManifestBytes)}
	if out := assemble(t, in); !out.Report.Valid {
		t.Errorf("a manifest at its bound: %q", found(out.Report.Diagnostics))
	}
	for _, over := range []registry.Document{{Data: padded(t, data, MaxManifestBytes+1)}, {Data: data[:1], Oversize: true}} {
		in.Manifest = over
		expectFound(t, found(assemble(t, in).Report.Diagnostics), "REG-DOC-SIZE federation")
	}
}

// A known index or lock names its registry when admitted under its bound;
// one over it is not admitted and names nothing (§4: no code refuses one).
func TestKnownDocumentBounds(t *testing.T) {
	in := twoSources(t)
	identity := `{"%s":"1.1.0","registry":{"id":"https://example.invalid/registries/billing"}}`
	for _, c := range []struct {
		member string
		bound  int
		set    func(*Known, registry.Document)
	}{
		{"indexFormatVersion", MaxIndexBytes, func(k *Known, d registry.Document) { k.Indexes = []registry.Document{d} }},
		{"lockFormatVersion", MaxLockBytes, func(k *Known, d registry.Document) { k.Locks = []registry.Document{d} }},
	} {
		data := []byte(fmt.Sprintf(identity, c.member))
		c.set(&in.Known, registry.Document{Data: padded(t, data, c.bound)})
		expectFound(t, found(assemble(t, in).Report.Diagnostics), "FED-SOURCE-ASSEMBLED registry billing")
		c.set(&in.Known, registry.Document{Data: padded(t, data, c.bound+1)})
		expectFound(t, found(assemble(t, in).Report.Diagnostics))
		in.Known = Known{}
	}
}

func TestKnownCount(t *testing.T) {
	in := twoSources(t)
	placeholder := registry.Document{Data: []byte(`{}`)}
	for _, c := range []struct {
		indexes, locks int
		want           []string
	}{
		{MaxKnown, MaxKnown, nil},
		{MaxKnown + 1, 0, []string{"FED-KNOWN-LIMIT federation"}},
		{0, MaxKnown + 1, []string{"FED-KNOWN-LIMIT federation"}},
	} {
		in.Known = Known{}
		for range c.indexes {
			in.Known.Indexes = append(in.Known.Indexes, placeholder)
		}
		for range c.locks {
			in.Known.Locks = append(in.Known.Locks, placeholder)
		}
		expectFound(t, found(assemble(t, in).Report.Diagnostics), c.want...)
	}
}

// A validation inside the assembly that lists only its first diagnostics
// and is not valid fails the assembly, whichever errors it left out; one
// that is valid does not.
func TestTruncatedValidationFailsClosed(t *testing.T) {
	truncated := registry.Diagnostic{Code: "REG-REPORT-TRUNCATED", Severity: SeverityWarning, Message: messageTruncated, Location: registry.Location{Document: registry.DocumentRegistry}}
	warning := registry.Diagnostic{Code: "REG-KIND-DEPRECATED", Severity: SeverityWarning, Location: registry.Location{Document: registry.DocumentRegistry, Pointer: "/kinds/0"}}
	for _, c := range []struct {
		valid bool
		fails bool
	}{{false, true}, {true, false}} {
		a := &assembly{collected: &collector{}}
		a.insertValidation(registry.Report{Valid: c.valid, Diagnostics: []registry.Diagnostic{warning, truncated}}, func(d registry.Diagnostic) (Diagnostic, bool) {
			return fromRegistry(d, "billing"), true
		})
		report := a.collected.report("")
		if a.collected.hasErrors() != c.fails || report.Valid == c.fails {
			t.Errorf("valid %v: the assembly fails %v", c.valid, a.collected.hasErrors())
		}
		if last := report.Diagnostics[len(report.Diagnostics)-1]; last.Code != "REG-REPORT-TRUNCATED" || len(report.Diagnostics) != 2 {
			t.Errorf("valid %v: %q", c.valid, found(report.Diagnostics))
		}
	}
}

// The sources' scopes are read together: a listing is shared when any
// earlier source lists the project for another environment, so a third
// listing for the first environment is shared with the second's.
func TestScopesSharedAcrossSources(t *testing.T) {
	scopes := func(env string) map[string]any {
		return map[string]any{"secretScopes": map[string]any{env: map[string]any{"gcp-sm": []any{"billing-dev-1"}}}}
	}
	for _, c := range []struct {
		environments []string
		want         []string
	}{
		{[]string{"dev", "dev"}, nil},
		{[]string{"dev", "prod"}, []string{"REF-SECRET-SCOPE-SHARED registry b /secretScopes/prod/gcp-sm/0"}},
		{[]string{"dev", "prod", "dev"}, []string{"REF-SECRET-SCOPE-SHARED registry b /secretScopes/prod/gcp-sm/0", "REF-SECRET-SCOPE-SHARED registry c /secretScopes/dev/gcp-sm/0"}},
		{[]string{"dev", "dev", "prod"}, []string{"REF-SECRET-SCOPE-SHARED registry c /secretScopes/prod/gcp-sm/0"}},
	} {
		a := &assembly{collected: &collector{}, manifest: &manifest{}}
		for i, env := range c.environments {
			a.manifest.sources = append(a.manifest.sources, &source{name: string(rune('a' + i)), registryRoot: scopes(env)})
		}
		a.sharedScopes()
		expectFound(t, found(a.collected.kept), c.want...)
		for _, d := range a.collected.kept {
			if d.Message != messageScopeShared {
				t.Errorf("message %q", d.Message)
			}
		}
	}
}

// The messages of the format 1.0.0 codes this package raises itself are
// the ones a 1.0.0 validation reports.
func TestRegistryMessagesAreTheReaders(t *testing.T) {
	message := func(report registry.Report, code string) string {
		for _, d := range report.Diagnostics {
			if d.Code == code {
				return d.Message
			}
		}
		t.Fatalf("no %s in %+v", code, report.Diagnostics)
		return ""
	}
	if got := message(registry.ValidateDocuments(registry.Document{Oversize: true}, nil).Report, "REG-DOC-SIZE"); got != messageDocSize {
		t.Errorf("REG-DOC-SIZE: %q", got)
	}
	_, refusals := registry.AdmitDocument([]byte(`{"a":-0}`), 64, "a", "", registry.Location{Document: DocumentIndex})
	if got := message(registry.Report{Diagnostics: refusals}, "REG-DOC-NUMBER"); got != messageDocNumber {
		t.Errorf("REG-DOC-NUMBER: %q", got)
	}
	shared := `{"registryFormatVersion":"1.0.0","id":"https://example.invalid/registries/r","version":"1.0.0","environments":["dev","prod"],
		"owners":["team-a"],"kinds":[],"entries":[],"secretScopes":{"dev":{"gcp-sm":["billing-dev-1"]},"prod":{"gcp-sm":["billing-dev-1"]}}}`
	if got := message(registry.Validate([]byte(shared), nil).Report, "REF-SECRET-SCOPE-SHARED"); got != messageScopeShared {
		t.Errorf("REF-SECRET-SCOPE-SHARED: %q", got)
	}
	entries := strings.Repeat(`{"x":1},`, MaxReportDiagnostics) + `{"x":1}`
	many := `{"registryFormatVersion":"1.0.0","id":"https://example.invalid/registries/r","version":"1.0.0","environments":["dev"],
		"owners":["team-a"],"kinds":[],"entries":[` + entries + `]}`
	if got := message(registry.Validate([]byte(many), nil).Report, "REG-REPORT-TRUNCATED"); got != messageTruncated {
		t.Errorf("REG-REPORT-TRUNCATED: %q", got)
	}
}
