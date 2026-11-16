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

// yamlOtherReadings matches every plain scalar that YAML 1.1's types or the
// YAML 1.2 core schema read as something other than a string (ADR-0094). A
// scalar that matches and is not one of §7's own forms is refused, because a
// YAML tool would read it as another value than import does.
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
	// YAML 1.1 timestamps: a date, or a date and a time.
	`[0-9]{4}-[0-9]{2}-[0-9]{2}`,
	`[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}(?:[Tt]|[ \t]+)[0-9]{1,2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]*)?(?:[ \t]*(?:Z|[-+][0-9]{1,2}(?::[0-9]{2})?))?`,
	// YAML 1.1's merge key and value key.
	`<<`, `=`,
}, "|") + `)$`)

// maxSafeDigits is the number of digits of 2^53-1; an integer with more is
// out of range whatever its digits, which spares big arithmetic on a long one.
const maxSafeDigits = 16

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
		if len(strings.TrimPrefix(text, "-")) > maxSafeDigits || !admittedNumber(json.Number(text)) {
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
	if yamlOtherReadings.MatchString(text) {
		return nil, "REG-YAML-SCALAR"
	}
	return text, ""
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
