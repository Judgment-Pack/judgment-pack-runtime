package dbroleloader

// The decode test of the golden package: the secret-position vector's
// generated files, which cmd/jpack-emit-go-loader's
// TestGoldenPackageIsTheVectorOutput holds to the emitter's output, compiled
// against the start-up module and run on resolved documents. Every value is
// synthetic.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/startup"
)

const (
	kindID   = "https://example.invalid/kinds/db-role"
	devRef   = "secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/3"
	otherRef = "secret:gcp-sm:projects/example-dev/secrets/other-db/versions/1"
	variable = "DBROLELOADER_GOLDEN_PASSWORD"
)

func pin(kind, id, version string) map[string]any {
	return map[string]any{"kind": kind, "id": id, "version": version, "digest": "sha256:" + strings.Repeat("ab", 32)}
}

func entry(kind, id string, value any) map[string]any {
	return map[string]any{"kind": kind, "id": id, "owner": "platform-data", "value": value}
}

// document writes a resolved document for dev, its pins and entries given in
// canonical order.
func document(t *testing.T, pins []any, entries ...any) []byte {
	t.Helper()
	if entries == nil {
		entries = []any{}
	}
	data, err := json.Marshal(map[string]any{
		"resolvedFormatVersion": "1.0.0",
		"registry":              map[string]any{"id": "https://example.invalid/registries/platform", "version": "4.0.0", "digest": "sha256:" + strings.Repeat("cd", 32)},
		"environment":           "dev",
		"kinds":                 pins,
		"entries":               entries,
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// registryDocument is a resolved document with an entry of another kind,
// which the package does not read, and two db-role entries.
func registryDocument(t *testing.T) []byte {
	return document(t,
		[]any{pin("database", "https://example.invalid/kinds/database", "1.0.0"), pin("db-role", kindID, "1.2.0")},
		entry("database", "billing", map[string]any{"engine": "postgres"}),
		entry("db-role", "billing-app", map[string]any{
			"database": "entry:database:billing", "privileges": []any{"read", "write"}, "password": devRef,
			"connectionLimit": 20, "comment": "a member the type does not have",
		}),
		entry("db-role", "reporting", map[string]any{"database": "entry:database:billing", "privileges": []any{"read"}}),
	)
}

func bindings() startup.Bindings {
	return startup.Bindings{Env: map[string]string{devRef: variable}}
}

func TestLoadDecodesEachEntry(t *testing.T) {
	t.Setenv(variable, "golden-password\n")
	r, err := Load(registryDocument(t), "dev", bindings())
	if err != nil {
		t.Fatal(err)
	}
	if ids := r.DbRoleIDs(); !slices.Equal(ids, []string{"billing-app", "reporting"}) {
		t.Fatalf("DbRoleIDs = %v", ids)
	}
	role, err := r.DbRole(context.Background(), "billing-app")
	if err != nil {
		t.Fatal(err)
	}
	if role.Database != "entry:database:billing" || !slices.Equal(role.Privileges, []DbRolePrivilegesItem{DbRolePrivilegesItemRead, DbRolePrivilegesItemWrite}) {
		t.Errorf("billing-app: %+v", role)
	}
	if role.ConnectionLimit == nil || *role.ConnectionLimit != 20 {
		t.Errorf("billing-app connectionLimit: %v", role.ConnectionLimit)
	}
	if role.Password == nil || string(role.Password.Reveal()) != "golden-password" {
		t.Errorf("billing-app password was not read through the reader")
	}
	reporting, err := r.DbRole(context.Background(), "reporting")
	if err != nil {
		t.Fatal(err)
	}
	if reporting.Password != nil || reporting.ConnectionLimit != nil || !slices.Equal(reporting.Privileges, []DbRolePrivilegesItem{DbRolePrivilegesItemRead}) {
		t.Errorf("reporting: %+v", reporting)
	}
}

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolved.json")
	if err := os.WriteFile(path, registryDocument(t), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := LoadFile(path, "dev", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ids := r.DbRoleIDs(); len(ids) != 2 {
		t.Fatalf("DbRoleIDs = %v", ids)
	}
	if _, err := LoadFile(path, "prod", nil); !errors.Is(err, startup.ErrEnvironment) {
		t.Fatalf("LoadFile for another environment: %v", err)
	}
}

// TestSecretsAreReadLazily: Load reads no secret, so a Registry loads with
// no binding for any of them; a secret is read only when its entry is
// decoded, and one with no binding refuses that entry alone.
func TestSecretsAreReadLazily(t *testing.T) {
	r, err := Load(registryDocument(t), "dev", startup.Bindings{Env: map[string]string{otherRef: variable}})
	if err != nil {
		t.Fatalf("Load read a secret: %v", err)
	}
	if _, err := r.DbRole(context.Background(), "reporting"); err != nil {
		t.Fatalf("an entry with no secret: %v", err)
	}
	_, err = r.DbRole(context.Background(), "billing-app")
	if !errors.Is(err, ErrSecret) || !errors.Is(err, startup.ErrNoBinding) {
		t.Fatalf("an entry whose secret has no binding: %v", err)
	}
	if noReader, err := Load(registryDocument(t), "dev", nil); err != nil {
		t.Fatal(err)
	} else if _, err := noReader.DbRole(context.Background(), "billing-app"); !errors.Is(err, ErrSecret) {
		t.Fatalf("a secret with no reader: %v", err)
	}
}

type zeroReader struct{}

func (zeroReader) ReadSecret(context.Context, string) (startup.Secret, error) {
	return startup.Secret{}, nil
}

func TestAReaderThatReturnsNoValueIsRefused(t *testing.T) {
	r, err := Load(registryDocument(t), "dev", zeroReader{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.DbRole(context.Background(), "billing-app"); !errors.Is(err, ErrSecret) {
		t.Fatalf("a zero Secret: %v", err)
	}
}

func TestNoSuchEntry(t *testing.T) {
	r, err := Load(registryDocument(t), "dev", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.DbRole(context.Background(), "billing"); !errors.Is(err, ErrNoEntry) {
		t.Fatalf("an entry of another kind's id: %v", err)
	}
}

// TestLoadHoldsThePinToTheKind: the document must pin db-role with the id
// the package was generated for, at version 1.x; a minor version either way
// only widens, so it is read.
func TestLoadHoldsThePinToTheKind(t *testing.T) {
	value := map[string]any{"database": "entry:database:billing", "privileges": []any{}}
	for _, c := range []struct {
		name string
		pins []any
		want error
	}{
		{"the version generated from", []any{pin("db-role", kindID, "1.2.0")}, nil},
		{"an older minor version", []any{pin("db-role", kindID, "1.0.0")}, nil},
		{"a newer minor version", []any{pin("db-role", kindID, "1.9.3")}, nil},
		{"another major version", []any{pin("db-role", kindID, "2.0.0")}, ErrKind},
		{"major version 0", []any{pin("db-role", kindID, "0.1.0")}, ErrKind},
		{"a version with no dot", []any{pin("db-role", kindID, "1")}, ErrKind},
		{"another id", []any{pin("db-role", "https://example.invalid/kinds/other-role", "1.2.0")}, ErrKind},
		{"no pin", []any{pin("database", "https://example.invalid/kinds/database", "1.0.0")}, ErrKind},
	} {
		var entries []any
		if c.pins[0].(map[string]any)["kind"] == "db-role" {
			entries = []any{entry("db-role", "a", value)}
		}
		_, err := Load(document(t, c.pins, entries...), "dev", nil)
		if !errors.Is(err, c.want) || (c.want == nil) != (err == nil) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

// TestValuesThatDoNotFitAreRefused: Load decodes every entry, and refuses one
// whose value does not fit the type, with an error that names the position
// and quotes no value.
func TestValuesThatDoNotFitAreRefused(t *testing.T) {
	const marker = "golden-value-marker"
	for _, c := range []struct {
		name  string
		value string
		at    string
	}{
		{"a fraction", `{"database":"d","privileges":[],"connectionLimit":1.5}`, "/connectionLimit"},
		{"an integer as a string", `{"database":"d","privileges":[],"connectionLimit":"20"}`, "/connectionLimit"},
		{"an integer past int64", `{"database":"d","privileges":[],"connectionLimit":9223372036854775808}`, "/connectionLimit"},
		{"a value outside the enumeration", `{"database":"d","privileges":["read","` + marker + `"]}`, "/privileges/1"},
		{"a required member absent", `{"privileges":[]}`, "/database"},
		{"a member named in another case", `{"Database":"` + marker + `","privileges":[]}`, "/database"},
		{"null for a string", `{"database":null,"privileges":[]}`, "/database"},
		{"an object for an array", `{"database":"d","privileges":{}}`, "/privileges"},
		{"a secret position that is not a string", `{"database":"d","privileges":[],"password":7}`, "/password"},
		{"a secret position that is not a reference", `{"database":"d","privileges":[],"password":"` + marker + `"}`, "/password"},
		{"a value that is not an object", `["` + marker + `"]`, ""},
	} {
		data := document(t, []any{pin("db-role", kindID, "1.2.0")}, entry("db-role", "a", json.RawMessage(c.value)))
		_, err := Load(data, "dev", nil)
		if !errors.Is(err, ErrValue) {
			t.Errorf("%s: %v, want ErrValue", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), `at "`+c.at+`"`) || strings.Contains(err.Error(), marker) {
			t.Errorf("%s: %q does not name %q, or quotes the value", c.name, err, c.at)
		}
	}
}

// TestIntegerForms: an integer is any JSON number of integer value within
// int64, whatever its spelling, as JSON Schema's integer is.
func TestIntegerForms(t *testing.T) {
	for literal, want := range map[string]int64{
		"120": 120, "120.0": 120, "1.2e2": 120, "1.2E+2": 120, "12000e-2": 120, "0": 0, "-0": 0, "0.0e-999999999": 0,
		"-7": -7, "9223372036854775807": 9223372036854775807, "-9223372036854775808": -9223372036854775808,
		"9.223372036854775807e18": 9223372036854775807, "1e0000002": 100,
	} {
		value := `{"database":"d","privileges":[],"connectionLimit":` + literal + `}`
		r, err := Load(document(t, []any{pin("db-role", kindID, "1.2.0")}, entry("db-role", "a", json.RawMessage(value))), "dev", nil)
		if err != nil {
			t.Errorf("%s: %v", literal, err)
			continue
		}
		role, err := r.DbRole(context.Background(), "a")
		if err != nil || role.ConnectionLimit == nil || *role.ConnectionLimit != want {
			t.Errorf("%s: %v, %v; want %d", literal, role, err, want)
		}
	}
	for _, literal := range []string{"1.5", "1e-1", "1e19", "9223372036854775808", "-9223372036854775809", "1e1000000", "12001e-2"} {
		value := `{"database":"d","privileges":[],"connectionLimit":` + literal + `}`
		if _, err := Load(document(t, []any{pin("db-role", kindID, "1.2.0")}, entry("db-role", "a", json.RawMessage(value))), "dev", nil); !errors.Is(err, ErrValue) {
			t.Errorf("%s: %v, want ErrValue", literal, err)
		}
	}
}

// TestTheSecretIsNeverPrinted: a decoded entry printed whole shows no
// secret byte.
func TestTheSecretIsNeverPrinted(t *testing.T) {
	t.Setenv(variable, "golden-password")
	r, err := Load(registryDocument(t), "dev", bindings())
	if err != nil {
		t.Fatal(err)
	}
	role, err := r.DbRole(context.Background(), "billing-app")
	if err != nil {
		t.Fatal(err)
	}
	printed, err := json.Marshal(role)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(printed), "golden-password") {
		t.Fatalf("the secret was printed: %s", printed)
	}
}
