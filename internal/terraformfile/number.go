package terraformfile

import (
	"bytes"
	"strings"
)

// exponentCeiling saturates an exponent's magnitude. A literal's digits are at
// most MaxBytesLimit (2^28), so any exponent past 2^40 already puts the
// normal form far beyond MaxNumberChars, and the arithmetic stays in int64.
const exponentCeiling = int64(1) << 40

// normalNumber returns the normal form of a JSON number literal the parser
// has admitted: RFC 0037 §2 rule 1, as the registry companion writes it. An
// integer value is written in base 10 with no leading zero and `-` for a
// negative; any other value with no exponent, no trailing zero after the point
// and a digit on each side of it; and every zero, `-0` included, as `0`. It
// works on the literal's digits alone, with no big arithmetic, and reports
// false, before building anything, when the form would be longer than
// MaxNumberChars.
func normalNumber(literal []byte) (string, bool) {
	negative := literal[0] == '-'
	if negative {
		literal = literal[1:]
	}
	cut := len(literal)
	exponent := int64(0)
	if at := bytes.IndexAny(literal, "eE"); at >= 0 {
		cut = at
		rest := literal[at+1:]
		sign := int64(1)
		if rest[0] == '+' || rest[0] == '-' {
			if rest[0] == '-' {
				sign = -1
			}
			rest = rest[1:]
		}
		for _, c := range rest {
			exponent = exponent*10 + int64(c-'0')
			if exponent > exponentCeiling {
				exponent = exponentCeiling
			}
		}
		exponent *= sign
	}
	mantissa := literal[:cut]
	integer, fraction := mantissa, []byte(nil)
	if at := bytes.IndexByte(mantissa, '.'); at >= 0 {
		integer, fraction = mantissa[:at], mantissa[at+1:]
	}
	total := len(integer) + len(fraction)
	digit := func(i int) byte {
		if i < len(integer) {
			return integer[i]
		}
		return fraction[i-len(integer)]
	}
	first := 0
	for first < total && digit(first) == '0' {
		first++
	}
	if first == total {
		return "0", true
	}
	last := total - 1
	for digit(last) == '0' {
		last--
	}
	significant := int64(last - first + 1)
	// The value is 0.D × 10^point, with D the significant digits.
	point := int64(len(integer)) - int64(first) + exponent
	var length int64
	switch {
	case point >= significant:
		length = point
	case point > 0:
		length = significant + 1
	default:
		length = 2 - point + significant
	}
	if negative {
		length++
	}
	if length > MaxNumberChars {
		return "", false
	}
	var out strings.Builder
	out.Grow(int(length))
	if negative {
		out.WriteByte('-')
	}
	switch {
	case point >= significant:
		for i := first; i <= last; i++ {
			out.WriteByte(digit(i))
		}
		for i := significant; i < point; i++ {
			out.WriteByte('0')
		}
	case point > 0:
		for i := int64(0); i < significant; i++ {
			if i == point {
				out.WriteByte('.')
			}
			out.WriteByte(digit(first + int(i)))
		}
	default:
		out.WriteString("0.")
		for i := point; i < 0; i++ {
			out.WriteByte('0')
		}
		for i := first; i <= last; i++ {
			out.WriteByte(digit(i))
		}
	}
	return out.String(), true
}
