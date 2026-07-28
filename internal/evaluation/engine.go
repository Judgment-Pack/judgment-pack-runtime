// Package evaluation implements the JPS Core §§7–8 experiment, as pinned by
// the specification's RFC 0006 (Draft), behind an explicitly experimental
// surface (ADR-0007).
//
// It claims no evaluator conformance — JPS 0.1.0-draft §3.4 forbids such
// claims outright — and may change or be removed without compatibility
// promise. Evaluation is a pure function of its inputs: a conformant pack,
// one facts document, the tri-state availability of declared evidence, and
// the supported-extension set. Errors are not dispositions: a non-conformant
// pack, a malformed input, or an undeclared evidence key is refused.
package evaluation

import (
	"fmt"
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
type Failure struct {
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
	// WorkBudget overrides DefaultWorkBudget. Zero or negative selects
	// DefaultWorkBudget. It applies only under the draft grammar, which is what
	// makes an evaluation-work limit load-bearing.
	WorkBudget int
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
func (e *Engine) EvaluateWith(pack, facts, evidence []byte, options Options) (result.Evaluation, *Failure) {
	// One byte-limit boundary for every surface: the MCP wire must refuse the
	// same oversized input the CLI's bounded reads refuse.
	for name, data := range map[string][]byte{"pack": pack, "facts": facts, "evidence": evidence} {
		if int64(len(data)) > carrier.HardMaxBytes {
			return result.Evaluation{}, &Failure{
				Code:     "JPS-RESOURCE-INPUT-BYTE-LIMIT",
				Message:  fmt.Sprintf("The %s input exceeds the hard %d-byte limit.", name, carrier.HardMaxBytes),
				ExitCode: result.ExitIO,
			}
		}
	}
	validated, packRoot, failure := e.conformance(pack, options)
	if failure != nil {
		return result.Evaluation{}, failure
	}

	factsDocument, failure := decodeInput(facts, "facts")
	if failure != nil {
		return result.Evaluation{}, failure
	}

	presence, failure := decodeEvidence(evidence, packRoot)
	if failure != nil {
		return result.Evaluation{}, failure
	}

	budget := options.WorkBudget
	if budget <= 0 {
		budget = DefaultWorkBudget
	}
	disposition, trace, failure := resolve(packRoot, factsDocument, &evaluator{
		evidence:    presence,
		quantifiers: options.RFC0008Quantifiers,
		budget:      budget,
	})
	if failure != nil {
		return result.Evaluation{}, failure
	}
	if trace == nil {
		trace = []result.TraceEntry{}
	}
	evaluation := result.Evaluation{
		OutputVersion:    result.OutputVersion,
		Tool:             result.CurrentTool(),
		Command:          options.Command,
		Status:           "evaluated",
		Experimental:     true,
		ConformanceClaim: result.EvaluationClaim,
		SpecVersion:      validated.SpecVersion,
		Disposition:      disposition,
		Trace:            trace,
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

// conformance establishes the pack's conformance status and returns its
// decoded root. Without the draft opt-in this is the published path unchanged:
// full document conformance against the bundled schema, or a refusal. Under the
// opt-in it is the RFC 0008 gate, which no pack reaches by accident.
func (e *Engine) conformance(pack []byte, options Options) (result.Validation, map[string]any, *Failure) {
	if options.RFC0008Quantifiers {
		return e.rfc0008Conformance(pack, options)
	}
	validated, operational := e.validator.Validate(pack, validation.Options{
		Through:             "semantic",
		SupportedExtensions: options.SupportedExtensions,
		Limits:              carrier.DefaultLimits(),
	})
	if operational != nil {
		return result.Validation{}, nil, &Failure{Code: operational.Code, Message: operational.Message, ExitCode: operational.ExitCode}
	}
	if validated.Status != "valid" {
		return result.Validation{}, nil, packNotConformant(validated)
	}
	document, failure := decodeInput(pack, "pack")
	if failure != nil {
		return result.Validation{}, nil, failure
	}
	packRoot, ok := document.(map[string]any)
	if !ok {
		return result.Validation{}, nil, &Failure{Code: "JPS-EVALUATION-INTERNAL", Message: "The validated pack did not decode to a JSON object.", ExitCode: result.ExitInternal}
	}
	return validated, packRoot, nil
}

// packNotConformant refuses a pack that did not reach full document
// conformance, naming the first diagnostic so the refusal is self-sufficient.
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
		Code:     "JPS-EVALUATION-PACK-NOT-CONFORMANT",
		Message:  fmt.Sprintf("The pack is not evaluated because its document conformance status is %q; evaluation requires a fully conformant pack. Run spec validate for the complete report.%s", validated.Status, detail),
		ExitCode: exitCode,
	}
}

// decodeInput decodes one JSON input with the same strict carrier rules the
// pack itself gets: duplicate member names, depth, and size limits are all
// enforced, and the failure names the offending input.
func decodeInput(data []byte, name string) (any, *Failure) {
	if len(data) == 0 {
		return nil, &Failure{
			Code:     "JPS-EVALUATION-INPUT-MISSING",
			Message:  fmt.Sprintf("The %s input is required and was empty.", name),
			ExitCode: result.ExitInvocation,
		}
	}
	document, failure := carrier.Decode(data, carrier.DefaultLimits())
	if failure != nil {
		return nil, decodeFailure(name, failure)
	}
	return document, nil
}

// decodeFailure maps a carrier failure on an evaluation input: a resource
// limit keeps its own JPS-RESOURCE-* code and the IO exit class, mirroring
// validation; anything else is a caller input error.
func decodeFailure(name string, failure *carrier.Failure) *Failure {
	if failure.Resource {
		return &Failure{
			Code:     failure.Diagnostic.Code,
			Message:  fmt.Sprintf("The %s input exceeds a resource limit: %s", name, failure.Diagnostic.Message),
			ExitCode: result.ExitIO,
		}
	}
	return &Failure{
		Code:     "JPS-EVALUATION-INPUT-JSON",
		Message:  fmt.Sprintf("The %s input is not acceptable JSON: %s", name, failure.Diagnostic.Message),
		ExitCode: result.ExitInvocation,
	}
}

// decodeEvidence decodes the optional tri-state evidence-availability input:
// a JSON object keyed by declared evidenceRequirements[].id with values
// "present", "absent", or "unknown". An omitted key means unknown; an
// undeclared key or any other value is an input error. Every declared
// requirement is represented in the returned map.
func decodeEvidence(data []byte, packRoot map[string]any) (map[string]tri, *Failure) {
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
		return declared, nil
	}
	document, failure := carrier.Decode(data, carrier.DefaultLimits())
	if failure != nil {
		return nil, decodeFailure("evidence", failure)
	}
	object, ok := document.(map[string]any)
	if !ok {
		return nil, &Failure{
			Code:     "JPS-EVALUATION-EVIDENCE-SHAPE",
			Message:  "The evidence input must be a JSON object mapping declared evidence-requirement ids to \"present\", \"absent\", or \"unknown\".",
			ExitCode: result.ExitInvocation,
		}
	}
	for key, value := range object {
		if _, known := declared[key]; !known {
			detail := "The pack declares no evidence requirements."
			if len(names) > 0 {
				detail = "Declared ids: " + strings.Join(names, ", ") + "."
			}
			return nil, &Failure{
				Code:     "JPS-EVALUATION-EVIDENCE-KEY",
				Message:  fmt.Sprintf("Evidence key %q does not name a declared evidence requirement. %s", display.Sanitize(key), detail),
				ExitCode: result.ExitInvocation,
			}
		}
		state, ok := value.(string)
		if !ok || (state != "present" && state != "absent" && state != "unknown") {
			return nil, &Failure{
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
