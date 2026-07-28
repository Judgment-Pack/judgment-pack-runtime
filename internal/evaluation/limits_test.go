package evaluation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The two §10 limits of the evaluator class, on the ordinary Core path and not
// only under the draft RFC 0008 opt-in: reachable, typed as §8.4 requires, and
// nowhere near the inputs anyone actually evaluates.

// corePack builds one semantically conforming Core pack — no draft grammar —
// whose single rule carries the given condition, so a row states only the thing
// it is about. requirements is the number of declared, non-required evidence
// requirements, which is how a row prices §8's own iteration without pricing a
// condition.
func corePack(when string, requirements int) []byte {
	declared := make([]string, 0, requirements)
	for index := 0; index < requirements; index++ {
		declared = append(declared, fmt.Sprintf(
			`{"id":"requirement-%d","description":"Declared requirement %d.","required":false}`, index, index))
	}
	evidence := ""
	if requirements > 0 {
		evidence = fmt.Sprintf(`"evidenceRequirements": [%s],`, strings.Join(declared, ","))
	}
	return []byte(fmt.Sprintf(`{
  "specVersion": "0.2.0-draft",
  "id": "https://example.invalid/judgment-packs/core-work-limit",
  "version": "0.1.0",
  "title": "Synthetic Core work-limit row",
  "description": "Invented content for specification testing; it authorizes nothing.",
  "decision": {
    "intent": "Exercise this runtime's Core-path evaluation-work limit.",
    "question": "Was the evaluation affordable?"
  },
  "outcomes": [
    {"id": "held", "label": "Held"},
    {"id": "did-not-hold", "label": "Did not hold"}
  ],
  %s
  "rules": [
    {
      "id": "the-rule",
      "description": "The one rule under test.",
      "when": %s,
      "outcome": "held",
      "onUnknown": "ignore"
    }
  ],
  "fallbackOutcome": "did-not-hold"
}`, evidence, when))
}

// membersFacts is one facts document carrying a collection of the given size,
// written as JSON nulls so a member costs exactly one work unit and the
// arithmetic in these rows can be done by hand.
func membersFacts(members int) []byte {
	items := make([]string, 0, members)
	for index := 0; index < members; index++ {
		items = append(items, "null")
	}
	return []byte(`{"items":[` + strings.Join(items, ",") + `]}`)
}

// inCondition compares the whole selected collection against candidates
// candidates. Membership is the cheapest amplification Core has: each candidate
// can be compared against the whole of the selected value, so one small
// condition charges the value once per candidate.
func inCondition(candidates int) string {
	values := make([]string, 0, candidates)
	for index := 0; index < candidates; index++ {
		values = append(values, fmt.Sprint(index))
	}
	return `{"op":"fact","path":"/items","operator":"in","value":[` + strings.Join(values, ",") + `]}`
}

// The documented default is reachable on the Core path, with §8.4's class and
// phase and no disposition at all. Both rows below are inside every admission
// limit — a 200,000-member collection is 200,002 parsed nodes against the
// carrier's 250,000, and about 1 MB against 10 MiB — so this is the evaluation
// phase and not the preflight, which is exactly the distinction §10 draws.
//
// The two candidate counts are derived from DefaultCoreWorkLimit rather than
// written down, because that limit is itself derived from the carrier's byte cap:
// a row that hard-coded either number would pass or fail on the cap's value. The
// collection measures 200,001 units and an in condition re-reads it once per
// candidate, so one count either side of limit/200,001 brackets the default — which
// is what pins the default rather than a configured budget.
func TestCoreWorkLimitIsReachableAtTheDocumentedDefault(t *testing.T) {
	engine := newTestEngine(t)
	facts := membersFacts(200_000)
	const collectionUnits = 200_001
	inside := DefaultCoreWorkLimit/collectionUnits - 1
	outside := DefaultCoreWorkLimit/collectionUnits + 1

	output, failure := engine.Evaluate(corePack(inCondition(inside), 0), facts, nil, nil, "test")
	if failure != nil {
		t.Fatalf("%d candidates (about %d units) is inside the default limit of %d: %+v", inside, inside*collectionUnits, DefaultCoreWorkLimit, failure)
	}
	if output.Disposition.Kind != "outcome" || output.Disposition.OutcomeID != "did-not-hold" {
		t.Fatalf("disposition = %+v; a collection is in no scalar candidate", output.Disposition)
	}

	output, failure = engine.Evaluate(corePack(inCondition(outside), 0), facts, nil, nil, "test")
	if failure == nil {
		t.Fatalf("%d candidates (about %d units) must exhaust the default limit of %d, got %+v", outside, outside*collectionUnits, DefaultCoreWorkLimit, output.Disposition)
	}
	if failure.Class != result.ClassResourceExhaustion || failure.Phase != result.PhaseEvaluation {
		t.Fatalf("class/phase = %q/%q, want %q/%q", failure.Class, failure.Phase, result.ClassResourceExhaustion, result.PhaseEvaluation)
	}
	if failure.Code != "JPS-RESOURCE-EVALUATION-WORK-LIMIT" || failure.ExitCode != result.ExitIO {
		t.Fatalf("code/exit = %q/%d", failure.Code, failure.ExitCode)
	}
	if !strings.Contains(failure.Message, fmt.Sprint(DefaultCoreWorkLimit)) {
		t.Fatalf("the error names the limit in force: %q", failure.Message)
	}
	if output.Disposition.Kind != "" || len(output.Disposition.Reasons) != 0 || output.Status != "" {
		t.Fatalf("an evaluation error reports no partial disposition: %+v", output)
	}
}

// The limit is configurable per evaluation, exactly as the draft grammar's
// budget is, and a caller that lowers it has lowered its own limit: the same
// inputs are refused with the same class and the error names the configured
// number rather than the default.
func TestCoreWorkLimitIsConfigurable(t *testing.T) {
	engine := newTestEngine(t)
	pack := corePack(inCondition(2), 0)
	facts := membersFacts(1_000)

	if _, failure := engine.Evaluate(pack, facts, nil, nil, "test"); failure != nil {
		t.Fatalf("two thousand units is inside the default limit: %+v", failure)
	}
	_, failure := engine.EvaluateWith(pack, facts, nil, Options{Command: "test", WorkBudget: 500})
	if failure == nil || failure.Class != result.ClassResourceExhaustion || failure.Phase != result.PhaseEvaluation {
		t.Fatalf("a configured limit of 500 units must be reached: %+v", failure)
	}
	if !strings.Contains(failure.Message, "500") {
		t.Fatalf("the error names the configured limit: %q", failure.Message)
	}
}

// §8's own iteration is charged, so a pack that evaluates one cheap condition
// and declares many evidence requirements is not free work. Step 2 inspects
// every declared requirement without evaluating a condition, which is precisely
// the work a charge over condition trees alone would miss.
//
// The pack below charges 1 for the rule's iteration, 40 for the forty
// requirements, and 1 for the literal: 42 exactly.
func TestCoreWorkLimitChargesSection8Iteration(t *testing.T) {
	engine := newTestEngine(t)
	pack := corePack(`{"op":"literal","value":true}`, 40)

	if _, failure := engine.EvaluateWith(pack, []byte(`{}`), nil, Options{Command: "test", WorkBudget: 42}); failure != nil {
		t.Fatalf("a limit equal to the charge must not be reached: %+v", failure)
	}
	_, failure := engine.EvaluateWith(pack, []byte(`{}`), nil, Options{Command: "test", WorkBudget: 41})
	if failure == nil || failure.Code != "JPS-RESOURCE-EVALUATION-WORK-LIMIT" {
		t.Fatalf("a limit one unit short must be reached: %+v", failure)
	}
	// The same forty requirements with the limit at one unit are refused before
	// step 1, so no trace and no partial state escapes with the error.
	output, failure := engine.EvaluateWith(pack, []byte(`{}`), nil, Options{Command: "test", WorkBudget: 1})
	if failure == nil || len(output.Trace) != 0 {
		t.Fatalf("the §8 charge lands before step 1: %+v / %+v", failure, output.Trace)
	}
}

// The collection-size limit of §10 on this path is the carrier's parsed-node
// cap, and this row is the determination rather than a mechanism: a collection
// above it is refused while the input is being *admitted*, so §10's phase split
// makes it malformed-input in the preflight phase and resource-exhaustion is
// never the class. A collection at that size cannot reach evaluation at all,
// which is why no second evaluation-phase counter states the same bound.
func TestCoreCollectionSizeLimitIsEnforcedAtAdmission(t *testing.T) {
	engine := newTestEngine(t)
	if CoreCollectionSizeLimit != carrier.DefaultMaxNodes {
		t.Fatalf("the documented collection-size limit is the carrier's node cap: %d vs %d", CoreCollectionSizeLimit, carrier.DefaultMaxNodes)
	}
	pack := corePack(inCondition(1), 0)

	_, failure := engine.Evaluate(pack, membersFacts(CoreCollectionSizeLimit), nil, nil, "test")
	if failure == nil {
		t.Fatal("a collection at the node cap is not admitted")
	}
	if failure.Class != result.ClassMalformedInput || failure.Phase != result.PhasePreflight {
		t.Fatalf("class/phase = %q/%q, want %q/%q", failure.Class, failure.Phase, result.ClassMalformedInput, result.PhasePreflight)
	}
	if failure.Code != "JPS-RESOURCE-NODE-LIMIT" {
		t.Fatalf("code = %q, want the carrier's node-limit code", failure.Code)
	}

	// Well inside the cap the same shape is admitted and evaluated, so the row
	// above turns on the cap and not on the shape.
	if _, failure := engine.Evaluate(pack, membersFacts(1_000), nil, nil, "test"); failure != nil {
		t.Fatalf("a thousand-member collection is ordinary input: %+v", failure)
	}
}

// The work limit is derived from the carrier's byte cap and not chosen, and the
// derivation is exactly the one documented: twice the largest admissible input, so
// one full read of every admitted byte fits and a single maximal cross-document
// comparison does not. A row that only checked the number would let the constant
// drift from the cap it is derived from.
func TestCoreWorkLimitIsDerivedFromTheCarrierByteCap(t *testing.T) {
	if want := int(2 * carrier.HardMaxBytes); DefaultCoreWorkLimit != want {
		t.Fatalf("DefaultCoreWorkLimit = %d, want exactly twice the carrier byte cap (%d)", DefaultCoreWorkLimit, want)
	}
	// One full read of the pack plus one of the facts document: each is admitted
	// under the byte cap and each unit is backed by at least one byte, so the sum of
	// the two admissible lengths is at most the limit and never above it.
	if int64(DefaultCoreWorkLimit) < carrier.HardMaxBytes+carrier.HardMaxBytes {
		t.Fatalf("one full read of every admitted byte must fit: %d < %d", DefaultCoreWorkLimit, carrier.HardMaxBytes*2)
	}
	// And no more than that is claimed: a comparison charging both a near-cap
	// authored value and a near-cap selected value reaches the same total with §8's
	// fixed charges still to pay, so it sits at the boundary rather than inside it.
	if int64(DefaultCoreWorkLimit) > carrier.HardMaxBytes+carrier.HardMaxBytes {
		t.Fatalf("the limit is exactly twice the cap, so the boundary case is at the boundary: %d > %d", DefaultCoreWorkLimit, carrier.HardMaxBytes*2)
	}
}

// The default limit never trips on the corpus, with room to spare that is stated
// as a number rather than asserted as a feeling: every row of the bundled
// evaluation corpus evaluates identically under a limit of 1,000 units, which is
// four orders of magnitude below DefaultCoreWorkLimit. A row that needed more
// than that would be a row this runtime's default could plausibly refuse.
func TestCoreWorkLimitNeverTripsOnTheCorpus(t *testing.T) {
	engine := newTestEngine(t)
	set, err := artifacts.Load(artifacts.EvaluatorDraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	manifest, failure := loadCorpusManifest(set, artifacts.EvaluatorDraftVersion)
	if failure != nil {
		t.Fatalf("%s: %s", failure.Code, failure.Message)
	}
	if DefaultCoreWorkLimit < 1_000*10_000 {
		t.Fatalf("DefaultCoreWorkLimit = %d leaves the corpus less than four orders of magnitude of headroom", DefaultCoreWorkLimit)
	}
	for _, item := range manifest.Cases {
		t.Run(item.ID, func(t *testing.T) {
			pack, err := set.EvaluationPack(item.Pack)
			if err != nil {
				t.Fatal(err)
			}
			options := Options{Command: "test", SupportedExtensions: item.SupportedExtensions, WorkBudget: 1_000}
			frugal, frugalFailure := engine.EvaluateWith(pack, item.Facts, item.EvidenceAvailability, options)
			generous, generousFailure := engine.Evaluate(pack, item.Facts, item.EvidenceAvailability, item.SupportedExtensions, "test")
			if frugalFailure != nil {
				t.Fatalf("a corpus row must not need a thousand work units: %s: %s", frugalFailure.Code, frugalFailure.Message)
			}
			if generousFailure != nil {
				t.Fatalf("a corpus row must evaluate under the default limit: %s: %s", generousFailure.Code, generousFailure.Message)
			}
			frugalBytes, err := frugal.Disposition.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			generousBytes, err := generous.Disposition.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			if string(frugalBytes) != string(generousBytes) {
				t.Fatalf("the limit in force must not change a disposition it is not reached by:\n  %s\n  %s", frugalBytes, generousBytes)
			}
		})
	}
}
