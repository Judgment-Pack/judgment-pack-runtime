package view_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	pv "github.com/Judgment-Pack/judgment-pack-runtime/sdk/view"
)

// The escaping rule, one class at a time, written by hand from the rule
// docs/building-with-packs.md states.
func TestEscapeMarkdownByClass(t *testing.T) {
	for _, test := range []struct {
		class, input, want string
	}{
		{"empty", "", ""},
		{"plain words", "Plain words 42", "Plain words 42"},
		{"pipe", "a|b", `a\|b`},
		{"backtick", "`code`", "\\`code\\`"},
		{"leading heading marker", "# Heading", `\# Heading`},
		{"list and quote markers", "- + > 1.", `\- \+ \> 1\.`},
		{"raw HTML", "<script>alert(1)</script>", `\<script\>alert\(1\)\<\/script\>`},
		{"link", "[x](https://example.invalid)", `\[x\]\(https\:\/\/example\.invalid\)`},
		{"link close and open", "](", `\]\(`},
		{"emphasis and strike", "*a* _b_ ~c~", `\*a\* \_b\_ \~c\~`},
		{"entity", "&amp;", `\&amp\;`},
		{"backslash", `a\b`, `a\\b`},
		{"newline", "a\nb", "a`U+000A`b"},
		{"carriage return and newline", "a\r\nb", "a`U+000D U+000A`b"},
		{"tab", "a\tb", "a`U+0009`b"},
		{"C0, DEL and C1 controls", "\x00\x1b\x7f\u0085", "`U+0000 U+001B U+007F U+0085`"},
		{"line and paragraph separators", "a\u2028b\u2029c", "a`U+2028`b`U+2029`c"},
		{"bidi override and pop", "a\u202ebc\u202c", "a`U+202E`bc`U+202C`"},
		{"bidi isolates", "\u2066x\u2069", "`U+2066`x`U+2069`"},
		{"direction marks", "\u200e\u200f\u061c", "`U+200E U+200F U+061C`"},
		{"zero-width joiner and byte order mark", "a\u200db\ufeff", "a`U+200D`b`U+FEFF`"},
		{"a format character outside the BMP", "a\U000E0001b", "a`U+E0001`b"},
		{"leading and trailing spaces", "  a b  ", "&#32;&#32;a b&#32;&#32;"},
		{"only spaces", "   ", "&#32;&#32;&#32;"},
		{"non-breaking space", "\u00a0a\u00a0", "\u00a0a\u00a0"},
		{"letters outside ASCII", "caf\u00e9 \U0001F600 \ufb01", "caf\u00e9 \U0001F600 \ufb01"},
	} {
		if got := pv.EscapeMarkdown(test.input); got != test.want {
			t.Errorf("%s: pv.EscapeMarkdown(%q) = %q, want %q", test.class, test.input, got, test.want)
		}
	}
	// Every ASCII character, by hand-stated class: a punctuation character
	// gains a backslash, a letter, a digit and an inner space do not.
	const punctuation = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
	for character := rune(0x21); character < 0x7f; character++ {
		want := string(character)
		if strings.ContainsRune(punctuation, character) {
			want = `\` + want
		}
		if got := pv.EscapeMarkdown("a" + string(character) + "a"); got != "a"+want+"a" {
			t.Errorf("escapeMarkdown of %q = %q, want %q", character, got, "a"+want+"a")
		}
	}
}

// renderedLinePrefixes are the renderer's own openings of a line. No line of a
// rendering begins with text taken from the pack.
var renderedLinePrefixes = []string{"# ", "## ", "### ", "- ", "| ", "*empty*"}

// Every line of every rendering opens with the renderer's own text, the
// rendering holds no character internal/display replaces other than its own
// line feeds, and every table row has exactly the four pipes of its three
// cells once escaped pipes are set aside.
func TestEveryMarkdownLineOpensWithTheRenderersText(t *testing.T) {
	for _, input := range viewInputs(t) {
		_, view := mustView(t, input.pack)
		markdown := string(view.Markdown())
		if !strings.HasSuffix(markdown, "\n") || strings.HasSuffix(markdown, "\n\n") || strings.Contains(markdown, "\n\n\n") {
			t.Errorf("%s: the rendering does not end in one newline, or holds two blank lines in a row", input.name)
		}
		for _, character := range markdown {
			if character != '\n' && display.Replaces(character) {
				t.Errorf("%s: the rendering holds %U", input.name, character)
			}
		}
		for number, line := range strings.Split(strings.TrimSuffix(markdown, "\n"), "\n") {
			if line == "" {
				continue
			}
			body := strings.TrimLeft(line, " ")
			if indent := len(line) - len(body); indent%2 != 0 || (indent > 0 && !strings.HasPrefix(body, "- ")) {
				t.Errorf("%s:%d: an indented line that is not a nested list item: %q", input.name, number+1, line)
			}
			opened := false
			for _, prefix := range renderedLinePrefixes {
				opened = opened || strings.HasPrefix(body, prefix)
			}
			if !opened {
				t.Errorf("%s:%d: the line opens with no renderer text: %q", input.name, number+1, line)
			}
			if strings.HasPrefix(line, "| ") {
				if cells := strings.Count(strings.ReplaceAll(line, `\|`, ""), "|"); cells != 4 {
					t.Errorf("%s:%d: a table row with %d pipes: %q", input.name, number+1, cells, line)
				}
			}
		}
	}
}

// Over the value space internal/jcs admits, the view's canonical writer is
// RFC 8785 as that package writes it: for every input document with its
// numbers and nulls turned into strings, and for names that order differently
// in UTF-16 and in UTF-8.
func TestTheCanonicalWriterIsJCSOnItsValueSpace(t *testing.T) {
	var strip func(value any) any
	strip = func(value any) any {
		switch typed := value.(type) {
		case nil:
			return "null"
		case json.Number:
			return string(typed)
		case []any:
			items := make([]any, len(typed))
			for index, item := range typed {
				items[index] = strip(item)
			}
			return items
		case map[string]any:
			object := make(map[string]any, len(typed))
			for name, item := range typed {
				object[name] = strip(item)
			}
			return object
		}
		return value
	}
	values := []any{
		map[string]any{"\ufb01": "ligature", "\U0001F600": "", "a": []any{}, "\u00e9": map[string]any{}, "": true},
		"\x00\x01\x1f\x7f\u0085\u2028\u2029\u202e\"\\/\b\f\n\r\t",
		[]any{false, "x", []any{map[string]any{"b": "1", "a": "2"}}},
	}
	for _, input := range viewInputs(t) {
		root, failure := carrier.Decode(input.pack, carrier.DefaultLimits())
		if failure != nil {
			t.Fatal(failure)
		}
		values = append(values, strip(root))
	}
	for index, value := range values {
		want, err := jcs.Encode(value)
		if err != nil {
			t.Fatalf("value %d: jcs: %v", index, err)
		}
		got, err := pv.CanonicalText(value)
		if err != nil {
			t.Fatalf("value %d: %v", index, err)
		}
		if got != string(want) {
			t.Errorf("value %d:\n got %s\nwant %s", index, got, want)
		}
	}
	// What jcs refuses, the view writes as the document holds it.
	for _, test := range []struct {
		value any
		want  string
	}{
		{nil, "null"},
		{json.Number("1.50"), "1.50"},
		{json.Number("-0"), "-0"},
		{json.Number("1E+2"), "1E+2"},
		{[]any{json.Number("1.0"), nil}, "[1.0,null]"},
	} {
		if got, err := pv.CanonicalText(test.value); err != nil || got != test.want {
			t.Errorf("pv.CanonicalText(%#v) = %q, %v; want %q", test.value, got, err, test.want)
		}
	}
	for _, refused := range []any{json.Number(""), json.Number("1,2"), json.Number("[1]"), json.Number("x"), 1.5, "\xff", map[string]any{"\xff": "x"}} {
		if _, err := pv.CanonicalText(refused); err == nil {
			t.Errorf("pv.CanonicalText(%#v) was not refused", refused)
		}
	}
}

// The builder refuses, with an error wrapping ErrInternal and never a partial
// view or a *sdk.Refusal, any admitted root it cannot represent: one the schema
// would refuse, which Load never admits, stands in for a construct a later
// schema could add. The error names the first such construct, the same way on
// every call.
func TestTheViewRefusesWhatItCannotRepresent(t *testing.T) {
	base := func(t *testing.T, version string) map[string]any {
		t.Helper()
		text := viewMinimalPack
		if version != pv.ViewCoreVersion {
			text = strings.Replace(text, `"specVersion": "0.3.0"`, `"specVersion": "`+version+`"`, 1)
		}
		root, failure := carrier.Decode([]byte(text), carrier.DefaultLimits())
		if failure != nil {
			t.Fatal(failure)
		}
		if _, err := pv.BuildView(root.(map[string]any)); err != nil {
			t.Fatalf("the unedited root does not view: %v", err)
		}
		return root.(map[string]any)
	}
	rule := func(root map[string]any) map[string]any { return root["rules"].([]any)[0].(map[string]any) }
	for _, test := range []struct {
		name    string
		version string
		edit    func(root map[string]any)
		pointer string
	}{
		{"an unknown member", pv.ViewCoreVersion, func(root map[string]any) { root["owner"] = "x" }, "/owner"},
		{"the first of two unknown members", pv.ViewCoreVersion, func(root map[string]any) { root["zz"] = "x"; root["aa"] = "x" }, "/aa"},
		{"an unknown member of a rule", pv.ViewCoreVersion, func(root map[string]any) { rule(root)["owner"] = "x" }, "/rules/0/owner"},
		{"a pointer-escaped member name", pv.ViewCoreVersion, func(root map[string]any) { rule(root)["a/b~c"] = "x" }, "/rules/0/a~1b~0c"},
		{"an unknown condition op", pv.ViewCoreVersion, func(root map[string]any) { rule(root)["when"] = map[string]any{"op": "xor"} }, "/rules/0/when/op"},
		{"a literal that is not a Boolean", pv.ViewCoreVersion, func(root map[string]any) { rule(root)["when"].(map[string]any)["value"] = "true" }, "/rules/0/when/value"},
		{"a present empty optional string", pv.ViewCoreVersion, func(root map[string]any) { root["description"] = "" }, "/description"},
		{"a string of the wrong type", pv.ViewCoreVersion, func(root map[string]any) { root["title"] = json.Number("7") }, "/title"},
		{"a missing required member", pv.ViewCoreVersion, func(root map[string]any) { delete(root, "outcomes") }, "/outcomes"},
		{"a null where a Boolean is required", pv.ViewCoreVersion, func(root map[string]any) {
			root["evidenceRequirements"] = []any{map[string]any{"id": "e", "description": "E.", "required": nil}}
		}, "/evidenceRequirements/0/required"},
		{"a missing Boolean", pv.ViewCoreVersion, func(root map[string]any) {
			root["evidenceRequirements"] = []any{map[string]any{"id": "e", "description": "E."}}
		}, "/evidenceRequirements/0/required"},
		{"an array of the wrong type", pv.ViewCoreVersion, func(root map[string]any) { root["sources"] = map[string]any{} }, "/sources"},
		{"an extension value outside the JSON data model", pv.ViewCoreVersion, func(root map[string]any) {
			root["extensions"] = map[string]any{"com.example.x": 1.5}
		}, "/extensions/com.example.x"},
		{"a profile in a 0.2.0-draft document", pv.ViewDraftVersion, func(root map[string]any) { root["profile"] = "base" }, "/profile"},
		{"routes in a 0.2.0-draft escalation", pv.ViewDraftVersion, func(root map[string]any) {
			root["escalation"] = map[string]any{"triggers": []any{"unknown"}, "target": map[string]any{"kind": "queue", "name": "Q"}, "routes": []any{}}
		}, "/escalation/routes"},
		{"a 0.2.0-draft escalation without a target", pv.ViewDraftVersion, func(root map[string]any) {
			root["escalation"] = map[string]any{"triggers": []any{"unknown"}}
		}, "/escalation/target"},
		{"a specification version the view does not read", pv.ViewCoreVersion, func(root map[string]any) { root["specVersion"] = "0.1.0-draft" }, "/specVersion"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := base(t, test.version)
			test.edit(root)
			view, err := pv.BuildView(root)
			if err == nil {
				t.Fatalf("the edited root viewed: %+v", view)
			}
			if !strings.HasPrefix(err.Error(), "The admitted pack holds a construct at "+test.pointer+" ") {
				t.Fatalf("the error %q does not name %s", err, test.pointer)
			}
			if _, again := pv.BuildView(root); again == nil || again.Error() != err.Error() {
				t.Fatalf("a second call said %v", again)
			}
			if view.Rules != nil || view.Identity != (pv.Identity{}) {
				t.Fatalf("a partial view came back: %+v", view)
			}
		})
	}
}
