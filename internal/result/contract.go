package result

import "fmt"

// EvaluatorContract explicitly identifies the Core grammar and ceilings applied
// to a disposition. The zero value preserves the original 0.2 contract.
type EvaluatorContract struct {
	SpecVersion string
	Profile     string
}

func LegacyEvaluatorContract() EvaluatorContract {
	return EvaluatorContract{SpecVersion: EvaluatorSpecVersion}
}
func Core030EvaluatorContract() EvaluatorContract {
	return EvaluatorContract{SpecVersion: "0.3.0", Profile: "base"}
}
func (c EvaluatorContract) Version() string {
	if c.SpecVersion == "" {
		return EvaluatorSpecVersion
	}
	return c.SpecVersion
}
func (c EvaluatorContract) Validate() error {
	if c.Version() != EvaluatorSpecVersion && c.Version() != "0.3.0" {
		return fmt.Errorf("unsupported evaluator contract %q", c.Version())
	}
	if c.Profile != "" && c.Profile != "base" {
		return fmt.Errorf("unsupported evaluator profile %q", c.Profile)
	}
	if c.Version() != "0.3.0" && c.Profile != "" {
		return fmt.Errorf("profile is not declared by Core %s", c.Version())
	}
	return nil
}
