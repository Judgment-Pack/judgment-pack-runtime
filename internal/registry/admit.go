package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
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
	walk(value, nil, func(path []string, item any) {
		if number, ok := item.(json.Number); ok && !admittedNumber(number) {
			at := location
			at.Pointer = carrier.Pointer(path)
			diagnostics.add("REG-DOC-NUMBER", at)
			bad = true
		}
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
// Big arithmetic is quadratic in a literal's digits, and builds 10^N for an
// exponent N, so a literal is first held to cheap bounds that only refuse
// what the rule refuses anyway:
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
// What passes is compared exactly, on a reduced literal of the same value:
// the significant digits and the exponent that places them.
func admittedNumber(number json.Number) bool {
	negative, integer, fraction, exponent, ok := splitJSONNumber(number.String())
	if !ok {
		return false
	}
	sign := ""
	if negative {
		sign = "-"
	}
	if fraction == "" && exponent == "" {
		if integer == "0" {
			return !negative
		}
		if len(integer) > maxSafeIntegerDigits {
			return false
		}
		magnitude, ok := new(big.Int).SetString(integer, 10)
		return ok && magnitude.Cmp(big.NewInt(maxSafeInteger)) <= 0
	}
	digits := integer + fraction
	lead := strings.IndexFunc(digits, func(r rune) bool { return r != '0' })
	if lead < 0 {
		// Zero, whatever its exponent.
		return !negative
	}
	trail := strings.LastIndexFunc(digits, func(r rune) bool { return r != '0' })
	significant := digits[lead : trail+1]
	if len(significant) > maxShortestDigits {
		return false
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
	if leading := int64(len(integer)-lead-1) + power; leading < minShortestExponent || leading > maxShortestExponent {
		return false
	}
	reduced := sign + significant + "e" + strconv.FormatInt(int64(len(integer)-trail-1)+power, 10)
	exact, ok := new(big.Rat).SetString(reduced)
	if !ok {
		return false
	}
	double, err := strconv.ParseFloat(reduced, 64)
	if err != nil {
		return false
	}
	shortest, ok := new(big.Rat).SetString(strconv.FormatFloat(double, 'g', -1, 64))
	return ok && shortest.Cmp(exact) == 0
}

// The bounds admittedNumber checks before big arithmetic.
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

// walk visits every value in document order, objects' members in sorted
// order so that diagnostics are produced deterministically.
func walk(value any, path []string, visit func([]string, any)) {
	visit(path, value)
	switch typed := value.(type) {
	case map[string]any:
		for _, name := range sortedKeys(typed) {
			walk(typed[name], appendPath(path, name), visit)
		}
	case []any:
		for index, item := range typed {
			walk(item, appendPath(path, strconv.Itoa(index)), visit)
		}
	}
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
