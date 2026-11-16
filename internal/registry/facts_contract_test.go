package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// contractFacts generates a kind's contract and returns its facts, one
// canonical item per line.
func contractFacts(t *testing.T, result *Result, kind string) (map[string]any, []string) {
	t.Helper()
	contract, diagnostics, err := result.FactsContract(kind)
	if err != nil || len(diagnostics) > 0 {
		t.Fatalf("FactsContract: %v %+v", err, diagnostics)
	}
	root := decode(t, string(contract))
	var lines []string
	for _, item := range root["facts"].([]any) {
		lines = append(lines, encode(t, item))
	}
	return root, lines
}

// The rows the RFC's example shows are the same here; the rest are the
// members the example leaves out.
func TestFactsContractOfTheRoleKind(t *testing.T) {
	registry, kinds := fixture(t)
	result := run(registry, kinds)
	root, lines := contractFacts(t, result, "db-role")
	want := []string{
		`{"pointer":"/entry","present":"always","type":"object"}`,
		`{"pointer":"/entry/id","present":"always","type":"string"}`,
		`{"pointer":"/entry/kind","present":"always","type":"string","values":["db-role"]}`,
		`{"pointer":"/entry/owner","present":"always","type":"string"}`,
		`{"pointer":"/environment","present":"always","type":"string","values":["dev","staging","prod"]}`,
		`{"pointer":"/references","present":"always","type":"object"}`,
		`{"pointer":"/references/~1database","present":"always","type":"object"}`,
		`{"pointer":"/references/~1database/id","present":"always","type":"string"}`,
		`{"pointer":"/references/~1database/kind","present":"always","type":"string","values":["database"]}`,
		`{"pointer":"/references/~1database/value","present":"always","type":"object"}`,
		`{"pointer":"/references/~1database/value","present":"always","type":"string"}`,
		`{"pointer":"/references/~1database/value/engine","present":"always","type":"string","values":["postgres","mysql"]}`,
		`{"pointer":"/references/~1database/value/region","present":"optional","type":"string"}`,
		`{"pointer":"/registry","present":"always","type":"object"}`,
		`{"pointer":"/registry/digest","present":"always","type":"string"}`,
		`{"pointer":"/registry/id","present":"always","type":"string"}`,
		`{"pointer":"/registry/version","present":"always","type":"string"}`,
		`{"pointer":"/value","present":"always","type":"object"}`,
		`{"pointer":"/value/connectionLimit","present":"optional","type":"decimal"}`,
		`{"pointer":"/value/database","present":"always","type":"string"}`,
		`{"pointer":"/value/legacyFlag","present":"optional","type":"boolean"}`,
		`{"pointer":"/value/password","present":"optional","secretReference":true,"type":"object"}`,
		`{"pointer":"/value/password/scope","present":"always","type":"string"}`,
		`{"pointer":"/value/password/store","present":"always","type":"string","values":["gcp-sm"]}`,
		`{"items":"string","pointer":"/value/privileges","present":"always","type":"array","values":["read","write","admin"]}`,
		`{"pointer":"/value/privileges/*","present":"optional","type":"string","values":["read","write","admin"]}`,
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("contract facts:\n%s", strings.Join(lines, "\n"))
	}
	source := encode(t, root["source"])
	if root["factsContractVersion"] != "1.0.0" || root["id"] != "https://example.invalid/kinds/db-role" || root["version"] != "1.2.0" ||
		source != `{"kind":{"digest":"`+digestOf(t, roleKind)+`","id":"https://example.invalid/kinds/db-role","version":"1.2.0"}}` {
		t.Errorf("contract identity: %v %s", root, source)
	}
	// Deterministic, whatever order the kinds come in.
	first, _, _ := result.FactsContract("db-role")
	again, _, _ := run(registry, []string{kinds[1], kinds[0]}).FactsContract("db-role")
	if !bytes.Equal(first, again) {
		t.Error("the contract depends on the order of the kinds")
	}
	if contract, diagnostics := ReadFactsContract(Document{Data: first}); contract == nil || len(diagnostics) > 0 {
		t.Errorf("a generated contract reads back: %+v", diagnostics)
	}
}

const widgetKind = `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/widget","version":"2.0.0","kind":"widget","owner":"team-a",
"schema":{"type":"object","required":["mode","size"],"additionalProperties":false,
 "$defs":{"port":{"type":"integer"}},
 "properties":{
  "mode":{"const":"fast"},
  "size":{"anyOf":[{"type":"integer"},{"type":"string","enum":["small","large"]}]},
  "port":{"$ref":"#/$defs/port"},
  "level":{"type":["integer","null"],"enum":[1,2.50,null,"x"]},
  "tags":{"type":"array","items":{"type":"string"}},
  "limits":{"allOf":[{"type":"object","properties":{"max":{"type":"number"}},"required":["max"]},{"properties":{"min":{"type":"number"}}}]},
  "accounts":{"type":"array","items":{"type":"object","required":["password"],"additionalProperties":false,"properties":{"password":{"type":"string"}}}},
  "extra":{}
 }},
"secrets":["/accounts/*/password"]}`

func TestFactsContractSchemaKeywords(t *testing.T) {
	registry, kinds := customRegistry(t, `{"kind":"widget","id":"w","owner":"team-a","value":{"mode":"fast","size":3}}`, widgetKind)
	result := run(registry, kinds)
	if !result.Report.Valid {
		t.Fatalf("%+v", result.Report.Diagnostics)
	}
	_, lines := contractFacts(t, result, "widget")
	var value []string
	for _, line := range lines {
		if strings.Contains(line, `"pointer":"/value`) {
			value = append(value, line)
		}
	}
	open := func(pointer string) []string {
		var out []string
		for _, name := range []string{"array", "boolean", "decimal", "null", "object", "string"} {
			out = append(out, fmt.Sprintf(`{"pointer":%q,"present":"optional","type":%q}`, pointer, name))
		}
		return out
	}
	want := []string{
		`{"pointer":"/value","present":"always","type":"object"}`,
		`{"items":"object","pointer":"/value/accounts","present":"optional","type":"array"}`,
		`{"pointer":"/value/accounts/*","present":"optional","type":"object"}`,
		`{"pointer":"/value/accounts/*/password","present":"always","secretReference":true,"type":"object"}`,
		`{"pointer":"/value/accounts/*/password/scope","present":"always","type":"string"}`,
		`{"pointer":"/value/accounts/*/password/store","present":"always","type":"string","values":["gcp-sm"]}`,
	}
	want = append(want, open("/value/extra")...)
	want = append(want,
		// enum with type: the values the types allow, numbers as decimals.
		`{"pointer":"/value/level","present":"optional","type":"decimal","values":["1"]}`,
		`{"pointer":"/value/level","present":"optional","type":"null"}`,
		// allOf narrows; a member of either branch is listed.
		`{"pointer":"/value/limits","present":"optional","type":"object"}`,
	)
	want = append(want, open("/value/limits/*")...)
	want = append(want,
		`{"pointer":"/value/limits/max","present":"always","type":"decimal"}`,
		`{"pointer":"/value/limits/min","present":"optional","type":"decimal"}`,
		`{"pointer":"/value/mode","present":"always","type":"string","values":["fast"]}`,
		`{"pointer":"/value/port","present":"optional","type":"decimal"}`,
		// anyOf: listed once per type.
		`{"pointer":"/value/size","present":"always","type":"decimal"}`,
		`{"pointer":"/value/size","present":"always","type":"string","values":["small","large"]}`,
		`{"items":"string","pointer":"/value/tags","present":"optional","type":"array"}`,
		`{"pointer":"/value/tags/*","present":"optional","type":"string"}`,
	)
	if strings.Join(value, "\n") != strings.Join(want, "\n") {
		t.Fatalf("value facts:\n%s", strings.Join(value, "\n"))
	}
}

func TestFactsContractOverTheWorkLimit(t *testing.T) {
	var groups []string
	for g := 0; g < 6; g++ {
		var branches []string
		for b := 0; b < 16; b++ {
			branches = append(branches, fmt.Sprintf(`{"properties":{"g%db%d":{"type":"string"}}}`, g, b))
		}
		groups = append(groups, `{"anyOf":[`+strings.Join(branches, ",")+`]}`)
	}
	kind := `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/wide","version":"1.0.0","kind":"wide","owner":"team-a",
"schema":{"type":"object","allOf":[` + strings.Join(groups, ",") + `]}}`
	registry, kinds := customRegistry(t, ``, kind)
	result := run(registry, kinds)
	if !result.Report.Valid {
		t.Fatalf("%+v", result.Report.Diagnostics)
	}
	contract, diagnostics, err := result.FactsContract("wide")
	if err != nil || contract != nil || len(diagnostics) != 1 || diagnostics[0].Code != "FACTS-CONTRACT-LIMIT" || diagnostics[0].Location.Kind != "wide" {
		t.Fatalf("%v %d %+v", err, len(contract), diagnostics)
	}
}

func TestFactsContractRefusals(t *testing.T) {
	registry, kinds := fixture(t)
	generated, _, err := run(registry, kinds).FactsContract("db-role")
	if err != nil {
		t.Fatal(err)
	}
	edit := func(change func(map[string]any)) []byte {
		root := decode(t, string(generated))
		change(root)
		data, err := json.Marshal(root)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	item := func(root map[string]any, i int) map[string]any { return root["facts"].([]any)[i].(map[string]any) }
	cases := map[string]struct {
		data    []byte
		code    string
		pointer string
	}{
		"an unknown member, named by no location": {edit(func(r map[string]any) { r["secret:gcp-sm:projects/example-prod/secrets/x/versions/1"] = true }), "FACTS-CONTRACT-FORM", ""},
		"an unknown item member":                  {edit(func(r map[string]any) { item(r, 0)["note"] = "x" }), "FACTS-CONTRACT-FORM", "/facts/0"},
		"a type outside the list":                 {edit(func(r map[string]any) { item(r, 0)["type"] = "integer" }), "FACTS-CONTRACT-FORM", "/facts/0/type"},
		"a presence outside the list":             {edit(func(r map[string]any) { item(r, 0)["present"] = "sometimes" }), "FACTS-CONTRACT-FORM", "/facts/0/present"},
		"a pointer that is not one":               {edit(func(r map[string]any) { item(r, 0)["pointer"] = "entry" }), "FACTS-CONTRACT-FORM", "/facts/0/pointer"},
		"items on a string":                       {edit(func(r map[string]any) { item(r, 1)["items"] = "string" }), "FACTS-CONTRACT-FORM", "/facts/1/items"},
		"secretReference on a string":             {edit(func(r map[string]any) { item(r, 1)["secretReference"] = true }), "FACTS-CONTRACT-FORM", "/facts/1/secretReference"},
		"a decimal value outside the grammar":     {edit(func(r map[string]any) { item(r, 18)["values"] = []any{"1e2"} }), "FACTS-CONTRACT-FORM", "/facts/18/values"},
		"a value twice":                           {edit(func(r map[string]any) { item(r, 2)["values"] = []any{"db-role", "db-role"} }), "FACTS-CONTRACT-FORM", "/facts/2/values"},
		"an empty values":                         {edit(func(r map[string]any) { item(r, 2)["values"] = []any{} }), "FACTS-CONTRACT-FORM", "/facts/2/values"},
		"a source that is neither form":           {edit(func(r map[string]any) { r["source"] = map[string]any{"authored": false} }), "FACTS-CONTRACT-FORM", "/source"},
		"an id that is not an absolute URI":       {edit(func(r map[string]any) { r["id"] = "db-role" }), "FACTS-CONTRACT-FORM", "/id"},
		"items out of order": {edit(func(r map[string]any) {
			facts := r["facts"].([]any)
			facts[0], facts[1] = facts[1], facts[0]
		}), "FACTS-CONTRACT-ORDER", "/facts/1"},
		"a pointer and type twice": {edit(func(r map[string]any) {
			r["facts"] = append(r["facts"].([]any)[:1], r["facts"].([]any)...)
		}), "FACTS-CONTRACT-ORDER", "/facts/1"},
		"another format selector": {edit(func(r map[string]any) { r["factsContractVersion"] = "2.0.0" }), "REG-DOC-SELECTOR", "/factsContractVersion"},
	}
	for name, test := range cases {
		contract, diagnostics := ReadFactsContract(Document{Data: test.data})
		if contract != nil || len(diagnostics) == 0 || diagnostics[0].Code != test.code || diagnostics[0].Location.Pointer != test.pointer || diagnostics[0].Location.Document != DocumentContract {
			t.Errorf("%s: %+v", name, diagnostics)
		}
		if strings.Contains(encode(t, diagnostics), "secret:") {
			t.Errorf("%s: a diagnostic carries a value", name)
		}
	}
	authored := edit(func(r map[string]any) { r["source"] = map[string]any{"authored": true} })
	if contract, diagnostics := ReadFactsContract(Document{Data: authored}); contract == nil || len(diagnostics) > 0 {
		t.Errorf("an authored contract: %+v", diagnostics)
	}
	if _, diagnostics := ReadFactsContract(Document{Oversize: true}); len(diagnostics) != 1 || diagnostics[0].Code != "REG-DOC-SIZE" {
		t.Errorf("oversize: %+v", diagnostics)
	}
}
