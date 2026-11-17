package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	host "github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// The vectors and the golden package live in the nested module golden/, so
// that the generated code, which imports the start-up module, is compiled
// against it there (ADR-0102).
var (
	vectorsDir = filepath.Join("golden", "vectors")
	goldenDir  = filepath.Join("golden", "dbroleloader")
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

type vector struct {
	name     string
	request  []byte
	failure  string            // the code expected-failure.json names
	expected map[string][]byte // nil for an expected-failure vector
}

// readVectors reads golden/vectors in the RFC 0036 §5 layout: request.json,
// or registry.json with kinds/ and an optional options.json, from which the
// request is formed in-process as the host forms it; and expected/ or
// expected-failure.json. A failure is the host's
// EMIT-PROCESS, for a refusal, or one of the emitter's codes.
func readVectors(t *testing.T) []vector {
	t.Helper()
	entries, err := os.ReadDir(vectorsDir)
	if err != nil {
		t.Fatal(err)
	}
	codes := []string{"EMIT-PROCESS", string(findSchema), string(findSecret), string(findName), string(findCollide)}
	var out []vector
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(vectorsDir, entry.Name())
		request, err := os.ReadFile(filepath.Join(dir, "request.json"))
		if errors.Is(err, fs.ErrNotExist) {
			request, err = formRequest(t, dir)
		}
		if err != nil {
			t.Fatal(err)
		}
		v := vector{name: entry.Name(), request: canonical(t, request)}
		if failure, err := os.ReadFile(filepath.Join(dir, "expected-failure.json")); err == nil {
			value, _ := decodeStrict(failure)
			object, _ := value.(map[string]any)
			code, _ := object["code"].(string)
			if len(object) != 1 || !slices.Contains(codes, code) {
				t.Fatalf("%s: expected-failure.json is %s", v.name, failure)
			}
			v.failure = code
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

// formRequest forms a registry-form vector's request as `jpack emit` does:
// the registry validated against its kinds, the options admitted, and the
// request formed by the host's own FormRequests under emitter.json. No
// program is run. The manifest's "environments": "all" gives one request.
func formRequest(t *testing.T, dir string) ([]byte, error) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "registry.json"))
	if err != nil {
		return nil, err
	}
	names, err := filepath.Glob(filepath.Join(dir, "kinds", "*.json"))
	if err != nil || len(names) == 0 {
		t.Fatalf("%s: no kinds/*.json (%v)", dir, err)
	}
	slices.Sort(names)
	var kinds []registry.Document
	for _, name := range names {
		kind, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		kinds = append(kinds, registry.Document{Data: kind})
	}
	result := registry.ValidateDocuments(registry.Document{Data: data}, kinds)
	if !result.Report.Valid {
		t.Fatalf("%s: the registry is not valid: %+v", dir, result.Report.Diagnostics)
	}
	var options any
	if raw, err := os.ReadFile(filepath.Join(dir, "options.json")); err == nil {
		value, code, pointer := registry.AdmitValue(raw, host.MaxOptionsBytes)
		if code != "" {
			t.Fatalf("%s: options.json refused: %s at %s", dir, code, pointer)
		}
		options = value
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	manifestData, err := os.ReadFile("emitter.json")
	if err != nil {
		return nil, err
	}
	m, diagnostics := host.ReadManifest(manifestData)
	if m == nil {
		t.Fatalf("the manifest is refused: %v", diagnostics)
	}
	requests, refused := host.FormRequests(m, host.Inputs{Registry: result, Options: options})
	if len(refused) > 0 || len(requests) != 1 {
		t.Fatalf("%s: the host forms %d requests, refusing %v", dir, len(requests), refused)
	}
	return requests[0].Bytes, nil
}

type response struct {
	EmitResponseVersion string `json:"emitResponseVersion"`
	Files               []struct {
		Path string `json:"path"`
		Text string `json:"text"`
	} `json:"files"`
	Diagnostics []struct {
		Severity string            `json:"severity"`
		Code     string            `json:"code"`
		Location map[string]string `json:"location"`
	} `json:"diagnostics"`
}

// decodeResponse checks that out is one canonical response of exactly the
// three members.
func decodeResponse(t *testing.T, out []byte) response {
	t.Helper()
	if !bytes.Equal(canonical(t, out), out) {
		t.Fatalf("the response is not in canonical form")
	}
	value, _ := decodeStrict(out)
	if !exactMembers(value.(map[string]any), "emitResponseVersion", "files", "diagnostics") {
		t.Fatalf("the response members are not exactly the three")
	}
	var r response
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatal(err)
	}
	if r.EmitResponseVersion != "1.0.0" || r.Files == nil || r.Diagnostics == nil {
		t.Fatalf("version %q, files %v, diagnostics %v", r.EmitResponseVersion, r.Files, r.Diagnostics)
	}
	return r
}

// readResponse checks that out is a response of files and no diagnostic,
// and returns its files by path, in order.
func readResponse(t *testing.T, out []byte) ([]string, map[string][]byte) {
	t.Helper()
	r := decodeResponse(t, out)
	if len(r.Diagnostics) != 0 {
		t.Fatalf("diagnostics %v", r.Diagnostics)
	}
	paths := []string{}
	files := map[string][]byte{}
	for _, f := range r.Files {
		paths = append(paths, f.Path)
		files[f.Path] = []byte(f.Text)
		if !strings.HasSuffix(f.Text, "\n") || strings.Contains(f.Text, "\r") {
			t.Errorf("%s does not end in a line feed, or holds a carriage return", f.Path)
		}
	}
	return paths, files
}

// readFinding checks that out is a response of no file and one error
// diagnostic located at a kind, and returns its code and kind.
func readFinding(t *testing.T, out []byte) (string, string) {
	t.Helper()
	r := decodeResponse(t, out)
	if len(r.Files) != 0 || len(r.Diagnostics) != 1 {
		t.Fatalf("files %d, diagnostics %v; want no file and one diagnostic", len(r.Files), r.Diagnostics)
	}
	d := r.Diagnostics[0]
	if d.Severity != "error" || len(d.Location) != 3 || d.Location["document"] != "kind" || d.Location["pointer"] != "" {
		t.Fatalf("diagnostic %+v", d)
	}
	return d.Code, d.Location["kind"]
}

func TestVectors(t *testing.T) {
	for _, v := range readVectors(t) {
		t.Run(v.name, func(t *testing.T) {
			out, err := emit(v.request)
			switch {
			case v.failure == "EMIT-PROCESS":
				var r refusal
				if !errors.As(err, &r) || out != nil {
					t.Fatalf("emit = %q, %v; want a refusal and no response", out, err)
				}
				var stdout, stderr bytes.Buffer
				if code := run(bytes.NewReader(v.request), &stdout, &stderr); code == 0 || stdout.Len() != 0 {
					t.Fatalf("run exited %d with %d bytes of output; want non-zero and none", code, stdout.Len())
				}
				return
			case err != nil:
				t.Fatalf("emit: %v", err)
			case v.failure != "":
				code, kind := readFinding(t, out)
				value, _ := decodeStrict(v.request)
				declarations := value.(map[string]any)["declarations"].([]any)
				if code != v.failure || !slices.ContainsFunc(declarations, func(d any) bool { return d.(map[string]any)["kind"] == kind }) {
					t.Fatalf("finding %s at kind %q; want %s at a kind of the request", code, kind, v.failure)
				}
				return
			}
			paths, files := readResponse(t, out)
			if !slices.IsSorted(paths) {
				t.Errorf("files are not sorted by path: %v", paths)
			}
			want := make([]string, 0, len(v.expected))
			for p := range v.expected {
				want = append(want, p)
			}
			slices.Sort(want)
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

// TestRefusingVectorsAreTheirRefusals pins each EMIT-PROCESS vector to the
// refusal it is about, so it cannot pass for another reason.
func TestRefusingVectorsAreTheirRefusals(t *testing.T) {
	for name, want := range map[string]refusal{
		"declaration-digest-mismatch": errPin,
		"missing-declaration":         errPin,
	} {
		request, err := os.ReadFile(filepath.Join(vectorsDir, name, "request.json"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := emit(request); err != want {
			t.Errorf("%s: emit: %v, want %v", name, err, want)
		}
		var stdout, stderr bytes.Buffer
		if code := run(bytes.NewReader(request), &stdout, &stderr); code != 1 || stdout.Len() != 0 || stderr.String() != string(want)+"\n" {
			t.Errorf("%s: run = %d, stdout %q, stderr %q", name, code, stdout.String(), stderr.String())
		}
	}
}

// TestVectorsAsWritten: the emitter reads a request whatever its white
// space, and a declaration's digest is over its RFC 8785 form, not the
// bytes as written.
func TestVectorsAsWritten(t *testing.T) {
	written, err := os.ReadFile(filepath.Join(vectorsDir, "secret-position", "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	a, err1 := emit(written)
	b, err2 := emit(canonical(t, written))
	if err1 != nil || err2 != nil || !bytes.Equal(a, b) {
		t.Fatalf("the request as written: %v, %v", err1, err2)
	}
}

func TestRunWritesTheResponseAndNothingElse(t *testing.T) {
	request, err := os.ReadFile(filepath.Join(vectorsDir, "arrays-and-maps", "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := emit(request)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(bytes.NewReader(request), &stdout, &stderr); code != 0 || !bytes.Equal(stdout.Bytes(), want) || stderr.Len() != 0 {
		t.Fatalf("run = %d, %d bytes out (want %d), stderr %q", code, stdout.Len(), len(want), stderr.String())
	}
}

func TestRunRefusesAnOversizedRequest(t *testing.T) {
	var stdout, stderr bytes.Buffer
	big := bytes.Repeat([]byte(" "), maxRequestBytes+1)
	if code := run(bytes.NewReader(big), &stdout, &stderr); code != 1 || stdout.Len() != 0 || stderr.String() != string(errRead)+"\n" {
		t.Fatalf("run = %d, stdout %d bytes, stderr %q", code, stdout.Len(), stderr.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestRunReportsAFailedWrite(t *testing.T) {
	request, err := os.ReadFile(filepath.Join(vectorsDir, "secret-position", "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if code := run(bytes.NewReader(request), failingWriter{}, &stderr); code != 1 || stderr.String() != string(errWrite)+"\n" {
		t.Fatalf("run = %d, stderr %q", code, stderr.String())
	}
}

func TestRefuseNeverWritesALibraryError(t *testing.T) {
	var stderr bytes.Buffer
	if code := refuse(&stderr, errors.New("the request said: secret")); code != 1 || stderr.String() != string(errInternal)+"\n" {
		t.Fatalf("refuse = %d, %q", code, stderr.String())
	}
}

// TestEmitIsDeterministic runs every vector twice: the response is the same.
func TestEmitIsDeterministic(t *testing.T) {
	for _, v := range readVectors(t) {
		first, err1 := emit(v.request)
		second, err2 := emit(v.request)
		if !bytes.Equal(first, second) || !errors.Is(err1, err2) {
			t.Errorf("%s: two runs differ", v.name)
		}
	}
}

// TestGoldenPackageIsTheVectorOutput holds golden/dbroleloader, the package
// whose tests compile and run the generated code, to the secret-position
// vector's output.
func TestGoldenPackageIsTheVectorOutput(t *testing.T) {
	request, err := os.ReadFile(filepath.Join(vectorsDir, "secret-position", "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := emit(request)
	if err != nil {
		t.Fatal(err)
	}
	paths, files := readResponse(t, out)
	if len(paths) != 2 {
		t.Fatalf("paths %v", paths)
	}
	for _, p := range paths {
		golden, err := os.ReadFile(filepath.Join(goldenDir, path.Base(p)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(golden, files[p]) {
			t.Errorf("%s/%s differs from the emitter's %s", goldenDir, path.Base(p), p)
		}
	}
}

// TestManifest holds emitter.json to the program: its identity, input,
// environments, request versions, the owns glob of the default directory,
// and an options schema whose rules are the ones readOptions applies.
func TestManifest(t *testing.T) {
	data, err := os.ReadFile("emitter.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := decodeStrict(data)
	if err != nil {
		t.Fatal(err)
	}
	m := value.(map[string]any)
	if !exactMembers(m, "emitterManifestVersion", "id", "version", "owner", "description", "reads", "input", "environments",
		"requestVersions", "options", "command", "env", "owns") {
		t.Fatalf("manifest members: %v", m)
	}
	var manifest struct {
		ManifestVersion string   `json:"emitterManifestVersion"`
		ID              string   `json:"id"`
		Version         string   `json:"version"`
		Input           string   `json:"input"`
		Environments    string   `json:"environments"`
		RequestVersions []string `json:"requestVersions"`
		Owns            []string `json:"owns"`
		Options         struct {
			AdditionalProperties *bool    `json:"additionalProperties"`
			Required             []string `json:"required"`
			Properties           map[string]struct {
				Type      string `json:"type"`
				MaxLength int    `json:"maxLength"`
				Pattern   string `json:"pattern"`
				Default   string `json:"default"`
				Not       struct {
					Enum    []string `json:"enum"`
					Pattern string   `json:"pattern"`
				} `json:"not"`
			} `json:"properties"`
		} `json:"options"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	// environments is "all": the files do not depend on the environment, so
	// under "each" every environment's run would write the same paths, which
	// the host refuses (ADR-0098).
	if manifest.ManifestVersion != "1.0.0" || manifest.ID != emitterID || manifest.Version != emitterVersion ||
		manifest.Input != "resolved" || manifest.Environments != "all" || !slices.Equal(manifest.RequestVersions, []string{requestVersion}) {
		t.Fatalf("manifest identity or input: %+v", manifest)
	}
	if !slices.Equal(manifest.Owns, []string{defaultDirectory + "/*.go"}) {
		t.Fatalf("owns %v", manifest.Owns)
	}
	o := manifest.Options
	if o.AdditionalProperties == nil || *o.AdditionalProperties || !slices.Equal(o.Required, []string{"package"}) || len(o.Properties) != 2 {
		t.Fatalf("options schema: %+v", o)
	}
	pkg, dir := o.Properties["package"], o.Properties["directory"]
	if pkg.Type != "string" || pkg.Pattern != packagePattern.String() || pkg.MaxLength != maxPackageBytes {
		t.Errorf("package schema: %+v", pkg)
	}
	refused := []string{"main", "documentation"}
	for k := range goKeywords {
		refused = append(refused, k)
	}
	slices.Sort(refused)
	if !slices.Equal(pkg.Not.Enum, refused) {
		t.Errorf("package names refused %v, want %v", pkg.Not.Enum, refused)
	}
	for _, name := range refused {
		if !notPackageName(name) {
			t.Errorf("the manifest refuses package %q and the program does not", name)
		}
	}
	if dir.Type != "string" || dir.Pattern != directoryPattern.String() || dir.Not.Pattern != testdataPattern.String() ||
		dir.MaxLength != maxDirectoryBytes || dir.Default != defaultDirectory {
		t.Errorf("directory schema: %+v", dir)
	}
	for _, v := range readVectors(t) {
		value, _ := decodeStrict(v.request)
		e := value.(map[string]any)["emitter"].(map[string]any)
		if e["id"] != emitterID || e["version"] != emitterVersion {
			t.Errorf("%s: the request names emitter %v", v.name, e)
		}
	}
}

// TestManifestPatternsMatchAsTheProgramDoes compiles the manifest's two
// patterns under the pattern subset the host validates options with, and
// holds them to the program's on the edges of each.
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
	pkg := compile(manifest.Options.Properties["package"].Pattern)
	for _, s := range []string{"p", "loader2", "", "P", "2p", "p_q", "p-q", "pé", "p\n"} {
		if pkg.MatchString(s) != packagePattern.MatchString(s) {
			t.Errorf("package: the manifest and the program differ on %q", s)
		}
	}
	dir := compile(manifest.Options.Properties["directory"].Pattern)
	notDir := compile(manifest.Options.Properties["directory"].Not.Pattern)
	for _, s := range []string{"generated/loader", "a", "a.b/c-d/e_", "", "/a", "a/", "a//b", ".", "..", "a/..", ".git", "a/.git",
		"a.", "-a", "a/b.", "a\\b", "a b", "a\n", "_a", "a/_b", "testdata", "a/testdata", "testdata/a", "a/testdata/b",
		"testdata2", "a/my_testdata", "testdatax/a", "xtestdata", "vendor", "a/vendor", "vendor/a", "a/vendor/b", "TestData",
		"a/Vendor", "VENDOR/a", "vendors", "a/myvendor", "vendorx/a"} {
		manifest := dir.MatchString(s) && !notDir.MatchString(s)
		program := directoryPattern.MatchString(s) && !testdataPattern.MatchString(s)
		if manifest != program {
			t.Errorf("directory: the manifest and the program differ on %q", s)
		}
	}
}

// decl is a declaration of kind with schema; extra is written after the
// schema, each member with its leading comma.
func decl(kind, schema, extra string) string {
	return `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/` + kind + `","version":"1.0.0","kind":"` + kind +
		`","owner":"platform-data","schema":` + schema + extra + `}`
}

// closed is the schema of an object closed with additionalProperties false
// and these properties.
func closed(properties string) string {
	return `{"type":"object","additionalProperties":false,"properties":{` + properties + `}}`
}

// digestOf is the digest of a declaration as written: the SHA-256 of its RFC
// 8785 form.
func digestOf(t *testing.T, declaration string) string {
	t.Helper()
	sum := sha256.Sum256(canonical(t, []byte(declaration)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// pinOf is the pin of a declaration as written.
func pinOf(t *testing.T, declaration string) string {
	t.Helper()
	value, err := decodeStrict([]byte(declaration))
	if err != nil {
		t.Fatal(err)
	}
	m := value.(map[string]any)
	return `{"kind":` + strconv.Quote(m["kind"].(string)) + `,"id":` + strconv.Quote(m["id"].(string)) +
		`,"version":` + strconv.Quote(m["version"].(string)) + `,"digest":"` + digestOf(t, declaration) + `"}`
}

func resolvedText(environment, pins, entries string) string {
	return `{"resolvedFormatVersion":"1.0.0","registry":{"id":"https://example.invalid/r","version":"1.0.0","digest":"sha256:00"},` +
		`"environment":"` + environment + `","kinds":[` + pins + `],"entries":[` + entries + `]}`
}

func requestText(options, declarations, resolved string) string {
	return `{"emitRequestVersion":"1.0.0","emitter":{"id":"https://example.invalid/e","version":"1.0.0"},"options":` + options +
		`,"declarations":[` + declarations + `],"resolved":[` + resolved + `]}`
}

// requestOf is a request of the declarations, given sorted by kind, each
// pinned with its own digest in one environment, dev, of no entry.
func requestOf(t *testing.T, declarations ...string) string {
	t.Helper()
	pins := make([]string, len(declarations))
	for i, d := range declarations {
		pins[i] = pinOf(t, d)
	}
	return requestText(`{"package":"p"}`, strings.Join(declarations, ","), resolvedText("dev", strings.Join(pins, ","), ""))
}

// thing is a declaration of the kind thing with these properties.
func thing(properties string) string {
	return decl("thing", closed(properties), "")
}

func TestRefusals(t *testing.T) {
	one := thing(`"a":{"type":"string"}`)
	other := decl("other", closed(`"b":{"type":"string"}`), "")
	base := requestOf(t, one)
	pin := pinOf(t, one)
	dev := resolvedText("dev", pin, "")
	cases := []struct {
		name    string
		request string
		want    refusal
	}{
		{"not JSON", `{`, errRequest},
		{"trailing data", base + `{}`, errRequest},
		{"repeated member", strings.Replace(base, `"options"`, `"options":{"package":"p"},"options"`, 1), errRequest},
		{"invalid UTF-8", strings.Replace(base, `"p"`, "\"p\xff\"", 1), errRequest},
		{"unpaired surrogate", strings.Replace(base, `"options"`, `"x\ud800":1,"options"`, 1), errRequest},
		{"too deep", strings.Replace(base, `{"package":"p"}`, `{"package":"p","x":`+strings.Repeat("[", maxDepth)+strings.Repeat("]", maxDepth)+`}`, 1), errRequest},
		{"not an object", `[]`, errRequest},
		{"an extra member", strings.Replace(base, `"options"`, `"extra":1,"options"`, 1), errRequest},
		{"no declarations", strings.Replace(base, `"declarations":[`+one+`],`, ``, 1), errRequest},
		{"the facts-contracts form", strings.Replace(base, `"resolved"`, `"factsContracts"`, 1), errRequest},
		{"another request version", strings.Replace(base, `"1.0.0"`, `"1.1.0"`, 1), errRequest},
		{"an emitter without a version", strings.Replace(base, `,"version":"1.0.0"},"options"`, `},"options"`, 1), errRequest},
		{"declarations not an array", strings.Replace(base, `"declarations":[`+one+`]`, `"declarations":{}`, 1), errRequest},
		{"resolved not an array", strings.Replace(base, `"resolved":[`+dev+`]`, `"resolved":{}`, 1), errRequest},
		{"declarations out of order", requestText(`{"package":"p"}`, one+","+other, resolvedText("dev", pinOf(t, other)+","+pin, "")), errRequest},
		{"a kind declared twice", requestText(`{"package":"p"}`, one+","+one, resolvedText("dev", pin+","+pin, "")), errRequest},
		{"an environment twice", requestText(`{"package":"p"}`, one, dev+","+dev), errRequest},
		{"pins out of order", requestText(`{"package":"p"}`, other+","+one, resolvedText("dev", pin+","+pinOf(t, other), "")), errRequest},
		{"an entry of a kind not pinned", requestText(`{"package":"p"}`, one, resolvedText("dev", pin, `{"kind":"other","id":"x","owner":"o","value":{}}`)), errRequest},
		{"an entry with an extra member", requestText(`{"package":"p"}`, one, resolvedText("dev", pin, `{"kind":"thing","id":"x","owner":"o","value":{},"x":1}`)), errRequest},
		{"a resolved document of another format", strings.Replace(base, `"resolvedFormatVersion":"1.0.0"`, `"resolvedFormatVersion":"2.0.0"`, 1), errRequest},
		{"an environment of another form", strings.Replace(base, `"environment":"dev"`, `"environment":"Dev"`, 1), errRequest},
		{"a pin with an extra member", strings.Replace(base, `"digest":"sha256:`+digestOf(t, one)[7:]+`"}`, `"digest":"`+digestOf(t, one)+`","x":1}`, 1), errRequest},

		{"no package", strings.Replace(base, `{"package":"p"}`, `{}`, 1), errOptions},
		{"an upper-case package", strings.Replace(base, `"package":"p"`, `"package":"P"`, 1), errOptions},
		{"a keyword package", strings.Replace(base, `"package":"p"`, `"package":"type"`, 1), errOptions},
		{"a long package", strings.Replace(base, `"package":"p"`, `"package":"p`+strings.Repeat("a", maxPackageBytes)+`"`, 1), errOptions},
		{"another option", strings.Replace(base, `{"package":"p"}`, `{"package":"p","schema":"x"}`, 1), errOptions},
		{"options not an object", strings.Replace(base, `{"package":"p"}`, `[]`, 1), errOptions},
		{"a parent directory", strings.Replace(base, `{"package":"p"}`, `{"package":"p","directory":"../x"}`, 1), errOptions},
		{"an absolute directory", strings.Replace(base, `{"package":"p"}`, `{"package":"p","directory":"/x"}`, 1), errOptions},
		{"a .git directory", strings.Replace(base, `{"package":"p"}`, `{"package":"p","directory":"a/.git"}`, 1), errOptions},
		{"a long directory", strings.Replace(base, `{"package":"p"}`, `{"package":"p","directory":"`+strings.Repeat("a", maxDirectoryBytes+1)+`"}`, 1), errOptions},

		{"another kind format", strings.Replace(base, `"kindFormatVersion":"1.0.0"`, `"kindFormatVersion":"1.1.0"`, 1), errDeclaration},
		{"a declaration with an unknown member", strings.Replace(base, `"owner":"platform-data"`, `"owner":"platform-data","x":1`, 1), errDeclaration},
		{"a declaration without a schema", requestOf(t, `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/thing","version":"1.0.0","kind":"thing","owner":"o"}`), errDeclaration},
		{"a relative id", strings.Replace(base, `"id":"https://example.invalid/kinds/thing"`, `"id":"thing"`, 1), errDeclaration},
		{"a version that is not SemVer", strings.Replace(base, `"version":"1.0.0","kind"`, `"version":"1.0","kind"`, 1), errDeclaration},
		{"a kind name of another form", strings.Replace(base, `"kind":"thing","owner"`, `"kind":"Thing","owner"`, 1), errDeclaration},
		{"an owner of another form", strings.Replace(base, `"owner":"platform-data"`, `"owner":"platform data"`, 1), errDeclaration},
		{"secrets not an array", requestOf(t, decl("thing", closed(`"a":{"type":"string"}`), `,"secrets":"/a"`)), errDeclaration},
		{"a secret that is not a pointer", requestOf(t, decl("thing", closed(`"a":{"type":"string"}`), `,"secrets":["a"]`)), errDeclaration},
		{"a secret with an unknown escape", requestOf(t, decl("thing", closed(`"a":{"type":"string"}`), `,"secrets":["/~3"]`)), errDeclaration},
		{"a declaration that is not an object", requestText(`{"package":"p"}`, `[]`, dev), errDeclaration},

		// Every pin against its declaration (registry companion §11.2).
		{"a pin with no declaration", requestText(`{"package":"p"}`, ``, dev), errPin},
		{"a declaration with no pin", requestText(`{"package":"p"}`, one, resolvedText("dev", ``, ``)), errPin},
		{"a declaration and no resolved document", requestText(`{"package":"p"}`, one, ``), errPin},
		{"a pin of another digest", strings.Replace(base, digestOf(t, one), "sha256:"+strings.Repeat("0", 64), 1), errPin},
		{"a declaration that is not the one pinned", requestText(`{"package":"p"}`, thing(`"a":{"type":"string","maxLength":2}`), dev), errPin},
		{"a pin of another id", strings.Replace(base, `"kind":"thing","id":"https://example.invalid/kinds/thing"`, `"kind":"thing","id":"https://example.invalid/kinds/other"`, 1), errPin},
		{"a pin of another version", strings.Replace(base, pin, strings.Replace(pin, `"version":"1.0.0"`, `"version":"1.0.1"`, 1), 1), errPin},
		{"a pin of another kind", requestText(`{"package":"p"}`, one, resolvedText("dev", strings.Replace(pin, `"kind":"thing"`, `"kind":"thinx"`, 1), "")), errPin},
		{"one environment of two with another digest", requestText(`{"package":"p"}`, one,
			dev+","+resolvedText("prod", strings.Replace(pin, digestOf(t, one), "sha256:"+strings.Repeat("1", 64), 1), "")), errPin},
		{"one environment of two without the pin", requestText(`{"package":"p"}`, one, dev+","+resolvedText("prod", ``, ``)), errPin},
		{"a pin more than the declarations", requestText(`{"package":"p"}`, one, resolvedText("dev", pinOf(t, other)+","+pin, "")), errPin},

		{"a directory segment past the bound", strings.Replace(base, `{"package":"p"}`, `{"package":"p","directory":"`+strings.Repeat("d", maxSegmentBytes+1)+`"}`, 1), errBounds},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := emit([]byte(c.request))
			if err != c.want || out != nil {
				t.Fatalf("emit = %d bytes, %v; want %v", len(out), err, c.want)
			}
		})
	}
	// The base request, and a request whose two environments pin the same
	// declarations, are read.
	for _, request := range []string{base, requestText(`{"package":"p"}`, one, dev+","+resolvedText("prod", pin, ""))} {
		if _, err := emit([]byte(request)); err != nil {
			t.Fatalf("a valid request: %v", err)
		}
	}
}

func TestNoDeclarationsWritesNoFile(t *testing.T) {
	for _, request := range []string{
		requestText(`{"package":"p"}`, ``, ``),
		requestText(`{"package":"p"}`, ``, resolvedText("dev", ``, ``)),
	} {
		out, err := emit([]byte(request))
		if err != nil || string(out) != `{"diagnostics":[],"emitResponseVersion":"1.0.0","files":[]}` {
			t.Fatalf("emit = %s, %v", out, err)
		}
	}
}

// TestFindings: a declaration the emitter reads but cannot generate code for
// is one error diagnostic at its kind, and no file.
func TestFindings(t *testing.T) {
	str := `{"type":"string"}`
	cases := []struct {
		name        string
		declaration string
		want        finding
	}{
		// Refused shapes (ADR-0102).
		{"an open object", thing(`"a":{"type":"object","properties":{"b":` + str + `}}`), findSchema},
		{"additionalProperties true", thing(`"a":{"type":"object","additionalProperties":true}`), findSchema},
		{"additionalProperties of any value", thing(`"a":{"type":"object","additionalProperties":{}}`), findSchema},
		{"properties beside an additionalProperties schema", thing(`"a":{"type":"object","properties":{"b":` + str + `},"additionalProperties":` + str + `}`), findSchema},
		{"an open root", decl("thing", `{"type":"object","properties":{"a":`+str+`}}`, ""), findSchema},
		{"a root that is not an object", decl("thing", str, ""), findSchema},
		{"a root map", decl("thing", `{"type":"object","additionalProperties":`+str+`}`, ""), findSchema},
		{"a root of false", decl("thing", `false`, ""), findSchema},
		{"anyOf", thing(`"a":{"anyOf":[` + str + `,{"type":"integer"}]}`), findSchema},
		{"anyOf beside a type", thing(`"a":{"type":"string","anyOf":[{"maxLength":1},{"minLength":3}]}`), findSchema},
		{"allOf", thing(`"a":{"allOf":[` + str + `]}`), findSchema},
		{"not", thing(`"a":{"type":"string","not":{"const":"x"}}`), findSchema},
		{"a type array", thing(`"a":{"type":["string","null"]}`), findSchema},
		{"null", thing(`"a":{"type":"null"}`), findSchema},
		{"a keyword outside the subset", thing(`"a":{"type":"string","format":"uri"}`), findSchema},
		{"patternProperties", thing(`"a":{"type":"object","additionalProperties":false,"patternProperties":{}}`), findSchema},
		{"a tuple", thing(`"a":{"type":"array","items":[` + str + `]}`), findSchema},
		{"false as a member", thing(`"a":false`), findSchema},
		{"a required member not declared", decl("thing", `{"type":"object","additionalProperties":false,"required":["b"],"properties":{"a":`+str+`}}`, ""), findSchema},
		{"a required member twice", decl("thing", `{"type":"object","additionalProperties":false,"required":["a","a"],"properties":{"a":`+str+`}}`, ""), findSchema},
		{"required at a map", thing(`"a":{"type":"object","additionalProperties":` + str + `,"required":["b"]}`), findSchema},
		{"a type of the empty string", thing(`"a":{"type":""}`), findSchema},
		{"an enum at an integer", thing(`"a":{"type":"integer","enum":[1,2]}`), findSchema},
		{"a const at a number", thing(`"a":{"type":"number","const":1.5}`), findSchema},
		{"a const at a boolean", thing(`"a":{"type":"boolean","const":true}`), findSchema},
		{"a const at an array", thing(`"a":{"type":"array","items":` + str + `,"const":["x"]}`), findSchema},
		{"an enum at an array", thing(`"a":{"type":"array","items":` + str + `,"enum":[["x"]]}`), findSchema},
		{"a const at an object", thing(`"a":{"type":"object","additionalProperties":false,"properties":{"b":` + str + `},"const":{"b":"x"}}`), findSchema},
		{"an enum at a map", thing(`"a":{"type":"object","additionalProperties":` + str + `,"enum":[{}]}`), findSchema},
		{"an enum at the root", decl("thing", `{"type":"object","additionalProperties":false,"properties":{"a":`+str+`},"enum":[{"a":"x"}]}`, ""), findSchema},
		{"properties without a type", thing(`"a":{"additionalProperties":false,"properties":{}}`), findSchema},
		{"items without a type", thing(`"a":{"items":` + str + `}`), findSchema},
		{"an enum of a number", thing(`"a":{"enum":["x",1]}`), findSchema},
		{"an enum beside a const", thing(`"a":{"type":"string","enum":["x"],"const":"x"}`), findSchema},
		{"an empty enum", thing(`"a":{"type":"string","enum":[]}`), findSchema},
		{"$ref beside a type", decl("thing", `{"type":"object","additionalProperties":false,"$defs":{"s":`+str+`},"properties":{"a":{"$ref":"#/$defs/s","type":"string"}}}`, ""), findSchema},
		{"$ref of another form", decl("thing", `{"type":"object","additionalProperties":false,"$defs":{"s":`+str+`},"properties":{"a":{"$ref":"#/definitions/s"}}}`, ""), findSchema},
		{"$ref to an undefined name", decl("thing", `{"type":"object","additionalProperties":false,"$defs":{"s":`+str+`},"properties":{"a":{"$ref":"#/$defs/t"}}}`, ""), findSchema},
		{"$ref in a cycle", decl("thing", `{"type":"object","additionalProperties":false,"$defs":{"s":{"type":"object","additionalProperties":false,"properties":{"next":{"$ref":"#/$defs/s"}}}},"properties":{"a":{"$ref":"#/$defs/s"}}}`, ""), findSchema},
		{"$defs below the root", thing(`"a":{"type":"string","$defs":{}}`), findSchema},

		// Secret positions.
		{"a secret at an enumeration", decl("thing", closed(`"a":{"type":"string","enum":["x","y"]}`), `,"secrets":["/a"]`), findSecret},
		{"a secret at an untyped enumeration", decl("thing", closed(`"a":{"enum":["x"]}`), `,"secrets":["/a"]`), findSecret},
		{"a secret at an integer", decl("thing", closed(`"a":{"type":"integer"}`), `,"secrets":["/a"]`), findSecret},
		{"a secret at an object", decl("thing", closed(`"a":`+closed(`"b":`+str)), `,"secrets":["/a"]`), findSecret},
		{"a secret at an array", decl("thing", closed(`"a":{"type":"array","items":`+str+`}`), `,"secrets":["/a"]`), findSecret},
		{"a secret at any value", decl("thing", closed(`"a":{}`), `,"secrets":["/a"]`), findSecret},
		{"a secret below any value", decl("thing", closed(`"a":{}`), `,"secrets":["/a/b"]`), findSecret},
		{"a secret at one element of an array", decl("thing", closed(`"a":{"type":"array","items":`+str+`}`), `,"secrets":["/a/0"]`), findSecret},
		{"a secret at one member of a map", decl("thing", closed(`"a":{"type":"object","additionalProperties":`+str+`}`), `,"secrets":["/a/b"]`), findSecret},
		{"a secret that matches nothing", decl("thing", closed(`"a":`+str), `,"secrets":["/b"]`), findSecret},
		{"a secret below a string", decl("thing", closed(`"a":`+str), `,"secrets":["/a/b"]`), findSecret},
		{"one secret of two that matches nothing", decl("thing", closed(`"a":`+str), `,"secrets":["/a","/b"]`), findSecret},

		// Names.
		{"a member with no word", thing(`"%%":` + str), findName},
		{"a value with no word", thing(`"a":{"enum":["x","!!"]}`), findName},
		{"a member of non-ASCII letters only", thing(`"éè":` + str), findName},

		// Collisions.
		{"two fields", thing(`"db-name":` + str + `,"db_name":` + str), findCollide},
		{"two constants", thing(`"a":{"enum":["x-y","x_y"]}`), findCollide},
		{"a type and a constant", thing(`"a":{"enum":["b"]},"a-b":` + closed(``)), findCollide},
		{"two types", thing(`"a":` + closed(`"b-c":`+closed(``)) + `,"a-b":` + closed(`"c":`+closed(``))), findCollide},
		{"a kind named as a support name", decl("registry", closed(``), ""), findCollide},
		{"a kind named as an error", decl("err-value", closed(``), ""), findCollide},
		{"a kind named as a function", decl("load", closed(``), ""), findCollide},
		{"a type below the kind named as a support name", decl("err", closed(`"kind":`+closed(``)), ""), findCollide},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := emit([]byte(requestOf(t, c.declaration)))
			if err != nil {
				t.Fatalf("emit: %v", err)
			}
			code, kind := readFinding(t, out)
			value, _ := decodeStrict([]byte(c.declaration))
			if code != string(c.want) || kind != value.(map[string]any)["kind"] {
				t.Fatalf("finding %s at kind %q, want %s", code, kind, c.want)
			}
		})
	}
}

// TestFindingsAcrossKinds: names collide across the kinds of one request,
// and the finding is located at the kind whose name was taken second.
func TestFindingsAcrossKinds(t *testing.T) {
	for _, c := range []struct {
		name         string
		declarations []string
		want         finding
		at           string
	}{
		{"a method named as another kind's ids method", []string{decl("x", closed(``), ""), decl("x-i-ds", closed(``), "")}, findCollide, "x-i-ds"},
		{"two kinds of one file", []string{decl("a--b", closed(``), ""), decl("a-b", closed(``), "")}, findCollide, "a-b"},
		{"a type named as another kind's type", []string{decl("a", closed(`"b":`+closed(``)), ""), decl("a-b", closed(``), "")}, findCollide, "a-b"},
		{"a refused shape in the second kind", []string{decl("a", closed(``), ""), decl("b", closed(`"c":{"not":{}}`), "")}, findSchema, "b"},
	} {
		out, err := emit([]byte(requestOf(t, c.declarations...)))
		if err != nil {
			t.Fatalf("%s: emit: %v", c.name, err)
		}
		if code, kind := readFinding(t, out); code != string(c.want) || kind != c.at {
			t.Errorf("%s: finding %s at %q, want %s at %q", c.name, code, kind, c.want, c.at)
		}
	}
}

// TestSecretTyping: each position a secret pattern matches is a
// startup.Secret, and no other is: a wild token reaches every element of an
// array and every member of a map.
func TestSecretTyping(t *testing.T) {
	str := `{"type":"string"}`
	declaration := decl("thing", closed(
		`"a":`+str+
			`,"b":`+str+
			`,"items":{"type":"array","items":`+closed(`"key":`+str+`,"name":`+str)+`}`+
			`,"tokens":{"type":"object","additionalProperties":`+str+`}`+
			`,"wild":`+closed(`"p":`+str+`,"q":`+str)),
		`,"secrets":["/a","/items/*/key","/tokens/*","/wild/*"]`)
	out, err := emit([]byte(requestOf(t, declaration)))
	if err != nil {
		t.Fatal(err)
	}
	_, files := readResponse(t, out)
	text := string(files["generated/loader/thing_kind.go"])
	for _, want := range []string{
		"\tA *startup.Secret\n",
		"\tB *string\n",
		"\tKey *startup.Secret\n",
		"\tName *string\n",
		"\tTokens map[string]startup.Secret\n",
		"\tP *startup.Secret\n",
		"\tQ *startup.Secret\n",
		"d.readSecret(at0, raw0)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the source does not hold %q", want)
		}
	}
	if strings.Count(text, "d.readSecret(") != 5 {
		t.Errorf("a secret position is not read as a secret, exactly:\n%s", text)
	}
	// A kind with no secret does not import the start-up module.
	out, err = emit([]byte(requestOf(t, thing(`"a":`+str))))
	if err != nil {
		t.Fatal(err)
	}
	_, files = readResponse(t, out)
	if strings.Contains(string(files["generated/loader/thing_kind.go"]), "startup") {
		t.Errorf("a kind with no secret imports the start-up module")
	}
}

// TestDeclarationTextIsQuoted: a kind's id, a member name and a value reach
// the source only as Go string literals written by strconv.QuoteToASCII, so
// no text of a declaration can end a comment or a literal, the files are
// ASCII, and the source parses to the declarations the emitter means to
// write and no other.
func TestDeclarationTextIsQuoted(t *testing.T) {
	payload := `x\"]; os.Exit(1); _ = m[\"` + "`" + `*/\né`
	declaration := `{"kindFormatVersion":"1.0.0","id":"https://example.invalid/kinds/x\"*/","version":"1.0.0","kind":"thing",` +
		`"owner":"platform-data","schema":` + closed(`"`+payload+`":{"type":"string"},"v":{"enum":["`+payload+`"]}`) + `}`
	out, err := emit([]byte(requestOf(t, declaration)))
	if err != nil {
		t.Fatal(err)
	}
	_, files := readResponse(t, out)
	for name, data := range files {
		for i, c := range data {
			if c >= 0x80 {
				t.Fatalf("%s holds a byte outside ASCII at %d", name, i)
			}
		}
	}
	text := string(files["generated/loader/thing_kind.go"])
	quoted := strconv.QuoteToASCII("x\"]; os.Exit(1); _ = m[\"`*/\né")
	for _, want := range []string{
		`members[` + quoted + `]`,
		`ThingVXOsExit1M ThingV = ` + quoted,
		`"https://example.invalid/kinds/x\"*/"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the source does not hold %s", want)
		}
	}
	loader := string(files["generated/loader/loader.go"])
	if !strings.Contains(loader, `id: "https://example.invalid/kinds/x\"*/", version: "1.0.0"`) {
		t.Errorf("loader.go does not hold the kind's id and version quoted")
	}
	for name, data := range files {
		f, err := parser.ParseFile(gotoken.NewFileSet(), name, data, parser.ParseComments)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, imported := range f.Imports {
			if path, _ := strconv.Unquote(imported.Path.Value); !slices.Contains(allowedImports, path) {
				t.Errorf("%s imports %s", name, path)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if selector, ok := n.(*ast.SelectorExpr); ok {
				if x, ok := selector.X.(*ast.Ident); ok && x.Name == "os" {
					t.Errorf("%s calls into os", name)
				}
			}
			return true
		})
	}
}

// allowedImports are the packages generated code may import: the standard
// library packages it uses, and the start-up module.
var allowedImports = []string{"bytes", "context", "encoding/json", "errors", "fmt", "sort", "strconv", "strings", startupPath}

// TestGeneratedImportsAreStandardOrStartup: every vector's files import the
// standard library and the start-up module only.
func TestGeneratedImportsAreStandardOrStartup(t *testing.T) {
	for _, v := range readVectors(t) {
		for name, data := range v.expected {
			f, err := parser.ParseFile(gotoken.NewFileSet(), name, data, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("%s/%s: %v", v.name, name, err)
			}
			for _, imported := range f.Imports {
				if path, _ := strconv.Unquote(imported.Path.Value); !slices.Contains(allowedImports, path) {
					t.Errorf("%s/%s imports %s", v.name, name, path)
				}
			}
		}
	}
}

// TestEntriesDoNotChangeTheFiles: the files depend on the declarations
// alone, not on the environments, their entries or the registry's identity.
func TestEntriesDoNotChangeTheFiles(t *testing.T) {
	one := thing(`"a":{"type":"string"}`)
	pin := pinOf(t, one)
	a, err1 := emit([]byte(requestOf(t, one)))
	b, err2 := emit([]byte(requestText(`{"package":"p"}`, one,
		strings.Replace(resolvedText("dev", pin, `{"kind":"thing","id":"x","owner":"o","value":{"a":"b"}}`), `"version":"1.0.0","digest":"sha256:00"`, `"version":"9.0.0","digest":"sha256:11"`, 1)+
			","+resolvedText("prod", pin, ""))))
	if err1 != nil || err2 != nil || !bytes.Equal(a, b) {
		t.Fatalf("the entries changed the files: %v, %v", err1, err2)
	}
}

func TestDirectoryOption(t *testing.T) {
	request := strings.Replace(requestOf(t, thing(`"a":{"type":"string"}`)), `{"package":"p"}`, `{"package":"p","directory":"internal/loader.gen"}`, 1)
	out, err := emit([]byte(request))
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := readResponse(t, out)
	if !slices.Equal(paths, []string{"internal/loader.gen/loader.go", "internal/loader.gen/thing_kind.go"}) {
		t.Fatalf("paths %v", paths)
	}
}

// TestPackageNamesTheToolchainCannotUseAreRefused: a package named main is a
// command, which no other package can import, documentation is ignored, and
// a keyword cannot be a package clause.
func TestPackageNamesTheToolchainCannotUseAreRefused(t *testing.T) {
	for _, c := range []struct {
		pkg  string
		want error
	}{
		{"loader", nil},
		{"mainloader", nil},
		{"main", errOptions},
		{"documentation", errOptions},
		{"documentations", nil},
		{"type", errOptions},
		{"Main", errOptions},
	} {
		request := strings.Replace(requestOf(t, thing(`"a":{"type":"string"}`)), `"package":"p"`, `"package":"`+c.pkg+`"`, 1)
		if _, err := emit([]byte(request)); err != c.want {
			t.Errorf("package %q: %v, want %v", c.pkg, err, c.want)
		}
	}
}

// TestDirectoryTheToolchainIgnoresIsRefused: the Go toolchain skips a
// directory whose name begins with "_" or "." and one named testdata, and
// treats vendor as vendored code, so code generated below one would never be
// compiled as the package.
func TestDirectoryTheToolchainIgnoresIsRefused(t *testing.T) {
	for _, c := range []struct {
		directory string
		want      error
	}{
		{"generated/loader", nil},
		{"internal/loader_", nil},
		{"testdata2", nil},
		{"a/my_testdata", nil},
		{"a/testdatax/b", nil},
		{"_gen", errOptions},
		{"generated/_loader", errOptions},
		{".gen", errOptions},
		{"generated/.loader", errOptions},
		{"testdata", errOptions},
		{"testdata/loader", errOptions},
		{"generated/testdata", errOptions},
		{"generated/testdata/loader", errOptions},
		{"vendor", errOptions},
		{"vendor/loader", errOptions},
		{"generated/vendor", errOptions},
		{"generated/vendor/loader", errOptions},
		{"TestData", errOptions},
		{"generated/Vendor/loader", errOptions},
		{"VENDOR", errOptions},
		{"vendors", nil},
		{"a/myvendor/b", nil},
	} {
		request := strings.Replace(requestOf(t, thing(`"a":{"type":"string"}`)), `{"package":"p"}`,
			`{"package":"p","directory":"`+c.directory+`"}`, 1)
		if _, err := emit([]byte(request)); err != c.want {
			t.Errorf("directory %q: %v, want %v", c.directory, err, c.want)
		}
	}
}

// TestPathSegmentBound: a directory name over 255 bytes is errBounds,
// although the directory is within its 256-byte bound: no common file system
// could hold it. A kind's file name is at most 71 bytes, by the kind name's
// bound.
func TestPathSegmentBound(t *testing.T) {
	for _, c := range []struct {
		name      string
		kind      string
		directory int
		want      error
	}{
		{"a directory name of 255 bytes", "thing", maxSegmentBytes, nil},
		{"a directory name of 256 bytes", "thing", maxSegmentBytes + 1, errBounds},
		{"the longest kind name", "k" + strings.Repeat("a", 62), 0, nil},
	} {
		request := requestOf(t, decl(c.kind, closed(``), ""))
		if c.directory > 0 {
			request = strings.Replace(request, `{"package":"p"}`, `{"package":"p","directory":"`+strings.Repeat("d", c.directory)+`"}`, 1)
		}
		out, err := emit([]byte(request))
		if err != c.want || (err == nil) == (out == nil) {
			t.Errorf("%s: %d bytes, %v; want %v", c.name, len(out), err, c.want)
		}
	}
}

// TestIdentifier is the identifier check: an exported Go identifier of ASCII
// letters and digits, and nothing else.
func TestIdentifier(t *testing.T) {
	for _, name := range []string{"A", "Ab9", "DbRole", "XIDs", "X2fa"} {
		if !identifier(name) {
			t.Errorf("identifier(%q) = false", name)
		}
	}
	for _, name := range []string{"", "a", "aB", "9A", "_A", "A_b", "A-b", "A b", "A\"", "A\n", "Aé", "É", "A٠", "A.b", "A/b", "A*"} {
		if identifier(name) {
			t.Errorf("identifier(%q) = true", name)
		}
	}
}

func TestPart(t *testing.T) {
	for in, want := range map[string]string{
		"db-role": "DbRole", "connectionLimit": "ConnectionLimit", "db_name": "DbName", "2fa": "2fa",
		"café au lait": "CafAuLait", "%%": "", "x-i-ds": "XIDs", "a~1b": "A1b",
	} {
		if got := part(in); got != want {
			t.Errorf("part(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNamesAreFormedByTheRule: a field that begins with a digit gets an X,
// and the type of a struct's or an enumeration's position is its parent's
// type and its member's part, or Item.
func TestNamesAreFormedByTheRule(t *testing.T) {
	out, err := emit([]byte(requestOf(t, thing(`"2fa":{"type":"boolean"},"grants":{"type":"array","items":`+
		closed(`"role":{"enum":["read-only"]}`)+`}`))))
	if err != nil {
		t.Fatal(err)
	}
	_, files := readResponse(t, out)
	text := string(files["generated/loader/thing_kind.go"])
	for _, want := range []string{"\tX2fa *bool\n", "type ThingGrantsItem struct {", "type ThingGrantsItemRole string",
		`ThingGrantsItemRoleReadOnly ThingGrantsItemRole = "read-only"`, "func (r *Registry) ThingIDs() []string {",
		"func (r *Registry) Thing(ctx context.Context, id string) (*Thing, error) {"} {
		if !strings.Contains(text, want) {
			t.Errorf("the source does not hold %q", want)
		}
	}
}
