package sdk

import (
	"context"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

const reusePack = `{
  "specVersion": "0.2.0-draft",
  "id": "https://example.invalid/judgment-packs/sdk-reuse",
  "version": "0.1.0",
  "title": "Synthetic reuse fixture",
  "decision": {"intent": "Exercise reuse of one loaded pack.", "question": "Is the flag set?"},
  "outcomes": [{"id": "set", "label": "Set"}, {"id": "unset", "label": "Unset"}],
  "rules": [
    {
      "id": "flag-set",
      "description": "The flag is true.",
      "when": {"op": "fact", "path": "/flag", "operator": "equals", "value": true},
      "outcome": "set",
      "onUnknown": "ignore"
    }
  ],
  "fallbackOutcome": "unset"
}`

// withValidator runs one test with Load using validator instead of the shared
// one, so the test owns the validator a Pack was loaded with.
func withValidator(t *testing.T, validator *validation.Engine) {
	t.Helper()
	previous := sharedValidator
	sharedValidator = func() (*validation.Engine, error) { return validator, nil }
	t.Cleanup(func() { sharedValidator = previous })
}

// Evaluate never validates the pack again. The validator a Pack was loaded with
// is emptied after Load: an emptied validator refuses every document, so an
// evaluation that consulted it would be refused, and every one here evaluates.
func TestEvaluateDoesNotValidateThePackAgain(t *testing.T) {
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	withValidator(t, validator)
	pack, err := Load(context.Background(), []byte(reusePack), LoadOptions{SupportedExtensions: []string{"com.example.b", "com.example.a", "com.example.b"}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	*validator = validation.Engine{}
	if _, operational := validator.Validate([]byte(reusePack), validation.Options{}); operational == nil {
		if report, _ := validator.Validate([]byte(reusePack), validation.Options{}); report.Status == "valid" {
			t.Fatal("the emptied validator still admits the pack, so this test proves nothing")
		}
	}
	for round := 0; round < 50; round++ {
		facts := `{"flag":true}`
		want := `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"set","reasons":[]}`
		if round%2 == 1 {
			facts = `{"flag":false}`
			want = `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"unset","reasons":[]}`
		}
		evaluated, err := pack.Evaluate(context.Background(), []byte(facts), nil)
		if err != nil {
			t.Fatalf("round %d: the pack was validated again: %v", round, err)
		}
		if got := string(evaluated.CanonicalDisposition()); got != want {
			t.Fatalf("round %d: %s, want %s", round, got, want)
		}
	}
}
