package display

import (
	"strings"
	"unicode"
)

// Sanitize removes terminal controls and bidi overrides from untrusted labels.
func Sanitize(value string) string {
	var output strings.Builder
	for _, char := range value {
		if unicode.IsControl(char) || unicode.Is(unicode.Cf, char) || unicode.Is(unicode.Zl, char) || unicode.Is(unicode.Zp, char) {
			output.WriteRune('?')
			continue
		}
		output.WriteRune(char)
	}
	return output.String()
}
