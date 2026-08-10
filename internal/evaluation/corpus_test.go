package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The bundled evaluation corpus runs in full, and every row's expected
// disposition is compared to this evaluator's as RFC 8785 canonical bytes — the
// comparison §8.3 defines. Passing every row is the evidence §3.4 requires of the
// claim this runtime makes (CONFORMANCE.md) and is not that claim: this test
// asserts nothing beyond the rows, and §3.4.1 is explicit that the rows are not
// exhaustive evidence of the claim either. A regression here is a failing row, and
// a failing row blocks the claim.
//
// A failing row does not decide who is wrong: §3.4 makes a divergence as likely
// to be a defect in the row as in this implementation, and the failure message
// prints both byte sequences so the question can be adjudicated against the
// specification text rather than guessed at.
func TestBundledEvaluationCorpus(t *testing.T) {
	engine := newTestEngine(t)
	output, failure := engine.RunCorpus(artifacts.EvaluatorDraftVersion, "test")
	if failure != nil {
		t.Fatalf("the bundled corpus must run: %s: %s", failure.Code, failure.Message)
	}
	if output.SuiteVersion != artifacts.EvaluatorDraftVersion || output.SpecVersion != artifacts.EvaluatorDraftVersion {
		t.Fatalf("the corpus must be the one published for its own version: %+v", output)
	}
	if output.Summary.Total != 20 {
		t.Fatalf("the %s corpus has twenty rows, got %d", artifacts.EvaluatorDraftVersion, output.Summary.Total)
	}
	if !output.Experimental || output.ConformanceClaimReference != result.EvaluationClaimReference || output.Label != result.EvaluationCorpusLabel {
		t.Fatalf("a corpus run references the claim it is evidence for, in band, and states none itself: %+v", output)
	}
	for _, item := range output.Cases {
		if item.Status != "passed" {
			t.Errorf("row %s [%s] did not match: %s\n  expected: %s\n  actual:   %s\n  expected class %q, actual class %q",
				item.ID, item.SpecSection, item.Detail, item.Expected, item.Actual, item.ExpectedErrorClass, item.ActualErrorClass)
		}
	}
	if output.Status != "passed" || output.Summary.Passed != output.Summary.Total {
		t.Fatalf("summary = %+v, status %q", output.Summary, output.Status)
	}
}

// Every row's produced disposition is a legal §8.3 disposition, whatever the row
// expected: outcomeId present exactly when the kind is an outcome, reasons empty
// exactly then, and triggeredBy present exactly when a handoff is requested and
// always a subset of the retained reasons. The corpus rows are the inputs; these
// invariants are the shape Core requires of any disposition at all.
func TestCorpusRowsProduceLegalDispositions(t *testing.T) {
	engine := newTestEngine(t)
	set, err := artifacts.Load(artifacts.EvaluatorDraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	manifest, failure := loadCorpusManifest(set, artifacts.EvaluatorDraftVersion)
	if failure != nil {
		t.Fatalf("%s: %s", failure.Code, failure.Message)
	}
	for _, item := range manifest.Cases {
		t.Run(item.ID, func(t *testing.T) {
			pack, err := set.EvaluationPack(item.Pack)
			if err != nil {
				t.Fatal(err)
			}
			output, failure := engine.Evaluate(pack, item.Facts, item.EvidenceAvailability, item.SupportedExtensions, "test")
			if failure != nil {
				if item.ExpectedErrorClass == "" {
					t.Fatalf("the row expects a disposition: %s: %s", failure.Code, failure.Message)
				}
				return
			}
			disposition := output.Disposition
			switch disposition.Kind {
			case "outcome":
				if disposition.OutcomeID == "" || len(disposition.Reasons) != 0 {
					t.Fatalf("an outcome names one outcome and retains no reasons: %+v", disposition)
				}
			case "not-applicable", "unresolved":
				if disposition.OutcomeID != "" || len(disposition.Reasons) == 0 {
					t.Fatalf("a non-outcome result names no outcome and retains at least one reason: %+v", disposition)
				}
			default:
				t.Fatalf("kind %q is outside the §8.3 vocabulary", disposition.Kind)
			}
			if disposition.Kind == "not-applicable" && !slices.Equal(disposition.Reasons, []string{"not-applicable"}) {
				t.Fatalf("a not-applicable result carries exactly that one reason: %+v", disposition)
			}
			if !slices.IsSorted(disposition.Reasons) {
				t.Fatalf("reasons are serialized in ascending code-point order: %+v", disposition.Reasons)
			}
			switch disposition.Handoff.State {
			case "requested":
				if len(disposition.Handoff.TriggeredBy) == 0 {
					t.Fatalf("a requested handoff names a non-empty trigger set: %+v", disposition.Handoff)
				}
			case "none":
				if disposition.Handoff.TriggeredBy != nil {
					t.Fatalf("triggeredBy is present only when a handoff is requested: %+v", disposition.Handoff)
				}
			default:
				t.Fatalf("handoff state %q is outside the §8.3 vocabulary", disposition.Handoff.State)
			}
			for _, trigger := range disposition.Handoff.TriggeredBy {
				if !slices.Contains(disposition.Reasons, trigger) {
					t.Fatalf("triggeredBy must be a subset of reasons: %+v", disposition)
				}
			}
		})
	}
}

// A specification version whose Core defines no evaluator class publishes no
// evaluation corpus, and asking for one says so rather than inventing a run.
func TestCorpusIsOnlyOfferedWhereItIsPublished(t *testing.T) {
	engine := newTestEngine(t)
	if _, failure := engine.RunCorpus(artifacts.DraftVersion, "test"); failure == nil {
		t.Fatalf("JPS %s publishes no evaluation corpus", artifacts.DraftVersion)
	} else if failure.Code != "JPS-CAPABILITY-EVALUATION-CORPUS" {
		t.Fatalf("code = %q, want JPS-CAPABILITY-EVALUATION-CORPUS", failure.Code)
	}
	if _, failure := engine.RunCorpus("9.9.9-draft", "test"); failure == nil || failure.Code != "JPS-CAPABILITY-SPEC-VERSION" {
		t.Fatalf("an unbundled version has no corpus: %+v", failure)
	}
}

// The three states of an expected handoff target are three different statements,
// and the carrier keeps them apart (ADR-0025). Absent is a nil RawMessage and
// reaches no comparison at all; the literal null decodes to no target and renders
// as null; an object decodes to that target and renders canonically. A carrier
// that folded the first two together would make "I assert nothing" and "I assert
// there is no target" the same row, which is the one distinction this member
// exists for.
func TestExpectedHandoffTargetSeparatesAbsentFromNull(t *testing.T) {
	var row MatrixCase
	if err := json.Unmarshal([]byte(`{"id":"absent","facts":{}}`), &row); err != nil {
		t.Fatal(err)
	}
	if row.ExpectedHandoffTarget != nil {
		t.Fatalf("an absent member must stay absent: %q", row.ExpectedHandoffTarget)
	}
	if err := json.Unmarshal([]byte(`{"id":"null","facts":{},"expectedHandoffTarget":null}`), &row); err != nil {
		t.Fatal(err)
	}
	if string(row.ExpectedHandoffTarget) != "null" {
		t.Fatalf("a stated null must survive the carrier: %q", row.ExpectedHandoffTarget)
	}

	for name, probe := range map[string]struct {
		raw      string
		rendered string
		refused  bool
	}{
		"null":              {raw: `null`, rendered: `null`},
		"a target":          {raw: `{"kind":"human-role","name":"Intake reviewer"}`, rendered: `{"kind":"human-role","name":"Intake reviewer"}`},
		"reordered members": {raw: `{"name":"Intake reviewer","kind":"human-role"}`, rendered: `{"kind":"human-role","name":"Intake reviewer"}`},
		"a bare string":     {raw: `"Intake reviewer"`, refused: true},
		"no name":           {raw: `{"kind":"human-role"}`, refused: true},
		"an empty kind":     {raw: `{"kind":"","name":"Intake reviewer"}`, refused: true},
		"a null member":     {raw: `{"kind":"human-role","name":null}`, refused: true},
		"an unknown member": {raw: `{"kind":"human-role","name":"Intake reviewer","urgency":"high"}`, refused: true},
		// encoding/json matches member names case-insensitively even under
		// DisallowUnknownFields, so a struct decode reads every spelling below as
		// an exact member and lets an alias overwrite the one an author wrote. A
		// closed shape has to refuse them, and none of them is a decoder option.
		"a capitalized kind":            {raw: `{"Kind":"human-role","name":"Intake reviewer"}`, refused: true},
		"a capitalized name":            {raw: `{"kind":"human-role","Name":"Intake reviewer"}`, refused: true},
		"a shouted member":              {raw: `{"KIND":"human-role","NAME":"Ops"}`, refused: true},
		"an alias beside the member":    {raw: `{"kind":"human-role","name":"Intake reviewer","Kind":"queue"}`, refused: true},
		"a duplicated member":           {raw: `{"kind":"human-role","kind":"queue","name":"Ops"}`, refused: true},
		"trailing JSON after null":      {raw: `null {"kind":"queue","name":"Ops"}`, refused: true},
		"trailing JSON after an object": {raw: `{"kind":"queue","name":"Ops"} null`, refused: true},
		"a nested value":                {raw: `{"kind":{"of":"thing"},"name":"Ops"}`, refused: true},
		"an array":                      {raw: `[{"kind":"queue","name":"Ops"}]`, refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			target, err := DecodeHandoffTarget([]byte(probe.raw))
			if probe.refused {
				if err == nil {
					t.Fatalf("this expectation must be refused: %+v", target)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := target.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			if string(rendered) != probe.rendered {
				t.Fatalf("rendered = %s, want %s", rendered, probe.rendered)
			}
		})
	}
}

// The bundled corpus payload is byte-identical to what it was before the
// handoff-target member existed, and this pins it rather than asserting it
// (ADR-0025).
//
// The claim the ADR makes is that a row asserting nothing reports nothing new.
// Checking empty Go fields would not catch a serialization change that started
// writing them, so the check is over the bytes: the digest below was taken from
// the same run on the commit this change branched from, and no member of the
// corpus manifest's closed case object can ever declare the new member.
func TestBundledCorpusPayloadIsUnchangedByTheHandoffTargetMember(t *testing.T) {
	const goldenCases = "9947a8e5cf7f4930ed090af5bdaa1b183e46257aafa015bad73e4efe906770ce"

	engine := newTestEngine(t)
	run, failure := engine.RunCorpus(artifacts.EvaluatorDraftVersion, "test")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	encoded, err := json.Marshal(run.Cases)
	if err != nil {
		t.Fatal(err)
	}
	if digest := hex.EncodeToString(sliceDigest(encoded)); digest != goldenCases {
		t.Fatalf("the corpus row payload changed: %s\nwant %s\n%s", digest, goldenCases, encoded)
	}
	for _, member := range []string{"expectedHandoffTarget", "actualHandoffTarget"} {
		if bytes.Contains(encoded, []byte(member)) {
			t.Fatalf("no corpus row may carry %q: %s", member, encoded)
		}
	}
}

func sliceDigest(data []byte) []byte {
	digest := sha256.Sum256(data)
	return digest[:]
}

// The three assertion states survive a round trip through the carrier type, and
// the middle one is why omitempty is there (ADR-0025).
//
// Without it, marshaling a row that asserted nothing writes
// expectedHandoffTarget: null, which reloads as the assertion that the
// evaluation reports no target — a round trip that invents an expectation
// nobody wrote, and the one confusion this member's three states exist to
// prevent.
func TestMatrixCaseRoundTripsTheThreeAssertionStates(t *testing.T) {
	for name, probe := range map[string]struct {
		row    string
		member string
	}{
		"absent":   {row: `{"id":"a","facts":{}}`, member: ""},
		"null":     {row: `{"id":"a","facts":{},"expectedHandoffTarget":null}`, member: `"expectedHandoffTarget":null`},
		"a target": {row: `{"id":"a","facts":{},"expectedHandoffTarget":{"kind":"queue","name":"Ops"}}`, member: `"expectedHandoffTarget":{"kind":"queue","name":"Ops"}`},
	} {
		t.Run(name, func(t *testing.T) {
			var row MatrixCase
			if err := json.Unmarshal([]byte(probe.row), &row); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			if probe.member == "" {
				if bytes.Contains(encoded, []byte("expectedHandoffTarget")) {
					t.Fatalf("an absent assertion must not be written back as null: %s", encoded)
				}
			} else if !bytes.Contains(encoded, []byte(probe.member)) {
				t.Fatalf("the assertion must survive the round trip: %s", encoded)
			}
			// And the reload states what the original stated.
			var again MatrixCase
			if err := json.Unmarshal(encoded, &again); err != nil {
				t.Fatal(err)
			}
			if string(again.ExpectedHandoffTarget) != string(row.ExpectedHandoffTarget) {
				t.Fatalf("reload = %q, want %q", again.ExpectedHandoffTarget, row.ExpectedHandoffTarget)
			}
		})
	}
}

// Equality is decided on the decoded targets and never on their capped
// renderings (ADR-0025).
//
// A rendering past result.HandoffTargetBudget keeps a prefix and sixty-four bits
// of SHA-256. Two targets sharing that prefix still render differently in
// practice, and "in practice" is not what a suite's verdict may rest on: a
// digest deciding pass or fail is a probabilistic answer to a question with an
// exact one. The pair below is the shape that would be decided wrongly if the
// rendering were the key — a shared 256-byte prefix, differing far past it — and
// the comparison must separate them on the strings.
func TestHandoffTargetEqualityIsDecidedOnTheDecodedValues(t *testing.T) {
	prefix := strings.Repeat("a", 4096)
	left := &result.HandoffTarget{Kind: "queue", Name: prefix + "left"}
	right := &result.HandoffTarget{Kind: "queue", Name: prefix + "right"}
	if sameHandoffTarget(left, right) {
		t.Fatal("two different targets are two targets, however they render")
	}
	if !sameHandoffTarget(left, &result.HandoffTarget{Kind: "queue", Name: prefix + "left"}) {
		t.Fatal("two equal targets are one target")
	}
	// Presence is compared as presence: neither direction is an equality.
	if sameHandoffTarget(nil, left) || sameHandoffTarget(left, nil) {
		t.Fatal("a target and no target are never the same statement")
	}
	if !sameHandoffTarget(nil, nil) {
		t.Fatal("no target and no target are the same statement")
	}
	// Each member is compared in full, so a difference in either separates them.
	if sameHandoffTarget(left, &result.HandoffTarget{Kind: "system", Name: left.Name}) {
		t.Fatal("the kind is part of the target")
	}
}

// One pack's configured target is rendered once per admitted pack, not once per
// asserting row (ADR-0025).
//
// §8.1 gives a pack one escalation target, so a suite asserting a target on n
// rows would otherwise canonicalize and hash the same authored string n times.
// That work is invisible to the retained-bytes budget — what that budget bounds
// is what a report keeps, and this is what producing it costs — so a cache whose
// only effect is a timing difference would be a cache nothing could hold to its
// purpose. This counts the renderings instead.
func TestOnePacksHandoffTargetIsRenderedOncePerRun(t *testing.T) {
	engine := newTestEngine(t)
	pack, err := os.ReadFile(filepath.Join("testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	admitted := engine.AdmitPack(pack)
	row := MatrixCase{
		Facts:                 json.RawMessage(`{"request":{"type":"unrelated"}}`),
		ExpectedDisposition:   json.RawMessage(`{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`),
		ExpectedHandoffTarget: json.RawMessage(`{"kind":"human-role","name":"Intake reviewer"}`),
	}
	for index := range 50 {
		row.ID = fmt.Sprint(index)
		outcome := engine.RunCaseAdmitted(admitted, row, "test")
		if outcome.Status != "passed" {
			t.Fatalf("row %d: %+v", index, outcome)
		}
		if outcome.ActualHandoffTarget != `{"kind":"human-role","name":"Intake reviewer"}` {
			t.Fatalf("the memo must return the rendering, not a stale or empty one: %+v", outcome)
		}
	}
	if renders := admitted.handoffTargetRenders(); renders != 1 {
		t.Fatalf("one pack, one target, one rendering: %d", renders)
	}

	// A row that asserts nothing renders nothing, and a null-reporting
	// evaluation never reaches the memo at all.
	outcome := engine.RunCaseAdmitted(admitted, MatrixCase{
		ID:                  "silent",
		Facts:               row.Facts,
		ExpectedDisposition: row.ExpectedDisposition,
	}, "test")
	if outcome.Status != "passed" || outcome.ActualHandoffTarget != "" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if renders := admitted.handoffTargetRenders(); renders != 1 {
		t.Fatalf("a row that asks nothing costs nothing: %d", renders)
	}
}
