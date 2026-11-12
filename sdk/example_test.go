package sdk_test

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
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
