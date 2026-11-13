package view_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
	pv "github.com/Judgment-Pack/judgment-pack-runtime/sdk/view"
)

// expense is an expense-approval pack written by hand for the comparison
// tests. Each field is JSON text placed in the document as it stands; an
// empty field leaves its member out, and the zero value is a Core 0.5.0 pack
// with one rule declining an amount over 5000.
type expense struct {
	spec, id, version  string
	declineExtensions  string
	ruleExtensions     string
	limit              string
	extraRules         string
	valueSets          string
	exceptions         string
	escalation         string
	rootExtensions     string
	outcomesSwapped    bool
	evidenceExtensions string
}

func (e expense) text() string {
	or := func(value, otherwise string) string {
		if value == "" {
			return otherwise
		}
		return value
	}
	member := func(name, value string) string {
		if value == "" {
			return ""
		}
		return `,"` + name + `":` + value
	}
	approve := `{"id":"approve","label":"Approve"}`
	decline := `{"id":"decline","label":"Decline"` + member("extensions", e.declineExtensions) + `}`
	outcomes := approve + "," + decline
	if e.outcomesSwapped {
		outcomes = decline + "," + approve
	}
	rule := `{"id":"over-limit","description":"An amount over the limit is declined.","when":{"op":"fact","path":"/expense/amount","operator":"greater-than","value":"` +
		or(e.limit, "5000") + `"},"outcome":"decline","onUnknown":"ignore"` + member("extensions", e.ruleExtensions) + `}`
	if e.extraRules != "" {
		rule += "," + e.extraRules
	}
	return `{"specVersion":"` + or(e.spec, "0.5.0") + `","id":"` + or(e.id, "https://example.invalid/packs/expense-approval") + `","version":"` + or(e.version, "1.0.0") + `",` +
		`"title":"Expense approval","decision":{"intent":"Approve ordinary expenses.","question":"May this expense be approved?"},` +
		`"outcomes":[` + outcomes + `],"rules":[` + rule + `]` + member("exceptions", e.exceptions) + member("valueSets", e.valueSets) +
		`,"fallbackOutcome":"approve"` + member("escalation", e.escalation) + member("extensions", e.rootExtensions) + `}`
}

func (e expense) load(t *testing.T) *sdk.Pack {
	t.Helper()
	return loadFilterPack(t, e.text())
}

func digestOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func twoWay(t *testing.T, older, newer expense) pv.Diff {
	t.Helper()
	compared, err := sdk.Diff(older.load(t), newer.load(t))
	if err != nil {
		t.Fatal(err)
	}
	return compared
}

func threeWay(t *testing.T, base, first, second expense) pv.ThreeWayDiff {
	t.Helper()
	compared, err := sdk.ThreeWayDiff(base.load(t), first.load(t), second.load(t))
	if err != nil {
		t.Fatal(err)
	}
	return compared
}

func decoded(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	return value
}

func changesOf(t *testing.T, report map[string]any, section string) []any {
	t.Helper()
	held, ok := report[section].(map[string]any)
	if !ok {
		t.Fatalf("no %s section: %v", section, report)
	}
	changes, ok := held["changes"].([]any)
	if !ok {
		t.Fatalf("%s has no changes: %v", section, held)
	}
	return changes
}

func encoded(t *testing.T, value any) string {
	t.Helper()
	text, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

// An annotation-only change to one outcome is reported with the outcome's id
// and the old and new values, and nothing else is. The expected bytes are
// written by hand; the digests are the SHA-256 of the two pack texts,
// computed here and not by the runtime.
func TestAnAnnotationOnlyChangeIsReportedByHand(t *testing.T) {
	older := expense{declineExtensions: `{"org.judgmentpack.annotations":{"severity":"medium"}}`}
	newer := expense{declineExtensions: `{"org.judgmentpack.annotations":{"severity":"high"}}`}
	compared := twoWay(t, older, newer)
	identity := func(text string) string {
		return `{"digest":"` + digestOf(text) + `","id":"https://example.invalid/packs/expense-approval","specVersion":"0.5.0","version":"1.0.0"}`
	}
	want := `{"diffVersion":"1","differs":true,"evidenceRequirements":{"changes":[]},"exceptions":{"changes":[]},` +
		`"new":` + identity(newer.text()) + `,"old":` + identity(older.text()) + `,` +
		`"outcomes":{"changes":[{"annotations":[{"change":"changed","key":"severity","new":"high","old":"medium"}],"change":"changed","id":"decline","members":[]}]},` +
		`"pack":{"annotations":[],"members":[]},"rules":{"changes":[]},"sources":{"changes":[]},"valueSets":{"changes":[]}}`
	if got := string(compared.JSON()); got != want {
		t.Fatalf("JSON\ngot:  %s\nwant: %s", got, want)
	}
	wantText := "STRUCTURAL COMPARISON (diff version 1): what the two documents say differently, member by member; it says nothing about how they decide any input\n" +
		`old: id "https://example.invalid/packs/expense-approval" version "1.0.0" specVersion "0.5.0" digest ` + digestOf(older.text()) + "\n" +
		`new: id "https://example.invalid/packs/expense-approval" version "1.0.0" specVersion "0.5.0" digest ` + digestOf(newer.text()) + "\n" +
		"outcomes:\n" +
		`  "decline" changed:` + "\n" +
		`    annotation "severity" changed: "medium" -> "high"` + "\n"
	if got := string(compared.Text()); got != wantText {
		t.Fatalf("text\ngot:  %q\nwant: %q", got, wantText)
	}
	if !compared.Differs || compared.DifferentDecisions || compared.Old.Digest != digestOf(older.text()) {
		t.Fatalf("typed fields: %+v", compared)
	}
}

// Annotation keys added and removed are reported on an outcome and on a rule,
// with every key treated alike, and the first annotations a member gains, or
// the last it loses, are reported as annotations and not as an extensions
// member appearing or vanishing.
func TestAnnotationKeysAddedAndRemoved(t *testing.T) {
	older := expense{
		declineExtensions: `{"org.judgmentpack.annotations":{"severity":"medium","owner":"finance"}}`,
		ruleExtensions:    `{"org.judgmentpack.annotations":{"review":"monthly"}}`,
	}
	newer := expense{
		declineExtensions: `{"org.judgmentpack.annotations":{"severity":"medium","team":["travel","audit"]}}`,
	}
	report := decoded(t, twoWay(t, older, newer).JSON())
	outcomes := changesOf(t, report, "outcomes")
	if got, want := encoded(t, outcomes), `[{"annotations":[{"change":"removed","key":"owner","old":"finance"},{"change":"added","key":"team","new":["travel","audit"]}],"change":"changed","id":"decline","members":[]}]`; got != want {
		t.Fatalf("outcomes\ngot:  %s\nwant: %s", got, want)
	}
	rules := changesOf(t, report, "rules")
	if got, want := encoded(t, rules), `[{"annotations":[{"change":"removed","key":"review","old":"monthly"}],"change":"changed","id":"over-limit","members":[]}]`; got != want {
		t.Fatalf("rules\ngot:  %s\nwant: %s", got, want)
	}
	// The other way round: the rule gains its first annotation.
	report = decoded(t, twoWay(t, newer, older).JSON())
	if got, want := encoded(t, changesOf(t, report, "rules")), `[{"annotations":[{"change":"added","key":"review","new":"monthly"}],"change":"changed","id":"over-limit","members":[]}]`; got != want {
		t.Fatalf("rules, reversed\ngot:  %s\nwant: %s", got, want)
	}
	// A vendor extension added beside annotations is reported by name.
	vendor := expense{ruleExtensions: `{"org.judgmentpack.annotations":{"review":"monthly"},"com.example.audit":{"sample":true}}`}
	report = decoded(t, twoWay(t, expense{}, vendor).JSON())
	if got, want := encoded(t, changesOf(t, report, "rules")), `[{"annotations":[{"change":"added","key":"review","new":"monthly"}],"change":"changed","id":"over-limit","members":[{"change":"added","new":{"sample":true},"pointer":"/extensions/com.example.audit"}]}]`; got != want {
		t.Fatalf("vendor\ngot:  %s\nwant: %s", got, want)
	}
}

// A changed threshold in a rule condition is reported, with its old and new
// values, whatever inputs exist: the comparison reads the documents, not
// evaluations of them.
func TestAChangedThresholdIsReported(t *testing.T) {
	compared := twoWay(t, expense{}, expense{limit: "4000"})
	if got, want := encoded(t, changesOf(t, decoded(t, compared.JSON()), "rules")), `[{"annotations":[],"change":"changed","id":"over-limit","members":[{"change":"changed","new":"4000","old":"5000","pointer":"/when/value"}]}]`; got != want {
		t.Fatalf("got:  %s\nwant: %s", got, want)
	}
	if !strings.Contains(string(compared.Text()), `    /when/value changed: "5000" -> "4000"`+"\n") {
		t.Fatalf("text: %s", compared.Text())
	}
}

const currencyRule = `{"id":"foreign","description":"A foreign currency is declined.","when":{"op":"fact","path":"/expense/currency","operator":"in","valueSet":"currencies"},"outcome":"decline","onUnknown":"ignore"}`

// A value-set entry added is reported by its value, an entry's annotations
// are compared as any carrier's are, and a value the set holds twice is told
// apart by its occurrence.
func TestValueSetEntries(t *testing.T) {
	older := expense{extraRules: currencyRule, valueSets: `[{"id":"currencies","entries":[{"value":"EUR"},{"value":"USD","extensions":{"org.judgmentpack.annotations":{"region":"americas"}}}]}]`}
	newer := expense{extraRules: currencyRule, valueSets: `[{"id":"currencies","entries":[{"value":"EUR"},{"value":"USD","extensions":{"org.judgmentpack.annotations":{"region":"north-america"}}},{"value":"GBP"},{"value":"EUR","description":"Again"}]}]`}
	compared := twoWay(t, older, newer)
	want := `[{"change":"changed","entries":{"changes":[` +
		`{"change":"added","new":{"value":"EUR","description":"Again"},"occurrence":2,"value":"EUR"},` +
		`{"change":"added","new":{"value":"GBP"},"value":"GBP"},` +
		`{"annotations":[{"change":"changed","key":"region","new":"north-america","old":"americas"}],"change":"changed","members":[],"value":"USD"}` +
		`]},"id":"currencies","members":[]}]`
	// Go's encoder orders the members of the decoded objects by name, so the
	// whole entry is compared on its own.
	got := encoded(t, changesOf(t, decoded(t, compared.JSON()), "valueSets"))
	if got != strings.Replace(want, `{"value":"EUR","description":"Again"}`, `{"description":"Again","value":"EUR"}`, 1) {
		t.Fatalf("got:  %s\nwant: %s", got, want)
	}
	text := string(compared.Text())
	for _, line := range []string{`    entry "EUR" (occurrence 2) added`, `    entry "GBP" added`, `    entry "USD" changed:`, `      annotation "region" changed: "americas" -> "north-america"`} {
		if !strings.Contains(text, line+"\n") {
			t.Fatalf("missing %q in:\n%s", line, text)
		}
	}
}

// Identical packs report nothing, in either form, and so do two documents
// that differ only in layout and member order, whose digests differ.
func TestIdenticalPacksReportNothing(t *testing.T) {
	pack := expense{declineExtensions: `{"org.judgmentpack.annotations":{"severity":"medium"}}`, extraRules: currencyRule, valueSets: `[{"id":"currencies","entries":[{"value":"EUR"}]}]`}
	compared := twoWay(t, pack, pack)
	if compared.Differs || !strings.HasSuffix(string(compared.Text()), "\nno structural differences\n") {
		t.Fatalf("%s", compared.Text())
	}
	report := decoded(t, compared.JSON())
	for _, section := range []string{"evidenceRequirements", "sources", "outcomes", "rules", "exceptions", "valueSets"} {
		held := report[section].(map[string]any)
		if len(changesOf(t, report, section)) != 0 || len(held) != 1 {
			t.Fatalf("%s: %v", section, held)
		}
	}
	if got := encoded(t, report["pack"]); got != `{"annotations":[],"members":[]}` {
		t.Fatalf("pack: %s", got)
	}
	reordered := strings.Replace(pack.text(), `{"id":"approve","label":"Approve"}`, `{ "label" : "Approve", "id" : "approve" }`, 1)
	if reordered == pack.text() {
		t.Fatal("the reordering did nothing")
	}
	layout, err := sdk.Diff(pack.load(t), loadFilterPack(t, reordered))
	if err != nil {
		t.Fatal(err)
	}
	if layout.Differs || layout.Old.Digest == layout.New.Digest {
		t.Fatalf("%s", layout.Text())
	}
	three := threeWay(t, pack, pack, pack)
	if three.Differs || three.Conflicts != 0 || !strings.HasSuffix(string(three.Text()), "\nno structural differences\n") {
		t.Fatalf("%s", three.Text())
	}
}

// The same packs give the same bytes, in a canonical form: decoded and
// written again canonically, the JSON is unchanged.
func TestTheComparisonIsDeterministic(t *testing.T) {
	older := expense{declineExtensions: `{"org.judgmentpack.annotations":{"severity":"medium","owner":"finance","team":["a","b"]}}`, extraRules: currencyRule, valueSets: `[{"id":"currencies","entries":[{"value":"EUR"},{"value":1.50}]}]`}
	newer := expense{declineExtensions: `{"org.judgmentpack.annotations":{"severity":"high","area":"travel"}}`, limit: "4000", outcomesSwapped: true, extraRules: currencyRule, valueSets: `[{"id":"currencies","entries":[{"value":1.5},{"value":"EUR"},{"value":"GBP"}]}]`}
	first, second := twoWay(t, older, newer), twoWay(t, older, newer)
	if !bytes.Equal(first.JSON(), second.JSON()) || !bytes.Equal(first.Text(), second.Text()) {
		t.Fatal("two comparisons of the same packs differ")
	}
	decoder := json.NewDecoder(bytes.NewReader(first.JSON()))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	again, err := pv.CanonicalText(value)
	if err != nil {
		t.Fatal(err)
	}
	if again != string(first.JSON()) {
		t.Fatalf("not canonical:\n%s\n%s", again, first.JSON())
	}
	// A number is compared and shown as written: 1.50 is not 1.5.
	if !strings.Contains(string(first.Text()), `entry 1.50 removed`) || !strings.Contains(string(first.Text()), `entry 1.5 added`) {
		t.Fatalf("%s", first.Text())
	}
	threeA, threeB := threeWay(t, older, newer, older), threeWay(t, older, newer, older)
	if !bytes.Equal(threeA.JSON(), threeB.JSON()) || !bytes.Equal(threeA.Text(), threeB.Text()) {
		t.Fatal("two three-way comparisons of the same packs differ")
	}
}

// The three-way form names, for a rule condition and for an annotation, each
// of the four statuses: only first changed it, only second, both alike, and
// both differently, which is a conflict.
func TestThreeWayStatuses(t *testing.T) {
	annotated := func(limit, severity string) expense {
		return expense{limit: limit, declineExtensions: `{"org.judgmentpack.annotations":{"severity":"` + severity + `"}}`}
	}
	base := annotated("5000", "medium")
	for _, row := range []struct {
		name          string
		first, second expense
		status        string
		conflicts     int
	}{
		{"first", annotated("4000", "high"), base, "first", 0},
		{"second", base, annotated("4000", "high"), "second", 0},
		{"both", annotated("4000", "high"), annotated("4000", "high"), "both", 0},
		{"conflict", annotated("4000", "high"), annotated("3000", "low"), "conflict", 2},
	} {
		t.Run(row.name, func(t *testing.T) {
			compared := threeWay(t, base, row.first, row.second)
			report := decoded(t, compared.JSON())
			firstLimit, secondLimit := row.first.limit, row.second.limit
			firstSeverity := strings.Split(strings.Split(row.first.declineExtensions, `"severity":"`)[1], `"`)[0]
			secondSeverity := strings.Split(strings.Split(row.second.declineExtensions, `"severity":"`)[1], `"`)[0]
			wantRules := `[{"annotations":[],"id":"over-limit","members":[{"base":"5000","first":"` + firstLimit + `","pointer":"/when/value","second":"` + secondLimit + `","status":"` + row.status + `"}]}]`
			if got := encoded(t, changesOf(t, report, "rules")); got != wantRules {
				t.Fatalf("rules\ngot:  %s\nwant: %s", got, wantRules)
			}
			wantOutcomes := `[{"annotations":[{"base":"medium","first":"` + firstSeverity + `","key":"severity","second":"` + secondSeverity + `","status":"` + row.status + `"}],"id":"decline","members":[]}]`
			if got := encoded(t, changesOf(t, report, "outcomes")); got != wantOutcomes {
				t.Fatalf("outcomes\ngot:  %s\nwant: %s", got, wantOutcomes)
			}
			if compared.Conflicts != row.conflicts || report["conflicts"] != float64(row.conflicts) || !compared.Differs {
				t.Fatalf("conflicts %d, %v", compared.Conflicts, report["conflicts"])
			}
			if !strings.Contains(string(compared.Text()), `    /when/value `+row.status+`: base "5000", first "`+firstLimit+`", second "`+secondLimit+`"`+"\n") {
				t.Fatalf("%s", compared.Text())
			}
		})
	}
}

const travelRule = `{"id":"travel","description":"Travel is approved.","when":{"op":"fact","path":"/expense/kind","operator":"equals","value":"travel"},"outcome":"approve","onUnknown":"ignore"}`

// A member absent from at least one of the three is reported whole, with
// what each descendant did to it. The expected bytes are written by hand.
func TestThreeWayWholeMembersByHand(t *testing.T) {
	base := expense{extraRules: travelRule}
	first := expense{}
	second := expense{extraRules: strings.Replace(travelRule, `"Travel is approved."`, `"Travel is approved at once."`, 1)}
	compared := threeWay(t, base, first, second)
	identity := func(e expense) string {
		return `{"digest":"` + digestOf(e.text()) + `","id":"https://example.invalid/packs/expense-approval","specVersion":"0.5.0","version":"1.0.0"}`
	}
	want := `{"base":` + identity(base) + `,"conflicts":1,"differs":true,"evidenceRequirements":{"changes":[]},"exceptions":{"changes":[]},` +
		`"first":` + identity(first) + `,"outcomes":{"changes":[]},"pack":{"annotations":[],"members":[]},` +
		`"rules":{"changes":[{"base":` + travelRule + `,"id":"travel","second":` + strings.Replace(travelRule, `"Travel is approved."`, `"Travel is approved at once."`, 1) + `,"status":"conflict"}]},` +
		`"second":` + identity(second) + `,"sources":{"changes":[]},"threeWayDiffVersion":"1","valueSets":{"changes":[]}}`
	// The whole rule is written as the view writes it, canonically.
	canonical := func(text string) string {
		var value any
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		written, err := pv.CanonicalText(value)
		if err != nil {
			t.Fatal(err)
		}
		return written
	}
	if got := string(compared.JSON()); got != canonical(want) {
		t.Fatalf("got:  %s\nwant: %s", got, canonical(want))
	}
	if !strings.Contains(string(compared.Text()), `  "travel" conflict: first removed, second changed`+"\n") || !strings.HasSuffix(string(compared.Text()), "conflicts: 1\n") {
		t.Fatalf("%s", compared.Text())
	}
	both := threeWay(t, expense{}, base, base)
	if got := encoded(t, changesOf(t, decoded(t, both.JSON()), "rules")); !strings.Contains(got, `"status":"both"`) || both.Conflicts != 0 {
		t.Fatalf("%s", got)
	}
}

// Core §6.7 makes escalation.triggers and a route's triggers sets, so their
// order is never a difference; routes are an ordered array, and a keyed
// collection's order is reported apart from its members.
func TestWhichOrdersAreReported(t *testing.T) {
	routed := func(triggers, routes string) expense {
		return expense{escalation: `{"triggers":` + triggers + `,"routes":` + routes + `}`}
	}
	queue := `{"target":{"kind":"queue","name":"Review"},"triggers":["unknown","conflict"]}`
	role := `{"target":{"kind":"human-role","name":"Oversight"}}`
	older := routed(`["unknown","conflict","no-match"]`, `[`+queue+`,`+role+`]`)
	sets := routed(`["no-match","conflict","unknown"]`, `[`+strings.Replace(queue, `["unknown","conflict"]`, `["conflict","unknown"]`, 1)+`,`+role+`]`)
	if compared := twoWay(t, older, sets); compared.Differs {
		t.Fatalf("a set's order was reported:\n%s", compared.Text())
	}
	routes := routed(`["unknown","conflict","no-match"]`, `[`+role+`,`+queue+`]`)
	members := decoded(t, twoWay(t, older, routes).JSON())["pack"].(map[string]any)["members"].([]any)
	if len(members) == 0 || !strings.HasPrefix(members[0].(map[string]any)["pointer"].(string), "/escalation/routes/0") {
		t.Fatalf("the routes' order was not reported: %v", members)
	}
	swapped := expense{outcomesSwapped: true}
	report := decoded(t, twoWay(t, expense{}, swapped).JSON())
	if got := encoded(t, report["outcomes"]); got != `{"changes":[],"order":{"new":["decline","approve"],"old":["approve","decline"]}}` {
		t.Fatalf("outcomes: %s", got)
	}
}

// An optional collection absent in one version and present and empty in the
// other holds the same members and is still reported.
func TestAnEmptyCollectionIsNotAnAbsentOne(t *testing.T) {
	report := decoded(t, twoWay(t, expense{}, expense{exceptions: `[]`}).JSON())
	if got := encoded(t, report["exceptions"]); got != `{"changes":[],"present":{"new":true,"old":false}}` {
		t.Fatalf("got %s", got)
	}
	if !report["differs"].(bool) {
		t.Fatal("not differing")
	}
}

// Packs whose ids differ are compared, flagged, and the id is a changed
// member; packs whose specVersions differ are refused, in both forms, with
// an error that is not a Refusal.
func TestIdsAndSpecVersions(t *testing.T) {
	compared := twoWay(t, expense{}, expense{id: "https://example.invalid/packs/purchase-approval"})
	if !compared.DifferentDecisions || !strings.HasPrefix(string(compared.Text()), "DIFFERENT DECISIONS:") {
		t.Fatalf("%s", compared.Text())
	}
	report := decoded(t, compared.JSON())
	if report["differentDecisions"] != true || !strings.Contains(encoded(t, report["pack"]), `"pointer":"/id"`) {
		t.Fatalf("%v", report)
	}
	if _, present := decoded(t, twoWay(t, expense{}, expense{}).JSON())["differentDecisions"]; present {
		t.Fatal("differentDecisions is written for one id")
	}
	core030 := expense{spec: "0.3.0"}.load(t)
	core050 := expense{}.load(t)
	_, err := sdk.Diff(core030, core050)
	var refusal *sdk.Refusal
	if !errors.Is(err, pv.ErrDiffSpecVersion) || errors.As(err, &refusal) || errors.Is(err, sdk.ErrInternal) {
		t.Fatalf("got %v", err)
	}
	if _, err := sdk.ThreeWayDiff(core050, core050, core030); !errors.Is(err, pv.ErrDiffSpecVersion) {
		t.Fatalf("three-way: got %v", err)
	}
	if _, err := sdk.Diff(nil, core050); err == nil || errors.Is(err, sdk.ErrInternal) {
		t.Fatalf("nil pack: got %v", err)
	}
	if _, err := sdk.ThreeWayDiff(core050, &sdk.Pack{}, core050); err == nil || errors.Is(err, sdk.ErrInternal) {
		t.Fatalf("zero pack: got %v", err)
	}
}

// Under a specVersion with no annotations and no value sets, those members
// of the comparison are absent, not empty, and an extension is compared as
// one value under its name.
func TestAVersionWithoutAnnotations(t *testing.T) {
	older := expense{spec: "0.3.0", declineExtensions: `{"com.example.review":{"severity":"medium"}}`}
	newer := expense{spec: "0.3.0", declineExtensions: `{"com.example.review":{"severity":"high"}}`}
	report := decoded(t, twoWay(t, older, newer).JSON())
	if _, present := report["valueSets"]; present {
		t.Fatal("valueSets is written under 0.3.0")
	}
	if got := encoded(t, report["pack"]); got != `{"members":[]}` {
		t.Fatalf("pack: %s", got)
	}
	if got := encoded(t, changesOf(t, report, "outcomes")); got != `[{"change":"changed","id":"decline","members":[{"change":"changed","new":{"severity":"high"},"old":{"severity":"medium"},"pointer":"/extensions/com.example.review"}]}]` {
		t.Fatalf("outcomes: %s", got)
	}
	three := decoded(t, threeWay(t, older, newer, older).JSON())
	if _, present := three["valueSets"]; present {
		t.Fatal("valueSets is written under 0.3.0, three-way")
	}
}

// Every string taken from a pack is written in the text as JSON in printable
// ASCII, so every code point can be read back.
func TestTheTextIsPrintableASCII(t *testing.T) {
	older := expense{declineExtensions: `{"org.judgmentpack.annotations":{"label":"Caf\u00e9"}}`}
	newer := expense{declineExtensions: `{"org.judgmentpack.annotations":{"label":"Cafe\u0301 \ud83d\ude00"}}`}
	text := string(twoWay(t, older, newer).Text())
	for index, character := range text {
		if character != '\n' && (character < 0x20 || character > 0x7e) {
			t.Fatalf("byte %d is %U:\n%s", index, character, text)
		}
	}
	if !strings.Contains(text, `annotation "label" changed: "Caf\u00e9" -> "Cafe\u0301 \ud83d\ude00"`) {
		t.Fatalf("%s", text)
	}
}

// A set is a set inside a value reported whole too: a descendant that only
// reorders escalation triggers, or a route's, has not changed what holds
// them, so when the other descendant removes the escalation, or a route,
// only the other changed it.
func TestASetInsideAWholeValueIsStillASet(t *testing.T) {
	escalation := func(triggers string) expense {
		return expense{escalation: `{"triggers":` + triggers + `,"target":{"kind":"queue","name":"q"}}`}
	}
	base, reordered := escalation(`["unknown","no-match"]`), escalation(`["no-match","unknown"]`)
	if compared := twoWay(t, base, reordered); compared.Differs {
		t.Fatalf("a set's order was reported:\n%s", compared.Text())
	}
	compared := threeWay(t, base, reordered, expense{})
	members := encoded(t, decoded(t, compared.JSON())["pack"].(map[string]any)["members"])
	if want := `[{"base":{"target":{"kind":"queue","name":"q"},"triggers":["unknown","no-match"]},"first":{"target":{"kind":"queue","name":"q"},"triggers":["no-match","unknown"]},"pointer":"/escalation","status":"second"}]`; members != want || compared.Conflicts != 0 {
		t.Fatalf("escalation removed by second\ngot:  %s (conflicts %d)\nwant: %s", members, compared.Conflicts, want)
	}
	routed := func(queueTriggers string, routes int) expense {
		all := []string{`{"target":{"kind":"queue","name":"Review"},"triggers":` + queueTriggers + `}`, `{"target":{"kind":"human-role","name":"Oversight"}}`}
		return expense{escalation: `{"triggers":["unknown","conflict","no-match"],"routes":[` + strings.Join(all[:routes], ",") + `]}`}
	}
	compared = threeWay(t, routed(`["unknown","conflict"]`, 2), routed(`["conflict","unknown"]`, 2), routed(`["unknown","conflict"]`, 1))
	changes := decoded(t, compared.JSON())["pack"].(map[string]any)["members"].([]any)
	if len(changes) != 1 || changes[0].(map[string]any)["pointer"] != "/escalation/routes" || changes[0].(map[string]any)["status"] != "second" || compared.Conflicts != 0 {
		t.Fatalf("route removed by second: %s (conflicts %d)", encoded(t, changes), compared.Conflicts)
	}
}

// pointers lists every member pointer a comparison reports, at any depth.
func pointers(value any) []string {
	found := []string{}
	switch typed := value.(type) {
	case map[string]any:
		if pointer, ok := typed["pointer"].(string); ok {
			found = append(found, pointer)
		}
		for _, member := range typed {
			found = append(found, pointers(member)...)
		}
	case []any:
		for _, item := range typed {
			found = append(found, pointers(item)...)
		}
	}
	return found
}

// An extensions object that holds only annotations is the empty object it is
// without them, at each position that carries annotations: against
// "extensions": {} it is the same object, both ways and three-way, and
// against an absent one its coming or going is reported once, as the
// annotation. An empty object against an absent one is still a change.
func TestAnExtensionsObjectHoldingOnlyAnnotations(t *testing.T) {
	entry := func(extensions string) expense {
		written := `{"value":"EUR"}`
		if extensions != "" {
			written = `{"value":"EUR","extensions":` + extensions + `}`
		}
		return expense{extraRules: currencyRule, valueSets: `[{"id":"currencies","entries":[` + written + `]}]`}
	}
	annotated := `{"org.judgmentpack.annotations":{"k":"v"}}`
	for _, position := range []struct {
		name string
		with func(string) expense
	}{
		{"outcome", func(extensions string) expense { return expense{declineExtensions: extensions} }},
		{"rule", func(extensions string) expense { return expense{ruleExtensions: extensions} }},
		{"pack", func(extensions string) expense { return expense{rootExtensions: extensions} }},
		{"entry", entry},
	} {
		t.Run(position.name, func(t *testing.T) {
			for _, row := range []struct {
				name          string
				older, newer  string
				pointers, say string
			}{
				{"empty to annotated", "{}", annotated, "", `annotation "k" added: "v"`},
				{"annotated to empty", annotated, "{}", "", `annotation "k" removed: "v"`},
				{"absent to annotated", "", annotated, "", `annotation "k" added: "v"`},
				{"annotated to absent", annotated, "", "", `annotation "k" removed: "v"`},
				{"absent to empty", "", "{}", "/extensions", "/extensions added: {}"},
				{"empty to absent", "{}", "", "/extensions", "/extensions removed: {}"},
			} {
				compared := twoWay(t, position.with(row.older), position.with(row.newer))
				if got := strings.Join(pointers(decoded(t, compared.JSON())), ","); got != row.pointers || !strings.Contains(string(compared.Text()), row.say+"\n") {
					t.Fatalf("%s: pointers %q, want %q\n%s", row.name, got, row.pointers, compared.Text())
				}
			}
			compared := threeWay(t, position.with("{}"), position.with(annotated), position.with("{}"))
			if got := pointers(decoded(t, compared.JSON())); len(got) != 0 || compared.Conflicts != 0 || !strings.Contains(string(compared.Text()), `annotation "k" first: base absent, first "v", second absent`+"\n") {
				t.Fatalf("three-way: pointers %q, conflicts %d\n%s", got, compared.Conflicts, compared.Text())
			}
		})
	}
}

const mealRule = `{"id":"meal","description":"Meals are approved.","when":{"op":"fact","path":"/expense/kind","operator":"equals","value":"meal"},"outcome":"approve","onUnknown":"ignore"}`

// The three-way order is read over the members both descendants hold, so a
// rule both add, at different places, is an order conflict, as the two-way
// comparison of the descendants says it is an order change; where they add
// it at one place, or only one adds it, there is no order difference.
func TestThreeWayOrderAmongAddedMembers(t *testing.T) {
	base := expense{extraRules: travelRule}
	last, middle := expense{extraRules: travelRule + "," + mealRule}, expense{extraRules: mealRule + "," + travelRule}
	if got := encoded(t, decoded(t, twoWay(t, last, middle).JSON())["rules"].(map[string]any)["order"]); got != `{"new":["over-limit","meal","travel"],"old":["over-limit","travel","meal"]}` {
		t.Fatalf("two-way order: %s", got)
	}
	compared := threeWay(t, base, last, middle)
	rules := decoded(t, compared.JSON())["rules"].(map[string]any)
	if got, want := encoded(t, rules["order"]), `{"base":["over-limit","travel"],"first":["over-limit","travel","meal"],"second":["over-limit","meal","travel"],"status":"conflict"}`; got != want || compared.Conflicts != 1 {
		t.Fatalf("three-way order\ngot:  %s (conflicts %d)\nwant: %s", got, compared.Conflicts, want)
	}
	if got := encoded(t, rules["changes"]); !strings.Contains(got, `"id":"meal"`) || !strings.Contains(got, `"status":"both"`) {
		t.Fatalf("changes: %s", got)
	}
	if !strings.Contains(string(compared.Text()), `  rules order conflict: base ["over-limit","travel"], first ["over-limit","travel","meal"], second ["over-limit","meal","travel"]`+"\n") {
		t.Fatalf("%s", compared.Text())
	}
	for _, row := range []struct {
		name          string
		first, second expense
	}{
		{"both at one place", middle, middle},
		{"only first adds", last, base},
		{"only second adds", base, middle},
	} {
		if order, present := decoded(t, threeWay(t, base, row.first, row.second).JSON())["rules"].(map[string]any)["order"]; present {
			t.Fatalf("%s: an order difference was reported: %v", row.name, order)
		}
	}
	moved := expense{extraRules: mealRule + "," + travelRule}
	if got := encoded(t, decoded(t, threeWay(t, expense{extraRules: travelRule + "," + mealRule}, moved, moved).JSON())["rules"].(map[string]any)["order"]); !strings.Contains(got, `"status":"both"`) {
		t.Fatalf("both moved: %s", got)
	}
}
