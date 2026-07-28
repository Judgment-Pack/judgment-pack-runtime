package evaluation

import (
	"encoding/json"
	"math/big"
)

// Draft RFC 0008 (bounded collection quantifiers) semantics and their work
// accounting. Nothing here is reachable unless the caller opts in: the
// operators belong to no published JPS version, a pack using them is not valid
// under 0.1.0-draft, and spec validate rejects it unchanged.

// DefaultWorkBudget is this runtime's §10 evaluation-work limit for one
// evaluation under the draft grammar, in the work units charged below. It is a
// runtime choice, not a portable one: RFC 0008 leaves the common limit open, so
// two evaluators may pick different numbers and an above-limit input is not
// portable between them. The number is set so the RFC's own attack sketch — an
// aggregate over 10³ elements each carrying an aggregate over 10³ — is refused
// with room to spare: with the short paths that sketch implies it charges about
// 5·10⁶ units against this budget of 100,000, a factor of about 50, while every
// collection a hand-authored pack plausibly quantifies over fits.
//
// It is also this runtime's §10 collection-size limit, which RFC 0008's uplift
// raises to a MUST alongside the evaluation-work one. No second knob states it,
// because the work budget already implies it: an aggregate whose where costs c
// units admits at most (budget - aggregate cost)/c elements, so the cheapest
// possible where (a one-unit literal) bounds one aggregate at just under
// 100,000 elements and an ordinary fact predicate over a short pointer — five
// units — bounds it at about 20,000. The document that carries those elements
// is bounded independently by the carrier layer's byte limit
// (carrier.HardMaxBytes) and nesting depth (carrier.DefaultLimits), which apply
// to the facts input before any condition is charged.
const DefaultWorkBudget = 100_000

// Work units. One unit is one elementary step of bounded cost: visiting one
// condition node, taking one step of a pointer resolution, or reading one byte
// of a path, an object member name, or a scalar token that a scan or a
// comparison has to look at.
//
// The byte-proportional half is load-bearing rather than pedantic. A pointer
// resolution scans, splits, and unescapes its whole path; a member lookup
// hashes the member name; numeric equality and decimal ordering parse whole
// tokens. Charging a flat unit for any of those would let one condition node
// buy unbounded processing, and the budget would bound the shape of the work
// rather than its amount.
const (
	unitNode = 1
	// unitPointerScan is the fixed part of compiling one authored pointer; its
	// variable part is the path's byte length.
	unitPointerScan = 1
	// unitPointerStep is the fixed part of one resolution of an already
	// compiled pointer; its variable part is the bytes of its reference tokens.
	unitPointerStep = 1
	// unitValue is the fixed part of one JSON node of a value; a scalar's
	// variable part is its token length and an object member's is its name.
	unitValue = 1
)

// quantify evaluates an RFC 0008 exists or every. The aggregate path resolves
// against the current condition root; where is then evaluated once per element
// with that element as the root, and the per-element results are combined by
// §7.2 (exists) and §7.1 (every) over a child list whose length is fixed at
// runtime. An unresolved or non-array path is unknown, mirroring §7.4. The
// empty array is the RFC's pinned choice, not an inheritance: false for exists
// and true for every, the identity elements of finite disjunction and
// conjunction. Short-circuiting is permitted on the dominant value only — true
// for exists, false for every — and never on unknown.
func (e *evaluator) quantify(node map[string]any, root any) tri {
	elements, resolved := e.selectArray(node, root)
	if !resolved {
		return triUnknown
	}
	// dominant decides on sight; vacuous is both the empty-array value and the
	// value an all-non-dominant array takes.
	dominant, vacuous := triTrue, triFalse
	if node["op"] == "every" {
		dominant, vacuous = triFalse, triTrue
	}
	sawUnknown := false
	for _, element := range elements {
		switch e.condition(node["where"], element) {
		case dominant:
			return dominant
		case triUnknown:
			sawUnknown = true
		}
	}
	if sawUnknown {
		return triUnknown
	}
	return vacuous
}

// uniform evaluates an RFC 0008 uniform by its five ordered clauses, the first
// that applies deciding: (1) an unresolved or non-array path is unknown; (2) an
// empty array is true; (3) any two members whose at-values both resolve and are
// unequal under §7.4 equality make it false, because a known counterexample
// dominates missing data; (4) otherwise an at that fails to resolve in any
// member — including a singleton — is unknown; (5) otherwise true.
//
// Clause 3 is checked against a set of representatives rather than over every
// pair. Determinable §7.4 equality is transitive over the values it can
// determine, so one representative stands for every value equal to it and a
// counterexample against it is a counterexample against them all.
// Determinability itself is not transitive, though: two members can be
// determinably unequal while both are undeterminable against a third (an
// unrepresentable number). One representative would lose that counterexample
// and make the answer depend on which member came first, so a value
// undeterminable against every current representative becomes a further
// representative instead of being discarded. The set grows only for a value no
// current representative can be compared with, so an ordinary collection keeps
// exactly one representative and the single linear pass the preflight charges;
// the passes a growing set costs are charged in advance too, per the accounting
// model on evaluate.
//
// One case is this runtime's own, not the RFC's. A pair whose equality §7.4
// cannot determine at all — two resolved values carrying a number the
// arithmetic cannot represent — is neither a counterexample nor a confirmation,
// and RFC 0008's five clauses do not say what it produces: clause 3 pins known
// inequality, clause 4 pins an at that fails to resolve, and neither covers a
// resolved pair whose comparison is indeterminate. This runtime treats it
// exactly as clause 4 treats a missing at, which keeps unknown uniform across
// both ways of not knowing and leaves clause 3's known counterexample dominant.
// An amendment folding indeterminate equality into clause 4, ordered after
// clause 3, is proposed to the RFC; until it is adopted this is a documented
// extension of the five clauses rather than conformance to them.
func (e *evaluator) uniform(node map[string]any, root any) tri {
	at, ok := node["at"].(string)
	if !ok {
		return triUnknown
	}
	members, resolved := e.selectArray(node, root)
	if !resolved {
		return triUnknown // clause 1
	}
	if len(members) == 0 {
		return triTrue // clause 2
	}
	values := make([]any, 0, len(members))
	missing := false
	for _, member := range members {
		value, resolved := e.resolve(member, at)
		if !resolved {
			missing = true
			continue
		}
		values = append(values, value)
	}
	representatives := make([]any, 0, 1)
	undeterminable := false
	for _, value := range values {
		absorbed := false
		for _, representative := range representatives {
			equal, determinable := jsonEqual(representative, value)
			if !determinable {
				undeterminable = true
				continue
			}
			if !equal {
				return triFalse // clause 3
			}
			absorbed = true
		}
		if !absorbed {
			representatives = append(representatives, value)
		}
	}
	if missing || undeterminable {
		// clause 4 for a missing at; the proposed amendment to it, documented
		// above, for an indeterminate comparison between resolved values.
		return triUnknown
	}
	return triTrue // clause 5
}

// selectArray resolves an aggregate's path against the current condition root
// and reports its elements. The second result is false when the path does not
// resolve or does not select a JSON array, which every aggregate reads as
// unknown. An empty path selects the current root, so an array-valued element
// can be quantified over directly.
func (e *evaluator) selectArray(node map[string]any, root any) ([]any, bool) {
	path, ok := node["path"].(string)
	if !ok {
		return nil, false
	}
	selected, resolved := e.resolve(root, path)
	if !resolved {
		return nil, false
	}
	elements, isArray := selected.([]any)
	if !isArray {
		return nil, false
	}
	return elements, true
}

// evaluate charges one whole condition tree and only then evaluates it.
//
// The accounting model, stated in full, is the candidate RFC 0008 leaves open:
//
//   - Work unit — one visited condition node, one step of a pointer
//     resolution, or one byte of a path, an object member name, or a scalar
//     token that a scan or a comparison has to read. Units are byte-sensitive
//     wherever the processing they stand for is: charging one flat unit for a
//     path, a string, or a numeric token would bound the number of nodes an
//     evaluation touches while leaving the processing per node unbounded, which
//     is not a limit at all.
//   - Preflight — charge walks the tree without evaluating any predicate. It
//     does resolve aggregate paths, because an aggregate's cost is its element
//     count and no cheaper approximation is honest; every such resolution is
//     charged before it is performed, so a pointer that fails to resolve still
//     costs what it took to look it up, an unresolved or non-array aggregate
//     path costs that lookup and nothing more, and a path too long to afford is
//     refused with its bytes still unread.
//   - Per pointer — resolving an authored pointer costs two things. Compiling
//     it — reading its bytes, splitting it, decoding its escapes — costs
//     1 + len(path) and is charged, and performed, at most once per distinct
//     authored pointer in one evaluation, because the compiled form is cached
//     and reused. Each resolution of the compiled form then costs
//     1 + Σ len(token): one step per reference token, plus the token bytes that
//     step hashes or compares against the document. A path a where resolves
//     once per element is therefore scanned once and stepped per element, and
//     the charge says exactly that.
//   - Per value — a scalar costs 1 plus its token length: a string its
//     characters, a number its digits, and a boolean or null nothing beyond the
//     1. A container costs 1 plus its members, and an object member also costs
//     its name's bytes, because comparing objects looks names up. This is what
//     a deep equality or an in over that value can cost at most, since §7.4
//     equality descends only where both sides have the same shape.
//   - Per node — literal and evidence-present cost 1; not and all/any cost 1
//     plus their children, every branch included, so a branch a short-circuiting
//     evaluator never reaches is still charged; fact costs 1 plus its pointer
//     plus the size of its authored operand, which bounds both deep equality and
//     in — and bounds a long decimal or numeric operand by its token length
//     rather than pretending it is one unit.
//   - Per aggregate — exists and every cost 1 plus their path resolution plus
//     the sum of their where's cost over the elements actually present, so a
//     ragged nesting costs Σᵢ|Bᵢ| and never |A|×|B|; uniform, which has no
//     where, costs 1 plus its path resolution plus, per member, one resolution
//     of at and the size of the value that resolution selected, plus — only
//     when some selected value carries a number §7.4 cannot compare — one
//     further pass over every selected value for each such value, which is what
//     bounds the extra comparisons an undeterminable value forces.
//   - Composition — sibling aggregates add, since the charge is a sum over the
//     tree; the budget accumulates across every condition of one evaluation.
//     A condition §8 never reaches is never charged: a suppressed rule, every
//     rule after a step-6 forced outcome, and everything after a false or
//     unknown applicability. The total is therefore a property of the §8 path
//     rather than of pack and facts alone — but §8 fixes that path, so it is
//     the same path, and the same total, for every evaluator that follows it.
//
// The charge is a sum over the elements present and over the distinct authored
// pointers the tree names, and neither depends on the order elements arrive in,
// so it is identical under any permutation of them. It is not identical under
// duplication: a duplicated element is one more element, and it is charged like
// any other, which may be what crosses the limit. Only the condition's value is
// duplication-invariant, and only while both inputs remain within the limits —
// which is exactly what RFC 0008's result-invariance wording says.
//
// Each condition tree's charge is complete before the first predicate of that
// tree runs. Short-circuiting can therefore only reduce actual work: it can
// never change whether the limit was exceeded. Exceeding it is an explicit
// evaluation error, never a disposition — the caller must refuse rather than
// report — so this returns unknown only to unwind, and the caller checks
// exceeded before reading the value.
func (e *evaluator) evaluate(node any, root any) tri {
	if e.quantifiers {
		e.preflight(node, root)
		if e.exceeded {
			return triUnknown
		}
	}
	return e.condition(node, root)
}

// charge adds units to the running total and reports whether the budget still
// holds. Preflight stops at the first overrun, which bounds the preflight's own
// work by the budget without changing the verdict: the total is a monotone sum,
// so a prefix exceeds the budget exactly when the whole tree does.
func (e *evaluator) charge(units int) bool {
	e.charged += units
	if e.charged > e.budget {
		e.exceeded = true
	}
	return !e.exceeded
}

// chargePointer reserves everything one resolution of an authored pointer can
// cost and reports whether the budget still holds. The scan that compiles the
// path is charged before a byte of it is read and is charged once per distinct
// authored pointer, because the compiled form is then cached for the rest of
// the evaluation; the resolution itself is charged per call, at one step per
// reference token plus the bytes that step reads. Reserving before resolving is
// the whole point: an authored path long enough to be expensive to scan is
// refused by the budget rather than scanned and then billed for.
func (e *evaluator) chargePointer(path string) bool {
	compiled, scanned := e.pointers[path]
	if !scanned {
		if !e.charge(unitPointerScan + len(path)) {
			return false
		}
		compiled = e.compiled(path)
	}
	return e.charge(unitPointerStep + compiled.bytes)
}

// preflight charges one condition tree under the model documented on evaluate.
func (e *evaluator) preflight(node any, root any) {
	if e.exceeded {
		return
	}
	condition, ok := node.(map[string]any)
	if !ok {
		e.charge(unitNode)
		return
	}
	switch condition["op"] {
	case "not":
		if e.charge(unitNode) {
			e.preflight(condition["condition"], root)
		}
	case "all", "any":
		if !e.charge(unitNode) {
			return
		}
		for _, child := range asArray(condition["conditions"]) {
			e.preflight(child, root)
			if e.exceeded {
				return
			}
		}
	case "fact":
		if !e.charge(unitNode) {
			return
		}
		path, _ := condition["path"].(string)
		if !e.chargePointer(path) {
			return
		}
		e.charge(valueUnits(condition["value"]))
	case "exists", "every":
		if !e.charge(unitNode) {
			return
		}
		path, _ := condition["path"].(string)
		if !e.chargePointer(path) {
			return
		}
		elements, _ := e.selectArray(condition, root)
		for _, element := range elements {
			e.preflight(condition["where"], element)
			if e.exceeded {
				return
			}
		}
	case "uniform":
		if !e.charge(unitNode) {
			return
		}
		path, _ := condition["path"].(string)
		if !e.chargePointer(path) {
			return
		}
		at, _ := condition["at"].(string)
		members, _ := e.selectArray(condition, root)
		opaque, selected := 0, 0
		for _, member := range members {
			// The member's at is reserved before it is resolved, exactly as the
			// aggregate's own path was.
			if !e.chargePointer(at) {
				return
			}
			if value, resolved := e.resolve(member, at); resolved {
				units := valueUnits(value)
				selected += units
				if bearsUnrepresentableNumber(value) {
					opaque++
				}
				if !e.charge(units) {
					return
				}
			}
		}
		// The one comparison per member charged above is the whole cost only
		// while §7.4 can compare what it is given. A value it cannot compare
		// becomes a further representative (see uniform), and each further
		// representative costs one more comparison against every member, so a
		// value that may create one is charged a pass over every selected value
		// in advance. Both factors are sums over the members, so the charge is
		// still the same under any permutation, and an ordinary collection —
		// where no value carries a number the arithmetic cannot represent — is
		// charged nothing extra.
		if opaque > 0 {
			e.charge(opaque * selected)
		}
	default:
		// literal, evidence-present, and any operand shape §7 does not define.
		e.charge(unitNode)
	}
}

// bearsUnrepresentableNumber reports whether a value carries, anywhere within
// it, a syntactically valid JSON number the arithmetic cannot represent. Such a
// number is the only thing that makes a §7.4 comparison undeterminable, so it
// is the only thing that can grow uniform's representative set: two values that
// both are free of one always compare determinably, so at most one
// representative is. The preflight uses it to charge the extra comparison
// passes in advance, which is what keeps the charge an honest bound on the
// work rather than a linear guess at a quadratic worst case.
func bearsUnrepresentableNumber(value any) bool {
	switch typed := value.(type) {
	case json.Number:
		_, representable := new(big.Rat).SetString(typed.String())
		return !representable
	case []any:
		for _, item := range typed {
			if bearsUnrepresentableNumber(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if bearsUnrepresentableNumber(item) {
				return true
			}
		}
	}
	return false
}

// valueUnits is the size of a value in work units: a container costs one plus
// its members, an object member costs its name's bytes as well, and a scalar
// costs one plus the length of its JSON token — a string's characters, a
// number's digits, and nothing beyond the one for a boolean or null. It is what
// a deep equality or an in over that value can cost at most, since §7.4
// equality descends only where both sides have the same shape.
//
// The token length is not decoration. §7.4 equality on numbers parses both
// tokens with arbitrary-precision arithmetic and RFC 0006's ordered comparison
// parses two decimal strings, so a long operand compared once per element is
// work proportional to its length times the element count; charging one unit
// per scalar would have priced that at one unit per element.
func valueUnits(value any) int {
	switch typed := value.(type) {
	case []any:
		units := unitValue
		for _, item := range typed {
			units += valueUnits(item)
		}
		return units
	case map[string]any:
		units := unitValue
		for name, item := range typed {
			units += len(name) + valueUnits(item)
		}
		return units
	case string:
		return unitValue + len(typed)
	case json.Number:
		return unitValue + len(typed.String())
	default:
		return unitValue
	}
}
