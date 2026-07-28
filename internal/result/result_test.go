package result

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The in-band claim value and the claim document are one statement in two places,
// so they are held to each other. §3.4.1 scopes a claim to one exact specVersion
// and §11 makes it non-inheritable, which means a bumped EvaluatorSpecVersion is
// not a version bump but a claim this runtime has not yet earned: this test fails
// until CONFORMANCE.md is restated for the new version, with that version's corpus
// run. The document must also say the two things §3.4.1 requires in the claim's own
// words — that every row passed, and that corpus results are not exhaustive
// evidence — rather than leaving either to a reader's inference.
func TestConformanceClaimDocumentAgreesWithTheInBandValue(t *testing.T) {
	claim, err := os.ReadFile(filepath.Join("..", "..", "CONFORMANCE.md"))
	if err != nil {
		t.Fatalf("the claim is one document, and it must exist: %v", err)
	}
	// Line wrapping is a typesetting choice; the sentence is the claim.
	document := strings.Join(strings.Fields(string(claim)), " ")
	if want := "evaluator-conformance:" + EvaluatorSpecVersion; EvaluationClaim != want {
		t.Fatalf("the in-band claim names the contract version it is made against: got %q, want %q", EvaluationClaim, want)
	}
	for _, required := range []string{
		"evaluator conformance",
		EvaluatorSpecVersion,
		"suiteVersion",
		"passed",
		"not exhaustive evidence",
		"§3.4.1",
	} {
		if !strings.Contains(document, required) {
			t.Fatalf("CONFORMANCE.md must state %q in the claim's own words", required)
		}
	}
	if !strings.Contains(document, "Every row of `suiteVersion` `"+EvaluatorSpecVersion+"` passed.") {
		t.Fatal("§3.4.1 requires the claim to state, in its own words, that every row of the named corpus version passed")
	}
}

// The values below are the machine-facing contract: consumers branch on the
// exit status and on outputVersion. Changing any of them is a compatibility
// break that must be a deliberate, documented decision rather than a side
// effect of an unrelated edit, so they are pinned by literal here.

func TestOutputVersionIsPinned(t *testing.T) {
	if OutputVersion != "1" {
		t.Fatalf("outputVersion is part of the machine contract: got %q, want %q", OutputVersion, "1")
	}
	if CLIName != "judgment-pack" {
		t.Fatalf("tool.name is part of the machine contract: got %q", CLIName)
	}
}

func TestExitClassesArePinned(t *testing.T) {
	for _, pinned := range []struct {
		name string
		got  int
		want int
	}{
		{"ExitSuccess", ExitSuccess, 0},
		{"ExitInvalid", ExitInvalid, 1},
		{"ExitUnsupported", ExitUnsupported, 2},
		{"ExitInvocation", ExitInvocation, 3},
		{"ExitIO", ExitIO, 4},
		{"ExitInternal", ExitInternal, 5},
	} {
		if pinned.got != pinned.want {
			t.Errorf("%s is part of the machine contract: got %d, want %d", pinned.name, pinned.got, pinned.want)
		}
	}
}

// Every payload carries the same four identifying members. A rename of any of
// them is invisible to Go's type checker but breaks every consumer, so assert
// the emitted JSON rather than the struct.
func TestEveryPayloadCarriesTheCommonEnvelope(t *testing.T) {
	for _, payload := range []struct {
		name  string
		value any
	}{
		{"Validation", Validation{OutputVersion: OutputVersion, Tool: CurrentTool(), Command: "spec validate", Status: "valid"}},
		{"Suite", Suite{OutputVersion: OutputVersion, Tool: CurrentTool(), Command: "spec test-conformance", Status: "valid"}},
		{"Schema", Schema{OutputVersion: OutputVersion, Tool: CurrentTool(), Command: "spec schema", Status: "valid"}},
		{"Version", Version{OutputVersion: OutputVersion, Tool: CurrentTool(), Command: "version", Status: "valid"}},
	} {
		encoded, err := json.Marshal(payload.value)
		if err != nil {
			t.Fatalf("%s: %v", payload.name, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("%s: %v", payload.name, err)
		}
		for _, member := range []string{"outputVersion", "tool", "command", "status"} {
			if _, present := decoded[member]; !present {
				t.Errorf("%s payload is missing the %q member: %s", payload.name, member, encoded)
			}
		}
		if decoded["outputVersion"] != "1" {
			t.Errorf("%s payload reports outputVersion %v, want \"1\"", payload.name, decoded["outputVersion"])
		}
	}
}

// The §8.3 disposition serializes as its own RFC 8785 canonical form, and these
// two values are the specification's own illustrative canonicalizations. If
// either byte sequence changes, the portability property §8.3 states — two
// implementations producing byte-identical canonicalized dispositions — has
// changed with it.
func TestDispositionCanonicalFormMatchesTheSpecificationExamples(t *testing.T) {
	cases := []struct {
		name        string
		disposition Disposition
		want        string
	}{
		{
			name:        "an outcome retains no reasons and requests no handoff",
			disposition: Disposition{Kind: "outcome", OutcomeID: "proceed", Reasons: []string{}, Handoff: Handoff{State: "none"}},
			want:        `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"proceed","reasons":[]}`,
		},
		{
			name: "a requested handoff names the reasons that triggered it",
			disposition: Disposition{
				Kind:    "unresolved",
				Reasons: []string{"missing-required-evidence"},
				Handoff: Handoff{State: "requested", TriggeredBy: []string{"missing-required-evidence"}},
			},
			want: `{"handoff":{"state":"requested","triggeredBy":["missing-required-evidence"]},"kind":"unresolved","reasons":["missing-required-evidence"]}`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			canonical, err := testCase.disposition.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			if string(canonical) != testCase.want {
				t.Fatalf("canonical form = %s, want %s", canonical, testCase.want)
			}
			// The JSON serialization of the disposition is that same canonical
			// form, so no payload can carry a second, differently ordered one.
			encoded, err := json.Marshal(testCase.disposition)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != testCase.want {
				t.Fatalf("json.Marshal = %s, want the canonical form %s", encoded, testCase.want)
			}
		})
	}
}

// Sets are sets: order and duplicates in the assembled value cannot change the
// canonical bytes, because §8.3 makes serialized order never a difference in the
// disposition.
func TestDispositionCanonicalFormIsStableUnderSetOrderAndDuplicates(t *testing.T) {
	sorted := Disposition{
		Kind:    "unresolved",
		Reasons: []string{"conflict", "unknown"},
		Handoff: Handoff{State: "requested", TriggeredBy: []string{"conflict"}},
	}
	scrambled := Disposition{
		Kind:    "unresolved",
		Reasons: []string{"unknown", "conflict", "unknown"},
		Handoff: Handoff{State: "requested", TriggeredBy: []string{"conflict", "conflict"}},
	}
	first, err := sorted.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	second, err := scrambled.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("canonical forms differ: %s and %s", first, second)
	}
	want := `{"handoff":{"state":"requested","triggeredBy":["conflict"]},"kind":"unresolved","reasons":["conflict","unknown"]}`
	if string(first) != want {
		t.Fatalf("canonical form = %s, want %s", first, want)
	}
}

// An absent member is omitted, never serialized as null (§8.3).
func TestAbsentDispositionMembersAreOmitted(t *testing.T) {
	encoded, err := Disposition{Kind: "not-applicable", Reasons: []string{"not-applicable"}, Handoff: Handoff{State: "none"}}.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"handoff":{"state":"none"},"kind":"not-applicable","reasons":["not-applicable"]}`
	if string(encoded) != want {
		t.Fatalf("canonical form = %s, want %s", encoded, want)
	}
}

// The §8.4 class identifiers are the portable part of an evaluation error and
// are pinned here, as the exit classes are: they are bare kebab-case names that
// cannot collide with an implementation-defined reverse-domain class.
func TestEvaluationErrorClassesArePinned(t *testing.T) {
	for _, pinned := range []struct {
		got  string
		want string
	}{
		{ClassPackNotConformant, "pack-not-conformant"},
		{ClassMalformedInput, "malformed-input"},
		{ClassUnsupportedRequiredExtension, "unsupported-required-extension"},
		{ClassResourceExhaustion, "resource-exhaustion"},
		{PhasePreflight, "preflight"},
		{PhaseEvaluation, "evaluation"},
	} {
		if pinned.got != pinned.want {
			t.Errorf("evaluation-error identifier = %q, want %q", pinned.got, pinned.want)
		}
	}
}

// An evaluation error carries its class and phase in band and no disposition.
func TestEvaluationErrorPayloadNamesTheClass(t *testing.T) {
	output := NewEvaluationError("experimental evaluate", "error", ClassMalformedInput, PhasePreflight, "JPS-EVALUATION-EVIDENCE-KEY", "detail")
	if output.EvaluationError == nil || output.EvaluationError.Class != ClassMalformedInput || output.EvaluationError.Phase != PhasePreflight {
		t.Fatalf("the payload must name the class and phase: %+v", output.EvaluationError)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"disposition"`) {
		t.Fatalf("an evaluation error carries no disposition: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"evaluationError":{"class":"malformed-input","phase":"preflight","evaluatorSpecVersion":"0.2.0-draft"}`) {
		t.Fatalf("the class is not in band: %s", encoded)
	}
	// An ordinary operational refusal is not classified at all.
	if NewOperationalError("version", "JPS-INVOCATION-FORMAT", "detail").EvaluationError != nil {
		t.Fatal("a refusal §8.4 does not classify must carry no class")
	}
}

// Canonical enumerates the disposition's members by hand, so the struct tags of
// Disposition and Handoff decide nothing about what a payload carries. A member
// added to either type would therefore be dropped from every payload silently,
// while the doc comment still promises "these members and no others". This test
// is the correspondence: every exported field of both types must appear, by its
// JSON name, in the canonical form of a fully populated value.
func TestEveryDispositionMemberReachesTheCanonicalForm(t *testing.T) {
	// No single value carries every member: §8.3 makes outcomeId and a non-empty
	// reasons set mutually exclusive, and triggeredBy is a non-empty subset of
	// reasons, so a disposition carrying outcomeId can carry neither. Each type's
	// members are therefore canonicalized from the legal value that carries them and
	// the two results are read together. Assembling one illegal value carrying all
	// four would test nothing: Canonical refuses it.
	outcome := Disposition{
		Kind:      "outcome",
		OutcomeID: "proceed",
		Reasons:   []string{},
		Handoff:   Handoff{State: "none"},
	}
	unresolved := Disposition{
		Kind:    "unresolved",
		Reasons: []string{"conflict"},
		Handoff: Handoff{State: "requested", TriggeredBy: []string{"conflict"}},
	}
	encoded := map[string]string{}
	for _, disposition := range []Disposition{outcome, unresolved} {
		canonical, err := disposition.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		encoded[disposition.Kind] = string(canonical)
	}
	for _, subject := range []reflect.Type{reflect.TypeOf(Disposition{}), reflect.TypeOf(Handoff{})} {
		for index := 0; index < subject.NumField(); index++ {
			field := subject.Field(index)
			if !field.IsExported() {
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				t.Fatalf("%s.%s carries no JSON member name", subject.Name(), field.Name)
			}
			if !strings.Contains(encoded["outcome"], `"`+name+`":`) && !strings.Contains(encoded["unresolved"], `"`+name+`":`) {
				t.Fatalf("%s.%s (%q) reaches no canonical form: Canonical() must enumerate it (§8.3)", subject.Name(), field.Name, name)
			}
		}
	}
}

// Canonical is the one place canonicalization lives, so it is also the place that
// refuses a disposition §8.3 does not admit. Each value below violates one
// invariant that section states about the disposition alone — a vocabulary, a
// presence rule, the not-applicable reason set, or the subset rule — and none of
// them may be serialized.
func TestCanonicalRefusesADispositionSection83Forbids(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		disposition Disposition
	}{
		{
			name:        "a kind outside the enumeration",
			disposition: Disposition{Kind: "escalated", Reasons: []string{"conflict"}, Handoff: Handoff{State: "none"}},
		},
		{
			name:        "an empty kind",
			disposition: Disposition{Reasons: []string{"conflict"}, Handoff: Handoff{State: "none"}},
		},
		{
			name:        "a handoff state outside the enumeration",
			disposition: Disposition{Kind: "unresolved", Reasons: []string{"conflict"}, Handoff: Handoff{State: "pending"}},
		},
		{
			name:        "an empty handoff state",
			disposition: Disposition{Kind: "unresolved", Reasons: []string{"conflict"}, Handoff: Handoff{}},
		},
		{
			name:        "a reason outside the vocabulary",
			disposition: Disposition{Kind: "unresolved", Reasons: []string{"needs-review"}, Handoff: Handoff{State: "none"}},
		},
		{
			name:        "a not-applicable result carrying another reason beside its own",
			disposition: Disposition{Kind: "not-applicable", Reasons: []string{"not-applicable", "unknown"}, Handoff: Handoff{State: "none"}},
		},
		{
			name:        "a not-applicable result whose one reason is a different one",
			disposition: Disposition{Kind: "not-applicable", Reasons: []string{"unknown"}, Handoff: Handoff{State: "none"}},
		},
		{
			name:        "a trigger that is not a retained reason",
			disposition: Disposition{Kind: "unresolved", Reasons: []string{"conflict"}, Handoff: Handoff{State: "requested", TriggeredBy: []string{"no-match"}}},
		},
		{
			// An outcome retains no reasons, so any trigger at all is outside the
			// retained set: §8.3's subset rule is what makes this illegal rather than
			// merely something this runtime's engine never produces.
			name:        "an outcome with a requested handoff",
			disposition: Disposition{Kind: "outcome", OutcomeID: "proceed", Reasons: []string{}, Handoff: Handoff{State: "requested", TriggeredBy: []string{"conflict"}}},
		},
		{
			name:        "an outcome with no outcomeId",
			disposition: Disposition{Kind: "outcome", Reasons: []string{}, Handoff: Handoff{State: "none"}},
		},
		{
			name:        "an outcome that retains a reason",
			disposition: Disposition{Kind: "outcome", OutcomeID: "proceed", Reasons: []string{"conflict"}, Handoff: Handoff{State: "none"}},
		},
		{
			name:        "a non-outcome carrying an outcomeId",
			disposition: Disposition{Kind: "unresolved", OutcomeID: "proceed", Reasons: []string{"conflict"}, Handoff: Handoff{State: "none"}},
		},
		{
			name:        "a non-outcome retaining no reason",
			disposition: Disposition{Kind: "unresolved", Reasons: []string{}, Handoff: Handoff{State: "none"}},
		},
		{
			name:        "a requested handoff with no trigger",
			disposition: Disposition{Kind: "unresolved", Reasons: []string{"conflict"}, Handoff: Handoff{State: "requested"}},
		},
		{
			name:        "a trigger without a requested handoff",
			disposition: Disposition{Kind: "unresolved", Reasons: []string{"conflict"}, Handoff: Handoff{State: "none", TriggeredBy: []string{"conflict"}}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := testCase.disposition.Canonical(); err == nil {
				t.Fatal("Canonical must refuse a disposition §8.3 does not admit")
			}
			if _, err := json.Marshal(testCase.disposition); err == nil {
				t.Fatal("MarshalJSON must refuse it too: it is the same canonicalization")
			}
		})
	}
}
