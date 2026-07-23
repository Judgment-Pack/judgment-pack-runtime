package validation

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

var idCollections = []string{"outcomes", "rules", "evidenceRequirements", "sources", "exceptions"}
var extensionNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9-]*)+$`)

func semanticDiagnostics(root map[string]any) ([]result.Diagnostic, bool) {
	diagnostics := newDiagnosticCollector()
	for _, collection := range idCollections {
		if !duplicateIDDiagnostics(root, collection, diagnostics) {
			return diagnostics.sorted(), diagnostics.truncated
		}
	}

	outcomes := idSet(root, "outcomes")
	rules := idSet(root, "rules")
	evidence := idSet(root, "evidenceRequirements")
	sources := idSet(root, "sources")

	for index, rule := range objectList(root["rules"]) {
		if !unresolved(diagnostics, stringValue(rule["outcome"]), outcomes, []string{"rules", fmt.Sprint(index), "outcome"}, "JPS-SEMANTIC-UNRESOLVED-OUTCOME", "Outcome reference does not resolve.") {
			return diagnostics.sorted(), diagnostics.truncated
		}
		for refIndex, reference := range stringList(rule["evidenceRequirementRefs"]) {
			if !unresolved(diagnostics, reference, evidence, []string{"rules", fmt.Sprint(index), "evidenceRequirementRefs", fmt.Sprint(refIndex)}, "JPS-SEMANTIC-UNRESOLVED-EVIDENCE", "Evidence requirement reference does not resolve.") {
				return diagnostics.sorted(), diagnostics.truncated
			}
		}
		for refIndex, reference := range stringList(rule["sourceRefs"]) {
			if !unresolved(diagnostics, reference, sources, []string{"rules", fmt.Sprint(index), "sourceRefs", fmt.Sprint(refIndex)}, "JPS-SEMANTIC-UNRESOLVED-SOURCE", "Source reference does not resolve.") {
				return diagnostics.sorted(), diagnostics.truncated
			}
		}
		if !walkCondition(rule["when"], []string{"rules", fmt.Sprint(index), "when"}, func(reference string, location []string) bool {
			return unresolved(diagnostics, reference, evidence, location, "JPS-SEMANTIC-UNRESOLVED-EVIDENCE", "Evidence requirement reference does not resolve.")
		}) {
			return diagnostics.sorted(), diagnostics.truncated
		}
	}

	if fallback, ok := root["fallbackOutcome"].(string); ok {
		if !unresolved(diagnostics, fallback, outcomes, []string{"fallbackOutcome"}, "JPS-SEMANTIC-UNRESOLVED-OUTCOME", "Outcome reference does not resolve.") {
			return diagnostics.sorted(), diagnostics.truncated
		}
	}
	if applicability, ok := root["applicability"]; ok {
		if !walkCondition(applicability, []string{"applicability"}, func(reference string, location []string) bool {
			return unresolved(diagnostics, reference, evidence, location, "JPS-SEMANTIC-UNRESOLVED-EVIDENCE", "Evidence requirement reference does not resolve.")
		}) {
			return diagnostics.sorted(), diagnostics.truncated
		}
	}

	for index, exception := range objectList(root["exceptions"]) {
		if target, ok := exception["targetRule"].(string); ok {
			if !unresolved(diagnostics, target, rules, []string{"exceptions", fmt.Sprint(index), "targetRule"}, "JPS-SEMANTIC-UNRESOLVED-RULE", "Rule reference does not resolve.") {
				return diagnostics.sorted(), diagnostics.truncated
			}
		}
		if outcome, ok := exception["outcome"].(string); ok {
			if !unresolved(diagnostics, outcome, outcomes, []string{"exceptions", fmt.Sprint(index), "outcome"}, "JPS-SEMANTIC-UNRESOLVED-OUTCOME", "Outcome reference does not resolve.") {
				return diagnostics.sorted(), diagnostics.truncated
			}
		}
		for refIndex, reference := range stringList(exception["sourceRefs"]) {
			if !unresolved(diagnostics, reference, sources, []string{"exceptions", fmt.Sprint(index), "sourceRefs", fmt.Sprint(refIndex)}, "JPS-SEMANTIC-UNRESOLVED-SOURCE", "Source reference does not resolve.") {
				return diagnostics.sorted(), diagnostics.truncated
			}
		}
		if !walkCondition(exception["when"], []string{"exceptions", fmt.Sprint(index), "when"}, func(reference string, location []string) bool {
			return unresolved(diagnostics, reference, evidence, location, "JPS-SEMANTIC-UNRESOLVED-EVIDENCE", "Evidence requirement reference does not resolve.")
		}) {
			return diagnostics.sorted(), diagnostics.truncated
		}
	}

	usedExtensions := coreExtensionNames(root)
	for index, required := range requiredExtensions(root) {
		if !usedExtensions[required] {
			if !diagnostics.add(result.ErrorDiagnostic(
				"JPS-SEMANTIC-MISSING-REQUIRED-EXTENSION",
				"semantic",
				carrier.Pointer([]string{"metadata", "requiredExtensions", fmt.Sprint(index)}),
				"Required extension has no value in a Core extension slot.",
			)) {
				break
			}
		}
	}
	return diagnostics.sorted(), diagnostics.truncated
}

func duplicateIDDiagnostics(root map[string]any, collection string, diagnostics *diagnosticCollector) bool {
	seen := map[string]bool{}
	for _, indexed := range indexedObjects(root[collection]) {
		id := stringValue(indexed.object["id"])
		if seen[id] {
			if !diagnostics.add(result.ErrorDiagnostic(
				"JPS-SEMANTIC-DUPLICATE-ID",
				"semantic",
				carrier.Pointer([]string{collection, fmt.Sprint(indexed.index), "id"}),
				"Identifier is duplicated within its collection.",
			)) {
				return false
			}
		}
		seen[id] = true
	}
	return true
}

func idSet(root map[string]any, collection string) map[string]bool {
	output := map[string]bool{}
	for _, item := range objectList(root[collection]) {
		output[stringValue(item["id"])] = true
	}
	return output
}

func unresolved(diagnostics *diagnosticCollector, reference string, targets map[string]bool, location []string, code, message string) bool {
	if !targets[reference] {
		return diagnostics.add(result.ErrorDiagnostic(code, "semantic", carrier.Pointer(location), message))
	}
	return true
}

func walkCondition(value any, location []string, visit func(string, []string) bool) bool {
	condition, ok := value.(map[string]any)
	if !ok {
		return true
	}
	switch stringValue(condition["op"]) {
	case "evidence-present":
		return visit(stringValue(condition["evidenceRequirement"]), appendLocation(location, "evidenceRequirement"))
	case "all", "any":
		for index, child := range list(condition["conditions"]) {
			if !walkCondition(child, appendLocation(location, "conditions", fmt.Sprint(index)), visit) {
				return false
			}
		}
	case "not":
		return walkCondition(condition["condition"], appendLocation(location, "condition"), visit)
	}
	return true
}

func requiredExtensions(root map[string]any) []string {
	metadata, ok := root["metadata"].(map[string]any)
	if !ok {
		return []string{}
	}
	return stringList(metadata["requiredExtensions"])
}

func coreExtensionNames(root map[string]any) map[string]bool {
	names := map[string]bool{}
	collectExtensionSlot(root, names)
	for _, singleton := range []string{"decision", "escalation", "metadata"} {
		if object, ok := root[singleton].(map[string]any); ok {
			collectExtensionSlot(object, names)
		}
	}
	for _, collection := range []string{"evidenceRequirements", "sources", "outcomes", "rules", "exceptions"} {
		for _, object := range objectList(root[collection]) {
			collectExtensionSlot(object, names)
		}
	}
	return names
}

func extensionNameDiagnostics(root map[string]any, diagnostics *diagnosticCollector) {
	if !collectExtensionSlotDiagnostics(root, nil, diagnostics) {
		return
	}
	for _, singleton := range []string{"decision", "escalation", "metadata"} {
		if object, ok := root[singleton].(map[string]any); ok {
			if !collectExtensionSlotDiagnostics(object, []string{singleton}, diagnostics) {
				return
			}
		}
	}
	for _, collection := range []string{"evidenceRequirements", "sources", "outcomes", "rules", "exceptions"} {
		for _, indexed := range indexedObjects(root[collection]) {
			if !collectExtensionSlotDiagnostics(indexed.object, []string{collection, fmt.Sprint(indexed.index)}, diagnostics) {
				return
			}
		}
	}
	if metadata, ok := root["metadata"].(map[string]any); ok {
		for index, value := range list(metadata["requiredExtensions"]) {
			name, ok := value.(string)
			if ok && !validExtensionName(name) {
				if !diagnostics.add(result.ErrorDiagnostic(
					"JPS-STRUCTURE-EXTENSION-NAME",
					"structural",
					carrier.Pointer([]string{"metadata", "requiredExtensions", fmt.Sprint(index)}),
					"Extension name must use an allowed reverse-domain namespace.",
				)) {
					return
				}
			}
		}
	}
}

func collectExtensionSlotDiagnostics(object map[string]any, location []string, diagnostics *diagnosticCollector) bool {
	extensions, ok := object["extensions"].(map[string]any)
	if !ok {
		return true
	}
	names := make([]string, 0, len(extensions))
	for name := range extensions {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !validExtensionName(name) {
			if !diagnostics.add(result.ErrorDiagnostic(
				"JPS-STRUCTURE-EXTENSION-NAME",
				"structural",
				carrier.Pointer(appendLocation(location, "extensions", name)),
				"Extension name must use an allowed reverse-domain namespace.",
			)) {
				return false
			}
		}
	}
	return true
}

func validExtensionName(name string) bool {
	return extensionNamePattern.MatchString(name) && !strings.HasPrefix(name, "org.judgmentpack.")
}

func collectExtensionSlot(object map[string]any, names map[string]bool) {
	extensions, ok := object["extensions"].(map[string]any)
	if !ok {
		return
	}
	for name := range extensions {
		names[name] = true
	}
}

type indexedObject struct {
	index  int
	object map[string]any
}

func indexedObjects(value any) []indexedObject {
	values := list(value)
	output := make([]indexedObject, 0, len(values))
	for index, item := range values {
		if object, ok := item.(map[string]any); ok {
			output = append(output, indexedObject{index: index, object: object})
		}
	}
	return output
}

func objectList(value any) []map[string]any {
	values := list(value)
	output := make([]map[string]any, 0, len(values))
	for _, item := range values {
		if object, ok := item.(map[string]any); ok {
			output = append(output, object)
		}
	}
	return output
}

func stringList(value any) []string {
	values := list(value)
	output := make([]string, 0, len(values))
	for _, item := range values {
		if text, ok := item.(string); ok {
			output = append(output, text)
		}
	}
	return output
}

func list(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	return []any{}
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func appendLocation(location []string, parts ...string) []string {
	output := append([]string(nil), location...)
	return append(output, parts...)
}
