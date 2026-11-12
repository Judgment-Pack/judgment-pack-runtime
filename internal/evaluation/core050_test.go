package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// A Core 0.5.0 pack is evaluated as its evaluation form (Core §8.5).
// These packs are written by hand twice: once with value sets, references and
// annotations, and once as the inline twin a reader would write without them.
// The expectation is that the two evaluate identically, trace included; no
// expectation here is recorded from a run.

// core050PackText writes the referencing pack: a reference in the applicability,
// under all and not in one rule's when, under any in another's, and in an
// exception's when that does not match; annotations on the pack, an outcome, a
// rule and a value-set entry; and one set no condition references.
func core050PackText(extra string) string {
	return `{"specVersion":"0.5.0","id":"https://example.com/packs/core050","version":"1.0.0","title":"Core 0.5.0",` +
		`"decision":{"intent":"Review","question":"Review?"},` +
		`"extensions":{"org.judgmentpack.annotations":{"owner":"intake-team"}},` +
		`"valueSets":[` +
		`{"id":"kinds","entries":[{"value":"intake"},{"value":"renewal","description":"Renewal","extensions":{"org.judgmentpack.annotations":{"tier":"two"}}}]},` +
		`{"id":"tiers","entries":[{"value":"tier-1"},{"value":"tier-2"}]},` +
		`{"id":"regions","entries":[{"value":"us"}]}` + extra + `],` +
		`"applicability":{"op":"fact","path":"/request/kind","operator":"in","valueSet":"kinds"},` +
		`"outcomes":[{"id":"accept","label":"Accept","extensions":{"org.judgmentpack.annotations":{"category":["a","b"]}}},{"id":"refer","label":"Refer"}],` +
		`"rules":[` +
		`{"id":"tiered","description":"Tiered","when":{"op":"all","conditions":[{"op":"fact","path":"/request/tier","operator":"in","valueSet":"tiers"},{"op":"not","condition":{"op":"fact","path":"/request/region","operator":"in","valueSet":"regions"}}]},"outcome":"accept","onUnknown":"ignore","extensions":{"org.judgmentpack.annotations":{"category":"core"},"com.example.note":{"text":"kept"}}},` +
		`{"id":"regional","description":"Regional","when":{"op":"any","conditions":[{"op":"fact","path":"/request/region","operator":"in","valueSet":"regions"}]},"outcome":"refer","onUnknown":"ignore"}],` +
		`"exceptions":[{"id":"blocked","description":"Blocked","when":{"op":"fact","path":"/request/region","operator":"in","valueSet":"regions"},"effect":"escalate","onUnknown":"ignore"}]}`
}

// inlineTwinText is the same pack written without sets or annotations.
func inlineTwinText(specVersion string) string {
	return `{"specVersion":"` + specVersion + `","id":"https://example.com/packs/core050","version":"1.0.0","title":"Core 0.5.0",` +
		`"decision":{"intent":"Review","question":"Review?"},` +
		`"applicability":{"op":"fact","path":"/request/kind","operator":"in","value":["intake","renewal"]},` +
		`"outcomes":[{"id":"accept","label":"Accept"},{"id":"refer","label":"Refer"}],` +
		`"rules":[` +
		`{"id":"tiered","description":"Tiered","when":{"op":"all","conditions":[{"op":"fact","path":"/request/tier","operator":"in","value":["tier-1","tier-2"]},{"op":"not","condition":{"op":"fact","path":"/request/region","operator":"in","value":["us"]}}]},"outcome":"accept","onUnknown":"ignore","extensions":{"com.example.note":{"text":"kept"}}},` +
		`{"id":"regional","description":"Regional","when":{"op":"any","conditions":[{"op":"fact","path":"/request/region","operator":"in","value":["us"]}]},"outcome":"refer","onUnknown":"ignore"}],` +
		`"exceptions":[{"id":"blocked","description":"Blocked","when":{"op":"fact","path":"/request/region","operator":"in","value":["us"]},"effect":"escalate","onUnknown":"ignore"}]}`
}

var core050Facts = []string{
	`{"request":{"kind":"intake","tier":"tier-2","region":"eu"}}`,
	`{"request":{"kind":"renewal","tier":"tier-1","region":"us"}}`,
	`{"request":{"kind":"other","tier":"tier-1","region":"eu"}}`,
	`{"request":{"kind":"intake","tier":"tier-9","region":"eu"}}`,
	`{"request":{"kind":"intake","region":"eu"}}`,
}

func evaluationShape(t *testing.T, evaluated result.Evaluation) string {
	t.Helper()
	disposition, err := evaluated.Disposition.CanonicalWith(evaluated.Disposition.Contract)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := json.Marshal(evaluated.Trace)
	if err != nil {
		t.Fatal(err)
	}
	contributors, err := json.Marshal(evaluated.Contributors)
	if err != nil {
		t.Fatal(err)
	}
	return string(disposition) + "\n" + string(trace) + "\n" + string(contributors)
}

func TestCore050PackEvaluatesAsItsInlineTwin(t *testing.T) {
	engine := newTestEngine(t)
	pack := []byte(core050PackText(""))
	twins := map[string][]byte{
		artifacts.Core050Version: []byte(inlineTwinText(artifacts.Core050Version)),
		artifacts.CoreVersion:    []byte(inlineTwinText(artifacts.CoreVersion)),
	}
	kinds := map[string]bool{}
	for index, facts := range core050Facts {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			got, failure := engine.EvaluateWith(pack, []byte(facts), nil, Options{SpecVersion: artifacts.Core050Version})
			if failure != nil {
				t.Fatalf("0.5.0 pack refused: %+v", failure)
			}
			if got.SpecVersion != artifacts.Core050Version || got.EvaluatorSpecVersion != artifacts.Core050Version {
				t.Fatalf("versions %q/%q", got.SpecVersion, got.EvaluatorSpecVersion)
			}
			kinds[got.Disposition.Kind] = true
			for version, twin := range twins {
				want, failure := engine.EvaluateWith(twin, []byte(facts), nil, Options{SpecVersion: version})
				if failure != nil {
					t.Fatalf("%s twin refused: %+v", version, failure)
				}
				if evaluationShape(t, got) != evaluationShape(t, want) {
					t.Fatalf("0.5.0 pack and its %s inline twin differ:\n0.5.0: %s\ntwin:   %s", version, evaluationShape(t, got), evaluationShape(t, want))
				}
			}
		})
	}
	// The facts reach an outcome, not-applicable and unresolved, so each
	// expansion position decides some row.
	for _, kind := range []string{"outcome", "not-applicable", "unresolved"} {
		if !kinds[kind] {
			t.Errorf("no row produced a %s disposition; the rows do not exercise every position", kind)
		}
	}
}

func TestCore050PackIdentityIsItsExactBytes(t *testing.T) {
	engine := newTestEngine(t)
	pack := []byte(core050PackText(""))
	admitted := engine.AdmitPack(pack)
	if admitted.Digest() != sha256.Sum256(pack) {
		t.Fatal("the admission digest is not over the pack's exact bytes")
	}
	evaluated, failure := engine.EvaluateAdmitted(admitted, []byte(core050Facts[0]), nil, Options{SpecVersion: artifacts.Core050Version})
	if failure != nil {
		t.Fatal(failure)
	}
	if evaluated.PackID != "https://example.com/packs/core050" || evaluated.PackVersion != "1.0.0" {
		t.Fatalf("identity %q %q", evaluated.PackID, evaluated.PackVersion)
	}
	if evaluated.Artifact == nil || evaluated.Artifact.SpecVersion != artifacts.Core050Version {
		t.Fatalf("artifact %+v", evaluated.Artifact)
	}
	// A pack differing only in an annotation is another pack: another digest,
	// the same evaluation.
	relabelled := bytes.Replace(pack, []byte(`"owner":"intake-team"`), []byte(`"owner":"review-team"`), 1)
	other := engine.AdmitPack(relabelled)
	if other.Digest() == admitted.Digest() {
		t.Fatal("an annotation change did not change the digest")
	}
	again, failure := engine.EvaluateAdmitted(other, []byte(core050Facts[0]), nil, Options{SpecVersion: artifacts.Core050Version})
	if failure != nil {
		t.Fatal(failure)
	}
	if evaluationShape(t, again) != evaluationShape(t, evaluated) {
		t.Fatal("an annotation changed the evaluation")
	}
}

// Document is the pack as written, never its evaluation form: the value sets,
// the references and the annotations are there, so a reader such as the pack
// view shows what the author wrote. The expected members are written here by
// hand from core050PackText.
func TestCore050DocumentIsTheDocumentAsWritten(t *testing.T) {
	engine := newTestEngine(t)
	admitted := engine.AdmitPack([]byte(core050PackText("")))
	document, failure := admitted.Document(Options{SpecVersion: artifacts.Core050Version})
	if failure != nil {
		t.Fatal(failure)
	}
	sets, ok := document["valueSets"].([]any)
	if !ok || len(sets) != 3 {
		t.Fatalf("the document's value sets are %#v", document["valueSets"])
	}
	applicability := document["applicability"].(map[string]any)
	if applicability["valueSet"] != "kinds" {
		t.Fatalf("the applicability is %#v", applicability)
	}
	if _, expanded := applicability["value"]; expanded {
		t.Fatalf("the document holds an expanded reference: %#v", applicability)
	}
	extensions := document["extensions"].(map[string]any)
	if _, kept := extensions["org.judgmentpack.annotations"]; !kept {
		t.Fatalf("the document lost its annotations: %#v", extensions)
	}
	// The same admission still evaluates as the inline twin does.
	twin := engine.AdmitPack([]byte(inlineTwinText(artifacts.Core050Version)))
	for _, facts := range core050Facts {
		evaluated, failure := engine.EvaluateAdmitted(admitted, []byte(facts), nil, Options{SpecVersion: artifacts.Core050Version})
		if failure != nil {
			t.Fatal(failure)
		}
		inline, failure := engine.EvaluateAdmitted(twin, []byte(facts), nil, Options{SpecVersion: artifacts.Core050Version})
		if failure != nil {
			t.Fatal(failure)
		}
		if evaluationShape(t, evaluated) != evaluationShape(t, inline) {
			t.Fatalf("after Document, %s evaluates otherwise than its inline twin", facts)
		}
	}
}

func TestCore050PackRefusals(t *testing.T) {
	engine := newTestEngine(t)
	base := core050PackText("")
	cases := []struct {
		name, pack, version, code, detail string
		class                             string
	}{
		{"unresolved reference", strings.Replace(base, `"valueSet":"tiers"`, `"valueSet":"tier"`, 1), artifacts.Core050Version, "JPS-EVALUATION-PACK-NOT-CONFORMANT", "JPS-SEMANTIC-UNRESOLVED-VALUE-SET at /rules/0/when/conditions/0/valueSet", result.ClassPackNotConformant},
		{"reference beside a value", strings.Replace(base, `"valueSet":"tiers"`, `"valueSet":"tiers","value":["tier-1"]`, 1), artifacts.Core050Version, "JPS-EVALUATION-PACK-NOT-CONFORMANT", "JPS-STRUCTURE-VALUE-SET-REFERENCE at /rules/0/when/conditions/0", result.ClassPackNotConformant},
		{"empty annotations", strings.Replace(base, `{"category":"core"}`, `{}`, 1), artifacts.Core050Version, "JPS-EVALUATION-PACK-NOT-CONFORMANT", "JPS-STRUCTURE-MIN-PROPERTIES at /rules/0/extensions/org.judgmentpack.annotations", result.ClassPackNotConformant},
		{"annotations in an exception", strings.Replace(base, `"effect":"escalate"`, `"effect":"escalate","extensions":{"org.judgmentpack.annotations":{"tier":"one"}}`, 1), artifacts.Core050Version, "JPS-EVALUATION-PACK-NOT-CONFORMANT", "JPS-STRUCTURE-EXTENSION-NAME at /exceptions/0/extensions/org.judgmentpack.annotations", result.ClassPackNotConformant},
		{"evaluated under 0.3.0", base, artifacts.CoreVersion, "JPS-EVALUATION-PACK-SPEC-VERSION", "", result.ClassPackNotConformant},
		{"extended profile", strings.Replace(base, `"title":"Core 0.5.0",`, `"title":"Core 0.5.0","profile":"extended",`, 1), artifacts.Core050Version, "profile-unsupported", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, failure := engine.EvaluateWith([]byte(tc.pack), []byte(core050Facts[0]), nil, Options{SpecVersion: tc.version})
			if failure == nil {
				t.Fatal("evaluated")
			}
			if failure.Code != tc.code || failure.Class != tc.class || !strings.Contains(failure.Message, tc.detail) {
				t.Fatalf("got %s/%s %q", failure.Code, failure.Class, failure.Message)
			}
		})
	}
}

func TestCore050PackSelectsItsOwnBaseContract(t *testing.T) {
	contract, failure := SelectContract([]byte(core050PackText("")))
	if failure != nil {
		t.Fatal(failure)
	}
	if contract != (result.EvaluatorContract{SpecVersion: artifacts.Core050Version, Profile: "base"}) {
		t.Fatalf("contract %+v", contract)
	}
}

// minimalBudget is the smallest work budget under which the pack evaluates.
func minimalBudget(t *testing.T, engine *Engine, pack []byte, facts string, version string) int {
	t.Helper()
	low, high := 1, 100000
	if _, failure := engine.EvaluateWith(pack, []byte(facts), nil, Options{SpecVersion: version, WorkBudget: high}); failure != nil {
		t.Fatalf("refused at the widest budget: %+v", failure)
	}
	for low < high {
		middle := (low + high) / 2
		_, failure := engine.EvaluateWith(pack, []byte(facts), nil, Options{SpecVersion: version, WorkBudget: middle})
		switch {
		case failure == nil:
			high = middle
		case failure.Code == "JPS-RESOURCE-EVALUATION-WORK-LIMIT":
			low = middle + 1
		default:
			t.Fatalf("refused: %+v", failure)
		}
	}
	return low
}

// TestCore050WorkIsChargedToTheEvaluationForm: the evaluation-work limit is
// charged on EF(P), so a pack's annotations, its sets and an unreferenced set
// are never charged, and a referenced set is charged as its inline twin's
// operand is, at each use.
func TestCore050WorkIsChargedToTheEvaluationForm(t *testing.T) {
	engine := newTestEngine(t)
	entries := make([]string, 0, 512)
	for index := range 512 {
		entries = append(entries, fmt.Sprintf(`{"value":"unused-%03d","extensions":{"org.judgmentpack.annotations":{"note":"never read"}}}`, index))
	}
	pack := []byte(core050PackText(`,{"id":"unused","entries":[` + strings.Join(entries, ",") + `]}`))
	twin := []byte(inlineTwinText(artifacts.Core050Version))
	for _, facts := range core050Facts {
		got := minimalBudget(t, engine, pack, facts, artifacts.Core050Version)
		want := minimalBudget(t, engine, twin, facts, artifacts.Core050Version)
		if got != want {
			t.Errorf("facts %s: the 0.5.0 pack needs %d work units, its inline twin %d", facts, got, want)
		}
	}
}

// The 0.5.0 evaluation corpus runs as a released corpus: 103 rows (the
// release notes' count), every one passing, under its own suiteVersion; the
// row a supported-extension set naming org.judgmentpack.annotations selects,
// which the released case schema admits, is run like every other.
func TestCore050EvaluationCorpusRuns(t *testing.T) {
	engine := newTestEngine(t)
	output, failure := engine.RunCorpus(artifacts.Core050Version, "experimental evaluate-corpus")
	if failure != nil {
		t.Fatal(failure)
	}
	if output.Status != "passed" || output.SuiteVersion != "0.5.0" || output.SpecVersion != "0.5.0" || output.CorpusStatus != "draft" {
		t.Fatalf("status %q suite %q spec %q corpus %q", output.Status, output.SuiteVersion, output.SpecVersion, output.CorpusStatus)
	}
	if output.Summary.Total != 103 || output.Summary.Passed != 103 || output.Summary.Mismatched != 0 {
		t.Fatalf("summary %+v", output.Summary)
	}
	ran := false
	for _, item := range output.Cases {
		if item.ID == "annotations-supported-extension-set" {
			ran = item.Status == "passed"
		}
	}
	if !ran {
		t.Fatal("the supported-extension-set row did not run and pass")
	}
}

// A pack declaring 0.4.0-draft or 0.4.0 is refused before admission as a
// version this runtime does not read, and the message names the release the
// draft became rather than suggesting a re-declaration to 0.2.0-draft.
func TestRetiredSelectorPackIsRefused(t *testing.T) {
	engine := newTestEngine(t)
	notes := map[string]string{
		"0.4.0-draft": "The staged draft selector 0.4.0-draft was never released: its text was released as Core 0.5.0, and a pack written to it moves up by changing its specVersion to \"0.5.0\" (Core \u00a711).",
		"0.4.0":       "No specVersion 0.4.0 exists: the staged draft selector 0.4.0-draft was released as Core 0.5.0 (Core \u00a711).",
	}
	for _, version := range []string{"0.4.0-draft", "0.4.0"} {
		pack := strings.Replace(core050PackText(""), `"specVersion":"0.5.0"`, `"specVersion":"`+version+`"`, 1)
		// Admission validates first, and validation reports the version as
		// unbundled (JPS-CAPABILITY-SPEC-VERSION), as for any version this
		// runtime does not bundle: the refusal is pack-not-conformant, with
		// the capability finding as its first diagnostic, and exits as
		// unsupported.
		_, failure := engine.EvaluateWith([]byte(pack), []byte(core050Facts[0]), nil, Options{})
		want := "The pack is not evaluated because its document conformance status is \"unsupported\"; evaluation requires a fully conformant pack. Run spec validate for the complete report. First diagnostic: JPS-CAPABILITY-SPEC-VERSION at /specVersion: The exact JPS specification version is not bundled with this CLI. " + notes[version]
		if failure == nil || failure.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" || failure.Class != result.ClassPackNotConformant || failure.ExitCode != result.ExitUnsupported || failure.Message != want {
			t.Fatalf("%s: %+v", version, failure)
		}
		// Contract selection reads the declaration alone.
		_, failure = SelectContract([]byte(pack))
		if failure == nil || failure.Code != "JPS-EVALUATION-PACK-SPEC-VERSION" || !strings.Contains(failure.Message, "released as Core 0.5.0") || strings.Contains(failure.Message, "re-declared") {
			t.Fatalf("%s contract: %+v", version, failure)
		}
	}
	for _, version := range []string{"0.4.0-draft", "0.4.0"} {
		if _, failure := engine.RunCorpus(version, "experimental evaluate-corpus"); failure == nil || failure.Code != "JPS-CAPABILITY-SPEC-VERSION" || !strings.Contains(failure.Message, "released as Core 0.5.0") {
			t.Fatalf("corpus %s: %+v", version, failure)
		}
	}
}

// TestReleasedRowsCarryOverTo050 holds 0.5.0 to 0.3.0's evaluator rules on
// the bundled released corpus: every 0.3.0 row, its pack's selector bytes
// replaced by 0.5.0's, holds under 0.5.0.
func TestReleasedRowsCarryOverTo050(t *testing.T) {
	engine := newTestEngine(t)
	set, manifest, failure := bundledCorpus(artifacts.CoreVersion)
	if failure != nil {
		t.Fatal(failure)
	}
	released, replaced := []byte(`"specVersion": "0.3.0"`), []byte(`"specVersion": "0.5.0"`)
	for _, item := range manifest.Cases {
		pack, err := set.EvaluationPack(item.Pack)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(pack, released) {
			t.Fatalf("%s: pack %s does not declare the released selector bytes", item.ID, item.Pack)
		}
		draft := bytes.Replace(pack, released, replaced, 1)
		outcome := engine.RunCaseAdmittedWith(engine.AdmitPack(draft), item.MatrixCase, HandoffTargetRendering{}, "test", Options{SpecVersion: artifacts.Core050Version})
		if outcome.Status != "passed" {
			t.Errorf("%s does not carry over: %s (expected %s, actual %s)", item.ID, outcome.Detail, outcome.Expected, outcome.Actual)
		}
	}
}

// The value-set members belong to Core 0.5.0 only. Under 0.3.0
// the schema closes the root and the fact condition, so a root valueSets and a
// valueSet on a fact in condition are unknown members (and a reference with no
// value also misses the required value), and admission refuses the pack as not
// conformant. The codes are read from the 0.3.0 schema and the validator's
// mapping of it, not from a run.
func TestReleasedPackRefusesValueSetMembers(t *testing.T) {
	engine := newTestEngine(t)
	pack := func(root, when string) string {
		return `{"specVersion":"0.3.0","id":"https://example.com/packs/released","version":"1.0.0","title":"Released",` +
			`"decision":{"intent":"Review","question":"Review?"},` +
			`"outcomes":[{"id":"accept","label":"Accept"},{"id":"refer","label":"Refer"}],` +
			`"rules":[{"id":"tiered","description":"Tiered","when":` + when + `,"outcome":"accept","onUnknown":"ignore"}]` + root + `}`
	}
	const inline = `{"op":"fact","path":"/request/tier","operator":"in","value":["tier-1","tier-2"]}`
	const sets = `,"valueSets":[{"id":"tiers","entries":[{"value":"tier-1"},{"value":"tier-2"}]}]`
	const unknown = "Member is not allowed in this Core object."
	const required = "Required member is missing."
	if !engine.AdmitPack([]byte(pack("", inline))).AdmitsWith(Options{SpecVersion: artifacts.CoreVersion}) {
		t.Fatal("the inline pack, without value-set members, must be admitted under 0.3.0")
	}
	type diagnostic struct{ code, path, message string }
	for _, tc := range []struct {
		name, pack string
		want       []diagnostic
	}{
		{"root valueSets", pack(sets, inline), []diagnostic{
			{"JPS-STRUCTURE-UNKNOWN-MEMBER", "/valueSets", unknown}}},
		{"valueSet beside a value", pack("", `{"op":"fact","path":"/request/tier","operator":"in","valueSet":"tiers","value":["tier-1"]}`), []diagnostic{
			{"JPS-STRUCTURE-UNKNOWN-MEMBER", "/rules/0/when/valueSet", unknown}}},
		{"valueSet without a value", pack("", `{"op":"fact","path":"/request/tier","operator":"in","valueSet":"tiers"}`), []diagnostic{
			{"JPS-STRUCTURE-REQUIRED-MEMBER", "/rules/0/when/value", required},
			{"JPS-STRUCTURE-UNKNOWN-MEMBER", "/rules/0/when/valueSet", unknown}}},
		{"both", pack(sets, `{"op":"fact","path":"/request/tier","operator":"in","valueSet":"tiers"}`), []diagnostic{
			{"JPS-STRUCTURE-REQUIRED-MEMBER", "/rules/0/when/value", required},
			{"JPS-STRUCTURE-UNKNOWN-MEMBER", "/rules/0/when/valueSet", unknown},
			{"JPS-STRUCTURE-UNKNOWN-MEMBER", "/valueSets", unknown}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := Options{SpecVersion: artifacts.CoreVersion}
			admitted := engine.AdmitPack([]byte(tc.pack))
			if admitted.AdmitsWith(options) {
				t.Fatal("a 0.3.0 pack carrying a value-set member was admitted")
			}
			// The admission's own validation call, whose report the refusal
			// below names the first diagnostic of.
			validated, operational := engine.validator.Validate([]byte(tc.pack), validation.Options{Through: "semantic", Limits: carrier.DefaultLimits()})
			if operational != nil {
				t.Fatal(operational)
			}
			if validated.SpecVersion != artifacts.CoreVersion || validated.Status != "invalid" {
				t.Fatalf("validated as %s/%s", validated.SpecVersion, validated.Status)
			}
			if len(validated.Diagnostics) != len(tc.want) {
				t.Fatalf("diagnostics: %+v", validated.Diagnostics)
			}
			for i, want := range tc.want {
				got := validated.Diagnostics[i]
				if got.Code != want.code || got.Layer != "structural" || got.Severity != "error" || got.InstancePath != want.path || got.Message != want.message {
					t.Fatalf("diagnostic %d: %+v", i, got)
				}
			}
			_, failure := engine.EvaluateWith([]byte(tc.pack), []byte(`{"request":{"tier":"tier-1"}}`), nil, options)
			if failure == nil {
				t.Fatal("evaluated")
			}
			first := tc.want[0]
			message := `The pack is not evaluated because its document conformance status is "invalid"; evaluation requires a fully conformant pack. Run spec validate for the complete report. First diagnostic: ` + first.code + " at " + first.path + ": " + first.message
			if failure.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" || failure.Class != result.ClassPackNotConformant || failure.Phase != result.PhasePreflight || failure.Message != message {
				t.Fatalf("got %s/%s/%s %q", failure.Code, failure.Class, failure.Phase, failure.Message)
			}
		})
	}
}
