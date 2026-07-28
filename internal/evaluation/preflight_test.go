package evaluation

import (
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// corpusPack is one bundled evaluation-corpus fixture, used here as an ordinary
// semantically conforming pack declaring 0.2.0-draft.
func corpusPack(t *testing.T, name string) []byte {
	t.Helper()
	set, err := artifacts.Load(artifacts.EvaluatorDraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := set.EvaluationPack("packs/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

// extensionPack is a semantically conforming pack that requires one extension
// capability. Requiring a capability is not a defect in the pack: it is §8.2's
// fourth preflight step that fails when the caller does not support it.
func extensionPack(t *testing.T) []byte {
	t.Helper()
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := set.Case("valid/required-extension-supported.json")
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

const notConformantPack = `{"specVersion":"0.2.0-draft"}`

// §8.2 admits the inputs in one order — the pack, the facts document, the
// evidence-availability document, then the required-extension set — and §8.4
// makes that order the error precedence, so each row below presents inputs that
// two classes apply to and pins the class Core requires be reported.
func TestPreflightOrderIsTheErrorPrecedence(t *testing.T) {
	engine := newTestEngine(t)
	triage := corpusPack(t, "data-request-intake-triage")
	goodFacts := []byte(`{"request":{"type":"data-access","completeness":"complete","appropriateness":"pass","embargoedInformationToUnauthorizedRecipients":false}}`)

	cases := []struct {
		name      string
		pack      []byte
		facts     []byte
		evidence  []byte
		supported []string
		wantClass string
		wantCode  string
	}{
		{
			name:      "a non-conformant pack outranks every other failure",
			pack:      []byte(notConformantPack),
			facts:     []byte(`{`),
			evidence:  []byte(`{"not-a-requirement":"present"}`),
			wantClass: result.ClassPackNotConformant,
			wantCode:  "JPS-EVALUATION-PACK-NOT-CONFORMANT",
		},
		{
			name:      "the facts document is admitted before the evidence document",
			pack:      triage,
			facts:     []byte(`{`),
			evidence:  []byte(`{"not-a-requirement":"present"}`),
			wantClass: result.ClassMalformedInput,
			wantCode:  "JPS-EVALUATION-INPUT-JSON",
		},
		{
			name:      "a malformed evidence document outranks an unsupported required extension",
			pack:      extensionPack(t),
			facts:     []byte(`{}`),
			evidence:  []byte(`{"not-a-requirement":"present"}`),
			wantClass: result.ClassMalformedInput,
			wantCode:  "JPS-EVALUATION-EVIDENCE-KEY",
		},
		{
			name:      "an unsupported required extension is reported once the documents are admitted",
			pack:      extensionPack(t),
			facts:     []byte(`{}`),
			wantClass: result.ClassUnsupportedRequiredExtension,
			wantCode:  "JPS-CAPABILITY-REQUIRED-EXTENSION",
		},
		{
			name:      "with the capability supported the same pack evaluates",
			pack:      extensionPack(t),
			facts:     []byte(`{}`),
			supported: []string{"com.example.review-policy"},
		},
		{
			name:  "the whole preflight passing leaves the disposition to §8",
			pack:  triage,
			facts: goodFacts,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			output, failure := engine.Evaluate(testCase.pack, testCase.facts, testCase.evidence, testCase.supported, "test")
			if testCase.wantClass == "" {
				if failure != nil {
					t.Fatalf("expected an admitted evaluation: %s: %s", failure.Code, failure.Message)
				}
				if output.Disposition.Kind == "" {
					t.Fatalf("an admitted evaluation produces a disposition: %+v", output)
				}
				return
			}
			if failure == nil {
				t.Fatalf("expected the %s error, got the disposition %+v", testCase.wantClass, output.Disposition)
			}
			if failure.Class != testCase.wantClass {
				t.Fatalf("class = %q, want %q (%s: %s)", failure.Class, testCase.wantClass, failure.Code, failure.Message)
			}
			if failure.Code != testCase.wantCode {
				t.Fatalf("code = %q, want %q", failure.Code, testCase.wantCode)
			}
			if failure.Phase != result.PhasePreflight {
				t.Fatalf("phase = %q, want %q", failure.Phase, result.PhasePreflight)
			}
			// §8.4: an evaluation error terminates the evaluation, and no
			// disposition — not even a partial one — accompanies it.
			if output.Disposition.Kind != "" || output.Status != "" {
				t.Fatalf("an evaluation error carries no disposition: %+v", output)
			}
		})
	}
}

// §8.2: because the preflight completes before step 1, no result can outrace an
// input error. A pack whose applicability is false, presented with an
// evidence-availability document carrying an undeclared key, is the
// malformed-input error and never the not-applicable disposition.
func TestPreflightCompletesBeforeResolutionBegins(t *testing.T) {
	engine := newTestEngine(t)
	pack := corpusPack(t, "data-request-intake-triage")
	notApplicableFacts := []byte(`{"request":{"type":"dataset-deletion"}}`)

	admitted, failure := engine.Evaluate(pack, notApplicableFacts, nil, nil, "test")
	if failure != nil {
		t.Fatalf("the same inputs without an evidence document are a disposition: %s", failure.Message)
	}
	if admitted.Disposition.Kind != "not-applicable" {
		t.Fatalf("expected the not-applicable disposition: %+v", admitted.Disposition)
	}

	output, failure := engine.Evaluate(pack, notApplicableFacts, []byte(`{"not-a-requirement":"present"}`), nil, "test")
	if failure == nil {
		t.Fatalf("an undeclared evidence key must outrace the terminal step-1 result: %+v", output.Disposition)
	}
	if failure.Class != result.ClassMalformedInput || failure.Phase != result.PhasePreflight {
		t.Fatalf("class/phase = %q/%q, want malformed-input/preflight", failure.Class, failure.Phase)
	}
	if output.Disposition.Kind != "" {
		t.Fatalf("no disposition accompanies the error: %+v", output.Disposition)
	}
}

// §8.2: an omitted evidence-availability document is the implicit empty object,
// which makes every declared requirement unknown. It is the only form absence
// takes, it is not an error, and it is indistinguishable from the empty document
// and from one that spells every requirement unknown.
func TestOmittedEvidenceIsTheImplicitEmptyObject(t *testing.T) {
	engine := newTestEngine(t)
	pack := corpusPack(t, "data-request-intake-triage")
	facts := []byte(`{"request":{"type":"data-access","completeness":"complete","appropriateness":"pass","embargoedInformationToUnauthorizedRecipients":false}}`)

	forms := map[string][]byte{
		"omitted document":                  nil,
		"the empty object":                  []byte(`{}`),
		"every requirement spelled unknown": []byte(`{"intake-form":"unknown","sponsor-endorsement":"unknown"}`),
	}
	canonical := map[string]string{}
	for name, evidence := range forms {
		output, failure := engine.Evaluate(pack, facts, evidence, nil, "test")
		if failure != nil {
			t.Fatalf("%s must be admitted: %s: %s", name, failure.Code, failure.Message)
		}
		encoded, err := output.Disposition.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		canonical[name] = string(encoded)
	}
	want := `{"handoff":{"state":"requested","triggeredBy":["unknown"]},"kind":"unresolved","reasons":["unknown"]}`
	for name, encoded := range canonical {
		if encoded != want {
			t.Fatalf("%s produced %s, want %s", name, encoded, want)
		}
	}
}

// §8.2: every shape violation of the evidence-availability input is the
// malformed-input error, whichever way it is malformed.
func TestEvidenceShapeViolationsAreMalformedInput(t *testing.T) {
	engine := newTestEngine(t)
	pack := corpusPack(t, "data-request-intake-triage")
	facts := []byte(`{"request":{"type":"data-access","completeness":"complete","appropriateness":"pass","embargoedInformationToUnauthorizedRecipients":false}}`)

	cases := []struct {
		name     string
		evidence string
		wantCode string
	}{
		{"an array is not a JSON object", `[]`, "JPS-EVALUATION-EVIDENCE-SHAPE"},
		{"a string is not a JSON object", `"present"`, "JPS-EVALUATION-EVIDENCE-SHAPE"},
		{"null is not a JSON object", `null`, "JPS-EVALUATION-EVIDENCE-SHAPE"},
		{"an undeclared member name", `{"sponsor":"present"}`, "JPS-EVALUATION-EVIDENCE-KEY"},
		{"a value outside the tri-state", `{"intake-form":"maybe"}`, "JPS-EVALUATION-EVIDENCE-VALUE"},
		{"a value of the wrong JSON type", `{"intake-form":true}`, "JPS-EVALUATION-EVIDENCE-VALUE"},
		{"a document that is not carrier-conforming JSON", `{"intake-form":`, "JPS-EVALUATION-INPUT-JSON"},
		{"a duplicate member name (§2.1)", `{"intake-form":"present","intake-form":"absent"}`, "JPS-EVALUATION-INPUT-JSON"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			output, failure := engine.Evaluate(pack, facts, []byte(testCase.evidence), nil, "test")
			if failure == nil {
				t.Fatalf("expected malformed-input, got %+v", output.Disposition)
			}
			if failure.Class != result.ClassMalformedInput || failure.Phase != result.PhasePreflight {
				t.Fatalf("class/phase = %q/%q, want malformed-input/preflight", failure.Class, failure.Phase)
			}
			if failure.Code != testCase.wantCode {
				t.Fatalf("code = %q, want %q", failure.Code, testCase.wantCode)
			}
		})
	}
}

// Each of §8.4's four classes is reachable, and every one of them terminates the
// evaluation with no disposition. resource-exhaustion is the one that belongs to
// the evaluation phase rather than to the preflight: it is a limit an admitted
// input turned out to require, which is exactly the distinction §10 draws.
func TestEveryEvaluationErrorClassIsReachable(t *testing.T) {
	engine := newTestEngine(t)
	facts := []byte(`{"cells":[{"ok":true},{"ok":true}]}`)

	cases := []struct {
		name      string
		pack      []byte
		facts     []byte
		evidence  []byte
		supported []string
		options   *Options
		wantClass string
		wantPhase string
	}{
		{
			name:      "pack-not-conformant",
			pack:      []byte(notConformantPack),
			facts:     []byte(`{}`),
			wantClass: result.ClassPackNotConformant,
			wantPhase: result.PhasePreflight,
		},
		{
			name:      "malformed-input",
			pack:      corpusPack(t, "data-request-intake-triage"),
			facts:     []byte(`not json`),
			wantClass: result.ClassMalformedInput,
			wantPhase: result.PhasePreflight,
		},
		{
			name:      "unsupported-required-extension",
			pack:      extensionPack(t),
			facts:     []byte(`{}`),
			wantClass: result.ClassUnsupportedRequiredExtension,
			wantPhase: result.PhasePreflight,
		},
		{
			name:      "resource-exhaustion",
			pack:      draftPack(`{"op":"exists","path":"/cells","where":{"op":"fact","path":"/ok","operator":"equals","value":true}}`, "ignore"),
			facts:     facts,
			options:   &Options{Command: "test", RFC0008Quantifiers: true, WorkBudget: 1},
			wantClass: result.ClassResourceExhaustion,
			wantPhase: result.PhaseEvaluation,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var output result.Evaluation
			var failure *Failure
			if testCase.options != nil {
				output, failure = engine.EvaluateWith(testCase.pack, testCase.facts, testCase.evidence, *testCase.options)
			} else {
				output, failure = engine.Evaluate(testCase.pack, testCase.facts, testCase.evidence, testCase.supported, "test")
			}
			if failure == nil {
				t.Fatalf("expected the %s error, got %+v", testCase.wantClass, output.Disposition)
			}
			if failure.Class != testCase.wantClass || failure.Phase != testCase.wantPhase {
				t.Fatalf("class/phase = %q/%q, want %q/%q (%s: %s)", failure.Class, failure.Phase, testCase.wantClass, testCase.wantPhase, failure.Code, failure.Message)
			}
			if failure.Code == "" {
				t.Fatal("the finer diagnostic code is the detail every class keeps")
			}
			if output.Disposition.Kind != "" || len(output.Disposition.Reasons) != 0 {
				t.Fatalf("an evaluation error reports no partial disposition: %+v", output.Disposition)
			}
		})
	}
}

// §8.4's order is fixed for inputs more than one class applies to, including the
// pair that spans the phase split: an unsupported required extension is settled
// in the preflight, so it is reported rather than the evaluation-work limit the
// same inputs would have reached.
func TestUnsupportedRequiredExtensionOutranksResourceExhaustion(t *testing.T) {
	engine := newTestEngine(t)
	pack := draftPackRequiringExtension(`{"op":"exists","path":"/cells","where":{"op":"fact","path":"/ok","operator":"equals","value":true}}`)
	facts := []byte(`{"cells":[{"ok":true},{"ok":true}]}`)

	_, failure := engine.EvaluateWith(pack, facts, nil, Options{Command: "test", RFC0008Quantifiers: true, WorkBudget: 1})
	if failure == nil || failure.Class != result.ClassUnsupportedRequiredExtension {
		t.Fatalf("the preflight class must be reported: %+v", failure)
	}
	// With the capability supported the same inputs reach the work limit, which
	// is what makes the row above a precedence test rather than a coincidence.
	_, failure = engine.EvaluateWith(pack, facts, nil, Options{
		Command:             "test",
		RFC0008Quantifiers:  true,
		WorkBudget:          1,
		SupportedExtensions: []string{"com.example.review-policy"},
	})
	if failure == nil || failure.Class != result.ClassResourceExhaustion {
		t.Fatalf("with the capability supported the work limit is reached: %+v", failure)
	}
}

// draftPackRequiringExtension is draftPack with one required extension
// capability, so a pack can present the preflight's fourth step and an
// evaluation-work limit at once.
func draftPackRequiringExtension(when string) []byte {
	pack := draftPack(when, "ignore")
	insert := `,
  "metadata": {"requiredExtensions": ["com.example.review-policy"]},
  "extensions": {"com.example.review-policy": {"note": "Invented content for specification testing."}}
}`
	return append(pack[:len(pack)-2], insert...)
}

// A caller that reads its own inputs — the CLI — cannot present bytes it refused
// to read, so it reports the byte limit through Options.OversizedInputs and the
// limit is reached at that input's own place in the preflight. §8.4's order then
// applies to it like any other preflight condition: the pack's limit before the
// facts document's, and a non-conformant pack before an oversized facts document.
func TestAnOversizedInputMarkerKeepsTheFixedOrder(t *testing.T) {
	engine := newTestEngine(t)
	triage := corpusPack(t, "data-request-intake-triage")
	goodFacts := []byte(`{"request":{"type":"data-access","completeness":"complete","appropriateness":"pass","embargoedInformationToUnauthorizedRecipients":false}}`)

	cases := []struct {
		name      string
		pack      []byte
		facts     []byte
		oversized []string
		wantClass string
		wantCode  string
	}{
		{
			name:      "an oversized facts document is malformed-input",
			pack:      triage,
			facts:     nil,
			oversized: []string{"facts"},
			wantClass: result.ClassMalformedInput,
			wantCode:  "JPS-RESOURCE-INPUT-BYTE-LIMIT",
		},
		{
			name:      "an oversized pack is pack-not-conformant",
			pack:      nil,
			facts:     goodFacts,
			oversized: []string{"pack"},
			wantClass: result.ClassPackNotConformant,
			wantCode:  "JPS-RESOURCE-INPUT-BYTE-LIMIT",
		},
		{
			name:      "the pack's limit is reported before the facts document's",
			pack:      nil,
			facts:     nil,
			oversized: []string{"pack", "facts"},
			wantClass: result.ClassPackNotConformant,
			wantCode:  "JPS-RESOURCE-INPUT-BYTE-LIMIT",
		},
		{
			name:      "a non-conformant pack outranks an oversized facts document",
			pack:      []byte(notConformantPack),
			facts:     nil,
			oversized: []string{"facts"},
			wantClass: result.ClassPackNotConformant,
			wantCode:  "JPS-EVALUATION-PACK-NOT-CONFORMANT",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			output, failure := engine.EvaluateWith(testCase.pack, testCase.facts, nil, Options{
				Command:         "test",
				OversizedInputs: testCase.oversized,
			})
			if failure == nil {
				t.Fatalf("expected the %s error, got %+v", testCase.wantClass, output.Disposition)
			}
			if failure.Class != testCase.wantClass || failure.Phase != result.PhasePreflight {
				t.Fatalf("class/phase = %q/%q, want %q/preflight", failure.Class, failure.Phase, testCase.wantClass)
			}
			if failure.Code != testCase.wantCode {
				t.Fatalf("code = %q, want %q", failure.Code, testCase.wantCode)
			}
		})
	}
}

// §8.2 makes the omitted evidence document the implicit empty object and not an
// error. An empty *supplied* document is not that: empty bytes are not a
// carrier-conforming JSON text, so it is malformed-input reached at the evidence
// document's own place in the preflight — after the pack, which still outranks it.
func TestAnEmptySuppliedEvidenceDocumentIsMalformedInput(t *testing.T) {
	engine := newTestEngine(t)
	pack := corpusPack(t, "data-request-intake-triage")
	facts := []byte(`{"request":{"type":"data-access"}}`)

	_, failure := engine.EvaluateWith(pack, facts, []byte{}, Options{Command: "test", EvidenceSupplied: true})
	if failure == nil || failure.Class != result.ClassMalformedInput || failure.Phase != result.PhasePreflight {
		t.Fatalf("an empty supplied evidence document is malformed-input: %+v", failure)
	}
	if failure.Code != "JPS-EVALUATION-INPUT-JSON" {
		t.Fatalf("code = %q", failure.Code)
	}
	// The same bytes with no document supplied are the absence §8.2 defines.
	if _, failure = engine.EvaluateWith(pack, facts, []byte{}, Options{Command: "test"}); failure != nil {
		t.Fatalf("an omitted evidence document is not an error: %+v", failure)
	}
	// A non-conformant pack still outranks it (§8.4).
	_, failure = engine.EvaluateWith([]byte(notConformantPack), facts, []byte{}, Options{Command: "test", EvidenceSupplied: true})
	if failure == nil || failure.Class != result.ClassPackNotConformant {
		t.Fatalf("the pack's class comes first: %+v", failure)
	}
}

// §8.4 fixes the class, not the message detail, but a detail that changes between
// identical runs is one no harness can pin. An evidence document violating two
// §8.2 rules at once therefore reports the same code every time: the member names
// are walked in sorted order, so the reported code is a function of the input.
func TestEvidenceDiagnosticDetailIsDeterministic(t *testing.T) {
	engine := newTestEngine(t)
	pack := corpusPack(t, "data-request-intake-triage")
	facts := []byte(`{"request":{"type":"data-access"}}`)
	evidence := []byte(`{"intake-form":"maybe","zzz-undeclared":"present"}`)

	first := ""
	for run := 0; run < 64; run++ {
		_, failure := engine.Evaluate(pack, facts, evidence, nil, "test")
		if failure == nil || failure.Class != result.ClassMalformedInput {
			t.Fatalf("both violations are malformed-input: %+v", failure)
		}
		if first == "" {
			first = failure.Code
		}
		if failure.Code != first {
			t.Fatalf("the reported code varies between identical runs: %q then %q", first, failure.Code)
		}
	}
	// Sorted order reaches "intake-form" before "zzz-undeclared", so the value
	// violation is the one reported. The point is that it is always the same one.
	if first != "JPS-EVALUATION-EVIDENCE-VALUE" {
		t.Fatalf("code = %q, want the first violation in sorted member order", first)
	}
}
