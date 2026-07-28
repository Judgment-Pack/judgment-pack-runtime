package jcs

import "testing"

// RFC 8785 orders object members by name and writes no whitespace. These rows
// are the ordering and escaping rules a disposition depends on for the byte
// agreement JPS §8.3 requires.
func TestCanonicalForm(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "members are ordered by name, not by insertion",
			value: map[string]any{"kind": "outcome", "handoff": map[string]any{"state": "none"}, "outcomeId": "proceed", "reasons": []any{}},
			want:  `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"proceed","reasons":[]}`,
		},
		{
			name:  "array order is preserved exactly as given",
			value: map[string]any{"reasons": []string{"conflict", "unknown"}},
			want:  `{"reasons":["conflict","unknown"]}`,
		},
		{
			name:  "an empty object and an empty array carry no whitespace",
			value: map[string]any{"a": map[string]any{}, "b": []any{}},
			want:  `{"a":{},"b":[]}`,
		},
		{
			name:  "the two mandatory escapes and the five short forms",
			value: map[string]any{"k": "\"\\\b\t\n\f\r"},
			want:  `{"k":"\"\\\b\t\n\f\r"}`,
		},
		{
			name:  "other control characters use lower-case four-digit escapes",
			value: map[string]any{"k": "\x00\x01\x1f"},
			want:  "{\"k\":\"\\u0000\\u0001\\u001f\"}",
		},
		{
			name:  "non-ASCII text is written as itself, unescaped",
			value: map[string]any{"k": "é☂"},
			want:  `{"k":"é☂"}`,
		},
		{
			name: "names are ordered by UTF-16 code units, not by UTF-8 bytes",
			// U+10000 is one supplementary code point: its UTF-8 bytes sort after
			// U+FFFD's, and its UTF-16 code units (a surrogate pair) sort before.
			value: map[string]any{"\U00010000": "second", "\uFFFD": "first"},
			want:  "{\"\U00010000\":\"second\",\"\uFFFD\":\"first\"}",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			encoded, err := Encode(testCase.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != testCase.want {
				t.Fatalf("Encode = %s, want %s", encoded, testCase.want)
			}
		})
	}
}

// Canonicalizing an already canonical form reproduces it, so a comparison
// between two canonicalized values is stable under repeated canonicalization.
func TestCanonicalizationIsIdempotent(t *testing.T) {
	value := map[string]any{"handoff": map[string]any{"state": "requested", "triggeredBy": []string{"conflict"}}, "kind": "unresolved", "reasons": []string{"conflict", "unknown"}}
	first, err := Encode(value)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encode(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("Encode is not stable: %s then %s", first, second)
	}
}

// The value space is deliberately narrow: §8.3's disposition is strings, arrays,
// and objects, and a value of any other type is refused rather than serialized
// on a guess.
func TestValuesOutsideTheDispositionSpaceAreRefused(t *testing.T) {
	for _, value := range []any{nil, true, 1, 1.5, map[string]any{"n": 1}, []any{false}} {
		if _, err := Encode(value); err == nil {
			t.Fatalf("value %#v must be refused", value)
		}
	}
}

func TestInvalidUTF8IsRefused(t *testing.T) {
	if _, err := Encode(map[string]any{"k": string([]byte{0xff})}); err == nil {
		t.Fatal("a string that is not valid UTF-8 must be refused")
	}
	if _, err := Encode(map[string]any{string([]byte{0xff}): "v"}); err == nil {
		t.Fatal("a member name that is not valid UTF-8 must be refused")
	}
}
