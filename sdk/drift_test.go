package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// The drift guard. Every row of both bundled evaluation corpora runs through
// Load and Evaluate, and each result is held to three things: the row's own
// expectation, the result the corpus runner reports for the row, and, byte for
// byte, the envelope the shared engine writes when called with the options of
// "jpack experimental evaluate" outside a project. A change on either side of
// this package that makes it disagree with the engine fails here.
func TestBothCorporaThroughThePublicAPI(t *testing.T) {
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	engine := evaluation.NewEngine(validator)
	for _, version := range []string{artifacts.EvaluatorDraftVersion, artifacts.CoreVersion} {
		t.Run(version, func(t *testing.T) {
			set, err := artifacts.Load(version)
			if err != nil {
				t.Fatal(err)
			}
			manifestBytes, err := set.EvaluationManifest()
			if err != nil {
				t.Fatal(err)
			}
			var manifest struct {
				Cases []struct {
					ID                   string          `json:"id"`
					Pack                 string          `json:"pack"`
					Facts                json.RawMessage `json:"facts"`
					EvidenceAvailability json.RawMessage `json:"evidenceAvailability"`
					SupportedExtensions  []string        `json:"supportedExtensions"`
					ExpectedErrorClass   string          `json:"expectedErrorClass"`
					ExpectedErrorPhase   string          `json:"expectedErrorPhase"`
				} `json:"cases"`
			}
			if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
				t.Fatal(err)
			}
			run, failure := engine.RunCorpus(version, "test")
			if failure != nil {
				t.Fatalf("RunCorpus: %+v", failure)
			}
			if len(manifest.Cases) == 0 || len(run.Cases) != len(manifest.Cases) {
				t.Fatalf("%d manifest rows, %d run rows", len(manifest.Cases), len(run.Cases))
			}
			for index, row := range manifest.Cases {
				reported := run.Cases[index]
				if reported.ID != row.ID {
					t.Fatalf("row %d: run reports %q, manifest %q", index, reported.ID, row.ID)
				}
				pack, err := set.EvaluationPack(row.Pack)
				if err != nil {
					t.Fatal(err)
				}
				// An omitted evidence document is a nil slice, as the corpus runner
				// passes it; a present one is the row's own bytes.
				var evidence []byte
				if row.EvidenceAvailability != nil {
					evidence = row.EvidenceAvailability
				}
				loaded, err := Load(context.Background(), pack, LoadOptions{SupportedExtensions: row.SupportedExtensions})
				if err != nil {
					t.Fatalf("%s: Load: %v", row.ID, err)
				}
				evaluated, err := loaded.Evaluate(context.Background(), row.Facts, evidence)
				if row.ExpectedErrorClass != "" {
					refusal, ok := err.(*Refusal)
					if !ok || refusal.Class != row.ExpectedErrorClass || (row.ExpectedErrorPhase != "" && refusal.Phase != row.ExpectedErrorPhase) {
						t.Fatalf("%s: %v, want class %s", row.ID, err, row.ExpectedErrorClass)
					}
					continue
				}
				if err != nil {
					t.Fatalf("%s: Evaluate: %v", row.ID, err)
				}
				canonical := string(evaluated.CanonicalDisposition())
				if reported.Status != "passed" || canonical != reported.Expected || canonical != reported.Actual {
					t.Fatalf("%s: public API %s; corpus expects %s, runner reports %s (%s)", row.ID, canonical, reported.Expected, reported.Actual, reported.Status)
				}
				if evaluated.EvaluatorSpecVersion != version || evaluated.SpecVersion != version {
					t.Fatalf("%s: contract %q, pack %q, want %q", row.ID, evaluated.EvaluatorSpecVersion, evaluated.SpecVersion, version)
				}
				assertTypedViewIsTheCanonicalBytes(t, row.ID, evaluated)

				direct, failure := engine.EvaluateWith(pack, row.Facts, evidence, evaluation.Options{
					SpecVersion:         version,
					Command:             command,
					SupportedExtensions: row.SupportedExtensions,
					EvidenceSupplied:    evidence != nil,
				})
				if failure != nil {
					t.Fatalf("%s: engine: %+v", row.ID, failure)
				}
				want := cliJSON(t, direct)
				got, err := evaluated.JSON()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("%s: JSON differs from the engine's envelope\n got %s\nwant %s", row.ID, got, want)
				}
			}
		})
	}
}

// assertTypedViewIsTheCanonicalBytes re-encodes the typed disposition the way
// §8.3 lays it out and compares it with the canonical bytes.
func assertTypedViewIsTheCanonicalBytes(t *testing.T, id string, evaluated *Evaluation) {
	t.Helper()
	disposition := result.Disposition{
		Kind:      evaluated.Disposition.Kind,
		OutcomeID: evaluated.Disposition.OutcomeID,
		Reasons:   evaluated.Disposition.Reasons,
		Handoff:   result.Handoff{State: evaluated.Disposition.Handoff.State, TriggeredBy: evaluated.Disposition.Handoff.TriggeredBy},
	}
	if evaluated.Disposition.Handoff.Routes != nil {
		disposition.Handoff.Routes = []result.HandoffTarget{}
		for _, route := range evaluated.Disposition.Handoff.Routes {
			disposition.Handoff.Routes = append(disposition.Handoff.Routes, result.HandoffTarget{Kind: route.Kind, Name: route.Name})
		}
	}
	contract := result.EvaluatorContract{SpecVersion: evaluated.EvaluatorSpecVersion}
	if contract.Version() == artifacts.CoreVersion {
		contract.Profile = "base"
	}
	encoded, err := disposition.CanonicalWith(contract)
	if err != nil {
		t.Fatalf("%s: typed view is not a disposition: %v", id, err)
	}
	if !bytes.Equal(encoded, evaluated.CanonicalDisposition()) {
		t.Fatalf("%s: typed view encodes to %s, canonical bytes are %s", id, encoded, evaluated.CanonicalDisposition())
	}
}

// cliJSON is the CLI's JSON writer: no HTML escaping, a trailing newline.
func cliJSON(t *testing.T, value any) []byte {
	t.Helper()
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// Refusals agree with the engine's: for each input, the refusal Load or
// Evaluate returns carries exactly the class, phase, code, message and profile
// of the failure the engine returns when called as the CLI calls it, contract
// selection included.
func TestRefusalsAreTheEnginesRefusals(t *testing.T) {
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	engine := evaluation.NewEngine(validator)
	draftPack := strings.Replace(reusePack, `"0.2.0-draft"`, `"0.1.0-draft"`, 1)
	extendedPack := `{"specVersion":"0.3.0","profile":"extended"}`
	oversizedFacts := bytes.Repeat([]byte(" "), int(carrier.HardMaxBytes)+1)
	rows := []struct {
		name            string
		pack            string
		facts, evidence []byte
		supported       []string
		atLoad          bool
	}{
		{name: "not conformant", pack: `{"specVersion":"0.2.0-draft"}`, facts: []byte(`{}`), atLoad: true},
		{name: "not JSON", pack: `{"specVersion":`, facts: []byte(`{}`), atLoad: true},
		{name: "no specVersion", pack: `{}`, facts: []byte(`{}`), atLoad: true},
		{name: "validation-only version", pack: draftPack, facts: []byte(`{}`), atLoad: true},
		{name: "unsupported profile", pack: extendedPack, facts: []byte(`{}`), atLoad: true},
		{name: "duplicate fact member", pack: reusePack, facts: []byte(`{"flag":true,"flag":false}`)},
		{name: "oversized facts", pack: reusePack, facts: oversizedFacts},
		{name: "evidence not an object", pack: reusePack, facts: []byte(`{}`), evidence: []byte(`[]`)},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			contract := result.LegacyEvaluatorContract()
			selected, selection := evaluation.SelectContract([]byte(row.pack))
			var want *evaluation.Failure
			if selection != nil && selection.Code != "JPS-EVALUATION-PACK-SPEC-VERSION" {
				want = selection
			} else {
				if selection == nil {
					contract = selected
				}
				_, want = engine.EvaluateWith([]byte(row.pack), row.facts, row.evidence, evaluation.Options{
					SpecVersion: contract.Version(), Command: command, SupportedExtensions: row.supported, EvidenceSupplied: row.evidence != nil,
				})
			}
			if want == nil {
				t.Fatal("the engine evaluated a row meant to be refused")
			}
			loaded, err := Load(context.Background(), []byte(row.pack), LoadOptions{SupportedExtensions: row.supported})
			if row.atLoad != (err != nil) {
				t.Fatalf("refused at Load: %v, want %v (%v)", err != nil, row.atLoad, err)
			}
			if err == nil {
				_, err = loaded.Evaluate(context.Background(), row.facts, row.evidence)
			}
			refusal, ok := err.(*Refusal)
			if !ok {
				t.Fatalf("%v (%T) is not a *Refusal", err, err)
			}
			got := evaluation.Failure{Class: refusal.Class, Phase: refusal.Phase, Code: refusal.Code, Message: refusal.Message, Profile: refusal.Profile}
			expected := evaluation.Failure{Class: want.Class, Phase: want.Phase, Code: want.Code, Message: want.Message, Profile: want.Profile}
			if !reflect.DeepEqual(got, expected) {
				t.Fatalf("refusal %+v\nengine  %+v", got, expected)
			}
			if !row.atLoad && refusal.Diagnostics != nil {
				t.Fatalf("an Evaluate refusal carries diagnostics: %+v", refusal.Diagnostics)
			}
		})
	}
}

// The diagnostics a Load refusal carries are the report "jpack spec validate"
// gives for the same bytes: that command's options, all of its diagnostics.
func TestLoadRefusalDiagnosticsAreTheSpecValidateReport(t *testing.T) {
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	pack := []byte(`{"specVersion":"0.2.0-draft","id":"not a uri"}`)
	report, operational := validator.Validate(pack, validation.Options{Through: "semantic", Limits: carrier.DefaultLimits()})
	if operational != nil || report.Status == "valid" || len(report.Diagnostics) < 2 {
		t.Fatalf("the vector needs a report with several diagnostics: %+v %+v", report, operational)
	}
	_, err = Load(context.Background(), pack, LoadOptions{})
	refusal, ok := err.(*Refusal)
	if !ok {
		t.Fatalf("%v is not a *Refusal", err)
	}
	if len(refusal.Diagnostics) != len(report.Diagnostics) || refusal.DiagnosticsTruncated != report.DiagnosticsTruncated {
		t.Fatalf("%d diagnostics (truncated %v), spec validate reports %d (truncated %v)", len(refusal.Diagnostics), refusal.DiagnosticsTruncated, len(report.Diagnostics), report.DiagnosticsTruncated)
	}
	for index, diagnostic := range report.Diagnostics {
		want := Diagnostic{Code: diagnostic.Code, Layer: diagnostic.Layer, Severity: diagnostic.Severity, InstancePath: diagnostic.InstancePath, Message: diagnostic.Message, Profile: diagnostic.Profile}
		if refusal.Diagnostics[index] != want {
			t.Fatalf("diagnostic %d = %+v, spec validate reports %+v", index, refusal.Diagnostics[index], want)
		}
	}
}
