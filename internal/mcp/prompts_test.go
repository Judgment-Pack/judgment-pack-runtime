package mcp

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

type pinnedPrompt struct {
	name       string
	artifact   string
	boundaries []string
}

func pinnedPrompts() []pinnedPrompt {
	return []pinnedPrompt{
		{
			name:     "author_pack",
			artifact: "judgment pack",
			boundaries: []string{
				"this runtime decides nothing",
				`only "spec validate" / the "validate" tool decides conformance`,
			},
		},
		{
			name:     "test_pack",
			artifact: "instance matrix",
			boundaries: []string{
				"in full and only, in CONFORMANCE.md",
				"THE POLICY TEXT IS THE ARBITER",
			},
		},
		{
			name:     "fix_pack",
			artifact: "diagnostics",
			boundaries: []string{
				"validity does not test logic",
				"this runtime decides nothing",
			},
		},
		{
			name:     "explain_disposition",
			artifact: "disposition",
			boundaries: []string{
				"The disposition is authoritative",
				"the trace beside it is informative",
				"nothing about the wisdom of acting",
			},
		},
		{
			name:     "present_pack",
			artifact: "judgment pack",
			boundaries: []string{
				"The pack is the only statement of what it is",
				"an omission silent is a misrepresentation",
				"authorize acting on any disposition",
			},
		},
		{
			name:     "replay_history",
			artifact: "past decisions",
			boundaries: []string{
				"DOCUMENTS WRITE THE RULES, PAST DECISIONS TEST THEM",
				"every row is a rehearsal, and nothing is recorded as a decision",
				"None of it is a verdict",
			},
		},
		{
			name:     "author_graph",
			artifact: "graph document",
			boundaries: []string{
				"a PROPOSAL for a human to review",
				"a matrix row is a rehearsal, not a decision",
				"You propose; the human commits",
			},
		},
	}
}

// renderPinnedPrompt returns the text getPrompt serves for name with no
// arguments, failing the test when the name is not advertised.
func renderPinnedPrompt(t *testing.T, name string) string {
	t.Helper()
	params, err := json.Marshal(map[string]any{"name": name})
	if err != nil {
		t.Fatal(err)
	}
	got, rpcErr := getPrompt(params)
	if rpcErr != nil {
		t.Fatalf("getPrompt(%q) must succeed for an advertised prompt: %s", name, rpcErr.Message)
	}
	messages, ok := got["messages"].([]map[string]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("getPrompt(%q) must return at least one message: %#v", name, got)
	}
	content, ok := messages[0]["content"].(map[string]any)
	if !ok {
		t.Fatalf("getPrompt(%q) message must carry content: %#v", name, messages[0])
	}
	text, ok := content["text"].(string)
	if !ok {
		t.Fatalf("getPrompt(%q) content must carry text: %#v", name, content)
	}
	return text
}

func TestPromptsPinClaimBoundaries(t *testing.T) {
	for _, pinned := range pinnedPrompts() {
		t.Run(pinned.name, func(t *testing.T) {
			text := renderPinnedPrompt(t, pinned.name)
			if strings.TrimSpace(text) == "" {
				t.Fatalf("%s must render non-empty text", pinned.name)
			}
			if !strings.Contains(strings.ToLower(text), strings.ToLower(pinned.artifact)) {
				t.Fatalf("%s must mention the artifact it is about (%q)", pinned.name, pinned.artifact)
			}
			for _, phrase := range pinned.boundaries {
				if !strings.Contains(text, phrase) {
					t.Fatalf("%s must carry its claim boundary %q", pinned.name, phrase)
				}
			}
		})
	}
}

// Every prompt the server advertises must have a pinning entry above, so a
// newly added prompt fails here until its claim boundary is pinned.
func TestEveryAdvertisedPromptIsPinned(t *testing.T) {
	pinned := map[string]bool{}
	for _, p := range pinnedPrompts() {
		pinned[p.name] = true
	}
	for _, entry := range listPrompts() {
		name, _ := entry["name"].(string)
		if name == "" {
			t.Fatal("listPrompts must advertise a name for every prompt")
		}
		if !pinned[name] {
			t.Fatalf("advertised prompt %q has no claim-boundary pin in pinnedPrompts", name)
		}
	}
}

// probePack is built so each of the test_pack prompt's probes can be made
// alone: every rule reads facts no other rule turns on in the same row, one
// rule escalates and one ignores an unknown, a third hides an ordered
// comparison behind an all, the one exception forces an outcome, one evidence
// requirement is required and one is optional and only cited.
const probePack = `{
  "specVersion": "0.2.0-draft",
  "id": "https://example.invalid/judgment-packs/test-pack-probes",
  "version": "0.1.0",
  "title": "Test-pack probes",
  "decision": {"intent": "Hold the test_pack prompt to the evaluator.", "question": "What does the evaluator answer?"},
  "applicability": {"op": "fact", "path": "/kind", "operator": "equals", "value": "claim"},
  "evidenceRequirements": [
    {"id": "form", "description": "A form.", "required": true, "kind": "document"},
    {"id": "photo", "description": "A photo.", "required": false, "kind": "document"}
  ],
  "outcomes": [
    {"id": "approve", "label": "Approve"},
    {"id": "decline", "label": "Decline"},
    {"id": "review", "label": "Review"},
    {"id": "hold", "label": "Hold"}
  ],
  "rules": [
    {"id": "over-limit", "description": "Decline over 100.", "when": {"op": "fact", "path": "/amount", "operator": "greater-than", "value": "100"}, "outcome": "decline", "onUnknown": "escalate", "evidenceRequirementRefs": ["photo"]},
    {"id": "flagged", "description": "Review a flagged claim.", "when": {"op": "fact", "path": "/flagged", "operator": "equals", "value": true}, "outcome": "review", "onUnknown": "ignore"},
    {"id": "masked", "description": "Hold a large urgent claim.", "when": {"op": "all", "conditions": [{"op": "fact", "path": "/urgent", "operator": "equals", "value": true}, {"op": "fact", "path": "/amount", "operator": "greater-than", "value": "1000"}]}, "outcome": "hold", "onUnknown": "escalate"}
  ],
  "exceptions": [
    {"id": "vip", "description": "Approve a VIP.", "when": {"op": "fact", "path": "/vip", "operator": "equals", "value": true}, "effect": "force-outcome", "outcome": "approve", "onUnknown": "ignore"}
  ],
  "fallbackOutcome": "approve",
  "escalation": {"triggers": ["unknown", "conflict", "missing-required-evidence", "not-applicable"], "target": {"kind": "human-role", "name": "Reviewer"}}
}`

// variant returns probePack with edit applied to its decoded form.
func variant(t *testing.T, edit func(pack map[string]any)) string {
	t.Helper()
	var pack map[string]any
	if err := json.Unmarshal([]byte(probePack), &pack); err != nil {
		t.Fatal(err)
	}
	edit(pack)
	data, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// entry expects one trace entry: the stage and id it is, its condition, and
// whatever else the probe's claim is about.
type entry struct {
	stage, id, condition, onUnknown string
	skipped, suppressed             bool
	// causes is the cause each unknown leaf is named with, as "pointer cause"
	// or "pointer cause factType"; nil expects none.
	causes []string
}

// evaluationProbe is one row the test_pack prompt describes: the sentences of
// the prompt that state what the evaluator answers, the inputs that make the
// row, and that answer.
type evaluationProbe struct {
	name     string
	claims   []string
	pack     string // probePack when empty
	facts    string
	evidence *string // nil sends no evidence document at all
	kind     string
	outcome  string
	reasons  []string
	handoff  string
	unmet    []result.UnmetEvidence
	entries  []entry
}

// The test_pack prompt tells an agent what the evaluator answers for each
// probe, and an agent that gets a different answer repairs a pack or a row that
// was right (issue #176). Each of those claims is held here to the evaluator's
// answer through the tool the prompt names, on a pack built so each probe can
// be made alone, and the claim's words are pinned beside its probe, so that
// rewording a claim fails here until its probe is checked again.
func TestTestPackPromptStatesWhatTheEvaluatorAnswers(t *testing.T) {
	facts := func(members string) string {
		base := map[string]any{"kind": "claim", "amount": "50", "flagged": false, "urgent": false, "vip": false}
		var changes map[string]any
		if err := json.Unmarshal([]byte("{"+members+"}"), &changes); err != nil {
			t.Fatal(err)
		}
		for key, value := range changes {
			if value == nil {
				delete(base, key)
			} else {
				base[key] = value
			}
		}
		data, err := json.Marshal(base)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	document := func(text string) *string { return &text }
	baseline := document(`{"form":"present","photo":"present"}`)
	exception := func(effect string, extra map[string]any) string {
		return variant(t, func(pack map[string]any) {
			vip := pack["exceptions"].([]any)[0].(map[string]any)
			vip["effect"] = effect
			delete(vip, "outcome")
			for key, value := range extra {
				vip[key] = value
			}
		})
	}
	triggers := func(names ...string) func(map[string]any) {
		return func(pack map[string]any) {
			list := []any{}
			for _, name := range names {
				list = append(list, name)
			}
			pack["escalation"].(map[string]any)["triggers"] = list
		}
	}
	quiet := []entry{{stage: "rule", id: "flagged", condition: "false"}, {stage: "rule", id: "masked", condition: "false"}}
	probes := []evaluationProbe{
		{name: "an outcome by the fallback", claims: []string{"or the fallbackOutcome when no rule fires"},
			facts: facts(``), evidence: baseline, kind: "outcome", outcome: "approve", handoff: "none",
			entries: []entry{{stage: "rule", id: "over-limit", condition: "false"}, quiet[0], quiet[1]}},
		{name: "an outcome by a rule", claims: []string{"One instance per reachable outcome, reached the way the pack reaches it: a rule,"},
			facts: facts(`"amount":"500"`), evidence: baseline, kind: "outcome", outcome: "decline", handoff: "none",
			entries: []entry{{stage: "rule", id: "over-limit", condition: "true"}}},
		{name: "conflict", claims: []string{`facts that make two rules with DIFFERENT outcomes true at once, with no exception forcing an outcome or suppressing either. Expect unresolved with reason "conflict".`},
			facts: facts(`"amount":"500","flagged":true`), evidence: baseline, kind: "unresolved", reasons: []string{"conflict"}, handoff: "requested"},
		{name: "a conflict under a forced outcome is no conflict", claims: []string{"with no exception forcing an outcome"},
			facts: facts(`"amount":"500","flagged":true,"vip":true`), evidence: baseline, kind: "outcome", outcome: "approve", handoff: "none"},
		{name: "an unknown rule that escalates", claims: []string{`omit one fact the rule turns on, so its condition is unknown, and read its onUnknown in the pack rather than inferring it from the result. With "escalate", expect unresolved with reason "unknown", and a handoff when the escalation triggers include "unknown".`},
			facts: facts(`"amount":null`), evidence: baseline, kind: "unresolved", reasons: []string{"unknown"}, handoff: "requested",
			entries: []entry{{stage: "rule", id: "over-limit", condition: "unknown", onUnknown: "escalate", causes: []string{"/amount absent"}}, quiet[0], quiet[1]}},
		{name: "an unknown rule that escalates, with no unknown trigger", claims: []string{`a handoff when the escalation triggers include "unknown"`},
			pack: variant(t, triggers("conflict")), facts: facts(`"amount":null`), evidence: baseline, kind: "unresolved", reasons: []string{"unknown"}, handoff: "none"},
		{name: "an unknown rule that ignores", claims: []string{`With "ignore", the rule does not fire and the rest of the pack answers.`},
			facts: facts(`"flagged":null`), evidence: baseline, kind: "outcome", outcome: "approve", handoff: "none",
			entries: []entry{{stage: "rule", id: "flagged", condition: "unknown", onUnknown: "ignore", causes: []string{"/flagged absent"}}}},
		{name: "an all another condition decides", claims: []string{"an all that another of its conditions makes false, or an any that another makes true, is decided whatever the changed leaf says"},
			facts: facts(`"amount":null`), evidence: baseline, kind: "unresolved", reasons: []string{"unknown"}, handoff: "requested",
			entries: []entry{{stage: "rule", id: "masked", condition: "false"}}},
		{name: "a required requirement absent", claims: []string{
			`mark that one requirement "absent". Expect unresolved with reason "missing-required-evidence", and unmetEvidence naming that requirement as absent.`,
			`a missing-required-evidence reason means the row's evidenceAvailability marked a requirement the pack requires "absent"`},
			facts: facts(``), evidence: document(`{"form":"absent","photo":"present"}`), kind: "unresolved", reasons: []string{"missing-required-evidence"}, handoff: "requested",
			unmet: []result.UnmetEvidence{{Requirement: "form", State: "absent"}}},
		{name: "no evidence document", claims: []string{`evaluate with no evidence document at all. Every requirement is then unknown, not absent: expect unresolved with reason "unknown", and unmetEvidence naming each required requirement as unknown.`},
			facts: facts(``), evidence: nil, kind: "unresolved", reasons: []string{"unknown"}, handoff: "requested",
			unmet: []result.UnmetEvidence{{Requirement: "form", State: "unknown"}}},
		{name: "a required requirement omitted", claims: []string{
			"A document that omits one required requirement does the same for that one;",
			`while one the row omits is unknown, and gives reason "unknown" when none is absent`},
			facts: facts(``), evidence: document(`{"photo":"present"}`), kind: "unresolved", reasons: []string{"unknown"}, handoff: "requested",
			unmet: []result.UnmetEvidence{{Requirement: "form", State: "unknown"}}},
		{name: "one requirement absent and another omitted", claims: []string{`if another is marked absent, the reason is "missing-required-evidence" and unmetEvidence names both.`},
			pack: variant(t, func(pack map[string]any) {
				pack["evidenceRequirements"] = append(pack["evidenceRequirements"].([]any), map[string]any{"id": "id-check", "description": "An identity check.", "required": true, "kind": "fact"})
			}),
			facts: facts(``), evidence: document(`{"form":"absent","photo":"present"}`), kind: "unresolved", reasons: []string{"missing-required-evidence"}, handoff: "requested",
			unmet: []result.UnmetEvidence{{Requirement: "form", State: "absent"}, {Requirement: "id-check", State: "unknown"}}},
		{name: "an optional requirement that is only cited", claims: []string{"evidenceRequirementRefs is a citation the evaluator never reads"},
			facts: facts(`"amount":"500"`), evidence: document(`{"form":"present","photo":"absent"}`), kind: "outcome", outcome: "decline", handoff: "none"},
		{name: "not applicable", claims: []string{"facts that make its condition false. Expect a not-applicable result, not an outcome."},
			facts: facts(`"kind":"other"`), evidence: baseline, kind: "not-applicable", reasons: []string{"not-applicable"}, handoff: "requested",
			entries: []entry{{stage: "applicability", condition: "false"}}},
		{name: "applicability unknown", claims: []string{"Facts that leave it unknown give unknown instead"},
			facts: facts(`"kind":null`), evidence: baseline, kind: "unresolved", reasons: []string{"unknown"}, handoff: "requested",
			entries: []entry{{stage: "applicability", condition: "unknown", causes: []string{"/kind absent"}}}},
		{name: "applicability that reads evidence", claims: []string{"an applicability condition that reads evidence is decided before evidence is checked, so probes 4 and 5 then answer not-applicable or unknown first."},
			pack: variant(t, func(pack map[string]any) {
				pack["applicability"] = map[string]any{"op": "evidence-present", "evidenceRequirement": "form"}
			}),
			facts: facts(``), evidence: document(`{"form":"absent","photo":"present"}`), kind: "not-applicable", reasons: []string{"not-applicable"}, handoff: "requested"},
		{name: "a force-outcome exception", claims: []string{"A force-outcome exception: facts that satisfy the exception AND a rule it should override; expect the exception's outcome with the rules skipped."},
			facts: facts(`"amount":"500","vip":true`), evidence: baseline, kind: "outcome", outcome: "approve", handoff: "none",
			entries: []entry{{stage: "rule", id: "over-limit", condition: "not-evaluated", skipped: true}, {stage: "rule", id: "flagged", condition: "not-evaluated", skipped: true}}},
		{name: "a suppress-rule exception", claims: []string{"A suppress-rule exception: expect its target rule suppressed and the rest of the pack deciding."},
			pack: exception("suppress-rule", map[string]any{"targetRule": "over-limit"}), facts: facts(`"amount":"500","vip":true`), evidence: baseline, kind: "outcome", outcome: "approve", handoff: "none",
			entries: []entry{{stage: "rule", id: "over-limit", condition: "not-evaluated", suppressed: true}, quiet[0]}},
		{name: "an escalate exception, with no trigger for it", claims: []string{`An escalate exception: expect unresolved with reason "exception-escalation" and a requested handoff.`},
			pack: variant(t, func(pack map[string]any) {
				triggers("conflict")(pack)
				vip := pack["exceptions"].([]any)[0].(map[string]any)
				vip["effect"] = "escalate"
				delete(vip, "outcome")
			}),
			facts: facts(`"vip":true`), evidence: baseline, kind: "unresolved", reasons: []string{"exception-escalation"}, handoff: "requested"},
		{name: "a number where a decimal string belongs", claims: []string{"supply the value as a JSON number instead of a decimal string, in a row where that comparison decides the rule. Expect the rule's condition unknown, handled per its onUnknown, and its trace entry naming the fact as not-comparable"},
			facts: facts(`"amount":50`), evidence: baseline, kind: "unresolved", reasons: []string{"unknown"}, handoff: "requested",
			entries: []entry{{stage: "rule", id: "over-limit", condition: "unknown", onUnknown: "escalate", causes: []string{"/amount not-comparable number"}}, {stage: "rule", id: "masked", condition: "false"}}},
	}

	// The prompt wraps its lines; a claim is compared with its whitespace
	// collapsed, so a re-wrap is not a change of claim and a changed word is.
	text := strings.Join(strings.Fields(renderPinnedPrompt(t, "test_pack")), " ")
	var calls []string
	for index, probe := range probes {
		for _, claim := range probe.claims {
			if !strings.Contains(text, claim) {
				t.Fatalf("%s: the prompt no longer states %q; check the probe against the evaluator and update both", probe.name, claim)
			}
		}
		pack := probe.pack
		if pack == "" {
			pack = probePack
		}
		arguments := map[string]any{"pack": pack, "facts": probe.facts}
		if probe.evidence != nil {
			arguments["evidence"] = *probe.evidence
		}
		calls = append(calls, toolCall(t, index+1, "experimental_evaluate", arguments))
	}
	responses := runServer(t, strings.Join(calls, ""))
	if len(responses) != len(probes) {
		t.Fatalf("responses = %d, want %d", len(responses), len(probes))
	}
	for index, probe := range probes {
		outcome := responses[index]["result"].(map[string]any)
		if outcome["isError"] != false {
			t.Fatalf("%s: the evaluation was refused: %s", probe.name, toolText(t, outcome))
		}
		var evaluation result.Evaluation
		decodeStructured(t, outcome, &evaluation)
		got := evaluation.Disposition
		wantReasons := probe.reasons
		if wantReasons == nil {
			wantReasons = []string{}
		}
		if got.Kind != probe.kind || got.OutcomeID != probe.outcome || got.Handoff.State != probe.handoff || !slices.Equal(got.Reasons, wantReasons) {
			t.Fatalf("%s: disposition = %+v, the prompt says kind %q outcome %q reasons %v handoff %q", probe.name, got, probe.kind, probe.outcome, wantReasons, probe.handoff)
		}
		if !reflect.DeepEqual(evaluation.UnmetEvidence, probe.unmet) {
			t.Fatalf("%s: unmetEvidence = %+v, want %+v", probe.name, evaluation.UnmetEvidence, probe.unmet)
		}
		for _, want := range probe.entries {
			checkEntry(t, probe.name, evaluation.Trace, want)
		}
	}
}

// checkEntry finds the trace entry want names and holds it to want.
func checkEntry(t *testing.T, probe string, trace []result.TraceEntry, want entry) {
	t.Helper()
	for _, got := range trace {
		if got.Stage != want.stage || got.ID != want.id {
			continue
		}
		var causes []string
		for _, cause := range got.UnknownCauses {
			named := cause.EvidenceRequirement
			if cause.Path != nil {
				named = *cause.Path
			}
			causes = append(causes, strings.TrimSpace(named+" "+cause.Cause+" "+cause.FactType))
		}
		if got.Condition != want.condition || got.OnUnknown != want.onUnknown || got.Skipped != want.skipped ||
			got.Suppressed != want.suppressed || !slices.Equal(causes, want.causes) {
			t.Fatalf("%s: trace entry %s %s = %+v (causes %v), want %+v", probe, want.stage, want.id, got, causes, want)
		}
		return
	}
	t.Fatalf("%s: the trace has no %s entry %q", probe, want.stage, want.id)
}

// The author_pack prompt tells an agent that equality is type-exact, so a
// detector written as equals true falls through to the fallback on a fact of
// another JSON type, and how to write one that does not (issue #199). Each
// claim is pinned beside the evaluator's answer, as the test_pack claims are.
func TestAuthorPackPromptStatesTheTypeTrap(t *testing.T) {
	detector := func(when string) string {
		return `{
  "specVersion": "0.2.0-draft",
  "id": "https://example.invalid/judgment-packs/type-trap",
  "version": "0.1.0",
  "title": "Type trap",
  "decision": {"intent": "Hold the author_pack prompt to the evaluator.", "question": "Is a provision violated?"},
  "applicability": {"op": "fact", "path": "/kind", "operator": "equals", "value": "claim"},
  "evidenceRequirements": [],
  "outcomes": [{"id": "permitted", "label": "Permitted"}, {"id": "violation", "label": "Violation"}],
  "rules": [{"id": "flag", "description": "The flag is set.", "when": ` + when + `, "outcome": "violation", "onUnknown": "escalate"}],
  "fallbackOutcome": "permitted",
  "escalation": {"triggers": ["unknown", "conflict", "missing-required-evidence", "not-applicable"], "target": {"kind": "human-role", "name": "Reviewer"}}
}`
	}
	equalsTrue := detector(`{"op": "fact", "path": "/flag", "operator": "equals", "value": true}`)
	notEqualsFalse := detector(`{"op": "fact", "path": "/flag", "operator": "not-equals", "value": false}`)
	failSafe := detector(`{"op": "not", "condition": {"op": "fact", "path": "/flag", "operator": "equals", "value": false}}`)
	trap := `Equality is type-exact: a fact of another JSON type than the value it is compared with ("true" or 1 against true, or null) makes equals FALSE and not-equals TRUE -- never unknown, so onUnknown does not catch it, and a detector written as equals true falls through to the fallback.`
	remedy := `Where a caller supplies a Boolean, write the detector as not(equals false), so that anything but an exact false fires it.`
	probes := []struct {
		name, claim, pack, flag, outcome string
	}{
		{"equals true on the string", trap, equalsTrue, `"true"`, "permitted"},
		{"equals true on the number", trap, equalsTrue, `1`, "permitted"},
		{"equals true on null", trap, equalsTrue, `null`, "permitted"},
		{"equals true on true", trap, equalsTrue, `true`, "violation"},
		{"not-equals false on the string", trap, notEqualsFalse, `"false"`, "violation"},
		{"the fail-safe detector on the string", remedy, failSafe, `"true"`, "violation"},
		{"the fail-safe detector on the number", remedy, failSafe, `1`, "violation"},
		{"the fail-safe detector on null", remedy, failSafe, `null`, "violation"},
		{"the fail-safe detector on false", remedy, failSafe, `false`, "permitted"},
	}
	text := strings.Join(strings.Fields(renderPinnedPrompt(t, "author_pack")), " ")
	var calls []string
	for index, probe := range probes {
		if !strings.Contains(text, probe.claim) {
			t.Fatalf("%s: the prompt no longer states %q; check the probe against the evaluator and update both", probe.name, probe.claim)
		}
		calls = append(calls, toolCall(t, index+1, "experimental_evaluate", map[string]any{"pack": probe.pack, "facts": `{"kind":"claim","flag":` + probe.flag + `}`}))
	}
	responses := runServer(t, strings.Join(calls, ""))
	if len(responses) != len(probes) {
		t.Fatalf("responses = %d, want %d", len(responses), len(probes))
	}
	for index, probe := range probes {
		outcome := responses[index]["result"].(map[string]any)
		if outcome["isError"] != false {
			t.Fatalf("%s: the evaluation was refused: %s", probe.name, toolText(t, outcome))
		}
		var evaluation result.Evaluation
		decodeStructured(t, outcome, &evaluation)
		if got := evaluation.Disposition; got.Kind != "outcome" || got.OutcomeID != probe.outcome || got.Handoff.State != "none" {
			t.Fatalf("%s: disposition = %+v, the prompt says outcome %q with no handoff", probe.name, got, probe.outcome)
		}
	}
}
