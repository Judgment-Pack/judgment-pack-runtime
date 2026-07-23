package display

import (
	"strings"
	"testing"
	"unicode"
)

func TestSanitizeRemovesTerminalAndDirectionControls(t *testing.T) {
	input := "safe\u009b31m\u061c\u200e\u200f\u202e\u2066\u2028text"
	actual := Sanitize(input)
	if !strings.Contains(actual, "safe") || !strings.Contains(actual, "text") {
		t.Fatalf("safe text was lost: %q", actual)
	}
	for _, character := range actual {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) || unicode.Is(unicode.Zl, character) || unicode.Is(unicode.Zp, character) {
			t.Fatalf("unsafe character U+%04X remains in %q", character, actual)
		}
	}
}
