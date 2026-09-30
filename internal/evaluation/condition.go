package evaluation

import (
	"encoding/json"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// tri is the three-valued condition result of the §7 experiment.
type tri int

const (
	triFalse tri = iota
	triTrue
	triUnknown
)

func (t tri) String() string {
	switch t {
	case triTrue:
		return "true"
	case triFalse:
		return "false"
	default:
		return "unknown"
	}
}

func triFromBool(value bool) tri {
	if value {
		return triTrue
	}
	return triFalse
}

// evaluator interprets condition trees. It carries the evidence tri-state and,
// when the caller opted into the draft RFC 0008 grammar, the work budget that
// grammar makes load-bearing. One evaluator serves one evaluation, so the
// budget is charged across the whole pack rather than per condition.
type evaluator struct {
	evidence map[string]tri
	// quantifiers admits the draft RFC 0008 aggregates and turns on the work
	// accounting they require. Without it nothing about §7 changes.
	quantifiers bool
	// outcomeValues turns on the resolution step of draft RFC 0016: the values a
	// produced outcome declares are resolved, and an outcome whose values do not
	// all resolve is not produced. Without it nothing about §8 changes.
	outcomeValues bool
	budget        int
	charged       int
	exceeded      bool
	// pointers memoizes the compiled form of every authored pointer this
	// evaluation has resolved, keyed by the pointer text as the pack wrote it.
	// Compiling scans the path's bytes; the cache is what makes a per-element
	// resolution cost its steps rather than a fresh scan, and the accounting
	// model on evaluate charges each scan exactly once to match.
	pointers map[string]compiledPointer
	// causes and mismatches are what one stage's condition is recorded with
	// (ADR-0040). A leaf that evaluates unknown appends its cause, and a
	// combinator whose own verdict is not unknown truncates back to where it
	// started, because an unknown a sibling's verdict overrode caused nothing.
	// What remains when a stage is unknown is exactly the leaves it came from.
	// An equality comparison across JSON types appends a mismatch whatever the
	// verdict, and nothing truncates those. evaluateStage resets both, so
	// neither ever reaches past one stage; neither changes a verdict.
	causes     []result.UnknownCause
	mismatches []result.TypeMismatch
	// collectionFailure is why an aggregate's collection pointer selected no
	// array, held until withinCollection has labelled the element-level causes.
	collectionFailure *result.UnknownCause
	// unmetEvidence is what §8 step 2 found wanting: every required
	// requirement whose presence is absent or unknown, in declared order
	// (ADR-0040). The payload reports it beside the trace, because step 2
	// evaluates no condition and ADR-0027 gives it no trace stage.
	unmetEvidence []result.UnmetEvidence
}

// noteUnknown records the leaf an unknown came from.
func (e *evaluator) noteUnknown(cause result.UnknownCause) {
	e.causes = append(e.causes, cause)
}

// keepCausesIf keeps what was recorded since mark only when the verdict it
// explains is unknown.
func (e *evaluator) keepCausesIf(verdict tri, mark int) tri {
	if verdict != triUnknown && len(e.causes) > mark {
		e.causes = e.causes[:mark]
	}
	return verdict
}

// evaluateStage charges and evaluates one stage's condition and returns what
// ADR-0040 records beside its verdict: the causes when the verdict is unknown,
// and every cross-type equality comparison the condition evaluated. Both are in
// first-occurrence order with repeats removed, which keeps the record a pure
// function of the inputs whatever the tree's shape. The repeats are found by
// map lookup rather than by scanning what was kept, so removing them costs
// work in proportion to what was recorded, as the rest of the walk does.
func (e *evaluator) evaluateStage(node any, root any) (tri, []result.UnknownCause, []result.TypeMismatch) {
	e.causes, e.mismatches = nil, nil
	verdict := e.evaluate(node, root)
	var causes []result.UnknownCause
	if verdict == triUnknown && !e.exceeded {
		seen := make(map[causeKey]bool, len(e.causes))
		for _, cause := range e.causes {
			key := keyOfCause(cause)
			if !seen[key] {
				seen[key] = true
				causes = append(causes, cause)
			}
		}
	}
	var mismatches []result.TypeMismatch
	seen := make(map[mismatchKey]bool, len(e.mismatches))
	for _, mismatch := range e.mismatches {
		key := keyOfMismatch(mismatch)
		if !seen[key] {
			seen[key] = true
			mismatches = append(mismatches, mismatch)
		}
	}
	e.causes, e.mismatches = nil, nil
	return verdict, causes, mismatches
}

// causeKey and mismatchKey are the values two records are the same by. The
// pointers in a record are compared by the text they point at, with whether
// they are set kept apart from what they say, so the root pointer "" and an
// unset pointer are two different keys.
type causeKey struct {
	pathSet, withinSet                         bool
	path, requirement, within, cause, factType string
}

type mismatchKey struct {
	withinSet                                      bool
	path, within, operator, factType, operandTypes string
}

func keyOfCause(cause result.UnknownCause) causeKey {
	key := causeKey{requirement: cause.EvidenceRequirement, cause: cause.Cause, factType: cause.FactType}
	if cause.Path != nil {
		key.pathSet, key.path = true, *cause.Path
	}
	if cause.Within != nil {
		key.withinSet, key.within = true, *cause.Within
	}
	return key
}

func keyOfMismatch(mismatch result.TypeMismatch) mismatchKey {
	key := mismatchKey{path: mismatch.Path, operator: mismatch.Operator, factType: mismatch.FactType,
		operandTypes: strings.Join(mismatch.OperandTypes, "\x00")}
	if mismatch.Within != nil {
		key.withinSet, key.within = true, *mismatch.Within
	}
	return key
}

// pointer is a pointer to one pointer's text, which is how a record carries a
// JSON Pointer that may be "".
func pointer(text string) *string {
	return &text
}

// jsonType names the JSON type of one decoded value as the carrier decodes it:
// objects to maps, arrays to slices, and numbers to json.Number.
func jsonType(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number, float64:
		return "number"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		return "unknown"
	}
}

// condition evaluates one node against the current condition root: the runtime
// facts document at the top level and, under draft RFC 0008, the selected
// element inside a quantifier's where. The pack has full document conformance,
// so node shapes are schema-guaranteed; anything unexpected still degrades to
// unknown ("unsupported operators or operand shapes ... produce unknown"),
// which is also what an aggregate op does when the caller did not opt in.
func (e *evaluator) condition(node any, root any) tri {
	condition, ok := node.(map[string]any)
	if !ok {
		e.noteUnknown(result.UnknownCause{Cause: "unsupported"})
		return triUnknown
	}
	switch condition["op"] {
	case "literal":
		if value, ok := condition["value"].(bool); ok {
			return triFromBool(value)
		}
		e.noteUnknown(result.UnknownCause{Cause: "unsupported"})
		return triUnknown
	case "all":
		mark := len(e.causes)
		return e.keepCausesIf(e.all(condition["conditions"], root), mark)
	case "any":
		mark := len(e.causes)
		return e.keepCausesIf(e.any(condition["conditions"], root), mark)
	case "not":
		switch e.condition(condition["condition"], root) {
		case triTrue:
			return triFalse
		case triFalse:
			return triTrue
		default:
			return triUnknown
		}
	case "fact":
		return e.evalFact(condition, root)
	case "evidence-present":
		name, ok := condition["evidenceRequirement"].(string)
		if !ok {
			e.noteUnknown(result.UnknownCause{Cause: "unsupported"})
			return triUnknown
		}
		presence, declared := e.evidence[name]
		if !declared || presence == triUnknown {
			e.noteUnknown(result.UnknownCause{EvidenceRequirement: name, Cause: "unknown"})
			return triUnknown
		}
		return presence
	case "exists", "every":
		if !e.quantifiers {
			e.noteUnknown(result.UnknownCause{Cause: "unsupported"})
			return triUnknown
		}
		return e.withinCollection(condition, func() tri { return e.quantify(condition, root) })
	case "uniform":
		if !e.quantifiers {
			e.noteUnknown(result.UnknownCause{Cause: "unsupported"})
			return triUnknown
		}
		return e.withinCollection(condition, func() tri { return e.uniform(condition, root) })
	default:
		e.noteUnknown(result.UnknownCause{Cause: "unsupported"})
		return triUnknown
	}
}

// withinCollection runs one draft RFC 0008 aggregate and labels what its
// element-level leaves recorded with the aggregate's collection pointer, since
// their paths are relative to an element rather than to the facts document. A
// label already set by an inner aggregate is kept, so the innermost collection
// names each leaf. An unknown the aggregate's own verdict overrode is discarded
// as it is for all and any.
func (e *evaluator) withinCollection(node map[string]any, run func() tri) tri {
	collection, _ := node["path"].(string)
	causeMark, mismatchMark := len(e.causes), len(e.mismatches)
	e.collectionFailure = nil
	verdict := run()
	for index := causeMark; index < len(e.causes); index++ {
		if e.causes[index].Within == nil && e.causes[index].Path != nil {
			e.causes[index].Within = pointer(collection)
		}
	}
	for index := mismatchMark; index < len(e.mismatches); index++ {
		if e.mismatches[index].Within == nil {
			e.mismatches[index].Within = pointer(collection)
		}
	}
	// The collection pointer itself is resolved against the aggregate's own
	// root, not an element, so its failure is added after the labelling and is
	// labelled only by an enclosing aggregate, if there is one.
	if e.collectionFailure != nil {
		e.noteUnknown(*e.collectionFailure)
		e.collectionFailure = nil
	}
	return e.keepCausesIf(verdict, causeMark)
}

// all is strong three-valued conjunction (§7.1): false if any child is
// false, true if every child is true, unknown otherwise.
func (e *evaluator) all(children any, root any) tri {
	items, ok := children.([]any)
	if !ok {
		return triUnknown
	}
	sawUnknown := false
	for _, item := range items {
		switch e.condition(item, root) {
		case triFalse:
			return triFalse
		case triUnknown:
			sawUnknown = true
		}
	}
	if sawUnknown {
		return triUnknown
	}
	return triTrue
}

// any is strong three-valued disjunction (§7.2): true if any child is
// true, false if every child is false, unknown otherwise.
func (e *evaluator) any(children any, root any) tri {
	items, ok := children.([]any)
	if !ok {
		return triUnknown
	}
	sawUnknown := false
	for _, item := range items {
		switch e.condition(item, root) {
		case triTrue:
			return triTrue
		case triUnknown:
			sawUnknown = true
		}
	}
	if sawUnknown {
		return triUnknown
	}
	return triFalse
}

// evalFact selects a value from the current condition root by RFC 6901 pointer
// and applies the declared operator (§7.4). The root is the facts document
// everywhere except inside a draft RFC 0008 where, which is the whole of that
// RFC's amendment to §7.4. A pointer that does not resolve, an unsupported
// operand shape, or an incomparable ordered pair produces unknown.
func (e *evaluator) evalFact(condition map[string]any, root any) tri {
	path, ok := condition["path"].(string)
	if !ok {
		e.noteUnknown(result.UnknownCause{Cause: "unsupported"})
		return triUnknown
	}
	value, resolved := e.resolve(root, path)
	if !resolved {
		e.noteUnknown(result.UnknownCause{Path: pointer(path), Cause: "absent"})
		return triUnknown
	}
	operand := condition["value"]
	operator, _ := condition["operator"].(string)
	if orderedOperators[operator] {
		comparison, comparable := decimalCompare(value, operand)
		if !comparable {
			e.noteUnknown(result.UnknownCause{Path: pointer(path), Cause: "not-comparable", FactType: jsonType(value)})
			return triUnknown
		}
		switch operator {
		case "greater-than":
			return triFromBool(comparison > 0)
		case "greater-than-or-equal":
			return triFromBool(comparison >= 0)
		case "less-than":
			return triFromBool(comparison < 0)
		default:
			return triFromBool(comparison <= 0)
		}
	}
	switch operator {
	case "equals":
		e.noteTypeMismatch(path, operator, value, []any{operand})
		return triFromBool(jsonEqual(value, operand))
	case "not-equals":
		// §7.4: equality is total over carrier-valid JSON, so not-equals is
		// simply its Boolean inverse.
		e.noteTypeMismatch(path, operator, value, []any{operand})
		return triFromBool(!jsonEqual(value, operand))
	case "in":
		items, ok := operand.([]any)
		if !ok {
			e.noteUnknown(result.UnknownCause{Path: pointer(path), Cause: "unsupported"})
			return triUnknown
		}
		e.noteTypeMismatch(path, operator, value, items)
		for _, item := range items {
			if jsonEqual(value, item) {
				return triTrue
			}
		}
		return triFalse
	default:
		e.noteUnknown(result.UnknownCause{Path: pointer(path), Cause: "unsupported"})
		return triUnknown
	}
}

// noteTypeMismatch records an equality comparison whose fact value has a JSON
// type none of the operand values has (ADR-0040). §7.4 equality never coerces
// between types, so such a comparison could not have been equal whatever the
// values were. An in whose operand is empty states no type and records nothing.
func (e *evaluator) noteTypeMismatch(path, operator string, value any, operands []any) {
	if len(operands) == 0 {
		return
	}
	factType := jsonType(value)
	var operandTypes []string
	for _, operand := range operands {
		if kind := jsonType(operand); !slices.Contains(operandTypes, kind) {
			operandTypes = append(operandTypes, kind)
		}
	}
	if slices.Contains(operandTypes, factType) {
		return
	}
	e.mismatches = append(e.mismatches, result.TypeMismatch{
		Path: path, Operator: operator, FactType: factType, OperandTypes: operandTypes,
	})
}

// orderedOperators is §7.4's ordered-comparison operator set: the operators
// whose result decimalCompare decides and whose operand the schema pins to a
// §2.2 decimal string. It is stated here once because a second surface reads
// it — the coverage derivation walks a pack's conditions for exactly these
// operators (ADR-0023) — and a restated list in that package could disagree
// with the dispatch that uses this one.
var orderedOperators = map[string]bool{
	"greater-than":          true,
	"greater-than-or-equal": true,
	"less-than":             true,
	"less-than-or-equal":    true,
}

// OrderedOperator reports whether one authored operator is an ordered
// comparison. The set is read by a second package and is therefore exported as
// a function rather than as the map itself: an exported map variable has no
// write barrier, so any importer could assign into it and silently rewrite
// §7.4's dispatch — `evaluation.OrderedOperators["equals"] = true` would change
// what the evaluator compares, from a package that only meant to read the set.
// A function exports the answer without exporting the ability to change it.
func OrderedOperator(operator string) bool {
	return orderedOperators[operator]
}

// DecimalCompare is RFC 0006's pinned ordering as the evaluator applies it,
// exported so a surface that must judge two decimals the way §7.4 judges them
// reads this rule rather than restating a second decimal grammar: "5000.0" and
// "5000" are one value, and a JSON number on either side is not comparable at
// all. The one-statement discipline ADR-0014 applied to the reason vocabulary,
// applied to a comparison (ADR-0023).
func DecimalCompare(fact, operand any) (int, bool) {
	return decimalCompare(fact, operand)
}

// DecimalKey renders one value's §2.2 decimal identity: big.Rat's own
// canonical string for the number the value denotes, or false for anything
// §7.4 cannot compare at all — a JSON number, a grouped "5,000", a non-string.
// Two values share a key exactly when DecimalCompare judges them equal, which
// is what lets a surface grouping decimals by value fold them in one pass
// through this grammar (ADR-0023's boundary identity) instead of comparing
// every pair. It reads the same pattern and the same big.Rat parse
// DecimalCompare does, so the two cannot drift apart.
func DecimalKey(value any) (string, bool) {
	parsed, ok := decimalValue(value)
	if !ok {
		return "", false
	}
	return parsed.RatString(), true
}

// DecimalValue admits one value as a §2.2 decimal and reads it into an exact
// number, exported for the same one-statement reason DecimalKey is: a surface
// deriving new decimals from a pack's own literals — the test-row input
// generator of ADR-0024 — must admit them through the grammar §7.4 admits,
// never through big.Rat's own SetString, which reads "1/3", "1e5", and " 5 "
// as numbers §2.2 has no spelling for. The returned value is this call's own
// and the caller may do arithmetic on it.
//
// It is the reading half of the pair whose writing half is DecimalString, and
// the two are exact inverses over every value either admits.
func DecimalValue(value any) (*big.Rat, bool) {
	return decimalValue(value)
}

// DecimalString renders one exact number as a §2.2 decimal string —
// -?(0|[1-9][0-9]*)(\.[0-9]+)? — or reports false for a number that grammar
// cannot spell.
//
// It exists because DecimalKey cannot do this and must not be made to: that
// function renders big.Rat's canonical form, which is "81/2" for 40.5, so it
// is an *identity* key and never an emission. A surface emitting a decimal a
// pack's own literals imply (ADR-0024) needs the emission, and a second decimal
// writer living outside this package could spell a value the evaluator then
// declines to compare.
//
// The contract is exactness in both directions. A number whose decimal
// expansion does not terminate — any value whose denominator in lowest terms
// keeps a prime factor other than 2 or 5 — is refused rather than rounded,
// because a rounded value is a different number and a generator that quietly
// substituted one would emit an input nobody asked for. Where the expansion
// terminates the rendering carries every digit of it, at the fewest digits that
// are exact, so the result has no trailing fractional zero and DecimalValue
// reads back the number this was called with.
//
// Both halves of that contract are about the number and not about the fraction
// it arrived as, so the value is reduced before it is read. A *big.Rat the
// arithmetic here produces is already in lowest terms, but this is exported and
// a caller can hand over one that is not — big.Rat's own Num and Denom return
// references a caller may set — and 2/4 rendered off an unreduced denominator
// is "0.50", which states a precision the number does not have, while 3/3 is
// refused outright for a denominator that clears on sight. Reducing a copy
// costs one normalization and leaves the caller's value untouched.
func DecimalString(value *big.Rat) (string, bool) {
	if value == nil {
		return "", false
	}
	// A denominator of zero is no fraction at all. It cannot arise from this
	// package's own arithmetic and is checked because SetFrac would panic on
	// it, and a panic is not an answer this function is allowed to give.
	if value.Denom().Sign() == 0 {
		return "", false
	}
	value = new(big.Rat).SetFrac(value.Num(), value.Denom())
	// The scale is the smaller power of ten that clears the denominator:
	// 10^d = 2^d·5^d, so d must reach the larger of the denominator's own two
	// exponents and nothing beyond it. Taking the smallest such d is what makes
	// the rendering unique — a larger one would append zeroes that state a
	// precision the number does not have.
	denominator := new(big.Int).Set(value.Denom())
	scale := 0
	for _, factor := range []int64{2, 5} {
		divisor := big.NewInt(factor)
		exponent := 0
		for {
			quotient, remainder := new(big.Int).QuoRem(denominator, divisor, new(big.Int))
			if remainder.Sign() != 0 {
				break
			}
			denominator, exponent = quotient, exponent+1
		}
		if exponent > scale {
			scale = exponent
		}
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return "", false
	}
	scaled := new(big.Int).Mul(value.Num(), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil))
	scaled.Quo(scaled, value.Denom())
	sign := ""
	if scaled.Sign() < 0 {
		sign = "-"
		scaled.Neg(scaled)
	}
	digits := scaled.String()
	if scale == 0 {
		return sign + digits, true
	}
	// §2.2 requires an integer part, so a value under one is padded to exactly
	// the "0.…" the grammar names rather than left as ".5".
	for len(digits) <= scale {
		digits = "0" + digits
	}
	return sign + digits[:len(digits)-scale] + "." + digits[len(digits)-scale:], true
}

// ResolvePointer resolves one RFC 6901 pointer against a decoded document
// exactly as a fact condition resolves its path, exported for the same reason
// as DecimalCompare: the coverage derivation must locate a fact by the
// evaluator's own resolution, not by a second pointer walker.
func ResolvePointer(document any, pointer string) (any, bool) {
	return resolvePointer(document, pointer)
}

// PointerTokens splits one authored RFC 6901 pointer into its escape-decoded
// reference tokens, or reports false for a pointer that is neither empty nor
// rooted at "/". A surface that must *place* a value at a pointer rather than
// read one needs the tokens themselves, and ResolvePointer only reads; without
// this export that surface would carry a second implementation of ~1 and ~0,
// and a pointer the two escaped differently would place a fact where no
// condition looks for it (ADR-0024).
//
// The empty pointer selects the whole document and yields no tokens, which is
// the caller's cue that there is no member to place: a caller placing into a
// document distinguishes that case rather than treating it as an error here.
func PointerTokens(pointer string) ([]string, bool) {
	compiled := compilePointer(pointer)
	if !compiled.rooted {
		return nil, false
	}
	return compiled.tokens, true
}

// ArrayIndex reads one RFC 6901 array-reference token as an index into a
// container of the given length — decimal digits with no leading zero other
// than "0" itself, no sign, no past-the-end "-", and in range — exported for
// the same single-source-of-truth reason PointerTokens is.
//
// A surface that PLACES a value at a pointer has to admit exactly the tokens
// the resolution admits. strconv.Atoi is the tempting substitute and it is a
// wider grammar: it reads "00" and "+0" as zero, so a candidate placed at
// /items/00 lands at the element the pointer does not address, where this
// evaluator then resolves nothing and no condition ever reads it (ADR-0024).
// One rule, stated here, is what keeps a placement and a resolution from
// disagreeing about which element a pointer names.
func ArrayIndex(token string, length int) (int, bool) {
	return arrayIndex(token, length)
}

// compiledPointer is an authored RFC 6901 pointer with its bytes already read:
// the escape-decoded reference tokens, in order, and the number of bytes those
// tokens carry. Compiling is the part of a resolution whose cost is the path's
// length; walking the compiled tokens against a document is the part whose cost
// is the tokens'. Separating them is what lets one scan be charged once and
// each of many per-element resolutions be charged for what it actually does.
type compiledPointer struct {
	tokens []string
	// bytes is the total length of the decoded tokens: what one resolution
	// hashes or compares against the document it walks.
	bytes int
	// rooted reports a syntactically usable pointer: the empty string, or one
	// beginning with "/". Anything else resolves against nothing.
	rooted bool
}

// compilePointer scans one authored pointer. This is the byte-length-sensitive
// step: it reads every byte of the path, splits it, and decodes the ~1 and ~0
// escapes. The accounting model charges it before it runs.
func compilePointer(pointer string) compiledPointer {
	if pointer == "" {
		return compiledPointer{rooted: true}
	}
	if !strings.HasPrefix(pointer, "/") {
		return compiledPointer{}
	}
	raw := strings.Split(pointer[1:], "/")
	compiled := compiledPointer{tokens: make([]string, 0, len(raw)), rooted: true}
	for _, token := range raw {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		compiled.tokens = append(compiled.tokens, token)
		compiled.bytes += len(token)
	}
	return compiled
}

// resolve walks a compiled pointer against a decoded JSON document. The empty
// pointer selects the root. It reports false for any pointer that does not
// resolve, including an invalid array traversal and the "-" past-the-end token.
func (p compiledPointer) resolve(document any) (any, bool) {
	if !p.rooted {
		return nil, false
	}
	current := document
	for _, token := range p.tokens {
		switch container := current.(type) {
		case map[string]any:
			value, ok := container[token]
			if !ok {
				return nil, false
			}
			current = value
		case []any:
			index, ok := arrayIndex(token, len(container))
			if !ok {
				return nil, false
			}
			current = container[index]
		default:
			return nil, false
		}
	}
	return current, true
}

// resolve resolves one authored pointer against a document through the
// evaluator's compiled-pointer cache, so an authored path is scanned once per
// evaluation however many elements it is later resolved against. It charges
// nothing: under the draft grammar every resolution it performs was already
// reserved by the preflight, and without the draft grammar there is no budget.
func (e *evaluator) resolve(document any, pointer string) (any, bool) {
	return e.compiled(pointer).resolve(document)
}

// compiled returns the cached compilation of an authored pointer, scanning it
// if this evaluation has not seen it before.
func (e *evaluator) compiled(pointer string) compiledPointer {
	if compiled, scanned := e.pointers[pointer]; scanned {
		return compiled
	}
	compiled := compilePointer(pointer)
	if e.pointers == nil {
		e.pointers = map[string]compiledPointer{}
	}
	e.pointers[pointer] = compiled
	return compiled
}

// resolvePointer resolves an RFC 6901 JSON Pointer against a decoded JSON
// document without a cache, for callers that resolve one pointer once.
func resolvePointer(document any, pointer string) (any, bool) {
	return compilePointer(pointer).resolve(document)
}

// arrayIndex parses an RFC 6901 array-reference token: decimal digits with no
// leading zero (other than "0" itself), in range. "-" refers past the end and
// never resolves.
func arrayIndex(token string, length int) (int, bool) {
	if token == "" || token == "-" {
		return 0, false
	}
	if len(token) > 1 && token[0] == '0' {
		return 0, false
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	index, err := strconv.Atoi(token)
	if err != nil || index >= length {
		return 0, false
	}
	return index, true
}

// jsonEqual is §7.4's type-preserving JSON equality: no coercion between
// types; numbers compare by mathematical value; arrays in order; objects by
// member name and value regardless of order.
//
// It is total over carrier-valid JSON. Every pair of decoded values is equal
// or unequal, and there is no third answer: numbers are decided on their
// tokens (numbersEqual), which needs no arithmetic type big enough to hold
// them, so an evaluator's inability to represent a value never becomes a
// semantics that another evaluator would have to reproduce. §7.4's
// "incomparable values ... produce unknown" governs the ordered comparisons
// (decimalCompare), which are defined only over §2.2 decimal strings and are
// untouched by this.
func jsonEqual(a, b any) bool {
	switch left := a.(type) {
	case nil:
		return b == nil
	case bool:
		right, ok := b.(bool)
		return ok && left == right
	case string:
		right, ok := b.(string)
		return ok && left == right
	case json.Number:
		right, ok := b.(json.Number)
		return ok && numbersEqual(left, right)
	case []any:
		right, ok := b.([]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for index := range left {
			if !jsonEqual(left[index], right[index]) {
				return false
			}
		}
		return true
	case map[string]any:
		right, ok := b.(map[string]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			other, present := right[key]
			if !present || !jsonEqual(value, other) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// numbersEqual decides §7.4 equality between two JSON number tokens by
// mathematical value, in one pass over each token and without materializing
// either value. Two tokens denote the same number exactly when their normal
// forms agree — same sign, same significant digits, same adjusted exponent —
// so 1e3, 1000, and 1.0e3 are one value and -0 is 0.
//
// Identical tokens are settled on the strings, before either is normalized.
// That short-circuit is what makes a canonicalized value (canonicalNumbers)
// cheap to compare repeatedly: equal numbers reach it as byte-identical tokens,
// so a caller that compares one value against many pays one normalization per
// value rather than one per comparison.
//
// Deciding it this way is what makes equality total. Materializing the values
// (big.Rat) answers the same question only for the values the machine can
// hold: 1e999999999 against 2e999999999 would need a gigabyte of digits to
// settle by arithmetic and settles in twenty bytes on the tokens. The earlier
// arithmetic-based path reported such a pair as undeterminable and let uniform
// and fact produce unknown from it, which made a disposition depend on the
// evaluator's number type rather than on the documents. Inability is a
// resource condition, not a semantics.
func numbersEqual(left, right json.Number) bool {
	if left.String() == right.String() {
		return true
	}
	return normalizeNumber(left.String()).equals(normalizeNumber(right.String()))
}

// numberNormalForm is one JSON number token reduced to the unique triple that
// identifies its value: the sign, the significant digits with every leading
// and trailing zero stripped, and the power of ten those digits are scaled by.
// Zero has no significant digits, which is what makes -0, 0, and 0.0e10 one
// value whatever their signs and exponents. The exponent is arbitrary-precision
// because the token's is — but it is the exponent that is materialized, never
// the value, so its cost is the token's length and not the number's magnitude.
//
// malformed marks a token outside the JSON number grammar. The carrier cannot
// produce one; a hand-built json.Number can, and it compares equal to nothing
// but an identical token.
type numberNormalForm struct {
	negative  bool
	digits    string
	exponent  *big.Int
	malformed bool
}

func (n numberNormalForm) equals(other numberNormalForm) bool {
	if n.malformed || other.malformed {
		return false
	}
	if n.digits == "" || other.digits == "" {
		// Zero, which has no sign and no exponent to disagree about.
		return n.digits == other.digits
	}
	return n.negative == other.negative && n.digits == other.digits && n.exponent.Cmp(other.exponent) == 0
}

// normalizeNumber computes that normal form in one pass over the token: split
// off the sign, the exponent, and the fraction; concatenate the integer and
// fraction digits into the significand; charge the exponent one power of ten
// for every fraction digit it absorbed; then strip the leading zeros, which
// carry no value, and the trailing zeros, each of which is one more power of
// ten.
func normalizeNumber(token string) numberNormalForm {
	form := numberNormalForm{exponent: new(big.Int)}
	rest := token
	if strings.HasPrefix(rest, "-") {
		form.negative, rest = true, rest[1:]
	}
	mantissa, exponent := rest, ""
	if index := strings.IndexAny(rest, "eE"); index >= 0 {
		mantissa, exponent = rest[:index], rest[index+1:]
	}
	integer, fraction := mantissa, ""
	if index := strings.IndexByte(mantissa, '.'); index >= 0 {
		integer, fraction = mantissa[:index], mantissa[index+1:]
	}
	if exponent != "" {
		if _, ok := form.exponent.SetString(exponent, 10); !ok {
			return numberNormalForm{malformed: true}
		}
	}
	digits := integer + fraction
	for index := 0; index < len(digits); index++ {
		if digits[index] < '0' || digits[index] > '9' {
			return numberNormalForm{malformed: true}
		}
	}
	form.exponent.Sub(form.exponent, big.NewInt(int64(len(fraction))))
	leading := 0
	for leading < len(digits) && digits[leading] == '0' {
		leading++
	}
	digits = digits[leading:]
	significant := len(digits)
	for significant > 0 && digits[significant-1] == '0' {
		significant--
	}
	form.exponent.Add(form.exponent, big.NewInt(int64(len(digits)-significant)))
	form.digits = digits[:significant]
	return form
}

// token writes a normal form back out as the one JSON number token that denotes
// its value: "0" for zero, the significant digits alone when they need no
// scaling, and the digits followed by "e" and the power of ten otherwise. It is
// injective on normal forms — two tokens denote the same number exactly when
// their canonical tokens are byte-identical — and normalizing a canonical token
// reproduces the same form, so canonicalizing twice changes nothing. Its length
// is within a small constant of the token it came from: the digits are a subset
// of the original's and the exponent is at most the original's exponent digits
// plus the fraction's length.
func (n numberNormalForm) token() string {
	if n.digits == "" {
		return "0" // Zero, which has neither a sign nor an exponent.
	}
	text := n.digits
	if n.negative {
		text = "-" + text
	}
	if n.exponent.Sign() == 0 {
		return text
	}
	return text + "e" + n.exponent.String()
}

// canonicalNumbers deep-copies a value with every number token replaced by the
// canonical token of its normal form, leaving every other leaf as it is. It
// costs one normalization per number, once, and it is what stops a value that
// is compared many times from being normalized once per comparison: afterwards
// two equal numbers are byte-identical tokens, which numbersEqual settles on
// the strings before it normalizes anything.
//
// §7.4 equality is unchanged by it, in both directions: canonicalization maps
// equal values to identical tokens and unequal values to different ones, so a
// comparison between canonical forms answers exactly what a comparison between
// the original tokens answers. uniform is the caller — it compares one elected
// representative against every other member, which is the shape that turns a
// per-comparison normalization into a reread of the longest token per member.
//
// A token outside the JSON number grammar has no normal form and is returned
// untouched, so it goes on comparing equal to an identical token and to nothing
// else.
func canonicalNumbers(value any) any {
	switch typed := value.(type) {
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = canonicalNumbers(item)
		}
		return items
	case map[string]any:
		members := make(map[string]any, len(typed))
		for name, item := range typed {
			members[name] = canonicalNumbers(item)
		}
		return members
	case json.Number:
		form := normalizeNumber(typed.String())
		if form.malformed {
			return typed
		}
		return json.Number(form.token())
	default:
		return value
	}
}

// decimalPattern is the §2.2 decimal grammar: an optional minus, an integer
// part with no leading zero, and an optional fraction. Exponents, leading
// plus signs, leading zeroes, NaN, and infinities are not admitted.
var decimalPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// decimalCompare implements RFC 0006's pinned ordering: an ordered comparison
// is defined iff both values are JSON strings conforming to the §2.2 decimal
// grammar, compared by mathematical value. Any other value — including a JSON
// number — is not comparable.
func decimalCompare(fact, operand any) (int, bool) {
	factValue, ok := decimalValue(fact)
	if !ok {
		return 0, false
	}
	operandValue, ok := decimalValue(operand)
	if !ok {
		return 0, false
	}
	return factValue.Cmp(operandValue), true
}

// decimalValue is the one place a value is admitted as a §2.2 decimal and read
// into a number: the grammar check and the parse together, so every surface
// that asks about a decimal — the comparison, the identity key — asks the same
// question of the same tokens.
// DecimalReadings counts every reading of a value through this grammar --
// a comparison reads two, a key or a value one -- so a surface that claims
// to read each decimal once can be held to it by a test.
var DecimalReadings atomic.Int64

func decimalValue(value any) (*big.Rat, bool) {
	DecimalReadings.Add(1)
	text, ok := value.(string)
	if !ok || !decimalPattern.MatchString(text) {
		return nil, false
	}
	return new(big.Rat).SetString(text)
}
