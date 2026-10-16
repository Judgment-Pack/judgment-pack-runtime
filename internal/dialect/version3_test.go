package dialect

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func version3Sample(t *testing.T, unbound bool) map[string]any {
	t.Helper()
	d := sampleDocument(t)
	d["version"] = 3
	for _, k := range []string{"provider", "facts", "evidence"} {
		delete(d, k)
	}
	if unbound {
		s := sourceAt(d, 0)
		delete(s, "profile")
		delete(s, "profileDigest")
		s["platform"] = "sample"
		s["operation"] = "fetch"
		s["shape"] = "mcp"
		s["record"] = "adapter-sample"
	}
	return d
}

func TestVersion3ValidationPreservesIdentityAndScope(t *testing.T) {
	for _, file := range []string{"version-3-bound/mapping.json", "unbound-bound-at-import/mapping.json", "unbound-bound-at-import/unbound.json"} {
		raw, err := os.ReadFile("testdata/" + file)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := Encode("test", raw)
		r, code := Validate("test", raw)
		if code != 0 || r.Status != "document-valid" || r.ValidationScope != "version-3-document" || r.FullDocumentConformance || r.Document != encoded.Document || r.Digest != encoded.Digest {
			t.Fatal(file, code, r)
		}
	}
	c := Contract("test").Capabilities
	if !c.UnboundOperationValidation || c.BindingVerification || c.ProfileCompatibility || c.PreparationVerification || strings.Join(c.ValidationSourceKinds, ",") != "operation,selected-file" {
		t.Fatal(c)
	}
}

func TestVersion3SchemaAndValidatorAgreeOnSourceForms(t *testing.T) {
	r, code := Schema("test", 3)
	if code != 0 || r.ValidationScope != "version-3-structure-aid" || r.Resource.Origin != "runtime-derived" {
		t.Fatal(r)
	}
	schema, err := validation.CompileSchema([]byte(r.Resource.Text), "urn:judgmentpack:runtime:dialect:3:authoring-schema:1")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"bound", "airbyte", "mcp", "http", "local-file", "google-drive"} {
		t.Run(mode, func(t *testing.T) {
			unbound := mode == "airbyte" || mode == "mcp" || mode == "http"
			d := version3Sample(t, unbound)
			s := sourceAt(d, 0)
			if unbound {
				s["shape"] = mode
			}
			if mode == "local-file" || mode == "google-drive" {
				s["kind"] = "selected-file"
				s["provider"] = mode
				if mode == "local-file" {
					for _, k := range []string{"profile", "profileDigest", "maxAge", "arguments"} {
						delete(s, k)
					}
				}
			}
			raw, _ := json.Marshal(d)
			checked, code := Validate("test", raw)
			if code != 0 {
				t.Fatal(checked)
			}
			value, failure := carrier.Decode(raw, carrier.DefaultLimits())
			if failure != nil {
				t.Fatal(failure)
			}
			if err := schema.Validate(value); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVersion3UnboundAndSpellingRefusals(t *testing.T) {
	schema, err := validation.CompileSchema([]byte(version3Schema), "urn:judgmentpack:runtime:dialect:3:authoring-schema:1")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, path string
		change     func(map[string]any)
	}{
		{"missing platform", "/sources/0/platform", func(d map[string]any) { delete(sourceAt(d, 0), "platform") }},
		{"missing operation", "/sources/0/operation", func(d map[string]any) { delete(sourceAt(d, 0), "operation") }},
		{"missing shape", "/sources/0/shape", func(d map[string]any) { delete(sourceAt(d, 0), "shape") }},
		{"missing record", "/sources/0/record", func(d map[string]any) { delete(sourceAt(d, 0), "record") }},
		{"empty platform", "/sources/0/platform", func(d map[string]any) { sourceAt(d, 0)["platform"] = "" }},
		{"bad shape", "/sources/0/shape", func(d map[string]any) { sourceAt(d, 0)["shape"] = "HTTP" }},
		{"mixed profile", "/sources/0/profile", func(d map[string]any) { sourceAt(d, 0)["profile"] = "secret" }},
		{"mixed digest", "/sources/0/profileDigest", func(d map[string]any) { sourceAt(d, 0)["profileDigest"] = "sha256:" + strings.Repeat("a", 64) }},
		{"cased member", "/sources/0/Platform", func(d map[string]any) { sourceAt(d, 0)["Platform"] = "other" }},
		{"root legacy", "/provider", func(d map[string]any) { d["provider"] = "" }},
		{"empty sources", "/sources", func(d map[string]any) { d["sources"] = []any{} }},
		{"empty unmapped", "/unmapped", func(d map[string]any) { d["unmapped"] = []any{} }},
		{"empty unmapped evidence", "/unmappedEvidence", func(d map[string]any) { d["unmappedEvidence"] = []any{} }},
		{"empty parameters", "/case/parameters", func(d map[string]any) { d["case"].(map[string]any)["parameters"] = map[string]any{} }},
		{"empty source parameters", "/sources/0/parameters", func(d map[string]any) { sourceAt(d, 0)["parameters"] = map[string]any{} }},
		{"empty admits facts", "/admits/facts", func(d map[string]any) { d["admits"].(map[string]any)["facts"] = map[string]any{} }},
		{"empty admits evidence", "/admits/evidence", func(d map[string]any) { d["admits"].(map[string]any)["evidence"] = map[string]any{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := version3Sample(t, true)
			tc.change(d)
			r, code := validateValue(t, d)
			if code != 1 || r.Document != "" || r.Digest != "" || len(r.Diagnostics) != 1 || r.Diagnostics[0].InstancePath != tc.path {
				t.Fatal(code, r)
			}
			raw, _ := json.Marshal(d)
			value, _ := carrier.Decode(raw, carrier.DefaultLimits())
			if schema.Validate(value) == nil {
				t.Fatal("schema admitted invalid form")
			}
		})
	}
}

func TestVersion3InternalReferencesAndCoverage(t *testing.T) {
	for _, unbound := range []bool{false, true} {
		d := version3Sample(t, unbound)
		sourceAt(d, 1)["parameters"].(map[string]any)["firstValue"].(map[string]any)["from"] = "later"
		r, code := validateValue(t, d)
		if code != 1 || r.Diagnostics[0].InstancePath != "/sources/1/parameters/firstValue/from" {
			t.Fatal(code, r)
		}
	}
	d := version3Sample(t, true)
	delete(d, "unmapped")
	delete(d, "unmappedEvidence")
	raw, _ := json.Marshal(d)
	engine, _ := validation.NewEngine()
	var pack map[string]any
	if err := json.Unmarshal(checkPack(t, "/first", false), &pack); err != nil {
		t.Fatal(err)
	}
	pack["evidenceRequirements"] = []any{
		map[string]any{"id": "first-proof", "description": "First source proof", "required": true, "kind": "document"},
		map[string]any{"id": "second-proof", "description": "Second source proof", "required": true, "kind": "document"},
	}
	packBytes, _ := json.Marshal(pack)
	r, code := Check("test", raw, packBytes, engine)
	if code != 0 || r.Compatibility == nil || r.ValidationScope != "version-3-document-and-declared-pack-inventory" || r.Compatibility.ValueTypesChecked || r.Compatibility.ExecutionChecked {
		t.Fatal(code, r)
	}
	for _, kind := range []string{"http", "sql", "web", "research", "invented"} {
		d := version3Sample(t, true)
		sourceAt(d, 0)["kind"] = kind
		raw, _ := json.Marshal(d)
		r, code := Check("test", raw, checkPack(t, "/first", false), engine)
		want := 2
		if kind == "invented" {
			want = 1
		}
		if code != want || r.Compatibility != nil || r.PackValidation != nil || r.Document != "" || r.Digest != "" || r.Diagnostics[0].InstancePath != "/sources/0/kind" {
			t.Fatal(kind, code, r)
		}
	}
	// Unbound names never extend the historical version-2 form.
	d = version3Sample(t, true)
	d["version"] = 2
	if r, code := validateValue(t, d); code != 1 {
		t.Fatal(code, r)
	}
}
