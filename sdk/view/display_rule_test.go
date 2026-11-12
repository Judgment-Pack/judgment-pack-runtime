package view_test

import (
	"encoding/json"
	"strings"
	"testing"

	pv "github.com/Judgment-Pack/judgment-pack-runtime/sdk/view"
)

// displayPack is a Core 0.5.0 pack written by hand around two annotation strings:
// rule a carries value, and rule b a list that holds twin.
func displayPack(t *testing.T, value, twin string) string {
	t.Helper()
	encode := func(text string) string {
		encoded, err := json.Marshal(text)
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	return `{
  "specVersion": "0.5.0",
  "id": "https://example.com/packs/display",
  "version": "1.0.0",
  "title": "Display",
  "decision": {"intent": "Route", "question": "Route?"},
  "outcomes": [{"id": "accept", "label": "Accept"}, {"id": "hold", "label": "Hold"}],
  "rules": [
    {"id": "a", "description": "A", "when": {"op": "fact", "path": "/x", "operator": "equals", "value": "1"}, "outcome": "accept", "onUnknown": "ignore",
     "extensions": {"org.judgmentpack.annotations": {"label": ` + encode(value) + `}}},
    {"id": "b", "description": "B", "when": {"op": "fact", "path": "/x", "operator": "equals", "value": "2"}, "outcome": "hold", "onUnknown": "ignore",
     "extensions": {"org.judgmentpack.annotations": {"label": [` + encode(twin) + `, "z"]}}}
  ]
}`
}

// printableASCII keeps only the characters a reader sees the same in every
// font: U+0020 to U+007E.
func printableASCII(text string) string {
	return strings.Map(func(character rune) rune {
		if character < 0x20 || character > 0x7e {
			return -1
		}
		return character
	}, text)
}

// rendered is what a reader is sure to see of a line in every renderer and
// font: its printable ASCII, U+0020 to U+007E, with each run of spaces
// collapsed to one, as an HTML renderer collapses it.
func rendered(text string) string {
	text = printableASCII(text)
	for strings.Contains(text, "  ") {
		text = strings.ReplaceAll(text, "  ", " ")
	}
	return text
}

// asciiFormOf returns the ASCII form a Markdown line ends with, "(ASCII `...`)"
// beside the string or "(ASCII only `...`)" in its place, and the JSON string
// its code span holds.
func asciiFormOf(t *testing.T, line string) (form, span string) {
	t.Helper()
	start := strings.LastIndex(line, "(ASCII ")
	if start < 0 || !strings.HasSuffix(line, "`)") {
		t.Fatalf("no ASCII form in %q", line)
	}
	form = line[start:]
	open := strings.Index(form, "`")
	if open < 0 {
		t.Fatalf("no code span in %q", form)
	}
	return form, form[open+1 : len(form)-len("`)")]
}

// recovered decodes the ASCII form a Markdown line ends with, by its own
// syntax: a JSON string in a code span.
func recovered(t *testing.T, line string) string {
	t.Helper()
	_, span := asciiFormOf(t, line)
	var value string
	if err := json.Unmarshal([]byte(span), &value); err != nil {
		t.Fatalf("the ASCII form in %q: %v", line, err)
	}
	return value
}

// D2 (RFC 0031 section 8, Core 0.5.0 section 9.2): wherever annotation values are
// listed or a filter is stated, every character outside printable ASCII is
// shown so that the reader can recover each code point. In the Markdown the
// view's annotation line, the filtered view's statement of the filter and its
// match each write the string escaped and then in ASCII, which holds nothing
// but printable ASCII and decodes to the string exactly; a string that looks
// like it never reads alike in printable ASCII. In the JSON each is the exact
// string. Each class is an admitted annotation string, and each line it must
// get is written here by hand. A key is printable ASCII by the key grammar.
//
// D1 and issue #380 (ADR-0078): a string whose characters can reorder the
// text beside it (bidirectional class R, AL or AN) is written only in ASCII,
// so its line holds none of them; and a string holding a run of spaces, which
// a renderer collapses, gets its ASCII form with each space of the run
// written \u0020, so it never renders like the string with one space.
func TestAnnotationStringsAreShownSoEveryCodePointCanBeRecovered(t *testing.T) {
	for _, test := range []struct {
		name, value, twin, shown string
	}{
		{"a Cyrillic look-alike", "c\u0430fe", "cafe", "c\u0430fe (ASCII `\"c\\u0430fe\"`)"},
		{"a combining mark", "cafe\u0301", "caf\u00e9", "cafe\u0301 (ASCII `\"cafe\\u0301\"`)"},
		{"an inner no-break space", "in\u00a0take", "in take", "in\u00a0take (ASCII `\"in\\u00a0take\"`)"},
		{"an inner em space", "in\u2003take", "in take", "in\u2003take (ASCII `\"in\\u2003take\"`)"},
		{"a right-to-left letter", "x\u05d0", "x", "(ASCII only `\"x\\u05d0\"`)"},
		{"an Arabic letter", "x\u0628", "x", "(ASCII only `\"x\\u0628\"`)"},
		{"an Arabic digit", "tier \u0662", "tier 2", "(ASCII only `\"tier \\u0662\"`)"},
		{"right-to-left text beside digits and neutrals", "\u05e9\u05dc\u05d5\u05dd 2-1", "2-1", "(ASCII only `\"\\u05e9\\u05dc\\u05d5\\u05dd 2-1\"`)"},
		{"right-to-left text and a run of spaces", "(\u05d0)  [b]", "(\u05d0) [b]", "(ASCII only `\"(\\u05d0)\\u0020\\u0020[b]\"`)"},
		{"two adjacent spaces", "in  take", "in take", "in  take (ASCII `\"in\\u0020\\u0020take\"`)"},
		{"a run of three spaces", "a   b", "a  b", "a   b (ASCII `\"a\\u0020\\u0020\\u0020b\"`)"},
		{"a run of spaces beside a letter outside ASCII", "caf\u00e9  bar", "caf\u00e9 bar", "caf\u00e9  bar (ASCII `\"caf\\u00e9\\u0020\\u0020bar\"`)"},
		{"single spaces between runs", "a b  c", "a b c", "a b  c (ASCII `\"a b\\u0020\\u0020c\"`)"},
		{"a European digit after a letter", "tier 2", "tier  2", "tier 2"},
		{"beyond the Basic Multilingual Plane", "go\U0001f680", "go", "go\U0001f680 (ASCII `\"go\\ud83d\\ude80\"`)"},
		{"an admitted format character", "a\ufffbb", "ab", "a`U+FFFB`b (ASCII `\"a\\ufffbb\"`)"},
		{"a backtick", "a`\u00e9", "a`e", "a\\`\u00e9 (ASCII `\"a\\u0060\\u00e9\"`)"},
	} {
		loaded := loadFilterPack(t, displayPack(t, test.value, test.twin))
		whole, err := loaded.View()
		if err != nil {
			t.Fatal(err)
		}
		if markdown := string(whole.Markdown()); !strings.Contains(markdown, "\n  - org\\.judgmentpack\\.annotations:\n    - label: "+test.shown+"\n") {
			t.Errorf("%s: the view does not write the annotation as %q:\n%s", test.name, test.shown, markdown)
		}
		filtered, err := loaded.FilteredView([]pv.Criterion{{Key: "label", Value: test.value}})
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if _, rules := matchedIDs(filtered); strings.Join(rules, ",") != "a" {
			t.Errorf("%s: matched %q", test.name, rules)
		}
		markdown := string(filtered.Markdown())
		statement := "  - label is " + test.shown
		if !strings.Contains(markdown, "\n"+statement+"\n") || !strings.Contains(markdown, "\n    - label: "+test.shown+"\n") {
			t.Errorf("%s: the filtered view does not state the filter and the match as %q:\n%s", test.name, test.shown, markdown)
		}
		if strings.Contains(test.shown, "(ASCII ") {
			if got := recovered(t, statement); got != test.value {
				t.Errorf("%s: the ASCII form decodes to %q, want %q", test.name, got, test.value)
			}
			if ascii, _ := asciiFormOf(t, statement); ascii != printableASCII(ascii) || strings.Contains(ascii, "  ") {
				t.Errorf("%s: the ASCII form holds more than printable ASCII, or a run of spaces: %q", test.name, ascii)
			}
		}

		// The look-alike: another string, which matches another rule, and
		// whose statement differs in printable ASCII.
		twin, err := loaded.FilteredView([]pv.Criterion{{Key: "label", Value: test.twin}})
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if _, rules := matchedIDs(twin); strings.Join(rules, ",") != "b" {
			t.Errorf("%s: the twin matched %q", test.name, rules)
		}
		var twinStatement string
		for _, line := range strings.Split(string(twin.Markdown()), "\n") {
			if strings.HasPrefix(line, "  - label is ") {
				twinStatement = line
			}
		}
		if twinStatement == "" || rendered(twinStatement) == rendered(statement) {
			t.Errorf("%s: %q and %q render alike", test.name, statement, twinStatement)
		}

		// The JSON carries the exact strings.
		var stated struct {
			Filter struct {
				Criteria []struct{ Key, Value string }
			}
		}
		if err := json.Unmarshal(filtered.JSON(), &stated); err != nil || len(stated.Filter.Criteria) != 1 || stated.Filter.Criteria[0].Value != test.value {
			t.Errorf("%s: the filtered view's JSON states %+v (%v)", test.name, stated.Filter.Criteria, err)
		}
		var described struct {
			Pack struct {
				Rules []struct {
					Extensions map[string]map[string]any
				}
			}
		}
		if err := json.Unmarshal(whole.JSON(), &described); err != nil || len(described.Pack.Rules) != 2 ||
			described.Pack.Rules[0].Extensions["org.judgmentpack.annotations"]["label"] != test.value {
			t.Errorf("%s: the view's JSON holds %+v (%v)", test.name, described.Pack.Rules, err)
		}
	}
}

// ADR-0078 changes annotation text only: a fact's value, which the view of
// every specification version writes, keeps its ASCII form with a run of
// spaces as spaces and a right-to-left letter beside it, so the view of a
// 0.3.0 pack is byte for byte what it was. Each line is written here by hand.
func TestAFactValueKeepsItsASCIIForm(t *testing.T) {
	for _, version := range []string{"0.3.0", "0.5.0"} {
		pack := `{
  "specVersion": "` + version + `",
  "id": "https://example.com/packs/fact-values",
  "version": "1.0.0",
  "title": "Fact values",
  "decision": {"intent": "Route", "question": "Route?"},
  "outcomes": [{"id": "accept", "label": "Accept"}, {"id": "refer", "label": "Refer"}],
  "rules": [
    {"id": "a", "description": "A", "when": {"op": "fact", "path": "/x", "operator": "equals", "value": "caf\u00e9  bar"}, "outcome": "accept", "onUnknown": "ignore"},
    {"id": "b", "description": "B", "when": {"op": "fact", "path": "/x", "operator": "equals", "value": "x\u05d0"}, "outcome": "accept", "onUnknown": "ignore"}
  ]
}`
		_, view := mustView(t, []byte(pack))
		markdown := string(view.Markdown())
		for _, line := range []string{
			"\n  - fact \\\"\\/x\\\" equals \\\"caf\u00e9  bar\\\" (ASCII `\"caf\\u00e9  bar\"`)\n",
			"\n  - fact \\\"\\/x\\\" equals \\\"x\u05d0\\\" (ASCII `\"x\\u05d0\"`)\n",
		} {
			if !strings.Contains(markdown, line) {
				t.Errorf("%s: the view does not hold %q:\n%s", version, line, markdown)
			}
		}
	}
}
