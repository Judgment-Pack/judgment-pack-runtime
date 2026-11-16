package registry

import (
	"encoding/json"
	"math/big"
	"slices"
	"strconv"
	"unicode/utf8"
)

// valueFinding is one schema finding at a path in the validated value.
type valueFinding struct {
	code string
	path []string
}

// validate checks value against node and appends findings. Every keyword is
// checked independently, as JSON Schema does, and a keyword applies only to
// values of its type. anyOf reports one finding at its own path when no branch
// holds, and none of the branches' findings, so two readers agree whatever
// order they try branches in. Warnings inside a failed anyOf branch are
// dropped with it.
func (node *schemaNode) validate(value any, path []string, out *[]valueFinding) {
	if node == nil {
		return
	}
	if node.boolean != nil {
		if !*node.boolean {
			*out = append(*out, valueFinding{"REG-VALUE-TYPE", path})
		}
		return
	}
	if node.deprecated {
		*out = append(*out, valueFinding{"REG-VALUE-DEPRECATED", path})
	}
	if node.types != nil && !slices.ContainsFunc(node.types, func(name string) bool { return hasType(value, name) }) {
		*out = append(*out, valueFinding{"REG-VALUE-TYPE", path})
	}
	if node.hasEnum && !slices.ContainsFunc(node.enum, func(item any) bool { return jsonEqual(item, value) }) {
		*out = append(*out, valueFinding{"REG-VALUE-ENUM", path})
	}
	if node.hasConst && !jsonEqual(node.constValue, value) {
		*out = append(*out, valueFinding{"REG-VALUE-CONST", path})
	}
	switch typed := value.(type) {
	case map[string]any:
		node.validateObject(typed, path, out)
	case []any:
		if node.minItems >= 0 && len(typed) < node.minItems || node.maxItems >= 0 && len(typed) > node.maxItems {
			*out = append(*out, valueFinding{"REG-VALUE-ITEM-COUNT", path})
		}
		if node.unique {
			for i := range typed {
				if slices.ContainsFunc(typed[:i], func(earlier any) bool { return jsonEqual(earlier, typed[i]) }) {
					*out = append(*out, valueFinding{"REG-VALUE-UNIQUE", path})
					break
				}
			}
		}
		if node.items != nil {
			for i, item := range typed {
				node.items.validate(item, appendPath(path, strconv.Itoa(i)), out)
			}
		}
	case string:
		length := utf8.RuneCountInString(typed)
		if node.minLength >= 0 && length < node.minLength || node.maxLength >= 0 && length > node.maxLength {
			*out = append(*out, valueFinding{"REG-VALUE-LENGTH", path})
		}
		if node.pattern != nil && !node.pattern.MatchString(typed) {
			*out = append(*out, valueFinding{"REG-VALUE-PATTERN", path})
		}
	case json.Number:
		if number, ok := new(big.Rat).SetString(typed.String()); ok {
			if node.minimum != nil && number.Cmp(node.minimum) < 0 ||
				node.maximum != nil && number.Cmp(node.maximum) > 0 ||
				node.exMinimum != nil && number.Cmp(node.exMinimum) <= 0 ||
				node.exMaximum != nil && number.Cmp(node.exMaximum) >= 0 {
				*out = append(*out, valueFinding{"REG-VALUE-RANGE", path})
			}
		}
	}
	for _, branch := range node.allOf {
		branch.validate(value, path, out)
	}
	if node.anyOf != nil {
		held := false
		var kept []valueFinding
		for _, branch := range node.anyOf {
			var trial []valueFinding
			branch.validate(value, path, &trial)
			if !hasError(trial) {
				held = true
				kept = trial
				break
			}
		}
		if held {
			*out = append(*out, kept...)
		} else {
			*out = append(*out, valueFinding{"REG-VALUE-ANY-OF", path})
		}
	}
	if node.not != nil {
		var trial []valueFinding
		node.not.validate(value, path, &trial)
		if !hasError(trial) {
			*out = append(*out, valueFinding{"REG-VALUE-NOT", path})
		}
	}
	if node.refNode != nil {
		node.refNode.validate(value, path, out)
	}
}

func (node *schemaNode) validateObject(object map[string]any, path []string, out *[]valueFinding) {
	if node.minProps >= 0 && len(object) < node.minProps || node.maxProps >= 0 && len(object) > node.maxProps {
		*out = append(*out, valueFinding{"REG-VALUE-PROPERTY-COUNT", path})
	}
	for _, name := range node.required {
		if _, ok := object[name]; !ok {
			*out = append(*out, valueFinding{"REG-VALUE-REQUIRED", appendPath(path, name)})
		}
	}
	for _, name := range sortedKeys(object) {
		member := appendPath(path, name)
		if node.names != nil {
			if node.names.maxLength >= 0 && utf8.RuneCountInString(name) > node.names.maxLength ||
				node.names.pattern != nil && !node.names.pattern.MatchString(name) {
				*out = append(*out, valueFinding{"REG-VALUE-PROPERTY-NAME", member})
			}
		}
		if child, ok := node.properties[name]; ok {
			child.validate(object[name], member, out)
			continue
		}
		if node.additional == nil {
			continue
		}
		if node.additional.boolean != nil && !*node.additional.boolean {
			*out = append(*out, valueFinding{"REG-VALUE-ADDITIONAL", member})
			continue
		}
		node.additional.validate(object[name], member, out)
	}
}

// warnings that never make a value invalid.
func hasError(findings []valueFinding) bool {
	for _, finding := range findings {
		if Codes[finding.code].severity == SeverityError {
			return true
		}
	}
	return false
}

func hasType(value any, name string) bool {
	switch name {
	case "null":
		return value == nil
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(json.Number)
		return ok
	case "integer":
		rat, ok := ratOf(value)
		return ok && rat.IsInt()
	}
	return false
}

// jsonEqual is JSON Schema's equality: numbers by value, objects regardless
// of member order, arrays item by item.
func jsonEqual(a, b any) bool {
	switch left := a.(type) {
	case nil:
		return b == nil
	case bool:
		right, ok := b.(bool)
		return ok && left == right
	case string:
		right, ok := b.(string)
		return ok && left == right
	case json.Number:
		l, okLeft := ratOf(left)
		r, okRight := ratOf(b)
		return okLeft && okRight && l.Cmp(r) == 0
	case []any:
		right, ok := b.([]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for i := range left {
			if !jsonEqual(left[i], right[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		right, ok := b.(map[string]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for name, item := range left {
			other, ok := right[name]
			if !ok || !jsonEqual(item, other) {
				return false
			}
		}
		return true
	}
	return false
}

// reachable reports whether some value the schema allows has a position at
// tokens. It reads properties, additionalProperties, items and type, and
// follows $ref, allOf and anyOf permissively (a position reachable through
// any of them counts), so a pattern is refused only when no reading of the
// schema can reach it.
func (node *schemaNode) reachable(tokens []patternToken) bool {
	if node == nil {
		return true
	}
	if node.boolean != nil {
		return *node.boolean
	}
	if len(tokens) == 0 {
		return true
	}
	if node.refNode != nil && node.refNode.reachable(tokens) {
		return true
	}
	for _, branch := range append(slices.Clone(node.allOf), node.anyOf...) {
		if branch.reachable(tokens) {
			return true
		}
	}
	token, rest := tokens[0], tokens[1:]
	allows := func(name string) bool {
		return node.types == nil || slices.Contains(node.types, name)
	}
	if allows("object") {
		if token.any {
			for _, name := range sortedKeys(node.properties) {
				if node.properties[name].reachable(rest) {
					return true
				}
			}
			if node.additional == nil || node.additional.reachable(rest) {
				return true
			}
		} else if child, ok := node.properties[token.literal]; ok {
			if child.reachable(rest) {
				return true
			}
		} else if node.additional == nil || node.additional.reachable(rest) {
			return true
		}
	}
	if allows("array") && (token.any || isIndexToken(token.literal)) {
		if node.items == nil || node.items.reachable(rest) {
			return true
		}
	}
	return false
}

func isIndexToken(token string) bool {
	_, ok := arrayIndex(token, 1<<30)
	return ok
}

// declares reports whether the schema, at the object position parent,
// declares a member called name in some properties keyword.
func (node *schemaNode) declares(parent []string, name string) bool {
	if node == nil || node.boolean != nil {
		return false
	}
	if node.refNode != nil && node.refNode.declares(parent, name) {
		return true
	}
	for _, branch := range append(slices.Clone(node.allOf), node.anyOf...) {
		if branch.declares(parent, name) {
			return true
		}
	}
	if len(parent) == 0 {
		_, ok := node.properties[name]
		return ok
	}
	token, rest := parent[0], parent[1:]
	if child, ok := node.properties[token]; ok && child.declares(rest, name) {
		return true
	}
	if node.additional != nil && node.additional.declares(rest, name) {
		return true
	}
	if isIndexToken(token) && node.items != nil && node.items.declares(rest, name) {
		return true
	}
	return false
}
