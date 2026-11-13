package sdk

import (
	"errors"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/vocabulary"
)

// The annotation vocabulary Pack.CheckVocabulary applies, its results and
// reasons, and its two codes (spec/annotation-vocabulary.md, ADR-0088).
// Experimental.
const (
	// VocabularyVersion is the one version of the Spec's annotation
	// vocabulary this runtime checks, "1.0.0".
	VocabularyVersion = vocabulary.Version

	VocabularyPass       = vocabulary.ResultPass
	VocabularyRefused    = vocabulary.ResultRefused
	VocabularyNotChecked = vocabulary.ResultNotChecked

	// VocabularyReasonSpecVersion is the one reason a loaded pack is not
	// checked: it declares a specVersion other than 0.5.0. Load refuses
	// every pack Core refuses and every profile this runtime does not read,
	// the vocabulary's two other reasons.
	VocabularyReasonSpecVersion = vocabulary.ReasonSpecVersion

	// CodeVocabularySeverityValue is a finding: an outcome's severity is not
	// a level.
	CodeVocabularySeverityValue = vocabulary.ValueCode
	// CodeVocabularySeverityIdentifier is an advisory: an outcome's
	// identifier ends with a level other than the outcome's.
	CodeVocabularySeverityIdentifier = vocabulary.IdentifierCode
)

// VocabularyReport is one check of a loaded pack against the Spec's
// annotation vocabulary. Experimental.
type VocabularyReport struct {
	// Vocabulary is "annotation-vocabulary", and Version the vocabulary
	// version applied, VocabularyVersion.
	Vocabulary string
	Version    string
	// Result is VocabularyPass, VocabularyRefused or VocabularyNotChecked.
	// A refusal is the vocabulary's, not Core's: the pack was admitted and
	// evaluates exactly as it would with the key removed.
	Result string
	// Reason is VocabularyReasonSpecVersion when Result is
	// VocabularyNotChecked, and empty otherwise.
	Reason string
	// Findings has one Diagnostic per outcome whose severity is not a level,
	// in document order, with Code CodeVocabularySeverityValue and Layer
	// "vocabulary"; it is empty, never nil, when there is none.
	Findings []Diagnostic
	// Advisories has one Diagnostic per outcome whose identifier ends with a
	// level other than its own, with Code CodeVocabularySeverityIdentifier;
	// it moves no Result, and is empty, never nil, when there is none.
	Advisories []Diagnostic
}

// CheckVocabulary checks the document Load admitted against the Spec's
// annotation vocabulary, version VocabularyVersion: each outcome's severity
// annotation, where it states one, must be one of the levels low, medium,
// high and critical, matched exactly, and never a list. The key at any other
// position, and every other key, is not read. It applies to Core 0.5.0 only,
// including a pack whose required extensions the caller does not support,
// and reports any other specVersion not checked. It reads the admitted
// document, never evaluates and changes nothing; it is the check jpack packs
// validate reports as annotation-vocabulary. An error wraps ErrInternal, as
// View's do, or is a plain error for a Pack Load did not return.
// Experimental.
func (p *Pack) CheckVocabulary() (VocabularyReport, error) {
	if p == nil || p.admitted == nil {
		return VocabularyReport{}, errors.New("sdk: CheckVocabulary on a Pack that Load did not return")
	}
	root, failure := p.admitted.Document(p.options(false))
	if failure != nil {
		return VocabularyReport{}, internalFault(failure.Code, failure.Message)
	}
	checked := vocabulary.Admitted(root)
	return VocabularyReport{
		Vocabulary: checked.Vocabulary,
		Version:    checked.Version,
		Result:     checked.Result,
		Reason:     checked.Reason,
		Findings:   diagnostics(checked.Findings),
		Advisories: diagnostics(checked.Advisories),
	}, nil
}

func diagnostics(from []result.Diagnostic) []Diagnostic {
	out := make([]Diagnostic, 0, len(from))
	for _, diagnostic := range from {
		out = append(out, Diagnostic{
			Code:         diagnostic.Code,
			Layer:        diagnostic.Layer,
			Severity:     diagnostic.Severity,
			InstancePath: diagnostic.InstancePath,
			Message:      diagnostic.Message,
			Profile:      diagnostic.Profile,
		})
	}
	return out
}
