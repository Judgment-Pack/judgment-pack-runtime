package startup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A resolved document written by hand in RFC 0034 §5's form, in RFC 8785
// canonical member order, for environment prod. Every name and digest is
// synthetic.
const prodDocument = `{"entries":[` +
	`{"id":"billing","kind":"database","owner":"team-billing","value":{"engine":"postgres"}},` +
	`{"id":"billing-api","kind":"db-role","owner":"team-billing","value":{"connectionLimit":120,"database":"entry:database/billing","password":"secret:gcp-sm:projects/example-prod/secrets/billing-db/versions/7","privileges":["read","write"]}},` +
	`{"id":"reporting","kind":"db-role","owner":"team-data","value":{"database":"entry:database/billing","privileges":["read"]}}` +
	`],"environment":"prod","kinds":[` +
	`{"digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111","id":"https://example.invalid/kinds/database","kind":"database","version":"1.0.0"},` +
	`{"digest":"sha256:2222222222222222222222222222222222222222222222222222222222222222","id":"https://example.invalid/kinds/db-role","kind":"db-role","version":"1.2.0"}` +
	`],"registry":{"digest":"sha256:3333333333333333333333333333333333333333333333333333333333333333","id":"https://example.invalid/registries/platform","version":"4.0.0"},` +
	`"resolvedFormatVersion":"1.0.0"}`

// variant replaces one exact piece of prodDocument, and fails the test when
// that piece is not there, so a variant never silently tests the original.
func variant(t *testing.T, old, replacement string) []byte {
	t.Helper()
	if strings.Count(prodDocument, old) != 1 {
		t.Fatalf("the document holds %q %d times, want once", old, strings.Count(prodDocument, old))
	}
	return []byte(strings.Replace(prodDocument, old, replacement, 1))
}

func TestReadResolvedReadsOneEnvironment(t *testing.T) {
	resolved, err := ReadResolved([]byte(prodDocument), "prod")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ResolvedFormatVersion != "1.0.0" || resolved.Environment != "prod" {
		t.Errorf("got %q, %q", resolved.ResolvedFormatVersion, resolved.Environment)
	}
	wantRegistry := RegistryIdentity{ID: "https://example.invalid/registries/platform", Version: "4.0.0", Digest: "sha256:" + strings.Repeat("3", 64)}
	if resolved.Registry != wantRegistry {
		t.Errorf("registry: got %+v", resolved.Registry)
	}
	if len(resolved.Kinds) != 2 || resolved.Kinds[1] != (KindPin{Kind: "db-role", ID: "https://example.invalid/kinds/db-role", Version: "1.2.0", Digest: "sha256:" + strings.Repeat("2", 64)}) {
		t.Errorf("kinds: got %+v", resolved.Kinds)
	}
	if len(resolved.Entries) != 3 {
		t.Fatalf("entries: got %d", len(resolved.Entries))
	}
	role, ok := resolved.Entry("db-role", "billing-api")
	if !ok || role.Owner != "team-billing" {
		t.Fatalf("got %+v, %v", role, ok)
	}
	// The value is the document's bytes, references as written.
	wantValue := `{"connectionLimit":120,"database":"entry:database/billing","password":"secret:gcp-sm:projects/example-prod/secrets/billing-db/versions/7","privileges":["read","write"]}`
	if string(role.Value) != wantValue {
		t.Errorf("value: got %s", role.Value)
	}
	for _, missing := range [][2]string{{"db-role", "billing"}, {"database", "billing-api"}, {"db-rol", "billing-api"}, {"", ""}, {"zzz", "a"}} {
		if entry, ok := resolved.Entry(missing[0], missing[1]); ok {
			t.Errorf("%v: found %+v", missing, entry)
		}
	}
	if roles := resolved.EntriesOf("db-role"); len(roles) != 2 || roles[0].ID != "billing-api" || roles[1].ID != "reporting" {
		t.Errorf("EntriesOf db-role: got %+v", roles)
	}
	if databases := resolved.EntriesOf("database"); len(databases) != 1 || databases[0].ID != "billing" {
		t.Errorf("EntriesOf database: got %+v", databases)
	}
	for _, kind := range []string{"", "data", "db-role-x", "zzz"} {
		if entries := resolved.EntriesOf(kind); len(entries) != 0 {
			t.Errorf("EntriesOf %q: got %+v", kind, entries)
		}
	}
	resolved.EntriesOf("db-role")[0].ID = "changed"
	if resolved.Entries[1].ID != "billing-api" {
		t.Error("EntriesOf returned the document's own slice")
	}
	if pin, ok := resolved.Kind("database"); !ok || pin.Version != "1.0.0" {
		t.Errorf("Kind database: got %+v, %v", pin, ok)
	}
	if _, ok := resolved.Kind("service"); ok {
		t.Error("found a kind the registry does not pin")
	}
}

func TestReadResolvedAcceptsEmptyEntriesAndInsignificantSpace(t *testing.T) {
	document := " {\n\t\"resolvedFormatVersion\" : \"1.0.0\", \"registry\": {\"id\": \"r\", \"version\": \"1.0.0\", \"digest\": \"d\"},\r\n" +
		"\"environment\": \"dev\", \"kinds\": [ ], \"entries\": [ ] } \n"
	resolved, err := ReadResolved([]byte(document), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Entries) != 0 || len(resolved.Kinds) != 0 || len(resolved.EntriesOf("x")) != 0 {
		t.Errorf("got %+v", resolved)
	}
	if _, ok := resolved.Entry("x", "y"); ok {
		t.Error("found an entry in an empty document")
	}
}

func TestReadResolvedRefusesASelectorItDoesNotRead(t *testing.T) {
	for _, selector := range []string{`"1.0"`, `"1"`, `"1.0.0 "`, `" 1.0.0"`, `"v1.0.0"`, `"1.0.1"`, `"1.1.0"`, `"2.0.0"`, `"0.9.0"`,
		`"1.0.0-draft"`, `"1.0.0+build"`, `"1.00.0"`, `""`, `1`, `1.0`, `null`, `true`, `["1.0.0"]`, `{"v":"1.0.0"}`} {
		document := variant(t, `"resolvedFormatVersion":"1.0.0"`, `"resolvedFormatVersion":`+selector)
		if _, err := ReadResolved(document, "prod"); !errors.Is(err, ErrSelector) {
			t.Errorf("%s: got %v, want ErrSelector", selector, err)
		}
	}
	missing := variant(t, `,"resolvedFormatVersion":"1.0.0"`, ``)
	if _, err := ReadResolved(missing, "prod"); !errors.Is(err, ErrSelector) {
		t.Errorf("missing: got %v, want ErrSelector", err)
	}
	// The selector is read before any other member, so a later format's
	// document is refused as a selector this module does not read, not as an
	// unknown member.
	later := variant(t, `"resolvedFormatVersion":"1.0.0"`, `"resolvedFormatVersion":"1.1.0","secretScopes":{}`)
	if _, err := ReadResolved(later, "prod"); !errors.Is(err, ErrSelector) {
		t.Errorf("later format: got %v, want ErrSelector", err)
	}
	for _, root := range []string{`[]`, `"1.0.0"`, `1`, `null`, `[{"resolvedFormatVersion":"1.0.0"}]`} {
		if _, err := ReadResolved([]byte(root), "prod"); !errors.Is(err, ErrSelector) {
			t.Errorf("root %s: got %v, want ErrSelector", root, err)
		}
	}
	versions := ResolvedFormatVersions()
	if len(versions) != 1 || versions[0] != "1.0.0" {
		t.Fatalf("got %v", versions)
	}
	versions[0] = "9.9.9"
	if ResolvedFormatVersions()[0] != "1.0.0" {
		t.Error("ResolvedFormatVersions returned the module's own slice")
	}
}

func TestReadResolvedRefusesAnotherEnvironment(t *testing.T) {
	for _, environment := range []string{"dev", "staging", "Prod", "prod ", "pro", "prod-1", ""} {
		_, err := ReadResolved([]byte(prodDocument), environment)
		if !errors.Is(err, ErrEnvironment) {
			t.Errorf("%q: got %v, want ErrEnvironment", environment, err)
		}
	}
	// The refusal names the environment the caller expected, never what the
	// document holds.
	document := variant(t, `"environment":"prod"`, `"environment":"never-printed"`)
	_, err := ReadResolved(document, "prod")
	if !errors.Is(err, ErrEnvironment) || strings.Contains(err.Error(), "never-printed") {
		t.Errorf("got %v", err)
	}
}

func TestReadResolvedRefusesDuplicateMembers(t *testing.T) {
	cases := map[string][]byte{
		// encoding/json alone would read prod from this, and a first-wins
		// reader dev.
		"environment": variant(t, `"environment":"prod"`, `"environment":"dev","environment":"prod"`),
		"selector":    variant(t, `"resolvedFormatVersion":"1.0.0"}`, `"resolvedFormatVersion":"2.0.0","resolvedFormatVersion":"1.0.0"}`),
		"registry id": variant(t, `"id":"https://example.invalid/registries/platform"`, `"id":"a","id":"https://example.invalid/registries/platform"`),
		"in a value":  variant(t, `{"engine":"postgres"}`, `{"engine":"postgres","engine":"mysql"}`),
		"deep":        variant(t, `{"engine":"postgres"}`, `{"a":[{"b":{"c":1,"c":2}}]}`),
		"empty name":  variant(t, `{"engine":"postgres"}`, `{"":1,"":2}`),
		"escaped":     variant(t, `{"engine":"postgres"}`, `{"engine":"postgres","engine":"mysql"}`),
	}
	for name, document := range cases {
		if _, err := ReadResolved(document, "prod"); !errors.Is(err, ErrDocument) {
			t.Errorf("%s: got %v, want ErrDocument", name, err)
		}
	}
	// One name in two objects is not a duplicate.
	twice := variant(t, `{"engine":"postgres"}`, `{"engine":"postgres","nested":{"engine":"x"},"list":[{"engine":1},{"engine":2}]}`)
	if _, err := ReadResolved(twice, "prod"); err != nil {
		t.Errorf("one name in several objects: got %v", err)
	}
	// A member named in another case is another member, unknown here,
	// whatever encoding/json's case-insensitive field matching would make of it.
	cased := variant(t, `"environment":"prod"`, `"environment":"prod","Environment":"dev"`)
	if _, err := ReadResolved(cased, "prod"); !errors.Is(err, ErrDocument) {
		t.Errorf("a member in another case: got %v, want ErrDocument", err)
	}
	upper := variant(t, `"environment":"prod"`, `"ENVIRONMENT":"prod"`)
	if _, err := ReadResolved(upper, "prod"); !errors.Is(err, ErrDocument) {
		t.Errorf("the environment member in upper case: got %v, want ErrDocument", err)
	}
}

func TestReadResolvedHoldsTheBounds(t *testing.T) {
	nest := func(depth int) []byte {
		// The root, entries, an entry and its value are four levels; the
		// value nests the rest.
		value := strings.Repeat("[", depth-4) + "1" + strings.Repeat("]", depth-4)
		return variant(t, `{"engine":"postgres"}`, `{"engine":`+value+`}`)
	}
	if _, err := ReadResolved(nest(MaxResolvedDepth), "prod"); err != nil {
		t.Errorf("at the depth bound: got %v", err)
	}
	if _, err := ReadResolved(nest(MaxResolvedDepth+1), "prod"); !errors.Is(err, ErrLimit) {
		t.Errorf("one past the depth bound: got %v, want ErrLimit", err)
	}
	if _, err := ReadResolved([]byte(strings.Repeat("[", 10000)), "prod"); !errors.Is(err, ErrLimit) {
		t.Errorf("deep and unterminated: got %v, want ErrLimit", err)
	}
	pad := func(size int) []byte {
		base := variant(t, `"engine":"postgres"`, `"engine":"postgres","pad":""`)
		return variant(t, `"engine":"postgres"`, `"engine":"postgres","pad":"`+strings.Repeat("p", size-len(base))+`"`)
	}
	at := pad(MaxResolvedBytes)
	if len(at) != MaxResolvedBytes {
		t.Fatalf("built %d bytes", len(at))
	}
	if _, err := ReadResolved(at, "prod"); err != nil {
		t.Errorf("at the size bound: got %v", err)
	}
	if _, err := ReadResolved(pad(MaxResolvedBytes+1), "prod"); !errors.Is(err, ErrLimit) {
		t.Errorf("one past the size bound: got %v, want ErrLimit", err)
	}
}

func TestReadResolvedRefusesWhatIsNotOneJSONValue(t *testing.T) {
	for name, document := range map[string][]byte{
		"empty":      nil,
		"space":      []byte(" \n"),
		"unclosed":   []byte(prodDocument[:len(prodDocument)-1]),
		"two values": []byte(prodDocument + prodDocument),
		"trailing":   []byte(prodDocument + "x"),
		"trailing ,": []byte(prodDocument + ","),
		"not UTF-8":  variant(t, `"postgres"`, "\"post\xffgres\""),
		"single":     []byte(`{'resolvedFormatVersion':'1.0.0'}`),
		"comment":    []byte("// c\n" + prodDocument),
		"bare word":  variant(t, `"postgres"`, `postgres`),
		"comma":      variant(t, `"postgres"}`, `"postgres",}`),
	} {
		if _, err := ReadResolved(document, "prod"); !errors.Is(err, ErrDocument) {
			t.Errorf("%s: got %v, want ErrDocument", name, err)
		}
	}
}

func TestReadResolvedRefusesAMalformedMember(t *testing.T) {
	// segment replaces the top-level member that starts at from and ends
	// where the member next starts.
	segment := func(from, next, replacement string) []byte {
		start, end := strings.Index(prodDocument, from), strings.Index(prodDocument, next)
		if start < 0 || end < start {
			t.Fatalf("no member %s before %s", from, next)
		}
		return []byte(prodDocument[:start] + replacement + prodDocument[end:])
	}
	cases := map[string][]byte{
		"unknown top-level":     variant(t, `"environment":"prod"`, `"environment":"prod","extra":1`),
		"missing environment":   variant(t, `"environment":"prod",`, ``),
		"missing registry":      variant(t, `"registry":{"digest":"sha256:3333333333333333333333333333333333333333333333333333333333333333","id":"https://example.invalid/registries/platform","version":"4.0.0"},`, ``),
		"missing kinds":         segment(`"kinds":`, `"registry":`, ``),
		"missing entries":       segment(`"entries":`, `"environment":`, ``),
		"environment number":    variant(t, `"environment":"prod"`, `"environment":1`),
		"environment null":      variant(t, `"environment":"prod"`, `"environment":null`),
		"registry array":        segment(`"registry":`, `"resolvedFormatVersion":`, `"registry":[],`),
		"registry unknown":      variant(t, `"version":"4.0.0"}`, `"version":"4.0.0","x":""}`),
		"registry missing":      variant(t, `,"version":"4.0.0"}`, `}`),
		"registry number":       variant(t, `"version":"4.0.0"}`, `"version":4}`),
		"kinds object":          segment(`"kinds":`, `"registry":`, `"kinds":{},`),
		"kinds null":            segment(`"kinds":`, `"registry":`, `"kinds":null,`),
		"kind string":           variant(t, `"kinds":[`, `"kinds":["x",`),
		"kind unknown":          variant(t, `"kind":"database","version":"1.0.0"}`, `"kind":"database","version":"1.0.0","x":1}`),
		"kind missing digest":   variant(t, `{"digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111","id"`, `{"id"`),
		"kind version number":   variant(t, `"kind":"database","version":"1.0.0"}`, `"kind":"database","version":1}`),
		"entries null":          segment(`"entries":`, `"environment":`, `"entries":null,`),
		"entries object":        segment(`"entries":`, `"environment":`, `"entries":{},`),
		"entry array":           variant(t, `{"entries":[`, `{"entries":[[],`),
		"entry unknown":         variant(t, `"owner":"team-data",`, `"owner":"team-data","environments":["prod"],`),
		"entry missing value":   variant(t, `,"value":{"engine":"postgres"}`, ``),
		"entry missing owner":   variant(t, `"owner":"team-data",`, ``),
		"entry id number":       variant(t, `{"id":"reporting"`, `{"id":7`),
		"entry kind null":       variant(t, `"kind":"database","owner"`, `"kind":null,"owner"`),
		"entry owner object":    variant(t, `"owner":"team-data"`, `"owner":{}`),
		"entry in another case": variant(t, `{"id":"reporting"`, `{"ID":"reporting"`),
	}
	for name, document := range cases {
		_, err := ReadResolved(document, "prod")
		if !errors.Is(err, ErrDocument) {
			t.Errorf("%s: got %v, want ErrDocument", name, err)
		}
	}
}

func TestReadResolvedRefusesEntriesOutOfOrder(t *testing.T) {
	database := `{"id":"billing","kind":"database","owner":"team-billing","value":{"engine":"postgres"}},`
	role := `{"id":"billing-api","kind":"db-role","owner":"team-billing","value":{"connectionLimit":120,"database":"entry:database/billing","password":"secret:gcp-sm:projects/example-prod/secrets/billing-db/versions/7","privileges":["read","write"]}},`
	reporting := `{"id":"reporting","kind":"db-role","owner":"team-data","value":{"database":"entry:database/billing","privileges":["read"]}}`
	entries := func(items ...string) []byte {
		joined := strings.TrimSuffix(strings.Join(items, ""), ",")
		return variant(t, database+role+reporting, joined)
	}
	reportingFirst := reporting + ","
	for name, document := range map[string][]byte{
		"kind order":      entries(role, database, reporting),
		"id order":        entries(database, reportingFirst, role),
		"duplicate entry": entries(database, role, role),
		"duplicate id":    entries(database, reportingFirst, reporting),
	} {
		if _, err := ReadResolved(document, "prod"); !errors.Is(err, ErrDocument) {
			t.Errorf("%s: got %v, want ErrDocument", name, err)
		}
	}
	databasePin := `{"digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111","id":"https://example.invalid/kinds/database","kind":"database","version":"1.0.0"}`
	rolePin := `{"digest":"sha256:2222222222222222222222222222222222222222222222222222222222222222","id":"https://example.invalid/kinds/db-role","kind":"db-role","version":"1.2.0"}`
	for name, kinds := range map[string]string{
		"kind pins out of order": rolePin + "," + databasePin,
		"a kind pinned twice":    databasePin + "," + databasePin,
	} {
		document := variant(t, databasePin+","+rolePin, kinds)
		if _, err := ReadResolved(document, "prod"); !errors.Is(err, ErrDocument) {
			t.Errorf("%s: got %v, want ErrDocument", name, err)
		}
	}
	// Code-point order: an id that is a prefix of another comes first.
	prefix := entries(database, `{"id":"billing","kind":"db-role","owner":"team-billing","value":{}},`, role, reporting)
	if _, err := ReadResolved(prefix, "prod"); err != nil {
		t.Errorf("a prefix before its extension: got %v", err)
	}
}

func TestReadResolvedErrorsCarryNoValue(t *testing.T) {
	documents := [][]byte{
		variant(t, `"privileges":["read"]`, `"privileges":["read"],"privileges":"never-printed"`),
		variant(t, `"owner":"team-data"`, `"owner":"team-data","never-printed":1`),
		variant(t, `"resolvedFormatVersion":"1.0.0"`, `"resolvedFormatVersion":"never-printed"`),
		variant(t, `"version":"4.0.0"}`, `"version":"4.0.0","never-printed":""}`),
	}
	for _, document := range documents {
		_, err := ReadResolved(document, "prod")
		if err == nil {
			t.Fatal("a malformed document was read")
		}
		for _, part := range []string{"never-printed", "billing-db", "example-prod", "postgres"} {
			if strings.Contains(err.Error(), part) {
				t.Errorf("the refusal %q carries %q", err, part)
			}
		}
	}
	_, err := ReadResolved(variant(t, `"owner":"team-data"`, `"owner":7`), "prod")
	if err == nil || !strings.Contains(err.Error(), "/entries/2/owner") {
		t.Errorf("the refusal does not locate the member: %v", err)
	}
}

func TestReadResolvedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prod.json")
	if err := os.WriteFile(path, []byte(prodDocument), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := ReadResolvedFile(path, "prod")
	if err != nil || len(resolved.Entries) != 3 {
		t.Fatalf("got %v", err)
	}
	if _, err := ReadResolvedFile(path, "dev"); !errors.Is(err, ErrEnvironment) {
		t.Errorf("dev: got %v, want ErrEnvironment", err)
	}
	if _, err := ReadResolvedFile(filepath.Join(dir, "absent.json"), "prod"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("absent: got %v", err)
	}
	large := filepath.Join(dir, "large.json")
	if err := os.WriteFile(large, bytes.Repeat([]byte(" "), MaxResolvedBytes+2), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadResolvedFile(large, "prod"); !errors.Is(err, ErrLimit) {
		t.Errorf("over the bound: got %v, want ErrLimit", err)
	}
	if _, err := ReadResolvedFile(dir, "prod"); err == nil {
		t.Error("a directory was read as a document")
	}
}
