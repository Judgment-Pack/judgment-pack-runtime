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

// §3.4 "Program size": a pattern's size is counted on the pattern as
// written, and only a pattern within the subset is held to it.
func TestAPatternsSizeIsCountedAsWritten(t *testing.T) {
	for _, c := range []struct {
		source string
		size   int
	}{
		{`a`, 1}, {`ab`, 2}, {`\.`, 1}, {`\d`, 1}, {`.`, 1}, {`[a-z]`, 1}, {`^a$`, 3}, {`(a)`, 1}, {`()`, 1},
		{`a*`, 3}, {`a+`, 2}, {`a?`, 2}, {`a|`, 3}, {`|`, 3}, {`a|b|c`, 5}, {`(ab|ac)`, 5},
		{`a{3}`, 3}, {`[ab]{2,5}`, 8}, {`(a){0}`, 1}, {`x{0,0}`, 1}, {`x{0,}`, 3}, {`x{2,}`, 3}, {`(ab){2,}`, 5},
		{`(a*)*`, 5}, {`(a|b)*c+d?`, 9},
		{`a{1000}a{1000}a{1000}a{1000}a{96}`, 4096}, {`((a{2}){500}){0}`, 1},
		{`((a{10}){10}){10}`, 1000}, {`(((a{1000}){1}){1}){1}a`, 1001},
	} {
		if _, size, err := translateSubset(c.source); err != nil || size != c.size {
			t.Errorf("%s: size %d, %v; want %d", c.source, size, err, c.size)
		}
	}
	for _, c := range []struct {
		source, code string
	}{
		{`a{1000}a{1000}a{1000}a{1000}a{96}`, ""},
		{`a{1000}a{1000}a{1000}a{1000}a{97}`, "REG-KIND-PATTERN-LIMIT"},
		{`(ab|ac){1000}`, "REG-KIND-PATTERN-LIMIT"},
		{`((a{2}){500}){0}`, ""},
		{`((a{2}){501}){0}`, "REG-KIND-PATTERN"},
		{`((a{1000}){1000}){1000}`, "REG-KIND-PATTERN"},
		{`(a{1000}){5}\w`, "REG-KIND-PATTERN"},
	} {
		if _, code := (&patternBudget{}).compile(c.source); code != c.code {
			t.Errorf("%s: %q, want %q", c.source, code, c.code)
		}
	}
	// Nested counts saturate rather than overflow.
	nested := strings.Repeat("(", 25) + "a" + strings.Repeat("){1000}", 25)
	if _, size, err := translateSubset(nested); err != nil || size != MaxPatternInstructions+1 {
		t.Errorf("25 nested {1000}: size %d, %v", size, err)
	}
	// The kind's total is the same size: sixteen patterns of 4096 fill it.
	budget := &patternBudget{}
	for i := 0; i < 16; i++ {
		if _, code := budget.compile(`a{1000}a{1000}a{1000}a{1000}a{96}`); code != "" {
			t.Fatalf("pattern %d: %s", i, code)
		}
	}
	if _, code := budget.compile(`a`); code != "REG-KIND-PATTERN-LIMIT" || budget.used != MaxKindPatternInstructions {
		t.Errorf("past the kind's total: %q, used %d", code, budget.used)
	}
}
