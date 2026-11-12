package display

import (
	"strings"
	"unicode"
)

// Sanitize removes terminal controls and bidi overrides from untrusted labels.
func Sanitize(value string) string {
	var output strings.Builder
	for _, char := range value {
		if Replaces(char) {
			output.WriteRune('?')
			continue
		}
		output.WriteRune(char)
	}
	return output.String()
}

// Replaces reports whether Sanitize replaces char: a control character, a
// format character (which includes the bidi overrides and isolates), or a
// line or paragraph separator. A renderer that must keep such a character
// visible rather than replace it uses the same set, so the two never disagree
// about which characters are unsafe to show as themselves.
func Replaces(char rune) bool {
	return unicode.IsControl(char) || unicode.Is(unicode.Cf, char) || unicode.Is(unicode.Zl, char) || unicode.Is(unicode.Zp, char)
}
