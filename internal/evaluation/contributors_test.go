package evaluation

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const contributorTrue = `{"op":"literal","value":true}`
const contributorFalse = `{"op":"literal","value":false}`
const contributorUnknown = `{"op":"fact","path":"/missing","operator":"equals","value":true}`

func contributorRule(id, condition, outcome, onUnknown string) string {
	return `{"id":"` + id + `","description":"Synthetic rule","when":` + condition + `,"outcome":"` + outcome + `","onUnknown":"` + onUnknown + `"}`
}

func contributorException(id, condition, effect, target string) string {
	return `{"id":"` + id + `","description":"Synthetic exception","when":` + condition + `,"effect":"` + effect + `",` + target + `,"onUnknown":"ignore"}`
}

func contributorPack(version, rules, extra string) []byte {
	return []byte(`{"specVersion":"` + version + `","id":"https://example.invalid/packs/contributors","version":"1.0.0","title":"Synthetic contributors","decision":{"intent":"Test contributor evidence","question":"Which entries supplied the outcome?"},"outcomes":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],"rules":[` + rules + `]` + extra + `}`)
}

// Expected memberships and disposition bytes are hand-authored independently of
// the attribution helper. Document order is deliberately opposite ID order.
func TestEvaluationContributorsCreditAllActualSources(t *testing.T) {
	z := contributorRule("z-rule", contributorTrue, "yes", "ignore")
	a := contributorRule("a-rule", contributorTrue, "yes", "ignore")
	f := contributorRule("false-rule", contributorFalse, "yes", "ignore")
	u := contributorRule("unknown-rule", contributorUnknown, "yes", "ignore")
	forceZ := contributorException("z-force", contributorTrue, "force-outcome", `"outcome":"yes"`)
	forceA := contributorException("a-force", contributorTrue, "force-outcome", `"outcome":"yes"`)
	falseForce := contributorException("false-force", contributorFalse, "force-outcome", `"outcome":"yes"`)
	suppress := contributorException("shield", contributorTrue, "suppress-rule", `"targetRule":"z-rule"`)
	escalate := `{"id":"review","description":"Review","when":` + contributorTrue + `,"effect":"escalate","onUnknown":"ignore"}`
	const yesDisposition = `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"yes","reasons":[]}`
	const noMatch = `{"handoff":{"state":"none"},"kind":"unresolved","reasons":["no-match"]}`
	cases := []struct {
		name, rules, extra, want, disposition string
	}{
		{"all agreeing rules", z + "," + f + "," + a + "," + u, "", `[{"kind":"rule","id":"a-rule"},{"kind":"rule","id":"z-rule"}]`, yesDisposition},
		{"one rule", z, "", `[{"kind":"rule","id":"z-rule"}]`, yesDisposition},
		{"suppressed rule excluded", z + "," + a, `,"exceptions":[` + suppress + `]`, `[{"kind":"rule","id":"a-rule"}]`, yesDisposition},
		{"all compatible forcing exceptions", z + "," + a, `,"exceptions":[` + forceZ + "," + falseForce + "," + forceA + "," + suppress + `]`, `[{"kind":"forcing-exception","id":"a-force"},{"kind":"forcing-exception","id":"z-force"}]`, yesDisposition},
		{"forcing exception and rule share local id", contributorRule("a-force", contributorTrue, "yes", "ignore"), `,"exceptions":[` + forceA + `]`, `[{"kind":"forcing-exception","id":"a-force"}]`, yesDisposition},
		{"false force does not hide rules", z + "," + a, `,"exceptions":[` + falseForce + `]`, `[{"kind":"rule","id":"a-rule"},{"kind":"rule","id":"z-rule"}]`, yesDisposition},
		{"fallback", f, `,"fallbackOutcome":"yes"`, "", yesDisposition},
		{"suppressed then fallback", z, `,"exceptions":[` + suppress + `],"fallbackOutcome":"yes"`, "", yesDisposition},
		{"no match", f, "", "", noMatch},
		{"conflicting rules", z + "," + contributorRule("other", contributorTrue, "no", "ignore"), "", "", `{"handoff":{"state":"none"},"kind":"unresolved","reasons":["conflict"]}`},
		{"unknown rule blocks true rule", z + "," + contributorRule("uncertain", contributorUnknown, "yes", "escalate"), "", "", `{"handoff":{"state":"none"},"kind":"unresolved","reasons":["unknown"]}`},
		{"conflicting forced outcomes", z, `,"exceptions":[` + forceZ + "," + contributorException("other", contributorTrue, "force-outcome", `"outcome":"no"`) + `]`, "", `{"handoff":{"state":"none"},"kind":"unresolved","reasons":["conflict"]}`},
		{"direct escalation blocks force", z, `,"exceptions":[` + forceZ + "," + escalate + `]`, "", `{"handoff":{"state":"requested","triggeredBy":["exception-escalation"]},"kind":"unresolved","reasons":["exception-escalation"]}`},
		{"not applicable", z, `,"applicability":` + contributorFalse, "", `{"handoff":{"state":"none"},"kind":"not-applicable","reasons":["not-applicable"]}`},
		{"applicability unknown", z, `,"applicability":` + contributorUnknown, "", `{"handoff":{"state":"none"},"kind":"unresolved","reasons":["unknown"]}`},
		{"missing evidence blocks force", z, `,"exceptions":[` + forceZ + `],"evidenceRequirements":[{"id":"proof","description":"Proof","required":true}]`, "", `{"handoff":{"state":"none"},"kind":"unresolved","reasons":["missing-required-evidence"]}`},
	}
	engine := newTestEngine(t)
	for _, version := range []string{result.EvaluatorSpecVersion, "0.3.0"} {
		for _, tc := range cases {
			t.Run(version+"/"+tc.name, func(t *testing.T) {
				var evidence []byte
				if tc.name == "missing evidence blocks force" {
					evidence = []byte(`{"proof":"absent"}`)
				}
				evaluated, failure := engine.EvaluateWith(contributorPack(version, tc.rules, tc.extra), []byte(`{}`), evidence, Options{SpecVersion: version})
				if failure != nil {
					t.Fatalf("evaluation refused: %+v", failure)
				}
				assertContributors(t, evaluated, tc.want)
				canonical, err := evaluated.Disposition.Canonical()
				if err != nil || string(canonical) != tc.disposition {
					t.Fatalf("disposition = %s (%v), want %s", canonical, err, tc.disposition)
				}
				if tc.name == "all agreeing rules" {
					var order []string
					for _, entry := range evaluated.Trace {
						order = append(order, entry.ID)
					}
					if !reflect.DeepEqual(order, []string{"z-rule", "false-rule", "a-rule", "unknown-rule"}) {
						t.Fatalf("sorting attribution reordered trace: %v", order)
					}
				}
			})
		}
	}
}

func assertContributors(t *testing.T, evaluated result.Evaluation, want string) {
	t.Helper()
	encoded, err := json.Marshal(evaluated)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if got := string(payload["contributors"]); got != want {
		t.Fatalf("contributors = %s, want %s", got, want)
	}
	if strings.Contains(string(payload["disposition"]), "contributor") {
		t.Fatal("attribution escaped into the closed disposition")
	}
	if _, present := payload["contributingRule"]; present {
		t.Fatal("arbitrary representative added")
	}
}

func TestEvaluationContributorsKeepLongExactLocalIDs(t *testing.T) {
	id := "r" + strings.Repeat("x", 4096)
	evaluated, failure := newTestEngine(t).EvaluateWith(contributorPack("0.3.0", contributorRule(id, contributorTrue, "yes", "ignore"), ""), []byte(`{}`), nil, Options{SpecVersion: "0.3.0"})
	if failure != nil {
		t.Fatal(failure)
	}
	assertContributors(t, evaluated, `[{"kind":"rule","id":"`+id+`"}]`)
}

func TestEvaluationContributorsDoNotImposeWorkflowEnvelopeBounds(t *testing.T) {
	// Workflow output admission has a separate 4096-member ceiling. Runtime
	// evaluation must retain every admitted contributor rather than truncate
	// membership or invent a new refusal at that downstream boundary.
	const count = 4097
	rules := make([]string, count)
	for index := range rules {
		rules[index] = contributorRule(fmt.Sprintf("r-%04d", count-index), contributorTrue, "yes", "ignore")
	}
	evaluated, failure := newTestEngine(t).EvaluateWith(contributorPack("0.3.0", strings.Join(rules, ","), ""), []byte(`{}`), nil, Options{SpecVersion: "0.3.0"})
	if failure != nil {
		t.Fatal(failure)
	}
	if len(evaluated.Contributors) != count {
		t.Fatalf("retained %d contributors, want all %d", len(evaluated.Contributors), count)
	}
	for index, got := range evaluated.Contributors {
		want := result.Contributor{Kind: "rule", ID: fmt.Sprintf("r-%04d", index+1)}
		if got != want {
			t.Fatalf("contributor %d = %+v, want %+v", index, got, want)
		}
	}
}

func TestEvaluationContributorsWithheldForUnresolvedPrototypeValue(t *testing.T) {
	engine := newTestEngine(t)
	for way, produced := range producedBy(`{"amount":{"type":"decimal","fromFact":"/missing"}}`, "") {
		t.Run(way, func(t *testing.T) {
			evaluated, failure := engine.EvaluateWith(produced.pack.bytes(), []byte(produced.facts), nil, valuesOptions())
			if failure != nil || evaluated.Disposition.Kind != "unresolved" {
				t.Fatalf("expected withheld outcome: %+v %+v", evaluated, failure)
			}
			assertContributors(t, evaluated, "")
		})
	}
}

func TestEvaluationContributorsAbsentOnWorkLimitFailure(t *testing.T) {
	evaluated, failure := newTestEngine(t).EvaluateWith(contributorPack("0.3.0", contributorRule("one", contributorTrue, "yes", "ignore"), ""), []byte(`{}`), nil, Options{SpecVersion: "0.3.0", WorkBudget: 1})
	if failure == nil || !reflect.DeepEqual(evaluated, result.Evaluation{}) {
		t.Fatalf("work refusal leaked a partial evaluation: %+v %+v", evaluated, failure)
	}
}
