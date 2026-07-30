package project

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// matrixCoverage derives, from one pack document's own declarations, the probe
// classes a matrix can witness through its rows' expected dispositions, and
// reports which of them some row's expectation does witness (ADR-0014). Each
// probe exists only where the declarations make its behavior reachable: a pack
// with no applicability member has no not-applicable probe to miss.
//
// The probes are one row per producible declared outcome — one some rule,
// force-outcome exception, or fallbackOutcome names — then a class per
// reachable §8 reason. That overlaps the test_pack method's probe list without being it, in
// both directions: two of the method's probes are deliberately not here,
// because no expected disposition can witness them — a forced outcome reads
// exactly like a rule-produced one (§8.3 carries no "forced" member, and a
// suppressed rule leaves no mark), and the ordered-comparison type probe lives
// in a row's facts, not its expectation — and two probes here,
// exception-escalation and no-match, are reachable reasons the method's
// numbered list does not name. Coverage reports what expectations can state
// and does not pretend to the rest.
//
// A missing probe is a fact — no row expects this — and never a failed row. In
// particular, a derived probe is reachable by declaration, not by proof: two
// rules naming different outcomes may exclude each other on facts, and a
// conflict row for them is then unconstructible. That is why coverage informs
// and never gates.
func matrixCoverage(pack map[string]any, matrix Matrix) []result.MatrixProbe {
	if pack == nil {
		return nil
	}
	witnesses := make([]ProbeWitness, 0, len(matrix.Cases))
	for _, row := range matrix.Cases {
		if len(row.ExpectedDisposition) == 0 {
			continue
		}
		if witness, ok := DecodeWitness(fmt.Sprintf("Row %q", display.Sanitize(row.ID)), row.ExpectedDisposition); ok {
			witnesses = append(witnesses, witness)
		}
	}
	return PackProbes(pack, witnesses, Reach{})
}

// ProbeWitness is the slice of one expectation coverage reads: a label naming
// where the expectation came from, and the disposition it states. A witness
// exists only for a legal §8.3 disposition — DecodeWitness is the gate — so a
// probe never reads covered on an expectation the comparator would refuse.
type ProbeWitness struct {
	Label     string
	Kind      string
	OutcomeID string
	Reasons   map[string]bool
}

// DecodeWitness decodes one raw expected disposition into a witness through
// the same strict gate the row comparator applies
// (evaluation.DecodeDisposition), stated once so no surface can decode a
// witness more leniently than it compares one. An expectation that does not
// decode witnesses nothing — the comparator already reports such a row as its
// own mismatch.
func DecodeWitness(label string, raw json.RawMessage) (ProbeWitness, bool) {
	expected, err := evaluation.DecodeDisposition(raw)
	if err != nil {
		return ProbeWitness{}, false
	}
	reasons := map[string]bool{}
	for _, reason := range expected.Reasons {
		reasons[reason] = true
	}
	return ProbeWitness{Label: label, Kind: expected.Kind, OutcomeID: expected.OutcomeID, Reasons: reasons}, true
}

// Reach narrows, for one pack, which inputs a caller can actually supply. The
// pack surface narrows nothing — any caller can hand a standalone evaluation
// any presence state — and a composed surface may narrow the evidence side: a
// graph edge that feeds a requirement fixes its state set, because a caller
// entry for an edge-fed requirement is refused rather than merged. The zero
// Reach is the pack surface's.
type Reach struct {
	// EvidenceStates returns, for one declared evidence requirement id, the
	// presence states that can actually arrive. nil means unnarrowed: all
	// three tri-states.
	EvidenceStates func(requirementID string) []string
}

// evidenceCanBe reports whether any required requirement can arrive in the
// given state under this reach — the reachability question behind both
// evidence-driven probes: missing-required-evidence needs a required
// requirement that can be absent, and evidence's door into reason unknown
// needs one that can be unknown.
func (r Reach) evidenceCanBe(pack map[string]any, state string) bool {
	for _, requirement := range asObjects(pack["evidenceRequirements"]) {
		if required, _ := requirement["required"].(bool); !required {
			continue
		}
		if r.EvidenceStates == nil {
			return true
		}
		id, _ := requirement["id"].(string)
		if slices.Contains(r.EvidenceStates(id), state) {
			return true
		}
	}
	return false
}

// ProducibleOutcomes lists the declared outcomes §8 can actually produce — the
// ones some rule, force-outcome exception, or fallbackOutcome names — in
// declaration order. Semantic validation checks only the forward direction —
// every named outcome must be declared — so the reverse is decided here: an
// outcome nothing references cannot be produced under §8, and deriving its
// probe would state an expectation no row could ever satisfy without
// mismatching.
func ProducibleOutcomes(pack map[string]any) []string {
	referenced := referencedOutcomes(pack)
	var outcomes []string
	for _, entry := range asObjects(pack["outcomes"]) {
		id, _ := entry["id"].(string)
		if id != "" && referenced[id] {
			outcomes = append(outcomes, id)
		}
	}
	return outcomes
}

// ReachableReasons lists the §8 reasons this pack's declarations make
// reachable under the given reach, in the order the probes report them. Every
// probe derivation goes through this one list, so no two surfaces can
// disagree about what a pack can do.
func ReachableReasons(pack map[string]any, reach Reach) []string {
	var reasons []string

	// §8 step 1: only a declared applicability can evaluate false.
	_, applicability := pack["applicability"]
	if applicability {
		reasons = append(reasons, evaluation.ReasonNotApplicable)
	}

	// §8 step 2: only a required requirement that can arrive absent can be
	// missing.
	if reach.evidenceCanBe(pack, "absent") {
		reasons = append(reasons, evaluation.ReasonMissingEvidence)
	}

	// Reason unknown has three doors: an applicability that evaluates unknown, a
	// required requirement whose presence can be unknown, and an escalating rule
	// or exception whose condition evaluates unknown. Any one of them makes the
	// reason reachable and the probe worth a row.
	if applicability || reach.evidenceCanBe(pack, "unknown") || anyEscalatingOnUnknown(pack) {
		reasons = append(reasons, evaluation.ReasonUnknown)
	}

	// §8 steps 5 and 8: a conflict needs two rules, or two true force-outcome
	// exceptions, naming different outcomes. Declarations say whether such a
	// pair exists; whether facts can make both fire together is the row author's
	// question, which is one of the reasons coverage never gates.
	if distinctRuleOutcomes(pack) > 1 || distinctForcedOutcomes(pack) > 1 {
		reasons = append(reasons, evaluation.ReasonConflict)
	}

	// A direct escalation is the one exception effect an expected disposition
	// can witness, through its own reason.
	if anyEscalateException(pack) {
		reasons = append(reasons, evaluation.ReasonExceptionEscalation)
	}

	// §8 step 10: no-match is reachable exactly while no fallbackOutcome is
	// declared.
	if _, declared := pack["fallbackOutcome"].(string); !declared {
		reasons = append(reasons, evaluation.ReasonNoMatch)
	}
	return reasons
}

// PackProbes derives one pack's probes — one per producible outcome, one per
// reachable reason — and reports each covered by its first witness or missing
// with a sentence naming what no witness expects. This is ADR-0014's whole
// derivation behind one entry point, so a surface composing packs derives
// exactly what the pack surface derives, narrowed only through Reach.
func PackProbes(pack map[string]any, witnesses []ProbeWitness, reach Reach) []result.MatrixProbe {
	if pack == nil {
		return nil
	}
	// nil until a probe is derived: a pack whose declarations derive none — a
	// shape only a document far from conformant can reach — carries no coverage
	// member rather than an empty one.
	var probes []result.MatrixProbe
	add := func(probe, missingDetail string, witnessed func(ProbeWitness) bool) {
		for _, witness := range witnesses {
			if witnessed(witness) {
				probes = append(probes, result.MatrixProbe{
					Probe:  probe,
					Status: result.MatrixProbeCovered,
					Detail: witness.Label + " expects it.",
				})
				return
			}
		}
		probes = append(probes, result.MatrixProbe{Probe: probe, Status: result.MatrixProbeMissing, Detail: missingDetail})
	}
	expectsReason := func(reason string) func(ProbeWitness) bool {
		return func(witness ProbeWitness) bool { return witness.Reasons[reason] }
	}

	for _, id := range ProducibleOutcomes(pack) {
		add("outcome:"+id,
			fmt.Sprintf("No row expects an outcome disposition naming %q.", display.Sanitize(id)),
			func(witness ProbeWitness) bool { return witness.Kind == "outcome" && witness.OutcomeID == id })
	}

	for _, reason := range ReachableReasons(pack, reach) {
		switch reason {
		case evaluation.ReasonNotApplicable:
			// A not-applicable disposition carries its kind, not a reason, so
			// this one probe is witnessed by the kind.
			add(reason,
				"The pack declares applicability, and no row expects a not-applicable disposition.",
				func(witness ProbeWitness) bool { return witness.Kind == "not-applicable" })
		case evaluation.ReasonMissingEvidence:
			add(reason,
				`The pack declares required evidence, and no row's expected reasons include "missing-required-evidence".`,
				expectsReason(reason))
		case evaluation.ReasonUnknown:
			add(reason,
				`The pack can reach reason "unknown", and no row's expected reasons include it.`,
				expectsReason(reason))
		case evaluation.ReasonConflict:
			add(reason,
				`Rules or forced outcomes name different outcomes, and no row's expected reasons include "conflict". Either construct facts that make two of them fire together, or confirm against the policy text that they exclude each other.`,
				expectsReason(reason))
		case evaluation.ReasonExceptionEscalation:
			add(reason,
				`An exception declares effect "escalate", and no row's expected reasons include "exception-escalation".`,
				expectsReason(reason))
		case evaluation.ReasonNoMatch:
			add(reason,
				`The pack declares no fallbackOutcome, and no row's expected reasons include "no-match".`,
				expectsReason(reason))
		}
	}

	return probes
}

// PackRoot decodes bytes the caller already holds — the file is not read a
// second time — with the same carrier rules the pack got everywhere else. A
// document that does not decode yields nil, which derives no probes; every
// caller has already refused such a pack before deriving coverage from it.
func PackRoot(data []byte) map[string]any {
	document, failure := carrier.Decode(data, carrier.DefaultLimits())
	if failure != nil {
		return nil
	}
	root, _ := document.(map[string]any)
	return root
}

// anyEscalatingOnUnknown reports whether any rule or exception declares
// onUnknown: escalate — the same member the resolver reads at its unknown
// verdicts.
func anyEscalatingOnUnknown(pack map[string]any) bool {
	for _, member := range []string{"rules", "exceptions"} {
		for _, entry := range asObjects(pack[member]) {
			if onUnknown, _ := entry["onUnknown"].(string); onUnknown == "escalate" {
				return true
			}
		}
	}
	return false
}

func anyEscalateException(pack map[string]any) bool {
	for _, exception := range asObjects(pack["exceptions"]) {
		if effect, _ := exception["effect"].(string); effect == "escalate" {
			return true
		}
	}
	return false
}

// referencedOutcomes collects every outcome id some rule, force-outcome
// exception, or fallbackOutcome names — the outcomes §8 can actually produce,
// which is the set the per-outcome probes are derived over.
func referencedOutcomes(pack map[string]any) map[string]bool {
	outcomes := map[string]bool{}
	for _, rule := range asObjects(pack["rules"]) {
		if outcome, ok := rule["outcome"].(string); ok && outcome != "" {
			outcomes[outcome] = true
		}
	}
	for _, exception := range asObjects(pack["exceptions"]) {
		if effect, _ := exception["effect"].(string); effect != "force-outcome" {
			continue
		}
		if outcome, ok := exception["outcome"].(string); ok && outcome != "" {
			outcomes[outcome] = true
		}
	}
	if fallback, ok := pack["fallbackOutcome"].(string); ok && fallback != "" {
		outcomes[fallback] = true
	}
	return outcomes
}

func distinctRuleOutcomes(pack map[string]any) int {
	outcomes := map[string]bool{}
	for _, rule := range asObjects(pack["rules"]) {
		if outcome, ok := rule["outcome"].(string); ok && outcome != "" {
			outcomes[outcome] = true
		}
	}
	return len(outcomes)
}

func distinctForcedOutcomes(pack map[string]any) int {
	outcomes := map[string]bool{}
	for _, exception := range asObjects(pack["exceptions"]) {
		if effect, _ := exception["effect"].(string); effect != "force-outcome" {
			continue
		}
		if outcome, ok := exception["outcome"].(string); ok && outcome != "" {
			outcomes[outcome] = true
		}
	}
	return len(outcomes)
}

func asObjects(value any) []map[string]any {
	items, _ := value.([]any)
	objects := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			objects = append(objects, object)
		}
	}
	return objects
}
