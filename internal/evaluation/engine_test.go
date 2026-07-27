package evaluation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	return NewEngine(validator)
}

func intakePack(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// factsJSON builds the /request facts document. An empty value omits the member.
func factsJSON(t *testing.T, requestType, completeness, appropriateness string, embargo *bool) []byte {
	t.Helper()
	request := map[string]any{}
	if requestType != "" {
		request["type"] = requestType
	}
	if completeness != "" {
		request["completeness"] = completeness
	}
	if appropriateness != "" {
		request["appropriateness"] = appropriateness
	}
	if embargo != nil {
		request["embargoedInformationToUnauthorizedRecipients"] = *embargo
	}
	data, err := json.Marshal(map[string]any{"request": request})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func boolPtr(v bool) *bool { return &v }

// TestRFC0006AppendixInstances executes the nine walked instances committed in
// spec RFC 0006's appendix, under its pinned semantics. These rows are the
// seed of the proposed evaluation corpus; every expected disposition was
// independently verified during the RFC's adversarial review.
func TestRFC0006AppendixInstances(t *testing.T) {
	engine := newTestEngine(t)
	pack := intakePack(t)
	no := boolPtr(false)
	yes := boolPtr(true)

	cases := []struct {
		name        string
		facts       []byte
		evidence    string
		wantKind    string
		wantOutcome string
		wantReasons []string
		wantHandoff string
	}{
		{
			name:        "1-not-applicable-type",
			facts:       factsJSON(t, "dataset-deletion", "", "", nil),
			evidence:    `{"intake-form":"present","sponsor-endorsement":"present"}`,
			wantKind:    "not-applicable",
			wantReasons: []string{"not-applicable"},
			wantHandoff: "requested",
		},
		{
			name:        "2-applicability-unknown",
			facts:       factsJSON(t, "", "complete", "pass", no),
			evidence:    `{"intake-form":"present","sponsor-endorsement":"present"}`,
			wantKind:    "unresolved",
			wantReasons: []string{"unknown"},
			wantHandoff: "requested",
		},
		{
			name:        "3-decline",
			facts:       factsJSON(t, "one-time-extract", "complete", "hard-fail", no),
			evidence:    `{"intake-form":"present","sponsor-endorsement":"present"}`,
			wantKind:    "outcome",
			wantOutcome: "decline-redirect",
			wantHandoff: "none",
		},
		{
			name:        "4-clarify-incomplete",
			facts:       factsJSON(t, "data-access", "incomplete", "pass", no),
			evidence:    `{"intake-form":"present","sponsor-endorsement":"present"}`,
			wantKind:    "outcome",
			wantOutcome: "clarify-return",
			wantHandoff: "none",
		},
		{
			name:        "5-clarify-pending",
			facts:       factsJSON(t, "data-access", "complete", "pending", no),
			evidence:    `{"intake-form":"present","sponsor-endorsement":"present"}`,
			wantKind:    "outcome",
			wantOutcome: "clarify-return",
			wantHandoff: "none",
		},
		{
			name:        "6-proceed",
			facts:       factsJSON(t, "new-data-pipeline", "complete", "pass", no),
			evidence:    `{"intake-form":"present","sponsor-endorsement":"present"}`,
			wantKind:    "outcome",
			wantOutcome: "proceed",
			wantHandoff: "none",
		},
		{
			name:        "7a-evidence-unknown",
			facts:       factsJSON(t, "dataset-onboarding", "complete", "pass", no),
			evidence:    `{"intake-form":"present","sponsor-endorsement":"unknown"}`,
			wantKind:    "unresolved",
			wantReasons: []string{"unknown"},
			wantHandoff: "requested",
		},
		{
			name:        "7b-evidence-absent",
			facts:       factsJSON(t, "dataset-onboarding", "complete", "pass", no),
			evidence:    `{"intake-form":"present","sponsor-endorsement":"absent"}`,
			wantKind:    "unresolved",
			wantReasons: []string{"missing-required-evidence"},
			wantHandoff: "requested",
		},
		{
			name:        "8-forced-outcome",
			facts:       factsJSON(t, "reporting-feed", "complete", "pass", yes),
			evidence:    `{"intake-form":"present","sponsor-endorsement":"present"}`,
			wantKind:    "outcome",
			wantOutcome: "decline-redirect",
			wantHandoff: "none",
		},
		{
			name:        "9-conflict",
			facts:       factsJSON(t, "pipeline-change", "incomplete", "hard-fail", no),
			evidence:    `{"intake-form":"present","sponsor-endorsement":"present"}`,
			wantKind:    "unresolved",
			wantReasons: []string{"conflict"},
			wantHandoff: "requested",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			output, failure := engine.Evaluate(pack, testCase.facts, []byte(testCase.evidence), nil, "test")
			if failure != nil {
				t.Fatalf("evaluation failed: %s: %s", failure.Code, failure.Message)
			}
			if !output.Experimental || output.ConformanceClaim != result.EvaluationClaim {
				t.Fatalf("evaluation must be labeled experimental with no claim: %+v", output)
			}
			disposition := output.Disposition
			if disposition.Kind != testCase.wantKind {
				t.Fatalf("kind = %q, want %q (%+v)", disposition.Kind, testCase.wantKind, disposition)
			}
			if disposition.OutcomeID != testCase.wantOutcome {
				t.Fatalf("outcomeId = %q, want %q", disposition.OutcomeID, testCase.wantOutcome)
			}
			wantReasons := testCase.wantReasons
			if wantReasons == nil {
				wantReasons = []string{}
			}
			if !reflect.DeepEqual(disposition.Reasons, wantReasons) {
				t.Fatalf("reasons = %v, want %v", disposition.Reasons, wantReasons)
			}
			if disposition.Handoff.State != testCase.wantHandoff {
				t.Fatalf("handoff = %q, want %q", disposition.Handoff.State, testCase.wantHandoff)
			}
			if testCase.wantHandoff == "requested" {
				target := disposition.Handoff.Target
				if target == nil || target.Kind != "human-role" || target.Name != "Intake reviewer" {
					t.Fatalf("a requested handoff must echo the declared target exactly: %+v", disposition.Handoff)
				}
			}
			if testCase.wantKind == "outcome" && len(disposition.Reasons) != 0 {
				t.Fatalf("an outcome result retains no reasons: %+v", disposition)
			}
		})
	}
}

// A forced outcome must skip normal rules entirely: the proceed rule would be
// true for instance 8's facts, and the trace must show it was never evaluated.
func TestForcedOutcomeSkipsRules(t *testing.T) {
	engine := newTestEngine(t)
	output, failure := engine.Evaluate(
		intakePack(t),
		factsJSON(t, "reporting-feed", "complete", "pass", boolPtr(true)),
		[]byte(`{"intake-form":"present","sponsor-endorsement":"present"}`),
		nil, "test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	skipped := 0
	for _, entry := range output.Trace {
		if entry.Stage == "rule" {
			if !entry.Skipped {
				t.Fatalf("rule %s must be skipped under a forced outcome: %+v", entry.ID, entry)
			}
			skipped++
		}
	}
	if skipped != 3 {
		t.Fatalf("all three rules should appear skipped in the trace, got %d", skipped)
	}
}

// Errors are not dispositions (ADR-0007): a non-conformant pack is refused
// with a self-sufficient message, never evaluated.
func TestNonConformantPackIsRefused(t *testing.T) {
	engine := newTestEngine(t)
	var document map[string]any
	if err := json.Unmarshal(intakePack(t), &document); err != nil {
		t.Fatal(err)
	}
	document["outcomes"] = document["outcomes"].([]any)[:1] // break references
	broken, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	_, failure := engine.Evaluate(broken, []byte(`{}`), nil, nil, "test")
	if failure == nil {
		t.Fatal("a non-conformant pack must be refused")
	}
	if failure.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" || failure.ExitCode != result.ExitInvalid {
		t.Fatalf("failure = %+v", failure)
	}
	if !strings.Contains(failure.Message, "JPS-") {
		t.Fatalf("the refusal must name the first diagnostic: %q", failure.Message)
	}
}

func TestEvidenceInputErrors(t *testing.T) {
	engine := newTestEngine(t)
	pack := intakePack(t)
	facts := factsJSON(t, "data-access", "complete", "pass", boolPtr(false))

	_, failure := engine.Evaluate(pack, facts, []byte(`{"no-such-requirement":"present"}`), nil, "test")
	if failure == nil || failure.Code != "JPS-EVALUATION-EVIDENCE-KEY" {
		t.Fatalf("an undeclared evidence key must be an input error: %+v", failure)
	}
	if !strings.Contains(failure.Message, "intake-form") {
		t.Fatalf("the error must list the declared ids: %q", failure.Message)
	}

	_, failure = engine.Evaluate(pack, facts, []byte(`{"intake-form":"maybe"}`), nil, "test")
	if failure == nil || failure.Code != "JPS-EVALUATION-EVIDENCE-VALUE" {
		t.Fatalf("a bad evidence value must be an input error: %+v", failure)
	}

	_, failure = engine.Evaluate(pack, nil, nil, nil, "test")
	if failure == nil || failure.Code != "JPS-EVALUATION-INPUT-MISSING" {
		t.Fatalf("an empty facts input must be refused: %+v", failure)
	}
}

// Omitting the evidence input entirely means every requirement is unknown, so
// the proceed rule cannot fire and RFC 0006's step-2 choice records unknown.
func TestOmittedEvidenceIsUnknown(t *testing.T) {
	engine := newTestEngine(t)
	output, failure := engine.Evaluate(
		intakePack(t),
		factsJSON(t, "data-access", "complete", "pass", boolPtr(false)),
		nil, nil, "test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if output.Disposition.Kind != "unresolved" || !reflect.DeepEqual(output.Disposition.Reasons, []string{"unknown"}) {
		t.Fatalf("omitted evidence must resolve as unknown: %+v", output.Disposition)
	}
}

// RFC 0006's step-2 clause "unknown iff any presence is unknown and none is
// false": when one required requirement is absent and another unknown, only
// missing-required-evidence is recorded.
func TestStepTwoAbsentBeatsUnknown(t *testing.T) {
	engine := newTestEngine(t)
	output, failure := engine.Evaluate(
		intakePack(t),
		factsJSON(t, "data-access", "complete", "pass", boolPtr(false)),
		[]byte(`{"intake-form":"absent","sponsor-endorsement":"unknown"}`),
		nil, "test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if !reflect.DeepEqual(output.Disposition.Reasons, []string{"missing-required-evidence"}) {
		t.Fatalf("absent evidence must record only missing-required-evidence: %+v", output.Disposition)
	}
}

// An exception whose condition is unknown with onUnknown: escalate records
// reason unknown and blocks resolution before any rule is evaluated; the
// unknown stays visible in the exception's trace entry.
func TestUnknownExceptionEscalates(t *testing.T) {
	engine := newTestEngine(t)
	output, failure := engine.Evaluate(
		intakePack(t),
		factsJSON(t, "data-access", "complete", "pass", nil), // embargo fact omitted
		[]byte(`{"intake-form":"present","sponsor-endorsement":"present"}`),
		nil, "test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if output.Disposition.Kind != "unresolved" || !reflect.DeepEqual(output.Disposition.Reasons, []string{"unknown"}) {
		t.Fatalf("an unknown escalating exception must block resolution: %+v", output.Disposition)
	}
	sawException := false
	for _, entry := range output.Trace {
		if entry.Stage == "exception" {
			if entry.Condition != "unknown" || entry.OnUnknown != "escalate" {
				t.Fatalf("the exception trace must show the unknown and its policy: %+v", entry)
			}
			sawException = true
		}
		if entry.Stage == "rule" && entry.Condition != "" && !entry.Skipped {
			t.Fatalf("no rule may be evaluated once step 5 blocks: %+v", entry)
		}
	}
	if !sawException {
		t.Fatalf("exception must appear in the trace: %+v", output.Trace)
	}
}

// Missing required evidence blocks a forced outcome: the exception fires, but
// step 5 produces unresolved after all exception effects were inspected.
func TestMissingEvidenceBlocksForcedOutcome(t *testing.T) {
	engine := newTestEngine(t)
	output, failure := engine.Evaluate(
		intakePack(t),
		factsJSON(t, "reporting-feed", "complete", "pass", boolPtr(true)), // embargo true -> force
		[]byte(`{"intake-form":"present","sponsor-endorsement":"absent"}`),
		nil, "test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if output.Disposition.Kind != "unresolved" || !reflect.DeepEqual(output.Disposition.Reasons, []string{"missing-required-evidence"}) {
		t.Fatalf("missing required evidence must block the forced outcome: %+v", output.Disposition)
	}
	forcedSeen := false
	for _, entry := range output.Trace {
		if entry.Stage == "exception" && entry.Effect == "force-outcome" {
			forcedSeen = true
		}
	}
	if !forcedSeen {
		t.Fatalf("the inspected forced effect must stay visible in the trace: %+v", output.Trace)
	}
}

// A pack declaring a required extension is refused without the capability and
// evaluated with it — errors are not dispositions, and the capability comes
// from the caller's supported set.
func TestRequiredExtensionCapabilityGate(t *testing.T) {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := set.Case("valid/required-extension-supported.json")
	if err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(t)

	_, failure := engine.Evaluate(pack, []byte(`{}`), nil, nil, "test")
	if failure == nil || failure.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" || failure.ExitCode != result.ExitUnsupported {
		t.Fatalf("an unsupported required extension must refuse evaluation: %+v", failure)
	}

	output, failure := engine.Evaluate(pack, []byte(`{}`), nil, []string{"com.example.review-policy"}, "test")
	if failure != nil {
		t.Fatalf("a supported required extension must evaluate: %s", failure.Message)
	}
	if output.Disposition.Kind == "" {
		t.Fatalf("a disposition must be produced: %+v", output)
	}
}

// Strict carrier rules apply to evaluation inputs: duplicate member names are
// refused in both the evidence and facts documents.
func TestDuplicateMembersAreInputErrors(t *testing.T) {
	engine := newTestEngine(t)
	pack := intakePack(t)
	facts := factsJSON(t, "data-access", "complete", "pass", boolPtr(false))

	_, failure := engine.Evaluate(pack, facts, []byte(`{"intake-form":"present","intake-form":"present"}`), nil, "test")
	if failure == nil || failure.Code != "JPS-EVALUATION-INPUT-JSON" {
		t.Fatalf("duplicate evidence keys must be an input error: %+v", failure)
	}

	_, failure = engine.Evaluate(pack, []byte(`{"request":{},"request":{}}`), nil, nil, "test")
	if failure == nil || failure.Code != "JPS-EVALUATION-INPUT-JSON" {
		t.Fatalf("duplicate facts members must be an input error: %+v", failure)
	}

	_, failure = engine.Evaluate(pack, facts, []byte(`["present"]`), nil, "test")
	if failure == nil || failure.Code != "JPS-EVALUATION-EVIDENCE-SHAPE" {
		t.Fatalf("a non-object evidence document must be a shape error: %+v", failure)
	}
}

// The byte-limit boundary lives in the engine so the MCP wire refuses the same
// oversized input the CLI's bounded reads refuse.
func TestOversizedInputsAreRefused(t *testing.T) {
	engine := newTestEngine(t)
	huge := make([]byte, carrier.HardMaxBytes+1)
	_, failure := engine.Evaluate(intakePack(t), huge, nil, nil, "test")
	if failure == nil || failure.Code != "JPS-RESOURCE-INPUT-BYTE-LIMIT" || failure.ExitCode != result.ExitIO {
		t.Fatalf("an oversized facts input must be refused with the byte-limit code: %+v", failure)
	}
}
