package registry

import (
	"slices"
	"strconv"
)

var registryMembers = map[string]bool{
	"registryFormatVersion": true, "id": true, "version": true, "environments": true, "owners": true,
	"kinds": true, "entries": true, "overlays": true, "secretScopes": true,
}

var registryRequired = []string{"id", "version", "environments", "owners", "kinds", "entries"}

// applies reports whether the entry applies in the environment.
func (e *entry) applies(environment string) bool {
	return e.envs == nil || slices.Contains(e.envs, environment)
}

// readRegistry runs step 3. A malformed member outside entries and overlays
// stops validation, since nothing after it can be read. A malformed entry, an
// overlay or patch is dropped and the rest goes on. An order error does not
// stop anything: the document can still be read.
func readRegistry(root map[string]any, diagnostics *collector) *registryDoc {
	stop := false
	fail := func(code string, path ...string) {
		diagnostics.add(code, Location{Document: DocumentRegistry, Pointer: pointerOf(path)})
	}
	for _, name := range sortedKeys(root) {
		if !registryMembers[name] {
			fail("REG-REGISTRY-UNKNOWN-MEMBER", name)
			stop = true
		}
	}
	for _, name := range registryRequired {
		if _, ok := root[name]; !ok {
			fail("REG-REGISTRY-MISSING-MEMBER", name)
			stop = true
		}
	}
	if stop {
		return nil
	}
	registry := &registryDoc{root: root, owners: map[string]bool{}, byName: map[EntryName]*entry{}, overlays: map[string]map[EntryName]*patch{}}
	if id, ok := root["id"].(string); ok && isAbsoluteURI(id) {
		registry.identity.ID = id
	} else {
		fail("REG-REGISTRY-FORM", "id")
		stop = true
	}
	if version, ok := root["version"].(string); ok && semverPattern.MatchString(version) {
		registry.identity.Version = version
	} else {
		fail("REG-REGISTRY-FORM", "version")
		stop = true
	}
	registry.environments, stop = readNames(root, "environments", environmentRegex.MatchString, 32, fail, stop)
	var owners []string
	owners, stop = readNames(root, "owners", ownerPattern.MatchString, 1<<20, fail, stop)
	for _, owner := range owners {
		registry.owners[owner] = true
	}
	stop = readPins(registry, root["kinds"], fail) || stop
	entries, isArray := root["entries"].([]any)
	if !isArray {
		fail("REG-REGISTRY-FORM", "entries")
		stop = true
	}
	if value, present := root["overlays"]; present {
		if _, ok := value.([]any); !ok {
			fail("REG-REGISTRY-FORM", "overlays")
			stop = true
		}
	}
	if value, present := root["secretScopes"]; present && !stop {
		stop = readScopes(registry, value, diagnostics)
	}
	if stop {
		return nil
	}
	for i, item := range entries {
		if e := readEntry(item, i, fail); e != nil {
			registry.entries = append(registry.entries, e)
			if _, exists := registry.byName[e.name]; !exists {
				registry.byName[e.name] = e
			}
		}
	}
	for i := 1; i < len(registry.pins); i++ {
		if registry.pins[i-1].kind > registry.pins[i].kind {
			fail("REG-REGISTRY-ORDER", "kinds", strconv.Itoa(registry.pins[i].index))
			break
		}
	}
	for i := 1; i < len(registry.entries); i++ {
		previous, current := registry.entries[i-1].name, registry.entries[i].name
		if previous.Kind > current.Kind || previous.Kind == current.Kind && previous.ID > current.ID {
			fail("REG-REGISTRY-ORDER", "entries", strconv.Itoa(registry.entries[i].index))
			break
		}
	}
	return registry
}

// readNames reads a non-empty array of distinct names of a grammar.
func readNames(root map[string]any, member string, valid func(string) bool, max int, fail func(string, ...string), stop bool) ([]string, bool) {
	items, ok := root[member].([]any)
	if !ok || len(items) == 0 || len(items) > max {
		fail("REG-REGISTRY-FORM", member)
		return nil, true
	}
	var names []string
	for i, item := range items {
		name, ok := item.(string)
		if !ok || !valid(name) {
			fail("REG-REGISTRY-FORM", member, strconv.Itoa(i))
			stop = true
			continue
		}
		if slices.Contains(names, name) {
			fail("REG-REGISTRY-DUPLICATE", member, strconv.Itoa(i))
			stop = true
			continue
		}
		names = append(names, name)
	}
	return names, stop
}

func readPins(registry *registryDoc, value any, fail func(string, ...string)) bool {
	items, ok := value.([]any)
	if !ok {
		fail("REG-REGISTRY-FORM", "kinds")
		return true
	}
	stop := false
	for i, item := range items {
		at := strconv.Itoa(i)
		object, ok := item.(map[string]any)
		if !ok {
			fail("REG-REGISTRY-FORM", "kinds", at)
			stop = true
			continue
		}
		for _, name := range sortedKeys(object) {
			if name != "kind" && name != "id" && name != "version" && name != "digest" {
				fail("REG-REGISTRY-UNKNOWN-MEMBER", "kinds", at, name)
				stop = true
			}
		}
		p := &pin{index: i}
		var good bool
		p.kind, good = object["kind"].(string)
		good = good && kindNamePattern.MatchString(p.kind)
		var idOK, versionOK, digestOK bool
		p.id, idOK = object["id"].(string)
		p.version, versionOK = object["version"].(string)
		p.digest, digestOK = object["digest"].(string)
		if !good || !idOK || !isAbsoluteURI(p.id) || !versionOK || !semverPattern.MatchString(p.version) || !digestOK || !digestPattern.MatchString(p.digest) {
			fail("REG-REGISTRY-FORM", "kinds", at)
			stop = true
			continue
		}
		registry.pins = append(registry.pins, p)
	}
	return stop
}

func readScopes(registry *registryDoc, value any, diagnostics *collector) bool {
	fail := func(code string, path ...string) {
		diagnostics.add(code, Location{Document: DocumentRegistry, Pointer: pointerOf(append([]string{"secretScopes"}, path...))})
	}
	object, ok := value.(map[string]any)
	if !ok {
		fail("REF-SECRET-SCOPE-FORM")
		return true
	}
	stop := false
	registry.scopes = map[string]map[string][]string{}
	for _, environment := range registry.environments {
		if _, ok := object[environment]; !ok {
			fail("REF-SECRET-SCOPE-FORM")
			stop = true
		}
	}
	owner := map[string]string{}
	for _, environment := range sortedKeys(object) {
		stores, ok := object[environment].(map[string]any)
		if !ok || !slices.Contains(registry.environments, environment) {
			fail("REF-SECRET-SCOPE-FORM", environment)
			stop = true
			continue
		}
		registry.scopes[environment] = map[string][]string{}
		for _, store := range sortedKeys(stores) {
			projects, ok := stores[store].([]any)
			if !ok || store != StoreGCPSecretManager {
				fail("REF-SECRET-SCOPE-FORM", environment, store)
				stop = true
				continue
			}
			for i, item := range projects {
				project, ok := item.(string)
				if !ok || !projectIDPattern.MatchString(project) || slices.Contains(registry.scopes[environment][store], project) {
					fail("REF-SECRET-SCOPE-FORM", environment, store, strconv.Itoa(i))
					stop = true
					continue
				}
				key := store + "\x00" + project
				if other, shared := owner[key]; shared && other != environment {
					fail("REF-SECRET-SCOPE-SHARED", environment, store, strconv.Itoa(i))
				}
				owner[key] = environment
				registry.scopes[environment][store] = append(registry.scopes[environment][store], project)
			}
		}
	}
	return stop
}

var entryMembers = map[string]bool{"kind": true, "id": true, "owner": true, "environments": true, "value": true}

func readEntry(item any, index int, fail func(string, ...string)) *entry {
	at := strconv.Itoa(index)
	object, ok := item.(map[string]any)
	if !ok {
		fail("REG-REGISTRY-FORM", "entries", at)
		return nil
	}
	bad := false
	for _, name := range sortedKeys(object) {
		if !entryMembers[name] {
			fail("REG-REGISTRY-UNKNOWN-MEMBER", "entries", at, name)
			bad = true
		}
	}
	for _, name := range []string{"kind", "id", "owner", "value"} {
		if _, ok := object[name]; !ok {
			fail("REG-REGISTRY-MISSING-MEMBER", "entries", at, name)
			bad = true
		}
	}
	if bad {
		return nil
	}
	e := &entry{index: index, value: object["value"]}
	kind, kindOK := object["kind"].(string)
	id, idOK := object["id"].(string)
	owner, ownerOK := object["owner"].(string)
	if !kindOK || !kindNamePattern.MatchString(kind) {
		fail("REG-REGISTRY-FORM", "entries", at, "kind")
		bad = true
	}
	if !idOK {
		fail("REG-REGISTRY-FORM", "entries", at, "id")
		bad = true
	}
	if !ownerOK || !ownerPattern.MatchString(owner) {
		fail("REG-REGISTRY-FORM", "entries", at, "owner")
		bad = true
	}
	if value, present := object["environments"]; present {
		items, ok := value.([]any)
		if !ok || len(items) == 0 {
			fail("REG-REGISTRY-FORM", "entries", at, "environments")
			bad = true
		}
		e.envs = []string{}
		for i, item := range items {
			name, ok := item.(string)
			if !ok || slices.Contains(e.envs, name) {
				fail("REG-REGISTRY-FORM", "entries", at, "environments", strconv.Itoa(i))
				bad = true
				continue
			}
			e.envs = append(e.envs, name)
		}
	}
	if bad {
		return nil
	}
	e.name, e.owner = EntryName{Kind: kind, ID: id}, owner
	return e
}

// checkOverlays runs step 6 and records the patches that pass.
func checkOverlays(registry *registryDoc, diagnostics *collector) {
	items, _ := registry.root["overlays"].([]any)
	fail := func(code string, entry *EntryName, path ...string) {
		diagnostics.add(code, Location{Document: DocumentRegistry, Entry: entry, Pointer: pointerOf(append([]string{"overlays"}, path...))})
	}
	for i, item := range items {
		at := strconv.Itoa(i)
		object, ok := item.(map[string]any)
		environment, environmentOK := object["environment"].(string)
		patches, patchesOK := object["patches"].([]any)
		if !ok || len(object) != 2 || !environmentOK || !patchesOK {
			fail("REG-REGISTRY-FORM", nil, at)
			continue
		}
		if !slices.Contains(registry.environments, environment) {
			fail("REG-OVERLAY-ENVIRONMENT", nil, at, "environment")
			continue
		}
		if _, duplicate := registry.overlays[environment]; duplicate {
			fail("REG-OVERLAY-DUPLICATE", nil, at, "environment")
			continue
		}
		registry.overlays[environment] = map[EntryName]*patch{}
		seen := map[EntryName]bool{}
		for j, patchItem := range patches {
			patchAt := []string{at, "patches", strconv.Itoa(j)}
			patchObject, ok := patchItem.(map[string]any)
			kind, kindOK := patchObject["kind"].(string)
			id, idOK := patchObject["id"].(string)
			set, setOK := patchObject["set"].(map[string]any)
			if !ok || len(patchObject) != 3 || !kindOK || !idOK || !setOK {
				fail("REG-REGISTRY-FORM", nil, patchAt...)
				continue
			}
			name := EntryName{Kind: kind, ID: id}
			if seen[name] {
				fail("REG-OVERLAY-PATCH-DUPLICATE", &name, patchAt...)
				continue
			}
			seen[name] = true
			target := registry.byName[name]
			if target == nil {
				fail("REG-OVERLAY-ENTRY-MISSING", &name, patchAt...)
				continue
			}
			if !target.passed {
				continue
			}
			if !target.applies(environment) {
				fail("REG-OVERLAY-ENTRY-ABSENT", &name, patchAt...)
				continue
			}
			p := &patch{entry: target}
			ok = true
			var paths [][]string
			for _, pointer := range sortedKeys(set) {
				pointerAt := append(slices.Clone(patchAt), "set", pointer)
				path, err := parsePointer(pointer)
				switch {
				case err != nil:
					fail("REG-OVERLAY-POINTER", &name, pointerAt...)
					ok = false
					continue
				case !slices.ContainsFunc(target.kind.overlayable, func(pattern pointerPattern) bool { return pattern.matches(path) }):
					fail("REG-OVERLAY-NOT-OVERLAYABLE", &name, pointerAt...)
					ok = false
					continue
				case !reachableTarget(target, path):
					fail("REG-OVERLAY-TARGET", &name, pointerAt...)
					ok = false
					continue
				}
				if set[pointer] == nil {
					fail("REG-OVERLAY-NULL", &name, pointerAt...)
					ok = false
					continue
				}
				for _, other := range paths {
					if isPrefix(other, path) || isPrefix(path, other) {
						fail("REG-OVERLAY-PREFIX", &name, pointerAt...)
						ok = false
					}
				}
				paths = append(paths, path)
				p.set = append(p.set, setOperation{path: path, value: set[pointer]})
			}
			if ok {
				registry.overlays[environment][name] = p
			}
		}
	}
}

// reachableTarget applies RFC 0034 §4: the pointer addresses a member or
// element that exists in the base value, or a member the schema declares
// under a parent that exists.
func reachableTarget(e *entry, path []string) bool {
	if len(path) == 0 {
		return false
	}
	if _, exists := lookup(e.value, path); exists {
		return true
	}
	parent, exists := lookup(e.value, path[:len(path)-1])
	if !exists {
		return false
	}
	if _, isObject := parent.(map[string]any); !isObject {
		return false
	}
	return e.kind.schema.declares(path[:len(path)-1], path[len(path)-1])
}
