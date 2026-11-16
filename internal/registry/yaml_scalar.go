package registry

import (
	"encoding/json"
	"regexp"
	"strings"
)

// The plain scalars RFC 0034 §7 reads as something other than a string, in
// JSON's own spelling: an integer with no leading zero and no sign but a
// minus, and a decimal with a fraction and no exponent.
var (
	yamlInteger = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	yamlDecimal = regexp.MustCompile(`^-?(0|[1-9][0-9]*)\.[0-9]+$`)
)

// yamlOtherReadings matches every plain scalar that YAML 1.1's types, the
// YAML 1.2 core schema or go-yaml v3 read as something other than a string
// (ADR-0094). A scalar that matches and is not one of §7's own forms is
// refused, because a YAML tool would read it as another value than import
// does. go-yaml's numbers, which it reads after dropping every underscore,
// are yamlLibraryNumber's.
var yamlOtherReadings = regexp.MustCompile(`^(?:` + strings.Join([]string{
	// YAML 1.1 booleans, and the 1.2 core schema's capitalized ones.
	`y|Y|yes|Yes|YES|n|N|no|No|NO|true|True|TRUE|false|False|FALSE|on|On|ON|off|Off|OFF`,
	// Null in both.
	`~|null|Null|NULL`,
	// YAML 1.1 integers: binary, octal, decimal with separators, hexadecimal,
	// sexagesimal.
	`[-+]?0b[01_]+`, `[-+]?0[0-7_]+`, `[-+]?(?:0|[1-9][0-9_]*)`, `[-+]?0x[0-9a-fA-F_]+`, `[-+]?[1-9][0-9_]*(?::[0-5]?[0-9])+`,
	// YAML 1.2 core integers: any decimal digits with a sign, octal, hexadecimal.
	`[-+]?[0-9]+`, `0o[0-7]+`, `0x[0-9a-fA-F]+`,
	// YAML 1.1 floats as its resolvers read them: no second dot.
	`[-+]?(?:[0-9][0-9_]*)?\.[0-9_]*(?:[eE][-+][0-9]+)?`, `[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*`,
	// YAML 1.2 core floats.
	`[-+]?(?:\.[0-9]+|[0-9]+(?:\.[0-9]*)?)(?:[eE][-+]?[0-9]+)?`,
	// Infinity and not-a-number in both.
	`[-+]?\.(?:inf|Inf|INF)`, `\.(?:nan|NaN|NAN)`,
	// Timestamps: a date, or a date and a time. YAML 1.1 writes a date's
	// month and day with two digits and a time's minute and second with two;
	// go-yaml v3 also reads one digit in each.
	`[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}`,
	`[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}(?:[Tt]|[ \t]+)[0-9]{1,2}:[0-9]{1,2}:[0-9]{1,2}(?:\.[0-9]*)?(?:[ \t]*(?:Z|[-+][0-9]{1,2}(?::[0-9]{2})?))?`,
	// YAML 1.1's merge key and value key.
	`<<`, `=`,
}, "|") + `)$`)

// yamlLibraryNumber matches, once every underscore is dropped, the numbers
// go-yaml v3 reads from a plain scalar that starts with a digit or a sign: an
// integer in any radix Go's own syntax has, its prefix in either case, and a
// float. go-yaml drops underscores anywhere in such a scalar, so 1_0e5 and
// 0o1_7 are numbers to it.
var yamlLibraryNumber = regexp.MustCompile(`^[-+]?(?:0[bB][01]+|0[oO][0-7]+|0[xX][0-9a-fA-F]+|[0-9]+|(?:\.[0-9]+|[0-9]+(?:\.[0-9]*)?)(?:[eE][-+]?[0-9]+)?)$`)

// resolvePlain reads a plain scalar by RFC 0034 §7: true, false and null; an
// integer or a decimal held to §1's number rules; any other scalar a string,
// unless YAML 1.1 or the 1.2 core schema reads it otherwise. It returns the
// value and the code of a refusal, or "".
func resolvePlain(text string) (any, string) {
	switch text {
	case "true":
		return true, ""
	case "false":
		return false, ""
	case "null":
		return nil, ""
	}
	if yamlInteger.MatchString(text) {
		if !admittedNumber(json.Number(text)) {
			return nil, "REG-DOC-NUMBER"
		}
		return json.Number(text), ""
	}
	if yamlDecimal.MatchString(text) {
		if !admittedNumber(json.Number(text)) {
			return nil, "REG-DOC-NUMBER"
		}
		return json.Number(text), ""
	}
	if yamlOtherReadings.MatchString(text) || readsAsLibraryNumber(text) {
		return nil, "REG-YAML-SCALAR"
	}
	return text, ""
}

// readsAsLibraryNumber says whether go-yaml v3 reads a plain scalar as a
// number: one that starts with a digit or a sign and, without its
// underscores, is a number in yamlLibraryNumber's grammar.
func readsAsLibraryNumber(text string) bool {
	if text == "" || !strings.ContainsRune("+-0123456789", rune(text[0])) {
		return false
	}
	return yamlLibraryNumber.MatchString(strings.ReplaceAll(text, "_", ""))
}

// plainKeyCode is the refusal of a plain scalar used as a key, or "": a key
// must read as a string.
func plainKeyCode(text string) string {
	if text == "<<" {
		return "REG-YAML-MERGE"
	}
	value, code := resolvePlain(text)
	switch {
	case code == "REG-YAML-SCALAR":
		return code
	case code != "":
		return "REG-YAML-KEY"
	}
	if _, isString := value.(string); !isString {
		return "REG-YAML-KEY"
	}
	return ""
}
