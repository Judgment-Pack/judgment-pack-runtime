package terraformfile

import "bytes"

// exponentCeiling saturates an exponent's magnitude. A literal's digits are at
// most MaxBytesLimit (2^28), so any exponent past 2^40 already puts the
// normal form far beyond MaxNumberChars, and the arithmetic stays in int64.
const exponentCeiling = int64(1) << 40

// numberForm is a number literal's normal form, measured but not built: the
// literal's digits, the span of its significant ones, where the point falls,
// and the form's length. A zero has first == total.
type numberForm struct {
	negative          bool
	integer, fraction []byte
	first, last       int
	significant       int64
	point             int64
	length            int64
}

// normalNumber returns the normal form of a JSON number literal the parser
// has admitted: RFC 0037 §2 rule 1, as the registry companion writes it. An
// integer value is written in base 10 with no leading zero and `-` for a
// negative; any other value with no exponent, no trailing zero after the point
// and a digit on each side of it; and every zero, `-0` included, as `0`. It
// works on the literal's digits alone, with no big arithmetic, and reports
// false, before building anything, when the form would be longer than
// MaxNumberChars.
func normalNumber(literal []byte) (string, bool) {
	form, ok := measureNumber(literal)
	if !ok {
		return "", false
	}
	return string(form.appendTo(make([]byte, 0, form.length))), true
}

// measureNumber measures a literal's normal form without building it, and
// reports false when the form would be longer than MaxNumberChars. The
// checking and counting passes need only this; the writing pass appends the
// form with appendTo, so no number allocates while the value is written.
func measureNumber(literal []byte) (numberForm, bool) {
	var form numberForm
	form.negative = literal[0] == '-'
	if form.negative {
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
	form.integer = mantissa
	if at := bytes.IndexByte(mantissa, '.'); at >= 0 {
		form.integer, form.fraction = mantissa[:at], mantissa[at+1:]
	}
	total := len(form.integer) + len(form.fraction)
	first := 0
	for first < total && form.digit(first) == '0' {
		first++
	}
	form.first = first
	if first == total {
		form.length = 1
		return form, true
	}
	last := total - 1
	for form.digit(last) == '0' {
		last--
	}
	form.last = last
	form.significant = int64(last - first + 1)
	// The value is 0.D × 10^point, with D the significant digits.
	form.point = int64(len(form.integer)) - int64(first) + exponent
	switch {
	case form.point >= form.significant:
		form.length = form.point
	case form.point > 0:
		form.length = form.significant + 1
	default:
		form.length = 2 - form.point + form.significant
	}
	if form.negative {
		form.length++
	}
	return form, form.length <= MaxNumberChars
}

func (f *numberForm) digit(i int) byte {
	if i < len(f.integer) {
		return f.integer[i]
	}
	return f.fraction[i-len(f.integer)]
}

// appendTo appends the normal form, exactly f.length bytes, to dst.
func (f *numberForm) appendTo(dst []byte) []byte {
	if f.first == len(f.integer)+len(f.fraction) {
		return append(dst, '0')
	}
	if f.negative {
		dst = append(dst, '-')
	}
	switch {
	case f.point >= f.significant:
		for i := f.first; i <= f.last; i++ {
			dst = append(dst, f.digit(i))
		}
		for i := f.significant; i < f.point; i++ {
			dst = append(dst, '0')
		}
	case f.point > 0:
		for i := int64(0); i < f.significant; i++ {
			if i == f.point {
				dst = append(dst, '.')
			}
			dst = append(dst, f.digit(f.first+int(i)))
		}
	default:
		dst = append(dst, '0', '.')
		for i := f.point; i < 0; i++ {
			dst = append(dst, '0')
		}
		for i := f.first; i <= f.last; i++ {
			dst = append(dst, f.digit(i))
		}
	}
	return dst
}
