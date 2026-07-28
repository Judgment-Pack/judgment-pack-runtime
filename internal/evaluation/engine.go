// Package evaluation implements the JPS Core §§7–10 evaluator contract behind an
// explicitly experimental surface (ADR-0007).
//
// It implements the evaluator class Core 0.2.0-draft defines: the input preflight
// of §8.2, the portable disposition of §8.3 with its RFC 8785 byte-agreement, the
// four error classes and fixed precedence of §8.4, and the §10 limits of limits.go.
// The conformance claim for this evaluator is stated, in full and only, in
// CONFORMANCE.md (ADR-0011); nothing in this package restates it, and every payload
// carries a reference to that file rather than a claim of its own
// (result.EvaluationClaimReference). The surface may still change or be removed
// without compatibility promise, which is what "experimental" names here.
//
// Only a pack declaring specVersion 0.2.0-draft is evaluated. §11 makes the
// declared value exact and says an unedited 0.1.0-draft pack "must be re-declared
// before an implementation claiming this draft evaluates it", so a pack declaring
// any other version is refused as pack-not-conformant in the preflight phase
// (declaredSpecVersion) rather than evaluated under a contract it has not opted
// into. Every payload still names the contract's own version
// (result.EvaluatorSpecVersion) beside the pack's, because they are two facts and
// a consumer should read the applied contract rather than infer it.
//
// Evaluation is a pure function of its inputs: a conformant pack, one facts
// document, the tri-state availability of declared evidence, and the
// supported-extension set. Errors are not dispositions: a non-conformant pack, a
// malformed input, an unsupported required extension, or an exhausted documented
// limit terminates the evaluation with no disposition at all (§8.4).
//
// The limits this evaluator enforces divide by §10's phase split. Admission is
// bounded by the carrier limits — 10 MiB per input document, 250,000 parsed
// nodes, depth 128, 1 MiB strings — and reaching one of those refuses the input
// rather than processing part of it, which is malformed-input in the preflight
// phase. The evaluation phase has the two limits §10 requires of the class,
// stated and derived in limits.go: an evaluation-work limit charged over every
// condition node, §8 iteration, pointer resolution, and compared byte
// (DefaultCoreWorkLimit, configurable per evaluation), and a collection-size
// limit that is the carrier's parsed-node cap because every collection this path
// traverses comes from a document admitted under it (CoreCollectionSizeLimit).
// Reaching the work limit is resource-exhaustion, the one class of the evaluation
// phase, and it is reachable on the ordinary Core path and not only under the
// draft RFC 0008 opt-in, which has its own smaller budget (ADR-0009).
package evaluation

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// Failure reports why an evaluation could not run. It is an operational
// refusal, never a disposition.
//
// Class and Phase are the JPS Core §8.4 identity of the failure when it is an
// evaluation error: one of the four Core classes, and the phase it was reached
// in. Code stays this runtime's finer-grained diagnostic identifier, which §8.4
// admits as message detail rather than as the class. A failure that §8.4 does not
// classify at all — a bad invocation, an internal fault — carries no class.
type Failure struct {
	Class    string
	Phase    string
	Code     string
	Message  string
	ExitCode int
}

// Options carries one evaluation's caller opt-ins. The zero value is the
// published surface: Core conditions only, no draft grammar, no work budget.
type Options struct {
	// Command names the reporting surface, exactly as in the describe package.
	Command string
	// SupportedExtensions is the consumer's extension capability set.
	SupportedExtensions []string
	// RFC0008Quantifiers admits the draft RFC 0008 aggregates (exists, every,
	// uniform). A pack using one is not valid under any published JPS version,
	// so every evaluation made under this opt-in is labeled a draft-RFC
	// prototype in its output.
	RFC0008Quantifiers bool
	// WorkBudget overrides this evaluation's §10 evaluation-work limit. Zero or
	// negative selects the default for the path in force: DefaultCoreWorkLimit on
	// the Core path, and the draft grammar's smaller DefaultWorkBudget under the
	// opt-in. A caller that configures a lower limit has configured its own limit,
	// and an input above it is refused with the resource-exhaustion error of §8.4
	// rather than evaluated to a disposition.
	WorkBudget int
	// EvidenceSupplied says the caller supplied an evidence-availability document,
	// which is what tells an empty document apart from an omitted one. §8.2 makes an
	// omitted document the implicit empty object and not an error; empty bytes are
	// not a carrier-conforming JSON text and are the malformed-input error. A caller
	// whose surface has no way to supply an empty document leaves this false.
	EvidenceSupplied bool
	// OversizedInputs names the inputs — "pack", "facts", or "evidence" — the
	// caller could not present in full because they exceed this runtime's
	// documented byte limit. A caller reports the condition here instead of
	// refusing it itself, so the byte limit is reached at that input's own place
	// in the §8.2 preflight and §8.4 assigns the class and the order: the pack's
	// limit before the facts document's, and a non-conformant pack before either.
	OversizedInputs []string
}

// oversized reports whether the caller marked one named input as above this
// runtime's byte limit before it could be presented in full.
func (o Options) oversized(name string) bool {
	return slices.Contains(o.OversizedInputs, name)
}

// workLimit is the §10 evaluation-work limit in force for this evaluation: the
// caller's, or the documented default of the path it selected.
func (o Options) workLimit() int {
	if o.WorkBudget > 0 {
		return o.WorkBudget
	}
	if o.RFC0008Quantifiers {
		return DefaultWorkBudget
	}
	return DefaultCoreWorkLimit
}

// Engine evaluates conformant packs. It wraps the validation engine so a pack
// is always checked to full document conformance before a single condition is
// interpreted.
type Engine struct {
	validator *validation.Engine
}

func NewEngine(validator *validation.Engine) *Engine {
	return &Engine{validator: validator}
}

// Evaluate validates the pack (with the caller's supported extensions), then
// applies the §§7–8 experiment to the facts and evidence inputs. command names
// the reporting surface, exactly as in the describe package.
func (e *Engine) Evaluate(pack, facts, evidence []byte, supported []string, command string) (result.Evaluation, *Failure) {
	return e.EvaluateWith(pack, facts, evidence, Options{Command: command, SupportedExtensions: supported})
}

// EvaluateWith is Evaluate with the caller's opt-ins stated explicitly.
//
// The §8.2 input preflight runs first and completely: the pack, then the facts
// document, then the evidence-availability document, then the pack's
// metadata.requiredExtensions against this caller's supported set. §8.4 makes
// that order the error precedence, so the first failure here is the class §8.4
// requires be reported, and no result can outrace an input error — a pack whose
// applicability is false, presented with an evidence document carrying an
// undeclared key, is the malformed-input error and never the not-applicable
// disposition.
func (e *Engine) EvaluateWith(pack, facts, evidence []byte, options Options) (result.Evaluation, *Failure) {
	// One byte-limit boundary for every surface: the MCP wire must refuse the
	// same oversized input the CLI's bounded reads refuse, and a CLI read that
	// stopped at the limit reports the condition through Options rather than
	// refusing it ahead of this preflight. Each input is checked at its own place
	// in the preflight order, so an oversized pack is reported as the pack's
	// failure even when the facts document is oversized too, and a non-conformant
	// pack outranks an oversized facts document (§8.4).
	if failure := byteLimit("pack", pack, result.ClassPackNotConformant, options.oversized("pack")); failure != nil {
		return result.Evaluation{}, failure
	}
	validated, packRoot, unsupportedExtensions, failure := e.conformance(pack, options)
	if failure != nil {
		return result.Evaluation{}, failure
	}
	// The version gate is part of admitting the pack, so it precedes the facts
	// document and every later step, and its class is the pack's own:
	// pack-not-conformant, which §8.4 orders first in any case.
	if failure := declaredSpecVersion(validated.SpecVersion); failure != nil {
		return result.Evaluation{}, failure
	}

	if failure := byteLimit("facts", facts, result.ClassMalformedInput, options.oversized("facts")); failure != nil {
		return result.Evaluation{}, failure
	}
	factsDocument, failure := decodeInput(facts, "facts", result.ClassMalformedInput)
	if failure != nil {
		return result.Evaluation{}, failure
	}

	if failure := byteLimit("evidence", evidence, result.ClassMalformedInput, options.oversized("evidence")); failure != nil {
		return result.Evaluation{}, failure
	}
	presence, failure := decodeEvidence(evidence, packRoot, options.EvidenceSupplied)
	if failure != nil {
		return result.Evaluation{}, failure
	}

	// The fourth and last preflight step (§8.2), reported after the two
	// documents are admitted so that §8.4's precedence holds: a malformed input
	// outranks an unsupported required extension on the same inputs.
	if unsupportedExtensions != nil {
		return result.Evaluation{}, unsupportedExtensions
	}

	disposition, target, trace, failure := resolve(packRoot, factsDocument, &evaluator{
		evidence:    presence,
		quantifiers: options.RFC0008Quantifiers,
		budget:      options.workLimit(),
	})
	if failure != nil {
		return result.Evaluation{}, failure
	}
	if trace == nil {
		trace = []result.TraceEntry{}
	}
	evaluation := result.Evaluation{
		OutputVersion:             result.OutputVersion,
		Tool:                      result.CurrentTool(),
		Command:                   options.Command,
		Status:                    "evaluated",
		Experimental:              true,
		ConformanceClaimReference: result.EvaluationClaimReference,
		SpecVersion:               validated.SpecVersion,
		EvaluatorSpecVersion:      result.EvaluatorSpecVersion,
		Disposition:               disposition,
		HandoffTarget:             target,
		Trace:                     trace,
	}
	if options.RFC0008Quantifiers {
		evaluation.DraftPrototype = draftPrototype(packRoot, validated.SpecVersion)
	}
	if set, err := artifacts.Load(validated.SpecVersion); err == nil {
		evaluation.Artifact = &result.Artifact{
			SpecVersion:  validated.SpecVersion,
			BundleDigest: set.Lock().BundleDigest.Value,
			Provenance:   set.Lock().Source.Kind,
		}
	}
	return evaluation, nil
}

// conformance establishes the pack's conformance status and returns its decoded
// root. Without the draft opt-in this is the published path unchanged: full
// document conformance against the bundled schema, or a refusal. Under the
// opt-in it is the RFC 0008 gate, which no pack reaches by accident.
//
// The third result is the deferred unsupported-required-extension error of
// §8.4, non-nil when the pack is semantically conforming but requires a
// capability this caller does not support. It is returned rather than reported
// because §8.2 compares the extension sets last, after the facts and evidence
// documents are admitted, and §8.4 makes that order the error precedence.
func (e *Engine) conformance(pack []byte, options Options) (result.Validation, map[string]any, *Failure, *Failure) {
	if options.RFC0008Quantifiers {
		return e.rfc0008Conformance(pack, options)
	}
	validated, operational := e.validator.Validate(pack, validation.Options{
		Through:             "semantic",
		SupportedExtensions: options.SupportedExtensions,
		Limits:              carrier.DefaultLimits(),
	})
	if operational != nil {
		return result.Validation{}, nil, nil, packOperationalFailure(operational)
	}
	if validated.Status != "valid" && unsupportedRequiredExtension(validated) == nil {
		return result.Validation{}, nil, nil, packNotConformant(validated)
	}
	document, failure := decodeInput(pack, "pack", result.ClassPackNotConformant)
	if failure != nil {
		return result.Validation{}, nil, nil, failure
	}
	packRoot, ok := document.(map[string]any)
	if !ok {
		return result.Validation{}, nil, nil, &Failure{Code: "JPS-EVALUATION-INTERNAL", Message: "The validated pack did not decode to a JSON object.", ExitCode: result.ExitInternal}
	}
	return validated, packRoot, unsupportedRequiredExtension(validated), nil
}

// packNotConformant refuses a pack that did not reach full document
// conformance, naming the first diagnostic so the refusal is self-sufficient.
// This is §8.4's pack-not-conformant class, whichever layer the pack failed at.
func packNotConformant(validated result.Validation) *Failure {
	exitCode := result.ExitInvalid
	if validated.Status == "unsupported" {
		exitCode = result.ExitUnsupported
	}
	detail := ""
	if len(validated.Diagnostics) > 0 {
		first := validated.Diagnostics[0]
		location := first.InstancePath
		if location == "" {
			location = "<root>"
		}
		detail = fmt.Sprintf(" First diagnostic: %s at %s: %s", first.Code, location, first.Message)
	}
	return &Failure{
		Class:    result.ClassPackNotConformant,
		Phase:    result.PhasePreflight,
		Code:     "JPS-EVALUATION-PACK-NOT-CONFORMANT",
		Message:  fmt.Sprintf("The pack is not evaluated because its document conformance status is %q; evaluation requires a fully conformant pack. Run spec validate for the complete report.%s", validated.Status, detail),
		ExitCode: exitCode,
	}
}

// declaredSpecVersion holds every evaluated pack to the one specVersion this
// evaluator's contract belongs to, which is the version scope of the claim in
// CONFORMANCE.md.
//
// §11 is the requirement, not a preference: "Because the value is exact (§4), an
// unedited 0.1.0-draft pack is not structurally conforming to 0.2.0-draft and must
// be re-declared before an implementation claiming this draft evaluates it." The
// claim in CONFORMANCE.md is made against that draft, so a pack declaring any other
// version has not opted into the §§7–8 semantics this evaluator applies, and
// evaluating it anyway would apply the claimed contract to an input the claim's own
// version scope does not cover. There is no second, unclaimed legacy path: one evaluator, one
// admitted version.
//
// The refusal is the pack's own §8.4 class — pack-not-conformant, in the preflight
// phase, since a document declaring another version is not structurally conforming
// to this one — and the message states the whole remedy, because the remedy really
// is one edit: §11 says 0.2.0-draft "changes no part of the document format", so
// re-declaring changes the specVersion string and nothing else in the document.
func declaredSpecVersion(specVersion string) *Failure {
	if specVersion == result.EvaluatorSpecVersion {
		return nil
	}
	return &Failure{
		Class: result.ClassPackNotConformant,
		Phase: result.PhasePreflight,
		Code:  "JPS-EVALUATION-PACK-SPEC-VERSION",
		Message: fmt.Sprintf(
			"The pack is not evaluated because it declares specVersion %q and this evaluator applies the JPS Core %s evaluator contract, whose conformance claim is stated in CONFORMANCE.md. JPS §11 makes the declared value exact: an unedited %s pack is not structurally conforming to %s and must be re-declared before an implementation claiming this draft evaluates it. Re-declaring is one edit — the specVersion string — and nothing else in the document changes, because §11 says %s changes no part of the document format: every member, every cross-field rule, and every document-conformance verdict of §§3.1-3.3 stays the same. The %s schema remains published, and spec validate still validates the pack as it stands.",
			specVersion, result.EvaluatorSpecVersion, specVersion, result.EvaluatorSpecVersion, result.EvaluatorSpecVersion, specVersion),
		ExitCode: result.ExitInvalid,
	}
}

// unsupportedRequiredExtension recognizes the one non-valid validation verdict
// that is not a defect in the pack: a semantically conforming document (§3.3)
// that requires a capability this caller does not support. §9 makes that report
// the unsupported-required-extension error of §8.4 for the evaluator class — the
// unsupported part may be the part that decides, so no disposition may be
// produced — and it keeps the validator's own capability code as the detail.
// Any other diagnostic in the verdict means the pack itself did not conform, so
// this returns nil and pack-not-conformant is reported instead.
func unsupportedRequiredExtension(validated result.Validation) *Failure {
	if validated.Status != "unsupported" || len(validated.Diagnostics) == 0 {
		return nil
	}
	for _, diagnostic := range validated.Diagnostics {
		if diagnostic.Code != "JPS-CAPABILITY-REQUIRED-EXTENSION" {
			return nil
		}
	}
	names := make([]string, 0, len(validated.Extensions.Unsupported))
	for _, name := range validated.Extensions.Unsupported {
		names = append(names, display.Sanitize(name))
	}
	return &Failure{
		Class: result.ClassUnsupportedRequiredExtension,
		Phase: result.PhasePreflight,
		Code:  "JPS-CAPABILITY-REQUIRED-EXTENSION",
		Message: fmt.Sprintf(
			"The pack is not evaluated because it requires extension capabilities this evaluation does not support: %s. No disposition is produced; the unsupported part may be the part that decides.",
			strings.Join(names, ", ")),
		ExitCode: result.ExitUnsupported,
	}
}

// packOperationalFailure maps a validator operational failure on the pack input.
// A documented carrier limit reached while admitting the pack means the document
// was refused rather than partly processed (§2.1), so it never became a
// conforming pack: pack-not-conformant, which §8.4 orders first in any case.
func packOperationalFailure(operational *validation.OperationalFailure) *Failure {
	return &Failure{
		Class:    result.ClassPackNotConformant,
		Phase:    result.PhasePreflight,
		Code:     operational.Code,
		Message:  operational.Message,
		ExitCode: operational.ExitCode,
	}
}

// byteLimit refuses one input above this runtime's documented byte limit. §2.1
// requires refusing such a document rather than processing part of it, so §8.2's
// preflight never admits it: for the facts or the evidence document that is
// malformed-input, and for the pack it is pack-not-conformant (§8.4, §10).
//
// oversized is the same condition observed by a caller that reads its inputs
// itself: a bounded read that stopped at the limit holds no bytes to measure
// here, and the limit is still reached at this point in the preflight and not
// before it.
func byteLimit(name string, data []byte, class string, oversized bool) *Failure {
	if !oversized && int64(len(data)) <= carrier.HardMaxBytes {
		return nil
	}
	return &Failure{
		Class:    class,
		Phase:    result.PhasePreflight,
		Code:     "JPS-RESOURCE-INPUT-BYTE-LIMIT",
		Message:  fmt.Sprintf("The %s input exceeds the hard %d-byte limit.", name, carrier.HardMaxBytes),
		ExitCode: result.ExitIO,
	}
}

// decodeInput decodes one JSON input with the same strict carrier rules the
// pack itself gets: duplicate member names, depth, and size limits are all
// enforced, and the failure names the offending input. class is the §8.4 class
// this input's failures belong to.
func decodeInput(data []byte, name, class string) (any, *Failure) {
	if len(data) == 0 {
		return nil, &Failure{
			Class:    class,
			Phase:    result.PhasePreflight,
			Code:     "JPS-EVALUATION-INPUT-MISSING",
			Message:  fmt.Sprintf("The %s input is required and was empty; empty bytes are not a carrier-conforming JSON text.", name),
			ExitCode: result.ExitInvocation,
		}
	}
	document, failure := carrier.Decode(data, carrier.DefaultLimits())
	if failure != nil {
		return nil, decodeFailure(name, class, failure)
	}
	return document, nil
}

// decodeFailure maps a carrier failure on an evaluation input: a resource
// limit keeps its own JPS-RESOURCE-* code and the IO exit class, mirroring
// validation; anything else is a caller input error. Either way the input was
// never admitted, so the §8.4 class is the one preflight assigns to this input
// and never resource-exhaustion, which §10 reserves for an admitted input.
func decodeFailure(name, class string, failure *carrier.Failure) *Failure {
	if failure.Resource {
		return &Failure{
			Class:    class,
			Phase:    result.PhasePreflight,
			Code:     failure.Diagnostic.Code,
			Message:  fmt.Sprintf("The %s input exceeds a resource limit: %s", name, failure.Diagnostic.Message),
			ExitCode: result.ExitIO,
		}
	}
	return &Failure{
		Class:    class,
		Phase:    result.PhasePreflight,
		Code:     "JPS-EVALUATION-INPUT-JSON",
		Message:  fmt.Sprintf("The %s input is not acceptable JSON: %s", name, failure.Diagnostic.Message),
		ExitCode: result.ExitInvocation,
	}
}

// decodeEvidence decodes the optional tri-state evidence-availability input of
// §8.2: a JSON object keyed by declared evidenceRequirements[].id with values
// "present", "absent", or "unknown". An omitted key means unknown, and an
// omitted document as a whole is the implicit empty object, which makes every
// declared requirement unknown; that is the only form absence takes and it is not
// an error — an empty *supplied* document is not that form, and is refused. A value that is not a JSON object, an undeclared member name, and a
// value outside the three strings are each the malformed-input error of §8.4.
// Every declared requirement is represented in the returned map.
//
// The document's own member names are walked in sorted order, not in map order,
// so a document violating two of these rules at once reports the same diagnostic
// on every run. §8.4 fixes the class, not the detail, but a detail that varies
// between identical runs is not a detail anyone can pin.
func decodeEvidence(data []byte, packRoot map[string]any, supplied bool) (map[string]tri, *Failure) {
	declared := map[string]tri{}
	names := []string{}
	for _, entry := range asArray(packRoot["evidenceRequirements"]) {
		if requirement, ok := entry.(map[string]any); ok {
			if id, ok := requirement["id"].(string); ok {
				declared[id] = triUnknown
				names = append(names, id)
			}
		}
	}
	sort.Strings(names)
	if len(data) == 0 {
		if !supplied {
			return declared, nil
		}
		// A document was supplied and is empty. That is not the absence §8.2
		// defines — absence is the omitted document — and empty bytes are not a
		// carrier-conforming JSON text, so it is the malformed-input error reached
		// at this input's own place in the preflight.
		return nil, &Failure{
			Class:    result.ClassMalformedInput,
			Phase:    result.PhasePreflight,
			Code:     "JPS-EVALUATION-INPUT-JSON",
			Message:  "The evidence input is empty; empty bytes are not a carrier-conforming JSON text. Supply a JSON object, or omit the evidence document to treat every declared requirement as unknown.",
			ExitCode: result.ExitInvocation,
		}
	}
	document, failure := carrier.Decode(data, carrier.DefaultLimits())
	if failure != nil {
		return nil, decodeFailure("evidence", result.ClassMalformedInput, failure)
	}
	object, ok := document.(map[string]any)
	if !ok {
		return nil, &Failure{
			Class:    result.ClassMalformedInput,
			Phase:    result.PhasePreflight,
			Code:     "JPS-EVALUATION-EVIDENCE-SHAPE",
			Message:  "The evidence input must be a JSON object mapping declared evidence-requirement ids to \"present\", \"absent\", or \"unknown\".",
			ExitCode: result.ExitInvocation,
		}
	}
	presented := make([]string, 0, len(object))
	for key := range object {
		presented = append(presented, key)
	}
	sort.Strings(presented)
	for _, key := range presented {
		value := object[key]
		if _, known := declared[key]; !known {
			detail := "The pack declares no evidence requirements."
			if len(names) > 0 {
				detail = "Declared ids: " + strings.Join(names, ", ") + "."
			}
			return nil, &Failure{
				Class:    result.ClassMalformedInput,
				Phase:    result.PhasePreflight,
				Code:     "JPS-EVALUATION-EVIDENCE-KEY",
				Message:  fmt.Sprintf("Evidence key %q does not name a declared evidence requirement. %s", display.Sanitize(key), detail),
				ExitCode: result.ExitInvocation,
			}
		}
		state, ok := value.(string)
		if !ok || (state != "present" && state != "absent" && state != "unknown") {
			return nil, &Failure{
				Class:    result.ClassMalformedInput,
				Phase:    result.PhasePreflight,
				Code:     "JPS-EVALUATION-EVIDENCE-VALUE",
				Message:  fmt.Sprintf("Evidence key %q must map to \"present\", \"absent\", or \"unknown\".", display.Sanitize(key)),
				ExitCode: result.ExitInvocation,
			}
		}
		switch state {
		case "present":
			declared[key] = triTrue
		case "absent":
			declared[key] = triFalse
		default:
			declared[key] = triUnknown
		}
	}
	return declared, nil
}
