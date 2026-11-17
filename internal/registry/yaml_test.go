package registry

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// Every fixture is synthetic: example.invalid ids and made-up projects.

// The registry fixture of registry_test.go, in YAML, with its kinds and
// entries out of canonical order and both block and flow styles.
const registryYAML = `# A synthetic registry.
registryFormatVersion: "1.0.0"
id: https://example.invalid/registries/platform
version: 4.0.0
environments: [dev, staging, prod]
owners:
  - platform-data
  - team-billing
secretScopes:
  dev: {gcp-sm: [example-dev]}
  staging: {gcp-sm: [example-staging]}
  prod: {gcp-sm: [example-prod]}
kinds:
  - kind: db-role
    id: https://example.invalid/kinds/db-role
    version: 1.2.0
    digest: ROLE
  - {kind: database, id: "https://example.invalid/kinds/database", version: 1.0.0, digest: DATABASE}
entries:
  - kind: db-role
    id: billing-backfill
    owner: team-billing
    environments: [dev, staging]
    value:
      database: "entry:database/billing#/engine"
      privileges: [read]
  - kind: db-role
    id: billing-api
    owner: team-billing
    value:
      database: entry:database/billing
      privileges:
      - read
      - write
      password: secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/3
      connectionLimit: 20 # a comment
  - kind: database
    id: billing
    owner: team-billing
    value: {engine: postgres}
overlays:
  - environment: staging
    patches:
      - kind: db-role
        id: billing-api
        set:
          /password: secret:gcp-sm:projects/example-staging/secrets/billing-db/versions/5
  - environment: prod
    patches:
      - kind: db-role
        id: billing-api
        set: {/connectionLimit: 120,
          /password: 'secret:gcp-sm:projects/example-prod/secrets/billing-db/versions/7'}
`

const roleKindYAML = `kindFormatVersion: '1.0.0'
id: https://example.invalid/kinds/db-role
version: 1.2.0
kind: db-role
owner: platform-data
entryId:
  pattern: ^[a-z][a-z0-9-]{0,62}$
schema:
  type: object
  required: [database, privileges]
  additionalProperties: false
  properties:
    database: {type: string, maxLength: 200}
    privileges:
      type: array
      items: {enum: [read, write, admin]}
      uniqueItems: true
    password: {type: string}
    connectionLimit: {type: integer, minimum: 1, maximum: 500}
    legacyFlag: {type: boolean, deprecated: true}
secrets: [/password]
references:
- pointer: /database
  kinds: [database]
overlayable: [/connectionLimit, /password]
bounds: {maxEntries: 1000, maxEntryBytes: 16384}
`

const databaseKindYAML = `---
kindFormatVersion: "1.0.0"
id: https://example.invalid/kinds/database
version: 1.0.0
kind: database
owner: platform-data
schema:
  type: object
  required: [engine]
  additionalProperties: false
  properties:
    engine: {enum: [postgres, mysql]}
    region: {type: string}
...
`

// readYAML reads a YAML file without import's selector and admission, and
// returns its canonical JSON.
func readYAML(source string) (string, []Diagnostic) {
	diagnostics := &collector{}
	text, ok := decodeYAMLText([]byte(source), diagnostics)
	if !ok {
		return "", diagnostics.sorted()
	}
	root := newYAMLParser(text, diagnostics).parse()
	if root == nil || diagnostics.errors() > 0 {
		return "", diagnostics.sorted()
	}
	out, err := jcs.EncodeDocument(root.toValue())
	if err != nil {
		panic(err)
	}
	return string(out), diagnostics.sorted()
}

func canonicalJSON(t *testing.T, document string) string {
	t.Helper()
	out, err := jcs.EncodeDocument(decode(t, document))
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func importText(source string) ([]byte, Report) {
	return Import(Document{Data: []byte(source)})
}

func TestImportGivesTheCanonicalJSONTheValidatorReads(t *testing.T) {
	registryJSON, _ := fixture(t)
	digests := strings.NewReplacer("DATABASE", digestOf(t, databaseKind), "ROLE", digestOf(t, roleKind))
	imported, report := importText(digests.Replace(registryYAML))
	if !report.Valid {
		t.Fatalf("registry: %+v", report.Diagnostics)
	}
	// The same bytes as the JSON fixture's canonical form: kinds and entries
	// in canonical order, numbers and strings as written.
	if want := canonicalJSON(t, registryJSON); string(imported) != want {
		t.Fatalf("registry:\n got %s\nwant %s", imported, want)
	}
	again, _ := importText(digests.Replace(registryYAML))
	if string(again) != string(imported) {
		t.Fatal("the same file gave other bytes")
	}
	var kinds [][]byte
	for _, pair := range [][2]string{{roleKindYAML, roleKind}, {databaseKindYAML, databaseKind}} {
		kind, report := importText(pair[0])
		if !report.Valid {
			t.Fatalf("kind: %+v", report.Diagnostics)
		}
		if want := canonicalJSON(t, pair[1]); string(kind) != want {
			t.Fatalf("kind:\n got %s\nwant %s", kind, want)
		}
		kinds = append(kinds, kind)
	}
	if result := Validate(imported, kinds); !result.Report.Valid || len(result.Report.Diagnostics) != 0 {
		t.Fatalf("the imported registry does not validate: %+v", result.Report.Diagnostics)
	}
}

func TestImportReadsTheSubset(t *testing.T) {
	for _, row := range []struct{ name, source, want string }{
		{"scalars", "a: 1\nb: -2\nc: 1.50\nd: true\ne: false\nf: null\ng: text\nh: 0\ni: -0.5\n",
			`{"a":1,"b":-2,"c":1.5,"d":true,"e":false,"f":null,"g":"text","h":0,"i":-0.5}`},
		{"integers at the bound", "a: 9007199254740991\nb: -9007199254740991\n", `{"a":9007199254740991,"b":-9007199254740991}`},
		{"strings that only look numeric", "v: 1.2.0\nu: https://example.invalid/x\nt: a:b\ns: 1.5.6\nr: 12:3a\nq: _5\np: 0x\no: 1,000\n",
			`{"o":"1,000","p":"0x","q":"_5","r":"12:3a","s":"1.5.6","t":"a:b","u":"https://example.invalid/x","v":"1.2.0"}`},
		{"quoted", `a: "yes"` + "\nb: 'it''s'\n" + `c: "t\tq\u00e9 \U0001F600 \ud83d\ude00 \x41\/\\\""` + "\nd: ''\n",
			`{"a":"yes","b":"it's","c":"t\tqé 😀 😀 A/\\\"","d":""}`},
		{"plain over lines", "a: one\n  two\n\n  three\nb: x\n", `{"a":"one two\nthree","b":"x"}`},
		{"quoted over lines", "a: \"one\n  two\\\n  three \"\nb: 'x\n\n  y'\n", `{"a":"one twothree ","b":"x\ny"}`},
		{"literal", "a: |\n  line 1\n    indented\n\n  line 3\nb: x\n", `{"a":"line 1\n  indented\n\nline 3\n","b":"x"}`},
		{"folded", "a: >\n  one\n  two\n\n  three\n    more\n  four\n", `{"a":"one two\nthree\n  more\nfour\n"}`},
		{"chomping and indentation", "a: |-\n  x\n\nb: |+\n  y\n\nc: >2\n   z\nd: |\ne: >-\n  last", `{"a":"x","b":"y\n\n","c":" z\n","d":"","e":"last"}`},
		{"sequences", "a:\n- 1\n- x\nb:\n  - - c\n    - d\n  - e: 1\n    f: 2\n", `{"a":[1,"x"],"b":[["c","d"],{"e":1,"f":2}]}`},
		{"flow", "a: [1, \"x\", {b: c, 'd': [ ]}, e: f]\nb: {\"k\":1}\nc: [\n  x,\n  z,\n  ]\n", `{"a":[1,"x",{"b":"c","d":[]},{"e":"f"}],"b":{"k":1},"c":["x","z"]}`},
		{"markers and comments", "# head\n--- # start\na: 1 # trailing\n# middle\nb: 2\n...\n# after\n", `{"a":1,"b":2}`},
		{"JSON is in the subset", "{\n  \"a\": [1, 2.5, \"x\"],\n  \"b\": {\"c\": null}\n}\n", `{"a":[1,2.5,"x"],"b":{"c":null}}`},
		{"line breaks", "a: 1\r\nb: |\r\n  x\r\nc: 2\rd: 3", `{"a":1,"b":"x\n","c":2,"d":3}`},
		{"a node on the start marker", "--- [1, 2]\n", `[1,2]`},
		{"keys", "\"a b\": 1\nc d : 2\n'3': 3\n", `{"3":3,"a b":1,"c d":2}`},
		{"a flow scalar starting with a dash", "a: [-b, {c: -d}]\n", `{"a":["-b",{"c":"-d"}]}`},
		{"a quoted flow key takes its value at once", "a: {\"b\":[0], 'c':{d: e}}\n", `{"a":{"b":[0],"c":{"d":"e"}}}`},
		{"spaces at the end of the file end a block scalar", "A: |\n  ", `{"A":""}`},
		{"spaces at the end of the file after content", "A: |\n  x\n ", `{"A":"x\n"}`},
		{"spaces at the end of the file, kept", "A: |+\n  ", `{"A":""}`},
		{"spaces at the end of the file, nested", "a:\n  b: >\n  ", `{"a":{"b":""}}`},
	} {
		got, diagnostics := readYAML(row.source)
		if len(diagnostics) != 0 || got != row.want {
			t.Errorf("%s: got %s %+v, want %s", row.name, got, diagnostics, row.want)
		}
	}
}

func TestImportRefusesOutsideTheSubset(t *testing.T) {
	nested := func(levels int) string {
		return "a: " + strings.Repeat("[", levels-1) + strings.Repeat("]", levels-1) + "\n"
	}
	type finding struct {
		code         string
		line, column int
	}
	rows := []struct {
		name   string
		source string
		want   []finding
	}{
		{"byte-order mark", "\ufeffa: 1\n", []finding{{"REG-YAML-ENCODING", 1, 1}}},
		{"not UTF-8", "a: \xff\n", []finding{{"REG-YAML-ENCODING", 1, 4}}},
		{"a control character", "a: 1\nb: \x01\n", []finding{{"REG-YAML-ENCODING", 2, 4}}},
		{"no document", "# only a comment\n", []finding{{"REG-YAML-DOCUMENTS", 2, 1}}},
		{"two documents", "a: 1\n---\nb: 2\n", []finding{{"REG-YAML-DOCUMENTS", 2, 1}}},
		{"a document after the end", "a: 1\n...\nb: 2\n", []finding{{"REG-YAML-DOCUMENTS", 3, 1}}},
		{"an empty document", "---\n", []finding{{"REG-YAML-EMPTY", 1, 1}}},
		{"a directive", "%YAML 1.2\n---\na: 1\n", []finding{{"REG-YAML-DIRECTIVE", 1, 1}}},
		{"a tag", "a: !!str 1\n", []finding{{"REG-YAML-TAG", 1, 4}}},
		{"an anchor and an alias", "a: &x 1\nb: *x\n", []finding{{"REG-YAML-ANCHOR", 1, 4}, {"REG-YAML-ANCHOR", 2, 4}}},
		{"a merge key", "a: 1\n<<: {b: 2}\n", []finding{{"REG-YAML-MERGE", 2, 1}}},
		{"a key YAML 1.1 reads as a boolean", "on: 1\n", []finding{{"REG-YAML-SCALAR", 1, 1}}},
		{"a key that is a number", "1: a\n", []finding{{"REG-YAML-KEY", 1, 1}}},
		{"a key that is null", "null: a\n", []finding{{"REG-YAML-KEY", 1, 1}}},
		{"an explicit key", "? a\n: b\n", []finding{{"REG-YAML-KEY", 1, 1}}},
		{"a complex key", "[a]: b\n", []finding{{"REG-YAML-KEY", 1, 1}}},
		{"a quoted key over lines", "\"a\n b\": 1\n", []finding{{"REG-YAML-KEY", 1, 1}}},
		{"a plain key over lines", "a\n b: c\n", []finding{{"REG-YAML-KEY", 1, 1}}},
		{"a flow key over lines", "a: {\"x\n y\": 1}\n", []finding{{"REG-YAML-KEY", 1, 5}}},
		{"a flow key and its colon on two lines", "a: {x\n : 1}\n", []finding{{"REG-YAML-KEY", 1, 5}}},
		{"an empty value", "a:\nb: 1\n", []finding{{"REG-YAML-EMPTY", 1, 2}}},
		{"an empty entry", "a:\n-\n- 1\n", []finding{{"REG-YAML-EMPTY", 2, 1}}},
		{"a flow key with no value", "a: {b}\n", []finding{{"REG-YAML-EMPTY", 1, 5}}},
		{"a duplicate key", "a: 1\n\"a\": 2\n", []finding{{"REG-DOC-DUPLICATE", 2, 1}}},
		{"a duplicate flow key", "a: {b: 1, b: 2}\n", []finding{{"REG-DOC-DUPLICATE", 1, 11}}},
		{"an integer past 2^53-1", "a: 9007199254740992\n", []finding{{"REG-DOC-NUMBER", 1, 4}}},
		{"an integer below -(2^53-1)", "a: -9007199254740992\n", []finding{{"REG-DOC-NUMBER", 1, 4}}},
		{"negative zero", "a: [-0, -0.0]\n", []finding{{"REG-DOC-NUMBER", 1, 5}, {"REG-DOC-NUMBER", 1, 9}}},
		{"a decimal that does not round-trip", "a: 0.10000000000000001\n", []finding{{"REG-DOC-NUMBER", 1, 4}}},
		{"a lone surrogate", `a: "x\ud800"` + "\n", []finding{{"REG-DOC-STRING", 1, 6}}},
		{"depth 33", nested(33), []finding{{"REG-DOC-DEPTH", 1, 35}}},
		{"a tab in the indentation", "a:\n\tb: 1\n", []finding{{"REG-YAML-SYNTAX", 2, 1}}},
		{"a mapping on a key's line", "a: b: c\n", []finding{{"REG-YAML-SYNTAX", 1, 4}}},
		{"an unterminated quote", "a: \"x\n", []finding{{"REG-YAML-SYNTAX", 1, 4}}},
		{"an unknown escape", `a: "\q"` + "\n", []finding{{"REG-YAML-SYNTAX", 1, 5}}},
		{"a bad indentation", "a:\n  b: 1\n c: 2\n", []finding{{"REG-YAML-SYNTAX", 3, 2}}},
		{"a flow line not indented", "a: [\nb]\n", []finding{{"REG-YAML-SYNTAX", 2, 1}}},
		{"a reserved indicator", "a: @x\n", []finding{{"REG-YAML-SYNTAX", 1, 4}}},
		{"a block scalar header", "a: |x\n  y\n", []finding{{"REG-YAML-SYNTAX", 1, 5}}},
		{"a tab before a compact mapping", "-\ta: 1\n", []finding{{"REG-YAML-SYNTAX", 1, 3}}},
		{"a flow scalar starting with ?", "a: {?b: c}\n", []finding{{"REG-YAML-KEY", 1, 5}}},
		{"a flow scalar starting with ? in a sequence", "a: [?b]\n", []finding{{"REG-YAML-KEY", 1, 5}}},
		{"a flow scalar starting with :", "a: [:b]\n", []finding{{"REG-YAML-KEY", 1, 5}}},
		{"a flow value starting with :", "a: {b: :c}\n", []finding{{"REG-YAML-KEY", 1, 8}}},
		{"a plain flow key with a sequence right after its colon", "a: {b:[0]}\n", []finding{{"REG-YAML-SYNTAX", 1, 7}}},
		{"a plain flow key with a mapping right after its colon", "a: [b:{c: d}]\n", []finding{{"REG-YAML-SYNTAX", 1, 7}}},
	}
	for _, scalar := range []string{"yes", "no", "on", "off", "y", "N", "~", "True", "NULL", "0o17", "0x1F", "1e3", "1.5e-3", ".inf", "-.Inf", ".nan",
		"007", "+5", "1_000", "12:30", ".5", "5.", "2026-11-16", "2026-11-16 10:00:00", "2026-11-16T10:00:00Z", "=", "<<",
		// What go-yaml v3 reads as a number or a timestamp: a radix prefix in
		// either case, an underscore anywhere in a number, and dates and
		// times with one-digit parts.
		"0X1F", "0O17", "0B101", "+0X1f", "-0B1", "-0o17", "1_0e5", "0o1_7", "1_", "1__0", "1_0.5e3", "0x1_F", "+1_0",
		"2001-1-2", "2026-1-5", "2001-01-02T3:4:5Z", "2001-1-2 3:04:05", "2001-1-2t3:4:5.5+01:00"} {
		rows = append(rows, struct {
			name   string
			source string
			want   []finding
		}{"the scalar " + scalar, "a: " + scalar + "\n", []finding{{"REG-YAML-SCALAR", 1, 4}}})
	}
	for _, row := range rows {
		got, diagnostics := readYAML(row.source)
		var findings []finding
		for _, diagnostic := range diagnostics {
			if diagnostic.Location.Document != DocumentYAML || diagnostic.Location.Pointer != "" {
				t.Errorf("%s: a location other than a line and column: %+v", row.name, diagnostic.Location)
			}
			findings = append(findings, finding{diagnostic.Code, diagnostic.Location.Line, diagnostic.Location.Column})
		}
		if got != "" || fmt.Sprint(findings) != fmt.Sprint(row.want) {
			t.Errorf("%s: got %q %v, want %v", row.name, got, findings, row.want)
		}
	}
	if got, diagnostics := readYAML(nested(32)); len(diagnostics) != 0 || !strings.HasPrefix(got, `{"a":[[`) {
		t.Errorf("depth 32: got %s %+v", got, diagnostics)
	}
}

func TestImportReportsEveryConstructItCanReadPast(t *testing.T) {
	_, report := importText("a: yes\nb: !t x\nc: &y 1\nd: on\ne:\n")
	var codes []string
	for _, diagnostic := range report.Diagnostics {
		codes = append(codes, fmt.Sprintf("%s@%d", diagnostic.Code, diagnostic.Location.Line))
	}
	if want := "[REG-YAML-SCALAR@1 REG-YAML-TAG@2 REG-YAML-ANCHOR@3 REG-YAML-SCALAR@4 REG-YAML-EMPTY@5]"; fmt.Sprint(codes) != want {
		t.Fatalf("got %v, want %s", codes, want)
	}
}

func TestImportAdmitsTheDocumentAsJSON(t *testing.T) {
	long := strings.Repeat("x", MaxKindBytes)
	for _, row := range []struct {
		name, source string
		line, column int
	}{
		{"no selector", "id: https://example.invalid/x\n", 1, 1},
		{"both selectors", "registryFormatVersion: \"1.0.0\"\nkindFormatVersion: \"1.0.0\"\n", 1, 1},
		{"another selector", "registryFormatVersion: 2.0.0\n", 1, 24},
		{"a number for a selector", "kindFormatVersion: 1.0\n", 1, 20},
		{"a root that is not a mapping", "- registryFormatVersion\n", 1, 1},
	} {
		document, report := importText(row.source)
		if document != nil || report.Valid || len(report.Diagnostics) != 1 {
			t.Fatalf("%s: got %s %+v", row.name, document, report)
		}
		if got := report.Diagnostics[0]; got.Code != "REG-DOC-SELECTOR" || got.Location != (Location{Document: DocumentYAML, Line: row.line, Column: row.column}) {
			t.Errorf("%s: got %+v", row.name, got)
		}
	}
	// A kind declaration past its own 256 KiB bound, in a file within the
	// import bound.
	_, report := importText("kindFormatVersion: \"1.0.0\"\ndescription: " + long + "\n")
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "REG-DOC-SIZE" || report.Diagnostics[0].Location.Line != 1 {
		t.Fatalf("kind over its bound: %+v", report.Diagnostics)
	}
	for _, source := range []Document{{Oversize: true}, {Data: []byte(strings.Repeat("#", MaxImportBytes+1))}} {
		if _, report := Import(source); len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "REG-DOC-SIZE" {
			t.Fatalf("oversize file: %+v", report.Diagnostics)
		}
	}
}

func TestImportWritesCanonicalOrder(t *testing.T) {
	head := "registryFormatVersion: \"1.0.0\"\nkinds: [{kind: b}, {kind: a}]\nentries:\n"
	one, _ := importText(head + "- {kind: b, id: x}\n- {kind: a, id: z}\n- {kind: a, id: w}\n")
	two, _ := importText(head + "- {kind: a, id: w}\n- {kind: b, id: x}\n- {kind: a, id: z}\n")
	want := `{"entries":[{"id":"w","kind":"a"},{"id":"z","kind":"a"},{"id":"x","kind":"b"}],"kinds":[{"kind":"a"},{"kind":"b"}],"registryFormatVersion":"1.0.0"}`
	if string(one) != want || string(two) != want {
		t.Fatalf("got\n%s\n%s\nwant %s", one, two, want)
	}
	// A list validation must report is left as written.
	kept, _ := importText("registryFormatVersion: \"1.0.0\"\nentries: [{kind: b, id: x}, {kind: a}]\n")
	if !strings.Contains(string(kept), `"entries":[{"id":"x","kind":"b"},{"kind":"a"}]`) {
		t.Fatalf("got %s", kept)
	}
}

func TestImportReportCarriesNoValue(t *testing.T) {
	values := []string{"example-billing-project", "secret:gcp-sm:projects/example-dev/secrets/db/versions/3", "hunter2-example", "on"}
	source := "registryFormatVersion: \"1.0.0\"\nproject: " + values[0] + "\npassword: " + values[1] + "\n" +
		values[2] + ": " + values[3] + "\nnote: &anchor-" + values[2] + " x\n"
	document, report := importText(source)
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if document != nil || report.Valid || len(report.Diagnostics) != 2 {
		t.Fatalf("want two refusals, got %s", encoded)
	}
	for _, value := range values[:3] {
		if strings.Contains(string(encoded), value) {
			t.Fatalf("the report carries %q: %s", value, encoded)
		}
	}
}

// A line holding only node properties is followed by the node on a later
// line. Lines of properties do not nest, so a file of nothing else, at the
// size bound, must not deepen the stack: it was a recursion, and 8 MiB of
// such lines overflowed it, which cannot be recovered. The refusal limit is
// lifted here so that every line is read.
func TestImportPropertiesAloneOnTheirLinesDoNotDeepenTheStack(t *testing.T) {
	for _, line := range []string{"!t\n", "&a\n"} {
		lines := MaxImportBytes / len(line)
		diagnostics := &collector{}
		text, ok := decodeYAMLText([]byte(strings.Repeat(line, lines)), diagnostics)
		if !ok {
			t.Fatal("not decoded")
		}
		parser := newYAMLParser(text, diagnostics)
		parser.refusalLimit = 0
		if root := parser.parse(); root == nil {
			t.Fatalf("%q: reading stopped", line)
		}
		// One refusal per line, and the empty node at the end.
		if got := len(diagnostics.diagnostics); got != lines+1 || diagnostics.diagnostics[got-1].Code != "REG-YAML-EMPTY" {
			t.Fatalf("%q: got %d refusals, want %d ending in REG-YAML-EMPTY", line, got, lines+1)
		}
	}
}

// allocatesWithin runs f and fails the test if it allocated more than budget
// bytes. Work is bounded by what it allocates, not by the clock, so a loaded
// machine cannot fail it: big arithmetic on a long literal allocates
// gigabytes (a million digits took 2.1 GB), a scan allocates a small multiple
// of the input.
func allocatesWithin(t *testing.T, budget uint64, name string, f func()) {
	t.Helper()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("%s: %d MiB allocated", name, allocated>>20)
	if allocated > budget {
		t.Errorf("%s: allocated %d MiB, budget %d MiB", name, allocated>>20, budget>>20)
	}
}

// A long number is held to §1 by a scan, not by big arithmetic, which is
// quadratic in its digits (8 MiB of them did not finish in minutes) and
// builds 10^N for an exponent N. Both readers share the check: JSON through
// Validate, YAML through Import. A long literal whose value is a double's
// shortest form, such as 1. and zeros, is admitted as before; any other is
// REG-DOC-NUMBER.
func TestAdmissionHoldsALongNumberQuickly(t *testing.T) {
	const longNumberBudget = 128 << 20 // the worst case measured 72 MiB
	// JSON's share: reading the document costs about twice its 8 MiB, 16 MiB
	// measured; 48 MiB while the decoder read it through the standard
	// decoder's tokens.
	const jsonLongNumberBudget = 32 << 20
	head, tail := `{"registryFormatVersion":"1.0.0","a":`, `}`
	room := MaxRegistryBytes - len(head) - len(tail) - 8
	numberCodes := func(diagnostics []Diagnostic) int {
		count := 0
		for _, diagnostic := range diagnostics {
			if diagnostic.Code == "REG-DOC-NUMBER" {
				count++
			}
		}
		return count
	}
	for _, row := range []struct {
		name, number string
		admitted     bool
	}{
		{"a decimal of 8 MiB of zeros, which is 1", "1." + strings.Repeat("0", room), true},
		{"a decimal of 8 MiB of zeros and a 1", "1." + strings.Repeat("0", room) + "1", false},
		{"a decimal of 8 MiB of digits", "1." + strings.Repeat("1", room), false},
		{"a decimal past 8 MiB of leading zeros", "0." + strings.Repeat("0", room) + "1", false},
		{"an integer of 8 MiB of digits", "1" + strings.Repeat("0", room), false},
		{"a huge exponent", "1e999999999", false},
		{"a huge negative exponent", "1e-999999999", false},
		{"zero with a huge exponent", "0e999999999", true},
		{"an exponent past any machine integer", "1e" + strings.Repeat("9", room), false},
		{"a long significand and a huge exponent", "1." + strings.Repeat("0", room/2) + "e999999999", false},
	} {
		allocatesWithin(t, jsonLongNumberBudget, "JSON "+row.name, func() {
			result := Validate([]byte(head+row.number+tail), nil)
			if got := numberCodes(result.Report.Diagnostics); (got == 0) != row.admitted || got > 1 {
				t.Errorf("JSON %s: %d REG-DOC-NUMBER, want admitted %v", row.name, got, row.admitted)
			}
		})
	}
	// big.Rat refuses an exponent past 10^6 itself, but one just under it
	// costs tens of milliseconds, and a document holds a million numbers.
	many := 5000
	allocatesWithin(t, jsonLongNumberBudget, "JSON many exponents under big.Rat's own cap", func() {
		// Every number is counted, so the report bound is lifted: the
		// time is the same either way, since every check runs.
		var result *Result
		withLimit(0, func() {
			result = Validate([]byte(head+"["+strings.Repeat("1e999999,", many/2)+strings.Repeat("1e-999999,", many/2-1)+"1e999999]"+tail), nil)
		})
		if got := numberCodes(result.Report.Diagnostics); got != many {
			t.Errorf("JSON many exponents: %d REG-DOC-NUMBER, want %d", got, many)
		}
	})
	yamlRoom := MaxImportBytes - 64
	selector := "kindFormatVersion: \"1.0.0\"\n"
	for _, row := range []struct{ name, source, code string }{
		{"a decimal of 8 MiB of zeros and a 1", "a: 1." + strings.Repeat("0", yamlRoom) + "1\n", "REG-DOC-NUMBER"},
		{"a decimal past 8 MiB of leading zeros", "a: 0." + strings.Repeat("0", yamlRoom) + "1\n", "REG-DOC-NUMBER"},
		{"an integer of 8 MiB of digits", "a: 1" + strings.Repeat("0", yamlRoom) + "\n", "REG-DOC-NUMBER"},
		// §7 has no exponent, so YAML refuses one as a scalar first.
		{"a huge exponent", "a: 1e999999999\n", "REG-YAML-SCALAR"},
		{"a huge negative exponent", "a: 1.0e-999999999\n", "REG-YAML-SCALAR"},
	} {
		allocatesWithin(t, longNumberBudget, "YAML "+row.name, func() {
			_, report := importText(selector + row.source)
			if report.Valid || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != row.code {
				t.Errorf("YAML %s: got %d diagnostics, want one %s", row.name, len(report.Diagnostics), row.code)
			}
		})
	}
	allocatesWithin(t, longNumberBudget, "YAML a decimal of 8 MiB of zeros", func() {
		document, report := importText(selector + "a: 1." + strings.Repeat("0", yamlRoom) + "\n")
		if want := `{"a":1,"kindFormatVersion":"1.0.0"}`; !report.Valid || string(document) != want {
			t.Errorf("YAML 1. and 8 MiB of zeros: got %s %+v, want %s", document, report.Diagnostics, want)
		}
	})
}

// exactlyAdmitted is RFC 0034 §1's rule by big arithmetic alone, as
// admittedNumber was before its bounds: the reference its bounds must agree
// with on every JSON number short enough to compute.
func exactlyAdmitted(literal string) bool {
	exact, ok := new(big.Rat).SetString(literal)
	if !ok {
		return false
	}
	if exact.Sign() == 0 && strings.HasPrefix(literal, "-") {
		return false
	}
	if !strings.ContainsAny(literal, ".eE") {
		return new(big.Rat).Abs(exact).Cmp(big.NewRat(maxSafeInteger, 1)) <= 0
	}
	double, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		return false
	}
	shortest, ok := new(big.Rat).SetString(strconv.FormatFloat(double, 'g', -1, 64))
	return ok && shortest.Cmp(exact) == 0
}

// exactlyAdmittedLiteral is the literal admission keeps under the exact rule:
// an integer as written, zero as 0, and any other number as the RFC 8785 form
// of its value, laid out here by string operations from the double's
// shortest digits rather than by jcs.AppendNumber.
func exactlyAdmittedLiteral(literal string) (string, bool) {
	if !exactlyAdmitted(literal) {
		return "", false
	}
	if !strings.ContainsAny(literal, ".eE") {
		return literal, true
	}
	double, _ := strconv.ParseFloat(literal, 64)
	if double == 0 {
		return "0", true
	}
	sign := ""
	if double < 0 {
		sign, double = "-", -double
	}
	mantissa, exponentText, _ := strings.Cut(strconv.FormatFloat(double, 'e', -1, 64), "e")
	digits := strings.Replace(mantissa, ".", "", 1)
	exponent, _ := strconv.Atoi(exponentText)
	k, n := len(digits), exponent+1
	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k), true
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:], true
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits, true
	}
	form := sign + digits[:1]
	if k > 1 {
		form += "." + digits[1:]
	}
	if n-1 >= 0 {
		form += "e+"
	} else {
		form += "e"
	}
	return form + strconv.Itoa(n-1), true
}

// The bounds refuse only what the exact rule refuses: at the edges of the
// doubles, at the 17-digit and 2^53 boundaries, and on literals built around
// many doubles' shortest forms. What they admit is kept as the exact rule's
// literal, byte for byte.
func TestAdmittedNumberBoundsKeepTheExactRule(t *testing.T) {
	literals := []string{
		"0", "-0", "0.0", "-0.0", "0e5", "-0E-5", "0.000e999", "1", "-1", "100", "1.50", "0.1", "1e-7", "1E+2", "1e0",
		"9007199254740991", "-9007199254740991", "9007199254740992", "-9007199254740992", "12345678901234567", "1000000000000000",
		"0.10000000000000001", "0.30000000000000004", "1.0000000000000000000000001", "123456789012345678e-2", "12345678901234567e-2",
		"5e-324", "4.9406564584124654e-324", "2e-324", "3e-324", "1e-324", "1e-325", "0.5e-323", "50e-325", "0.0000005e-317",
		"1.7976931348623157e308", "1.7976931348623158e308", "1.7976931348623159e308", "1e308", "1e309", "0.1e309", "10e307",
		"17976931348623157e292", "179769313486231570000e288", "9.999999999999999e307", "2.2250738585072014e-308",
	}
	random := rand.New(rand.NewPCG(94, 34))
	for i := 0; i < 4000; i++ {
		double := math.Float64frombits(random.Uint64())
		if math.IsNaN(double) || math.IsInf(double, 0) {
			continue
		}
		shortest := strconv.FormatFloat(double, 'e', -1, 64)
		mantissa, exponent, _ := strings.Cut(shortest, "e")
		power, _ := strconv.Atoi(exponent)
		literals = append(literals, shortest, strconv.FormatFloat(double, 'g', -1, 64),
			// The same value with zeros added, or written with the point moved.
			mantissa+"000e"+exponent, "0.00"+strings.Replace(mantissa, ".", "", 1)+"e"+strconv.Itoa(power+3),
			// A digit more, which no longer reads back as the same text.
			mantissa+"1e"+exponent, strconv.FormatFloat(double, 'e', 17, 64), strconv.FormatFloat(double, 'e', 16, 64))
	}
	kept := 0
	for _, literal := range literals {
		if got, want := admittedNumber(json.Number(literal)), exactlyAdmitted(literal); got != want {
			t.Errorf("%s: admitted %v, the exact rule says %v", literal, got, want)
		}
		got, ok := admittedLiteral(json.Number(literal))
		want, wantOK := exactlyAdmittedLiteral(literal)
		if ok != wantOK || ok && string(got) != want {
			t.Errorf("%s: kept %q %v, the exact rule keeps %q %v", literal, got, ok, want, wantOK)
		}
		if ok && string(got) != literal {
			kept++
		}
	}
	// Some admitted literals are rewritten, so the comparison is of more than
	// the text as written.
	if kept < 1000 {
		t.Errorf("only %d admitted literals were rewritten", kept)
	}
	// Outside JSON's grammar, nothing is admitted.
	for _, literal := range []string{"", "-", "+5", "01", "-01", "1.", ".5", "1e", "1e+", "0x10", "1/2", "Infinity", "NaN", "1 ", " 1", "1_0", "1.5.0"} {
		if admittedNumber(json.Number(literal)) {
			t.Errorf("%q is not a JSON number but was admitted", literal)
		}
	}
}

// One import reports at most maxImportRefusals refusals and then one
// REG-YAML-TRUNCATED where reading stopped: 8 MiB of aliases would
// otherwise give millions, and a report larger than the file.
func TestImportStopsAfterTheRefusalLimit(t *testing.T) {
	if !strings.Contains(Codes["REG-YAML-TRUNCATED"].message, strconv.Itoa(maxImportRefusals)) {
		t.Fatalf("the message does not name the limit, %d", maxImportRefusals)
	}
	for _, row := range []struct{ name, source, code string }{
		{"aliases in a flow sequence", "[" + strings.Repeat("*a,", MaxImportBytes/3-1) + "]", "REG-YAML-ANCHOR"},
		{"tags in a block sequence", strings.Repeat("- !t a\n", MaxImportBytes/7), "REG-YAML-TAG"},
		{"properties alone on their lines", strings.Repeat("!t\n", MaxImportBytes/3), "REG-YAML-TAG"},
	} {
		document, report := importText(row.source)
		diagnostics := report.Diagnostics
		if document != nil || report.Valid || len(diagnostics) != maxImportRefusals+1 {
			t.Fatalf("%s: got %d diagnostics, want %d", row.name, len(diagnostics), maxImportRefusals+1)
		}
		for _, diagnostic := range diagnostics[:maxImportRefusals] {
			if diagnostic.Code != row.code {
				t.Fatalf("%s: got %s before the limit, want %s", row.name, diagnostic.Code, row.code)
			}
		}
		if last := diagnostics[maxImportRefusals]; last.Code != "REG-YAML-TRUNCATED" || last.Location.Line < 1 || last.Location.Column < 1 {
			t.Fatalf("%s: last %+v, want REG-YAML-TRUNCATED with a line and column", row.name, last)
		}
	}
	// At the limit, nothing is truncated.
	_, report := importText("[" + strings.Repeat("*a,", maxImportRefusals-1) + "*a]")
	if len(report.Diagnostics) != maxImportRefusals || report.Diagnostics[maxImportRefusals-1].Code != "REG-YAML-ANCHOR" {
		t.Fatalf("at the limit: got %d diagnostics", len(report.Diagnostics))
	}
}

// FuzzImport holds import's invariants on any input: no panic, the same
// result twice, locations by line and column only, and bytes written only
// when they pass step 1's admission as the document their selector names.
func FuzzImport(f *testing.F) {
	for _, seed := range []string{
		registryYAML, roleKindYAML, databaseKindYAML,
		"a: 1\nb: -2\nc: 1.50\nd: true\ne: false\nf: null\ng: text\n",
		"a: |\n  x\n\n  y\nb: >-\n  z\n", "a: [1, \"x\", {b: c}, e: f]\n", "a: {\"b\":[0]}\n",
		"--- [1, 2]\n...\n", "a: !t &x *y\n", "? a\n: b\n", "a: 0X1F\nb: 2001-1-2\n", "a: |\n  ",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}
		first, report := Import(Document{Data: data})
		second, again := Import(Document{Data: data})
		if string(first) != string(second) || fmt.Sprint(report) != fmt.Sprint(again) {
			t.Fatal("two imports of one file differ")
		}
		if report.Valid != (first != nil) {
			t.Fatalf("valid %v with %d bytes", report.Valid, len(first))
		}
		if len(report.Diagnostics) > maxImportRefusals+1 {
			t.Fatalf("%d diagnostics", len(report.Diagnostics))
		}
		for _, diagnostic := range report.Diagnostics {
			at := diagnostic.Location
			if at.Document != DocumentYAML || at.Pointer != "" || at.Kind != "" || at.Entry != nil || at.Environment != "" || at.KindDocument != "" {
				t.Fatalf("a location with more than a line and column: %+v", at)
			}
			if diagnostic.Code != "REG-DOC-SIZE" && (at.Line < 1 || at.Column < 1) {
				t.Fatalf("no line and column: %+v", diagnostic)
			}
		}
		if first != nil {
			registry := admit(first, MaxRegistryBytes, "registryFormatVersion", RegistryFormatVersion, Location{}, &collector{})
			kind := admit(first, MaxKindBytes, "kindFormatVersion", KindFormatVersion, Location{}, &collector{})
			if registry == nil && kind == nil {
				t.Fatalf("bytes written that admission refuses: %s", first)
			}
		}
	})
}
