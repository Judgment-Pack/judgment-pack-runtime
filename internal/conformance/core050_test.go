package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// core050StrictFindings are the 0.5.0 cases the strict runner fails, each for
// a reason in the strict rule rather than in this reader. The one is the
// exact-overlap advisory case, which strict mode rejects as it rejects the
// released 0.3.0 case of the same name: the overlap is advice, outside
// conformance, and the document stays valid. This list is what keeps a new
// strict failure from passing unnoticed.
var core050StrictFindings = map[string]bool{
	"advisory-rules-overlap-exact": true,
}

func core050Manifest(t *testing.T) Manifest {
	t.Helper()
	set, err := artifacts.Load(artifacts.Core050Version)
	if err != nil {
		t.Fatal(err)
	}
	data, err := set.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

// extendedFixtures lists the 0.5.0 fixtures written for the extended
// profile, read from the fixtures themselves rather than written down here.
func extendedFixtures(t *testing.T, manifest Manifest) map[string]bool {
	t.Helper()
	set, err := artifacts.Load(artifacts.Core050Version)
	if err != nil {
		t.Fatal(err)
	}
	extended := map[string]bool{}
	for _, item := range manifest.Cases {
		fixture, err := set.Case(item.Path)
		if err != nil {
			t.Fatal(err)
		}
		if declaredProfile(fixture) == "extended" {
			extended[item.ID] = true
		}
	}
	return extended
}

// The 0.5.0 corpus runs under the base reader: 369 conformance cases and 5
// advisory cases (the release notes' counts), of which the 18 written for the
// extended profile are refused as profile-unsupported and reported skipped,
// and every other case passes, under strict too but for core050StrictFindings.
func TestCore050DocumentCasesPassUnderTheBaseReader(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	manifest := core050Manifest(t)
	extended := extendedFixtures(t, manifest)
	if len(manifest.Cases) != 374 || len(extended) != 18 {
		t.Fatalf("%d cases, %d extended; want 374 and 18", len(manifest.Cases), len(extended))
	}
	if !slices.Equal(manifest.ReaderProfiles, []string{"base", "extended"}) {
		t.Fatalf("readerProfiles = %v", manifest.ReaderProfiles)
	}
	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprintf("strict=%v", strict), func(t *testing.T) {
			output, failure := NewRunner(engine).RunWithOptions("", artifacts.Core050Version, strict)
			if failure != nil {
				t.Fatalf("0.5.0 run refused: %+v", failure)
			}
			if output.SuiteVersion != "0.5.0" || output.SpecVersion != "0.5.0" {
				t.Fatalf("suite=%q spec=%q", output.SuiteVersion, output.SpecVersion)
			}
			want := map[string]bool{}
			if strict {
				want = core050StrictFindings
			}
			if output.Summary.Total != 374 || output.Summary.Skipped != 18 || output.Summary.Mismatched != len(want) || output.Summary.Passed != 374-18-len(want) {
				t.Fatalf("summary %+v", output.Summary)
			}
			mismatched := map[string]bool{}
			for _, item := range output.Cases {
				switch item.Status {
				case "skipped":
					if !extended[item.ID] {
						t.Errorf("%s skipped, but its fixture declares base", item.ID)
					}
				case "mismatch":
					mismatched[item.ID] = true
					if extended[item.ID] {
						t.Errorf("%s: an extended fixture was read instead of refused", item.ID)
					}
				}
			}
			for id := range mismatched {
				if !want[id] {
					t.Errorf("%s mismatched", id)
				}
			}
			for id := range want {
				if !mismatched[id] {
					t.Errorf("%s no longer fails strict; remove it from core050StrictFindings", id)
				}
			}
		})
	}
}

// The one case that lists every finding it gives passes only when the reader
// gives both, and its report carries the list.
func TestCore050CaseWithTwoFindingsReportsBoth(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	output, failure := NewRunner(engine).RunWithOptions("", artifacts.Core050Version, true)
	if failure != nil {
		t.Fatal(failure)
	}
	const id = "structural-value-set-reference-and-value-operator-equals"
	want := []result.ExpectedDiagnostic{
		{Code: "JPS-STRUCTURE-VALUE-SET-REFERENCE", Path: "/rules/0/when"},
		{Code: "JPS-STRUCTURE-VALUE-SET-REFERENCE", Path: "/rules/0/when/operator"},
	}
	for _, item := range output.Cases {
		if item.ID != id {
			continue
		}
		if item.Status != "passed" || !slices.Equal(item.ExpectedDiagnostics, want) || len(item.StrayDiagnostics) != 0 {
			t.Fatalf("%s: %+v", id, item)
		}
		return
	}
	t.Fatalf("%s is not in the 0.5.0 run", id)
}

// Findings compare as a set of (code, pointer) pairs: a case that lists
// several needs every one of them, a finding reported twice is one, and under
// strict a finding none of them names is stray.
func TestExpectedFindingsAreASet(t *testing.T) {
	first := result.ExpectedDiagnostic{Code: "JPS-STRUCTURE-A", Path: "/a"}
	listed := []manifestDiagnostic{{Code: "JPS-STRUCTURE-A", Path: "/a"}, {Code: "JPS-STRUCTURE-B", Path: "/b"}}
	expected := expectedFindings(&first, listed)
	a := result.ErrorDiagnostic("JPS-STRUCTURE-A", "structural", "/a", "one")
	aAgain := result.ErrorDiagnostic("JPS-STRUCTURE-A", "structural", "/a", "the same finding in other words")
	b := result.ErrorDiagnostic("JPS-STRUCTURE-B", "structural", "/b", "two")
	c := result.ErrorDiagnostic("JPS-STRUCTURE-B", "structural", "/c", "elsewhere")
	if expectedFindingsPresent(expected, []result.Diagnostic{a}) {
		t.Fatal("a case listing two findings passed with one")
	}
	if !expectedFindingsPresent(expected, []result.Diagnostic{b, a}) {
		t.Fatal("the order of findings must carry no meaning")
	}
	if stray := strayDiagnostics(expected, []result.Diagnostic{a, aAgain, b}); len(stray) != 0 {
		t.Fatalf("a finding reported twice is one finding: %v", stray)
	}
	if stray := strayDiagnostics(expected, []result.Diagnostic{a, b, c}); len(stray) != 1 || stray[0].InstancePath != "/c" {
		t.Fatalf("stray = %v", stray)
	}
	// Without a list, the one expected diagnostic is the set.
	if !slices.Equal(expectedFindings(&first, nil), []result.ExpectedDiagnostic{first}) || expectedFindings(nil, nil) != nil {
		t.Fatal("a case without expectedDiagnostics expects its expectedDiagnostic alone")
	}
}

// A manifest whose list does not begin with the case's expectedDiagnostic is
// refused before any case runs.
func TestExpectedDiagnosticsMustBeginWithTheExpectedDiagnostic(t *testing.T) {
	manifest := core050Manifest(t)
	for index := range manifest.Cases {
		if len(manifest.Cases[index].ExpectedDiagnostics) == 0 {
			continue
		}
		if failure := checkCases(manifest); failure != nil {
			t.Fatalf("the bundled manifest was refused: %+v", failure)
		}
		listed := manifest.Cases[index].ExpectedDiagnostics
		manifest.Cases[index].ExpectedDiagnostics = []manifestDiagnostic{listed[1], listed[0]}
		if failure := checkCases(manifest); failure == nil || failure.Code != "JPS-SUITE-MANIFEST" {
			t.Fatalf("a list that does not begin with expectedDiagnostic: %+v", failure)
		}
		return
	}
	t.Fatal("no 0.5.0 case lists expectedDiagnostics")
}

// A local suite under 0.5.0 runs as the bundled one does: the same cases, the
// same profile skips and the same verdicts.
func TestCore050LocalSuiteRunsLikeTheBundledOne(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	set, err := artifacts.Load(artifacts.Core050Version)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	write := func(relative string, data []byte) {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifestBytes, err := set.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	schemaBytes, err := set.ManifestSchema()
	if err != nil {
		t.Fatal(err)
	}
	write("manifest.json", manifestBytes)
	write("manifest.schema.json", schemaBytes)
	for _, item := range core050Manifest(t).Cases {
		data, err := set.Case(item.Path)
		if err != nil {
			t.Fatal(err)
		}
		write(item.Path, data)
	}
	bundled, failure := NewRunner(engine).RunWithOptions("", artifacts.Core050Version, false)
	if failure != nil {
		t.Fatal(failure)
	}
	local, failure := NewRunner(engine).RunWithOptions(root, "", false)
	if failure != nil {
		t.Fatalf("local 0.5.0 suite refused: %+v", failure)
	}
	if local.Summary != bundled.Summary || local.CorpusDigest != bundled.CorpusDigest || local.Provenance != "local-suite" {
		t.Fatalf("local %+v %s, bundled %+v %s", local.Summary, local.CorpusDigest, bundled.Summary, bundled.CorpusDigest)
	}
}

// 0.4.0-draft, the working selector 0.5.0 was staged under, and 0.4.0, which
// no release assigns, are not bundled; the refusal says where the draft went.
func TestRetiredSelectorsAreRefusedAsUnbundled(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"0.4.0-draft", "0.4.0"} {
		_, failure := NewRunner(engine).RunWithOptions("", version, false)
		if failure == nil || failure.Code != "JPS-CAPABILITY-SPEC-VERSION" || failure.ExitCode != result.ExitUnsupported || !strings.Contains(failure.Message, "released as Core 0.5.0") {
			t.Fatalf("%s: %+v", version, failure)
		}
	}
	_, failure := NewRunner(engine).RunWithOptions("", "0.9.0", false)
	if failure == nil || failure.Message != "The exact JPS specification version is not bundled with this CLI." {
		t.Fatalf("another unbundled version must keep its message: %+v", failure)
	}
}

type finding struct{ code, path, severity string }

func findings(diagnostics []result.Diagnostic) []finding {
	out := make([]finding, 0, len(diagnostics))
	for _, d := range diagnostics {
		out = append(out, finding{d.Code, d.InstancePath, d.Severity})
	}
	slices.SortFunc(out, func(a, b finding) int {
		return cmpStrings(a.code+"\x00"+a.path+"\x00"+a.severity, b.code+"\x00"+b.path+"\x00"+b.severity)
	})
	return out
}

func cmpStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// TestReleasedCasesCarryOverTo050 holds Core 0.5.0 to 0.3.0's rules on the
// documents 0.3.0 describes: every released 0.3.0 case, its fixture's root
// selector bytes "specVersion": "0.3.0" replaced by "specVersion": "0.5.0",
// gives its released result and diagnostics under 0.5.0. A fixture that does not
// declare 0.3.0 is run unchanged. Status and every finding (code, pointer and
// severity, advice included) must be those the released fixture gets under
// 0.3.0, so the released runner's normal and strict verdicts both carry over.
func TestReleasedCasesCarryOverTo050(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	set, err := artifacts.Load(artifacts.CoreVersion)
	if err != nil {
		t.Fatal(err)
	}
	data, err := set.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	released, replaced := []byte(`"specVersion": "0.3.0"`), []byte(`"specVersion": "0.5.0"`)
	carried := 0
	for _, item := range manifest.Cases {
		fixture, err := set.Case(item.Path)
		if err != nil {
			t.Fatal(err)
		}
		draft := fixture
		if bytes.Contains(fixture, released) {
			draft = bytes.Replace(fixture, released, replaced, 1)
			carried++
		}
		options := func(version string) validation.Options {
			return validation.Options{Through: item.Layer, ExpectedSpecVersion: version, SupportedExtensions: item.SupportedExtensions, Limits: carrier.DefaultLimits()}
		}
		want, operational := engine.Validate(fixture, options(artifacts.CoreVersion))
		if operational != nil {
			t.Fatalf("%s under 0.3.0: %+v", item.ID, operational)
		}
		got, operational := engine.Validate(draft, options(artifacts.Core050Version))
		if operational != nil {
			t.Fatalf("%s under 0.5.0: %+v", item.ID, operational)
		}
		if got.Status != want.Status || !slices.Equal(findings(got.Diagnostics), findings(want.Diagnostics)) {
			t.Errorf("%s does not carry over:\n 0.3.0: %s %v\n 0.5.0: %s %v", item.ID, want.Status, findings(want.Diagnostics), got.Status, findings(got.Diagnostics))
		}
	}
	if carried < len(manifest.Cases)/2 {
		t.Fatalf("only %d of %d released fixtures declared the 0.3.0 selector bytes; the replacement is not exercised", carried, len(manifest.Cases))
	}
}
