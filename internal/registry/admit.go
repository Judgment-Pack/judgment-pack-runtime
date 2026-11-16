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
	if len(data) > maxBytes {
		diagnostics.add("REG-DOC-SIZE", location)
		return nil
	}
	value, failure := carrier.Decode(data, carrier.Limits{MaxDepth: MaxDepth, MaxNodes: maxBytes, MaxStringBytes: maxBytes})
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
			// The node and string limits are set to the size bound, which a
			// document within that bound cannot exceed.
			diagnostics.add("REG-DOC-SIZE", location)
		}
		return nil
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

// admittedNumber applies RFC 0034 §1: an integer of magnitude at most 2^53-1,
// or a decimal whose exact value equals that of the shortest decimal reading
// back as the same double; never negative zero.
func admittedNumber(number json.Number) bool {
	literal := number.String()
	exact, ok := new(big.Rat).SetString(literal)
	if !ok {
		return false
	}
	if exact.Sign() == 0 && strings.HasPrefix(literal, "-") {
		return false
	}
	if !strings.ContainsAny(literal, ".eE") {
		limit := big.NewRat(maxSafeInteger, 1)
		return new(big.Rat).Abs(exact).Cmp(limit) <= 0
	}
	double, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		return false
	}
	shortest, ok := new(big.Rat).SetString(strconv.FormatFloat(double, 'g', -1, 64))
	return ok && shortest.Cmp(exact) == 0
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
