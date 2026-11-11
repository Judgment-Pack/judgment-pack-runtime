package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"slices"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/describe"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// The four JPS Core §8.4 error classes a Refusal can carry, and the two
// phases. A refusal §8.4 does not classify carries an empty Class.
const (
	ClassPackNotConformant            = result.ClassPackNotConformant
	ClassMalformedInput               = result.ClassMalformedInput
	ClassUnsupportedRequiredExtension = result.ClassUnsupportedRequiredExtension
	ClassResourceExhaustion           = result.ClassResourceExhaustion

	PhasePreflight  = result.PhasePreflight
	PhaseEvaluation = result.PhaseEvaluation
)

// command names this surface in the JSON envelope, where the CLI writes
// "experimental evaluate" and the MCP server "mcp experimental_evaluate".
const command = "sdk evaluate"

// modulePath is this module's import path, which identifies it in a program's
// build information.
const modulePath = "github.com/Judgment-Pack/judgment-pack-runtime"

// sharedValidator compiles the bundled schemas once per process. Every Pack
// shares it; it is safe for concurrent use.
var sharedValidator = sync.OnceValues(validation.NewEngine)

// errNilContext is returned for a nil context instead of a panic.
var errNilContext = errors.New("sdk: nil context")

// ErrInternal is wrapped by every error that is a fault of this runtime
// rather than a refusal of the caller's input: bundled specification
// artifacts that fail their integrity check, or an evaluator result this
// package cannot read. Test for it with errors.Is. Such an error is never a
// *Refusal.
var ErrInternal = errors.New("sdk: internal fault")

// internalFault wraps ErrInternal with the runtime's code and message.
func internalFault(code, message string) error {
	return fmt.Errorf("%w: %s: %s", ErrInternal, code, display.Sanitize(message))
}

// LoadOptions are the caller's capabilities for one loaded pack.
type LoadOptions struct {
	// SupportedExtensions is the set of extension capability names the caller
	// supports, compared with the pack's metadata.requiredExtensions. Order and
	// duplicates do not matter. It is fixed at Load because admitting the pack
	// depends on it; the unsupported-required-extension refusal it can cause is
	// still reported by Evaluate, after the facts and evidence documents, where
	// JPS Core §8.2 places it.
	SupportedExtensions []string
}

// Pack is one pack admitted by Load. It is immutable and safe for concurrent
// use; Evaluate never validates the pack again.
type Pack struct {
	engine   *evaluation.Engine
	admitted *evaluation.AdmittedPack
	contract result.EvaluatorContract
	// supported is Load's own copy of LoadOptions.SupportedExtensions.
	supported []string
}

// Load validates pack to full document conformance and admits it for
// evaluation, without a project directory, a file, the environment or the
// network. pack is copied; the caller may reuse its slice afterwards.
//
// The evaluator contract is the one the pack declares (specVersion
// "0.2.0-draft", or "0.3.0" with the base profile), selected exactly as
// "jpack experimental evaluate" selects it outside a project.
//
// A pack the evaluator would refuse before reading the facts document is
// refused here with a *Refusal: the same class, phase, code and message the
// CLI reports for it, plus the diagnostics "jpack spec validate" reports for
// the same bytes when the pack failed validation. A pack that requires an
// extension outside options.SupportedExtensions is not refused here; Evaluate
// refuses it.
//
// ctx is checked on entry and again before validation starts. A validation
// that has started runs to completion, bounded by the documented limits; it
// is not interrupted. A done context is reported as ctx.Err(), never as a
// Refusal.
func Load(ctx context.Context, pack []byte, options LoadOptions) (*Pack, error) {
	if ctx == nil {
		return nil, errNilContext
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	validator, err := sharedValidator()
	if err != nil {
		return nil, integrityFault()
	}
	// The contract selection of the CLI and the MCP server: a pack whose
	// specVersion is missing or not an evaluator contract is evaluated under
	// the legacy contract, whose own version gate then refuses it.
	contract := result.LegacyEvaluatorContract()
	selected, failure := evaluation.SelectContract(pack)
	if failure != nil && failure.Code != "JPS-EVALUATION-PACK-SPEC-VERSION" {
		return nil, packError(validator, pack, failure)
	}
	if failure == nil {
		contract = selected
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	loaded := &Pack{contract: contract, supported: slices.Clone(options.SupportedExtensions)}
	loaded.engine = evaluation.NewEngine(validator)
	loaded.admitted = loaded.engine.AdmitPack(pack)
	// The pack half of the §8.2 preflight runs here, once, through the
	// engine's own steps, and the admission is memoized under the options
	// Evaluate uses, so Evaluate replays it rather than validating again.
	if failure := loaded.admitted.PackFailure(loaded.options(false)); failure != nil {
		return nil, packError(validator, pack, failure)
	}
	return loaded, nil
}

// options are the evaluation options of "jpack experimental evaluate" run
// outside a project, with this surface's command name.
func (p *Pack) options(evidenceSupplied bool) evaluation.Options {
	return evaluation.Options{
		SpecVersion:         p.contract.Version(),
		Command:             command,
		SupportedExtensions: p.supported,
		EvidenceSupplied:    evidenceSupplied,
	}
}

// Evaluate applies the JPS Core §§7–8 evaluation to one facts document and one
// evidence-availability document, under the contract and capabilities fixed
// at Load. Both are JSON texts held to the same carrier rules and byte limit
// as the pack.
//
// A nil evidence slice means the document was omitted, which §8.2 treats as
// the empty object. A non-nil slice is a supplied document, so an empty one
// is refused as malformed-input. facts is required.
//
// A refusal is a *Refusal carrying the §8.4 class and phase, with the code
// and message the CLI reports. The §10 limits are the CLI's: the 10 MiB byte
// limit per input, the 250,000-member collection limit, and the default
// evaluation-work limit.
//
// ctx is checked on entry only. An evaluation that has started runs to
// completion, bounded by those limits; it is not interrupted. A done context
// is reported as ctx.Err(), never as a Refusal.
func (p *Pack) Evaluate(ctx context.Context, facts, evidence []byte) (*Evaluation, error) {
	if ctx == nil {
		return nil, errNilContext
	}
	if p == nil || p.admitted == nil {
		return nil, errors.New("sdk: Evaluate on a Pack that Load did not return")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	evaluated, failure := p.engine.EvaluateAdmitted(p.admitted, facts, evidence, p.options(evidence != nil))
	if failure != nil {
		return nil, failureError(failure)
	}
	return newEvaluation(evaluated)
}

// Evaluation is one evaluated result. Its methods are for a value returned by
// a successful Evaluate; they are not defined on a nil or zero Evaluation.
type Evaluation struct {
	// SpecVersion is the specVersion the evaluated pack declares.
	SpecVersion string
	// EvaluatorSpecVersion is the JPS Core evaluator contract applied.
	EvaluatorSpecVersion string
	PackID               string
	PackVersion          string
	// Disposition is the §8.3 portable disposition, decoded from the bytes
	// CanonicalDisposition returns.
	Disposition Disposition
	// HandoffTarget is the pack's escalation target when the disposition
	// requests a handoff to it, and nil otherwise.
	HandoffTarget *Target

	canonical []byte
	envelope  result.Evaluation
}

// CanonicalDisposition returns the §8.3 portable disposition in its RFC 8785
// canonical form: the bytes two conforming evaluators agree on. The caller
// owns the returned slice.
func (e *Evaluation) CanonicalDisposition() []byte {
	return bytes.Clone(e.canonical)
}

// JSON returns the evaluation in the JSON envelope
// "jpack experimental evaluate --format json" writes outside a project, with
// a trailing newline. The one difference is the command member, which is
// "sdk evaluate". The envelope's shape follows the CLI's output contract, not
// this package's. Its tool.version is the version compiled into this module,
// which is "0.0.0-dev" unless the program set it at link time; it is not
// Description.RuntimeVersion.
func (e *Evaluation) JSON() ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(e.envelope); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// Disposition is the JPS Core §8.3 portable disposition.
type Disposition struct {
	// Kind is "outcome", "not-applicable" or "unresolved".
	Kind string
	// OutcomeID is the outcome's id when Kind is "outcome", and empty otherwise.
	OutcomeID string
	// Reasons is the retained reason set, sorted and duplicate-free; empty
	// exactly when Kind is "outcome".
	Reasons []string
	Handoff Handoff
}

// Handoff is the disposition's handoff object.
type Handoff struct {
	// State is "none" or "requested".
	State string
	// TriggeredBy is present exactly when State is "requested", and nil
	// otherwise.
	TriggeredBy []string
	// Routes is nil when the pack declares no escalation routes, and non-nil
	// (possibly empty) for a Core 0.3.0 pack that declares them, in the
	// pack's order.
	Routes []Target
}

// Target is one handoff destination.
type Target struct {
	Kind string
	Name string
}

// canonicalShape decodes the canonical disposition bytes, so the typed view
// and the bytes cannot disagree.
type canonicalShape struct {
	Kind      string   `json:"kind"`
	OutcomeID string   `json:"outcomeId"`
	Reasons   []string `json:"reasons"`
	Handoff   struct {
		State       string   `json:"state"`
		TriggeredBy []string `json:"triggeredBy"`
		Routes      []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"routes"`
	} `json:"handoff"`
}

func newEvaluation(evaluated result.Evaluation) (*Evaluation, error) {
	canonical, err := evaluated.Disposition.Canonical()
	if err != nil {
		return nil, internalFault("JPS-EVALUATION-INTERNAL", "The evaluator produced a disposition that does not canonicalize.")
	}
	var shape canonicalShape
	if err := json.Unmarshal(canonical, &shape); err != nil {
		return nil, internalFault("JPS-EVALUATION-INTERNAL", "The canonical disposition could not be decoded.")
	}
	disposition := Disposition{
		Kind:      shape.Kind,
		OutcomeID: shape.OutcomeID,
		Reasons:   shape.Reasons,
		Handoff:   Handoff{State: shape.Handoff.State, TriggeredBy: shape.Handoff.TriggeredBy},
	}
	if shape.Handoff.Routes != nil {
		disposition.Handoff.Routes = make([]Target, 0, len(shape.Handoff.Routes))
		for _, route := range shape.Handoff.Routes {
			disposition.Handoff.Routes = append(disposition.Handoff.Routes, Target{Kind: route.Kind, Name: route.Name})
		}
	}
	output := &Evaluation{
		SpecVersion:          evaluated.SpecVersion,
		EvaluatorSpecVersion: evaluated.EvaluatorSpecVersion,
		PackID:               evaluated.PackID,
		PackVersion:          evaluated.PackVersion,
		Disposition:          disposition,
		canonical:            canonical,
		envelope:             evaluated,
	}
	if evaluated.HandoffTarget != nil {
		output.HandoffTarget = &Target{Kind: evaluated.HandoffTarget.Kind, Name: evaluated.HandoffTarget.Name}
	}
	return output, nil
}

// Refusal is an input the evaluator refused. It is the error Load and
// Evaluate return for every refusal of the caller's input, and only for that:
// a fault of this runtime wraps ErrInternal, a done context is ctx.Err(), and
// a misuse of this package (a nil context, a Pack Load did not return) is a
// plain error.
type Refusal struct {
	// Class is the JPS Core §8.4 error class, one of the Class constants. It
	// is empty for exactly one refusal: Code "profile-unsupported", a Core
	// 0.3.0 pack declaring a profile this evaluator does not implement, which
	// is refused before §8.4 applies and which the CLI reports with no class
	// either.
	Class string
	// Phase is PhasePreflight or PhaseEvaluation when the refusal has a phase.
	Phase string
	// Code is this runtime's diagnostic code, as the CLI reports it.
	Code string
	// Message is the CLI's message. It may quote pack content; Error
	// sanitizes it for display and this field does not.
	Message string
	// Profile names the Core profile refused when Code is
	// "profile-unsupported".
	Profile string
	// Diagnostics is the report "jpack spec validate" gives for the pack's
	// bytes, when Load refused a pack that failed validation; nil otherwise.
	Diagnostics []Diagnostic
	// DiagnosticsTruncated is the validator's own truncation flag for
	// Diagnostics.
	DiagnosticsTruncated bool
}

// Error formats the refusal on one line, with terminal controls replaced.
func (r *Refusal) Error() string {
	text := r.Code
	if r.Class != "" {
		text = r.Class + ": " + text
	}
	if r.Profile != "" {
		text += " (" + r.Profile + ")"
	}
	if r.Message != "" {
		text += ": " + r.Message
	}
	return "sdk: " + display.Sanitize(text)
}

// Diagnostic is one validation diagnostic, as "jpack spec validate" reports it.
type Diagnostic struct {
	Code         string
	Layer        string
	Severity     string
	InstancePath string
	Message      string
	Profile      string
}

// profileUnsupported is the one refusal of the caller's input that carries
// no §8.4 class.
const profileUnsupported = "profile-unsupported"

// failureError is the error for one engine failure: a *Refusal for a
// refusal of the caller's input, which the engine marks with a §8.4 class or
// as profile-unsupported, and an ErrInternal fault for any other failure.
func failureError(failure *evaluation.Failure) error {
	if failure.Class == "" && failure.Code != profileUnsupported {
		return internalFault(failure.Code, failure.Message)
	}
	return &Refusal{
		Class:   failure.Class,
		Phase:   failure.Phase,
		Code:    failure.Code,
		Message: failure.Message,
		Profile: failure.Profile,
	}
}

// packError is a Load error. A refusal carries the validation report of the
// same bytes when there is one to carry: not for a pack past the byte limit,
// which the validator refuses unread, and not when validation itself fails
// operationally.
func packError(validator *validation.Engine, pack []byte, failure *evaluation.Failure) error {
	err := failureError(failure)
	refused, ok := err.(*Refusal)
	if !ok || int64(len(pack)) > carrier.HardMaxBytes {
		return err
	}
	report, operational := validator.Validate(pack, validation.Options{Through: "semantic", Limits: carrier.DefaultLimits()})
	if operational != nil || report.Status == "valid" {
		return err
	}
	refused.Diagnostics = make([]Diagnostic, 0, len(report.Diagnostics))
	for _, diagnostic := range report.Diagnostics {
		refused.Diagnostics = append(refused.Diagnostics, Diagnostic{
			Code:         diagnostic.Code,
			Layer:        diagnostic.Layer,
			Severity:     diagnostic.Severity,
			InstancePath: diagnostic.InstancePath,
			Message:      diagnostic.Message,
			Profile:      diagnostic.Profile,
		})
	}
	refused.DiagnosticsTruncated = report.DiagnosticsTruncated
	return refused
}

func integrityFault() error {
	return internalFault("JPS-ARTIFACT-INTEGRITY", "Bundled JPS artifacts failed their integrity check.")
}

// Description says what this runtime evaluates.
type Description struct {
	// RuntimeVersion is this module's version as the program's build
	// information records it: the required version, or a replacement's
	// version when the requirement is replaced by another module version. It
	// is empty when the build records none: a build inside this module's own
	// source tree, a replacement by a local directory, or a build without
	// module information. It is not the tool.version member of
	// Evaluation.JSON, which is the version compiled into the module.
	RuntimeVersion string
	// SpecVersions are the JPS specification versions bundled for validation.
	SpecVersions []string
	// Evaluators are the evaluator contracts Load can select.
	Evaluators []Evaluator
	// ConformanceClaimReference names the file in this module that states the
	// runtime's conformance claim, in full and only there.
	ConformanceClaimReference string
}

// Evaluator is one evaluator contract.
type Evaluator struct {
	// SpecVersion is the JPS Core version of the contract.
	SpecVersion string
	// Profiles are the Core profiles supported under it, if it defines any.
	Profiles []string
	// CorpusSuiteVersion is the suiteVersion of the evaluation corpus bundled
	// for SpecVersion, as "jpack experimental evaluate-corpus" reports it.
	CorpusSuiteVersion string
}

// Describe reports the bundled specification versions and evaluator
// contracts, from the same source as "jpack describe", and the version of the
// evaluation corpus bundled for each contract.
func Describe() (Description, error) {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		return Description{}, integrityFault()
	}
	runtime := describe.Runtime(set, "sdk describe")
	output := Description{
		RuntimeVersion:            moduleVersion(),
		SpecVersions:              slices.Clone(runtime.SupportedSpecs),
		ConformanceClaimReference: result.EvaluationClaimReference,
	}
	for _, capability := range runtime.EvaluatorCapabilities {
		suite, failure := evaluation.CorpusSuiteVersion(capability.SpecVersion)
		if failure != nil {
			// The bundle is fixed at build time, so a corpus it lacks is a fault.
			return Description{}, internalFault(failure.Code, failure.Message)
		}
		output.Evaluators = append(output.Evaluators, Evaluator{
			SpecVersion:        capability.SpecVersion,
			Profiles:           slices.Clone(capability.SupportedProfiles),
			CorpusSuiteVersion: suite,
		})
	}
	return output, nil
}

// moduleVersion reads this module's version from the program's build
// information, which is in the binary and needs no I/O.
func moduleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	if info.Main.Path == modulePath {
		if info.Main.Version == "(devel)" {
			return ""
		}
		return info.Main.Version
	}
	for _, dependency := range info.Deps {
		if dependency.Path == modulePath {
			if dependency.Replace != nil {
				return dependency.Replace.Version
			}
			return dependency.Version
		}
	}
	return ""
}
