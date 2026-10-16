package dialect

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
	"os"
	"strings"
	"testing"
)

func researchSample(t *testing.T) map[string]any {
	t.Helper()
	raw := `{"version":3,"case":{"parameters":{"at":{"pointer":"/at","type":"timestamp"}},"facts":[],"evidence":[]},"sources":[{"name":"policy","kind":"research","maxAge":3600,"question":{"$param":"at"},"constraints":{},"model":"example-model","evidence":[],"fields":[{"name":"retention","type":"integer","required":true,"description":"Published retention period."}],"capabilities":["answer"],"read":{"copy":{"facts":[{"target":"/policy/retention","source":"/fields/retention/value"}],"evidence":[]}}}],"admits":{"facts":{"/policy/retention":["generated"]}}}`
	var d map[string]any
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	return d
}
func TestResearchAuthoringChecksAndSchema(t *testing.T) {
	for _, kind := range []string{"integer", "decimal", "boolean", "text", "text-list", "range"} {
		d := researchSample(t)
		source := sourceAt(d, 0)
		source["fields"].([]any)[0].(map[string]any)["type"] = kind
		for _, bound := range []bool{false, true} {
			if bound {
				source["profile"] = "ai-local"
				source["profileDigest"] = "sha256:" + strings.Repeat("a", 64)
				source["webProfiles"] = map[string]any{}
			}
			report, code := validateValue(t, d)
			if code != 0 || report.Status != "document-valid" || report.FullDocumentConformance {
				t.Fatal(kind, bound, report)
			}
		}
	}
	raw, err := os.ReadFile("testdata/research-authoring/exchange-preset.json")
	if err != nil {
		t.Fatal(err)
	}
	report, code := Validate("test", raw)
	if code != 0 {
		t.Fatal(report)
	}
	schemaReport, _ := Schema("test", 3)
	schema, err := validation.CompileSchema([]byte(schemaReport.Resource.Text), "urn:judgmentpack:runtime:dialect:3:authoring-schema:1")
	if err != nil {
		t.Fatal(err)
	}
	var value any
	json.Unmarshal(raw, &value)
	if err = schema.Validate(value); err != nil {
		t.Fatal(err)
	}
}
func TestResearchAuthoringRefusesInvalidDeclarations(t *testing.T) {
	tests := map[string]func(map[string]any){
		"partial profile": func(s map[string]any) { s["profile"] = "local" },
		"bound web mismatch": func(s map[string]any) {
			s["profile"] = "local"
			s["profileDigest"] = "sha256:" + strings.Repeat("b", 64)
			s["webProfiles"] = map[string]any{"page": map[string]any{"profile": "web", "profileDigest": "sha256:" + strings.Repeat("c", 64)}}
		},
		"missing answer":            func(s map[string]any) { s["capabilities"] = []string{"page"} },
		"unordered capabilities":    func(s map[string]any) { s["capabilities"] = []string{"answer", "page", "search"} },
		"duplicate fields":          func(s map[string]any) { s["fields"] = append(s["fields"].([]any), s["fields"].([]any)[0]) },
		"invalid required":          func(s map[string]any) { s["fields"].([]any)[0].(map[string]any)["required"] = "true" },
		"field UTF8 bound":          func(s map[string]any) { s["fields"].([]any)[0].(map[string]any)["name"] = strings.Repeat("界", 22) },
		"model UTF8 bound":          func(s map[string]any) { s["model"] = strings.Repeat("界", 86) },
		"evidence bound":            func(s map[string]any) { s["evidence"] = []string{strings.Repeat("x", 4097)} },
		"constraints":               func(s map[string]any) { s["constraints"] = []any{} },
		"runAt":                     func(s map[string]any) { s["question"] = map[string]any{"$param": "runAt"} },
		"unknown question template": func(s map[string]any) { s["question"] = map[string]any{"$other": "x"} },
		"undeclared field pointer": func(s map[string]any) {
			s["read"].(map[string]any)["copy"].(map[string]any)["facts"].([]any)[0].(map[string]any)["source"] = "/fields/unknown/value"
		},
		"select conflicting observation": func(s map[string]any) {
			s["read"].(map[string]any)["copy"].(map[string]any)["facts"].([]any)[0].(map[string]any)["source"] = "/fields/retention/values/0/value"
		},
		"extra member": func(s map[string]any) { s["relationship"] = "parent" },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			d := researchSample(t)
			change(sourceAt(d, 0))
			r, code := validateValue(t, d)
			if code == 0 || r.Document != "" || r.Digest != "" {
				t.Fatal(r)
			}
		})
	}
}
func TestResearchReconciliationClosedReads(t *testing.T) {
	raw, err := os.ReadFile("testdata/research-authoring/exchange-preset.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, pointer := range []string{"/current/fields/employee-count/cites", "/current/fields/employee-count/status/child", "/current/fields/employee-count/arbitrary"} {
		var d map[string]any
		json.Unmarshal(raw, &d)
		read := sourceAt(d, 3)["read"].(map[string]any)
		clause := read["rule"].(map[string]any)["clauses"].([]any)[0].(map[string]any)
		clause["when"] = map[string]any{"op": "all", "of": []any{clause["when"], map[string]any{"op": "exists", "field": pointer}}}
		if r, code := validateValue(t, d); code == 0 {
			t.Fatal(pointer, r)
		}
	}
}

func TestResearchFixtureProvenance(t *testing.T) {
	raw, err := os.ReadFile("testdata/research-authoring/provenance.json")
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Files map[string]struct {
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err = json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	for name, record := range p.Files {
		raw, err := os.ReadFile("testdata/research-authoring/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(raw)) != record.SHA256 {
			t.Fatalf("changed evidence: %s", name)
		}
	}
}
