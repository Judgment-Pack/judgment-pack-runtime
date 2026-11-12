package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func TestCoreURIGrammarMCP(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, version := range artifacts.SupportedVersions() {
		set, err := artifacts.Load(version)
		if err != nil {
			t.Fatal(err)
		}
		base, err := set.Case("valid/minimal-literal.json")
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			id    string
			valid bool
		}{
			{"urn:test:[x]", false}, {"https://example.invalid/[x]", false},
			{"https://%65xample.invalid/a", true}, {"foo://[v1.a]/", true},
			{"foo://[V1.a]/%5b?x=/a?b#c/d?e", true}, {"urn:test:foo\n", false},
		} {
			t.Run(version+"/"+tc.id, func(t *testing.T) {
				id, _ := json.Marshal(tc.id)
				pack := string(bytes.Replace(base, []byte(`"https://example.com/judgment-packs/minimal-literal"`), id, 1))
				report := core030Both(t, toolCall(t, 1, "validate", map[string]any{"document": pack, "through": "structural"}))
				want := "invalid"
				if tc.valid {
					want = "valid"
				}
				if report["status"] != want {
					t.Fatal(report)
				}
				if !tc.valid {
					encoded, _ := json.Marshal(report)
					var validation result.Validation
					if err := json.Unmarshal(encoded, &validation); err != nil || len(validation.Diagnostics) != 1 || validation.Diagnostics[0].Code != "JPS-STRUCTURE-FORMAT-URI" || validation.Diagnostics[0].InstancePath != "/id" {
						t.Fatal(err, report)
					}
				}
				if version == artifacts.DraftVersion {
					return
				} // Core 0.1 has document support only.
				call := toolCall(t, 2, "experimental_evaluate", map[string]any{"pack": pack, "facts": "{}", "rehearsal": true})
				if tc.valid {
					report = core030Both(t, call)
					if report["status"] != "evaluated" || report["packId"] != tc.id {
						t.Fatal(report)
					}
				} else {
					response := runServer(t, call)[0]["result"].(map[string]any)
					report = response["structuredContent"].(map[string]any)
					if response["isError"] != true || !strings.Contains(toolText(t, response), "JPS-STRUCTURE-FORMAT-URI at /id") {
						t.Fatal(response)
					}
					diagnostics := report["diagnostics"].([]any)
					if len(diagnostics) != 1 || diagnostics[0].(map[string]any)["code"] != "JPS-EVALUATION-PACK-NOT-CONFORMANT" {
						t.Fatal(report)
					}
				}
			})
		}
	}
}
