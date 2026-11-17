package registry

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// The fuzz targets here hold the reference readers to the registry
// companion's grammars (§3.5, §6.2, §6.3) on any input. Each grammar is
// restated as one expression, apart from the readers' code, and an input is
// accepted exactly when it matches. A crasher a scheduled run finds is checked
// in under testdata/fuzz/<target>/ by the pull request that fixes it.

// entryReferenceGrammar is §6.2's entry-ref, but for the pointer's bound of
// 512 code points, which is counted apart: a kind name, an entry-id of 1 to
// 128 of U+0021..U+007E but / and #, and an RFC 6901 pointer, in which a ~ is
// followed by 0 or 1.
var entryReferenceGrammar = regexp.MustCompile(`^entry:[a-z][a-z0-9-]{0,62}/[\x21\x22\x24-\x2e\x30-\x7e]{1,128}(#(/([^~]|~[01])*)?)?$`)

// secretReferenceGrammar is §6.3's secret-ref of the one store of 1.0.0.
var secretReferenceGrammar = regexp.MustCompile(`^secret:gcp-sm:projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/secrets/[A-Za-z0-9_-]{1,255}/versions/[1-9][0-9]{0,8}$`)

// secretNameGrammar is §6.3's name.
var secretNameGrammar = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)

// pointerPatternGrammar is §3.5's pointer pattern, but for its bound of 512
// bytes: it begins with /, and a ~ is followed by 0, 1 or 2.
var pointerPatternGrammar = regexp.MustCompile(`^/([^~]|~[012])*$`)

// renderEntryReference writes a parsed entry reference back as text.
func renderEntryReference(reference entryReference) string {
	text := "entry:" + reference.kind + "/" + reference.id
	if reference.hasPointer {
		text += "#" + pointerOf(reference.pointer)
	}
	return text
}

// FuzzEntryReference: parseEntryReference never panics, gives one input the
// same result twice, and accepts exactly the input of §6.2's grammar. An
// accepted reference in UTF-8, as every string of an admitted document is
// (§2.2), written back is the input, and reads back the same.
func FuzzEntryReference(f *testing.F) {
	for _, seed := range []string{
		"entry:database/billing", "entry:database/billing#/engine", "entry:database/billing#",
		"entry:node/a", "entry:node/b", "entry:blob/x#/a~0b/c~1d/0", "entry:k/!\"$%&'()*+,-.:;<=>?@[\\]^_`{|}~",
		"entry:database/billing#" + "/" + strings.Repeat("é", 511), "entry:database/billing#" + "/" + strings.Repeat("é", 512),
		"billing", "entry:", "entry:database", "entry:database/", "entry:Database/billing", "entry:1db/billing",
		"entry:database/billing#engine", "entry:database/billing#/a~2", "entry:database/billing#/a~", "entry:database/a b",
		"entry:database/billing/x", "entry:database/" + strings.Repeat("i", 129), "entry:database/billing#/a#b",
		"secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/3", "entry:a/0#/\xff0",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		reference, ok := parseEntryReference(text)
		again, okAgain := parseEntryReference(text)
		if ok != okAgain || !reflect.DeepEqual(reference, again) {
			t.Fatalf("%q: two reads differ", text)
		}
		_, pointer, hasPointer := strings.Cut(text, "#")
		grammar := entryReferenceGrammar.MatchString(text) && (!hasPointer || utf8.RuneCountInString(pointer) <= 512)
		if ok != grammar {
			t.Fatalf("%q: accepted %v, the grammar says %v", text, ok, grammar)
		}
		if !ok {
			if !reflect.DeepEqual(reference, entryReference{}) {
				t.Fatalf("%q: refused with a value %+v", text, reference)
			}
			return
		}
		if !utf8.ValidString(text) {
			// Written back, a byte that is not UTF-8 becomes U+FFFD.
			return
		}
		rendered := renderEntryReference(reference)
		if rendered != text {
			t.Fatalf("%q: written back as %q", text, rendered)
		}
		if back, ok := parseEntryReference(rendered); !ok || !reflect.DeepEqual(back, reference) {
			t.Fatalf("%q: read back as %+v (%v), not %+v", text, back, ok, reference)
		}
	})
}

// secretCodeBySpec is §6.3's precedence, step by step as the companion
// lists it, for a string value.
func secretCodeBySpec(text string) string {
	rest, ok := strings.CutPrefix(text, "secret:")
	if !ok {
		return "REF-SECRET-FORM"
	}
	store, locator, ok := strings.Cut(rest, ":")
	switch {
	case !ok:
		return "REF-SECRET-FORM"
	case store != "gcp-sm":
		return "REF-SECRET-STORE"
	}
	segments := strings.Split(locator, "/")
	if len(segments) != 6 || segments[0] != "projects" || segments[2] != "secrets" || segments[4] != "versions" {
		return "REF-SECRET-FORM"
	}
	project, name, version := segments[1], segments[3], segments[5]
	allDigits := func(s string) bool { return s != "" && strings.Trim(s, "0123456789") == "" }
	if secretNameGrammar.MatchString(name) && version != "" && (!allDigits(version) || allDigits(project)) {
		return "REF-SECRET-PIN"
	}
	if !secretReferenceGrammar.MatchString(text) {
		return "REF-SECRET-FORM"
	}
	return ""
}

// FuzzSecretReference: parseSecretReference never panics, gives one input
// the same result twice, accepts exactly the input of §6.3's grammar, holds
// the store and project the input names, and gives a refusal the one code
// §6.3's precedence gives it. A value that is not a string is a form error.
func FuzzSecretReference(f *testing.F) {
	for _, seed := range []string{
		"secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/3",
		"secret:gcp-sm:projects/example-prod/secrets/billing-db/versions/7",
		"secret:gcp-sm:projects/example-dev/secrets/x/versions/999999999",
		"secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/latest",
		"secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/0",
		"secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/03",
		"secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/1000000000",
		"secret:gcp-sm:projects/123456789/secrets/billing-db/versions/3",
		"secret:gcp-sm:projects/Example-dev/secrets/billing-db/versions/3",
		"secret:gcp-sm:projects/example-dev/secrets/billing.db/versions/3",
		"secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/",
		"secret:gcp-sm:projects/example-dev/secrets/billing-db",
		"secret:vault:projects/example-dev/secrets/billing-db/versions/3", "secret::x", "secret:gcp-sm",
		"secret:gcp-sm:zz-db", "hunter2", "", "entry:database/billing",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		reference, code := parseSecretReference(text)
		again, codeAgain := parseSecretReference(text)
		if code != codeAgain || reference != again {
			t.Fatalf("%q: two reads differ", text)
		}
		if want := secretCodeBySpec(text); code != want {
			t.Fatalf("%q: code %q, §6.3 gives %q", text, code, want)
		}
		if (code == "") != secretReferenceGrammar.MatchString(text) {
			t.Fatalf("%q: code %q against the grammar", text, code)
		}
		if code != "" {
			if reference != (secretReference{}) {
				t.Fatalf("%q: refused with a value %+v", text, reference)
			}
		} else if project := strings.Split(text, "/")[1]; reference.store != StoreGCPSecretManager || reference.project != project {
			t.Fatalf("%q: read as %+v", text, reference)
		}
		if _, code := parseSecretReference([]byte(text)); code != "REF-SECRET-FORM" {
			t.Fatalf("%q as bytes: code %q", text, code)
		}
	})
}

// renderPointerPattern writes a pattern's tokens back as a pattern.
func renderPointerPattern(tokens []patternToken) string {
	var out strings.Builder
	for _, token := range tokens {
		out.WriteByte('/')
		if token.any {
			out.WriteByte('*')
			continue
		}
		out.WriteString(strings.NewReplacer("~", "~0", "/", "~1", "*", "~2").Replace(token.literal))
	}
	return out.String()
}

// openSchema reaches every pointer (§3.5: an absent additionalProperties
// reaches every token below it), and closedSchema few.
const (
	openSchema   = `{"type":"object"}`
	closedSchema = `{"type":"object","additionalProperties":false,"properties":{"database":{"type":"string"},` +
		`"grants":{"type":"array","items":{"type":"object","properties":{"*":{"type":"string"},"role":{"type":"string"}}}}}}`
)

// readReferenceKind admits a kind declaration with one references item and
// reads it, as a registry run's steps 1 and 2 do (ValidateDocuments). It
// returns the kind, its diagnostics, and the pointer and kinds as admitted.
func readReferenceKind(t *testing.T, schema, pointer string, kinds []string) (*Kind, []string, string, []string) {
	t.Helper()
	item, err := json.Marshal(map[string]any{"pointer": pointer, "kinds": kinds})
	if err != nil {
		t.Fatal(err)
	}
	document := `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/fuzz","version":"1.0.0","kind":"fuzz",` +
		`"owner":"platform","schema":` + schema + `,"references":[` + string(item) + `]}`
	collected := &collector{}
	location := Location{Document: DocumentKind, KindDocument: rawDigest([]byte(document))}
	root := admit([]byte(document), MaxKindBytes, "kindFormatVersion", KindFormatVersion, location, collected)
	if root == nil {
		t.Fatalf("%s: not admitted: %v", document, collected.sorted())
	}
	admitted := root["references"].([]any)[0].(map[string]any)
	var admittedKinds []string
	for _, name := range admitted["kinds"].([]any) {
		admittedKinds = append(admittedKinds, name.(string))
	}
	kind := readKind(root, location, collected)
	var found []string
	for _, d := range collected.sorted() {
		found = append(found, d.Code+" "+d.Location.Pointer)
	}
	slices.Sort(found)
	return kind, found, admitted["pointer"].(string), admittedKinds
}

// FuzzKindReferencePatterns: a kind's references (§3.1, §3.5), read through
// admission and readKind, never panic and read the same twice. Under a
// schema that reaches every pointer, a declaration is valid exactly when its
// pattern is of §3.5's grammar and its kinds are distinct kind names, each
// failure has its own code at its own location, and an accepted pattern
// written back from its tokens reads back to the same tokens. Under a schema
// that reaches few, an accepted pattern is still of the grammar.
func FuzzKindReferencePatterns(f *testing.F) {
	for _, seed := range []struct {
		pointer, first, second string
		closed                 bool
	}{
		{"/database", "database", "", false}, {"/database", "database", "", true},
		{"/next", "node", "", false}, {"/refs/*", "blob", "", false}, {"/grants/*/role", "role", "account", true},
		{"/grants/0/~2", "role", "", true}, {"/a~0b/c~1d/~2", "k", "", false}, {"/", "k", "", false},
		{"", "database", "", false}, {"database", "database", "", false}, {"/a~3", "database", "", false},
		{"/a~", "database", "", false}, {"/" + strings.Repeat("é", 255), "k", "", false}, {"/" + strings.Repeat("é", 256), "k", "", false},
		{"/database", "Database", "", false}, {"/database", "database", "database", false}, {"/database", "", "", false},
		{"/password/inner", "database", "", true}, {"/x/y", "a-b", "c0", false},
	} {
		f.Add(seed.pointer, seed.first, seed.second, seed.closed)
	}
	f.Fuzz(func(t *testing.T, pointer, first, second string, closed bool) {
		if len(pointer) > 4096 || len(first) > 256 || len(second) > 256 {
			return
		}
		kinds := []string{first}
		if second != "" {
			kinds = append(kinds, second)
		}
		schema := openSchema
		if closed {
			schema = closedSchema
		}
		kind, found, admittedPointer, admittedKinds := readReferenceKind(t, schema, pointer, kinds)
		againKind, againFound, _, _ := readReferenceKind(t, schema, pointer, kinds)
		if (kind == nil) != (againKind == nil) || !slices.Equal(found, againFound) ||
			kind != nil && fmt.Sprint(kind.references) != fmt.Sprint(againKind.references) {
			t.Fatalf("%q %q: two reads differ: %v, %v", pointer, kinds, found, againFound)
		}
		pattern := len(admittedPointer) <= 512 && pointerPatternGrammar.MatchString(admittedPointer)
		if kind != nil {
			if !pattern {
				t.Fatalf("%q: accepted outside §3.5's grammar", admittedPointer)
			}
			if len(kind.references) != 1 || kind.references[0].pattern.source != admittedPointer || !slices.Equal(kind.references[0].kinds, admittedKinds) {
				t.Fatalf("%q %q: read as %+v", admittedPointer, admittedKinds, kind.references)
			}
			tokens := kind.references[0].pattern.tokens
			back, err := parsePointerPattern(renderPointerPattern(tokens))
			if err != nil || !reflect.DeepEqual(back.tokens, tokens) {
				t.Fatalf("%q: written back as %q, read back as %+v (%v)", admittedPointer, renderPointerPattern(tokens), back.tokens, err)
			}
		}
		if closed {
			return
		}
		named := true
		for i, name := range admittedKinds {
			if !kindNamePattern.MatchString(name) || slices.Contains(admittedKinds[:i], name) {
				named = false
			}
		}
		var want []string
		if !named {
			want = append(want, "REG-KIND-FORM /references/0/kinds")
		}
		if !pattern {
			want = append(want, "REG-KIND-POINTER-FORM /references/0/pointer")
		}
		if !slices.Equal(found, want) || (kind != nil) != (len(want) == 0) {
			t.Fatalf("%q %q: got %v (valid %v), want %v", admittedPointer, admittedKinds, found, kind != nil, want)
		}
	})
}
