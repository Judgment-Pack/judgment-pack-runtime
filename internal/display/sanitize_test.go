package display

import "testing"

func TestSanitize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "ESC introducer", input: "\x1b[31mred", want: "?[31mred"},
		{name: "full ANSI colour sequence", input: "\x1b[31mred\x1b[0m", want: "?[31mred?[0m"},
		{name: "tab newline and carriage return", input: "a\tb\nc\rd", want: "a?b?c?d"},
		{name: "DEL", input: "\x7f", want: "?"},
		{name: "C1 control", input: "\u009b", want: "?"},
		{name: "bidi override", input: "a\u202eb", want: "a?b"},
		{name: "line separator", input: "a\u2028b", want: "a?b"},
		{name: "paragraph separator", input: "a\u2029b", want: "a?b"},
		{name: "mixed direction controls", input: "safe\u009b31m\u061c\u200e\u200f\u202e\u2066\u2028text", want: "safe?31m??????text"},
		{name: "plain ASCII", input: "safetext", want: "safetext"},
		{name: "ordinary space", input: "a b", want: "a b"},
		{name: "non-breaking space", input: "a\u00a0b", want: "a\u00a0b"},
		{name: "multi-byte character", input: "café", want: "café"},
		{name: "emoji", input: "😀", want: "😀"},
		{name: "empty string", input: "", want: ""},
		{name: "rune count", input: "a\x1b😀\u2029", want: "a?😀?"},
		{name: "idempotency", input: "a\x1b\u202eb", want: "a??b"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Sanitize(test.input)
			if got != test.want {
				t.Fatalf("Sanitize(%q) = %q, want %q", test.input, got, test.want)
			}

			switch test.name {
			case "rune count":
				if gotCount, wantCount := len([]rune(got)), len([]rune(test.input)); gotCount != wantCount {
					t.Fatalf("Sanitize(%q) has %d runes, want %d", test.input, gotCount, wantCount)
				}
			case "idempotency":
				if sanitizedAgain := Sanitize(got); sanitizedAgain != got {
					t.Fatalf("Sanitize(Sanitize(%q)) = %q, want %q", test.input, sanitizedAgain, got)
				}
			}
		})
	}
}

// Replaces names the characters Sanitize replaces, by hand for each class,
// and agrees with Sanitize on every code point.
func TestReplacesIsWhatSanitizeReplaces(t *testing.T) {
	for _, char := range []rune{0x00, 0x09, 0x0a, 0x0d, 0x1b, 0x7f, 0x85, 0x9b, 0x061c, 0x200b, 0x200d, 0x200e, 0x200f, 0x202a, 0x202e, 0x2066, 0x2069, 0x2028, 0x2029, 0xfeff, 0xe0001} {
		if !Replaces(char) {
			t.Errorf("Replaces(%U) = false", char)
		}
	}
	for _, char := range []rune{'a', ' ', '?', '|', 0xa0, 0xe9, 0x3000, 0xfb01, 0x1f600} {
		if Replaces(char) {
			t.Errorf("Replaces(%U) = true", char)
		}
	}
	for char := rune(0); char <= 0x10ffff; char++ {
		if char >= 0xd800 && char <= 0xdfff || char == '?' {
			continue
		}
		if replaced := Sanitize(string(char)) == "?"; replaced != Replaces(char) {
			t.Fatalf("Sanitize and Replaces disagree on %U", char)
		}
	}
}
