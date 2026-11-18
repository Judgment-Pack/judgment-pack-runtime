package federation

import (
	"strconv"
	"strings"

	"golang.org/x/text/cases"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// The bounds of §2's documents, each admitted by registry §2.2 under its own.
const (
	MaxManifestBytes = 256 * 1024
	MaxIndexBytes    = 8 * 1024 * 1024
	MaxBoundsBytes   = 256 * 1024
	MaxLockBytes     = 1024 * 1024
	MaxRegistryBytes = registry.MaxRegistryBytes
	MaxKindBytes     = registry.MaxKindBytes
)

// The selectors of §2's documents.
const (
	FederationFormatVersion = "1.1.0"
	IndexFormatVersion      = "1.1.0"
	BoundsFormatVersion     = "1.1.0"
	LockFormatVersion       = "1.1.0"
)

// The count bounds of §§3–4 and registry §4.
const (
	MaxSources       = 256
	MaxPaths         = 4096
	MaxSourceKinds   = 64
	MaxKnown         = 256
	MaxEnvironments  = 32
	MaxPathBytes     = 512
	MaxRepositoryURI = 2048
)

// The roles of §3. A later version may add a layer role; this reader refuses
// it rather than read the manifest as a flat assembly.
const (
	RoleStandard   = "standard"
	RoleRepository = "repository"
)

// The roles of a document a source supplies, as the index records them (§5).
const (
	documentRegistry = "registry"
	documentBounds   = "bounds"
	documentKind     = "kind"
)

var manifestMembers = []string{"federationFormatVersion", "registry", "environments", "sources"}

var sourceMembers = map[string]bool{
	"name": true, "role": true, "repository": true, "environments": true, "registry": true, "bounds": true, "kinds": true,
}

// sourcePath is one path a source item names: the manifest pointer of the
// member naming it, the path, and the role of its document.
type sourcePath struct {
	at, path, role string
}

// source is one item of the manifest's sources, and what an assembly reads
// of it.
type source struct {
	index         int
	name          string
	role          string
	repository    any
	hasRepository bool
	serves        []string
	paths         []sourcePath

	commit    string
	files     map[string]registry.Document
	documents map[string]indexDocument

	registryRoot   map[string]any
	registryData   []byte
	registryDigest string
	boundsRoot     map[string]any
	kinds          []*kindDocument

	validation *registry.Result
	document   *view
}

func (s *source) standard() bool { return s.role == RoleStandard }

func (s *source) at(member ...string) string {
	return carrier.Pointer(append([]string{"sources", strconv.Itoa(s.index)}, member...))
}

// kindDocument is one admitted kind declaration a source supplies.
type kindDocument struct {
	source string
	data   []byte // the bytes validation reads: the file's, or a YAML file's import
	raw    string // the digest of data, the kindDocument of its diagnostics
	digest string // its canonical digest, which pins name
	root   map[string]any
}

// name is the declaration's kind member, which it governs when the standard
// supplies it.
func (k *kindDocument) name() (string, bool) {
	name, ok := k.root["kind"].(string)
	return name, ok
}

// indexDocument is one item of an index source's documents (§5).
type indexDocument struct {
	path, role, contentDigest, digest string
}

// manifest is an admitted federation manifest: its root, and its sources
// read for their form. Its id, version and environments are those of a
// manifest of its form.
type manifest struct {
	root         map[string]any
	id, version  string
	environments []string
	sources      []*source
}

// readManifest runs §4 step 1's admission and §3's form. It returns nil when
// the manifest is not admitted; a manifest that is admitted and not of its
// form is returned with its FED-MANIFEST diagnostics.
func readManifest(data []byte, collected *collector) *manifest {
	root, refusals := registry.AdmitDocument(data, MaxManifestBytes, "federationFormatVersion", FederationFormatVersion, registry.Location{Document: DocumentFederation})
	for _, d := range refusals {
		collected.insert(fromRegistry(d, ""))
	}
	if root == nil {
		return nil
	}
	bad := func(pointer string) {
		collected.add("FED-MANIFEST", Location{Document: DocumentFederation, Pointer: pointer})
	}
	m := &manifest{root: root}
	for name := range root {
		if !containsString(manifestMembers, name) {
			bad(carrier.Pointer([]string{name}))
		}
	}
	for _, name := range manifestMembers {
		if _, ok := root[name]; !ok {
			bad("/" + name)
		}
	}
	if value, ok := root["registry"]; ok {
		identity, isObject := value.(map[string]any)
		if !isObject {
			bad("/registry")
		} else {
			for name := range identity {
				if name != "id" && name != "version" {
					bad(carrier.Pointer([]string{"registry", name}))
				}
			}
			id, _ := identity["id"].(string)
			if !registry.IsAbsoluteURI(id) {
				bad("/registry/id")
			}
			version, _ := identity["version"].(string)
			if !registry.ValidVersion(version) {
				bad("/registry/version")
			}
			m.id, m.version = id, version
		}
	}
	if value, ok := root["environments"]; ok {
		items, isList := value.([]any)
		if !isList || len(items) == 0 || len(items) > MaxEnvironments {
			bad("/environments")
		} else {
			for i, item := range items {
				name, isString := item.(string)
				if !isString || !registry.ValidEnvironment(name) || containsString(m.environments, name) {
					bad("/environments/" + strconv.Itoa(i))
				}
				if isString {
					m.environments = append(m.environments, name)
				}
			}
		}
	}
	value, ok := root["sources"]
	if !ok {
		return m
	}
	items, isList := value.([]any)
	if !isList || len(items) < 1 || len(items) > MaxSources {
		bad("/sources")
	}
	standards, paths := 0, 0
	var names []string
	for i, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			bad("/sources/" + strconv.Itoa(i))
			continue
		}
		s := readSource(i, object, m.environments, names, &standards, bad)
		if _, isString := object["name"].(string); isString {
			names = append(names, s.name)
		}
		m.sources = append(m.sources, s)
		paths += len(s.paths)
	}
	if len(items) > 0 && standards == 0 {
		bad("/sources")
	}
	if paths > MaxPaths {
		bad("/sources")
	}
	return m
}

// readSource reads one source item's form (§3).
func readSource(index int, item map[string]any, environments, names []string, standards *int, bad func(string)) *source {
	s := &source{index: index}
	for name := range item {
		if !sourceMembers[name] {
			bad(s.at(name))
		}
	}
	for _, name := range []string{"name", "role", "repository"} {
		if _, ok := item[name]; !ok {
			bad(s.at(name))
		}
	}
	if value, ok := item["name"]; ok {
		name, isString := value.(string)
		switch {
		case !isString || !registry.ValidKindName(name) || containsString(names, name):
			bad(s.at("name"))
		case len(names) > 0 && name < names[len(names)-1]:
			bad(s.at("name"))
		}
		s.name = name
	}
	if value, ok := item["role"]; ok {
		role, _ := value.(string)
		switch role {
		case RoleStandard:
			*standards++
			if *standards > 1 {
				bad(s.at("role"))
			}
		case RoleRepository:
		default:
			bad(s.at("role"))
		}
		s.role = role
	}
	s.repository, s.hasRepository = item["repository"]
	s.serves = environments
	if value, ok := item["environments"]; ok {
		served, valid := servedEnvironments(value, environments)
		if s.role != RoleRepository || !valid {
			bad(s.at("environments"))
		} else {
			s.serves = served
		}
	}
	if _, ok := item["registry"]; s.role == RoleRepository && !ok {
		bad(s.at("registry"))
	}
	for _, member := range []string{documentRegistry, documentBounds} {
		if value, ok := item[member]; ok {
			path, isString := value.(string)
			if !isString || !validPath(path) {
				bad(s.at(member))
			} else {
				s.paths = append(s.paths, sourcePath{at: s.at(member), path: path, role: member})
			}
		}
	}
	if value, ok := item["kinds"]; ok {
		kinds, isList := value.([]any)
		if !isList || len(kinds) > MaxSourceKinds {
			bad(s.at("kinds"))
		} else {
			var seen []string
			for k, entry := range kinds {
				at := s.at("kinds", strconv.Itoa(k))
				path, isString := entry.(string)
				if !isString || !validPath(path) || containsString(seen, path) {
					bad(at)
				} else {
					s.paths = append(s.paths, sourcePath{at: at, path: path, role: documentKind})
				}
				if isString {
					seen = append(seen, path)
				}
			}
		}
	}
	// No two paths of one source are equal once case is folded: on a file
	// system that folds case they would name one file.
	folded := map[string]bool{}
	for _, p := range s.paths {
		key := cases.Fold().String(p.path)
		if folded[key] {
			bad(p.at)
		}
		folded[key] = true
	}
	return s
}

// servedEnvironments reads a repository's served environments: a non-empty
// subset of the manifest's, in their order.
func servedEnvironments(value any, environments []string) ([]string, bool) {
	items, isList := value.([]any)
	if !isList || len(items) == 0 {
		return nil, false
	}
	served := make([]string, 0, len(items))
	for _, item := range items {
		name, isString := item.(string)
		if !isString || !containsString(environments, name) || containsString(served, name) {
			return nil, false
		}
		served = append(served, name)
	}
	next := 0
	for _, name := range environments {
		if next < len(served) && served[next] == name {
			next++
		}
	}
	return served, next == len(served)
}

// validPath is §3's path: registry §11.3's response path form without its
// owns condition, of at most 512 bytes.
func validPath(path string) bool {
	return len(path) <= MaxPathBytes && emit.ValidPath(path)
}

// validRepository is §3's repository identity: an absolute URI of at most
// 2 048 characters, with no userinfo, no query and no fragment.
func validRepository(value any) bool {
	text, isString := value.(string)
	if !isString || len(text) > MaxRepositoryURI || !registry.IsAbsoluteURI(text) || strings.Contains(text, "?") {
		return false
	}
	// The authority follows "//" and ends at the next "/", "?" or end; an
	// "@" in it sets userinfo apart. An "@" in a path is a path character.
	_, rest, _ := strings.Cut(text, ":")
	if authority, ok := strings.CutPrefix(rest, "//"); ok {
		if end := strings.IndexAny(authority, "/?"); end >= 0 {
			authority = authority[:end]
		}
		if strings.Contains(authority, "@") {
			return false
		}
	}
	return true
}

func anyStrings(list []string) []any {
	out := make([]any, len(list))
	for i, item := range list {
		out[i] = item
	}
	return out
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// Path is one path a manifest names for a source, with the size bound of
// the document it holds: a YAML path's is registry.MaxImportBytes, the
// bound of the file import reads.
type Path struct {
	Path     string
	MaxBytes int
}

// SourcePaths returns, by source name, the paths a manifest names that are
// of their form (§3): what a caller reads at each source's commit to give an
// assembly its bytes. A manifest that is not admitted names none; the
// assembly reports why.
func SourcePaths(data []byte) map[string][]Path {
	out := map[string][]Path{}
	m := readManifest(data, &collector{})
	if m == nil {
		return out
	}
	for _, s := range m.sources {
		if s.name == "" {
			continue
		}
		for _, p := range s.paths {
			bound := MaxRegistryBytes
			switch {
			case strings.HasSuffix(p.path, ".yaml") || strings.HasSuffix(p.path, ".yml"):
				bound = registry.MaxImportBytes
			case p.role == documentBounds:
				bound = MaxBoundsBytes
			case p.role == documentKind:
				bound = MaxKindBytes
			}
			out[s.name] = append(out[s.name], Path{Path: p.path, MaxBytes: bound})
		}
	}
	return out
}
