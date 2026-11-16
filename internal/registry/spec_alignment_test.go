package registry

import (
	"testing"
)

// Each test here pins one place where the registry follows the registry
// companion's text.

// §7.3: every step-2 diagnostic of a declaration whose kind member is a kind
// name carries it, whatever members come before it.
func TestEveryKindDiagnosticCarriesTheKind(t *testing.T) {
	collected := &collector{}
	root := decode(t, `{"kindFormatVersion":"1.0.0","id":"not a uri","extra":1,"version":"1.0.0","schema":{"$ref":"#/$defs/none"},"kind":"k"}`)
	if kind := readKind(root, Location{Document: DocumentKind}, collected); kind != nil {
		t.Fatal("the declaration was admitted")
	}
	want := map[string]bool{
		"REG-KIND-UNKNOWN-MEMBER /extra": false, "REG-KIND-MISSING-MEMBER /owner": false,
		"REG-KIND-FORM /id": false, "REG-KIND-SCHEMA-REF /schema/$ref": false,
	}
	for _, d := range collected.sorted() {
		if d.Location.Kind != "k" {
			t.Errorf("%s at %s has kind %q", d.Code, d.Location.Pointer, d.Location.Kind)
		}
		if _, ok := want[d.Code+" "+d.Location.Pointer]; ok {
			want[d.Code+" "+d.Location.Pointer] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("%s not reported", name)
		}
	}
}
