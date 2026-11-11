package sdk_test

// These tests use the package the way a program outside this module does: the
// public path and the standard library, nothing under internal/. Every vector
// is written by hand from JPS Core §§7–8.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
)

// thresholdPack is one rule over one decimal-string fact: at or above 1000 is
// one outcome, the fallback is the other, and a fact the rule cannot order is
// unknown and escalates to the declared queue.
const thresholdPack = `{
  "specVersion": "0.2.0-draft",
  "id": "https://example.invalid/judgment-packs/sdk-threshold",
  "version": "0.1.0",
  "title": "Synthetic threshold fixture",
  "description": "Invented content for testing the in-process API; it authorizes nothing.",
  "decision": {
    "intent": "Exercise loading and evaluating a pack in-process.",
    "question": "Is the declared amount at or above the threshold?"
  },
  "outcomes": [
    {"id": "at-or-above", "label": "At or above"},
    {"id": "below", "label": "Below"}
  ],
  "rules": [
    {
      "id": "at-or-above-threshold",
      "description": "The declared amount is at or above 1000.",
      "when": {"op": "fact", "path": "/request/amount", "operator": "greater-than-or-equal", "value": "1000"},
      "outcome": "at-or-above",
      "onUnknown": "escalate"
    }
  ],
  "fallbackOutcome": "below",
  "escalation": {
    "triggers": ["unknown"],
    "target": {"kind": "queue", "name": "Threshold review"}
  }
}`

// extensionPack requires one extension capability.
const extensionPack = `{
  "specVersion": "0.2.0-draft",
  "id": "https://example.invalid/judgment-packs/sdk-required-extension",
  "version": "0.1.0",
  "title": "Synthetic required-extension fixture",
  "decision": {
    "intent": "Exercise required-extension negotiation in-process.",
    "question": "Can this caller interpret the pack?"
  },
  "outcomes": [{"id": "accept", "label": "Accept"}, {"id": "reject", "label": "Reject"}],
  "rules": [
    {
      "id": "always",
      "description": "Always accept.",
      "when": {"op": "literal", "value": true},
      "outcome": "accept",
      "onUnknown": "ignore",
      "extensions": {"com.example.review-policy": {"reviewMode": "two-person"}}
    }
  ],
  "metadata": {"requiredExtensions": ["com.example.review-policy"]}
}`

const (
	atOrAbove = `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"at-or-above","reasons":[]}`
	below     = `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"below","reasons":[]}`
	escalated = `{"handoff":{"state":"requested","triggeredBy":["unknown"]},"kind":"unresolved","reasons":["unknown"]}`
)

func load(t *testing.T, pack string, options sdk.LoadOptions) *sdk.Pack {
	t.Helper()
	loaded, err := sdk.Load(context.Background(), []byte(pack), options)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return loaded
}

func refusalOf(t *testing.T, err error) *sdk.Refusal {
	t.Helper()
	var refusal *sdk.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("error %v (%T) is not a *sdk.Refusal", err, err)
	}
	return refusal
}

func TestLoadThenEvaluateReadsTheDisposition(t *testing.T) {
	pack := load(t, thresholdPack, sdk.LoadOptions{})
	rows := []struct {
		name, facts, canonical string
		disposition            sdk.Disposition
		target                 *sdk.Target
	}{
		{
			name: "at the threshold", facts: `{"request":{"amount":"1000.00"}}`, canonical: atOrAbove,
			disposition: sdk.Disposition{Kind: "outcome", OutcomeID: "at-or-above", Reasons: []string{}, Handoff: sdk.Handoff{State: "none"}},
		},
		{
			name: "below the threshold", facts: `{"request":{"amount":"999.99"}}`, canonical: below,
			disposition: sdk.Disposition{Kind: "outcome", OutcomeID: "below", Reasons: []string{}, Handoff: sdk.Handoff{State: "none"}},
		},
		{
			name: "a JSON number is not ordered", facts: `{"request":{"amount":1000}}`, canonical: escalated,
			disposition: sdk.Disposition{Kind: "unresolved", Reasons: []string{"unknown"}, Handoff: sdk.Handoff{State: "requested", TriggeredBy: []string{"unknown"}}},
			target:      &sdk.Target{Kind: "queue", Name: "Threshold review"},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			evaluated, err := pack.Evaluate(context.Background(), []byte(row.facts), nil)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if got := string(evaluated.CanonicalDisposition()); got != row.canonical {
				t.Fatalf("canonical disposition = %s, want %s", got, row.canonical)
			}
			if !reflect.DeepEqual(evaluated.Disposition, row.disposition) {
				t.Fatalf("disposition = %#v, want %#v", evaluated.Disposition, row.disposition)
			}
			if !reflect.DeepEqual(evaluated.HandoffTarget, row.target) {
				t.Fatalf("handoff target = %#v, want %#v", evaluated.HandoffTarget, row.target)
			}
			if evaluated.SpecVersion != "0.2.0-draft" || evaluated.EvaluatorSpecVersion != "0.2.0-draft" {
				t.Fatalf("versions = %q, %q", evaluated.SpecVersion, evaluated.EvaluatorSpecVersion)
			}
			if evaluated.PackID != "https://example.invalid/judgment-packs/sdk-threshold" || evaluated.PackVersion != "0.1.0" {
				t.Fatalf("pack identity = %q, %q", evaluated.PackID, evaluated.PackVersion)
			}
		})
	}
}

func TestCanonicalDispositionIsTheCallersCopy(t *testing.T) {
	evaluated, err := load(t, thresholdPack, sdk.LoadOptions{}).Evaluate(context.Background(), []byte(`{"request":{"amount":"1"}}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	first := evaluated.CanonicalDisposition()
	first[0] = 'X'
	if got := string(evaluated.CanonicalDisposition()); got != below {
		t.Fatalf("canonical disposition after editing a returned copy = %s", got)
	}
}

func TestLoadTakesASnapshotOfThePack(t *testing.T) {
	data := []byte(thresholdPack)
	pack, err := sdk.Load(context.Background(), data, sdk.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite the threshold in the caller's slice, keeping its length.
	copy(data[bytes.Index(data, []byte(`"1000"`)):], `"0000"`)
	evaluated, err := pack.Evaluate(context.Background(), []byte(`{"request":{"amount":"999.99"}}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(evaluated.CanonicalDisposition()); got != below {
		t.Fatalf("edited caller slice changed the loaded pack: %s", got)
	}
}

func TestEvaluateRefusesEmptyFactsAndEmptySuppliedEvidence(t *testing.T) {
	pack := load(t, thresholdPack, sdk.LoadOptions{})
	_, err := pack.Evaluate(context.Background(), nil, nil)
	assertRefusal(t, err, sdk.Refusal{
		Class: sdk.ClassMalformedInput, Phase: sdk.PhasePreflight, Code: "JPS-EVALUATION-INPUT-MISSING",
		Message: "The facts input is required and was empty; empty bytes are not a carrier-conforming JSON text.",
	})
	// nil evidence is an omitted document; an empty non-nil slice is a supplied one.
	if _, err := pack.Evaluate(context.Background(), []byte(`{}`), nil); err != nil {
		t.Fatalf("omitted evidence: %v", err)
	}
	_, err = pack.Evaluate(context.Background(), []byte(`{}`), []byte{})
	assertRefusal(t, err, sdk.Refusal{
		Class: sdk.ClassMalformedInput, Phase: sdk.PhasePreflight, Code: "JPS-EVALUATION-INPUT-JSON",
		Message: "The evidence input is empty; empty bytes are not a carrier-conforming JSON text. Supply a JSON object, or omit the evidence document to treat every declared requirement as unknown.",
	})
}

func TestUnsupportedRequiredExtensionIsRefusedByEvaluateAfterTheInputs(t *testing.T) {
	pack := load(t, extensionPack, sdk.LoadOptions{})
	_, err := pack.Evaluate(context.Background(), []byte(`{}`), nil)
	assertRefusal(t, err, sdk.Refusal{
		Class: sdk.ClassUnsupportedRequiredExtension, Phase: sdk.PhasePreflight, Code: "JPS-CAPABILITY-REQUIRED-EXTENSION",
		Message: "The pack is not evaluated because it requires extension capabilities this evaluation does not support: com.example.review-policy. No disposition is produced; the unsupported part may be the part that decides.",
	})
	// §8.4 precedence: a malformed facts document outranks the extension.
	_, err = pack.Evaluate(context.Background(), []byte(`[`), nil)
	if refusal := refusalOf(t, err); refusal.Class != sdk.ClassMalformedInput {
		t.Fatalf("malformed facts with an unsupported extension: class %q, want %q", refusal.Class, sdk.ClassMalformedInput)
	}
	supported := load(t, extensionPack, sdk.LoadOptions{SupportedExtensions: []string{"com.example.review-policy"}})
	evaluated, err := supported.Evaluate(context.Background(), []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("supported extension: %v", err)
	}
	if got := string(evaluated.CanonicalDisposition()); got != `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"accept","reasons":[]}` {
		t.Fatalf("supported extension disposition = %s", got)
	}
}

func TestLoadRefusesANonConformantPackWithItsDiagnostics(t *testing.T) {
	_, err := sdk.Load(context.Background(), []byte(`{"specVersion":"0.2.0-draft"}`), sdk.LoadOptions{})
	refusal := refusalOf(t, err)
	if refusal.Class != sdk.ClassPackNotConformant || refusal.Phase != sdk.PhasePreflight || refusal.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" {
		t.Fatalf("refusal = %#v", refusal)
	}
	if len(refusal.Diagnostics) == 0 {
		t.Fatal("a pack refused for failing validation carries no diagnostics")
	}
	first := refusal.Diagnostics[0]
	location := first.InstancePath
	if location == "" {
		location = "<root>"
	}
	want := fmt.Sprintf(`The pack is not evaluated because its document conformance status is "invalid"; evaluation requires a fully conformant pack. Run spec validate for the complete report. First diagnostic: %s at %s: %s`, first.Code, location, first.Message)
	if refusal.Message != want {
		t.Fatalf("message = %q, want %q", refusal.Message, want)
	}
	for _, diagnostic := range refusal.Diagnostics {
		if diagnostic.Code == "" || diagnostic.Layer == "" || diagnostic.Severity == "" || diagnostic.Message == "" {
			t.Fatalf("incomplete diagnostic %#v", diagnostic)
		}
	}
}

func TestRefusalErrorIsOneSanitizedLine(t *testing.T) {
	refusal := &sdk.Refusal{Class: sdk.ClassMalformedInput, Code: "JPS-X", Message: "a\nb\x1b[31m"}
	if got, want := refusal.Error(), "sdk: malformed-input: JPS-X: a?b?[31m"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestADoneContextIsNotARefusal(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sdk.Load(cancelled, []byte(thresholdPack), sdk.LoadOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load with a cancelled context: %v", err)
	}
	pack := load(t, thresholdPack, sdk.LoadOptions{})
	_, err := pack.Evaluate(cancelled, []byte(`{}`), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Evaluate with a cancelled context: %v", err)
	}
	var refusal *sdk.Refusal
	if errors.As(err, &refusal) {
		t.Fatalf("a done context was reported as a refusal: %v", refusal)
	}
	var noContext context.Context
	if _, err := pack.Evaluate(noContext, []byte(`{}`), nil); err == nil || errors.As(err, &refusal) {
		t.Fatalf("Evaluate with a nil context: %v", err)
	}
	if _, err := sdk.Load(noContext, []byte(thresholdPack), sdk.LoadOptions{}); err == nil || errors.As(err, &refusal) {
		t.Fatalf("Load with a nil context: %v", err)
	}
	var zero sdk.Pack
	if _, err := zero.Evaluate(context.Background(), []byte(`{}`), nil); err == nil || errors.As(err, &refusal) {
		t.Fatalf("Evaluate on a zero Pack: %v", err)
	}
}

// TestOnePackServesConcurrentEvaluations is meant for -race: one loaded pack,
// many goroutines, each checking its own answer.
func TestOnePackServesConcurrentEvaluations(t *testing.T) {
	pack := load(t, thresholdPack, sdk.LoadOptions{})
	inputs := []struct{ facts, want string }{
		{`{"request":{"amount":"1000"}}`, atOrAbove},
		{`{"request":{"amount":"5"}}`, below},
		{`{"request":{"amount":true}}`, escalated},
		{`{}`, escalated},
	}
	var group sync.WaitGroup
	failures := make(chan string, 64)
	for worker := 0; worker < 16; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for round := 0; round < 25; round++ {
				input := inputs[(worker+round)%len(inputs)]
				evaluated, err := pack.Evaluate(context.Background(), []byte(input.facts), []byte(`{}`))
				if err != nil {
					failures <- err.Error()
					return
				}
				if got := string(evaluated.CanonicalDisposition()); got != input.want {
					failures <- fmt.Sprintf("%s: %s, want %s", input.facts, got, input.want)
					return
				}
			}
		}()
	}
	// Loads of other packs run alongside, sharing the one validator.
	for worker := 0; worker < 4; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := sdk.Load(context.Background(), []byte(extensionPack), sdk.LoadOptions{}); err != nil {
				failures <- err.Error()
			}
		}()
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
}

func TestDescribeNamesTheBundledContractsAndCorpora(t *testing.T) {
	description, err := sdk.Describe()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"0.1.0-draft", "0.2.0-draft", "0.3.0"}; !reflect.DeepEqual(description.SpecVersions, want) {
		t.Fatalf("spec versions = %v, want %v", description.SpecVersions, want)
	}
	want := []sdk.Evaluator{
		{SpecVersion: "0.2.0-draft", CorpusSuiteVersion: "0.2.0-draft"},
		{SpecVersion: "0.3.0", Profiles: []string{"base"}, CorpusSuiteVersion: "0.3.0"},
	}
	if !reflect.DeepEqual(description.Evaluators, want) {
		t.Fatalf("evaluators = %#v, want %#v", description.Evaluators, want)
	}
	if description.ConformanceClaimReference != "CONFORMANCE.md" {
		t.Fatalf("claim reference = %q", description.ConformanceClaimReference)
	}
	// Inside this module's own tests the build carries no released version.
	if strings.HasPrefix(description.RuntimeVersion, "(") {
		t.Fatalf("runtime version = %q", description.RuntimeVersion)
	}
}

func assertRefusal(t *testing.T, err error, want sdk.Refusal) {
	t.Helper()
	if got := refusalOf(t, err); !reflect.DeepEqual(*got, want) {
		t.Fatalf("refusal = %#v\nwant      %#v", *got, want)
	}
}
