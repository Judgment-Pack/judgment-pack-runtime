package registry

import (
	"slices"
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

// §3.3: the cycle check covers every $ref that names a definition, whatever
// other diagnostic the schema has: a $ref that names no definition, or an
// unknown keyword, does not hide a cycle.
func TestACycleIsReportedBesideOtherSchemaErrors(t *testing.T) {
	for _, c := range []struct {
		schema string
		want   []string
	}{
		{`{"$defs":{"a":{"$ref":"#/$defs/none"},"b":{"$ref":"#/$defs/c"},"c":{"$ref":"#/$defs/b"}},"$ref":"#/$defs/b"}`,
			[]string{"REG-KIND-SCHEMA-REF /schema/$defs/a/$ref", "REG-KIND-SCHEMA-REF /schema/$defs/b/$ref"}},
		{`{"$defs":{"b":{"$ref":"#/$defs/c"},"c":{"allOf":[{"$ref":"#/$defs/b"}],"bogus":1}}}`,
			[]string{"REG-KIND-SCHEMA-REF /schema/$defs/b/$ref", "REG-KIND-SCHEMA-KEYWORD /schema/$defs/c/bogus"}},
	} {
		collected := &collector{}
		if node := compileSchema(decodeAny(t, c.schema), []string{"schema"}, Location{Document: DocumentKind}, collected, &patternBudget{}); node != nil {
			t.Fatalf("%s: admitted", c.schema)
		}
		var got []string
		for _, d := range collected.sorted() {
			got = append(got, d.Code+" "+d.Location.Pointer)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.schema, got, c.want)
		}
	}
}
