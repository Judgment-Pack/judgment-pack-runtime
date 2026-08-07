package project

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// matrixCoverage derives, from one pack document's own declarations, the probe
// classes a matrix can witness through its rows, and reports which of them some
// row does witness (ADR-0014, ADR-0023). Each probe exists only where the
// declarations make its behavior reachable: a pack with no applicability member
// has no not-applicable probe to miss.
//
// The probes come in two families. The disposition family is one probe per
// producible declared outcome — one some rule, force-outcome exception, or
// fallbackOutcome names — then a class per reachable §8 reason, each witnessed
// by what a row expects. That overlaps the test_pack method's probe list
// without being it, in both directions: two of the method's probes are
// deliberately not derived, and two probes here — exception-escalation and
// no-match — are reachable reasons the method's numbered list does not name.
//
// The two the method has and this does not: the forced-outcome probe, because
// no expected disposition can witness it (a forced outcome reads exactly like
// a rule-produced one — §8.3 carries no "forced" member, and a suppressed rule
// leaves no mark); and the ordered-comparison type probe, a JSON number where
// a §2.2 decimal string belongs. That second one the boundary family below
// makes derivable — it reads facts, and a number at a compared pointer is a
// readable fact — and it is deliberately left out anyway: the schema pins an
// ordered operand to a decimal string, so the mistake lives in fact
// production, upstream of the pack and outside what a pack document can be
// held to. It is a named follow-up, not a refusal (ADR-0023).
//
// The boundary family is one probe per distinct fact pointer and decimal value
// the pack's own conditions compare, witnessed by a row whose facts place that
// pointer's value exactly at the literal and whose expected disposition leaves
// that comparison reachable under §8's order. It reads a row's authored facts,
// not what an evaluation produced, and it is derived here rather than inside
// PackProbes because only a matrix row carries a facts document (ADR-0023).
//
// A missing probe is a fact — no row states this — and never a failed row. In
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
	return append(PackProbes(pack, witnesses, Reach{}), boundaryProbes(pack, matrix)...)
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

// siteStage is where in §8's evaluation order a comparison sits, which decides
// which rows can have exercised it. §8 evaluates applicability first (step 1),
// then required evidence (step 2), and only then rules and exceptions; each of
// the first two steps can halt the evaluation before any rule condition is
// read. So the stage of a comparison and the disposition a row expects
// together say whether that row's evaluation could have reached the
// comparison at all.
//
// The two stages are ordered by permissiveness — every row that exercises a
// rule-staged site also exercises an applicability-staged one — which is what
// lets a boundary compared at several sites carry the smallest stage among
// them and answer for all of them in one test.
type siteStage int

const (
	// stageApplicability: §8 step 1, evaluated on every evaluation that
	// reaches §8 at all.
	stageApplicability siteStage = iota
	// stageRules: a rule's or an exception's when, reached only when steps 1
	// and 2 did not halt first.
	stageRules
)

// exercisedBy reports whether a row expecting this disposition can have
// reached a comparison at this stage, read off §8's evaluation order rather
// than off a run.
//
// An applicability comparison is exercised by any row whose expectation
// decodes: applicability runs first, so even a not-applicable row exercised it
// — that disposition is what the comparison produced.
//
// A rule- or exception-staged comparison is exercised only by a row whose
// expected disposition implies the evaluation got past steps 1 and 2. Exactly
// two dispositions prove it did not: kind not-applicable is step 1's halt
// (§8.3 pins its reason set to exactly {"not-applicable"}, so it is
// unambiguous), and an unresolved carrying no reason but
// missing-required-evidence is step 2's. Every other disposition is admitted.
//
// That admission is deliberately generous in one place and honest about it: an
// unresolved whose reasons include "unknown" may have come from an
// applicability that evaluated unknown, in which case no rule ran either — and
// no derivation over declarations alone can tell those apart without the
// fact-value reachability analysis this family refuses. Generous is the right
// direction here, because the cost of the strict reading is a demand for a row
// the author already wrote, and an over-demand is the failure mode ADR-0023
// refuses; the cost of the generous one is a probe covered by a row that does
// place the value at the boundary.
func (stage siteStage) exercisedBy(witness ProbeWitness) bool {
	if stage == stageApplicability {
		return true
	}
	switch witness.Kind {
	case "not-applicable":
		return false
	case "unresolved":
		for reason := range witness.Reasons {
			if reason != evaluation.ReasonMissingEvidence {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// comparisonSite is one ordered comparison a pack declares: the fact pointer
// it reads, the decimal literal it compares against and that literal's
// canonical value, the operator that decides the comparison, the declaration
// that owns it, and that declaration's §8 stage. The owner and the operator
// are carried for the report only — neither is part of a probe's identity,
// because neither changes which rows witness it. The stage is not identity
// either, but it does change which rows witness.
type comparisonSite struct {
	path    string
	literal string
	// key is the literal's canonical decimal value (evaluation.DecimalKey),
	// read once here so grouping folds by value in one pass rather than by
	// comparing every pair of literals.
	key      string
	operator string
	owner    string
	stage    siteStage
}

// boundaryGroup is one derived boundary: a fact pointer, the first literal
// authored at it, every site comparing that pointer against that value, and
// the smallest §8 stage among those sites. Two sites sharing a pointer and a
// value have the same facts test, so they are one question and one probe
// (ADR-0023); a row witnesses the group when its facts pass that test and its
// expectation could have reached at least one of the sites, which is exactly
// the smallest stage's test.
type boundaryGroup struct {
	path    string
	literal string
	sites   []comparisonSite
	stage   siteStage
	// pathIndex addresses this group's pointer in the derivation's
	// distinct-path list, so a pointer compared against several literals is
	// resolved once per row rather than once per probe.
	pathIndex int
}

// boundaryProbes derives one probe per distinct fact pointer and decimal value
// the pack's conditions compare, and reports each covered by the first row
// whose facts place that pointer's value exactly at the literal.
//
// A row at exactly the literal is the one input where a strict and a
// non-strict encoding of the same threshold disagree; every other row leaves
// the two indistinguishable. The witness is expectation-gated as well as
// facts-located: a row expecting a §8.4 error class is refused before §7
// compares anything, and a row whose expectation does not decode mismatches
// forever, so neither pins what the pack does at the boundary.
//
// The expectation does more than gate. It also says, through §8's evaluation
// order, whether that row's evaluation could have reached this particular
// comparison — a not-applicable row never reached a rule's when — so
// eligibility is decided per site and a row witnesses a group when it is
// eligible for at least one of the group's sites. siteStage.exercisedBy
// carries that rule and its derivation.
//
// Each row's facts are decoded once here, under the same carrier rules
// LoadMatrix used, and each distinct pointer is resolved once against that one
// decoded document however many literals are compared at it: coverage re-runs
// and re-validates nothing (issue #78), and the cost stays one decode and one
// resolution per row and pointer rather than one per probe.
func boundaryProbes(pack map[string]any, matrix Matrix) []result.MatrixProbe {
	groups := boundaryGroups(comparisonSites(pack))
	if len(groups) == 0 {
		return nil
	}
	paths := make([]string, 0, len(groups))
	pathIndex := map[string]int{}
	for index := range groups {
		at, seen := pathIndex[groups[index].path]
		if !seen {
			at = len(paths)
			pathIndex[groups[index].path] = at
			paths = append(paths, groups[index].path)
		}
		groups[index].pathIndex = at
	}
	witnesses := make([]string, len(groups))
	values := make([]any, len(paths))
	for _, row := range matrix.Cases {
		if len(row.ExpectedDisposition) == 0 {
			continue
		}
		label := fmt.Sprintf("Row %q", display.Sanitize(row.ID))
		witness, ok := DecodeWitness(label, row.ExpectedDisposition)
		if !ok {
			continue
		}
		facts, failure := carrier.Decode(row.Facts, carrier.DefaultLimits())
		if failure != nil {
			continue
		}
		for index, path := range paths {
			value, found := evaluation.ResolvePointer(facts, path)
			if !found {
				value = nil
			}
			values[index] = value
		}
		for index, group := range groups {
			if witnesses[index] != "" || !group.stage.exercisedBy(witness) {
				continue
			}
			// The evaluator's own comparison decides equality, so "5000.0"
			// witnesses the literal "5000" and a JSON number witnesses nothing
			// — the value §7.4 cannot compare never exercises the boundary.
			if comparison, comparable := evaluation.DecimalCompare(values[group.pathIndex], group.literal); comparable && comparison == 0 {
				witnesses[index] = label
			}
		}
	}
	probes := make([]result.MatrixProbe, 0, len(groups))
	for index, group := range groups {
		probe := "boundary:" + group.path + ":" + group.literal
		if witnesses[index] != "" {
			probes = append(probes, result.MatrixProbe{
				Probe:  probe,
				Status: result.MatrixProbeCovered,
				Detail: witnesses[index] + " places the compared value there.",
			})
			continue
		}
		probes = append(probes, result.MatrixProbe{
			Probe:  probe,
			Status: result.MatrixProbeMissing,
			Detail: fmt.Sprintf("No row's facts place %q at %s, the literal compared by %s. A row at exactly that value is the one input where a strict and a non-strict encoding of this threshold differ; the policy text is the arbiter of which one the pack should carry.",
				display.Sanitize(group.path), display.Sanitize(group.literal), joinCapped(siteDescriptions(group.sites), 6)),
		})
	}
	return probes
}

// siteDescriptions names each site of one boundary the way a reader would open
// the pack at it: the owning declaration and the operator it compares with.
// The operator is the half of a threshold that can disagree with the prose, so
// it is reported even though it is not part of the probe's identity.
// Identical descriptions are listed once — one rule may compare a pointer
// against a value twice, once under a not, and naming the same member twice
// tells a reader nothing the first mention did not.
func siteDescriptions(sites []comparisonSite) []string {
	described := make([]string, 0, len(sites))
	listed := map[string]bool{}
	for _, site := range sites {
		description := fmt.Sprintf("%s (%s)", display.Sanitize(site.owner), display.Sanitize(site.operator))
		if listed[description] {
			continue
		}
		listed[description] = true
		described = append(described, description)
	}
	return described
}

// boundaryGroups folds sites into probes keyed by pointer text and decimal
// value: "70" and "70.0" at one pointer are one boundary, judged by the
// evaluator's own canonical value rather than by a second equality. Each
// literal is read once, when its site is collected, so the fold is one pass
// rather than a comparison against every group already open.
//
// The key is the pointer text, a NUL, and the canonical value. A canonical
// decimal contains no NUL, so the last NUL in a key is always the separator
// and two different (pointer, value) pairs cannot share a key — including when
// the authored pointer itself carries one.
//
// Order is first occurrence in walk order, the probe carries the
// first-authored spelling of the literal, and the group keeps the smallest §8
// stage among its sites.
func boundaryGroups(sites []comparisonSite) []boundaryGroup {
	var groups []boundaryGroup
	index := map[string]int{}
	for _, site := range sites {
		at, opened := index[site.path+"\x00"+site.key]
		if !opened {
			at = len(groups)
			index[site.path+"\x00"+site.key] = at
			groups = append(groups, boundaryGroup{path: site.path, literal: site.literal, stage: site.stage})
		}
		groups[at].sites = append(groups[at].sites, site)
		if site.stage < groups[at].stage {
			groups[at].stage = site.stage
		}
	}
	return groups
}

// comparisonSites collects every ordered comparison the pack's condition trees
// state, in declaration order: applicability, then each rule's when, then each
// exception's when, descending only through all, any, and not.
//
// The walk is structure-keyed, deliberately the opposite of ADR-0020's
// shape-keyed pointer collector. That collector over-approximates because an
// over-reported pointer is a visible, arguable line; an over-reported probe is
// a demand for a row, and a demand derived from a condition-shaped object
// carried as data — inside a value literal or an extension — would be a
// boundary the evaluator never evaluates and no row could ever witness. For a
// warning over-report is the safe direction; for a demand under-report is.
// Nothing is lost by it: every pack reaching coverage is admitted under the
// closed core grammar, where conditions recurse through exactly these three
// doors.
func comparisonSites(pack map[string]any) []comparisonSite {
	var sites []comparisonSite
	collectComparisons(pack["applicability"], "applicability", stageApplicability, &sites)
	for _, member := range []string{"rules", "exceptions"} {
		noun := "rule"
		if member == "exceptions" {
			noun = "exception"
		}
		for index, entry := range asObjects(pack[member]) {
			owner := fmt.Sprintf("%s %d", noun, index)
			if id, _ := entry["id"].(string); id != "" {
				owner = fmt.Sprintf("%s %q", noun, id)
			}
			collectComparisons(entry["when"], owner, stageRules, &sites)
		}
	}
	return sites
}

// collectComparisons walks one condition tree depth-first and appends every
// ordered comparison over a §2.2 decimal literal, tagged with the stage of the
// declaration the walk entered from. The operand check is defensive rather
// than load-bearing: the schema pins an ordered operator's value to a decimal
// string, and a pack that failed that check never reaches coverage at all. It
// reads the literal's canonical value here, once per site, which is also what
// admits it.
func collectComparisons(node any, owner string, stage siteStage, sites *[]comparisonSite) {
	condition, ok := node.(map[string]any)
	if !ok {
		return
	}
	switch condition["op"] {
	case "all", "any":
		children, _ := condition["conditions"].([]any)
		for _, child := range children {
			collectComparisons(child, owner, stage, sites)
		}
	case "not":
		collectComparisons(condition["condition"], owner, stage, sites)
	case "fact":
		operator, _ := condition["operator"].(string)
		if !evaluation.OrderedOperator(operator) {
			return
		}
		path, ok := condition["path"].(string)
		if !ok {
			return
		}
		literal, ok := condition["value"].(string)
		if !ok {
			return
		}
		key, decimal := evaluation.DecimalKey(literal)
		if !decimal {
			return
		}
		*sites = append(*sites, comparisonSite{path: path, literal: literal, key: key, operator: operator, owner: owner, stage: stage})
	}
}

// joinCapped lists values with a cap, so one boundary compared at many sites
// names the first few and counts the rest instead of printing an unbounded
// line. It is a copy of internal/validation's helper of the same name, not a
// shared one: that package's is unexported, two internal packages cannot read
// each other's helpers without one of them exporting it, and exporting a
// string-joining detail from a validator to a coverage derivation would state
// a dependency neither package has. The cap differs too — a site description
// is a phrase where validation lists ids.
func joinCapped(values []string, limit int) string {
	if len(values) <= limit {
		return strings.Join(values, ", ")
	}
	return strings.Join(values[:limit], ", ") + fmt.Sprintf(" (and %d more)", len(values)-limit)
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
