package view

import (
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evalform"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/canonjson"
)

// The view's canonical JSON is RFC 8785 in everything but numbers: object
// members ordered by the UTF-16 code units of their names, the RFC's minimal
// string escapes, no whitespace. A number is written as the literal token the
// carrier decoder kept, because a pack's numbers are compared and reviewed as
// written — 1.50 is not 1.5 to a reader of the view — and the RFC's number
// form would rewrite them. Null, which the RFC writes as null, is written so.
// internal/jcs implements the same rules over the disposition's value space
// and refuses numbers and null; a test holds this writer to it on that space.
// The writer is package canonjson's, which the decision record shares
// (ADR-0079), so the two formats cannot write one value two ways.

// rawJSON is JSON text already in canonical form, written verbatim.
type rawJSON = canonjson.Raw

// canonicalText returns the canonical JSON text of one decoded value.
func canonicalText(value any) (string, error) {
	return canonjson.Text(value)
}

// compareUTF16 orders two member names by their UTF-16 code units, as RFC
// 8785 orders them; that is not the UTF-8 byte order for a name holding a
// code point outside the Basic Multilingual Plane.
func compareUTF16(left, right string) int {
	return canonjson.CompareUTF16(left, right)
}

// encodeViewJSON writes the view's canonical JSON from its typed fields, so
// the bytes say exactly what the typed view says and nothing the root held
// that the view did not carry.
func encodeViewJSON(view PackView) ([]byte, error) {
	document, err := view.documentValue()
	if err != nil {
		return nil, err
	}
	encoded, err := canonicalText(map[string]any{
		"digest":      view.Identity.Digest,
		"pack":        document,
		"viewVersion": ViewVersion,
	})
	if err != nil {
		return nil, &fault{message: "The view does not encode: " + err.Error() + "."}
	}
	return []byte(encoded), nil
}

// documentValue rebuilds the document's JSON value from the typed view.
func (v PackView) documentValue() (map[string]any, error) {
	document := map[string]any{
		"specVersion": v.Identity.SpecVersion,
		"id":          v.Identity.ID,
		"version":     v.Identity.Version,
		"title":       v.Title,
		"decision":    withExtensions(map[string]any{"intent": v.Decision.Intent, "question": v.Decision.Question}, v.Decision.Extensions),
	}
	putString(document, "profile", v.Identity.Profile)
	putString(document, "description", v.Description)
	if v.Applicability != nil {
		applicability, err := conditionValue(*v.Applicability)
		if err != nil {
			return nil, err
		}
		document["applicability"] = applicability
	}
	if v.EvidenceRequirements != nil {
		items := make([]any, 0, len(v.EvidenceRequirements))
		for _, requirement := range v.EvidenceRequirements {
			item := map[string]any{"id": requirement.ID, "description": requirement.Description, "required": requirement.Required}
			putString(item, "kind", requirement.Kind)
			items = append(items, withExtensions(item, requirement.Extensions))
		}
		document["evidenceRequirements"] = items
	}
	if v.Sources != nil {
		items := make([]any, 0, len(v.Sources))
		for _, source := range v.Sources {
			item := map[string]any{
				"id":      source.ID,
				"title":   source.Title,
				"locator": map[string]any{"kind": source.Locator.Kind, "value": source.Locator.Value},
			}
			putString(item, "publisher", source.Publisher)
			putString(item, "publishedAt", source.PublishedAt)
			if source.Citation != nil {
				item["citation"] = map[string]any{"location": source.Citation.Location, "excerpt": source.Citation.Excerpt}
			}
			putString(item, "rights", source.Rights)
			items = append(items, withExtensions(item, source.Extensions))
		}
		document["sources"] = items
	}
	outcomes := make([]any, 0, len(v.Outcomes))
	for _, outcome := range v.Outcomes {
		outcomes = append(outcomes, outcomeValue(outcome))
	}
	document["outcomes"] = outcomes
	rules := make([]any, 0, len(v.Rules))
	for _, rule := range v.Rules {
		item, err := ruleValue(rule)
		if err != nil {
			return nil, err
		}
		rules = append(rules, item)
	}
	document["rules"] = rules
	if v.Exceptions != nil {
		items := make([]any, 0, len(v.Exceptions))
		for _, exception := range v.Exceptions {
			when, err := conditionValue(exception.When)
			if err != nil {
				return nil, err
			}
			item := map[string]any{
				"id":          exception.ID,
				"description": exception.Description,
				"when":        when,
				"effect":      exception.Effect,
				"onUnknown":   exception.OnUnknown,
			}
			putString(item, "targetRule", exception.TargetRule)
			putString(item, "outcome", exception.Outcome)
			putStrings(item, "sourceRefs", exception.SourceRefs)
			items = append(items, withExtensions(item, exception.Extensions))
		}
		document["exceptions"] = items
	}
	if v.ValueSets != nil {
		items := make([]any, 0, len(v.ValueSets))
		for _, set := range v.ValueSets {
			entries := make([]any, 0, len(set.Entries))
			for _, entry := range set.Entries {
				item := map[string]any{"value": rawJSON(entry.Value)}
				putString(item, "description", entry.Description)
				entries = append(entries, withAnnotatedExtensions(item, entry.Extensions, entry.Annotations))
			}
			item := map[string]any{"id": set.ID, "entries": entries}
			putString(item, "description", set.Description)
			items = append(items, item)
		}
		document["valueSets"] = items
	}
	putString(document, "fallbackOutcome", v.FallbackOutcome)
	if v.Escalation != nil {
		escalation := map[string]any{}
		putStrings(escalation, "triggers", v.Escalation.Triggers)
		if v.Escalation.Target != nil {
			escalation["target"] = targetValue(*v.Escalation.Target)
		}
		putString(escalation, "message", v.Escalation.Message)
		if v.Escalation.Routes != nil {
			routes := make([]any, 0, len(v.Escalation.Routes))
			for _, route := range v.Escalation.Routes {
				item := map[string]any{"target": targetValue(route.Target)}
				putStrings(item, "triggers", route.Triggers)
				routes = append(routes, item)
			}
			escalation["routes"] = routes
		}
		document["escalation"] = withExtensions(escalation, v.Escalation.Extensions)
	}
	if v.Metadata != nil {
		metadata := map[string]any{}
		putStrings(metadata, "authors", v.Metadata.Authors)
		putString(metadata, "createdAt", v.Metadata.CreatedAt)
		putString(metadata, "license", v.Metadata.License)
		putStrings(metadata, "requiredExtensions", v.Metadata.RequiredExtensions)
		if v.Metadata.Reviews != nil {
			reviews := make([]any, 0, len(v.Metadata.Reviews))
			for _, review := range v.Metadata.Reviews {
				item := map[string]any{"reviewer": review.Reviewer, "reviewedAt": review.ReviewedAt, "disposition": review.Disposition}
				putString(item, "note", review.Note)
				reviews = append(reviews, item)
			}
			metadata["reviews"] = reviews
		}
		document["metadata"] = withExtensions(metadata, v.Metadata.Extensions)
	}
	return withAnnotatedExtensions(document, v.Extensions, v.Annotations), nil
}

// outcomeValue rebuilds one member of outcomes from its typed form.
func outcomeValue(outcome Outcome) map[string]any {
	item := map[string]any{"id": outcome.ID, "label": outcome.Label}
	putString(item, "description", outcome.Description)
	return withAnnotatedExtensions(item, outcome.Extensions, outcome.Annotations)
}

// ruleValue rebuilds one member of rules from its typed form.
func ruleValue(rule Rule) (map[string]any, error) {
	when, err := conditionValue(rule.When)
	if err != nil {
		return nil, err
	}
	item := map[string]any{
		"id":          rule.ID,
		"description": rule.Description,
		"when":        when,
		"outcome":     rule.Outcome,
		"onUnknown":   rule.OnUnknown,
	}
	putStrings(item, "evidenceRequirementRefs", rule.EvidenceRequirementRefs)
	putStrings(item, "sourceRefs", rule.SourceRefs)
	putString(item, "rationale", rule.Rationale)
	return withAnnotatedExtensions(item, rule.Extensions, rule.Annotations), nil
}

// conditionValue rebuilds one condition node from its typed form.
func conditionValue(condition Condition) (map[string]any, error) {
	node := map[string]any{"op": condition.Op}
	switch condition.Op {
	case "literal":
		node["value"] = rawJSON(condition.Value)
	case "all", "any":
		items := make([]any, 0, len(condition.Conditions))
		for _, child := range condition.Conditions {
			item, err := conditionValue(child)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		node["conditions"] = items
	case "not":
		if condition.Condition == nil {
			return nil, &fault{message: "A not condition of the view holds no condition."}
		}
		inner, err := conditionValue(*condition.Condition)
		if err != nil {
			return nil, err
		}
		node["condition"] = inner
	case "fact":
		node["path"] = condition.Path
		node["operator"] = condition.Operator
		// A value is written unless the condition references a value set and
		// has none, so every condition of a released version is unchanged.
		if condition.Value != "" || condition.ValueSet == "" {
			node["value"] = rawJSON(condition.Value)
		}
		if condition.ValueSet != "" {
			node["valueSet"] = condition.ValueSet
		}
	case "evidence-present":
		node["evidenceRequirement"] = condition.EvidenceRequirement
	default:
		return nil, &fault{message: "A condition of the view has an op it does not describe."}
	}
	return node, nil
}

func targetValue(target Target) map[string]any {
	return map[string]any{"kind": target.Kind, "name": target.Name}
}

// putString sets an optional string member, absent when the view's string is
// empty.
func putString(object map[string]any, name, value string) {
	if value != "" {
		object[name] = value
	}
}

// putStrings sets an optional array of strings, absent exactly when the
// view's slice is nil.
func putStrings(object map[string]any, name string, values []string) {
	if values == nil {
		return
	}
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}
	object[name] = items
}

// withExtensions sets the extensions member, absent exactly when the view's
// slice is nil.
func withExtensions(object map[string]any, extensions []Extension) map[string]any {
	if extensions == nil {
		return object
	}
	members := make(map[string]any, len(extensions))
	for _, extension := range extensions {
		members[extension.Name] = rawJSON(extension.Value)
	}
	object["extensions"] = members
	return object
}

// withAnnotatedExtensions sets the extensions member with its annotations
// member written from the typed annotations: absent exactly when both are
// nil, and holding the annotations member exactly when they are not.
func withAnnotatedExtensions(object map[string]any, extensions []Extension, annotations []Annotation) map[string]any {
	object = withExtensions(object, extensions)
	if annotations == nil {
		return object
	}
	members, _ := object["extensions"].(map[string]any)
	if members == nil {
		members = map[string]any{}
		object["extensions"] = members
	}
	typed := make(map[string]any, len(annotations))
	for _, annotation := range annotations {
		if annotation.List == nil {
			typed[annotation.Key] = annotation.Value
			continue
		}
		items := make([]any, 0, len(annotation.List))
		for _, item := range annotation.List {
			items = append(items, item)
		}
		typed[annotation.Key] = items
	}
	members[evalform.AnnotationsName] = typed
	return object
}
