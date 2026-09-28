package evaluation

import (
	"fmt"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Draft RFC 0016 (outcome values): the admission gate, the Core projection, the
// resolution step and the output marker (ADR-0039). Nothing here is reachable
// unless the caller opts in. The extension name is reserved, the bundled schema
// refuses it in both places the RFC names, and spec validate rejects a pack that
// carries it, unchanged.
//
// The work is divided as it is for RFC 0008. The gate here owns what the draft
// adds: the form of a value declaration, and the rule that ties a declaration on
// an outcome to the name's entry in metadata.requiredExtensions. Everything else
// is the untouched validator's, run over the pack's Core projection. That
// includes the RFC's rule that the name appears on an outcome and nowhere else.
// The projection takes the name out of each outcome's extensions and out of no
// other place, so a pack that carries it on the root, on a rule or on any other
// object still carries it in the projection, where the validator refuses the
// reserved name as it always has. The gate lists no such place, and so it cannot
// fall out of step with the places the validator knows.

// OutcomeValuesExtension is the extension name of draft RFC 0016.
const OutcomeValuesExtension = "org.judgmentpack.outcome-values"

// The three types a value source may declare.
const (
	valueTypeString  = "string"
	valueTypeDecimal = "decimal"
	valueTypeBoolean = "boolean"
)

// rfc0016Grammar is the draft RFC 0016 gate.
var rfc0016Grammar = draftGrammar{
	rfc:         "0016",
	diagnostics: rfc0016Diagnostics,
	project:     projectOutcomeValues,
	projection:  "The instance path is relative to that projection, in which the value declaration of each outcome and each entry of metadata.requiredExtensions that names " + OutcomeValuesExtension + " are removed, so an entry of metadata.requiredExtensions that follows a removed one is named one place earlier.",
}

// oneDraft refuses a call that opts into both drafts. One evaluation runs under
// one draft grammar, and the marker of a payload names one RFC (ADR-0039). The
// refusal is an invocation's: it has no §8.4 class, because no input was read
// and nothing was evaluated.
func oneDraft(options Options) *Failure {
	if !options.RFC0008Quantifiers || !options.RFC0016OutcomeValues {
		return nil
	}
	return &Failure{
		Code:     "JPS-INVOCATION-DRAFT-RFC",
		Message:  "The draft RFC 0008 quantifiers and the draft RFC 0016 outcome values are separate prototypes, and one evaluation runs under one of them. Opt into one.",
		ExitCode: result.ExitInvocation,
	}
}

// rfc0016Diagnostics reports what the draft grammar refuses in one decoded pack.
// Outcomes are read in the order the pack lists them and the values of one
// declaration in the order of their names, so a pack with two faults reports the
// same one first on every run.
//
// A member of the wrong JSON type on the way to a declaration is not reported
// here. An outcomes member that is not an array, an outcome that is not an
// object and an extensions member that is not an object are all in the
// projection as the author wrote them, and the validator refuses them there.
func rfc0016Diagnostics(root map[string]any) []result.Diagnostic {
	diagnostics := []result.Diagnostic{}
	carried := 0
	for index, entry := range asArray(root["outcomes"]) {
		declaration, present := valueDeclaration(entry)
		if !present {
			continue
		}
		carried++
		location := []string{"outcomes", fmt.Sprint(index), "extensions", OutcomeValuesExtension}
		declarationDiagnostics(declaration, location, &diagnostics)
	}

	// The rule between the declarations and the name's entry as required. An
	// outcome that carries a declaration needs the entry, which is the RFC's
	// rule. An entry with no outcome carrying a declaration is §9's fault, a
	// required name with no value, which the validator cannot find in a
	// projection that no longer holds the entry. And an entry made twice is the
	// schema's uniqueItems, lost the same way.
	listed := []int{}
	if metadata, ok := root["metadata"].(map[string]any); ok {
		for index, item := range asArray(metadata["requiredExtensions"]) {
			if name, ok := item.(string); ok && name == OutcomeValuesExtension {
				listed = append(listed, index)
			}
		}
	}
	required := func(parts []string, message string) {
		diagnostics = append(diagnostics, result.ErrorDiagnostic(
			"JPS-EVALUATION-RFC0016-REQUIRED", "semantic", carrier.Pointer(parts), message))
	}
	switch {
	case carried > 0 && len(listed) == 0:
		required([]string{"metadata", "requiredExtensions"},
			"Draft RFC 0016 requires a pack in which an outcome carries a value declaration to list "+OutcomeValuesExtension+" in metadata.requiredExtensions.")
	case carried == 0 && len(listed) > 0:
		required([]string{"metadata", "requiredExtensions", fmt.Sprint(listed[0])},
			OutcomeValuesExtension+" is listed as required and no outcome carries a value declaration. JPS §9 requires a required name to have a value, and draft RFC 0016 admits that value on an outcome only.")
	}
	if len(listed) > 1 {
		required([]string{"metadata", "requiredExtensions", fmt.Sprint(listed[1])},
			OutcomeValuesExtension+" is listed more than once in metadata.requiredExtensions, whose items are unique.")
	}
	return diagnostics
}

// valueDeclaration reads the value of the extension off one entry of the pack's
// outcomes. The second result says whether the entry carries the name at all,
// whatever its value is. An entry that is no object, or whose extensions member
// is none, carries nothing: each assertion then leaves a nil map, and a nil map
// has no member.
func valueDeclaration(entry any) (any, bool) {
	outcome, _ := entry.(map[string]any)
	extensions, _ := outcome["extensions"].(map[string]any)
	declaration, present := extensions[OutcomeValuesExtension]
	return declaration, present
}

// declarationDiagnostics holds one value declaration to the RFC's Declaration
// section: a non-empty object whose member names are value names and whose
// members are value sources.
func declarationDiagnostics(declaration any, location []string, diagnostics *[]result.Diagnostic) {
	fault := func(parts []string, message string) {
		*diagnostics = append(*diagnostics, result.ErrorDiagnostic(
			"JPS-EVALUATION-RFC0016-DECLARATION", "structural", carrier.Pointer(parts), message))
	}
	values, ok := declaration.(map[string]any)
	if !ok {
		fault(location, "A draft RFC 0016 value declaration must be a JSON object.")
		return
	}
	if len(values) == 0 {
		fault(location, "A draft RFC 0016 value declaration must declare at least one value.")
		return
	}
	for _, name := range sortedMembers(values) {
		at := appendPath(location, name)
		if !result.OutcomeValueName(name) {
			fault(at, fmt.Sprintf("%q is not a draft RFC 0016 value name: one lowercase ASCII letter, then ASCII letters and digits, and nothing else.", display.Sanitize(name)))
		}
		sourceDiagnostics(values[name], at, fault)
	}
}

// sourceDiagnostics holds one value source to the RFC's table: the members type,
// constant and fromFact and no others, a type of the three, exactly one of the
// other two, a constant of the declared type, and a pointer in the grammar of
// fact.path.
func sourceDiagnostics(value any, location []string, fault func([]string, string)) {
	source, ok := value.(map[string]any)
	if !ok {
		fault(location, "A draft RFC 0016 value source must be a JSON object.")
		return
	}
	for _, member := range sortedMembers(source) {
		if member != "type" && member != "constant" && member != "fromFact" {
			fault(appendPath(location, member), "Member is not allowed on a draft RFC 0016 value source, whose members are type, constant and fromFact.")
		}
	}
	declared, typed := source["type"].(string)
	known := typed && (declared == valueTypeString || declared == valueTypeDecimal || declared == valueTypeBoolean)
	if !known {
		fault(appendPath(location, "type"), "Draft RFC 0016 requires the type of a value source, and it is \"string\", \"decimal\" or \"boolean\".")
	}
	constant, hasConstant := source["constant"]
	pointer, hasPointer := source["fromFact"]
	if hasConstant == hasPointer {
		fault(location, "A draft RFC 0016 value source carries exactly one of constant and fromFact.")
	}
	// A constant is judged against the type only where the type is one of the
	// three. Against any other, the fault is the type's and is already reported.
	if hasConstant && known && !admittedValue(declared, constant) {
		fault(appendPath(location, "constant"), fmt.Sprintf("The constant of a draft RFC 0016 value of type %q must be %s.", declared, valueForm(declared)))
	}
	if hasPointer {
		text, isString := pointer.(string)
		if !isString || !pointerPattern.MatchString(text) {
			fault(appendPath(location, "fromFact"), "Draft RFC 0016 fromFact must be a JSON Pointer string, as fact.path is.")
		}
	}
}

// admittedValue is the RFC's one rule for what a declared type admits, read by
// the gate for a constant and by the resolution step for a value selected from
// the facts, so the two cannot differ.
//
// A string is a sequence of Unicode scalar values. Every string this function is
// handed came out of carrier.Decode, which refuses a document that is not valid
// UTF-8 and one that holds an unpaired surrogate escape, so every such string
// already is one and no string is turned away here. The RFC's two surrogate
// cases are therefore decided before this function is reached: a pack that holds
// one is refused by the carrier layer, and a facts document that holds one is
// refused as a malformed input and selects nothing (ADR-0039).
func admittedValue(declared string, value any) bool {
	switch declared {
	case valueTypeString:
		_, ok := value.(string)
		return ok
	case valueTypeDecimal:
		text, ok := value.(string)
		return ok && decimalPattern.MatchString(text)
	case valueTypeBoolean:
		_, ok := value.(bool)
		return ok
	default:
		return false
	}
}

// valueForm names what a declared type admits, for a diagnostic.
func valueForm(declared string) string {
	switch declared {
	case valueTypeDecimal:
		return "a JSON string that satisfies the decimal grammar of JPS §2.2"
	case valueTypeBoolean:
		return "a JSON Boolean"
	default:
		return "a JSON string"
	}
}

// projectOutcomeValues produces the pack's Core projection under RFC 0016: the
// same document with the extension's value removed from each outcome's
// extensions object, and with each entry that names the extension removed from
// metadata.requiredExtensions. Both containers stay where they were, empty if
// the name was all they held, and the bundled schema admits both empty.
//
// The name is removed from an outcome's extensions and from nowhere else. On any
// other object it stays, and the validator refuses it there.
//
// The projection is a validation instrument only. It is never evaluated:
// evaluation and resolution read the original document, which is left untouched.
func projectOutcomeValues(root map[string]any) map[string]any {
	projected := copyObject(root)
	if metadata, ok := root["metadata"].(map[string]any); ok {
		if required, ok := metadata["requiredExtensions"].([]any); ok {
			kept := make([]any, 0, len(required))
			for _, item := range required {
				if name, ok := item.(string); ok && name == OutcomeValuesExtension {
					continue
				}
				kept = append(kept, item)
			}
			copied := copyObject(metadata)
			copied["requiredExtensions"] = kept
			projected["metadata"] = copied
		}
	}
	if outcomes, ok := root["outcomes"].([]any); ok {
		copied := make([]any, 0, len(outcomes))
		for _, entry := range outcomes {
			if _, present := valueDeclaration(entry); !present {
				copied = append(copied, entry)
				continue
			}
			outcome := entry.(map[string]any)
			extensions := copyObject(outcome["extensions"].(map[string]any))
			delete(extensions, OutcomeValuesExtension)
			item := copyObject(outcome)
			item["extensions"] = extensions
			copied = append(copied, item)
		}
		projected["outcomes"] = copied
	}
	return projected
}

// outcomeValuesPrototype builds the in-band marker every evaluation under the
// RFC 0016 opt-in carries. It says what draftPrototype says for RFC 0008: the
// pack is not valid under the published specification, and what it uses is a
// prototype of an open proposal that may never be accepted. It adds what is
// particular to this draft, that the disposition's value member is the RFC's and
// that a value drawn from a fact is a copy of that fact.
func outcomeValuesPrototype(packRoot map[string]any, specVersion string) *result.DraftPrototype {
	outcomes := []string{}
	for _, entry := range asArray(packRoot["outcomes"]) {
		if _, present := valueDeclaration(entry); present {
			id, _ := entry.(map[string]any)["id"].(string)
			outcomes = append(outcomes, id)
		}
	}
	sort.Strings(outcomes)
	note := fmt.Sprintf(
		"Draft RFC 0016 (outcome values) prototype. A value declaration is a draft-RFC prototype, not part of JPS %s; a pack carrying one is NOT valid under it and spec validate rejects it. Packs carrying one are not inputs the JPS Core %s evaluator class defines — that class describes conforming packs of that exact specVersion, which this is not — so this result is evidence for nothing about any requirement of that class; see CONFORMANCE.md. The value member of the disposition is the draft's and no member of §8.3, and the draft can withhold an outcome Core would produce. A value drawn from a fact is a copy of that fact and is not verified: keep a disposition that carries one as the facts are kept.",
		specVersion, result.EvaluatorSpecVersion)
	if len(outcomes) == 0 {
		note = fmt.Sprintf(
			"Draft RFC 0016 (outcome values) prototype was enabled, but no outcome of this pack declares a value, so it remains a plain JPS %s pack.",
			specVersion)
	}
	return &result.DraftPrototype{
		RFC:                       "0016",
		Status:                    "draft-rfc-prototype",
		Operators:                 []string{},
		Outcomes:                  outcomes,
		PackValidUnderSpecVersion: len(outcomes) == 0,
		Note:                      note,
	}
}

// Trace vocabulary of the resolution step. One entry is recorded for each value
// the produced outcome declares, in the order of the value names. An entry names
// the value and the outcome and says whether the value resolved. It never
// carries the value.
const (
	traceStageOutcomeValue = "outcome-value"
	traceValueResolved     = "resolved"
	traceValueUnresolved   = "unresolved"
)

// resolveValues is the resolution step of draft RFC 0016, run once for the one
// outcome §8 produced. It returns the resolved values, or reports that one did
// not resolve. A nil map with a true report is an outcome that declares no
// value, for which the step does nothing.
//
// Every declared value is resolved, including those after one that did not
// resolve, so the trace says of each value whether it resolved. The result does
// not depend on that: one value that does not resolve withholds the outcome
// whatever the others did.
//
// The work is charged to the evaluation's §10 limit before it is done, as a
// condition tree's is: one unit for each outcome the pack declares, which is
// what finding the produced one among them costs, one for each declared value,
// the resolution of each pointer, and the size of each value that is carried,
// which is what the type check reads and what the canonical form writes. An
// outcome that declares no value is charged nothing, so a pack without a
// declaration is charged under the opt-in exactly what it is charged without
// it. The caller reads exceeded before it reads the result.
func (r *resolver) resolveValues(outcomeID string) (map[string]any, bool) {
	declaration := r.declaredValues(outcomeID)
	if declaration == nil {
		return nil, true
	}
	names := sortedMembers(declaration)
	if !r.eval.charge((len(asArray(r.pack["outcomes"])) + len(names)) * unitNode) {
		return nil, false
	}
	resolved := make(map[string]any, len(names))
	complete := true
	for _, name := range names {
		source, _ := declaration[name].(map[string]any)
		declared, _ := source["type"].(string)
		value, selected := source["constant"]
		if pointer, from := source["fromFact"].(string); from {
			if !r.eval.chargePointer(pointer) {
				return nil, false
			}
			value, selected = r.eval.resolve(r.facts, pointer)
		}
		if selected && !r.eval.charge(valueUnits(value)) {
			return nil, false
		}
		// A pointer that did not resolve selected nothing, and nothing is of no
		// type, so the one question covers both ways a value does not resolve.
		entry := result.TraceEntry{Stage: traceStageOutcomeValue, ID: name, Condition: traceValueResolved, Outcome: outcomeID}
		if admittedValue(declared, value) {
			resolved[name] = value
		} else {
			complete = false
			entry.Condition = traceValueUnresolved
		}
		r.trace = append(r.trace, entry)
	}
	if !complete {
		return nil, false
	}
	return resolved, true
}

// declaredValues returns the value declaration of the outcome the pack declares
// under one id, or nil where that outcome declares none. The pack was admitted
// through the gate above, so a declaration that is present is a non-empty object
// of value sources.
func (r *resolver) declaredValues(outcomeID string) map[string]any {
	for _, entry := range asArray(r.pack["outcomes"]) {
		outcome, _ := entry.(map[string]any)
		if id, _ := outcome["id"].(string); id != outcomeID {
			continue
		}
		declaration, _ := valueDeclaration(entry)
		values, _ := declaration.(map[string]any)
		return values
	}
	return nil
}
