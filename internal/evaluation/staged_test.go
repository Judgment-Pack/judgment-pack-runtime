package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The staged draft selector (EXPERIMENTAL) is evaluated as its evaluation form.
// These packs are written by hand twice: once with value sets, references and
// annotations, and once as the inline twin a reader would write without them.
// The expectation is that the two evaluate identically, trace included; no
// expectation here is recorded from a run.

// stagedPackText writes the referencing pack: a reference in the applicability,
// under all and not in one rule's when, under any in another's, and in an
// exception's when that does not match; annotations on the pack, an outcome, a
// rule and a value-set entry; and one set no condition references.
func stagedPackText(extra string) string {
	return `{"specVersion":"0.4.0-draft","id":"https://example.com/packs/staged","version":"1.0.0","title":"Staged",` +
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
	return `{"specVersion":"` + specVersion + `","id":"https://example.com/packs/staged","version":"1.0.0","title":"Staged",` +
		`"decision":{"intent":"Review","question":"Review?"},` +
		`"applicability":{"op":"fact","path":"/request/kind","operator":"in","value":["intake","renewal"]},` +
		`"outcomes":[{"id":"accept","label":"Accept"},{"id":"refer","label":"Refer"}],` +
		`"rules":[` +
		`{"id":"tiered","description":"Tiered","when":{"op":"all","conditions":[{"op":"fact","path":"/request/tier","operator":"in","value":["tier-1","tier-2"]},{"op":"not","condition":{"op":"fact","path":"/request/region","operator":"in","value":["us"]}}]},"outcome":"accept","onUnknown":"ignore","extensions":{"com.example.note":{"text":"kept"}}},` +
		`{"id":"regional","description":"Regional","when":{"op":"any","conditions":[{"op":"fact","path":"/request/region","operator":"in","value":["us"]}]},"outcome":"refer","onUnknown":"ignore"}],` +
		`"exceptions":[{"id":"blocked","description":"Blocked","when":{"op":"fact","path":"/request/region","operator":"in","value":["us"]},"effect":"escalate","onUnknown":"ignore"}]}`
}

var stagedFacts = []string{
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

func TestStagedPackEvaluatesAsItsInlineTwin(t *testing.T) {
	engine := newTestEngine(t)
	pack := []byte(stagedPackText(""))
	twins := map[string][]byte{
		artifacts.StagedDraftVersion: []byte(inlineTwinText(artifacts.StagedDraftVersion)),
		artifacts.CoreVersion:        []byte(inlineTwinText(artifacts.CoreVersion)),
	}
	kinds := map[string]bool{}
	for index, facts := range stagedFacts {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			got, failure := engine.EvaluateWith(pack, []byte(facts), nil, Options{SpecVersion: artifacts.StagedDraftVersion})
			if failure != nil {
				t.Fatalf("staged pack refused: %+v", failure)
			}
			if got.SpecVersion != artifacts.StagedDraftVersion || got.EvaluatorSpecVersion != artifacts.StagedDraftVersion {
				t.Fatalf("versions %q/%q", got.SpecVersion, got.EvaluatorSpecVersion)
			}
			kinds[got.Disposition.Kind] = true
			for version, twin := range twins {
				want, failure := engine.EvaluateWith(twin, []byte(facts), nil, Options{SpecVersion: version})
				if failure != nil {
					t.Fatalf("%s twin refused: %+v", version, failure)
				}
				if evaluationShape(t, got) != evaluationShape(t, want) {
					t.Fatalf("staged pack and its %s inline twin differ:\nstaged: %s\ntwin:   %s", version, evaluationShape(t, got), evaluationShape(t, want))
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

func TestStagedPackIdentityIsItsExactBytes(t *testing.T) {
	engine := newTestEngine(t)
	pack := []byte(stagedPackText(""))
	admitted := engine.AdmitPack(pack)
	if admitted.Digest() != sha256.Sum256(pack) {
		t.Fatal("the admission digest is not over the pack's exact bytes")
	}
	evaluated, failure := engine.EvaluateAdmitted(admitted, []byte(stagedFacts[0]), nil, Options{SpecVersion: artifacts.StagedDraftVersion})
	if failure != nil {
		t.Fatal(failure)
	}
	if evaluated.PackID != "https://example.com/packs/staged" || evaluated.PackVersion != "1.0.0" {
		t.Fatalf("identity %q %q", evaluated.PackID, evaluated.PackVersion)
	}
	if evaluated.Artifact == nil || evaluated.Artifact.SpecVersion != artifacts.StagedDraftVersion {
		t.Fatalf("artifact %+v", evaluated.Artifact)
	}
	// A pack differing only in an annotation is another pack: another digest,
	// the same evaluation.
	relabelled := bytes.Replace(pack, []byte(`"owner":"intake-team"`), []byte(`"owner":"review-team"`), 1)
	other := engine.AdmitPack(relabelled)
	if other.Digest() == admitted.Digest() {
		t.Fatal("an annotation change did not change the digest")
	}
	again, failure := engine.EvaluateAdmitted(other, []byte(stagedFacts[0]), nil, Options{SpecVersion: artifacts.StagedDraftVersion})
	if failure != nil {
		t.Fatal(failure)
	}
	if evaluationShape(t, again) != evaluationShape(t, evaluated) {
		t.Fatal("an annotation changed the evaluation")
	}
}

func TestStagedPackRefusals(t *testing.T) {
	engine := newTestEngine(t)
	base := stagedPackText("")
	cases := []struct {
		name, pack, version, code, detail string
		class                             string
	}{
		{"unresolved reference", strings.Replace(base, `"valueSet":"tiers"`, `"valueSet":"tier"`, 1), artifacts.StagedDraftVersion, "JPS-EVALUATION-PACK-NOT-CONFORMANT", "JPS-SEMANTIC-UNRESOLVED-VALUE-SET at /rules/0/when/conditions/0/valueSet", result.ClassPackNotConformant},
		{"reference beside a value", strings.Replace(base, `"valueSet":"tiers"`, `"valueSet":"tiers","value":["tier-1"]`, 1), artifacts.StagedDraftVersion, "JPS-EVALUATION-PACK-NOT-CONFORMANT", "JPS-STRUCTURE-VALUE-SET-REFERENCE at /rules/0/when/conditions/0", result.ClassPackNotConformant},
		{"empty annotations", strings.Replace(base, `{"category":"core"}`, `{}`, 1), artifacts.StagedDraftVersion, "JPS-EVALUATION-PACK-NOT-CONFORMANT", "JPS-STRUCTURE-MIN-PROPERTIES at /rules/0/extensions/org.judgmentpack.annotations", result.ClassPackNotConformant},
		{"annotations in an exception", strings.Replace(base, `"effect":"escalate"`, `"effect":"escalate","extensions":{"org.judgmentpack.annotations":{"tier":"one"}}`, 1), artifacts.StagedDraftVersion, "JPS-EVALUATION-PACK-NOT-CONFORMANT", "JPS-STRUCTURE-EXTENSION-NAME at /exceptions/0/extensions/org.judgmentpack.annotations", result.ClassPackNotConformant},
		{"evaluated under 0.3.0", base, artifacts.CoreVersion, "JPS-EVALUATION-PACK-SPEC-VERSION", "", result.ClassPackNotConformant},
		{"extended profile", strings.Replace(base, `"title":"Staged",`, `"title":"Staged","profile":"extended",`, 1), artifacts.StagedDraftVersion, "profile-unsupported", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, failure := engine.EvaluateWith([]byte(tc.pack), []byte(stagedFacts[0]), nil, Options{SpecVersion: tc.version})
			if failure == nil {
				t.Fatal("evaluated")
			}
			if failure.Code != tc.code || failure.Class != tc.class || !strings.Contains(failure.Message, tc.detail) {
				t.Fatalf("got %s/%s %q", failure.Code, failure.Class, failure.Message)
			}
		})
	}
}

func TestStagedPackSelectsItsOwnBaseContract(t *testing.T) {
	contract, failure := SelectContract([]byte(stagedPackText("")))
	if failure != nil {
		t.Fatal(failure)
	}
	if contract != (result.EvaluatorContract{SpecVersion: artifacts.StagedDraftVersion, Profile: "base"}) {
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

// TestStagedWorkIsChargedToTheEvaluationForm: the evaluation-work limit is
// charged on EF(P), so a pack's annotations, its sets and an unreferenced set
// are never charged, and a referenced set is charged as its inline twin's
// operand is, at each use.
func TestStagedWorkIsChargedToTheEvaluationForm(t *testing.T) {
	engine := newTestEngine(t)
	entries := make([]string, 0, 512)
	for index := range 512 {
		entries = append(entries, fmt.Sprintf(`{"value":"unused-%03d","extensions":{"org.judgmentpack.annotations":{"note":"never read"}}}`, index))
	}
	pack := []byte(stagedPackText(`,{"id":"unused","entries":[` + strings.Join(entries, ",") + `]}`))
	twin := []byte(inlineTwinText(artifacts.StagedDraftVersion))
	for _, facts := range stagedFacts {
		got := minimalBudget(t, engine, pack, facts, artifacts.StagedDraftVersion)
		want := minimalBudget(t, engine, twin, facts, artifacts.StagedDraftVersion)
		if got != want {
			t.Errorf("facts %s: the staged pack needs %d work units, its inline twin %d", facts, got, want)
		}
	}
}

func TestStagedEvaluationRowsRun(t *testing.T) {
	engine := newTestEngine(t)
	set, err := artifacts.Load(artifacts.StagedDraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	_, cases, held, failure := loadStagedRows(set)
	if failure != nil {
		t.Fatal(failure)
	}
	output, failure := engine.RunCorpus(artifacts.StagedDraftVersion, "experimental evaluate-corpus")
	if failure != nil {
		t.Fatal(failure)
	}
	if output.Status != "passed" || output.CorpusStatus != "staged" || output.SuiteVersion != "" {
		t.Fatalf("status %q corpus %q suite %q", output.Status, output.CorpusStatus, output.SuiteVersion)
	}
	if output.Summary.Total != len(cases) || output.Summary.Passed != len(cases) || output.Summary.Held != len(held) || len(held) == 0 {
		t.Fatalf("summary %+v for %d rows and %d held", output.Summary, len(cases), len(held))
	}
	for _, item := range output.Cases {
		if item.Status == "held" && (item.Detail == "" || item.Actual != "") {
			t.Errorf("held row %s ran or gave no reason", item.ID)
		}
	}
}

// TestReleasedRowsCarryOverToTheStagedSelector is the staged rows' carriesOver
// rule for the bundled released corpus: every 0.3.0 row, its pack's selector
// bytes replaced, holds under the draft.
func TestReleasedRowsCarryOverToTheStagedSelector(t *testing.T) {
	engine := newTestEngine(t)
	set, manifest, failure := bundledCorpus(artifacts.CoreVersion)
	if failure != nil {
		t.Fatal(failure)
	}
	released, replaced := []byte(`"specVersion": "0.3.0"`), []byte(`"specVersion": "0.4.0-draft"`)
	for _, item := range manifest.Cases {
		pack, err := set.EvaluationPack(item.Pack)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(pack, released) {
			t.Fatalf("%s: pack %s does not declare the released selector bytes", item.ID, item.Pack)
		}
		draft := bytes.Replace(pack, released, replaced, 1)
		outcome := engine.RunCaseAdmittedWith(engine.AdmitPack(draft), item.MatrixCase, HandoffTargetRendering{}, "test", Options{SpecVersion: artifacts.StagedDraftVersion})
		if outcome.Status != "passed" {
			t.Errorf("%s does not carry over: %s (expected %s, actual %s)", item.ID, outcome.Detail, outcome.Expected, outcome.Actual)
		}
	}
}
