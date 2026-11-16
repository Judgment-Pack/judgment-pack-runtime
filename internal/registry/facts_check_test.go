package registry

import (
	"strconv"
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

// openRoleKind leaves "meta" open, and openKind leaves every member but one
// open, since it has no additionalProperties.
const openRoleKind = `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/role","version":"1.0.0","kind":"role","owner":"team-a",
"schema":{"type":"object","required":["database"],"additionalProperties":false,"properties":{"database":{"type":"string"},"meta":{}}},
"references":[{"pointer":"/database","kinds":["database"]}]}`

const openKind = `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/loose","version":"1.0.0","kind":"loose","owner":"team-a",
"schema":{"type":"object","properties":{"name":{"type":"string"}}}}`

// A pointer below an open position is listed: the contract has one row of
// every type under the position, and the check reads every pointer below
// that row as listed.
func TestCheckPackListsPointersBelowAnOpenPosition(t *testing.T) {
	registry, kinds := customRegistry(t, `{"kind":"database","id":"d","owner":"team-a","value":{"engine":"postgres"}},
{"kind":"role","id":"r","owner":"team-a","value":{"database":"entry:database/d","meta":{"team":"x"}}}`, databaseKind, openKind, openRoleKind)
	result := run(registry, kinds)
	if !result.Report.Valid {
		t.Fatalf("%+v", result.Report.Diagnostics)
	}
	read := func(kind string) *FactsContractDoc {
		generated, diagnostics, err := result.FactsContract(kind)
		if err != nil || len(diagnostics) > 0 {
			t.Fatalf("%v %+v", err, diagnostics)
		}
		contract, diagnostics := ReadFactsContract(Document{Data: generated})
		if contract == nil {
			t.Fatalf("%+v", diagnostics)
		}
		return contract
	}
	role, loose := read("role"), read("loose")
	deep := strings.Repeat("/x", maxCheckDepth-3)
	cases := []struct {
		name     string
		contract *FactsContractDoc
		cond     string
		code     string
	}{
		{"a member under an open member", role, fact("/value/meta/team", "equals", `"x"`), ""},
		{"a member two levels under an open member", role, fact("/value/meta/team/lead", "not-equals", `"y"`), ""},
		{"an object under an open member", role, fact("/value/meta/team", "equals", `{"lead":{"name":"y"}}`), ""},
		{"an ordered comparison under an open member", role, fact("/value/meta/team/size", "greater-than", `"3"`), ""},
		{"the open member itself", role, fact("/value/meta", "equals", `{"team":"x"}`), ""},
		{"a member the closed object does not have", role, fact("/value/team", "equals", `"x"`), "FACTS-POINTER-UNKNOWN"},
		{"under a closed string member", role, fact("/value/database/name", "equals", `"x"`), "FACTS-POINTER-UNKNOWN"},
		{"a number under an open member", role, fact("/value/meta/team", "equals", `3`), "FACTS-VALUE-UNREACHABLE"},
		{"a member of an object without additionalProperties", loose, fact("/value/team", "equals", `"x"`), ""},
		{"deeper under it", loose, fact("/value/team/a/b/c", "in", `["x",true,null]`), ""},
		{"as deep as a facts document can nest", loose, fact("/value"+deep, "equals", `"x"`), ""},
	}
	for _, test := range cases {
		diagnostics := test.contract.CheckPack(decode(t, `{"applicability":`+test.cond+`}`))
		if test.code == "" && len(diagnostics) != 0 || test.code != "" && (len(diagnostics) != 1 || diagnostics[0].Code != test.code) {
			t.Errorf("%s: want %q, got %+v", test.name, test.code, diagnostics)
		}
	}
}

// A member named * is not the wildcard: it does not list the object's other
// members.
func TestCheckPackReadsAMemberNamedStar(t *testing.T) {
	registry, kinds := customRegistry(t, ``, widgetKind)
	generated, _, err := run(registry, kinds).FactsContract("widget")
	if err != nil {
		t.Fatal(err)
	}
	contract, diagnostics := ReadFactsContract(Document{Data: generated})
	if contract == nil {
		t.Fatalf("%+v", diagnostics)
	}
	for condition, code := range map[string]string{
		fact("/value/*", "equals", `"x"`):     "",
		fact("/value/*", "equals", `true`):    "FACTS-VALUE-UNREACHABLE",
		fact("/value/other", "equals", `"x"`): "FACTS-POINTER-UNKNOWN",
	} {
		diagnostics := contract.CheckPack(decode(t, `{"applicability":`+condition+`}`))
		if code == "" && len(diagnostics) != 0 || code != "" && (len(diagnostics) != 1 || diagnostics[0].Code != code) {
			t.Errorf("%s: want %q, got %+v", condition, code, diagnostics)
		}
	}
}

// starContract is an authored contract listing every pointer of k tokens,
// each a or *: a fact pointer of k a's matches 2^k of them.
func starContract(t *testing.T, k int) *FactsContractDoc {
	t.Helper()
	var items []string
	for mask := 0; mask < 1<<k; mask++ {
		var b strings.Builder
		for i := k - 1; i >= 0; i-- {
			if mask&(1<<i) != 0 {
				b.WriteString("/a")
			} else {
				b.WriteString("/*")
			}
		}
		items = append(items, `{"pointer":"`+b.String()+`","present":"optional","type":"string"}`)
	}
	contract, diagnostics := ReadFactsContract(Document{Data: []byte(`{"factsContractVersion":"1.0.0","id":"https://example.invalid/c","version":"1.0.0","source":{"authored":true},"facts":[` + strings.Join(items, ",") + `]}`)})
	if contract == nil {
		t.Fatalf("%+v", diagnostics[:1])
	}
	return contract
}

func conditions(paths ...string) map[string]any {
	var list []any
	for _, path := range paths {
		list = append(list, map[string]any{"op": "fact", "path": path, "operator": "equals", "value": "x"})
	}
	return map[string]any{"rules": []any{map[string]any{"when": map[string]any{"op": "all", "conditions": list}}}}
}

// Each pointer is looked up once per check, and the check has one work
// bound: a pack that keeps reading the contract's widest views stops at it.
func TestCheckPackWorkIsBoundedOverOneCheck(t *testing.T) {
	contract := starContract(t, 16)
	path := strings.Repeat("/a", 16)
	c := contract.checker()
	first := 0
	for i := 0; i < 10; i++ {
		if code := c.checkCondition(map[string]any{"op": "fact", "path": path, "operator": "equals", "value": "x"}); code != "" || c.exceeded {
			t.Fatalf("%q %v", code, c.exceeded)
		}
		if i == 0 {
			first = c.work
		}
	}
	if first > 1<<17 || c.work-first > 100 {
		t.Fatalf("the first read cost %d, nine more %d", first, c.work-first)
	}
	// Every distinct last token makes a view of 2^15 positions.
	var paths []string
	for i := 0; i < 200; i++ {
		paths = append(paths, strings.Repeat("/a", 15)+"/t"+strconv.Itoa(i))
	}
	diagnostics := contract.CheckPack(conditions(paths...))
	if len(diagnostics) == 0 {
		t.Fatal("a check over its work bound reports it")
	}
	last := diagnostics[len(diagnostics)-1]
	if last.Code != "FACTS-CHECK-LIMIT" || last.Severity != SeverityError || last.Location.Document != DocumentPack || !strings.HasPrefix(last.Location.Pointer, "/rules/0/when/conditions/") {
		t.Fatalf("%d diagnostics, last %+v", len(diagnostics), last)
	}
	for _, d := range diagnostics[:len(diagnostics)-1] {
		if d.Code == "FACTS-CHECK-LIMIT" {
			t.Fatalf("the limit is reported once: %+v", d)
		}
	}
	// A fact pointer deeper than a facts document can nest.
	diagnostics = contract.CheckPack(conditions(strings.Repeat("/a", maxCheckDepth+1)))
	if len(diagnostics) != 1 || diagnostics[0].Code != "FACTS-CHECK-LIMIT" {
		t.Fatalf("%+v", diagnostics)
	}
}

// FACTS-VALUE-UNREACHABLE is reported for an in when one item is
// unreachable, and its message says so.
func TestFactsValueUnreachableMessageCoversAnInItem(t *testing.T) {
	contract := roleContract(t)
	diagnostics := contract.CheckPack(decode(t, `{"applicability":`+fact("/environment", "in", `["prod","qa"]`)+`}`))
	if len(diagnostics) != 1 || diagnostics[0].Code != "FACTS-VALUE-UNREACHABLE" {
		t.Fatalf("%+v", diagnostics)
	}
	if message := diagnostics[0].Message; strings.Contains(message, "never match") || !strings.Contains(message, "at least one item of an in") {
		t.Fatalf("%q", message)
	}
}
