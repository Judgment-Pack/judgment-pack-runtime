package dialect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func TestDialectResourcesIdentityAndScope(t *testing.T) {
	schema, code := Schema("test", 2)
	if code != 0 || schema.Status != "available" || schema.Resource.Origin != "runtime-derived" || schema.ValidationScope != "version-2-structure-aid" || schema.FullDocumentConformance {
		t.Fatal(schema)
	}
	reference := Reference("test")
	for _, r := range []Report{schema, reference} {
		b := []byte(r.Resource.Text)
		sum := sha256.Sum256(b)
		if r.Resource.SHA256 != hex.EncodeToString(sum[:]) || r.Resource.Bytes != len(b) || r.Document != "" || r.Digest != "" {
			t.Fatal("resource identity is not dialect identity", r)
		}
	}
	b, err := os.ReadFile("testdata/spec-dialect.md")
	if err != nil {
		t.Fatal(err)
	}
	if reference.Resource.Text != string(b) || reference.Resource.Origin != "pinned-specification" {
		t.Fatal("reference was rewritten")
	}
	for _, version := range []int{0, 1, 3, 4} {
		if r, code := Schema("test", version); code != 2 || r.Resource != nil {
			t.Fatal(version, r)
		}
	}
}

func TestAuthoringSchemaCoversValidDocumentsWithoutClaimingSemantics(t *testing.T) {
	schema, err := validation.CompileSchema([]byte(version2Schema), "urn:judgmentpack:runtime:dialect:2:authoring-schema:1")
	if err != nil {
		t.Fatal(err)
	}
	accept := func(data []byte) {
		t.Helper()
		r, code := Validate("test", data)
		if code != 0 {
			t.Fatal(r.Diagnostics)
		}
		value, failure := carrier.Decode(data, carrier.DefaultLimits())
		if failure != nil {
			t.Fatal(failure)
		}
		if err := schema.Validate(value); err != nil {
			t.Fatal("schema refused validator-admitted document", err)
		}
	}
	files, _ := filepath.Glob("testdata/*/mapping.json")
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		accept(data)
	}
	data, _ := json.Marshal(sampleDocument(t))
	accept(data)
	for _, provider := range []string{"local-file", "google-drive"} {
		doc := sampleDocument(t)
		s := sourceAt(doc, 0)
		s["kind"] = "selected-file"
		s["provider"] = provider
		if provider == "local-file" {
			for _, k := range []string{"profile", "profileDigest", "maxAge", "arguments"} {
				delete(s, k)
			}
		}
		data, _ := json.Marshal(doc)
		accept(data)
	}
	for _, raw := range []string{
		`{"version":3,"case":{"facts":[],"evidence":[]}}`,
		`{"version":2,"case":{"facts":null,"evidence":[]}}`,
		`{"version":2,"case":{"facts":[],"evidence":[]},"extra":true}`,
		`{"version":2,"case":{"facts":[{"target":"","source":""}],"evidence":[]}}`,
	} {
		v, _ := carrier.Decode([]byte(raw), carrier.DefaultLimits())
		if err := schema.Validate(v); err == nil {
			t.Fatal("invalid form passed schema", raw)
		}
	}
	// Cross-references are intentionally the validator's responsibility. The
	// editing schema is not presented as a complete semantic oracle.
	doc := sampleDocument(t)
	sourceAt(doc, 1)["parameters"].(map[string]any)["firstValue"].(map[string]any)["from"] = "later"
	data, _ = json.Marshal(doc)
	v, _ := carrier.Decode(data, carrier.DefaultLimits())
	if schema.Validate(v) != nil {
		t.Fatal("structurally valid reference rejected")
	}
	if _, code := Validate("test", data); code == 0 {
		t.Fatal("unresolved source reference accepted")
	}
}

func TestAuthoringSchemaRejectsMalformedRequestTemplates(t *testing.T) {
	schema, err := validation.CompileSchema([]byte(version2Schema), "urn:judgmentpack:runtime:dialect:2:authoring-schema:1")
	if err != nil {
		t.Fatal(err)
	}
	for _, arguments := range []string{
		`{"$param":"id"}`,
		`{"tool":"read","arguments":{"$param":null}}`,
		`{"tool":"read","arguments":{"$text":1}}`,
		`{"tool":"read","arguments":{"$bogus":"x"}}`,
		`{"tool":"read","arguments":{"$param":"id","other":true}}`,
		`{"tool":"read","arguments":{"$param":"runAt"}}`,
		`{"tool":"read","arguments":{"$":"x"}}`,
	} {
		t.Run(arguments, func(t *testing.T) {
			doc := sampleDocument(t)
			var request any
			if err := json.Unmarshal([]byte(arguments), &request); err != nil {
				t.Fatal(err)
			}
			sourceAt(doc, 0)["arguments"] = request
			if err := schema.Validate(doc); err == nil {
				t.Fatal("malformed request template admitted by schema")
			}
			if r, code := validateValue(t, doc); code == 0 {
				t.Fatal("malformed request template admitted by validator", r)
			}
		})
	}
}
