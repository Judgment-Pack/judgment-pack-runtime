package registry

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

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
	scopes       map[string]map[string][]string
}

type resolvedEntry struct {
	entry *entry
	value any
}

// Validate runs RFC 0034 §6's eight checks, in order, on a registry and the
// kind declarations supplied with it.
func Validate(registryBytes []byte, kindDocuments [][]byte) *Result {
	diagnostics := &collector{}
	result := &Result{resolved: map[string][]resolvedEntry{}}
	finish := func() *Result {
		result.Report = Report{
			ReportFormatVersion: ReportFormatVersion,
			Valid:               diagnostics.errors() == 0,
			Diagnostics:         diagnostics.sorted(),
		}
		if result.registry != nil {
			identity := result.registry.identity
			result.Report.Registry = &identity
		}
		if !result.Report.Valid {
			result.resolved = nil
		}
		return result
	}

	// Step 1: admission, of the registry and of each kind declaration.
	registryLocation := Location{Document: DocumentRegistry}
	root := admit(registryBytes, MaxRegistryBytes, "registryFormatVersion", RegistryFormatVersion, registryLocation, diagnostics)
	type suppliedKind struct {
		location Location
		root     map[string]any
		digest   string
	}
	var supplied []suppliedKind
	for _, document := range kindDocuments {
		location := Location{Document: DocumentKind, KindDocument: rawDigest(document)}
		kindRoot := admit(document, MaxKindBytes, "kindFormatVersion", KindFormatVersion, location, diagnostics)
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
	slices.SortFunc(supplied, func(a, b suppliedKind) int { return cmp.Compare(a.digest, b.digest) })
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
			name := e.name
			return Location{Document: DocumentRegistry, Entry: &name, Pointer: pointerOf(append([]string{"entries", strconv.Itoa(e.index)}, member...))}
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
			diagnostics.add("REG-ENTRY-ID", at("id"))
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
	for _, environment := range registry.environments {
		var resolved []resolvedEntry
		values := map[EntryName]any{}
		for _, e := range registry.entries {
			if !e.passed || !e.environment[environment] {
				continue
			}
			value := deepCopy(e.value)
			if p, ok := registry.overlays[environment][e.name]; ok {
				for _, operation := range p.set {
					value = setAt(value, operation.path, deepCopy(operation.value))
				}
			}
			values[e.name] = value
			resolved = append(resolved, resolvedEntry{entry: e, value: value})
		}
		for _, r := range resolved {
			checkResolvedEntry(registry, environment, r, values, diagnostics)
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
			canonical, err := jcs.EncodeDocument(r.value)
			if err != nil || len(canonical) > r.entry.kind.maxBytes {
				name := r.entry.name
				diagnostics.add("REG-BOUND-ENTRY-BYTES", Location{Document: DocumentValue, Entry: &name, Environment: environment, Pointer: ""})
			}
		}
	}
	return finish()
}

func checkResolvedEntry(registry *registryDoc, environment string, r resolvedEntry, values map[EntryName]any, diagnostics *collector) {
	name := r.entry.name
	at := func(path []string) Location {
		return Location{Document: DocumentValue, Entry: &name, Environment: environment, Pointer: pointerOf(path)}
	}
	var findings []valueFinding
	r.entry.kind.schema.validate(r.value, nil, &findings)
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
			case !slices.Contains(registry.scopes[environment][reference.store], reference.project):
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
	kinds := make([]any, 0, len(r.registry.pins))
	for _, p := range r.registry.pins {
		kinds = append(kinds, map[string]any{"kind": p.kind, "id": p.id, "version": p.version, "digest": p.digest})
	}
	entries := make([]any, 0, len(r.resolved[environment]))
	for _, resolved := range r.resolved[environment] {
		entries = append(entries, map[string]any{
			"kind": resolved.entry.name.Kind, "id": resolved.entry.name.ID, "owner": resolved.entry.owner, "value": resolved.value,
		})
	}
	document := map[string]any{
		"resolvedFormatVersion": ResolvedFormatVersion,
		"registry":              map[string]any{"id": r.registry.identity.ID, "version": r.registry.identity.Version, "digest": r.registry.identity.Digest},
		"environment":           environment,
		"kinds":                 kinds,
		"entries":               entries,
	}
	canonical, err := jcs.EncodeDocument(document)
	if err != nil {
		return nil, err
	}
	if len(canonical) > MaxResolvedBytes {
		return nil, fmt.Errorf("registry: the resolved registry for %q exceeds %d bytes", environment, MaxResolvedBytes)
	}
	return canonical, nil
}

// Environments returns the registry's environments in order, or nil when the
// registry's shape could not be read.
func (r *Result) Environments() []string {
	if r.registry == nil {
		return nil
	}
	return slices.Clone(r.registry.environments)
}

func deepCopy(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for name, item := range typed {
			out[name] = deepCopy(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = deepCopy(item)
		}
		return out
	}
	return value
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

// TooLarge is the result for a document a caller stopped reading at its size
// bound (RFC 0034 §1): one REG-DOC-SIZE, located in the registry or in a kind
// declaration, and nothing resolvable.
func TooLarge(document string) *Result {
	diagnostics := &collector{}
	diagnostics.add("REG-DOC-SIZE", Location{Document: document})
	return &Result{Report: Report{ReportFormatVersion: ReportFormatVersion, Valid: false, Diagnostics: diagnostics.sorted()}}
}
