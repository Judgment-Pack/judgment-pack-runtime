package main

import (
	"slices"
	"strings"
)

// Reading a kind's schema (registry companion §3.2) into the positions the
// generator writes Go for, and marking the kind's secret positions on them
// (companion §3.5, §6.3). Every shape the emitter does not generate is
// findSchema, never approximated (ADR-0102):
//   - a keyword outside companion §3.2's subset, or $defs below the root;
//   - anyOf, allOf and not;
//   - a type array, and the type null;
//   - $ref beside a keyword that shapes the type (type, enum, const,
//     properties, required, additionalProperties or items), a $ref of
//     another form than #/$defs/<name>, to a name $defs does not define, or
//     in a cycle;
//   - an open object: an object whose additionalProperties is absent, true
//     or a schema without a type, or whose properties sit beside an
//     additionalProperties schema;
//   - a required member that an object closed with additionalProperties
//     false does not declare;
//   - properties, additionalProperties, items or required without a type,
//     required at a map, and a type of the empty string;
//   - enum or const at an integer, number, boolean, object or array
//     position, the root's included;
//   - enum or const at a string position, or with no type, whose values are
//     not all strings, and enum beside const there;
//   - a tuple items (an array of subschemas), and false as a subschema other
//     than additionalProperties false;
//   - a root that is not an object closed with additionalProperties false;
//   - more than maxPositions positions, $ref expanded.

// shape is what a position holds, and so its Go type.
type shape int

const (
	shapeStruct  shape = iota // an object closed with additionalProperties false
	shapeMap                  // an object whose members all have one schema
	shapeArray                // an array whose items have one schema
	shapeString               // a string
	shapeEnum                 // a string of a closed list of values
	shapeInteger              // a number of integer value
	shapeNumber               // a number
	shapeBoolean              // a boolean
	shapeRaw                  // any JSON value: the subschema true, or one of no shape keyword
	shapeSecret               // a string at a secret position: a secret reference
)

// token is one reference token of a position: a member name, or the element
// of an array or map.
type token struct {
	wild bool
	name string
}

// position is one position of a kind's value.
type position struct {
	shape  shape
	tokens []token
	// members are a struct's members, sorted by name in code-point order.
	members []*member
	// element is an array's items, or a map's members.
	element *position
	// values are an enumeration's values, in the schema's order, each once.
	values []string

	// typeName, set by the namer, is a struct's or an enumeration's type.
	typeName string
}

// member is one member of a struct.
type member struct {
	name     string
	required bool
	at       *position
	field    string // set by the namer
}

// pointer is the position as an RFC 6901 pointer into the value, with * for
// the element of an array or map, for the generated comments.
func (p *position) pointer() string {
	var b strings.Builder
	for _, t := range p.tokens {
		b.WriteByte('/')
		if t.wild {
			b.WriteByte('*')
			continue
		}
		b.WriteString(escapeToken(t.name))
	}
	return b.String()
}

// escapeToken writes a member name as an RFC 6901 token.
func escapeToken(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, "~", "~0"), "/", "~1")
}

// Keywords of companion §3.2, by what the emitter does with them.
var (
	// shapeKeywords decide a position's Go type.
	shapeKeywords = map[string]bool{
		"type": true, "enum": true, "const": true, "properties": true, "required": true,
		"additionalProperties": true, "items": true, "$ref": true, "anyOf": true, "allOf": true,
		"not": true, "$defs": true,
	}
	// checkKeywords only narrow the values of a type, and annotations
	// change nothing: the generated type is the same with or without them,
	// so the decoder never applies them, default included (companion §3.2).
	checkKeywords = map[string]bool{
		"minProperties": true, "maxProperties": true, "propertyNames": true, "minItems": true,
		"maxItems": true, "uniqueItems": true, "minLength": true, "maxLength": true, "pattern": true,
		"minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true,
		"title": true, "description": true, "default": true, "deprecated": true, "examples": true,
	}
)

// maxEnumValues is companion §3.2's bound on an enum.
const maxEnumValues = 1024

// builder reads one kind's schema.
type builder struct {
	defs map[string]any
	// active are the definitions being expanded, to refuse a cycle.
	active map[string]bool
	count  int
}

// buildKind reads the kind's schema into its root position and marks its
// secret positions.
func buildKind(k *kind) (*position, int, error) {
	b := &builder{active: map[string]bool{}}
	schema, ok := k.schema.(map[string]any)
	if !ok {
		return nil, 0, findSchema
	}
	if raw, present := schema["$defs"]; present {
		defs, ok := raw.(map[string]any)
		if !ok {
			return nil, 0, findSchema
		}
		b.defs = defs
	}
	root, err := b.build(schema, nil, true)
	if err != nil {
		return nil, 0, err
	}
	if root.shape != shapeStruct {
		return nil, 0, findSchema
	}
	for _, pattern := range k.secrets {
		matched, err := markSecret(root, pattern)
		if err != nil {
			return nil, 0, err
		}
		if !matched {
			return nil, 0, findSecret
		}
	}
	return root, b.count, nil
}

func child(tokens []token, t token) []token {
	return append(slices.Clone(tokens), t)
}

// build reads one subschema at the position tokens name.
func (b *builder) build(schema any, tokens []token, root bool) (*position, error) {
	b.count++
	if b.count > maxPositions {
		return nil, findSchema
	}
	switch s := schema.(type) {
	case bool:
		if s {
			return &position{shape: shapeRaw, tokens: tokens}, nil
		}
	case map[string]any:
		return b.object(s, tokens, root)
	}
	return nil, findSchema
}

func (b *builder) object(s map[string]any, tokens []token, root bool) (*position, error) {
	for name := range s {
		if !shapeKeywords[name] && !checkKeywords[name] {
			return nil, findSchema
		}
	}
	for _, name := range []string{"anyOf", "allOf", "not"} {
		if _, present := s[name]; present {
			return nil, findSchema
		}
	}
	if _, present := s["$defs"]; present && !root {
		return nil, findSchema
	}
	if ref, present := s["$ref"]; present {
		for name := range s {
			if shapeKeywords[name] && name != "$ref" && name != "$defs" {
				return nil, findSchema
			}
		}
		return b.ref(ref, tokens)
	}
	typeName := ""
	if raw, present := s["type"]; present {
		name, ok := raw.(string)
		if !ok || name == "" {
			return nil, findSchema
		}
		typeName = name
	}
	switch typeName {
	case "integer", "number", "boolean", "object", "array":
		// Only a string's enum or const is generated and checked, so one at
		// any other position, the root's included, is refused rather than
		// dropped.
		_, hasEnum := s["enum"]
		_, hasConst := s["const"]
		if hasEnum || hasConst {
			return nil, findSchema
		}
	}
	switch typeName {
	case "object":
		return b.objectType(s, tokens)
	case "array":
		return b.arrayType(s, tokens)
	case "integer":
		return &position{shape: shapeInteger, tokens: tokens}, nil
	case "number":
		return &position{shape: shapeNumber, tokens: tokens}, nil
	case "boolean":
		return &position{shape: shapeBoolean, tokens: tokens}, nil
	case "string", "":
		values, isEnum, err := stringValues(s)
		if err != nil {
			return nil, err
		}
		if isEnum {
			return &position{shape: shapeEnum, tokens: tokens, values: values}, nil
		}
		if typeName == "string" {
			return &position{shape: shapeString, tokens: tokens}, nil
		}
		for _, name := range []string{"properties", "additionalProperties", "items", "required"} {
			if _, present := s[name]; present {
				return nil, findSchema
			}
		}
		return &position{shape: shapeRaw, tokens: tokens}, nil
	}
	return nil, findSchema
}

// stringValues reads enum or const at a string position, or one with no
// type: a closed list of strings, each kept once, in the schema's order.
func stringValues(s map[string]any) ([]string, bool, error) {
	enum, hasEnum := s["enum"]
	constant, hasConst := s["const"]
	var list []any
	switch {
	case hasEnum && hasConst:
		return nil, false, findSchema
	case hasEnum:
		items, ok := enum.([]any)
		if !ok || len(items) == 0 || len(items) > maxEnumValues {
			return nil, false, findSchema
		}
		list = items
	case hasConst:
		list = []any{constant}
	default:
		return nil, false, nil
	}
	values := make([]string, 0, len(list))
	for _, item := range list {
		value, ok := item.(string)
		if !ok {
			return nil, false, findSchema
		}
		if !slices.Contains(values, value) {
			values = append(values, value)
		}
	}
	return values, true, nil
}

// ref expands a $ref to its definition, at the same position. The
// definition is read again at every $ref, so each use has its own type.
func (b *builder) ref(raw any, tokens []token) (*position, error) {
	source, ok := raw.(string)
	if !ok {
		return nil, findSchema
	}
	escaped, ok := strings.CutPrefix(source, "#/$defs/")
	if !ok || escaped == "" || strings.Contains(escaped, "/") {
		return nil, findSchema
	}
	name, ok := unescapeRef(escaped)
	if !ok {
		return nil, findSchema
	}
	definition, defined := b.defs[name]
	if !defined || b.active[name] {
		return nil, findSchema
	}
	b.active[name] = true
	defer delete(b.active, name)
	return b.build(definition, tokens, false)
}

// unescapeRef decodes a $ref's name: ~0 and ~1 as RFC 6901 decodes them, and
// no other ~ escape (companion §3.3).
func unescapeRef(escaped string) (string, bool) {
	var name strings.Builder
	for i := 0; i < len(escaped); i++ {
		if escaped[i] != '~' {
			name.WriteByte(escaped[i])
			continue
		}
		if i+1 == len(escaped) || escaped[i+1] != '0' && escaped[i+1] != '1' {
			return "", false
		}
		if escaped[i+1] == '0' {
			name.WriteByte('~')
		} else {
			name.WriteByte('/')
		}
		i++
	}
	return name.String(), true
}

// objectType reads an object position: a struct when additionalProperties
// is false, and a map when it is a schema of a type and there are no
// properties. Anything else is an open object, refused.
func (b *builder) objectType(s map[string]any, tokens []token) (*position, error) {
	var properties map[string]any
	if raw, present := s["properties"]; present {
		object, ok := raw.(map[string]any)
		if !ok {
			return nil, findSchema
		}
		properties = object
	}
	var required []string
	requiredSet := map[string]bool{}
	if raw, present := s["required"]; present {
		items, ok := raw.([]any)
		if !ok {
			return nil, findSchema
		}
		for _, item := range items {
			name, ok := item.(string)
			if !ok || requiredSet[name] {
				return nil, findSchema
			}
			requiredSet[name] = true
			required = append(required, name)
		}
	}
	additional, hasAdditional := s["additionalProperties"]
	if !hasAdditional || additional == true {
		return nil, findSchema
	}
	if additional == false {
		p := &position{shape: shapeStruct, tokens: tokens}
		names := make([]string, 0, len(properties))
		for name := range properties {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			at, err := b.build(properties[name], child(tokens, token{name: name}), false)
			if err != nil {
				return nil, err
			}
			p.members = append(p.members, &member{name: name, required: requiredSet[name], at: at})
		}
		for _, name := range required {
			if _, declared := properties[name]; !declared {
				return nil, findSchema
			}
		}
		return p, nil
	}
	if len(properties) > 0 || len(required) > 0 {
		// A map's decoder does not check required, so a map with it is
		// refused rather than rendered without it.
		return nil, findSchema
	}
	element, err := b.build(additional, child(tokens, token{wild: true}), false)
	if err != nil {
		return nil, err
	}
	if element.shape == shapeRaw {
		// additionalProperties {} is additionalProperties true.
		return nil, findSchema
	}
	return &position{shape: shapeMap, tokens: tokens, element: element}, nil
}

// arrayType reads an array position. Without items, or with items true, its
// elements are any JSON value.
func (b *builder) arrayType(s map[string]any, tokens []token) (*position, error) {
	items, present := s["items"]
	if !present {
		items = true
	}
	if _, tuple := items.([]any); tuple {
		return nil, findSchema
	}
	element, err := b.build(items, child(tokens, token{wild: true}), false)
	if err != nil {
		return nil, err
	}
	return &position{shape: shapeArray, tokens: tokens, element: element}, nil
}

// markSecret marks the positions a secret pattern matches, and reports
// whether it matched one. A matched position must be a string, which becomes
// a secret; anything else is findSecret, and so is a pattern that reaches a
// position of any JSON value, or names one member of a map or one element of
// an array: their elements share one type, so it cannot be a secret for some
// and not for others. A token below a string, number or boolean matches
// nothing there.
func markSecret(p *position, tokens []patternToken) (bool, error) {
	if len(tokens) == 0 {
		switch p.shape {
		case shapeString, shapeSecret:
			p.shape = shapeSecret
			return true, nil
		}
		// An enumeration too: a secret is not checked against a list, so its
		// list would be dropped.
		return false, findSecret
	}
	t := tokens[0]
	switch p.shape {
	case shapeStruct:
		matched := false
		for _, m := range p.members {
			if !t.wild && m.name != t.literal {
				continue
			}
			ok, err := markSecret(m.at, tokens[1:])
			if err != nil {
				return false, err
			}
			matched = matched || ok
		}
		return matched, nil
	case shapeMap, shapeArray:
		if !t.wild {
			return false, findSecret
		}
		return markSecret(p.element, tokens[1:])
	case shapeRaw:
		return false, findSecret
	}
	return false, nil
}
