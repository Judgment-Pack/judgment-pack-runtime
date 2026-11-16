package terraformfile

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

func selectionPlan(t *testing.T) []byte {
	return plan(t,
		change{address: "synthetic_db.main", actions: []string{"update"}},
		change{address: "synthetic_db.old", actions: []string{"delete"}},
		change{address: "synthetic_db.swap", actions: []string{"delete", "create"}},
		change{address: "synthetic_db.fresh", actions: []string{"create"}},
		change{address: "synthetic_db.same", actions: []string{"no-op"}},
		change{address: "data.synthetic_db.lookup", mode: "data", actions: []string{"read"}},
		change{address: "synthetic_db.counted[0]", index: 0, actions: []string{"create"}},
		change{address: "synthetic_db.counted[1]", index: 1, actions: []string{"create"}},
		change{address: `synthetic_db.keyed["blue"]`, index: "blue", actions: []string{"create"}},
		change{address: `synthetic_db.keyed["a.b"]`, index: "a.b", actions: []string{"create"}},
		change{address: "module.app.synthetic_db.main", actions: []string{"update"}},
		change{address: "module.app.module.inner.synthetic_db.main", actions: []string{"create"}},
		change{address: "synthetic_user.admin", actions: []string{"create"}},
	)
}

func TestSelection(t *testing.T) {
	data := selectionPlan(t)
	cases := []struct {
		name string
		sel  Select
		want []string
	}{
		{"by type", Select{Types: []string{"synthetic_user"}}, []string{"synthetic_user.admin"}},
		{"two types, managed by default", Select{Types: []string{"synthetic_user", "synthetic_db"}}, []string{
			"module.app.module.inner.synthetic_db.main", "module.app.synthetic_db.main",
			"synthetic_db.counted[0]", "synthetic_db.counted[1]", "synthetic_db.fresh", `synthetic_db.keyed["a.b"]`, `synthetic_db.keyed["blue"]`,
			"synthetic_db.main", "synthetic_db.old", "synthetic_db.same", "synthetic_db.swap", "synthetic_user.admin"}},
		{"data sources", Select{Types: []string{"synthetic_db"}, Mode: ModeData}, []string{"data.synthetic_db.lookup"}},
		{"a type selects nothing", Select{Types: []string{"synthetic_none"}}, []string{}},
		{"the root module only", Select{Types: []string{"synthetic_db"}, Address: ptr("synthetic_db.ma*")}, []string{"synthetic_db.main"}},
		{"one module down", Select{Types: []string{"synthetic_db"}, Address: ptr("module.*.synthetic_db.*")}, []string{"module.app.synthetic_db.main"}},
		{"two modules down", Select{Types: []string{"synthetic_db"}, Address: ptr("module.*.module.*.*.*")}, []string{"module.app.module.inner.synthetic_db.main"}},
		{"any index", Select{Types: []string{"synthetic_db"}, Address: ptr("synthetic_db.*[*]")}, []string{"synthetic_db.counted[0]", "synthetic_db.counted[1]", `synthetic_db.keyed["blue"]`}},
		{"a key with a dot", Select{Types: []string{"synthetic_db"}, Address: ptr("synthetic_db.keyed[*.*]")}, []string{`synthetic_db.keyed["a.b"]`}},
		{"a literal", Select{Types: []string{"synthetic_db"}, Address: ptr(`synthetic_db.keyed["blue"]`)}, []string{`synthetic_db.keyed["blue"]`}},
		{"a star stays in its segment", Select{Types: []string{"synthetic_db"}, Address: ptr("*")}, []string{}},
		{"a whole-address match", Select{Types: []string{"synthetic_db"}, Address: ptr("synthetic_db.mai")}, []string{}},
		{"the empty pattern", Select{Types: []string{"synthetic_db"}, Address: ptr("")}, []string{}},
		{"deletes, a replacement included", Select{Types: []string{"synthetic_db"}, Actions: []string{"delete"}}, []string{"synthetic_db.old", "synthetic_db.swap"}},
		{"no-op", Select{Types: []string{"synthetic_db"}, Actions: []string{"no-op"}}, []string{"synthetic_db.same"}},
		{"read, of a data source", Select{Types: []string{"synthetic_db"}, Mode: ModeData, Actions: []string{"read"}}, []string{"data.synthetic_db.lookup"}},
		{"an empty actions array", Select{Types: []string{"synthetic_db"}, Actions: []string{}}, []string{}},
		{"everything at once", Select{Types: []string{"synthetic_db"}, Address: ptr("*.*.synthetic_db.*"), Actions: []string{"update"}}, []string{"module.app.synthetic_db.main"}},
		{"max counts the selected only", Select{Types: []string{"synthetic_user"}, Max: 1}, []string{"synthetic_user.admin"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			acquisition := mustAccept(t, data, Source{Document: DocumentPlan, Select: c.sel})
			if got := addresses(acquisition); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("selected %q, want %q", got, c.want)
			}
			if acquisition.Selected != len(c.want) {
				t.Fatalf("selected %d", acquisition.Selected)
			}
		})
	}
}

func TestTheSelectionMaxIsABound(t *testing.T) {
	many := func(n int) []byte {
		changes := make([]change, n)
		for i := range changes {
			changes[i] = change{address: "synthetic_db.r" + strconv.Itoa(i)}
		}
		return plan(t, changes...)
	}
	atDefault := many(DefaultSelectMax)
	if got := mustAccept(t, atDefault, planSource("synthetic_db")); got.Selected != DefaultSelectMax {
		t.Fatalf("selected %d", got.Selected)
	}
	mustRefuse(t, many(DefaultSelectMax+1), planSource("synthetic_db"), CodeSelectBound)
	three := many(3)
	mustAccept(t, three, Source{Document: DocumentPlan, Select: Select{Types: []string{"synthetic_db"}, Max: 3}})
	mustRefuse(t, three, Source{Document: DocumentPlan, Select: Select{Types: []string{"synthetic_db"}, Max: 2}}, CodeSelectBound)
	mustRefuse(t, three, Source{Document: DocumentPlan, Select: Select{Types: []string{"synthetic_db"}, Max: 1}}, CodeSelectBound)
}

func TestTheOrderIsByAddressAndStable(t *testing.T) {
	// Code-point order, which is not UTF-16 order: U+FFFD sorts before
	// U+1F600 here, and after it among member names.
	data := plan(t,
		change{address: "synthetic_db.b", after: object("n", 1)},
		change{address: "synthetic_db.\U0001F600", after: object("n", 2)},
		change{address: "synthetic_db.A", after: object("n", 3)},
		change{address: "synthetic_db.�", after: object("n", 4)},
		change{address: "synthetic_db.é", after: object("n", 5)},
		change{address: "synthetic_db.a", after: object("n", 6)},
		// Deposed objects share their address, and keep the file's order.
		change{address: "synthetic_db.dup", after: object("n", "first"), deposed: "00000001"},
		change{address: "synthetic_db.dup", after: object("n", "second")},
		change{address: "synthetic_db.dup", after: object("n", "third"), deposed: "00000002"},
	)
	acquisition := mustAccept(t, data, planSource("synthetic_db"))
	want := []string{"synthetic_db.A", "synthetic_db.a", "synthetic_db.b", "synthetic_db.dup", "synthetic_db.dup", "synthetic_db.dup", "synthetic_db.é", "synthetic_db.�", "synthetic_db.\U0001F600"}
	if got := addresses(acquisition); !reflect.DeepEqual(got, want) {
		t.Fatalf("order %q", got)
	}
	got := resources(t, acquisition)
	for i, label := range []string{"first", "second", "third"} {
		if n := got[3+i]["after"].(map[string]any)["n"]; n != label {
			t.Fatalf("deposed order: %d is %v", i, n)
		}
	}
	// The same bytes read twice give the same value.
	again := mustAccept(t, data, planSource("synthetic_db"))
	if !bytes.Equal(again.Value(), acquisition.Value()) {
		t.Fatal("two reads differ")
	}
}

func TestTheValueIsCanonical(t *testing.T) {
	after := json.RawMessage(`{"b":1,"a":{"z":[true,false,null],"y":"x"},"` + uEscape("ffff") + `":0,"` +
		uEscape("d83d") + uEscape("de00") + `":0,"aa":"q` + uEscape("0001") + `\n\"\\\/` + uEscape("2028") + uEscape("00e9") + `","é":-0.0}`)
	data := plan(t, change{address: "synthetic_db.main", after: after})
	acquisition := mustAccept(t, data, planSource("synthetic_db"))
	gotAfter := string(acquisition.Resources[0].JSON)
	// The control character keeps its escape; U+2028 and é are written as
	// themselves; members are in UTF-16 order, so U+1F600 precedes U+FFFF.
	want := `"after":{"a":{"y":"x","z":[true,false,null]},"aa":"q` + uEscape("0001") + `\n\"\\/` + "\u2028\u00e9" +
		`","b":"1","é":"0","` + "\U0001F600" + `":"0","` + "\uffff" + `":"0"}`
	if !bytes.Contains([]byte(gotAfter), []byte(want)) {
		t.Fatalf("value %s\nwant it to hold %s", gotAfter, want)
	}
	crossCheck(t, acquisition.Value())
}

// crossCheck holds a value to the runtime's own RFC 8785 encoder: decoded and
// encoded again, it is the same bytes.
func crossCheck(t testing.TB, value []byte) {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(value, &decoded); err != nil {
		t.Fatalf("value %s: %v", value, err)
	}
	encoded, err := jcs.EncodeDocument(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, value) {
		t.Fatalf("not canonical:\n got %s\njcs  %s", value, encoded)
	}
}

func TestNumberNormalForm(t *testing.T) {
	long64 := "1234567890123456789012345678901234567890123456789012345678901234"
	cases := map[string]string{
		"0": "0", "-0": "0", "-0.0e5": "0", "0e1000000": "0", "0.000e-99999999999999999999": "0",
		"1": "1", "-1": "-1", "1.50": "1.5", "1e2": "100", "1E+2": "100", "10.0": "10", "120": "120",
		"1e-7": "0.0000001", "-12.340": "-12.34", "0.001": "0.001", "123e-2": "1.23", "5e-1": "0.5",
		"0.5": "0.5", "-0.05": "-0.05", "1.2345e2": "123.45", "12345e-5": "0.12345",
		"1e63": "1" + repeat("0", 63), "-1e62": "-1" + repeat("0", 62), "1e-62": "0." + repeat("0", 61) + "1",
		long64: long64, "0." + long64[2:]: "0." + long64[2:],
		"1e64": "", "-1e63": "", "1e-63": "", "1e1000000": "", "1e-1000000": "", "1" + long64: "",
		"1e99999999999999999999999": "", "-1e-99999999999999999999999": "", "1." + long64: "",
	}
	for literal, want := range cases {
		got, ok := normalNumber([]byte(literal))
		if want == "" {
			if ok {
				t.Errorf("%s: %q, want refused", literal, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("%s: %q %v, want %q", literal, got, ok, want)
		}
		if len(got) > MaxNumberChars {
			t.Errorf("%s: %d characters", literal, len(got))
		}
	}
}

func repeat(s string, n int) string { return string(bytes.Repeat([]byte(s), n)) }

func TestTF_NUMBERChecksTheAcquiredValueOnly(t *testing.T) {
	mustAccept(t, plan(t, change{address: "synthetic_db.main", after: object("n", json.Number("1e63"))}), planSource("synthetic_db"))
	mustRefuse(t, plan(t, change{address: "synthetic_db.main", after: object("n", json.Number("1e64"))}), planSource("synthetic_db"), CodeNumber)
	mustRefuse(t, plan(t, change{address: "synthetic_db.main", before: object("n", json.Number("-1e-63"))}), planSource("synthetic_db"), CodeNumber)
	mustRefuse(t, plan(t, change{address: "synthetic_db.main", index: json.Number("1e64")}), planSource("synthetic_db"), CodeNumber)
	mustRefuse(t, state(t, module{resources: []stateResource{{address: "synthetic_db.main", values: object("n", json.Number("1e1000000"))}}}), stateSource("synthetic_db"), CodeNumber)
	// Not in the acquired value: a removed position, an unselected resource,
	// and a section the reader never reads.
	mustAccept(t, plan(t,
		change{address: "synthetic_db.main", after: object("n", json.Number("1e1000000"), "m", 1), afterSens: object("n", true)},
		change{address: "synthetic_other.x", after: object("n", json.Number("1e1000000"))},
	), planSource("synthetic_db"))
	doc := planWith([]any{})
	doc["configuration"] = object("n", json.Number("1e1000000"))
	mustAccept(t, encode(t, doc), planSource("synthetic_db"))
}

func TestPatternMatcher(t *testing.T) {
	cases := []struct {
		pattern, address string
		want             bool
	}{
		{"a.b", "a.b", true}, {"a.b", "a.bc", false}, {"a.*", "a.", true}, {"a.*", "a.b.c", false},
		{"*", "", true}, {"*", "abc", true}, {"*", "a[0]", false}, {"*[*]", "a[0]", true}, {"*[*]", "a[]", true},
		{"a*b*c", "abc", true}, {"a*b*c", "aXbYc", true}, {"a*b*c", "acb", false}, {"a*a", "a", false}, {"a*a", "aa", true},
		{"*.*", "a.b.c", false}, {"*.*.*", "a.b.c", true}, {"a[*]", "a[\"x.y\"]", false}, {"a[*.*]", "a[\"x.y\"]", true},
		{"é*", "éé", true}, {"x*y", "x\U0001F600y", true}, {"a]", "a]", true}, {"a]", "a[", false},
	}
	for _, c := range cases {
		if got := compilePattern(c.pattern).match(c.address); got != c.want {
			t.Errorf("%q against %q: %v", c.pattern, c.address, got)
		}
	}
}
