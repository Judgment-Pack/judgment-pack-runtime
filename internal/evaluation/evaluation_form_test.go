package evaluation

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// EvaluationForm is the root the evaluator walks, not another lowering of the
// pack: for Core 0.5.0 it is the one EF(P) the admission made, equal to the
// hand-written inline twin's document, and for every other version it is the
// document itself.
func TestEvaluationFormIsTheRootTheEvaluatorReads(t *testing.T) {
	engine := newTestEngine(t)
	options := Options{SpecVersion: artifacts.Core050Version}
	admitted := engine.AdmitPack([]byte(core050PackText("")))
	form, failure := admitted.EvaluationForm(options)
	if failure != nil {
		t.Fatal(failure)
	}
	if reflect.ValueOf(form).Pointer() != reflect.ValueOf(admitted.admission(options).packRoot).Pointer() {
		t.Fatal("EvaluationForm is not the admission's evaluated root")
	}
	parsed, decodeFailure := carrier.Decode([]byte(inlineTwinText(artifacts.Core050Version)), carrier.DefaultLimits())
	if decodeFailure != nil {
		t.Fatal(decodeFailure)
	}
	twin := parsed.(map[string]any)
	if !reflect.DeepEqual(form, twin) {
		t.Fatalf("the evaluation form is not the inline twin:\n%#v\n%#v", form, twin)
	}
	document, failure := admitted.Document(options)
	if failure != nil {
		t.Fatal(failure)
	}
	if _, kept := document["valueSets"]; !kept {
		t.Fatal("asking for the evaluation form changed the document")
	}
	for _, version := range []string{artifacts.CoreVersion, artifacts.Core050Version} {
		inline := engine.AdmitPack([]byte(inlineTwinText(version)))
		form, failure := inline.EvaluationForm(Options{SpecVersion: version})
		if failure != nil {
			t.Fatal(version, failure)
		}
		document, failure := inline.Document(Options{SpecVersion: version})
		if failure != nil {
			t.Fatal(version, failure)
		}
		if version == artifacts.CoreVersion && reflect.ValueOf(form).Pointer() != reflect.ValueOf(document).Pointer() {
			t.Fatal("a Core 0.3.0 pack's evaluation form is not its document")
		}
		if !reflect.DeepEqual(form, document) {
			t.Fatal(version, "an inline pack's evaluation form differs from its document")
		}
	}
}

// A pack an evaluation would not go on to evaluate has no form to read: the
// pack's own refusal is returned, the deferred unsupported required extension
// included, which PackFailure leaves to EvaluateAdmitted.
func TestEvaluationFormRefusesWhatEvaluationRefuses(t *testing.T) {
	engine := newTestEngine(t)
	options := Options{SpecVersion: artifacts.Core050Version}
	unresolved := strings.Replace(core050PackText(""), `"valueSet":"regions"}`, `"valueSet":"missing"}`, 1)
	if form, failure := engine.AdmitPack([]byte(unresolved)).EvaluationForm(options); form != nil || failure == nil || failure.Class != result.ClassPackNotConformant || !strings.Contains(failure.Message, "JPS-SEMANTIC-UNRESOLVED-VALUE-SET") {
		t.Fatalf("unresolved reference: %v %+v", form, failure)
	}
	required := strings.Replace(core050PackText(""), `"extensions":{"org.judgmentpack.annotations":{"owner":"intake-team"}}`, `"metadata":{"requiredExtensions":["com.example.custom"]},"extensions":{"com.example.custom":{},"org.judgmentpack.annotations":{"owner":"intake-team"}}`, 1)
	if required == core050PackText("") {
		t.Fatal("the edit did not apply")
	}
	admitted := engine.AdmitPack([]byte(required))
	if admitted.PackFailure(options) != nil {
		t.Fatal("PackFailure reports the required extension")
	}
	form, failure := admitted.EvaluationForm(options)
	if form != nil || failure == nil || failure.Class != "unsupported-required-extension" || failure.Code != "JPS-CAPABILITY-REQUIRED-EXTENSION" {
		t.Fatalf("required extension: %v %+v", form, failure)
	}
	supported := options
	supported.SupportedExtensions = []string{"com.example.custom"}
	if form, failure := admitted.EvaluationForm(supported); failure != nil || form["valueSets"] != nil {
		t.Fatalf("a supported required extension: %v %+v", form, failure)
	}
}
