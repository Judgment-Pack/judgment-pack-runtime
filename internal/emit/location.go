package emit

import (
	"bytes"
	"encoding/json"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// requestIndex is what an emitter diagnostic's location may name: the
// environments, kinds and entries of the request it answers, and the
// request's facts contracts in that form. It is built once per response, in
// one pass over the request, and each location is then resolved with map
// lookups and one pointer walk, so the check is linear in the request and
// the diagnostics.
type requestIndex struct {
	facts bool
	// The resolved form: each environment's kinds and entries, the
	// environments in order, and the kinds of the first, which every
	// environment shares.
	environments map[string]*environmentIndex
	order        []string
	// The facts form: the request's factsContracts array, and the digests
	// of the kinds the contracts were made from.
	contracts       any
	contractDigests map[string]bool
}

type environmentIndex struct {
	kinds   map[string]string // kind name to its pin's digest
	digests map[string]bool
	entries map[registry.EntryName]any
}

// indexRequest reads a request this host formed. A request that cannot be
// read indexes nothing, so that no location that names anything resolves.
func indexRequest(request []byte) *requestIndex {
	index := &requestIndex{environments: map[string]*environmentIndex{}, contractDigests: map[string]bool{}}
	decoder := json.NewDecoder(bytes.NewReader(request))
	decoder.UseNumber()
	var root map[string]any
	if decoder.Decode(&root) != nil {
		return index
	}
	if contracts, ok := root["factsContracts"].([]any); ok {
		index.facts = true
		index.contracts = contracts
		for _, contract := range contracts {
			if digest, ok := member(contract, "source", "kind", "digest").(string); ok {
				index.contractDigests[digest] = true
			}
		}
		return index
	}
	resolved, _ := root["resolved"].([]any)
	for _, document := range resolved {
		environment, ok := member(document, "environment").(string)
		if !ok || index.environments[environment] != nil {
			continue
		}
		e := &environmentIndex{kinds: map[string]string{}, digests: map[string]bool{}, entries: map[registry.EntryName]any{}}
		kinds, _ := member(document, "kinds").([]any)
		for _, pin := range kinds {
			kind, kindOK := member(pin, "kind").(string)
			digest, digestOK := member(pin, "digest").(string)
			if kindOK && digestOK {
				e.kinds[kind] = digest
				e.digests[digest] = true
			}
		}
		entries, _ := member(document, "entries").([]any)
		for _, entry := range entries {
			kind, kindOK := member(entry, "kind").(string)
			id, idOK := member(entry, "id").(string)
			object, _ := entry.(map[string]any)
			if value, present := object["value"]; kindOK && idOK && present {
				e.entries[registry.EntryName{Kind: kind, ID: id}] = value
			}
		}
		index.environments[environment] = e
		index.order = append(index.order, environment)
	}
	return index
}

// member returns the value at a path of object members, or nil.
func member(value any, path ...string) any {
	for _, name := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[name]
	}
	return value
}

// resolves reports whether every member of a location names something in
// the request (RFC 0036 §3: a location carries no value, so it may name only
// what the emitter was given):
//   - an environment is one the request holds; with none, a request of one
//     environment is read as naming it;
//   - a kind is one the request pins, and a kindDocument is the digest of
//     one (of the location's kind, when it names one);
//   - an entry is one of the request's entries of that kind, in the
//     location's environment; a request of several environments ("all")
//     must say which;
//   - the pointer names an existing position in the entry's value, or, with
//     no entry, is "" (the whole document);
//   - in the facts-contracts form there is no environment, kind name or
//     entry: a kindDocument is the digest of a kind a contract was made from,
//     and the pointer names a position in the request's factsContracts
//     array, its first token the contract's index;
//   - nothing in a request is YAML, so no location has a line or a column.
func (index *requestIndex) resolves(location registry.Location) bool {
	if location.Line != 0 || location.Column != 0 {
		return false
	}
	if index.facts {
		return location.Environment == "" && location.Kind == "" && location.Entry == nil &&
			(location.KindDocument == "" || index.contractDigests[location.KindDocument]) &&
			registry.PointerResolves(index.contracts, location.Pointer)
	}
	var environment *environmentIndex
	switch {
	case location.Environment != "":
		environment = index.environments[location.Environment]
		if environment == nil {
			return false
		}
	case len(index.order) == 1:
		environment = index.environments[index.order[0]]
	}
	kinds := environment
	if kinds == nil && len(index.order) > 0 {
		kinds = index.environments[index.order[0]]
	}
	if location.Kind != "" || location.KindDocument != "" || location.Entry != nil {
		if kinds == nil {
			return false
		}
	}
	if location.Kind != "" {
		digest, ok := kinds.kinds[location.Kind]
		if !ok || location.KindDocument != "" && location.KindDocument != digest {
			return false
		}
	} else if location.KindDocument != "" && !kinds.digests[location.KindDocument] {
		return false
	}
	if location.Entry == nil {
		return location.Pointer == ""
	}
	if environment == nil || location.Kind != "" && location.Kind != location.Entry.Kind {
		return false
	}
	value, ok := environment.entries[*location.Entry]
	return ok && registry.PointerResolves(value, location.Pointer)
}
