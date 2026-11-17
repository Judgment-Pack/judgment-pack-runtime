package dbrolefacts

// These tests compile and run the code the emitter writes for the
// db-role-generated vector (the files beside this one, which the emitter's
// own tests hold to its output), and compare what Build returns with
// canonical JSON written by hand.

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

const registryDigest = "sha256:89abcdef0123456789abcdef0123456789abcdef0123456789abcdef01234567"

// facts is the RFC 0037 §2 example entry, with synthetic values.
func facts() *DbRoleFacts {
	f := NewDbRoleFacts(
		NewDbRoleEntry("billing-api", DbRoleEntryKindDbRole, "team-billing"),
		DbRoleEnvironmentProd,
		NewDbRoleReferences(NewDbRoleReferencesDatabase("billing", DbRoleReferencesDatabaseKindDatabase,
			NewDbRoleReferencesDatabaseValue("postgres"))),
		NewDbRoleRegistry(registryDigest, "https://example.invalid/registries/platform", "4.0.0"),
		NewDbRoleValue("entry:database/billing", []DbRoleValuePrivilegesItem{DbRoleValuePrivilegesItemRead, DbRoleValuePrivilegesItemWrite}),
	)
	f.Value.ConnectionLimit = Ptr(MustParseDecimal("120"))
	f.Value.Password = NewDbRoleValuePassword("example-prod", DbRoleValuePasswordStoreGcpSm)
	return f
}

const wantFacts = `{"entry":{"id":"billing-api","kind":"db-role","owner":"team-billing"},"environment":"prod",` +
	`"references":{"/database":{"id":"billing","kind":"database","value":{"engine":"postgres"}}},` +
	`"registry":{"digest":"` + registryDigest + `","id":"https://example.invalid/registries/platform","version":"4.0.0"},` +
	`"value":{"connectionLimit":"120","database":"entry:database/billing","password":{"scope":"example-prod","store":"gcp-sm"},"privileges":["read","write"]}}`

// build runs Build and checks its output against the runtime's own RFC 8785
// writer.
func build(t *testing.T, f *DbRoleFacts) string {
	t.Helper()
	out, err := f.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(out))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("Build wrote JSON that does not decode: %v", err)
	}
	canonical, err := jcs.EncodeDocument(value)
	if err != nil || !bytes.Equal(canonical, out) {
		t.Fatalf("Build wrote %s, whose canonical form is %s (%v)", out, canonical, err)
	}
	return string(out)
}

func TestBuild(t *testing.T) {
	if got := build(t, facts()); got != wantFacts {
		t.Fatalf("Build =\n%s\nwant\n%s", got, wantFacts)
	}
}

func TestOptionalMembersLeftOut(t *testing.T) {
	f := facts()
	f.Value.ConnectionLimit = nil
	f.Value.Password = nil
	f.Value.Privileges = nil
	want := `{"entry":{"id":"billing-api","kind":"db-role","owner":"team-billing"},"environment":"prod",` +
		`"references":{"/database":{"id":"billing","kind":"database","value":{"engine":"postgres"}}},` +
		`"registry":{"digest":"` + registryDigest + `","id":"https://example.invalid/registries/platform","version":"4.0.0"},` +
		`"value":{"database":"entry:database/billing","privileges":[]}}`
	if got := build(t, f); got != want {
		t.Fatalf("Build =\n%s\nwant\n%s", got, want)
	}
}

func TestDecimalNormalForms(t *testing.T) {
	for _, c := range []struct {
		in   Decimal
		want string
	}{
		{MustParseDecimal("1.0"), "1"},
		{MustParseDecimal("100"), "100"},
		{MustParseDecimal("1.50"), "1.5"},
		{MustParseDecimal("0.0000001"), "0.0000001"},
		{MustParseDecimal("-0"), "0"},
		{MustParseDecimal("-0.000"), "0"},
		{MustParseDecimal("9007199254740991"), "9007199254740991"},
		{MustParseDecimal("-9007199254740991"), "-9007199254740991"},
		{MustParseDecimal("123456789012345678901234567890.5"), "123456789012345678901234567890.5"},
		{DecimalFromInt64(1<<53 - 1), "9007199254740991"},
		{DecimalFromInt64(-(1<<53 - 1)), "-9007199254740991"},
		{DecimalFromInt64(0), "0"},
		{DecimalFromInt64(math.MinInt64), "-9223372036854775808"},
		{Decimal{}, "0"},
	} {
		f := facts()
		f.Value.ConnectionLimit = &c.in
		want := `"connectionLimit":"` + c.want + `"`
		if got := build(t, f); !bytes.Contains([]byte(got), []byte(want)) {
			t.Errorf("Build holds no %s: %s", want, got)
		}
		if c.in.String() != c.want {
			t.Errorf("String = %q, want %q", c.in.String(), c.want)
		}
	}
}

// TestParseDecimalRefuses: only a whole string of the JPS Core decimal
// grammar is a Decimal. An exponent ("1e-7", whose value is written
// "0.0000001") is outside it.
func TestParseDecimalRefuses(t *testing.T) {
	for _, s := range []string{"", "-", "1e-7", "1E7", "01", "+1", ".5", "1.", "-.5", " 1", "1 ", "0x1", "1.2.3", "NaN", "Infinity", "１", "1_000"} {
		if d, err := ParseDecimal(s); !errors.Is(err, ErrNotDecimal) || d != (Decimal{}) {
			t.Errorf("ParseDecimal(%q) = %v, %v; want ErrNotDecimal", s, d, err)
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustParseDecimal took 1e-7")
		}
	}()
	MustParseDecimal("1e-7")
}

func refused(t *testing.T, f *DbRoleFacts, pointer, reason string) {
	t.Helper()
	out, err := f.Build()
	var b *BuildError
	if out != nil || !errors.As(err, &b) || b.Pointer != pointer || b.Reason != reason {
		t.Fatalf("Build = %q, %v; want a refusal at %q: %s", out, err, pointer, reason)
	}
}

const (
	notOneOf  = "the value is not one of the contract's values"
	isNil     = "a value the contract requires is nil"
	notUTF8   = "the string is not valid UTF-8"
	errorText = `facts: "/value/privileges/1": the value is not one of the contract's values`
)

func TestValuesOutsideTheContractAreRefused(t *testing.T) {
	f := facts()
	f.Environment = "qa"
	refused(t, f, "/environment", notOneOf)

	f = facts()
	f.Environment = ""
	refused(t, f, "/environment", notOneOf)

	f = facts()
	f.Entry.Kind = "DB-ROLE"
	refused(t, f, "/entry/kind", notOneOf)

	f = facts()
	f.References.Database.Kind = "table"
	refused(t, f, "/references/~1database/kind", notOneOf)

	f = facts()
	f.Value.Password.Store = "aws-sm"
	refused(t, f, "/value/password/store", notOneOf)

	f = facts()
	f.Value.Privileges = append(f.Value.Privileges[:1], "owner")
	refused(t, f, "/value/privileges/1", notOneOf)
	if _, err := f.Build(); err.Error() != errorText {
		t.Fatalf("Error = %q, want %q", err.Error(), errorText)
	}
}

func TestMissingRequiredValuesAreRefused(t *testing.T) {
	var none *DbRoleFacts
	refused(t, none, "", isNil)

	f := facts()
	f.Entry = nil
	refused(t, f, "/entry", isNil)

	f = facts()
	f.References.Database = nil
	refused(t, f, "/references/~1database", isNil)

	f = facts()
	f.References.Database.Value = nil
	refused(t, f, "/references/~1database/value", isNil)

	f = facts()
	f.Registry = nil
	refused(t, f, "/registry", isNil)

	f = facts()
	f.Value = nil
	refused(t, f, "/value", isNil)
}

func TestStrings(t *testing.T) {
	f := facts()
	f.Entry.Owner = "q\"b\\s\b\t\n\f\r\x00\x1f\x7f/é😀"
	want := `"owner":"q\"b\\s\b\t\n\f\r\u0000\u001f` + "\x7f/é😀" + `"`
	if got := build(t, f); !bytes.Contains([]byte(got), []byte(want)) {
		t.Fatalf("Build holds no %s: %s", want, got)
	}

	f = facts()
	f.Value.Password.Scope = "example\xff"
	refused(t, f, "/value/password/scope", notUTF8)
}

// TestCanonicalOrder: object members are in RFC 8785 order, by UTF-16 code
// units, which is not code-point order above U+FFFF.
func TestCanonicalOrder(t *testing.T) {
	names := sortedNames(map[string]int{"": 1, "😀": 2, "b": 3, "a": 4, "é": 5, "aa": 6})
	if want := []string{"a", "aa", "b", "é", "😀", ""}; !slices.Equal(names, want) {
		t.Fatalf("sortedNames = %q, want %q", names, want)
	}
	if got := member("/labels", "a/b~c"); got != "/labels/a~1b~0c" {
		t.Fatalf("member = %q", got)
	}
	if got := index("/lines", 12); got != "/lines/12" {
		t.Fatalf("index = %q", got)
	}
}
