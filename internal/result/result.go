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
	CLIName       = "judgment-pack"
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

// TraceEntry records one exception or rule evaluation. The trace is
// informative: §8 requires an unknown that resolution ignored to remain
// visible, and permits recording contributing ids.
type TraceEntry struct {
	Stage      string `json:"stage"`
	ID         string `json:"id"`
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
type Evaluation struct {
	OutputVersion             string          `json:"outputVersion"`
	Tool                      Tool            `json:"tool"`
	Command                   string          `json:"command"`
	Status                    string          `json:"status"`
	Experimental              bool            `json:"experimental"`
	ConformanceClaimReference string          `json:"conformanceClaimReference"`
	SpecVersion               string          `json:"specVersion"`
	EvaluatorSpecVersion      string          `json:"evaluatorSpecVersion"`
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
type EvaluationCorpusCase struct {
	ID                 string `json:"id"`
	Origin             string `json:"origin"`
	SpecSection        string `json:"specSection"`
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
