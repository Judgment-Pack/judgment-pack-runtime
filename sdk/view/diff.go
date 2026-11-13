package view

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/viewhook"
)

// The structural comparison of pack documents (ADR-0086). It reads two views,
// or three, of one specVersion and reports what the documents say
// differently, member by member: never how they decide an input, which only
// an evaluation shows (experimental compare, ADR-0045).
//
// Every keyed collection — evidenceRequirements, sources, outcomes, rules,
// exceptions, valueSets and the entries of each set — is compared by key: an
// id, or for an entry its value's canonical JSON text and, when the set holds
// that value more than once, its occurrence among them in document order. A
// member present in every version is compared member by member: objects by
// member name, arrays of one length position by position, and anything else
// as one value. The two sets of Core §6.7, escalation.triggers and a route's
// triggers, are compared as sets, so their order is never a difference; every
// other array keeps its order, and the order of a keyed collection's members
// is reported apart from their content. Annotations (Core 0.5.0 only) are
// compared by key at each position that carries them, pack, outcome, rule and
// value-set entry, a string or a list being one value; no key is treated
// apart from any other.

// DiffVersion is the version of the structural comparison format: the members
// of Diff.JSON and the layout of Diff.Text, which carry it in band. It moves
// by the rule ViewVersion moves by. Experimental.
const DiffVersion = "1"

// ThreeWayDiffVersion is the version of the three-way comparison format: the
// members of ThreeWayDiff.JSON and the layout of ThreeWayDiff.Text, which
// carry it in band in place of diffVersion. It moves by the rule ViewVersion
// moves by. Experimental.
const ThreeWayDiffVersion = "1"

// ErrDiffSpecVersion is wrapped by the error for packs whose specVersions
// differ. Such packs are refused rather than compared, because the members a
// document may hold depend on its specVersion — only Core 0.5.0 has value
// sets and annotations, and 0.2.0-draft has no routes or profile — so a
// member would be reported added, removed or unchanged for a reason that is
// the version and not the document. Experimental.
var ErrDiffSpecVersion = errors.New("view: the packs' specVersions differ")

// Diff is the structural comparison of two pack documents of one specVersion
// (EXPERIMENTAL): what the newer document says differently from the older,
// member by member, with old and new values. It reports and changes nothing,
// and it says nothing about how either document decides an input.
//
// A Diff is the caller's own copy, as a PackView is: JSON and Text return the
// encodings of the comparison as sdk.Diff built it, and the zero Diff has
// none.
type Diff struct {
	Old Identity
	New Identity
	// DifferentDecisions is true when the two ids differ: the documents are
	// two decisions rather than two versions of one. They are compared all
	// the same, and the id is reported as a changed pack member.
	DifferentDecisions bool
	// Differs is false exactly when nothing is reported: the two documents
	// say the same thing member by member, though their bytes may differ in
	// layout and their digests with them.
	Differs bool

	json []byte
	text []byte
}

// JSON returns the comparison in the canonical JSON form the view uses, with
// no trailing newline; docs/building-with-packs.md states its members. The
// caller owns the returned slice. Experimental.
func (d Diff) JSON() []byte { return bytes.Clone(d.json) }

// Text returns the comparison as short plain text, ending in one newline, in
// which every string taken from a pack is written as JSON in printable ASCII.
// The caller owns the returned slice. Experimental.
func (d Diff) Text() []byte { return bytes.Clone(d.text) }

// ThreeWayDiff is the structural comparison of two pack documents with their
// common base (EXPERIMENTAL): for each member that differs anywhere, whether
// only the first changed it, only the second, both identically, or both
// differently, which is a conflict. It reports and merges nothing.
//
// A ThreeWayDiff is the caller's own copy, as a Diff is.
type ThreeWayDiff struct {
	Base   Identity
	First  Identity
	Second Identity
	// DifferentDecisions is true when any two of the three ids differ.
	DifferentDecisions bool
	// Differs is false exactly when nothing is reported.
	Differs bool
	// Conflicts is the number of reported members, annotations, orders and
	// array presences whose status is conflict, whole collection members
	// among them.
	Conflicts int

	json []byte
	text []byte
}

// JSON returns the three-way comparison in the canonical JSON form the view
// uses, with no trailing newline. The caller owns the returned slice.
// Experimental.
func (d ThreeWayDiff) JSON() []byte { return bytes.Clone(d.json) }

// Text returns the three-way comparison as short plain text, ending in one
// newline, written as Diff.Text is. The caller owns the returned slice.
// Experimental.
func (d ThreeWayDiff) Text() []byte { return bytes.Clone(d.text) }

// The words of both formats.
const (
	changeAdded    = "added"
	changeRemoved  = "removed"
	changeChanged  = "changed"
	statusFirst    = "first"
	statusSecond   = "second"
	statusBoth     = "both"
	statusConflict = "conflict"
)

type nodeKind int

const (
	leafNode nodeKind = iota
	objectNode
	arrayNode
	setNode
)

// node is one value of a compared member, as a tree. written is its canonical
// JSON text in document order, the text a report shows; same is the text two
// nodes are compared by, which is written but for a set, whose items it
// sorts, and for every object and array holding a set at any depth, whose
// same is built from its children's, so that a set's order is never a
// difference, whether the set is reached by descending or compared inside a
// value reported whole. annotationsOnly marks an extensions object that held
// annotations and nothing else, read as an empty one (vendorExtensions).
type node struct {
	kind            nodeKind
	members         map[string]*node
	names           []string
	items           []*node
	extensions      bool
	annotationsOnly bool
	written         string
	same            string
}

// setPositions are the arrays of a pack's own members that Core §6.7 makes
// sets: escalation.triggers and each route's triggers.
var setPositions = regexp.MustCompile(`^/escalation/(?:triggers|routes/[0-9]+/triggers)$`)

// treeOf reads one value the view's writer built into a node. sets says which
// arrays are sets, by pointer; it is nil for a collection member, none of
// which holds one.
func treeOf(value any, pointer string, sets *regexp.Regexp, extensions bool) (*node, error) {
	switch typed := value.(type) {
	case map[string]any:
		object := &node{kind: objectNode, members: make(map[string]*node, len(typed)), extensions: extensions}
		for name := range typed {
			object.names = append(object.names, name)
		}
		slices.SortFunc(object.names, compareUTF16)
		var text, same strings.Builder
		text.WriteByte('{')
		same.WriteByte('{')
		for index, name := range object.names {
			child, err := treeOf(typed[name], pointer+"/"+pointerEscaper.Replace(name), sets, name == "extensions")
			if err != nil {
				return nil, err
			}
			object.members[name] = child
			encoded, err := canonicalText(name)
			if err != nil {
				return nil, err
			}
			if index > 0 {
				text.WriteByte(',')
				same.WriteByte(',')
			}
			text.WriteString(encoded + ":" + child.written)
			same.WriteString(encoded + ":" + child.same)
		}
		text.WriteByte('}')
		same.WriteByte('}')
		object.written = text.String()
		object.same = same.String()
		return object, nil
	case []any:
		array := &node{kind: arrayNode}
		if sets != nil && sets.MatchString(pointer) {
			array.kind = setNode
		}
		written := make([]string, 0, len(typed))
		same := make([]string, 0, len(typed))
		for index, item := range typed {
			child, err := treeOf(item, pointer+"/"+strconv.Itoa(index), sets, false)
			if err != nil {
				return nil, err
			}
			array.items = append(array.items, child)
			written = append(written, child.written)
			same = append(same, child.same)
		}
		if array.kind == setNode {
			slices.Sort(same)
		}
		array.written = "[" + strings.Join(written, ",") + "]"
		array.same = "[" + strings.Join(same, ",") + "]"
		return array, nil
	default:
		text, err := canonicalText(typed)
		if err != nil {
			return nil, err
		}
		return &node{kind: leafNode, written: text, same: text}, nil
	}
}

func sameNode(left, right *node) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.same == right.same
}

// memberChange is one reported member: its JSON Pointer, relative to the
// collection member or, for the pack's own members, to the document, its
// value in each version (nil where it is absent), and its change or status.
type memberChange struct {
	pointer string
	values  []*node
	word    string
}

// annotationChange is one reported annotation key: its value in each version
// as canonical JSON, a string or a list, nil where the key is absent.
type annotationChange struct {
	key    string
	values []*string
	word   string
}

// itemKey names one member of a keyed collection: an id, or an entry's value
// text and its occurrence among the entries holding that value.
type itemKey struct {
	text       string
	occurrence int
	entry      bool
}

func compareKeys(left, right itemKey) int {
	if order := compareUTF16(left.text, right.text); order != 0 {
		return order
	}
	return left.occurrence - right.occurrence
}

// item is one member of a keyed collection in one version.
type item struct {
	key       itemKey
	written   string
	tree      *node
	annotated bool
	// annotations maps each annotation key to its value's canonical JSON;
	// nil when the member has no annotations member.
	annotations map[string]string
	entries     *collection
}

// collection is one keyed collection in one version.
type collection struct {
	present bool
	items   []item
}

// itemChange is one reported member of a keyed collection. whole holds its
// text in each version, nil where it is absent, when it is absent from at
// least one; otherwise members, annotations and entries say what differs.
type itemChange struct {
	key         itemKey
	whole       []*string
	word        string
	members     []memberChange
	annotated   bool
	annotations []annotationChange
	entries     *collectionChange
}

// orderChange is the order, in each version, of the collection members both
// of the last two versions hold (old and new; first and second), each version
// listing those of them it holds, reported when two versions put the members
// both hold in different orders.
type orderChange struct {
	orders [][]itemKey
	word   string
}

// presenceChange says, for each version, whether the collection's array is
// present, reported when that is not the same in all: an absent optional
// collection and an empty one hold the same members and are not the same
// document.
type presenceChange struct {
	present []bool
	word    string
}

type collectionChange struct {
	presence *presenceChange
	order    *orderChange
	items    []itemChange
}

func (c *collectionChange) empty() bool {
	return c.presence == nil && c.order == nil && len(c.items) == 0
}

// section is one keyed collection of the pack, by its member name and the
// heading the text uses.
type section struct {
	name    string
	heading string
}

var sections = []section{
	{"evidenceRequirements", "evidence requirements"},
	{"sources", "sources"},
	{"outcomes", "outcomes"},
	{"rules", "rules"},
	{"exceptions", "exceptions"},
	{"valueSets", "value sets"},
}

// document is one version, read for comparison.
type document struct {
	identity    Identity
	tree        *node
	annotated   bool
	annotations map[string]string
	collections map[string]*collection
}

// comparison is the comparison of two versions (old, new) or three (base,
// first, second).
type comparison struct {
	sides       int
	identities  []Identity
	annotated   bool
	valueSets   bool
	members     []memberChange
	annotations []annotationChange
	collections map[string]*collectionChange
	conflicts   int
}

// vendorExtensions marks the extensions member of a carrier's tree, built
// from the carrier without its annotations, when the carrier's extensions
// object held annotations and nothing else. The tree holds that object as
// the empty one it is without them, so against "extensions": {} it is the
// same object; walk reads it as absent against an absent one (descendable),
// so the first annotation added to a member, or the last removed, is
// reported once, as annotations.
func vendorExtensions(tree *node, extensions []Extension, annotations []Annotation) {
	if len(extensions) != 0 || annotations == nil {
		return
	}
	if held := tree.members["extensions"]; held != nil && held.kind == objectNode {
		held.annotationsOnly = true
	}
}

func annotationTexts(annotations []Annotation) (map[string]string, error) {
	if annotations == nil {
		return nil, nil
	}
	texts := make(map[string]string, len(annotations))
	for _, annotation := range annotations {
		var value any = annotation.Value
		if annotation.List != nil {
			items := make([]any, 0, len(annotation.List))
			for _, entry := range annotation.List {
				items = append(items, entry)
			}
			value = items
		}
		text, err := canonicalText(value)
		if err != nil {
			return nil, err
		}
		texts[annotation.Key] = text
	}
	return texts, nil
}

// newItem reads one collection member: its whole text from full, its tree
// from bare, which is the same member without annotations and nested
// collections.
func newItem(key itemKey, full, bare map[string]any) (item, error) {
	written, err := canonicalText(full)
	if err != nil {
		return item{}, err
	}
	tree, err := treeOf(bare, "", nil, false)
	if err != nil {
		return item{}, err
	}
	return item{key: key, written: written, tree: tree}, nil
}

func (it *item) annotate(annotations []Annotation, annotated bool) error {
	if !annotated {
		return nil
	}
	texts, err := annotationTexts(annotations)
	if err != nil {
		return err
	}
	it.annotated, it.annotations = true, texts
	return nil
}

// read reads one view for comparison.
func read(v PackView) (document, error) {
	annotated := result.HasCore050Rules(v.Identity.SpecVersion)
	doc := document{identity: v.Identity, annotated: annotated, collections: map[string]*collection{}}
	bare := v
	bare.Annotations = nil
	members, err := bare.documentValue()
	if err != nil {
		return document{}, err
	}
	for _, named := range sections {
		delete(members, named.name)
	}
	if doc.tree, err = treeOf(members, "", setPositions, false); err != nil {
		return document{}, err
	}
	if annotated {
		vendorExtensions(doc.tree, v.Extensions, v.Annotations)
		if doc.annotations, err = annotationTexts(v.Annotations); err != nil {
			return document{}, err
		}
	}
	id := func(text string) itemKey { return itemKey{text: text} }

	requirements := &collection{present: v.EvidenceRequirements != nil}
	for _, requirement := range v.EvidenceRequirements {
		value := evidenceRequirementValue(requirement)
		read, err := newItem(id(requirement.ID), value, value)
		if err != nil {
			return document{}, err
		}
		requirements.items = append(requirements.items, read)
	}
	doc.collections["evidenceRequirements"] = requirements

	sources := &collection{present: v.Sources != nil}
	for _, source := range v.Sources {
		value := sourceValue(source)
		read, err := newItem(id(source.ID), value, value)
		if err != nil {
			return document{}, err
		}
		sources.items = append(sources.items, read)
	}
	doc.collections["sources"] = sources

	outcomes := &collection{present: true}
	for _, outcome := range v.Outcomes {
		stripped := outcome
		stripped.Annotations = nil
		read, err := newItem(id(outcome.ID), outcomeValue(outcome), outcomeValue(stripped))
		if err != nil {
			return document{}, err
		}
		vendorExtensions(read.tree, outcome.Extensions, outcome.Annotations)
		if err := read.annotate(outcome.Annotations, annotated); err != nil {
			return document{}, err
		}
		outcomes.items = append(outcomes.items, read)
	}
	doc.collections["outcomes"] = outcomes

	rules := &collection{present: true}
	for _, rule := range v.Rules {
		stripped := rule
		stripped.Annotations = nil
		full, err := ruleValue(rule)
		if err != nil {
			return document{}, err
		}
		bareRule, err := ruleValue(stripped)
		if err != nil {
			return document{}, err
		}
		read, err := newItem(id(rule.ID), full, bareRule)
		if err != nil {
			return document{}, err
		}
		vendorExtensions(read.tree, rule.Extensions, rule.Annotations)
		if err := read.annotate(rule.Annotations, annotated); err != nil {
			return document{}, err
		}
		rules.items = append(rules.items, read)
	}
	doc.collections["rules"] = rules

	exceptions := &collection{present: v.Exceptions != nil}
	for _, exception := range v.Exceptions {
		value, err := exceptionValue(exception)
		if err != nil {
			return document{}, err
		}
		read, err := newItem(id(exception.ID), value, value)
		if err != nil {
			return document{}, err
		}
		exceptions.items = append(exceptions.items, read)
	}
	doc.collections["exceptions"] = exceptions

	if annotated {
		sets := &collection{present: v.ValueSets != nil}
		for _, set := range v.ValueSets {
			bareSet := valueSetValue(set)
			delete(bareSet, "entries")
			read, err := newItem(id(set.ID), valueSetValue(set), bareSet)
			if err != nil {
				return document{}, err
			}
			read.entries = &collection{present: true}
			seen := map[string]int{}
			for _, entry := range set.Entries {
				seen[entry.Value]++
				stripped := entry
				stripped.Annotations = nil
				readEntry, err := newItem(itemKey{text: entry.Value, occurrence: seen[entry.Value], entry: true}, entryValue(entry), entryValue(stripped))
				if err != nil {
					return document{}, err
				}
				vendorExtensions(readEntry.tree, entry.Extensions, entry.Annotations)
				if err := readEntry.annotate(entry.Annotations, true); err != nil {
					return document{}, err
				}
				read.entries.items = append(read.entries.items, readEntry)
			}
			sets.items = append(sets.items, read)
		}
		doc.collections["valueSets"] = sets
	}
	return doc, nil
}

// word classifies a reported value from its value in each version: for two
// versions, added, removed or changed; for three, the status, counting a
// conflict.
func (c *comparison) word(absent []bool, same func(int, int) bool) string {
	if c.sides == 2 {
		switch {
		case absent[0]:
			return changeAdded
		case absent[1]:
			return changeRemoved
		}
		return changeChanged
	}
	first, second := !same(0, 1), !same(0, 2)
	switch {
	case first && !second:
		return statusFirst
	case !first && second:
		return statusSecond
	case same(1, 2):
		return statusBoth
	}
	c.conflicts++
	return statusConflict
}

func allSame(count int, same func(int, int) bool) bool {
	for index := 1; index < count; index++ {
		if !same(0, index) {
			return false
		}
	}
	return true
}

func nodesAbsent(values []*node) []bool {
	absent := make([]bool, len(values))
	for index, value := range values {
		absent[index] = value == nil
	}
	return absent
}

// walk compares one value across the versions and reports, at the deepest
// pointer it can, every place they differ. It descends into objects present
// in every version, and into arrays that are not sets and have one length in
// every version; anything else that differs is reported whole. An extensions
// object absent from some versions is read there as an empty one when every
// version that has it holds a member, so each vendor member is reported by
// name, or held only annotations, so that their coming or going is reported
// once, as annotations.
func (c *comparison) walk(pointer string, values []*node, out *[]memberChange) {
	same := func(left, right int) bool { return sameNode(values[left], values[right]) }
	if allSame(len(values), same) {
		return
	}
	if objects := descendable(values); objects != nil {
		names, seen := []string{}, map[string]bool{}
		for _, object := range objects {
			for _, name := range object.names {
				if !seen[name] {
					seen[name] = true
					names = append(names, name)
				}
			}
		}
		slices.SortFunc(names, compareUTF16)
		for _, name := range names {
			children := make([]*node, len(objects))
			for index, object := range objects {
				children[index] = object.members[name]
			}
			c.walk(pointer+"/"+pointerEscaper.Replace(name), children, out)
		}
		return
	}
	if alike(values) {
		for position := range values[0].items {
			children := make([]*node, len(values))
			for index, value := range values {
				children[index] = value.items[position]
			}
			c.walk(pointer+"/"+strconv.Itoa(position), children, out)
		}
		return
	}
	*out = append(*out, memberChange{pointer: pointer, values: values, word: c.word(nodesAbsent(values), same)})
}

// descendable returns the objects to descend into, or nil: the values when
// every one is an object, and an extensions object read as empty where it is
// absent when every version that has it holds a member or held only
// annotations. An extensions object that is empty in the document, against
// an absent one, is reported whole: that object came or went.
func descendable(values []*node) []*node {
	objects := make([]*node, len(values))
	missing := false
	for index, value := range values {
		if value == nil {
			missing = true
			continue
		}
		if value.kind != objectNode {
			return nil
		}
		objects[index] = value
	}
	if !missing {
		return objects
	}
	for index, value := range values {
		if value == nil {
			objects[index] = &node{kind: objectNode, members: map[string]*node{}, extensions: true, written: "{}", same: "{}"}
			continue
		}
		if !value.extensions || (len(value.names) == 0 && !value.annotationsOnly) {
			return nil
		}
	}
	return objects
}

// alike reports whether every value is an array, not a set, of one length.
func alike(values []*node) bool {
	for _, value := range values {
		if value == nil || value.kind != arrayNode || len(value.items) != len(values[0].items) {
			return false
		}
	}
	return true
}

func (c *comparison) compareAnnotations(sides []map[string]string) []annotationChange {
	keys, seen := []string{}, map[string]bool{}
	for _, side := range sides {
		for key := range side {
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	slices.SortFunc(keys, compareUTF16)
	changes := []annotationChange{}
	for _, key := range keys {
		values := make([]*string, len(sides))
		absent := make([]bool, len(sides))
		for index, side := range sides {
			if text, present := side[key]; present {
				values[index] = &text
			} else {
				absent[index] = true
			}
		}
		same := func(left, right int) bool {
			if values[left] == nil || values[right] == nil {
				return values[left] == values[right]
			}
			return *values[left] == *values[right]
		}
		if allSame(len(sides), same) {
			continue
		}
		changes = append(changes, annotationChange{key: key, values: values, word: c.word(absent, same)})
	}
	return changes
}

func (c *comparison) compareCollection(sides []*collection) *collectionChange {
	change := &collectionChange{}
	present := make([]bool, len(sides))
	for index, side := range sides {
		present[index] = side.present
	}
	samePresence := func(left, right int) bool { return present[left] == present[right] }
	if !allSame(len(sides), samePresence) {
		absent := make([]bool, len(sides))
		for index := range sides {
			absent[index] = !present[index]
		}
		change.presence = &presenceChange{present: present, word: c.word(absent, samePresence)}
	}
	keys, seen := []itemKey{}, map[itemKey]bool{}
	byKey := make([]map[itemKey]*item, len(sides))
	for index, side := range sides {
		byKey[index] = map[itemKey]*item{}
		for position := range side.items {
			member := &side.items[position]
			if _, held := byKey[index][member.key]; held {
				continue
			}
			byKey[index][member.key] = member
			if !seen[member.key] {
				seen[member.key] = true
				keys = append(keys, member.key)
			}
		}
	}
	slices.SortFunc(keys, compareKeys)
	for _, key := range keys {
		members := make([]*item, len(sides))
		missing := false
		for index := range sides {
			members[index] = byKey[index][key]
			missing = missing || members[index] == nil
		}
		if missing {
			whole := make([]*string, len(sides))
			absent := make([]bool, len(sides))
			for index, member := range members {
				if member == nil {
					absent[index] = true
					continue
				}
				whole[index] = &member.written
			}
			same := func(left, right int) bool {
				if whole[left] == nil || whole[right] == nil {
					return whole[left] == whole[right]
				}
				return *whole[left] == *whole[right]
			}
			change.items = append(change.items, itemChange{key: key, whole: whole, word: c.word(absent, same)})
			continue
		}
		reported := itemChange{key: key, word: changeChanged, annotated: members[0].annotated, members: []memberChange{}}
		trees := make([]*node, len(sides))
		for index, member := range members {
			trees[index] = member.tree
		}
		c.walk("", trees, &reported.members)
		if reported.annotated {
			annotations := make([]map[string]string, len(sides))
			for index, member := range members {
				annotations[index] = member.annotations
			}
			reported.annotations = c.compareAnnotations(annotations)
		}
		if members[0].entries != nil {
			entries := make([]*collection, len(sides))
			for index, member := range members {
				entries[index] = member.entries
			}
			reported.entries = c.compareCollection(entries)
		}
		if len(reported.members) > 0 || len(reported.annotations) > 0 || (reported.entries != nil && !reported.entries.empty()) {
			change.items = append(change.items, reported)
		}
	}
	// The order is read over the members the last two versions both hold, so
	// that a member both descendants add, at different places, is an order
	// difference between them. Two versions' orders are the same when they
	// put the members both hold in one order; that relation is checked for
	// every pair, since a base that lacks a member says nothing of its place.
	orders := make([][]itemKey, len(sides))
	for index, side := range sides {
		orders[index] = []itemKey{}
		for _, member := range side.items {
			if byKey[len(sides)-2][member.key] != nil && byKey[len(sides)-1][member.key] != nil {
				orders[index] = append(orders[index], member.key)
			}
		}
	}
	held := func(order []itemKey, side int) []itemKey {
		kept := []itemKey{}
		for _, key := range order {
			if byKey[side][key] != nil {
				kept = append(kept, key)
			}
		}
		return kept
	}
	sameOrder := func(left, right int) bool {
		return slices.Equal(held(orders[left], right), held(orders[right], left))
	}
	if !everyPairSame(len(sides), sameOrder) {
		change.order = &orderChange{orders: orders, word: c.word(make([]bool, len(sides)), sameOrder)}
	}
	return change
}

// everyPairSame reports whether same holds for every two versions.
func everyPairSame(count int, same func(int, int) bool) bool {
	for left := 0; left < count; left++ {
		for right := left + 1; right < count; right++ {
			if !same(left, right) {
				return false
			}
		}
	}
	return true
}

// compare is the comparison of two or three views of one specVersion.
func compare(views []PackView) (*comparison, error) {
	for _, other := range views[1:] {
		if other.Identity.SpecVersion != views[0].Identity.SpecVersion {
			quoted := make([]string, 0, len(views))
			for _, v := range views {
				quoted = append(quoted, strconv.QuoteToASCII(v.Identity.SpecVersion))
			}
			return nil, fmt.Errorf("%w: they are %s, and a structural comparison reads packs of one specVersion, because the members a document may hold depend on it", ErrDiffSpecVersion, strings.Join(quoted, ", "))
		}
	}
	documents := make([]document, 0, len(views))
	for _, v := range views {
		read, err := read(v)
		if err != nil {
			return nil, &fault{message: "The comparison could not read a view: " + err.Error() + "."}
		}
		documents = append(documents, read)
	}
	c := &comparison{sides: len(views), annotated: documents[0].annotated, valueSets: documents[0].annotated, collections: map[string]*collectionChange{}}
	trees := make([]*node, len(documents))
	for index, read := range documents {
		c.identities = append(c.identities, read.identity)
		trees[index] = read.tree
	}
	c.members = []memberChange{}
	c.walk("", trees, &c.members)
	if c.annotated {
		annotations := make([]map[string]string, len(documents))
		for index, read := range documents {
			annotations[index] = read.annotations
		}
		c.annotations = c.compareAnnotations(annotations)
	}
	for _, named := range sections {
		if named.name == "valueSets" && !c.valueSets {
			continue
		}
		collections := make([]*collection, len(documents))
		for index, read := range documents {
			collections[index] = read.collections[named.name]
		}
		c.collections[named.name] = c.compareCollection(collections)
	}
	return c, nil
}

func (c *comparison) differs() bool {
	if len(c.members) > 0 || len(c.annotations) > 0 {
		return true
	}
	for _, change := range c.collections {
		if !change.empty() {
			return true
		}
	}
	return false
}

func (c *comparison) differentDecisions() bool {
	for _, identity := range c.identities[1:] {
		if identity.ID != c.identities[0].ID {
			return true
		}
	}
	return false
}

// sideNames are the member names of the versions, in order.
func (c *comparison) sideNames() []string {
	if c.sides == 2 {
		return []string{"old", "new"}
	}
	return []string{"base", "first", "second"}
}

// diffTwo is the comparison of an older view and a newer one.
func diffTwo(older, newer PackView) (Diff, error) {
	c, err := compare([]PackView{older, newer})
	if err != nil {
		return Diff{}, err
	}
	d := Diff{Old: older.Identity, New: newer.Identity, DifferentDecisions: c.differentDecisions(), Differs: c.differs()}
	if d.json, err = c.encode(); err != nil {
		return Diff{}, err
	}
	d.text = c.render()
	return d, nil
}

// diffThree is the comparison of two views with their common base.
func diffThree(base, first, second PackView) (ThreeWayDiff, error) {
	c, err := compare([]PackView{base, first, second})
	if err != nil {
		return ThreeWayDiff{}, err
	}
	d := ThreeWayDiff{Base: base.Identity, First: first.Identity, Second: second.Identity, DifferentDecisions: c.differentDecisions(), Differs: c.differs(), Conflicts: c.conflicts}
	if d.json, err = c.encode(); err != nil {
		return ThreeWayDiff{}, err
	}
	d.text = c.render()
	return d, nil
}

// The canonical JSON form.

func identityValue(identity Identity) map[string]any {
	value := map[string]any{"digest": identity.Digest, "id": identity.ID, "specVersion": identity.SpecVersion, "version": identity.Version}
	putString(value, "profile", identity.Profile)
	return value
}

// classified sets the change (two versions) or status (three) member.
func (c *comparison) classified(object map[string]any, word string) map[string]any {
	if c.sides == 2 {
		object["change"] = word
	} else {
		object["status"] = word
	}
	return object
}

func (c *comparison) memberValues(changes []memberChange) []any {
	items := make([]any, 0, len(changes))
	names := c.sideNames()
	for _, change := range changes {
		object := map[string]any{"pointer": change.pointer}
		for index, value := range change.values {
			if value != nil {
				object[names[index]] = rawJSON(value.written)
			}
		}
		items = append(items, c.classified(object, change.word))
	}
	return items
}

func (c *comparison) annotationValues(changes []annotationChange) []any {
	items := make([]any, 0, len(changes))
	names := c.sideNames()
	for _, change := range changes {
		object := map[string]any{"key": change.key}
		for index, value := range change.values {
			if value != nil {
				object[names[index]] = rawJSON(*value)
			}
		}
		items = append(items, c.classified(object, change.word))
	}
	return items
}

func keyValue(object map[string]any, key itemKey) {
	if !key.entry {
		object["id"] = key.text
		return
	}
	object["value"] = rawJSON(key.text)
	if key.occurrence > 1 {
		object["occurrence"] = rawJSON(strconv.Itoa(key.occurrence))
	}
}

func (c *comparison) collectionValue(change *collectionChange) map[string]any {
	names := c.sideNames()
	changes := make([]any, 0, len(change.items))
	for _, reported := range change.items {
		object := map[string]any{}
		keyValue(object, reported.key)
		if reported.whole != nil {
			for index, value := range reported.whole {
				if value != nil {
					object[names[index]] = rawJSON(*value)
				}
			}
			changes = append(changes, c.classified(object, reported.word))
			continue
		}
		object["members"] = c.memberValues(reported.members)
		if reported.annotated {
			object["annotations"] = c.annotationValues(reported.annotations)
		}
		if reported.entries != nil {
			object["entries"] = c.collectionValue(reported.entries)
		}
		if c.sides == 2 {
			object["change"] = changeChanged
		}
		changes = append(changes, object)
	}
	value := map[string]any{"changes": changes}
	if change.order != nil {
		order := map[string]any{}
		for index, keys := range change.order.orders {
			listed := make([]any, 0, len(keys))
			for _, key := range keys {
				if key.entry {
					listed = append(listed, rawJSON(key.text))
				} else {
					listed = append(listed, key.text)
				}
			}
			order[names[index]] = listed
		}
		if c.sides == 3 {
			order["status"] = change.order.word
		}
		value["order"] = order
	}
	if change.presence != nil {
		presence := map[string]any{}
		for index, present := range change.presence.present {
			presence[names[index]] = present
		}
		if c.sides == 3 {
			presence["status"] = change.presence.word
		}
		value["present"] = presence
	}
	return value
}

func (c *comparison) encode() ([]byte, error) {
	names := c.sideNames()
	pack := map[string]any{"members": c.memberValues(c.members)}
	if c.annotated {
		pack["annotations"] = c.annotationValues(c.annotations)
	}
	document := map[string]any{"pack": pack, "differs": c.differs()}
	for index, identity := range c.identities {
		document[names[index]] = identityValue(identity)
	}
	if c.sides == 2 {
		document["diffVersion"] = DiffVersion
	} else {
		document["threeWayDiffVersion"] = ThreeWayDiffVersion
		document["conflicts"] = rawJSON(strconv.Itoa(c.conflicts))
	}
	if c.differentDecisions() {
		document["differentDecisions"] = true
	}
	for name, change := range c.collections {
		document[name] = c.collectionValue(change)
	}
	encoded, err := canonicalText(document)
	if err != nil {
		return nil, &fault{message: "The comparison does not encode: " + err.Error() + "."}
	}
	return []byte(encoded), nil
}

// The text form. Every string taken from a pack — an id, a version, a value,
// an annotation key, an extension name in a pointer — is written as JSON in
// printable ASCII (asciiJSON), so no character of it can act on a terminal,
// reorder the line, or read like another; a pointer of plain characters only
// is written bare.

// asciiJSON writes a canonical JSON text with every code point outside
// printable ASCII as a JSON \u escape, a surrogate pair beyond the Basic
// Multilingual Plane. The result decodes to the same value, and two values
// that differ in any code point differ in it.
func asciiJSON(text string) string {
	var builder strings.Builder
	for _, character := range text {
		switch {
		case character >= 0x20 && character <= 0x7e:
			builder.WriteRune(character)
		case character > 0xffff:
			high, low := utf16.EncodeRune(character)
			fmt.Fprintf(&builder, `\u%04x\u%04x`, high, low)
		default:
			fmt.Fprintf(&builder, `\u%04x`, character)
		}
	}
	return builder.String()
}

// quoted is a pack string written as asciiJSON writes it.
func quoted(value string) string {
	text, err := canonicalText(value)
	if err != nil {
		return strconv.QuoteToASCII(value)
	}
	return asciiJSON(text)
}

var barePointer = regexp.MustCompile(`^(?:/[A-Za-z0-9._~-]*)*$`)

func pointerText(pointer string) string {
	if barePointer.MatchString(pointer) {
		return pointer
	}
	return quoted(pointer)
}

func keyText(key itemKey) string {
	if !key.entry {
		return quoted(key.text)
	}
	text := "entry " + asciiJSON(key.text)
	if key.occurrence > 1 {
		text += " (occurrence " + strconv.Itoa(key.occurrence) + ")"
	}
	return text
}

type textWriter struct{ builder strings.Builder }

func (w *textWriter) line(depth int, text string) {
	w.builder.WriteString(strings.Repeat("  ", depth))
	w.builder.WriteString(text)
	w.builder.WriteByte('\n')
}

// sidesText writes a value in each version: "base <v>, first <v>, second
// <v>", with absent for a version that does not hold it.
func (c *comparison) sidesText(values []*string) string {
	names := c.sideNames()
	parts := make([]string, 0, len(values))
	for index, value := range values {
		shown := "absent"
		if value != nil {
			shown = asciiJSON(*value)
		}
		parts = append(parts, names[index]+" "+shown)
	}
	return strings.Join(parts, ", ")
}

// changeText is what follows a reported name: for two versions the change
// and the values, for three the status and each version's value.
func (c *comparison) changeText(word string, values []*string) string {
	if c.sides == 3 {
		return word + ": " + c.sidesText(values)
	}
	switch word {
	case changeAdded:
		return word + ": " + asciiJSON(*values[1])
	case changeRemoved:
		return word + ": " + asciiJSON(*values[0])
	}
	return word + ": " + asciiJSON(*values[0]) + " -> " + asciiJSON(*values[1])
}

func writtenValues(values []*node) []*string {
	texts := make([]*string, len(values))
	for index, value := range values {
		if value != nil {
			texts[index] = &value.written
		}
	}
	return texts
}

func (c *comparison) writeMembers(w *textWriter, depth int, members []memberChange, annotations []annotationChange) {
	for _, change := range members {
		w.line(depth, pointerText(change.pointer)+" "+c.changeText(change.word, writtenValues(change.values)))
	}
	for _, change := range annotations {
		w.line(depth, "annotation "+quoted(change.key)+" "+c.changeText(change.word, change.values))
	}
}

// actions says what each descendant did to a member of a keyed collection
// absent from at least one version.
func actions(whole []*string) string {
	names := []string{"first", "second"}
	parts := make([]string, 0, 2)
	for index, value := range whole[1:] {
		action := "unchanged"
		switch {
		case whole[0] == nil && value != nil:
			action = "added"
		case whole[0] != nil && value == nil:
			action = "removed"
		case whole[0] != nil && value != nil && *whole[0] != *value:
			action = "changed"
		case whole[0] == nil && value == nil:
			action = "absent"
		}
		parts = append(parts, names[index]+" "+action)
	}
	return strings.Join(parts, ", ")
}

func (c *comparison) orderText(keys []itemKey) string {
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if key.entry {
			parts = append(parts, asciiJSON(key.text))
		} else {
			parts = append(parts, quoted(key.text))
		}
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func (c *comparison) writeCollection(w *textWriter, depth int, change *collectionChange, noun string) {
	names := c.sideNames()
	if change.presence != nil {
		if c.sides == 2 {
			word := changeAdded
			if !change.presence.present[1] {
				word = changeRemoved
			}
			w.line(depth, "the "+noun+" array itself "+word)
		} else {
			parts := make([]string, 0, 3)
			for index, present := range change.presence.present {
				shown := "absent"
				if present {
					shown = "present"
				}
				parts = append(parts, names[index]+" "+shown)
			}
			w.line(depth, "the "+noun+" array itself "+change.presence.word+": "+strings.Join(parts, ", "))
		}
	}
	for _, reported := range change.items {
		if reported.whole != nil {
			if c.sides == 2 {
				w.line(depth, keyText(reported.key)+" "+reported.word)
			} else {
				w.line(depth, keyText(reported.key)+" "+reported.word+": "+actions(reported.whole))
			}
			continue
		}
		if c.sides == 2 {
			w.line(depth, keyText(reported.key)+" changed:")
		} else {
			w.line(depth, keyText(reported.key)+":")
		}
		c.writeMembers(w, depth+1, reported.members, reported.annotations)
		if reported.entries != nil {
			c.writeCollection(w, depth+1, reported.entries, "entries")
		}
	}
	if change.order != nil {
		orders := change.order.orders
		if c.sides == 2 {
			w.line(depth, noun+" order changed: "+c.orderText(orders[0])+" -> "+c.orderText(orders[1]))
		} else {
			parts := make([]string, 0, 3)
			for index, keys := range orders {
				parts = append(parts, names[index]+" "+c.orderText(keys))
			}
			w.line(depth, noun+" order "+change.order.word+": "+strings.Join(parts, ", "))
		}
	}
}

func (c *comparison) render() []byte {
	w := &textWriter{}
	if c.differentDecisions() {
		w.line(0, "DIFFERENT DECISIONS: the packs have different ids, so this compares decisions, not versions of one")
	}
	if c.sides == 2 {
		w.line(0, "STRUCTURAL COMPARISON (diff version "+DiffVersion+"): what the two documents say differently, member by member; it says nothing about how they decide any input")
	} else {
		w.line(0, "THREE-WAY STRUCTURAL COMPARISON (three-way diff version "+ThreeWayDiffVersion+"): which of first and second changed each member from base, member by member; nothing is merged")
	}
	for index, identity := range c.identities {
		text := c.sideNames()[index] + ": id " + quoted(identity.ID) + " version " + quoted(identity.Version) + " specVersion " + quoted(identity.SpecVersion)
		if identity.Profile != "" {
			text += " profile " + quoted(identity.Profile)
		}
		w.line(0, text+" digest "+asciiJSON(identity.Digest))
	}
	if !c.differs() {
		w.line(0, "no structural differences")
		return []byte(w.builder.String())
	}
	if len(c.members) > 0 || len(c.annotations) > 0 {
		w.line(0, "pack:")
		c.writeMembers(w, 1, c.members, c.annotations)
	}
	for _, named := range sections {
		change, present := c.collections[named.name]
		if !present || change.empty() {
			continue
		}
		w.line(0, named.heading+":")
		c.writeCollection(w, 1, change, named.name)
	}
	if c.sides == 3 {
		w.line(0, "conflicts: "+strconv.Itoa(c.conflicts))
	}
	return []byte(w.builder.String())
}

func init() {
	viewhook.Diff = func(views []any) (any, error) {
		built := make([]PackView, 0, len(views))
		for _, given := range views {
			described, ok := given.(PackView)
			if !ok || described.json == nil {
				return nil, &fault{message: "The comparison was given no view that View built."}
			}
			built = append(built, described)
		}
		switch len(built) {
		case 2:
			return diffTwo(built[0], built[1])
		case 3:
			return diffThree(built[0], built[1], built[2])
		}
		return nil, &fault{message: "The comparison was given neither two views nor three."}
	}
}
