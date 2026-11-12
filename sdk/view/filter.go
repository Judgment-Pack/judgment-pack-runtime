package view

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// FilteredViewVersion is the version of the filtered view format: the members
// of FilteredView.JSON and the layout of FilteredView.Markdown, which carry it
// in band. A filtered view carries filteredViewVersion where a view carries
// viewVersion, and has no pack member, so a reader of views never takes one
// for the pack. Experimental, like the rest of the view.
const FilteredViewVersion = "1"

// ErrFilterCriterion is wrapped by the error for a criterion no annotation
// could match: a key or value the Core 0.5.0 schema does not allow, a repeated
// criterion, or no criterion at all. Experimental.
var ErrFilterCriterion = errors.New("view: invalid filter criterion")

// ErrFilterSelector is wrapped by the error for a filter on a pack whose
// specVersion defines no annotations: only Core 0.5.0 does. Such a pack is
// refused rather than shown with no match,
// because a pack that cannot carry annotations has nothing to match, and an
// org.judgmentpack.annotations member under another selector is an ordinary
// extension, not annotations. Experimental.
var ErrFilterSelector = errors.New("view: the pack's specVersion defines no annotations")

// Criterion is one criterion of a filter by annotation (EXPERIMENTAL): it
// matches an outcome or a rule whose annotations hold the key Key with the
// string Value, or with a list that holds Value. Key and Value are compared
// with the annotation's by exact code point: no case folding, no Unicode
// normalization, no trimming.
type Criterion struct {
	Key   string
	Value string
}

// ParseCriterion reads one criterion written key=value, split at the first
// equals sign: no annotation key holds one, while a value may.
func ParseCriterion(text string) (Criterion, error) {
	key, value, found := strings.Cut(text, "=")
	if !found {
		return Criterion{}, fmt.Errorf("%w: %s is not written key=value", ErrFilterCriterion, strconv.QuoteToASCII(text))
	}
	criterion := Criterion{Key: key, Value: value}
	if err := criterion.check(); err != nil {
		return Criterion{}, err
	}
	return criterion, nil
}

// annotationKey is the Core 0.5.0 schema's pattern for an annotation key
// ($defs/annotations, propertyNames), which also allows at most 64
// characters; a test holds the two equal on the cases it names.
var annotationKey = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// codePoints is a list of inclusive code point ranges.
type codePoints [][2]rune

func (ranges codePoints) holds(character rune) bool {
	for _, span := range ranges {
		if character >= span[0] && character <= span[1] {
			return true
		}
	}
	return false
}

// annotationRefused is Core 0.5.0's refused class for an annotation string
// (§9.2; the schema's $defs/annotationString, not): the
// controls, the line and paragraph separators, every bidirectional format
// character and every other invisible, default-ignorable character, none of
// which an annotation string may hold anywhere. Core makes the list, not
// a Unicode property, the rule, so it is written out here and does not follow
// the Go toolchain's Unicode tables.
var annotationRefused = codePoints{
	{0x0000, 0x001f}, {0x007f, 0x009f}, {0x00ad, 0x00ad}, {0x034f, 0x034f}, {0x061c, 0x061c},
	{0x115f, 0x1160}, {0x17b4, 0x17b5}, {0x180b, 0x180f}, {0x200b, 0x200f}, {0x2028, 0x202e},
	{0x2060, 0x206f}, {0x3164, 0x3164}, {0xfe00, 0xfe0f}, {0xfeff, 0xfeff}, {0xffa0, 0xffa0},
	{0xfff0, 0xfff8}, {0x1bca0, 0x1bca3}, {0x1d173, 0x1d17a}, {0xe0000, 0xe0fff},
}

// annotationWhiteSpace is Core 0.5.0's list of white-space characters
// (§9.2, the Unicode White_Space property written out), which an annotation
// string may not begin or end with; those the refused class does not hold
// are admitted between two other characters.
var annotationWhiteSpace = codePoints{
	{0x0009, 0x000d}, {0x0020, 0x0020}, {0x0085, 0x0085}, {0x00a0, 0x00a0}, {0x1680, 0x1680},
	{0x2000, 0x200a}, {0x2028, 0x2029}, {0x202f, 0x202f}, {0x205f, 0x205f}, {0x3000, 0x3000},
}

// check refuses a criterion whose key or value no annotation the Core 0.5.0
// schema admits could hold, so a criterion that could never match is an
// error and never an empty filtered view. Each message names a refused code
// point by its U+ number, never as itself.
func (c Criterion) check() error {
	if len(c.Key) > 64 || !annotationKey.MatchString(c.Key) {
		return fmt.Errorf("%w: the key %s is not an annotation key, which is lower-case letters and digits in hyphen-separated words, starting with a letter, at most 64 characters", ErrFilterCriterion, strconv.QuoteToASCII(c.Key))
	}
	if !utf8.ValidString(c.Value) {
		return fmt.Errorf("%w: the value for %s is not UTF-8", ErrFilterCriterion, c.Key)
	}
	characters := []rune(c.Value)
	if len(characters) < 1 || len(characters) > 256 {
		return fmt.Errorf("%w: the value for %s must have 1 to 256 characters, as an annotation string does", ErrFilterCriterion, c.Key)
	}
	for _, character := range characters {
		if annotationRefused.holds(character) {
			return fmt.Errorf("%w: the value for %s holds U+%04X, which no annotation string may hold", ErrFilterCriterion, c.Key, character)
		}
	}
	if first := characters[0]; annotationWhiteSpace.holds(first) {
		return fmt.Errorf("%w: the value for %s begins with U+%04X, white space, which no annotation string may begin or end with", ErrFilterCriterion, c.Key, first)
	}
	if last := characters[len(characters)-1]; annotationWhiteSpace.holds(last) {
		return fmt.Errorf("%w: the value for %s ends with U+%04X, white space, which no annotation string may begin or end with", ErrFilterCriterion, c.Key, last)
	}
	return nil
}

// matches reports whether annotations hold the criterion's key with its
// value, or with a list that holds its value.
func (c Criterion) matches(annotations []Annotation) bool {
	for _, annotation := range annotations {
		if annotation.Key != c.Key {
			continue
		}
		if annotation.List == nil {
			return annotation.Value == c.Value
		}
		return slices.Contains(annotation.List, c.Value)
	}
	return false
}

// FilteredView is the outcomes and rules of one pack whose annotations match
// every criterion of a filter (EXPERIMENTAL). It is not the pack: it leaves
// out everything else the document holds, and it says so in both encodings,
// with the filter, the counts and the full identity of the pack it was taken
// from. Each matched member keeps its document order and its position in the
// pack.
//
// A FilteredView is the caller's own copy, as a PackView is: JSON and
// Markdown return the encodings of the filtered view as sdk.Pack.FilteredView
// built it, and the zero FilteredView has none.
type FilteredView struct {
	Identity Identity
	Title    string
	// Criteria is the filter, every criterion of which a member matches,
	// ordered by key and then value (UTF-16 code units), so the same filter
	// given in another order is stated, and encoded, the same way.
	Criteria []Criterion
	// Outcomes are the matched outcomes in document order; OutcomeCount is
	// the number of outcomes the pack has.
	Outcomes     []FilteredOutcome
	OutcomeCount int
	// Rules are the matched rules in document order; RuleCount is the number
	// of rules the pack has.
	Rules     []FilteredRule
	RuleCount int

	json     []byte
	markdown []byte
}

// FilteredOutcome is one matched outcome and its 0-based index in the pack's
// outcomes.
type FilteredOutcome struct {
	Index   int
	Outcome Outcome
}

// FilteredRule is one matched rule and its 0-based index in the pack's rules.
type FilteredRule struct {
	Index int
	Rule  Rule
}

// JSON returns the filtered view in the canonical JSON form the view uses,
// with no trailing newline: an object with the members digest, filter (the
// criteria and the match rule, "all"), filteredViewVersion, id, outcomes and
// rules (each with count, the number in the pack, and matched, the matched
// members in document order, each with its JSON Pointer in the pack and the
// member as the view writes it), profile when the pack states one,
// specVersion, title and version. The caller owns the returned slice.
// Experimental.
func (f FilteredView) JSON() []byte {
	return bytes.Clone(f.json)
}

// Markdown returns the filtered view rendered as a plain Markdown document,
// ending in one newline, escaped as the view's Markdown is. The caller owns
// the returned slice. Experimental.
func (f FilteredView) Markdown() []byte {
	return bytes.Clone(f.markdown)
}

// filter is the filtered view of a built view: the outcomes and rules whose
// annotations match every criterion. A criterion is refused before the
// selector is looked at, so the same criteria are refused the same way on
// every pack.
func filter(view PackView, criteria []Criterion) (FilteredView, error) {
	if len(criteria) == 0 {
		return FilteredView{}, fmt.Errorf("%w: a filter needs at least one criterion", ErrFilterCriterion)
	}
	ordered := slices.Clone(criteria)
	for _, criterion := range ordered {
		if err := criterion.check(); err != nil {
			return FilteredView{}, err
		}
	}
	slices.SortFunc(ordered, func(left, right Criterion) int {
		if order := compareUTF16(left.Key, right.Key); order != 0 {
			return order
		}
		return compareUTF16(left.Value, right.Value)
	})
	for index := 1; index < len(ordered); index++ {
		if ordered[index] == ordered[index-1] {
			return FilteredView{}, fmt.Errorf("%w: %s=%s is given twice", ErrFilterCriterion, ordered[index].Key, strconv.QuoteToASCII(ordered[index].Value))
		}
	}
	if !result.HasCore050Rules(view.Identity.SpecVersion) {
		return FilteredView{}, fmt.Errorf("%w: it is %s, and only Core %s defines annotations", ErrFilterSelector, strconv.QuoteToASCII(view.Identity.SpecVersion), result.Core050SpecVersion)
	}
	matchesAll := func(annotations []Annotation) bool {
		for _, criterion := range ordered {
			if !criterion.matches(annotations) {
				return false
			}
		}
		return true
	}
	filtered := FilteredView{
		Identity:     view.Identity,
		Title:        view.Title,
		Criteria:     ordered,
		Outcomes:     []FilteredOutcome{},
		OutcomeCount: len(view.Outcomes),
		Rules:        []FilteredRule{},
		RuleCount:    len(view.Rules),
	}
	for index, outcome := range view.Outcomes {
		if matchesAll(outcome.Annotations) {
			filtered.Outcomes = append(filtered.Outcomes, FilteredOutcome{Index: index, Outcome: outcome})
		}
	}
	for index, rule := range view.Rules {
		if matchesAll(rule.Annotations) {
			filtered.Rules = append(filtered.Rules, FilteredRule{Index: index, Rule: rule})
		}
	}
	var err error
	if filtered.json, err = encodeFilteredJSON(filtered); err != nil {
		return FilteredView{}, err
	}
	filtered.markdown = renderFilteredMarkdown(filtered)
	return filtered, nil
}

func encodeFilteredJSON(f FilteredView) ([]byte, error) {
	criteria := make([]any, 0, len(f.Criteria))
	for _, criterion := range f.Criteria {
		criteria = append(criteria, map[string]any{"key": criterion.Key, "value": criterion.Value})
	}
	outcomes := make([]any, 0, len(f.Outcomes))
	for _, matched := range f.Outcomes {
		outcomes = append(outcomes, map[string]any{"pointer": "/outcomes/" + strconv.Itoa(matched.Index), "outcome": outcomeValue(matched.Outcome)})
	}
	rules := make([]any, 0, len(f.Rules))
	for _, matched := range f.Rules {
		rule, err := ruleValue(matched.Rule)
		if err != nil {
			return nil, err
		}
		rules = append(rules, map[string]any{"pointer": "/rules/" + strconv.Itoa(matched.Index), "rule": rule})
	}
	document := map[string]any{
		"digest":              f.Identity.Digest,
		"filter":              map[string]any{"criteria": criteria, "match": "all"},
		"filteredViewVersion": FilteredViewVersion,
		"id":                  f.Identity.ID,
		"outcomes":            map[string]any{"count": rawJSON(strconv.Itoa(f.OutcomeCount)), "matched": outcomes},
		"rules":               map[string]any{"count": rawJSON(strconv.Itoa(f.RuleCount)), "matched": rules},
		"specVersion":         f.Identity.SpecVersion,
		"title":               f.Title,
		"version":             f.Identity.Version,
	}
	putString(document, "profile", f.Identity.Profile)
	encoded, err := canonicalText(document)
	if err != nil {
		return nil, &fault{message: "The filtered view does not encode: " + err.Error() + "."}
	}
	return []byte(encoded), nil
}

// renderFilteredMarkdown renders a filtered view, filtered view version 1: a
// title heading that says it is filtered, a sentence that says it is not the
// pack, the identity and the filter, the counts, then each matched outcome and
// rule under a heading that gives its position in the pack, written as the
// view writes it.
func renderFilteredMarkdown(f FilteredView) []byte {
	m := &markdownWriter{}
	m.line(0, "# Filtered view: "+escapeMarkdown(f.Title))
	m.builder.WriteByte('\n')
	m.line(0, "This is not the pack. It shows only the outcomes and rules whose annotations match every criterion below, each at its position in the pack, and leaves out every other member.")
	m.builder.WriteByte('\n')
	m.field(0, "Id", f.Identity.ID)
	m.field(0, "Version", f.Identity.Version)
	m.field(0, "Spec version", f.Identity.SpecVersion)
	m.field(0, "Profile", f.Identity.Profile)
	m.line(0, "- Digest: "+f.Identity.Digest)
	m.line(0, "- Filtered view version: "+FilteredViewVersion)
	m.line(0, "- Filter, every criterion matching:")
	for _, criterion := range f.Criteria {
		m.line(1, "- "+annotationText(criterion.Key)+" is "+annotationText(criterion.Value))
	}
	m.line(0, "- Outcomes matched: "+strconv.Itoa(len(f.Outcomes))+" of "+strconv.Itoa(f.OutcomeCount))
	m.line(0, "- Rules matched: "+strconv.Itoa(len(f.Rules))+" of "+strconv.Itoa(f.RuleCount))

	m.section(2, "Outcomes")
	if len(f.Outcomes) == 0 {
		m.line(0, "*none matched*")
	}
	for _, matched := range f.Outcomes {
		outcome := matched.Outcome
		m.section(3, "Outcome "+strconv.Itoa(matched.Index+1)+" of "+strconv.Itoa(f.OutcomeCount)+": "+escapeMarkdown(outcome.ID))
		m.field(0, "Label", outcome.Label)
		m.field(0, "Description", outcome.Description)
		m.annotatedExtensions(0, outcome.Extensions, outcome.Annotations)
	}

	m.section(2, "Rules")
	if len(f.Rules) == 0 {
		m.line(0, "*none matched*")
	}
	for _, matched := range f.Rules {
		m.section(3, "Rule "+strconv.Itoa(matched.Index+1)+" of "+strconv.Itoa(f.RuleCount)+": "+escapeMarkdown(matched.Rule.ID))
		m.rule(matched.Rule)
	}
	return []byte(m.builder.String())
}
