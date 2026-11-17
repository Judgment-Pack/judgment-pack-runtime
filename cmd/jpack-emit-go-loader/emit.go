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
	emitterID      = "https://github.com/Judgment-Pack/judgment-pack-runtime/cmd/jpack-emit-go-loader"
	emitterVersion = "0.1.0"
)

// The formats this emitter reads and writes.
const (
	requestVersion  = "1.0.0"
	responseVersion = "1.0.0"
	resolvedVersion = "1.0.0"
	kindVersion     = "1.0.0"
)

// startupPath is the import path of the start-up module, the one module
// outside the standard library the generated code imports.
const startupPath = "github.com/Judgment-Pack/judgment-pack-runtime/startup"

// defaultDirectory is the output directory when the options name none. The
// manifest's owns glob names it.
const defaultDirectory = "generated/loader"

// Bounds. The response bounds are companion §11.3's, so the host never sees a
// response it would refuse for its size.
const (
	maxRequestBytes  = 64 << 20
	maxResponseBytes = 64 << 20
	maxFiles         = 10000
	maxFileBytes     = 16 << 20
	maxPathBytes     = 512
	// maxSegmentBytes bounds each segment of a path, the file name and each
	// directory: the name length of common file systems, past which the host
	// could not write the file.
	maxSegmentBytes = 255
	// maxDepth bounds the request's JSON nesting. A declaration and a
	// resolved document each nest at most 32 deep (companion §2.1's bound,
	// which the start-up module holds a resolved document to as well), and
	// each sits two below the request's root.
	maxDepth          = 32 + 2
	maxPackageBytes   = 64
	maxDirectoryBytes = 256
	// maxPositions bounds the positions one kind's type has, $ref expanded,
	// at companion §3.3's bound on a schema's expanded size.
	maxPositions = 4096
	// maxRequestPositions bounds the positions of every kind of a request
	// together. The response bound would refuse their source past about
	// this many, so the emitter stops before writing it.
	maxRequestPositions = 1 << 16
)

// options are the emitter's options (the manifest's options schema).
type options struct {
	pkg       string
	directory string
}

var (
	packagePattern = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	// directoryPattern is a literal relative path: segments of letters,
	// digits, "_", "." and "-", each beginning with a letter or a digit and
	// ending with a letter, a digit or "_". So no segment is ".", "..",
	// ".git" or ends in ".", and none begins with "." or "_", which the Go
	// toolchain ignores, so the generated package would never be compiled.
	directoryPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9_.\-]*[A-Za-z0-9_])?(/[A-Za-z0-9]([A-Za-z0-9_.\-]*[A-Za-z0-9_])?)*$`)
	// testdataPattern is a directory with a segment testdata or vendor, in
	// any case, which the Go toolchain also ignores or treats as vendored
	// code, and which a case-insensitive file system would read as one. The
	// manifest's schema refuses it with the same pattern under "not", spelled
	// with classes since a JSON Schema pattern has no case flag.
	testdataPattern = regexp.MustCompile(`^(.*/)?([Tt][Ee][Ss][Tt][Dd][Aa][Tt][Aa]|[Vv][Ee][Nn][Dd][Oo][Rr])(/.*)?$`)
)

// notPackageName says whether the package option refuses name: a Go
// keyword, which a package clause cannot use; main, which makes a command
// that no other package can import; or documentation, whose files the Go
// toolchain ignores.
func notPackageName(name string) bool {
	return goKeywords[name] || name == "main" || name == "documentation"
}

// goKeywords are the names a package clause cannot use.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true, "default": true,
	"defer": true, "else": true, "fallthrough": true, "for": true, "func": true, "go": true,
	"goto": true, "if": true, "import": true, "interface": true, "map": true, "package": true,
	"range": true, "return": true, "select": true, "struct": true, "switch": true, "type": true,
	"var": true,
}

// emit is the emitter: one request in, one response out. It returns one of
// the refusals, and no response, for a request it does not read, and a
// response of one error diagnostic for a declaration it cannot generate code
// for.
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
	declarations, ok := root["declarations"].([]any)
	if !ok {
		return nil, errRequest
	}
	resolved, ok := root["resolved"].([]any)
	if !ok {
		return nil, errRequest
	}
	kinds, err := readDeclarations(declarations)
	if err != nil {
		return nil, err
	}
	// Every pin of every environment against the declarations, before any
	// schema is read.
	if err := checkPins(resolved, kinds); err != nil {
		return nil, err
	}
	files, err := generate(kinds, opts)
	var found *findingAt
	if errors.As(err, &found) {
		return encodeResponse(nil, found)
	}
	if err != nil {
		return nil, err
	}
	return encodeResponse(files, nil)
}

// file is one file of the response.
type file struct {
	path string
	text string
}

// encodeResponse writes the response as RFC 8785 canonical JSON: the files,
// or, for a finding, no file and the finding's error diagnostic. It holds the
// files to the response bounds and the text rules of companion §11.3 (UTF-8
// that ends with a line feed and holds no carriage return), and each path
// segment to maxSegmentBytes.
func encodeResponse(files []file, found *findingAt) ([]byte, error) {
	if len(files) > maxFiles {
		return nil, errBounds
	}
	slices.SortFunc(files, func(a, b file) int { return strings.Compare(a.path, b.path) })
	items := make([]any, len(files))
	for i, f := range files {
		if len(f.path) > maxPathBytes || len(f.text) > maxFileBytes {
			return nil, errBounds
		}
		for _, segment := range strings.Split(f.path, "/") {
			if len(segment) > maxSegmentBytes {
				return nil, errBounds
			}
		}
		if !utf8.ValidString(f.text) || !strings.HasSuffix(f.text, "\n") || strings.ContainsRune(f.text, '\r') {
			return nil, errInternal
		}
		items[i] = map[string]any{"path": f.path, "text": f.text}
	}
	diagnostics := []any{}
	if found != nil {
		// A location of the companion's §7.3 form that names only the kind,
		// which the request pins: the host resolves it, and it carries no
		// value (RFC 0036 §3).
		diagnostics = append(diagnostics, map[string]any{
			"severity": "error",
			"code":     string(found.code),
			"location": map[string]any{"document": "kind", "kind": found.kind, "pointer": ""},
		})
	}
	response, err := jcs.EncodeDocument(map[string]any{
		"emitResponseVersion": responseVersion,
		"files":               items,
		"diagnostics":         diagnostics,
	})
	if err != nil {
		return nil, errInternal
	}
	if len(response) > maxResponseBytes {
		return nil, errBounds
	}
	return response, nil
}

// readOptions reads the options: package, required, and directory, which
// defaults to defaultDirectory. The host has validated them against the
// manifest's schema; they are checked again here, to the same rules.
func readOptions(value any) (options, error) {
	m, ok := value.(map[string]any)
	if !ok {
		return options{}, errOptions
	}
	for name := range m {
		if name != "package" && name != "directory" {
			return options{}, errOptions
		}
	}
	pkg, ok := m["package"].(string)
	if !ok || len(pkg) > maxPackageBytes || !packagePattern.MatchString(pkg) || notPackageName(pkg) {
		return options{}, errOptions
	}
	opts := options{pkg: pkg, directory: defaultDirectory}
	if raw, present := m["directory"]; present {
		directory, ok := raw.(string)
		if !ok || len(directory) > maxDirectoryBytes || !directoryPattern.MatchString(directory) || testdataPattern.MatchString(directory) {
			return options{}, errOptions
		}
		opts.directory = directory
	}
	return opts, nil
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
