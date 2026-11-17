package jcs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
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
		number, err := parseFloat(typed.String(), 64)
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
	var scratch [maxNumberBytes]byte
	out, err := AppendNumber(scratch[:0], number)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func writeNumber(builder *strings.Builder, number float64) error {
	var scratch [maxNumberBytes]byte
	out, err := AppendNumber(scratch[:0], number)
	if err != nil {
		return err
	}
	builder.Write(out)
	return nil
}

// maxNumberBytes is room for any form AppendNumber writes, and for the
// shortest digits strconv writes before it lays them out.
const maxNumberBytes = 32

// maxNumberFormBytes is the longest form AppendNumber writes: a sign, a point
// and 17 digits after "0." and five zeros (-0.0000033333333333333333 is 25
// bytes). A sign, 21 digits is 22; a sign, 17 digits, a point and an exponent
// of "e-324" is 24.
const maxNumberFormBytes = 25

// parseFloat is strconv.ParseFloat, the one parse of a json.Number's literal;
// a test counts its calls.
var parseFloat = strconv.ParseFloat

// AppendNumber appends a finite double as RFC 8785 §3.2.2.3 requires, the
// form FormatNumber returns, to dst. With room in dst it allocates nothing.
func AppendNumber(dst []byte, number float64) ([]byte, error) {
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return dst, fmt.Errorf("jcs: a number that is not finite has no canonical form")
	}
	if number == 0 {
		return append(dst, '0'), nil
	}
	if number < 0 {
		dst = append(dst, '-')
		number = -number
	}
	// Shortest round-trip digits, as "d.ddde±x".
	var scratch [maxNumberBytes]byte
	scientific := strconv.AppendFloat(scratch[:0], number, 'e', -1, 64)
	at := bytes.IndexByte(scientific, 'e')
	if at < 1 || at+2 >= len(scientific) || (scientific[at+1] != '+' && scientific[at+1] != '-') {
		return dst, fmt.Errorf("jcs: unexpected number layout %q", string(scientific))
	}
	var digitScratch [maxNumberBytes]byte
	digits := append(digitScratch[:0], scientific[0])
	if at > 1 {
		digits = append(digits, scientific[2:at]...)
	}
	exponent := 0
	for _, digit := range scientific[at+2:] {
		exponent = exponent*10 + int(digit-'0')
	}
	if scientific[at+1] == '-' {
		exponent = -exponent
	}
	k := len(digits)
	n := exponent + 1
	switch {
	case k <= n && n <= 21:
		dst = append(dst, digits...)
		for range n - k {
			dst = append(dst, '0')
		}
	case 0 < n && n <= 21:
		dst = append(dst, digits[:n]...)
		dst = append(dst, '.')
		dst = append(dst, digits[n:]...)
	case -6 < n && n <= 0:
		dst = append(dst, '0', '.')
		for range -n {
			dst = append(dst, '0')
		}
		dst = append(dst, digits...)
	default:
		dst = append(dst, digits[0])
		if k > 1 {
			dst = append(dst, '.')
			dst = append(dst, digits[1:]...)
		}
		dst = append(dst, 'e')
		if n-1 >= 0 {
			dst = append(dst, '+')
		}
		dst = strconv.AppendInt(dst, int64(n-1), 10)
	}
	return dst, nil
}

// ErrTooLong is EncodeDocumentWithin's refusal of a value whose canonical
// form is longer than the bound.
var ErrTooLong = errors.New("jcs: the canonical form is longer than the bound")

// EncodeDocumentWithin is EncodeDocument held to a bound: a value whose
// canonical form is longer than maxBytes is refused with ErrTooLong, and the
// form is never built past the bound. A value EncodeDocument refuses is
// refused with its error, over the bound or not.
//
// A first walk measures the form with each number at the longest form a
// number can have, without parsing one. When that fits the bound, the form is
// built in one pass, and each number is parsed once, as EncodeDocument parses
// it. Otherwise the exact length is measured first, without building the
// form or allocating, so a refusal costs a walk of the value however far its
// form passes the bound: a number's form can be far longer than its literal
// (1e20 is written as 21 digits). Only a value whose form is within the bound
// though its longest reading is not has its numbers parsed twice, once to
// measure and once to build.
func EncodeDocumentWithin(value any, maxBytes int) ([]byte, error) {
	if span := (sizeSpan{limit: maxBytes}); span.add(value) {
		var builder strings.Builder
		builder.Grow(span.hint)
		if err := writeDocument(&builder, value); err != nil {
			return nil, err
		}
		return []byte(builder.String()), nil
	}
	size, err := DocumentSize(value)
	if err != nil {
		return nil, err
	}
	if size > maxBytes {
		return nil, ErrTooLong
	}
	var builder strings.Builder
	builder.Grow(size)
	if err := writeDocument(&builder, value); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}

// sizeSpan measures the form writeDocument writes without parsing a number.
// most counts each number at maxNumberFormBytes, and every other part at its
// exact length, as documentSize measures it. hint, the builder's first
// capacity, counts a json.Number at numberFormHint's guess and a float64 at
// one byte.
type sizeSpan struct {
	hint, most, limit int
}

// add adds value's form to the span. It reports false, and stops, once most
// passes limit, or at a part with no form; the exact measure then decides,
// and names the encoder's refusal.
func (s *sizeSpan) add(value any) bool {
	switch typed := value.(type) {
	case nil:
		s.grow(len("null"))
	case json.Number:
		s.hint += numberFormHint(typed.String())
		s.most += maxNumberFormBytes
	case float64:
		s.hint++
		s.most += maxNumberFormBytes
	case string:
		size, ok := stringSize(typed)
		if !ok {
			return false
		}
		s.grow(size)
	case bool:
		if typed {
			s.grow(len("true"))
		} else {
			s.grow(len("false"))
		}
	case []string:
		s.grow(2 + max(len(typed)-1, 0))
		for _, item := range typed {
			size, ok := stringSize(item)
			if !ok {
				return false
			}
			if s.grow(size); s.most > s.limit {
				return false
			}
		}
	case []any:
		s.grow(2 + max(len(typed)-1, 0))
		for _, item := range typed {
			if !s.add(item) {
				return false
			}
		}
	case map[string]any:
		s.grow(2 + max(len(typed)-1, 0))
		for name, item := range typed {
			size, ok := stringSize(name)
			if !ok {
				return false
			}
			if s.grow(size + 1); !s.add(item) {
				return false
			}
		}
	default:
		return false
	}
	return s.most <= s.limit
}

func (s *sizeSpan) grow(size int) {
	s.hint += size
	s.most += size
}

// numberFormHint guesses the length of a JSON number literal's form from its
// significant digits, at most 17, and the place of the first, as AppendNumber
// lays them out, without parsing it. The guess is exact for a literal that is
// a shortest round-trip decimal, in any layout, and for one of at most 15
// significant digits in the normal range; it only sizes a builder, so a wrong
// guess costs a regrowth and never a byte. A literal it cannot read is
// guessed at its length.
func numberFormHint(literal string) int {
	fallback := min(max(len(literal), 1), maxNumberFormBytes)
	rest, negative := strings.CutPrefix(literal, "-")
	mantissa, exponentText, marked := rest, "", false
	if at := strings.IndexAny(rest, "eE"); at >= 0 {
		mantissa, exponentText, marked = rest[:at], rest[at+1:], true
	}
	integer, fraction, _ := strings.Cut(mantissa, ".")
	digit := func(i int) byte {
		if i < len(integer) {
			return integer[i]
		}
		return fraction[i-len(integer)]
	}
	total := len(integer) + len(fraction)
	lead, trail := -1, -1
	for i := range total {
		c := digit(i)
		if c < '0' || c > '9' {
			return fallback
		}
		if c != '0' {
			if lead < 0 {
				lead = i
			}
			trail = i
		}
	}
	exponent := 0
	exponentDigits := strings.TrimLeft(exponentText, "+-")
	if len(exponentText)-len(exponentDigits) > 1 || marked && exponentDigits == "" {
		return fallback
	}
	for _, c := range exponentDigits {
		if c < '0' || c > '9' {
			return fallback
		}
		exponent = min(exponent*10+int(c-'0'), 1<<20)
	}
	if strings.HasPrefix(exponentText, "-") {
		exponent = -exponent
	}
	if total == 0 {
		return fallback
	}
	if lead < 0 {
		return 1
	}
	k := min(trail-lead+1, 17)
	n := len(integer) - lead + exponent
	size := 0
	if negative {
		size++
	}
	switch {
	case k <= n && n <= 21:
		size += n
	case 0 < n && n <= 21:
		size += k + 1
	case -6 < n && n <= 0:
		size += 2 - n + k
	default:
		if k > 1 {
			size += k + 1
		} else {
			size++
		}
		// "e", the exponent's sign and its digits.
		size += 3
		for power := max(n-1, 1-n); power >= 10; power /= 10 {
			size++
		}
	}
	return min(size, maxNumberFormBytes)
}

// DocumentSize returns the length of EncodeDocument's form of value, or its
// error, without building the form. It allocates nothing for a value that
// has a form.
func DocumentSize(value any) (int, error) {
	if size, ok := documentSize(value); ok {
		return size, nil
	}
	// The value has no canonical form; the encoder names why, as it would
	// have without the measure.
	encoded, err := EncodeDocument(value)
	if err != nil {
		return 0, err
	}
	return len(encoded), nil
}

// documentSize measures the form writeDocument writes, member order aside,
// which does not change its length. It reports false for any value
// writeDocument refuses.
func documentSize(value any) (int, bool) {
	switch typed := value.(type) {
	case nil:
		return len("null"), true
	case json.Number:
		number, err := parseFloat(typed.String(), 64)
		if err != nil {
			return 0, false
		}
		return numberSize(number)
	case float64:
		return numberSize(typed)
	case string:
		return stringSize(typed)
	case bool:
		if typed {
			return len("true"), true
		}
		return len("false"), true
	case []string:
		size := 2 + max(len(typed)-1, 0)
		for _, item := range typed {
			itemSize, ok := stringSize(item)
			if !ok {
				return 0, false
			}
			size += itemSize
		}
		return size, true
	case []any:
		size := 2 + max(len(typed)-1, 0)
		for _, item := range typed {
			itemSize, ok := documentSize(item)
			if !ok {
				return 0, false
			}
			size += itemSize
		}
		return size, true
	case map[string]any:
		size := 2 + max(len(typed)-1, 0)
		for name, item := range typed {
			nameSize, ok := stringSize(name)
			if !ok {
				return 0, false
			}
			itemSize, ok := documentSize(item)
			if !ok {
				return 0, false
			}
			size += nameSize + 1 + itemSize
		}
		return size, true
	default:
		return 0, false
	}
}

func numberSize(number float64) (int, bool) {
	var scratch [maxNumberBytes]byte
	out, err := AppendNumber(scratch[:0], number)
	return len(out), err == nil
}

// stringSize is the length of writeString's form of value: its bytes and
// quotes, and one more byte for each two-byte escape and five more for each
// \u00xx.
func stringSize(value string) (int, bool) {
	if !utf8.ValidString(value) {
		return 0, false
	}
	size := len(value) + 2
	for i := 0; i < len(value); i++ {
		switch c := value[i]; {
		case c == '"' || c == '\\' || c == '\b' || c == '\t' || c == '\n' || c == '\f' || c == '\r':
			size++
		case c < 0x20:
			size += 5
		}
	}
	return size, true
}
