package result

import (
	"fmt"
	"strconv"
)

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

// Core050SpecVersion is Core 0.5.0 (RFC 0031: annotations and value sets),
// read under the base profile. A 0.5.0 pack is evaluated as its evaluation
// form EF(P) (Core §8.5), which is a Core 0.3.0 pack, so 0.5.0 keeps
// 0.3.0's grammar, profiles and evaluator rules and adds the members EF
// removes.
const Core050SpecVersion = "0.5.0"

// HasCore030Rules reports whether a Core version applies Core 0.3.0's grammar,
// profiles and evaluator rules: 0.3.0 itself, and 0.5.0, whose evaluation
// form is a Core 0.3.0 pack. It is the one place that says 0.5.0 behaves as
// 0.3.0; callers ask it rather than compare version strings.
func HasCore030Rules(version string) bool {
	return version == "0.3.0" || version == Core050SpecVersion
}

// HasCore050Rules reports whether a version adds Core 0.5.0's members to
// 0.3.0's rules: annotations, value sets and the evaluation form.
func HasCore050Rules(version string) bool { return version == Core050SpecVersion }

// Core030Rules reports whether this contract applies Core 0.3.0's rules.
func (c EvaluatorContract) Core030Rules() bool { return HasCore030Rules(c.Version()) }

// Core050Rules reports whether this contract is Core 0.5.0's.
func (c EvaluatorContract) Core050Rules() bool { return HasCore050Rules(c.Version()) }

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

// SurfaceSpecVersionUnsupportedCode is the code of every refusal, by a
// surface that does not read Core 0.5.0, of a pack declaring it. The surface
// either rests on a companion the specification still pins to Core 0.3.0 or
// reads a pack's conditions or layout without the evaluation form, and it
// refuses the pack explicitly rather than reading members it was not written
// for.
const SurfaceSpecVersionUnsupportedCode = "JPS-SURFACE-SPEC-VERSION-UNSUPPORTED"

// SurfaceSpecVersionUnsupportedMessage is the one sentence every such refusal
// carries; surface names the refusing surface and version the declared one.
func SurfaceSpecVersionUnsupportedMessage(surface, version string) string {
	return fmt.Sprintf("%s does not read packs declaring specVersion %s; it reads Core 0.2.0-draft and 0.3.0 packs.", surface, strconv.QuoteToASCII(version))
}

// RetiredSelectorNote is the sentence a refusal of a pack or a request naming
// 0.4.0-draft or 0.4.0 adds to its message, and "" for every other version.
// 0.4.0-draft was the working selector the text of Core 0.5.0 was staged
// under; it was never released, and no specVersion 0.4.0 exists (Core
// §11). An earlier runtime admitted 0.4.0-draft EXPERIMENTALLY; this one
// does not read it.
func RetiredSelectorNote(version string) string {
	switch version {
	case "0.4.0-draft":
		return "The staged draft selector 0.4.0-draft was never released: its text was released as Core 0.5.0, and a pack written to it moves up by changing its specVersion to \"0.5.0\" (Core §11)."
	case "0.4.0":
		return "No specVersion 0.4.0 exists: the staged draft selector 0.4.0-draft was released as Core 0.5.0 (Core §11)."
	}
	return ""
}

// WithRetiredSelectorNote appends RetiredSelectorNote(version) to message,
// and returns message unchanged for every other version.
func WithRetiredSelectorNote(message, version string) string {
	if note := RetiredSelectorNote(version); note != "" {
		return message + " " + note
	}
	return message
}
