package federation

import (
	"bytes"
	"runtime"
	"slices"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// allocatedBy returns the bytes allocated while call runs. It measures
// allocation, not time, so it holds on any machine; a fuzz worker runs one
// input at a time.
func allocatedBy(call func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	call()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// The allocation budget of one fuzzed call: a multiple of its input and a
// fixed part for the reader's own tables. A reader that allocates by what a
// document says rather than by its bytes is what this catches.
func withinBudget(t *testing.T, allocated uint64, input int, perByte, fixed uint64) {
	t.Helper()
	if budget := perByte*uint64(input) + fixed; allocated > budget {
		t.Fatalf("allocated %d bytes for %d bytes of input, over the budget of %d", allocated, input, budget)
	}
}

func checkReport(t *testing.T, report Report) {
	t.Helper()
	if len(report.Diagnostics) > MaxReportDiagnostics+1 {
		t.Fatalf("%d diagnostics", len(report.Diagnostics))
	}
	if !slices.IsSortedFunc(report.Diagnostics, func(a, b Diagnostic) int {
		if a.Code == "REG-REPORT-TRUNCATED" || b.Code == "REG-REPORT-TRUNCATED" {
			return 0
		}
		return compareDiagnostics(a, b)
	}) {
		t.Fatal("the diagnostics are not in the report's order")
	}
	errors := 0
	for _, d := range report.Diagnostics {
		if d.Message == "" || (d.Severity != SeverityError && d.Severity != SeverityWarning) {
			t.Fatalf("diagnostic %+v", d)
		}
		if d.Severity == SeverityError {
			errors++
		}
	}
	if report.Valid && errors > 0 {
		t.Fatal("a valid report with an error")
	}
}

func FuzzManifest(f *testing.F) {
	for _, name := range []string{"federation/two-sources.json", "federation/layer-role.json", "federation/names-a-commit.json", "federation/repository-userinfo.json"} {
		f.Add(testdataFile(f, name))
	}
	f.Add([]byte(`{"federationFormatVersion":"1.1.0","sources":[{"name":"a","name":"b"}]}`))
	f.Add([]byte(`{"federationFormatVersion":"1.1.0","environments":["dev","dev"],"sources":[{"kinds":["A","a"]}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var first, second []string
		allocated := allocatedBy(func() {
			collected := &collector{}
			readManifest(data, collected)
			first = found(collected.kept)
		})
		withinBudget(t, allocated, len(data), 64, 1<<20)
		collected := &collector{}
		readManifest(data, collected)
		second = found(collected.kept)
		if !slices.Equal(first, second) {
			t.Fatalf("two reads differ:\n%q\n%q", first, second)
		}
	})
}

func FuzzBounds(f *testing.F) {
	for _, name := range []string{"sources/platform/bounds.json", "sources/billing/bounds.json", "sources/billing/bounds-form.json", "sources/billing/bounds-pattern.json"} {
		f.Add(testdataFile(f, name))
	}
	f.Add([]byte(`{"boundsFormatVersion":"1.1.0","kinds":[{"kind":"a","fields":[{"pointer":"/*","bound":{"enum":[1,1.0],"maximum":1e308,"exclusiveMaximum":-0.5,"patterns":["^(a|b)*$"]},"environments":{"dev":{"type":["integer","number"]}}}]}]}`))
	values := []any{nil, true, "a", []any{"a", "a"}, map[string]any{"a": "b"}}
	f.Fuzz(func(t *testing.T, data []byte) {
		read := func() []string {
			root, refusals := registry.AdmitDocument(data, MaxBoundsBytes, "boundsFormatVersion", BoundsFormatVersion, registry.Location{Document: DocumentBounds})
			if root == nil {
				if len(refusals) == 0 {
					t.Fatal("a document refused with no diagnostic")
				}
				return nil
			}
			collected := &collector{}
			for _, standard := range []bool{true, false} {
				s := &source{name: "billing", role: RoleRepository, serves: []string{"dev"}}
				if standard {
					s.role = RoleStandard
				}
				b := readBounds(root, s, []string{"dev", "prod"}, collected)
				for _, k := range b.kinds {
					for _, f := range k.fields {
						for _, other := range k.fields {
							widenings(f.bound, other.bound)
							f.overlaps(other)
						}
						for _, env := range []string{"dev", "prod"} {
							for _, value := range values {
								f.admits(env, value)
							}
						}
					}
				}
			}
			return found(collected.kept)
		}
		var first []string
		withinBudget(t, allocatedBy(func() { first = read() }), len(data), 1024, 8<<20)
		if second := read(); !slices.Equal(first, second) {
			t.Fatalf("two reads differ:\n%q\n%q", first, second)
		}
	})
}

func FuzzIndex(f *testing.F) {
	in := twoSources(f)
	built := testdataFile(f, "assembled/two-sources/index.json")
	f.Add(built)
	f.Add(testdataFile(f, "vectors/index-check-other-difference/index.json"))
	f.Add([]byte(`{"indexFormatVersion":"1.1.0","sources":[{"documents":[{"path":"registry/bounds.json","contentDigest":7}]}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var report Report
		var err error
		allocated := allocatedBy(func() { report, err = CheckIndex(in, registry.Document{Data: data}) })
		if err != nil {
			t.Fatal(err)
		}
		withinBudget(t, allocated, len(data), 64, 32<<20)
		checkReport(t, report)
		again, err := CheckIndex(in, registry.Document{Data: data})
		if err != nil {
			t.Fatal(err)
		}
		first, _ := report.Canonical()
		second, _ := again.Canonical()
		if !bytes.Equal(first, second) {
			t.Fatalf("two checks differ:\n%s\n%s", first, second)
		}
		if report.Valid {
			// A check passes only for the index the assembly writes.
			root, _ := registry.AdmitDocument(data, MaxIndexBytes, "indexFormatVersion", IndexFormatVersion, registry.Location{Document: DocumentIndex})
			canonical, err := jcs.EncodeDocument(root)
			if err != nil || !bytes.Equal(canonical, built) {
				t.Fatalf("a check passed an index that is not the assembly's: %s", data)
			}
		}
	})
}

// FuzzAssembly assembles with the repository's registry and bounds fuzzed.
func FuzzAssembly(f *testing.F) {
	base := twoSources(f)
	f.Add(testdataFile(f, "sources/billing/registry.json"), testdataFile(f, "sources/billing/bounds.json"))
	f.Add(testdataFile(f, "sources/billing/registry.json"), testdataFile(f, "sources/billing/bounds-form.json"))
	f.Add([]byte(`{"registryFormatVersion":"1.0.0","id":"https://example.invalid/registries/billing","version":"1.0.0","environments":["dev","prod"],"owners":["a"],"kinds":[],"entries":[{"kind":"db-role","id":"x","owner":"a","value":{}}]}`), []byte(`{}`))
	f.Fuzz(func(t *testing.T, registryData, boundsData []byte) {
		in := Input{Manifest: base.Manifest, Sources: map[string]SourceInput{}}
		for name, s := range base.Sources {
			files := map[string]registry.Document{}
			for path, document := range s.Files {
				files[path] = document
			}
			in.Sources[name] = SourceInput{Commit: s.Commit, Files: files}
		}
		in.Sources["billing"].Files["registry/registry.json"] = registry.Document{Data: registryData}
		in.Sources["billing"].Files["registry/bounds.json"] = registry.Document{Data: boundsData}
		var out Assembly
		var err error
		allocated := allocatedBy(func() { out, err = Assemble(in) })
		if err != nil {
			t.Fatal(err)
		}
		withinBudget(t, allocated, len(registryData)+len(boundsData), 1024, 64<<20)
		checkReport(t, out.Report)
		if out.Report.Valid != (out.Registry != nil) || out.Report.Valid != (out.Index != nil) {
			t.Fatal("an assembly wrote its output without being valid, or the reverse")
		}
		again, err := Assemble(in)
		if err != nil {
			t.Fatal(err)
		}
		first, _ := out.Report.Canonical()
		second, _ := again.Report.Canonical()
		if !bytes.Equal(first, second) || !bytes.Equal(out.Registry, again.Registry) || !bytes.Equal(out.Index, again.Index) {
			t.Fatalf("two assemblies differ:\n%s\n%s", first, second)
		}
		if out.Report.Valid {
			if result := registry.Validate(out.Registry, nil); result.Report.Registry == nil || result.Report.Registry.Digest != out.Report.Registry.Digest {
				t.Fatal("the assembled registry is not the one the report names")
			}
		}
	})
}
