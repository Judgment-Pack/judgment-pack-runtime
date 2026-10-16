package dialect

import (
	"encoding/json"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
	"strings"
	"testing"
)

func TestNamedArtifactFormats(t *testing.T) {
	for _, f := range []string{"2.0.0", "3.0.0"} {
		r, code := InitArtifact("test", f, "urn:judgmentpack:dialect:example", "0.1.0")
		expected := `{"case":{"evidence":[],"facts":[]},"formatVersion":"` + f + `","id":"urn:judgmentpack:dialect:example","version":"0.1.0"}`
		if code != 0 || r.Status != "document-valid" || r.Document != expected || r.ArtifactID != "urn:judgmentpack:dialect:example" || r.ArtifactVersion != "0.1.0" || r.FormatVersion != f {
			t.Fatalf("%d %+v", code, r)
		}
		other, _ := Validate("test", []byte(strings.Replace(expected, `"0.1.0"`, `"0.1.1"`, 1)))
		if other.Digest == r.Digest {
			t.Fatal("artifact version absent from digest")
		}
		sr, c := ArtifactSchema("test", f)
		if c != 0 || sr.Resource == nil || !strings.Contains(sr.Resource.Text, `"formatVersion"`) {
			t.Fatalf("schema %+v", sr)
		}
		for _, field := range []string{`"provider":""`, `"facts":null`, `"sources":[]`, `"admits":{"facts":{}}`} {
			raw := strings.Replace(expected, "{", "{"+field+",", 1)
			if bad, c := Validate("test", []byte(raw)); c == 0 || bad.Document != "" || bad.Digest != "" {
				t.Fatalf("admitted forbidden identity: %+v", bad)
			}
		}
	}
	for _, v := range []string{"2", "01.0.0", "1.0.0-a", "1.0.0+build", "1.0.0\n", "1.0.0\r"} {
		if _, code := InitArtifact("test", "2.0.0", "example", v); code == 0 {
			t.Fatal(v)
		}
	}
	for _, id := range []string{"", "a b", "a\x7f", strings.Repeat("é", 513)} {
		if _, code := InitArtifact("test", "2.0.0", id, "1.0.0"); code == 0 {
			t.Fatal("bad id")
		}
	}
	for _, f := range []string{"2", "2.1.0", "03.0.0"} {
		if _, code := InitArtifact("test", f, "example", "1.0.0"); code == 0 {
			t.Fatal(f)
		}
	}
	// A numeric artifact version is never accepted by coercion.
	raw, _ := json.Marshal(map[string]any{"formatVersion": "2.0.0", "id": "example", "version": 2, "case": map[string]any{"facts": []any{}, "evidence": []any{}}})
	if _, code := Validate("test", raw); code == 0 {
		t.Fatal("coerced numeric version")
	}
}

func TestNamedAuthoringSchemaChecksIdentityAndOmission(t *testing.T) {
	for _, format := range []string{"2.0.0", "3.0.0"} {
		report, _ := ArtifactSchema("test", format)
		schema, err := validation.CompileSchema([]byte(report.Resource.Text), "urn:test:"+format)
		if err != nil {
			t.Fatal(err)
		}
		starter, _ := InitArtifact("test", format, "urn:test", "1.0.0")
		var base map[string]any
		json.Unmarshal([]byte(starter.Document), &base)
		if err := schema.Validate(base); err != nil {
			t.Fatal("starter", err)
		}
		for _, change := range []func(map[string]any){
			func(m map[string]any) { m["id"] = "urn:test\n" },
			func(m map[string]any) { m["version"] = "1.0.0\n" },
			func(m map[string]any) { m["sources"] = []any{} },
			func(m map[string]any) { m["unmapped"] = []any{} },
			func(m map[string]any) { m["case"].(map[string]any)["parameters"] = map[string]any{} },
			func(m map[string]any) { m["admits"] = map[string]any{"facts": map[string]any{}} },
		} {
			var doc map[string]any
			json.Unmarshal([]byte(starter.Document), &doc)
			change(doc)
			if schema.Validate(doc) == nil {
				t.Fatal("schema admitted malformed identity", doc)
			}
		}
	}
}
