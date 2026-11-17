package registry

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
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

// AdmitValue and AdmitValueNodes refuse a document with the REG-DOC-* code
// and the pointer of the value that refuses it, and give the value otherwise.
func TestAdmitValue(t *testing.T) {
	deep := strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1)
	cases := []struct {
		name, data         string
		maxBytes, maxNodes int
		code, pointer      string
	}{
		{"admitted", `{"a":[1,"x"]}`, 64, 64, "", ""},
		{"past the size bound", `{"a":1}`, 6, 64, "REG-DOC-SIZE", ""},
		{"past the node bound", `{"a":[1,2,3]}`, 64, 3, "REG-DOC-SIZE", "/a/1"},
		{"a duplicate member", `{"a":1,"a":2}`, 64, 64, "REG-DOC-DUPLICATE", "/a"},
		{"not JSON", `{"a":`, 64, 64, "REG-DOC-JSON", ""},
		{"a lone surrogate", `{"a":"\ud800"}`, 64, 64, "REG-DOC-STRING", ""},
		{"negative zero", `{"a":[-0]}`, 64, 64, "REG-DOC-NUMBER", "/a/0"},
		{"past the depth bound", deep, 1024, 1024, "REG-DOC-DEPTH", "/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0/0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			value, code, pointer := AdmitValueNodes([]byte(c.data), c.maxBytes, c.maxNodes)
			if code != c.code || pointer != c.pointer {
				t.Fatalf("got %s at %q, want %s at %q", code, pointer, c.code, c.pointer)
			}
			if (value == nil) != (c.code != "") {
				t.Fatalf("value %v with code %q", value, code)
			}
		})
	}
	value, code, _ := AdmitValue([]byte(`{"n":1.50}`), 64)
	if code != "" || encode(t, value) != `{"n":1.5}` {
		t.Fatalf("AdmitValue: %v %s", value, code)
	}
	if _, code, _ := AdmitValue([]byte(`{"n":1}`), 6); code != "REG-DOC-SIZE" {
		t.Fatalf("AdmitValue past its bound: %s", code)
	}
}

// CompileSchema refuses a schema outside the subset, pointed into the schema;
// Validate gives a value's error findings sorted by pointer, without the
// deprecation warning.
func TestCompileSchemaAndValidate(t *testing.T) {
	schema, findings := CompileSchema(decodeAny(t, `{"type":"object","additionalProperties":false,
		"properties":{"name":{"type":"string"},"old":{"deprecated":true},"size":{"type":"integer","maximum":3}}}`))
	if schema == nil || findings != nil {
		t.Fatalf("refused: %+v", findings)
	}
	got := schema.Validate(decodeAny(t, `{"size":9,"name":1,"old":true,"zz":0}`))
	want := []SchemaFinding{{"REG-VALUE-TYPE", "/name"}, {"REG-VALUE-RANGE", "/size"}, {"REG-VALUE-ADDITIONAL", "/zz"}}
	if !slices.Equal(got, want) {
		t.Fatalf("findings %+v, want %+v", got, want)
	}
	if got := schema.Validate(decodeAny(t, `{"name":"a","old":1}`)); len(got) != 0 {
		t.Fatalf("a deprecated member is returned: %+v", got)
	}
	for text, want := range map[string]SchemaFinding{
		`{"properties":{"a":{"format":"email"}}}`: {"REG-KIND-SCHEMA-KEYWORD", "/properties/a/format"},
		`{"pattern":"(?=a)"}`:                     {"REG-KIND-PATTERN", "/pattern"},
		`{"$ref":"#/$defs/none"}`:                 {"REG-KIND-SCHEMA-REF", "/$ref"},
	} {
		if schema, findings := CompileSchema(decodeAny(t, text)); schema != nil || len(findings) != 1 || findings[0] != want {
			t.Errorf("%s: %+v, want %+v", text, findings, want)
		}
	}
}

// The identifier grammars of §3.1, as the emitter host reads a manifest.
func TestIdentifierGrammars(t *testing.T) {
	for _, c := range []struct {
		valid func(string) bool
		text  string
		want  bool
	}{
		{ValidKindName, "db-role", true}, {ValidKindName, "Db", false}, {ValidKindName, "", false},
		{ValidOwner, "platform-data", true}, {ValidOwner, "Platform Data", false},
		{ValidVersion, "1.2.0", true}, {ValidVersion, "1.2.0-rc.1", true}, {ValidVersion, "1.2.0+build", false}, {ValidVersion, "01.2.0", false},
		{IsAbsoluteURI, "https://example.invalid/kinds/a", true}, {IsAbsoluteURI, "kinds/a", false},
	} {
		if got := c.valid(c.text); got != c.want {
			t.Errorf("%q: %v, want %v", c.text, got, c.want)
		}
	}
}

// ReadLocation reads a location of §7.3's form and refuses every member out
// of its form, and every member the table does not define.
func TestReadLocation(t *testing.T) {
	digest := "sha256:" + strings.Repeat("ab", 32)
	location, ok := ReadLocation(decodeAny(t, `{"document":"value","kindDocument":"`+digest+`","kind":"db-role",
		"entry":{"kind":"db-role","id":"billing-api"},"environment":"prod","pointer":"/a~1b"}`))
	want := Location{Document: DocumentValue, KindDocument: digest, Kind: "db-role", Entry: &EntryName{Kind: "db-role", ID: "billing-api"},
		Environment: "prod", Pointer: "/a~1b"}
	if !ok || location.Entry == nil || *location.Entry != *want.Entry {
		t.Fatalf("location %+v, %v", location, ok)
	}
	location.Entry, want.Entry = nil, nil
	if location != want {
		t.Fatalf("location %+v, want %+v", location, want)
	}
	if location, ok := ReadLocation(decodeAny(t, `{"document":"yaml","pointer":"","line":3,"column":1}`)); !ok || location.Line != 3 || location.Column != 1 {
		t.Fatalf("a YAML location: %+v %v", location, ok)
	}
	for name, text := range map[string]string{
		"not an object":                   `[]`,
		"an unknown member":               `{"document":"value","pointer":"","value":"x"}`,
		"no document":                     `{"pointer":""}`,
		"an unknown document":             `{"document":"other","pointer":""}`,
		"no pointer":                      `{"document":"value"}`,
		"a pointer not a pointer":         `{"document":"value","pointer":"a"}`,
		"a pointer past its bound":        `{"document":"value","pointer":"/` + strings.Repeat("a", maxLocationPointerBytes) + `"}`,
		"a kindDocument not a digest":     `{"document":"kind","kindDocument":"sha256:00","pointer":""}`,
		"a kind outside its grammar":      `{"document":"kind","kind":"Db","pointer":""}`,
		"an environment not a string":     `{"document":"value","environment":1,"pointer":""}`,
		"an entry with a member more":     `{"document":"value","entry":{"kind":"db-role","id":"a","owner":"x"},"pointer":""}`,
		"an entry id outside its grammar": `{"document":"value","entry":{"kind":"db-role","id":"a/b"},"pointer":""}`,
		"a line outside YAML":             `{"document":"value","pointer":"","line":1,"column":1}`,
		"a YAML line with a pointer":      `{"document":"yaml","pointer":"/a","line":1,"column":1}`,
		"a line of zero":                  `{"document":"yaml","pointer":"","line":0,"column":1}`,
		"a column with no line":           `{"document":"yaml","pointer":"","column":1}`,
	} {
		if _, ok := ReadLocation(decodeAny(t, text)); ok {
			t.Errorf("%s: read", name)
		}
	}
}

// PointerResolves says whether each token names a member or an index of
// what it meets, reading ~0 and ~1.
func TestPointerResolves(t *testing.T) {
	value := decodeAny(t, `{"a":[{"b~/c":1}],"":2}`)
	for pointer, want := range map[string]bool{
		"": true, "/a": true, "/a/0": true, "/a/0/b~0~1c": true, "/": true,
		"/a/1": false, "/a/-": false, "/a/00": false, "/a/0/b~/c": false, "/b": false, "a": false, "/a/0/b~0~1c/d": false,
	} {
		if got := PointerResolves(value, pointer); got != want {
			t.Errorf("%q: %v, want %v", pointer, got, want)
		}
	}
}

// ResolvedFor gives the document resolve writes, with only the kinds keep
// admits, and the whole registry's digest; it refuses an environment the
// registry does not declare, and an invalid registry.
func TestResolvedFor(t *testing.T) {
	registry, kinds := fixture(t)
	result := run(registry, kinds)
	whole, err := result.ResolvedFor("prod", nil)
	if err != nil {
		t.Fatal(err)
	}
	written, err := result.Resolve("prod")
	if err != nil {
		t.Fatal(err)
	}
	if data, err := jcs.EncodeDocument(whole); err != nil || !bytes.Equal(data, written) {
		t.Fatalf("ResolvedFor is not what resolve writes: %v", err)
	}
	roles, err := result.ResolvedFor("prod", func(kind string) bool { return kind == "db-role" })
	if err != nil {
		t.Fatal(err)
	}
	pins, entries := roles["kinds"].([]any), roles["entries"].([]any)
	if len(pins) != 1 || pins[0].(map[string]any)["kind"] != "db-role" || len(entries) != 1 ||
		entries[0].(map[string]any)["id"] != "billing-api" || encode(t, roles["registry"]) != encode(t, whole["registry"]) {
		t.Fatalf("filtered: %s", encode(t, roles))
	}
	if _, err := result.ResolvedFor("qa", nil); err == nil || errors.Is(err, ErrInvalid) {
		t.Fatalf("an undeclared environment: %v", err)
	}
	if _, err := run(`{}`, kinds).ResolvedFor("prod", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an invalid registry: %v", err)
	}
	if got := result.Environments(); !slices.Equal(got, []string{"dev", "staging", "prod"}) {
		t.Fatalf("environments: %v", got)
	}
	if got := run(`{`, kinds).Environments(); got != nil {
		t.Fatalf("environments of a registry not read: %v", got)
	}
	if got := run(`{}`, kinds).Pins(); got != nil {
		t.Fatalf("pins of an invalid registry: %v", got)
	}
}
