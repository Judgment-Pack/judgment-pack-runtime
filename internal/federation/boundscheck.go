package federation

import (
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// declaration returns the kind declaration of a canonical digest that format
// 1.0.0's validation reads when several supplied documents have it: the one
// whose bytes have the smallest digest. It returns nil when no source
// supplies one.
func (a *assembly) declaration(digest string) *kindDocument {
	var out *kindDocument
	for _, s := range a.manifest.sources {
		for _, k := range s.kinds {
			if k.digest == digest && (out == nil || k.raw < out.raw) {
				out = k
			}
		}
	}
	return out
}

// readable is a declaration read as registry §7.1's step 2 reads it, or nil
// when that declaration is unusable.
func readable(k *kindDocument) *registry.Kind {
	if k == nil {
		return nil
	}
	kind, _ := registry.ReadKind(k.root, registry.Location{Document: registry.DocumentKind, KindDocument: k.raw})
	return kind
}

// declarationFor is the declaration that governs or is pinned for a kind
// (§6.1): the assembled registry's pin's, and otherwise the standard's for
// that kind name.
func (a *assembly) declarationFor(kind string) *kindDocument {
	if a.assembled != nil {
		for _, p := range pinsOf(a.assembled.root) {
			if p.kind == kind {
				if k := a.declaration(p.digest); k != nil {
					return k
				}
			}
		}
	}
	if standard := a.standard(); standard != nil {
		for _, k := range standard.kinds {
			if name, ok := k.name(); ok && name == kind {
				return k
			}
		}
	}
	return nil
}

// pinnedKinds are the kind names the assembled registry pins, or, when there
// is none, the union of the sources' pins.
func (a *assembly) pinnedKinds() map[string]bool {
	out := map[string]bool{}
	if a.assembled != nil {
		for _, p := range pinsOf(a.assembled.root) {
			out[p.kind] = true
		}
		return out
	}
	for _, s := range a.manifest.sources {
		for _, p := range pinsOf(s.registryRoot) {
			out[p.kind] = true
		}
	}
	return out
}

// Step 7: the bounds (§6).
func (a *assembly) step7() {
	environments := a.manifest.environments
	governed := a.governed()
	pinned := a.pinnedKinds()
	var documents []*boundsDocument
	var standard *boundsDocument
	for _, s := range a.manifest.sources {
		if s.boundsRoot == nil {
			continue
		}
		b := readBounds(s.boundsRoot, s, environments, a.collected)
		documents = append(documents, b)
		if s.standard() && standard == nil {
			standard = b
		}
		for _, bk := range b.kinds {
			_, isGoverned := governed[bk.kind]
			if !isGoverned && (s.standard() || !pinned[bk.kind]) {
				a.collected.add("FED-BOUND-KIND", Location{Document: DocumentBounds, Source: s.name, Pointer: bk.at + "/kind"})
				continue
			}
			kind := readable(a.declarationFor(bk.kind))
			if kind == nil {
				continue
			}
			for _, f := range bk.fields {
				if !f.whole && !kind.Reaches(f.pattern) {
					a.collected.add("FED-BOUND-POINTER-UNREACHABLE", Location{Document: DocumentBounds, Source: s.name, Pointer: f.at + "/pointer"})
				}
			}
		}
	}
	widens := func(b *boundsDocument, pointer string) {
		a.collected.add("FED-BOUND-WIDENS", Location{Document: DocumentBounds, Source: b.source.name, Pointer: pointer})
	}
	for _, b := range documents {
		for _, bk := range b.kinds {
			for _, f := range bk.fields {
				for _, env := range sortedKeys(f.environments) {
					for _, member := range widenings(f.environments[env], f.bound) {
						widens(b, f.at+"/environments/"+escapeToken(env)+"/"+member)
					}
				}
			}
		}
	}
	if standard != nil {
		for _, b := range documents {
			if b == standard {
				continue
			}
			a.compare(b, standard, widens)
		}
	}
	a.values(documents, standard)
	a.entryCounts(documents, standard)
}

// compare is §6.5's first check: a repository's bounds against the
// standard's, kind by kind, field by field, and per environment it serves.
func (a *assembly) compare(b, standard *boundsDocument, widens func(*boundsDocument, string)) {
	for _, bk := range b.kinds {
		sk := standard.kind(bk.kind)
		if sk == nil {
			continue
		}
		if bk.maxEntries >= 0 && sk.maxEntries >= 0 && bk.maxEntries > sk.maxEntries {
			widens(b, bk.at+"/maxEntries")
		}
		for _, f := range bk.fields {
			var same *field
			for _, sf := range sk.fields {
				if sf.pointer == f.pointer {
					same = sf
					break
				}
			}
			if same == nil {
				for _, sf := range sk.fields {
					if f.overlaps(sf) {
						a.collected.add("FED-BOUND-OVERLAP", Location{Document: DocumentBounds, Source: b.source.name, Pointer: f.at + "/pointer"})
						break
					}
				}
				continue
			}
			for _, member := range widenings(f.bound, same.bound) {
				widens(b, f.at+"/bound/"+member)
			}
			for _, env := range b.source.serves {
				r, fromEnv := f.effective(env)
				s, _ := same.effective(env)
				for _, member := range widenings(r, s) {
					if fromEnv[member] {
						widens(b, f.at+"/environments/"+escapeToken(env)+"/"+member)
					} else {
						widens(b, f.at+"/bound/"+member)
					}
				}
			}
		}
	}
}

// values is §6.5's second check, on the resolved values of the entries
// registry §7.1's step 7 checks in the assembled registry.
func (a *assembly) values(documents []*boundsDocument, standard *boundsDocument) {
	built := a.assembled
	if built == nil || built.result.Report.Registry == nil {
		return
	}
	if truncated(built.result.Report) && !built.result.Report.Valid {
		// Which entries step 7 checked is read from the report, and a
		// report that lists only its first diagnostics does not say. The
		// assembly is refused already (insertValidation).
		return
	}
	bySource := map[string]*boundsDocument{}
	for _, b := range documents {
		bySource[b.source.name] = b
	}
	checked := a.checked()
	for _, env := range a.manifest.environments {
		for k, e := range built.view.entries {
			if !checked(k, env) {
				continue
			}
			value := built.view.resolved(k, env)
			holder := built.entries[e.index].source
			var checks []*boundsDocument
			if standard != nil {
				checks = append(checks, standard)
			}
			if own := bySource[holder]; own != nil && !own.source.standard() {
				checks = append(checks, own)
			}
			for _, b := range checks {
				bk := b.kind(e.name.Kind)
				if bk == nil {
					continue
				}
				for _, f := range bk.fields {
					for _, position := range f.positions(value) {
						if !f.admits(env, position.Value) {
							a.collected.add("FED-VALUE-BOUND", Location{
								Document: registry.DocumentValue, Source: b.source.name, Entry: entryName(e.name),
								Environment: env, Pointer: position.Pointer,
							})
						}
					}
				}
			}
		}
	}
}

// checked reports whether registry §7.1's step 7 checks the assembled
// registry's kept entry k in an environment: it passed step 5, it applies
// there, and no patch of it there failed step 6. Format 1.0.0's validation
// keeps the resolved values only of a valid registry, so this reads which
// entries passed from its report: an entry with an error at its pointer did
// not, nor did an entry whose kind's pin is not usable.
func (a *assembly) checked() func(k int, env string) bool {
	built := a.assembled
	excluded := map[int]bool{}
	unusable := map[int]bool{}
	failed := map[string]map[registry.EntryName]bool{}
	for _, d := range built.result.Report.Diagnostics {
		if d.Severity != SeverityError || d.Location.Document != registry.DocumentRegistry || d.Code == "REG-REGISTRY-ORDER" {
			continue
		}
		tokens, err := registry.ParsePointer(d.Location.Pointer)
		if err != nil || len(tokens) < 2 {
			continue
		}
		i, err := strconv.Atoi(tokens[1])
		if err != nil {
			continue
		}
		switch tokens[0] {
		case "entries":
			excluded[i] = true
		case "kinds":
			if d.Code == "REG-PIN-MISSING" || d.Code == "REG-PIN-MISMATCH" {
				unusable[i] = true
			}
		case "overlays":
			if d.Location.Entry != nil && i < len(built.overlays) {
				env := built.overlays[i].environment
				if failed[env] == nil {
					failed[env] = map[registry.EntryName]bool{}
				}
				failed[env][*d.Location.Entry] = true
			}
		}
	}
	usable := map[string]bool{}
	for _, p := range pinsOf(built.root) {
		usable[p.kind] = !unusable[p.index] && readable(a.declaration(p.digest)) != nil
	}
	return func(k int, env string) bool {
		e := built.view.entries[k]
		return !excluded[e.index] && usable[e.name.Kind] && e.applies(env) && !failed[env][e.name]
	}
}

// entryCounts is §6.5's third check.
func (a *assembly) entryCounts(documents []*boundsDocument, standard *boundsDocument) {
	for _, s := range a.manifest.sources {
		if s.validation == nil || s.validation.Report.Registry == nil {
			continue
		}
		limits := map[string][]int{}
		held := []*boundsDocument{}
		if standard != nil {
			held = append(held, standard)
		}
		for _, b := range documents {
			if b.source == s && !s.standard() {
				held = append(held, b)
			}
		}
		for _, b := range held {
			for _, bk := range b.kinds {
				if bk.maxEntries >= 0 {
					limits[bk.kind] = append(limits[bk.kind], bk.maxEntries)
				}
			}
		}
		counts := map[string]int{}
		for _, e := range s.document.entries {
			counts[e.name.Kind]++
			for _, most := range limits[e.name.Kind] {
				if counts[e.name.Kind] == most+1 {
					a.collected.add("FED-BOUND-ENTRIES", Location{Document: registry.DocumentRegistry, Source: s.name, Entry: entryName(e.name), Pointer: carrier.Pointer([]string{"entries", strconv.Itoa(e.index)})})
					break
				}
			}
		}
	}
	built := a.assembled
	if standard == nil || built == nil {
		return
	}
	counts := map[string]int{}
	items, _ := built.root["entries"].([]any)
	for i, item := range items {
		entry, _ := item.(map[string]any)
		kind, _ := entry["kind"].(string)
		id, _ := entry["id"].(string)
		counts[kind]++
		if bk := standard.kind(kind); bk != nil && bk.maxAssembly >= 0 && counts[kind] == bk.maxAssembly+1 {
			from := built.entries[i]
			a.collected.add("FED-BOUND-ENTRIES", Location{Document: registry.DocumentRegistry, Source: from.source, Entry: entryName(registry.EntryName{Kind: kind, ID: id}), Pointer: carrier.Pointer([]string{"entries", strconv.Itoa(from.index)})})
		}
	}
}
