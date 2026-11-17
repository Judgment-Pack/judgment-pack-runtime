package conformance

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
)

// The harness's hostile responses (check 3, RFC 0036 §5): crafted responses
// it substitutes for an emitter's, each breaking one rule of §3, to confirm
// that the host refuses each one. They go through emit.ParseResponse, the
// step emit.Run applies to an emitter's standard output, for a fixed manifest
// of its own that owns gen/**, so they are the same whatever the emitter
// under test is. Each is a row of its own, which passes when the host
// refuses the response with EMIT-RESPONSE at the pointer the hostile names.

// hostileManifestSource is the manifest the hostile responses answer.
const hostileManifestSource = `{"emitterManifestVersion":"1.0.0","id":"https://example.invalid/jpack-harness/hostile","version":"1.0.0",` +
	`"owner":"jpack-harness","reads":[],"requestVersions":["1.0.0"],"environments":"all","command":["jpack-harness-hostile"],"owns":["gen/**"]}`

// hostileRequestVersion is the request version the hostile responses answer.
const hostileRequestVersion = "1.0.0"

var hostileManifest = sync.OnceValue(func() *emit.Manifest {
	m, diagnostics := emit.ReadManifest([]byte(hostileManifestSource))
	if m == nil {
		panic("conformance: the hostile manifest is refused: " + diagnostics[0].Code)
	}
	return m
})

// hostile is one crafted response: its name, the pointer the host must
// refuse it at, and its bytes, made when its row runs.
type hostile struct {
	name     string
	pointer  string
	response func() []byte
}

// hostileFile is one file of a crafted response, as JSON.
func hostileFile(path string, members ...string) string {
	encoded, _ := json.Marshal(path)
	if len(members) == 0 {
		members = []string{`"text":"x\n"`}
	}
	return `{"path":` + string(encoded) + "," + strings.Join(members, ",") + `}`
}

func hostileResponse(files ...string) func() []byte {
	return func() []byte {
		return []byte(`{"emitResponseVersion":"1.0.0","files":[` + strings.Join(files, ",") + `],"diagnostics":[]}`)
	}
}

// hostiles are the crafted responses, in the order their rows are reported.
var hostiles = []hostile{
	{"path-traversal", "/files/0/path", hostileResponse(hostileFile("gen/../escape"))},
	{"absolute-path", "/files/0/path", hostileResponse(hostileFile("/gen/a"))},
	{"drive-letter", "/files/0/path", hostileResponse(hostileFile("C:/gen/a"))},
	{"backslash", "/files/0/path", hostileResponse(hostileFile(`gen\a`))},
	{"git-directory", "/files/0/path", hostileResponse(hostileFile("gen/.git/config"))},
	{"tilde-segment", "/files/0/path", hostileResponse(hostileFile("gen/~a"))},
	{"trailing-dot", "/files/0/path", hostileResponse(hostileFile("gen/a."))},
	{"device-name", "/files/0/path", hostileResponse(hostileFile("gen/nul.txt"))},
	{"path-too-long", "/files/0/path", hostileResponse(hostileFile("gen/" + strings.Repeat("a", emit.MaxPathBytes-3)))},
	{"outside-owns", "/files/0/path", hostileResponse(hostileFile("src/a"))},
	{"duplicate-path", "/files/1/path", hostileResponse(hostileFile("gen/a"), hostileFile("gen/a"))},
	{"case-folded-duplicate", "/files/1/path", hostileResponse(hostileFile("gen/A"), hostileFile("gen/a"))},
	{"directory-of-a-file", "/files/1/path", hostileResponse(hostileFile("gen/a"), hostileFile("gen/a/b"))},
	{"unsorted", "/files/1/path", hostileResponse(hostileFile("gen/b"), hostileFile("gen/a"))},
	{"unknown-member", "/files/0/size", hostileResponse(hostileFile("gen/a", `"text":"x\n"`, `"size":2`))},
	{"text-and-base64", "/files/0", hostileResponse(hostileFile("gen/a", `"text":"x\n"`, `"base64":"eAo="`))},
	{"text-carriage-return", "/files/0/text", hostileResponse(hostileFile("gen/a", `"text":"x\r\n"`))},
	{"text-no-final-newline", "/files/0/text", hostileResponse(hostileFile("gen/a", `"text":"x"`))},
	{"base64-malformed", "/files/0/base64", hostileResponse(hostileFile("gen/a", `"base64":"eA!="`))},
	{"base64-noncanonical", "/files/0/base64", hostileResponse(hostileFile("gen/a", `"base64":"QR=="`))},
	{"bad-mode", "/files/0/mode", hostileResponse(hostileFile("gen/a", `"text":"x\n"`, `"mode":"0777"`))},
	{"file-too-large", "/files/0/text", func() []byte {
		return hostileResponse(hostileFile("gen/a", `"text":"`+strings.Repeat("x", emit.MaxFileBytes)+`\n"`))()
	}},
	{"too-many-files", "/files", func() []byte {
		files := make([]string, emit.MaxFiles+1)
		for i := range files {
			files[i] = hostileFile("gen/" + strconv.Itoa(100000+i))
		}
		return hostileResponse(files...)()
	}},
	{"response-too-large", "", func() []byte {
		return append(hostileResponse(hostileFile("gen/a"))(), bytes.Repeat([]byte(" "), emit.MaxResponseBytes)...)
	}},
	{"not-utf8", "", func() []byte {
		return []byte("{\"emitResponseVersion\":\"1.0.0\",\"files\":[],\"diagnostics\":[],\"x\":\"\xff\"}")
	}},
	{"response-version-too-new", "/emitResponseVersion", func() []byte {
		return []byte(`{"emitResponseVersion":"1.1.0","files":[],"diagnostics":[]}`)
	}},
	{"diagnostics-missing", "/diagnostics", func() []byte { return []byte(`{"emitResponseVersion":"1.0.0","files":[]}`) }},
}

// hostileRows checks each hostile response against the host.
func hostileRows() []Row {
	m := hostileManifest()
	rows := make([]Row, 0, len(hostiles))
	for _, h := range hostiles {
		row := Row{Vector: "hostile/" + h.name, Check: checkPathSafety, Name: checkNames[checkPathSafety], Expected: "EMIT-RESPONSE"}
		response, findings := emit.ParseResponse(h.response(), hostileRequestVersion, m)
		refused := false
		for _, d := range findings {
			refused = refused || d.Code == "EMIT-RESPONSE" && d.Location.Pointer == h.pointer
		}
		switch {
		case len(findings) == 0 && response != nil:
			row.Status, row.Reason = StatusFail, reasonHostileAccept
		case !refused:
			row.Status, row.Reason, row.Diagnostics = StatusFail, reasonHostilePlace, bounded(findings)
		default:
			row.Status = StatusPass
		}
		rows = append(rows, row)
	}
	return rows
}
