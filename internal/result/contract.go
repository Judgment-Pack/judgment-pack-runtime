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

// StagedDraftSpecVersion is the staged draft selector of RFC 0031 (annotations
// and value sets), admitted EXPERIMENTALLY from a staged, unreleased commit of
// the specification. It is not a Core release, carries no conformance claim, and
// may be renamed or changed before any release.
const StagedDraftSpecVersion = "0.4.0-draft"

// HasCore030Rules reports whether a Core version applies Core 0.3.0's grammar,
// profiles and evaluator rules: 0.3.0 itself, and the staged draft selector,
// whose evaluation form is a Core 0.3.0 pack. It is the one place that says the
// draft behaves as 0.3.0; callers ask it rather than compare version strings.
func HasCore030Rules(version string) bool {
	return version == "0.3.0" || version == StagedDraftSpecVersion
}

// IsStagedDraft reports whether a version is the staged draft selector.
func IsStagedDraft(version string) bool { return version == StagedDraftSpecVersion }

// Core030Rules reports whether this contract applies Core 0.3.0's rules.
func (c EvaluatorContract) Core030Rules() bool { return HasCore030Rules(c.Version()) }

// Staged reports whether this contract is the staged draft selector's.
func (c EvaluatorContract) Staged() bool { return IsStagedDraft(c.Version()) }

func (c EvaluatorContract) Validate() error {
	if c.Version() != EvaluatorSpecVersion && !c.Core030Rules() {
		return fmt.Errorf("unsupported evaluator contract %q", c.Version())
	}
	if c.Profile != "" && c.Profile != "base" {
		return fmt.Errorf("unsupported evaluator profile %q", c.Profile)
	}
	if !c.Core030Rules() && c.Profile != "" {
		return fmt.Errorf("profile is not declared by Core %s", c.Version())
	}
	return nil
}

// StagedDraftUnsupportedCode is the code of every refusal of the staged draft
// selector by a surface that does not read it.
const StagedDraftUnsupportedCode = "JPS-STAGED-DRAFT-UNSUPPORTED"

// StagedDraftUnsupportedMessage is the one sentence every staged-draft refusal
// carries; surface names the refusing surface.
func StagedDraftUnsupportedMessage(surface string) string {
	return fmt.Sprintf("%s does not read packs declaring the staged draft selector %s, which is EXPERIMENTAL; it reads Core 0.2.0-draft and 0.3.0 packs.", surface, StagedDraftSpecVersion)
}
