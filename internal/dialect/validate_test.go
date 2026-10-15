package dialect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func sampleDocument(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(`{
	 "version":2,
	 "case":{"parameters":{"id":{"pointer":"/id","type":"string"},"count":{"pointer":"/count","type":"integer"}},"facts":[{"target":"/case/id","source":"/id"}],"evidence":[]},
	 "sources":[
	  {"name":"first","kind":"operation","profile":"profile","profileDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","maxAge":60,"arguments":{"tool":"fetch","arguments":{"id":{"$param":"id"},"count":{"$text":"{{count}}"}}},"read":{"copy":{"facts":[{"target":"/first","source":""}],"evidence":[{"requirement":"first-proof","source":"/evidence"}]}}},
	  {"name":"second","kind":"operation","profile":"profile","profileDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","maxAge":60,"parameters":{"firstValue":{"from":"first","pointer":"/first","type":"integer"}},"arguments":{"value":{"$param":"firstValue"}},"calculation":{"inputs":{"value":"firstValue"},"tables":{"rates":315360000}},"read":{"unwrap":["/content"],"rule":{"ruleVersion":"1","parameters":{"firstValue":"integer","runAt":"timestamp"},"clauses":[{"when":{"op":"equalsParam","field":"/value","param":"firstValue"},"claim":{"facts":[{"pointer":"/second","from":"/value"}],"evidence":{"second-proof":"present"},"acquisitionStatus":"resolved"},"reason":"match"},{"when":{"op":"always"},"claim":{"facts":[],"evidence":{"second-proof":"unknown"},"acquisitionStatus":"unknown"},"reason":"otherwise"}]}}}
	 ],
	 "admits":{"facts":{"/first":["record"]},"evidence":{"first-proof":["asserted","record"]}},
	 "unmapped":["/not-mapped"],"unmappedEvidence":["not-mapped"],"provider":"","facts":null,"evidence":null
	}`), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func validateValue(t *testing.T, value any) (Report, int) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return Validate("test", data)
}
func sourceAt(d map[string]any, i int) map[string]any {
	return d["sources"].([]any)[i].(map[string]any)
}
func sampleRule(d map[string]any) map[string]any {
	return sourceAt(d, 1)["read"].(map[string]any)["rule"].(map[string]any)
}
func sampleClause(d map[string]any, i int) map[string]any {
	return sampleRule(d)["clauses"].([]any)[i].(map[string]any)
}

func TestVersion2DocumentValidation(t *testing.T) {
	d := sampleDocument(t)
	r, code := validateValue(t, d)
	if code != 0 || r.Status != "document-valid" || r.ValidationScope != "version-2-document" || r.FullDocumentConformance || r.Document == "" || r.Digest == "" {
		t.Fatal(code, r)
	}
	before, _ := json.Marshal(d)
	encoded, _ := Encode("test", before)
	if r.Document != encoded.Document || r.Digest != encoded.Digest {
		t.Fatal("validation rewrote identity")
	}
	for _, version := range []int{2, 3} {
		starter, _ := Init("test", version)
		r, code := Validate("test", []byte(starter.Document))
		if version == 2 && code != 0 {
			t.Fatal(r)
		}
		if version == 3 && (code != result.ExitUnsupported || r.Document != "" || r.Digest != "") {
			t.Fatal(code, r)
		}
	}
	files, err := filepath.Glob("testdata/*/mapping.json")
	if err != nil || len(files) != 12 {
		t.Fatal(files, err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var input struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			t.Fatal(err)
		}
		r, code := Validate("test", data)
		if input.Version == 3 {
			if code != result.ExitUnsupported || r.Document != "" || r.Digest != "" {
				t.Fatalf("unsupported version-3 validation: %s %+v", file, r)
			}
		} else if input.Version != 2 || code != 0 {
			t.Fatalf("%s: %v", file, r.Diagnostics)
		}
	}
}

func TestVersion2DocumentRefusals(t *testing.T) {
	cases := []struct {
		name, path string
		change     func(map[string]any)
	}{
		{"copy requires facts", "/sources/0/read/copy/facts", func(d map[string]any) {
			delete(sourceAt(d, 0)["read"].(map[string]any)["copy"].(map[string]any), "facts")
		}},
		{"copy requires evidence", "/sources/0/read/copy/evidence", func(d map[string]any) {
			delete(sourceAt(d, 0)["read"].(map[string]any)["copy"].(map[string]any), "evidence")
		}},
		{"copy requires both arrays", "/sources/0/read/copy/facts", func(d map[string]any) { sourceAt(d, 0)["read"].(map[string]any)["copy"] = map[string]any{} }},
		{"unknown root", "/extra", func(d map[string]any) { d["extra"] = true }},
		{"missing case", "/case", func(d map[string]any) { delete(d, "case") }},
		{"null facts", "/case/facts", func(d map[string]any) { d["case"].(map[string]any)["facts"] = nil }},
		{"legacy provider", "/provider", func(d map[string]any) { d["provider"] = "file" }},
		{"legacy facts", "/facts", func(d map[string]any) { d["facts"] = []any{} }},
		{"pointer escape", "/unmapped/0", func(d map[string]any) { d["unmapped"] = []any{"/a~2"} }},
		{"duplicate unmapped", "/unmapped/1", func(d map[string]any) { d["unmapped"] = []any{"/a", "/a"} }},
		{"empty evidence id", "/unmappedEvidence/0", func(d map[string]any) { d["unmappedEvidence"] = []any{""} }},
		{"case scope reserved", "/case/parameters/runAt", func(d map[string]any) {
			d["case"].(map[string]any)["parameters"].(map[string]any)["runAt"] = map[string]any{"pointer": "/now", "type": "timestamp"}
		}},
		{"source reserved", "/sources/0/name", func(d map[string]any) { sourceAt(d, 0)["name"] = "case" }},
		{"source repeated", "/sources/1/name", func(d map[string]any) { sourceAt(d, 1)["name"] = "first" }},
		{"version3 kind", "/sources/0/kind", func(d map[string]any) { sourceAt(d, 0)["kind"] = "research" }},
		{"unbound source", "/sources/0/platform", func(d map[string]any) { sourceAt(d, 0)["platform"] = "web" }},
		{"bad digest", "/sources/0/profileDigest", func(d map[string]any) { sourceAt(d, 0)["profileDigest"] = "sha256:" + strings.Repeat("A", 64) }},
		{"zero age", "/sources/0/maxAge", func(d map[string]any) { sourceAt(d, 0)["maxAge"] = 0 }},
		{"bool age", "/sources/0/maxAge", func(d map[string]any) { sourceAt(d, 0)["maxAge"] = true }},
		{"source parameter forward", "/sources/1/parameters/firstValue/from", func(d map[string]any) {
			sourceAt(d, 1)["parameters"].(map[string]any)["firstValue"].(map[string]any)["from"] = "second"
		}},
		{"source parameter ancestor", "/sources/1/parameters/firstValue/pointer", func(d map[string]any) {
			sourceAt(d, 1)["parameters"].(map[string]any)["firstValue"].(map[string]any)["pointer"] = "/"
		}},
		{"source parameter shadow", "/sources/1/parameters/id", func(d map[string]any) {
			sourceAt(d, 1)["parameters"].(map[string]any)["id"] = map[string]any{"from": "first", "pointer": "/first", "type": "string"}
		}},
		{"request array", "/sources/0/arguments", func(d map[string]any) { sourceAt(d, 0)["arguments"] = []any{} }},
		{"request form", "/sources/0/arguments", func(d map[string]any) { sourceAt(d, 0)["arguments"] = map[string]any{"$param": "id"} }},
		{"request runAt", "/sources/0/arguments/x", func(d map[string]any) {
			sourceAt(d, 0)["arguments"] = map[string]any{"x": map[string]any{"$param": "runAt"}}
		}},
		{"request unknown parameter", "/sources/0/arguments/x", func(d map[string]any) {
			sourceAt(d, 0)["arguments"] = map[string]any{"x": map[string]any{"$param": "missing"}}
		}},
		{"request mixed form", "/sources/0/arguments/x", func(d map[string]any) {
			sourceAt(d, 0)["arguments"] = map[string]any{"x": map[string]any{"$param": "id", "other": true}}
		}},
		{"request unknown form", "/sources/0/arguments/x", func(d map[string]any) {
			sourceAt(d, 0)["arguments"] = map[string]any{"x": map[string]any{"$unknown": "id"}}
		}},
		{"text unclosed", "/sources/0/arguments/x", func(d map[string]any) {
			sourceAt(d, 0)["arguments"] = map[string]any{"x": map[string]any{"$text": "{{count"}}
		}},
		{"text noninteger", "/sources/0/arguments/x", func(d map[string]any) {
			sourceAt(d, 0)["arguments"] = map[string]any{"x": map[string]any{"$text": "{{id}}"}}
		}},
		{"read exclusive", "/sources/0/read", func(d map[string]any) { sourceAt(d, 0)["read"].(map[string]any)["rule"] = sampleRule(d) }},
		{"unwrap empty", "/sources/1/read/unwrap", func(d map[string]any) { sourceAt(d, 1)["read"].(map[string]any)["unwrap"] = []any{} }},
		{"target root", "/sources/0/read/copy/facts/0/target", func(d map[string]any) {
			sourceAt(d, 0)["read"].(map[string]any)["copy"].(map[string]any)["facts"].([]any)[0].(map[string]any)["target"] = ""
		}},
		{"target overlap", "/sources/0/read/copy/facts/0/target", func(d map[string]any) {
			d["case"].(map[string]any)["facts"] = []any{map[string]any{"target": "/first/a", "source": ""}}
		}},
		{"copy evidence duplicate", "/sources/0/read/copy/evidence/1/requirement", func(d map[string]any) {
			c := sourceAt(d, 0)["read"].(map[string]any)["copy"].(map[string]any)
			c["evidence"] = []any{map[string]any{"requirement": "same", "source": ""}, map[string]any{"requirement": "same", "source": ""}}
		}},
		{"rule version", "/sources/1/read/rule/ruleVersion", func(d map[string]any) { sampleRule(d)["ruleVersion"] = 1 }},
		{"rule scope", "/sources/1/read/rule/parameters/firstValue", func(d map[string]any) { sampleRule(d)["parameters"].(map[string]any)["firstValue"] = "string" }},
		{"last not always", "/sources/1/read/rule/clauses/1/when", func(d map[string]any) { sampleClause(d, 1)["when"] = map[string]any{"op": "exists", "field": ""} }},
		{"condition unknown", "/sources/1/read/rule/clauses/0/when/op", func(d map[string]any) { sampleClause(d, 0)["when"] = map[string]any{"op": "eval"} }},
		{"condition extra", "/sources/1/read/rule/clauses/0/when/extra", func(d map[string]any) { sampleClause(d, 0)["when"].(map[string]any)["extra"] = true }},
		{"condition scope", "/sources/1/read/rule/clauses/0/when/param", func(d map[string]any) { sampleClause(d, 0)["when"].(map[string]any)["param"] = "id" }},
		{"empty clause reason type", "/sources/1/read/rule/clauses/0/reason", func(d map[string]any) { sampleClause(d, 0)["reason"] = nil }},
		{"claim status", "/sources/1/read/rule/clauses/0/claim/acquisitionStatus", func(d map[string]any) { sampleClause(d, 0)["claim"].(map[string]any)["acquisitionStatus"] = "error" }},
		{"claim availability", "/sources/1/read/rule/clauses/0/claim/evidence/second-proof", func(d map[string]any) {
			sampleClause(d, 0)["claim"].(map[string]any)["evidence"].(map[string]any)["second-proof"] = true
		}},
		{"calculation runAt", "/sources/1/calculation/inputs/value", func(d map[string]any) {
			sourceAt(d, 1)["calculation"].(map[string]any)["inputs"].(map[string]any)["value"] = "runAt"
		}},
		{"calculation age", "/sources/1/calculation/tables/rates", func(d map[string]any) {
			sourceAt(d, 1)["calculation"].(map[string]any)["tables"].(map[string]any)["rates"] = 315360001
		}},
		{"admission unwritten", "/admits/facts/~1unknown", func(d map[string]any) { d["admits"].(map[string]any)["facts"].(map[string]any)["/unknown"] = []any{} }},
		{"admission class", "/admits/facts/~1first/0", func(d map[string]any) {
			d["admits"].(map[string]any)["facts"].(map[string]any)["/first"] = []any{"trusted"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := sampleDocument(t)
			tc.change(d)
			r, code := validateValue(t, d)
			if code != result.ExitInvalid || r.Status != "refused" || r.Document != "" || r.Digest != "" || len(r.Diagnostics) != 1 || r.Diagnostics[0].InstancePath != tc.path {
				t.Fatalf("code=%d report=%+v", code, r)
			}
		})
	}
}

func TestVersion2ReadConditionsAndFiles(t *testing.T) {
	for _, condition := range []any{
		map[string]any{"op": "always"}, map[string]any{"op": "exists", "field": ""}, map[string]any{"op": "equals", "field": "/x", "to": map[string]any{"n": nil}},
		map[string]any{"op": "isTrue", "field": "/x"}, map[string]any{"op": "isDecimalString", "field": "/x"},
		// Types determine freshWithin's truth, not whether its declared form is valid.
		map[string]any{"op": "freshWithin", "field": "/x", "asOf": "firstValue", "maxAge": "runAt"},
		map[string]any{"op": "all", "of": []any{}}, map[string]any{"op": "any", "of": []any{map[string]any{"op": "not", "of": map[string]any{"op": "always"}}}},
	} {
		d := sampleDocument(t)
		sampleClause(d, 0)["when"] = condition
		if r, code := validateValue(t, d); code != 0 {
			t.Fatal(condition, r.Diagnostics)
		}
	}
	for _, provider := range []string{"local-file", "google-drive"} {
		d := sampleDocument(t)
		s := sourceAt(d, 0)
		s["kind"] = "selected-file"
		s["provider"] = provider
		if provider == "local-file" {
			for _, k := range []string{"profile", "profileDigest", "maxAge", "arguments"} {
				delete(s, k)
			}
		}
		if r, code := validateValue(t, d); code != 0 {
			t.Fatal(provider, r.Diagnostics)
		}
		s["calculation"] = map[string]any{"inputs": map[string]any{}, "tables": map[string]any{}}
		if r, code := validateValue(t, d); code == 0 {
			t.Fatal("selected-file accepted calculation", r)
		}
	}
	// A malformed profile-dependent MCP/HTTP request is not claimed checked.
	d := sampleDocument(t)
	sourceAt(d, 0)["arguments"] = map[string]any{"query": map[string]any{"n": 10}}
	if r, code := validateValue(t, d); code != 0 || r.FullDocumentConformance {
		t.Fatal(code, r)
	}
}

func minimalDocument() map[string]any {
	return map[string]any{"version": 2, "case": map[string]any{"facts": []any{}, "evidence": []any{}}}
}
func TestVersion2CountBoundaries(t *testing.T) {
	for _, test := range []struct {
		name string
		max  int
		set  func(map[string]any, int)
	}{
		{"sources", 16, func(d map[string]any, n int) {
			a := []any{}
			for i := 0; i < n; i++ {
				a = append(a, map[string]any{"name": fmt.Sprint(i), "kind": "selected-file", "provider": "local-file", "read": map[string]any{"copy": map[string]any{"facts": []any{}, "evidence": []any{}}}})
			}
			d["sources"] = a
		}},
		{"parameters", 63, func(d map[string]any, n int) {
			p := map[string]any{}
			for i := 0; i < n; i++ {
				p[fmt.Sprint(i)] = map[string]any{"pointer": "", "type": "integer"}
			}
			d["case"].(map[string]any)["parameters"] = p
		}},
		{"facts", 256, func(d map[string]any, n int) {
			a := []any{}
			for i := 0; i < n; i++ {
				a = append(a, map[string]any{"target": "/" + fmt.Sprint(i), "source": ""})
			}
			d["case"].(map[string]any)["facts"] = a
		}},
		{"evidence", 128, func(d map[string]any, n int) {
			a := []any{}
			for i := 0; i < n; i++ {
				a = append(a, map[string]any{"requirement": fmt.Sprint(i), "source": ""})
			}
			d["case"].(map[string]any)["evidence"] = a
		}},
	} {
		for _, n := range []int{test.max, test.max + 1} {
			t.Run(fmt.Sprintf("%s-%d", test.name, n), func(t *testing.T) {
				d := minimalDocument()
				test.set(d, n)
				r, code := validateValue(t, d)
				if (code == 0) != (n == test.max) {
					t.Fatal(code, r)
				}
				if code != 0 && r.Diagnostics[0].Code != "JPS-DIALECT-COUNT-LIMIT" {
					t.Fatal(r)
				}
			})
		}
	}
	for _, n := range []int{64, 65} {
		d := sampleDocument(t)
		a := []any{}
		for i := 0; i < n; i++ {
			a = append(a, map[string]any{"op": "always"})
		}
		sampleClause(d, 0)["when"] = map[string]any{"op": "all", "of": a}
		if r, code := validateValue(t, d); (code == 0) != (n == 64) {
			t.Fatal(n, r)
		}
		d = sampleDocument(t)
		a = []any{}
		for i := 0; i < n; i++ {
			a = append(a, sampleClause(d, 1))
		}
		sampleRule(d)["clauses"] = a
		if r, code := validateValue(t, d); (code == 0) != (n == 64) {
			t.Fatal(n, r)
		}
	}
}

func FuzzDialectValidation(f *testing.F) {
	for _, seed := range []string{`{"version":2,"case":{"facts":[],"evidence":[]}}`, `{"version":2,"case":null}`, `{"version":2,"sources":[null]}`, `{"version":3}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		r, code := Validate("test", data)
		if code != 0 && (r.Document != "" || r.Digest != "") {
			t.Fatal("refusal retained a usable artifact")
		}
		if code == 0 {
			again, exit := Validate("test", []byte(r.Document))
			if exit != 0 || again.Digest != r.Digest {
				t.Fatal("validation is not stable")
			}
		}
	})
}

func TestVersion2TargetOwnershipAndTokenPrefixes(t *testing.T) {
	for _, tc := range []struct {
		a, b  string
		valid bool
	}{
		{"/a", "/ab", true}, {"/a~1b", "/a/b", true}, {"/a~01", "/a~1", true},
		{"/a", "/a", false}, {"/a", "/a/", false}, {"/a~1b", "/a~1b/c", false},
	} {
		d := minimalDocument()
		d["case"].(map[string]any)["facts"] = []any{map[string]any{"target": tc.a, "source": ""}, map[string]any{"target": tc.b, "source": ""}}
		if r, code := validateValue(t, d); (code == 0) != tc.valid {
			t.Fatal(tc, r)
		}
	}
	// Alternative clauses can name the same target: the source has one target
	// regardless of which clause produces it, and the whole suite counts it once.
	d := sampleDocument(t)
	sampleClause(d, 1)["claim"] = sampleClause(d, 0)["claim"]
	if r, code := validateValue(t, d); code != 0 {
		t.Fatal(r)
	}
	// An evidence target may repeat across clauses, but cannot have two owners.
	d = sampleDocument(t)
	d["case"].(map[string]any)["evidence"] = []any{map[string]any{"requirement": "first-proof", "source": "/proof"}}
	if r, code := validateValue(t, d); code != 1 || !strings.Contains(r.Diagnostics[0].Message, "more than one source") {
		t.Fatal(r)
	}
	// A global target count still applies when every individual copy fits.
	for _, n := range []int{128, 129} {
		d = minimalDocument()
		sources := []any{}
		for j := 0; j < 2; j++ {
			facts := []any{}
			for i := 0; i < n; i++ {
				facts = append(facts, map[string]any{"target": fmt.Sprintf("/source%d/%d", j, i), "source": ""})
			}
			sources = append(sources, map[string]any{"name": fmt.Sprint(j), "kind": "selected-file", "provider": "local-file", "read": map[string]any{"copy": map[string]any{"facts": facts, "evidence": []any{}}}})
		}
		d["sources"] = sources
		if r, code := validateValue(t, d); (code == 0) != (n == 128) {
			t.Fatal(n, r)
		}
	}
}
