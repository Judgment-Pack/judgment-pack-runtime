package registry

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Cases added by the review round of the pull request that introduced this
// package. Every fixture is synthetic.

func codes(result *Result) []string {
	var out []string
	for _, d := range result.Report.Diagnostics {
		out = append(out, d.Code)
	}
	return out
}

func hasCode(result *Result, code string) bool {
	for _, d := range result.Report.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

// mutated returns the fixture with the role kind and the registry changed.
func mutated(t *testing.T, role func(map[string]any), registry func(map[string]any)) *Result {
	t.Helper()
	roleText := roleKind
	if role != nil {
		value := decode(t, roleKind)
		role(value)
		roleText = encode(t, value)
	}
	registryText := strings.NewReplacer("DATABASE", digestOf(t, databaseKind), "ROLE", digestOf(t, roleText)).Replace(registryTemplate)
	value := decode(t, registryText)
	if registry != nil {
		registry(value)
	}
	return run(encode(t, value), []string{databaseKind, roleText})
}

const leak = "secret:gcp-sm:projects/example-prod/secrets/zz-db/versions/3"

// HIGH 1: a malformed entry id or patch id, or a set key, is never named.
func TestNamesThatCouldBeValuesAreNotReported(t *testing.T) {
	for name, change := range map[string]func(map[string]any){
		"entry id": func(r map[string]any) { r["entries"].([]any)[1].(map[string]any)["id"] = leak },
		"entry id within the grammar but outside the kind's pattern": func(r map[string]any) {
			r["entries"].([]any)[1].(map[string]any)["id"] = "secret:gcp-sm:zz-db"
		},
		"patch id": func(r map[string]any) {
			r["overlays"].([]any)[1].(map[string]any)["patches"].([]any)[0].(map[string]any)["id"] = leak
		},
		"set key that is not a pointer": func(r map[string]any) { prodSet(r)[leak] = json.Number("3") },
		"set key no pattern matches":    func(r map[string]any) { prodSet(r)["/"+strings.ReplaceAll(leak, "/", "~1")] = json.Number("3") },
	} {
		t.Run(name, func(t *testing.T) {
			result := mutated(t, nil, change)
			report, err := json.Marshal(result.Report)
			if err != nil {
				t.Fatal(err)
			}
			if result.Report.Valid {
				t.Fatalf("want an error, got %v", codes(result))
			}
			for _, fragment := range []string{"zz-db", "example-prod", "secret:"} {
				if strings.Contains(string(report), fragment) {
					t.Fatalf("the report carries %q: %s", fragment, report)
				}
			}
		})
	}
}

// LOW 8: the report names the registry by digest only.
func TestReportNamesTheRegistryByDigestOnly(t *testing.T) {
	registry, kinds := fixture(t)
	report, _ := json.Marshal(run(registry, kinds).Report)
	if strings.Contains(string(report), "example.invalid/registries") || strings.Contains(string(report), `"4.0.0"`) || !strings.Contains(string(report), `"registry":{"digest":"sha256:`) {
		t.Fatalf("report: %s", report)
	}
}

// HIGH 2: an entry whose patch failed in an environment is not checked there.
func TestAFailedPatchSkipsTheEntryInThatEnvironment(t *testing.T) {
	result := mutated(t, nil, func(r map[string]any) { prodSet(r)["/privileges"] = []any{"admin"} })
	if got := codes(result); len(got) != 1 || got[0] != "REG-OVERLAY-NOT-OVERLAYABLE" {
		t.Fatalf("want only REG-OVERLAY-NOT-OVERLAYABLE, got %v", got)
	}
	duplicate := mutated(t, nil, func(r map[string]any) {
		overlay := r["overlays"].([]any)[1].(map[string]any)
		patches := overlay["patches"].([]any)
		// The first patch would fail step 7 (900 is out of range): the
		// duplicate fails the pair, so step 7 must not run on it.
		prodSet(r)["/connectionLimit"] = json.Number("900")
		second := map[string]any{"kind": "db-role", "id": "billing-api", "set": map[string]any{"/connectionLimit": json.Number("7")}}
		overlay["patches"] = append(patches, second)
	})
	if got := codes(duplicate); len(got) != 1 || got[0] != "REG-OVERLAY-PATCH-DUPLICATE" {
		t.Fatalf("want only REG-OVERLAY-PATCH-DUPLICATE, got %v", got)
	}
}

// HIGH 3: an empty fragment pointer is the whole entry.
func TestAnEmptyFragmentPointerIsAccepted(t *testing.T) {
	result := mutated(t, nil, func(r map[string]any) { entryValue(r, 2)["database"] = "entry:database/billing#" })
	if !result.Report.Valid {
		t.Fatalf("want valid, got %v", codes(result))
	}
	if _, ok := parseEntryReference("entry:database/billing#"); !ok {
		t.Fatal("the empty pointer was refused")
	}
}

// MEDIUM 4: a resolved document over its bound is a finding.
func TestAResolvedDocumentOverItsBoundIsAFinding(t *testing.T) {
	registry, kinds := fixture(t)
	saved := maxResolvedBytes
	defer func() { maxResolvedBytes = saved }()
	prod, _ := run(registry, kinds).Resolve("prod")
	maxResolvedBytes = len(prod) - 1
	result := run(registry, kinds)
	found := false
	for _, d := range result.Report.Diagnostics {
		if d.Code == "REG-DOC-SIZE" && d.Location.Document == DocumentRegistry && d.Location.Environment == "prod" {
			found = true
		}
	}
	if !found || result.Report.Valid {
		t.Fatalf("want REG-DOC-SIZE for prod, got %+v", result.Report.Diagnostics)
	}
	if _, err := result.Resolve("dev"); err == nil {
		t.Fatal("an invalid registry resolved")
	}
}

// MEDIUM 5: an oversize kind is one finding among the others.
func TestAnOversizeKindDoesNotHideTheRest(t *testing.T) {
	registry, _ := fixture(t)
	result := ValidateDocuments(Document{Data: []byte(registry)}, []Document{{Data: []byte(databaseKind)}, {Oversize: true}})
	if !hasCode(result, "REG-DOC-SIZE") || !hasCode(result, "REG-PIN-MISSING") {
		t.Fatalf("want REG-DOC-SIZE and the pin it leaves missing, got %v", codes(result))
	}
	whole := ValidateDocuments(Document{Oversize: true}, []Document{{Data: []byte(databaseKind)}, {Data: []byte("{")}})
	if !hasCode(whole, "REG-DOC-SIZE") || !hasCode(whole, "REG-DOC-JSON") {
		t.Fatalf("an oversize registry must not hide a kind's finding, got %v", codes(whole))
	}
}

// MEDIUM 7: the overlay rules without a case before.
func TestOverlayPrefixTargetAndDuplicates(t *testing.T) {
	withOptions := func(k map[string]any) {
		properties := k["schema"].(map[string]any)["properties"].(map[string]any)
		properties["options"] = map[string]any{"type": "object", "properties": map[string]any{"pool": map[string]any{"type": "integer"}}}
		k["overlayable"] = append(k["overlayable"].([]any), "/options", "/options/pool")
	}
	prefix := mutated(t, withOptions, func(r map[string]any) {
		entryValue(r, 1)["options"] = map[string]any{"pool": json.Number("1")}
		prodSet(r)["/options"] = map[string]any{"pool": json.Number("2")}
		prodSet(r)["/options/pool"] = json.Number("3")
	})
	if !hasCode(prefix, "REG-OVERLAY-PREFIX") {
		t.Fatalf("want REG-OVERLAY-PREFIX, got %v", codes(prefix))
	}
	target := mutated(t, withOptions, func(r map[string]any) { prodSet(r)["/options/pool"] = json.Number("3") })
	if !hasCode(target, "REG-OVERLAY-TARGET") {
		t.Fatalf("want REG-OVERLAY-TARGET, got %v", codes(target))
	}
	duplicate := mutated(t, nil, func(r map[string]any) {
		entries := r["entries"].([]any)
		r["entries"] = append([]any{entries[0], entries[0]}, entries[1:]...)
	})
	if !hasCode(duplicate, "REG-ENTRY-DUPLICATE") {
		t.Fatalf("want REG-ENTRY-DUPLICATE, got %v", codes(duplicate))
	}
}

func schemaFindings(t *testing.T, schema string, value string) []string {
	t.Helper()
	collected := &collector{}
	node := compileSchema(decodeAny(t, schema), []string{"schema"}, Location{Document: DocumentKind}, collected, &patternBudget{})
	if node == nil || collected.errors() > 0 {
		t.Fatalf("schema refused: %+v", collected.sorted())
	}
	var findings []valueFinding
	node.validate(decodeAny(t, value), nil, &findings)
	var out []string
	for _, f := range findings {
		out = append(out, f.code)
	}
	return out
}

func schemaRefusal(t *testing.T, schema string) []string {
	t.Helper()
	collected := &collector{}
	compileSchema(decodeAny(t, schema), []string{"schema"}, Location{Document: DocumentKind}, collected, &patternBudget{})
	var out []string
	for _, d := range collected.sorted() {
		out = append(out, d.Code)
	}
	return out
}

func decodeAny(t *testing.T, text string) any {
	t.Helper()
	var value any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestEveryValueCode(t *testing.T) {
	for _, c := range []struct{ code, schema, bad, good string }{
		{"REG-VALUE-ANY-OF", `{"anyOf":[{"type":"string"},{"type":"integer"}]}`, `true`, `3`},
		{"REG-VALUE-CONST", `{"const":"a"}`, `"b"`, `"a"`},
		{"REG-VALUE-ITEM-COUNT", `{"type":"array","maxItems":1}`, `[1,2]`, `[1]`},
		{"REG-VALUE-ITEM-COUNT", `{"type":"array","minItems":2}`, `[1]`, `[1,2]`},
		{"REG-VALUE-LENGTH", `{"type":"string","maxLength":2}`, `"abc"`, `"ab"`},
		{"REG-VALUE-LENGTH", `{"type":"string","minLength":2}`, `"a"`, `"ab"`},
		{"REG-VALUE-NOT", `{"not":{"const":"x"}}`, `"x"`, `"y"`},
		{"REG-VALUE-PATTERN", `{"type":"string","pattern":"^a.b$"}`, "\"a b\"", `"a-b"`},
		{"REG-VALUE-PATTERN", `{"type":"string","pattern":"^a.b$"}`, `"a\rb"`, `"a€b"`},
		{"REG-VALUE-PROPERTY-COUNT", `{"type":"object","maxProperties":1}`, `{"a":1,"b":2}`, `{"a":1}`},
		{"REG-VALUE-PROPERTY-NAME", `{"type":"object","propertyNames":{"pattern":"^[a-z]+$"}}`, `{"A":1}`, `{"a":1}`},
		{"REG-VALUE-RANGE", `{"type":"number","exclusiveMaximum":1}`, `1`, `0.5`},
		{"REG-VALUE-RANGE", `{"type":"integer","minimum":1}`, `0`, `1`},
	} {
		t.Run(c.code+" "+c.bad, func(t *testing.T) {
			if got := schemaFindings(t, c.schema, c.bad); len(got) != 1 || got[0] != c.code {
				t.Fatalf("%s on %s: want [%s], got %v", c.schema, c.bad, c.code, got)
			}
			if got := schemaFindings(t, c.schema, c.good); len(got) != 0 {
				t.Fatalf("%s on %s: want none, got %v", c.schema, c.good, got)
			}
		})
	}
}

func TestSchemaLimits(t *testing.T) {
	branches := func(n int) string {
		items := make([]string, n)
		for i := range items {
			items[i] = fmt.Sprintf(`{"const":%d}`, i)
		}
		return `{"anyOf":[` + strings.Join(items, ",") + `]}`
	}
	enum := func(n int) string {
		items := make([]string, n)
		for i := range items {
			items[i] = fmt.Sprint(i)
		}
		return `{"enum":[` + strings.Join(items, ",") + `]}`
	}
	// properties holds n subschemas beside the root.
	properties := func(n int) string {
		items := make([]string, n)
		for i := range items {
			items[i] = fmt.Sprintf(`"p%d":{}`, i)
		}
		return `{"type":"object","properties":{` + strings.Join(items, ",") + `}}`
	}
	for _, c := range []struct {
		name, schema string
		refused      bool
	}{
		{"16 branches", branches(MaxBranches), false},
		{"17 branches", branches(MaxBranches + 1), true},
		{"1024 enum values", enum(MaxEnumValues), false},
		{"1025 enum values", enum(MaxEnumValues + 1), true},
		{"4096 subschemas", properties(MaxSubschemas - 1), false},
		{"4097 subschemas", properties(MaxSubschemas), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := schemaRefusal(t, c.schema)
			if c.refused != (len(got) == 1 && got[0] == "REG-KIND-SCHEMA-LIMIT") || !c.refused && len(got) != 0 {
				t.Fatalf("refused %v, got %v", c.refused, got)
			}
		})
	}
}

func TestPatternBoundsAndDot(t *testing.T) {
	if _, err := CompilePattern("^a{1000}$"); err != nil {
		t.Fatalf("{1000} refused: %v", err)
	}
	if _, err := CompilePattern("^a{1,1000}$"); err != nil {
		t.Fatalf("{1,1000} refused: %v", err)
	}
	for _, source := range []string{"^a{1001}$", "^a{0,1001}$"} {
		if _, err := CompilePattern(source); err == nil {
			t.Fatalf("%s admitted", source)
		}
	}
	dot, err := CompilePattern("^.$")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"\n", "\r", " ", " "} {
		if dot.MatchString(s) {
			t.Errorf(". matched %q", s)
		}
	}
	for _, s := range []string{"a", "€", "\U0001F600", "\t"} {
		if !dot.MatchString(s) {
			t.Errorf(". did not match %q", s)
		}
	}
}
