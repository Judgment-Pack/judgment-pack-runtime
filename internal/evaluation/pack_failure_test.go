package evaluation

import (
	"bytes"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
)

// PackFailure is exactly the refusal EvaluateAdmitted reaches before the facts
// document, and nil when the pack reaches it, including for a pack whose
// unsupported required extension §8.2 reports later.
func TestPackFailureIsThePackHalfOfEvaluateAdmitted(t *testing.T) {
	engine := newTestEngine(t)
	extension, err := artifacts.Load(artifacts.EvaluatorDraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	required, err := extension.Case("valid/required-extension-supported.json")
	if err != nil {
		t.Fatal(err)
	}
	conformant := corePack(`{"op":"literal","value":true}`, 0)
	rows := []struct {
		name    string
		pack    []byte
		options Options
		refused bool
	}{
		{"conformant", conformant, Options{}, false},
		{"unsupported extension is deferred", required, Options{}, false},
		{"not conformant", []byte(`{"specVersion":"0.2.0-draft"}`), Options{}, true},
		{"wrong contract", conformant, Options{SpecVersion: artifacts.CoreVersion}, true},
		{"oversized", bytes.Repeat([]byte(" "), int(carrier.HardMaxBytes)+1), Options{}, true},
		{"oversized by report", conformant, Options{OversizedInputs: []string{"pack"}}, true},
		{"two drafts", conformant, Options{RFC0008Quantifiers: true, RFC0016OutcomeValues: true}, true},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			admitted := engine.AdmitPack(row.pack)
			failure := admitted.PackFailure(row.options)
			if (failure != nil) != row.refused {
				t.Fatalf("PackFailure = %+v, refused %v", failure, row.refused)
			}
			_, evaluated := engine.EvaluateAdmitted(admitted, nil, nil, row.options)
			if !row.refused {
				if evaluated == nil || evaluated.Code != "JPS-EVALUATION-INPUT-MISSING" {
					t.Fatalf("a pack PackFailure admits stops at %+v, want the missing facts document", evaluated)
				}
				return
			}
			if evaluated == nil || *evaluated != *failure {
				t.Fatalf("PackFailure %+v, EvaluateAdmitted %+v", failure, evaluated)
			}
			failure.Message = "edited"
			if again := admitted.PackFailure(row.options); again.Message == "edited" {
				t.Fatal("a caller's edit reached the memoized refusal")
			}
		})
	}
}
