package carrier

import "testing"

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
