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

// routedPack is a Core 0.3.0 pack that declares escalation routes and no
// fallback: a fact other than 1 matches no rule, which the triggers turn into
// a handoff request, and every route the reasons select is listed in the
// pack's order.
const routedPack = `{
  "specVersion": "0.3.0",
  "id": "https://example.invalid/judgment-packs/sdk-routes",
  "version": "1.0.0",
  "title": "Synthetic routed fixture",
  "decision": {"intent": "Exercise routed handoff in-process.", "question": "Is the score one?"},
  "outcomes": [{"id": "yes", "label": "Yes"}, {"id": "no", "label": "No"}],
  "rules": [
    {
      "id": "score-one",
      "description": "The score is one.",
      "when": {"op": "fact", "path": "/score", "operator": "equals", "value": 1},
      "outcome": "yes",
      "onUnknown": "ignore"
    }
  ],
  "escalation": {
    "triggers": ["no-match"],
    "routes": [
      {"target": {"kind": "queue", "name": "Review"}},
      {"target": {"kind": "system", "name": "Ledger"}, "triggers": ["no-match"]}
    ]
  }
}`

func TestACore030PackReportsItsRoutes(t *testing.T) {
	pack := load(t, routedPack, sdk.LoadOptions{})
	evaluated, err := pack.Evaluate(context.Background(), []byte(`{"score":2}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(evaluated.CanonicalDisposition()), `{"handoff":{"routes":[{"kind":"queue","name":"Review"},{"kind":"system","name":"Ledger"}],"state":"requested","triggeredBy":["no-match"]},"kind":"unresolved","reasons":["no-match"]}`; got != want {
		t.Fatalf("canonical disposition = %s, want %s", got, want)
	}
	want := sdk.Disposition{
		Kind:    "unresolved",
		Reasons: []string{"no-match"},
		Handoff: sdk.Handoff{State: "requested", TriggeredBy: []string{"no-match"}, Routes: []sdk.Target{{Kind: "queue", Name: "Review"}, {Kind: "system", Name: "Ledger"}}},
	}
	if !reflect.DeepEqual(evaluated.Disposition, want) {
		t.Fatalf("disposition = %#v, want %#v", evaluated.Disposition, want)
	}
	if evaluated.HandoffTarget != nil || evaluated.EvaluatorSpecVersion != "0.3.0" || evaluated.SpecVersion != "0.3.0" {
		t.Fatalf("target %#v, contract %q, pack %q", evaluated.HandoffTarget, evaluated.EvaluatorSpecVersion, evaluated.SpecVersion)
	}
	encoded, err := evaluated.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{`"command":"sdk evaluate"`, `"evaluatorSpecVersion":"0.3.0"`, `"disposition":{"handoff":{"routes":[{"kind":"queue","name":"Review"},{"kind":"system","name":"Ledger"}],"state":"requested","triggeredBy":["no-match"]},"kind":"unresolved","reasons":["no-match"]}`} {
		if !strings.Contains(string(encoded), member) {
			t.Fatalf("JSON() lacks %s:\n%s", member, encoded)
		}
	}
	if !strings.HasSuffix(string(encoded), "}\n") {
		t.Fatalf("JSON() does not end in one newline: %q", encoded[len(encoded)-4:])
	}

	// An outcome selects no route, and a routed pack still says so with an
	// empty, non-nil route list.
	evaluated, err = pack.Evaluate(context.Background(), []byte(`{"score":1}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if evaluated.Disposition.Handoff.Routes == nil || len(evaluated.Disposition.Handoff.Routes) != 0 {
		t.Fatalf("routes of an outcome = %#v, want empty and non-nil", evaluated.Disposition.Handoff.Routes)
	}
	if got := string(evaluated.CanonicalDisposition()); got != `{"handoff":{"routes":[],"state":"none"},"kind":"outcome","outcomeId":"yes","reasons":[]}` {
		t.Fatalf("canonical disposition = %s", got)
	}
}

func TestLoadRefusesAPackBeforeItsFacts(t *testing.T) {
	limit := 10 << 20
	atLimit := []byte(thresholdPack + strings.Repeat(" ", limit-len(thresholdPack)))
	if _, err := sdk.Load(context.Background(), atLimit, sdk.LoadOptions{}); err != nil {
		t.Fatalf("a pack of exactly the byte limit: %v", err)
	}
	rows := []struct {
		name string
		pack []byte
		want sdk.Refusal
	}{
		{
			name: "one byte over the limit",
			pack: append(atLimit, ' '),
			want: sdk.Refusal{Class: sdk.ClassPackNotConformant, Phase: sdk.PhasePreflight, Code: "JPS-RESOURCE-INPUT-BYTE-LIMIT", Message: "The pack input exceeds the hard 10485760-byte limit."},
		},
		{
			name: "an unsupported Core profile",
			pack: []byte(strings.Replace(routedPack, `"version": "1.0.0",`, `"version": "1.0.0", "profile": "extended",`, 1)),
			want: sdk.Refusal{Phase: sdk.PhasePreflight, Code: "profile-unsupported", Profile: "extended", Diagnostics: []sdk.Diagnostic{{Code: "profile-unsupported", Profile: "extended"}}},
		},
		{
			name: "a valid pack of the validation-only version",
			pack: []byte(strings.Replace(thresholdPack, `"0.2.0-draft"`, `"0.1.0-draft"`, 1)),
			want: sdk.Refusal{
				Class: sdk.ClassPackNotConformant, Phase: sdk.PhasePreflight, Code: "JPS-EVALUATION-PACK-SPEC-VERSION",
				Message: `The pack is not evaluated because it declares specVersion "0.1.0-draft" and this evaluator applies the JPS Core 0.2.0-draft evaluator contract, whose conformance claim is stated in CONFORMANCE.md. JPS §11 makes the declared value exact: an unedited 0.1.0-draft pack is not structurally conforming to 0.2.0-draft and must be re-declared before an implementation claiming this draft evaluates it. Re-declaring is one edit — the specVersion string — and nothing else in the document changes, because §11 says 0.2.0-draft changes no part of the document format: every member, every cross-field rule, and every document-conformance verdict of §§3.1-3.3 stays the same. The 0.1.0-draft schema remains published, and spec validate still validates the pack as it stands. To REPLAY a decision this pack already made, do not re-declare it: fetch the evaluator release that made it and verify its digest first — the replay tuple, docs/building-with-packs.md "Replaying a decision: pin the tuple, not the pack".`,
			},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			_, err := sdk.Load(context.Background(), row.pack, sdk.LoadOptions{})
			assertRefusal(t, err, row.want)
		})
	}
	_, err := sdk.Load(context.Background(), rows[1].pack, sdk.LoadOptions{})
	if got := err.Error(); got != "sdk: profile-unsupported (extended)" {
		t.Fatalf("Error() = %q", got)
	}
}

// Arbitrary bytes are refused as a pack, never a panic and never an internal
// fault, and every such refusal carries its §8.4 class.
func TestArbitraryBytesAreRefusedAsAPack(t *testing.T) {
	for _, input := range [][]byte{
		nil,
		{},
		[]byte(" "),
		[]byte("null"),
		[]byte("[]"),
		[]byte(`"0.2.0-draft"`),
		[]byte("not JSON"),
		[]byte("\xff\xfe{}"),
		[]byte("\ufeff{}"),
		[]byte(`{"specVersion":"0.2.0-draft"}{}`),
		[]byte(`{"specVersion":"0.2.0-draft","specVersion":"0.3.0"}`),
		[]byte(`{"specVersion":` + strings.Repeat("9", 100_000) + `}`),
		[]byte(`{"specVersion":1e999999}`),
		[]byte(strings.Repeat("[", 100_000) + strings.Repeat("]", 100_000)),
		[]byte(`{"a":` + strings.Repeat(`{"a":`, 10_000) + `0` + strings.Repeat("}", 10_001)),
		[]byte(`{"specVersion":"0.3.0","profile":7}`),
	} {
		_, err := sdk.Load(context.Background(), input, sdk.LoadOptions{})
		var refusal *sdk.Refusal
		if !errors.As(err, &refusal) || errors.Is(err, sdk.ErrInternal) {
			t.Fatalf("%.40q: %v (%T), want a refusal", input, err, err)
		}
		if refusal.Class != sdk.ClassPackNotConformant || refusal.Phase != sdk.PhasePreflight {
			t.Fatalf("%.40q: class %q phase %q", input, refusal.Class, refusal.Phase)
		}
	}
}
