package federation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
)

func decodeBound(t testing.TB, text string) bound {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader([]byte(text)))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func readTestBounds(t testing.TB, text string, standard bool) (*boundsDocument, []string) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader([]byte(text)))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		t.Fatal(err)
	}
	s := &source{name: "billing", role: RoleRepository, serves: []string{"dev", "prod"}}
	if standard {
		s.name, s.role = "standard", RoleStandard
	}
	collected := &collector{}
	b := readBounds(root, s, []string{"dev", "prod"}, collected)
	return b, found(collected.kept)
}

// TestWidenings holds §6.2's rows, one comparison each, R against S.
func TestWidenings(t *testing.T) {
	for _, c := range []struct {
		r, s string
		want []string
	}{
		{`{"type":"integer"}`, `{"type":"number"}`, nil},
		{`{"type":["integer","string"]}`, `{"type":"integer"}`, []string{"type"}},
		{`{"type":"number"}`, `{"type":"integer"}`, []string{"type"}},
		{`{"enum":["read"]}`, `{"enum":["read","write"]}`, nil},
		{`{"enum":["read","admin"]}`, `{"enum":["read","write"]}`, []string{"enum"}},
		{`{"const":"write"}`, `{"enum":["read"]}`, []string{"const"}},
		{`{"enum":[1.0]}`, `{"const":1}`, nil},
		// Every value R names satisfies S's whole bound, a set or not.
		{`{"enum":[5]}`, `{"maximum":3}`, []string{"enum"}},
		{`{"enum":[2]}`, `{"maximum":3}`, nil},
		{`{"const":"ab"}`, `{"patterns":["^a$"]}`, []string{"const"}},
		// Upper bounds: tighter, equal, looser, inclusive and exclusive.
		{`{"maximum":50}`, `{"maximum":100}`, nil},
		{`{"maximum":100}`, `{"maximum":100}`, nil},
		{`{"maximum":150}`, `{"maximum":100}`, []string{"maximum"}},
		{`{"exclusiveMaximum":100}`, `{"maximum":100}`, nil},
		{`{"maximum":100}`, `{"exclusiveMaximum":100}`, []string{"maximum"}},
		{`{"exclusiveMaximum":11}`, `{"maximum":10}`, []string{"exclusiveMaximum"}},
		{`{"maximum":10,"exclusiveMaximum":10}`, `{"exclusiveMaximum":10}`, nil},
		{`{"maximum":0.30000000000000004}`, `{"maximum":0.3}`, []string{"maximum"}},
		// Lower bounds.
		{`{"minimum":5}`, `{"minimum":1}`, nil},
		{`{"minimum":1}`, `{"minimum":1}`, nil},
		{`{"minimum":0}`, `{"minimum":1}`, []string{"minimum"}},
		{`{"exclusiveMinimum":1}`, `{"minimum":1}`, nil},
		{`{"minimum":1}`, `{"exclusiveMinimum":1}`, []string{"minimum"}},
		// Counts.
		{`{"maxLength":8,"minLength":2}`, `{"maxLength":8,"minLength":2}`, nil},
		{`{"maxLength":9}`, `{"maxLength":8}`, []string{"maxLength"}},
		{`{"minLength":1}`, `{"minLength":2}`, []string{"minLength"}},
		{`{"maxItems":3}`, `{"maxItems":2}`, []string{"maxItems"}},
		{`{"minItems":0}`, `{"minItems":1}`, []string{"minItems"}},
		{`{"maxProperties":5}`, `{"maxProperties":4}`, []string{"maxProperties"}},
		{`{"minProperties":1}`, `{"minProperties":2}`, []string{"minProperties"}},
		// Uniqueness.
		{`{"uniqueItems":true}`, `{"uniqueItems":true}`, nil},
		{`{"uniqueItems":false}`, `{"uniqueItems":true}`, []string{"uniqueItems"}},
		{`{"uniqueItems":false}`, `{"uniqueItems":false}`, nil},
		// A row only one side states is added or inherited.
		{`{"maximum":5}`, `{}`, nil},
		{`{}`, `{"maximum":5}`, nil},
		{`{"required":["a"],"patterns":["^a"]}`, `{"required":["b"],"patterns":["^b"]}`, nil},
	} {
		got := widenings(decodeBound(t, c.r), decodeBound(t, c.s))
		if !slices.Equal(got, c.want) {
			t.Errorf("R %s against S %s: %q, want %q", c.r, c.s, got, c.want)
		}
	}
}

// TestEffectiveBound holds §6.4: an environment's row replaces the field's,
// a row of a group as one, and required and patterns are added.
func TestEffectiveBound(t *testing.T) {
	b, got := readTestBounds(t, `{"boundsFormatVersion":"1.1.0","kinds":[{"kind":"db-role","fields":[{"pointer":"/limit",
		"bound":{"type":"integer","minimum":1,"maximum":200,"required":["a"],"patterns":["^a"]},
		"environments":{"prod":{"exclusiveMaximum":100,"required":["b"],"patterns":["^b"]}}}]}]}`, true)
	expectFound(t, got)
	f := b.kinds[0].fields[0]
	effective, fromEnv := f.effective("prod")
	raw, _ := json.Marshal(effective)
	if string(raw) != `{"exclusiveMaximum":100,"minimum":1,"patterns":["^a","^b"],"required":["a","b"],"type":"integer"}` {
		t.Errorf("prod: %s", raw)
	}
	if !fromEnv["exclusiveMaximum"] || fromEnv["minimum"] || fromEnv["type"] {
		t.Errorf("prod: from the environment %v", fromEnv)
	}
	effective, _ = f.effective("dev")
	raw, _ = json.Marshal(effective)
	if string(raw) != `{"maximum":200,"minimum":1,"patterns":["^a"],"required":["a"],"type":"integer"}` {
		t.Errorf("dev: %s", raw)
	}
	if !f.admits("prod", json.Number("99")) || f.admits("prod", json.Number("100")) || !f.admits("dev", json.Number("200")) {
		t.Error("the effective bounds admit the wrong values")
	}
}

func TestBoundsCounts(t *testing.T) {
	values := func(n int) string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("%q", fmt.Sprintf("v%04d", i))
		}
		return "[" + join(out) + "]"
	}
	patterns := func(n int) string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("%q", fmt.Sprintf("^p%02d", i))
		}
		return "[" + join(out) + "]"
	}
	document := func(member, bound string) string {
		return `{"boundsFormatVersion":"1.1.0","kinds":[{"kind":"db-role"` + member + `,"fields":[{"pointer":"/a","bound":` + bound + `}]}]}`
	}
	for _, c := range []struct {
		text string
		want []string
	}{
		{document("", `{"enum":`+values(MaxBoundEnum)+`}`), nil},
		{document("", `{"enum":`+values(MaxBoundEnum+1)+`}`), []string{"FED-BOUND-FORM bounds billing /kinds/0/fields/0/bound/enum"}},
		{document("", `{"patterns":`+patterns(MaxBoundPatterns)+`}`), nil},
		{document("", `{"patterns":`+patterns(MaxBoundPatterns+1)+`}`), []string{"FED-BOUND-FORM bounds billing /kinds/0/fields/0/bound/patterns"}},
		{document(`,"maxEntries":100000`, `{}`), nil},
		{document(`,"maxEntries":100001`, `{}`), []string{"FED-BOUND-FORM bounds billing /kinds/0/maxEntries"}},
		{document(`,"maxEntries":-1`, `{}`), []string{"FED-BOUND-FORM bounds billing /kinds/0/maxEntries"}},
		{document(`,"maxEntries":1.5`, `{}`), []string{"FED-BOUND-FORM bounds billing /kinds/0/maxEntries"}},
		{document("", `{"maxLength":9007199254740991}`), nil},
		{document("", `{"maxLength":9007199254740992}`), []string{"FED-BOUND-FORM bounds billing /kinds/0/fields/0/bound/maxLength"}},
	} {
		_, got := readTestBounds(t, c.text, false)
		expectFound(t, got, c.want...)
	}
}

func TestBoundsForm(t *testing.T) {
	for _, c := range []struct {
		text     string
		standard bool
		want     []string
	}{
		{`{"boundsFormatVersion":"1.1.0","kinds":[{"kind":"b"},{"kind":"a"}]}`, false, []string{"FED-BOUND-FORM bounds billing /kinds/1"}},
		{`{"boundsFormatVersion":"1.1.0","kinds":[{"kind":"a"},{"kind":"a"}]}`, false, []string{"FED-BOUND-FORM bounds billing /kinds/1"}},
		{`{"boundsFormatVersion":"1.1.0","kinds":[{"kind":"a","fields":[{"pointer":"/b","bound":{}},{"pointer":"/a","bound":{}}]}]}`, false, []string{"FED-BOUND-FORM bounds billing /kinds/0/fields/1"}},
		{`{"boundsFormatVersion":"1.1.0","kinds":[{"kind":"a","maxAssemblyEntries":4}]}`, false, []string{"FED-BOUND-FORM bounds billing /kinds/0/maxAssemblyEntries"}},
		{`{"boundsFormatVersion":"1.1.0","kinds":[{"kind":"a","maxAssemblyEntries":4}]}`, true, nil},
		{`{"boundsFormatVersion":"1.1.0","kinds":[{"kind":"a","fields":[{"pointer":"/a","bound":{"anyOf":[]}}]}]}`, false, []string{"FED-BOUND-NOT-COMPARABLE bounds billing /kinds/0/fields/0/bound/anyOf"}},
		{`{"boundsFormatVersion":"1.1.0","kinds":[{"kind":"a","fields":[{"pointer":"/a","bound":{"oneOf":[]}}]}]}`, false, []string{"FED-BOUND-FORM bounds billing /kinds/0/fields/0/bound/oneOf"}},
		{`{"boundsFormatVersion":"1.1.0","kinds":[{"kind":"a","fields":[{"pointer":"/a","bound":{},"environments":{"stage":{}}}]}]}`, false, []string{"FED-BOUND-ENVIRONMENT bounds billing /kinds/0/fields/0/environments/stage"}},
		{`{"boundsFormatVersion":"1.1.0","kinds":[{"kind":"a","fields":[{"pointer":"a","bound":{}}]}]}`, false, []string{"FED-BOUND-FORM bounds billing /kinds/0/fields/0/pointer"}},
		{`{"boundsFormatVersion":"1.1.0","kinds":{}}`, false, []string{"FED-BOUND-FORM bounds billing /kinds"}},
	} {
		_, got := readTestBounds(t, c.text, c.standard)
		expectFound(t, got, c.want...)
	}
}

// A bound that does not compile as a schema admits nothing: a defect in
// reading one never lets a value past it.
func TestUncompiledBoundAdmitsNothing(t *testing.T) {
	if (&boundSchema{}).admits(json.Number("1")) {
		t.Error("a bound with no schema admitted a value")
	}
	if compileBound(bound{"type": "no-such-type"}).admits("x") {
		t.Error("a bound that does not compile admitted a value")
	}
}

func join(items []string) string {
	var buffer bytes.Buffer
	for i, item := range items {
		if i > 0 {
			buffer.WriteByte(',')
		}
		buffer.WriteString(item)
	}
	return buffer.String()
}
