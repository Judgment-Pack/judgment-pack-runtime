// Package jcs canonicalizes the one JSON value JPS Core §8.3 requires two
// implementations to be able to compare byte for byte: the portable
// disposition.
//
// RFC 8785 (JCS) is implemented here only over the value space a disposition
// occupies — objects, arrays, and strings — because that is the whole of what
// §8.3 admits: "A disposition contains no numbers, so that specification's
// number rules never engage." A number, a boolean, or a null is refused rather
// than serialized on a guess, so a disposition that grows a member of another
// type has to extend this encoder deliberately instead of silently acquiring an
// ordering RFC 8785 does not give it.
//
// Two rules are the whole of the canonicalization at this value space: object
// members are ordered by the UTF-16 code units of their names, and strings carry
// the minimal escapes ECMAScript's JSON.stringify emits. No whitespace is
// written anywhere.
package jcs

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Encode returns the RFC 8785 canonical form of value. Admitted values are
// map[string]any, []any, []string, and string, nested arbitrarily.
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
func compareUTF16(left, right string) int {
	leftUnits, rightUnits := utf16.Encode([]rune(left)), utf16.Encode([]rune(right))
	return slices.Compare(leftUnits, rightUnits)
}
