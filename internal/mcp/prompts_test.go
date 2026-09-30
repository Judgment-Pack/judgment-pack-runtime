package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// evaluationProbe is one probe the test_pack prompt describes: the sentences of
// the prompt that state what the evaluator answers, the inputs that make the
// probe, and that answer.
type evaluationProbe struct {
	name     string
	claims   []string
	facts    string
	evidence *string // nil sends no evidence document at all
	kind     string
	outcome  string
	reason   string
	handoff  string
	unmet    []result.UnmetEvidence
	trace    func(t *testing.T, trace []result.TraceEntry)
}

// The test_pack prompt tells an agent what the evaluator answers for each
// probe, and an agent that gets a different answer repairs a pack or a row that
// was right (issue #176). Each claim is held here to the evaluator's answer on
// the specification's own expense example, through the tool the prompt names,
// and the claim's words are pinned beside the probe, so that rewording a claim
// fails here until its probe is checked again.
func TestTestPackPromptStatesWhatTheEvaluatorAnswers(t *testing.T) {
	pack, err := os.ReadFile(filepath.Join("..", "artifacts", "jps", "0.2.0-draft", "cases", "valid", "minimal-expense-approval.json"))
	if err != nil {
		t.Fatal(err)
	}
	expense := func(members string) string {
		return `{"expense":{"type":"employee-expense","activeInvestigation":false,` + members + `}}`
	}
	document := func(text string) *string { return &text }
	present := document(`{"receipt":"present","cost-center":"present"}`)
	skippedRules := func(t *testing.T, trace []result.TraceEntry) {
		rules := 0
		for _, entry := range trace {
			if entry.Stage == "rule" {
				rules++
				if !entry.Skipped {
					t.Fatalf("rule %s was evaluated under a forced outcome: %+v", entry.ID, entry)
				}
			}
		}
		if rules == 0 {
			t.Fatal("the trace names no rule at all")
		}
	}
	notComparable := func(t *testing.T, trace []result.TraceEntry) {
		for _, entry := range trace {
			if entry.Stage != "rule" || entry.ID != "large-expense" {
				continue
			}
			for _, cause := range entry.UnknownCauses {
				if cause.Path != nil && *cause.Path == "/expense/amount" && cause.Cause == "not-comparable" && cause.FactType == "number" {
					return
				}
			}
			t.Fatalf("the rule's entry does not name the amount as not-comparable: %+v", entry)
		}
		t.Fatal("the trace has no entry for the rule that compares the amount")
	}
	absentRow := `a missing-required-evidence reason means the row's evidenceAvailability marked a requirement the pack requires "absent"`
	omittedRow := `while one the row omits is unknown and gives reason "unknown"`
	probes := []evaluationProbe{
		{name: "approve", claims: []string{`One instance per declared outcome, engineered so exactly that outcome's rule fires.`},
			facts: expense(`"category":"travel","amount":"120"`), evidence: present, kind: "outcome", outcome: "approve", handoff: "none"},
		{name: "reject", claims: []string{`One instance per declared outcome`},
			facts: expense(`"category":"personal","amount":"120"`), evidence: present, kind: "outcome", outcome: "reject", handoff: "none"},
		{name: "manual-review", claims: []string{`One instance per declared outcome`},
			facts: expense(`"category":"travel","amount":"6000"`), evidence: present, kind: "outcome", outcome: "manual-review", handoff: "none"},
		{name: "conflict", claims: []string{`facts that make two rules with DIFFERENT outcomes true at once. Expect unresolved with reason "conflict"`},
			facts: expense(`"category":"personal","amount":"6000"`), evidence: present, kind: "unresolved", reason: "conflict", handoff: "requested"},
		{name: "unknown fact", claims: []string{`omit one fact that rule needs. Expect unresolved with reason "unknown" and a handoff if the trigger is wired.`},
			facts: expense(`"category":"travel"`), evidence: present, kind: "unresolved", reason: "unknown", handoff: "requested"},
		{name: "receipt absent", claims: []string{
			`an evidence document that marks that one requirement "absent" and every other one "present". Expect unresolved with reason "missing-required-evidence", and unmetEvidence naming that requirement as absent.`,
			absentRow},
			facts: expense(`"category":"travel","amount":"120"`), evidence: document(`{"receipt":"absent","cost-center":"present"}`),
			kind: "unresolved", reason: "missing-required-evidence", handoff: "requested",
			unmet: []result.UnmetEvidence{{Requirement: "receipt", State: "absent"}}},
		{name: "cost-center absent", claims: []string{`A missing-evidence probe per required evidence requirement`, absentRow},
			facts: expense(`"category":"travel","amount":"120"`), evidence: document(`{"receipt":"present","cost-center":"absent"}`),
			kind: "unresolved", reason: "missing-required-evidence", handoff: "requested",
			unmet: []result.UnmetEvidence{{Requirement: "cost-center", State: "absent"}}},
		{name: "no evidence document", claims: []string{
			`evaluate with no evidence document at all. Every requirement is then unknown, not absent: expect unresolved with reason "unknown", and unmetEvidence naming each required requirement as unknown.`},
			facts: expense(`"category":"travel","amount":"120"`), evidence: nil,
			kind: "unresolved", reason: "unknown", handoff: "requested",
			unmet: []result.UnmetEvidence{{Requirement: "receipt", State: "unknown"}, {Requirement: "cost-center", State: "unknown"}}},
		{name: "requirement omitted", claims: []string{`A document that omits a requirement does the same for that one requirement.`, omittedRow},
			facts: expense(`"category":"travel","amount":"120"`), evidence: document(`{"receipt":"present"}`),
			kind: "unresolved", reason: "unknown", handoff: "requested",
			unmet: []result.UnmetEvidence{{Requirement: "cost-center", State: "unknown"}}},
		{name: "not applicable", claims: []string{`facts outside scope. Expect a not-applicable result, not an outcome.`},
			facts: `{"expense":{"type":"contractor-invoice","activeInvestigation":false,"category":"travel","amount":"120"}}`, evidence: present,
			kind: "not-applicable", reason: "not-applicable", handoff: "requested"},
		{name: "forced outcome", claims: []string{`facts that satisfy the exception AND a rule it should override. Expect the exception's outcome with the rule skipped.`},
			facts: `{"expense":{"type":"employee-expense","activeInvestigation":true,"category":"travel","amount":"120"}}`, evidence: present,
			kind: "outcome", outcome: "manual-review", handoff: "none", trace: skippedRules},
		{name: "number where a decimal string belongs", claims: []string{
			`supply the value as a JSON number instead of a decimal string. Expect unknown behavior per the rule's onUnknown, and the rule's trace entry naming the fact as not-comparable`},
			facts: expense(`"category":"travel","amount":120`), evidence: present,
			kind: "unresolved", reason: "unknown", handoff: "requested", trace: notComparable},
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
		arguments := map[string]any{"pack": string(pack), "facts": probe.facts}
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
		if got.Kind != probe.kind || got.OutcomeID != probe.outcome || got.Handoff.State != probe.handoff {
			t.Fatalf("%s: disposition = %+v, the prompt says kind %q outcome %q handoff %q", probe.name, got, probe.kind, probe.outcome, probe.handoff)
		}
		if probe.reason != "" && !slices.Contains(got.Reasons, probe.reason) {
			t.Fatalf("%s: reasons = %v, the prompt says %q", probe.name, got.Reasons, probe.reason)
		}
		if probe.reason == "" && len(got.Reasons) != 0 {
			t.Fatalf("%s: reasons = %v, the prompt promises an outcome", probe.name, got.Reasons)
		}
		if !reflect.DeepEqual(evaluation.UnmetEvidence, probe.unmet) {
			t.Fatalf("%s: unmetEvidence = %+v, want %+v", probe.name, evaluation.UnmetEvidence, probe.unmet)
		}
		if probe.trace != nil {
			probe.trace(t, evaluation.Trace)
		}
	}
}
