package evaluation

import (
	"sort"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// reason vocabulary of the §8 experiment. exception-escalation is a direct
// request rather than a trigger-selected one.
const (
	reasonNotApplicable       = "not-applicable"
	reasonMissingEvidence     = "missing-required-evidence"
	reasonUnknown             = "unknown"
	reasonConflict            = "conflict"
	reasonNoMatch             = "no-match"
	reasonExceptionEscalation = "exception-escalation"
)

// resolver walks §8 over one conformant pack. It is a pure function of its
// inputs: the decoded pack, the decoded facts document, and the tri-state
// presence of every declared evidence requirement, which the evaluator carries
// along with the running work charge of the draft RFC 0008 opt-in.
type resolver struct {
	pack  map[string]any
	facts any
	eval  *evaluator

	reasons          map[string]bool
	directEscalation bool
	trace            []result.TraceEntry
}

// resolve produces the RFC 0006 disposition and the informative trace. The
// third result is an evaluation error rather than a disposition: exhausting the
// draft RFC 0008 work budget interrupts §8 wherever it happens, and no partial
// disposition is reported.
func resolve(pack map[string]any, facts any, eval *evaluator) (result.Disposition, []result.TraceEntry, *Failure) {
	r := &resolver{pack: pack, facts: facts, eval: eval, reasons: map[string]bool{}}

	// Step 1: omitted applicability is the literal value true. False is a
	// terminal not-applicable result; unknown produces unresolved and stops.
	applicability := triTrue
	if condition, present := pack["applicability"]; present {
		applicability = r.eval.evaluate(condition, facts)
		if r.eval.exceeded {
			return result.Disposition{}, r.trace, workLimitFailure(r.eval)
		}
	}
	switch applicability {
	case triFalse:
		r.reasons[reasonNotApplicable] = true
		return r.disposition("not-applicable", ""), r.trace, nil
	case triUnknown:
		r.reasons[reasonUnknown] = true
		return r.disposition("unresolved", ""), r.trace, nil
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
		r.reasons[reasonMissingEvidence] = true
	} else if requiredUnknown {
		r.reasons[reasonUnknown] = true
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
			return result.Disposition{}, r.trace, workLimitFailure(r.eval)
		}
		entry := result.TraceEntry{Stage: "exception", ID: id, Condition: verdict.String()}
		switch verdict {
		case triUnknown:
			onUnknown, _ := exception["onUnknown"].(string)
			entry.OnUnknown = onUnknown
			if onUnknown == "escalate" {
				r.reasons[reasonUnknown] = true
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
				r.reasons[reasonExceptionEscalation] = true
			}
		}
		r.trace = append(r.trace, entry)
	}

	// Step 5: incompatible forced outcomes are a conflict. Any blocking state
	// discovered so far — after all exceptions were inspected — produces
	// unresolved with every retained reason. A direct escalation takes
	// precedence over suppression and forced outcomes.
	if len(forced) > 1 {
		r.reasons[reasonConflict] = true
	}
	if len(r.reasons) > 0 {
		return r.disposition("unresolved", ""), r.trace, nil
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
			return r.disposition("outcome", outcome), r.trace, nil
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
			return result.Disposition{}, r.trace, workLimitFailure(r.eval)
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
				r.reasons[reasonUnknown] = true
			}
		}
		r.trace = append(r.trace, entry)
	}
	if len(candidates) > 1 {
		r.reasons[reasonConflict] = true
	}
	if len(r.reasons) > 0 {
		return r.disposition("unresolved", ""), r.trace, nil
	}

	// Step 9: one distinct outcome and no blocking reason: produce it.
	if len(candidates) == 1 {
		for outcome := range candidates {
			return r.disposition("outcome", outcome), r.trace, nil
		}
	}

	// Step 10: no true rule contributed an outcome. Use the fallback when
	// declared; otherwise unresolved with reason no-match.
	if fallback, ok := r.pack["fallbackOutcome"].(string); ok {
		return r.disposition("outcome", fallback), r.trace, nil
	}
	r.reasons[reasonNoMatch] = true
	return r.disposition("unresolved", ""), r.trace, nil
}

// disposition assembles the RFC 0006 result: sorted deduplicated reasons and
// the §8.1 handoff axis. An outcome result retains no reasons and requests no
// handoff.
func (r *resolver) disposition(kind, outcomeID string) result.Disposition {
	disposition := result.Disposition{Kind: kind, OutcomeID: outcomeID, Reasons: []string{}, Handoff: result.Handoff{State: "none"}}
	if kind == "outcome" {
		return disposition
	}
	for reason := range r.reasons {
		disposition.Reasons = append(disposition.Reasons, reason)
	}
	sort.Strings(disposition.Reasons)

	escalation, _ := r.pack["escalation"].(map[string]any)
	triggered := false
	if escalation != nil {
		for _, trigger := range asArray(escalation["triggers"]) {
			if name, ok := trigger.(string); ok && r.reasons[name] {
				triggered = true
				break
			}
		}
	}
	// §8.1: a direct exception escalation is a request regardless of the
	// trigger list, using the configured target when one exists; without an
	// escalation object it remains a request with no Core-defined destination.
	if r.directEscalation || triggered {
		disposition.Handoff.State = "requested"
		if escalation != nil {
			if target, ok := escalation["target"].(map[string]any); ok {
				kind, _ := target["kind"].(string)
				name, _ := target["name"].(string)
				disposition.Handoff.Target = &result.HandoffTarget{Kind: kind, Name: name}
			}
		}
	}
	return disposition
}

func asArray(value any) []any {
	items, _ := value.([]any)
	return items
}
