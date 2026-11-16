package registry

import (
	"slices"
	"strings"
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

// §6.2: an entry reference's #pointer is at most 512 code points as
// written; a pointer pattern (§3.5) stays at most 512 bytes.
func TestAnEntryReferencePointerIsBoundedInCodePoints(t *testing.T) {
	for _, c := range []struct {
		codePoints int
		admitted   bool
	}{{300, true}, {512, true}, {513, false}} {
		pointer := "/" + strings.Repeat("é", c.codePoints-1)
		if _, ok := parseEntryReference("entry:database/billing#" + pointer); ok != c.admitted {
			t.Errorf("a pointer of %d code points (%d bytes): admitted %v", c.codePoints, len(pointer), ok)
		}
	}
	if _, err := parsePointerPattern("/" + strings.Repeat("é", 299)); err == nil {
		t.Error("a pointer pattern of 599 bytes was admitted")
	}
}

// §3.1: an id is an RFC 3986 absolute-URI, in ASCII, of at most 2048
// characters. The same rule holds a registry's id, a pin's id and a facts
// contract's ids, which share the reader.
func TestAnIDIsAnRFC3986AbsoluteURI(t *testing.T) {
	long := "https://example.invalid/" + strings.Repeat("k", 2048-len("https://example.invalid/"))
	for _, id := range []string{
		"https://example.invalid/kinds/k", "urn:example:kind", "a:", "s:/a", "s:%41", "a+b-c.d:x",
		"tag:example.invalid,2026:k", "mailto:team@example.invalid", "file:///kinds/k",
		"https://user:pw@example.invalid:8443/a?b=c/d?e", "https://example.invalid/%7ek?", "https://example.invalid:/k",
		"https://[2001:db8::1]:443/k", "https://[::ffff:192.0.2.1]/k", "https://[v1.fe:x]/", "https://192.0.2.1/k",
		"https://example.invalid/!$&'()*+,;=:@-._~", long,
	} {
		if !isAbsoluteURI(id) {
			t.Errorf("refused %q", id)
		}
	}
	for _, id := range []string{
		"", ":x", "1a:b", "a_b:x", "//example.invalid/k", "example.invalid/k", long + "k",
		"https://example.invalid/k#f", "https://example.invalid/k#", "https://exämple.invalid/", "urn:é",
		"https://example.invalid/<k>", `https://example.invalid/\k`, `https://example.invalid/"k"`,
		"https://example.invalid/{k}", "https://example.invalid/^k", "https://example.invalid/`k`",
		"https://example.invalid/a|b", "https://example.invalid/a b", "https://example.invalid/a\tb", "urn:a\n",
		"https://example.invalid/%4", "https://example.invalid/%zz", "https://example.invalid/%",
		"https://example.invalid/[k]", "https://example.invalid/k?[q]", "https://[2001:db8::1/k", "https://example.invalid]/",
		"https://[fe80::1%25eth0]/", "https://[192.0.2.1]/", "https://[2001:db8::1]x/", "https://[v1.]/", "https://[vg.x]/",
		"https://a@b@example.invalid/", "https://example.invalid:8x/", "https://example.invalid:1:2/",
	} {
		if isAbsoluteURI(id) {
			t.Errorf("admitted %q", id)
		}
	}
}
