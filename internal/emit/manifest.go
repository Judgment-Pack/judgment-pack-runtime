package emit

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// Inputs a manifest can name (RFC 0036 §1).
const (
	InputResolved       = "resolved"
	InputFactsContracts = "factsContracts"
	EnvironmentsEach    = "each"
	EnvironmentsAll     = "all"
)

// Manifest is an admitted, valid emitter manifest.
type Manifest struct {
	ID              string
	Version         string
	Owner           string
	Reads           []Read
	Input           string
	RequestVersions []string
	Environments    string
	Options         *registry.Schema
	Command         []string
	Owns            []Glob
	Env             []string
	Deprecated      bool
}

// Read is one kind a manifest reads.
type Read struct {
	Kind     string
	ID       string
	Versions Range
}

var (
	envNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
	reservedEnv    = map[string]bool{"PATH": true, "HOME": true, "LANG": true, "TZ": true, "SOURCE_DATE_EPOCH": true}
	manifestFields = map[string]bool{
		"emitterManifestVersion": true, "id": true, "version": true, "owner": true, "description": true, "reads": true,
		"input": true, "requestVersions": true, "environments": true, "options": true, "command": true, "owns": true,
		"env": true, "deprecated": true,
	}
	manifestRequired = []string{"emitterManifestVersion", "id", "version", "owner", "reads", "requestVersions", "command", "owns"}
)

// defaultOptionsSchema is the options schema of a manifest that gives none.
var defaultOptionsSchema = map[string]any{"type": "object", "additionalProperties": false}

// ReadManifest admits and checks an emitter manifest. It returns the
// manifest, or nil and the EMIT-MANIFEST diagnostics, each at the pointer of
// the member that refuses it, that say why not.
func ReadManifest(data []byte) (*Manifest, []Diagnostic) {
	var diagnostics []Diagnostic
	fail := func(path ...string) {
		diagnostics = append(diagnostics, diagnostic("EMIT-MANIFEST", Location{Document: DocumentManifest, Pointer: carrier.Pointer(path)}))
	}
	value, code, pointer := registry.AdmitValue(data, MaxManifestBytes)
	if code != "" {
		diagnostics = append(diagnostics, diagnostic("EMIT-MANIFEST", Location{Document: DocumentManifest, Pointer: pointer}))
		return nil, diagnostics
	}
	root, ok := value.(map[string]any)
	if !ok {
		fail()
		return nil, diagnostics
	}
	if selector, ok := root["emitterManifestVersion"].(string); !ok || selector != ManifestVersion {
		fail("emitterManifestVersion")
		return nil, diagnostics
	}
	for _, name := range sortedKeys(root) {
		if !manifestFields[name] {
			fail(name)
		}
	}
	for _, name := range manifestRequired {
		if _, ok := root[name]; !ok {
			fail(name)
		}
	}
	m := &Manifest{Input: InputResolved}
	text := func(name string, valid func(string) bool) string {
		value, present := root[name]
		if !present {
			return ""
		}
		s, ok := value.(string)
		if !ok || !valid(s) {
			fail(name)
			return ""
		}
		return s
	}
	m.ID = text("id", registry.IsAbsoluteURI)
	m.Version = text("version", registry.ValidVersion)
	m.Owner = text("owner", registry.ValidOwner)
	text("description", func(string) bool { return true })
	if _, present := root["input"]; present {
		m.Input = text("input", func(s string) bool { return s == InputResolved || s == InputFactsContracts })
	}
	_, hasEnvironments := root["environments"]
	switch {
	case m.Input == InputResolved && !hasEnvironments:
		fail("environments")
	case m.Input == InputFactsContracts && hasEnvironments:
		fail("environments")
	case hasEnvironments:
		m.Environments = text("environments", func(s string) bool { return s == EnvironmentsEach || s == EnvironmentsAll })
	}
	if value, present := root["deprecated"]; present {
		deprecated, ok := value.(bool)
		if !ok {
			fail("deprecated")
		}
		m.Deprecated = deprecated
	}
	m.Reads = readReads(root["reads"], fail)
	m.RequestVersions = readStrings(root, "requestVersions", true, registry.ValidVersion, fail)
	m.Env = readStrings(root, "env", false, func(s string) bool { return envNamePattern.MatchString(s) && !reservedEnv[s] }, fail)
	m.Command = readCommand(root["command"], fail)
	for i, source := range readStrings(root, "owns", true, func(string) bool { return true }, fail) {
		glob, err := parseGlob(source)
		if err != nil {
			fail("owns", strconv.Itoa(i))
			continue
		}
		m.Owns = append(m.Owns, glob)
	}
	schema := any(defaultOptionsSchema)
	if value, present := root["options"]; present {
		schema = value
	}
	compiled, findings := registry.CompileSchema(schema)
	for _, finding := range findings {
		diagnostics = append(diagnostics, diagnostic("EMIT-MANIFEST", Location{Document: DocumentManifest, Pointer: "/options" + finding.Pointer}))
	}
	m.Options = compiled
	if len(diagnostics) > 0 {
		return nil, diagnostics
	}
	return m, nil
}

func readReads(value any, fail func(...string)) []Read {
	items, ok := value.([]any)
	if !ok {
		if value != nil {
			fail("reads")
		}
		return nil
	}
	var reads []Read
	for i, item := range items {
		at := strconv.Itoa(i)
		object, ok := item.(map[string]any)
		if !ok || len(object) != 3 {
			fail("reads", at)
			continue
		}
		kind, kindOK := object["kind"].(string)
		id, idOK := object["id"].(string)
		versions, versionsOK := object["versions"].(string)
		switch {
		case !kindOK || !registry.ValidKindName(kind) || slices.ContainsFunc(reads, func(r Read) bool { return r.Kind == kind }):
			fail("reads", at, "kind")
		case !idOK || !registry.IsAbsoluteURI(id):
			fail("reads", at, "id")
		case !versionsOK:
			fail("reads", at, "versions")
		default:
			r, err := ParseRange(versions)
			if err != nil {
				fail("reads", at, "versions")
				continue
			}
			reads = append(reads, Read{Kind: kind, ID: id, Versions: r})
		}
	}
	return reads
}

// readStrings reads an array of distinct strings, each valid; required means
// present and non-empty.
func readStrings(root map[string]any, name string, required bool, valid func(string) bool, fail func(...string)) []string {
	value, present := root[name]
	if !present {
		return nil
	}
	items, ok := value.([]any)
	if !ok || required && len(items) == 0 {
		fail(name)
		return nil
	}
	var out []string
	for i, item := range items {
		s, ok := item.(string)
		if !ok || !valid(s) || slices.Contains(out, s) {
			fail(name, strconv.Itoa(i))
			continue
		}
		out = append(out, s)
	}
	return out
}

// readCommand reads command: an argv, not a shell string. Its first item is
// a program name with no "/", looked up on PATH, or a relative path inside the
// repository (an optional leading "./", then the path rules). No item names
// a path outside the repository: none begins with "/" or has a ".." segment.
func readCommand(value any, fail func(...string)) []string {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		if value != nil {
			fail("command")
		}
		return nil
	}
	var argv []string
	for i, item := range items {
		s, ok := item.(string)
		bad := !ok || strings.ContainsRune(s, 0) || strings.HasPrefix(s, "/") || slices.Contains(strings.Split(s, "/"), "..")
		if !bad && i == 0 {
			bad = !validProgram(s)
		}
		if bad {
			fail("command", strconv.Itoa(i))
			continue
		}
		argv = append(argv, s)
	}
	return argv
}

func validProgram(s string) bool {
	if !strings.Contains(s, "/") {
		return s != "" && s != "." && s != ".." && checkPathForm(s) == nil
	}
	return checkPathForm(strings.TrimPrefix(s, "./")) == nil
}

func sortedKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// Range is a range over kind versions (RFC 0036 §1): "^x.y.z", "~x.y.z",
// "=x.y.z", or ">=x.y.z <x.y.z". Its versions are release versions; a
// version with a pre-release part is in no range, since no range can name
// one.
type Range struct {
	source string
	lower  [3]string // inclusive
	upper  [3]string // exclusive; unset for "="
	exact  bool
}

// String returns the range as written.
func (r Range) String() string { return r.source }

var releasePattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type rangeError struct{}

func (rangeError) Error() string { return "emit: not a version range" }

// ParseRange reads a range. A caret on a 0.y.z version keeps its leftmost
// non-zero part, as SemVer ranges conventionally do: ^0.2.3 is
// >=0.2.3 <0.3.0, and ^0.0.3 is >=0.0.3 <0.0.4.
func ParseRange(source string) (Range, error) {
	release := func(text string) ([3]string, bool) {
		match := releasePattern.FindStringSubmatch(text)
		if match == nil {
			return [3]string{}, false
		}
		return [3]string{match[1], match[2], match[3]}, true
	}
	r := Range{source: source}
	switch {
	case strings.HasPrefix(source, ">="):
		lowerText, upperText, ok := strings.Cut(source[2:], " <")
		lower, lowerOK := release(lowerText)
		upper, upperOK := release(upperText)
		if !ok || !lowerOK || !upperOK || compareRelease(lower, upper) >= 0 {
			return Range{}, rangeError{}
		}
		r.lower, r.upper = lower, upper
	case strings.HasPrefix(source, "^"), strings.HasPrefix(source, "~"), strings.HasPrefix(source, "="):
		v, ok := release(source[1:])
		if !ok {
			return Range{}, rangeError{}
		}
		r.lower = v
		switch {
		case source[0] == '=':
			r.exact = true
		case source[0] == '~':
			r.upper = [3]string{v[0], increment(v[1]), "0"}
		case v[0] != "0":
			r.upper = [3]string{increment(v[0]), "0", "0"}
		case v[1] != "0":
			r.upper = [3]string{"0", increment(v[1]), "0"}
		default:
			r.upper = [3]string{"0", "0", increment(v[2])}
		}
	default:
		return Range{}, rangeError{}
	}
	return r, nil
}

// Contains reports whether a kind version is in the range.
func (r Range) Contains(version string) bool {
	match := releasePattern.FindStringSubmatch(version)
	if match == nil {
		return false
	}
	v := [3]string{match[1], match[2], match[3]}
	if r.exact {
		return v == r.lower
	}
	return compareRelease(v, r.lower) >= 0 && compareRelease(v, r.upper) < 0
}

// compareRelease compares two release versions part by part. Each part is a
// decimal without leading zeros, so a longer one is larger, and parts of one
// length compare as text: no part is ever read into a machine integer.
func compareRelease(a, b [3]string) int {
	for i := range a {
		if len(a[i]) != len(b[i]) {
			if len(a[i]) < len(b[i]) {
				return -1
			}
			return 1
		}
		if c := strings.Compare(a[i], b[i]); c != 0 {
			return c
		}
	}
	return 0
}

// increment adds one to a decimal without leading zeros.
func increment(digits string) string {
	out := []byte(digits)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i] < '9' {
			out[i]++
			return string(out)
		}
		out[i] = '0'
	}
	return "1" + string(out)
}

// compareVersions orders two SemVer versions by SemVer 2.0.0 precedence; it is
// used for request selectors, which are release versions.
func compareVersions(a, b string) int {
	pa, pb := releasePattern.FindStringSubmatch(a), releasePattern.FindStringSubmatch(b)
	if pa == nil || pb == nil {
		return strings.Compare(a, b)
	}
	return compareRelease([3]string{pa[1], pa[2], pa[3]}, [3]string{pb[1], pb[2], pb[3]})
}

// majorMinor returns a SemVer version's major and minor, as text.
func majorMinor(version string) (string, string, bool) {
	parts := strings.SplitN(strings.SplitN(version, "-", 2)[0], ".", 3)
	if len(parts) != 3 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
