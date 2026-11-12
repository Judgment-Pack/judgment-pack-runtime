package sdk

import (
	"context"
	"errors"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// A fault of this runtime is never a refusal of the caller's input: bundled
// artifacts that fail their integrity check, and any engine failure that
// carries no §8.4 class and is not the profile refusal, wrap ErrInternal.
func TestAnInternalFaultIsNotARefusal(t *testing.T) {
	previous := sharedValidator
	sharedValidator = func() (*validation.Engine, error) { return nil, errors.New("integrity") }
	t.Cleanup(func() { sharedValidator = previous })
	_, err := Load(context.Background(), []byte(reusePack), LoadOptions{})
	var refusal *Refusal
	if !errors.Is(err, ErrInternal) || errors.As(err, &refusal) {
		t.Fatalf("failed integrity check: %v (%T)", err, err)
	}
	if got, want := err.Error(), "sdk: internal fault: JPS-ARTIFACT-INTEGRITY: Bundled JPS artifacts failed their integrity check."; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}

	unclassified := failureError(&evaluation.Failure{Code: "JPS-EVALUATION-INTERNAL", Message: "The validated pack did not decode to a JSON object."})
	if !errors.Is(unclassified, ErrInternal) || errors.As(unclassified, &refusal) {
		t.Fatalf("an unclassified engine failure: %v (%T)", unclassified, unclassified)
	}
	profile := failureError(&evaluation.Failure{Code: "profile-unsupported", Profile: "extended", Phase: "preflight"})
	if !errors.As(profile, &refusal) || errors.Is(profile, ErrInternal) {
		t.Fatalf("the profile refusal: %v (%T)", profile, profile)
	}
}
