package validation

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evalform"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// collectCore050Structural maps Core 0.5.0's annotation and value-set
// constraints to the codes its corpus names (conformance/README.md). A
// structural code is Core's code for the definition where Core
// has one, otherwise Core's code for the failing keyword, otherwise one new code
// per keyword; two refusals take a code named for the rule instead.
//
// It reports whether it handled the failure. A failure it does not recognize
// falls through to the released mappings unchanged. It keys on the failing
// keyword and the instance location only, never on the schema's identifier.
//
// The engine reports a propertyNames failure at an unreliable instance location
// (and its causes at none), so an annotation key is reported from the failure
// at the annotations object that holds it: recurse handles every other cause.
func collectCore050Structural(validationError *jsonschema.ValidationError, location []string, document any, diagnostics *diagnosticCollector, recurse func(*jsonschema.ValidationError)) bool {
	add := func(code string, parts []string, message string) {
		diagnostics.addError(code, "structural", carrier.Pointer(parts), message)
	}
	annotations := annotationsDepth(location)
	if annotations == len(location) && len(validationError.Causes) > 0 {
		for _, cause := range validationError.Causes {
			if names, ok := cause.ErrorKind.(*kind.PropertyNames); ok {
				key := append(slices.Clone(location), names.Property)
				annotationKeyDiagnostics(cause, key, add)
				continue
			}
			recurse(cause)
		}
		return true
	}
	switch typed := validationError.ErrorKind.(type) {
	case *kind.PropertyNames:
		// Every other propertyNames failure is an extensions object's name
		// grammar; extensionNameDiagnostics reports it at the name, once.
		return true
	case *kind.MinProperties:
		add("JPS-STRUCTURE-MIN-PROPERTIES", location, fmt.Sprintf("Object has %d member%s; at least %d %s required.", typed.Got, plural(typed.Got), typed.Want, isAre(typed.Want)))
		return true
	case *kind.MaxProperties:
		add("JPS-STRUCTURE-MAX-PROPERTIES", location, fmt.Sprintf("Object has %d members; at most %d are allowed by the selected profile.", typed.Got, typed.Want))
		return true
	case *kind.MaxLength:
		add("JPS-STRUCTURE-MAX-LENGTH", location, fmt.Sprintf("String has %d characters; at most %d are allowed by the selected profile.", typed.Got, typed.Want))
		return true
	case *kind.MaxItems:
		add("JPS-STRUCTURE-MAXITEMS", location, fmt.Sprintf("Collection has %d items; at most %d are allowed by the selected profile.", typed.Got, typed.Want))
		return true
	case *kind.Not:
		switch {
		case annotations >= 0 && annotations < len(location):
			add("JPS-STRUCTURE-NOT", location, "Annotation string must not contain a control character, a line or paragraph separator, a bidirectional format character or an invisible character, and must not begin or end with white space.")
			return true
		case conditionPosition(location):
			// The only not in a condition shape is the value-set reference rule:
			// a fact condition carries value or valueSet, never both (§3.3).
			add("JPS-STRUCTURE-VALUE-SET-REFERENCE", location, "A fact condition carries either value or valueSet, not both.")
			return true
		}
		if code, ok := lineTerminatorCode(location); ok {
			// The not beside a local identifier's, the pack version's, a decimal
			// operand's or a required extension's pattern refuses a line
			// terminator for engines whose $ admits a trailing one (§4). It is
			// the definition's refusal, so it takes the definition's code, and
			// the message the pattern gives, so that a value both refuse is one
			// finding. An extension name is reported, once, by
			// extensionNameDiagnostics.
			if code == "JPS-STRUCTURE-EXTENSION-NAME" {
				return true
			}
			if refused, isString := stringAt(document, location); isString {
				add(code, location, patternMessage(code, &kind.Pattern{Got: refused}))
				return true
			}
		}
	case *kind.Pattern:
		// 0.5.0 states the reserved prefix in a not beside the name grammar,
		// so the grammar's pattern no longer names it; it is still the
		// extension-name grammar, reported once by extensionNameDiagnostics.
		if typed.Want == core050ExtensionNamePattern {
			return true
		}
	case *kind.Const:
		// The only const on a fact condition's operator is the value-set
		// reference rule: a reference is read only by in (§3.3).
		if n := len(location); n >= 2 && location[n-1] == "operator" && conditionPosition(location[:n-1]) {
			add("JPS-STRUCTURE-VALUE-SET-REFERENCE", location, "A valueSet reference requires the in operator.")
			return true
		}
	}
	return false
}

// core050ExtensionNamePattern is the extension-name grammar of the 0.5.0
// schema, without the reserved-prefix lookahead the 0.3.0 pattern carries.
const core050ExtensionNamePattern = `^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9\-]*)+$`

// lineTerminatorCode returns the code of the definition whose line-terminator
// not refuses the value at location: the pack version, a required extension
// name, a decimal operand, or a local identifier, wherever the schema uses
// one. It reports false for every other location, which the released
// mappings then handle.
func lineTerminatorCode(location []string) (string, bool) {
	n := len(location)
	switch {
	case n == 1 && location[0] == "version":
		return "JPS-STRUCTURE-PACK-VERSION", true
	case n == 3 && location[0] == "metadata" && location[1] == "requiredExtensions":
		return "JPS-STRUCTURE-EXTENSION-NAME", true
	case comparisonOperandValue(location):
		return "JPS-STRUCTURE-DECIMAL-OPERAND", true
	case n == 1 && location[0] == "fallbackOutcome":
		return "JPS-STRUCTURE-LOCAL-ID", true
	case n >= 2 && (location[n-2] == "evidenceRequirementRefs" || location[n-2] == "sourceRefs"):
		return "JPS-STRUCTURE-LOCAL-ID", true
	case n >= 2:
		switch location[n-1] {
		case "id", "outcome", "targetRule", "valueSet", "evidenceRequirement":
			return "JPS-STRUCTURE-LOCAL-ID", true
		}
	}
	return "", false
}

// stringAt returns the string at location in document, and whether there is
// one.
func stringAt(document any, location []string) (string, bool) {
	node := document
	for _, part := range location {
		switch typed := node.(type) {
		case map[string]any:
			node = typed[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return "", false
			}
			node = typed[index]
		default:
			return "", false
		}
	}
	text, ok := node.(string)
	return text, ok
}

// annotationKeyDiagnostics reports one refused annotation key. The key grammar
// is the local-identifier grammar, and the not beside it repeats that grammar's
// refusal for engines whose $ admits a trailing line feed, so both take
// JPS-STRUCTURE-LOCAL-ID with one message: a key refused by both is one finding.
func annotationKeyDiagnostics(validationError *jsonschema.ValidationError, key []string, add func(string, []string, string)) {
	name := last(key)
	for _, cause := range validationError.Causes {
		switch typed := cause.ErrorKind.(type) {
		case *kind.Pattern, *kind.Not:
			// The key is quoted in ASCII, so a look-alike or an invisible
			// character in it can be told apart in the message (D2).
			add("JPS-STRUCTURE-LOCAL-ID", key, fmt.Sprintf("Annotation key %s has an invalid shape; use lowercase words joined by single hyphens, for example %q.", strconv.QuoteToASCII(name), "review-tier"))
		case *kind.MaxLength:
			add("JPS-STRUCTURE-MAX-LENGTH", key, fmt.Sprintf("Annotation key has %d characters; at most %d are allowed by the selected profile.", typed.Got, typed.Want))
		default:
			add("JPS-STRUCTURE-SCHEMA", key, "Annotation key does not satisfy a structural constraint.")
		}
	}
}

// annotationsDepth returns the length of the location prefix that names an
// annotations object (…/extensions/org.judgmentpack.annotations), or -1 when
// the location is not inside one.
func annotationsDepth(location []string) int {
	for index := 1; index < len(location); index++ {
		if location[index] == evalform.AnnotationsName && location[index-1] == "extensions" {
			return index + 1
		}
	}
	return -1
}

// conditionPosition reports whether a location names a condition: the root
// applicability, a rule's or an exception's when, a not's condition, or an item
// of an all's or any's conditions.
func conditionPosition(location []string) bool {
	n := len(location)
	if n == 0 {
		return false
	}
	switch location[n-1] {
	case "applicability", "when", "condition":
		return true
	}
	return n >= 2 && location[n-2] == "conditions"
}

func isAre(count int) string {
	if count == 1 {
		return "is"
	}
	return "are"
}
