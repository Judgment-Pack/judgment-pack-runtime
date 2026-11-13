// Package vocabulary is this runtime's checker for the Spec's annotation
// vocabulary: the one well-known annotation key, severity, on an outcome,
// with its closed list of levels (spec/annotation-vocabulary.md, version
// 1.0.0, ADR-0088). It reads a pack Core 0.5.0 already admitted and reports a
// result of its own, never a Core result: a pack it refuses stays
// semantically conforming and evaluates exactly as before (§2.6, §6.4).
//
// The levels are written here, not read from the Spec's vocabulary.json at
// run time; a test holds them to the vendored copy of that file.
package vocabulary

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	// Name is the vocabulary's name, as its vocabulary.json states it.
	Name = "annotation-vocabulary"
	// Version is the one vocabulary version this runtime applies, which
	// every report states (§6.1).
	Version = "1.0.0"
	// SpecVersion is the one Core selector the vocabulary applies to (§6.2).
	SpecVersion = "0.5.0"
	// Key is the one key the vocabulary defines, on an outcome's annotations.
	Key = "severity"
	// AnnotationsName is the extensions member that holds annotations.
	AnnotationsName = "org.judgmentpack.annotations"
	// Layer is the layer of this checker's diagnostics. It is not one of
	// Core's layers, so no reader takes a finding for Core's refusal.
	Layer = "vocabulary"

	// ValueCode is the refusal of an outcome severity that is not a level
	// (§6.3).
	ValueCode = "JPS-VOCABULARY-SEVERITY-VALUE"
	// IdentifierCode is the advisory for an outcome identifier whose last
	// segment is a level other than the outcome's (§3.1).
	IdentifierCode = "JPS-VOCABULARY-SEVERITY-IDENTIFIER"

	// The three results a report can hold.
	ResultPass       = "pass"
	ResultRefused    = "refused"
	ResultNotChecked = "not-checked"

	// The three reasons a report gives when the vocabulary was not applied.
	ReasonSpecVersion        = "spec-version"
	ReasonCoreRefused        = "core-refused"
	ReasonProfileUnsupported = "profile-unsupported"
)

// levels are vocabulary 1.0.0's levels in ascending order (§2.2).
var levels = [...]string{"low", "medium", "high", "critical"}

// Levels returns the levels in ascending order, least severe first. The
// caller owns the slice.
func Levels() []string {
	return append([]string(nil), levels[:]...)
}

// Rank is a level's place in the ascending order, 0 for low; ok is false for
// every other value, a string that differs from a level only in case or by a
// look-alike character included: matching is by exact code point (§2.4).
func Rank(value string) (rank int, ok bool) {
	for index, level := range levels {
		if value == level {
			return index, true
		}
	}
	return 0, false
}

// IsLevel reports whether a decoded annotation value is a level: a string
// equal to one, never a list, whatever it holds (§2.5).
func IsLevel(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	_, ok = Rank(text)
	return ok
}

// Check is the vocabulary check of one pack's bytes, given Core's validation
// of the same bytes through the semantic layer, or nil when that validation
// failed operationally. It follows §6.2: the selector first, read with only
// the parsing needed to find it, then Core's result, then the outcomes.
func Check(data []byte, core *result.Validation, limits carrier.Limits) result.VocabularyReport {
	scanned, failure := carrier.ScanRootDeclarations(data, limits)
	if failure == nil {
		if version, ok := scanned["specVersion"].(string); ok && version != SpecVersion {
			return notChecked(ReasonSpecVersion, "")
		}
	}
	// A pack whose bytes do not parse, or whose specVersion is missing or not
	// a string, is Core's refusal, which the result below reports.
	if core == nil {
		return notChecked(ReasonCoreRefused, "carrier")
	}
	switch core.Status {
	case "valid":
	case "unsupported":
		for _, diagnostic := range core.Diagnostics {
			if diagnostic.Code == ReasonProfileUnsupported {
				return notChecked(ReasonProfileUnsupported, "")
			}
		}
		// A required extension this runtime does not support is reported
		// only after the semantic layer passed, and does not stop the check
		// (§6.2). Any other unsupported verdict is Core's.
		if !onlyRequiredExtensions(core) {
			return notChecked(ReasonCoreRefused, failedLayer(core, "capability"))
		}
	default:
		return notChecked(ReasonCoreRefused, failedLayer(core, "structural"))
	}
	value, failure := carrier.Decode(data, limits)
	root, isObject := value.(map[string]any)
	if failure != nil || !isObject {
		// Unreachable: Core decoded these bytes to an object above.
		return notChecked(ReasonCoreRefused, "carrier")
	}
	return Admitted(root)
}

// onlyRequiredExtensions reports whether an unsupported verdict is the one
// the capability layer gives a semantically conforming pack for its required
// extensions.
func onlyRequiredExtensions(core *result.Validation) bool {
	semantic := false
	for _, layer := range core.Layers {
		if layer.Name == "semantic" && layer.Status == "passed" {
			semantic = true
		}
	}
	if !semantic || len(core.Diagnostics) == 0 {
		return false
	}
	for _, diagnostic := range core.Diagnostics {
		if diagnostic.Code != "JPS-CAPABILITY-REQUIRED-EXTENSION" {
			return false
		}
	}
	return true
}

// failedLayer is the Core layer that refused the pack: the last failed layer
// of the validation, or fallback when it names none.
func failedLayer(core *result.Validation, fallback string) string {
	for index := len(core.Layers) - 1; index >= 0; index-- {
		if core.Layers[index].Status == "failed" {
			return core.Layers[index].Name
		}
	}
	return fallback
}

// Admitted is the vocabulary check of one admitted document's root: not
// applied for a specVersion other than 0.5.0, and otherwise every outcome's
// severity, if it states one, held to the levels. It reads only
// /outcomes/<i>/extensions/org.judgmentpack.annotations/severity and the
// outcome's id; the key anywhere else, and every other key, is not read
// (§2.1, §6.3). It reports every refused outcome, in document order, and the
// identifier advisory exactly where §3.1's three conditions hold.
func Admitted(root map[string]any) result.VocabularyReport {
	if version, _ := root["specVersion"].(string); version != SpecVersion {
		return notChecked(ReasonSpecVersion, "")
	}
	report := newReport(ResultPass)
	outcomes, _ := root["outcomes"].([]any)
	for index, raw := range outcomes {
		outcome, _ := raw.(map[string]any)
		value, present := severityOf(outcome)
		if !present {
			continue
		}
		id, _ := outcome["id"].(string)
		level, isString := value.(string)
		if _, ok := Rank(level); !isString || !ok {
			report.Findings = append(report.Findings, result.ErrorDiagnostic(ValueCode, Layer,
				fmt.Sprintf("/outcomes/%d/extensions/%s/%s", index, AnnotationsName, Key),
				refusalMessage(id, value)))
			continue
		}
		if segment := lastSegment(id); IsLevel(segment) && segment != level {
			report.Advisories = append(report.Advisories, result.WarningDiagnostic(IdentifierCode, Layer,
				fmt.Sprintf("/outcomes/%d/id", index),
				fmt.Sprintf("Outcome %s ends its identifier with the level %s and states the severity %s. An identifier states no level (Core §5), so the two disagree; annotation vocabulary %s advises an identifier that names no level.", quote(id), quote(segment), quote(level), Version)))
		}
	}
	if len(report.Findings) > 0 {
		report.Result = ResultRefused
	}
	return report
}

// severityOf returns the value of an outcome's severity annotation, and
// whether the outcome states one.
func severityOf(outcome map[string]any) (any, bool) {
	extensions, _ := outcome["extensions"].(map[string]any)
	annotations, _ := extensions[AnnotationsName].(map[string]any)
	value, present := annotations[Key]
	return value, present
}

// SeverityOf returns an outcome's level from a decoded outcome object: the
// value of its severity annotation when that is a level, and "" when it
// states none or states a value that is not one. It never maps a value onto
// a level it resembles (§4).
func SeverityOf(outcome map[string]any) string {
	value, present := severityOf(outcome)
	if !present || !IsLevel(value) {
		return ""
	}
	return value.(string)
}

// lastSegment is an identifier's last hyphen-separated segment, or the whole
// identifier when it holds no hyphen (§3.1).
func lastSegment(id string) string {
	return id[strings.LastIndexByte(id, '-')+1:]
}

func refusalMessage(id string, value any) string {
	if _, isList := value.([]any); isList {
		return fmt.Sprintf("Outcome %s states its severity as the list %s. The key's value is one level, a string, so annotation vocabulary %s refuses every list, a one-element list included; Core admits the pack, and this is the vocabulary's refusal, not Core's.", quote(id), quote(value), Version)
	}
	return fmt.Sprintf("Outcome %s states the severity %s, which is not a level of annotation vocabulary %s (%s; matched exactly, by code point); Core admits the pack, and this is the vocabulary's refusal, not Core's.", quote(id), quote(value), Version, strings.Join(levels[:], ", "))
}

// quote writes a decoded value as JSON text in printable ASCII: every code
// point outside it is written as a \u escape, so a look-alike character, a
// format character or a control cannot pass for the level it resembles in a
// message, a terminal or a log.
func quote(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "(a value that does not encode)"
	}
	var builder strings.Builder
	for _, character := range string(encoded) {
		if character >= 0x20 && character < 0x7f {
			builder.WriteRune(character)
			continue
		}
		if character > 0xffff {
			high, low := utf16.EncodeRune(character)
			fmt.Fprintf(&builder, `\u%04x\u%04x`, high, low)
			continue
		}
		fmt.Fprintf(&builder, `\u%04x`, character)
	}
	return builder.String()
}

func newReport(status string) result.VocabularyReport {
	return result.VocabularyReport{
		Vocabulary: Name,
		Version:    Version,
		Result:     status,
		Findings:   []result.Diagnostic{},
		Advisories: []result.Diagnostic{},
	}
}

func notChecked(reason, layer string) result.VocabularyReport {
	report := newReport(ResultNotChecked)
	report.Reason = reason
	report.CoreLayer = layer
	return report
}
