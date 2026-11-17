package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	host "github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// canonical returns data in its RFC 8785 form, as the host writes a request.
func canonical(t *testing.T, data []byte) []byte {
	t.Helper()
	value, err := decodeStrict(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	out, err := jcs.EncodeDocument(value)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	return out
}

// js decodes a JSON text as the emitter does, numbers kept as their literals.
func js(t *testing.T, text string) any {
	t.Helper()
	value, err := decodeStrict([]byte(text))
	if err != nil {
		t.Fatalf("decode %s: %v", text, err)
	}
	return value
}

func readManifest(t *testing.T) *host.Manifest {
	t.Helper()
	data, err := os.ReadFile("emitter.json")
	if err != nil {
		t.Fatal(err)
	}
	m, diagnostics := host.ReadManifest(data)
	if m == nil {
		t.Fatalf("the manifest is refused: %v", diagnostics)
	}
	return m
}

type vector struct {
	name     string
	requests []host.Request
	result   *registry.Result  // nil for a request given whole
	failure  string            // the code an expected-failure vector names
	expected map[string][]byte // nil for an expected-failure vector
}

// readVectors reads vectors/ in the RFC 0036 §5 layout. A vector's request is
// formed from registry.json and kinds/ as the host forms it (`jpack emit`),
// with the manifest's reads and environments, or is request.json, given
// whole.
func readVectors(t *testing.T, m *host.Manifest) []vector {
	t.Helper()
	entries, err := os.ReadDir("vectors")
	if err != nil {
		t.Fatal(err)
	}
	var out []vector
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join("vectors", entry.Name())
		v := vector{name: entry.Name()}
		if _, err := os.Stat(filepath.Join(dir, "options.json")); err == nil {
			t.Fatalf("%s: options.json is not read by this test", v.name)
		}
		if data, err := os.ReadFile(filepath.Join(dir, "request.json")); err == nil {
			request := canonical(t, data)
			var given struct {
				Resolved []struct {
					Environment string `json:"environment"`
				} `json:"resolved"`
			}
			if err := json.Unmarshal(request, &given); err != nil || len(given.Resolved) != 1 {
				t.Fatalf("%s: request.json does not hold one resolved document", v.name)
			}
			v.requests = []host.Request{{Environment: given.Resolved[0].Environment, Version: requestVersion, Bytes: request}}
		} else {
			v.result, v.requests = formRequests(t, m, dir)
		}
		if data, err := os.ReadFile(filepath.Join(dir, "expected-failure.json")); err == nil {
			var failure struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(data, &failure); err != nil || string(canonical(t, data)) != `{"code":"`+failure.Code+`"}` {
				t.Fatalf("%s: expected-failure.json is %s", v.name, data)
			}
			v.failure = failure.Code
			out = append(out, v)
			continue
		}
		v.expected = map[string][]byte{}
		root := filepath.Join(dir, "expected")
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(p)
			v.expected[filepath.ToSlash(rel)] = data
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if len(out) < 6 {
		t.Fatalf("read %d vectors, want at least 6", len(out))
	}
	return out
}

// formRequests validates a vector's registry and forms its requests.
func formRequests(t *testing.T, m *host.Manifest, dir string) (*registry.Result, []host.Request) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	names, err := filepath.Glob(filepath.Join(dir, "kinds", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(names)
	var kinds []registry.Document
	for _, name := range names {
		kind, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, registry.Document{Data: kind})
	}
	result := registry.ValidateDocuments(registry.Document{Data: data}, kinds)
	if !result.Report.Valid {
		var codes []string
		for _, d := range result.Report.Diagnostics {
			codes = append(codes, d.Code+" "+d.Location.Pointer)
		}
		t.Fatalf("%s: the registry is not valid: %v", dir, codes)
	}
	requests, refused := host.FormRequests(m, host.Inputs{Registry: result})
	if len(refused) > 0 {
		t.Fatalf("%s: the host refuses the request: %v", dir, refused)
	}
	return result, requests
}

// respond runs the emitter on one request and reads its response as the host
// does (host.ParseResponse), so a location that does not resolve against the
// request, or a file the manifest does not own, fails the test.
func respond(t *testing.T, m *host.Manifest, request host.Request) (*host.Response, []host.Diagnostic, []byte) {
	t.Helper()
	out, err := emit(request.Bytes)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !bytes.Equal(canonical(t, out), out) {
		t.Fatalf("the response is not in canonical form")
	}
	response, findings := host.ParseResponse(out, request.Version, request.Bytes, m)
	if response == nil {
		t.Fatalf("the host cannot read the response: %v", findings)
	}
	return response, findings, out
}

// refusedWith checks that a response refuses with code alone: no files, every
// diagnostic an error of that code, and every location one the host
// resolves, so that each finding is the error diagnostic itself
// (/diagnostics/<i>) and none is about its location.
func refusedWith(t *testing.T, m *host.Manifest, request host.Request, code string) {
	t.Helper()
	response, findings, out := respond(t, m, request)
	var written struct {
		Files       []any `json:"files"`
		Diagnostics []any `json:"diagnostics"`
	}
	if err := json.Unmarshal(out, &written); err != nil {
		t.Fatal(err)
	}
	if len(written.Files) != 0 || len(written.Diagnostics) == 0 || len(response.Diagnostics) != len(written.Diagnostics) {
		t.Fatalf("files %d, diagnostics %d, read by the host %d: want no files and every diagnostic read",
			len(written.Files), len(written.Diagnostics), len(response.Diagnostics))
	}
	for i, d := range response.Diagnostics {
		if d.Code != code || d.Severity != host.SeverityError {
			t.Errorf("diagnostic %d is %s %s, want an error %s", i, d.Severity, d.Code, code)
		}
	}
	if len(findings) != len(response.Diagnostics) {
		t.Fatalf("findings %v", findings)
	}
	for i, f := range findings {
		if f.Code != "EMIT-RESPONSE" || f.Location.Pointer != "/diagnostics/"+strconv.Itoa(i) {
			t.Errorf("finding %d is %s at %s: a location the host does not resolve", i, f.Code, f.Location.Pointer)
		}
	}
}

func TestVectors(t *testing.T) {
	m := readManifest(t)
	for _, v := range readVectors(t, m) {
		t.Run(v.name, func(t *testing.T) {
			if v.failure != "" {
				if len(v.requests) != 1 {
					t.Fatalf("%d requests, want one", len(v.requests))
				}
				refusedWith(t, m, v.requests[0], v.failure)
				return
			}
			files := map[string][]byte{}
			for _, request := range v.requests {
				response, findings, _ := respond(t, m, request)
				if len(findings) > 0 {
					t.Fatalf("%s: the host refuses the response: %v", request.Environment, findings)
				}
				if len(response.Files) != 1 || response.Files[0].Path != "generated/constants/"+request.Environment+".ts" {
					t.Fatalf("%s: want the one file of the environment", request.Environment)
				}
				for _, f := range response.Files {
					if _, twice := files[f.Path]; twice {
						t.Fatalf("two runs write %s", f.Path)
					}
					files[f.Path] = f.Data
				}
			}
			paths := slices.Sorted(maps.Keys(files))
			want := slices.Sorted(maps.Keys(v.expected))
			if !slices.Equal(paths, want) {
				t.Fatalf("paths %v, want %v", paths, want)
			}
			for _, p := range paths {
				if !bytes.Equal(files[p], v.expected[p]) {
					t.Errorf("%s differs from expected/%s", p, p)
				}
			}
		})
	}
}

// lastToken is the last reference token of a pointer or pointer pattern,
// decoded (~2 is "*" in a pattern), or "" for the wildcard.
func lastToken(pointer string) string {
	token := pointer[strings.LastIndex(pointer, "/")+1:]
	if token == "*" {
		return ""
	}
	return strings.NewReplacer("~2", "*", "~1", "/", "~0", "~").Replace(token)
}

// TestNoSecretReachesTheOutput is the emitter's safeguard: for every
// environment of every vector formed from a registry, no output file, and no
// response, holds a run of 32 characters of any string at a secret position
// (all of it, when shorter), nor the name of any member at a secret
// position: the last token of each secret position the registry finds
// (registry.Result.SecretStrings, an oracle independent of the emitter's own
// reading of the patterns), and the literal last token of each of the
// declarations' secrets patterns.
func TestNoSecretReachesTheOutput(t *testing.T) {
	m := readManifest(t)
	counted := map[string]int{}
	for _, v := range readVectors(t, m) {
		if v.result == nil || v.failure != "" {
			continue
		}
		for _, request := range v.requests {
			secrets, err := v.result.SecretStrings(request.Environment, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, findings, out := respond(t, m, request)
			if len(findings) > 0 {
				t.Fatalf("%s %s: %v", v.name, request.Environment, findings)
			}
			outputs := []string{string(out)}
			for _, f := range response.Files {
				outputs = append(outputs, string(f.Data))
			}
			var names []string
			var declared struct {
				Declarations []struct {
					Secrets []string `json:"secrets"`
				} `json:"declarations"`
			}
			if err := json.Unmarshal(request.Bytes, &declared); err != nil {
				t.Fatal(err)
			}
			for _, d := range declared.Declarations {
				for _, pattern := range d.Secrets {
					names = append(names, lastToken(pattern))
				}
			}
			for _, s := range secrets {
				// The request holds the secret: the test is not vacuous.
				if !bytes.Contains(request.Bytes, []byte(s.Value)) {
					t.Fatalf("%s: the request does not hold the string at %s", v.name, s.Pointer)
				}
				names = append(names, lastToken(s.Pointer))
				for i := 0; i == 0 || i+32 <= len(s.Value); i++ {
					run := s.Value[i:min(len(s.Value), i+32)]
					for _, output := range outputs {
						if strings.Contains(output, run) {
							t.Errorf("%s %s: an output holds a run of the string at %s %s", v.name, request.Environment, s.Entry.ID, s.Pointer)
						}
					}
				}
			}
			for _, name := range names {
				if name == "" {
					continue
				}
				if !bytes.Contains(request.Bytes, []byte(`"`+name+`"`)) {
					t.Fatalf("%s: the request does not hold the member name %q", v.name, name)
				}
				for _, output := range outputs {
					if strings.Contains(output, name) {
						t.Errorf("%s %s: an output holds %q, the name of a member at a secret position", v.name, request.Environment, name)
					}
				}
			}
			counted[v.name] += len(secrets)
		}
	}
	// A member, each member of a map, and a member of each value of a map,
	// in two environments; a member of each item of an array.
	if counted["secret-omission"] != 8 || counted["array"] != 2 {
		t.Fatalf("secret positions checked: %v", counted)
	}
}

// TestRunWritesTheResponseAndNothingElse: a request the emitter refuses
// with diagnostics is answered like any other, with exit status 0.
func TestRunWritesTheResponseAndNothingElse(t *testing.T) {
	m := readManifest(t)
	for _, v := range readVectors(t, m) {
		for _, request := range v.requests {
			want, err := emit(request.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := run(bytes.NewReader(request.Bytes), &stdout, &stderr); code != 0 || !bytes.Equal(stdout.Bytes(), want) || stderr.Len() != 0 {
				t.Fatalf("%s: run = %d, %d bytes out (want %d), stderr %q", v.name, code, stdout.Len(), len(want), stderr.String())
			}
		}
	}
}

func TestRunRefusesAnOversizedRequest(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(bytes.NewReader(make([]byte, maxRequestBytes+1)), &stdout, &stderr); code != 1 || stdout.Len() != 0 || stderr.String() != string(errRead)+"\n" {
		t.Fatalf("run = %d, stdout %d bytes, stderr %q", code, stdout.Len(), stderr.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestRunReportsAFailedWrite(t *testing.T) {
	m := readManifest(t)
	request := readVectors(t, m)[0].requests[0].Bytes
	var stderr bytes.Buffer
	if code := run(bytes.NewReader(request), failingWriter{}, &stderr); code != 1 || stderr.String() != string(errWrite)+"\n" {
		t.Fatalf("run = %d, stderr %q", code, stderr.String())
	}
}

func TestRefuseNeverWritesALibraryError(t *testing.T) {
	var stderr bytes.Buffer
	if code := refuse(&stderr, errors.New(`a "quoted" request value`)); code != 1 || stderr.String() != string(errInternal)+"\n" {
		t.Fatalf("refuse = %d, %q", code, stderr.String())
	}
}

// TestEmitIsDeterministic: the same request gives the same bytes, and the
// order of the request's pins, entries and declarations does not change them.
func TestEmitIsDeterministic(t *testing.T) {
	m := readManifest(t)
	for _, v := range readVectors(t, m) {
		for _, request := range v.requests {
			first, err := emit(request.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			value := js(t, string(request.Bytes)).(map[string]any)
			doc := value["resolved"].([]any)[0].(map[string]any)
			slices.Reverse(doc["kinds"].([]any))
			slices.Reverse(doc["entries"].([]any))
			slices.Reverse(value["declarations"].([]any))
			reordered, err := jcs.EncodeDocument(value)
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range [][]byte{request.Bytes, reordered} {
				again, err := emit(input)
				if err != nil || !bytes.Equal(first, again) {
					t.Fatalf("%s %s: a second run answered differently", v.name, request.Environment)
				}
			}
		}
	}
}

func TestManifest(t *testing.T) {
	m := readManifest(t)
	if m.ID != emitterID || m.Version != emitterVersion || m.Input != host.InputResolved || m.Environments != host.EnvironmentsEach {
		t.Fatalf("manifest %s %s %s %s", m.ID, m.Version, m.Input, m.Environments)
	}
	data, err := os.ReadFile("emitter.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		RequestVersions []string `json:"requestVersions"`
		Owns            []string `json:"owns"`
		Options         struct {
			Properties map[string]struct {
				Default string `json:"default"`
			} `json:"properties"`
		} `json:"options"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(manifest.RequestVersions, []string{requestVersion}) ||
		manifest.Options.Properties["directory"].Default != defaultDirectory ||
		!slices.Equal(manifest.Owns, []string{defaultDirectory + "/*.ts"}) {
		t.Fatalf("manifest %+v", manifest)
	}
}

func TestManifestPatternsMatchAsTheProgramDoes(t *testing.T) {
	data, err := os.ReadFile("emitter.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Options struct {
			Properties map[string]struct {
				Pattern string `json:"pattern"`
				Not     struct {
					Pattern string `json:"pattern"`
				} `json:"not"`
			} `json:"properties"`
		} `json:"options"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	compile := func(source string) *registry.Pattern {
		t.Helper()
		compiled, err := registry.CompilePattern(source)
		if err != nil {
			t.Fatalf("%q: %v", source, err)
		}
		return compiled
	}
	dir := compile(manifest.Options.Properties["directory"].Pattern)
	notDir := compile(manifest.Options.Properties["directory"].Not.Pattern)
	for _, s := range []string{"generated/constants", "a", "a.b/c-d/e_", "", "/a", "a/", "a//b", ".", "..", "a/..", ".git",
		"a/.git", "a.", "-a", "a/b.", "a\\b", "a b", "a\n", "_a", "a/_b", "node_modules", "a/node_modules", "node_modules/a",
		"Node_Modules/a", "a/NODE_MODULES/b", "node_modules2", "my_node_modules", "vendor", "a/Vendor", "VENDOR/a", "vendors",
		"testdata", "a/TestData/b", "xtestdata"} {
		manifest := dir.MatchString(s) && !notDir.MatchString(s)
		program := directoryPattern.MatchString(s) && !ignoredPattern.MatchString(s)
		if manifest != program {
			t.Errorf("directory: the manifest and the program differ on %q", s)
		}
	}
}

// A fixture is a resolved-form request for one environment, built from
// declarations whose pins are computed.
type fixture struct {
	environment  string
	options      any
	declarations []any
	pins         []any
	entries      []any
}

func declaration(t *testing.T, kind, schema string, secrets ...string) map[string]any {
	t.Helper()
	d := map[string]any{
		"kindFormatVersion": "1.0.0", "id": "https://example.org/kinds/" + kind, "version": "1.0.0",
		"kind": kind, "owner": "platform", "schema": js(t, schema),
	}
	if len(secrets) > 0 {
		items := make([]any, len(secrets))
		for i, s := range secrets {
			items[i] = s
		}
		d["secrets"] = items
	}
	return d
}

func pinOf(t *testing.T, d map[string]any) map[string]any {
	t.Helper()
	digest, ok := digestOf(d)
	if !ok {
		t.Fatal("digest")
	}
	return map[string]any{"kind": d["kind"], "id": d["id"], "version": d["version"], "digest": digest}
}

func entryOf(t *testing.T, kind, id, value string) map[string]any {
	return map[string]any{"kind": kind, "id": id, "owner": "platform", "value": js(t, value)}
}

// fixtureOf pins each declaration and holds the entries.
func fixtureOf(t *testing.T, declarations []map[string]any, entries ...map[string]any) *fixture {
	f := &fixture{environment: "dev", options: map[string]any{}}
	for _, d := range declarations {
		f.declarations = append(f.declarations, d)
		f.pins = append(f.pins, pinOf(t, d))
	}
	for _, e := range entries {
		f.entries = append(f.entries, e)
	}
	return f
}

func (f *fixture) request(t *testing.T) host.Request {
	t.Helper()
	declarations, pins, entries := f.declarations, f.pins, f.entries
	for _, list := range []*[]any{&declarations, &pins, &entries} {
		if *list == nil {
			*list = []any{}
		}
	}
	data, err := jcs.EncodeDocument(map[string]any{
		"emitRequestVersion": requestVersion,
		"emitter":            map[string]any{"id": emitterID, "version": emitterVersion},
		"options":            f.options,
		"declarations":       declarations,
		"resolved": []any{map[string]any{
			"resolvedFormatVersion": resolvedVersion,
			"registry": map[string]any{"id": "https://example.org/registries/fixture", "version": "1.0.0",
				"digest": "sha256:" + strings.Repeat("ab", 32)},
			"environment": f.environment,
			"kinds":       pins,
			"entries":     entries,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return host.Request{Environment: f.environment, Version: requestVersion, Bytes: data}
}

// generated runs the emitter on a fixture the test expects it to accept,
// and returns the file's text.
func generated(t *testing.T, m *host.Manifest, f *fixture) string {
	t.Helper()
	response, findings, _ := respond(t, m, f.request(t))
	if len(findings) > 0 || len(response.Files) != 1 {
		t.Fatalf("the response is refused: %v", findings)
	}
	return string(response.Files[0].Data)
}

const objectSchema = `{"type":"object","properties":{"host":{"type":"string"},"dbPassword":{"type":"string"}}}`

// TestDeclarationsAreCheckedAgainstTheirPins: every pin has a declaration
// whose RFC 8785 SHA-256 is its digest and whose id and version are its, and
// every declaration has a pin. Each refusal has its own case.
func TestDeclarationsAreCheckedAgainstTheirPins(t *testing.T) {
	m := readManifest(t)
	database := func() map[string]any { return declaration(t, "database", objectSchema, "/dbPassword") }
	value := entryOf(t, "database", "orders", `{"host":"h","dbPassword":"secret:gcp-sm:projects/example-dev/secrets/s/versions/1"}`)
	cases := []struct {
		name   string
		change func(f *fixture)
		code   string
	}{
		{"no declaration", func(f *fixture) { f.declarations = nil }, codeDeclarationMissing},
		{"a declaration changed after it was pinned", func(f *fixture) {
			delete(f.declarations[0].(map[string]any), "secrets")
		}, codeDeclarationMismatch},
		{"a declaration with a member added", func(f *fixture) {
			f.declarations[0].(map[string]any)["description"] = "added"
		}, codeDeclarationMismatch},
		{"a pin of another digest", func(f *fixture) {
			f.pins[0].(map[string]any)["digest"] = "sha256:" + strings.Repeat("0", 64)
		}, codeDeclarationMismatch},
		{"a pin of another id, with the declaration's digest", func(f *fixture) {
			f.pins[0].(map[string]any)["id"] = "https://example.org/kinds/other"
		}, codeDeclarationMismatch},
		{"a pin of another version, with the declaration's digest", func(f *fixture) {
			f.pins[0].(map[string]any)["version"] = "1.0.1"
		}, codeDeclarationMismatch},
		{"a declaration with no pin", func(f *fixture) {
			f.declarations = append(f.declarations, declaration(t, "queue", `{"type":"object"}`))
		}, codeDeclarationUnpinned},
		{"two declarations of one kind", func(f *fixture) {
			f.declarations = append(f.declarations, database())
		}, codeDeclarationForm},
		{"a declaration that is not an object", func(f *fixture) {
			f.declarations = append(f.declarations, "database")
		}, codeDeclarationForm},
		{"a declaration with no kind", func(f *fixture) {
			f.declarations = append(f.declarations, map[string]any{"id": "x"})
		}, codeDeclarationForm},
	}
	// Only each case's change refuses: the unchanged fixture is accepted.
	generated(t, m, fixtureOf(t, []map[string]any{database()}, value))
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := fixtureOf(t, []map[string]any{database()}, value)
			c.change(f)
			refusedWith(t, m, f.request(t), c.code)
		})
	}
}

// TestDeclarationFormIsRefused: a declaration whose digest is its pin's but
// that this emitter cannot read fails closed.
func TestDeclarationFormIsRefused(t *testing.T) {
	m := readManifest(t)
	cases := map[string]func(d map[string]any){
		"another kindFormatVersion": func(d map[string]any) { d["kindFormatVersion"] = "2.0.0" },
		"a member the companion does not list": func(d map[string]any) {
			d["redact"] = []any{"/host"}
		},
		"a schema that is not an object or a boolean": func(d map[string]any) { d["schema"] = "object" },
		"$defs that is not an object":                 func(d map[string]any) { d["schema"].(map[string]any)["$defs"] = []any{} },
		"secrets that is not an array":                func(d map[string]any) { d["secrets"] = "/dbPassword" },
		"a secrets pattern that is not a pointer":     func(d map[string]any) { d["secrets"] = []any{"dbPassword"} },
		"a secrets pattern with a bad escape":         func(d map[string]any) { d["secrets"] = []any{"/db~3Password"} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			d := declaration(t, "database", objectSchema, "/dbPassword")
			change(d)
			refusedWith(t, m, fixtureOf(t, []map[string]any{d}).request(t), codeDeclarationForm)
		})
	}
}

// TestSchemaRefusals: a schema this emitter does not generate is refused,
// and never generated as something else.
func TestSchemaRefusals(t *testing.T) {
	m := readManifest(t)
	cases := []struct {
		name, schema string
		secrets      []string
		code         string
	}{
		{"not", `{"type":"object","properties":{"mode":{"not":{"const":"off"}}}}`, nil, codeSchemaUnsupported},
		{"not at the root", `{"not":{"type":"null"}}`, nil, codeSchemaUnsupported},
		{"oneOf", `{"oneOf":[{"type":"string"}]}`, nil, codeSchemaUnsupported},
		{"patternProperties", `{"type":"object","patternProperties":{"^a":{"type":"string"}}}`, nil, codeSchemaUnsupported},
		{"$defs below the root", `{"type":"object","properties":{"a":{"$defs":{}}}}`, nil, codeSchemaUnsupported},
		{"a $ref outside $defs", `{"$ref":"#/properties/a"}`, nil, codeSchemaUnsupported},
		{"a $ref to no definition", `{"$ref":"#/$defs/missing","$defs":{}}`, nil, codeSchemaUnsupported},
		{"a cyclic $ref", `{"$ref":"#/$defs/a","$defs":{"a":{"type":"object","properties":{"b":{"$ref":"#/$defs/a"}}}}}`, nil, codeSchemaUnsupported},
		{"an items array", `{"type":"array","items":[{"type":"string"}]}`, nil, codeSchemaUnsupported},
		{"an unknown type", `{"type":"decimal"}`, nil, codeSchemaUnsupported},
		{"an empty type list", `{"type":[]}`, nil, codeSchemaUnsupported},
		{"an empty enum", `{"enum":[]}`, nil, codeSchemaUnsupported},
		{"too many branches", `{"anyOf":[` + strings.Repeat(`true,`, maxBranches) + `true]}`, nil, codeSchemaUnsupported},
		{"a secret below an object const", `{"type":"object","properties":{"a":{"const":{"b":"x"}}}}`, []string{"/a/b"}, codeSecretLiteral},
		{"a secret below an array enum", `{"type":"object","properties":{"a":{"enum":[["x"],["y"]]}}}`, []string{"/a/0"}, codeSecretLiteral},
		// A secret at a member the properties do not declare is typed by the
		// index signature, and a literal there could be the secret.
		{"a secret undeclared member, a const index", `{"type":"object","properties":{"host":{"type":"string"}},"additionalProperties":{"type":"string","const":"secret:gcp-sm:projects/example-dev/secrets/p/versions/3"}}`, []string{"/password"}, codeSecretLiteral},
		{"a secret undeclared member, an enum index", `{"type":"object","additionalProperties":{"enum":["a","b"]}}`, []string{"/password"}, codeSecretLiteral},
		{"a secret undeclared member, an index through a $ref", `{"type":"object","additionalProperties":{"$ref":"#/$defs/v"},"$defs":{"v":{"const":"a"}}}`, []string{"/password"}, codeSecretLiteral},
		{"a secret undeclared member, a literal deep in the index", `{"type":"object","additionalProperties":{"type":["string","array"],"items":{"anyOf":[{"type":"number"},{"const":"a"}]}}}`, []string{"/password"}, codeSecretLiteral},
		{"a secret undeclared member, a declared member's literal in the index", `{"type":"object","properties":{"mode":{"const":"fast"}},"additionalProperties":{"type":"string"}}`, []string{"/password"}, codeSecretLiteral},
		{"a secret undeclared member in an anyOf branch", `{"anyOf":[{"type":"object","properties":{"password":{"type":"string"}}},{"type":"object","additionalProperties":{"const":"a"}}]}`, []string{"/password"}, codeSecretLiteral},
		{"a secret undeclared member in an allOf branch", `{"allOf":[{"type":"object"},{"type":"object","additionalProperties":{"anyOf":[{"type":"string"},{"const":"a"}]}}]}`, []string{"/password"}, codeSecretLiteral},
		{"a secret undeclared member of an array item", `{"type":"object","properties":{"list":{"type":"array","items":{"type":"object","additionalProperties":{"enum":["a"]}}}}}`, []string{"/list/*/password"}, codeSecretLiteral},
		{"a secret undeclared member below an undeclared one", `{"type":"object","additionalProperties":{"type":"object","additionalProperties":{"const":"a"}}}`, []string{"/*/password"}, codeSecretLiteral},
		{"a secret array item", `{"type":"object","properties":{"keys":{"type":"array","items":{"type":"string"}}}}`, []string{"/keys/*"}, codeSecretInArray},
		{"a secret array item by index", `{"type":"object","properties":{"keys":{"type":"array","items":{"type":"string"}}}}`, []string{"/keys/0"}, codeSecretInArray},
		{"a secret that may be an array item", `{"type":"object","properties":{"any":true}}`, []string{"/any/*"}, codeSecretInArray},
		{"a secret that may be an array item, by index", `{"type":"object"}`, []string{"/list/3"}, codeSecretInArray},
		{"a property __proto__", `{"type":"object","properties":{"__proto__":{"type":"string"}}}`, nil, codeMemberProto},
		{"a const holding __proto__", `{"const":{"a":{"__proto__":1}}}`, nil, codeMemberProto},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := declaration(t, "rule", c.schema, c.secrets...)
			refusedWith(t, m, fixtureOf(t, []map[string]any{d}).request(t), c.code)
		})
	}
}

// TestSchemaVisitsAreBounded: a schema whose $refs expand past the bound is
// refused, not written out.
func TestSchemaVisitsAreBounded(t *testing.T) {
	m := readManifest(t)
	defs := []string{`"d0":{"type":"string"}`}
	for i := 1; i <= 13; i++ {
		ref := `{"$ref":"#/$defs/d` + strconv.Itoa(i-1) + `"}`
		defs = append(defs, `"d`+strconv.Itoa(i)+`":{"type":"object","properties":{"a":`+ref+`,"b":`+ref+`}}`)
	}
	schema := `{"$ref":"#/$defs/d13","$defs":{` + strings.Join(defs, ",") + `}}`
	refusedWith(t, m, fixtureOf(t, []map[string]any{declaration(t, "rule", schema)}).request(t), codeSchemaUnsupported)
}

// TestSharedTypesAreWrittenWithinTheBound: a member's type is shared with
// the index signature's union, so a chain of $refs within the visit bound
// writes out to 2^n types. The writer stops at the file bound and the kind
// is refused with x-ts-bounds, quickly, not after walking the whole tree.
func TestSharedTypesAreWrittenWithinTheBound(t *testing.T) {
	m := readManifest(t)
	const n = 60
	defs := []string{`"d` + strconv.Itoa(n) + `":{"type":"string"}`}
	for i := 0; i < n; i++ {
		ref := `{"$ref":"#/$defs/d` + strconv.Itoa(i+1) + `"}`
		defs = append(defs, `"d`+strconv.Itoa(i)+`":{"type":"object","properties":{"p":`+ref+`},"additionalProperties":{"type":"string"}}`)
	}
	schema := `{"$ref":"#/$defs/d0","$defs":{` + strings.Join(defs, ",") + `}}`
	request := fixtureOf(t, []map[string]any{declaration(t, "rule", schema)}).request(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		refusedWith(t, m, request, codeBounds)
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("the shared types were walked past the file bound")
	}
}

// TestNamesAreRefusedNeverRenamed: a kind whose names are reserved, or that
// another kind or the generated code already holds, is refused.
func TestNamesAreRefusedNeverRenamed(t *testing.T) {
	m := readManifest(t)
	cases := []struct {
		kinds []string
		code  string
	}{
		{[]string{"delete"}, codeNameReserved},
		{[]string{"class"}, codeNameReserved},
		{[]string{"await"}, codeNameReserved},
		{[]string{"let"}, codeNameReserved},
		{[]string{"eval"}, codeNameReserved},
		{[]string{"arguments"}, codeNameReserved},
		{[]string{"undefined"}, codeNameReserved},
		{[]string{"default"}, codeNameReserved},
		{[]string{"json-value"}, codeNameReserved},
		{[]string{"record"}, codeNameReserved},
		{[]string{"readonly"}, codeNameReserved},
		{[]string{"db-role", "db-role-entries"}, codeNameCollision},
		{[]string{"a-1", "a1"}, codeNameCollision},
		{[]string{"api-key", "api--key"}, codeNameCollision},
		{[]string{"json-value-x", "x"}, ""},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.kinds, "+"), func(t *testing.T) {
			var declarations []map[string]any
			for _, kind := range c.kinds {
				declarations = append(declarations, declaration(t, kind, `{"type":"object"}`))
			}
			f := fixtureOf(t, declarations)
			if c.code == "" {
				generated(t, m, f)
				return
			}
			refusedWith(t, m, f.request(t), c.code)
		})
	}
}

// TestEveryKindNameGivesIdentifiers: any kind name the grammar admits gives
// identifiers of the form, so no other text becomes a name.
func TestEveryKindNameGivesIdentifiers(t *testing.T) {
	for _, kind := range []string{"a", "a0", "a-0", "a--b", "a-", "z9-9z", strings.Repeat("a-", 31) + "b"} {
		if !kindPattern.MatchString(kind) {
			t.Fatalf("%q is not a kind name", kind)
		}
		n := namesOf(kind)
		for _, name := range []string{n.typeName, n.constName, n.entriesName} {
			if !identifierPattern.MatchString(name) {
				t.Errorf("%q gives %q", kind, name)
			}
		}
	}
}

// TestProtoIsRefused: a member or an entry id __proto__, which an object
// literal reads as its prototype, is refused, never written, even quoted.
func TestProtoIsRefused(t *testing.T) {
	m := readManifest(t)
	d := declaration(t, "flags", `{"type":"object"}`)
	for name, e := range map[string]map[string]any{
		"a member":         entryOf(t, "flags", "main", `{"__proto__":{"polluted":true}}`),
		"a nested member":  entryOf(t, "flags", "main", `{"a":[{"__proto__":1}]}`),
		"an entry id":      entryOf(t, "flags", "__proto__", `{"a":1}`),
		"a member deep in": entryOf(t, "flags", "main", `{"a":{"b":{"__proto__":null}}}`),
	} {
		t.Run(name, func(t *testing.T) {
			refusedWith(t, m, fixtureOf(t, []map[string]any{d}, e).request(t), codeMemberProto)
		})
	}
	// A secret member __proto__ is left out, so it is no refusal.
	secret := declaration(t, "flags", `{"type":"object"}`, "/__proto__")
	text := generated(t, m, fixtureOf(t, []map[string]any{secret},
		entryOf(t, "flags", "main", `{"__proto__":"secret:gcp-sm:projects/example-dev/secrets/s/versions/1","a":1}`)))
	if strings.Contains(text, "__proto__") || strings.Contains(text, "secret:") {
		t.Fatalf("the secret member is written:\n%s", text)
	}
}

// TestDeviceNameEnvironmentIsRefused: the host refuses a path whose name is
// a Windows device, so the emitter refuses it first, with a diagnostic.
// TestPrototypeMemberNamesAreRefused: a schema property named after a member
// of Object.prototype is refused, optional or required, since an entry
// without the optional one reads a function from the prototype and the file
// does not compile. At a secret position the property is left out, so it is
// no refusal, and a value's or a const's member of such a name is written:
// the object holds it.
func TestPrototypeMemberNamesAreRefused(t *testing.T) {
	m := readManifest(t)
	names := []string{"constructor", "toString", "toLocaleString", "valueOf", "hasOwnProperty", "isPrototypeOf",
		"propertyIsEnumerable", "__defineGetter__", "__defineSetter__", "__lookupGetter__", "__lookupSetter__"}
	for _, name := range names {
		for _, required := range []string{`[]`, `["` + name + `"]`} {
			t.Run(name+" "+required, func(t *testing.T) {
				d := declaration(t, "flags", `{"type":"object","properties":{"n":{"type":"string"},"`+name+`":{"type":"number"}},"required":`+required+`}`)
				refusedWith(t, m, fixtureOf(t, []map[string]any{d}, entryOf(t, "flags", "main", `{"n":"x"}`)).request(t), codeMemberProto)
			})
		}
	}
	t.Run("at a secret position", func(t *testing.T) {
		d := declaration(t, "flags", `{"type":"object","properties":{"n":{"type":"string"},"valueOf":{"type":"string"}}}`, "/valueOf")
		text := generated(t, m, fixtureOf(t, []map[string]any{d}, entryOf(t, "flags", "main", `{"n":"x"}`)))
		if strings.Contains(text, "valueOf") {
			t.Errorf("the secret member is written:\n%s", text)
		}
	})
	t.Run("in a value and a const", func(t *testing.T) {
		d := declaration(t, "flags", `{"type":"object","properties":{"c":{"const":{"toString":1}}}}`)
		text := generated(t, m, fixtureOf(t, []map[string]any{d}, entryOf(t, "flags", "main", `{"c":{"toString":1},"constructor":2}`)))
		if strings.Count(text, `"toString": 1`) != 2 || !strings.Contains(text, `"constructor": 2`) {
			t.Errorf("the members are not written:\n%s", text)
		}
	})
}

func TestDeviceNameEnvironmentIsRefused(t *testing.T) {
	m := readManifest(t)
	for _, environment := range []string{"con", "nul", "com1", "lpt9"} {
		f := fixtureOf(t, nil)
		f.environment = environment
		refusedWith(t, m, f.request(t), codePath)
	}
	for _, environment := range []string{"com0", "console", "lpt"} {
		f := fixtureOf(t, nil)
		f.environment = environment
		generated(t, m, f)
	}
}

// TestUnreadableRequestsAreRefused: a request that is not of the resolved
// form for one environment, or whose options are not this emitter's, is
// refused with a fixed line and no response.
func TestUnreadableRequestsAreRefused(t *testing.T) {
	d := declaration(t, "database", objectSchema)
	base := func() map[string]any {
		return js(t, string(fixtureOf(t, []map[string]any{d}, entryOf(t, "database", "a", `{"host":"h"}`)).request(t).Bytes)).(map[string]any)
	}
	resolvedDoc := func(r map[string]any) map[string]any { return r["resolved"].([]any)[0].(map[string]any) }
	cases := []struct {
		name   string
		change func(r map[string]any)
		want   refusal
	}{
		{"another request version", func(r map[string]any) { r["emitRequestVersion"] = "1.1.0" }, errRequest},
		{"a member added", func(r map[string]any) { r["extra"] = true }, errRequest},
		{"no declarations", func(r map[string]any) { delete(r, "declarations") }, errRequest},
		{"declarations not an array", func(r map[string]any) { r["declarations"] = map[string]any{} }, errRequest},
		{"two resolved documents", func(r map[string]any) { r["resolved"] = append(r["resolved"].([]any), resolvedDoc(r)) }, errRequest},
		{"no resolved document", func(r map[string]any) { r["resolved"] = []any{} }, errRequest},
		{"facts contracts", func(r map[string]any) { delete(r, "resolved"); r["factsContracts"] = []any{} }, errRequest},
		{"another resolved version", func(r map[string]any) { resolvedDoc(r)["resolvedFormatVersion"] = "2.0.0" }, errRequest},
		{"an environment outside the grammar", func(r map[string]any) { resolvedDoc(r)["environment"] = "Dev" }, errRequest},
		{"a pin of a kind outside the grammar", func(r map[string]any) {
			resolvedDoc(r)["kinds"].([]any)[0].(map[string]any)["kind"] = "Database"
		}, errRequest},
		{"two pins of one kind", func(r map[string]any) {
			doc := resolvedDoc(r)
			doc["kinds"] = append(doc["kinds"].([]any), doc["kinds"].([]any)[0])
		}, errRequest},
		{"an entry of a kind not pinned", func(r map[string]any) {
			resolvedDoc(r)["entries"].([]any)[0].(map[string]any)["kind"] = "queue"
		}, errRequest},
		{"an entry twice", func(r map[string]any) {
			doc := resolvedDoc(r)
			doc["entries"] = append(doc["entries"].([]any), doc["entries"].([]any)[0])
		}, errRequest},
		{"an unknown option", func(r map[string]any) { r["options"] = map[string]any{"package": "p"} }, errOptions},
		{"a directory under node_modules", func(r map[string]any) { r["options"] = map[string]any{"directory": "web/node_modules/c"} }, errOptions},
		{"a directory under vendor", func(r map[string]any) { r["options"] = map[string]any{"directory": "Vendor/c"} }, errOptions},
		{"a directory that climbs", func(r map[string]any) { r["options"] = map[string]any{"directory": "a/../b"} }, errOptions},
		{"a hidden directory", func(r map[string]any) { r["options"] = map[string]any{"directory": ".c"} }, errOptions},
		{"a directory segment past its bound", func(r map[string]any) {
			r["options"] = map[string]any{"directory": strings.Repeat("a", maxSegmentBytes+1)}
		}, errOptions},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := base()
			c.change(r)
			data, err := jcs.EncodeDocument(r)
			if err != nil {
				t.Fatal(err)
			}
			out, err := emit(data)
			if err != c.want || out != nil {
				t.Fatalf("emit = %d bytes, %v; want %v", len(out), err, c.want)
			}
			var stdout, stderr bytes.Buffer
			if code := run(bytes.NewReader(data), &stdout, &stderr); code != 1 || stdout.Len() != 0 || stderr.String() != string(c.want)+"\n" {
				t.Fatalf("run = %d, stdout %d bytes, stderr %q", code, stdout.Len(), stderr.String())
			}
		})
	}
	for name, data := range map[string]string{
		"not JSON":             `{`,
		"a repeated member":    `{"a":1,"a":2}`,
		"a lone surrogate":     `{"a":"\ud800"}`,
		"invalid UTF-8":        "{\"a\":\"\xff\"}",
		"a value after it":     `{} {}`,
		"an array, not object": `[]`,
	} {
		if out, err := emit([]byte(data)); err != errRequest || out != nil {
			t.Errorf("%s: emit = %v, want %v", name, err, errRequest)
		}
	}
}

// TestDepthBound: a resolved document of 32 containers, the registry's
// bound, is read; one of 33 is refused before it is read further.
func TestDepthBound(t *testing.T) {
	m := readManifest(t)
	d := declaration(t, "deep", `true`)
	nested := func(containers int) string {
		return strings.Repeat("[", containers) + "1" + strings.Repeat("]", containers)
	}
	// The document, its entries and the entry are three containers.
	f := fixtureOf(t, []map[string]any{d}, entryOf(t, "deep", "a", nested(32-3)))
	generated(t, m, f)
	f = fixtureOf(t, []map[string]any{d}, entryOf(t, "deep", "a", nested(32-3+1)))
	if out, err := emit(f.request(t).Bytes); err != errRequest || out != nil {
		t.Fatalf("emit = %v, want %v", err, errRequest)
	}
}

// TestJSString: every string literal is ASCII, holds no line terminator,
// and is a JSON string literal of the same string, since the escapes it
// uses are common to both languages.
func TestJSString(t *testing.T) {
	for _, s := range []string{"", "plain", `a"b`, `a\b`, "a\u2028b", "a\u2029b", "\x00\x01\x1f\x7f", "\b\t\n\f\r",
		"caf\u00e9", "\U0001F6D2", "\ufeff", "*/ </script> ${x} `", "\u0085", "\ud7ff\ue000\uffff"} {
		literal := jsString(s)
		for i := 0; i < len(literal); i++ {
			if c := literal[i]; c < 0x20 || c >= 0x7f {
				t.Errorf("%q: byte %#x in %s", s, c, literal)
			}
		}
		var back string
		if err := json.Unmarshal([]byte(literal), &back); err != nil || back != s {
			t.Errorf("%q: %s reads as %q, %v", s, literal, back, err)
		}
	}
	for s, want := range map[string]string{"a\u2028b": `"a\u2028b"`, "\U0001F6D2": `"\ud83d\uded2"`, "\x7f": `"\u007f"`} {
		if got := jsString(s); got != want {
			t.Errorf("%q: %s, want %s", s, got, want)
		}
	}
}

// TestNumbersAreWrittenAsGiven: a number is its JSON literal, which is a
// JavaScript one, never re-formatted through a float.
func TestNumbersAreWrittenAsGiven(t *testing.T) {
	d := declaration(t, "limits", `{"type":"object","additionalProperties":{"type":"number"}}`)
	request := fixtureOf(t, []map[string]any{d}, entryOf(t, "limits", "a", `"VALUE"`)).request(t)
	// The host's request is canonical; one given otherwise is written as it
	// is given, too.
	request.Bytes = bytes.Replace(request.Bytes, []byte(`"VALUE"`),
		[]byte(`{"big":9007199254740993,"small":1e-7,"neg":-0.5,"exp":1E+30,"zero":-0}`), 1)
	out, err := emit(request.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Files []struct {
			Text string `json:"text"`
		} `json:"files"`
	}
	if err := json.Unmarshal(out, &response); err != nil || len(response.Files) != 1 {
		t.Fatalf("response %s", out)
	}
	text := response.Files[0].Text
	for _, want := range []string{`"big": 9007199254740993,`, `"small": 1e-7,`, `"neg": -0.5,`, `"exp": 1E+30,`, `"zero": -0,`} {
		if !strings.Contains(text, want) {
			t.Errorf("the file does not hold %s:\n%s", want, text)
		}
	}
}

// TestSecretsAreLeftOutOfTypeAndValue: each form of secret position, in a
// fixture: a member, every member of a map, a member of each value of a
// map, of each array item, and below a $ref used at two positions, one of
// them secret.
func TestSecretsAreLeftOutOfTypeAndValue(t *testing.T) {
	m := readManifest(t)
	d := declaration(t, "cred", `{"type":"object","properties":{
		"user":{"$ref":"#/$defs/pair"},
		"admin":{"$ref":"#/$defs/pair"},
		"keys":{"type":"object","additionalProperties":{"type":"string"}},
		"items":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"itemKey":{"type":"string"}}}}},
		"$defs":{"pair":{"type":"object","properties":{"login":{"type":"string"},"adminPass":{"type":"string"}}}}}`,
		"/admin/adminPass", "/keys/*", "/items/*/itemKey", "/~2literal")
	ref := func(n int) string {
		return "secret:gcp-sm:projects/example-dev/secrets/s" + strconv.Itoa(n) + "/versions/1"
	}
	text := generated(t, m, fixtureOf(t, []map[string]any{d}, entryOf(t, "cred", "main", `{
		"user":{"login":"u","adminPass":"not-secret-here"},
		"admin":{"login":"a","adminPass":"`+ref(1)+`"},
		"keys":{"mapKeyName":"`+ref(2)+`"},
		"items":[{"name":"n","itemKey":"`+ref(3)+`"}],
		"*literal":"`+ref(4)+`"}`)))
	for _, absent := range []string{"secret:", "mapKeyName", "itemKey", "*literal"} {
		if strings.Contains(text, absent) {
			t.Errorf("the file holds %q:\n%s", absent, text)
		}
	}
	// adminPass is a secret below admin only: under user it is written, in
	// the type and in the value.
	if strings.Count(text, `"adminPass"`) != 2 || !strings.Contains(text, `"not-secret-here"`) {
		t.Errorf("adminPass under user is not written once in the type and once in the value:\n%s", text)
	}
}

// TestSecretUndeclaredMembersWithoutLiterals: a secret at a member the
// properties do not declare is left out, and the kind is not refused, when
// no literal can type it: an index signature of a type without one, or of
// JsonValue. Nor is a declared secret member's const: the member is left
// out, type and all.
func TestSecretUndeclaredMembersWithoutLiterals(t *testing.T) {
	m := readManifest(t)
	secret := "secret:gcp-sm:projects/example-dev/secrets/pw/versions/3"
	for name, schema := range map[string]string{
		"a string index":    `{"type":"object","properties":{"host":{"type":"string"}},"additionalProperties":{"type":"string"}}`,
		"a JsonValue index": `{"type":"object","properties":{"host":{"type":"string"}}}`,
		"a declared const secret member": `{"type":"object","properties":{"host":{"type":"string"},"password":{"const":"` + secret + `"}},
			"additionalProperties":{"type":"string"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			d := declaration(t, "database", schema, "/password")
			text := generated(t, m, fixtureOf(t, []map[string]any{d},
				entryOf(t, "database", "orders", `{"host":"h","password":"`+secret+`"}`)))
			if strings.Contains(text, "secret:") || strings.Contains(text, "password") || !strings.Contains(text, `"host"`) {
				t.Errorf("the file:\n%s", text)
			}
		})
	}
}
