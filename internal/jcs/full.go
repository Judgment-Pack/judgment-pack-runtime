package jcs

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// EncodeDocument returns the RFC 8785 canonical form of a whole JSON document:
// the value space of Encode plus null and numbers. It exists for documents
// whose identity bytes are their JCS form and that may hold numbers, such as
// the registry companion's documents (spec RFC 0034 §1). Encode keeps refusing
// numbers and null, because the disposition it serves must never acquire one.
//
// Numbers are admitted as json.Number (as carrier.Decode produces them) or
// float64, and written as RFC 8785 §3.2.2.3 says: the shortest decimal that
// round-trips through an IEEE 754 double, in ECMAScript's Number::toString
// layout. Negative zero is written as 0. A literal that does not parse, or is
// not finite, is refused.
func EncodeDocument(value any) ([]byte, error) {
	var builder strings.Builder
	if err := writeDocument(&builder, value); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}

func writeDocument(builder *strings.Builder, value any) error {
	switch typed := value.(type) {
	case nil:
		builder.WriteString("null")
		return nil
	case json.Number:
		number, err := strconv.ParseFloat(typed.String(), 64)
		if err != nil {
			return fmt.Errorf("jcs: number %q is outside the IEEE 754 double range", typed.String())
		}
		return writeNumber(builder, number)
	case float64:
		return writeNumber(builder, typed)
	case []any:
		builder.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				builder.WriteByte(',')
			}
			if err := writeDocument(builder, item); err != nil {
				return err
			}
		}
		builder.WriteByte(']')
		return nil
	case map[string]any:
		names := make([]string, 0, len(typed))
		for name := range typed {
			names = append(names, name)
		}
		slices.SortFunc(names, compareUTF16)
		builder.WriteByte('{')
		for index, name := range names {
			if index > 0 {
				builder.WriteByte(',')
			}
			if err := writeString(builder, name); err != nil {
				return err
			}
			builder.WriteByte(':')
			if err := writeDocument(builder, typed[name]); err != nil {
				return err
			}
		}
		builder.WriteByte('}')
		return nil
	default:
		return write(builder, value)
	}
}

// FormatNumber writes a finite double as RFC 8785 §3.2.2.3 requires.
func FormatNumber(number float64) (string, error) {
	var builder strings.Builder
	if err := writeNumber(&builder, number); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func writeNumber(builder *strings.Builder, number float64) error {
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return fmt.Errorf("jcs: a number that is not finite has no canonical form")
	}
	if number == 0 {
		builder.WriteByte('0')
		return nil
	}
	if number < 0 {
		builder.WriteByte('-')
		number = -number
	}
	// Shortest round-trip digits, as "d.ddde±x".
	scientific := strconv.FormatFloat(number, 'e', -1, 64)
	mantissa, exponentText, _ := strings.Cut(scientific, "e")
	digits := strings.Replace(mantissa, ".", "", 1)
	exponent, err := strconv.Atoi(exponentText)
	if err != nil {
		return fmt.Errorf("jcs: unexpected number layout %q", scientific)
	}
	k := len(digits)
	n := exponent + 1
	switch {
	case k <= n && n <= 21:
		builder.WriteString(digits)
		builder.WriteString(strings.Repeat("0", n-k))
	case 0 < n && n <= 21:
		builder.WriteString(digits[:n])
		builder.WriteByte('.')
		builder.WriteString(digits[n:])
	case -6 < n && n <= 0:
		builder.WriteString("0.")
		builder.WriteString(strings.Repeat("0", -n))
		builder.WriteString(digits)
	default:
		builder.WriteByte(digits[0])
		if k > 1 {
			builder.WriteByte('.')
			builder.WriteString(digits[1:])
		}
		builder.WriteByte('e')
		if n-1 >= 0 {
			builder.WriteByte('+')
		}
		builder.WriteString(strconv.Itoa(n - 1))
	}
	return nil
}
