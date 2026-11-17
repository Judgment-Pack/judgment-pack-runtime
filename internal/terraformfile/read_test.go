package terraformfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestAPlanReadsAsItsCanonicalAcquiredValue(t *testing.T) {
	data := plan(t,
		change{address: "synthetic_db.main", actions: []string{"update"},
			before:     object("size", 2, "password", "SECRET-OLD", "labels", object("team", "a")),
			after:      object("size", 3.50, "password", "SECRET-NEW", "labels", object("team", "b")),
			beforeSens: object("password", true), afterSens: object("password", true),
			afterUnknown: object("endpoint", true)},
		change{address: "synthetic_db.audit", actions: []string{"create"}, before: nil,
			after: object("size", 1e2), afterUnknown: object("id", true)},
	)
	acquisition := mustAccept(t, data, planSource("synthetic_db"))
	want := `[{"actions":["create"],"address":"synthetic_db.audit","after":{"size":"100"},"afterUnknown":["/id"],"before":null,"mode":"managed","name":"audit","providerName":"example.test/synthetic/cloud","type":"synthetic_db"},` +
		`{"actions":["update"],"address":"synthetic_db.main","after":{"labels":{"team":"b"},"size":"3.5"},"afterUnknown":["/endpoint"],"before":{"labels":{"team":"a"},"size":"2"},"mode":"managed","name":"main","providerName":"example.test/synthetic/cloud","type":"synthetic_db"}]`
	if got := string(acquisition.Value()); got != want {
		t.Fatalf("acquired value\n got %s\nwant %s", got, want)
	}
	sum := sha256.Sum256(data)
	wantRecord := `{"bytes":` + strconv.Itoa(len(data)) + `,"document":"plan","formatTable":"proposed-1","formatVersion":"1.2","kind":"terraform-file","removedSensitive":2,"selected":2,"sha256":"` + hex.EncodeToString(sum[:]) + `"}`
	if got := string(acquisition.Record()); got != wantRecord {
		t.Fatalf("record\n got %s\nwant %s", got, wantRecord)
	}
	if got := addresses(acquisition); !reflect.DeepEqual(got, []string{"synthetic_db.audit", "synthetic_db.main"}) {
		t.Fatalf("addresses %v", got)
	}
	joined := "[" + string(acquisition.Resources[0].JSON) + "," + string(acquisition.Resources[1].JSON) + "]"
	if joined != want {
		t.Fatalf("resource spans do not make up the value: %s", joined)
	}
	noSecret(t, acquisition.Value(), acquisition.Record())
}

func TestAStateReadsWithChildModulesAndIndexes(t *testing.T) {
	data := state(t, module{
		resources: []stateResource{
			{address: "synthetic_service.web", values: object("name", "web", "token", "SECRET-TOKEN"), sensitive: object("token", true)},
			{address: "data.synthetic_image.base", mode: "data", values: object("id", "img-1")},
		},
		children: []module{{
			address: "module.app",
			resources: []stateResource{
				{address: "module.app.synthetic_service.worker[0]", index: 0, values: object("name", "w0")},
				{address: `module.app.synthetic_service.worker["blue"]`, index: "blue", values: object("name", "wb")},
			},
			children: []module{{
				address:   "module.app.module.inner",
				resources: []stateResource{{address: "module.app.module.inner.synthetic_service.deep", values: object("name", "deep")}},
			}},
		}},
	})
	acquisition := mustAccept(t, data, stateSource("synthetic_service"))
	wantAddresses := []string{"module.app.module.inner.synthetic_service.deep", `module.app.synthetic_service.worker["blue"]`, "module.app.synthetic_service.worker[0]", "synthetic_service.web"}
	if got := addresses(acquisition); !reflect.DeepEqual(got, wantAddresses) {
		t.Fatalf("addresses %v", got)
	}
	got := resources(t, acquisition)
	if got[1]["index"] != "blue" || got[2]["index"] != "0" {
		t.Fatalf("indexes %v and %v, want the string and the number 0 as \"0\"", got[1]["index"], got[2]["index"])
	}
	if _, ok := got[0]["index"]; ok {
		t.Fatal("a resource with no index has one in the value")
	}
	if want := `{"address":"synthetic_service.web","mode":"managed","name":"web","providerName":"example.test/synthetic/cloud","type":"synthetic_service","values":{"name":"web"}}`; string(acquisition.Resources[3].JSON) != want {
		t.Fatalf("state resource %s", acquisition.Resources[3].JSON)
	}
	if acquisition.RemovedSensitive != 1 || acquisition.Selected != 4 || acquisition.FormatVersion != "1.0" {
		t.Fatalf("record %s", acquisition.Record())
	}
	noSecret(t, acquisition.Value(), acquisition.Record())

	data2 := mustAccept(t, data, Source{Document: DocumentState, Select: Select{Types: []string{"synthetic_image"}, Mode: ModeData}})
	if got := addresses(data2); !reflect.DeepEqual(got, []string{"data.synthetic_image.base"}) {
		t.Fatalf("data source selection %v", got)
	}
}

func TestEveryRefusalCodeIsReported(t *testing.T) {
	valid := plan(t, change{address: "synthetic_db.main", after: object("size", 1)})
	cases := []struct {
		name string
		data []byte
		src  Source
		code string
	}{
		{"size", valid, Source{Document: DocumentPlan, Select: Select{Types: []string{"synthetic_db"}}, MaxBytes: int64(len(valid) - 1)}, CodeSize},
		{"json", []byte(`{"format_version":"1.2",}`), planSource("synthetic_db"), CodeJSON},
		{"format", []byte(`{"format_version":"9.9","planned_values":{}}`), planSource("synthetic_db"), CodeFormat},
		{"document", []byte(`{"format_version":"1.2"}`), planSource("synthetic_db"), CodeDocument},
		{"sensitivity", encode(t, planWith([]any{func() map[string]any {
			c := change{address: "synthetic_db.main", after: object("size", 1)}.json()
			delete(c["change"].(map[string]any), "after_sensitive")
			return c
		}()})), planSource("synthetic_db"), CodeSensitivity},
		{"select bound", plan(t, change{address: "synthetic_db.a"}, change{address: "synthetic_db.b"}),
			Source{Document: DocumentPlan, Select: Select{Types: []string{"synthetic_db"}, Max: 1}}, CodeSelectBound},
		{"number", plan(t, change{address: "synthetic_db.main", after: object("size", json.Number("1e64"))}), planSource("synthetic_db"), CodeNumber},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustRefuse(t, c.data, c.src, c.code)
			seen[c.code] = true
		})
	}
	// TF-VALUE-LIMIT is held at its boundary in TestTheValueLimitHoldsAtItsBoundary.
	for _, code := range Codes() {
		if !seen[code] && code != CodeValueLimit {
			t.Errorf("no case reports %s", code)
		}
	}
}

func TestTheSizeBoundRecordsTheWholeInput(t *testing.T) {
	data := plan(t, change{address: "synthetic_db.main", after: object("size", 1)})
	exact := Source{Document: DocumentPlan, Select: Select{Types: []string{"synthetic_db"}}, MaxBytes: int64(len(data))}
	acquisition := mustAccept(t, data, exact)
	sum := sha256.Sum256(data)
	if acquisition.SHA256 != hex.EncodeToString(sum[:]) || acquisition.Bytes != int64(len(data)) {
		t.Fatalf("accepted at maxBytes with %s/%d", acquisition.SHA256, acquisition.Bytes)
	}
	longer := append(append([]byte(nil), data...), ' ')
	refusal := mustRefuse(t, longer, exact, CodeSize)
	if refusal.Bytes != int64(len(data))+1 {
		t.Fatalf("TF-SIZE records %d bytes", refusal.Bytes)
	}
	// Far past the bound, the digest is still of the whole input.
	huge := append(bytes.Repeat([]byte(" "), 3<<20), data...)
	mustRefuse(t, huge, exact, CodeSize)
}

func nested(depth int, leaf string) string {
	// The top-level object is depth 1, "configuration"'s value depth 2, and
	// each array one deeper, so depth-2 arrays put leaf at depth.
	return `{"format_version":"1.2","planned_values":{},"configuration":` + strings.Repeat("[", depth-2) + leaf + strings.Repeat("]", depth-2) + `}`
}

func TestNestingIsBoundedAt128(t *testing.T) {
	mustAccept(t, []byte(nested(128, "0")), planSource("synthetic_db"))
	mustRefuse(t, []byte(nested(129, "0")), planSource("synthetic_db"), CodeJSON)
	mustAccept(t, []byte(nested(128, "[]")), planSource("synthetic_db"))
	mustRefuse(t, []byte(nested(128, "[0]")), planSource("synthetic_db"), CodeJSON)
	mustRefuse(t, []byte(nested(128, `{"a":0}`)), planSource("synthetic_db"), CodeJSON)
	mustAccept(t, []byte(nested(128, `{}`)), planSource("synthetic_db"))
}

func TestStrictJSONRefusals(t *testing.T) {
	base := `{"format_version":"1.2","planned_values":{},"configuration":%s}`
	at := func(value string) []byte { return []byte(strings.Replace(base, "%s", value, 1)) }
	refused := map[string][]byte{
		"byte-order mark":           append([]byte{0xEF, 0xBB, 0xBF}, at("0")...),
		"invalid UTF-8":             at("\"\xff\""),
		"encoded surrogate":         at("\"\xed\xa0\x80\""),
		"lone high surrogate":       at(`"\ud800"`),
		"lone high then text":       at(`"\ud800x"`),
		"high then high":            at(`"\ud800\ud800"`),
		"lone low surrogate":        at(`"\udc00"`),
		"raw control character":     at("\"a\tb\""),
		"bad escape":                at(`"\x"`),
		"short unicode escape":      at(`"\u12"`),
		"trailing data":             append(at("0"), []byte(` {}`)...),
		"trailing comma":            at("[0,]"),
		"two values":                []byte(`{} {}`),
		"leading zero":              at("01"),
		"bare point":                at("1."),
		"leading point":             at(".5"),
		"plus sign":                 at("+1"),
		"empty exponent":            at("1e"),
		"NaN":                       at("NaN"),
		"single quotes":             at("'a'"),
		"duplicate member":          []byte(`{"format_version":"1.2","planned_values":{},"format_version":"1.2"}`),
		"duplicate after escape":    at(`{"name":1,"n` + uEscape("0061") + `me":2}`),
		"duplicate deep":            at(`[[{"b":1,"a":2,"b":3}]]`),
		"binary plan (zip archive)": append([]byte("PK\x03\x04\x14\x00\x00\x00\x08\x00"), []byte("tfplan")...),
		"empty":                     {},
		"white space only":          []byte("  \n"),
		"truncated":                 at("[0")[:30],
	}
	for name, data := range refused {
		t.Run(name, func(t *testing.T) { mustRefuse(t, data, planSource("synthetic_db"), CodeJSON) })
	}
	accepted := map[string][]byte{
		"surrogate pair":          at(`"` + uEscape("d83d") + uEscape("de00") + `"`),
		"escaped solidus":         at(`"a\/b"`),
		"BOM inside a string":     at("\"\xef\xbb\xbf\""),
		"white space around":      []byte(" \t\r\n" + string(at("0")) + " \n"),
		"names differ by escape":  at(`{"a\"":1,"a":2}`),
		"exponent forms":          at("[1E5,1e+5,1e-5,-0,0.0e0]"),
		"same name, other object": at(`[{"a":1},{"a":2}]`),
	}
	for name, data := range accepted {
		t.Run(name, func(t *testing.T) { mustAccept(t, data, planSource("synthetic_db")) })
	}
}

func TestTheFormatTableIsClosed(t *testing.T) {
	for _, version := range []string{"1.0", "1.1", "1.2"} {
		mustAccept(t, []byte(`{"format_version":"`+version+`","planned_values":{}}`), planSource("synthetic_db"))
	}
	mustAccept(t, []byte(`{"format_version":"1.0"}`), stateSource("synthetic_db"))
	mustAccept(t, []byte(`{"format_version":"1`+uEscape("002e")+`0"}`), stateSource("synthetic_db"))
	refused := map[string]struct {
		data string
		src  Source
	}{
		"newer minor":            {`{"format_version":"1.3","planned_values":{}}`, planSource("synthetic_db")},
		"newer major":            {`{"format_version":"2.0","planned_values":{}}`, planSource("synthetic_db")},
		"a state's later minor":  {`{"format_version":"1.1"}`, stateSource("synthetic_db")},
		"padded":                 {`{"format_version":"1.2 ","planned_values":{}}`, planSource("synthetic_db")},
		"a number":               {`{"format_version":1.2,"planned_values":{}}`, planSource("synthetic_db")},
		"missing":                {`{"planned_values":{}}`, planSource("synthetic_db")},
		"not an object":          {`[{"format_version":"1.2"}]`, planSource("synthetic_db")},
		"a scalar":               {`"1.2"`, planSource("synthetic_db")},
		"nested only":            {`{"values":{"format_version":"1.0"}}`, stateSource("synthetic_db")},
		"raw state file":         {`{"version":4,"terraform_version":"0.0.0","serial":1,"lineage":"00000000-0000-0000-0000-000000000000","outputs":{},"resources":[]}`, stateSource("synthetic_db")},
		"raw state file as plan": {`{"version":4,"serial":1,"resources":[]}`, planSource("synthetic_db")},
	}
	for name, c := range refused {
		t.Run(name, func(t *testing.T) { mustRefuse(t, []byte(c.data), c.src, CodeFormat) })
	}
}

func TestTheDocumentShape(t *testing.T) {
	resourceChange := func(edit func(map[string]any)) []any {
		c := change{address: "synthetic_db.main"}.json()
		edit(c)
		return []any{c}
	}
	plans := map[string]any{
		"no planned_values":       object("format_version", "1.2"),
		"planned_values a list":   object("format_version", "1.2", "planned_values", []any{}),
		"planned_values null":     object("format_version", "1.2", "planned_values", nil),
		"resource_changes object": object("format_version", "1.2", "planned_values", object(), "resource_changes", object()),
		"resource_changes null":   object("format_version", "1.2", "planned_values", object(), "resource_changes", nil),
		"a change not an object":  planWith([]any{"synthetic_db.main"}),
		"address a number":        planWith(resourceChange(func(c map[string]any) { c["address"] = 1 })),
		"no type":                 planWith(resourceChange(func(c map[string]any) { delete(c, "type") })),
		"no provider_name":        planWith(resourceChange(func(c map[string]any) { delete(c, "provider_name") })),
		"mode null":               planWith(resourceChange(func(c map[string]any) { c["mode"] = nil })),
		"index null":              planWith(resourceChange(func(c map[string]any) { c["index"] = nil })),
		"index an object":         planWith(resourceChange(func(c map[string]any) { c["index"] = object() })),
		"no change":               planWith(resourceChange(func(c map[string]any) { delete(c, "change") })),
		"change a list":           planWith(resourceChange(func(c map[string]any) { c["change"] = []any{} })),
		"no actions":              planWith(resourceChange(func(c map[string]any) { delete(c["change"].(map[string]any), "actions") })),
		"actions a string":        planWith(resourceChange(func(c map[string]any) { c["change"].(map[string]any)["actions"] = "create" })),
		"an action a number":      planWith(resourceChange(func(c map[string]any) { c["change"].(map[string]any)["actions"] = []any{1} })),
		"after_unknown a string":  planWith(resourceChange(func(c map[string]any) { c["change"].(map[string]any)["after_unknown"] = "x" })),
		"after_unknown nested":    planWith(resourceChange(func(c map[string]any) { c["change"].(map[string]any)["after_unknown"] = object("a", []any{nil}) })),
	}
	for name, doc := range plans {
		t.Run("plan/"+name, func(t *testing.T) { mustRefuse(t, encode(t, doc), planSource("synthetic_db"), CodeDocument) })
	}
	resource := func(edit func(map[string]any)) map[string]any {
		r := stateResource{address: "synthetic_db.main"}.json()
		edit(r)
		return r
	}
	states := map[string]any{
		"planned_values":          object("format_version", "1.0", "planned_values", object()),
		"resource_changes":        object("format_version", "1.0", "resource_changes", []any{}),
		"resource_changes null":   object("format_version", "1.0", "resource_changes", nil),
		"values a list":           object("format_version", "1.0", "values", []any{}),
		"values null":             object("format_version", "1.0", "values", nil),
		"root_module a list":      object("format_version", "1.0", "values", object("root_module", []any{})),
		"root_module a string":    object("format_version", "1.0", "values", object("root_module", "x")),
		"root_module null":        object("format_version", "1.0", "values", object("root_module", nil)),
		"resources an object":     object("format_version", "1.0", "values", object("root_module", object("resources", object()))),
		"a resource a string":     object("format_version", "1.0", "values", object("root_module", object("resources", []any{"x"}))),
		"child_modules an object": object("format_version", "1.0", "values", object("root_module", object("child_modules", object()))),
		"a child module a list":   object("format_version", "1.0", "values", object("root_module", object("child_modules", []any{[]any{}}))),
		"no address":              object("format_version", "1.0", "values", object("root_module", object("resources", []any{resource(func(r map[string]any) { delete(r, "address") })}))),
		"deep resource, bad name": object("format_version", "1.0", "values", object("root_module", object("child_modules", []any{object("child_modules", []any{object("resources", []any{resource(func(r map[string]any) { r["name"] = false })})})}))),
	}
	for name, doc := range states {
		t.Run("state/"+name, func(t *testing.T) { mustRefuse(t, encode(t, doc), stateSource("synthetic_db"), CodeDocument) })
	}
	// A state with no values is a state with no resources; so is a plan with
	// no resource_changes.
	empty := mustAccept(t, []byte(`{"format_version":"1.0"}`), stateSource("synthetic_db"))
	if string(empty.Value()) != "[]" || empty.Selected != 0 {
		t.Fatalf("empty state %s", empty.Value())
	}
	emptyPlan := mustAccept(t, []byte(`{"format_version":"1.2","planned_values":{}}`), planSource("synthetic_db"))
	if string(emptyPlan.Value()) != "[]" {
		t.Fatalf("empty plan %s", emptyPlan.Value())
	}
	// values without a root_module is an empty state too (RFC 0038 §2 step 4),
	// whatever else values holds; the plan reads resource_changes, so a
	// planned_values without a root_module does not matter either.
	for _, doc := range []string{
		`{"format_version":"1.0","values":{}}`,
		`{"format_version":"1.0","values":{"outputs":{}}}`,
	} {
		emptyValues := mustAccept(t, []byte(doc), stateSource("synthetic_db"))
		if string(emptyValues.Value()) != "[]" || emptyValues.Selected != 0 || len(emptyValues.Resources) != 0 || emptyValues.RemovedSensitive != 0 {
			t.Fatalf("%s: value %s, %d selected", doc, emptyValues.Value(), emptyValues.Selected)
		}
	}
	rootless := planWith([]any{change{address: "synthetic_db.main", after: object("size", 1)}.json()})
	rootless["planned_values"] = object()
	noRoot := mustAccept(t, encode(t, rootless), planSource("synthetic_db"))
	if noRoot.Selected != 1 {
		t.Fatalf("plan without planned_values.root_module: %d selected", noRoot.Selected)
	}
}

func TestEveryResourceCarriesItsMirror(t *testing.T) {
	without := func(member string) map[string]any {
		c := change{address: "synthetic_other.unselected"}.json()
		delete(c["change"].(map[string]any), member)
		return c
	}
	selected := change{address: "synthetic_db.main", after: object("size", 1)}.json()
	for _, member := range []string{"before_sensitive", "after_sensitive"} {
		// Unselected: the type is not selected, and it still refuses.
		mustRefuse(t, encode(t, planWith([]any{selected, without(member)})), planSource("synthetic_db"), CodeSensitivity)
		// Selected.
		c := change{address: "synthetic_db.other"}.json()
		delete(c["change"].(map[string]any), member)
		mustRefuse(t, encode(t, planWith([]any{c})), planSource("synthetic_db"), CodeSensitivity)
	}
	for name, mirror := range map[string]any{
		"null": json.RawMessage("null"), "a string": "true", "a number": 1,
		"a nested null": object("a", nil), "a nested string": object("a", []any{"x"}),
	} {
		t.Run(name, func(t *testing.T) {
			mustRefuse(t, plan(t, change{address: "synthetic_db.main", after: object("a", 1), afterSens: mirror}), planSource("synthetic_db"), CodeSensitivity)
			mustRefuse(t, state(t, module{resources: []stateResource{{address: "synthetic_db.main", sensitive: mirror}}}), stateSource("synthetic_db"), CodeSensitivity)
		})
	}
	// A state resource deep in the module tree, not selected, without its mirror.
	r := stateResource{address: "module.a.module.b.synthetic_other.x"}.json()
	delete(r, "sensitive_values")
	doc := object("format_version", "1.0", "values", object("root_module", object(
		"resources", []any{stateResource{address: "synthetic_db.main"}.json()},
		"child_modules", []any{object("child_modules", []any{object("resources", []any{r})})}),
	))
	mustRefuse(t, encode(t, doc), stateSource("synthetic_db"), CodeSensitivity)
	// The empty mirrors are markers that nothing is sensitive.
	for _, mirror := range []any{false, object(), []any{}} {
		mustAccept(t, plan(t, change{address: "synthetic_db.main", after: object("a", 1), beforeSens: mirror, afterSens: mirror}), planSource("synthetic_db"))
	}
}

func TestAMalformedSelectIsAnInvalidSourceNotARefusal(t *testing.T) {
	long := strings.Repeat("x", MaxPatternChars+1)
	cases := map[string]Source{
		"document":              {Document: "plans", Select: Select{Types: []string{"a"}}},
		"maxBytes negative":     {Document: DocumentPlan, Select: Select{Types: []string{"a"}}, MaxBytes: -1},
		"maxBytes past the cap": {Document: DocumentPlan, Select: Select{Types: []string{"a"}}, MaxBytes: MaxBytesLimit + 1},
		"no types":              {Document: DocumentPlan},
		"too many types":        {Document: DocumentPlan, Select: Select{Types: manyTypes(MaxTypes + 1)}},
		"a type twice":          {Document: DocumentPlan, Select: Select{Types: []string{"a", "a"}}},
		"an empty type":         {Document: DocumentPlan, Select: Select{Types: []string{""}}},
		"mode":                  {Document: DocumentPlan, Select: Select{Types: []string{"a"}, Mode: "Managed"}},
		"address too long":      {Document: DocumentPlan, Select: Select{Types: []string{"a"}, Address: &long}},
		"address not UTF-8":     {Document: DocumentPlan, Select: Select{Types: []string{"a"}, Address: ptr("\xff")}},
		"actions on a state":    {Document: DocumentState, Select: Select{Types: []string{"a"}, Actions: []string{"create"}}},
		"empty actions, state":  {Document: DocumentState, Select: Select{Types: []string{"a"}, Actions: []string{}}},
		"an unknown action":     {Document: DocumentPlan, Select: Select{Types: []string{"a"}, Actions: []string{"replace"}}},
		"max past the cap":      {Document: DocumentPlan, Select: Select{Types: []string{"a"}, Max: SelectMaxLimit + 1}},
		"max negative":          {Document: DocumentPlan, Select: Select{Types: []string{"a"}, Max: -1}},
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if err := src.Validate(); !errors.Is(err, ErrInvalidSource) {
				t.Fatalf("Validate: %v", err)
			}
			acquisition, refusal, err := Read(strings.NewReader(`{}`), src)
			if !errors.Is(err, ErrInvalidSource) || acquisition != nil || refusal != nil {
				t.Fatalf("Read: %v %v %v", acquisition, refusal, err)
			}
		})
	}
	edge := strings.Repeat("é", MaxPatternChars)
	valid := []Source{
		{Document: DocumentPlan, Select: Select{Types: manyTypes(MaxTypes)}},
		{Document: DocumentPlan, Select: Select{Types: []string{"a"}, Address: &edge, Max: SelectMaxLimit, Actions: []string{"create", "update", "delete", "read", "no-op"}}, MaxBytes: MaxBytesLimit},
		{Document: DocumentState, Select: Select{Types: []string{"a"}, Mode: ModeData, Max: 1}, MaxBytes: 1},
	}
	for _, src := range valid {
		if err := src.Validate(); err != nil {
			t.Fatalf("Validate(%+v): %v", src.Select, err)
		}
	}
}

func manyTypes(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "synthetic_" + strings.Repeat("t", i+1)
	}
	return out
}

func TestTheValueLimitHoldsAtItsBoundary(t *testing.T) {
	data := plan(t, change{address: "synthetic_db.main", after: object("size", json.Number("1e40"))})
	full := mustAccept(t, data, planSource("synthetic_db"))
	size := int64(len(full.Value()))
	for _, c := range []struct {
		limit int64
		want  string
	}{{size, ""}, {size - 1, CodeValueLimit}, {1, CodeValueLimit}} {
		acquisition, refusal, err := read(bytes.NewReader(data), planSource("synthetic_db"), false, c.limit, ValueBytesPerInputByte)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case c.want == "" && refusal != nil:
			t.Fatalf("limit %d: refused %s", c.limit, refusal.Code)
		case c.want == "" && !bytes.Equal(acquisition.Value(), full.Value()):
			t.Fatalf("limit %d: value differs", c.limit)
		case c.want != "" && (refusal == nil || refusal.Code != c.want):
			t.Fatalf("limit %d: %v, want %s", c.limit, refusal, c.want)
		}
		if refusal != nil {
			checkRefusal(t, data, refusal)
		}
	}
	// Pointers count against the limit too: many unknowns under one long name.
	// The value is about 32 times this input, so the source bound is lifted
	// here to hold the fixed limit alone; TestTheValueIsBoundedByItsSource
	// holds the source bound.
	pointerPlan := longPointerPlan(t)
	const unbounded = MaxValueBytes
	if _, refusal, _ := read(bytes.NewReader(pointerPlan), planSource("synthetic_db"), false, 64*4096, unbounded); refusal == nil || refusal.Code != CodeValueLimit {
		t.Fatalf("afterUnknown pointers past the limit: %v", refusal)
	}
	if _, refusal, _ := read(bytes.NewReader(pointerPlan), planSource("synthetic_db"), false, 80*4096, unbounded); refusal != nil {
		t.Fatalf("afterUnknown pointers within the limit: %v", refusal.Code)
	}
}

// longPointerPlan has 64 unknowns under one 4096-byte name: 64 pointers that
// repeat it.
func longPointerPlan(t *testing.T) []byte {
	long := strings.Repeat("n", 4096)
	unknowns := map[string]any{}
	for i := 0; i < 64; i++ {
		unknowns[strconv.Itoa(i)] = true
	}
	return plan(t, change{address: "synthetic_db.main", after: object(long, object()), afterUnknown: object(long, unknowns)})
}

// The value is held to perInputByte times the input as well as to the fixed
// limit: a multiple k bounds it exactly as a fixed limit of k times the input
// does, on both sides of the value's size. Read applies
// ValueBytesPerInputByte, which these long pointers exceed and the ordinary
// plans of the other tests stay within.
func TestTheValueIsBoundedByItsSource(t *testing.T) {
	data := longPointerPlan(t)
	input := int64(len(data))
	full, refusal, err := read(bytes.NewReader(data), planSource("synthetic_db"), false, MaxValueBytes, MaxValueBytes)
	if err != nil || refusal != nil {
		t.Fatalf("unbounded: %v %v", refusal, err)
	}
	size := int64(len(full.Value()))
	// Below: k times the input is under the value. Above: it holds the value
	// and the 8-byte span of each of the 64 pointers.
	below, above := (size-1)/input, (size+8*64)/input+1
	for _, c := range []struct {
		k      int64
		refuse bool
	}{{below, true}, {above, false}} {
		_, byMultiple, _ := read(bytes.NewReader(data), planSource("synthetic_db"), false, MaxValueBytes, c.k)
		_, byLimit, _ := read(bytes.NewReader(data), planSource("synthetic_db"), false, c.k*input, MaxValueBytes)
		if (byMultiple != nil) != c.refuse || (byLimit != nil) != c.refuse {
			t.Fatalf("multiple %d: %v, limit %d: %v, want refused %v", c.k, byMultiple, c.k*input, byLimit, c.refuse)
		}
		if byMultiple != nil {
			if byMultiple.Code != CodeValueLimit {
				t.Fatalf("multiple %d: %s, want %s", c.k, byMultiple.Code, CodeValueLimit)
			}
			checkRefusal(t, data, byMultiple)
		}
	}
	if below < ValueBytesPerInputByte {
		t.Fatalf("the long pointers are %d bytes for %d of input, within the source bound", size, input)
	}
	mustRefuse(t, data, planSource("synthetic_db"), CodeValueLimit)
}

// A file of maximal numbers, `1e63` over and over, is accepted: the normal
// form grows it about 13.4 times, the most any number can, so the source
// bound must sit above that. A bound of 13 or less refuses this file.
func TestMaximalNumbersStayWithinTheSourceBound(t *testing.T) {
	list := make([]any, 200000)
	for i := range list {
		list[i] = json.Number("1e63")
	}
	data := plan(t, change{address: "synthetic_db.main", after: object("n", list)})
	acquisition := mustAccept(t, data, planSource("synthetic_db"))
	input, size := int64(len(data)), int64(len(acquisition.Value()))
	if size <= 13*input {
		t.Fatalf("the value is %d bytes for %d of input, not past 13 times", size, input)
	}
	if size > ValueBytesPerInputByte*input {
		t.Fatalf("the value is %d bytes for %d of input, past the source bound", size, input)
	}
}

func TestAfterUnknownSpansCountAgainstTheLimit(t *testing.T) {
	// 2000 short pointers: their bytes and the value fit a limit that their
	// 8-byte spans do not.
	list := make([]any, 2000)
	for i := range list {
		list[i] = true
	}
	data := plan(t, change{address: "synthetic_db.main", after: object(), afterUnknown: object("l", list)})
	size := int64(len(mustAccept(t, data, planSource("synthetic_db")).Value()))
	if _, refusal, _ := read(bytes.NewReader(data), planSource("synthetic_db"), false, size, ValueBytesPerInputByte); refusal == nil || refusal.Code != CodeValueLimit {
		t.Fatalf("spans past the limit: %v", refusal)
	}
	if _, refusal, _ := read(bytes.NewReader(data), planSource("synthetic_db"), false, size+8*2000, ValueBytesPerInputByte); refusal != nil {
		t.Fatalf("spans within the limit: %v", refusal.Code)
	}
}

func TestDiagnosticsNeverCarryTheFile(t *testing.T) {
	// A refused file whose member names are themselves secret.
	secretName := "SECRET-NAME-xyzzy"
	files := [][]byte{
		[]byte(`{"format_version":"1.2","planned_values":{},"` + secretName + `":1,"` + secretName + `":2}`),
		encode(t, planWith([]any{func() map[string]any {
			c := change{address: "synthetic_db.main", after: object(secretName, "SECRET-VALUE")}.json()
			delete(c["change"].(map[string]any), "after_sensitive")
			return c
		}()})),
		plan(t, change{address: "synthetic_db.main", after: object(secretName, json.Number("1e999"))}),
		[]byte(`{"format_version":"` + secretName + `"}`),
	}
	for _, data := range files {
		_, refusal := readBytes(t, data, planSource("synthetic_db"))
		if refusal == nil {
			t.Fatal("accepted")
		}
		checkRefusal(t, data, refusal)
		noSecret(t, refusal.JSON(), []byte(refusal.Error()))
	}
}

func TestRefusalJSONIsCanonical(t *testing.T) {
	refusal := newRefusal(CodeJSON, strings.Repeat("ab", 32), 12)
	want := `{"bytes":12,"code":"TF-JSON","message":` + string(encode(t, messages[CodeJSON])) + `,"sha256":"` + strings.Repeat("ab", 32) + `"}`
	if string(refusal.JSON()) != want {
		t.Fatalf("refusal JSON %s", refusal.JSON())
	}
	for _, code := range Codes() {
		if Message(code) == "" {
			t.Fatalf("%s has no message", code)
		}
	}
}

func TestReadErrorsAreNotRefusals(t *testing.T) {
	failure := errors.New("synthetic read failure")
	acquisition, refusal, err := Read(failingReader{failure}, planSource("synthetic_db"))
	if !errors.Is(err, failure) || acquisition != nil || refusal != nil {
		t.Fatalf("%v %v %v", acquisition, refusal, err)
	}
	// A reader's error can carry a path; the message Read returns is fixed.
	pathError := &fs.PathError{Op: "read", Path: "/synthetic/dir/plan.json", Err: failure}
	_, _, err = Read(failingReader{pathError}, planSource("synthetic_db"))
	if !errors.Is(err, ErrRead) || !errors.Is(err, failure) || err.Error() != ErrRead.Error() || strings.Contains(err.Error(), "synthetic") {
		t.Fatalf("read error %v", err)
	}
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }
