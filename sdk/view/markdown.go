package view

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
)

// The Markdown rendering of a view, view version 1. The layout is fixed and
// plain: a title heading, an identity list, then one section per member in
// the order a reviewer reads a pack — decision, applicability, outcomes,
// rules, exceptions, fallback outcome, escalation, evidence requirements,
// sources, metadata, extensions. A section the document has no member for is
// left out; a member that is present and empty says *empty*.
//
// Every string taken from the pack passes through escapeMarkdown, and only
// the renderer's own words are written unescaped. A string in a position the
// evaluator matches on — a fact's path, the strings and member names of a
// fact's value, an extension's name and the strings and member names of its
// value — is also written in ASCII beside it when it holds anything else
// (matched). No line begins with pack
// text: each follows a heading marker, a list marker and label, or a table
// pipe.

// escapeMarkdown makes one pack string safe to place in a line of Markdown
// text, including a table cell:
//
//   - every ASCII punctuation character is written with a backslash before it,
//     which CommonMark renders as the character itself, so no character of the
//     string can open emphasis, a code span, a link, raw HTML, an entity, a
//     table cell or any block;
//   - each run of characters internal/display replaces (controls, format
//     characters including the bidi overrides and isolates, and line and
//     paragraph separators) is written as a code span naming each code point,
//     `U+202E`, so it stays visible and cannot act;
//   - a space at either end of the string is written as &#32;, so it is
//     neither trimmed from a table cell nor read as a hard line break;
//   - every other character is written as itself.
func escapeMarkdown(value string) string {
	runes := []rune(value)
	first, last := 0, len(runes)
	for first < last && runes[first] == ' ' {
		first++
	}
	for last > first && runes[last-1] == ' ' {
		last--
	}
	var builder strings.Builder
	for index := 0; index < len(runes); index++ {
		character := runes[index]
		switch {
		case character == ' ' && (index < first || index >= last):
			builder.WriteString("&#32;")
		case display.Replaces(character):
			builder.WriteByte('`')
			for run := 0; index < len(runes) && display.Replaces(runes[index]); index, run = index+1, run+1 {
				if run > 0 {
					builder.WriteByte(' ')
				}
				fmt.Fprintf(&builder, "U+%04X", runes[index])
			}
			index--
			builder.WriteByte('`')
		case asciiPunctuation(character):
			builder.WriteByte('\\')
			builder.WriteRune(character)
		default:
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

// asciiPunctuation is CommonMark's ASCII punctuation character class, every
// member of which a backslash escapes.
func asciiPunctuation(character rune) bool {
	return (character >= '!' && character <= '/') || (character >= ':' && character <= '@') ||
		(character >= '[' && character <= '`') || (character >= '{' && character <= '~')
}

type markdownWriter struct {
	builder strings.Builder
}

func (m *markdownWriter) line(depth int, text string) {
	m.builder.WriteString(strings.Repeat("  ", depth))
	m.builder.WriteString(text)
	m.builder.WriteByte('\n')
}

// section starts a heading, with the one blank line that separates it from
// what came before.
func (m *markdownWriter) section(level int, text string) {
	if !strings.HasSuffix(m.builder.String(), "\n\n") {
		m.builder.WriteByte('\n')
	}
	m.line(0, strings.Repeat("#", level)+" "+text)
	m.builder.WriteByte('\n')
}

// field writes one labelled pack string, or nothing when it is absent.
func (m *markdownWriter) field(depth int, label, value string) {
	if value != "" {
		m.line(depth, "- "+label+": "+escapeMarkdown(value))
	}
}

// list writes one labelled array of pack strings, or nothing when it is
// absent.
func (m *markdownWriter) list(depth int, label string, values []string) {
	if values == nil {
		return
	}
	if len(values) == 0 {
		m.line(depth, "- "+label+": *empty*")
		return
	}
	m.line(depth, "- "+label+":")
	for _, value := range values {
		m.line(depth+1, "- "+escapeMarkdown(value))
	}
}

// extensions writes one labelled extensions object, or nothing when it is
// absent.
func (m *markdownWriter) extensions(depth int, extensions []Extension) {
	if extensions == nil {
		return
	}
	if len(extensions) == 0 {
		m.line(depth, "- Extensions: *empty*")
		return
	}
	m.line(depth, "- Extensions:")
	m.extensionItems(depth+1, extensions)
}

func (m *markdownWriter) extensionItems(depth int, extensions []Extension) {
	for _, extension := range extensions {
		name, _ := canonicalText(extension.Name)
		m.line(depth, "- "+escapeMarkdown(extension.Name)+asciiForm(name)+": "+matched(extension.Value))
	}
}

func (m *markdownWriter) target(depth int, label string, target Target) {
	m.line(depth, "- "+label+":")
	m.field(depth+1, "Kind", target.Kind)
	m.field(depth+1, "Name", target.Name)
}

// condition writes one condition node as a list item and its operands as a
// nested list. A fact's path and value are written as their canonical JSON,
// so an empty path and a string that looks like a number stay distinct.
func (m *markdownWriter) condition(depth int, condition Condition) {
	switch condition.Op {
	case "literal":
		m.line(depth, "- literal "+escapeMarkdown(condition.Value))
	case "all", "any":
		if len(condition.Conditions) == 0 {
			m.line(depth, "- "+condition.Op+" of: *empty*")
			return
		}
		m.line(depth, "- "+condition.Op+" of:")
		for _, child := range condition.Conditions {
			m.condition(depth+1, child)
		}
	case "not":
		m.line(depth, "- not:")
		if condition.Condition != nil {
			m.condition(depth+1, *condition.Condition)
		}
	case "fact":
		path, _ := canonicalText(condition.Path)
		m.line(depth, "- fact "+matched(path)+" "+escapeMarkdown(condition.Operator)+" "+matched(condition.Value))
	case "evidence-present":
		m.line(depth, "- evidence present "+escapeMarkdown(condition.EvidenceRequirement))
	}
}

// matched writes the canonical JSON text of a value from a position the
// evaluator matches on as escapeMarkdown writes it, followed by its ASCII
// form when it has one (asciiForm).
func matched(text string) string {
	return escapeMarkdown(text) + asciiForm(text)
}

// asciiForm is empty for a canonical JSON text whose strings and member
// names hold only printable ASCII, U+0020 to U+007E. For any other it is the
// text again, in a code span, with every code point outside that range
// written as a JSON \u escape — a surrogate pair beyond the Basic
// Multilingual Plane — and the backtick as \u0060. The span then holds only
// printable ASCII and no backtick, so its content cannot end it, and it
// decodes to the same value; two such values that differ in any code point
// differ in ASCII, though they render alike — a blank Hangul filler, a
// combining mark, a decomposed letter or a look-alike from another script.
func asciiForm(text string) string {
	if !needsASCII(text) {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(" (ASCII `")
	for _, character := range text {
		switch {
		case character == '`':
			builder.WriteString(`\u0060`)
		case character >= 0x20 && character <= 0x7e:
			builder.WriteRune(character)
		case character > 0xffff:
			high, low := utf16.EncodeRune(character)
			fmt.Fprintf(&builder, `\u%04x\u%04x`, high, low)
		default:
			fmt.Fprintf(&builder, `\u%04x`, character)
		}
	}
	builder.WriteString("`)")
	return builder.String()
}

// needsASCII reports whether a canonical JSON text holds, in a string or a
// member name, a code point outside printable ASCII: one written as itself,
// or a control character, which the canonical form writes as an escape. The
// only other escapes it writes are \\ and \".
func needsASCII(text string) bool {
	for index := 0; index < len(text); index++ {
		character := text[index]
		if character < 0x20 || character > 0x7e {
			return true
		}
		if character == '\\' {
			if index+1 < len(text) && (text[index+1] == '\\' || text[index+1] == '"') {
				index++
				continue
			}
			return true
		}
	}
	return false
}

func (m *markdownWriter) empty() {
	m.line(0, "*empty*")
}

// renderMarkdown renders a view, view version 1.
func renderMarkdown(view PackView) []byte {
	m := &markdownWriter{}
	m.line(0, "# "+escapeMarkdown(view.Title))
	m.builder.WriteByte('\n')
	m.field(0, "Id", view.Identity.ID)
	m.field(0, "Version", view.Identity.Version)
	m.field(0, "Spec version", view.Identity.SpecVersion)
	m.field(0, "Profile", view.Identity.Profile)
	m.line(0, "- Digest: "+view.Identity.Digest)
	m.line(0, "- View version: "+ViewVersion)
	m.field(0, "Description", view.Description)

	m.section(2, "Decision")
	m.field(0, "Intent", view.Decision.Intent)
	m.field(0, "Question", view.Decision.Question)
	m.extensions(0, view.Decision.Extensions)

	if view.Applicability != nil {
		m.section(2, "Applicability")
		m.condition(0, *view.Applicability)
	}

	m.section(2, "Outcomes")
	m.line(0, "| Id | Label | Description |")
	m.line(0, "| --- | --- | --- |")
	for _, outcome := range view.Outcomes {
		description := ""
		if outcome.Description != "" {
			description = " " + escapeMarkdown(outcome.Description)
		}
		m.line(0, "| "+escapeMarkdown(outcome.ID)+" | "+escapeMarkdown(outcome.Label)+" |"+description+" |")
	}
	for index, outcome := range view.Outcomes {
		if outcome.Extensions == nil {
			continue
		}
		m.section(3, "Extensions of outcome "+strconv.Itoa(index+1)+": "+escapeMarkdown(outcome.ID))
		if len(outcome.Extensions) == 0 {
			m.empty()
			continue
		}
		m.extensionItems(0, outcome.Extensions)
	}

	m.section(2, "Rules")
	if len(view.Rules) == 0 {
		m.empty()
	}
	for index, rule := range view.Rules {
		m.section(3, "Rule "+strconv.Itoa(index+1)+": "+escapeMarkdown(rule.ID))
		m.field(0, "Description", rule.Description)
		m.field(0, "Outcome", rule.Outcome)
		m.field(0, "On unknown", rule.OnUnknown)
		m.list(0, "Evidence requirement refs", rule.EvidenceRequirementRefs)
		m.list(0, "Source refs", rule.SourceRefs)
		m.field(0, "Rationale", rule.Rationale)
		m.extensions(0, rule.Extensions)
		m.line(0, "- When:")
		m.condition(1, rule.When)
	}

	if view.Exceptions != nil {
		m.section(2, "Exceptions")
		if len(view.Exceptions) == 0 {
			m.empty()
		}
		for index, exception := range view.Exceptions {
			m.section(3, "Exception "+strconv.Itoa(index+1)+": "+escapeMarkdown(exception.ID))
			m.field(0, "Description", exception.Description)
			m.field(0, "Effect", exception.Effect)
			m.field(0, "Target rule", exception.TargetRule)
			m.field(0, "Outcome", exception.Outcome)
			m.field(0, "On unknown", exception.OnUnknown)
			m.list(0, "Source refs", exception.SourceRefs)
			m.extensions(0, exception.Extensions)
			m.line(0, "- When:")
			m.condition(1, exception.When)
		}
	}

	if view.FallbackOutcome != "" {
		m.section(2, "Fallback outcome")
		m.field(0, "Outcome", view.FallbackOutcome)
	}

	if view.Escalation != nil {
		m.section(2, "Escalation")
		m.list(0, "Triggers", view.Escalation.Triggers)
		if view.Escalation.Target != nil {
			m.target(0, "Target", *view.Escalation.Target)
		}
		m.field(0, "Message", view.Escalation.Message)
		if view.Escalation.Routes != nil {
			if len(view.Escalation.Routes) == 0 {
				m.line(0, "- Routes: *empty*")
			} else {
				m.line(0, "- Routes:")
			}
			for index, route := range view.Escalation.Routes {
				m.line(1, "- Route "+strconv.Itoa(index+1)+":")
				m.target(2, "Target", route.Target)
				m.list(2, "Triggers", route.Triggers)
			}
		}
		m.extensions(0, view.Escalation.Extensions)
	}

	if view.EvidenceRequirements != nil {
		m.section(2, "Evidence requirements")
		if len(view.EvidenceRequirements) == 0 {
			m.empty()
		}
		for index, requirement := range view.EvidenceRequirements {
			m.section(3, "Evidence requirement "+strconv.Itoa(index+1)+": "+escapeMarkdown(requirement.ID))
			m.field(0, "Description", requirement.Description)
			m.line(0, "- Required: "+strconv.FormatBool(requirement.Required))
			m.field(0, "Kind", requirement.Kind)
			m.extensions(0, requirement.Extensions)
		}
	}

	if view.Sources != nil {
		m.section(2, "Sources")
		if len(view.Sources) == 0 {
			m.empty()
		}
		for index, source := range view.Sources {
			m.section(3, "Source "+strconv.Itoa(index+1)+": "+escapeMarkdown(source.ID))
			m.field(0, "Title", source.Title)
			m.field(0, "Publisher", source.Publisher)
			m.field(0, "Published at", source.PublishedAt)
			m.line(0, "- Locator:")
			m.field(1, "Kind", source.Locator.Kind)
			m.field(1, "Value", source.Locator.Value)
			if source.Citation != nil {
				m.line(0, "- Citation:")
				m.field(1, "Location", source.Citation.Location)
				m.field(1, "Excerpt", source.Citation.Excerpt)
			}
			m.field(0, "Rights", source.Rights)
			m.extensions(0, source.Extensions)
		}
	}

	if view.Metadata != nil {
		m.section(2, "Metadata")
		metadata := view.Metadata
		if metadata.Authors == nil && metadata.CreatedAt == "" && metadata.License == "" &&
			metadata.RequiredExtensions == nil && metadata.Reviews == nil && metadata.Extensions == nil {
			m.empty()
		}
		m.list(0, "Authors", metadata.Authors)
		m.field(0, "Created at", metadata.CreatedAt)
		m.field(0, "License", metadata.License)
		m.list(0, "Required extensions", metadata.RequiredExtensions)
		if metadata.Reviews != nil {
			if len(metadata.Reviews) == 0 {
				m.line(0, "- Reviews: *empty*")
			} else {
				m.line(0, "- Reviews:")
			}
			for index, review := range metadata.Reviews {
				m.line(1, "- Review "+strconv.Itoa(index+1)+":")
				m.field(2, "Reviewer", review.Reviewer)
				m.field(2, "Reviewed at", review.ReviewedAt)
				m.field(2, "Disposition", review.Disposition)
				m.field(2, "Note", review.Note)
			}
		}
		m.extensions(0, metadata.Extensions)
	}

	if view.Extensions != nil {
		m.section(2, "Extensions")
		if len(view.Extensions) == 0 {
			m.empty()
		}
		m.extensionItems(0, view.Extensions)
	}
	return []byte(m.builder.String())
}
