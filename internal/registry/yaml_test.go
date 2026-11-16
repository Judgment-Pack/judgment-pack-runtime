package registry

import (
	"encoding/json"
	"fmt"
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
		{"strings that only look numeric", "v: 1.2.0\nu: https://example.invalid/x\nt: a:b\ns: 1.5.6\nr: 12:3a\nq: 2026-1-5\n",
			`{"q":"2026-1-5","r":"12:3a","s":"1.5.6","t":"a:b","u":"https://example.invalid/x","v":"1.2.0"}`},
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
	}
	for _, scalar := range []string{"yes", "no", "on", "off", "y", "N", "~", "True", "NULL", "0o17", "0x1F", "1e3", "1.5e-3", ".inf", "-.Inf", ".nan",
		"007", "+5", "1_000", "12:30", ".5", "5.", "2026-11-16", "2026-11-16 10:00:00", "2026-11-16T10:00:00Z", "=", "<<"} {
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
