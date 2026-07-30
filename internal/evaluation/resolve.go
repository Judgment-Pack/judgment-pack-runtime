package evaluation

import (
	"slices"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Reason vocabulary of the §8 experiment. exception-escalation is a direct
// request rather than a trigger-selected one. Exported because the resolver is
// not the vocabulary's only reader: a project matrix's coverage report derives
// which of these reasons a pack's own declarations make reachable, and a
// vocabulary stated in two packages could disagree with itself.
const (
	ReasonNotApplicable       = "not-applicable"
	ReasonMissingEvidence     = "missing-required-evidence"
	ReasonUnknown             = "unknown"
	ReasonConflict            = "conflict"
	ReasonNoMatch             = "no-match"
	ReasonExceptionEscalation = "exception-escalation"
)

// resolver walks §8 over one conformant pack. It is a pure function of its
// inputs: the decoded pack, the decoded facts document, and the tri-state
// presence of every declared evidence requirement, which the evaluator carries
// along with the running charge against this evaluation's §10 evaluation-work
// limit.
type resolver struct {
	pack  map[string]any
	facts any
	eval  *evaluator

	reasons          map[string]bool
	directEscalation bool
	trace            []result.TraceEntry
}

// resolve produces the §8.3 disposition, the configured escalation target when
// one is requested, and the informative trace. The target is reported beside the
// disposition and never inside it (§8.3). The last result is an evaluation error
// rather than a disposition: reaching this evaluation's §10 evaluation-work limit
// interrupts §8 wherever it happens, and no partial disposition is reported.
func resolve(pack map[string]any, facts any, eval *evaluator) (result.Disposition, *result.HandoffTarget, []result.TraceEntry, *Failure) {
	r := &resolver{pack: pack, facts: facts, eval: eval, reasons: map[string]bool{}}

	// §8's own iteration, charged once and before step 1. Every authored evidence
	// requirement, exception, and rule is one step of the walk below whether or
	// not its condition is evaluated: step 2 inspects every requirement without
	// evaluating anything, and a suppressed or step-6-skipped rule is still
	// visited and still recorded in the trace. Charging the three counts up front
	// keeps the term independent of the order §8 reaches them in and complete
	// before the walk starts, which is the same discipline every condition tree's
	// charge follows.
	if !eval.charge(len(asArray(pack["evidenceRequirements"])) + len(asArray(pack["exceptions"])) + len(asArray(pack["rules"]))) {
		return result.Disposition{}, nil, nil, workLimitFailure(eval)
	}

	// Step 1: omitted applicability is the literal value true. False is a
	// terminal not-applicable result; unknown produces unresolved and stops.
	applicability := triTrue
	if condition, present := pack["applicability"]; present {
		applicability = r.eval.evaluate(condition, facts)
		if r.eval.exceeded {
			return result.Disposition{}, nil, r.trace, workLimitFailure(r.eval)
		}
	}
	switch applicability {
	case triFalse:
		r.reasons[ReasonNotApplicable] = true
		return r.dispose("not-applicable", "")
	case triUnknown:
		r.reasons[ReasonUnknown] = true
		return r.dispose("unresolved", "")
	}

	// Step 2, as pinned by RFC 0006: missing-required-evidence iff any
	// required requirement's presence is false; unknown iff any required
	// requirement's presence is unknown and none is false.
	requiredFalse, requiredUnknown := false, false
	for _, entry := range asArray(pack["evidenceRequirements"]) {
		requirement, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if required, _ := requirement["required"].(bool); !required {
			continue
		}
		id, _ := requirement["id"].(string)
		switch r.eval.evidence[id] {
		case triFalse:
			requiredFalse = true
		case triUnknown:
			requiredUnknown = true
		}
	}
	if requiredFalse {
		r.reasons[ReasonMissingEvidence] = true
	} else if requiredUnknown {
		r.reasons[ReasonUnknown] = true
	}

	// Steps 3-4: evaluate every exception and combine true effects. An
	// unknown exception with onUnknown: escalate records reason unknown; with
	// onUnknown: ignore it contributes nothing but stays visible in the trace.
	suppressed := map[string]bool{}
	forced := map[string]bool{}
	for _, entry := range asArray(pack["exceptions"]) {
		exception, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id, _ := exception["id"].(string)
		effect, _ := exception["effect"].(string)
		verdict := r.eval.evaluate(exception["when"], facts)
		if r.eval.exceeded {
			return result.Disposition{}, nil, r.trace, workLimitFailure(r.eval)
		}
		entry := result.TraceEntry{Stage: "exception", ID: id, Condition: verdict.String()}
		switch verdict {
		case triUnknown:
			onUnknown, _ := exception["onUnknown"].(string)
			entry.OnUnknown = onUnknown
			if onUnknown == "escalate" {
				r.reasons[ReasonUnknown] = true
			}
		case triTrue:
			entry.Effect = effect
			switch effect {
			case "suppress-rule":
				if target, ok := exception["targetRule"].(string); ok {
					suppressed[target] = true
				}
			case "force-outcome":
				if outcome, ok := exception["outcome"].(string); ok {
					forced[outcome] = true
					entry.Outcome = outcome
				}
			case "escalate":
				r.directEscalation = true
				r.reasons[ReasonExceptionEscalation] = true
			}
		}
		r.trace = append(r.trace, entry)
	}

	// Step 5: incompatible forced outcomes are a conflict. Any blocking state
	// discovered so far — after all exceptions were inspected — produces
	// unresolved with every retained reason. A direct escalation takes
	// precedence over suppression and forced outcomes.
	if len(forced) > 1 {
		r.reasons[ReasonConflict] = true
	}
	if len(r.reasons) > 0 {
		return r.dispose("unresolved", "")
	}

	// Step 6: one compatible forced outcome, no blocking state: produce it
	// without evaluating normal rules.
	if len(forced) == 1 {
		for outcome := range forced {
			for _, entry := range asArray(pack["rules"]) {
				if rule, ok := entry.(map[string]any); ok {
					id, _ := rule["id"].(string)
					r.trace = append(r.trace, result.TraceEntry{Stage: "rule", ID: id, Condition: "not-evaluated", Skipped: true})
				}
			}
			return r.dispose("outcome", outcome)
		}
	}

	// Steps 6-8: remove suppressed rules, evaluate the rest, and collect
	// candidate outcomes. An unknown rule with onUnknown: escalate records
	// reason unknown and blocks both a candidate outcome and the fallback.
	candidates := map[string]bool{}
	for _, entry := range asArray(pack["rules"]) {
		rule, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id, _ := rule["id"].(string)
		if suppressed[id] {
			r.trace = append(r.trace, result.TraceEntry{Stage: "rule", ID: id, Condition: "not-evaluated", Suppressed: true})
			continue
		}
		verdict := r.eval.evaluate(rule["when"], facts)
		if r.eval.exceeded {
			return result.Disposition{}, nil, r.trace, workLimitFailure(r.eval)
		}
		entry := result.TraceEntry{Stage: "rule", ID: id, Condition: verdict.String()}
		switch verdict {
		case triTrue:
			if outcome, ok := rule["outcome"].(string); ok {
				candidates[outcome] = true
				entry.Outcome = outcome
			}
		case triUnknown:
			onUnknown, _ := rule["onUnknown"].(string)
			entry.OnUnknown = onUnknown
			if onUnknown == "escalate" {
				r.reasons[ReasonUnknown] = true
			}
		}
		r.trace = append(r.trace, entry)
	}
	if len(candidates) > 1 {
		r.reasons[ReasonConflict] = true
	}
	if len(r.reasons) > 0 {
		return r.dispose("unresolved", "")
	}

	// Step 9: one distinct outcome and no blocking reason: produce it.
	if len(candidates) == 1 {
		for outcome := range candidates {
			return r.dispose("outcome", outcome)
		}
	}

	// Step 10: no true rule contributed an outcome. Use the fallback when
	// declared; otherwise unresolved with reason no-match.
	if fallback, ok := r.pack["fallbackOutcome"].(string); ok {
		return r.dispose("outcome", fallback)
	}
	r.reasons[ReasonNoMatch] = true
	return r.dispose("unresolved", "")
}

// dispose returns one resolution's §8.3 disposition, the configured escalation
// target when a handoff is requested, and the trace, in the shape resolve's
// callers return.
func (r *resolver) dispose(kind, outcomeID string) (result.Disposition, *result.HandoffTarget, []result.TraceEntry, *Failure) {
	disposition, target := r.disposition(kind, outcomeID)
	return disposition, target, r.trace, nil
}

// disposition assembles the portable disposition of §8.3: the result kind, the
// outcome id exactly when the kind is an outcome, the retained reason set sorted
// and duplicate-free, and the handoff object with its state and the reasons that
// triggered a request. An outcome result retains no reasons and requests no
// handoff.
//
// The second result is the pack's configured escalation target, reported beside
// the disposition and never inside it: §8.3 keeps the target out, because
// carrying a copy would let a disposition disagree with the pack it came from,
// and a requested handoff is a request rather than evidence that one occurred.
func (r *resolver) disposition(kind, outcomeID string) (result.Disposition, *result.HandoffTarget) {
	disposition := result.Disposition{Kind: kind, OutcomeID: outcomeID, Reasons: []string{}, Handoff: result.Handoff{State: "none"}}
	if kind == "outcome" {
		return disposition, nil
	}
	for reason := range r.reasons {
		disposition.Reasons = append(disposition.Reasons, reason)
	}
	sort.Strings(disposition.Reasons)

	// §8.1 and §8.3: triggeredBy is every retained reason the pack names as a
	// trigger, plus exception-escalation when a true exception made a direct
	// request. It is always a subset of the retained reason set, and it is
	// smaller than that set whenever escalation.triggers does not name every
	// retained reason.
	escalation, _ := r.pack["escalation"].(map[string]any)
	triggeredBy := []string{}
	if escalation != nil {
		for _, trigger := range asArray(escalation["triggers"]) {
			name, ok := trigger.(string)
			if ok && r.reasons[name] && !slices.Contains(triggeredBy, name) {
				triggeredBy = append(triggeredBy, name)
			}
		}
	}
	// A direct exception escalation is a request regardless of the trigger list,
	// using the configured target when one exists; without an escalation object
	// it remains a request with no Core-defined destination.
	if r.directEscalation && !slices.Contains(triggeredBy, ReasonExceptionEscalation) {
		triggeredBy = append(triggeredBy, ReasonExceptionEscalation)
	}
	if len(triggeredBy) == 0 {
		return disposition, nil
	}
	sort.Strings(triggeredBy)
	disposition.Handoff = result.Handoff{State: "requested", TriggeredBy: triggeredBy}
	if escalation != nil {
		if target, ok := escalation["target"].(map[string]any); ok {
			targetKind, _ := target["kind"].(string)
			targetName, _ := target["name"].(string)
			return disposition, &result.HandoffTarget{Kind: targetKind, Name: targetName}
		}
	}
	return disposition, nil
}

func asArray(value any) []any {
	items, _ := value.([]any)
	return items
}
