package evaluation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// The evaluation corpus of JPS §3.4.1, run against this runtime's evaluator.
// Running it produces results: §3.4.1 makes those results the required evidence
// for the one permitted claim and not exhaustive evidence of it. The claim is
// stated, in full and only, in CONFORMANCE.md (ADR-0011); every payload here
// carries a reference to that file and restates nothing of it
// (result.EvaluationClaimReference, result.EvaluationCorpusLabel). A passing run is
// that evidence; it is not the claim.

// corpusManifest is the case carrier of the bundled evaluation corpus. facts,
// evidenceAvailability, and expectedDisposition are kept as raw bytes: the
// evaluator takes its inputs as documents, and re-encoding them here would put a
// second serializer between the corpus and the evaluation.
type corpusManifest struct {
	SuiteVersion string       `json:"suiteVersion"`
	SpecVersion  string       `json:"specVersion"`
	Status       string       `json:"status"`
	Label        string       `json:"label"`
	Cases        []corpusCase `json:"cases"`
}

// MatrixCase is one case-carrier row: one pack, one facts document, the
// tri-state availability of its declared evidence, the consumer's supported
// extensions, and exactly one expectation — a §8.3 disposition to compare byte
// for byte, or the §8.4 error class the evaluation must be refused with.
//
// It is exported because the bundled evaluation corpus is not the only carrier
// of this shape: a project's own instance matrix (ADR-0012) uses the same rows
// and is compared by the same code, so a matrix a builder writes and the corpus
// this runtime ships are read, run, and judged identically rather than by two
// implementations of one comparison.
//
// facts, evidenceAvailability, and expectedDisposition are kept as raw bytes:
// the evaluator takes its inputs as documents, and re-encoding them here would
// put a second serializer between the carrier and the evaluation.
//
// ExpectedHandoffTarget is the one optional second expectation (ADR-0025), and
// it is raw bytes for a reason the other members do not have: the assertion has
// three states and only raw bytes distinguish them. Absent asserts nothing; the
// literal null asserts that the evaluation reports no target; an object asserts
// the target it names. It carries `omitempty` for the same reason — without it,
// marshaling a row that asserted nothing writes `expectedHandoffTarget: null`,
// which reloads as the assertion that the evaluation reports no target, so a
// round trip through this type would invent an expectation nobody wrote.
//
// The member is a **project-only extension** of what the two carriers share,
// which is the fields this comparator reads — not a claim that a row moves
// between them untouched. The bundled corpus never carries it, because that
// manifest's schema closes its case object; the same schema separately requires
// pack, origin, supportedExtensions, focus, and specSection, which a project row
// need not declare. So lifting a project row into a corpus means supplying those
// and dropping any target assertion. The member is reachable only from a project
// matrix, which is the carrier whose rows are written about a pack the project
// itself maintains, and only from matrixVersion 2 (project.MatrixVersion).
type MatrixCase struct {
	ID                    string          `json:"id"`
	Origin                string          `json:"origin"`
	Facts                 json.RawMessage `json:"facts"`
	EvidenceAvailability  json.RawMessage `json:"evidenceAvailability"`
	SupportedExtensions   []string        `json:"supportedExtensions"`
	ExpectedDisposition   json.RawMessage `json:"expectedDisposition"`
	ExpectedHandoffTarget json.RawMessage `json:"expectedHandoffTarget,omitempty"`
	ExpectedErrorClass    string          `json:"expectedErrorClass"`
	ExpectedErrorPhase    string          `json:"expectedErrorPhase"`
	Focus                 string          `json:"focus"`
	SpecSection           string          `json:"specSection"`
}

// corpusCase is one bundled corpus row: a MatrixCase plus the bundled pack
// fixture it runs against. A project matrix names its pack in the project
// configuration instead. That is the difference this type carries, and not the
// only difference between the two carriers: the bundled manifest's schema also
// requires origin, supportedExtensions, focus, and specSection, which a project
// row need not declare, and refuses expectedHandoffTarget, which only a project
// row may.
type corpusCase struct {
	MatrixCase
	Pack string `json:"pack"`
}

// HandoffTargetRendering is one pack's declared escalation target, rendered for
// the report, computed **once per pack per run** by whatever owns the row loop
// and handed to every row of that pack (ADR-0025).
//
// It is a value and not a cache, and that is the whole design. Three earlier
// drafts tried to make the rendering cheap where it was needed — per row, then
// memoized on the target's content, then stored on a capability admission — and
// each was a bound on the wrong thing, because each left the *decision about
// when to compute* on the row path. A rendering is a function of the pack's
// bytes alone: §8.1 gives a pack one escalation target, every admission of one
// pack decodes the identical declared target, and a capability set has nothing
// to do with it. So it is computed where a pack is loaded, which is a place
// visited once, and carried down. The row path then has no cache to miss, no
// admission to re-enter, no mutex, and no atomic — not as a claim about a fast
// path, but because there is no state there to reach.
//
// **Its members are unexported and it has no constructor but PackHandoffTarget**,
// which is the one place a target is rendered and the one place the count is
// taken. That is what makes "the row path renders nothing" and "PackHandoffTarget
// is the sole rendering site" true by construction rather than by convention: a
// caller of RunCaseAdmitted cannot hand it a rendering it made itself, cannot
// hand it stale bytes for a target the pack does not declare, and cannot reach a
// path that would render one per row behind the counter's back.
//
// The zero value is legal and means exactly "no rendering was computed for this
// pack". A row that asserts a target and is given it reports the
// result.HandoffTargetUnavailable convention on the actual side rather than
// having one rendered for it — the report degrades and says so, and the verdict
// is unaffected, because the verdict rests on the decoded values (see
// sameHandoffTarget). No in-tree caller reaches that state: the project runner
// computes a rendering whenever a row asserts, RunCase computes its own, and no
// bundled corpus row can assert at all.
type HandoffTargetRendering struct {
	rendered string
	present  bool
	err      error
}

// PackHandoffTarget renders the escalation target a decoded pack declares. It is
// the only function in this runtime that renders one, and the only constructor
// of a non-zero HandoffTargetRendering.
//
// It is a pure function of the decoded pack, with one exception that is not
// state the answer depends on: the Engine counts the renderings it has produced,
// because "once per pack per run" is a claim this record makes and a claim
// nothing can observe is a claim nobody can hold. Since this is the sole
// rendering site, that count is complete rather than a sample. See
// HandoffTargetRenders.
func (e *Engine) PackHandoffTarget(packRoot map[string]any) HandoffTargetRendering {
	escalation, _ := packRoot["escalation"].(map[string]any)
	if escalation == nil {
		return HandoffTargetRendering{}
	}
	target, _ := escalation["target"].(map[string]any)
	if target == nil {
		return HandoffTargetRendering{}
	}
	kind, _ := target["kind"].(string)
	name, _ := target["name"].(string)
	if kind == "" || name == "" {
		return HandoffTargetRendering{}
	}
	e.targetRenders.Add(1)
	rendered, err := (&result.HandoffTarget{Kind: kind, Name: name}).Rendered()
	if err != nil {
		return HandoffTargetRendering{err: err}
	}
	return HandoffTargetRendering{rendered: rendered, present: true}
}

// Present reports whether a rendering was computed — that is, whether the pack
// declares an escalation target §8.1 admits and rendering it succeeded.
func (h HandoffTargetRendering) Present() bool { return h.present }

// HandoffTargetRenders reports how many escalation targets this engine has
// rendered. It exists so the bound above is observable rather than asserted:
// a target name is an authored string §2.1 admits at a megabyte, so the number
// of times one is canonicalized and hashed is the difference between a bounded
// run and a matrix that costs gigabytes while staying inside every byte limit.
//
// The observation is complete because PackHandoffTarget is the sole rendering
// site and the sole constructor: there is no second path that could produce a
// rendering this number does not count.
func (e *Engine) HandoffTargetRenders() int64 { return e.targetRenders.Load() }

// reportedHandoffTarget reports the escalation target one evaluation produced,
// out of the rendering its pack was given. It renders nothing, and it cannot:
// it has no constructor for a rendering and no target-shaped input beyond the
// one it was handed.
//
// Where the caller supplied none, the actual side degrades to the "unavailable"
// convention rather than being computed here. That is the whole of the
// difference this narrowing makes: the report says it cannot state the target,
// instead of the row path quietly doing the work a run was supposed to do once.
func reportedHandoffTarget(produced *result.HandoffTarget, declared HandoffTargetRendering) (string, error) {
	if produced == nil {
		return result.NoHandoffTarget, nil
	}
	if declared.err != nil {
		return "", declared.err
	}
	if declared.present {
		return declared.rendered, nil
	}
	return result.HandoffTargetUnavailable, nil
}

// RunCorpus runs the evaluation corpus bundled for one exact specification
// version through this evaluator and reports every row's result.
//
// A row carrying an expected disposition passes when the disposition this
// evaluator produces canonicalizes, under RFC 8785, to the same bytes as the
// row's — the byte agreement §8.3 requires of two implementations, applied to
// one implementation and one pinned expectation. A row carrying an expected
// §8.4 error class instead passes when the evaluation is refused with that
// class, and with that phase when the row names one.
//
// A failing row decides nothing by itself: §3.4 makes a divergence as likely to
// be a defect in the row as in the implementation, and neither this runtime nor
// this function adjudicates that question.
func (e *Engine) RunCorpus(specVersion, command string) (result.EvaluationCorpus, *Failure) {
	if specVersion == "" {
		specVersion = artifacts.EvaluatorDraftVersion
	}
	set, err := artifacts.Load(specVersion)
	if err != nil {
		return result.EvaluationCorpus{}, &Failure{
			Code:     "JPS-CAPABILITY-SPEC-VERSION",
			Message:  "The exact JPS specification version is not bundled with this runtime.",
			ExitCode: result.ExitUnsupported,
		}
	}
	if !set.HasEvaluationCorpus() {
		return result.EvaluationCorpus{}, &Failure{
			Code:     "JPS-CAPABILITY-EVALUATION-CORPUS",
			Message:  fmt.Sprintf("JPS %s publishes no evaluation corpus; its Core defines no evaluator conformance class.", specVersion),
			ExitCode: result.ExitUnsupported,
		}
	}
	manifest, failure := loadCorpusManifest(set, specVersion)
	if failure != nil {
		return result.EvaluationCorpus{}, failure
	}

	output := result.EvaluationCorpus{
		OutputVersion:             result.OutputVersion,
		Tool:                      result.CurrentTool(),
		Command:                   command,
		Status:                    "passed",
		Experimental:              true,
		ConformanceClaimReference: result.EvaluationClaimReference,
		Label:                     result.EvaluationCorpusLabel,
		SpecVersion:               manifest.SpecVersion,
		SuiteVersion:              manifest.SuiteVersion,
		CorpusStatus:              manifest.Status,
		CorpusLabel:               manifest.Label,
		Provenance:                set.Lock().Source.Kind,
		Cases:                     []result.EvaluationCorpusCase{},
	}
	admissions := map[string]*AdmittedPack{}
	for _, item := range manifest.Cases {
		caseResult := e.runCorpusCase(set, admissions, item)
		output.Summary.Total++
		if caseResult.Status == "passed" {
			output.Summary.Passed++
		} else {
			output.Summary.Mismatched++
			output.Status = "mismatch"
		}
		output.Cases = append(output.Cases, caseResult)
	}
	return output, nil
}

// loadCorpusManifest reads the bundled corpus index and holds it to its own
// bundled schema before a single row runs, so a carrier defect is never mistaken
// for an evaluation result.
func loadCorpusManifest(set *artifacts.Set, specVersion string) (corpusManifest, *Failure) {
	manifestBytes, err := set.EvaluationManifest()
	if err != nil {
		return corpusManifest{}, internalCorpusFailure("Bundled evaluation-corpus metadata is unavailable.")
	}
	schemaBytes, err := set.EvaluationManifestSchema()
	if err != nil {
		return corpusManifest{}, internalCorpusFailure("Bundled evaluation-corpus schema is unavailable.")
	}
	document, carrierFailure := carrier.Decode(manifestBytes, carrier.DefaultLimits())
	if carrierFailure != nil {
		return corpusManifest{}, internalCorpusFailure("Bundled evaluation-corpus metadata is not valid strict JSON.")
	}
	compiled, err := validation.CompileSchema(schemaBytes, "urn:judgmentpack:jps:"+specVersion+":evaluation-manifest")
	if err != nil {
		return corpusManifest{}, internalCorpusFailure("Bundled evaluation-corpus schema could not be compiled.")
	}
	if err := compiled.Validate(document); err != nil {
		return corpusManifest{}, internalCorpusFailure("Bundled evaluation-corpus metadata does not satisfy its schema.")
	}
	var manifest corpusManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return corpusManifest{}, internalCorpusFailure("Bundled evaluation-corpus metadata could not be decoded.")
	}
	if manifest.SpecVersion != specVersion || len(manifest.Cases) == 0 {
		return corpusManifest{}, internalCorpusFailure("Bundled evaluation corpus does not target its own specification version.")
	}
	return manifest, nil
}

// runCorpusCase runs one row. The pack is a bundled, digest-locked fixture; the
// facts and evidence inputs are the row's own bytes, unaltered. Fixtures
// repeat across the corpus, so admissions are shared per fixture name
// (issue #78) — the same memo the project suite uses.
func (e *Engine) runCorpusCase(set *artifacts.Set, admissions map[string]*AdmittedPack, item corpusCase) result.EvaluationCorpusCase {
	pack, err := set.EvaluationPack(item.Pack)
	if err != nil {
		return corpusMismatch(result.EvaluationCorpusCase{
			ID:                 item.ID,
			Origin:             item.Origin,
			SpecSection:        item.SpecSection,
			Status:             "passed",
			ExpectedErrorClass: item.ExpectedErrorClass,
			ExpectedErrorPhase: item.ExpectedErrorPhase,
		}, "The row's pack fixture is not bundled.")
	}
	admitted, ok := admissions[item.Pack]
	if !ok {
		admitted = e.AdmitPack(pack)
		admissions[item.Pack] = admitted
	}
	// No rendering is computed for a bundled row: that manifest's schema closes
	// its case object, so no corpus row can declare expectedHandoffTarget and no
	// corpus row can reach the member this would feed (ADR-0025).
	return e.RunCaseAdmitted(admitted, item.MatrixCase, HandoffTargetRendering{}, "experimental evaluate-corpus")
}

// RunCase runs one case-carrier row against one pack document and reports the
// row's result.
//
// A row carrying an expected disposition passes when the disposition this
// evaluator produces canonicalizes, under RFC 8785, to the same bytes as the
// row's. A row carrying an expected §8.4 error class instead passes when the
// evaluation is refused with that class, and with that phase when the row names
// one. That is the whole comparison, and it is the same one whether the row came
// from the bundled corpus or from a project's own matrix (ADR-0012): a project
// gets the byte comparison §8.3 defines rather than a looser one written for it.
//
// A row may state one further expectation, and only one further expectation:
// expectedHandoffTarget, which is compared against the target reported beside
// the disposition rather than inside it (ADR-0025). It is optional, it is an
// assertion and not a report, and a row that omits it is judged exactly as it
// was before the member existed.
//
// command names the reporting surface, exactly as elsewhere in this package.
func (e *Engine) RunCase(pack []byte, item MatrixCase, command string) result.EvaluationCorpusCase {
	// The pack's hard byte limit comes first, before anything reads the bytes.
	// It is the same check EvaluateAdmitted makes and it is made through the same
	// helper, so the two cannot drift: before this function rendered anything it
	// delegated straight to admission, where the limit was the first thing that
	// ran, and a decode placed ahead of it would scan an oversized document —
	// arbitrarily long trailing whitespace and all — before the refusal §8.4
	// requires. A refusal returns as the row's own result, exactly as the
	// evaluation path's would.
	if failure := byteLimit("pack", pack, result.ClassPackNotConformant, false); failure != nil {
		outcome := result.EvaluationCorpusCase{
			ID:                 item.ID,
			Origin:             item.Origin,
			SpecSection:        item.SpecSection,
			Status:             "passed",
			ExpectedErrorClass: item.ExpectedErrorClass,
			ExpectedErrorPhase: item.ExpectedErrorPhase,
			ActualErrorClass:   failure.Class,
			ActualErrorPhase:   failure.Phase,
		}
		if item.ExpectedHandoffTarget != nil {
			outcome.ExpectedHandoffTarget = result.HandoffTargetUnavailable
			outcome.ActualHandoffTarget = result.HandoffTargetUnavailable
		}
		if item.ExpectedErrorClass == "" {
			return corpusMismatch(outcome, "The evaluation was refused where a disposition was expected: "+failure.Code+": "+failure.Message)
		}
		if failure.Class != item.ExpectedErrorClass {
			return corpusMismatch(outcome, "The evaluation error class differs: "+failure.Code+": "+failure.Message)
		}
		if item.ExpectedErrorPhase != "" && failure.Phase != item.ExpectedErrorPhase {
			return corpusMismatch(outcome, "The evaluation error phase differs: "+failure.Code+": "+failure.Message)
		}
		return outcome
	}
	// One row, one pack: this is the caller for whom "once per pack per run" and
	// "once per row" are the same number, so it renders its own and hands it on.
	// The rendering is skipped entirely unless the row asks about a target, and
	// it happens only for a pack that already passed the limit above.
	var declared HandoffTargetRendering
	if item.ExpectedHandoffTarget != nil {
		if document, failure := carrier.Decode(pack, carrier.DefaultLimits()); failure == nil {
			if root, ok := document.(map[string]any); ok {
				declared = e.PackHandoffTarget(root)
			}
		}
	}
	return e.RunCaseAdmitted(e.AdmitPack(pack), item, declared, command)
}

// RunCaseAdmitted is RunCase over a pack admitted once for the whole suite
// (issue #78): the same judgment, without re-validating and re-decoding the
// pack bytes for every row.
func (e *Engine) RunCaseAdmitted(admitted *AdmittedPack, item MatrixCase, declared HandoffTargetRendering, command string) result.EvaluationCorpusCase {
	outcome := result.EvaluationCorpusCase{
		ID:                 item.ID,
		Origin:             item.Origin,
		SpecSection:        item.SpecSection,
		Status:             "passed",
		ExpectedErrorClass: item.ExpectedErrorClass,
		ExpectedErrorPhase: item.ExpectedErrorPhase,
	}
	if item.ExpectedDisposition != nil {
		expected, err := canonicalDisposition(item.ExpectedDisposition)
		if err != nil {
			return corpusMismatch(outcome, "The row's expected disposition is not a canonicalizable §8.3 disposition, which is a defect in the bundled carrier and not a result: "+err.Error())
		}
		outcome.Expected = expected
	}
	// The second expectation, when the row states one (ADR-0025). Both members of
	// the pair are set to "unavailable" the moment the row is seen to declare
	// one, and each is replaced when there is something true to put there: the
	// expected side once it decodes, the actual side once an evaluation produces
	// one. So the pair appears together on every path a declaring row can take —
	// including the paths that return before an evaluation exists — and neither
	// half is ever silently absent while the other is present.
	var expectedTarget *result.HandoffTarget
	if item.ExpectedHandoffTarget != nil {
		outcome.ExpectedHandoffTarget = result.HandoffTargetUnavailable
		outcome.ActualHandoffTarget = result.HandoffTargetUnavailable
		decoded, err := DecodeHandoffTarget(item.ExpectedHandoffTarget)
		if err != nil {
			return corpusMismatch(outcome, "The row's expectedHandoffTarget is neither null nor a {kind, name} object: "+err.Error())
		}
		rendered, err := decoded.Rendered()
		if err != nil {
			return corpusMismatch(outcome, "The row's expectedHandoffTarget could not be canonicalized: "+err.Error())
		}
		expectedTarget, outcome.ExpectedHandoffTarget = decoded, rendered
	}

	evaluated, failure := e.EvaluateAdmitted(admitted, item.Facts, item.EvidenceAvailability,
		Options{Command: command, SupportedExtensions: item.SupportedExtensions})
	if failure != nil {
		outcome.ActualErrorClass, outcome.ActualErrorPhase = failure.Class, failure.Phase
		if item.ExpectedErrorClass == "" {
			return corpusMismatch(outcome, "The evaluation was refused where a disposition was expected: "+failure.Code+": "+failure.Message)
		}
		if failure.Class != item.ExpectedErrorClass {
			return corpusMismatch(outcome, "The evaluation error class differs: "+failure.Code+": "+failure.Message)
		}
		if item.ExpectedErrorPhase != "" && failure.Phase != item.ExpectedErrorPhase {
			return corpusMismatch(outcome, "The evaluation error phase differs: "+failure.Code+": "+failure.Message)
		}
		return outcome
	}
	// The identity echo is read off the pack that was actually evaluated, so a row
	// result names the document it ran against and never a name from its own
	// carrier. A row refused above carries none: a refusal produces no evaluation
	// to read the identity off, whether it came before the pack was admitted or
	// after — a reached §10 evaluation-work limit is refused with the pack in hand.
	outcome.PackID, outcome.PackVersion = evaluated.PackID, evaluated.PackVersion
	// The produced target is rendered beside the produced disposition and only
	// for a row that asked about it, so a row asserting nothing carries the
	// members it always carried. It is rendered before the disposition is
	// compared, so a row failing both comparisons still reports both sides of
	// each: the detail names the first difference, the payload names them all.
	// This is also where "unavailable" stops being the actual side's value —
	// there is now an evaluation to read one off.
	//
	// The rendering was computed once, for this pack, by whoever owns the row
	// loop, and is handed in. Nothing is rendered, compared, or locked here.
	// Rendering per row would canonicalize and hash the same authored string
	// once per row — ten thousand rows against a pack whose target name is at
	// §2.1's megabyte is ten gigabytes of work the retained-bytes budget cannot
	// see, because what is retained is capped and what is *processed* is not.
	if item.ExpectedHandoffTarget != nil {
		actualTarget, err := reportedHandoffTarget(evaluated.HandoffTarget, declared)
		if err != nil {
			return corpusMismatch(outcome, "The evaluation's handoff target could not be canonicalized: "+err.Error())
		}
		outcome.ActualHandoffTarget = actualTarget
	}
	actual, err := evaluated.Disposition.Canonical()
	if err != nil {
		return corpusMismatch(outcome, "The disposition could not be canonicalized: "+err.Error())
	}
	outcome.Actual = string(actual)
	if item.ExpectedErrorClass != "" {
		return corpusMismatch(outcome, "A disposition was produced where an evaluation error was expected.")
	}
	if outcome.Actual != outcome.Expected {
		return corpusMismatch(outcome, "The canonical disposition bytes differ.")
	}
	// §8.3 keeps the configured target outside the disposition, so this row-level
	// comparison is the only one that can see it: a pack mutation reaching only
	// escalation.target leaves every disposition byte-identical (ADR-0025).
	//
	// It is decided on the DECODED values and never on the rendered ones. The
	// renderings are capped for the report, and a capped rendering ends in
	// sixty-four bits of digest: deciding equality on it would let two different
	// long targets pass as one, which is a comparison this record exists to make
	// exact. So presence is compared as presence and each member as its whole
	// string, and the renderings are carried beside the verdict rather than
	// standing in for it.
	if item.ExpectedHandoffTarget != nil && !sameHandoffTarget(expectedTarget, evaluated.HandoffTarget) {
		return corpusMismatch(outcome, "The evaluation's handoff target differs from the row's expectation: "+handoffTargetDifference(expectedTarget, evaluated.HandoffTarget))
	}
	return outcome
}

// sameHandoffTarget is the whole of the target comparison: presence against
// presence, then each member in full.
//
// Comparing the authored strings rather than their capped renderings costs
// nothing a suite can feel. A row's expectation lives in the matrix, which
// MaxMatrixBytes bounds whole, so the total length compared across a run is
// bounded by the matrix rather than by the pack — and Go compares a string's
// length before its bytes, so an assertion of a different length is settled
// without reading either.
func sameHandoffTarget(expected, actual *result.HandoffTarget) bool {
	if expected == nil || actual == nil {
		return expected == nil && actual == nil
	}
	return expected.Kind == actual.Kind && expected.Name == actual.Name
}

// handoffTargetDifference names the shape of a handoff-target difference in
// bounded, runtime-authored words. The two renderings are reported as their own
// members of the row and are deliberately not inlined here: a target's kind and
// name are authored strings, and a detail that repeated them would put the same
// unbounded bytes in a payload for the third time (ADR-0023's rendering budget,
// applied to the reason it exists rather than to its exact mechanism).
//
// It reads the decoded values for the same reason the comparison does: "the row
// expects no target" is a fact about a nil, not about a rendering that happens
// to spell null.
//
// It is reached only after an evaluation produced a disposition. A refused
// evaluation returns earlier, with the actual side left at
// result.HandoffTargetUnavailable and a detail naming the refusal — so there is
// no refused case to describe here, and an earlier draft's parameter for one was
// dead in every caller. A branch nothing can reach is a branch that documents a
// behavior the code does not have.
func handoffTargetDifference(expected, actual *result.HandoffTarget) string {
	switch {
	case expected == nil:
		return "the row expects no target and the evaluation reports one."
	case actual == nil:
		return "the row expects a target and the evaluation reports none."
	default:
		return "both name a target and the two differ; the row's expectedHandoffTarget and actualHandoffTarget carry them."
	}
}

// DecodeHandoffTarget decodes one row's expected escalation target: JSON null,
// which asserts that the evaluation reports no target at all, or an object
// stating both kind and name. It is exported for the same one-gate reason
// DecodeDisposition is — the matrix loader and the row comparator must not be
// able to disagree about what a well-formed expectation is.
//
// It is written against the token stream rather than against a struct, and that
// is not style. `encoding/json` matches member names **case-insensitively**
// even under DisallowUnknownFields, so a struct decode reads `{"Kind":"queue"}`
// as a kind and lets `{"kind":"human-role","Kind":"queue"}` overwrite the exact
// member with the alias; it also accepts a duplicated member silently, and
// stops at the end of the first value so trailing JSON rides along unread. A
// closed shape has to refuse all three, and none of them is expressible as a
// decoder option.
//
// Both members are required and neither may be empty, because an escalation
// target §8.1 admits states both and a pack declaring an empty one is refused
// long before a row could match it. The kind vocabulary is deliberately not
// restated here: the enumeration belongs to the pack schema, which has already
// held every evaluated pack to it, and a row naming a kind outside it is
// reported as the loud mismatch it is rather than by a second copy of a list
// this package does not own.
func DecodeHandoffTarget(raw json.RawMessage) (*result.HandoffTarget, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token == nil {
		if err := endOfValue(decoder); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if token != json.Delim('{') {
		return nil, errors.New("an expected handoff target is null or a {kind, name} object")
	}
	target := &result.HandoffTarget{}
	seen := map[string]bool{}
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := nameToken.(string)
		if !ok {
			return nil, errors.New("an object member name is not a string")
		}
		member, known := map[string]*string{"kind": &target.Kind, "name": &target.Name}[name]
		if !known {
			return nil, fmt.Errorf("an expected handoff target states kind and name and no other member; %q is not one of them", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("member %q appears more than once", name)
		}
		seen[name] = true
		valueToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		value, ok := valueToken.(string)
		if !ok {
			return nil, fmt.Errorf("the %s of an expected handoff target is a string", name)
		}
		*member = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("an expected handoff target is null or a {kind, name} object")
	}
	if err := endOfValue(decoder); err != nil {
		return nil, err
	}
	if target.Kind == "" || target.Name == "" {
		return nil, errors.New("an expected handoff target states a non-empty kind and a non-empty name")
	}
	return target, nil
}

// endOfValue refuses anything after the value just read. A decoder that stops at
// the end of the first value would read `null {"kind":"queue","name":"Ops"}` as
// an assertion of no target, silently dropping the half an author meant.
func endOfValue(decoder *json.Decoder) error {
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("an expected handoff target carries trailing data after its value")
	}
	return nil
}

func corpusMismatch(outcome result.EvaluationCorpusCase, detail string) result.EvaluationCorpusCase {
	outcome.Status = "mismatch"
	outcome.Detail = detail
	return outcome
}

// canonicalDisposition canonicalizes one row's expected disposition through the
// same type, the same set normalization, and the same encoder the actual side
// uses, so the two sides of the comparison are produced identically.
//
// Comparing is then disposition equality as §8.3 defines it rather than raw byte
// equality of what the manifest happens to store: reasons and
// handoff.triggeredBy are sets, and "serialized order is never a difference in
// the disposition". Every bundled row stores them sorted, so this changes no
// current result; a future row that did not would be reported as the equal
// disposition it is instead of as a mismatch. Canonical() also holds the
// expectation to §8.3's three presence rules, so a carrier defect the manifest
// schema cannot catch is named as one.
func canonicalDisposition(raw json.RawMessage) (string, error) {
	disposition, err := DecodeDisposition(raw)
	if err != nil {
		return "", err
	}
	encoded, err := disposition.Canonical()
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// DecodeDisposition decodes one §8.3 disposition strictly — unknown members
// refused — and holds it to every invariant §8.3 states about the disposition
// alone, through the same canonicalization the row comparator applies. It is
// exported because the comparator is not the only reader of expected
// dispositions: the coverage derivation reads them too, and a second, looser
// decoder could accept as a witness the exact expectation the comparator
// refuses. One gate, however many readers.
func DecodeDisposition(raw json.RawMessage) (result.Disposition, error) {
	var disposition result.Disposition
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&disposition); err != nil {
		return result.Disposition{}, err
	}
	if _, err := disposition.Canonical(); err != nil {
		return result.Disposition{}, err
	}
	return disposition, nil
}

func internalCorpusFailure(message string) *Failure {
	return &Failure{Code: "JPS-ARTIFACT-EVALUATION-CORPUS", Message: message, ExitCode: result.ExitInternal}
}
