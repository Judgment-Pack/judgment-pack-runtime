package evaluation

import (
	"slices"
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
