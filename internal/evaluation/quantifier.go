package evaluation

import "encoding/json"

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
// with room to spare: with the short paths and small values that sketch implies
// it charges about 6·10⁶ units against this budget of 100,000, a factor of
// about 60, while every collection a hand-authored pack plausibly quantifies
// over fits.
//
// It is also this runtime's §10 collection-size limit, which RFC 0008's uplift
// raises to a MUST alongside the evaluation-work one. No second knob states it,
// because the work budget already implies it: an aggregate whose where costs c
// units admits at most (budget - aggregate cost)/c elements, so the cheapest
// possible where (a one-unit literal) bounds one aggregate at just under
// 100,000 elements and an ordinary fact predicate over a short pointer, a
// boolean selected and a boolean authored — six units — bounds it at about
// 16,000. The document that carries those elements
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
// hashes the member name, including an evidence requirement's; numeric equality
// normalizes whole tokens and decimal ordering parses them. Charging a flat
// unit for any of those would let one condition node buy unbounded processing,
// and the budget would bound the shape of the work rather than its amount.
const (
	unitNode = 1
	// unitPointerScan is the fixed part of compiling one authored pointer; its
	// variable part is the path's byte length.
	unitPointerScan = 1
	// unitPointerStep is one step of a resolution of an already compiled
	// pointer: one traversal per reference token, and one for a pointer with no
	// tokens, which still selects the root. The variable part of a resolution
	// is the bytes of those tokens.
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
// The five clauses are the whole of it, and they need no sixth arm. §7.4
// equality is total over carrier-valid JSON — jsonEqual decides numbers on
// their tokens rather than on an arithmetic type that has to hold them — so
// there is no resolved pair whose comparison is "indeterminate" and no third
// thing for a clause to say about one. An implementation that cannot compare
// two admitted values has a resource problem, which is an error, not a
// disposition; it is not a semantics, and pinning one would have made the
// result depend on the evaluator's number type instead of on the documents.
//
// One representative therefore suffices for clause 3: total equality is
// transitive, so every member equal to the first stands for it and the first
// member that differs is a counterexample against all of them, in whatever
// order the members arrive. Clause 3 still dominates clause 4 — a member whose
// at is missing is skipped rather than answered, and the unknown it earns is
// only reached when the whole pass found no counterexample.
//
// The representative is compared against every other resolved member, so each
// value is canonicalized (canonicalNumbers) once as it is resolved rather than
// normalized inside each comparison. Both are the same equality; the difference
// is that one long token among many short equal ones is then read once instead
// of once per member. The accounting model reserves the comparisons either way
// — see the reread term on evaluate — but a bound is not a reason to spend it.
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
	var representative any
	elected, missing := false, false
	for _, member := range members {
		value, resolved := e.resolve(member, at)
		if !resolved {
			missing = true
			continue
		}
		value = canonicalNumbers(value)
		if !elected {
			representative, elected = value, true
			continue
		}
		if !jsonEqual(representative, value) {
			return triFalse // clause 3
		}
	}
	if missing {
		return triUnknown // clause 4
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
// The accounting model, stated in full, is the candidate RFC 0008 leaves open.
// It is the third candidate, and each predecessor was broken by a demonstrated
// attack rather than retired on taste. The first charged a flat unit per node,
// and a megabyte-long authored pointer resolved once per element bought
// unbounded scanning for two units apiece. The second made authored bytes
// count — paths and operands — and left the runtime values a condition
// actually reads unpriced, so a two-byte operand compared against a megabyte
// of facts, once per element, was again free. This one prices both sides:
// every authored operand and every selected value.
//
//   - Work unit — one visited condition node, one step of a pointer
//     resolution, or one byte of a path, an object member name, or a scalar
//     token that a scan or a comparison has to read. Units are byte-sensitive
//     wherever the processing they stand for is: charging one flat unit for a
//     path, a string, or a numeric token would bound the number of nodes an
//     evaluation touches while leaving the processing per node unbounded, which
//     is not a limit at all.
//   - Preflight — charge walks the tree without evaluating any predicate, but
//     it does resolve pointers, including a fact's. An aggregate's cost is its
//     element count and a comparison's cost is the size of the value its
//     pointer selected; neither has an honest cheaper approximation, and a
//     model that priced only what the author wrote would price only the half
//     of a comparison an attacker does not control. Every resolution is charged
//     before it is performed, so a pointer that fails to resolve still costs
//     what it took to look it up, an unresolved or non-array aggregate path
//     costs that lookup and nothing more, an unresolved fact path costs its
//     lookup and its authored operand, and a path too long to afford is refused
//     with its bytes still unread. Measuring a resolved value is the one step
//     that runs before its own charge is known — its size cannot be charged
//     until it is known — and it is bounded anyway: the charge lands
//     immediately after, so the preflight stops within one selected value of
//     the budget. A charged resolution is performed twice, once to price it and
//     once to evaluate it, and a charged value is walked twice for the same
//     reason; the budget bounds the work up to that constant factor of two,
//     which is what a work unit is for.
//   - Per pointer — resolving an authored pointer costs two things. Compiling
//     it — reading its bytes, splitting it, decoding its escapes — costs
//     1 + len(path) and is charged, and performed, at most once per distinct
//     authored pointer in one evaluation, because the compiled form is cached
//     and reused. Each resolution of the compiled form then costs
//     max(1, tokens) + Σ len(token): one step per reference token, plus the
//     token bytes that step hashes or compares against the document. The steps
//     are counted rather than folded into the bytes, because a reference
//     token can be empty and still cost a lookup — "/a////b" is five traversals
//     over two bytes of token — and the floor of one is the resolution that
//     walks no token at all, which still selects the root. A path a where
//     resolves once per element is therefore scanned once and stepped per
//     element, and the charge says exactly that.
//   - Per value — a scalar costs 1 plus its token length: a string its
//     characters, a number its digits, and a boolean or null nothing beyond the
//     1. A container costs 1 plus its members, and an object member also costs
//     its name's bytes, because comparing objects looks names up. This is what
//     a deep equality or an in over that value can cost at most, since §7.4
//     equality descends only where both sides have the same shape.
//   - Per node — literal costs 1; evidence-present costs 1 plus the bytes of
//     the requirement id, which every one of its lookups hashes and which a
//     where inside an aggregate looks up once per element; not and all/any cost
//     1 plus their children, every branch included, so a branch a
//     short-circuiting evaluator never reaches is still charged; fact costs 1
//     plus its pointer, plus the size of the value that pointer selected, plus
//     the size of its authored operand. Both sides, because a comparison reads
//     both and only one of them is authored — and each by its bytes, so a long
//     decimal or numeric token is not pretended to be one unit on either side.
//     in charges the selected value once per candidate, since each candidate
//     can be compared against the whole of it, and the candidate array's own
//     size once.
//   - Per aggregate — exists and every cost 1 plus their path resolution plus
//     the sum of their where's cost over the elements actually present, so a
//     ragged nesting costs Σᵢ|Bᵢ| and never |A|×|B|; uniform, which has no
//     where, costs 1 plus its path resolution plus two terms over its members.
//     The first is per member: one resolution of at and the size of the value
//     that resolution selected, which pays for the single pass that reads each
//     value once. The second is the reread term, (n-1)×max over the n resolved
//     values, which pays for the comparisons. §7.4 equality is total, so one
//     representative absorbs every member it equals and the comparison count
//     stays linear — but the representative is one of the values and it is
//     compared against each of the other n-1, and any of those comparisons can
//     read the whole of it, so the sum of the values is not a bound on the pass
//     and the earlier "one pass is the whole cost" claim was wrong: one long
//     token among short equal ones was work proportional to long×members priced
//     at long+members. The term is a maximum over the resolved values rather
//     than the elected representative's size precisely so that it does not
//     depend on which member is elected, and therefore not on the order the
//     members arrive in; it bounds the pass whichever one is. The evaluator
//     then spends less than the bound — it canonicalizes each value once, so a
//     comparison compares canonical forms — but the charge is the bound.
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
//
// The step count is the token count and not a flat one, because a resolution
// loops once per token whatever the tokens weigh: a pointer of a hundred empty
// reference tokens ("////…") carries no token bytes at all and still performs a
// hundred map traversals on every one of its resolutions. Charging the bytes
// alone priced that at one unit per resolution. The floor of one is the empty
// pointer, whose single "resolution" selects the root.
func (e *evaluator) chargePointer(path string) bool {
	compiled, scanned := e.pointers[path]
	if !scanned {
		if !e.charge(unitPointerScan + len(path)) {
			return false
		}
		compiled = e.compiled(path)
	}
	steps := len(compiled.tokens)
	if steps < 1 {
		steps = 1
	}
	return e.charge(steps*unitPointerStep + compiled.bytes)
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
		// The resolution the pointer charge just paid for is performed here, so
		// the value the comparison will actually read can be priced. Charging
		// the authored operand alone would price the half of the comparison the
		// author wrote and leave the half the facts document supplies free,
		// which is the same defect the flat pointer charge had.
		selected := 0
		if value, resolved := e.resolve(root, path); resolved {
			selected = valueUnits(value)
		}
		// in compares the selected value against each candidate in turn, and
		// each of those comparisons can read all of it.
		comparisons := 1
		if condition["operator"] == "in" {
			comparisons = len(asArray(condition["value"]))
		}
		e.charge(comparisons*selected + valueUnits(condition["value"]))
	case "evidence-present":
		// The lookup hashes the requirement id, and a where does it once per
		// element, so the id's bytes are charged like any other name a lookup
		// reads.
		name, _ := condition["evidenceRequirement"].(string)
		e.charge(unitNode + len(name))
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
		// Two terms. The first is one resolution of at per member plus the size
		// of the value it selected: the single pass that reads each value once.
		// The second is the reread term below, which prices the comparisons.
		resolvedValues, largest := 0, 0
		for _, member := range members {
			// The member's at is reserved before it is resolved, exactly as the
			// aggregate's own path was.
			if !e.chargePointer(at) {
				return
			}
			value, resolved := e.resolve(member, at)
			if !resolved {
				continue
			}
			units := valueUnits(value)
			resolvedValues++
			if units > largest {
				largest = units
			}
			if !e.charge(units) {
				return
			}
		}
		// §7.4 equality is total, so one elected representative absorbs every
		// member equal to it and there are n-1 comparisons rather than n²/2 —
		// but the representative is a value like any other, and each of those
		// comparisons can read the whole of it. Charging one reread of the
		// largest resolved value per comparison bounds the pass whichever
		// member is elected, which is also what keeps the term independent of
		// the members' order: the count and the maximum are properties of the
		// set. Both are unchanged by any permutation, so the total is.
		if resolvedValues > 1 {
			e.charge((resolvedValues - 1) * largest)
		}
	default:
		// literal and any operand shape §7 does not define.
		e.charge(unitNode)
	}
}

// valueUnits is the size of a value in work units: a container costs one plus
// its members, an object member costs its name's bytes as well, and a scalar
// costs one plus the length of its JSON token — a string's characters, a
// number's digits, and nothing beyond the one for a boolean or null. It is what
// a deep equality or an in over that value can cost at most, since §7.4
// equality descends only where both sides have the same shape.
//
// The token length is not decoration. §7.4 equality on numbers normalizes both
// tokens byte by byte and RFC 0006's ordered comparison parses two decimal
// strings, so a long value compared once per element is work proportional to
// its length times the element count; charging one unit per scalar would have
// priced that at one unit per element. It applies to whichever side is long:
// the operand the author wrote and the value the pointer selected are charged
// by the same function for the same reason.
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
