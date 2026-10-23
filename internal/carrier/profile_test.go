package carrier

import (
	"reflect"
	"testing"
)

func TestRootDeclarationScanRetainsOnlyExactRootDeclarations(t *testing.T) {
	root, failure := ScanRootDeclarations([]byte(`{"specVersion":"0.3.0","profile":"base","nested":{"profile":"extended","specVersion":"wrong"},"values":[1,{},[]]}`), DefaultLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	if !reflect.DeepEqual(root, map[string]any{"specVersion": "0.3.0", "profile": "base"}) {
		t.Fatalf("declarations: %#v", root)
	}
	for _, raw := range []string{
		`{"profile":"extended",`,
		`{"profile":"extended"} {}`,
		`{"profile":"extended","nested":{"x":1,"x":2}}`,
		`{"profile":"extended","profile":"base"}`,
		`{"profile":"extended","nested":"\ud800"}`,
	} {
		if _, failure := ScanRootDeclarations([]byte(raw), DefaultLimits()); failure == nil {
			t.Errorf("accepted unreadable/ambiguous carrier %s", raw)
		}
	}
	limits := DefaultLimits()
	limits.MaxDepth = 2
	if _, failure := ScanRootDeclarations([]byte(`{"specVersion":"0.3.0","profile":"extended","nested":[[]]}`), limits); failure == nil || !failure.Resource {
		t.Fatalf("unbounded scan: %+v", failure)
	}
}
