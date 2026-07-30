package project

import (
	"encoding/json"
	"fmt"

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
// The probes are one row per declared outcome, then a class per reachable §8
// reason. That overlaps the test_pack method's probe list without being it, in
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
	rows := expectedRows(matrix)
	// nil until a probe is derived: a pack whose declarations derive none — a
	// shape only a document far from conformant can reach — carries no coverage
	// member rather than an empty one.
	var probes []result.MatrixProbe
	add := func(probe, missingDetail string, witnesses func(expectedRow) bool) {
		for _, row := range rows {
			if witnesses(row) {
				probes = append(probes, result.MatrixProbe{
					Probe:  probe,
					Status: result.MatrixProbeCovered,
					Detail: fmt.Sprintf("Row %q expects it.", display.Sanitize(row.id)),
				})
				return
			}
		}
		probes = append(probes, result.MatrixProbe{Probe: probe, Status: result.MatrixProbeMissing, Detail: missingDetail})
	}
	expectsReason := func(reason string) func(expectedRow) bool {
		return func(row expectedRow) bool { return row.reasons[reason] }
	}

	// One probe per declared outcome, in declaration order. Forced and fallback
	// outcomes are declared outcomes too, so this is also every target an
	// exception or fallbackOutcome can name.
	for _, entry := range asObjects(pack["outcomes"]) {
		id, _ := entry["id"].(string)
		if id == "" {
			continue
		}
		add("outcome:"+id,
			fmt.Sprintf("No row expects an outcome disposition naming %q.", display.Sanitize(id)),
			func(row expectedRow) bool { return row.kind == "outcome" && row.outcomeID == id })
	}

	// §8 step 1: only a declared applicability can evaluate false.
	if _, declared := pack["applicability"]; declared {
		add(evaluation.ReasonNotApplicable,
			"The pack declares applicability, and no row expects a not-applicable disposition.",
			func(row expectedRow) bool { return row.kind == "not-applicable" })
	}

	// §8 step 2: only a required requirement can be missing.
	if anyRequiredEvidence(pack) {
		add(evaluation.ReasonMissingEvidence,
			`The pack declares required evidence, and no row's expected reasons include "missing-required-evidence".`,
			expectsReason(evaluation.ReasonMissingEvidence))
	}

	// Reason unknown has three doors: an applicability that evaluates unknown, a
	// required requirement whose presence is unknown, and an escalating rule or
	// exception whose condition evaluates unknown. Any one of them makes the
	// reason reachable and the probe worth a row.
	_, applicability := pack["applicability"]
	if applicability || anyRequiredEvidence(pack) || anyEscalatingOnUnknown(pack) {
		add(evaluation.ReasonUnknown,
			`The pack can reach reason "unknown", and no row's expected reasons include it.`,
			expectsReason(evaluation.ReasonUnknown))
	}

	// §8 steps 5 and 8: a conflict needs two rules, or two true force-outcome
	// exceptions, naming different outcomes. Declarations say whether such a
	// pair exists; whether facts can make both fire together is the row author's
	// question, which is one of the reasons coverage never gates.
	if distinctRuleOutcomes(pack) > 1 || distinctForcedOutcomes(pack) > 1 {
		add(evaluation.ReasonConflict,
			`Rules or forced outcomes name different outcomes, and no row's expected reasons include "conflict". Either construct facts that make two of them fire together, or confirm against the policy text that they exclude each other.`,
			expectsReason(evaluation.ReasonConflict))
	}

	// A direct escalation is the one exception effect an expected disposition
	// can witness, through its own reason.
	if anyEscalateException(pack) {
		add(evaluation.ReasonExceptionEscalation,
			`An exception declares effect "escalate", and no row's expected reasons include "exception-escalation".`,
			expectsReason(evaluation.ReasonExceptionEscalation))
	}

	// §8 step 10: no-match is reachable exactly while no fallbackOutcome is
	// declared.
	if _, declared := pack["fallbackOutcome"].(string); !declared {
		add(evaluation.ReasonNoMatch,
			`The pack declares no fallbackOutcome, and no row's expected reasons include "no-match".`,
			expectsReason(evaluation.ReasonNoMatch))
	}

	return probes
}

// expectedRow is the slice of one matrix row coverage reads: the row's id and
// the disposition it expects. A row that expects an error class expects no
// disposition and can witness no probe; a row whose expected disposition does
// not decode is dropped here and fails its own byte comparison anyway.
type expectedRow struct {
	id        string
	kind      string
	outcomeID string
	reasons   map[string]bool
}

func expectedRows(matrix Matrix) []expectedRow {
	rows := []expectedRow{}
	for _, row := range matrix.Cases {
		if len(row.ExpectedDisposition) == 0 {
			continue
		}
		var decoded struct {
			Kind      string   `json:"kind"`
			OutcomeID string   `json:"outcomeId"`
			Reasons   []string `json:"reasons"`
		}
		if err := json.Unmarshal(row.ExpectedDisposition, &decoded); err != nil {
			continue
		}
		reasons := map[string]bool{}
		for _, reason := range decoded.Reasons {
			reasons[reason] = true
		}
		rows = append(rows, expectedRow{id: row.ID, kind: decoded.Kind, outcomeID: decoded.OutcomeID, reasons: reasons})
	}
	return rows
}

// packRoot decodes bytes the caller already holds — the file is not read a
// second time — with the same carrier rules the pack got everywhere else. A
// document that does not decode yields nil, which derives no probes; testPack
// has already refused such a pack before any row ran.
func packRoot(data []byte) map[string]any {
	document, failure := carrier.Decode(data, carrier.DefaultLimits())
	if failure != nil {
		return nil
	}
	root, _ := document.(map[string]any)
	return root
}

func anyRequiredEvidence(pack map[string]any) bool {
	for _, requirement := range asObjects(pack["evidenceRequirements"]) {
		if required, _ := requirement["required"].(bool); required {
			return true
		}
	}
	return false
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
