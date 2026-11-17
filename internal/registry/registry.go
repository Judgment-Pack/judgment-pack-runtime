package registry

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"unsafe"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// maxResolvedBytes is MaxResolvedBytes; a test lowers it.
var maxResolvedBytes = MaxResolvedBytes

// Result is the outcome of validating one registry with its kinds.
type Result struct {
	Report   Report
	registry *registryDoc
	resolved map[string][]resolvedEntry
}

type pin struct {
	kind, id, version, digest string
	index                     int
	usable                    *Kind
}

type entry struct {
	name        EntryName
	owner       string
	envs        []string
	value       any
	index       int
	kind        *Kind
	passed      bool
	environment map[string]bool
}

type patch struct {
	entry *entry
	set   []setOperation
}

type setOperation struct {
	path  []string
	value any
}

type registryDoc struct {
	root         map[string]any
	identity     Identity
	environments []string
	owners       map[string]bool
	pins         []*pin
	entries      []*entry
	byName       map[EntryName]*entry
	overlays     map[string]map[EntryName]*patch
	failed       map[string]map[EntryName]bool
	scopes       map[string]map[string]map[string]bool
}

type resolvedEntry struct {
	entry *entry
	// value is read-only. Where no overlay applies it is the entry's own
	// value, shared by every environment; an overlay copies only the
	// containers on its pointers' paths.
	value any
	// measure is the value's canonical form measured once, shared with every
	// environment that holds the same value.
	measure *valueMeasure
}

// valueMeasure is what step 7 and step 8 need of a value's canonical form:
// its length, its nesting and its digest. A value with no canonical form has
// encoded false.
type valueMeasure struct {
	encoded bool
	size    int
	nesting int
	digest  string
}

// measureValue encodes value once and measures it.
func measureValue(value any) *valueMeasure {
	canonical, err := jcs.EncodeDocument(value)
	if err != nil {
		return &valueMeasure{}
	}
	sum := sha256.Sum256(canonical)
	return &valueMeasure{encoded: true, size: len(canonical), nesting: nesting(value), digest: hex.EncodeToString(sum[:])}
}

// nesting is the number of containers nested at a value's deepest point, as
// carrier admission counts depth: 0 for a scalar, 1 for an empty object.
func nesting(value any) int {
	deepest := 0
	switch typed := value.(type) {
	case map[string]any:
		for _, item := range typed {
			deepest = max(deepest, nesting(item))
		}
	case []any:
		for _, item := range typed {
			deepest = max(deepest, nesting(item))
		}
	default:
		return 0
	}
	return deepest + 1
}

// Validate runs RFC 0034 §6's eight checks, in order, on a registry and the
// kind declarations supplied with it.
// Document is one input document. Oversize marks a document its reader
// stopped reading at the size bound: it is reported as REG-DOC-SIZE, and
// validation goes on with the other documents.
type Document struct {
	Data     []byte
	Oversize bool
}

// Validate validates a registry with the kind declarations it pins.
func Validate(registryBytes []byte, kindDocuments [][]byte) *Result {
	kinds := make([]Document, len(kindDocuments))
	for i, data := range kindDocuments {
		kinds[i] = Document{Data: data}
	}
	return ValidateDocuments(Document{Data: registryBytes}, kinds)
}

// ValidateDocuments is Validate over documents that may be marked oversize.
func ValidateDocuments(registryDocument Document, kindDocuments []Document) *Result {
	diagnostics := &collector{limit: maxReportDiagnostics}
	result := &Result{resolved: map[string][]resolvedEntry{}}
	finish := func() *Result {
		result.Report = Report{
			ReportFormatVersion: ReportFormatVersion,
			Valid:               diagnostics.errors() == 0,
			Diagnostics:         diagnostics.sorted(),
		}
		if result.registry != nil {
			result.Report.Registry = &Digested{Digest: result.registry.identity.Digest}
		}
		if !result.Report.Valid {
			result.resolved = nil
		}
		return result
	}

	// Step 1: admission, of the registry and of each kind declaration.
	registryLocation := Location{Document: DocumentRegistry}
	var root map[string]any
	if registryDocument.Oversize {
		diagnostics.add("REG-DOC-SIZE", registryLocation)
	} else {
		root = admit(registryDocument.Data, MaxRegistryBytes, "registryFormatVersion", RegistryFormatVersion, registryLocation, diagnostics)
	}
	type suppliedKind struct {
		location Location
		root     map[string]any
		digest   string
	}
	var supplied []suppliedKind
	for _, document := range kindDocuments {
		if document.Oversize {
			// Its bytes past the bound were not read, so it has no digest.
			diagnostics.add("REG-DOC-SIZE", Location{Document: DocumentKind})
			continue
		}
		location := Location{Document: DocumentKind, KindDocument: rawDigest(document.Data)}
		kindRoot := admit(document.Data, MaxKindBytes, "kindFormatVersion", KindFormatVersion, location, diagnostics)
		if kindRoot == nil {
			continue
		}
		digest, err := Digest(kindRoot)
		if err != nil {
			diagnostics.add("REG-DOC-NUMBER", location)
			continue
		}
		supplied = append(supplied, suppliedKind{location: location, root: kindRoot, digest: digest})
	}

	// Step 2: kind declarations.
	kindsByDigest := map[string]*Kind{}
	suppliedDigests := map[string]bool{}
	// Two supplied documents of one canonical digest are one kind, read once.
	// Ties are broken by the digest of each one's bytes, the kindDocument its
	// diagnostics are located at, so the one read does not depend on the
	// order the kinds were supplied in.
	slices.SortFunc(supplied, func(a, b suppliedKind) int {
		return cmp.Or(cmp.Compare(a.digest, b.digest), cmp.Compare(a.location.KindDocument, b.location.KindDocument))
	})
	for _, document := range supplied {
		if suppliedDigests[document.digest] {
			continue
		}
		suppliedDigests[document.digest] = true
		if kind := readKind(document.root, document.location, diagnostics); kind != nil {
			kind.Digest = document.digest
			kindsByDigest[document.digest] = kind
		}
	}
	if root == nil {
		return finish()
	}

	// Step 3: the registry's shape.
	registry := readRegistry(root, diagnostics)
	if registry == nil {
		return finish()
	}
	digest, err := Digest(root)
	if err != nil {
		diagnostics.add("REG-DOC-NUMBER", registryLocation)
		return finish()
	}
	registry.identity.Digest = digest
	result.registry = registry

	// Step 4: kind pins.
	pinnedDigests := map[string]bool{}
	seenKinds := map[string]bool{}
	for _, p := range registry.pins {
		at := Location{Document: DocumentRegistry, Kind: p.kind, Pointer: pointerOf([]string{"kinds", strconv.Itoa(p.index)})}
		pinnedDigests[p.digest] = true
		if seenKinds[p.kind] {
			diagnostics.add("REG-REGISTRY-DUPLICATE", at)
			continue
		}
		seenKinds[p.kind] = true
		kind, valid := kindsByDigest[p.digest]
		switch {
		case !suppliedDigests[p.digest]:
			diagnostics.add("REG-PIN-MISSING", at)
		case !valid:
			// Its own diagnostics already say why it cannot be used.
		case kind.Name != p.kind || kind.ID != p.id || kind.Version != p.version:
			diagnostics.add("REG-PIN-MISMATCH", at)
		default:
			p.usable = kind
			if kind.Deprecated {
				diagnostics.add("REG-KIND-DEPRECATED", at)
			}
		}
	}
	for _, document := range supplied {
		if kind, ok := kindsByDigest[document.digest]; ok && !pinnedDigests[document.digest] {
			at := document.location
			at.Kind = kind.Name
			diagnostics.add("REG-PIN-UNPINNED", at)
		}
	}
	pinsByKind := map[string]*pin{}
	for _, p := range registry.pins {
		if _, ok := pinsByKind[p.kind]; !ok {
			pinsByKind[p.kind] = p
		}
	}

	// Step 5: entries.
	seenEntries := map[EntryName]bool{}
	for _, e := range registry.entries {
		at := func(member ...string) Location {
			return Location{Document: DocumentRegistry, Entry: locatable(e.name), Pointer: pointerOf(append([]string{"entries", strconv.Itoa(e.index)}, member...))}
		}
		p, pinned := pinsByKind[e.name.Kind]
		if !pinned {
			diagnostics.add("REG-ENTRY-KIND", at("kind"))
			continue
		}
		if p.usable == nil {
			continue
		}
		e.kind = p.usable
		ok := true
		if !validEntryID(e.name.ID) || !e.kind.entryID.MatchString(e.name.ID) {
			// The id is outside its kind's pattern, so it is not named.
			location := at("id")
			location.Entry = nil
			diagnostics.add("REG-ENTRY-ID", location)
			ok = false
		}
		if seenEntries[e.name] {
			diagnostics.add("REG-ENTRY-DUPLICATE", at("id"))
			ok = false
		}
		seenEntries[e.name] = true
		if !registry.owners[e.owner] {
			diagnostics.add("REG-ENTRY-OWNER", at("owner"))
			ok = false
		}
		e.environment = map[string]bool{}
		if e.envs == nil {
			for _, environment := range registry.environments {
				e.environment[environment] = true
			}
		} else {
			for i, environment := range e.envs {
				if !slices.Contains(registry.environments, environment) {
					diagnostics.add("REG-ENTRY-ENVIRONMENT", at("environments", strconv.Itoa(i)))
					ok = false
				}
				e.environment[environment] = true
			}
		}
		e.passed = ok
	}

	// Step 6: overlays.
	checkOverlays(registry, diagnostics)

	// Step 7: per environment, on resolved values.
	schemas := &schemaChecks{validation: validation{work: maxValidationWork}, seen: map[string][]valueFinding{}}
	// An entry's own value, measured once, for the environments no overlay
	// changes it in: resolution then costs the registry, not the registry
	// times its environments.
	shared := map[*entry]*valueMeasure{}
	for _, environment := range registry.environments {
		var resolved []resolvedEntry
		values := map[EntryName]any{}
		for _, e := range registry.entries {
			if !e.passed || !e.environment[environment] || registry.failed[environment][e.name] {
				continue
			}
			value := e.value
			var measure *valueMeasure
			if p, ok := registry.overlays[environment][e.name]; ok && len(p.set) > 0 {
				value = overlaid(value, p.set)
				measure = measureValue(value)
			} else {
				if shared[e] == nil {
					shared[e] = measureValue(value)
				}
				measure = shared[e]
			}
			values[e.name] = value
			resolved = append(resolved, resolvedEntry{entry: e, value: value, measure: measure})
		}
		for _, r := range resolved {
			checkResolvedEntry(registry, environment, r, values, schemas, diagnostics)
		}
		result.resolved[environment] = resolved
	}

	// Step 8: bounds.
	counts := map[string]int{}
	for _, e := range registry.entries {
		counts[e.name.Kind]++
	}
	for _, p := range registry.pins {
		if p.usable != nil && counts[p.kind] > p.usable.maxEntries {
			diagnostics.add("REG-BOUND-ENTRIES", Location{Document: DocumentRegistry, Kind: p.kind, Pointer: pointerOf([]string{"kinds", strconv.Itoa(p.index)})})
		}
	}
	for _, environment := range registry.environments {
		for _, r := range result.resolved[environment] {
			if !r.measure.encoded || r.measure.size > r.entry.kind.maxBytes {
				name := r.entry.name
				diagnostics.add("REG-BOUND-ENTRY-BYTES", Location{Document: DocumentValue, Entry: &name, Environment: environment, Pointer: ""})
			}
		}
	}
	// RFC 0034 §1: each environment's resolved document is held to its
	// bounds, the size and the depth. An overlay can set a value at a deep
	// pointer, so a registry within the depth bound can resolve past it.
	for _, environment := range registry.environments {
		at := Location{Document: DocumentRegistry, Environment: environment, Pointer: ""}
		size, depth, ok := result.measureResolved(environment)
		switch {
		case !ok || size > maxResolvedBytes:
			diagnostics.add("REG-DOC-SIZE", at)
		case depth > MaxDepth:
			diagnostics.add("REG-DOC-DEPTH", at)
		}
	}
	return finish()
}

// measureResolved returns the length and nesting of one environment's
// resolved document without encoding its values again. The document is
// encoded with every value as null, and each value's own length takes the
// place of null's four bytes: RFC 8785 writes a container as its members'
// forms joined, so the sum is exactly the document's length. A value sits
// inside the root object, the entries array and its entry, so the document
// nests three more than its deepest value, and three when no value nests.
// Depth is counted as carrier admission counts it, the root object as one,
// and 32 are admitted, so what resolve writes is what a reader admits.
func (r *Result) measureResolved(environment string) (size, depth int, ok bool) {
	skeleton, err := r.encodeDocument(environment, func(resolvedEntry) any { return nil })
	if err != nil {
		return 0, 0, false
	}
	size, depth = len(skeleton), 3
	for _, resolved := range r.resolved[environment] {
		if !resolved.measure.encoded {
			return 0, 0, false
		}
		size += resolved.measure.size - len("null")
		depth = max(depth, 3+resolved.measure.nesting)
	}
	return size, depth, true
}

// overlaid returns value with each set operation applied. It copies only
// the containers on each operation's path, once each, and shares everything
// else with value, which it never changes. Step 6 checked that each path's
// parent exists.
func overlaid(value any, set []setOperation) any {
	owned := map[unsafe.Pointer]bool{}
	for _, operation := range set {
		value = setCopying(value, operation.path, operation.value, owned)
	}
	return value
}

func setCopying(node any, path []string, value any, owned map[unsafe.Pointer]bool) any {
	if len(path) == 0 {
		return value
	}
	switch typed := node.(type) {
	case map[string]any:
		if !owned[reflect.ValueOf(typed).UnsafePointer()] {
			typed = maps.Clone(typed)
			owned[reflect.ValueOf(typed).UnsafePointer()] = true
		}
		if len(path) == 1 {
			typed[path[0]] = value
		} else {
			typed[path[0]] = setCopying(typed[path[0]], path[1:], value, owned)
		}
		return typed
	case []any:
		index, ok := arrayIndex(path[0], len(typed))
		if !ok {
			return typed
		}
		if !owned[unsafe.Pointer(unsafe.SliceData(typed))] {
			typed = slices.Clone(typed)
			owned[unsafe.Pointer(unsafe.SliceData(typed))] = true
		}
		typed[index] = setCopying(typed[index], path[1:], value, owned)
		return typed
	}
	return node
}

// schemaChecks holds step 7's schema checks across every environment: the
// work bound they share, and the findings of each value already checked.
type schemaChecks struct {
	validation validation
	// seen maps a kind's digest and a value's canonical digest to the
	// value's findings, which hold no location beyond a path in the value.
	seen map[string][]valueFinding
}

// check returns the findings of value against kind's schema. The same value
// of the same kind, in another environment or another entry, is checked once.
// It reports false when the work bound is spent: the value, and every value
// after it, is then not checked against its schema.
func (s *schemaChecks) check(kind *Kind, value any, measure *valueMeasure) ([]valueFinding, bool) {
	if s.validation.exceeded {
		return nil, false
	}
	key := ""
	if measure.encoded {
		// The key costs the value's bytes, wherever it was measured, so it
		// is charged like the work it saves.
		if !s.validation.charge(1 + measure.size/16) {
			return nil, false
		}
		key = kind.Digest + "\x00" + measure.digest
		if findings, ok := s.seen[key]; ok {
			return findings, true
		}
	}
	var findings []valueFinding
	kind.schema.validate(&s.validation, value, nil, &findings)
	if s.validation.exceeded {
		return nil, false
	}
	if key != "" {
		s.seen[key] = findings
	}
	return findings, true
}

func checkResolvedEntry(registry *registryDoc, environment string, r resolvedEntry, values map[EntryName]any, schemas *schemaChecks, diagnostics *collector) {
	name := r.entry.name
	at := func(path []string) Location {
		return Location{Document: DocumentValue, Entry: &name, Environment: environment, Pointer: pointerOf(path)}
	}
	alreadySpent := schemas.validation.exceeded
	findings, checked := schemas.check(r.entry.kind, r.value, r.measure)
	if !checked && !alreadySpent {
		// Reported once, at the value where the bound ran out.
		diagnostics.add("REG-BOUND-VALIDATION-WORK", at(nil))
	}
	for _, finding := range findings {
		diagnostics.add(finding.code, at(finding.path))
	}
	for _, secret := range r.entry.kind.secrets {
		for _, place := range secret.positions(r.value) {
			reference, code := parseSecretReference(place.value)
			switch {
			case code != "":
				diagnostics.add(code, at(place.path))
			case registry.scopes == nil:
				diagnostics.add("REF-SECRET-UNSCOPED", at(place.path))
			case !registry.scopes[environment][reference.store][reference.project]:
				diagnostics.add("REF-SECRET-SCOPE", at(place.path))
			}
		}
	}
	for _, pattern := range r.entry.kind.references {
		for _, place := range pattern.pattern.positions(r.value) {
			text, isString := place.value.(string)
			reference, ok := parseEntryReference(text)
			target := registry.byName[EntryName{Kind: reference.kind, ID: reference.id}]
			switch {
			case !isString || !ok:
				diagnostics.add("REF-ENTRY-FORM", at(place.path))
			case !slices.Contains(pattern.kinds, reference.kind):
				diagnostics.add("REF-ENTRY-KIND", at(place.path))
			case target == nil:
				diagnostics.add("REF-ENTRY-MISSING", at(place.path))
			case !target.applies(environment):
				diagnostics.add("REF-ENTRY-ABSENT-IN-ENVIRONMENT", at(place.path))
			case reference.hasPointer:
				if value, resolved := values[target.name]; resolved {
					if _, found := lookup(value, reference.pointer); !found {
						diagnostics.add("REF-ENTRY-POINTER", at(place.path))
					}
				}
			}
		}
	}
}

// ErrInvalid is returned by Resolve for a registry with an error.
var ErrInvalid = errors.New("registry: the registry is not valid")

// Resolve returns the canonical bytes of the resolved registry for one
// environment (RFC 0034 §5). It requires a valid registry.
func (r *Result) Resolve(environment string) ([]byte, error) {
	if !r.Report.Valid || r.registry == nil {
		return nil, ErrInvalid
	}
	if !slices.Contains(r.registry.environments, environment) {
		return nil, fmt.Errorf("registry: no environment %q", environment)
	}
	return r.encodeResolved(environment)
}

// encodeResolved builds one environment's resolved document. Step 8 checks
// its size and depth, so Resolve never meets one over either bound.
func (r *Result) encodeResolved(environment string) ([]byte, error) {
	return r.encodeDocument(environment, func(resolved resolvedEntry) any { return resolved.value })
}

// encodeDocument encodes one environment's resolved document with each
// entry's value given by value.
func (r *Result) encodeDocument(environment string, value func(resolvedEntry) any) ([]byte, error) {
	canonical, err := jcs.EncodeDocument(r.documentWith(environment, nil, value))
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

// resolvedDocument is one environment's resolved document as a value, with
// the pins and entries of the kinds keep admits (every kind when keep is
// nil). Its values are the registry's own, not copies.
func (r *Result) resolvedDocument(environment string, keep func(kind string) bool) map[string]any {
	return r.documentWith(environment, keep, func(resolved resolvedEntry) any { return resolved.value })
}

// documentWith builds one environment's resolved document, keeping the
// kinds keep admits (every kind when keep is nil), with each entry's value
// given by value.
func (r *Result) documentWith(environment string, keep func(kind string) bool, value func(resolvedEntry) any) map[string]any {
	kinds := make([]any, 0, len(r.registry.pins))
	for _, p := range r.registry.pins {
		if keep != nil && !keep(p.kind) {
			continue
		}
		kinds = append(kinds, map[string]any{"kind": p.kind, "id": p.id, "version": p.version, "digest": p.digest})
	}
	entries := make([]any, 0, len(r.resolved[environment]))
	for _, resolved := range r.resolved[environment] {
		if keep != nil && !keep(resolved.entry.name.Kind) {
			continue
		}
		entries = append(entries, map[string]any{
			"kind": resolved.entry.name.Kind, "id": resolved.entry.name.ID, "owner": resolved.entry.owner, "value": value(resolved),
		})
	}
	return map[string]any{
		"resolvedFormatVersion": ResolvedFormatVersion,
		"registry":              map[string]any{"id": r.registry.identity.ID, "version": r.registry.identity.Version, "digest": r.registry.identity.Digest},
		"environment":           environment,
		"kinds":                 kinds,
		"entries":               entries,
	}
}

// Environments returns the registry's environments in order, or nil when the
// registry's shape could not be read.
func (r *Result) Environments() []string {
	if r.registry == nil {
		return nil
	}
	return slices.Clone(r.registry.environments)
}

// setAt replaces the value at path, whose parent exists (step 6 checked it).
func setAt(root any, path []string, value any) any {
	if len(path) == 0 {
		return value
	}
	parent, _ := lookup(root, path[:len(path)-1])
	last := path[len(path)-1]
	switch typed := parent.(type) {
	case map[string]any:
		typed[last] = value
	case []any:
		if index, ok := arrayIndex(last, len(typed)); ok {
			typed[index] = value
		}
	}
	return root
}

// locatable returns the name for a location, or nil when the kind or the id
// is not well formed: a report never carries a value, and a malformed id
// could be one (a secret reference written in the wrong member).
func locatable(name EntryName) *EntryName {
	if !kindNamePattern.MatchString(name.Kind) || !validEntryID(name.ID) {
		return nil
	}
	return &name
}
