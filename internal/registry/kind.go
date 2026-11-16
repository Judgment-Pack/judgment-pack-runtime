package registry

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/uriformat"
)

var (
	kindNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	ownerPattern     = regexp.MustCompile(`^[A-Za-z0-9._@/-]{1,128}$`)
	semverPattern    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$`)
	defaultEntryID   = mustPattern(`^[a-z][a-z0-9-]{0,62}$`)
	environmentRegex = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

// Default kind bounds (RFC 0034 §2).
const (
	DefaultMaxEntries    = 10000
	DefaultMaxEntryBytes = 65536
)

func mustPattern(source string) *Pattern {
	pattern, err := CompilePattern(source)
	if err != nil {
		panic(err)
	}
	return pattern
}

// Kind is an admitted, valid kind declaration.
type Kind struct {
	Digest      string
	ID          string
	Version     string
	Name        string
	Owner       string
	Deprecated  bool
	Document    map[string]any
	entryID     *Pattern
	schema      *schemaNode
	secrets     []pointerPattern
	references  []referencePattern
	overlayable []pointerPattern
	maxEntries  int
	maxBytes    int
}

type referencePattern struct {
	pattern pointerPattern
	kinds   []string
}

var kindMembers = map[string]bool{
	"kindFormatVersion": true, "id": true, "version": true, "kind": true, "owner": true, "description": true,
	"entryId": true, "schema": true, "secrets": true, "references": true, "overlayable": true, "bounds": true, "deprecated": true,
}

var kindRequired = []string{"id", "version", "kind", "owner", "schema"}

// readKind runs step 2 on one admitted kind declaration.
func readKind(root map[string]any, location Location, diagnostics *collector) *Kind {
	before := diagnostics.errors()
	// Every step-2 diagnostic of a declaration whose kind member is a kind
	// name carries it (§7.3), so it is read before any other member.
	if name, ok := root["kind"].(string); ok && location.Kind == "" && kindNamePattern.MatchString(name) {
		location.Kind = name
	}
	fail := func(code string, path ...string) {
		at := location
		at.Pointer = pointerOf(path)
		diagnostics.add(code, at)
	}
	for _, name := range sortedKeys(root) {
		if !kindMembers[name] {
			fail("REG-KIND-UNKNOWN-MEMBER", name)
		}
	}
	for _, name := range kindRequired {
		if _, ok := root[name]; !ok {
			fail("REG-KIND-MISSING-MEMBER", name)
		}
	}
	kind := &Kind{Document: root, entryID: defaultEntryID, maxEntries: DefaultMaxEntries, maxBytes: DefaultMaxEntryBytes}
	patterns := &patternBudget{}
	stringMember := func(name string, valid func(string) bool) string {
		value, present := root[name]
		if !present {
			return ""
		}
		text, ok := value.(string)
		if !ok || !valid(text) {
			fail("REG-KIND-FORM", name)
			return ""
		}
		return text
	}
	kind.ID = stringMember("id", isAbsoluteURI)
	kind.Version = stringMember("version", semverPattern.MatchString)
	kind.Name = stringMember("kind", kindNamePattern.MatchString)
	kind.Owner = stringMember("owner", ownerPattern.MatchString)
	stringMember("description", func(string) bool { return true })
	if value, ok := root["deprecated"]; ok {
		deprecated, isBool := value.(bool)
		if !isBool {
			fail("REG-KIND-FORM", "deprecated")
		}
		kind.Deprecated = deprecated
	}
	if value, ok := root["entryId"]; ok {
		object, isObject := value.(map[string]any)
		source, isString := object["pattern"].(string)
		switch {
		case !isObject || len(object) != 1 || !isString:
			fail("REG-KIND-FORM", "entryId")
		default:
			pattern, code := patterns.compile(source)
			if code != "" {
				fail(code, "entryId", "pattern")
			} else {
				kind.entryID = pattern
			}
		}
	}
	if value, ok := root["bounds"]; ok {
		kind.readBounds(value, fail)
	}
	if value, ok := root["schema"]; ok {
		kind.schema = compileSchema(value, []string{"schema"}, location, diagnostics, patterns)
	}
	kind.secrets = readPointerPatterns(root, "secrets", kind.schema, fail)
	kind.overlayable = readPointerPatterns(root, "overlayable", kind.schema, fail)
	kind.references = readReferencePatterns(root, kind.schema, fail)
	for i, secret := range kind.secrets {
		for _, reference := range kind.references {
			if secret.overlaps(reference.pattern) {
				fail("REG-KIND-POINTER-CONFLICT", "secrets", strconv.Itoa(i))
				break
			}
		}
	}
	if diagnostics.errors() > before {
		return nil
	}
	return kind
}

func (kind *Kind) readBounds(value any, fail func(string, ...string)) {
	object, ok := value.(map[string]any)
	if !ok {
		fail("REG-KIND-FORM", "bounds")
		return
	}
	for _, name := range sortedKeys(object) {
		n, ok := nonNegativeInteger(object[name])
		switch {
		case name == "maxEntries" && ok && n >= 1:
			kind.maxEntries = n
		case name == "maxEntryBytes" && ok && n >= 1 && n <= MaxResolvedBytes:
			kind.maxBytes = n
		case name == "maxEntries" || name == "maxEntryBytes":
			fail("REG-KIND-FORM", "bounds", name)
		default:
			fail("REG-KIND-UNKNOWN-MEMBER", "bounds", name)
		}
	}
}

// readPointerPatterns reads an optional array of distinct pointer patterns,
// each of which must reach a position the schema allows.
func readPointerPatterns(root map[string]any, member string, schema *schemaNode, fail func(string, ...string)) []pointerPattern {
	value, present := root[member]
	if !present {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		fail("REG-KIND-FORM", member)
		return nil
	}
	var out []pointerPattern
	seen := make(map[string]bool, len(items))
	for i, item := range items {
		at := strconv.Itoa(i)
		text, ok := item.(string)
		if !ok || seen[text] {
			fail("REG-KIND-FORM", member, at)
			continue
		}
		seen[text] = true
		pattern, err := parsePointerPattern(text)
		if err != nil {
			fail("REG-KIND-POINTER-FORM", member, at)
			continue
		}
		if schema != nil && !schema.reachable(pattern.tokens) {
			fail("REG-KIND-POINTER-UNREACHABLE", member, at)
			continue
		}
		out = append(out, pattern)
	}
	return out
}

func readReferencePatterns(root map[string]any, schema *schemaNode, fail func(string, ...string)) []referencePattern {
	value, present := root["references"]
	if !present {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		fail("REG-KIND-FORM", "references")
		return nil
	}
	var out []referencePattern
	sources := make(map[string]bool, len(items))
	for i, item := range items {
		at := strconv.Itoa(i)
		object, ok := item.(map[string]any)
		if !ok {
			fail("REG-KIND-FORM", "references", at)
			continue
		}
		bad := false
		for _, name := range sortedKeys(object) {
			if name != "pointer" && name != "kinds" {
				fail("REG-KIND-UNKNOWN-MEMBER", "references", at, name)
				bad = true
			}
		}
		text, isString := object["pointer"].(string)
		kindsRaw, isArray := object["kinds"].([]any)
		if !isString || !isArray || len(kindsRaw) == 0 {
			fail("REG-KIND-FORM", "references", at)
			continue
		}
		var kinds []string
		named := make(map[string]bool, len(kindsRaw))
		for _, kindRaw := range kindsRaw {
			name, ok := kindRaw.(string)
			if !ok || !kindNamePattern.MatchString(name) || named[name] {
				fail("REG-KIND-FORM", "references", at, "kinds")
				bad = true
				break
			}
			named[name] = true
			kinds = append(kinds, name)
		}
		pattern, err := parsePointerPattern(text)
		if err != nil {
			fail("REG-KIND-POINTER-FORM", "references", at, "pointer")
			continue
		}
		if schema != nil && !schema.reachable(pattern.tokens) {
			fail("REG-KIND-POINTER-UNREACHABLE", "references", at, "pointer")
			continue
		}
		if sources[pattern.source] {
			fail("REG-KIND-FORM", "references", at, "pointer")
			bad = true
		}
		if !bad {
			sources[pattern.source] = true
			out = append(out, referencePattern{pattern: pattern, kinds: kinds})
		}
	}
	return out
}

// isAbsoluteURI reports whether text is an RFC 3986 absolute-URI of at most
// 2048 characters (registry companion §3.1): scheme ":" hier-part
// [ "?" query ], so no fragment, in ASCII, with no white space. The grammar
// is the runtime's one RFC 3986 reader (ADR-0064). It is the form of a
// kind's and a registry's id, of a pin's id, and of a facts contract's id
// and source kind id.
func isAbsoluteURI(text string) bool {
	return text != "" && len(text) <= 2048 && !strings.Contains(text, "#") && uriformat.Valid(text)
}

func sortedKeys[V any](object map[string]V) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
