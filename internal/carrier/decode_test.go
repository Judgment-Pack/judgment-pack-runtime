package carrier

import (
	"strings"
	"testing"
)

func TestDuplicateMemberReportsNestedPointer(t *testing.T) {
	_, failure := Decode([]byte(`{"outer":{"a":1,"\u0061":2}}`), DefaultLimits())
	if failure == nil || failure.Resource || failure.Diagnostic.Code != "JPS-CARRIER-DUPLICATE-MEMBER" || failure.Diagnostic.InstancePath != "/outer/a" {
		t.Fatalf("unexpected failure: %#v", failure)
	}
}

func TestInvalidUTF8IsCarrierInvalid(t *testing.T) {
	_, failure := Decode([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}, DefaultLimits())
	if failure == nil || failure.Resource || failure.Diagnostic.Code != "JPS-CARRIER-INVALID-JSON" {
		t.Fatalf("unexpected failure: %#v", failure)
	}
}

func TestDepthLimitIsOperational(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxDepth = 2
	_, failure := Decode([]byte(`[[[]]]`), limits)
	if failure == nil || !failure.Resource || failure.Diagnostic.Code != "JPS-RESOURCE-DEPTH-LIMIT" {
		t.Fatalf("unexpected failure: %#v", failure)
	}
}

func TestMissingCommaReportsLocationAndCause(t *testing.T) {
	// A missing comma between members is a parse error; the diagnostic should
	// carry a line, column, and byte offset rather than a vague "incomplete
	// object" message.
	_, failure := Decode([]byte("{\n  \"a\": 1\n  \"b\": 2\n}"), DefaultLimits())
	if failure == nil || failure.Resource || failure.Diagnostic.Code != "JPS-CARRIER-INVALID-JSON" {
		t.Fatalf("expected a carrier invalid-JSON failure: %#v", failure)
	}
	message := failure.Diagnostic.Message
	for _, want := range []string{"line", "column", "byte offset"} {
		if !strings.Contains(message, want) {
			t.Fatalf("carrier message should include %q: %q", want, message)
		}
	}
}

// An unpaired surrogate escape is refused rather than silently repaired.
//
// Go's decoder replaces \uD800 with U+FFFD without complaint, which makes two
// different documents canonicalize to the same bytes — an authored lone
// surrogate and an authored literal replacement character become the same
// string, so a byte comparison §8.3 requires to be exact quietly stops being
// one. RFC 8785 §3.2.2.2 makes such a value invalid, so this decoder terminates
// on it, once, for every document that reaches this runtime.
func TestUnpairedSurrogateEscapesAreRefused(t *testing.T) {
	for name, document := range map[string]string{
		"a lone high surrogate":             `{"name":"\ud800"}`,
		"a lone low surrogate":              `{"name":"\udc00"}`,
		"a high surrogate followed by text": `{"name":"\ud83dX"}`,
		"a high surrogate at the end":       `{"name":"\ud83d"}`,
		"a reversed pair":                   `{"name":"\ude00\ud83d"}`,
		"one inside an array":               `["\udfff"]`,
		"one in a member name":              `{"\ud800":"value"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, failure := Decode([]byte(document), DefaultLimits())
			if failure == nil || failure.Diagnostic.Code != "JPS-CARRIER-INVALID-JSON" {
				t.Fatalf("an unpaired surrogate must be refused: %#v", failure)
			}
			if !strings.Contains(failure.Diagnostic.Message, "unpaired surrogate") {
				t.Fatalf("the refusal must name what it refused: %q", failure.Diagnostic.Message)
			}
		})
	}

	// A well-formed pair is a legal character and is admitted, so the refusal is
	// about pairing and not about escapes.
	for name, document := range map[string]string{
		"a valid surrogate pair":  `{"name":"\ud83d\ude00"}`,
		"a literal astral rune":   `{"name":"😀"}`,
		"an escaped backslash u":  `{"name":"\\ud800"}`,
		"a non-surrogate escape":  `{"name":"\u00e9"}`,
		"an escaped quote before": `{"name":"\"😀"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, failure := Decode([]byte(document), DefaultLimits()); failure != nil {
				t.Fatalf("this document is legal: %#v", failure)
			}
		})
	}

	// An escaped character and the literal it names are the same string, and the
	// scan does not change that: only invalid Unicode is refused.
	escaped, failure := Decode([]byte(`{"name":"caf\u00e9"}`), DefaultLimits())
	if failure != nil {
		t.Fatal(failure.Diagnostic.Message)
	}
	literal, failure := Decode([]byte("{\"name\":\"café\"}"), DefaultLimits())
	if failure != nil {
		t.Fatal(failure.Diagnostic.Message)
	}
	if escaped.(map[string]any)["name"] != literal.(map[string]any)["name"] {
		t.Fatal("an escape and its literal are one string")
	}
	// Normalization is not equality: NFC "é" and NFD "é" are different
	// strings, and nothing here normalizes either toward the other.
	decomposed, failure := Decode([]byte(`{"name":"café"}`), DefaultLimits())
	if failure != nil {
		t.Fatal(failure.Diagnostic.Message)
	}
	if decomposed.(map[string]any)["name"] == literal.(map[string]any)["name"] {
		t.Fatal("NFD and NFC are different strings; this runtime normalizes neither")
	}
}
