package registry

import (
	"strings"
	"testing"
)

func roleContract(t *testing.T) *FactsContractDoc {
	t.Helper()
	registry, kinds := fixture(t)
	generated, _, err := run(registry, kinds).FactsContract("db-role")
	if err != nil {
		t.Fatal(err)
	}
	contract, diagnostics := ReadFactsContract(Document{Data: generated})
	if contract == nil {
		t.Fatalf("%+v", diagnostics)
	}
	return contract
}

// fact is one fact condition of a pack's evaluation form.
func fact(path, operator, value string) string {
	return `{"op":"fact","path":"` + path + `","operator":"` + operator + `","value":` + value + `}`
}

func TestCheckPackAdvisories(t *testing.T) {
	contract := roleContract(t)
	cases := []struct {
		name, condition, code string
	}{
		{"a listed pointer, a listed value", fact("/environment", "equals", `"prod"`), ""},
		{"an environment the registry does not declare", fact("/environment", "equals", `"qa"`), "FACTS-VALUE-UNREACHABLE"},
		{"a misspelt member", fact("/value/conectionLimit", "greater-than", `"100"`), "FACTS-POINTER-UNKNOWN"},
		{"under a member that has nothing under it", fact("/value/database/name", "equals", `"x"`), "FACTS-POINTER-UNKNOWN"},
		{"an ordered comparison on a decimal", fact("/value/connectionLimit", "greater-than", `"100"`), ""},
		{"an ordered comparison on a string", fact("/value/database", "less-than", `"1"`), "FACTS-TYPE-ORDERED"},
		{"a decimal compared with a number", fact("/value/connectionLimit", "equals", `120`), "FACTS-VALUE-UNREACHABLE"},
		{"a decimal compared with its string", fact("/value/connectionLimit", "not-equals", `"120"`), ""},
		{"a decimal compared with a boolean", fact("/value/connectionLimit", "equals", `true`), "FACTS-VALUE-UNREACHABLE"},
		{"an array of listed elements", fact("/value/privileges", "equals", `["read","write"]`), ""},
		{"an array with an unlisted element", fact("/value/privileges", "equals", `["read","root"]`), "FACTS-VALUE-UNREACHABLE"},
		{"an element by index", fact("/value/privileges/0", "in", `["read","admin"]`), ""},
		{"an in with one unlisted value", fact("/references/~1database/value/engine", "in", `["postgres","oracle"]`), "FACTS-VALUE-UNREACHABLE"},
		{"an in with listed values", fact("/references/~1database/value/engine", "in", `["postgres","mysql"]`), ""},
		{"a reference by pointer carries a string", fact("/references/~1database/value", "equals", `"postgres"`), ""},
		{"a secret's store", fact("/value/password/store", "equals", `"vault"`), "FACTS-VALUE-UNREACHABLE"},
		{"a secret's scope", fact("/value/password/scope", "equals", `"example-prod"`), ""},
		{"a secret as an object", fact("/value/password", "equals", `{"store":"gcp-sm","scope":"example-prod"}`), ""},
		{"a secret object without its scope", fact("/value/password", "equals", `{"store":"gcp-sm"}`), "FACTS-VALUE-UNREACHABLE"},
		{"a secret object with another member", fact("/value/password", "equals", `{"store":"gcp-sm","scope":"x","name":"y"}`), "FACTS-VALUE-UNREACHABLE"},
		{"the reference string itself", fact("/value/password", "equals", `"secret:gcp-sm:projects/example-prod/secrets/x/versions/1"`), "FACTS-VALUE-UNREACHABLE"},
		{"null where no null is listed", fact("/value/legacyFlag", "equals", `null`), "FACTS-VALUE-UNREACHABLE"},
		{"a boolean", fact("/value/legacyFlag", "equals", `false`), ""},
		{"a pointer with ~2, which a pack cannot read", fact("/value/~2", "equals", `"x"`), "FACTS-POINTER-UNKNOWN"},
	}
	for _, test := range cases {
		form := decode(t, `{"applicability":`+test.condition+`}`)
		diagnostics := contract.CheckPack(form)
		switch {
		case test.code == "" && len(diagnostics) != 0:
			t.Errorf("%s: want none, got %+v", test.name, diagnostics)
		case test.code != "" && (len(diagnostics) != 1 || diagnostics[0].Code != test.code || diagnostics[0].Location != Location{Document: DocumentPack, Pointer: "/applicability"}):
			t.Errorf("%s: want %s at /applicability, got %+v", test.name, test.code, diagnostics)
		}
		if strings.Contains(encode(t, diagnostics), "secret:") || strings.Contains(encode(t, diagnostics), "conection") {
			t.Errorf("%s: a diagnostic carries pack text", test.name)
		}
	}
}

func TestCheckPackWalksEveryConditionPosition(t *testing.T) {
	contract := roleContract(t)
	bad := fact("/value/nope", "equals", `"x"`)
	good := fact("/environment", "equals", `"prod"`)
	form := decode(t, `{"applicability":{"op":"all","conditions":[`+good+`,{"op":"not","condition":`+bad+`}]},
"exceptions":[{"when":{"op":"any","conditions":[`+good+`,`+good+`,`+bad+`]}}],
"rules":[{"when":`+good+`},{"when":`+fact("/value/database", "greater-than-or-equal", `"1"`)+`},{"when":{"op":"literal","value":true}}]}`)
	diagnostics := contract.CheckPack(form)
	var got []string
	for _, d := range diagnostics {
		got = append(got, d.Code+" "+d.Location.Pointer)
	}
	want := "FACTS-POINTER-UNKNOWN /applicability/conditions/1/condition|FACTS-POINTER-UNKNOWN /exceptions/0/when/conditions/2|FACTS-TYPE-ORDERED /rules/1/when"
	if strings.Join(got, "|") != want {
		t.Fatalf("got %s", strings.Join(got, "|"))
	}
	for _, d := range diagnostics {
		if d.Severity != SeverityWarning || d.Location.Document != DocumentPack {
			t.Errorf("%+v", d)
		}
	}
}

func TestCheckPackAgainstAnAuthoredContract(t *testing.T) {
	contract, diagnostics := ReadFactsContract(Document{Data: []byte(`{"factsContractVersion":"1.0.0","id":"https://example.invalid/facts/request","version":"1.0.0",
"source":{"authored":true},"facts":[
{"pointer":"/request","present":"always","type":"object"},
{"pointer":"/request/amount","present":"always","type":"decimal","values":["10","20"]},
{"pointer":"/request/labels","present":"optional","type":"array"},
{"pointer":"/request/labels/*","present":"optional","type":"string"},
{"pointer":"/request/meta","present":"optional","type":"object"}]}`)})
	if contract == nil {
		t.Fatalf("%+v", diagnostics)
	}
	cases := map[string]struct {
		condition string
		code      string
	}{
		"a listed decimal":                  {fact("/request/amount", "equals", `"10"`), ""},
		"an unlisted decimal":               {fact("/request/amount", "equals", `"10.0"`), "FACTS-VALUE-UNREACHABLE"},
		"a string where only decimals":      {fact("/request/amount", "equals", `"ten"`), "FACTS-VALUE-UNREACHABLE"},
		"array elements by wildcard":        {fact("/request/labels", "equals", `["a"]`), ""},
		"array elements of another type":    {fact("/request/labels", "equals", `[true]`), "FACTS-VALUE-UNREACHABLE"},
		"an object with nothing listed":     {fact("/request/meta", "equals", `{"any":"thing"}`), ""},
		"an object missing an always":       {fact("/request", "equals", `{}`), "FACTS-VALUE-UNREACHABLE"},
		"the root, which is never listed":   {fact("", "greater-than", `"1"`), "FACTS-TYPE-ORDERED"},
		"under an object with nothing more": {fact("/request/meta/a", "equals", `"x"`), "FACTS-POINTER-UNKNOWN"},
	}
	for name, test := range cases {
		diagnostics := contract.CheckPack(decode(t, `{"rules":[{"when":`+test.condition+`}]}`))
		if test.code == "" && len(diagnostics) != 0 || test.code != "" && (len(diagnostics) != 1 || diagnostics[0].Code != test.code) {
			t.Errorf("%s: want %q, got %+v", name, test.code, diagnostics)
		}
	}
}
