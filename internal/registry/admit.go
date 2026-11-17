package registry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// Size bounds (RFC 0034 §1).
const (
	MaxKindBytes     = 256 * 1024
	MaxRegistryBytes = 8 * 1024 * 1024
	MaxResolvedBytes = 8 * 1024 * 1024
	MaxDepth         = 32
	maxSafeInteger   = 9007199254740991
)

// admit runs step 1 on one document: the size bound, strict I-JSON within the
// depth bound, the number rules, and the exact selector. It returns the
// decoded root object, or nil after recording why not.
func admit(data []byte, maxBytes int, selectorMember, selector string, location Location, diagnostics *collector) map[string]any {
	value, admitted := admitValue(data, maxBytes, maxBytes, location, diagnostics)
	if !admitted {
		return nil
	}
	root, ok := value.(map[string]any)
	if !ok {
		diagnostics.add("REG-DOC-SELECTOR", location)
		return nil
	}
	if got, ok := root[selectorMember].(string); !ok || got != selector {
		at := location
		at.Pointer = "/" + selectorMember
		diagnostics.add("REG-DOC-SELECTOR", at)
		return nil
	}
	return root
}

// admitValue is admit without the selector: the size bound, strict I-JSON
// within the depth bound and at most maxNodes JSON values, and the number
// rules. It returns the decoded value and true, or false after recording why
// not.
func admitValue(data []byte, maxBytes, maxNodes int, location Location, diagnostics *collector) (any, bool) {
	if len(data) > maxBytes {
		diagnostics.add("REG-DOC-SIZE", location)
		return nil, false
	}
	value, failure := carrier.Decode(data, carrier.Limits{MaxDepth: MaxDepth, MaxNodes: maxNodes, MaxStringBytes: maxBytes})
	if failure != nil {
		at := location
		at.Pointer = failure.Diagnostic.InstancePath
		switch failure.Diagnostic.Code {
		case "JPS-RESOURCE-DEPTH-LIMIT":
			diagnostics.add("REG-DOC-DEPTH", at)
		case "JPS-CARRIER-DUPLICATE-MEMBER":
			diagnostics.add("REG-DOC-DUPLICATE", at)
		case "JPS-CARRIER-INVALID-JSON":
			if _, found := carrier.UnpairedSurrogateEscape(data); found && json.Valid(data) {
				diagnostics.add("REG-DOC-STRING", location)
			} else {
				diagnostics.add("REG-DOC-JSON", location)
			}
		default:
			// The string limit is the size bound, which a document within
			// that bound cannot exceed; the node limit is the size bound too,
			// or a lower one a caller sets, which refuses the document as it
			// is decoded.
			diagnostics.add("REG-DOC-SIZE", at)
		}
		return nil, false
	}
	bad := false
	value = admitNumbers(value, func(path []string) {
		at := location
		at.Pointer = carrier.Pointer(path)
		diagnostics.add("REG-DOC-NUMBER", at)
		bad = true
	})
	if bad {
		return nil, false
	}
	return value, true
}

// admittedNumber applies RFC 0034 §1: an integer of magnitude at most 2^53-1,
// or a decimal whose exact value equals that of the shortest decimal reading
// back as the same double; never negative zero.
//
// A literal is first held to cheap bounds that only refuse what the rule
// refuses anyway, so that the work on one is bounded whatever its length:
//   - an integer has at most 16 digits, those of 2^53-1 (JSON allows no
//     leading zero, so every digit counts);
//   - a decimal has at most 17 significant digits once the significand's
//     leading and trailing zeros are dropped, because the shortest decimal
//     that reads back as a double never has more;
//   - a nonzero decimal's leading digit is at 10^X with -324 <= X <= 308,
//     because the shortest decimal of a finite nonzero double lies between
//     that of the smallest subnormal, 5e-324, and that of the largest finite
//     double, 1.7976931348623157e308.
//
// What passes is compared exactly, with no big arithmetic, which costs
// hundreds of bytes a literal and builds 10^N for an exponent N: the
// significant digits and the exponent that places the leading one are
// compared with the double's shortest decimal, digit for digit. Neither has a
// leading or a trailing zero, so two decimals of one value have the same
// digits and the same exponent.
func admittedNumber(number json.Number) bool {
	_, ok := admittedLiteral(number)
	return ok
}

// admittedLiteral applies admittedNumber's rule and returns the literal
// admission keeps: an integer as written, which is at most 16 digits, and any
// other number as the RFC 8785 form of its value, the shortest decimal that
// reads back as the same double, which the rule makes exactly its value. So
// trailing fraction zeros, leading zeros the exponent moves past, and an
// exponent's own leading zeros never outlive admission, and every later step
// (a range check, an equality, a facts mapping) parses a literal of at most
// 25 bytes, however long the one in the document was.
func admittedLiteral(number json.Number) (json.Number, bool) {
	literal := number.String()
	negative, integer, fraction, exponent, ok := splitJSONNumber(literal)
	if !ok {
		return "", false
	}
	if fraction == "" && exponent == "" {
		if integer == "0" {
			return "0", !negative
		}
		if len(integer) > maxSafeIntegerDigits {
			return "", false
		}
		magnitude, err := strconv.ParseUint(integer, 10, 64)
		return number, err == nil && magnitude <= maxSafeInteger
	}
	// The digits of the integer and the fraction, read as one string.
	digit := func(i int) byte {
		if i < len(integer) {
			return integer[i]
		}
		return fraction[i-len(integer)]
	}
	total := len(integer) + len(fraction)
	lead := 0
	for lead < total && digit(lead) == '0' {
		lead++
	}
	if lead == total {
		// Zero, whatever its exponent.
		return "0", !negative
	}
	trail := total - 1
	for digit(trail) == '0' {
		trail--
	}
	if trail-lead+1 > maxShortestDigits {
		return "", false
	}
	// The exponent saturates far past the bound below: a literal within the
	// size bound moves the point by fewer than 2^40 places.
	var power int64
	for _, digit := range strings.TrimLeft(exponent, "+-") {
		power = min(power*10+int64(digit-'0'), 1<<40)
	}
	if strings.HasPrefix(exponent, "-") {
		power = -power
	}
	leading := int64(len(integer)-lead-1) + power
	if leading < minShortestExponent || leading > maxShortestExponent {
		return "", false
	}
	// The reduced literal: the significant digits and the exponent that
	// places the last one, of at most 25 bytes.
	var reducedScratch [32]byte
	reduced := reducedScratch[:0]
	if negative {
		reduced = append(reduced, '-')
	}
	for i := lead; i <= trail; i++ {
		reduced = append(reduced, digit(i))
	}
	reduced = append(reduced, 'e')
	reduced = strconv.AppendInt(reduced, int64(len(integer)-trail-1)+power, 10)
	double, err := strconv.ParseFloat(string(reduced), 64)
	if err != nil {
		return "", false
	}
	// The double's shortest decimal, "d.ddde±x".
	var shortestScratch [32]byte
	shortest := strconv.AppendFloat(shortestScratch[:0], math.Abs(double), 'e', -1, 64)
	at := bytes.IndexByte(shortest, 'e')
	if at < 1 {
		return "", false
	}
	shortestExponent, err := strconv.ParseInt(string(shortest[at+1:]), 10, 64)
	if err != nil || shortestExponent != leading {
		return "", false
	}
	var digitsScratch [32]byte
	shortestDigits := digitsScratch[:0]
	for _, c := range shortest[:at] {
		if c != '.' {
			shortestDigits = append(shortestDigits, c)
		}
	}
	// Shortest digits have no trailing zero; dropping any keeps the
	// comparison exact all the same.
	shortestDigits = bytes.TrimRight(shortestDigits, "0")
	if len(shortestDigits) != trail-lead+1 {
		return "", false
	}
	for i, want := range shortestDigits {
		if digit(lead+i) != want {
			return "", false
		}
	}
	var canonicalScratch [32]byte
	canonical, err := jcs.AppendNumber(canonicalScratch[:0], double)
	if err != nil {
		return "", false
	}
	if string(canonical) == literal {
		return number, true
	}
	return json.Number(canonical), true
}

// The bounds admittedNumber checks before it compares digits.
const (
	maxSafeIntegerDigits = 16   // the digits of 2^53-1
	maxShortestDigits    = 17   // the most a shortest round-trip decimal of a double has
	minShortestExponent  = -324 // the leading digit's exponent in 5e-324, the smallest subnormal
	maxShortestExponent  = 308  // the leading digit's exponent in 1.7976931348623157e308
)

// splitJSONNumber splits a literal by JSON's number grammar,
// -?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][-+]?[0-9]+)?, into its sign, integer
// digits, fraction digits and exponent (its sign and digits), in one scan.
func splitJSONNumber(literal string) (negative bool, integer, fraction, exponent string, ok bool) {
	rest := literal
	if strings.HasPrefix(rest, "-") {
		negative, rest = true, rest[1:]
	}
	digits := func(s string) int {
		n := 0
		for n < len(s) && s[n] >= '0' && s[n] <= '9' {
			n++
		}
		return n
	}
	n := digits(rest)
	if n == 0 || n > 1 && rest[0] == '0' {
		return false, "", "", "", false
	}
	integer, rest = rest[:n], rest[n:]
	if strings.HasPrefix(rest, ".") {
		n = digits(rest[1:])
		if n == 0 {
			return false, "", "", "", false
		}
		fraction, rest = rest[1:1+n], rest[1+n:]
	}
	if strings.HasPrefix(rest, "e") || strings.HasPrefix(rest, "E") {
		signed := 1
		if len(rest) > 1 && (rest[1] == '+' || rest[1] == '-') {
			signed = 2
		}
		n = digits(rest[signed:])
		if n == 0 {
			return false, "", "", "", false
		}
		exponent, rest = rest[1:signed+n], rest[signed+n:]
	}
	if rest != "" {
		return false, "", "", "", false
	}
	return negative, integer, fraction, exponent, true
}

// admitNumbers visits every value in document order, objects' members in
// sorted order so that diagnostics are produced deterministically. It reports
// each number admission refuses by its path, and returns the value with each
// admitted number replaced by the literal admission keeps (admittedLiteral).
// A path is built only for a refusal, and a number whose literal is kept as
// written is not copied, so an admitted value costs no more than its own
// numbers' changed literals.
func admitNumbers(value any, refuse func([]string)) any {
	walk := &numberWalk{steps: make([]numberStep, 0, MaxDepth+1), refuse: refuse}
	return walk.visit(value)
}

// numberWalk is admitNumbers' walk: the steps to the value being visited and
// where a refusal goes.
type numberWalk struct {
	steps  []numberStep
	refuse func([]string)
}

// numberStep is one step of a path: a member's name, or an array's index
// when index is not negative.
type numberStep struct {
	name  string
	index int
}

func (w *numberWalk) visit(value any) any {
	switch typed := value.(type) {
	case json.Number:
		literal, ok := admittedLiteral(typed)
		if !ok {
			w.refuse(w.path())
			return value
		}
		if literal == typed {
			return value
		}
		return literal
	case map[string]any:
		for _, name := range sortedKeys(typed) {
			w.steps = append(w.steps, numberStep{name: name, index: -1})
			typed[name] = w.visit(typed[name])
			w.steps = w.steps[:len(w.steps)-1]
		}
	case []any:
		for index, item := range typed {
			w.steps = append(w.steps, numberStep{index: index})
			typed[index] = w.visit(item)
			w.steps = w.steps[:len(w.steps)-1]
		}
	}
	return value
}

// path is the path of the value being visited, as tokens.
func (w *numberWalk) path() []string {
	path := make([]string, len(w.steps))
	for i, step := range w.steps {
		path[i] = step.name
		if step.index >= 0 {
			path[i] = strconv.Itoa(step.index)
		}
	}
	return path
}

func appendPath(path []string, token string) []string {
	out := make([]string, len(path), len(path)+1)
	copy(out, path)
	return append(out, token)
}

// Digest returns "sha256:" and the hex SHA-256 of value's RFC 8785 form.
func Digest(value any) (string, error) {
	canonical, err := jcs.EncodeDocument(value)
	if err != nil {
		return "", err
	}
	return rawDigest(canonical), nil
}

func rawDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
