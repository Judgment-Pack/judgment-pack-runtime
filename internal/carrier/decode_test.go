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
