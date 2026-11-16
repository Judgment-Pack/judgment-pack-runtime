package emit

import (
	"encoding/base64"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// File modes a response can give (RFC 0036 §3).
const (
	ModeRegular    = "0644"
	ModeExecutable = "0755"
)

// File is one validated file of a response: a safe owned path, its bytes
// and its mode.
type File struct {
	Path string
	Data []byte
	Mode string
}

// EmitterDiagnostic is one diagnostic an emitter reported, validated: an
// x- code, a severity and a location of the companion's §7.3 form. It never
// carries a value.
type EmitterDiagnostic struct {
	Environment string            `json:"environment,omitempty"`
	Severity    string            `json:"severity"`
	Code        string            `json:"code"`
	Location    registry.Location `json:"location"`
}

// Response is a validated response.
type Response struct {
	Version     string
	Files       []File
	Diagnostics []EmitterDiagnostic
}

// emitterCode is an emitter diagnostic's code: "x-" and lower-case words
// joined by "-", at most 64 bytes. The RFC says only that codes begin with
// "x-"; the narrower grammar keeps a code from carrying a value.
var emitterCode = regexp.MustCompile(`^x-[a-z0-9]+(-[a-z0-9]+)*$`)

const maxEmitterCodeBytes = 64

var (
	responseMembers   = map[string]bool{"emitResponseVersion": true, "files": true, "diagnostics": true}
	fileMembers       = map[string]bool{"path": true, "text": true, "base64": true, "mode": true}
	diagnosticMembers = map[string]bool{"severity": true, "code": true, "location": true}
)

// ParseResponse validates an emitter's standard output completely, against
// the request version it answers and the manifest's owns globs: one I-JSON
// document within MaxResponseBytes, of at most four JSON values a file and a
// fixed slack (responseNodes); exactly its members; a response version
// this host accepts that does not exceed the request's major and minor; at
// most MaxFiles files, each a safe owned path with exactly one of text and
// base64, at most MaxFileBytes, and a mode of 0644 or 0755; paths in strictly
// increasing code-point order and unique once case is folded and
// normalized, none of them a directory of another; and diagnostics of their
// form. An error diagnostic fails it. It returns the response and nothing, or the response's diagnostics (when
// they could be read) and the EMIT-RESPONSE findings, each at a pointer into
// the response and never with a value from it.
func ParseResponse(data []byte, request string, m *Manifest) (*Response, []Diagnostic) {
	var findings []Diagnostic
	fail := func(path ...string) {
		findings = append(findings, diagnostic("EMIT-RESPONSE", Location{Document: DocumentResponse, Pointer: carrier.Pointer(path)}))
	}
	if len(data) > maxResponseBytes || !utf8.Valid(data) {
		fail()
		return nil, findings
	}
	value, code, pointer := registry.AdmitValueNodes(data, maxResponseBytes, responseNodes())
	if code != "" {
		findings = append(findings, diagnostic("EMIT-RESPONSE", Location{Document: DocumentResponse, Pointer: pointer}))
		return nil, findings
	}
	root, ok := value.(map[string]any)
	if !ok {
		fail()
		return nil, findings
	}
	for _, name := range sortedKeys(root) {
		if !responseMembers[name] {
			fail(name)
		}
	}
	for name := range responseMembers {
		if _, ok := root[name]; !ok {
			fail(name)
		}
	}
	response := &Response{}
	response.Version, _ = root["emitResponseVersion"].(string)
	if _, present := root["emitResponseVersion"]; present && !acceptedResponse(response.Version, request) {
		fail("emitResponseVersion")
	}

	items, ok := root["files"].([]any)
	switch {
	case !ok:
		if _, present := root["files"]; present {
			fail("files")
		}
	case len(items) > maxFiles:
		fail("files")
		items = nil
	}
	paths := newPathSet()
	previous := ""
	for i, item := range items {
		at := strconv.Itoa(i)
		file, ok := readFile(item, m, func(member ...string) { fail(append([]string{"files", at}, member...)...) })
		if !ok {
			continue
		}
		bad := i > 0 && file.Path <= previous
		previous = file.Path
		bad = paths.add(file.Path) || bad
		if bad {
			fail("files", at, "path")
		}
		response.Files = append(response.Files, file)
	}

	rawDiagnostics, ok := root["diagnostics"].([]any)
	if !ok {
		if _, present := root["diagnostics"]; present {
			fail("diagnostics")
		}
	}
	for i, item := range rawDiagnostics {
		at := strconv.Itoa(i)
		object, ok := item.(map[string]any)
		if !ok {
			fail("diagnostics", at)
			continue
		}
		bad := false
		for _, name := range sortedKeys(object) {
			if !diagnosticMembers[name] {
				fail("diagnostics", at, name)
				bad = true
			}
		}
		severity, severityOK := object["severity"].(string)
		code, codeOK := object["code"].(string)
		location, locationOK := registry.ReadLocation(object["location"])
		switch {
		case !severityOK || severity != SeverityError && severity != SeverityWarning:
			fail("diagnostics", at, "severity")
		case !codeOK || len(code) > maxEmitterCodeBytes || !emitterCode.MatchString(code):
			fail("diagnostics", at, "code")
		case !locationOK:
			fail("diagnostics", at, "location")
		case bad:
		default:
			response.Diagnostics = append(response.Diagnostics, EmitterDiagnostic{Severity: severity, Code: code, Location: location})
			if severity == SeverityError {
				// RFC 0036 §3: an error diagnostic fails the run once the
				// response is read; nothing is written.
				fail("diagnostics", at)
			}
		}
	}
	if len(findings) > 0 {
		return &Response{Diagnostics: response.Diagnostics}, findings
	}
	return response, nil
}

// responseNodeSlack is the JSON values a response may hold besides four for
// each of its files (the object, path, text or base64, and mode): the root
// and its members, and the diagnostics. It is fixed at what 10 000 files of
// several members each would hold, so about 8 000 diagnostics fit.
const responseNodeSlack = 8 * MaxFiles

// responseNodes bounds the JSON values a response is decoded into, so that a
// response past MaxFiles is refused while it is decoded and not after: a
// "files" array of 64 MiB of small values would otherwise be held whole.
func responseNodes() int { return 4*maxFiles + responseNodeSlack }

// readFile reads one item of files.
func readFile(item any, m *Manifest, fail func(...string)) (File, bool) {
	object, ok := item.(map[string]any)
	if !ok {
		fail()
		return File{}, false
	}
	good := true
	for _, name := range sortedKeys(object) {
		if !fileMembers[name] {
			fail(name)
			good = false
		}
	}
	path, ok := object["path"].(string)
	switch {
	case !ok || checkPathForm(path) != nil:
		fail("path")
		good = false
	case !owned(m.Owns, path):
		fail("path")
		good = false
	}
	file := File{Path: path, Mode: ModeRegular}
	if value, present := object["mode"]; present {
		mode, ok := value.(string)
		if !ok || mode != ModeRegular && mode != ModeExecutable {
			fail("mode")
			good = false
		}
		file.Mode = mode
	}
	text, isText := object["text"]
	encoded, isBase64 := object["base64"]
	switch {
	case isText == isBase64:
		fail()
		good = false
	case isText:
		s, ok := text.(string)
		if !ok || len(s) > maxFileBytes || !strings.HasSuffix(s, "\n") || strings.Contains(s, "\r") {
			fail("text")
			good = false
		} else {
			file.Data = []byte(s)
		}
	default:
		data, ok := decodeBase64(encoded)
		if !ok {
			fail("base64")
			good = false
		} else {
			file.Data = data
		}
	}
	return file, good
}

// decodeBase64 reads RFC 4648 §4 base64 with padding, canonically: no line
// break or other character outside the alphabet, and no bits set past the
// data, at most MaxFileBytes once decoded.
func decodeBase64(value any) ([]byte, bool) {
	s, ok := value.(string)
	if !ok || len(s) > base64.StdEncoding.EncodedLen(maxFileBytes) || strings.ContainsAny(s, "\r\n") {
		return nil, false
	}
	data, err := base64.StdEncoding.Strict().DecodeString(s)
	if err != nil || len(data) > maxFileBytes {
		return nil, false
	}
	return data, true
}

func owned(owns []Glob, path string) bool {
	for _, glob := range owns {
		if glob.Match(path) {
			return true
		}
	}
	return false
}

// acceptedResponse says whether a response version is one this host accepts
// for a request version: listed, and not past the request's major and minor.
func acceptedResponse(response, request string) bool {
	listed := false
	for _, v := range responseVersions {
		listed = listed || v == response
	}
	if !listed {
		return false
	}
	responseMajor, responseMinor, ok1 := majorMinor(response)
	requestMajor, requestMinor, ok2 := majorMinor(request)
	if !ok1 || !ok2 {
		return false
	}
	if responseMajor != requestMajor {
		return false
	}
	return compareRelease([3]string{responseMajor, responseMinor, "0"}, [3]string{requestMajor, requestMinor, "0"}) <= 0
}
