// Package evalform computes the evaluation form EF(P) of a pack declaring the
// staged draft selector 0.4.0-draft (EXPERIMENTAL; draft §8.5, RFC 0031).
//
// EF(P) is P with three changes and no others: every fact condition in a
// condition position that carries valueSet instead carries value, the array of
// the named set's entry values in entry order; the root valueSets member is
// removed; and the annotations extension is removed from the root's, each
// outcome's and each rule's extensions object, removing that object only when
// the removal empties it (an object already empty in P is kept).
//
// The steps act on positions, never on content: a literal operand, an entry
// value and a vendor extension's value are not inspected, so a literal that
// looks like a reference or like an annotations object is left as it is.
//
// The form is computed once, where a pack is admitted, so everything that reads
// an admitted pack's conditions -- the evaluator, its trace, contributors,
// advice -- sees EF(P) and nothing else. Identity is not the form's: a pack's
// digest, id and version stay those of P's exact bytes.
package evalform

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
)

// AnnotationsName is the reserved annotations extension name.
const AnnotationsName = "org.judgmentpack.annotations"

// UnresolvedError reports a valueSet member naming no declared set. A pack
// carrying one is not semantically conforming and has no evaluation form; the
// validator refuses it first, so reaching this is a defect in the caller.
type UnresolvedError struct {
	Pointer string
	ID      string
}

func (e *UnresolvedError) Error() string {
	return fmt.Sprintf("value-set reference %q at %s names no declared set", e.ID, e.Pointer)
}

// Of returns EF(root). root is a decoded pack (objects map[string]any, arrays
// []any) and is never modified: every changed object is a copy, and every
// unchanged subtree is shared with root. All references to one set share one
// value array, which the evaluator only reads. A pack with no valueSets member,
// no reference and no annotations is returned as it is.
func Of(root map[string]any) (map[string]any, error) {
	sets, err := declaredSets(root)
	if err != nil {
		return nil, err
	}
	form := maps.Clone(root)
	delete(form, "valueSets")
	if applicability, present := root["applicability"]; present {
		expanded, _, err := expand(applicability, "/applicability", sets)
		if err != nil {
			return nil, err
		}
		form["applicability"] = expanded
	}
	for _, collection := range []string{"rules", "exceptions"} {
		items, ok := root[collection].([]any)
		if !ok {
			continue
		}
		copied := slices.Clone(items)
		for index, item := range items {
			object, ok := item.(map[string]any)
			if !ok {
				continue
			}
			when, present := object["when"]
			if !present {
				continue
			}
			expanded, changed, err := expand(when, "/"+collection+"/"+strconv.Itoa(index)+"/when", sets)
			if err != nil {
				return nil, err
			}
			if !changed {
				continue
			}
			object = maps.Clone(object)
			object["when"] = expanded
			copied[index] = object
		}
		form[collection] = copied
	}
	form = withoutAnnotations(form)
	for _, collection := range []string{"outcomes", "rules"} {
		items, ok := form[collection].([]any)
		if !ok {
			continue
		}
		copied := slices.Clone(items)
		for index, item := range items {
			if object, ok := item.(map[string]any); ok {
				copied[index] = withoutAnnotations(object)
			}
		}
		form[collection] = copied
	}
	return form, nil
}

// declaredSets maps each declared set id to the array of its entries' values in
// entry order. The first declaration of an id wins; two declarations of one id
// are refused by the validator before any form is computed.
func declaredSets(root map[string]any) (map[string][]any, error) {
	sets := map[string][]any{}
	declared, _ := root["valueSets"].([]any)
	for _, item := range declared {
		set, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, ok := set["id"].(string)
		if !ok {
			continue
		}
		if _, seen := sets[id]; seen {
			continue
		}
		entries, _ := set["entries"].([]any)
		values := make([]any, 0, len(entries))
		for _, entry := range entries {
			object, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			values = append(values, object["value"])
		}
		sets[id] = values
	}
	return sets, nil
}

// expand returns condition with every reference in it expanded, copying only
// the objects on a path to a reference, and whether anything changed. pointer
// is the condition's position in P, for the error a defective caller would see.
func expand(condition any, pointer string, sets map[string][]any) (any, bool, error) {
	object, ok := condition.(map[string]any)
	if !ok {
		return condition, false, nil
	}
	switch object["op"] {
	case "fact":
		reference, carries := object["valueSet"]
		if !carries {
			return condition, false, nil
		}
		id, _ := reference.(string)
		values, declared := sets[id]
		if !declared {
			return nil, false, &UnresolvedError{Pointer: pointer + "/valueSet", ID: id}
		}
		changed := maps.Clone(object)
		delete(changed, "valueSet")
		changed["value"] = values
		return changed, true, nil
	case "all", "any":
		children, ok := object["conditions"].([]any)
		if !ok {
			return condition, false, nil
		}
		var copied []any
		for index, child := range children {
			expanded, changed, err := expand(child, pointer+"/conditions/"+strconv.Itoa(index), sets)
			if err != nil {
				return nil, false, err
			}
			if changed {
				if copied == nil {
					copied = slices.Clone(children)
				}
				copied[index] = expanded
			}
		}
		if copied == nil {
			return condition, false, nil
		}
		changed := maps.Clone(object)
		changed["conditions"] = copied
		return changed, true, nil
	case "not":
		child, present := object["condition"]
		if !present {
			return condition, false, nil
		}
		expanded, changed, err := expand(child, pointer+"/condition", sets)
		if err != nil || !changed {
			return condition, false, err
		}
		copied := maps.Clone(object)
		copied["condition"] = expanded
		return copied, true, nil
	}
	return condition, false, nil
}

// withoutAnnotations returns object without the annotations extension. The
// extensions member is removed when that removal empties it, and kept when it
// was already empty or holds anything else.
func withoutAnnotations(object map[string]any) map[string]any {
	extensions, ok := object["extensions"].(map[string]any)
	if !ok {
		return object
	}
	if _, annotated := extensions[AnnotationsName]; !annotated {
		return object
	}
	changed := maps.Clone(object)
	if len(extensions) == 1 {
		delete(changed, "extensions")
		return changed
	}
	remaining := maps.Clone(extensions)
	delete(remaining, AnnotationsName)
	changed["extensions"] = remaining
	return changed
}
