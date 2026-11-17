package registry

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Every fixture is synthetic: example.invalid ids and made-up projects.

const databaseKind = `{
  "kindFormatVersion": "1.0.0",
  "id": "https://example.invalid/kinds/database",
  "version": "1.0.0",
  "kind": "database",
  "owner": "platform-data",
  "schema": {
    "type": "object", "required": ["engine"], "additionalProperties": false,
    "properties": { "engine": { "enum": ["postgres", "mysql"] }, "region": { "type": "string" } }
  }
}`

const roleKind = `{
  "kindFormatVersion": "1.0.0",
  "id": "https://example.invalid/kinds/db-role",
  "version": "1.2.0",
  "kind": "db-role",
  "owner": "platform-data",
  "entryId": { "pattern": "^[a-z][a-z0-9-]{0,62}$" },
  "schema": {
    "type": "object", "required": ["database", "privileges"], "additionalProperties": false,
    "properties": {
      "database": { "type": "string", "maxLength": 200 },
      "privileges": { "type": "array", "items": { "enum": ["read", "write", "admin"] }, "uniqueItems": true },
      "password": { "type": "string" },
      "connectionLimit": { "type": "integer", "minimum": 1, "maximum": 500 },
      "legacyFlag": { "type": "boolean", "deprecated": true }
    }
  },
  "secrets": ["/password"],
  "references": [ { "pointer": "/database", "kinds": ["database"] } ],
  "overlayable": ["/connectionLimit", "/password"],
  "bounds": { "maxEntries": 1000, "maxEntryBytes": 16384 }
}`

const registryTemplate = `{
  "registryFormatVersion": "1.0.0",
  "id": "https://example.invalid/registries/platform",
  "version": "4.0.0",
  "environments": ["dev", "staging", "prod"],
  "owners": ["platform-data", "team-billing"],
  "secretScopes": {
    "dev": { "gcp-sm": ["example-dev"] },
    "staging": { "gcp-sm": ["example-staging"] },
    "prod": { "gcp-sm": ["example-prod"] }
  },
  "kinds": [
    { "kind": "database", "id": "https://example.invalid/kinds/database", "version": "1.0.0", "digest": "DATABASE" },
    { "kind": "db-role", "id": "https://example.invalid/kinds/db-role", "version": "1.2.0", "digest": "ROLE" }
  ],
  "entries": [
    { "kind": "database", "id": "billing", "owner": "team-billing", "value": { "engine": "postgres" } },
    { "kind": "db-role", "id": "billing-api", "owner": "team-billing",
      "value": { "database": "entry:database/billing", "privileges": ["read", "write"],
                 "password": "secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/3",
                 "connectionLimit": 20 } },
    { "kind": "db-role", "id": "billing-backfill", "owner": "team-billing", "environments": ["dev", "staging"],
      "value": { "database": "entry:database/billing#/engine", "privileges": ["read"] } }
  ],
  "overlays": [
    { "environment": "staging",
      "patches": [ { "kind": "db-role", "id": "billing-api", "set": {
        "/password": "secret:gcp-sm:projects/example-staging/secrets/billing-db/versions/5" } } ] },
    { "environment": "prod",
      "patches": [ { "kind": "db-role", "id": "billing-api", "set": { "/connectionLimit": 120,
        "/password": "secret:gcp-sm:projects/example-prod/secrets/billing-db/versions/7" } } ] }
  ]
}`

func digestOf(t *testing.T, document string) string {
	t.Helper()
	var value any
	decoder := json.NewDecoder(strings.NewReader(document))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	digest, err := Digest(value)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func fixture(t *testing.T) (string, []string) {
	t.Helper()
	registry := strings.NewReplacer("DATABASE", digestOf(t, databaseKind), "ROLE", digestOf(t, roleKind)).Replace(registryTemplate)
	return registry, []string{databaseKind, roleKind}
}

func run(registry string, kinds []string) *Result {
	documents := make([][]byte, len(kinds))
	for i, kind := range kinds {
		documents[i] = []byte(kind)
	}
	return Validate([]byte(registry), documents)
}

func decode(t *testing.T, document string) map[string]any {
	t.Helper()
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(document))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func encode(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestValidRegistryResolves(t *testing.T) {
	registry, kinds := fixture(t)
	result := run(registry, kinds)
	if !result.Report.Valid {
		t.Fatalf("want valid, got %+v", result.Report.Diagnostics)
	}
	if len(result.Report.Diagnostics) != 0 {
		t.Fatalf("want no diagnostics, got %+v", result.Report.Diagnostics)
	}
	prod, err := result.Resolve("prod")
	if err != nil {
		t.Fatal(err)
	}
	text := string(prod)
	for _, want := range []string{
		`"connectionLimit":120`,
		`"password":"secret:gcp-sm:projects/example-prod/secrets/billing-db/versions/7"`,
		`"resolvedFormatVersion":"1.0.0"`,
		`"environment":"prod"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("prod lacks %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, "billing-backfill") {
		t.Error("an entry that does not apply in prod was resolved into it")
	}
	dev, _ := result.Resolve("dev")
	if !strings.Contains(string(dev), `"connectionLimit":20`) || !strings.Contains(string(dev), "billing-backfill") {
		t.Errorf("dev is not the base values:\n%s", dev)
	}
	again, _ := run(registry, []string{roleKind, databaseKind}).Resolve("prod")
	if !bytes.Equal(prod, again) {
		t.Error("resolution depends on the order kinds are supplied in")
	}
	if _, err := result.Resolve("qa"); err == nil {
		t.Error("an undeclared environment must be refused")
	}
}

type mutation struct {
	name     string
	registry func(map[string]any)
	role     func(map[string]any)
	code     string
	pointer  string
	document string
	env      string
}

func entryValue(registry map[string]any, index int) map[string]any {
	return registry["entries"].([]any)[index].(map[string]any)["value"].(map[string]any)
}

func prodSet(registry map[string]any) map[string]any {
	overlay := registry["overlays"].([]any)[1].(map[string]any)
	return overlay["patches"].([]any)[0].(map[string]any)["set"].(map[string]any)
}

func TestEachRuleReportsItsCode(t *testing.T) {
	cases := []mutation{
		{name: "unknown registry member", registry: func(r map[string]any) { r["extra"] = true }, code: "REG-REGISTRY-UNKNOWN-MEMBER", pointer: "/extra"},
		{name: "missing owners", registry: func(r map[string]any) { delete(r, "owners") }, code: "REG-REGISTRY-MISSING-MEMBER", pointer: "/owners"},
		{name: "bad environment name", registry: func(r map[string]any) { r["environments"] = []any{"Dev"} }, code: "REG-REGISTRY-FORM", pointer: "/environments/0"},
		{name: "duplicate environment", registry: func(r map[string]any) { r["environments"] = []any{"dev", "dev"} }, code: "REG-REGISTRY-DUPLICATE", pointer: "/environments/1"},
		{name: "entries out of order", registry: func(r map[string]any) {
			entries := r["entries"].([]any)
			entries[1], entries[2] = entries[2], entries[1]
		}, code: "REG-REGISTRY-ORDER", pointer: "/entries/2"},
		{name: "unpinned entry kind", registry: func(r map[string]any) {
			r["entries"].([]any)[0].(map[string]any)["kind"] = "cache"
		}, code: "REG-ENTRY-KIND", pointer: "/entries/0/kind"},
		{name: "entry id outside its pattern", registry: func(r map[string]any) {
			r["entries"].([]any)[1].(map[string]any)["id"] = "Billing-API"
		}, code: "REG-ENTRY-ID", pointer: "/entries/1/id"},
		{name: "unknown owner", registry: func(r map[string]any) {
			r["entries"].([]any)[0].(map[string]any)["owner"] = "team-other"
		}, code: "REG-ENTRY-OWNER", pointer: "/entries/0/owner"},
		{name: "undeclared entry environment", registry: func(r map[string]any) {
			r["entries"].([]any)[2].(map[string]any)["environments"] = []any{"dev", "qa"}
		}, code: "REG-ENTRY-ENVIRONMENT", pointer: "/entries/2/environments/1"},
		{name: "pin digest missing", registry: func(r map[string]any) {
			r["kinds"].([]any)[0].(map[string]any)["digest"] = "sha256:" + strings.Repeat("0", 64)
		}, code: "REG-PIN-MISSING", pointer: "/kinds/0"},
		{name: "pin version mismatch", registry: func(r map[string]any) {
			r["kinds"].([]any)[1].(map[string]any)["version"] = "1.3.0"
		}, code: "REG-PIN-MISMATCH", pointer: "/kinds/1"},
		{name: "overlay for undeclared environment", registry: func(r map[string]any) {
			r["overlays"].([]any)[1].(map[string]any)["environment"] = "qa"
		}, code: "REG-OVERLAY-ENVIRONMENT", pointer: "/overlays/1/environment"},
		{name: "two overlays for one environment", registry: func(r map[string]any) {
			r["overlays"].([]any)[1].(map[string]any)["environment"] = "staging"
		}, code: "REG-OVERLAY-DUPLICATE", pointer: "/overlays/1/environment"},
		{name: "patch for a missing entry", registry: func(r map[string]any) {
			r["overlays"].([]any)[1].(map[string]any)["patches"].([]any)[0].(map[string]any)["id"] = "nobody"
		}, code: "REG-OVERLAY-ENTRY-MISSING", pointer: "/overlays/1/patches/0"},
		{name: "patch for an entry absent in the environment", registry: func(r map[string]any) {
			patch := r["overlays"].([]any)[1].(map[string]any)["patches"].([]any)[0].(map[string]any)
			patch["id"] = "billing-backfill"
			patch["set"] = map[string]any{"/connectionLimit": json.Number("5")}
		}, code: "REG-OVERLAY-ENTRY-ABSENT", pointer: "/overlays/1/patches/0"},
		{name: "patch on a member that is not overlayable", registry: func(r map[string]any) {
			prodSet(r)["/privileges"] = []any{"admin"}
		}, code: "REG-OVERLAY-NOT-OVERLAYABLE", pointer: "/overlays/1/patches/0/set"},
		{name: "patch sets null", registry: func(r map[string]any) { prodSet(r)["/connectionLimit"] = nil }, code: "REG-OVERLAY-NULL", pointer: "/overlays/1/patches/0/set/~1connectionLimit"},
		{name: "malformed patch pointer", registry: func(r map[string]any) { prodSet(r)["connectionLimit"] = json.Number("3") }, code: "REG-OVERLAY-POINTER", pointer: "/overlays/1/patches/0/set"},
		{name: "overlay breaks the schema in one environment", registry: func(r map[string]any) {
			prodSet(r)["/connectionLimit"] = json.Number("900")
		}, code: "REG-VALUE-RANGE", pointer: "/connectionLimit", document: DocumentValue, env: "prod"},
		{name: "wrong type", registry: func(r map[string]any) { entryValue(r, 1)["privileges"] = "read" }, code: "REG-VALUE-TYPE", pointer: "/privileges", document: DocumentValue, env: "dev"},
		{name: "not in enum", registry: func(r map[string]any) { entryValue(r, 0)["engine"] = "oracle" }, code: "REG-VALUE-ENUM", pointer: "/engine", document: DocumentValue, env: "dev"},
		{name: "required member absent", registry: func(r map[string]any) { delete(entryValue(r, 0), "engine") }, code: "REG-VALUE-REQUIRED", pointer: "/engine", document: DocumentValue, env: "dev"},
		{name: "additional member", registry: func(r map[string]any) { entryValue(r, 0)["size"] = "large" }, code: "REG-VALUE-ADDITIONAL", pointer: "/size", document: DocumentValue, env: "dev"},
		{name: "items not unique", registry: func(r map[string]any) { entryValue(r, 1)["privileges"] = []any{"read", "read"} }, code: "REG-VALUE-UNIQUE", pointer: "/privileges", document: DocumentValue, env: "dev"},
		{name: "deprecated member", registry: func(r map[string]any) { entryValue(r, 1)["legacyFlag"] = true }, code: "REG-VALUE-DEPRECATED", pointer: "/legacyFlag", document: DocumentValue, env: "dev"},
		{name: "secret written as a literal", registry: func(r map[string]any) { entryValue(r, 1)["password"] = "hunter2" }, code: "REF-SECRET-FORM", pointer: "/password", document: DocumentValue, env: "dev"},
		{name: "secret not pinned", registry: func(r map[string]any) {
			entryValue(r, 1)["password"] = "secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/latest"
		}, code: "REF-SECRET-PIN", pointer: "/password", document: DocumentValue, env: "dev"},
		{name: "secret version zero", registry: func(r map[string]any) {
			entryValue(r, 1)["password"] = "secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/0"
		}, code: "REF-SECRET-FORM", pointer: "/password", document: DocumentValue, env: "dev"},
		{name: "secret project number", registry: func(r map[string]any) {
			entryValue(r, 1)["password"] = "secret:gcp-sm:projects/123456789/secrets/billing-db/versions/3"
		}, code: "REF-SECRET-PIN", pointer: "/password", document: DocumentValue, env: "dev"},
		{name: "secret in an unknown store", registry: func(r map[string]any) {
			entryValue(r, 1)["password"] = "secret:vault:kv/billing"
		}, code: "REF-SECRET-STORE", pointer: "/password", document: DocumentValue, env: "dev"},
		{name: "production secret from a development project", registry: func(r map[string]any) {
			prodSet(r)["/password"] = "secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/7"
		}, code: "REF-SECRET-SCOPE", pointer: "/password", document: DocumentValue, env: "prod"},
		{name: "no scopes declared", registry: func(r map[string]any) { delete(r, "secretScopes") }, code: "REF-SECRET-UNSCOPED", pointer: "/password", document: DocumentValue, env: "dev"},
		{name: "scope shared between environments", registry: func(r map[string]any) {
			r["secretScopes"].(map[string]any)["staging"] = map[string]any{"gcp-sm": []any{"example-dev"}}
		}, code: "REF-SECRET-SCOPE-SHARED", pointer: "/secretScopes/staging/gcp-sm/0"},
		{name: "scope missing for an environment", registry: func(r map[string]any) {
			delete(r["secretScopes"].(map[string]any), "staging")
		}, code: "REF-SECRET-SCOPE-FORM", pointer: "/secretScopes"},
		{name: "entry reference malformed", registry: func(r map[string]any) { entryValue(r, 1)["database"] = "billing" }, code: "REF-ENTRY-FORM", pointer: "/database", document: DocumentValue, env: "dev"},
		{name: "entry reference to a missing entry", registry: func(r map[string]any) { entryValue(r, 1)["database"] = "entry:database/ledger" }, code: "REF-ENTRY-MISSING", pointer: "/database", document: DocumentValue, env: "dev"},
		{name: "entry reference to a kind the position does not allow", registry: func(r map[string]any) {
			entryValue(r, 1)["database"] = "entry:db-role/billing-backfill"
		}, code: "REF-ENTRY-KIND", pointer: "/database", document: DocumentValue, env: "dev"},
		{name: "entry reference absent in an environment", registry: func(r map[string]any) {
			r["entries"].([]any)[0].(map[string]any)["environments"] = []any{"dev", "staging"}
		}, code: "REF-ENTRY-ABSENT-IN-ENVIRONMENT", pointer: "/database", document: DocumentValue, env: "prod"},
		{name: "entry reference pointer addresses nothing", registry: func(r map[string]any) {
			entryValue(r, 2)["database"] = "entry:database/billing#/region"
		}, code: "REF-ENTRY-POINTER", pointer: "/database", document: DocumentValue, env: "dev"},
		{name: "kind's maxEntries exceeded", role: func(k map[string]any) {
			k["bounds"] = map[string]any{"maxEntries": json.Number("1")}
		}, code: "REG-BOUND-ENTRIES", pointer: "/kinds/1"},
		{name: "kind's maxEntryBytes exceeded", role: func(k map[string]any) {
			k["bounds"] = map[string]any{"maxEntryBytes": json.Number("20")}
		}, code: "REG-BOUND-ENTRY-BYTES", pointer: "", document: DocumentValue, env: "dev"},
		{name: "deprecated kind", role: func(k map[string]any) { k["deprecated"] = true }, code: "REG-KIND-DEPRECATED", pointer: "/kinds/1"},
		{name: "kind member unknown", role: func(k map[string]any) { k["colour"] = "blue" }, code: "REG-KIND-UNKNOWN-MEMBER", pointer: "/colour", document: DocumentKind},
		{name: "kind schema keyword outside the subset", role: func(k map[string]any) {
			k["schema"].(map[string]any)["properties"].(map[string]any)["database"].(map[string]any)["format"] = "uri"
		}, code: "REG-KIND-SCHEMA-KEYWORD", pointer: "/schema/properties/database/format", document: DocumentKind},
		{name: "kind schema oneOf", role: func(k map[string]any) {
			k["schema"].(map[string]any)["oneOf"] = []any{true}
		}, code: "REG-KIND-SCHEMA-KEYWORD", pointer: "/schema/oneOf", document: DocumentKind},
		{name: "kind schema pattern with look-ahead", role: func(k map[string]any) {
			k["schema"].(map[string]any)["properties"].(map[string]any)["database"].(map[string]any)["pattern"] = "^(?=a)"
		}, code: "REG-KIND-PATTERN", pointer: "/schema/properties/database/pattern", document: DocumentKind},
		{name: "kind $ref to nothing", role: func(k map[string]any) {
			k["schema"].(map[string]any)["properties"].(map[string]any)["database"] = map[string]any{"$ref": "#/$defs/missing"}
		}, code: "REG-KIND-SCHEMA-REF", pointer: "/schema/properties/database/$ref", document: DocumentKind},
		{name: "kind pointer pattern unreachable", role: func(k map[string]any) {
			k["secrets"] = []any{"/password/inner"}
		}, code: "REG-KIND-POINTER-UNREACHABLE", pointer: "/secrets/0", document: DocumentKind},
		{name: "kind secrets pattern not a pointer", role: func(k map[string]any) {
			k["secrets"] = []any{"password"}
		}, code: "REG-KIND-POINTER-FORM", pointer: "/secrets/0", document: DocumentKind},
		{name: "kind reference pattern not a pointer", role: func(k map[string]any) {
			k["references"] = []any{map[string]any{"pointer": "database", "kinds": []any{"database"}}}
		}, code: "REG-KIND-POINTER-FORM", pointer: "/references/0/pointer", document: DocumentKind},
		{name: "kind position both secret and reference", role: func(k map[string]any) {
			k["secrets"] = []any{"/database"}
		}, code: "REG-KIND-POINTER-CONFLICT", pointer: "/secrets/0", document: DocumentKind},
		{name: "kind entry-id pattern outside the subset", role: func(k map[string]any) {
			k["entryId"] = map[string]any{"pattern": `^\w+$`}
		}, code: "REG-KIND-PATTERN", pointer: "/entryId/pattern", document: DocumentKind},
		{name: "registry id not an absolute URI", registry: func(r map[string]any) { r["id"] = "platform" }, code: "REG-REGISTRY-FORM", pointer: "/id"},
		{name: "registry version not semver", registry: func(r map[string]any) { r["version"] = "4" }, code: "REG-REGISTRY-FORM", pointer: "/version"},
		{name: "no environments", registry: func(r map[string]any) { r["environments"] = []any{} }, code: "REG-REGISTRY-FORM", pointer: "/environments"},
		{name: "kinds not an array", registry: func(r map[string]any) { r["kinds"] = map[string]any{} }, code: "REG-REGISTRY-FORM", pointer: "/kinds"},
		{name: "pin not an object", registry: func(r map[string]any) { r["kinds"].([]any)[0] = "database" }, code: "REG-REGISTRY-FORM", pointer: "/kinds/0"},
		{name: "pin with an unknown member", registry: func(r map[string]any) { r["kinds"].([]any)[0].(map[string]any)["extra"] = true }, code: "REG-REGISTRY-UNKNOWN-MEMBER", pointer: "/kinds/0/extra"},
		{name: "pin digest malformed", registry: func(r map[string]any) { r["kinds"].([]any)[0].(map[string]any)["digest"] = "sha256:00" }, code: "REG-REGISTRY-FORM", pointer: "/kinds/0"},
		{name: "pins out of order", registry: func(r map[string]any) {
			pins := r["kinds"].([]any)
			pins[0], pins[1] = pins[1], pins[0]
		}, code: "REG-REGISTRY-ORDER", pointer: "/kinds/1"},
		{name: "entries not an array", registry: func(r map[string]any) { r["entries"] = map[string]any{} }, code: "REG-REGISTRY-FORM", pointer: "/entries"},
		{name: "overlays not an array", registry: func(r map[string]any) { r["overlays"] = "prod" }, code: "REG-REGISTRY-FORM", pointer: "/overlays"},
		{name: "secret scopes not an object", registry: func(r map[string]any) { r["secretScopes"] = []any{} }, code: "REF-SECRET-SCOPE-FORM", pointer: "/secretScopes"},
		{name: "secret scopes of an undeclared environment", registry: func(r map[string]any) {
			r["secretScopes"].(map[string]any)["qa"] = map[string]any{"gcp-sm": []any{"example-qa"}}
		}, code: "REF-SECRET-SCOPE-FORM", pointer: "/secretScopes/qa"},
		{name: "secret scopes of an unknown store", registry: func(r map[string]any) {
			r["secretScopes"].(map[string]any)["dev"].(map[string]any)["vault"] = []any{"example-dev"}
		}, code: "REF-SECRET-SCOPE-FORM", pointer: "/secretScopes/dev/vault"},
		{name: "entry not an object", registry: func(r map[string]any) { r["entries"].([]any)[0] = "billing" }, code: "REG-REGISTRY-FORM", pointer: "/entries/0"},
		{name: "entry with an unknown member", registry: func(r map[string]any) { r["entries"].([]any)[0].(map[string]any)["extra"] = true }, code: "REG-REGISTRY-UNKNOWN-MEMBER", pointer: "/entries/0/extra"},
		{name: "entry without an owner", registry: func(r map[string]any) { delete(r["entries"].([]any)[0].(map[string]any), "owner") }, code: "REG-REGISTRY-MISSING-MEMBER", pointer: "/entries/0/owner"},
		{name: "entry kind not a kind name", registry: func(r map[string]any) { r["entries"].([]any)[0].(map[string]any)["kind"] = "Database" }, code: "REG-REGISTRY-FORM", pointer: "/entries/0/kind"},
		{name: "entry id not a string", registry: func(r map[string]any) { r["entries"].([]any)[0].(map[string]any)["id"] = true }, code: "REG-REGISTRY-FORM", pointer: "/entries/0/id"},
		{name: "entry owner malformed", registry: func(r map[string]any) { r["entries"].([]any)[0].(map[string]any)["owner"] = "Team Billing" }, code: "REG-REGISTRY-FORM", pointer: "/entries/0/owner"},
		{name: "entry environments empty", registry: func(r map[string]any) { r["entries"].([]any)[0].(map[string]any)["environments"] = []any{} }, code: "REG-REGISTRY-FORM", pointer: "/entries/0/environments"},
		{name: "overlay with an unknown member", registry: func(r map[string]any) { r["overlays"].([]any)[0].(map[string]any)["extra"] = true }, code: "REG-REGISTRY-FORM", pointer: "/overlays/0"},
		{name: "patch without a set", registry: func(r map[string]any) {
			delete(r["overlays"].([]any)[1].(map[string]any)["patches"].([]any)[0].(map[string]any), "set")
		}, code: "REG-REGISTRY-FORM", pointer: "/overlays/1/patches/0"},
		{name: "kind deprecated not a boolean", role: func(k map[string]any) { k["deprecated"] = "yes" }, code: "REG-KIND-FORM", pointer: "/deprecated", document: DocumentKind},
		{name: "kind description not a string", role: func(k map[string]any) { k["description"] = true }, code: "REG-KIND-FORM", pointer: "/description", document: DocumentKind},
		{name: "kind entry id not an object", role: func(k map[string]any) { k["entryId"] = "^[a-z]+$" }, code: "REG-KIND-FORM", pointer: "/entryId", document: DocumentKind},
		{name: "kind bounds not an object", role: func(k map[string]any) { k["bounds"] = []any{} }, code: "REG-KIND-FORM", pointer: "/bounds", document: DocumentKind},
		{name: "kind maxEntries below one", role: func(k map[string]any) {
			k["bounds"].(map[string]any)["maxEntries"] = json.Number("0")
		}, code: "REG-KIND-FORM", pointer: "/bounds/maxEntries", document: DocumentKind},
		{name: "kind bounds with an unknown member", role: func(k map[string]any) {
			k["bounds"].(map[string]any)["maxDepth"] = json.Number("3")
		}, code: "REG-KIND-UNKNOWN-MEMBER", pointer: "/bounds/maxDepth", document: DocumentKind},
		{name: "kind secrets not an array", role: func(k map[string]any) { k["secrets"] = "/password" }, code: "REG-KIND-FORM", pointer: "/secrets", document: DocumentKind},
		{name: "kind secret not a pointer", role: func(k map[string]any) { k["secrets"] = []any{"password"} }, code: "REG-KIND-POINTER-FORM", pointer: "/secrets/0", document: DocumentKind},
		{name: "kind references not an array", role: func(k map[string]any) { k["references"] = map[string]any{} }, code: "REG-KIND-FORM", pointer: "/references", document: DocumentKind},
		{name: "kind reference not an object", role: func(k map[string]any) { k["references"] = []any{"/database"} }, code: "REG-KIND-FORM", pointer: "/references/0", document: DocumentKind},
		{name: "kind reference with an unknown member", role: func(k map[string]any) {
			k["references"].([]any)[0].(map[string]any)["extra"] = true
		}, code: "REG-KIND-UNKNOWN-MEMBER", pointer: "/references/0/extra", document: DocumentKind},
		{name: "kind reference naming no kinds", role: func(k map[string]any) {
			k["references"].([]any)[0].(map[string]any)["kinds"] = []any{}
		}, code: "REG-KIND-FORM", pointer: "/references/0", document: DocumentKind},
		{name: "kind reference naming a kind twice", role: func(k map[string]any) {
			k["references"].([]any)[0].(map[string]any)["kinds"] = []any{"database", "database"}
		}, code: "REG-KIND-FORM", pointer: "/references/0/kinds", document: DocumentKind},
		{name: "kind reference pointer malformed", role: func(k map[string]any) {
			k["references"].([]any)[0].(map[string]any)["pointer"] = "database"
		}, code: "REG-KIND-POINTER-FORM", pointer: "/references/0/pointer", document: DocumentKind},
		{name: "kind reference pointer the schema cannot reach", role: func(k map[string]any) {
			k["references"].([]any)[0].(map[string]any)["pointer"] = "/nowhere"
		}, code: "REG-KIND-POINTER-UNREACHABLE", pointer: "/references/0/pointer", document: DocumentKind},
		{name: "kind reference pointer given twice", role: func(k map[string]any) {
			k["references"] = append(k["references"].([]any), map[string]any{"pointer": "/database", "kinds": []any{"database"}})
		}, code: "REG-KIND-FORM", pointer: "/references/1/pointer", document: DocumentKind},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			role := roleKind
			if c.role != nil {
				value := decode(t, roleKind)
				c.role(value)
				role = encode(t, value)
			}
			registryText := strings.NewReplacer("DATABASE", digestOf(t, databaseKind), "ROLE", digestOf(t, role)).Replace(registryTemplate)
			registry := decode(t, registryText)
			if c.registry != nil {
				c.registry(registry)
			}
			result := run(encode(t, registry), []string{databaseKind, role})
			document := c.document
			if document == "" {
				document = DocumentRegistry
			}
			found := false
			for _, d := range result.Report.Diagnostics {
				if d.Code == c.code && d.Location.Pointer == c.pointer && d.Location.Document == document && d.Location.Environment == c.env {
					found = true
				}
			}
			if !found {
				t.Fatalf("want %s at %s %q (env %q); got %+v", c.code, document, c.pointer, c.env, result.Report.Diagnostics)
			}
			if Codes[c.code].severity == SeverityError && result.Report.Valid {
				t.Fatal("a registry with an error was reported valid")
			}
			if Codes[c.code].severity == SeverityError {
				if _, err := result.Resolve("dev"); err == nil {
					t.Fatal("an invalid registry resolved")
				}
			}
		})
	}
}

// The report carries no value: no secret reference, entry reference or
// member value from the documents.
func TestReportCarriesNoValue(t *testing.T) {
	registry, kinds := fixture(t)
	value := decode(t, registry)
	entryValue(value, 1)["password"] = "secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/latest"
	entryValue(value, 1)["database"] = "entry:database/ledger-secret-name"
	entryValue(value, 0)["engine"] = "oracle-sentinel"
	result := run(encode(t, value), kinds)
	report, err := json.Marshal(result.Report)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"versions/latest", "billing-db", "ledger-secret-name", "oracle-sentinel", "example-dev"} {
		if strings.Contains(string(report), leak) {
			t.Errorf("the report carries %q", leak)
		}
	}
	if result.Report.Valid || len(result.Report.Diagnostics) < 3 {
		t.Fatalf("want three errors, got %+v", result.Report.Diagnostics)
	}
}

func TestAdmission(t *testing.T) {
	registry, kinds := fixture(t)
	cases := []struct {
		name, document, code string
	}{
		{"not JSON", `{`, "REG-DOC-JSON"},
		{"trailing data", registry + "x", "REG-DOC-JSON"},
		{"duplicate member", `{"registryFormatVersion":"1.0.0","id":"a","id":"b"}`, "REG-DOC-DUPLICATE"},
		{"lone surrogate", `{"registryFormatVersion":"1.0.0","id":"\ud800"}`, "REG-DOC-STRING"},
		{"too deep", `{"registryFormatVersion":"1.0.0","x":` + strings.Repeat("[", 32) + strings.Repeat("]", 32) + `}`, "REG-DOC-DEPTH"},
		{"integer too large", `{"registryFormatVersion":"1.0.0","x":9007199254740992}`, "REG-DOC-NUMBER"},
		{"decimal that does not round-trip", `{"registryFormatVersion":"1.0.0","x":0.10000000000000001}`, "REG-DOC-NUMBER"},
		{"negative zero", `{"registryFormatVersion":"1.0.0","x":-0.0}`, "REG-DOC-NUMBER"},
		{"wrong selector", strings.Replace(registry, `"registryFormatVersion": "1.0.0"`, `"registryFormatVersion": "1.0"`, 1), "REG-DOC-SELECTOR"},
		{"not an object", `[]`, "REG-DOC-SELECTOR"},
		{"too large", `{"registryFormatVersion":"1.0.0","x":"` + strings.Repeat("a", MaxRegistryBytes) + `"}`, "REG-DOC-SIZE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := run(c.document, kinds)
			if result.Report.Valid || len(result.Report.Diagnostics) == 0 || result.Report.Diagnostics[0].Code != c.code {
				t.Fatalf("want %s first, got %+v", c.code, result.Report.Diagnostics)
			}
		})
	}
	// A decimal that round-trips by value is admitted, whatever its text.
	for _, literal := range []string{"1.50", "0.1", "1e-7", "-9007199254740991", "100"} {
		if !admittedNumber(json.Number(literal)) {
			t.Errorf("%s should be admitted", literal)
		}
	}
}

func TestUnpinnedSuppliedKindWarns(t *testing.T) {
	registry, kinds := fixture(t)
	extra := strings.Replace(databaseKind, `"kind": "database"`, `"kind": "cache"`, 1)
	result := run(registry, append(kinds, extra))
	if !result.Report.Valid {
		t.Fatalf("an unpinned extra kind is a warning, got %+v", result.Report.Diagnostics)
	}
	if len(result.Report.Diagnostics) != 1 || result.Report.Diagnostics[0].Code != "REG-PIN-UNPINNED" || result.Report.Diagnostics[0].Location.Kind != "cache" {
		t.Fatalf("want one REG-PIN-UNPINNED for cache, got %+v", result.Report.Diagnostics)
	}
}
