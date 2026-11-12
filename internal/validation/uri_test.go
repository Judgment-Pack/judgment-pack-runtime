package validation

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func TestCoreURIGrammarAllDocumentVersions(t *testing.T) {
	raw, err := os.ReadFile("../uriformat/testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		URI   string `json:"uri"`
		Valid bool   `json:"valid"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range artifacts.SupportedVersions() {
		set, err := artifacts.Load(version)
		if err != nil {
			t.Fatal(err)
		}
		original, err := set.Case("valid/minimal-literal.json")
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range cases {
			t.Run(version+"/"+tc.URI, func(t *testing.T) {
				id, _ := json.Marshal(tc.URI)
				pack := bytes.Replace(original, []byte(`"https://example.com/judgment-packs/minimal-literal"`), id, 1)
				before := bytes.Clone(pack)
				for _, through := range []string{"structural", "semantic"} {
					report, failure := engine.Validate(pack, Options{Through: through})
					if failure != nil {
						t.Fatal(failure)
					}
					if tc.Valid {
						if report.Status != "valid" || len(report.Diagnostics) != 0 {
							t.Fatalf("valid URI refused: %+v", report)
						}
					} else {
						if report.Status != "invalid" || len(report.Diagnostics) != 1 {
							t.Fatalf("invalid URI result: %+v", report)
						}
						want := result.ErrorDiagnostic("JPS-STRUCTURE-FORMAT-URI", "structural", "/id", "Value does not satisfy the required uri format.")
						if report.Diagnostics[0] != want {
							t.Fatalf("finite URI diagnostic changed: %+v", report.Diagnostics)
						}
					}
				}
				if !bytes.Equal(pack, before) {
					t.Fatal("validation changed source bytes")
				}
			})
		}
	}
}

func TestURIGrammarAppliesToEverySchemaAssertion(t *testing.T) {
	const id = "urn:test:uri-all-locations"
	schema, err := CompileSchema([]byte(`{"type":"object","properties":{"root":{"format":"uri"},"nested":{"type":"array","items":{"format":"uri"}}}}`), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []map[string]any{
		{"root": "urn:test:[x]"}, {"nested": []any{"urn:test:[x]"}},
	} {
		if schema.Validate(value) == nil {
			t.Fatal("URI assertion omitted at a schema location", value)
		}
	}
	if err := schema.Validate(map[string]any{"root": "https://%65xample.invalid/a", "nested": []any{"foo://[V1.a]/"}}); err != nil {
		t.Fatal(err)
	}
}
