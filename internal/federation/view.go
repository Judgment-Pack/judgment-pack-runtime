package federation

import (
	"maps"
	"slices"
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// view is what an assembly reads of a registry document beside what
// format 1.0.0's validation reports: the entries registry §7.1's step 3
// keeps, and each environment's patches. Validation keeps them too, but
// returns resolved values only for a valid registry, and an assembly reads
// them in registries that are not valid yet (§4, steps 5 to 7).
type view struct {
	entries []keptEntry
	first   map[registry.EntryName]int
	patches map[string]map[registry.EntryName]map[string]any
}

// keptEntry is one entry of the form step 3 keeps: its index in the
// document's entries, its object and its name.
type keptEntry struct {
	index        int
	object       map[string]any
	name         registry.EntryName
	environments []string
	hasEnvs      bool
}

var entryMembers = map[string]bool{"kind": true, "id": true, "owner": true, "value": true, "environments": true}

// newView reads a registry document that passed step 3's shape.
func newView(root map[string]any) *view {
	v := &view{first: map[registry.EntryName]int{}, patches: map[string]map[registry.EntryName]map[string]any{}}
	items, _ := root["entries"].([]any)
	for i, item := range items {
		e, ok := keepEntry(i, item)
		if !ok {
			continue
		}
		if _, seen := v.first[e.name]; !seen {
			v.first[e.name] = len(v.entries)
		}
		v.entries = append(v.entries, e)
	}
	environments := stringItems(root["environments"])
	overlays, _ := root["overlays"].([]any)
	for _, item := range overlays {
		overlay, ok := item.(map[string]any)
		if !ok || len(overlay) != 2 {
			continue
		}
		env, envOK := overlay["environment"].(string)
		patches, patchesOK := overlay["patches"].([]any)
		if !envOK || !patchesOK || !containsString(environments, env) {
			continue
		}
		if _, seen := v.patches[env]; seen {
			continue
		}
		named := map[registry.EntryName]map[string]any{}
		v.patches[env] = named
		for _, value := range patches {
			patch, ok := value.(map[string]any)
			if !ok || len(patch) != 3 {
				continue
			}
			kind, kindOK := patch["kind"].(string)
			id, idOK := patch["id"].(string)
			set, setOK := patch["set"].(map[string]any)
			if !kindOK || !idOK || !setOK {
				continue
			}
			name := registry.EntryName{Kind: kind, ID: id}
			if _, seen := named[name]; !seen {
				named[name] = set
			}
		}
	}
	return v
}

// keepEntry is step 3's form of one entry, as format 1.0.0 reads it.
func keepEntry(index int, item any) (keptEntry, bool) {
	object, ok := item.(map[string]any)
	if !ok {
		return keptEntry{}, false
	}
	for name := range object {
		if !entryMembers[name] {
			return keptEntry{}, false
		}
	}
	for _, name := range []string{"kind", "id", "owner", "value"} {
		if _, present := object[name]; !present {
			return keptEntry{}, false
		}
	}
	kind, kindOK := object["kind"].(string)
	id, idOK := object["id"].(string)
	owner, ownerOK := object["owner"].(string)
	if !kindOK || !registry.ValidKindName(kind) || !idOK || !ownerOK || !registry.ValidOwner(owner) {
		return keptEntry{}, false
	}
	e := keptEntry{index: index, object: object, name: registry.EntryName{Kind: kind, ID: id}}
	if value, present := object["environments"]; present {
		items, isList := value.([]any)
		if !isList || len(items) == 0 || !distinctStrings(items) {
			return keptEntry{}, false
		}
		e.environments, e.hasEnvs = stringItems(items), true
	}
	return e, true
}

// applies reports whether the entry applies in an environment (registry §4).
func (e keptEntry) applies(env string) bool {
	return !e.hasEnvs || containsString(e.environments, env)
}

// resolved is the value of the kept entry at position i in an environment:
// its own, with the environment's patch for its name set into a copy.
func (v *view) resolved(i int, env string) any {
	e := v.entries[i]
	value := e.object["value"]
	set := v.patches[env][e.name]
	for _, key := range sortedKeys(set) {
		tokens, err := registry.ParsePointer(key)
		if err != nil {
			continue
		}
		value = setCopy(value, tokens, set[key])
	}
	return value
}

// referenceAt reads the entry reference at a pointer of a named entry's
// resolved value in an environment.
func (v *view) referenceAt(name registry.EntryName, env, pointer string) (registry.EntryName, bool) {
	i, ok := v.first[name]
	if !ok {
		return registry.EntryName{}, false
	}
	tokens, err := registry.ParsePointer(pointer)
	if err != nil {
		return registry.EntryName{}, false
	}
	value, found := registry.Lookup(v.resolved(i, env), tokens)
	text, isString := value.(string)
	if !found || !isString {
		return registry.EntryName{}, false
	}
	kind, id, ok := registry.ParseEntryReference(text)
	return registry.EntryName{Kind: kind, ID: id}, ok
}

// setCopy sets a value at a path, copying each container on the way so
// that the document's own values are not changed.
func setCopy(node any, path []string, value any) any {
	if len(path) == 0 {
		return value
	}
	switch typed := node.(type) {
	case map[string]any:
		typed = maps.Clone(typed)
		typed[path[0]] = setCopy(typed[path[0]], path[1:], value)
		return typed
	case []any:
		index, err := strconv.Atoi(path[0])
		if err != nil || index < 0 || index >= len(typed) || strconv.Itoa(index) != path[0] {
			return typed
		}
		typed = slices.Clone(typed)
		typed[index] = setCopy(typed[index], path[1:], value)
		return typed
	}
	return node
}

func stringItems(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

// entryName is a location's entry: present when the kind and the id are
// of their forms (registry §7.3).
func entryName(name registry.EntryName) *registry.EntryName {
	if !registry.ValidKindName(name.Kind) || !registry.ValidEntryID(name.ID) {
		return nil
	}
	return &name
}
