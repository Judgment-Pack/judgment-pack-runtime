// Package canonjson is the canonical JSON writer of package sdk's formats:
// the pack view's JSON (ADR-0074) and the decision record, with the digests
// of the facts and evidence documents it carries (ADR-0079). Only sdk and its
// subpackages may import it, so the formats share one writer.
//
// The form is RFC 8785 in everything but numbers: object members ordered by
// the UTF-16 code units of their names, the RFC's minimal string escapes, no
// whitespace. A number is written as the literal token the carrier decoder
// kept, because numbers are compared and reviewed as written — 1.50 is not
// 1.5 to a reader of the view — and the RFC's number form would rewrite them.
// Null, which the RFC writes as null, is written so. internal/jcs implements
// the same rules over the disposition's value space and refuses numbers and
// null; a test of the view holds this writer to it on that space.
package canonjson

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Raw is JSON text already in canonical form, written verbatim.
type Raw string

// Text returns the canonical JSON text of one decoded value: a value of the
// JSON data model as the carrier decoder returns it (map[string]any, []any,
// string, json.Number, bool, nil), with Raw text anywhere a value may stand.
// Any other value, a number that is not a JSON number token, and a string
// that is not valid UTF-8 are refused.
func Text(value any) (string, error) {
	var builder strings.Builder
	if err := writeCanonical(&builder, value); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func writeCanonical(builder *strings.Builder, value any) error {
	switch typed := value.(type) {
	case nil:
		builder.WriteString("null")
	case bool:
		if typed {
			builder.WriteString("true")
		} else {
			builder.WriteString("false")
		}
	case string:
		return writeCanonicalString(builder, typed)
	case json.Number:
		token := string(typed)
		if token == "" || !strings.ContainsRune("-0123456789", rune(token[0])) || !json.Valid([]byte(token)) {
			return errors.New("a number that is not a JSON number token")
		}
		builder.WriteString(token)
	case Raw:
		builder.WriteString(string(typed))
	case []any:
		builder.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				builder.WriteByte(',')
			}
			if err := writeCanonical(builder, item); err != nil {
				return err
			}
		}
		builder.WriteByte(']')
	case map[string]any:
		names := make([]string, 0, len(typed))
		for name := range typed {
			names = append(names, name)
		}
		slices.SortFunc(names, CompareUTF16)
		builder.WriteByte('{')
		for index, name := range names {
			if index > 0 {
				builder.WriteByte(',')
			}
			if err := writeCanonicalString(builder, name); err != nil {
				return err
			}
			builder.WriteByte(':')
			if err := writeCanonical(builder, typed[name]); err != nil {
				return err
			}
		}
		builder.WriteByte('}')
	default:
		return fmt.Errorf("a value of type %T, outside the JSON data model", value)
	}
	return nil
}

// writeCanonicalString writes one string with RFC 8785's escapes: the two
// mandatory ones, the five short forms, and \u00xx for every other control
// character below U+0020. Every other code point is written as itself.
func writeCanonicalString(builder *strings.Builder, value string) error {
	if !utf8.ValidString(value) {
		return errors.New("a string that is not valid UTF-8")
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

// CompareUTF16 orders two member names by their UTF-16 code units, as RFC
// 8785 orders them; that is not the UTF-8 byte order for a name holding a
// code point outside the Basic Multilingual Plane. It decodes as []rune does,
// an invalid byte as U+FFFD, and allocates nothing.
func CompareUTF16(left, right string) int {
	for left != "" && right != "" {
		l, lsize := utf8.DecodeRuneInString(left)
		r, rsize := utf8.DecodeRuneInString(right)
		if l != r {
			return cmp.Compare(unitOrder(l), unitOrder(r))
		}
		left, right = left[lsize:], right[rsize:]
	}
	return cmp.Compare(len(left), len(right))
}

// unitOrder keys a code point by where its first UTF-16 unit sorts: a
// supplementary code point leads with a surrogate, above U+D7FF and below
// U+E000, so U+E000 to U+FFFF are moved above every supplementary one.
func unitOrder(c rune) rune {
	if c >= 0xE000 && c <= 0xFFFF {
		return c + 0x200000
	}
	return c
}
