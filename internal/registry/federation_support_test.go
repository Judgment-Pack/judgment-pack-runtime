package registry

import (
	"encoding/json"
	"slices"
	"testing"
)

// The exports internal/federation reads are the functions they wrap, as
// they are.
func TestFederationSupportIsTheReaderUnchanged(t *testing.T) {
	at := Location{Document: "federation"}
	root, refusals := AdmitDocument([]byte(`{"federationFormatVersion":"1.1.0","a":1}`), 64, "federationFormatVersion", "1.1.0", at)
	if root == nil || len(refusals) != 0 {
		t.Fatalf("admitted: %v %v", root, refusals)
	}
	for _, c := range []struct {
		data, code, pointer string
		bound               int
	}{
		{`{"federationFormatVersion":"1.1.0","a":1,"a":2}`, "REG-DOC-DUPLICATE", "/a", 64},
		{`{"federationFormatVersion":"1.0.0"}`, "REG-DOC-SELECTOR", "/federationFormatVersion", 64},
		{`{"federationFormatVersion":"1.1.0"}`, "REG-DOC-SIZE", "", 8},
	} {
		root, refusals := AdmitDocument([]byte(c.data), c.bound, "federationFormatVersion", "1.1.0", at)
		if root != nil || len(refusals) != 1 || refusals[0].Code != c.code || refusals[0].Location.Pointer != c.pointer || refusals[0].Location.Document != "federation" {
			t.Errorf("%s: %v %+v", c.data, root, refusals)
		}
	}

	kindRoot, refusals := AdmitDocument([]byte(nodeKind), MaxKindBytes, "kindFormatVersion", KindFormatVersion, Location{Document: DocumentKind})
	if len(refusals) != 0 {
		t.Fatal(refusals)
	}
	kind, findings := ReadKind(kindRoot, Location{Document: DocumentKind})
	if kind == nil || len(findings) != 0 || kind.Name != "node" {
		t.Fatalf("ReadKind: %v %v", kind, findings)
	}
	reach := func(text string) bool {
		pattern, err := ParsePointerPattern(text)
		if err != nil {
			t.Fatal(err)
		}
		return kind.Reaches(pattern)
	}
	if !reach("/next") || !reach("/*") || reach("/missing") || reach("/next/x") {
		t.Error("Reaches disagrees with the kind's schema")
	}
	if _, err := ParsePointerPattern(""); err == nil {
		t.Error("the empty pattern was parsed")
	}
	if broken, _ := ReadKind(map[string]any{"kindFormatVersion": "1.0.0"}, Location{Document: DocumentKind}); broken != nil {
		t.Error("ReadKind read an unusable declaration")
	}

	a, _ := ParsePointerPattern("/a/*")
	b, _ := ParsePointerPattern("/*/b")
	c, _ := ParsePointerPattern("/a")
	if !a.Overlaps(b) || a.Overlaps(c) {
		t.Error("Overlaps")
	}
	var value any
	if err := json.Unmarshal([]byte(`{"a":{"x":1,"y":2},"b":3}`), &value); err != nil {
		t.Fatal(err)
	}
	var pointers []string
	for _, p := range a.Positions(value) {
		pointers = append(pointers, p.Pointer)
	}
	if !slices.Equal(pointers, []string{"/a/x", "/a/y"}) {
		t.Errorf("Positions: %v", pointers)
	}
	if kind, id, ok := ParseEntryReference("entry:db-role/billing-api"); !ok || kind != "db-role" || id != "billing-api" {
		t.Errorf("ParseEntryReference: %s %s %v", kind, id, ok)
	}
	if _, _, ok := ParseEntryReference("db-role/billing-api"); ok {
		t.Error("ParseEntryReference read a string without its prefix")
	}
	tokens, err := ParsePointer("/a~1b/~0c")
	if err != nil || !slices.Equal(tokens, []string{"a/b", "~c"}) {
		t.Errorf("ParsePointer: %v %v", tokens, err)
	}
	if found, ok := Lookup(value, []string{"a", "y"}); !ok || found != 2.0 {
		t.Errorf("Lookup: %v %v", found, ok)
	}
	if !ValidEnvironment("prod") || ValidEnvironment("Prod") || !ValidEntryID("billing-api") || ValidEntryID("a/b") {
		t.Error("the name grammars")
	}
	if !JSONEqual(json.Number("1.0"), json.Number("1")) || JSONEqual("1", json.Number("1")) {
		t.Error("JSONEqual")
	}
}
