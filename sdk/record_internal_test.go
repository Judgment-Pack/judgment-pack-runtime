package sdk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/recordhook"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/record"
)

// recordPack030 is written by hand for the decision-record tests: one ordered
// rule over a decimal-string fact, a fallback outcome, and one declared
// evidence requirement that no rule reads.
const recordPack030 = `{
  "specVersion": "0.3.0",
  "id": "https://example.invalid/judgment-packs/sdk-record",
  "version": "1.0.0",
  "title": "Synthetic decision-record fixture",
  "decision": {"intent": "Exercise the decision record in-process.", "question": "Is the declared amount at or above 1000?"},
  "evidenceRequirements": [{"id": "invoice", "description": "An invoice is on file.", "required": false}],
  "outcomes": [{"id": "review", "label": "Review"}, {"id": "accept", "label": "Accept"}],
  "rules": [
    {
      "id": "large",
      "description": "The declared amount is at or above 1000.",
      "when": {"op": "fact", "path": "/amount", "operator": "greater-than-or-equal", "value": "1000"},
      "outcome": "review",
      "onUnknown": "ignore"
    }
  ],
  "fallbackOutcome": "accept"
}`

// recordPack050 is recordPack030 under Core 0.5.0, with annotations on the
// outcome the rule selects.
const recordPack050 = `{
  "specVersion": "0.5.0",
  "id": "https://example.invalid/judgment-packs/sdk-record",
  "version": "1.0.0",
  "title": "Synthetic decision-record fixture",
  "decision": {"intent": "Exercise the decision record in-process.", "question": "Is the declared amount at or above 1000?"},
  "evidenceRequirements": [{"id": "invoice", "description": "An invoice is on file.", "required": false}],
  "outcomes": [
    {"id": "review", "label": "Review", "extensions": {"org.judgmentpack.annotations": {"team": "intake", "follow-up": ["call", "write"]}}},
    {"id": "accept", "label": "Accept"}
  ],
  "rules": [
    {
      "id": "large",
      "description": "The declared amount is at or above 1000.",
      "when": {"op": "fact", "path": "/amount", "operator": "greater-than-or-equal", "value": "1000"},
      "outcome": "review",
      "onUnknown": "ignore"
    }
  ],
  "fallbackOutcome": "accept"
}`

// sha is "sha256:" and the hex SHA-256 of text, computed here and not by the
// code under test, so every digest below follows from bytes written by hand.
func sha(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func withRuntimeVersion(t *testing.T, version string) {
	t.Helper()
	previous := runtimeVersion
	runtimeVersion = func() string { return version }
	t.Cleanup(func() { runtimeVersion = previous })
}

// The whole of two records, written by hand: a Core 0.3.0 evaluation with a
// supplied evidence document, a binding and a runtime version, and a Core
// 0.5.0 one with the evidence omitted, no binding and no runtime version. The
// facts are given in a spelling other than their canonical text, which is
// what each digest is over: the 0.5.0 facts hold two member names that UTF-16
// and UTF-8 order differently, U+1F600 before U+E000, and a string an HTML-safe
// encoder would escape.
func TestTwoDecisionRecordsByHand(t *testing.T) {
	const (
		preparation = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
		settings    = "sha256:abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"
		disposition = `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"review","reasons":[]}`
	)
	rows := []struct {
		name, pack, facts, evidence, runtime string
		omitEvidence                         bool
		binding                              record.Binding
		want                                 string
	}{
		{
			name: "core 0.3.0", pack: recordPack030, runtime: "v0.45.0",
			facts: "{ \"amount\" : \"12\\u0035\\u0030\" }", evidence: `{"invoice": "present"}`,
			binding: record.Binding{"settings": settings, "judgment-pack-runner/preparation/1": preparation},
			want: `{"binding":{"judgment-pack-runner/preparation/1":"` + preparation + `","settings":"` + settings + `"},` +
				`"disposition":` + disposition + `,` +
				`"evaluator":{"runtimeVersion":"v0.45.0","specVersion":"0.3.0"},` +
				`"evidenceDigest":"` + sha(`{"invoice":"present"}`) + `",` +
				`"factsDigest":"` + sha(`{"amount":"1250"}`) + `",` +
				`"pack":{"digest":"` + sha(recordPack030) + `","id":"https://example.invalid/judgment-packs/sdk-record","specVersion":"0.3.0","version":"1.0.0"},` +
				`"recordVersion":"1.0.0"}`,
		},
		{
			name: "core 0.5.0", pack: recordPack050, runtime: "",
			facts: `{"\ue000": 1, "note": "a\tb", "amount": "1250", "\ud83d\ude00": "\u003c&>", "n": [1.50, -0, null, true]}`, omitEvidence: true,
			want: `{"disposition":` + disposition + `,` +
				`"evaluator":{"specVersion":"0.5.0"},` +
				`"evidenceDigest":"` + sha(`{}`) + `",` +
				`"factsDigest":"` + sha(`{"amount":"1250","n":[1.50,-0,null,true],"note":"a\tb",`+"\"\U0001F600\":\"<&>\",\"\ue000\":1}") + `",` +
				`"pack":{"digest":"` + sha(recordPack050) + `","id":"https://example.invalid/judgment-packs/sdk-record","specVersion":"0.5.0","version":"1.0.0"},` +
				`"recordVersion":"1.0.0"}`,
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			withRuntimeVersion(t, row.runtime)
			loaded, err := Load(context.Background(), []byte(row.pack), LoadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			var evidence []byte
			if !row.omitEvidence {
				evidence = []byte(row.evidence)
			}
			evaluated, decided, err := loaded.EvaluateWithRecord(context.Background(), []byte(row.facts), evidence, row.binding)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(decided.JSON()); got != row.want {
				t.Fatalf("record\n got %s\nwant %s", got, row.want)
			}
			if got, want := decided.Digest(), sha(row.want); got != want {
				t.Fatalf("digest = %s, want %s", got, want)
			}
			if decided.Disposition != disposition || string(evaluated.CanonicalDisposition()) != disposition {
				t.Fatalf("disposition = %s, evaluation's %s", decided.Disposition, evaluated.CanonicalDisposition())
			}
			if decided.Evaluator != (record.Evaluator{RuntimeVersion: row.runtime, SpecVersion: evaluated.EvaluatorSpecVersion}) {
				t.Fatalf("evaluator = %#v", decided.Evaluator)
			}
		})
	}
}

// An Evaluation that holds no Pack Load returned has no annotations to read,
// and says so with a plain error, even when it names an outcome.
func TestOutcomeAnnotationsNeedAnEvaluationOfALoadedPack(t *testing.T) {
	for name, evaluated := range map[string]*Evaluation{
		"no pack":         {Disposition: Disposition{Kind: "outcome", OutcomeID: "review"}},
		"an unloaded one": {Disposition: Disposition{Kind: "outcome", OutcomeID: "review"}, pack: &Pack{}},
	} {
		if annotations, err := evaluated.OutcomeAnnotations(); err == nil || errors.Is(err, ErrInternal) || annotations != nil {
			t.Fatalf("%s: annotations %#v, error %v", name, annotations, err)
		}
	}
}

// A record this runtime cannot build is a fault of this runtime, never a
// *Refusal and never a record: a builder that is not initialised, one that
// fails, and one that returns something else.
func TestARecordFaultIsAnInternalFault(t *testing.T) {
	loaded, err := Load(context.Background(), []byte(recordPack030), LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	previous := recordhook.Build
	t.Cleanup(func() { recordhook.Build = previous })
	for name, builder := range map[string]func(recordhook.Input) (any, error){
		"not initialised": nil,
		"fails":           func(recordhook.Input) (any, error) { return nil, errors.New("the facts document does not encode") },
		"returns another": func(recordhook.Input) (any, error) { return "a record", nil },
	} {
		t.Run(name, func(t *testing.T) {
			recordhook.Build = builder
			evaluated, decided, err := loaded.EvaluateWithRecord(context.Background(), []byte(`{"amount":"1"}`), nil, nil)
			var refusal *Refusal
			if !errors.Is(err, ErrInternal) || errors.As(err, &refusal) || !strings.Contains(err.Error(), recordFaultCode) {
				t.Fatalf("error %v (%T), want an internal fault", err, err)
			}
			if evaluated != nil || decided.JSON() != nil || decided.Digest() != "" {
				t.Fatalf("a fault returned %v and a record %q", evaluated, decided.JSON())
			}
		})
	}
	// The builder holds what it is given to the record's shape even when the
	// binding reached it without the check before evaluation.
	recordhook.Build = previous
	if _, err := recordhook.Build(recordhook.Input{
		PackID: "p", PackVersion: "1", PackSpecVersion: "0.3.0", PackDigest: sha(""), EvaluatorSpecVersion: "0.3.0",
		Facts: []byte(`{}`), Evidence: nil, Disposition: []byte(`{"kind":"not-applicable"}`),
		Binding: map[string]string{"Upper": sha("")},
	}); !errors.Is(err, record.ErrBinding) {
		t.Fatalf("a malformed binding given to the builder: %v", err)
	}
	for name, input := range map[string]recordhook.Input{
		"an empty identity member":  {PackID: "", PackVersion: "1", PackSpecVersion: "0.3.0", PackDigest: sha(""), EvaluatorSpecVersion: "0.3.0", Facts: []byte(`{}`), Evidence: nil, Disposition: []byte(`{"kind":"not-applicable"}`)},
		"facts the carrier refuses": {PackID: "p", PackVersion: "1", PackSpecVersion: "0.3.0", PackDigest: sha(""), EvaluatorSpecVersion: "0.3.0", Facts: []byte(`{"a":1,"a":2}`), Evidence: nil, Disposition: []byte(`{"kind":"not-applicable"}`)},
		"a spaced disposition":      {PackID: "p", PackVersion: "1", PackSpecVersion: "0.3.0", PackDigest: sha(""), EvaluatorSpecVersion: "0.3.0", Facts: []byte(`{}`), Evidence: nil, Disposition: []byte(`{"kind": "not-applicable"}`)},
		"a disposition not JSON":    {PackID: "p", PackVersion: "1", PackSpecVersion: "0.3.0", PackDigest: sha(""), EvaluatorSpecVersion: "0.3.0", Facts: []byte(`{}`), Evidence: nil, Disposition: []byte(`{"kind"`)},
		"a disposition not object":  {PackID: "p", PackVersion: "1", PackSpecVersion: "0.3.0", PackDigest: sha(""), EvaluatorSpecVersion: "0.3.0", Facts: []byte(`{}`), Evidence: nil, Disposition: []byte(`[]`)},
	} {
		if built, err := recordhook.Build(input); err == nil {
			t.Errorf("%s: built %#v", name, built)
		}
	}
}
