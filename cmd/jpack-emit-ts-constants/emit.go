package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// The emitter's identity, which its manifest (emitter.json) repeats and the
// generated-file header carries.
const (
	emitterID      = "https://github.com/Judgment-Pack/judgment-pack-runtime/cmd/jpack-emit-ts-constants"
	emitterVersion = "0.1.0"
)

// The formats this emitter reads and writes.
const (
	requestVersion    = "1.0.0"
	responseVersion   = "1.0.0"
	resolvedVersion   = "1.0.0"
	kindFormatVersion = "1.0.0"
)

// defaultDirectory is the output directory when the options name none. The
// manifest's owns glob names it.
const defaultDirectory = "generated/constants"

// Bounds. The response bounds are companion §11.3's, so the host never sees a
// response it would refuse for its size.
const (
	maxRequestBytes  = 64 << 20
	maxResponseBytes = 64 << 20
	maxFileBytes     = 16 << 20
	// maxSegmentBytes bounds each directory segment of the directory option:
	// the name length of common file systems.
	maxSegmentBytes   = 255
	maxDirectoryBytes = 256
	// maxDepth bounds the request's JSON nesting, each value counting one
	// level. A resolved document and a declaration each nest at most 32
	// containers (RFC 0034 §1), two below the request's root, and a scalar
	// inside the deepest is one more.
	maxDepth = 35
	// maxDiagnostics bounds the diagnostics of one refused response.
	maxDiagnostics = 100
)

var (
	kindPattern        = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	environmentPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	digestPattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	versionPattern     = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
	// directoryPattern is a literal relative path: segments of letters,
	// digits, "_", "." and "-", each beginning with a letter or a digit and
	// ending with a letter, a digit or "_". So no segment is ".", "..",
	// ".git" or ends in ".", and none begins with "." or "_".
	directoryPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9_.\-]*[A-Za-z0-9_])?(/[A-Za-z0-9]([A-Za-z0-9_.\-]*[A-Za-z0-9_])?)*$`)
	// ignoredPattern is a directory with a segment node_modules, vendor or
	// testdata, in any case: package managers own node_modules, vendor is
	// read as third-party code, and testdata is skipped by Go tooling, so
	// generated code there would be replaced or never checked. A
	// case-insensitive file system reads NODE_MODULES as node_modules. The
	// manifest's schema refuses it with the same pattern under "not",
	// spelled with classes since a JSON Schema pattern has no case flag.
	ignoredPattern = regexp.MustCompile(`^(.*/)?([Nn][Oo][Dd][Ee]_[Mm][Oo][Dd][Uu][Ll][Ee][Ss]|[Vv][Ee][Nn][Dd][Oo][Rr]|[Tt][Ee][Ss][Tt][Dd][Aa][Tt][Aa])(/.*)?$`)
)

// options are the emitter's options (the manifest's options schema).
type options struct {
	directory string
}

// pin is one pinned kind of the resolved document.
type pin struct {
	kind, id, version, digest string
}

// entry is one resolved entry.
type entry struct {
	kind, id string
	value    any
}

// resolved is the request's one resolved document, read.
type resolved struct {
	environment    string
	registryDigest string
	pins           []pin // sorted by kind
	entries        []entry
}

// emit is the emitter: one request in, one response out. It returns one of
// the refusals, and no response, for a request it cannot read. A request it
// reads but refuses gets a response of no files and error diagnostics.
func emit(request []byte) ([]byte, error) {
	value, err := decodeStrict(request)
	if err != nil {
		return nil, err
	}
	root, ok := value.(map[string]any)
	if !ok || !exactMembers(root, "emitRequestVersion", "emitter", "options", "declarations", "resolved") {
		return nil, errRequest
	}
	if root["emitRequestVersion"] != requestVersion {
		return nil, errRequest
	}
	emitter, ok := root["emitter"].(map[string]any)
	if !ok || !exactMembers(emitter, "id", "version") || !isString(emitter["id"]) || !isString(emitter["version"]) {
		return nil, errRequest
	}
	opts, err := readOptions(root["options"])
	if err != nil {
		return nil, err
	}
	documents, ok := root["resolved"].([]any)
	if !ok || len(documents) != 1 {
		return nil, errRequest
	}
	doc, err := readResolved(documents[0])
	if err != nil {
		return nil, err
	}
	declarations, ok := root["declarations"].([]any)
	if !ok {
		return nil, errRequest
	}
	kinds, diagnostics := bindDeclarations(doc, declarations)
	if len(diagnostics) > 0 {
		return encodeRefusal(diagnostics)
	}
	f, diagnostics, err := generate(doc, kinds, opts)
	if err != nil {
		return nil, err
	}
	if len(diagnostics) > 0 {
		return encodeRefusal(diagnostics)
	}
	return encodeResponse(f, doc)
}

// readResolved reads the resolved document of RFC 0034 §5 (companion §8):
// exactly its five members, the pins of distinct kinds, and entries of
// pinned kinds, each (kind, id) once.
func readResolved(value any) (*resolved, error) {
	document, ok := value.(map[string]any)
	if !ok || !exactMembers(document, "resolvedFormatVersion", "registry", "environment", "kinds", "entries") ||
		document["resolvedFormatVersion"] != resolvedVersion {
		return nil, errRequest
	}
	registry, ok := document["registry"].(map[string]any)
	if !ok || !exactMembers(registry, "id", "version", "digest") || !isString(registry["id"]) || !isString(registry["version"]) {
		return nil, errRequest
	}
	doc := &resolved{}
	doc.registryDigest, _ = registry["digest"].(string)
	doc.environment, _ = document["environment"].(string)
	if !digestPattern.MatchString(doc.registryDigest) || !environmentPattern.MatchString(doc.environment) {
		return nil, errRequest
	}
	pins, ok := document["kinds"].([]any)
	if !ok {
		return nil, errRequest
	}
	pinned := map[string]bool{}
	for _, item := range pins {
		object, ok := item.(map[string]any)
		if !ok || !exactMembers(object, "kind", "id", "version", "digest") {
			return nil, errRequest
		}
		var p pin
		p.kind, _ = object["kind"].(string)
		p.id, _ = object["id"].(string)
		p.version, _ = object["version"].(string)
		p.digest, _ = object["digest"].(string)
		if !kindPattern.MatchString(p.kind) || p.id == "" || !versionPattern.MatchString(p.version) ||
			!digestPattern.MatchString(p.digest) || pinned[p.kind] {
			return nil, errRequest
		}
		pinned[p.kind] = true
		doc.pins = append(doc.pins, p)
	}
	slices.SortFunc(doc.pins, func(a, b pin) int { return strings.Compare(a.kind, b.kind) })
	entries, ok := document["entries"].([]any)
	if !ok {
		return nil, errRequest
	}
	seen := map[[2]string]bool{}
	for _, item := range entries {
		object, ok := item.(map[string]any)
		if !ok || !exactMembers(object, "kind", "id", "owner", "value") || !isString(object["owner"]) {
			return nil, errRequest
		}
		var e entry
		e.kind, _ = object["kind"].(string)
		e.id, _ = object["id"].(string)
		e.value = object["value"]
		name := [2]string{e.kind, e.id}
		if !pinned[e.kind] || e.id == "" || seen[name] {
			return nil, errRequest
		}
		seen[name] = true
		doc.entries = append(doc.entries, e)
	}
	// The output never depends on the order given: entries are written by
	// kind, then by id in RFC 8785 order.
	slices.SortFunc(doc.entries, func(a, b entry) int {
		if c := strings.Compare(a.kind, b.kind); c != 0 {
			return c
		}
		return compareUTF16(a.id, b.id)
	})
	return doc, nil
}

// readOptions reads the options: directory, which defaults to
// defaultDirectory. The host has validated them against the manifest's
// schema; they are checked again here, to the same rules, and each segment
// is also held to maxSegmentBytes.
func readOptions(value any) (options, error) {
	m, ok := value.(map[string]any)
	if !ok {
		return options{}, errOptions
	}
	for name := range m {
		if name != "directory" {
			return options{}, errOptions
		}
	}
	opts := options{directory: defaultDirectory}
	if raw, present := m["directory"]; present {
		directory, ok := raw.(string)
		if !ok || len(directory) > maxDirectoryBytes || !directoryPattern.MatchString(directory) || ignoredPattern.MatchString(directory) {
			return options{}, errOptions
		}
		for _, segment := range strings.Split(directory, "/") {
			if len(segment) > maxSegmentBytes {
				return options{}, errOptions
			}
		}
		opts.directory = directory
	}
	return opts, nil
}

// file is the one file of the response.
type file struct {
	path string
	text string
}

// encodeResponse writes the response as RFC 8785 canonical JSON, after
// holding the file to the response bounds and the text rules of companion
// §11.3 (UTF-8 that ends with a line feed and holds no carriage return). A
// file past the bounds is refused with x-ts-bounds.
func encodeResponse(f file, doc *resolved) ([]byte, error) {
	if !utf8.ValidString(f.text) || !strings.HasSuffix(f.text, "\n") || strings.ContainsRune(f.text, '\r') {
		return nil, errInternal
	}
	bounds := []diagnostic{{code: codeBounds, location: registryLocation(doc.environment)}}
	if len(f.text) > maxFileBytes {
		return encodeRefusal(bounds)
	}
	response, err := jcs.EncodeDocument(map[string]any{
		"emitResponseVersion": responseVersion,
		"files":               []any{map[string]any{"path": f.path, "text": f.text}},
		"diagnostics":         []any{},
	})
	if err != nil {
		return nil, errInternal
	}
	if len(response) > maxResponseBytes {
		return encodeRefusal(bounds)
	}
	return response, nil
}

// encodeRefusal writes a response of no files and the error diagnostics.
func encodeRefusal(diagnostics []diagnostic) ([]byte, error) {
	if len(diagnostics) > maxDiagnostics {
		diagnostics = diagnostics[:maxDiagnostics]
	}
	items := make([]any, len(diagnostics))
	for i, d := range diagnostics {
		items[i] = map[string]any{"severity": "error", "code": d.code, "location": d.location}
	}
	response, err := jcs.EncodeDocument(map[string]any{
		"emitResponseVersion": responseVersion,
		"files":               []any{},
		"diagnostics":         items,
	})
	if err != nil {
		return nil, errInternal
	}
	return response, nil
}

func exactMembers(m map[string]any, names ...string) bool {
	if len(m) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := m[name]; !ok {
			return false
		}
	}
	return true
}

func isString(value any) bool {
	_, ok := value.(string)
	return ok
}

// decodeStrict reads one JSON value and nothing after it. Numbers are kept
// as their literals. It refuses what encoding/json would otherwise read
// around: invalid UTF-8 and unpaired surrogate escapes, which it would
// replace with U+FFFD, and a repeated member name, of which it would keep the
// last.
func decodeStrict(data []byte) (any, error) {
	if !utf8.Valid(data) || unpairedSurrogate(data) {
		return nil, errRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := decodeValue(decoder, 1)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errRequest
	}
	return value, nil
}

func decodeValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > maxDepth {
		return nil, errRequest
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, errRequest
	}
	delim, isDelim := token.(json.Delim)
	switch {
	case !isDelim:
		return token, nil
	case delim == '{':
		object := map[string]any{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil, errRequest
			}
			name, ok := key.(string)
			if !ok {
				return nil, errRequest
			}
			if _, repeated := object[name]; repeated {
				return nil, errRequest
			}
			if object[name], err = decodeValue(decoder, depth+1); err != nil {
				return nil, err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return nil, errRequest
		}
		return object, nil
	case delim == '[':
		array := []any{}
		for decoder.More() {
			item, err := decodeValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, item)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, errRequest
		}
		return array, nil
	}
	return nil, errRequest
}

// unpairedSurrogate reports a \u escape of a high surrogate not followed by
// one of a low surrogate, or of a low surrogate not preceded by one of a high
// surrogate. A backslash appears in well-formed JSON only inside a string, so
// the escapes are read in order without tracking strings; input that is not
// well-formed JSON is refused by the decoder after this scan.
func unpairedSurrogate(data []byte) bool {
	high := false
	for i := 0; i < len(data); {
		if data[i] != '\\' || i+1 == len(data) || data[i+1] != 'u' || i+6 > len(data) {
			if high {
				return true
			}
			if data[i] == '\\' {
				i += 2
			} else {
				i++
			}
			continue
		}
		code, err := strconv.ParseUint(string(data[i+2:i+6]), 16, 32)
		if err != nil {
			return false
		}
		switch {
		case code >= 0xD800 && code <= 0xDBFF:
			if high {
				return true
			}
			high = true
		case code >= 0xDC00 && code <= 0xDFFF:
			if !high {
				return true
			}
			high = false
		default:
			if high {
				return true
			}
		}
		i += 6
	}
	return high
}
