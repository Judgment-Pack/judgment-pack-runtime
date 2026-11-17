package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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

type vector struct {
	name     string
	request  []byte
	expected map[string][]byte // nil for an expected-failure vector
}

// readVectors reads vectors/ in the RFC 0036 §5 layout: request.json, and
// expected/ or expected-failure.json.
func readVectors(t *testing.T) []vector {
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
		request, err := os.ReadFile(filepath.Join(dir, "request.json"))
		if err != nil {
			t.Fatal(err)
		}
		v := vector{name: entry.Name(), request: canonical(t, request)}
		if failure, err := os.ReadFile(filepath.Join(dir, "expected-failure.json")); err == nil {
			if string(canonical(t, failure)) != `{"code":"EMIT-PROCESS"}` {
				t.Fatalf("%s: expected-failure.json is %s", v.name, failure)
			}
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
	if len(out) < 5 {
		t.Fatalf("read %d vectors, want at least 5", len(out))
	}
	return out
}

type response struct {
	EmitResponseVersion string `json:"emitResponseVersion"`
	Files               []struct {
		Path string `json:"path"`
		Text string `json:"text"`
	} `json:"files"`
	Diagnostics []any `json:"diagnostics"`
}

// readResponse checks that out is one canonical response of exactly the
// three members, and returns its files by path, in order.
func readResponse(t *testing.T, out []byte) ([]string, map[string][]byte) {
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
	if r.EmitResponseVersion != "1.0.0" || r.Diagnostics == nil || len(r.Diagnostics) != 0 {
		t.Fatalf("version %q, diagnostics %v", r.EmitResponseVersion, r.Diagnostics)
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

func TestVectors(t *testing.T) {
	for _, v := range readVectors(t) {
		t.Run(v.name, func(t *testing.T) {
			out, err := emit(v.request)
			if v.expected == nil {
				var r refusal
				if !errors.As(err, &r) || out != nil {
					t.Fatalf("emit = %q, %v; want a refusal and no response", out, err)
				}
				var stdout, stderr bytes.Buffer
				if code := run(bytes.NewReader(v.request), &stdout, &stderr); code == 0 || stdout.Len() != 0 {
					t.Fatalf("run exited %d with %d bytes of output; want non-zero and none", code, stdout.Len())
				}
				return
			}
			if err != nil {
				t.Fatalf("emit: %v", err)
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

// TestCollidingNamesVectorIsTheCollision pins the expected-failure vector to
// the refusal it is about, so it cannot pass for another reason.
func TestCollidingNamesVectorIsTheCollision(t *testing.T) {
	request, err := os.ReadFile(filepath.Join("vectors", "colliding-names", "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := emit(request); err != errCollide {
		t.Fatalf("emit: %v, want %v", err, errCollide)
	}
	var stdout, stderr bytes.Buffer
	if code := run(bytes.NewReader(request), &stdout, &stderr); code != 1 || stdout.Len() != 0 || stderr.String() != string(errCollide)+"\n" {
		t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRunWritesTheResponseAndNothingElse(t *testing.T) {
	request, err := os.ReadFile(filepath.Join("vectors", "every-type", "request.json"))
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
	request, err := os.ReadFile(filepath.Join("vectors", "every-type", "request.json"))
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

// TestEmitIsDeterministic runs every vector twice, and the two-contract
// vector with its contracts in the other order: the response is the same.
func TestEmitIsDeterministic(t *testing.T) {
	for _, v := range readVectors(t) {
		first, err1 := emit(v.request)
		second, err2 := emit(v.request)
		if !bytes.Equal(first, second) || !errors.Is(err1, err2) {
			t.Errorf("%s: two runs differ", v.name)
		}
	}
	request, err := os.ReadFile(filepath.Join("vectors", "two-contracts", "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	value, _ := decodeStrict(request)
	contracts := value.(map[string]any)["factsContracts"].([]any)
	slices.Reverse(contracts)
	reversed, err := jcs.EncodeDocument(value)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := emit(request)
	b, err := emit(reversed)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("the contracts' order changed the response: %v", err)
	}
}

// TestGoldenPackageIsTheVectorOutput holds internal/golden/dbrolefacts, the
// package whose tests compile and run the generated code, to the
// db-role-generated vector's output.
func TestGoldenPackageIsTheVectorOutput(t *testing.T) {
	request, err := os.ReadFile(filepath.Join("vectors", "db-role-generated", "request.json"))
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
		golden, err := os.ReadFile(filepath.Join("internal", "golden", "dbrolefacts", path.Base(p)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(golden, files[p]) {
			t.Errorf("internal/golden/dbrolefacts/%s differs from the emitter's %s", path.Base(p), p)
		}
	}
}

// TestManifest holds emitter.json to the program: its identity, input,
// request versions, the owns glob of the default directory, and an options
// schema whose rules are the ones readOptions applies.
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
	if !exactMembers(m, "emitterManifestVersion", "id", "version", "owner", "description", "reads", "input", "requestVersions", "options", "command", "env", "owns") {
		t.Fatalf("manifest members: %v", m)
	}
	var manifest struct {
		ManifestVersion string   `json:"emitterManifestVersion"`
		ID              string   `json:"id"`
		Version         string   `json:"version"`
		Input           string   `json:"input"`
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
	if manifest.ManifestVersion != "1.0.0" || manifest.ID != emitterID || manifest.Version != emitterVersion ||
		manifest.Input != "factsContracts" || !slices.Equal(manifest.RequestVersions, []string{requestVersion}) {
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

// base is a valid request of one contract, which each refusal case edits.
func base(facts string) string {
	return `{"emitRequestVersion":"1.0.0","emitter":{"id":"https://example.invalid/e","version":"1.0.0"},` +
		`"options":{"package":"p"},"factsContracts":[` + contractText("https://example.invalid/c/thing", facts) + `]}`
}

func contractText(id, facts string) string {
	return `{"factsContractVersion":"1.0.0","id":"` + id + `","version":"1.0.0","source":{"authored":true},"facts":[` + facts + `]}`
}

func TestRefusals(t *testing.T) {
	long := strings.Repeat("/a", 36)
	cases := []struct {
		name    string
		request string
		want    refusal
	}{
		{"not JSON", `{`, errRequest},
		{"trailing data", base(``) + `{}`, errRequest},
		{"repeated member", strings.Replace(base(``), `"options"`, `"options":{"package":"p"},"options"`, 1), errRequest},
		{"invalid UTF-8", strings.Replace(base(``), `"p"`, "\"p\xff\"", 1), errRequest},
		{"unpaired surrogate", strings.Replace(base(``), `"options"`, `"x\ud800":1,"options"`, 1), errRequest},
		{"too deep", strings.Replace(base(``), `{"package":"p"}`, `{"package":"p","x":`+strings.Repeat("[", maxDepth)+strings.Repeat("]", maxDepth)+`}`, 1), errRequest},
		{"not an object", `[]`, errRequest},
		{"an extra member", strings.Replace(base(``), `"options"`, `"extra":1,"options"`, 1), errRequest},
		{"the resolved form", strings.Replace(base(``), `"factsContracts"`, `"resolved"`, 1), errRequest},
		{"another request version", strings.Replace(base(``), `"1.0.0"`, `"1.1.0"`, 1), errRequest},
		{"an emitter without a version", strings.Replace(base(``), `,"version":"1.0.0"},"options"`, `},"options"`, 1), errRequest},
		{"contracts not an array", strings.Replace(base(``), `"factsContracts":[`, `"factsContracts":{"a":[`, 1) + `}`, errRequest},
		{"no package", strings.Replace(base(``), `{"package":"p"}`, `{}`, 1), errOptions},
		{"an upper-case package", strings.Replace(base(``), `"package":"p"`, `"package":"P"`, 1), errOptions},
		{"a keyword package", strings.Replace(base(``), `"package":"p"`, `"package":"type"`, 1), errOptions},
		{"a long package", strings.Replace(base(``), `"package":"p"`, `"package":"p`+strings.Repeat("a", maxPackageBytes)+`"`, 1), errOptions},
		{"another option", strings.Replace(base(``), `{"package":"p"}`, `{"package":"p","schema":"x"}`, 1), errOptions},
		{"options not an object", strings.Replace(base(``), `{"package":"p"}`, `[]`, 1), errOptions},
		{"a parent directory", strings.Replace(base(``), `{"package":"p"}`, `{"package":"p","directory":"../x"}`, 1), errOptions},
		{"an absolute directory", strings.Replace(base(``), `{"package":"p"}`, `{"package":"p","directory":"/x"}`, 1), errOptions},
		{"a .git directory", strings.Replace(base(``), `{"package":"p"}`, `{"package":"p","directory":"a/.git"}`, 1), errOptions},
		{"a directory ending in a slash", strings.Replace(base(``), `{"package":"p"}`, `{"package":"p","directory":"a/"}`, 1), errOptions},
		{"a long directory", strings.Replace(base(``), `{"package":"p"}`, `{"package":"p","directory":"`+strings.Repeat("a", maxDirectoryBytes+1)+`"}`, 1), errOptions},

		{"another contract version", strings.Replace(base(``), `"factsContractVersion":"1.0.0"`, `"factsContractVersion":"2.0.0"`, 1), errContract},
		{"a relative id", strings.Replace(base(``), `"https://example.invalid/c/thing"`, `"thing"`, 1), errContract},
		{"an id with a fragment", strings.Replace(base(``), `"https://example.invalid/c/thing"`, `"https://example.invalid/c/thing#x"`, 1), errContract},
		{"a version that is not SemVer", strings.Replace(base(``), `"version":"1.0.0","source"`, `"version":"1.0","source"`, 1), errContract},
		{"authored false", strings.Replace(base(``), `{"authored":true}`, `{"authored":false}`, 1), errContract},
		{"a kind without a digest", strings.Replace(base(``), `{"authored":true}`, `{"kind":{"id":"https://example.invalid/k","version":"1.0.0"}}`, 1), errContract},
		{"a kind with a bad digest", strings.Replace(base(``), `{"authored":true}`, `{"kind":{"id":"https://example.invalid/k","version":"1.0.0","digest":"sha256:00"}}`, 1), errContract},
		{"a contract with an extra member", strings.Replace(base(``), `"facts":[`, `"x":1,"facts":[`, 1), errContract},
		{"rows out of order", base(`{"pointer":"/b","type":"string","present":"always"},{"pointer":"/a","type":"string","present":"always"}`), errContract},
		{"a row listed twice", base(`{"pointer":"/a","type":"string","present":"always"},{"pointer":"/a","type":"string","present":"always"}`), errContract},
		{"types out of order", base(`{"pointer":"/a","type":"string","present":"always"},{"pointer":"/a","type":"decimal","present":"always"}`), errContract},
		{"a row with an extra member", base(`{"pointer":"/a","type":"string","present":"always","x":1}`), errContract},
		{"an unknown type", base(`{"pointer":"/a","type":"number","present":"always"}`), errContract},
		{"an unknown presence", base(`{"pointer":"/a","type":"string","present":"sometimes"}`), errContract},
		{"a pointer without a slash", base(`{"pointer":"a","type":"string","present":"always"}`), errContract},
		{"an unknown escape", base(`{"pointer":"/~3","type":"string","present":"always"}`), errContract},
		{"a pointer of 36 tokens", base(`{"pointer":"` + long + `","type":"string","present":"always"}`), errContract},
		{"items on a string", base(`{"pointer":"/a","type":"string","present":"always","items":"string"}`), errContract},
		{"secretReference false", base(`{"pointer":"/a","type":"object","present":"always","secretReference":false}`), errContract},
		{"secretReference on a string", base(`{"pointer":"/a","type":"string","present":"always","secretReference":true}`), errContract},
		{"empty values", base(`{"pointer":"/a","type":"string","present":"always","values":[]}`), errContract},
		{"values on a null", base(`{"pointer":"/a","type":"null","present":"always","values":[null]}`), errContract},
		{"values on an object", base(`{"pointer":"/a","type":"object","present":"always","values":[{}]}`), errContract},
		{"a value listed twice", base(`{"pointer":"/a","type":"string","present":"always","values":["x","x"]}`), errContract},
		{"a value of another type", base(`{"pointer":"/a","type":"string","present":"always","values":[1]}`), errContract},
		{"a decimal with an exponent", base(`{"pointer":"/a","type":"decimal","present":"always","values":["1e-7"]}`), errContract},
		{"a decimal listed twice", base(`{"pointer":"/a","type":"decimal","present":"always","values":["1","1"]}`), errContract},
		{"a boolean listed twice", base(`{"pointer":"/a","type":"boolean","present":"always","values":[true,true]}`), errContract},

		{"a pointer of two types", base(`{"pointer":"/a","type":"decimal","present":"always"},{"pointer":"/a","type":"string","present":"always"}`), errUnsupported},
		{"a row without its parent", base(`{"pointer":"/a/b","type":"string","present":"always"}`), errUnsupported},
		{"* at the root", base(`{"pointer":"/*","type":"string","present":"optional"}`), errUnsupported},
		{"members and *", base(`{"pointer":"/a","type":"object","present":"always"},{"pointer":"/a/*","type":"string","present":"optional"},{"pointer":"/a/b","type":"string","present":"always"}`), errUnsupported},
		{"* then a member", base(`{"pointer":"/a","type":"object","present":"always"},{"pointer":"/a/*","type":"string","present":"optional"},{"pointer":"/a/~2","type":"string","present":"always"}`), errUnsupported},
		{"an array without items", base(`{"pointer":"/a","type":"array","present":"always"}`), errUnsupported},
		{"a member under an array", base(`{"pointer":"/a","type":"array","present":"always","items":"string"},{"pointer":"/a/b","type":"string","present":"always"}`), errUnsupported},
		{"a row under a string", base(`{"pointer":"/a","type":"string","present":"always"},{"pointer":"/a/b","type":"string","present":"always"}`), errUnsupported},
		{"an element of another type", base(`{"pointer":"/a","type":"array","present":"always","items":"string"},{"pointer":"/a/*","type":"decimal","present":"optional"}`), errUnsupported},
		{"element values that differ", base(`{"pointer":"/a","type":"array","present":"always","items":"string","values":["x"]},{"pointer":"/a/*","type":"string","present":"optional","values":["y"]}`), errUnsupported},
		{"an array of objects without its element", base(`{"pointer":"/a","type":"array","present":"always","items":"object"}`), errUnsupported},
		{"values on an array of objects", base(`{"pointer":"/a","type":"array","present":"always","items":"object","values":[{}]}`), errUnsupported},
		{"a secret with a third member", base(`{"pointer":"/s","type":"object","present":"always","secretReference":true},{"pointer":"/s/scope","type":"string","present":"always"},{"pointer":"/s/store","type":"string","present":"always"},{"pointer":"/s/x","type":"string","present":"always"}`), errUnsupported},
		{"a secret with an optional scope", base(`{"pointer":"/s","type":"object","present":"always","secretReference":true},{"pointer":"/s/scope","type":"string","present":"optional"},{"pointer":"/s/store","type":"string","present":"always"}`), errUnsupported},

		{"an id with no word", strings.Replace(base(``), `"https://example.invalid/c/thing"`, `"https://example.invalid/c/---"`, 1), errName},
		{"an id beginning with a digit", strings.Replace(base(``), `"https://example.invalid/c/thing"`, `"https://example.invalid/c/2fa"`, 1), errName},
		{"a member with no word", base(`{"pointer":"/%%","type":"string","present":"always"}`), errName},
		{"an object token with no word", base(`{"pointer":"/%%","type":"object","present":"always"}`), errName},
		{"a value with no word", base(`{"pointer":"/a","type":"string","present":"always","values":["!!"]}`), errName},

		{"two fields", base(`{"pointer":"/a-b","type":"string","present":"always"},{"pointer":"/a_b","type":"string","present":"always"}`), errCollide},
		{"a field named Build", base(`{"pointer":"/build","type":"string","present":"always"}`), errCollide},
		{"a type named as the facts struct", base(`{"pointer":"/facts","type":"object","present":"always"}`), errCollide},
		{"two constants", base(`{"pointer":"/a","type":"string","present":"always","values":["x-y","x_y"]}`), errCollide},
		{"a type named as a support name", strings.Replace(base(`{"pointer":"/error","type":"object","present":"always"}`), `/c/thing`, `/c/build`, 1), errCollide},
		{"an argument named as its type", strings.Replace(base(`{"pointer":"/8","type":"object","present":"always"},{"pointer":"/8/int8","type":"string","present":"always"}`), `/c/thing`, `/c/int`, 1), errCollide},
		{"a type named as a constructor", strings.Replace(base(``), contractText("https://example.invalid/c/thing", ``),
			contractText("https://example.invalid/c/a", ``)+","+contractText("https://example.invalid/c/new-a", ``), 1), errCollide},
		{"two files", strings.Replace(base(``), contractText("https://example.invalid/c/thing", ``),
			contractText("https://example.invalid/c/thing", ``)+","+contractText("https://example.invalid/d/thing", ``), 1), errCollide},

		{"a file name past the bound", strings.Replace(base(``), `/c/thing`, `/c/`+strings.Repeat("a", maxPathBytes), 1), errBounds},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := emit([]byte(c.request))
			if err != c.want || out != nil {
				t.Fatalf("emit = %d bytes, %v; want %v", len(out), err, c.want)
			}
		})
	}
}

func TestNoContractsWritesNoFile(t *testing.T) {
	out, err := emit([]byte(strings.Replace(base(``), contractText("https://example.invalid/c/thing", ``), ``, 1)))
	if err != nil || string(out) != `{"diagnostics":[],"emitResponseVersion":"1.0.0","files":[]}` {
		t.Fatalf("emit = %s, %v", out, err)
	}
}

func TestDirectoryOption(t *testing.T) {
	request := strings.Replace(base(`{"pointer":"/a","type":"string","present":"always"}`), `{"package":"p"}`, `{"package":"p","directory":"internal/facts.gen"}`, 1)
	out, err := emit([]byte(request))
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := readResponse(t, out)
	if !slices.Equal(paths, []string{"internal/facts.gen/facts_support.go", "internal/facts.gen/thing_facts.go"}) {
		t.Fatalf("paths %v", paths)
	}
}

// TestContractTextIsQuoted: a member name, a value and a pointer reach the
// source only as Go string literals, so no text of the contract can end a
// comment or a literal, and the files are ASCII.
func TestContractTextIsQuoted(t *testing.T) {
	request := base(`{"pointer":"/caf\u00e9 *~1\n\"x","type":"string","present":"always","values":["\u00e9\"\\` + "`" + `v"]}`)
	out, err := emit([]byte(request))
	if err != nil {
		t.Fatal(err)
	}
	_, files := readResponse(t, out)
	text := string(files["generated/facts/thing_facts.go"])
	for _, want := range []string{
		`b = appendName(b, start, "\"caf\u00e9 */\\n\\\"x\":")`,
		`"/caf\u00e9 *~1\n\"x"`,
		"ThingCafXV ThingCafX = " + `"\u00e9\"\\` + "`" + `v"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the source does not hold %s", want)
		}
	}
	for i := 0; i < len(text); i++ {
		if text[i] >= 0x80 {
			t.Fatalf("the source holds a byte outside ASCII at %d", i)
		}
	}
}

func TestNormalDecimal(t *testing.T) {
	for in, want := range map[string]string{
		"0": "0", "-0": "0", "-0.0": "0", "1.0": "1", "1.50": "1.5", "100": "100", "-100": "-100",
		"0.0000001": "0.0000001", "9007199254740991": "9007199254740991", "-9007199254740991": "-9007199254740991",
		"12345678901234567890.123456789": "12345678901234567890.123456789",
	} {
		if got, ok := normalDecimal(in); !ok || got != want {
			t.Errorf("normalDecimal(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "-", "01", "+1", ".5", "1.", "1e-7", "1E7", " 1", "1 ", "0x10", "1.2.3", "--1", "１"} {
		if got, ok := normalDecimal(in); ok {
			t.Errorf("normalDecimal(%q) = %q; want a refusal", in, got)
		}
	}
}

// TestDecimalValuesAreNormalized: a contract's decimal values are compared
// in normal form, and two spellings of one value are listed once.
func TestDecimalValuesAreNormalized(t *testing.T) {
	out, err := emit([]byte(base(`{"pointer":"/a","type":"decimal","present":"always","values":["1.0","1","2.50"]}`)))
	if err != nil {
		t.Fatal(err)
	}
	_, files := readResponse(t, out)
	if !strings.Contains(string(files["generated/facts/thing_facts.go"]), "\tcase \"1\", \"2.5\":\n") {
		t.Fatalf("the decimal check is not over the normal forms")
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
	for _, s := range []string{"p", "facts2", "", "P", "2p", "p_q", "p-q", "pé", "p\n"} {
		if pkg.MatchString(s) != packagePattern.MatchString(s) {
			t.Errorf("package: the manifest and the program differ on %q", s)
		}
	}
	dir := compile(manifest.Options.Properties["directory"].Pattern)
	notDir := compile(manifest.Options.Properties["directory"].Not.Pattern)
	for _, s := range []string{"generated/facts", "a", "a.b/c-d/e_", "", "/a", "a/", "a//b", ".", "..", "a/..", ".git", "a/.git",
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

// TestPackageMainIsRefused: a package named main is a command, which no
// other package can import, so the generated code could never be used.
func TestPackageMainIsRefused(t *testing.T) {
	for _, c := range []struct {
		pkg  string
		want error
	}{
		{"facts", nil},
		{"mainfacts", nil},
		{"main", errOptions},
		{"documentation", errOptions},
		{"documentations", nil},
		{"type", errOptions},
		{"Main", errOptions},
	} {
		request := strings.Replace(base(`{"pointer":"/a","type":"string","present":"always"}`), `"package":"p"`, `"package":"`+c.pkg+`"`, 1)
		if _, err := emit([]byte(request)); err != c.want {
			t.Errorf("package %q: %v, want %v", c.pkg, err, c.want)
		}
	}
}

// TestDirectoryTheToolchainIgnoresIsRefused: the Go toolchain skips a
// directory whose name begins with "_" or "." and one named testdata, so
// code generated below one would never be compiled.
func TestDirectoryTheToolchainIgnoresIsRefused(t *testing.T) {
	for _, c := range []struct {
		directory string
		want      error
	}{
		{"generated/facts", nil},
		{"internal/facts_", nil},
		{"testdata2", nil},
		{"a/my_testdata", nil},
		{"a/testdatax/b", nil},
		{"_gen", errOptions},
		{"generated/_facts", errOptions},
		{".gen", errOptions},
		{"generated/.facts", errOptions},
		{"testdata", errOptions},
		{"testdata/facts", errOptions},
		{"generated/testdata", errOptions},
		{"generated/testdata/facts", errOptions},
		{"vendor", errOptions},
		{"vendor/facts", errOptions},
		{"generated/vendor", errOptions},
		{"generated/vendor/facts", errOptions},
		{"TestData", errOptions},
		{"generated/Vendor/facts", errOptions},
		{"VENDOR", errOptions},
		{"vendors", nil},
		{"a/myvendor/b", nil},
	} {
		request := strings.Replace(base(`{"pointer":"/a","type":"string","present":"always"}`), `{"package":"p"}`,
			`{"package":"p","directory":"`+c.directory+`"}`, 1)
		if _, err := emit([]byte(request)); err != c.want {
			t.Errorf("directory %q: %v, want %v", c.directory, err, c.want)
		}
	}
}

// TestPathSegmentBound: a file name, or a directory name, over 255 bytes is
// errBounds, although its path is within the 512-byte bound: no common file
// system could hold it.
func TestPathSegmentBound(t *testing.T) {
	stem := len("_facts.go")
	for _, c := range []struct {
		name      string
		segment   int // the contract id's last segment
		directory int // one directory segment; 0 for the default
		want      error
	}{
		{"a file name of 255 bytes", maxSegmentBytes - stem, 0, nil},
		{"a file name of 256 bytes", maxSegmentBytes - stem + 1, 0, errBounds},
		{"a file name of 300 letters and more", 300, 0, errBounds},
		{"a directory name of 255 bytes", 1, maxSegmentBytes, nil},
		{"a directory name of 256 bytes", 1, maxSegmentBytes + 1, errBounds},
	} {
		request := strings.Replace(base(``), `/c/thing`, `/c/`+strings.Repeat("a", c.segment), 1)
		if c.directory > 0 {
			request = strings.Replace(request, `{"package":"p"}`, `{"package":"p","directory":"`+strings.Repeat("d", c.directory)+`"}`, 1)
		}
		out, err := emit([]byte(request))
		if err != c.want || (err == nil) == (out == nil) {
			t.Errorf("%s: %d bytes, %v; want %v", c.name, len(out), err, c.want)
		}
	}
}

// TestMembersAreWrittenInCanonicalOrder: Build writes an object's members by
// the UTF-16 code units of their names (RFC 8785), not in the contract's
// order of escaped tokens: "/b" (written ~1b) comes before "a", and a name
// with a character above U+FFFF before one with U+E000.
func TestMembersAreWrittenInCanonicalOrder(t *testing.T) {
	request := base(`{"pointer":"/a","type":"string","present":"always"},` +
		`{"pointer":"/x\ue000a","type":"string","present":"always"},` +
		`{"pointer":"/x\ud83d\ude00b","type":"string","present":"always"},` +
		`{"pointer":"/~1b","type":"string","present":"always"}`)
	out, err := emit([]byte(request))
	if err != nil {
		t.Fatal(err)
	}
	_, files := readResponse(t, out)
	text := string(files["generated/facts/thing_facts.go"])
	last := -1
	for _, name := range []string{`"\"/b\":"`, `"\"a\":"`, `"\"x\U0001f600b\":"`, `"\"x\ue000a\":"`} {
		i := strings.Index(text, "b = appendName(b, start, "+name+")")
		if i <= last {
			t.Fatalf("%s is not written after the members before it in canonical order", name)
		}
		last = i
	}
}
