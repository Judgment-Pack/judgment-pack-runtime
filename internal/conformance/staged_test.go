package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// stagedStrictFindings are the staged cases that the strict runner fails at
// the vendored staged commit, each for a reason in the case rather than in
// this reader: the expected diagnostic is reported, and so is a second one the
// case does not expect. They are reported against the specification's staged
// cases; this list is what keeps a new strict failure from passing unnoticed.
var stagedStrictFindings = map[string]string{
	// The fixture's escalation declares neither target nor routes, so the
	// escalation's own oneOf fails too (JPS-STRUCTURE-ONEOF at /escalation).
	"structural-annotations-in-escalation": "JPS-STRUCTURE-ONEOF /escalation",
	// Both rules are `literal true` with different outcomes, which the
	// recommended overlap check reports as an exact overlap (advice, outside
	// conformance), as it does for one released 0.3.0 case.
	"valid-annotations-one-element-list": "JPS-RULES-OVERLAP /rules/0/when",
}

func stagedManifest(t *testing.T) Manifest {
	t.Helper()
	set, err := artifacts.Load(artifacts.StagedDraftVersion)
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

// extendedFixtures counts the staged fixtures written for the extended
// profile, read from the fixtures themselves rather than written down here.
func extendedFixtures(t *testing.T, manifest Manifest) map[string]bool {
	t.Helper()
	set, err := artifacts.Load(artifacts.StagedDraftVersion)
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

func TestStagedDocumentCasesPassUnderTheBaseReader(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	manifest := stagedManifest(t)
	cases := manifest.Cases
	extended := extendedFixtures(t, manifest)
	if len(extended) == 0 {
		t.Fatal("the staged cases carry no extended-profile fixture; the skip rule is untested")
	}
	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprintf("strict=%v", strict), func(t *testing.T) {
			output, failure := NewRunner(engine).RunWithOptions("", artifacts.StagedDraftVersion, strict)
			if failure != nil {
				t.Fatalf("staged run refused: %+v", failure)
			}
			if output.SuiteStatus != "staged" || output.SuiteVersion != "" || output.SpecVersion != artifacts.StagedDraftVersion {
				t.Fatalf("staged run is not labelled staged: status=%q suite=%q spec=%q", output.SuiteStatus, output.SuiteVersion, output.SpecVersion)
			}
			if output.Summary.Total != len(cases) || output.Summary.Skipped != len(extended) {
				t.Fatalf("summary %+v, want %d cases with %d skipped", output.Summary, len(cases), len(extended))
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
			want := map[string]bool{}
			if strict {
				for id := range stagedStrictFindings {
					want[id] = true
				}
			}
			for id := range mismatched {
				if !want[id] {
					t.Errorf("%s mismatched", id)
				}
			}
			for id := range want {
				if !mismatched[id] {
					t.Errorf("%s no longer fails strict; remove it from stagedStrictFindings", id)
				}
			}
		})
	}
}

func TestStagedSelectorIsRefusedForALocalSuite(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	_, failure := NewRunner(engine).RunWithOptions(t.TempDir(), artifacts.StagedDraftVersion, false)
	if failure == nil || failure.Code != "JPS-SUITE-STAGED" || failure.ExitCode != result.ExitInvocation {
		t.Fatalf("local suite under the staged selector: %+v", failure)
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

// TestReleasedCasesCarryOverToTheStagedSelector is the staged manifest's
// carriesOver rule: every released 0.3.0 case, its fixture's root selector
// bytes "specVersion": "0.3.0" replaced by "specVersion": "0.4.0-draft", gives
// its released result and diagnostics under the draft. A fixture that does not
// declare 0.3.0 is run unchanged. Status and every finding (code, pointer and
// severity, advice included) must be those the released fixture gets under
// 0.3.0, so the released runner's normal and strict verdicts both carry over.
func TestReleasedCasesCarryOverToTheStagedSelector(t *testing.T) {
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
	released, replaced := []byte(`"specVersion": "0.3.0"`), []byte(`"specVersion": "0.4.0-draft"`)
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
		got, operational := engine.Validate(draft, options(artifacts.StagedDraftVersion))
		if operational != nil {
			t.Fatalf("%s under the draft: %+v", item.ID, operational)
		}
		if got.Status != want.Status || !slices.Equal(findings(got.Diagnostics), findings(want.Diagnostics)) {
			t.Errorf("%s does not carry over:\n 0.3.0: %s %v\n draft: %s %v", item.ID, want.Status, findings(want.Diagnostics), got.Status, findings(got.Diagnostics))
		}
	}
	if carried < len(manifest.Cases)/2 {
		t.Fatalf("only %d of %d released fixtures declared the 0.3.0 selector bytes; the replacement is not exercised", carried, len(manifest.Cases))
	}
}
