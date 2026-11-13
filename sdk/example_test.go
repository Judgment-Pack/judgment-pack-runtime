package sdk_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/record"
)

// Load a pack once, evaluate it, and read the disposition or the refusal.
func Example() {
	ctx := context.Background()
	pack, err := sdk.Load(ctx, []byte(thresholdPack), sdk.LoadOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}

	evaluated, err := pack.Evaluate(ctx, []byte(`{"request":{"amount":"1250"}}`), nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(evaluated.Disposition.Kind, evaluated.Disposition.OutcomeID)
	fmt.Println(string(evaluated.CanonicalDisposition()))

	_, err = pack.Evaluate(ctx, []byte(`{"request":`), nil)
	var refusal *sdk.Refusal
	if errors.As(err, &refusal) {
		fmt.Println(refusal.Class, refusal.Phase)
	}
	// Output:
	// outcome at-or-above
	// {"handoff":{"state":"none"},"kind":"outcome","outcomeId":"at-or-above","reasons":[]}
	// malformed-input preflight
}

// Describe a loaded pack without evaluating it: its identity, its rules as
// typed data, and the same view as Markdown for a reviewer. The view is
// experimental.
func ExamplePack_View() {
	pack, err := sdk.Load(context.Background(), []byte(thresholdPack), sdk.LoadOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	view, err := pack.View()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(view.Identity.ID, view.Identity.Version, view.Identity.SpecVersion)
	fmt.Println(strings.HasPrefix(view.Identity.Digest, "sha256:"), sdk.Identity(view.Identity) == pack.Identity())
	for _, rule := range view.Rules {
		fmt.Println(rule.ID, rule.When.Op, rule.When.Path, rule.When.Operator, rule.When.Value, rule.Outcome)
	}
	markdown := string(view.Markdown())
	start, end := strings.Index(markdown, "### Rule 1"), strings.Index(markdown, "## Fallback outcome")
	fmt.Print(markdown[start:end])
	// Output:
	// https://example.invalid/judgment-packs/sdk-threshold 0.1.0 0.2.0-draft
	// true true
	// at-or-above-threshold fact /request/amount greater-than-or-equal "1000" at-or-above
	// ### Rule 1: at\-or\-above\-threshold
	//
	// - Description: The declared amount is at or above 1000\.
	// - Outcome: at\-or\-above
	// - On unknown: escalate
	// - When:
	//   - fact \"\/request\/amount\" greater\-than\-or\-equal \"1000\"
}

// Evaluate and take a decision record of the evaluation: the pack's identity,
// digests of the facts and the evidence as evaluated, the disposition, the
// evaluator and the caller's binding, as canonical bytes with their own
// digest. Nothing is written; storing or sending the record is the caller's.
// The chosen outcome's annotations are read from the same evaluation. The
// record and OutcomeAnnotations are experimental.
func ExamplePack_EvaluateWithRecord() {
	ctx := context.Background()
	pack, err := sdk.Load(ctx, []byte(annotatedPack), sdk.LoadOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	digest := func(data []byte) string {
		sum := sha256.Sum256(data)
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	// The binding names what the caller says the evaluation was run under,
	// here the digest of a settings file the caller read. It is carried as
	// given.
	binding := record.Binding{"settings": digest([]byte("threshold=1000\n"))}

	evaluated, decided, err := pack.EvaluateWithRecord(ctx, []byte(`{ "amount": "1250" }`), nil, binding)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(decided.Pack.ID, decided.Pack.Version, decided.Pack.SpecVersion, decided.Pack.Digest == pack.Identity().Digest)
	fmt.Println(decided.Disposition)
	fmt.Println(decided.FactsDigest == digest([]byte(`{"amount":"1250"}`)), decided.EvidenceDigest == digest([]byte(`{}`)))
	fmt.Println(decided.Evaluator.SpecVersion, decided.Binding["settings"] == binding["settings"])
	fmt.Println(decided.Digest() == digest(decided.JSON()), strings.HasPrefix(string(decided.JSON()), `{"binding":`))

	annotations, err := evaluated.OutcomeAnnotations()
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, annotation := range annotations {
		if annotation.List != nil {
			fmt.Println(annotation.Key, strings.Join(annotation.List, ", "))
		} else {
			fmt.Println(annotation.Key, annotation.Value)
		}
	}

	_, _, err = pack.EvaluateWithRecord(ctx, []byte(`{"amount":"1250"}`), nil, record.Binding{"Settings": "sha256:0"})
	fmt.Println(errors.Is(err, record.ErrBinding))
	// Output:
	// https://example.invalid/judgment-packs/sdk-record 1.0.0 0.5.0 true
	// {"handoff":{"state":"none"},"kind":"outcome","outcomeId":"review","reasons":[]}
	// true true
	// 0.5.0 true
	// true true
	// follow-up call, write
	// team intake
	// true
}

// Take a decision record whose facts and evidence are named by keyed
// digests the caller computes with its own key, over the canonical texts
// package sdk/record returns, so that a reader without the key cannot test
// candidate facts against the record's document digests. The disposition
// still says what the pack decided, and a reader holding the pack can test
// a candidate against it (docs/decision-records.md). The record carries the
// algorithm's label and the key identifier, never the key. A reader holding
// the key checks the documents; one without it learns that they cannot be
// checked.
// The keyed record is experimental.
func ExamplePack_EvaluateWithKeyedRecord() {
	ctx := context.Background()
	pack, err := sdk.Load(ctx, []byte(annotatedPack), sdk.LoadOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	key := []byte("a synthetic key, kept by the caller")
	keyedDigest := func(algorithm, keyID string, canonical []byte) ([]byte, error) {
		if algorithm != record.HMACSHA256 || keyID != "k-2f9c" {
			return nil, errors.New("no key for that identifier")
		}
		mac := hmac.New(sha256.New, key)
		mac.Write(canonical)
		return mac.Sum(nil), nil
	}
	facts := []byte(`{ "amount": "1250" }`)
	canonicalFacts, err := record.CanonicalFacts(facts)
	if err != nil {
		fmt.Println(err)
		return
	}
	canonicalEvidence, _ := record.CanonicalEvidence(nil) // omitted: {}
	factsDigest, _ := keyedDigest(record.HMACSHA256, "k-2f9c", canonicalFacts)
	evidenceDigest, _ := keyedDigest(record.HMACSHA256, "k-2f9c", canonicalEvidence)

	_, decided, err := pack.EvaluateWithKeyedRecord(ctx, facts, nil, nil, record.KeyedDigests{
		Algorithm: record.HMACSHA256, KeyID: "k-2f9c", Facts: factsDigest, Evidence: evidenceDigest,
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(canonicalFacts), decided.Form(), decided.FactsDigest == "")
	fmt.Println(strings.Contains(string(decided.JSON()), `"keyedDigests":{"algorithm":"hmac-sha256",`))

	stored, err := record.Read(decided.JSON())
	if err != nil {
		fmt.Println(err)
		return
	}
	withKey, _ := stored.CheckDocuments(facts, nil, keyedDigest)
	withoutKey, _ := stored.CheckDocuments(facts, nil, nil)
	otherFacts, _ := stored.CheckDocuments([]byte(`{"amount":"1251"}`), nil, keyedDigest)
	fmt.Println(withKey.Facts, withoutKey.Facts, otherFacts.Facts)
	// Output:
	// {"amount":"1250"} keyed true
	// true
	// match not-checkable-without-key mismatch
}
