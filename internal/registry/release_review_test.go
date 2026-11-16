package registry

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp/syntax"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The findings of the v0.47.0 release review (#432), each with the review's
// input built here. The work each test does is bounded by what it
// allocates, never by the clock.

// reviewKind is a kind declaration of kind k around schema.
func reviewKind(schema string) string {
	return `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/k","version":"1.0.0","kind":"k","owner":"team-a","schema":` + schema + `}`
}

// reviewEntry is one entry of kind k holding value, written as JSON text.
func reviewEntry(value string) string {
	return `{"kind":"k","id":"e","owner":"team-a","value":` + value + `}`
}

// kindCodes returns the codes reported at the kind document, with their
// pointers.
func kindCodes(result *Result) []string {
	var out []string
	for _, diagnostic := range result.Report.Diagnostics {
		if diagnostic.Location.Document == DocumentKind {
			out = append(out, diagnostic.Code+" "+diagnostic.Location.Pointer)
		}
	}
	return out
}

// chainedDefinitions is the review's H1 schema: definitions d0 to dk-1, each
// an allOf of branches $refs to the next, and dk a string.
func chainedDefinitions(k, branches int) string {
	defs := make([]string, 0, k+1)
	for i := 0; i < k; i++ {
		refs := make([]string, branches)
		for b := range refs {
			refs[b] = fmt.Sprintf(`{"$ref":"#/$defs/d%d"}`, i+1)
		}
		defs = append(defs, fmt.Sprintf(`"d%d":{"allOf":[%s]}`, i, strings.Join(refs, ",")))
	}
	defs = append(defs, fmt.Sprintf(`"d%d":{"type":"string"}`, k))
	return `{"$defs":{` + strings.Join(defs, ",") + `},"$ref":"#/$defs/d0"}`
}

// H1. Each definition is counted as often as a $ref names it, so a 3 KB kind
// cannot expand to 16^8 subschemas. With k=2 the schema expands to 546
// subschemas and is admitted; with k=3 to 8738, and is refused, whether the
// registry holds a valid value, an invalid one or none.
func TestASchemaIsBoundedExpanded(t *testing.T) {
	const budget = 64 << 20
	registry, kinds := customRegistry(t, reviewEntry(`"x"`), reviewKind(chainedDefinitions(2, 16)))
	if result := run(registry, kinds); !result.Report.Valid {
		t.Fatalf("k=2: %+v", result.Report.Diagnostics)
	}
	for _, k := range []int{3, 7, 8} {
		for _, value := range []string{`"x"`, `1`, ``} {
			entries := ""
			if value != "" {
				entries = reviewEntry(value)
			}
			allocatesWithin(t, budget, fmt.Sprintf("k=%d value %q", k, value), func() {
				registry, kinds := customRegistry(t, entries, reviewKind(chainedDefinitions(k, 16)))
				result := run(registry, kinds)
				if got := kindCodes(result); result.Report.Valid || !slices.Equal(got, []string{"REG-KIND-SCHEMA-LIMIT /schema"}) {
					t.Fatalf("k=%d value %q: %v", k, value, got)
				}
				if _, _, err := result.FactsContract("k"); err != ErrInvalid {
					t.Fatalf("k=%d: a refused kind has no contract: %v", k, err)
				}
			})
		}
	}
}

// The expanded count's boundary: a schema of 63 members, each a $ref to a
// definition of 63 members, expands to 1 + 63*(1+1+63) = 4096 subschemas and
// is admitted; one member more is refused. Written, it has 128.
func TestTheExpandedCountsBoundary(t *testing.T) {
	members := func(n int, value string) string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf(`"m%02d":%s`, i, value)
		}
		return strings.Join(out, ",")
	}
	schema := func(extra string) string {
		return `{"$defs":{"d":{"properties":{` + members(63, `{}`) + `}}},"properties":{` + members(63, `{"$ref":"#/$defs/d"}`) + extra + `}}`
	}
	collected := &collector{}
	root := compileSchema(decodeAny(t, schema("")), []string{"schema"}, Location{Document: DocumentKind}, collected, &patternBudget{})
	if root == nil || collected.errors() > 0 || expandedSize(root) != MaxSubschemas {
		t.Fatalf("4096 expanded: %+v", collected.sorted())
	}
	if got := schemaRefusal(t, schema(`,"x":true`)); !slices.Equal(got, []string{"REG-KIND-SCHEMA-LIMIT"}) {
		t.Fatalf("4097 expanded: %v", got)
	}
}

// flatten is charged to the generation's work, each node once however many
// $refs reach it, and stops once the work is spent.
func TestFlattenIsChargedToTheGenerationsWork(t *testing.T) {
	collected := &collector{}
	root := compileSchema(decodeAny(t, chainedDefinitions(2, 16)), []string{"schema"}, Location{Document: DocumentKind}, collected, &patternBudget{})
	if root == nil {
		t.Fatalf("%+v", collected.sorted())
	}
	g := &generator{}
	atoms, _, _ := g.flatten([]*schemaNode{root})
	// The root, d0 and its 16 $ref branches, d1 and its 16, and d2: each
	// branch names the same definition, which is collected once.
	if len(atoms) != 36 || g.work != len(atoms) || g.exceeded {
		t.Fatalf("flatten collected %d atoms for %d work", len(atoms), g.work)
	}
	g = &generator{work: maxShapeWork - 10}
	if g.flatten([]*schemaNode{root}); !g.exceeded {
		t.Fatalf("flatten past the work bound: %d", g.work)
	}
	if g = (&generator{work: maxShapeWork - 10}); g.fromSchemas([]*schemaNode{root}) != nil || !g.exceeded {
		t.Fatal("a reading past the work bound gives no shape")
	}
}

// A closed list is checked against each schema that applies once, not once
// per $ref path, and each check is charged to the generation's work: a chain
// of 200 definitions under an enum of 1024 values is a contract, and a chain
// of 2000, two million checks, is past the work bound and refused.
func TestAClosedListIsCheckedOncePerSchema(t *testing.T) {
	chain := func(depth int) string {
		defs := make([]string, 0, depth+1)
		for i := 0; i < depth; i++ {
			defs = append(defs, fmt.Sprintf(`"d%d":{"allOf":[{"$ref":"#/$defs/d%d"}]}`, i, i+1))
		}
		defs = append(defs, fmt.Sprintf(`"d%d":{"type":"string"}`, depth))
		values := make([]string, 1024)
		for i := range values {
			values[i] = strconv.Quote(fmt.Sprintf("v%04d", i))
		}
		return `{"$defs":{` + strings.Join(defs, ",") + `},"enum":[` + strings.Join(values, ",") + `],"$ref":"#/$defs/d0"}`
	}
	registry, kinds := customRegistry(t, reviewEntry(`"v0001"`), reviewKind(chain(200)))
	result := run(registry, kinds)
	if !result.Report.Valid {
		t.Fatalf("%+v", result.Report.Diagnostics)
	}
	allocatesWithin(t, 128<<20, "a contract over a chain of 200 definitions", func() {
		_, lines := contractFacts(t, result, "k")
		if !slices.ContainsFunc(lines, func(line string) bool {
			// "values" and the 1024 values.
			return strings.HasPrefix(line, `{"pointer":"/value",`) && strings.Count(line, `"v`) == 1+1024
		}) {
			t.Fatalf("the value lists 1024 values: %v", lines)
		}
	})
	registry, kinds = customRegistry(t, reviewEntry(`"v0001"`), reviewKind(chain(2000)))
	result = run(registry, kinds)
	if !result.Report.Valid {
		t.Fatalf("%+v", result.Report.Diagnostics)
	}
	allocatesWithin(t, 128<<20, "a contract over a chain of 2000 definitions", func() {
		_, diagnostics, err := result.FactsContract("k")
		if err != nil || len(diagnostics) != 1 || diagnostics[0].Code != "FACTS-CONTRACT-LIMIT" {
			t.Fatalf("%v %+v", err, diagnostics)
		}
	})
}

// H2. A pattern within RFC 0034 §2.2's subset compiles to as many as a
// quarter of a million instructions, so its program is bounded, alone and
// with the kind's others, before it is compiled. The review's input is 880
// such patterns in a kind of 259 KB.
func TestAPatternsProgramIsBounded(t *testing.T) {
	huge := "(" + strings.Repeat("a", 248) + "){1000}"
	properties := make([]string, 880)
	want := make([]string, 880)
	for i := range properties {
		name := fmt.Sprintf("p%03d", i)
		properties[i] = fmt.Sprintf(`%q:{"type":"string","pattern":%q}`, name, huge)
		want[i] = "REG-KIND-PATTERN-LIMIT /schema/properties/" + name + "/pattern"
	}
	kind := reviewKind(`{"type":"object","properties":{` + strings.Join(properties, ",") + `}}`)
	if len(kind) > MaxKindBytes {
		t.Fatalf("the kind is %d bytes", len(kind))
	}
	allocatesWithin(t, 64<<20, "880 patterns of 248000 instructions", func() {
		registry, kinds := customRegistry(t, reviewEntry(`{"p000":"a"}`), kind)
		if got := kindCodes(run(registry, kinds)); !slices.Equal(got, want) {
			t.Fatalf("%d codes: %v", len(got), got[:min(len(got), 3)])
		}
	})

	// One pattern: (aaaa){1000} is 4000 instructions, and each literal after
	// it one more.
	exact := "(aaaa){1000}" + strings.Repeat("b", 96)
	if _, err := CompilePattern(exact); err != nil {
		t.Fatalf("4096 instructions: %v", err)
	}
	if _, err := CompilePattern(exact + "b"); err != errPatternSize {
		t.Fatalf("4097 instructions: %v", err)
	}
	if got := schemaRefusal(t, `{"pattern":"`+exact+`b"}`); !slices.Equal(got, []string{"REG-KIND-PATTERN-LIMIT"}) {
		t.Fatalf("4097 instructions in a schema: %v", got)
	}

	// The kind: an entryId pattern of 1 and sixteen of 4096 are 65537, so
	// the last of the sixteen does not fit; a refused pattern is not
	// counted, so the propertyNames pattern of 1 does. Without the two,
	// the sixteen are exactly 65536 and fit: the default entry id is not
	// the kind's.
	properties = properties[:0]
	for i := 0; i < 16; i++ {
		properties = append(properties, fmt.Sprintf(`"q%02d":{"pattern":%q}`, i, exact))
	}
	kind = `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/k","version":"1.0.0","kind":"k","owner":"team-a","entryId":{"pattern":"e"},` +
		`"schema":{"properties":{` + strings.Join(properties, ",") + `},"propertyNames":{"pattern":"q"}}}`
	registry, kinds := customRegistry(t, reviewEntry(`{}`), kind)
	if got := kindCodes(run(registry, kinds)); !slices.Equal(got, []string{"REG-KIND-PATTERN-LIMIT /schema/properties/q15/pattern"}) {
		t.Fatalf("the kind's patterns: %v", got)
	}
	kind = strings.Replace(strings.Replace(kind, `"entryId":{"pattern":"e"},`, ``, 1), `,"propertyNames":{"pattern":"q"}`, ``, 1)
	registry, kinds = customRegistry(t, reviewEntry(`{}`), kind)
	if result := run(registry, kinds); !result.Report.Valid {
		t.Fatalf("65536 instructions with the default entry id: %+v", result.Report.Diagnostics)
	}
}

// A pattern's size, counted on the pattern as written, is an upper bound on
// the program RE2 compiles, less its two fixed instructions, and close to it.
func TestPatternSizeBoundsTheCompiledProgram(t *testing.T) {
	for _, source := range []string{
		`^[a-z][a-z0-9-]{0,62}$`, `(` + strings.Repeat("a", 248) + `){1000}`, `[a-z]{0,1000}`, `.{0,1000}`,
		`(ab|cd){1000}`, `^([a-z0-9-]{1,63}\.){1,10}[a-z]{2,63}$`, `a{1000}`, `(a|b)*c+d?`, `(a*)*`, `x{2,}`, `(x{2,5}){3}`, `[^\d]`, `a||b`, `()`,
	} {
		translated, size, err := translateSubset(source)
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		parsed, err := syntax.Parse(translated, syntax.Perl)
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		program, err := syntax.Compile(parsed.Simplify())
		if err != nil {
			t.Fatal(err)
		}
		if compiled := len(program.Inst); min(compiled, MaxPatternInstructions+1) > size+2 || size > 2*compiled {
			t.Errorf("%s: counted %d, compiled %d", source, size, compiled)
		}
	}
}

// H3. Each list that must hold distinct items is checked in one pass. The
// review's inputs, at the sizes it measured: each took seconds to a minute.
func TestDistinctListsAreCheckedInOnePass(t *testing.T) {
	names := func(n int, alphabet string, length int) []string {
		out := make([]string, n)
		for i := range out {
			var name []byte
			for j, x := 0, i; j < length; j, x = j+1, x/len(alphabet) {
				name = append(name, alphabet[x%len(alphabet)])
			}
			out[i] = string(name)
		}
		return out
	}
	quoted := func(items []string) string {
		out := make([]string, len(items))
		for i, item := range items {
			out[i] = strconv.Quote(item)
		}
		return strings.Join(out, ",")
	}
	const budget = 512 << 20
	lower := "abcdefghijklmnopqrstuvwxyz"
	owners := names(100000, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", 4)
	scopes := names(100000, lower, 6)
	head := `{"registryFormatVersion":"1.0.0","id":"https://example.invalid/r","version":"1.0.0","environments":["prod"],"kinds":[],"entries":[],`
	for _, row := range []struct {
		name, document, code, pointer string
	}{
		{"100000 distinct owners", head + `"owners":[` + quoted(owners) + `]}`, "", ""},
		{"100000 owners, the last a repeat", head + `"owners":[` + quoted(append(owners[:99999:99999], owners[0])) + `]}`, "REG-REGISTRY-DUPLICATE", "/owners/99999"},
		{"100000 distinct projects", head + `"owners":["o"],"secretScopes":{"prod":{"gcp-sm":[` + quoted(scopes) + `]}}}`, "", ""},
		{"100000 projects, the last a repeat", head + `"owners":["o"],"secretScopes":{"prod":{"gcp-sm":[` + quoted(append(scopes[:99999:99999], scopes[0])) + `]}}}`, "REF-SECRET-SCOPE-FORM", "/secretScopes/prod/gcp-sm/99999"},
	} {
		allocatesWithin(t, budget, row.name, func() {
			result := Validate([]byte(row.document), nil)
			var got []string
			for _, diagnostic := range result.Report.Diagnostics {
				got = append(got, diagnostic.Code+" "+diagnostic.Location.Pointer)
			}
			if want := []string{row.code + " " + row.pointer}; row.code == "" && len(got) > 0 || row.code != "" && !slices.Equal(got, want) {
				t.Fatalf("%s: %v", row.name, got)
			}
		})
	}

	// uniqueItems over 10000 numbers, in each of the two environments.
	numbers := make([]string, 10000)
	for i := range numbers {
		numbers[i] = strconv.Itoa(i)
	}
	kind := reviewKind(`{"type":"array","uniqueItems":true}`)
	for _, row := range []struct {
		name, value string
		valid       bool
	}{
		{"10000 distinct numbers", `[` + strings.Join(numbers, ",") + `]`, true},
		{"10000 numbers, the last 1.0", `[` + strings.Join(numbers[:9999], ",") + `,1.0]`, false},
		{"10000 items, the last an object repeated in another order", `[` + strings.Join(numbers[:9998], ",") + `,{"a":1,"b":[2.0]},{"b":[2],"a":1}]`, false},
	} {
		allocatesWithin(t, budget, row.name, func() {
			registry, kinds := customRegistry(t, reviewEntry(row.value), kind)
			result := run(registry, kinds)
			unique := 0
			for _, diagnostic := range result.Report.Diagnostics {
				if diagnostic.Code == "REG-VALUE-UNIQUE" {
					unique++
				}
			}
			if result.Report.Valid != row.valid || !row.valid && unique != 2 {
				t.Fatalf("%s: %+v", row.name, result.Report.Diagnostics[:min(len(result.Report.Diagnostics), 5)])
			}
		})
	}

	// A contract's values: 100000 of them.
	values := names(100000, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", 4)
	contract := func(values []string) []byte {
		return []byte(`{"factsContractVersion":"1.0.0","id":"https://example.invalid/c","version":"1.0.0","source":{"authored":true},` +
			`"facts":[{"pointer":"/value","type":"string","present":"always","values":[` + quoted(values) + `]}]}`)
	}
	allocatesWithin(t, budget, "a contract of 100000 values", func() {
		read, diagnostics := ReadFactsContract(Document{Data: contract(values)})
		if read == nil || len(diagnostics) > 0 {
			t.Fatalf("100000 distinct values: %+v", diagnostics)
		}
		listed := map[string]any{"rules": []any{map[string]any{"when": map[string]any{"op": "all", "conditions": []any{map[string]any{"op": "fact", "path": "/value", "operator": "equals", "value": values[99999]}}}}}}
		if got := read.CheckPack(listed); len(got) > 0 {
			t.Fatalf("a listed pointer: %+v", got)
		}
		_, diagnostics = ReadFactsContract(Document{Data: contract(append(values[:99999:99999], values[0]))})
		if len(diagnostics) != 1 || diagnostics[0].Code != "FACTS-CONTRACT-FORM" || diagnostics[0].Location.Pointer != "/facts/0/values" {
			t.Fatalf("a repeated value: %+v", diagnostics)
		}
	})
}

// The other distinct lists, and the overlay's prefix check, at sizes a
// quadratic check would take minutes over.
func TestOtherDistinctListsAreCheckedInOnePass(t *testing.T) {
	const budget = 512 << 20
	items := func(n int, format string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf(format, i)
		}
		return out
	}
	// required, and a kind's secrets.
	required := items(20000, `"r%d"`)
	secrets := items(20000, `"/s%d"`)
	for _, row := range []struct{ name, kind, want string }{
		{"20000 required", reviewKind(`{"required":[` + strings.Join(required, ",") + `]}`), ""},
		{"20000 required, the last a repeat", reviewKind(`{"required":[` + strings.Join(required, ",") + `,"r0"]}`), "REG-KIND-SCHEMA-FORM /schema/required"},
		{"20000 secrets, the last a repeat", strings.Replace(reviewKind(`{}`), `"schema":{}`, `"schema":{},"secrets":[`+strings.Join(secrets, ",")+`,"/s0"]`, 1), "REG-KIND-FORM /secrets/20000"},
	} {
		allocatesWithin(t, budget, row.name, func() {
			registry, kinds := customRegistry(t, "", row.kind)
			got := kindCodes(run(registry, kinds))
			if row.want == "" && len(got) > 0 || row.want != "" && !slices.Equal(got, []string{row.want}) {
				t.Fatalf("%s: %v", row.name, got)
			}
		})
	}
	// An entry's environments, and an overlay of 20000 pointers where the
	// last is under the first.
	kind := strings.Replace(reviewKind(`{"type":"object"}`), `"schema":{"type":"object"}`, `"schema":{"type":"object"},"overlayable":["/*","/*/*"],"bounds":{"maxEntryBytes":1048576}`, 1)
	set := items(20000, `"/m%05d":1`)
	base := append([]string{`"m00000":{"x":0}`}, items(20000, `"m%05d":0`)[1:]...)
	allocatesWithin(t, budget, "an overlay of 20000 pointers", func() {
		registry, kinds := customRegistry(t, reviewEntry(`{`+strings.Join(base, ",")+`}`), kind)
		overlay := `,"overlays":[{"environment":"prod","patches":[{"kind":"k","id":"e","set":{` + strings.Join(set, ",") + `}}]}]}`
		if result := run(strings.TrimSuffix(registry, "}")+overlay, kinds); !result.Report.Valid {
			t.Fatalf("20000 distinct pointers: %+v", result.Report.Diagnostics[:min(len(result.Report.Diagnostics), 3)])
		}
		overlay = strings.Replace(overlay, `"/m19999":1`, `"/m00000/x":1`, 1)
		result := run(strings.TrimSuffix(registry, "}")+overlay, kinds)
		var got []string
		for _, diagnostic := range result.Report.Diagnostics {
			got = append(got, diagnostic.Code+" "+diagnostic.Location.Pointer)
		}
		if !slices.Equal(got, []string{"REG-OVERLAY-PREFIX /overlays/0/patches/0/set/~1m00000~1x"}) {
			t.Fatalf("a pointer under another: %v", got)
		}
	})
	envs := items(100000, `"x%d"`)
	allocatesWithin(t, budget, "an entry of 100000 environments, the last a repeat", func() {
		entry := `{"kind":"k","id":"e","owner":"team-a","environments":[` + strings.Join(envs, ",") + `,"x0"],"value":{}}`
		registry, kinds := customRegistry(t, entry, reviewKind(`{}`))
		if !hasCode(run(registry, kinds), "REG-REGISTRY-FORM") {
			t.Fatal("a repeated environment is refused")
		}
	})
}

// The prefix tree finds a prefix either way, and an equal path.
func TestPathTreeFindsAPrefixEitherWay(t *testing.T) {
	tree := &pathTree{}
	for _, row := range []struct {
		path     []string
		conflict bool
	}{
		{[]string{"a", "b"}, false},
		{[]string{"a", "c"}, false},
		{[]string{"a"}, true},
		{[]string{"a", "b", "c"}, true},
		{[]string{"b"}, false},
		{[]string{"b"}, true},
		{[]string{"ab"}, false},
	} {
		if got := tree.insertConflicts(row.path); got != row.conflict {
			t.Errorf("%v: conflict %v, want %v", row.path, got, row.conflict)
		}
	}
}

// H4. A contract's report is capped as validate's is: the review's contract
// of numbers admission refuses, at a size that keeps the test quick.
func TestAContractsReportIsCapped(t *testing.T) {
	const n = 100000
	data := []byte(`{"factsContractVersion":"1.0.0","facts":[` + strings.TrimSuffix(strings.Repeat("-0,", n), ",") + `]}`)
	var diagnostics []Diagnostic
	allocatesWithin(t, 256<<20, "a contract of 100000 refused numbers", func() {
		_, diagnostics = ReadFactsContract(Document{Data: data})
	})
	var uncapped []Diagnostic
	withLimit(0, func() { _, uncapped = ReadFactsContract(Document{Data: data}) })
	if len(uncapped) != n {
		t.Fatalf("uncapped: %d", len(uncapped))
	}
	checkCappedAt(t, "numbers", diagnostics, uncapped, DocumentContract)

	// The form diagnostics are capped with them.
	items := make([]string, 2000)
	for i := range items {
		items[i] = `{"pointer":"x","type":"t","present":"p"}`
	}
	data = []byte(`{"factsContractVersion":"1.0.0","id":"https://example.invalid/c","version":"1.0.0","source":{"authored":true},"facts":[` + strings.Join(items, ",") + `]}`)
	_, diagnostics = ReadFactsContract(Document{Data: data})
	withLimit(0, func() { _, uncapped = ReadFactsContract(Document{Data: data}) })
	if len(uncapped) <= maxReportDiagnostics {
		t.Fatalf("uncapped form diagnostics: %d", len(uncapped))
	}
	checkCappedAt(t, "form", diagnostics, uncapped, DocumentContract)
}

// checkCappedAt is checkCapped with the truncation located at document.
func checkCappedAt(t *testing.T, name string, capped, uncapped []Diagnostic, document string) {
	t.Helper()
	slices.SortFunc(uncapped, compareDiagnostics)
	if len(capped) != maxReportDiagnostics+1 || !reflect.DeepEqual(capped[:maxReportDiagnostics], uncapped[:maxReportDiagnostics]) {
		t.Fatalf("%s: %d diagnostics, not the first %d of %d", name, len(capped), maxReportDiagnostics, len(uncapped))
	}
	if last := capped[maxReportDiagnostics]; last.Code != "REG-REPORT-TRUNCATED" || last.Severity != SeverityWarning || !reflect.DeepEqual(last.Location, Location{Document: document}) {
		t.Fatalf("%s: last %+v", name, last)
	}
}

// H5. A long literal is kept as its shortest form at admission, so it is
// compared with the range like any other. Each literal here is 1 written
// at a million zeros' length, past big.Rat's own exponent cap, where the
// range check was skipped.
func TestALongDecimalIsComparedWithItsRange(t *testing.T) {
	zeros := strings.Repeat("0", 1000001)
	kind := reviewKind(`{"type":"number","maximum":0}`)
	for _, literal := range []string{
		"1.0000000000",
		"1." + zeros,
		"1" + zeros + "e-1000001",
		"0." + zeros + "1e+1000002",
		"1e" + zeros + "0",
	} {
		allocatesWithin(t, 128<<20, fmt.Sprintf("a literal of %d bytes", len(literal)), func() {
			registry, kinds := customRegistry(t, reviewEntry(literal), kind)
			result := run(registry, kinds)
			if result.Report.Valid || !hasCode(result, "REG-VALUE-RANGE") {
				t.Fatalf("%d bytes: %+v", len(literal), result.Report.Diagnostics)
			}
		})
	}
	// Resolved, 1. and two million zeros is 1.
	allocatesWithin(t, 128<<20, "resolving 1. and two million zeros", func() {
		registry, kinds := customRegistry(t, reviewEntry("1."+strings.Repeat("0", 2000000)), reviewKind(`{"type":"integer"}`))
		result := run(registry, kinds)
		resolved, err := result.Resolve("prod")
		if err != nil || !strings.Contains(string(resolved), `"value":1}`) {
			t.Fatalf("%v %.200s %+v", err, resolved, result.Report.Diagnostics)
		}
	})
}

// Admission keeps each number as a literal of at most 25 bytes, whatever its
// length as written.
func TestAdmissionKeepsTheShortestLiteral(t *testing.T) {
	zeros := strings.Repeat("0", 100000)
	for written, kept := range map[string]string{
		"1.0":                        "1",
		"1." + zeros:                 "1",
		"1.5" + zeros:                "1.5",
		"1" + zeros + "e-100000":     "1",
		"0." + zeros + "25e100001":   "2.5",
		"1e" + zeros + "5":           "100000",
		"-1.25e-" + zeros + "7":      "-1.25e-7",
		"0." + zeros + "e5":          "0",
		"9007199254740991":           "9007199254740991",
		"-17":                        "-17",
		"1.7976931348623157e308":     "1.7976931348623157e+308",
		"-0.0000012345678901234567":  "-0.0000012345678901234567",
		"123456789012345678901234.0": "",
	} {
		root := admit([]byte(`{"s":"1.0.0","n":`+written+`}`), MaxRegistryBytes, "s", "1.0.0", Location{}, &collector{})
		if kept == "" {
			if root != nil {
				t.Errorf("%.40s: admitted", written)
			}
			continue
		}
		if root == nil || root["n"] != json.Number(kept) || len(kept) > 25 {
			t.Errorf("%.40s: kept %v, want %s", written, root["n"], kept)
		}
	}
}

// A number that cannot be compared with a range is outside it, never let
// through: big.Rat refuses an exponent past 10^6.
func TestAnIncomparableNumberIsOutsideTheRange(t *testing.T) {
	for _, literal := range []string{"1e9999999", "-1e9999999", "1e-9999999"} {
		for _, schema := range []string{`{"maximum":0}`, `{"minimum":0}`, `{"exclusiveMaximum":1}`, `{"exclusiveMinimum":-1}`} {
			collected := &collector{}
			node := compileSchema(decodeAny(t, schema), []string{"schema"}, Location{Document: DocumentKind}, collected, &patternBudget{})
			var findings []valueFinding
			node.validate(&validation{work: maxValidationWork}, json.Number(literal), nil, &findings)
			if len(findings) != 1 || findings[0].code != "REG-VALUE-RANGE" {
				t.Errorf("%s against %s: %+v", literal, schema, findings)
			}
		}
	}
	if got := schemaFindings(t, `{"maximum":0}`, "0"); len(got) != 0 {
		t.Errorf("0 against a maximum of 0: %v", got)
	}
}

// A number with no normal decimal is an error to the facts mapping and to a
// contract's closed list, never a value skipped or a list widened. Admission
// lets no such number through; these are the paths should one arrive.
func TestANumberWithNoNormalDecimalIsAnError(t *testing.T) {
	for _, literal := range []string{"1e9999999", "1e-9999999"} {
		if _, ok := normalDecimal(json.Number(literal)); ok {
			t.Errorf("%s: a normal decimal", literal)
		}
		if _, err := mapNumbers(map[string]any{"a": []any{json.Number(literal)}}); err != errUnmapped {
			t.Errorf("%s: mapped: %v", literal, err)
		}
		g := &generator{}
		if g.literal(json.Number(literal)) != nil || !g.unmapped {
			t.Errorf("%s: a contract literal", literal)
		}
	}
}

// M1. Two byte-different documents of one kind are one kind; which one is
// read, and so the kindDocument its findings are located at, does not depend
// on the order of --kind.
func TestTheKindReadDoesNotDependOnTheOrderSupplied(t *testing.T) {
	compact := `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/k","version":"1.0.0","kind":"k","owner":"team-a","schema":{},"extra":1}`
	spaced := strings.ReplaceAll(compact, ",", ", ")
	registry, _ := customRegistry(t, "", compact)
	forward := encode(t, run(registry, []string{compact, spaced}).Report)
	backward := encode(t, run(registry, []string{spaced, compact}).Report)
	if forward != backward || !strings.Contains(forward, "REG-KIND-UNKNOWN-MEMBER") {
		t.Fatalf("the report depends on the order:\n%s\n%s", forward, backward)
	}
	first := min(rawDigest([]byte(compact)), rawDigest([]byte(spaced)))
	if !strings.Contains(forward, `"kindDocument":"`+first+`"`) {
		t.Fatalf("the kind read is not the one whose bytes sort first: %s", forward)
	}
}

// L1. U+0085, U+2028 and U+2029 are line breaks to go-yaml and YAML 1.1 and
// content to YAML 1.2, so a file holding one is refused; the escapes that
// write them are read.
func TestImportRefusesTheCharactersYAMLReadsTwoWays(t *testing.T) {
	selector := "kindFormatVersion: \"1.0.0\"\n"
	for _, row := range []struct {
		source       string
		line, column int
	}{
		{"description: allowed   denied\n", 2, 21},
		{"x: \"a\u0085b\"\n", 2, 6},
		{"x: a b\n", 2, 5},
		{"#  \n", 2, 3},
	} {
		_, report := importText(selector + row.source)
		want := []Diagnostic{{Code: "REG-YAML-ENCODING", Severity: SeverityError, Location: Location{Document: DocumentYAML, Line: row.line, Column: row.column}, Message: Codes["REG-YAML-ENCODING"].message}}
		if report.Valid || !reflect.DeepEqual(report.Diagnostics, want) {
			t.Errorf("%q: %+v", row.source, report.Diagnostics)
		}
	}
	document, report := importText(selector + `x: "a\Nb\Lc\Pd"` + "\n")
	if want := "{\"kindFormatVersion\":\"1.0.0\",\"x\":\"a\u0085b c d\"}"; !report.Valid || string(document) != want {
		t.Errorf("escapes: %s %+v", document, report.Diagnostics)
	}
}

// expandingArrayKind is the H6 review's kind: an array whose items are d0,
// each definition an allOf of three $refs to the next, six levels deep, so
// checking one item visits about 2187 schema nodes.
func expandingArrayKind() string {
	defs := make([]string, 0, 7)
	for i := 0; i < 6; i++ {
		ref := fmt.Sprintf(`{"$ref":"#/$defs/d%d"}`, i+1)
		defs = append(defs, fmt.Sprintf(`"d%d":{"allOf":[%s,%s,%s]}`, i, ref, ref, ref))
	}
	defs = append(defs, `"d6":{}`)
	return `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/k","version":"1.0.0","kind":"k","owner":"team-a",` +
		`"bounds":{"maxEntryBytes":1048576},"schema":{"type":"array","items":{"$ref":"#/$defs/d0"},"$defs":{` + strings.Join(defs, ",") + `}}}`
}

func zeros(n int) string {
	return "[" + strings.TrimSuffix(strings.Repeat("0,", n), ",") + "]"
}

// H6. Step 7's schema checks share one work bound over the registry, so a
// value whose positions multiply its schema's expansion is refused at the
// entry where the bound runs out, once, rather than checked for minutes in
// every environment.
func TestValidationIsBoundedOverTheRegistry(t *testing.T) {
	allocatesWithin(t, 64<<20, "40000 items of a 2187-node expansion", func() {
		registry, kinds := customRegistry(t, reviewEntry(zeros(40000)), expandingArrayKind())
		result := run(registry, kinds)
		var got []string
		for _, d := range result.Report.Diagnostics {
			entry := ""
			if d.Location.Entry != nil {
				entry = d.Location.Entry.ID
			}
			got = append(got, d.Code+" "+string(d.Location.Document)+" "+entry+" "+d.Location.Environment+" "+d.Location.Pointer)
		}
		if result.Report.Valid || !slices.Equal(got, []string{"REG-BOUND-VALIDATION-WORK value e dev "}) {
			t.Fatalf("%v", got)
		}
	})
}

// H6. A value is checked against its kind once, however many environments
// hold it: 20000 items cost over half the bound, so checking them in both
// environments would spend it.
func TestAValueIsCheckedOnceAcrossEnvironments(t *testing.T) {
	registry, kinds := customRegistry(t, reviewEntry(zeros(20000)), expandingArrayKind())
	if result := run(registry, kinds); !result.Report.Valid {
		t.Fatalf("%+v", result.Report.Diagnostics)
	}
	// A small value of the same kind is checked as before.
	registry, kinds = customRegistry(t, reviewEntry(`["x"]`), expandingArrayKind())
	if result := run(registry, kinds); !result.Report.Valid {
		t.Fatalf("a small value: %+v", result.Report.Diagnostics)
	}
}

// H6 (self-check). Work within one visit grows with the value, so it is
// charged too: 512 uniqueItems nodes at one position, over 20000 distinct
// numbers, visit about 1200 nodes but key ten million items. Each key is
// charged, so the bound runs out partway.
func TestWorkWithinAVisitIsCharged(t *testing.T) {
	level := func(name, next string) string {
		ref := `{"$ref":"#/$defs/` + next + `"}`
		return `"` + name + `":{"allOf":[` + strings.TrimSuffix(strings.Repeat(ref+",", 8), ",") + `]}`
	}
	kind := `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/k","version":"1.0.0","kind":"k","owner":"team-a",` +
		`"bounds":{"maxEntryBytes":1048576},"schema":{"$ref":"#/$defs/d0","$defs":{` +
		level("d0", "d1") + "," + level("d1", "d2") + "," + level("d2", "u") + `,"u":{"uniqueItems":true}}}}`
	items := make([]string, 20000)
	for i := range items {
		items[i] = strconv.Itoa(i)
	}
	registry, kinds := customRegistry(t, reviewEntry("["+strings.Join(items, ",")+"]"), kind)
	result := run(registry, kinds)
	var got []string
	for _, d := range result.Report.Diagnostics {
		got = append(got, d.Code)
	}
	if result.Report.Valid || !slices.Equal(got, []string{"REG-BOUND-VALIDATION-WORK"}) {
		t.Fatalf("%v", got)
	}
	// The same kind over a short array is checked in full.
	registry, kinds = customRegistry(t, reviewEntry(`[1,2,3]`), kind)
	if result := run(registry, kinds); !result.Report.Valid {
		t.Fatalf("a short array: %+v", result.Report.Diagnostics)
	}
}
