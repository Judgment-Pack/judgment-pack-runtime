package emit

import (
	"encoding/base64"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// anyOwner owns every path, so that only the path rules decide.
func anyOwner(t *testing.T) *Manifest {
	// A bare "**" is refused; "*/**" owns every path of one or more segments.
	return mustManifest(t, manifestDoc([]string{"exit", "0"}, map[string]any{"owns": []any{"*/**"}}))
}

func TestResponseRefusesEachUnsafePath(t *testing.T) {
	m := anyOwner(t)
	for name, path := range map[string]string{
		"absolute":            "/etc/a",
		"parent segment":      "gen/../a",
		"dot segment":         "gen/./a",
		"empty segment":       "gen//a",
		"tilde segment":       "gen/~a",
		"trailing slash":      "gen/a/",
		"empty":               "",
		"NUL":                 "gen/a\x00b",
		"backslash":           `gen\a`,
		"UNC":                 `\\host\share\a`,
		"control":             "gen/a\x07",
		"C1 control":          "gen/a\u0085",
		"delete":              "gen/a\x7f",
		"drive letter":        "C:/a",
		"drive relative":      "c:a",
		"git directory":       ".git/config",
		"git directory below": "gen/.git/hooks/pre-commit",
		"git in another case": "gen/.GiT",
		"one past the length": strings.Repeat("a/", 256) + "b",
	} {
		t.Run(name, func(t *testing.T) {
			_, findings := ParseResponse([]byte(responseJSON(t, map[string]any{"path": path, "text": "x\n"})), "1.0.0", m)
			if got := codes(findings); !slices.Equal(got, []string{"EMIT-RESPONSE@/files/0/path"}) {
				t.Fatalf("%q: %v", path, got)
			}
		})
	}
	atLimit := strings.Repeat("a/", 255) + "bc"
	if len(atLimit) != MaxPathBytes {
		t.Fatalf("the path at the limit has %d bytes", len(atLimit))
	}
	if _, findings := ParseResponse([]byte(responseJSON(t, map[string]any{"path": atLimit, "text": "x\n"})), "1.0.0", m); findings != nil {
		t.Fatalf("a path of %d bytes: %v", MaxPathBytes, codes(findings))
	}
}

func TestResponseRefusesUnownedCollidingAndUnsortedPaths(t *testing.T) {
	m := mustManifest(t, manifestDoc([]string{"exit", "0"}, nil))
	file := func(path string) map[string]any { return map[string]any{"path": path, "text": "x\n"} }
	cases := map[string][]map[string]any{
		"not owned":               {file("src/a")},
		"case-fold collision":     {file("gen/README.md"), file("gen/readme.md")},
		"full case folding":       {file("gen/STRASSE"), file("gen/stra\u00dfe")},
		"normalization collision": {file("gen/caf\u00e9"), file("gen/cafe\u0301")},
		"unsorted":                {file("gen/b"), file("gen/a")},
		"duplicate":               {file("gen/a"), file("gen/a")},
		"a directory of another":  {file("gen/a"), file("gen/a/b")},
		"a folded directory":      {file("gen/A"), file("gen/a/b")},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			_, findings := ParseResponse([]byte(responseJSON(t, files...)), "1.0.0", m)
			want := "EMIT-RESPONSE@/files/" + map[bool]string{true: "0", false: "1"}[len(files) == 1] + "/path"
			if got := codes(findings); !slices.Contains(got, want) {
				t.Fatalf("got %v, want %s", got, want)
			}
		})
	}
	// "gen/a-x" sorts between "gen/a" and "gen/a/b".
	if _, findings := ParseResponse([]byte(responseJSON(t, file("gen/a"), file("gen/a-x"), file("gen/a/b"))), "1.0.0", m); !slices.Equal(codes(findings), []string{"EMIT-RESPONSE@/files/2/path"}) {
		t.Fatalf("a directory of a path two before: %v", codes(findings))
	}
	if _, findings := ParseResponse([]byte(responseJSON(t, file("gen/A"), file("gen/a-x/b"), file("gen/b"))), "1.0.0", m); findings != nil {
		t.Fatalf("distinct sorted paths: %v", codes(findings))
	}
}

func TestResponseRefusesEachMalformedPart(t *testing.T) {
	m := mustManifest(t, manifestDoc([]string{"exit", "0"}, nil))
	valid := `{"severity":"warning","code":"x-unused-role","location":{"document":"value","entry":{"kind":"queue","id":"orders"},"environment":"prod","pointer":"/retention"}}`
	cases := map[string]struct{ response, want string }{
		"not JSON":                 {`{"emitResponseVersion":`, ""},
		"invalid UTF-8":            {"{\"emitResponseVersion\":\"1.0.0\",\"files\":[],\"diagnostics\":[],\"x\":\"\xff\"}", ""},
		"lone surrogate":           {`{"emitResponseVersion":"1.0.0","files":[{"path":"gen/a","text":"\ud800\n"}],"diagnostics":[]}`, ""},
		"duplicate member":         {`{"emitResponseVersion":"1.0.0","files":[],"files":[],"diagnostics":[]}`, "/files"},
		"unknown member":           {`{"emitResponseVersion":"1.0.0","files":[],"diagnostics":[],"notes":[]}`, "/notes"},
		"no diagnostics":           {`{"emitResponseVersion":"1.0.0","files":[]}`, "/diagnostics"},
		"later minor":              {`{"emitResponseVersion":"1.1.0","files":[],"diagnostics":[]}`, "/emitResponseVersion"},
		"other major":              {`{"emitResponseVersion":"2.0.0","files":[],"diagnostics":[]}`, "/emitResponseVersion"},
		"text without line feed":   {`{"emitResponseVersion":"1.0.0","files":[{"path":"gen/a","text":"x"}],"diagnostics":[]}`, "/files/0/text"},
		"text with carriage":       {`{"emitResponseVersion":"1.0.0","files":[{"path":"gen/a","text":"x\r\n"}],"diagnostics":[]}`, "/files/0/text"},
		"text and base64":          {`{"emitResponseVersion":"1.0.0","files":[{"path":"gen/a","text":"x\n","base64":"eA=="}],"diagnostics":[]}`, "/files/0"},
		"neither text nor base64":  {`{"emitResponseVersion":"1.0.0","files":[{"path":"gen/a"}],"diagnostics":[]}`, "/files/0"},
		"base64 with a line break": {`{"emitResponseVersion":"1.0.0","files":[{"path":"gen/a","base64":"eA==\n"}],"diagnostics":[]}`, "/files/0/base64"},
		"base64 not canonical":     {`{"emitResponseVersion":"1.0.0","files":[{"path":"gen/a","base64":"eB=="}],"diagnostics":[]}`, "/files/0/base64"},
		"base64 unpadded":          {`{"emitResponseVersion":"1.0.0","files":[{"path":"gen/a","base64":"eA"}],"diagnostics":[]}`, "/files/0/base64"},
		"mode":                     {`{"emitResponseVersion":"1.0.0","files":[{"path":"gen/a","text":"x\n","mode":"0600"}],"diagnostics":[]}`, "/files/0/mode"},
		"file member":              {`{"emitResponseVersion":"1.0.0","files":[{"path":"gen/a","text":"x\n","owner":"root"}],"diagnostics":[]}`, "/files/0/owner"},
		"error diagnostic":         {`{"emitResponseVersion":"1.0.0","files":[],"diagnostics":[` + strings.Replace(valid, "warning", "error", 1) + `]}`, "/diagnostics/0"},
		"diagnostic message":       {`{"emitResponseVersion":"1.0.0","files":[],"diagnostics":[` + strings.Replace(valid, `"code"`, `"message":"m","code"`, 1) + `]}`, "/diagnostics/0/message"},
		"diagnostic code":          {`{"emitResponseVersion":"1.0.0","files":[],"diagnostics":[` + strings.Replace(valid, "x-unused-role", "REG-VALUE-TYPE", 1) + `]}`, "/diagnostics/0/code"},
		"diagnostic severity":      {`{"emitResponseVersion":"1.0.0","files":[],"diagnostics":[` + strings.Replace(valid, "warning", "info", 1) + `]}`, "/diagnostics/0/severity"},
		// RFC 0036's example location, {"kind", "id"}, is not of the
		// companion's §7.3 form, which governs.
		"location of the RFC example": {`{"emitResponseVersion":"1.0.0","files":[],"diagnostics":[{"severity":"warning","code":"x-a","location":{"kind":"queue","id":"orders"}}]}`, "/diagnostics/0/location"},
		"location with a value":       {`{"emitResponseVersion":"1.0.0","files":[],"diagnostics":[{"severity":"warning","code":"x-a","location":{"document":"value","pointer":"/a","value":"secret"}}]}`, "/diagnostics/0/location"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, findings := ParseResponse([]byte(c.response), "1.0.0", m)
			if got := codes(findings); !slices.Contains(got, "EMIT-RESPONSE@"+c.want) {
				t.Fatalf("got %v, want EMIT-RESPONSE at %q", got, c.want)
			}
		})
	}
	response, findings := ParseResponse([]byte(`{"emitResponseVersion":"1.0.0","files":[{"path":"gen/a.bin","base64":"AAEC","mode":"0755"}],"diagnostics":[`+valid+`]}`), "1.0.0", m)
	if findings != nil || len(response.Files) != 1 || string(response.Files[0].Data) != "\x00\x01\x02" || response.Files[0].Mode != ModeExecutable ||
		len(response.Diagnostics) != 1 || response.Diagnostics[0].Location.Entry.ID != "orders" {
		t.Fatalf("valid response: %+v %v", response, codes(findings))
	}
}

func TestResponseBoundsAtTheirLimitAndOnePast(t *testing.T) {
	m := mustManifest(t, manifestDoc([]string{"exit", "0"}, nil))
	defer func(files, bytes, response int) { maxFiles, maxFileBytes, maxResponseBytes = files, bytes, response }(maxFiles, maxFileBytes, maxResponseBytes)
	maxFiles, maxFileBytes = 3, 8
	files := func(n int) []map[string]any {
		var out []map[string]any
		for i := 0; i < n; i++ {
			out = append(out, map[string]any{"path": "gen/" + string(rune('a'+i)), "text": "x\n"})
		}
		return out
	}
	if _, findings := ParseResponse([]byte(responseJSON(t, files(3)...)), "1.0.0", m); findings != nil {
		t.Fatalf("files at the limit: %v", codes(findings))
	}
	if _, findings := ParseResponse([]byte(responseJSON(t, files(4)...)), "1.0.0", m); !slices.Equal(codes(findings), []string{"EMIT-RESPONSE@/files"}) {
		t.Fatalf("files one past: %v", codes(findings))
	}
	for _, c := range []struct {
		file map[string]any
		ok   bool
	}{
		{map[string]any{"path": "gen/a", "text": "1234567\n"}, true},
		{map[string]any{"path": "gen/a", "text": "12345678\n"}, false},
		{map[string]any{"path": "gen/a", "base64": base64.StdEncoding.EncodeToString([]byte("12345678"))}, true},
		{map[string]any{"path": "gen/a", "base64": base64.StdEncoding.EncodeToString([]byte("123456789"))}, false},
	} {
		_, findings := ParseResponse([]byte(responseJSON(t, c.file)), "1.0.0", m)
		if (findings == nil) != c.ok {
			t.Errorf("%v: %v", c.file, codes(findings))
		}
	}
	maxFiles, maxFileBytes = MaxFiles, MaxFileBytes
	response := responseJSON(t, map[string]any{"path": "gen/a", "text": "x\n"})
	maxResponseBytes = len(response)
	if _, findings := ParseResponse([]byte(response), "1.0.0", m); findings != nil {
		t.Fatalf("a response at the limit: %v", codes(findings))
	}
	maxResponseBytes = len(response) - 1
	if _, findings := ParseResponse([]byte(response), "1.0.0", m); !slices.Equal(codes(findings), []string{"EMIT-RESPONSE@"}) {
		t.Fatalf("a response one past: %v", codes(findings))
	}
}

// A "files" array of small values past MaxFiles is refused while the
// response is decoded, in memory that does not grow with the array; a
// response of MaxFiles full files and many diagnostics still fits the bound.
func TestEmitResponseRefusesTooManyFilesWhileDecoding(t *testing.T) {
	m := mustManifest(t, manifestDoc([]string{"exit", "0"}, nil))
	const zeros = 4 << 20 // 8 MiB of "0,", 4 million values
	data := []byte(`{"emitResponseVersion":"1.0.0","files":[` + strings.Repeat("0,", zeros-1) + `0],"diagnostics":[]}`)
	var findings []Diagnostic
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, findings = ParseResponse(data, "1.0.0", m)
	runtime.ReadMemStats(&after)
	if len(findings) != 1 || findings[0].Code != "EMIT-RESPONSE" || !strings.HasPrefix(findings[0].Location.Pointer, "/files") {
		t.Fatalf("findings: %v", codes(findings))
	}
	// The decoder allocates some 250 bytes a value: about 30 MiB up to the
	// bound, and some 1 GiB for the whole array.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 64<<20 {
		t.Fatalf("refusing %d values allocated %d MiB", zeros, allocated>>20)
	}

	var files, diagnostics []string
	for i := 0; i < MaxFiles; i++ {
		files = append(files, fmt.Sprintf(`{"path":"gen/%05d","text":"x\n","mode":"0644"}`, i))
	}
	for i := 0; i < 7000; i++ {
		diagnostics = append(diagnostics, `{"severity":"warning","code":"x-a","location":{"document":"value","pointer":"/a"}}`)
	}
	full := `{"emitResponseVersion":"1.0.0","files":[` + strings.Join(files, ",") + `],"diagnostics":[` + strings.Join(diagnostics, ",") + `]}`
	if response, findings := ParseResponse([]byte(full), "1.0.0", m); findings != nil || len(response.Files) != MaxFiles {
		t.Fatalf("%d full files and 7000 diagnostics: %v", MaxFiles, codes(findings))
	}
}
