// Package view is the view of one admitted pack document that sdk.Pack.View
// returns: the whole document as typed data, with a canonical JSON encoding
// and a Markdown rendering for review (ADR-0074). Only package sdk builds a
// PackView; a PackView made any other way is the zero value or a copy of one
// View returned. For a pack under the staged draft selector 0.4.0-draft, the
// view carries its value sets and annotations, and FilteredView, which only
// sdk.Pack.FilteredView builds, is its outcomes and rules filtered by
// annotation (ADR-0075). Everything in this package is experimental.
package view

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evalform"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/viewhook"
)

// ViewVersion is the version of the view format: the members of PackView.JSON
// and the layout of PackView.Markdown, which carry it in band. It moves
// whenever the bytes View gives for a pack an earlier view version could
// describe would change, in either encoding; it does not move when View
// learns to describe packs it refused before, such as those of a newly
// admitted specification version: the value sets and annotations of the
// staged draft selector 0.4.0-draft are members no earlier pack holds, so
// describing them left it at "1". Experimental, like the rest of the view.
const ViewVersion = "1"

// Identity is what names the viewed pack: the id, version and specVersion
// members of the document, its profile member when the document states one,
// and the digest of the exact bytes the pack was loaded from. It holds the
// same values sdk.Pack.Identity returns for the same pack. Experimental.
type Identity struct {
	ID          string
	Version     string
	SpecVersion string
	// Profile is the document's profile member, and empty when the document
	// does not state one. It is never inferred: the schema's default is not
	// a statement the document made, and the evaluator applies that default
	// whether or not the view shows it.
	Profile string
	// Digest is "sha256:" followed by the 64 lower-case hex digits of the
	// SHA-256 of the bytes the pack was loaded from, the form the
	// reviewed-set lock and the audit record use for a pack.
	Digest string
}

// PackView is the whole of one admitted pack document as typed data, in the
// document's own terms (JPS Core §3). Experimental.
//
// It is faithful: every member of the document is carried, nothing is
// inferred, and an absent member stays absent. An absent optional string is
// the empty string, which the schema never allows a present one to be; an
// absent optional array is nil and a present empty one is a non-nil empty
// slice; an absent optional object is a nil pointer. Arrays keep document
// order. Values whose shape the specification leaves to the author — every
// extensions member, and a condition's value — are carried as canonical JSON
// text, with numbers exactly as written.
//
// A PackView is the caller's own copy. JSON and Markdown return the encodings
// of the view as View built it; changing the fields of the value afterwards
// does not change them. A copy of a PackView shares its slices and pointers
// with the original, so an edit through one is seen through the other, while
// neither copy's encodings follow it. The zero PackView, and any PackView
// sdk.Pack.View did not return, has no encodings: JSON and Markdown return
// nil.
type PackView struct {
	Identity    Identity
	Title       string
	Description string
	Decision    Decision
	// Applicability is the document's applicability condition, or nil.
	Applicability        *Condition
	EvidenceRequirements []EvidenceRequirement
	Sources              []Source
	Outcomes             []Outcome
	Rules                []Rule
	Exceptions           []Exception
	// ValueSets is the document's valueSets member, which only the staged
	// draft selector 0.4.0-draft defines (EXPERIMENTAL).
	ValueSets       []ValueSet
	FallbackOutcome string
	Escalation      *Escalation
	Metadata        *Metadata
	// Extensions is the document's top-level extensions member, without its
	// annotations member, which Annotations carries typed. It is non-nil
	// exactly when the document has an extensions member, so an extensions
	// object that holds only annotations is a non-nil empty slice.
	Extensions []Extension
	// Annotations is the annotations member of the document's top-level
	// extensions object (org.judgmentpack.annotations, staged draft selector
	// 0.4.0-draft only, EXPERIMENTAL), or nil when it has none.
	Annotations []Annotation

	json     []byte
	markdown []byte
}

// JSON returns the view in its canonical JSON form, with no trailing newline:
// an object with the members digest, pack (the document, member for member as
// the view carries it) and viewVersion. Object members are ordered by the
// UTF-16 code units of their names and strings carry the RFC 8785 escapes; a
// number is written as the literal token the document holds, not in the RFC
// 8785 number form, so it reads exactly as written. Two calls on the same pack
// bytes, in the same runtime version, return the same bytes. The caller owns
// the returned slice. Experimental.
func (v PackView) JSON() []byte {
	return bytes.Clone(v.json)
}

// Markdown returns the view rendered as a plain Markdown document, ending in
// one newline. The layout is the one docs/building-with-packs.md describes
// for this view version, and every string taken from the pack is escaped so
// that no Markdown syntax in it can add structure to the rendered document;
// the document says what a renderer or forge may still link. The caller owns the
// returned slice. Experimental.
func (v PackView) Markdown() []byte {
	return bytes.Clone(v.markdown)
}

// Decision is the document's decision member.
type Decision struct {
	Intent     string
	Question   string
	Extensions []Extension
}

// EvidenceRequirement is one member of evidenceRequirements.
type EvidenceRequirement struct {
	ID          string
	Description string
	Required    bool
	Kind        string
	Extensions  []Extension
}

// Source is one member of sources.
type Source struct {
	ID          string
	Title       string
	Publisher   string
	PublishedAt string
	Locator     Locator
	Citation    *Citation
	Rights      string
	Extensions  []Extension
}

// Locator is a source's locator member.
type Locator struct {
	Kind  string
	Value string
}

// Citation is a source's citation member.
type Citation struct {
	Location string
	Excerpt  string
}

// Outcome is one member of outcomes.
type Outcome struct {
	ID          string
	Label       string
	Description string
	Extensions  []Extension
	// Annotations is the annotations member of the outcome's extensions, as
	// for PackView.Annotations.
	Annotations []Annotation
}

// Rule is one member of rules, in document order.
type Rule struct {
	ID                      string
	Description             string
	When                    Condition
	Outcome                 string
	OnUnknown               string
	EvidenceRequirementRefs []string
	SourceRefs              []string
	Rationale               string
	Extensions              []Extension
	// Annotations is the annotations member of the rule's extensions, as for
	// PackView.Annotations.
	Annotations []Annotation
}

// Exception is one member of exceptions, in document order.
type Exception struct {
	ID          string
	Description string
	When        Condition
	Effect      string
	TargetRule  string
	Outcome     string
	OnUnknown   string
	SourceRefs  []string
	Extensions  []Extension
}

// Escalation is the document's escalation member. Target is nil when the
// document declares routes instead, which JPS Core 0.3.0 allows; Routes is
// nil when the document declares none.
type Escalation struct {
	Triggers   []string
	Target     *Target
	Message    string
	Routes     []Route
	Extensions []Extension
}

// Route is one member of an escalation's routes.
type Route struct {
	Target   Target
	Triggers []string
}

// Metadata is the document's metadata member.
type Metadata struct {
	Authors            []string
	CreatedAt          string
	License            string
	RequiredExtensions []string
	Reviews            []Review
	Extensions         []Extension
}

// Review is one member of metadata.reviews.
type Review struct {
	Reviewer    string
	ReviewedAt  string
	Disposition string
	Note        string
}

// Condition is one condition node. Op names its shape, and only the fields
// that shape has are set:
//
//   - "literal": Value, the canonical JSON true or false;
//   - "all" and "any": Conditions, in document order;
//   - "not": Condition;
//   - "fact": Path (a JSON pointer, which may be the empty string), Operator
//     and Value, the canonical JSON of the comparison value; or, under the
//     staged draft selector 0.4.0-draft, ValueSet in place of Value: the id
//     of the value set the condition references, as written and never
//     expanded;
//   - "evidence-present": EvidenceRequirement.
type Condition struct {
	Op                  string
	Value               string
	Conditions          []Condition
	Condition           *Condition
	Path                string
	Operator            string
	ValueSet            string
	EvidenceRequirement string
}

// ValueSet is one member of valueSets (staged draft selector 0.4.0-draft,
// EXPERIMENTAL): its id, its description, and its entries in document order.
type ValueSet struct {
	ID          string
	Description string
	Entries     []ValueSetEntry
}

// ValueSetEntry is one entry of a value set: its value as canonical JSON,
// with numbers exactly as written, its description, and its extensions
// member, which may hold only annotations. Extensions is non-nil exactly when
// the entry has an extensions member.
type ValueSetEntry struct {
	Value       string
	Description string
	Extensions  []Extension
	Annotations []Annotation
}

// Annotation is one member of an annotations object (staged draft selector
// 0.4.0-draft, EXPERIMENTAL). Its value is one string or a list of strings,
// and the view keeps the two apart: Value is the string, and empty for a
// list, which the schema never allows a string annotation to be; List is the
// list in document order, and nil for a string. An annotations slice is
// ordered by the UTF-16 code units of the keys, the canonical member order.
type Annotation struct {
	Key   string
	Value string
	List  []string
}

// Extension is one member of an extensions object: its name, and its value as
// canonical JSON. An extensions slice is ordered by the UTF-16 code units of
// the names, the canonical member order.
type Extension struct {
	Name  string
	Value string
}

// Target is one destination named by an escalation: its target member, or
// the target of one of its routes. Experimental.
type Target struct {
	Kind string
	Name string
}

// viewReader walks an admitted root into a PackView. It records the first
// construct it cannot represent and keeps walking harmlessly, so the walk has
// one exit and one error.
type viewReader struct {
	version string
	err     error
}

func (r *viewReader) fail(at *location, detail string) {
	if r.err == nil {
		pointer := at.String()
		if pointer == "" {
			pointer = "<root>"
		}
		r.err = &fault{message: fmt.Sprintf("The admitted pack holds a construct at %s that this view cannot represent (%s); no partial view is returned.", pointer, detail)}
	}
}

// location is a JSON pointer into the admitted document, kept as a chain of
// reference tokens and written out only when a failure names it, so reading a
// member builds no string. The nil location is the whole document.
type location struct {
	parent *location
	name   string
	// index is the array index of an item, and negative for a member.
	index int
}

func (l *location) member(name string) *location {
	return &location{parent: l, name: name, index: -1}
}

func (l *location) item(index int) *location {
	return &location{parent: l, index: index}
}

// pointerEscaper writes a member name as a JSON pointer reference token.
var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

func (l *location) String() string {
	var tokens []string
	for at := l; at != nil; at = at.parent {
		if at.index >= 0 {
			tokens = append(tokens, strconv.Itoa(at.index))
		} else {
			tokens = append(tokens, pointerEscaper.Replace(at.name))
		}
	}
	var builder strings.Builder
	for index := len(tokens) - 1; index >= 0; index-- {
		builder.WriteByte('/')
		builder.WriteString(tokens[index])
	}
	return builder.String()
}

// viewObject is one JSON object being read. Every member read is recorded,
// and done refuses a member nothing read: a member this view does not know is
// an error, never a member silently left out.
type viewObject struct {
	reader  *viewReader
	pointer *location
	members map[string]any
	read    map[string]bool
}

func (r *viewReader) object(value any, pointer *location) *viewObject {
	members, ok := value.(map[string]any)
	if !ok {
		r.fail(pointer, "expected an object")
	}
	return &viewObject{reader: r, pointer: pointer, members: members, read: map[string]bool{}}
}

func (o *viewObject) at(name string) *location {
	return o.pointer.member(name)
}

func (o *viewObject) take(name string) (any, bool) {
	o.read[name] = true
	value, present := o.members[name]
	return value, present
}

func (o *viewObject) required(name string) any {
	value, present := o.take(name)
	if !present {
		o.reader.fail(o.at(name), "a required member is missing")
	}
	return value
}

// text reads a string. An empty one is refused unless the member allows it,
// because the view's absent optional string is the empty string.
func (r *viewReader) text(value any, pointer *location, emptyAllowed bool) string {
	text, ok := value.(string)
	if !ok {
		r.fail(pointer, "expected a string")
		return ""
	}
	if text == "" && !emptyAllowed {
		r.fail(pointer, "an empty string, which the view cannot tell apart from an absent member")
	}
	return text
}

func (o *viewObject) requiredString(name string) string {
	value, present := o.take(name)
	if !present {
		o.reader.fail(o.at(name), "a required member is missing")
		return ""
	}
	return o.reader.text(value, o.at(name), false)
}

func (o *viewObject) optionalString(name string) string {
	value, present := o.take(name)
	if !present {
		return ""
	}
	return o.reader.text(value, o.at(name), false)
}

func (o *viewObject) requiredBool(name string) bool {
	value, present := o.take(name)
	if !present {
		o.reader.fail(o.at(name), "a required member is missing")
		return false
	}
	flag, ok := value.(bool)
	if !ok {
		o.reader.fail(o.at(name), "expected a Boolean")
	}
	return flag
}

// array reads an array member. The slice is nil exactly when the member is
// absent.
func (o *viewObject) array(name string, required bool) []any {
	value, present := o.take(name)
	if !present {
		if required {
			o.reader.fail(o.at(name), "a required member is missing")
		}
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		o.reader.fail(o.at(name), "expected an array")
		return nil
	}
	if items == nil {
		items = []any{}
	}
	return items
}

func (o *viewObject) strings(name string, required bool) []string {
	items := o.array(name, required)
	if items == nil {
		return nil
	}
	values := make([]string, 0, len(items))
	for index, item := range items {
		values = append(values, o.reader.text(item, o.at(name).item(index), false))
	}
	return values
}

// extensions reads the object's extensions member. Its values are the
// author's, carried as canonical JSON.
func (o *viewObject) extensions() []Extension {
	return o.extensionMembers("")
}

// annotatedExtensions reads the extensions member of an object that may hold
// annotations under the staged draft selector (the root, an outcome, a rule
// and a value-set entry): its annotations member as typed Annotations, and
// every other member as an Extension. Under every other version no
// annotations member exists, and every member is an Extension.
func (o *viewObject) annotatedExtensions() ([]Extension, []Annotation) {
	if !result.IsStagedDraft(o.reader.version) {
		return o.extensions(), nil
	}
	extensions := o.extensionMembers(evalform.AnnotationsName)
	members, _ := o.members["extensions"].(map[string]any)
	value, present := members[evalform.AnnotationsName]
	if !present {
		return extensions, nil
	}
	return extensions, o.reader.annotations(value, o.at("extensions").member(evalform.AnnotationsName))
}

// annotations reads one annotations object, its members in canonical order.
func (r *viewReader) annotations(value any, pointer *location) []Annotation {
	members, ok := value.(map[string]any)
	if !ok {
		r.fail(pointer, "expected an object")
		return nil
	}
	keys := make([]string, 0, len(members))
	for key := range members {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareUTF16)
	annotations := make([]Annotation, 0, len(keys))
	for _, key := range keys {
		at := pointer.member(key)
		switch typed := members[key].(type) {
		case string:
			annotations = append(annotations, Annotation{Key: key, Value: r.text(typed, at, false)})
		case []any:
			list := make([]string, 0, len(typed))
			for index, item := range typed {
				list = append(list, r.text(item, at.item(index), false))
			}
			annotations = append(annotations, Annotation{Key: key, List: list})
		default:
			r.fail(at, "an annotation value must be a string or a list of strings")
		}
	}
	return annotations
}

// extensionMembers reads the extensions member, leaving out the member named
// typed, which the caller reads as a typed field; "" leaves out nothing.
func (o *viewObject) extensionMembers(typed string) []Extension {
	value, present := o.take("extensions")
	if !present {
		return nil
	}
	members, ok := value.(map[string]any)
	if !ok {
		o.reader.fail(o.at("extensions"), "expected an object")
		return nil
	}
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	slices.SortFunc(names, compareUTF16)
	extensions := make([]Extension, 0, len(names))
	for _, name := range names {
		if typed != "" && name == typed {
			continue
		}
		encoded, err := canonicalText(members[name])
		if err != nil {
			o.reader.fail(o.at("extensions").member(name), err.Error())
		}
		extensions = append(extensions, Extension{Name: name, Value: encoded})
	}
	return extensions
}

// done refuses every member of the object nothing read, naming the first in
// canonical order so the error is the same on every run.
func (o *viewObject) done() {
	unread := []string{}
	for name := range o.members {
		if !o.read[name] {
			unread = append(unread, name)
		}
	}
	if len(unread) > 0 {
		slices.SortFunc(unread, compareUTF16)
		o.reader.fail(o.at(unread[0]), "a member this view does not carry")
	}
}

// The specVersions whose documents this view reads. Each is a version Load
// admits; a document of another version is refused before any member is read.
// The staged draft selector is read with Core 0.3.0's members
// (result.HasCore030Rules) and its own value sets and annotations
// (result.IsStagedDraft); it is EXPERIMENTAL and may be renamed or changed.
const (
	viewDraftVersion  = "0.2.0-draft"
	viewCoreVersion   = "0.3.0"
	viewStagedVersion = result.StagedDraftSpecVersion
)

// buildView reads an admitted root, member by member, into a PackView.
func buildView(root map[string]any) (PackView, error) {
	r := &viewReader{}
	o := r.object(root, nil)
	view := PackView{}
	view.Identity.SpecVersion = o.requiredString("specVersion")
	r.version = view.Identity.SpecVersion
	if r.version != viewDraftVersion && !result.HasCore030Rules(r.version) {
		r.fail(o.at("specVersion"), "a specification version this view does not describe")
		return PackView{}, r.err
	}
	view.Identity.ID = o.requiredString("id")
	view.Identity.Version = o.requiredString("version")
	if result.HasCore030Rules(r.version) {
		view.Identity.Profile = o.optionalString("profile")
	}
	view.Title = o.requiredString("title")
	view.Description = o.optionalString("description")
	view.Decision = r.decision(o.required("decision"), o.at("decision"))
	if value, present := o.take("applicability"); present {
		condition := r.condition(value, o.at("applicability"))
		view.Applicability = &condition
	}
	if items := o.array("evidenceRequirements", false); items != nil {
		view.EvidenceRequirements = make([]EvidenceRequirement, 0, len(items))
		for index, item := range items {
			view.EvidenceRequirements = append(view.EvidenceRequirements, r.evidenceRequirement(item, indexed(o.at("evidenceRequirements"), index)))
		}
	}
	if items := o.array("sources", false); items != nil {
		view.Sources = make([]Source, 0, len(items))
		for index, item := range items {
			view.Sources = append(view.Sources, r.source(item, indexed(o.at("sources"), index)))
		}
	}
	outcomes := o.array("outcomes", true)
	view.Outcomes = make([]Outcome, 0, len(outcomes))
	for index, item := range outcomes {
		view.Outcomes = append(view.Outcomes, r.outcome(item, indexed(o.at("outcomes"), index)))
	}
	rules := o.array("rules", true)
	view.Rules = make([]Rule, 0, len(rules))
	for index, item := range rules {
		view.Rules = append(view.Rules, r.rule(item, indexed(o.at("rules"), index)))
	}
	if items := o.array("exceptions", false); items != nil {
		view.Exceptions = make([]Exception, 0, len(items))
		for index, item := range items {
			view.Exceptions = append(view.Exceptions, r.exception(item, indexed(o.at("exceptions"), index)))
		}
	}
	if result.IsStagedDraft(r.version) {
		if items := o.array("valueSets", false); items != nil {
			view.ValueSets = make([]ValueSet, 0, len(items))
			for index, item := range items {
				view.ValueSets = append(view.ValueSets, r.valueSet(item, indexed(o.at("valueSets"), index)))
			}
		}
	}
	view.FallbackOutcome = o.optionalString("fallbackOutcome")
	if value, present := o.take("escalation"); present {
		view.Escalation = r.escalation(value, o.at("escalation"))
	}
	if value, present := o.take("metadata"); present {
		view.Metadata = r.metadata(value, o.at("metadata"))
	}
	view.Extensions, view.Annotations = o.annotatedExtensions()
	o.done()
	if r.err != nil {
		return PackView{}, r.err
	}
	return view, nil
}

// valueSet reads one value set and its entries, in document order.
func (r *viewReader) valueSet(value any, pointer *location) ValueSet {
	o := r.object(value, pointer)
	set := ValueSet{ID: o.requiredString("id"), Description: o.optionalString("description")}
	entries := o.array("entries", true)
	set.Entries = make([]ValueSetEntry, 0, len(entries))
	for index, item := range entries {
		entry := r.object(item, indexed(o.at("entries"), index))
		read := ValueSetEntry{
			Value:       r.canonicalMember(entry.required("value"), entry.at("value")),
			Description: entry.optionalString("description"),
		}
		read.Extensions, read.Annotations = entry.annotatedExtensions()
		entry.done()
		set.Entries = append(set.Entries, read)
	}
	o.done()
	return set
}

func indexed(pointer *location, index int) *location {
	return pointer.item(index)
}

func (r *viewReader) decision(value any, pointer *location) Decision {
	o := r.object(value, pointer)
	decision := Decision{
		Intent:     o.requiredString("intent"),
		Question:   o.requiredString("question"),
		Extensions: o.extensions(),
	}
	o.done()
	return decision
}

func (r *viewReader) evidenceRequirement(value any, pointer *location) EvidenceRequirement {
	o := r.object(value, pointer)
	requirement := EvidenceRequirement{
		ID:          o.requiredString("id"),
		Description: o.requiredString("description"),
		Required:    o.requiredBool("required"),
		Kind:        o.optionalString("kind"),
		Extensions:  o.extensions(),
	}
	o.done()
	return requirement
}

func (r *viewReader) source(value any, pointer *location) Source {
	o := r.object(value, pointer)
	source := Source{
		ID:          o.requiredString("id"),
		Title:       o.requiredString("title"),
		Publisher:   o.optionalString("publisher"),
		PublishedAt: o.optionalString("publishedAt"),
	}
	locator := r.object(o.required("locator"), o.at("locator"))
	source.Locator = Locator{Kind: locator.requiredString("kind"), Value: locator.requiredString("value")}
	locator.done()
	if value, present := o.take("citation"); present {
		citation := r.object(value, o.at("citation"))
		source.Citation = &Citation{Location: citation.requiredString("location"), Excerpt: citation.requiredString("excerpt")}
		citation.done()
	}
	source.Rights = o.optionalString("rights")
	source.Extensions = o.extensions()
	o.done()
	return source
}

func (r *viewReader) outcome(value any, pointer *location) Outcome {
	o := r.object(value, pointer)
	outcome := Outcome{
		ID:          o.requiredString("id"),
		Label:       o.requiredString("label"),
		Description: o.optionalString("description"),
	}
	outcome.Extensions, outcome.Annotations = o.annotatedExtensions()
	o.done()
	return outcome
}

func (r *viewReader) rule(value any, pointer *location) Rule {
	o := r.object(value, pointer)
	rule := Rule{
		ID:                      o.requiredString("id"),
		Description:             o.requiredString("description"),
		When:                    r.condition(o.required("when"), o.at("when")),
		Outcome:                 o.requiredString("outcome"),
		OnUnknown:               o.requiredString("onUnknown"),
		EvidenceRequirementRefs: o.strings("evidenceRequirementRefs", false),
		SourceRefs:              o.strings("sourceRefs", false),
		Rationale:               o.optionalString("rationale"),
	}
	rule.Extensions, rule.Annotations = o.annotatedExtensions()
	o.done()
	return rule
}

func (r *viewReader) exception(value any, pointer *location) Exception {
	o := r.object(value, pointer)
	exception := Exception{
		ID:          o.requiredString("id"),
		Description: o.requiredString("description"),
		When:        r.condition(o.required("when"), o.at("when")),
		Effect:      o.requiredString("effect"),
		TargetRule:  o.optionalString("targetRule"),
		Outcome:     o.optionalString("outcome"),
		OnUnknown:   o.requiredString("onUnknown"),
		SourceRefs:  o.strings("sourceRefs", false),
		Extensions:  o.extensions(),
	}
	o.done()
	return exception
}

func (r *viewReader) target(value any, pointer *location) Target {
	o := r.object(value, pointer)
	target := Target{Kind: o.requiredString("kind"), Name: o.requiredString("name")}
	o.done()
	return target
}

// escalation reads the escalation member. Under 0.2.0-draft the target is
// required and routes do not exist, so a routes member is left unread and
// refused by done; under 0.3.0 and the staged draft selector either may be
// present.
func (r *viewReader) escalation(value any, pointer *location) *Escalation {
	o := r.object(value, pointer)
	escalation := &Escalation{Triggers: o.strings("triggers", true)}
	if declared, present := o.take("target"); present {
		target := r.target(declared, o.at("target"))
		escalation.Target = &target
	} else if r.version == viewDraftVersion {
		r.fail(o.at("target"), "a required member is missing")
	}
	escalation.Message = o.optionalString("message")
	if result.HasCore030Rules(r.version) {
		if items := o.array("routes", false); items != nil {
			escalation.Routes = make([]Route, 0, len(items))
			for index, item := range items {
				routePointer := indexed(o.at("routes"), index)
				route := r.object(item, routePointer)
				escalation.Routes = append(escalation.Routes, Route{
					Target:   r.target(route.required("target"), route.at("target")),
					Triggers: route.strings("triggers", false),
				})
				route.done()
			}
		}
	}
	escalation.Extensions = o.extensions()
	o.done()
	return escalation
}

func (r *viewReader) metadata(value any, pointer *location) *Metadata {
	o := r.object(value, pointer)
	metadata := &Metadata{
		Authors:            o.strings("authors", false),
		CreatedAt:          o.optionalString("createdAt"),
		License:            o.optionalString("license"),
		RequiredExtensions: o.strings("requiredExtensions", false),
	}
	if items := o.array("reviews", false); items != nil {
		metadata.Reviews = make([]Review, 0, len(items))
		for index, item := range items {
			review := r.object(item, indexed(o.at("reviews"), index))
			metadata.Reviews = append(metadata.Reviews, Review{
				Reviewer:    review.requiredString("reviewer"),
				ReviewedAt:  review.requiredString("reviewedAt"),
				Disposition: review.requiredString("disposition"),
				Note:        review.optionalString("note"),
			})
			review.done()
		}
	}
	metadata.Extensions = o.extensions()
	o.done()
	return metadata
}

// condition reads one condition node. The shapes are the JPS Core condition
// grammar's, the same under every version this view reads but for the staged
// draft selector's valueSet reference; an op outside it is refused rather than
// carried as a node of no known shape.
func (r *viewReader) condition(value any, pointer *location) Condition {
	o := r.object(value, pointer)
	condition := Condition{Op: o.requiredString("op")}
	switch condition.Op {
	case "literal":
		literal := o.required("value")
		if _, ok := literal.(bool); !ok {
			r.fail(o.at("value"), "a literal condition's value must be a Boolean")
		}
		condition.Value = r.canonicalMember(literal, o.at("value"))
	case "all", "any":
		items := o.array("conditions", true)
		condition.Conditions = make([]Condition, 0, len(items))
		for index, item := range items {
			condition.Conditions = append(condition.Conditions, r.condition(item, indexed(o.at("conditions"), index)))
		}
	case "not":
		inner := r.condition(o.required("condition"), o.at("condition"))
		condition.Condition = &inner
	case "fact":
		path, present := o.take("path")
		if !present {
			r.fail(o.at("path"), "a required member is missing")
		} else {
			// The empty pointer names the whole facts document, so an empty
			// path is a value, not an absence.
			condition.Path = r.text(path, o.at("path"), true)
		}
		condition.Operator = o.requiredString("operator")
		if !result.IsStagedDraft(r.version) {
			condition.Value = r.canonicalMember(o.required("value"), o.at("value"))
			break
		}
		// Under the staged draft selector a fact condition carries value or a
		// valueSet reference, which the view shows as the set's id and never
		// expands: the view is the document, not its evaluation form.
		value, hasValue := o.take("value")
		reference, hasReference := o.take("valueSet")
		if hasValue {
			condition.Value = r.canonicalMember(value, o.at("value"))
		}
		if hasReference {
			condition.ValueSet = r.text(reference, o.at("valueSet"), false)
		}
		if !hasValue && !hasReference {
			r.fail(o.at("value"), "a required member is missing")
		}
	case "evidence-present":
		condition.EvidenceRequirement = o.requiredString("evidenceRequirement")
	default:
		r.fail(o.at("op"), "a condition op this view does not describe")
	}
	o.done()
	return condition
}

func (r *viewReader) canonicalMember(value any, pointer *location) string {
	encoded, err := canonicalText(value)
	if err != nil {
		r.fail(pointer, err.Error())
	}
	return encoded
}

// fault is an admitted construct the view cannot represent, or a view that
// does not encode. Package sdk reports its message as a fault of this
// runtime; it is never the caller's input refused.
type fault struct{ message string }

func (f *fault) Error() string { return f.message }

// build is the view of one admitted root, with the digest of the bytes it was
// admitted from, and its two encodings.
func build(root map[string]any, digest string) (PackView, error) {
	built, err := buildView(root)
	if err != nil {
		return PackView{}, err
	}
	built.Identity.Digest = digest
	if built.json, err = encodeViewJSON(built); err != nil {
		return PackView{}, err
	}
	built.markdown = renderMarkdown(built)
	return built, nil
}

func init() {
	viewhook.Build = func(root map[string]any, digest string) (any, string) {
		built, err := build(root, digest)
		if err != nil {
			return nil, err.Error()
		}
		return built, ""
	}
	viewhook.Filter = func(built any, criteria any) (any, error) {
		described, ok := built.(PackView)
		if !ok || described.json == nil {
			return nil, &fault{message: "The filter was given no view that View built."}
		}
		given, ok := criteria.([]Criterion)
		if !ok {
			return nil, &fault{message: "The filter was given no criteria."}
		}
		return filter(described, given)
	}
}
