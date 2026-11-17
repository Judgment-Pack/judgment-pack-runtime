// Package jcs canonicalizes the one JSON value JPS Core §8.3 requires two
// implementations to be able to compare byte for byte: the portable
// disposition.
//
// RFC 8785 (JCS) is implemented here only over the value space a disposition
// occupies — objects, arrays, and strings — because that is the whole of what
// §8.3 admits: "A disposition contains no numbers, so that specification's
// number rules never engage." A number or a null is refused rather than
// serialized on a guess, so a disposition that grows a member of another type
// has to extend this encoder deliberately instead of silently acquiring an
// ordering RFC 8785 does not give it.
//
// A Boolean is the one deliberate extension so far. The value member of the
// specification's RFC 0016 (Draft) holds strings and Booleans, and this runtime
// produces that member under an opt-in only (ADR-0039). RFC 8785 §3.2.2 writes a
// Boolean as the literal true or false, which leaves nothing to choose. No
// disposition of JPS Core 0.2.0-draft holds one, and the type that builds a
// disposition admits one inside that member and nowhere else.
//
// Two rules are the whole of the canonicalization at this value space: object
// members are ordered by the UTF-16 code units of their names, and strings carry
// the minimal escapes ECMAScript's JSON.stringify emits. No whitespace is
// written anywhere.
package jcs

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Encode returns the RFC 8785 canonical form of value. Admitted values are
// map[string]any, []any, []string, string, and bool, nested arbitrarily.
func Encode(value any) ([]byte, error) {
	var builder strings.Builder
	if err := write(&builder, value); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}

func write(builder *strings.Builder, value any) error {
	switch typed := value.(type) {
	case string:
		return writeString(builder, typed)
	case bool:
		if typed {
			builder.WriteString("true")
		} else {
			builder.WriteString("false")
		}
		return nil
	case []string:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		return write(builder, items)
	case []any:
		builder.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				builder.WriteByte(',')
			}
			if err := write(builder, item); err != nil {
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
			if err := write(builder, typed[name]); err != nil {
				return err
			}
		}
		builder.WriteByte('}')
		return nil
	default:
		return fmt.Errorf("jcs: value of type %T is outside the canonicalized value space", value)
	}
}

// writeString emits one JSON string with RFC 8785's escapes: the two mandatory
// ones, the five short forms for control characters that have them, and a
// lower-case \u00xx for every other control character. Every other code point is
// written as itself, in UTF-8.
func writeString(builder *strings.Builder, value string) error {
	if !utf8.ValidString(value) {
		return errors.New("jcs: string is not valid UTF-8")
	}
	builder.WriteByte('"')
	for _, character := range value {
		switch character {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\b':
			builder.WriteString(`\b`)
		case '\t':
			builder.WriteString(`\t`)
		case '\n':
			builder.WriteString(`\n`)
		case '\f':
			builder.WriteString(`\f`)
		case '\r':
			builder.WriteString(`\r`)
		default:
			if character < 0x20 {
				fmt.Fprintf(builder, `\u%04x`, character)
				continue
			}
			builder.WriteRune(character)
		}
	}
	builder.WriteByte('"')
	return nil
}

// compareUTF16 orders two member names as RFC 8785 requires: by their UTF-16
// code units, which is not the same order as their UTF-8 bytes for a name
// carrying a supplementary-plane code point.
//
// It walks both names in place and allocates nothing. The bytes they share
// are skipped without decoding: equal bytes are equal code points. The skip
// backs up to the first byte of the code point it stopped in, so decoding
// starts where it would have from each name's first byte, a malformed name's
// included. From there, two different code points compare by their first
// code unit; when those are equal, both are supplementary and the second unit
// decides. A name that runs out first is a prefix of the other and sorts
// first.
func compareUTF16(left, right string) int {
	common := 0
	for common < len(left) && common < len(right) && left[common] == right[common] {
		common++
	}
	continues := func(name string, i int) bool { return i < len(name) && !utf8.RuneStart(name[i]) }
	for common > 0 && (continues(left, common) || continues(right, common)) {
		common--
	}
	left, right = left[common:], right[common:]
	for len(left) > 0 && len(right) > 0 {
		l, ln := utf8.DecodeRuneInString(left)
		r, rn := utf8.DecodeRuneInString(right)
		if l != r {
			lu, lnext := utf16Units(l)
			ru, rnext := utf16Units(r)
			if lu != ru {
				return cmp.Compare(lu, ru)
			}
			return cmp.Compare(lnext, rnext)
		}
		left, right = left[ln:], right[rn:]
	}
	return cmp.Compare(len(left), len(right))
}

// utf16Units is a code point's UTF-16 code units: itself and 0 below
// U+10000, and its surrogate pair above. A decoded name holds no surrogate
// code point, and none above U+10FFFF.
func utf16Units(r rune) (uint16, uint16) {
	if r < 0x10000 {
		return uint16(r), 0
	}
	r -= 0x10000
	return uint16(0xD800 + r>>10), uint16(0xDC00 + r&0x3FF)
}
