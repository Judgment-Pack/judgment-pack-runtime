package view_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
)

// The completeness of the view is derived from the schema, not from a list
// written beside the view. Each member the schema of an admitted version
// allows has a key: the definition or inline object it belongs to and its
// name, such as "rule.rationale", "source.locator.kind" or
// "condition{fact}.path" — a condition is labelled by the op of the oneOf
// branch the member belongs to, so every op's members are keys of their own.
// The allOf and the member-free oneOf constraints refine which documents are
// valid and add no member, so they are not walked. An extensions member, and a
// member whose schema admits any value, is a leaf: its contents are the
// author's.
//
// For each version the test then holds the inputs the faithfulness test views
// to three statements: every key is present in some input; every optional key
// is absent from some input that holds its parent; and every array or object
// key the schema lets be empty is present and empty in some input. Together
// with TestEveryAdmittedPackViewsFaithfullyAndStably, which requires every
// input's view to be its document, that is: a member the view drops, or turns
// from absent to empty or from empty to absent, fails for some input; and a
// member a later schema adds fails here until an input holds it.

type schemaKeys struct {
	defs map[string]any
	// members are every key; optional the keys a document may leave out;
	// mayBeEmpty the array and object keys a document may hold empty.
	members, optional, mayBeEmpty map[string]bool
	walked                        map[string]bool
}

type instanceKeys struct {
	present, absent, empty map[string]bool
}

func asObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

// resolve follows a local $ref, returning the definition and its name.
func (s *schemaKeys) resolve(node any) (map[string]any, string) {
	object := asObject(node)
	if reference, ok := object["$ref"].(string); ok {
		name := strings.TrimPrefix(reference, "#/$defs/")
		return asObject(s.defs[name]), name
	}
	return object, ""
}

// branches returns a condition-like oneOf's branches by op, or nil.
func branches(node map[string]any) map[string]map[string]any {
	choices, _ := node["oneOf"].([]any)
	found := map[string]map[string]any{}
	for _, choice := range choices {
		branch := asObject(choice)
		op := asObject(asObject(branch["properties"])["op"])
		if constant, ok := op["const"].(string); ok {
			found[constant] = branch
		}
		values, _ := op["enum"].([]any)
		for _, value := range values {
			found[value.(string)] = branch
		}
	}
	if len(found) == 0 {
		return nil
	}
	return found
}

func (s *schemaKeys) object(scope string, node map[string]any) {
	required := map[string]bool{}
	names, _ := node["required"].([]any)
	for _, name := range names {
		required[name.(string)] = true
	}
	for name, property := range asObject(node["properties"]) {
		key := scope + "." + name
		s.members[key] = true
		if !required[name] {
			s.optional[key] = true
		}
		s.member(key, property)
	}
}

// member walks the schema of one member, or of one array's items.
func (s *schemaKeys) member(key string, node any) {
	resolved, definition := s.resolve(node)
	if definition == "extensions" {
		s.mayBeEmpty[key] = true
		return
	}
	scope := key
	if definition != "" {
		scope = definition
	}
	switch {
	case branches(resolved) != nil:
		if s.walked[scope] {
			return
		}
		s.walked[scope] = true
		for op, branch := range branches(resolved) {
			s.object(scope+"{"+op+"}", branch)
		}
	case resolved["type"] == "object" && resolved["properties"] != nil:
		if names, _ := resolved["required"].([]any); len(names) == 0 {
			s.mayBeEmpty[key] = true
		}
		if s.walked[scope] {
			return
		}
		s.walked[scope] = true
		s.object(scope, resolved)
	case resolved["type"] == "array":
		if minimum, _ := resolved["minItems"].(float64); minimum == 0 {
			s.mayBeEmpty[key] = true
		}
		if items, ok := resolved["items"]; ok {
			s.member(key, items)
		}
	}
}

func readSchemaKeys(t *testing.T, version string) *schemaKeys {
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
	keys := &schemaKeys{defs: asObject(schema["$defs"]), members: map[string]bool{}, optional: map[string]bool{}, mayBeEmpty: map[string]bool{}, walked: map[string]bool{}}
	keys.object("pack", schema)
	return keys
}

// instance walks one decoded document along its schema, recording each key
// it finds present, absent from a parent that is present, or present and
// empty.
func (s *schemaKeys) instance(keys *instanceKeys, scope string, node map[string]any, value map[string]any) {
	for name, property := range asObject(node["properties"]) {
		key := scope + "." + name
		member, present := value[name]
		if !present {
			keys.absent[key] = true
			continue
		}
		keys.present[key] = true
		s.instanceMember(keys, key, property, member)
	}
}

func (s *schemaKeys) instanceMember(keys *instanceKeys, key string, node any, value any) {
	resolved, definition := s.resolve(node)
	scope := key
	if definition != "" {
		scope = definition
	}
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 {
			keys.empty[key] = true
		}
		if definition == "extensions" {
			return
		}
		if choices := branches(resolved); choices != nil {
			op, _ := typed["op"].(string)
			s.instance(keys, scope+"{"+op+"}", choices[op], typed)
			return
		}
		if resolved["type"] == "object" {
			s.instance(keys, scope, resolved, typed)
		}
	case []any:
		if len(typed) == 0 {
			keys.empty[key] = true
		}
		if resolved["type"] != "array" {
			return
		}
		for _, item := range typed {
			s.instanceMember(keys, key, resolved["items"], item)
		}
	}
}

func (s *schemaKeys) walkDocument(t *testing.T, root map[string]any, schema map[string]any) *instanceKeys {
	t.Helper()
	keys := &instanceKeys{present: map[string]bool{}, absent: map[string]bool{}, empty: map[string]bool{}}
	s.instance(keys, "pack", schema, root)
	return keys
}

func missing(want, have map[string]bool) []string {
	var keys []string
	for key := range want {
		if !have[key] {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

func TestTheViewCarriesEveryMemberTheSchemaAllows(t *testing.T) {
	inputs := viewInputs(t)
	for _, version := range viewVersions {
		t.Run(version, func(t *testing.T) {
			keys := readSchemaKeys(t, version)
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
			// A floor on the walk itself, so a walk that finds nothing
			// cannot pass: the members of a rule and of a fact condition.
			for _, key := range []string{"pack.rules", "rule.rationale", "rule.when", "condition{fact}.path", "condition{not}.condition", "source.locator.kind", "metadata.reviews", "pack.metadata"} {
				if !keys.members[key] {
					t.Fatalf("the schema walk did not find %s; it found %v", key, slices.Sorted(maps.Keys(keys.members)))
				}
			}

			union := &instanceKeys{present: map[string]bool{}, absent: map[string]bool{}, empty: map[string]bool{}}
			count := 0
			for _, input := range inputs {
				if input.version != version {
					continue
				}
				count++
				root, failure := carrier.Decode(input.pack, carrier.DefaultLimits())
				if failure != nil {
					t.Fatalf("%s: %+v", input.name, failure)
				}
				document := keys.walkDocument(t, asObject(root), schema)
				_, view := mustView(t, input.pack)
				var decoded struct {
					Pack map[string]any `json:"pack"`
				}
				decoder := json.NewDecoder(bytes.NewReader(view.JSON()))
				decoder.UseNumber()
				if err := decoder.Decode(&decoded); err != nil {
					t.Fatal(err)
				}
				viewed := keys.walkDocument(t, decoded.Pack, schema)
				if !maps.Equal(document.present, viewed.present) || !maps.Equal(document.absent, viewed.absent) || !maps.Equal(document.empty, viewed.empty) {
					t.Errorf("%s: the view's members differ from the document's: present %v, absent %v, empty %v",
						input.name, missing(document.present, viewed.present), missing(document.absent, viewed.absent), missing(document.empty, viewed.empty))
				}
				for key := range document.present {
					if !keys.members[key] {
						t.Errorf("%s holds %s, which the schema walk did not find", input.name, key)
					}
					union.present[key] = true
				}
				maps.Copy(union.absent, document.absent)
				maps.Copy(union.empty, document.empty)
			}
			if count == 0 {
				t.Fatal("no input of this version")
			}
			if keys := missing(keys.members, union.present); len(keys) > 0 {
				t.Errorf("no input holds %v", keys)
			}
			if keys := missing(keys.optional, union.absent); len(keys) > 0 {
				t.Errorf("no input leaves out %v", keys)
			}
			if keys := missing(keys.mayBeEmpty, union.empty); len(keys) > 0 {
				t.Errorf("no input holds empty %v", keys)
			}
		})
	}
}
