package federation

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// Input is what an assembly is given (§4): the manifest, each source's
// commit and the bytes of the paths the manifest names for it, and the known
// list. A reader never fetches: a path whose bytes are not given is
// FED-SOURCE-MISSING.
type Input struct {
	Manifest registry.Document
	Sources  map[string]SourceInput
	Known    Known
}

// SourceInput is what an assembly is given for one source, by its name: the
// commit it is read at, and the bytes of each path, by the path the manifest
// names. Oversize marks a file read only to its bound.
type SourceInput struct {
	Commit string
	Files  map[string]registry.Document
}

// Known is §4's list of known assembled indexes and their locks. It has no
// default; an empty list is given as such. A list longer than MaxKnown is
// FED-KNOWN-LIMIT before any document of it, or any source, is read.
type Known struct {
	Indexes []registry.Document
	Locks   []registry.Document
}

// Assembly is an assembly's report and, for an assembly with no error, the
// two documents step 8 writes: the assembled registry and the index, each
// its JCS bytes.
type Assembly struct {
	Report   Report
	Registry []byte
	Index    []byte
}

var commitPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// deferred are the codes step 5 defers to step 6 on a reference whose target
// another source holds.
var deferred = map[string]bool{
	"REF-ENTRY-MISSING": true, "REF-ENTRY-ABSENT-IN-ENVIRONMENT": true, "REF-ENTRY-POINTER": true,
}

// assembled is step 6's registry, with where each of its parts came from.
type assembled struct {
	root     map[string]any
	data     []byte
	view     *view
	result   *registry.Result
	entries  []origin // per assembled entry: its source and its index there
	pins     []origin // per assembled pin
	overlays []assembledOverlay
}

type origin struct {
	source string
	index  int
}

type assembledOverlay struct {
	environment string
	first       origin
	patches     []patchOrigin
}

type patchOrigin struct {
	source         string
	overlay, patch int
}

type assembly struct {
	collected *collector
	manifest  *manifest
	assembled *assembled
	index     map[string]any
}

// Assemble runs §4's assembly. The error reports a document that could not
// be written in its canonical form, which an admitted document never is.
func Assemble(in Input) (Assembly, error) {
	a := &assembly{collected: &collector{}}
	if err := a.run(in); err != nil {
		return Assembly{}, err
	}
	out := Assembly{Report: a.collected.report(a.digest())}
	if out.Report.Valid {
		index, err := jcs.EncodeDocument(a.index)
		if err != nil {
			return Assembly{}, err
		}
		out.Registry, out.Index = a.assembled.data, index
	}
	return out, nil
}

// digest is the report's registry: the digest of the registry the assembly
// validates by registry §7, once that validation passed its step 3.
func (a *assembly) digest() string {
	if a.assembled == nil || a.assembled.result == nil || a.assembled.result.Report.Registry == nil {
		return ""
	}
	return a.assembled.result.Report.Registry.Digest
}

func (a *assembly) run(in Input) error {
	if len(in.Known.Indexes) > MaxKnown || len(in.Known.Locks) > MaxKnown {
		// A refusal of the assembly before any source is read, at the
		// manifest's root.
		a.collected.add("FED-KNOWN-LIMIT", Location{Document: DocumentFederation})
		return nil
	}
	if !a.step1(in) || !a.step2() || !a.step3(in.Known) {
		return nil
	}
	a.step4()
	a.step5()
	if err := a.step6(); err != nil {
		return err
	}
	a.step7()
	if !a.collected.hasErrors() {
		return a.step8()
	}
	return nil
}

// Step 1: the manifest, each source's commit, its repository identity and
// the presence of its paths.
func (a *assembly) step1(in Input) bool {
	if in.Manifest.Oversize {
		a.collected.addRegistry("REG-DOC-SIZE", Location{Document: DocumentFederation})
		return false
	}
	m := readManifest(in.Manifest.Data, a.collected)
	if m == nil {
		return false
	}
	a.manifest = m
	commits := map[string]string{}
	for _, s := range m.sources {
		given, ok := in.Sources[s.name]
		if s.name == "" {
			ok = false
		}
		if ok {
			s.commit, s.files = given.Commit, given.Files
		}
		repository, isString := s.repository.(string)
		switch {
		case !commitPattern.MatchString(s.commit):
			a.collected.add("FED-COMMIT", Location{Document: DocumentFederation, Pointer: s.at()})
		case isString:
			if first, seen := commits[repository]; seen && first != s.commit {
				a.collected.add("FED-COMMIT", Location{Document: DocumentFederation, Pointer: s.at()})
			}
			if _, seen := commits[repository]; !seen {
				commits[repository] = s.commit
			}
		}
		if s.hasRepository && !validRepository(s.repository) {
			a.collected.add("FED-REPOSITORY", Location{Document: DocumentFederation, Pointer: s.at("repository")})
		}
		for _, p := range s.paths {
			if _, given := s.files[p.path]; !given {
				a.collected.add("FED-SOURCE-MISSING", Location{Document: DocumentFederation, Pointer: p.at})
			}
		}
	}
	return !a.collected.hasErrors()
}

// Step 2: each path's content digest, the import of each YAML path, and the
// admission of every document under its own selector and bound.
func (a *assembly) step2() bool {
	for _, s := range a.manifest.sources {
		s.documents = map[string]indexDocument{}
		for _, p := range s.paths {
			file := s.files[p.path]
			sum := sha256.Sum256(file.Data)
			item := indexDocument{path: p.path, role: p.role, contentDigest: "sha256:" + hex.EncodeToString(sum[:])}
			data, ok := a.imported(s, p, file)
			if ok {
				switch p.role {
				case documentRegistry:
					if root := a.admit(s, data, MaxRegistryBytes, "registryFormatVersion", registry.RegistryFormatVersion, registry.Location{Document: registry.DocumentRegistry}); root != nil {
						if digest, ok := a.digestOf(s, root, registry.Location{Document: registry.DocumentRegistry}); ok {
							s.registryRoot, s.registryData, s.registryDigest = root, data, digest
							item.digest = digest
						}
					}
				case documentBounds:
					if root := a.admit(s, data, MaxBoundsBytes, "boundsFormatVersion", BoundsFormatVersion, registry.Location{Document: DocumentBounds}); root != nil {
						if digest, ok := a.digestOf(s, root, registry.Location{Document: DocumentBounds}); ok {
							s.boundsRoot = root
							item.digest = digest
						}
					}
				default:
					raw := rawDigest(data)
					if root := a.admit(s, data, MaxKindBytes, "kindFormatVersion", registry.KindFormatVersion, registry.Location{Document: registry.DocumentKind, KindDocument: raw}); root != nil {
						if digest, ok := a.digestOf(s, root, registry.Location{Document: registry.DocumentKind, KindDocument: raw}); ok {
							s.kinds = append(s.kinds, &kindDocument{source: s.name, data: data, raw: raw, digest: digest, root: root})
							item.digest = digest
						}
					}
				}
			}
			s.documents[p.path] = item
		}
	}
	return !a.collected.hasErrors()
}

// imported returns the bytes a path's document is admitted from: the file's,
// or, for a path ending in .yaml or .yml, its import (registry §9). Format
// 1.0.0's import reads a registry or a kind declaration, which it tells by
// its selector; a bounds document in YAML is refused by it
// (REG-DOC-SELECTOR), never read as another document.
func (a *assembly) imported(s *source, p sourcePath, file registry.Document) ([]byte, bool) {
	if !strings.HasSuffix(p.path, ".yaml") && !strings.HasSuffix(p.path, ".yml") {
		if file.Oversize {
			at := registry.Location{Document: registry.DocumentRegistry}
			switch p.role {
			case documentBounds:
				at.Document = DocumentBounds
			case documentKind:
				at.Document = registry.DocumentKind
			}
			a.collected.addRegistry("REG-DOC-SIZE", fromRegistry(registry.Diagnostic{Location: at}, s.name).Location)
			return nil, false
		}
		return file.Data, true
	}
	canonical, report := registry.Import(file)
	for _, d := range report.Diagnostics {
		a.collected.insert(fromRegistry(d, s.name))
	}
	if !report.Valid {
		a.collected.failed()
		return nil, false
	}
	return canonical, true
}

// admit admits one document of a source, reporting its refusals located in
// that source. A refused kind declaration's size is located without a
// kindDocument: its bytes past the bound are not its digest's.
func (a *assembly) admit(s *source, data []byte, bound int, member, selector string, at registry.Location) map[string]any {
	root, refusals := registry.AdmitDocument(data, bound, member, selector, at)
	for _, d := range refusals {
		if d.Code == "REG-DOC-SIZE" {
			d.Location.KindDocument = ""
		}
		a.collected.insert(fromRegistry(d, s.name))
	}
	return root
}

// digestOf is an admitted document's digest (registry §2.3). A document
// without a canonical form is REG-DOC-NUMBER, as format 1.0.0 reports it.
func (a *assembly) digestOf(s *source, root map[string]any, at registry.Location) (string, bool) {
	digest, err := registry.Digest(root)
	if err != nil {
		a.collected.addRegistry("REG-DOC-NUMBER", fromRegistry(registry.Diagnostic{Location: at}, s.name).Location)
		return "", false
	}
	return digest, true
}

// Step 3: each source's environments, the uniqueness of registry ids, and no
// source an assembled registry the known list names.
func (a *assembly) step3(known Known) bool {
	digests, ids := knownIdentities(known)
	seen := map[string]bool{}
	for _, s := range a.manifest.sources {
		if s.registryRoot == nil {
			continue
		}
		at := func(pointer string) Location {
			return Location{Document: registry.DocumentRegistry, Source: s.name, Pointer: pointer}
		}
		if !sameStrings(s.registryRoot["environments"], s.serves) {
			a.collected.add("FED-ENVIRONMENTS", at("/environments"))
		}
		id, isString := s.registryRoot["id"].(string)
		if isString {
			if id == a.manifest.id || seen[id] {
				a.collected.add("FED-REGISTRY-DUPLICATE", at("/id"))
			}
			seen[id] = true
		}
		if digests[s.registryDigest] || (isString && ids[id]) {
			a.collected.add("FED-SOURCE-ASSEMBLED", at(""))
		}
	}
	return !a.collected.hasErrors()
}

// knownIdentities are the registry digests and ids the known indexes and
// locks name. A known document that is not admitted names nothing: no code
// refuses one, and an assembled registry the list does not name is read as
// a repository's own (§4).
func knownIdentities(known Known) (digests, ids map[string]bool) {
	digests, ids = map[string]bool{}, map[string]bool{}
	read := func(document registry.Document, bound int, member, selector string) {
		if document.Oversize {
			return
		}
		root, _ := registry.AdmitDocument(document.Data, bound, member, selector, registry.Location{Document: DocumentIndex})
		identity, _ := root["registry"].(map[string]any)
		if digest, ok := identity["digest"].(string); ok {
			digests[digest] = true
		}
		if id, ok := identity["id"].(string); ok {
			ids[id] = true
		}
	}
	for _, document := range known.Indexes {
		read(document, MaxIndexBytes, "indexFormatVersion", IndexFormatVersion)
	}
	for _, document := range known.Locks {
		read(document, MaxLockBytes, "lockFormatVersion", LockFormatVersion)
	}
	return digests, ids
}

func (a *assembly) standard() *source {
	for _, s := range a.manifest.sources {
		if s.standard() {
			return s
		}
	}
	return nil
}

// governed maps each kind name a declaration the standard supplies governs
// to the digests of the declarations it supplies for it (§4, step 4).
func (a *assembly) governed() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	if standard := a.standard(); standard != nil {
		for _, k := range standard.kinds {
			if name, ok := k.name(); ok {
				if out[name] == nil {
					out[name] = map[string]bool{}
				}
				out[name][k.digest] = true
			}
		}
	}
	return out
}

// pin is one of a source registry's pins of a kind name and a digest.
type pin struct {
	index        int
	kind, digest string
}

func pinsOf(root map[string]any) []pin {
	items, _ := root["kinds"].([]any)
	var out []pin
	for i, item := range items {
		object, _ := item.(map[string]any)
		kind, kindOK := object["kind"].(string)
		digest, digestOK := object["digest"].(string)
		if kindOK && digestOK {
			out = append(out, pin{index: i, kind: kind, digest: digest})
		}
	}
	return out
}

// Step 4: a governed kind is pinned to a declaration the standard supplies,
// and one kind name to one digest.
func (a *assembly) step4() {
	governed := a.governed()
	first := map[string]string{}
	for _, s := range a.manifest.sources {
		for _, p := range pinsOf(s.registryRoot) {
			at := Location{Document: registry.DocumentRegistry, Source: s.name, Kind: p.kind, Pointer: "/kinds/" + strconv.Itoa(p.index)}
			if digests, ok := governed[p.kind]; ok && !digests[p.digest] {
				a.collected.add("FED-KIND-NOT-STANDARD", at)
			}
			if digest, ok := first[p.kind]; ok && digest != p.digest {
				a.collected.add("FED-KIND-CONFLICT", at)
			}
			if _, ok := first[p.kind]; !ok {
				first[p.kind] = p.digest
			}
		}
	}
}

// declarations are the kind declarations, from whichever source supplies
// them, whose digest is one of digests.
func (a *assembly) declarations(digests map[string]bool) []registry.Document {
	var out []registry.Document
	for _, s := range a.manifest.sources {
		for _, k := range s.kinds {
			if digests[k.digest] {
				out = append(out, registry.Document{Data: k.data})
			}
		}
	}
	return out
}

// kindSource is the source a kind declaration's diagnostic is about: the
// first source, in source order, that supplies the bytes it names.
func (a *assembly) kindSource(raw string) string {
	for _, s := range a.manifest.sources {
		for _, k := range s.kinds {
			if k.raw == raw {
				return s.name
			}
		}
	}
	return ""
}

// insertValidation records the diagnostics of a validation run inside the
// assembly. A validation that lists only its first 1000 diagnostics and is
// not valid fails the assembly, whichever of its errors were left out.
func (a *assembly) insertValidation(report registry.Report, keep func(registry.Diagnostic) (Diagnostic, bool)) {
	truncated := false
	for _, d := range report.Diagnostics {
		if d.Code == "REG-REPORT-TRUNCATED" {
			truncated = true
			a.collected.dropped = true
			continue
		}
		if out, ok := keep(d); ok {
			a.collected.insert(out)
		}
	}
	if truncated && !report.Valid {
		a.collected.failed()
	}
}

func truncated(report registry.Report) bool {
	return slices.ContainsFunc(report.Diagnostics, func(d registry.Diagnostic) bool { return d.Code == "REG-REPORT-TRUNCATED" })
}

// Step 5: each source's registry validated alone, with the declarations its
// own pins name. A reference whose target another source holds is left to
// step 6.
func (a *assembly) step5() {
	holders := map[registry.EntryName][]string{}
	for _, s := range a.manifest.sources {
		items, _ := s.registryRoot["entries"].([]any)
		for _, item := range items {
			object, _ := item.(map[string]any)
			kind, kindOK := object["kind"].(string)
			id, idOK := object["id"].(string)
			if kindOK && idOK {
				name := registry.EntryName{Kind: kind, ID: id}
				holders[name] = append(holders[name], s.name)
			}
		}
	}
	for _, s := range a.manifest.sources {
		if s.registryRoot == nil {
			continue
		}
		digests := map[string]bool{}
		for _, p := range pinsOf(s.registryRoot) {
			digests[p.digest] = true
		}
		s.validation = registry.ValidateDocuments(registry.Document{Data: s.registryData}, a.declarations(digests))
		s.document = newView(s.registryRoot)
		a.insertValidation(s.validation.Report, func(d registry.Diagnostic) (Diagnostic, bool) {
			if d.Location.Document == registry.DocumentKind {
				return fromRegistry(d, a.kindSource(d.Location.KindDocument)), true
			}
			if deferred[d.Code] && d.Location.Document == registry.DocumentValue && d.Location.Entry != nil {
				target, ok := s.document.referenceAt(*d.Location.Entry, d.Location.Environment, d.Location.Pointer)
				if ok && slices.ContainsFunc(holders[target], func(holder string) bool { return holder != s.name }) {
					return Diagnostic{}, false
				}
			}
			return fromRegistry(d, s.name), true
		})
	}
	a.scopeWidenings()
}

// scopeWidenings is step 5's FED-SCOPE-WIDENS: where the standard declares
// secret scopes, a repository's list, for each environment and store, only
// projects the standard lists for them.
func (a *assembly) scopeWidenings() {
	standard := a.standard()
	if standard == nil {
		return
	}
	allowed, ok := standard.registryRoot["secretScopes"].(map[string]any)
	if !ok {
		return
	}
	for _, s := range a.manifest.sources {
		if s.standard() || s.validation == nil || s.validation.Report.Registry == nil {
			continue
		}
		scopes, ok := s.registryRoot["secretScopes"].(map[string]any)
		if !ok {
			continue
		}
		for _, env := range sortedKeys(scopes) {
			stores, _ := scopes[env].(map[string]any)
			standardStores, _ := allowed[env].(map[string]any)
			for _, store := range sortedKeys(stores) {
				projects, _ := stores[store].([]any)
				listed := stringItems(standardStores[store])
				for i, project := range projects {
					if text, ok := project.(string); !ok || !containsString(listed, text) {
						a.collected.add("FED-SCOPE-WIDENS", Location{Document: registry.DocumentRegistry, Source: s.name, Pointer: carrier.Pointer([]string{"secretScopes", env, store, strconv.Itoa(i)})})
					}
				}
			}
		}
	}
}

// Step 6: the assembled registry, validated by registry §7 unchanged, its
// diagnostics located in the source that holds what each is about.
func (a *assembly) step6() error {
	for _, s := range a.manifest.sources {
		if s.registryRoot != nil && (s.validation == nil || s.validation.Report.Registry == nil) {
			// A source whose registry stops at registry §7.1's step 3 has no
			// entries to assemble; its own diagnostics refuse the assembly.
			return nil
		}
	}
	m := a.manifest
	built := &assembled{}
	owners := map[string]bool{}
	pins := map[string]origin{}
	pinValues := map[string]any{}
	type assembledEntry struct {
		kind, id string
		seq      int
		value    map[string]any
		from     origin
	}
	var entries []assembledEntry
	overlays := map[string]*assembledOverlay{}
	var patches = map[string][]any{}
	for _, s := range m.sources {
		if s.registryRoot == nil {
			continue
		}
		for _, owner := range stringItems(s.registryRoot["owners"]) {
			owners[owner] = true
		}
		kindItems, _ := s.registryRoot["kinds"].([]any)
		for _, p := range pinsOf(s.registryRoot) {
			if _, seen := pins[p.kind]; !seen {
				pins[p.kind], pinValues[p.kind] = origin{source: s.name, index: p.index}, kindItems[p.index]
			}
		}
		fewer := !slices.Equal(s.serves, m.environments)
		for _, e := range s.document.entries {
			item := maps.Clone(e.object)
			if fewer && !e.hasEnvs {
				item["environments"] = anyStrings(s.serves)
			}
			entries = append(entries, assembledEntry{kind: e.name.Kind, id: e.name.ID, seq: len(entries), value: item, from: origin{source: s.name, index: e.index}})
		}
		items, _ := s.registryRoot["overlays"].([]any)
		for o, item := range items {
			overlay, _ := item.(map[string]any)
			env, envOK := overlay["environment"].(string)
			list, listOK := overlay["patches"].([]any)
			if !envOK || !listOK {
				continue
			}
			if overlays[env] == nil {
				overlays[env] = &assembledOverlay{environment: env, first: origin{source: s.name, index: o}}
			}
			for p, patch := range list {
				overlays[env].patches = append(overlays[env].patches, patchOrigin{source: s.name, overlay: o, patch: p})
				patches[env] = append(patches[env], patch)
			}
		}
	}
	slices.SortFunc(entries, func(x, y assembledEntry) int {
		return cmp.Or(strings.Compare(x.kind, y.kind), strings.Compare(x.id, y.id), cmp.Compare(x.seq, y.seq))
	})
	root := map[string]any{
		"registryFormatVersion": registry.RegistryFormatVersion,
		"id":                    m.id,
		"version":               m.version,
		"environments":          anyStrings(m.environments),
		"owners":                anyStrings(sortedKeys(owners)),
	}
	kinds := []any{}
	for _, name := range sortedKeys(pins) {
		kinds = append(kinds, pinValues[name])
		built.pins = append(built.pins, pins[name])
	}
	root["kinds"] = kinds
	items := []any{}
	for _, e := range entries {
		items = append(items, e.value)
		built.entries = append(built.entries, e.from)
	}
	root["entries"] = items
	var overlayItems []any
	for _, env := range m.environments {
		if overlay, ok := overlays[env]; ok {
			overlayItems = append(overlayItems, map[string]any{"environment": env, "patches": patches[env]})
			built.overlays = append(built.overlays, *overlay)
		}
	}
	if len(overlayItems) > 0 {
		root["overlays"] = overlayItems
	}
	standard := a.standard()
	if standard != nil && standard.registryRoot != nil {
		if scopes, ok := standard.registryRoot["secretScopes"]; ok {
			root["secretScopes"] = scopes
		}
	}
	data, err := jcs.EncodeDocument(root)
	if err != nil {
		return fmt.Errorf("federation: the assembled registry has no canonical form: %w", err)
	}
	built.root, built.data, built.view = root, data, newView(root)
	digests := map[string]bool{}
	for _, p := range pinsOf(root) {
		digests[p.digest] = true
	}
	built.result = registry.ValidateDocuments(registry.Document{Data: data}, a.declarations(digests))
	a.assembled = built
	standardName := ""
	if standard != nil {
		standardName = standard.name
	}
	a.insertValidation(built.result.Report, func(d registry.Diagnostic) (Diagnostic, bool) {
		return a.locate(d, standardName), true
	})
	a.gaps()
	a.sharedScopes()
	return nil
}

// locate places a diagnostic of the assembled registry in the source that
// holds what it is about (§4, step 6): an entry, a pin or a patch at its
// pointer in that source's registry, an overlay at the first source's that
// patches its environment, and the secret scopes at the standard's. A value's
// diagnostic keeps its pointer into the value, located in the source that
// holds the entry. Anything else is about the assembly as a whole.
func (a *assembly) locate(d registry.Diagnostic, standard string) Diagnostic {
	built := a.assembled
	switch d.Location.Document {
	case registry.DocumentKind:
		return fromRegistry(d, a.kindSource(d.Location.KindDocument))
	case registry.DocumentValue:
		source := ""
		if d.Location.Entry != nil {
			if i, ok := built.view.first[*d.Location.Entry]; ok {
				source = built.entries[i].source
			}
		}
		return fromRegistry(d, source)
	case registry.DocumentRegistry:
	default:
		return fromRegistry(d, "")
	}
	tokens, err := registry.ParsePointer(d.Location.Pointer)
	if err != nil || len(tokens) == 0 {
		return fromRegistry(d, "")
	}
	index := func(at int, length int) (int, bool) {
		if len(tokens) <= at {
			return 0, false
		}
		i, err := strconv.Atoi(tokens[at])
		return i, err == nil && i >= 0 && i < length && strconv.Itoa(i) == tokens[at]
	}
	moved := func(source string, head []string, rest []string) Diagnostic {
		out := fromRegistry(d, source)
		out.Location.Pointer = carrier.Pointer(append(head, rest...))
		return out
	}
	switch tokens[0] {
	case "entries":
		if i, ok := index(1, len(built.entries)); ok {
			from := built.entries[i]
			return moved(from.source, []string{"entries", strconv.Itoa(from.index)}, tokens[2:])
		}
	case "kinds":
		if i, ok := index(1, len(built.pins)); ok {
			from := built.pins[i]
			return moved(from.source, []string{"kinds", strconv.Itoa(from.index)}, tokens[2:])
		}
	case "overlays":
		if o, ok := index(1, len(built.overlays)); ok {
			overlay := built.overlays[o]
			if len(tokens) >= 3 && tokens[2] == "patches" {
				if p, ok := index(3, len(overlay.patches)); ok {
					from := overlay.patches[p]
					return moved(from.source, []string{"overlays", strconv.Itoa(from.overlay), "patches", strconv.Itoa(from.patch)}, tokens[4:])
				}
			}
			return moved(overlay.first.source, []string{"overlays", strconv.Itoa(overlay.first.index)}, tokens[2:])
		}
	case "secretScopes":
		return fromRegistry(d, standard)
	}
	return fromRegistry(d, "")
}

// gaps is step 6's FED-ENVIRONMENT-GAP: beside each
// REF-ENTRY-ABSENT-IN-ENVIRONMENT whose target is held only by a source that
// does not serve the environment, at the reference.
func (a *assembly) gaps() {
	built := a.assembled
	served := map[string][]string{}
	for _, s := range a.manifest.sources {
		served[s.name] = s.serves
	}
	for _, d := range built.result.Report.Diagnostics {
		if d.Code != "REF-ENTRY-ABSENT-IN-ENVIRONMENT" || d.Location.Entry == nil {
			continue
		}
		target, ok := built.view.referenceAt(*d.Location.Entry, d.Location.Environment, d.Location.Pointer)
		if !ok {
			continue
		}
		holder, ok := built.view.first[target]
		if !ok || containsString(served[built.entries[holder].source], d.Location.Environment) {
			continue
		}
		at := fromRegistry(d, built.entries[built.view.first[*d.Location.Entry]].source).Location
		a.collected.add("FED-ENVIRONMENT-GAP", at)
	}
}

// sharedScopes reads the sources' own scopes together, as registry §6.4's
// sharing rule reads one list: a project listed for one environment in a
// source and for another in a later one is REF-SECRET-SCOPE-SHARED at the
// later listing, in source order.
func (a *assembly) sharedScopes() {
	listed := map[string][]string{}
	for _, s := range a.manifest.sources {
		scopes, ok := s.registryRoot["secretScopes"].(map[string]any)
		if !ok {
			continue
		}
		for _, env := range sortedKeys(scopes) {
			stores, _ := scopes[env].(map[string]any)
			for _, store := range sortedKeys(stores) {
				projects, _ := stores[store].([]any)
				for i, project := range projects {
					text, ok := project.(string)
					if !ok {
						continue
					}
					key := store + "\x00" + text
					if slices.ContainsFunc(listed[key], func(other string) bool { return other != env }) {
						a.collected.addRegistry("REF-SECRET-SCOPE-SHARED", Location{Document: registry.DocumentRegistry, Source: s.name, Pointer: carrier.Pointer([]string{"secretScopes", env, store, strconv.Itoa(i)})})
					}
					if !containsString(listed[key], env) {
						listed[key] = append(listed[key], env)
					}
				}
			}
		}
	}
}

// Step 8: the index of an assembly with no error (§5).
func (a *assembly) step8() error {
	m := a.manifest
	federation, err := registry.Digest(m.root)
	if err != nil {
		return fmt.Errorf("federation: the manifest has no canonical form: %w", err)
	}
	sources := make([]any, 0, len(m.sources))
	for _, s := range m.sources {
		documents := make([]any, 0, len(s.documents))
		for _, path := range sortedKeys(s.documents) {
			d := s.documents[path]
			documents = append(documents, map[string]any{"path": d.path, "role": d.role, "contentDigest": d.contentDigest, "digest": d.digest})
		}
		sources = append(sources, map[string]any{
			"name": s.name, "role": s.role, "repository": s.repository, "commit": s.commit, "documents": documents,
		})
	}
	a.index = map[string]any{
		"indexFormatVersion": IndexFormatVersion,
		"federation":         map[string]any{"digest": federation},
		"registry":           map[string]any{"id": m.id, "version": m.version, "digest": a.digest()},
		"sources":            sources,
	}
	return nil
}

func rawDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// sameStrings reports whether a document's value is the array of exactly
// these strings, in this order.
func sameStrings(value any, list []string) bool {
	items, ok := value.([]any)
	if !ok || len(items) != len(list) {
		return false
	}
	for i, item := range items {
		if text, ok := item.(string); !ok || text != list[i] {
			return false
		}
	}
	return true
}
