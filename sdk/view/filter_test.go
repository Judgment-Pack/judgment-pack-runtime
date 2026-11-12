package view_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
	pv "github.com/Judgment-Pack/judgment-pack-runtime/sdk/view"
)

// filterPack is written by hand for the filter tests: annotations on the pack,
// on two of three outcomes (one a one-element list) and on four of five
// rules, with a string, a list, a key that differs only in case, and a value
// written once composed (U+00E9) and once decomposed (e, U+0301).
const filterPack = `{
  "specVersion": "0.5.0",
  "id": "https://example.com/packs/filtered",
  "version": "1.0.0",
  "title": "Filtered *pack*",
  "decision": {"intent": "Route", "question": "Route?"},
  "extensions": {"org.judgmentpack.annotations": {"team": "intake"}},
  "outcomes": [
    {"id": "accept", "label": "Accept", "extensions": {"org.judgmentpack.annotations": {"team": "intake", "tier": ["one"]}}},
    {"id": "refer", "label": "Refer", "extensions": {"org.judgmentpack.annotations": {"team": "review"}}},
    {"id": "hold", "label": "Hold"}
  ],
  "rules": [
    {"id": "a", "description": "A", "when": {"op": "fact", "path": "/x", "operator": "equals", "value": "1"}, "outcome": "accept", "onUnknown": "ignore",
     "extensions": {"org.judgmentpack.annotations": {"team": "intake", "tier": "one"}}},
    {"id": "b", "description": "B", "when": {"op": "fact", "path": "/x", "operator": "equals", "value": "2"}, "outcome": "refer", "onUnknown": "ignore",
     "extensions": {"org.judgmentpack.annotations": {"team": ["intake", "review"]}}},
    {"id": "c", "description": "C", "when": {"op": "fact", "path": "/x", "operator": "equals", "value": "3"}, "outcome": "hold", "onUnknown": "ignore",
     "extensions": {"org.judgmentpack.annotations": {"team": "Intake", "label": "Caf\u00e9"}}},
    {"id": "d", "description": "D", "when": {"op": "fact", "path": "/x", "operator": "equals", "value": "4"}, "outcome": "hold", "onUnknown": "ignore",
     "extensions": {"org.judgmentpack.annotations": {"label": "Cafe\u0301"}}},
    {"id": "e", "description": "E", "when": {"op": "fact", "path": "/x", "operator": "equals", "value": "5"}, "outcome": "hold", "onUnknown": "ignore"}
  ]
}`

func loadFilterPack(t *testing.T, text string) *sdk.Pack {
	t.Helper()
	loaded, err := sdk.Load(context.Background(), []byte(text), sdk.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func criteria(t *testing.T, texts ...string) []pv.Criterion {
	t.Helper()
	parsed := make([]pv.Criterion, 0, len(texts))
	for _, text := range texts {
		criterion, err := pv.ParseCriterion(text)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		parsed = append(parsed, criterion)
	}
	return parsed
}

func matchedIDs(filtered pv.FilteredView) (outcomes, rules []string) {
	for _, matched := range filtered.Outcomes {
		outcomes = append(outcomes, matched.Outcome.ID)
	}
	for _, matched := range filtered.Rules {
		rules = append(rules, matched.Rule.ID)
	}
	return outcomes, rules
}

// The whole of one filtered view, in both encodings, written by hand: two
// criteria, given out of order, which an outcome and a rule match together
// and no other member matches both of.
func TestOneFilteredViewByHand(t *testing.T) {
	loaded := loadFilterPack(t, filterPack)
	filtered, err := loaded.FilteredView(criteria(t, "tier=one", "team=intake"))
	if err != nil {
		t.Fatal(err)
	}
	digest := loaded.Identity().Digest
	wantJSON := `{"digest":"` + digest + `","filter":{"criteria":[{"key":"team","value":"intake"},{"key":"tier","value":"one"}],"match":"all"},` +
		`"filteredViewVersion":"1","id":"https://example.com/packs/filtered",` +
		`"outcomes":{"count":3,"matched":[{"outcome":{"extensions":{"org.judgmentpack.annotations":{"team":"intake","tier":["one"]}},"id":"accept","label":"Accept"},"pointer":"/outcomes/0"}]},` +
		`"rules":{"count":5,"matched":[{"pointer":"/rules/0","rule":{"description":"A","extensions":{"org.judgmentpack.annotations":{"team":"intake","tier":"one"}},` +
		`"id":"a","onUnknown":"ignore","outcome":"accept","when":{"op":"fact","operator":"equals","path":"/x","value":"1"}}}]},` +
		`"specVersion":"0.5.0","title":"Filtered *pack*","version":"1.0.0"}`
	if got := string(filtered.JSON()); got != wantJSON {
		t.Fatalf("JSON:\n got %s\nwant %s", got, wantJSON)
	}
	wantMarkdown := "# Filtered view: Filtered \\*pack\\*\n" +
		"\n" +
		"This is not the pack. It shows only the outcomes and rules whose annotations match every criterion below, each at its position in the pack, and leaves out every other member.\n" +
		"\n" +
		"- Id: https\\:\\/\\/example\\.com\\/packs\\/filtered\n" +
		"- Version: 1\\.0\\.0\n" +
		"- Spec version: 0\\.5\\.0\n" +
		"- Digest: " + digest + "\n" +
		"- Filtered view version: 1\n" +
		"- Filter, every criterion matching:\n" +
		"  - team is intake\n" +
		"  - tier is one\n" +
		"- Outcomes matched: 1 of 3\n" +
		"- Rules matched: 1 of 5\n" +
		"\n" +
		"## Outcomes\n" +
		"\n" +
		"### Outcome 1 of 3: accept\n" +
		"\n" +
		"- Label: Accept\n" +
		"- Extensions:\n" +
		"  - org\\.judgmentpack\\.annotations:\n" +
		"    - team: intake\n" +
		"    - tier:\n" +
		"      - one\n" +
		"\n" +
		"## Rules\n" +
		"\n" +
		"### Rule 1 of 5: a\n" +
		"\n" +
		"- Description: A\n" +
		"- Outcome: accept\n" +
		"- On unknown: ignore\n" +
		"- Extensions:\n" +
		"  - org\\.judgmentpack\\.annotations:\n" +
		"    - team: intake\n" +
		"    - tier: one\n" +
		"- When:\n" +
		"  - fact \\\"\\/x\\\" equals \\\"1\\\"\n"
	if got := string(filtered.Markdown()); got != wantMarkdown {
		t.Fatalf("Markdown:\n got %q\nwant %q", got, wantMarkdown)
	}
	// It is not the pack: no reader of views takes it for one.
	var members map[string]json.RawMessage
	if err := json.Unmarshal(filtered.JSON(), &members); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pack", "viewVersion"} {
		if _, present := members[name]; present {
			t.Fatalf("the filtered view has a %s member", name)
		}
	}
	// The same filter in another order is the same filtered view.
	again, err := loaded.FilteredView(criteria(t, "team=intake", "tier=one"))
	if err != nil || string(again.JSON()) != wantJSON || string(again.Markdown()) != wantMarkdown {
		t.Fatalf("the criteria's order changed the filtered view: %v", err)
	}
}

// Each criterion must match (all, never any); a list matches when it holds
// the value, a one-element list as well as a string; keys and values compare
// by exact code point, with no case folding and no normalization; and the
// matched members keep document order and their positions in the pack.
func TestFilterMatchesEveryCriterionByExactCodePoint(t *testing.T) {
	loaded := loadFilterPack(t, filterPack)
	for _, test := range []struct {
		criteria []string
		outcomes []string
		rules    []string
		indexes  []int
	}{
		{[]string{"team=intake"}, []string{"accept"}, []string{"a", "b"}, []int{0, 1}},
		{[]string{"team=review"}, []string{"refer"}, []string{"b"}, []int{1}},
		{[]string{"team=intake", "team=review"}, nil, []string{"b"}, []int{1}},
		{[]string{"tier=one"}, []string{"accept"}, []string{"a"}, []int{0}},
		{[]string{"team=Intake"}, nil, []string{"c"}, []int{2}},
		{[]string{"label=Caf\u00e9"}, nil, []string{"c"}, []int{2}},
		{[]string{"label=Cafe\u0301"}, nil, []string{"d"}, []int{3}},
		{[]string{"label=Cafe"}, nil, nil, nil},
		{[]string{"team=in take"}, nil, nil, nil},
		{[]string{"team=intak"}, nil, nil, nil},
		{[]string{"owner=intake"}, nil, nil, nil},
	} {
		filtered, err := loaded.FilteredView(criteria(t, test.criteria...))
		if err != nil {
			t.Fatalf("%q: %v", test.criteria, err)
		}
		outcomes, rules := matchedIDs(filtered)
		if strings.Join(outcomes, ",") != strings.Join(test.outcomes, ",") || strings.Join(rules, ",") != strings.Join(test.rules, ",") {
			t.Fatalf("%q matched outcomes %q and rules %q, want %q and %q", test.criteria, outcomes, rules, test.outcomes, test.rules)
		}
		for index, matched := range filtered.Rules {
			if matched.Index != test.indexes[index] {
				t.Fatalf("%q: rule %s is at index %d, want %d", test.criteria, matched.Rule.ID, matched.Index, test.indexes[index])
			}
		}
		if filtered.OutcomeCount != 3 || filtered.RuleCount != 5 {
			t.Fatalf("%q: counts %d and %d", test.criteria, filtered.OutcomeCount, filtered.RuleCount)
		}
		if len(test.rules) == 0 && !strings.Contains(string(filtered.Markdown()), "- Rules matched: 0 of 5\n\n## Outcomes\n") {
			t.Fatalf("%q: no-match Markdown:\n%s", test.criteria, filtered.Markdown())
		}
	}
}

// A criterion and the value it is compared with are escaped as every pack
// string is, and a criterion holding a code point outside printable ASCII is
// written in ASCII beside it, so a format character or a look-alike in the
// filter is visible. U+FFF9 is a format character that annotation strings
// admit, because it is not default-ignorable. Every line opens with the
// renderer's own text.
func TestTheFilterIsEscapedAndWrittenInASCIIToo(t *testing.T) {
	pack := strings.Replace(filterPack, `{"team": "intake", "tier": "one"}`, `{"team": "intake", "note": "<b>*x*</b> a\ufff9b"}`, 1)
	loaded := loadFilterPack(t, pack)
	filtered, err := loaded.FilteredView(criteria(t, "note=<b>*x*</b> a\ufff9b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, rules := matchedIDs(filtered); strings.Join(rules, ",") != "a" {
		t.Fatalf("matched %q", rules)
	}
	markdown := string(filtered.Markdown())
	want := "  - note is \\<b\\>\\*x\\*\\<\\/b\\> a`U+FFF9`b (ASCII `\"<b>*x*</b> a\\ufff9b\"`)\n"
	if !strings.Contains(markdown, want) {
		t.Fatalf("the criterion is not written as %q:\n%s", want, markdown)
	}
	for number, line := range strings.Split(strings.TrimSuffix(markdown, "\n"), "\n") {
		if line == "" {
			continue
		}
		trimmed := strings.TrimLeft(line, " ")
		if !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "- ") && !strings.HasPrefix(trimmed, "This is not the pack.") && !strings.HasPrefix(trimmed, "*none matched*") {
			t.Fatalf("line %d opens with pack text: %q", number+1, line)
		}
	}
}

// A criterion no annotation could hold is refused, with an error wrapping
// ErrFilterCriterion, before the pack is looked at; a pack whose specVersion
// defines no annotations is refused with one wrapping ErrFilterSelector, never
// shown with nothing matched. Neither is a *sdk.Refusal or an ErrInternal, and
// neither returns a filtered view.
func TestTheFilterRefusesWhatItCannotMatch(t *testing.T) {
	annotated := loadFilterPack(t, filterPack)
	core := loadFilterPack(t, viewMinimalPack)
	draft := loadFilterPack(t, strings.Replace(viewMinimalPack, `"specVersion": "0.3.0"`, `"specVersion": "0.2.0-draft"`, 1))
	for _, test := range []struct {
		name     string
		pack     *sdk.Pack
		criteria []pv.Criterion
		want     error
	}{
		{"no criterion", annotated, nil, pv.ErrFilterCriterion},
		{"an empty key", annotated, []pv.Criterion{{Key: "", Value: "x"}}, pv.ErrFilterCriterion},
		{"an upper-case key", annotated, []pv.Criterion{{Key: "Team", Value: "x"}}, pv.ErrFilterCriterion},
		{"a key over 64 characters", annotated, []pv.Criterion{{Key: strings.Repeat("a", 65), Value: "x"}}, pv.ErrFilterCriterion},
		{"an empty value", annotated, []pv.Criterion{{Key: "team", Value: ""}}, pv.ErrFilterCriterion},
		{"a value over 256 characters", annotated, []pv.Criterion{{Key: "team", Value: strings.Repeat("\u00e9", 257)}}, pv.ErrFilterCriterion},
		{"a value that is not UTF-8", annotated, []pv.Criterion{{Key: "team", Value: "a\xffb"}}, pv.ErrFilterCriterion},
		{"a value with a control character", annotated, []pv.Criterion{{Key: "team", Value: "a\u0007b"}}, pv.ErrFilterCriterion},
		{"a value with a line separator", annotated, []pv.Criterion{{Key: "team", Value: "a\u2028b"}}, pv.ErrFilterCriterion},
		{"a value with a bidi mark", annotated, []pv.Criterion{{Key: "team", Value: "a\u200fb"}}, pv.ErrFilterCriterion},
		{"a value with an isolate", annotated, []pv.Criterion{{Key: "team", Value: "a\u2067b"}}, pv.ErrFilterCriterion},
		{"a value with a zero-width space", annotated, []pv.Criterion{{Key: "team", Value: "intake\u200b"}}, pv.ErrFilterCriterion},
		{"a value with a tag character", annotated, []pv.Criterion{{Key: "team", Value: "intake\U000e0041"}}, pv.ErrFilterCriterion},
		{"a value that ends in a space", annotated, []pv.Criterion{{Key: "team", Value: "intake "}}, pv.ErrFilterCriterion},
		{"a value that begins with a no-break space", annotated, []pv.Criterion{{Key: "team", Value: "\u00a0intake"}}, pv.ErrFilterCriterion},
		{"a value of white space only", annotated, []pv.Criterion{{Key: "team", Value: "\u3000"}}, pv.ErrFilterCriterion},
		{"a repeated criterion", annotated, []pv.Criterion{{Key: "team", Value: "x"}, {Key: "team", Value: "x"}}, pv.ErrFilterCriterion},
		{"a bad criterion on a 0.3.0 pack", core, []pv.Criterion{{Key: "Team", Value: "x"}}, pv.ErrFilterCriterion},
		{"a 0.3.0 pack", core, []pv.Criterion{{Key: "team", Value: "intake"}}, pv.ErrFilterSelector},
		{"a 0.2.0-draft pack", draft, []pv.Criterion{{Key: "team", Value: "intake"}}, pv.ErrFilterSelector},
	} {
		t.Run(test.name, func(t *testing.T) {
			filtered, err := test.pack.FilteredView(test.criteria)
			if !errors.Is(err, test.want) {
				t.Fatalf("the error %v does not wrap %v", err, test.want)
			}
			var refusal *sdk.Refusal
			if errors.As(err, &refusal) || errors.Is(err, sdk.ErrInternal) {
				t.Fatalf("the error %v is a refusal or a fault", err)
			}
			if filtered.JSON() != nil || filtered.Markdown() != nil || filtered.Rules != nil {
				t.Fatalf("a filtered view came back: %+v", filtered)
			}
		})
	}
	var zero pv.FilteredView
	if zero.JSON() != nil || zero.Markdown() != nil {
		t.Fatal("the zero filtered view has encodings")
	}
	if _, err := (*sdk.Pack)(nil).FilteredView(criteria(t, "team=intake")); err == nil {
		t.Fatal("a nil Pack filtered")
	}
}

// ParseCriterion splits at the first equals sign, so a value may hold one,
// and refuses text with none.
func TestParseCriterion(t *testing.T) {
	if got, err := pv.ParseCriterion("team=a=b,c"); err != nil || got != (pv.Criterion{Key: "team", Value: "a=b,c"}) {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, text := range []string{"team", "=intake", "team="} {
		if _, err := pv.ParseCriterion(text); !errors.Is(err, pv.ErrFilterCriterion) {
			t.Fatalf("%q: %v", text, err)
		}
	}
}

// The criterion check mirrors the Core 0.5.0 schema's annotation key and string:
// for each key and value written here, a criterion is accepted exactly when a
// pack holding that annotation is admitted.
func TestTheCriterionCheckIsTheSchemas(t *testing.T) {
	keys := []string{"a", "a1", "a-b", "a-1", "ab-cd-ef", strings.Repeat("a", 64), strings.Repeat("a", 65), "1a", "A", "aB", "a--b", "a-", "-a", "a_b", "a.b", "a b", "\u00e9"}
	values := []string{"x", " ", "a=b", strings.Repeat("\u00e9", 256), strings.Repeat("\u00e9", 257), "\u0000", "\u001f", "\u007f", "\u0085", "\u009f", "\u00a0", "\u2027", "\u2028", "\u202a", "\u202e", "\u202f", "\u2067", "\U0001F600", "\ufeff"}
	check := func(key, value string) {
		t.Helper()
		encodedKey, _ := json.Marshal(key)
		encodedValue, _ := json.Marshal(value)
		pack := strings.Replace(filterPack, `"extensions": {"org.judgmentpack.annotations": {"team": "intake"}},`,
			`"extensions": {"org.judgmentpack.annotations": {`+string(encodedKey)+`: `+string(encodedValue)+`}},`, 1)
		_, loadErr := sdk.Load(context.Background(), []byte(pack), sdk.LoadOptions{})
		_, criterionErr := pv.ParseCriterion(key + "=" + value)
		if (loadErr == nil) != (criterionErr == nil) {
			t.Fatalf("key %q value %q: the pack's load says %v and the criterion check %v", key, value, loadErr, criterionErr)
		}
	}
	for _, key := range keys {
		check(key, "x")
	}
	for _, value := range values {
		check("team", value)
	}
}

// draftRefused and draftWhiteSpace are copied by hand from the Core 0.5.0
// text (§9.2, "Annotation strings"): the code points no annotation string may
// hold, and the white-space characters it may not begin or end with.
var (
	draftRefused = [][2]rune{
		{0x0000, 0x001f}, {0x007f, 0x009f}, {0x00ad, 0x00ad}, {0x034f, 0x034f}, {0x061c, 0x061c},
		{0x115f, 0x1160}, {0x17b4, 0x17b5}, {0x180b, 0x180f}, {0x200b, 0x200f}, {0x2028, 0x202e},
		{0x2060, 0x206f}, {0x3164, 0x3164}, {0xfe00, 0xfe0f}, {0xfeff, 0xfeff}, {0xffa0, 0xffa0},
		{0xfff0, 0xfff8}, {0x1bca0, 0x1bca3}, {0x1d173, 0x1d17a}, {0xe0000, 0xe0fff},
	}
	draftWhiteSpace = [][2]rune{
		{0x0009, 0x000d}, {0x0020, 0x0020}, {0x0085, 0x0085}, {0x00a0, 0x00a0}, {0x1680, 0x1680},
		{0x2000, 0x200a}, {0x2028, 0x2029}, {0x202f, 0x202f}, {0x205f, 0x205f}, {0x3000, 0x3000},
	}
)

func inDraftList(list [][2]rune, character rune) bool {
	for _, span := range list {
		if character >= span[0] && character <= span[1] {
			return true
		}
	}
	return false
}

// The criterion check refuses exactly the strings the draft's character rules
// refuse, at both ends of every range: each end of each refused range, and
// the code point on either side of it, inside a string; and each white-space
// range's ends and neighbours at the start, at the end, inside and alone. The
// expectation is the text's lists above; the released schema, through a pack
// that carries the same string, must agree with it too.
func TestTheCriterionCheckIsTheDrafts(t *testing.T) {
	type value struct {
		text     string
		admitted bool
	}
	var values []value
	for _, span := range draftRefused {
		for _, character := range []rune{span[0] - 1, span[0], span[1], span[1] + 1} {
			if character < 0 {
				continue
			}
			values = append(values, value{"a" + string(character) + "b", !inDraftList(draftRefused, character)})
		}
	}
	for _, span := range draftWhiteSpace {
		for _, character := range []rune{span[0] - 1, span[0], span[1], span[1] + 1} {
			refused, white := inDraftList(draftRefused, character), inDraftList(draftWhiteSpace, character)
			values = append(values,
				value{string(character) + "a", !refused && !white},
				value{"a" + string(character), !refused && !white},
				value{"a" + string(character) + "b", !refused},
				value{string(character), !refused && !white})
		}
	}
	for _, test := range values {
		_, criterionErr := pv.ParseCriterion("team=" + test.text)
		if (criterionErr == nil) != test.admitted {
			t.Errorf("value %q: the criterion check says %v, want admitted=%v", test.text, criterionErr, test.admitted)
		}
		encoded, _ := json.Marshal(test.text)
		pack := strings.Replace(filterPack, `"extensions": {"org.judgmentpack.annotations": {"team": "intake"}},`,
			`"extensions": {"org.judgmentpack.annotations": {"team": `+string(encoded)+`}},`, 1)
		if _, loadErr := sdk.Load(context.Background(), []byte(pack), sdk.LoadOptions{}); (loadErr == nil) != test.admitted {
			t.Errorf("value %q: the pack's load says %v, want admitted=%v", test.text, loadErr, test.admitted)
		}
	}
}

// Each refusal of a criterion's value names the code point by its U+ number,
// never as itself, and the first refused one wins over white space at an
// end.
func TestTheCriterionRefusalsNameTheCodePoint(t *testing.T) {
	for _, test := range []struct{ text, want string }{
		{"team=intake\u200b", "view: invalid filter criterion: the value for team holds U+200B, which no annotation string may hold"},
		{"team=a\U000e0041", "view: invalid filter criterion: the value for team holds U+E0041, which no annotation string may hold"},
		{"team= \u200dx", "view: invalid filter criterion: the value for team holds U+200D, which no annotation string may hold"},
		{"team=\u00a0intake", "view: invalid filter criterion: the value for team begins with U+00A0, white space, which no annotation string may begin or end with"},
		{"team=intake\u3000", "view: invalid filter criterion: the value for team ends with U+3000, white space, which no annotation string may begin or end with"},
		{"team= ", "view: invalid filter criterion: the value for team begins with U+0020, white space, which no annotation string may begin or end with"},
		{"t\u0430am=x", "view: invalid filter criterion: the key \"t\\u0430am\" is not an annotation key, which is lower-case letters and digits in hyphen-separated words, starting with a letter, at most 64 characters"},
	} {
		_, err := pv.ParseCriterion(test.text)
		if err == nil || err.Error() != test.want || !errors.Is(err, pv.ErrFilterCriterion) {
			t.Errorf("%q: got %v\nwant %s", test.text, err, test.want)
		}
	}
}
