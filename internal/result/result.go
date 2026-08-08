package result

import (
	"errors"
	"fmt"
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// OutputVersion is the machine-output protocol version of every payload this
// runtime writes. VERSIONING.md makes it a protocol version and not the CLI
// release version, and requires a change that breaks machine-output
// compatibility to increment it deliberately. It became "2" when the
// experimental-evaluation payloads replaced their conformanceClaim member with
// the conformanceClaimReference member of this file (ADR-0011): a consumer that
// read the old member name finds no member of that name, which is a break and
// not an added field. The CHANGELOG entry for that change carries the migration.
const (
	OutputVersion = "2"
	CLIName       = "jpack"
)

// CLIVersion may be replaced at build time with -ldflags once releases are approved.
var CLIVersion = "0.0.0-dev"

const (
	ExitSuccess     = 0
	ExitInvalid     = 1
	ExitUnsupported = 2
	ExitInvocation  = 3
	ExitIO          = 4
	ExitInternal    = 5
)

type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func CurrentTool() Tool {
	return Tool{Name: CLIName, Version: CLIVersion}
}

type Diagnostic struct {
	Code          string `json:"code"`
	CodeStability string `json:"codeStability"`
	Layer         string `json:"layer"`
	Severity      string `json:"severity"`
	InstancePath  string `json:"instancePath"`
	Message       string `json:"message"`
}

func ErrorDiagnostic(code, layer, instancePath, message string) Diagnostic {
	return Diagnostic{
		Code:          code,
		CodeStability: "provisional",
		Layer:         layer,
		Severity:      "error",
		InstancePath:  instancePath,
		Message:       message,
	}
}

type Layer struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type ValidationScope struct {
	RequestedThrough        string `json:"requestedThrough"`
	FullDocumentConformance bool   `json:"fullDocumentConformance"`
}

type Extensions struct {
	Required    []string `json:"required"`
	Supported   []string `json:"supported"`
	Unsupported []string `json:"unsupported"`
}

type Artifact struct {
	SpecVersion  string `json:"specVersion"`
	BundleDigest string `json:"bundleDigest"`
	Provenance   string `json:"provenance"`
}

type Validation struct {
	OutputVersion        string          `json:"outputVersion"`
	Tool                 Tool            `json:"tool"`
	Command              string          `json:"command"`
	Status               string          `json:"status"`
	SpecVersion          string          `json:"specVersion,omitempty"`
	ValidationScope      ValidationScope `json:"validationScope"`
	Layers               []Layer         `json:"layers"`
	Extensions           Extensions      `json:"extensions"`
	Diagnostics          []Diagnostic    `json:"diagnostics"`
	DiagnosticsTruncated bool            `json:"diagnosticsTruncated"`
	Artifact             *Artifact       `json:"artifact,omitempty"`
}

func NewValidation(through string) Validation {
	return Validation{
		OutputVersion: OutputVersion,
		Tool:          CurrentTool(),
		Command:       "spec validate",
		ValidationScope: ValidationScope{
			RequestedThrough:        through,
			FullDocumentConformance: through == "semantic",
		},
		Layers:      []Layer{},
		Diagnostics: []Diagnostic{},
		Extensions: Extensions{
			Required:    []string{},
			Supported:   []string{},
			Unsupported: []string{},
		},
	}
}

type ExpectedDiagnostic struct {
	Code string `json:"code"`
	Path string `json:"path"`
}

type Case struct {
	ID                   string              `json:"id"`
	ExpectedStatus       string              `json:"expectedStatus"`
	ActualStatus         string              `json:"actualStatus"`
	Status               string              `json:"status"`
	ExpectedDiagnostic   *ExpectedDiagnostic `json:"expectedDiagnostic"`
	ActualDiagnostics    []Diagnostic        `json:"actualDiagnostics"`
	DiagnosticsTruncated bool                `json:"diagnosticsTruncated"`
}

type SuiteSummary struct {
	Total      int `json:"total"`
	Passed     int `json:"passed"`
	Mismatched int `json:"mismatched"`
}

type Suite struct {
	OutputVersion         string       `json:"outputVersion"`
	Tool                  Tool         `json:"tool"`
	Command               string       `json:"command"`
	Status                string       `json:"status"`
	SpecVersion           string       `json:"specVersion"`
	SuiteVersion          string       `json:"suiteVersion"`
	CorpusDigest          string       `json:"corpusDigest"`
	CorpusDigestAlgorithm string       `json:"corpusDigestAlgorithm"`
	Provenance            string       `json:"provenance"`
	Summary               SuiteSummary `json:"summary"`
	Cases                 []Case       `json:"cases"`
	Diagnostics           []Diagnostic `json:"diagnostics"`
	DiagnosticsTruncated  bool         `json:"diagnosticsTruncated"`
}

type Schema struct {
	OutputVersion string `json:"outputVersion"`
	Tool          Tool   `json:"tool"`
	Command       string `json:"command"`
	Status        string `json:"status"`
	SpecVersion   string `json:"specVersion"`
	SchemaID      string `json:"schemaId"`
	Bytes         int    `json:"bytes"`
	SHA256        string `json:"sha256"`
	Provenance    string `json:"provenance"`
	WrittenTo     string `json:"writtenTo,omitempty"`
}

type Version struct {
	OutputVersion      string   `json:"outputVersion"`
	Tool               Tool     `json:"tool"`
	Command            string   `json:"command"`
	Status             string   `json:"status"`
	SupportedSpecs     []string `json:"supportedSpecVersions"`
	ArtifactProvenance string   `json:"artifactProvenance"`
}

// ExampleKind labels the example payloads: the bundled examples are the
// runtime's digest-locked conformance fixtures surfaced read-only, not authored
// templates. It appears in-band so a consumer never mistakes one for a template.
const ExampleKind = "version-pinned-conformance-fixture"

type ExampleSummary struct {
	Name        string `json:"name"`
	Focus       string `json:"focus"`
	SpecSection string `json:"specSection"`
}

type Examples struct {
	OutputVersion string           `json:"outputVersion"`
	Tool          Tool             `json:"tool"`
	Command       string           `json:"command"`
	Status        string           `json:"status"`
	SpecVersion   string           `json:"specVersion"`
	Provenance    string           `json:"provenance"`
	Kind          string           `json:"kind"`
	Examples      []ExampleSummary `json:"examples"`
}

type Example struct {
	OutputVersion string `json:"outputVersion"`
	Tool          Tool   `json:"tool"`
	Command       string `json:"command"`
	Status        string `json:"status"`
	SpecVersion   string `json:"specVersion"`
	Name          string `json:"name"`
	Focus         string `json:"focus"`
	SpecSection   string `json:"specSection"`
	Bytes         int    `json:"bytes"`
	SHA256        string `json:"sha256"`
	Provenance    string `json:"provenance"`
	Kind          string `json:"kind"`
	WrittenTo     string `json:"writtenTo,omitempty"`
}

// --- experimental evaluation (ADR-0007; spec RFC 0006) ---

// EvaluationClaimReference is carried by every experimental-evaluation payload,
// and it is a locator rather than a claim: the conformance claim is stated, in
// full and only, in CONFORMANCE.md, and this member says where to read it
// (ADR-0011).
//
// It is deliberately not a restatement. §3.4.1 fixes the entire form a claim may
// take — the class, one exact specVersion, the corpus version, the results
// obtained, and in the claim's own words that every row of that corpus version
// passed — so a payload member naming only a class and a version would carry part
// of that form and omit the rest, which is the partial claim §3.4.1 forbids. The
// payload therefore points at the one document that carries all of it and asserts
// nothing about conformance itself. The version scope a consumer needs in band is
// EvaluatorSpecVersion, which is a fact about the contract this evaluator applied
// and not a claim about conformance to it.
//
// The member was "none" under ADR-0007 and ADR-0010, when denying a claim was
// accurate; it is now a reference, and its member name changed with its meaning
// (conformanceClaimReference, OutputVersion "2").
const EvaluationClaimReference = "CONFORMANCE.md"

// EvaluatorSpecVersion names the exact JPS Core version whose evaluator contract
// this runtime's experimental evaluator applies: the §8.2 input preflight, the
// §8.3 portable disposition, and the §8.4 error classes and their precedence.
//
// It is reported independently of the pack's own specVersion, and every
// evaluation payload carries both, because they are two different facts even
// though this evaluator now requires them to be equal: §11 makes the declared
// value exact, so a pack must declare this version to be evaluated at all, and a
// pack declaring any other is refused as pack-not-conformant in the preflight
// phase (evaluation.declaredSpecVersion). Keeping both members means a consumer
// reads the contract that was applied from the payload rather than inferring it
// from the pack, and it survives a later version that admits more than one.
//
// It is also the exact version the claim in CONFORMANCE.md is made against, and
// therefore the version scope of that claim: nothing is claimed under
// 0.1.0-draft, which defines no evaluator class and under which §3.4.1 forbids
// such a claim outright.
const EvaluatorSpecVersion = "0.2.0-draft"

// HandoffTarget echoes the pack's declared escalation target when a handoff is
// requested. It is reported beside the disposition and never inside it: §8.3
// keeps the configured target out of the disposition object, so a disposition
// can never disagree with the pack it came from.
type HandoffTarget struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// Handoff is the disposition's handoff object (§8.3): the state, and — exactly
// when the state is "requested" — the non-empty subset of the retained reasons
// that triggered the request. A direct exception escalation on a pack with no
// escalation object is a requested handoff with no Core-defined destination.
type Handoff struct {
	State       string   `json:"state"`
	TriggeredBy []string `json:"triggeredBy,omitempty"`
}

// Disposition is the portable disposition of JPS Core §8.3: kind ("outcome",
// "not-applicable", or "unresolved"), the outcome id exactly when kind is
// "outcome", the retained reason set as a sorted duplicate-free array (empty
// exactly when kind is "outcome"), and the handoff object. It carries these
// members and no others.
//
// It serializes as its own RFC 8785 canonical form, so the disposition member of
// any JSON payload this runtime writes without --pretty is the byte sequence
// §8.3 requires two implementations to agree on, with no second serializer to
// drift from. Under --pretty the member order and both sets are still canonical,
// but the payload's indentation reaches inside this member too, so those exact
// bytes are not present: §8.3 requires canonicalization "where a byte comparison
// is required", and a byte comparison must recanonicalize either side it did not
// itself produce.
type Disposition struct {
	Kind      string   `json:"kind"`
	OutcomeID string   `json:"outcomeId,omitempty"`
	Reasons   []string `json:"reasons"`
	Handoff   Handoff  `json:"handoff"`
}

// Canonical returns the disposition's RFC 8785 canonicalization: members ordered
// by name, both sets serialized as sorted duplicate-free arrays, an absent
// outcomeId or triggeredBy omitted rather than written as null, and no
// whitespace. Sorting happens here as well as at the source of the sets, so a
// caller cannot produce a non-canonical byte sequence by assembling a
// disposition itself.
//
// It refuses a value that is not a legal §8.3 disposition rather than serializing
// one, enforcing every invariant that section states about the disposition alone:
// the three enumerated vocabularies (kind, handoff.state, and the reason set's
// members), the three iff rules a Go struct cannot express — outcomeId is present
// iff kind is "outcome", reasons is empty iff kind is "outcome", and triggeredBy
// is present iff the handoff state is "requested" — the exact reason set of a
// not-applicable result, and triggeredBy being a subset of reasons. The one place
// canonicalization lives is therefore also the place that holds a caller to them.
// The engine builds no illegal value; an exported type can be handed one.
//
// The one §8.3 requirement it cannot enforce is the one that is not about the
// disposition alone: that outcomeId "MUST name a declared outcome of the pack
// evaluated" is a fact about a pack this type never sees, so it stays where the
// pack is — the engine names only ids it read from the pack, and semantic
// validation has already refused a pack whose rules name an undeclared outcome.
// Whether the handoff state agrees with the pack's escalation object (§8.1) is
// pack-dependent in the same way and is likewise the engine's.
func (d Disposition) Canonical() ([]byte, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	handoff := map[string]any{"state": d.Handoff.State}
	if len(d.Handoff.TriggeredBy) > 0 {
		handoff["triggeredBy"] = sortedSet(d.Handoff.TriggeredBy)
	}
	object := map[string]any{
		"kind":    d.Kind,
		"reasons": sortedSet(d.Reasons),
		"handoff": handoff,
	}
	if d.OutcomeID != "" {
		object["outcomeId"] = d.OutcomeID
	}
	return jcs.Encode(object)
}

// MarshalJSON writes the canonical form of §8.3.
func (d Disposition) MarshalJSON() ([]byte, error) {
	return d.Canonical()
}

// The three closed vocabularies of §8.3. A value outside any of them is not a
// disposition at all: "no other value is admitted" is that section's own wording
// for the reason set, and kind and handoff.state are stated as enumerations.
// The evaluation engine has its own constants for the reasons it produces; these
// are the gate every disposition passes on its way to bytes, whoever built it.
var (
	dispositionKinds   = []string{"outcome", "not-applicable", "unresolved"}
	handoffStates      = []string{"none", "requested"}
	dispositionReasons = []string{
		"not-applicable",
		"missing-required-evidence",
		"unknown",
		"conflict",
		"no-match",
		"exception-escalation",
	}
)

// validate holds one disposition to every §8.3 invariant that is about the
// disposition alone: the three closed vocabularies, the three presence iff rules,
// the exact reason set of a not-applicable result, and triggeredBy ⊆ reasons.
//
// None of the presence rules can be a struct tag: an omitempty tag omits an empty
// value, and §8.3 makes an empty value in those positions illegal rather than
// absent. The reason set is compared as a set, since duplicates in the assembled
// value are the caller's and not a difference in the disposition.
func (d Disposition) validate() error {
	if !slices.Contains(dispositionKinds, d.Kind) {
		return fmt.Errorf("§8.3: kind must be \"outcome\", \"not-applicable\", or \"unresolved\"; got %q", d.Kind)
	}
	if !slices.Contains(handoffStates, d.Handoff.State) {
		return fmt.Errorf("§8.3: handoff.state must be \"requested\" or \"none\"; got %q", d.Handoff.State)
	}
	reasons := sortedSet(d.Reasons)
	for _, reason := range reasons {
		if !slices.Contains(dispositionReasons, reason) {
			return fmt.Errorf("§8.3: %q is not a reason the disposition admits", reason)
		}
	}
	if d.Kind == "outcome" {
		if d.OutcomeID == "" {
			return errors.New("§8.3: outcomeId must be present when kind is \"outcome\"")
		}
		if len(reasons) > 0 {
			return errors.New("§8.3: reasons is empty if and only if kind is \"outcome\"")
		}
	} else {
		if d.OutcomeID != "" {
			return errors.New("§8.3: outcomeId must be absent when kind is not \"outcome\"")
		}
		if len(reasons) == 0 {
			return errors.New("§8.3: reasons is empty if and only if kind is \"outcome\"")
		}
	}
	// §8.3: "When kind is not-applicable its one member is not-applicable." A
	// not-applicable result carrying any other reason, or another reason beside it,
	// is a different result reported under that kind.
	if d.Kind == "not-applicable" && (len(reasons) != 1 || reasons[0] != "not-applicable") {
		return errors.New("§8.3: the reason set of a not-applicable result is exactly {\"not-applicable\"}")
	}
	if (d.Handoff.State == "requested") != (len(d.Handoff.TriggeredBy) > 0) {
		return errors.New("§8.3: handoff.triggeredBy is present, and non-empty, if and only if state is \"requested\"")
	}
	// §8.3: triggeredBy "is always a subset of reasons". With reasons empty exactly
	// when kind is "outcome", this is also what makes an outcome with a requested
	// handoff illegal rather than merely unproduced: it would name a trigger the
	// retained reason set does not contain.
	for _, trigger := range d.Handoff.TriggeredBy {
		if !slices.Contains(reasons, trigger) {
			return fmt.Errorf("§8.3: handoff.triggeredBy must be a subset of reasons; %q is not a retained reason", trigger)
		}
	}
	return nil
}

// sortedSet is one §8.3 reason set as it must be serialized: ascending by
// Unicode code point, duplicate-free, and an empty set as an empty array.
func sortedSet(values []string) []string {
	output := make([]string, 0, len(values))
	for _, value := range values {
		if !slices.Contains(output, value) {
			output = append(output, value)
		}
	}
	slices.Sort(output)
	return output
}

// Evaluation-error classes of JPS Core §8.4, in the fixed precedence order that
// section requires: the first class that applies to one evaluation's inputs is
// the class reported. An evaluation error is never a disposition.
const (
	ClassPackNotConformant            = "pack-not-conformant"
	ClassMalformedInput               = "malformed-input"
	ClassUnsupportedRequiredExtension = "unsupported-required-extension"
	ClassResourceExhaustion           = "resource-exhaustion"
)

// Evaluation-error phases of JPS Core §8.4: a limit reached while admitting an
// input (§8.2) belongs to the preflight, and one reached while evaluating an
// admitted input belongs to the evaluation.
const (
	PhasePreflight  = "preflight"
	PhaseEvaluation = "evaluation"
)

// EvaluationError names the §8.4 class of an evaluation error and the phase it
// was reached in. It is the coarse, portable identity of the failure; the
// diagnostic beside it keeps this runtime's finer JPS-* code, which is the
// detail and not the class. EvaluatorSpecVersion names the Core version whose
// §8.4 contract assigned the class, which is a fact about this evaluator and not
// about the pack that was refused.
type EvaluationError struct {
	Class                string `json:"class"`
	Phase                string `json:"phase"`
	EvaluatorSpecVersion string `json:"evaluatorSpecVersion"`
}

// TraceEntry records one applicability, exception, or rule evaluation. The
// trace is informative: §8 requires an unknown that resolution ignored to remain
// visible, and permits recording contributing ids. A pack's applicability is one
// unnamed condition rather than an authored declaration, so its entry carries no
// id at all rather than an empty one.
type TraceEntry struct {
	Stage      string `json:"stage"`
	ID         string `json:"id,omitempty"`
	Condition  string `json:"condition"`
	Effect     string `json:"effect,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	Suppressed bool   `json:"suppressed,omitempty"`
	OnUnknown  string `json:"onUnknown,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
}

// DraftPrototype marks an evaluation that ran under a draft-RFC grammar
// extension rather than a published JPS version. It is present exactly when the
// caller opted into such a grammar, and it says in band what the operators are
// and that the pack carrying them is not valid under the specification the rest
// of the payload names. A consumer that ignores it is reading a disposition
// produced by operators no JPS version defines.
type DraftPrototype struct {
	RFC                       string   `json:"rfc"`
	Status                    string   `json:"status"`
	Operators                 []string `json:"operators"`
	PackValidUnderSpecVersion bool     `json:"packValidUnderSpecVersion"`
	Note                      string   `json:"note"`
}

// Evaluation is the experimental-evaluation envelope. Experimental is always
// true so no consumer can read the payload as a standard: this surface may change
// or be removed without compatibility promise (ADR-0007).
// ConformanceClaimReference is always set and always the same locator; it makes
// no claim, and the claim it locates is CONFORMANCE.md's alone.
// SpecVersion is the version the evaluated pack declares; EvaluatorSpecVersion is
// the version of the evaluator contract applied to it (§8.2–§8.4). Both are always
// present, and this evaluator requires them to agree: §11 makes the declared value
// exact, so a pack declaring another version is refused rather than evaluated.
//
// PackID and PackVersion echo the evaluated pack's own id and version members.
// They are additive members and not a new fact: they are read off the document
// that was evaluated, so a payload cannot name a pack the evaluation did not
// read. That is what makes them safe to echo — a project configuration's pinned
// version, a filename, and this echo are all validated references to the one
// statement of identity the pack document carries, never independent truths
// beside it (ADR-0012). Adding them does not move OutputVersion, on two
// VERSIONING.md rules read together: its compatibility-dimensions clause makes a
// break in machine-output compatibility the thing that increments the protocol
// version, and its MINOR bullet classes an added output field as a
// backward-compatible change. A consumer that never read these members reads the
// same payload it read before, so nothing broke.
type Evaluation struct {
	OutputVersion             string          `json:"outputVersion"`
	Tool                      Tool            `json:"tool"`
	Command                   string          `json:"command"`
	Status                    string          `json:"status"`
	Experimental              bool            `json:"experimental"`
	ConformanceClaimReference string          `json:"conformanceClaimReference"`
	SpecVersion               string          `json:"specVersion"`
	EvaluatorSpecVersion      string          `json:"evaluatorSpecVersion"`
	PackID                    string          `json:"packId"`
	PackVersion               string          `json:"packVersion"`
	DraftPrototype            *DraftPrototype `json:"draftPrototype,omitempty"`
	Disposition               Disposition     `json:"disposition"`
	HandoffTarget             *HandoffTarget  `json:"handoffTarget,omitempty"`
	Trace                     []TraceEntry    `json:"trace"`
	Artifact                  *Artifact       `json:"artifact,omitempty"`
}

// EvaluationCorpusLabel labels every evaluation-corpus run this runtime reports.
// A run is results, and the claim those results are evidence for is one document
// with one scope: §3.4.1 makes corpus results required evidence for the claim and
// not exhaustive evidence of it, so a passing run neither is the claim nor
// exhausts it. The label points at the claim rather than denying one, which is
// what it did before ADR-0011.
const EvaluationCorpusLabel = "corpus results, the required evidence for the claim in CONFORMANCE.md"

// EvaluationCorpusCase is one evaluation-corpus row's result. Expected and
// actual are the RFC 8785 canonical dispositions compared byte for byte — both
// produced by the same canonicalizer, so the comparison is disposition equality
// as §8.3 defines it and not raw equality of what the manifest stores — or the
// §8.4 class and phase for a row that expects an error instead of a disposition.
//
// PackID and PackVersion echo the identity members of the pack this row ran
// against, exactly as an evaluation payload does, and are absent for a row that
// produced no disposition — whether the refusal came before the pack was
// admitted or after, as a reached §10 limit does. The echo is read off the
// evaluation that produced it, and inventing one from the row's carrier would be
// the independent truth this echo is deliberately not.
type EvaluationCorpusCase struct {
	ID                 string `json:"id"`
	Origin             string `json:"origin"`
	SpecSection        string `json:"specSection"`
	PackID             string `json:"packId,omitempty"`
	PackVersion        string `json:"packVersion,omitempty"`
	Status             string `json:"status"`
	Expected           string `json:"expected"`
	Actual             string `json:"actual"`
	ExpectedErrorClass string `json:"expectedErrorClass,omitempty"`
	ActualErrorClass   string `json:"actualErrorClass,omitempty"`
	ExpectedErrorPhase string `json:"expectedErrorPhase,omitempty"`
	ActualErrorPhase   string `json:"actualErrorPhase,omitempty"`
	Detail             string `json:"detail,omitempty"`
}

// EvaluationCorpus is one run of the evaluation corpus bundled for an exact
// specification version. Like every experimental-evaluation payload it carries
// Experimental and ConformanceClaimReference, and it additionally carries Label so
// no reader can take a passing run for the claim of §3.4.1.
type EvaluationCorpus struct {
	OutputVersion             string                 `json:"outputVersion"`
	Tool                      Tool                   `json:"tool"`
	Command                   string                 `json:"command"`
	Status                    string                 `json:"status"`
	Experimental              bool                   `json:"experimental"`
	ConformanceClaimReference string                 `json:"conformanceClaimReference"`
	Label                     string                 `json:"label"`
	SpecVersion               string                 `json:"specVersion"`
	SuiteVersion              string                 `json:"suiteVersion"`
	CorpusStatus              string                 `json:"corpusStatus"`
	CorpusLabel               string                 `json:"corpusLabel"`
	Provenance                string                 `json:"provenance"`
	Summary                   SuiteSummary           `json:"summary"`
	Cases                     []EvaluationCorpusCase `json:"cases"`
}

// --- the jpack.json project convention (ADR-0012) ---

// ProjectKind labels every payload of the project convention, in band, as what
// it is: a convention of this runtime and not part of the specification. A
// consumer that meets one of these payloads without reading ADR-0012 still
// learns from the payload itself that nothing here is normative, and that no
// other JPS implementation is obliged to understand a jpack.json.
const ProjectKind = "non-normative-runtime-convention"

// PackMatrixLabel labels every project matrix run. A project's matrix is the
// project's own rows about the project's own packs; it is not the specification's
// evaluation corpus, and running it reports what those rows did. The evaluator
// that ran them is the experimental one, whose conformance claim is stated, in
// full and only, in CONFORMANCE.md; this label points there and says nothing of
// its own about it.
const PackMatrixLabel = "project matrix results: rows a project wrote about its own packs, run through the experimental evaluator; that evaluator's conformance claim is stated, in full and only, in CONFORMANCE.md"

// The status of one derived coverage probe in a packs test report. A probe is
// covered when some row witnesses it — by its expectation for a disposition
// probe, by its authored facts for a boundary probe — and missing when no row
// does. There is no third status: a probe the pack's own declarations make
// unreachable is not derived at all, because listing it as skipped would state
// a reachability claim in a status field.
const (
	MatrixProbeCovered = "covered"
	MatrixProbeMissing = "missing"
)

// MatrixProbe is one derived coverage probe: a behavior or a comparison
// boundary the pack's own declarations make reachable, and whether any matrix
// row witnesses it. Coverage reads what the rows document — a disposition
// probe is witnessed by what a row expects, a boundary probe by what a row's
// authored facts state — and never what an evaluation produced. A covered
// probe says "a row states this", never that the row is right, and a matrix
// with every probe covered has stated something about every derivable
// behavior, which is not a statement that the pack decides well or that its
// rows expect the right things.
type MatrixProbe struct {
	Probe  string `json:"probe"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// The status of one check in a packs validate report. A skipped check is one the
// configuration did not ask for — an absent expectedVersion, an absent matrix,
// absent hints, a filename outside the optional convention — or one an earlier
// failure left nothing to run, and it is reported rather than silently omitted,
// so a reader can tell "this passed" from "this was never checked".
const (
	PackCheckPassed  = "passed"
	PackCheckFailed  = "failed"
	PackCheckSkipped = "skipped"
)

// The expectedVersion outcomes of one inventory row. "unset" means the entry
// pins no version, "matches" and "differs" compare the pin against the pack
// document's own version member, and "unknown" means the document could not be
// read to compare against — which is not a match and is never reported as one.
const (
	PackVersionUnset   = "unset"
	PackVersionMatches = "matches"
	PackVersionDiffers = "differs"
	PackVersionUnknown = "unknown"
)

// ProjectHint is one non-normative agent hint declared in a jpack.json entry:
// where a fact or a piece of evidence is held, and how to obtain it. The runtime
// carries it and never acts on it — it holds no credential, opens no connection,
// and reads no source (ADR-0004, ADR-0006, ADR-0012). Key is the RFC 6901 JSON
// Pointer for a fact hint and the declared evidence-requirement id for an
// evidence hint.
type ProjectHint struct {
	Key    string `json:"key"`
	Source string `json:"source,omitempty"`
	Hint   string `json:"hint,omitempty"`
}

// PackSummary is one resolved inventory row: the project's decision id beside
// the pack document's own identity, which are two different names and are
// reported as two members for that reason. Detail is present when the document
// could not be read or decoded, in which case the identity members are empty
// rather than guessed.
//
// ConsultedFactPaths is the pointers the document's conditions carry, sorted
// and deduplicated (ADR-0020); the empty string is the root pointer. It
// reports what the document says, not a verdict on it: listing is not
// validating, a string that is not a legal pointer is reported verbatim, and
// a document that could not be read carries [] beside its Detail exactly as
// evidenceRequirements does. The list over-approximates by design — an
// object shaped like a condition but carried as data is collected too — so a
// consumer treats it as candidate pointers from an untrusted document, never
// as instructions or as proof of a read. An additive member, so outputVersion
// stays "2" by the same two VERSIONING.md rules ADR-0012 cites.
type PackSummary struct {
	ID                    string        `json:"id"`
	PackID                string        `json:"packId"`
	PackVersion           string        `json:"packVersion"`
	Path                  string        `json:"path"`
	MatrixPath            string        `json:"matrixPath,omitempty"`
	Matrix                bool          `json:"matrix"`
	Description           string        `json:"description,omitempty"`
	ExpectedVersion       string        `json:"expectedVersion,omitempty"`
	ExpectedVersionStatus string        `json:"expectedVersionStatus"`
	EvidenceRequirements  []string      `json:"evidenceRequirements"`
	ConsultedFactPaths    []string      `json:"consultedFactPaths"`
	Facts                 []ProjectHint `json:"facts"`
	Evidence              []ProjectHint `json:"evidence"`
	Detail                string        `json:"detail,omitempty"`
}

// PackInventory is the resolved project inventory. Status is "none" when no
// configuration was found at the resolved location, which is an answer and not a
// failure: a project may simply not use the convention, and Note says where the
// runtime looked.
type PackInventory struct {
	OutputVersion string        `json:"outputVersion"`
	Tool          Tool          `json:"tool"`
	Command       string        `json:"command"`
	Status        string        `json:"status"`
	Kind          string        `json:"kind"`
	ConfigPath    string        `json:"configPath"`
	ConfigVersion string        `json:"configVersion,omitempty"`
	Note          string        `json:"note,omitempty"`
	Packs         []PackSummary `json:"packs"`
}

// PackDocument is the metadata beside one pack document served by decision id.
// The bytes travel separately, exactly as a bundled example's do.
//
// Status is "valid" when the served bytes decoded and the identity members below
// were read off them, and "undecodable" when they did not, in which case Detail
// says why and those members are empty rather than guessed. Neither value is a
// verdict on the document's conformance: serving a pack does not validate it,
// and spec validate is what reports that.
type PackDocument struct {
	OutputVersion string `json:"outputVersion"`
	Tool          Tool   `json:"tool"`
	Command       string `json:"command"`
	Status        string `json:"status"`
	Kind          string `json:"kind"`
	ConfigPath    string `json:"configPath"`
	ID            string `json:"id"`
	PackID        string `json:"packId"`
	PackVersion   string `json:"packVersion"`
	SpecVersion   string `json:"specVersion"`
	Path          string `json:"path"`
	Description   string `json:"description,omitempty"`
	Bytes         int    `json:"bytes"`
	SHA256        string `json:"sha256"`
	Detail        string `json:"detail,omitempty"`
}

// PackCounts summarizes a per-pack report: one count per pack checked, and one
// pack is either passed or failed. A check the configuration never asked for is
// skipped per check rather than per pack, which is where PackCheckSkipped
// reports it — a pack with four passed checks and one skipped has passed.
type PackCounts struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

// PackCheck is one named check applied to one configured pack.
type PackCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// PackValidationEntry is every check applied to one configured pack, in the
// order they were applied. A failed check that makes a later one meaningless —
// a path that escapes the project root — leaves the rest skipped rather than
// reporting checks that never ran.
type PackValidationEntry struct {
	ID     string      `json:"id"`
	Path   string      `json:"path"`
	Status string      `json:"status"`
	Checks []PackCheck `json:"checks"`
}

// PackValidation is one packs validate report.
//
// Checks are the checks about the configuration itself rather than about any
// one pack — today the containment of the audit directory, which no pack entry
// owns. They are a separate list because the summary counts packs: a
// configuration-level failure moves Status without moving a pack count, and
// folding it into a pack's checks would report it against a pack that is fine.
// An additive member, so outputVersion stays "2" by the same two VERSIONING.md
// rules ADR-0012 cites.
type PackValidation struct {
	OutputVersion string                `json:"outputVersion"`
	Tool          Tool                  `json:"tool"`
	Command       string                `json:"command"`
	Status        string                `json:"status"`
	Kind          string                `json:"kind"`
	ConfigPath    string                `json:"configPath"`
	ConfigVersion string                `json:"configVersion"`
	Summary       PackCounts            `json:"summary"`
	Checks        []PackCheck           `json:"checks,omitempty"`
	Packs         []PackValidationEntry `json:"packs"`
}

// LintCounts is one packs lint summary. Unlike PackCounts it carries a
// skipped member, because a lint entry has three outcomes and a summary that
// folded skipped packs into neither number would misdescribe its own total.
type LintCounts struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

// PackProducersLintEntry is one pack's producer lint: whether every pointer
// its conditions consult has a producer, and whether declared and supplied
// evidence agree. Status follows the packs test
// discipline — "skipped" when nothing was checkable, never "passed".
type PackProducersLintEntry struct {
	ID          string      `json:"id"`
	PackID      string      `json:"packId"`
	PackVersion string      `json:"packVersion"`
	Path        string      `json:"path"`
	Status      string      `json:"status"`
	Checks      []PackCheck `json:"checks"`
}

// PackProducersLint is one packs lint report (ADR-0022). It is its own
// payload rather than a member of an existing one, on the same reasoning
// PackLock records: a surface this new should be removable without taking a
// member out of a payload consumers already read. ProducersSource says which
// producer declaration the run was held to — the configuration's own hints,
// or an explicit manifest.
type PackProducersLint struct {
	OutputVersion   string                   `json:"outputVersion"`
	Tool            Tool                     `json:"tool"`
	Command         string                   `json:"command"`
	Status          string                   `json:"status"`
	Kind            string                   `json:"kind"`
	ConfigPath      string                   `json:"configPath"`
	ConfigVersion   string                   `json:"configVersion"`
	ProducersSource string                   `json:"producersSource"`
	Summary         LintCounts               `json:"summary"`
	Checks          []PackCheck              `json:"checks,omitempty"`
	Packs           []PackProducersLintEntry `json:"packs"`
}

// PackLock is one packs lock report: the reviewed set this run declared
// (ADR-0019). It is its own payload rather than a member of an existing one,
// on ADR-0017's precedent — a surface this new should be removable without
// taking a member out of a payload consumers already read.
type PackLock struct {
	OutputVersion string     `json:"outputVersion"`
	Tool          Tool       `json:"tool"`
	Command       string     `json:"command"`
	Status        string     `json:"status"`
	Kind          string     `json:"kind"`
	ConfigPath    string     `json:"configPath"`
	LockPath      string     `json:"lockPath"`
	LockVersion   string     `json:"lockVersion"`
	ConfigDigest  string     `json:"configDigest"`
	Summary       PackCounts `json:"summary"`
	Entries       []LockedID `json:"entries"`
	WrittenTo     string     `json:"writtenTo,omitempty"`
}

// LockedID is one declared document in the reviewed set, reported in the
// sorted order every project payload uses.
type LockedID struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// PackLockVerification is one packs verify report: every difference between a
// project's current documents and the reviewed set its lock declares.
//
// Summary counts the documents the configuration declares, and nothing else, so
// Passed plus Failed is always Total. Two findings are therefore outside it and
// each is reported in its own place: the configuration's own drift, which is a
// configuration-level check exactly as it is in PackValidation, and an entry the
// reviewed set names that the configuration no longer declares, which is counted
// by StaleEntries because it is not one of the documents Total is about. Every
// finding of every kind is in Findings regardless.
type PackLockVerification struct {
	OutputVersion string        `json:"outputVersion"`
	Tool          Tool          `json:"tool"`
	Command       string        `json:"command"`
	Status        string        `json:"status"`
	Kind          string        `json:"kind"`
	ConfigPath    string        `json:"configPath"`
	LockPath      string        `json:"lockPath"`
	LockVersion   string        `json:"lockVersion"`
	Summary       PackCounts    `json:"summary"`
	StaleEntries  int           `json:"staleEntries"`
	Checks        []PackCheck   `json:"checks,omitempty"`
	Findings      []LockFinding `json:"findings"`
}

// LockFinding is one named difference between a document and the reviewed set.
// Kind is "pack", "graph", or absent for the configuration itself: a pack and a
// graph may share an id, so the pair is the finding's identity.
type LockFinding struct {
	Name   string `json:"name"`
	Kind   string `json:"kind,omitempty"`
	ID     string `json:"id,omitempty"`
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// PackTestEntry is one pack's matrix run. Rows reuse the corpus row type,
// because a project matrix row is compared exactly as a corpus row is: the RFC
// 8785 canonical disposition byte for byte, or the expected §8.4 error class and
// phase.
//
// Coverage is present when the matrix loaded as rows, the evaluator admits
// the pack — one the preflight refuses never reaches §8, so no derivation
// describes it — and the pack's declarations derive at least one probe.
// Mismatched rows are included, because it reads what the rows document —
// their expectations, and their authored facts — which exist whether or not
// they held. It never moves Status — a missing probe is a fact about what the
// rows state, not a failed row (ADR-0014, ADR-0023).
// An additive member, so outputVersion stays "2" by the same two
// VERSIONING.md rules ADR-0012 cites.
// Origins counts the rows by the origin each declares, so a suite whose rows
// were mostly machine-supplied inputs says so where a reviewer and a CI log
// both read it (ADR-0024). It counts and never gates: an origin moves no
// status, no summary, and no exit code, because the member that decides
// anything about a row is its expectation and an expectation is always
// authored. Gating on it would be worse than useless — the marker is deletable
// in one edit, and a gate would teach exactly that deletion, destroying the one
// signal that measures how much of a suite a generator supplied. An additive
// member, so outputVersion stays "2" by the same two VERSIONING.md rules
// ADR-0012 cites.
type PackTestEntry struct {
	ID          string                 `json:"id"`
	PackID      string                 `json:"packId"`
	PackVersion string                 `json:"packVersion"`
	Path        string                 `json:"path"`
	MatrixPath  string                 `json:"matrixPath,omitempty"`
	Status      string                 `json:"status"`
	Summary     SuiteSummary           `json:"summary"`
	Rows        []EvaluationCorpusCase `json:"rows"`
	Coverage    []MatrixProbe          `json:"coverage,omitempty"`
	Origins     []OriginCount          `json:"origins,omitempty"`
	Detail      string                 `json:"detail,omitempty"`
}

// OriginCount is how many of one pack's matrix rows declare one origin, in
// sorted order of the origin. Only rows that declare one are counted, so a
// suite in which nothing declares an origin carries no member rather than a
// zero: the absence of the marker is not a claim that every row was hand
// written, and a count of "" would read like one.
type OriginCount struct {
	Origin string `json:"origin"`
	Rows   int    `json:"rows"`
}

// PackTest is one packs test run. It carries Experimental,
// ConformanceClaimReference, and EvaluatorSpecVersion like every other payload
// the evaluator produces, because the evaluator that produced its rows is that
// surface — the applied contract version stays in band (ADR-0011) even for a
// run whose every pack was skipped and carries no row to infer it from. An
// additive member, so outputVersion stays "2" by the same two VERSIONING.md
// rules ADR-0012 cites.
type PackTest struct {
	OutputVersion             string          `json:"outputVersion"`
	Tool                      Tool            `json:"tool"`
	Command                   string          `json:"command"`
	Status                    string          `json:"status"`
	Experimental              bool            `json:"experimental"`
	EvaluatorSpecVersion      string          `json:"evaluatorSpecVersion"`
	ConformanceClaimReference string          `json:"conformanceClaimReference"`
	Label                     string          `json:"label"`
	Kind                      string          `json:"kind"`
	ConfigPath                string          `json:"configPath"`
	ConfigVersion             string          `json:"configVersion"`
	Summary                   SuiteSummary    `json:"summary"`
	Packs                     []PackTestEntry `json:"packs"`
}

// PackSuggestionLabel labels every packs suggest report with what the run
// produced and what it did not. It is the corpus label's discipline applied to
// a generator: the one thing a reader must not take from a candidate document
// is that anything in it has been decided.
const PackSuggestionLabel = "candidate test-row inputs derived from a pack's own literals: facts documents with no expectation, which are not rows and are not tests until a human writes the expectation from the policy text"

// SuggestionCounts summarizes one packs suggest run. Total counts the packs
// selected; Candidates counts the candidate inputs derived across all of them,
// which is the number a reviewer is about to read.
type SuggestionCounts struct {
	Total      int `json:"total"`
	Suggested  int `json:"suggested"`
	Skipped    int `json:"skipped"`
	Candidates int `json:"candidates"`
}

// SuggestionSkip is one derivation this run did not perform, and why. A
// dimension the generator cannot address is reported rather than silently
// omitted, on ADR-0022's skipped-not-passed precedent: a candidate set that
// quietly left out a pack's collection quantifiers would read as the whole
// derivable set when it is not.
type SuggestionSkip struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

// PackSuggestionEntry is one pack's derivation: how many candidate inputs it
// produced and every dimension it did not. Status is "suggested" when the pack
// derived at least one candidate and "skipped" when it derived none — never
// "passed", because nothing here was checked.
//
// Unreadable separates the two ways of deriving nothing, which are different
// facts and must not be reported as one. A pack this runtime read and found no
// derivable comparison in states no such comparison; a pack it could not read
// states nothing knowable at all, and a report that said "no comparison" of it
// would be asserting something about a document nobody parsed.
type PackSuggestionEntry struct {
	ID          string           `json:"id"`
	PackID      string           `json:"packId"`
	PackVersion string           `json:"packVersion"`
	Path        string           `json:"path"`
	Status      string           `json:"status"`
	Unreadable  bool             `json:"unreadable,omitempty"`
	Candidates  int              `json:"candidates"`
	Skipped     []SuggestionSkip `json:"skipped,omitempty"`
	Detail      string           `json:"detail,omitempty"`
}

// PackSuggestion is one packs suggest report (ADR-0024): the report *about* a
// derivation, which is a different artifact from the candidate document the
// derivation emits. The document goes to --write or to stdout and carries
// candidatesVersion and candidates; this payload carries the counts, the pack
// identities the candidates came from, and every skipped dimension. Keeping
// them apart is what lets the document stay inputs-only — a provenance member
// inside it would be a member of a file whose whole purpose is to be edited
// into a matrix — while a reader still learns which pack version a run read.
//
// It is its own payload rather than a member of an existing one, on the
// reasoning PackLock and PackProducersLint record: a surface this new should be
// removable without taking a member out of a payload consumers already read.
type PackSuggestion struct {
	OutputVersion     string                `json:"outputVersion"`
	Tool              Tool                  `json:"tool"`
	Command           string                `json:"command"`
	Status            string                `json:"status"`
	Kind              string                `json:"kind"`
	Label             string                `json:"label"`
	ConfigPath        string                `json:"configPath"`
	ConfigVersion     string                `json:"configVersion"`
	CandidatesVersion string                `json:"candidatesVersion"`
	Summary           SuggestionCounts      `json:"summary"`
	Packs             []PackSuggestionEntry `json:"packs"`
	WrittenTo         string                `json:"writtenTo,omitempty"`
}

// ConfigSchema describes the exact embedded jpack.json schema bytes, on the same
// shape spec schema reports for a bundled JPS schema. It names no specification
// version, because the configuration format is this runtime's and has a version
// of its own. ConfigVersion is the newest shape the schema describes;
// SupportedConfigVersions lists every one the runtime reads, so this payload
// cannot imply that an older shape stopped being read. The list is an added
// member, so outputVersion is unchanged under VERSIONING.md's machine-output
// rules.
type ConfigSchema struct {
	OutputVersion           string   `json:"outputVersion"`
	Tool                    Tool     `json:"tool"`
	Command                 string   `json:"command"`
	Status                  string   `json:"status"`
	Kind                    string   `json:"kind"`
	ConfigVersion           string   `json:"configVersion"`
	SupportedConfigVersions []string `json:"supportedConfigVersions"`
	SchemaID                string   `json:"schemaId"`
	Bytes                   int      `json:"bytes"`
	SHA256                  string   `json:"sha256"`
	WrittenTo               string   `json:"writtenTo,omitempty"`
}

type OperationalError struct {
	OutputVersion string `json:"outputVersion"`
	Tool          Tool   `json:"tool"`
	Command       string `json:"command"`
	Status        string `json:"status"`
	// EvaluationError is present exactly when the failure is an evaluation error
	// of JPS Core §8.4, naming its class and phase. Its absence means the failure
	// is an ordinary operational refusal — a bad invocation, an unreadable input,
	// an internal fault — which §8.4 does not classify.
	EvaluationError *EvaluationError `json:"evaluationError,omitempty"`
	Diagnostics     []Diagnostic     `json:"diagnostics"`
}

func NewOperationalError(command, code, message string) OperationalError {
	return NewOperationalResult(command, "error", code, message)
}

// NewEvaluationError reports one JPS Core §8.4 evaluation error: the class and
// phase in band, and this runtime's finer diagnostic code beside them. No
// disposition accompanies it, ever.
func NewEvaluationError(command, status, class, phase, code, message string) OperationalError {
	output := NewOperationalResult(command, status, code, message)
	output.EvaluationError = &EvaluationError{Class: class, Phase: phase, EvaluatorSpecVersion: EvaluatorSpecVersion}
	return output
}

func NewOperationalResult(command, status, code, message string) OperationalError {
	return OperationalError{
		OutputVersion: OutputVersion,
		Tool:          CurrentTool(),
		Command:       command,
		Status:        status,
		Diagnostics: []Diagnostic{
			ErrorDiagnostic(code, "operation", "", message),
		},
	}
}
