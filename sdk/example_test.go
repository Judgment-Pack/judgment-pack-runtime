package sdk_test

import (
	"context"
	"errors"
	"fmt"

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
