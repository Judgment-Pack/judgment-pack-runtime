package view_test

import (
	"encoding/json"
	"strings"
	"testing"

	pv "github.com/Judgment-Pack/judgment-pack-runtime/sdk/view"
)

// displayPack is a staged pack written by hand around two annotation strings:
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
  "specVersion": "0.4.0-draft",
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

// recovered decodes the ASCII form a Markdown line ends with, by its own
// syntax: a JSON string in a code span.
func recovered(t *testing.T, line string) string {
	t.Helper()
	start := strings.LastIndex(line, " (ASCII `")
	if start < 0 || !strings.HasSuffix(line, "`)") {
		t.Fatalf("no ASCII form in %q", line)
	}
	var value string
	if err := json.Unmarshal([]byte(line[start+len(" (ASCII `"):len(line)-len("`)")]), &value); err != nil {
		t.Fatalf("the ASCII form in %q: %v", line, err)
	}
	return value
}

// D2 (RFC 0031 section 8, the staged draft section 9.2): wherever annotation values are
// listed or a filter is stated, every character outside printable ASCII is
// shown so that the reader can recover each code point. In the Markdown the
// view's annotation line, the filtered view's statement of the filter and its
// match each write the string escaped and then in ASCII, which holds nothing
// but printable ASCII and decodes to the string exactly; a string that looks
// like it never reads alike in printable ASCII. In the JSON each is the exact
// string. Each class is an admitted annotation string, and each line it must
// get is written here by hand. A key is printable ASCII by the key grammar.
func TestAnnotationStringsAreShownSoEveryCodePointCanBeRecovered(t *testing.T) {
	for _, test := range []struct {
		name, value, twin, shown string
	}{
		{"a Cyrillic look-alike", "c\u0430fe", "cafe", "c\u0430fe (ASCII `\"c\\u0430fe\"`)"},
		{"a combining mark", "cafe\u0301", "caf\u00e9", "cafe\u0301 (ASCII `\"cafe\\u0301\"`)"},
		{"an inner no-break space", "in\u00a0take", "in take", "in\u00a0take (ASCII `\"in\\u00a0take\"`)"},
		{"an inner em space", "in\u2003take", "in take", "in\u2003take (ASCII `\"in\\u2003take\"`)"},
		{"a right-to-left letter", "x\u05d0", "x", "x\u05d0 (ASCII `\"x\\u05d0\"`)"},
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
		if got := recovered(t, statement); got != test.value {
			t.Errorf("%s: the ASCII form decodes to %q, want %q", test.name, got, test.value)
		}
		if ascii := statement[strings.LastIndex(statement, " (ASCII `"):]; ascii != printableASCII(ascii) {
			t.Errorf("%s: the ASCII form holds more than printable ASCII: %q", test.name, ascii)
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
		if twinStatement == "" || printableASCII(twinStatement) == printableASCII(statement) {
			t.Errorf("%s: %q and %q read alike in printable ASCII", test.name, statement, twinStatement)
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
