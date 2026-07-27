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

// evalCondition applies §7's experimental interpretation, as pinned by spec
// RFC 0006, to one condition node. The pack has full document conformance, so
// node shapes are schema-guaranteed; anything unexpected still degrades to
// unknown ("unsupported operators or operand shapes ... produce unknown").
func evalCondition(node any, facts any, evidence map[string]tri) tri {
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
		return evalAll(condition["conditions"], facts, evidence)
	case "any":
		return evalAny(condition["conditions"], facts, evidence)
	case "not":
		switch evalCondition(condition["condition"], facts, evidence) {
		case triTrue:
			return triFalse
		case triFalse:
			return triTrue
		default:
			return triUnknown
		}
	case "fact":
		return evalFact(condition, facts)
	case "evidence-present":
		name, ok := condition["evidenceRequirement"].(string)
		if !ok {
			return triUnknown
		}
		presence, declared := evidence[name]
		if !declared {
			return triUnknown
		}
		return presence
	default:
		return triUnknown
	}
}

// evalAll is strong three-valued conjunction (§7.1): false if any child is
// false, true if every child is true, unknown otherwise.
func evalAll(children any, facts any, evidence map[string]tri) tri {
	items, ok := children.([]any)
	if !ok {
		return triUnknown
	}
	sawUnknown := false
	for _, item := range items {
		switch evalCondition(item, facts, evidence) {
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

// evalAny is strong three-valued disjunction (§7.2): true if any child is
// true, false if every child is false, unknown otherwise.
func evalAny(children any, facts any, evidence map[string]tri) tri {
	items, ok := children.([]any)
	if !ok {
		return triUnknown
	}
	sawUnknown := false
	for _, item := range items {
		switch evalCondition(item, facts, evidence) {
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

// evalFact selects a value from the facts document by RFC 6901 pointer and
// applies the declared operator (§7.4). A pointer that does not resolve, an
// unsupported operand shape, or an incomparable ordered pair produces unknown.
func evalFact(condition map[string]any, facts any) tri {
	path, ok := condition["path"].(string)
	if !ok {
		return triUnknown
	}
	value, resolved := resolvePointer(facts, path)
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

// resolvePointer resolves an RFC 6901 JSON Pointer against a decoded JSON
// document. The empty string selects the root. It reports false for any
// pointer that does not resolve, including an invalid array traversal and the
// "-" past-the-end token.
func resolvePointer(document any, pointer string) (any, bool) {
	if pointer == "" {
		return document, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	current := document
	for _, token := range strings.Split(pointer[1:], "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
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
