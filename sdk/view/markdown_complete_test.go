package view_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	pv "github.com/Judgment-Pack/judgment-pack-runtime/sdk/view"
)

// markdownSections names, for each top-level member of a pack, the heading
// of the Markdown section that shows it; "" is the part before the first
// section heading. Written by hand from the layout docs/building-with-packs.md
// states.
var markdownSections = map[string]string{
	"title": "", "id": "", "version": "", "specVersion": "", "profile": "", "description": "",
	"decision":             "## Decision",
	"applicability":        "## Applicability",
	"outcomes":             "## Outcomes",
	"rules":                "## Rules",
	"exceptions":           "## Exceptions",
	"valueSets":            "## Value sets",
	"fallbackOutcome":      "## Fallback outcome",
	"escalation":           "## Escalation",
	"evidenceRequirements": "## Evidence requirements",
	"sources":              "## Sources",
	"metadata":             "## Metadata",
	"extensions":           "## Extensions",
}

// markdownLabels names, for each member the schema walk finds, the text the
// Markdown writes when that member is present, written by hand from the same
// layout. A member the schema adds fails the test until it has a label here.
var markdownLabels = map[string]string{
	"condition{all}.conditions": "- all of:", "condition{all}.op": "- all of:",
	"condition{any}.conditions": "- any of:", "condition{any}.op": "- any of:",
	"condition{evidence-present}.evidenceRequirement": "- evidence present ", "condition{evidence-present}.op": "- evidence present ",
	"condition{fact}.op": "- fact ", "condition{fact}.operator": "- fact ", "condition{fact}.path": "- fact ", "condition{fact}.value": "- fact ",
	"condition{literal}.op": "- literal ", "condition{literal}.value": "- literal ",
	"condition{not}.condition": "- not:", "condition{not}.op": "- not:",
	"decision.extensions": "- Extensions:", "decision.intent": "- Intent: ", "decision.question": "- Question: ",
	"escalation.extensions": "- Extensions:", "escalation.message": "- Message: ",
	"escalation.routes": "- Routes:", "escalation.routes.target": "    - Target:", "escalation.routes.target.kind": "      - Kind: ",
	"escalation.routes.target.name": "      - Name: ", "escalation.routes.triggers": "    - Triggers:",
	"escalation.target": "- Target:", "escalation.target.kind": "  - Kind: ", "escalation.target.name": "  - Name: ",
	"escalation.triggers":             "- Triggers:",
	"evidenceRequirement.description": "- Description: ", "evidenceRequirement.extensions": "- Extensions:",
	"evidenceRequirement.id": "### Evidence requirement 1: ", "evidenceRequirement.kind": "- Kind: ", "evidenceRequirement.required": "- Required: ",
	"exception.description": "- Description: ", "exception.effect": "- Effect: ", "exception.extensions": "- Extensions:",
	"exception.id": "### Exception 1: ", "exception.onUnknown": "- On unknown: ", "exception.outcome": "- Outcome: ",
	"exception.sourceRefs": "- Source refs:", "exception.targetRule": "- Target rule: ", "exception.when": "- When:",
	"metadata.authors": "- Authors:", "metadata.createdAt": "- Created at: ", "metadata.extensions": "- Extensions:",
	"metadata.license": "- License: ", "metadata.requiredExtensions": "- Required extensions:", "metadata.reviews": "- Reviews:",
	"metadata.reviews.disposition": "    - Disposition: ", "metadata.reviews.note": "    - Note: ",
	"metadata.reviews.reviewedAt": "    - Reviewed at: ", "metadata.reviews.reviewer": "    - Reviewer: ",
	"outcome.description": "| Id | Label | Description |", "outcome.extensions": "### Extensions of outcome ",
	"outcome.id": "| Id | Label | Description |", "outcome.label": "| Id | Label | Description |",
	"pack.applicability": "## Applicability", "pack.decision": "## Decision", "pack.description": "- Description: ",
	"pack.escalation": "## Escalation", "pack.evidenceRequirements": "## Evidence requirements", "pack.exceptions": "## Exceptions",
	"pack.extensions": "## Extensions", "pack.fallbackOutcome": "## Fallback outcome", "pack.id": "- Id: ",
	"pack.metadata": "## Metadata", "pack.outcomes": "## Outcomes", "pack.profile": "- Profile: ", "pack.rules": "## Rules",
	"pack.sources": "## Sources", "pack.specVersion": "- Spec version: ", "pack.title": "# ", "pack.version": "- Version: ",
	"rule.description": "- Description: ", "rule.evidenceRequirementRefs": "- Evidence requirement refs:", "rule.extensions": "- Extensions:",
	"rule.id": "### Rule 1: ", "rule.onUnknown": "- On unknown: ", "rule.outcome": "- Outcome: ", "rule.rationale": "- Rationale: ",
	"rule.sourceRefs": "- Source refs:", "rule.when": "- When:",
	"source.citation": "- Citation:", "source.citation.excerpt": "  - Excerpt: ", "source.citation.location": "  - Location: ",
	"source.extensions": "- Extensions:", "source.id": "### Source 1: ", "source.locator": "- Locator:",
	"source.locator.kind": "  - Kind: ", "source.locator.value": "  - Value: ", "source.publishedAt": "- Published at: ",
	"source.publisher": "- Publisher: ", "source.rights": "- Rights: ", "source.title": "- Title: ",
	// The staged draft selector's members (EXPERIMENTAL).
	"pack.valueSets": "## Value sets", "valueSet.id": "### Value set 1: ", "valueSet.description": "- Description: ",
	"valueSet.entries": "- Entries:", "valueSetEntry.value": "  - Entry 1: ", "valueSetEntry.description": "    - Description: ",
	"valueSetEntry.extensions": "    - Extensions:", "condition{fact}.valueSet": " value set ",
	"annotatedExtensions.org.judgmentpack.annotations": "- org\\.judgmentpack\\.annotations:",
	"entryExtensions.org.judgmentpack.annotations":     "      - org\\.judgmentpack\\.annotations:",
}

// labelSection is the top-level member whose section holds a member key's
// label; "" means anywhere, for a condition and for an extensions object that
// may hold annotations, which several sections hold.
func labelSection(key string) string {
	prefix, _, _ := strings.Cut(key, ".")
	switch {
	case prefix == "pack":
		return strings.TrimPrefix(key, "pack.")
	case strings.HasPrefix(prefix, "condition{"), prefix == "annotatedExtensions", prefix == "entryExtensions":
		return ""
	case prefix == "evidenceRequirement", prefix == "rule", prefix == "exception", prefix == "outcome", prefix == "source":
		return prefix + "s"
	case prefix == "valueSet", prefix == "valueSetEntry":
		return "valueSets"
	}
	return prefix
}

// sectionOf returns the text of one section of a rendering: from its
// heading to the next section heading, or the part before the first.
func sectionOf(markdown, heading string) (string, bool) {
	if heading == "" {
		before, _, _ := strings.Cut(markdown, "\n## ")
		return before, true
	}
	start := strings.Index(markdown, "\n"+heading+"\n")
	if start < 0 {
		return "", false
	}
	rest := markdown[start+1:]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end+1]
	}
	return rest, true
}

// leafForms are the texts a string or number leaf may be shown as: escaped
// as itself, or, inside a value shown as canonical JSON, escaped as its JSON
// form.
func leafForms(t *testing.T, leaf any) []string {
	t.Helper()
	switch value := leaf.(type) {
	case string:
		text, err := pv.CanonicalText(value)
		if err != nil {
			t.Fatal(err)
		}
		return []string{pv.EscapeMarkdown(value), pv.EscapeMarkdown(text)}
	case json.Number:
		return []string{pv.EscapeMarkdown(value.String())}
	}
	return nil
}

// leaves collects the string and number leaves of a value, depth first,
// members in name order. A condition's op member is left out: it names the
// form of the condition, which the Markdown writes as a label ("evidence
// present", "all of:"), and the label check holds it.
func leaves(value any, out *[]any) {
	switch node := value.(type) {
	case map[string]any:
		names := slices.Sorted(maps.Keys(node))
		_, condition := node["op"]
		for _, name := range names {
			if condition && name == "op" {
				continue
			}
			leaves(node[name], out)
		}
	case []any:
		for _, item := range node {
			leaves(item, out)
		}
	case string, json.Number:
		*out = append(*out, node)
	}
}

// find reports where the first form of a leaf occurs in text at or after
// from, and how long it is.
func find(t *testing.T, text string, from int, leaf any) (int, int) {
	t.Helper()
	best, length := -1, 0
	for _, form := range leafForms(t, leaf) {
		if form == "" {
			continue
		}
		if at := strings.Index(text[from:], form); at >= 0 && (best < 0 || from+at < best) {
			best, length = from+at, len(form)
		}
	}
	return best, length
}

// checkOrder holds every array under value to document order in a section:
// each element's anchor — its id, or else the first of its leaves no
// sibling shares — must be found after the previous element's.
func checkOrder(t *testing.T, name, section string, value any) {
	t.Helper()
	switch node := value.(type) {
	case map[string]any:
		for _, member := range slices.Sorted(maps.Keys(node)) {
			checkOrder(t, name+"/"+member, section, node[member])
		}
	case []any:
		cursor := 0
		for index, item := range node {
			anchor := arrayAnchor(node, index)
			if anchor == nil {
				continue
			}
			at, length := find(t, section, cursor, anchor)
			if at < 0 {
				t.Errorf("%s/%d: %v is not shown after the element before it", name, index, anchor)
				return
			}
			cursor = at + length
			checkOrder(t, name+"/"+strconv.Itoa(index), section, item)
		}
	}
}

func arrayAnchor(array []any, index int) any {
	if object, ok := array[index].(map[string]any); ok {
		if id, ok := object["id"].(string); ok && id != "" {
			return id
		}
	}
	var own []any
	leaves(array[index], &own)
	shared := map[any]bool{}
	for other, item := range array {
		if other == index {
			continue
		}
		var theirs []any
		leaves(item, &theirs)
		for _, leaf := range theirs {
			shared[leaf] = true
		}
	}
	for _, leaf := range own {
		if text, ok := leaf.(string); ok && text == "" {
			continue
		}
		if !shared[leaf] {
			return leaf
		}
	}
	return nil
}

func loadSchema(t *testing.T, version string) map[string]any {
	t.Helper()
	set, err := artifacts.Load(version)
	if err != nil {
		t.Fatal(err)
	}
	data, err := set.Schema()
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

// The Markdown of every pack the view is held to shows the whole document,
// checked against the document and not against a rendering: every string
// and number of each top-level member is in that member's section in
// escaped form, every array keeps document order there, and every member the
// schema walk finds present has its label. A renderer that drops a field, an
// array element or a nested object fails here for every pack that holds it,
// whether or not a hand-written expectation covers that pack.
func TestTheMarkdownShowsEveryLeafInItsSection(t *testing.T) {
	inputs := viewInputs(t)
	for _, version := range viewVersions {
		keys := readSchemaKeys(t, version)
		for _, key := range slices.Sorted(maps.Keys(keys.members)) {
			if _, ok := markdownLabels[key]; !ok {
				t.Errorf("%s: the schema member %s has no Markdown label in this test", version, key)
			}
		}
		set := loadSchema(t, version)
		for _, input := range inputs {
			if input.version != version {
				continue
			}
			root, failure := carrier.Decode(input.pack, carrier.DefaultLimits())
			if failure != nil {
				t.Fatalf("%s: %+v", input.name, failure)
			}
			document := asObject(root)
			_, view := mustView(t, input.pack)
			markdown := string(view.Markdown())
			for _, member := range slices.Sorted(maps.Keys(document)) {
				heading, known := markdownSections[member]
				if !known {
					t.Errorf("%s: the member %s has no section in this test", input.name, member)
					continue
				}
				section, ok := sectionOf(markdown, heading)
				if !ok {
					t.Errorf("%s: the member %s is present and its section %q is not", input.name, member, heading)
					continue
				}
				var found []any
				leaves(document[member], &found)
				for _, leaf := range found {
					if at, _ := find(t, section, 0, leaf); at < 0 {
						t.Errorf("%s: %s holds %v, which its section does not show in any escaped form", input.name, member, leaf)
					}
				}
				checkOrder(t, input.name+": "+member, section, document[member])
			}
			present := keys.walkDocument(t, document, set)
			for key := range present.present {
				label := markdownLabels[key]
				section, ok := sectionOf(markdown, markdownSections[labelSection(key)])
				if labelSection(key) == "" {
					section, ok = markdown, true
				}
				if !ok || !strings.Contains(section, label) {
					t.Errorf("%s: %s is present and its label %q is not shown where it belongs", input.name, key, label)
				}
			}
		}
	}
}

// handWritten names the fixtures whose Markdown and canonical JSON are
// written by hand under testdata/expected, from the documented layout and
// the fixture itself, never recorded from a run: one pack holding every
// member of each admitted specification version, the adversarial strings,
// and the staged example of the staged draft selector, read from the bundle.
// Only each digest is computed, with sha256sum over the fixture.
var handWritten = []string{"0.2.0-draft/every-member", "0.3.0/every-member", "0.3.0/adversarial-strings", "0.4.0-draft/every-member", "0.4.0-draft/example/expense-approval-annotated"}

// handWrittenPack reads a hand-written expectation's pack: a fixture under
// testdata/packs, or, for a name under <version>/example/, the staged example
// the bundle of that version carries.
func handWrittenPack(t *testing.T, name string) []byte {
	t.Helper()
	if version, example, ok := strings.Cut(name, "/example/"); ok {
		set, err := artifacts.Load(version)
		if err != nil {
			t.Fatal(err)
		}
		data, err := set.StagedExample(example + ".json")
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	data, err := os.ReadFile(filepath.Join("testdata", "packs", filepath.FromSlash(name)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The Markdown of each hand-written fixture is its expectation, line for
// line.
func TestHandWrittenMarkdown(t *testing.T) {
	for _, name := range handWritten {
		want, err := os.ReadFile(filepath.Join("testdata", "expected", filepath.FromSlash(name)+".md"))
		if err != nil {
			t.Fatal(err)
		}
		_, view := mustView(t, handWrittenPack(t, name))
		got := strings.Split(string(view.Markdown()), "\n")
		lines := strings.Split(string(want), "\n")
		for index := range max(len(got), len(lines)) {
			var have, expected string
			if index < len(got) {
				have = got[index]
			}
			if index < len(lines) {
				expected = lines[index]
			}
			if have != expected {
				t.Fatalf("%s line %d:\n got %q\nwant %q", name, index+1, have, expected)
			}
		}
	}
}

// The canonical JSON of each hand-written fixture is its expectation, byte
// for byte. The expectation is written indented, for review, and in ASCII:
// encoding/json's Compact removes only the whitespace between tokens, and
// the view's code points from U+007F up are written as JSON escapes before
// the comparison. That rewrite hides nothing, because the view's own escapes
// are first held to the ones RFC 8785 writes, none of which is for a code
// point from U+007F up.
func TestHandWrittenCanonicalJSON(t *testing.T) {
	for _, name := range handWritten {
		written, err := os.ReadFile(filepath.Join("testdata", "expected", filepath.FromSlash(name)+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var want bytes.Buffer
		if err := json.Compact(&want, written); err != nil {
			t.Fatalf("%s: the expectation is not JSON: %v", name, err)
		}
		_, view := mustView(t, handWrittenPack(t, name))
		got := view.JSON()
		for index := 0; index < len(got); index++ {
			if got[index] != '\\' {
				continue
			}
			if index+1 < len(got) && strings.IndexByte(`"\\bfnrt`, got[index+1]) >= 0 {
				index++
				continue
			}
			if index+5 < len(got) && got[index+1] == 'u' && string(got[index+2:index+4]) == "00" && (got[index+4] == '0' || got[index+4] == '1') {
				index += 5
				continue
			}
			t.Fatalf("%s: the view writes an escape RFC 8785 does not, at byte %d: %q", name, index, got[index:min(index+6, len(got))])
		}
		var ascii strings.Builder
		for _, character := range string(got) {
			switch {
			case character < 0x7f:
				ascii.WriteRune(character)
			case character > 0xffff:
				high, low := utf16.EncodeRune(character)
				fmt.Fprintf(&ascii, `\u%04x\u%04x`, high, low)
			default:
				fmt.Fprintf(&ascii, `\u%04x`, character)
			}
		}
		if ascii.String() != want.String() {
			have, expected := ascii.String(), want.String()
			at := 0
			for at < len(have) && at < len(expected) && have[at] == expected[at] {
				at++
			}
			t.Fatalf("%s: the canonical JSON differs at byte %d:\n got %q\nwant %q", name, at, have[max(0, at-40):min(len(have), at+40)], expected[max(0, at-40):min(len(expected), at+40)])
		}
	}
}
