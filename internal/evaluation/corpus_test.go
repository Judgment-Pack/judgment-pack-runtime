package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
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

// The row comparator renders no escalation target: it reports the rendering it
// was handed (ADR-0025).
//
// This is the property the previous three attempts kept failing to have. §8.1
// gives a pack one escalation target, so a suite asserting one on n rows must
// not canonicalize and hash the same authored string n times — work invisible to
// the retained-bytes budget, because that budget bounds what a report keeps and
// this is what producing it costs. The fix is not a faster cache; it is that the
// row path has nothing to compute. Rendering is a function of the pack's bytes,
// so it happens where a pack is loaded, and the row is handed a value.
func TestTheRowComparatorRendersNoHandoffTarget(t *testing.T) {
	engine := newTestEngine(t)
	pack, err := os.ReadFile(filepath.Join("testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	admitted := engine.AdmitPack(pack)
	// The rendering can only come from the one place that renders: the type's
	// members are unexported and it has no other constructor, so this test could
	// not fabricate one even to check that the row path ignores it.
	declared := engine.PackHandoffTarget(pack)
	rendersAfterOne := engine.HandoffTargetRenders()

	row := MatrixCase{
		Facts:                 json.RawMessage(`{"request":{"type":"unrelated"}}`),
		ExpectedDisposition:   json.RawMessage(`{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`),
		ExpectedHandoffTarget: json.RawMessage(`{"kind":"human-role","name":"Intake reviewer"}`),
	}
	// Capability sets vary per row on purpose: they select distinct admissions,
	// which is where two earlier drafts hid a per-row rendering. The rendering
	// handed in is the same value for all of them, because it is a property of
	// the pack and not of an admission.
	for index := range 200 {
		row.ID = fmt.Sprint(index)
		row.SupportedExtensions = nil
		if index%2 == 1 {
			row.SupportedExtensions = []string{fmt.Sprintf("https://example.com/spare-%d", index)}
		}
		outcome := engine.RunCaseAdmitted(admitted, row, declared, "test")
		if outcome.Status != "passed" {
			t.Fatalf("row %d: %+v", index, outcome)
		}
		if outcome.ActualHandoffTarget != `{"kind":"human-role","name":"Intake reviewer"}` {
			t.Fatalf("row %d must report the rendering it was handed: %+v", index, outcome)
		}
	}
	if renders := engine.HandoffTargetRenders(); renders != rendersAfterOne {
		t.Fatalf("the row path renders nothing: %d renderings across 200 rows", renders-rendersAfterOne)
	}

	// A row that asserts a target and is handed no rendering renders nothing on
	// its own: the report degrades to the unavailable convention and says so.
	// This is the state the narrowing created on purpose — it is what a direct
	// caller of this primitive gets instead of a per-row rendering behind the
	// counter's back — and no surface of this runtime reaches it.
	row.ID, row.SupportedExtensions = "unrendered", nil
	before := engine.HandoffTargetRenders()
	outcome := engine.RunCaseAdmitted(admitted, row, HandoffTargetRendering{}, "test")
	if outcome.ActualHandoffTarget != result.HandoffTargetUnavailable {
		t.Fatalf("no rendering means the report says so: %+v", outcome)
	}
	if engine.HandoffTargetRenders() != before {
		t.Fatal("the row path renders nothing, including when it was handed nothing")
	}
	// The verdict is unaffected: it rests on the decoded values, which are equal
	// here, so the row passes while its report degrades.
	if outcome.Status != "passed" {
		t.Fatalf("the verdict does not depend on the rendering: %+v", outcome)
	}

	// A row asserting nothing reports nothing and is handed nothing.
	outcome = engine.RunCaseAdmitted(admitted, MatrixCase{
		ID:                  "silent",
		Facts:               row.Facts,
		ExpectedDisposition: row.ExpectedDisposition,
	}, HandoffTargetRendering{}, "test")
	if outcome.Status != "passed" || outcome.ActualHandoffTarget != "" {
		t.Fatalf("outcome = %+v", outcome)
	}
}

// PackHandoffTarget reads one pack's declared escalation target and renders it,
// and answers "none" for a pack that declares none (ADR-0025).
func TestPackHandoffTargetRendersWhatThePackDeclares(t *testing.T) {
	engine := newTestEngine(t)
	pack, err := os.ReadFile(filepath.Join("testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	before := engine.HandoffTargetRenders()
	declared := engine.PackHandoffTarget(pack)
	// Present is the only member a caller can read, on purpose: the rendering
	// itself is for the row result to report, not for a caller to inspect or to
	// rebuild one from. What it renders is asserted through a row, below.
	if !declared.Present() {
		t.Fatalf("this pack declares a target: %+v", declared)
	}
	if engine.HandoffTargetRenders() != before+1 {
		t.Fatal("one call, one rendering")
	}
	reported := engine.RunCaseAdmitted(engine.AdmitPack(pack), MatrixCase{
		ID:                    "reports-the-rendering",
		Facts:                 json.RawMessage(`{"request":{"type":"unrelated"}}`),
		ExpectedDisposition:   json.RawMessage(`{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`),
		ExpectedHandoffTarget: json.RawMessage(`{"kind":"human-role","name":"Intake reviewer"}`),
	}, declared, "test")
	if reported.Status != "passed" || reported.ActualHandoffTarget != `{"kind":"human-role","name":"Intake reviewer"}` {
		t.Fatalf("reported = %+v", reported)
	}

	// A pack with no escalation object declares no target, and nothing is
	// rendered for it: an escalate exception with no escalation object is a
	// requested handoff with no Core-defined destination (§8.1).
	escalate, err := os.ReadFile(filepath.Join("..", "artifacts", "jps", "0.2.0-draft", "cases", "valid", "exception-escalate.json"))
	if err != nil {
		t.Fatal(err)
	}
	before = engine.HandoffTargetRenders()
	if declared = engine.PackHandoffTarget(escalate); declared.Present() {
		t.Fatalf("a pack with no escalation object declares no target: %+v", declared)
	}
	if engine.HandoffTargetRenders() != before {
		t.Fatal("a pack with no target renders nothing")
	}
	// And a row asserting null against it passes on the value it was handed,
	// with the comparator rendering nothing either.
	outcome := engine.RunCaseAdmitted(engine.AdmitPack(escalate), MatrixCase{
		ID:                    "no-destination",
		Facts:                 json.RawMessage(`{}`),
		ExpectedDisposition:   json.RawMessage(`{"kind":"unresolved","reasons":["exception-escalation"],"handoff":{"state":"requested","triggeredBy":["exception-escalation"]}}`),
		ExpectedHandoffTarget: json.RawMessage(`null`),
	}, declared, "test")
	if outcome.Status != "passed" || outcome.ActualHandoffTarget != result.NoHandoffTarget {
		t.Fatalf("outcome = %+v", outcome)
	}
}

// RunCase refuses an oversized pack before it reads a byte of it (ADR-0025).
//
// The hard byte limit is §8.4's own preflight refusal, and before this record
// touched RunCase the function delegated straight to the admission path where
// that limit ran first. Rendering the pack's target inside RunCase put a decode
// in front of it — carrier.Decode has no raw-byte cap of its own — so a
// syntactically valid pack padded past the limit was scanned whole, and its
// megabyte-long target canonicalized and hashed, before the refusal that was
// always going to happen. Trailing whitespace alone made that work unbounded.
//
// The limit therefore runs first, through the same helper the evaluation path
// uses so the two cannot drift, and nothing is rendered for a pack that will be
// refused.
func TestRunCaseRefusesAnOversizedPackBeforeRenderingAnything(t *testing.T) {
	engine := newTestEngine(t)
	pack, err := os.ReadFile(filepath.Join("testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Valid JSON, a megabyte-long target, and padding past the hard limit: every
	// byte of it would be scanned by a decode placed ahead of the limit.
	oversized := strings.Replace(string(pack), `"name": "Intake reviewer"`, `"name": "`+strings.Repeat("q", 1<<20)+`"`, 1)
	oversized += strings.Repeat(" ", int(carrier.HardMaxBytes)+1-len(oversized))
	if int64(len(oversized)) <= carrier.HardMaxBytes {
		t.Fatalf("this test needs a pack past the limit: %d bytes", len(oversized))
	}

	row := MatrixCase{
		ID:                    "oversized",
		Facts:                 json.RawMessage(`{"request":{"type":"unrelated"}}`),
		ExpectedDisposition:   json.RawMessage(`{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`),
		ExpectedHandoffTarget: json.RawMessage(`{"kind":"human-role","name":"Intake reviewer"}`),
	}
	before := engine.HandoffTargetRenders()
	outcome := engine.RunCase([]byte(oversized), row, "test")
	if engine.HandoffTargetRenders() != before {
		t.Fatalf("a pack that will be refused renders nothing: %d", engine.HandoffTargetRenders()-before)
	}
	// The refusal is the one the pre-existing contract names: the §8.4 class and
	// phase the evaluation path would have produced.
	if outcome.Status != "mismatch" || outcome.ActualErrorClass != result.ClassPackNotConformant {
		t.Fatalf("outcome = %+v", outcome)
	}
	if outcome.ActualErrorPhase != result.PhasePreflight {
		t.Fatalf("the limit is a preflight refusal: %+v", outcome)
	}
	if !strings.Contains(outcome.Detail, "JPS-RESOURCE-INPUT-BYTE-LIMIT") {
		t.Fatalf("the detail names the limit: %q", outcome.Detail)
	}
	// Every row preprocessing step still ran: the expected disposition is
	// canonicalized and the expected target decoded and reported, exactly as
	// they are for any other refusal. The limit declines to *decode* the pack;
	// it does not shortcut the row.
	if outcome.Expected == "" {
		t.Fatalf("the expected disposition is canonicalized whatever the pack does: %+v", outcome)
	}
	if outcome.ExpectedHandoffTarget != `{"kind":"human-role","name":"Intake reviewer"}` {
		t.Fatalf("the row's own assertion is reported back: %+v", outcome)
	}
	// The pair still appears together, and the actual side says nothing could be
	// stated rather than naming a target nobody produced.
	if outcome.ActualHandoffTarget != result.HandoffTargetUnavailable {
		t.Fatalf("outcome = %+v", outcome)
	}

	// A malformed expectation is still caught behind an oversized pack: the row
	// preprocessing that finds carrier defects runs before the evaluation is
	// attempted, so an expected pack-limit error cannot carry a bad expectation
	// to a pass.
	malformed := MatrixCase{
		ID:                  "malformed-beside-an-oversized-pack",
		Facts:               json.RawMessage(`{}`),
		ExpectedDisposition: json.RawMessage(`{"kind":"outcome","reasons":["unknown"],"handoff":{"state":"none"}}`),
	}
	if outcome = engine.RunCase([]byte(oversized), malformed, "test"); outcome.Status != "mismatch" {
		t.Fatalf("a malformed expectation is a carrier defect whatever the pack weighs: %+v", outcome)
	}
	if !strings.Contains(outcome.Detail, "not a canonicalizable §8.3 disposition") {
		t.Fatalf("the detail names the expectation's defect: %q", outcome.Detail)
	}
	malformed.ExpectedDisposition = nil
	malformed.ExpectedErrorClass, malformed.ExpectedErrorPhase = result.ClassPackNotConformant, result.PhasePreflight
	malformed.ExpectedHandoffTarget = json.RawMessage(`{"kind":"human-role"}`)
	if outcome = engine.RunCase([]byte(oversized), malformed, "test"); outcome.Status != "mismatch" {
		t.Fatalf("a malformed target assertion is caught too: %+v", outcome)
	}

	// A row expecting that refusal passes, which is what makes this the same
	// contract the evaluation path has always had rather than a new one.
	row.ExpectedDisposition, row.ExpectedHandoffTarget = nil, nil
	row.ExpectedErrorClass, row.ExpectedErrorPhase = result.ClassPackNotConformant, result.PhasePreflight
	if outcome = engine.RunCase([]byte(oversized), row, "test"); outcome.Status != "passed" {
		t.Fatalf("outcome = %+v", outcome)
	}
}

// A rendering minted for one pack cannot be reported against another
// (ADR-0025).
//
// Unexported members stop a caller fabricating a rendering; they do not stop one
// transplanting a genuine rendering, because PackHandoffTarget renders whatever
// root it is handed. A transplanted rendering would be reported verbatim while
// the verdict compared the evaluated pack's real target — a row passing while
// its actualHandoffTarget named a destination the pack never declared, which is
// this record's own defect inverted. The rendering therefore carries the target
// it was minted from, and a row uses it only when that is the target its
// evaluation produced.
func TestATransplantedRenderingIsNotReported(t *testing.T) {
	engine := newTestEngine(t)
	pack, err := os.ReadFile(filepath.Join("testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Pack B: the same policy, a different destination. Its rows must never
	// report pack A's.
	other := strings.Replace(string(pack), `"name": "Intake reviewer"`, `"name": "Disclosure office"`, 1)
	if other == string(pack) {
		t.Fatal("the fixture must declare the target this test changes")
	}

	fromA, fromB := engine.PackHandoffTarget(pack), engine.PackHandoffTarget([]byte(other))
	if !fromA.Present() || !fromB.Present() {
		t.Fatal("both packs declare a target")
	}

	// Pack B's own rows, asserting pack B's target: the row passes and reports
	// pack B's destination.
	row := MatrixCase{
		ID:                    "b",
		Facts:                 json.RawMessage(`{"request":{"type":"unrelated"}}`),
		ExpectedDisposition:   json.RawMessage(`{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`),
		ExpectedHandoffTarget: json.RawMessage(`{"kind":"human-role","name":"Disclosure office"}`),
	}
	admittedB := engine.AdmitPack([]byte(other))
	outcome := engine.RunCaseAdmitted(admittedB, row, fromB, "test")
	if outcome.Status != "passed" || outcome.ActualHandoffTarget != `{"kind":"human-role","name":"Disclosure office"}` {
		t.Fatalf("a matching rendering reports normally: %+v", outcome)
	}

	// The same row and the same pack, handed pack A's rendering. The verdict is
	// unchanged — it never read the rendering — and the report refuses to state
	// a target rather than stating pack A's.
	transplanted := engine.RunCaseAdmitted(admittedB, row, fromA, "test")
	if transplanted.Status != outcome.Status {
		t.Fatalf("the verdict does not depend on the rendering: %+v", transplanted)
	}
	if transplanted.ActualHandoffTarget != result.HandoffTargetUnavailable {
		t.Fatalf("a transplanted rendering must not be reported: %+v", transplanted)
	}
	if strings.Contains(transplanted.ActualHandoffTarget, "Intake reviewer") {
		t.Fatalf("pack A's destination must not appear in pack B's row: %+v", transplanted)
	}

	// An errored foreign handle degrades too, and this is the case ordering
	// decides: propagating its error would flip this row's verdict to mismatch,
	// which is a stronger effect than the false report the binding exists to
	// prevent. The handle is built here because PackHandoffTarget can no longer
	// produce one — it takes pack bytes, and the carrier refuses the invalid
	// UTF-8 that is jcs.Encode's only failure — so what is pinned is the
	// ordering, which outlives today's carrier rules.
	errored := HandoffTargetRendering{err: errors.New("would not render"), digest: sha256.Sum256([]byte("some other pack"))}
	degraded := engine.RunCaseAdmitted(admittedB, row, errored, "test")
	if degraded.Status != outcome.Status {
		t.Fatalf("a foreign handle must not change a verdict: %+v", degraded)
	}
	if degraded.ActualHandoffTarget != result.HandoffTargetUnavailable {
		t.Fatalf("a foreign errored handle degrades the report: %+v", degraded)
	}
	if strings.Contains(degraded.Detail, "would not render") {
		t.Fatalf("another pack's rendering failure is not this row's: %+v", degraded)
	}

	// And the mismatching direction is unaffected: a row asserting pack A's
	// target against pack B still fails on the decoded comparison, whichever
	// rendering it was handed.
	row.ExpectedHandoffTarget = json.RawMessage(`{"kind":"human-role","name":"Intake reviewer"}`)
	for name, supplied := range map[string]HandoffTargetRendering{"pack B's": fromB, "pack A's": fromA} {
		failing := engine.RunCaseAdmitted(admittedB, row, supplied, "test")
		if failing.Status != "mismatch" {
			t.Fatalf("%s rendering: the verdict rests on decoded values: %+v", name, failing)
		}
	}
}

// The binding is decided on the pack digest, before anything else the handle
// carries (ADR-0025).
//
// This is a unit test of the ordering rather than of a reachable state, and the
// distinction is worth stating. An errored rendering is not constructible
// through PackHandoffTarget any more: it takes pack bytes, carrier.Decode
// refuses invalid UTF-8 and unpaired surrogates, and jcs.Encode's only failure
// is invalid UTF-8 — so the error door is closed at the entrance. It is kept,
// and tested here, because "unreachable" is a property of today's carrier rules
// and the ordering it protects is not: a foreign handle carrying an error would
// otherwise propagate that error onto a row it has nothing to do with, flipping
// a verdict to mismatch, which is a stronger effect than the false report the
// binding exists to prevent.
func TestTheHandoffTargetBindingIsDecidedOnTheDigestFirst(t *testing.T) {
	produced := &result.HandoffTarget{Kind: "human-role", Name: "Intake reviewer"}
	mine := sha256.Sum256([]byte("this pack"))
	foreign := sha256.Sum256([]byte("some other pack"))
	broken := errors.New("this target would not render")

	for name, probe := range map[string]struct {
		handle   HandoffTargetRendering
		reported string
		fails    bool
	}{
		"a rendering of this pack": {
			handle:   HandoffTargetRendering{rendered: `{"kind":"human-role","name":"Intake reviewer"}`, present: true, digest: mine},
			reported: `{"kind":"human-role","name":"Intake reviewer"}`,
		},
		"a rendering of another pack": {
			handle:   HandoffTargetRendering{rendered: `{"kind":"queue","name":"Somewhere else"}`, present: true, digest: foreign},
			reported: result.HandoffTargetUnavailable,
		},
		"no rendering at all": {
			handle:   HandoffTargetRendering{},
			reported: result.HandoffTargetUnavailable,
		},
		"this pack's rendering, which failed": {
			handle: HandoffTargetRendering{err: broken, digest: mine},
			fails:  true,
		},
		"another pack's rendering, which failed": {
			// The ordering that matters: a foreign handle is refused as foreign
			// before its error is looked at, so it degrades the report instead of
			// changing this row's verdict.
			handle:   HandoffTargetRendering{err: broken, digest: foreign},
			reported: result.HandoffTargetUnavailable,
		},
		"this pack, which declares no target": {
			handle:   HandoffTargetRendering{digest: mine},
			reported: result.HandoffTargetUnavailable,
		},
	} {
		t.Run(name, func(t *testing.T) {
			reported, err := reportedHandoffTarget(produced, probe.handle, mine)
			if probe.fails {
				if err == nil {
					t.Fatal("this pack's own rendering failed, and the row says so")
				}
				return
			}
			if err != nil {
				t.Fatalf("a handle that is not this pack's must not fail the row: %v", err)
			}
			if reported != probe.reported {
				t.Fatalf("reported = %q, want %q", reported, probe.reported)
			}
		})
	}

	// An evaluation that produced no target answers before the digest is
	// consulted at all: "no target" is one constant whatever pack produced it.
	if reported, err := reportedHandoffTarget(nil, HandoffTargetRendering{digest: foreign}, mine); err != nil || reported != result.NoHandoffTarget {
		t.Fatalf("reported = %q %v", reported, err)
	}
}

// Two mints over the same pack bytes are interchangeable, because the binding
// is over the bytes and the same bytes declare the same target (ADR-0025).
//
// This is the honest consequence of binding by digest rather than by mint
// identity, and it is the behaviour worth having: a rendering another engine
// made over this very pack reports the truth, so nothing degrades for a caller
// that shares work. What it costs is observation scope — the local counter
// counts local mints — which is a fact about the number, not a hole in the
// bound it observes.
func TestARenderingOfTheSameBytesIsReportedWhicheverEngineMintedIt(t *testing.T) {
	pack, err := os.ReadFile(filepath.Join("testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	minting, reporting := newTestEngine(t), newTestEngine(t)
	elsewhere := minting.PackHandoffTarget(pack)
	if !elsewhere.Present() {
		t.Fatal("this pack declares a target")
	}
	outcome := reporting.RunCaseAdmitted(reporting.AdmitPack(pack), MatrixCase{
		ID:                    "shared",
		Facts:                 json.RawMessage(`{"request":{"type":"unrelated"}}`),
		ExpectedDisposition:   json.RawMessage(`{"kind":"not-applicable","reasons":["not-applicable"],"handoff":{"state":"requested","triggeredBy":["not-applicable"]}}`),
		ExpectedHandoffTarget: json.RawMessage(`{"kind":"human-role","name":"Intake reviewer"}`),
	}, elsewhere, "test")
	if outcome.Status != "passed" || outcome.ActualHandoffTarget != `{"kind":"human-role","name":"Intake reviewer"}` {
		t.Fatalf("same bytes, same target, reported honestly: %+v", outcome)
	}
	// The reporting engine minted nothing, and says so.
	if reporting.HandoffTargetRenders() != 0 {
		t.Fatalf("the counter counts local mints: %d", reporting.HandoffTargetRenders())
	}
	if minting.HandoffTargetRenders() != 1 {
		t.Fatalf("the mint happened once, elsewhere: %d", minting.HandoffTargetRenders())
	}
}
