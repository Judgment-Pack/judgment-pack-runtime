package registry

import (
	"errors"
	"testing"
)

// Declarations gives each pinned kind's declaration as admitted, by the
// pins' filter and sorted by kind, each hashing to its pin's digest.
func TestDeclarations(t *testing.T) {
	registry, kinds := fixture(t)
	result := run(registry, kinds)
	if !result.Report.Valid {
		t.Fatalf("fixture: %+v", result.Report.Diagnostics)
	}
	pins := map[string]string{}
	for _, pin := range result.Pins() {
		pins[pin.Kind] = pin.Digest
	}
	all, err := result.Declarations(nil)
	if err != nil || len(all) != 2 || all[0].Kind != "database" || all[1].Kind != "db-role" {
		t.Fatalf("declarations: %+v %v", all, err)
	}
	for i, declaration := range all {
		digest, err := Digest(declaration.Value)
		if err != nil || digest != pins[declaration.Kind] || declaration.Digest != digest {
			t.Fatalf("%s: digest %s, pin %s, %v", declaration.Kind, digest, pins[declaration.Kind], err)
		}
		if want := digestOf(t, kinds[i]); digest != want {
			t.Fatalf("%s: not the document supplied", declaration.Kind)
		}
	}
	kept, err := result.Declarations(func(kind string) bool { return kind == "db-role" })
	if err != nil || len(kept) != 1 || kept[0].Kind != "db-role" {
		t.Fatalf("filtered: %+v %v", kept, err)
	}
	if none, err := result.Declarations(func(string) bool { return false }); err != nil || len(none) != 0 {
		t.Fatalf("none kept: %+v %v", none, err)
	}
	invalid := run(`{}`, kinds)
	if _, err := invalid.Declarations(nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid: %v", err)
	}
}
