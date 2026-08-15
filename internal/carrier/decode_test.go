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

// The decoder's limits are enforced in decode.go but were pinned only for
// depth. Each case below pairs a document exactly AT the limit with one a
// single unit over it, because a test that only shows the over-limit input
// failing cannot tell a correct boundary from an off-by-one that refuses
// conforming documents.
func TestResourceLimitsAreEnforcedAtTheirExactBoundary(t *testing.T) {
	for _, tt := range []struct {
		name     string
		limits   func(Limits) Limits
		atLimit  string
		overBy1  string
		code     string
		instance string
	}{
		{
			name:    "node limit",
			limits:  func(l Limits) Limits { l.MaxNodes = 3; return l },
			atLimit: `[1,2]`, // the array plus two members
			overBy1: `[1,2,3]`,
			code:    "JPS-RESOURCE-NODE-LIMIT",
		},
		{
			name:    "string value limit",
			limits:  func(l Limits) Limits { l.MaxStringBytes = 4; return l },
			atLimit: `{"a":"abcd"}`,
			overBy1: `{"a":"abcde"}`,
			code:    "JPS-RESOURCE-STRING-LIMIT",
		},
		{
			name:    "object member name limit",
			limits:  func(l Limits) Limits { l.MaxStringBytes = 4; return l },
			atLimit: `{"abcd":1}`,
			overBy1: `{"abcde":1}`,
			code:    "JPS-RESOURCE-STRING-LIMIT",
		},
		{
			name:    "depth limit",
			limits:  func(l Limits) Limits { l.MaxDepth = 2; return l },
			atLimit: `[[1]]`,
			overBy1: `[[[1]]]`,
			code:    "JPS-RESOURCE-DEPTH-LIMIT",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			limits := tt.limits(DefaultLimits())

			if _, failure := Decode([]byte(tt.atLimit), limits); failure != nil {
				t.Fatalf("a document exactly at the limit must decode, got %#v", failure)
			}

			_, failure := Decode([]byte(tt.overBy1), limits)
			if failure == nil {
				t.Fatal("a document one unit over the limit must be refused")
			}
			if !failure.Resource {
				t.Fatalf("a limit is an operational refusal, not a carrier defect: %#v", failure)
			}
			if failure.Diagnostic.Code != tt.code {
				t.Fatalf("code = %q, want %q", failure.Diagnostic.Code, tt.code)
			}
		})
	}
}

// A number token has its own diagnostic code but NOT its own budget: it is
// measured against MaxStringBytes, the same field a string value is. That is
// easy to miss from the distinct code alone, and a future change that split the
// two would want a test saying which one was intended, so it is pinned here.
func TestNumberTokenLimitIsOperationalAndSharesTheStringBudget(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxStringBytes = 4

	if _, failure := Decode([]byte(`{"a":1234}`), limits); failure != nil {
		t.Fatalf("a number token exactly at the limit must decode, got %#v", failure)
	}
	_, failure := Decode([]byte(`{"a":12345}`), limits)
	if failure == nil || !failure.Resource || failure.Diagnostic.Code != "JPS-RESOURCE-NUMBER-LIMIT" {
		t.Fatalf("unexpected failure: %#v", failure)
	}
	if failure.Diagnostic.InstancePath != "/a" {
		t.Fatalf("the refusal must name where it happened, got %q", failure.Diagnostic.InstancePath)
	}
}

// Trailing data is a carrier defect rather than a resource refusal: the input
// is not one JSON text, which is a statement about the document and not about
// this implementation's capacity.
func TestTrailingDataIsACarrierDefect(t *testing.T) {
	for _, document := range []string{`{} {}`, `{}null`, `[] 1`, "{}\n{}"} {
		t.Run(strings.ReplaceAll(document, "\n", "\\n"), func(t *testing.T) {
			_, failure := Decode([]byte(document), DefaultLimits())
			if failure == nil {
				t.Fatal("a second JSON value must be refused")
			}
			if failure.Resource {
				t.Fatalf("trailing data is not a resource refusal: %#v", failure)
			}
			if failure.Diagnostic.Code != "JPS-CARRIER-INVALID-JSON" {
				t.Fatalf("code = %q", failure.Diagnostic.Code)
			}
		})
	}
	// Trailing whitespace is not trailing data.
	if _, failure := Decode([]byte("{\"a\":1}\n\t "), DefaultLimits()); failure != nil {
		t.Fatalf("trailing whitespace must be accepted, got %#v", failure)
	}
}

// RFC 6901 escaping, checked directly rather than only through a diagnostic:
// `~` becomes `~0` and `/` becomes `~1`.
//
// The current implementation is a per-rune switch, so its two cases are
// order-independent and reversing them changes nothing — verified by mutation
// rather than assumed. What these cases DO pin is the result against an
// implementation that escaped sequentially, which is the usual way this is
// written and the usual way it goes wrong: replacing `/` before `~` turns
// `a/b` into `a~01b` instead of `a~1b`, because the `~1` it just produced is
// then escaped again. The `a/b` case below catches exactly that.
func TestPointerEscapesReservedCharacters(t *testing.T) {
	for _, tt := range []struct {
		name  string
		parts []string
		want  string
	}{
		{name: "nil parts", parts: nil, want: ""},
		{name: "empty parts", parts: []string{}, want: ""},
		{name: "plain path", parts: []string{"rules", "0", "when"}, want: "/rules/0/when"},
		{name: "reserved characters", parts: []string{"a/b", "c~d"}, want: "/a~1b/c~0d"},
		{name: "a literal tilde-one stays recoverable", parts: []string{"~1"}, want: "/~01"},
		{name: "empty member name", parts: []string{""}, want: "/"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Pointer(tt.parts); got != tt.want {
				t.Fatalf("Pointer(%#v) = %q, want %q", tt.parts, got, tt.want)
			}
		})
	}
}

// And the escaping reaches a real diagnostic, not just the helper: a duplicate
// member whose name carries both reserved characters must be reported at an
// escaped path a consumer can resolve.
func TestDiagnosticPathEscapesReservedCharactersInMemberNames(t *testing.T) {
	_, failure := Decode([]byte(`{"a/b~c":1,"a/b~c":2}`), DefaultLimits())
	if failure == nil || failure.Diagnostic.Code != "JPS-CARRIER-DUPLICATE-MEMBER" {
		t.Fatalf("unexpected failure: %#v", failure)
	}
	if failure.Diagnostic.InstancePath != "/a~1b~0c" {
		t.Fatalf("instance path = %q, want %q", failure.Diagnostic.InstancePath, "/a~1b~0c")
	}
}
