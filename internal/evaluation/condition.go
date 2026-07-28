package evaluation

import (
	"encoding/json"
	"math/big"
	"regexp"
	"strconv"
	"strings"
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
	budget      int
	charged     int
	exceeded    bool
	// pointers memoizes the compiled form of every authored pointer this
	// evaluation has resolved, keyed by the pointer text as the pack wrote it.
	// Compiling scans the path's bytes; the cache is what makes a per-element
	// resolution cost its steps rather than a fresh scan, and the accounting
	// model on evaluate charges each scan exactly once to match.
	pointers map[string]compiledPointer
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
		return triUnknown
	}
	switch condition["op"] {
	case "literal":
		if value, ok := condition["value"].(bool); ok {
			return triFromBool(value)
		}
		return triUnknown
	case "all":
		return e.all(condition["conditions"], root)
	case "any":
		return e.any(condition["conditions"], root)
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
			return triUnknown
		}
		presence, declared := e.evidence[name]
		if !declared {
			return triUnknown
		}
		return presence
	case "exists", "every":
		if !e.quantifiers {
			return triUnknown
		}
		return e.quantify(condition, root)
	case "uniform":
		if !e.quantifiers {
			return triUnknown
		}
		return e.uniform(condition, root)
	default:
		return triUnknown
	}
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
		return triUnknown
	}
	value, resolved := e.resolve(root, path)
	if !resolved {
		return triUnknown
	}
	operand := condition["value"]
	switch condition["operator"] {
	case "equals":
		equal, determinable := jsonEqual(value, operand)
		if !determinable {
			return triUnknown
		}
		return triFromBool(equal)
	case "not-equals":
		// §7.4: not-equals is the Boolean inverse of equals only when equality
		// can be determined.
		equal, determinable := jsonEqual(value, operand)
		if !determinable {
			return triUnknown
		}
		return triFromBool(!equal)
	case "in":
		items, ok := operand.([]any)
		if !ok {
			return triUnknown
		}
		sawUndeterminable := false
		for _, item := range items {
			equal, determinable := jsonEqual(value, item)
			if !determinable {
				sawUndeterminable = true
				continue
			}
			if equal {
				return triTrue
			}
		}
		if sawUndeterminable {
			return triUnknown
		}
		return triFalse
	case "greater-than", "greater-than-or-equal", "less-than", "less-than-or-equal":
		comparison, comparable := decimalCompare(value, operand)
		if !comparable {
			return triUnknown
		}
		switch condition["operator"] {
		case "greater-than":
			return triFromBool(comparison > 0)
		case "greater-than-or-equal":
			return triFromBool(comparison >= 0)
		case "less-than":
			return triFromBool(comparison < 0)
		default:
			return triFromBool(comparison <= 0)
		}
	default:
		return triUnknown
	}
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
// member name and value regardless of order. The second result reports
// whether equality could be determined at all: a syntactically valid JSON
// number the arithmetic cannot represent (for example an exponent beyond
// big.Rat's range) makes the comparison undeterminable rather than silently
// false — §7.4: "incomparable values ... produce unknown".
func jsonEqual(a, b any) (bool, bool) {
	switch left := a.(type) {
	case nil:
		return b == nil, true
	case bool:
		right, ok := b.(bool)
		return ok && left == right, true
	case string:
		right, ok := b.(string)
		return ok && left == right, true
	case json.Number:
		right, ok := b.(json.Number)
		if !ok {
			return false, true
		}
		if left.String() == right.String() {
			// Identical tokens are equal regardless of representability.
			return true, true
		}
		leftRat, leftOK := new(big.Rat).SetString(left.String())
		rightRat, rightOK := new(big.Rat).SetString(right.String())
		if !leftOK || !rightOK {
			return false, false
		}
		return leftRat.Cmp(rightRat) == 0, true
	case []any:
		right, ok := b.([]any)
		if !ok || len(left) != len(right) {
			return false, true
		}
		sawUndeterminable := false
		for i := range left {
			equal, determinable := jsonEqual(left[i], right[i])
			if !determinable {
				sawUndeterminable = true
				continue
			}
			if !equal {
				return false, true
			}
		}
		if sawUndeterminable {
			return false, false
		}
		return true, true
	case map[string]any:
		right, ok := b.(map[string]any)
		if !ok || len(left) != len(right) {
			return false, true
		}
		sawUndeterminable := false
		for key, value := range left {
			other, present := right[key]
			if !present {
				return false, true
			}
			equal, determinable := jsonEqual(value, other)
			if !determinable {
				sawUndeterminable = true
				continue
			}
			if !equal {
				return false, true
			}
		}
		if sawUndeterminable {
			return false, false
		}
		return true, true
	default:
		return false, true
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
	factText, ok := fact.(string)
	if !ok || !decimalPattern.MatchString(factText) {
		return 0, false
	}
	operandText, ok := operand.(string)
	if !ok || !decimalPattern.MatchString(operandText) {
		return 0, false
	}
	factValue, factOK := new(big.Rat).SetString(factText)
	operandValue, operandOK := new(big.Rat).SetString(operandText)
	if !factOK || !operandOK {
		return 0, false
	}
	return factValue.Cmp(operandValue), true
}
