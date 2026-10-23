package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func TestCoreURIGrammarCLI(t *testing.T) {
	t.Chdir(t.TempDir())
	facts := writeDocument(t, "facts.json", `{}`)
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
				code, text, stderr := runTest(t, []string{"spec", "validate", "-", "--through", "structural", "--format", "json"}, pack)
				var validation result.Validation
				if err := json.Unmarshal([]byte(text), &validation); err != nil {
					t.Fatal(err, text)
				}
				if stderr != "" || (code == 0) != tc.valid {
					t.Fatal(code, text, stderr)
				}
				if !tc.valid && (code != result.ExitInvalid || len(validation.Diagnostics) != 1 || validation.Diagnostics[0].Code != "JPS-STRUCTURE-FORMAT-URI" || validation.Diagnostics[0].InstancePath != "/id") {
					t.Fatal(code, validation)
				}
				if version == artifacts.DraftVersion {
					return
				} // Core 0.1 has document support only.
				code, text, stderr = runTest(t, []string{"experimental", "evaluate", "-", "--facts", facts, "--format", "json"}, pack)
				if stderr != "" || (code == 0) != tc.valid {
					t.Fatal(code, text, stderr)
				}
				if tc.valid {
					var report result.Evaluation
					if err := json.Unmarshal([]byte(text), &report); err != nil || report.PackID != tc.id {
						t.Fatal(err, text)
					}
				} else {
					var report result.OperationalError
					if err := json.Unmarshal([]byte(text), &report); err != nil || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" {
						t.Fatal(err, text)
					}
				}
			})
		}
	}
}
