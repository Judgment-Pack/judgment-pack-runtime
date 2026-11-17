package main

import (
	"slices"
	"strings"
)

// The schema subset (companion §3.2) as TypeScript types. A type is a sound
// reading of its schema: every value the schema admits has it, while a
// constraint TypeScript cannot state (a bound, a pattern, uniqueItems) is
// not stated.
//
//	type          a union of null, boolean, number (number and integer),
//	              string, readonly E[] and an object type, in that order
//	enum, const   a union of the values' literal types; the schema's other
//	              keywords are not read, since the values already bound it
//	properties    readonly members, optional unless required
//	additional-   a readonly index signature of the additional members'
//	Properties    type, the declared members' types (unless the first is
//	              JsonValue, which holds them) and, when one is optional,
//	              undefined; none when it is false
//	items         readonly E[]
//	anyOf, allOf  a union, an intersection
//	$ref          the definition's type, written where it is used
//	not           refused, in this version
//	true, or no   JsonValue
//	keyword that
//	narrows it
//
// A $ref is written out where it is used, never as a named alias: a
// definition can sit at a secret position in one place and not in another,
// and what is left out follows the position, not the definition.
//
// Secret positions. Each position carries the kind's secrets patterns that
// can still match below it. A member at which one ends is left out of its
// object type, so neither its name nor its type is written. A pattern that
// ends at an array item, or that may (below a position whose schema does not
// say it is an object), is refused: an item cannot be left out without
// changing the array. A pattern that can match a position whose type holds a
// literal is refused as well, as the literal could be the secret: one below
// an enum or a const with an object or array value, whose literal type would
// hold the member, and one that ends at a member the properties do not
// declare, when the index signature's type holds a literal.

// The bounds a schema is read within. A kind the registry admits is within
// both (companion §3.3): its expanded size is at most 4096 subschemas, and a
// composition has at most 16 branches.
const (
	maxSchemaVisits = 4096
	maxBranches     = 16
)

// typeOp is a type node's form.
type typeOp int

const (
	opKeyword typeOp = iota
	opLiteral
	opArray
	opObject
	opUnion
	opIntersection
)

// typeNode is one TypeScript type.
type typeNode struct {
	op      typeOp
	keyword string // opKeyword: null, boolean, number, string, never, undefined or JsonValue
	value   any    // opLiteral: a JSON value
	elem    *typeNode
	members []typeMember // opObject, in RFC 8785 order of their names
	index   *typeNode    // opObject: the index signature's type, or nil
	parts   []*typeNode  // opUnion, opIntersection
}

type typeMember struct {
	name     string
	optional bool
	t        *typeNode
}

var (
	typeNull      = &typeNode{op: opKeyword, keyword: "null"}
	typeBoolean   = &typeNode{op: opKeyword, keyword: "boolean"}
	typeNumber    = &typeNode{op: opKeyword, keyword: "number"}
	typeString    = &typeNode{op: opKeyword, keyword: "string"}
	typeNever     = &typeNode{op: opKeyword, keyword: "never"}
	typeUndefined = &typeNode{op: opKeyword, keyword: "undefined"}
	typeJSON      = &typeNode{op: opKeyword, keyword: "JsonValue"}
)

// keywords are the keywords of the subset this emitter reads, other than
// $defs (the root's only) and not (refused).
var keywords = map[string]bool{
	"type": true, "enum": true, "const": true,
	"properties": true, "required": true, "additionalProperties": true, "minProperties": true,
	"maxProperties": true, "propertyNames": true,
	"items": true, "minItems": true, "maxItems": true, "uniqueItems": true,
	"minLength": true, "maxLength": true, "pattern": true,
	"minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true,
	"anyOf": true, "allOf": true, "$ref": true,
	"title": true, "description": true, "default": true, "deprecated": true, "examples": true,
}

var (
	objectKeywords = []string{"properties", "required", "additionalProperties", "minProperties", "maxProperties", "propertyNames"}
	arrayKeywords  = []string{"items", "minItems", "maxItems", "uniqueItems"}
)

// builder builds one kind's type. On a refusal it records the code and
// returns nil from then on.
type builder struct {
	defs   map[string]any
	visits int
	active map[string]bool
	code   string
}

// kindType returns a kind's type, or "" and the code that refuses its
// schema.
func kindType(k *kindDecl) (*typeNode, string) {
	b := &builder{defs: k.defs, active: map[string]bool{}}
	t := b.schema(k.schema, k.secrets, true)
	if b.code != "" {
		return nil, b.code
	}
	return t, ""
}

func (b *builder) refuse(code string) *typeNode {
	if b.code == "" {
		b.code = code
	}
	return nil
}

// schema returns the type of a subschema at a position below which ps may
// still match. No pattern of ps has ended here: the caller leaves out a
// secret position, or refuses it.
func (b *builder) schema(s any, ps patterns, root bool) *typeNode {
	if b.code != "" {
		return nil
	}
	if b.visits++; b.visits > maxSchemaVisits {
		return b.refuse(codeSchemaUnsupported)
	}
	if allowed, ok := s.(bool); ok {
		if !allowed {
			return typeNever
		}
		return b.json(ps)
	}
	v, ok := s.(map[string]any)
	if !ok {
		return b.refuse(codeSchemaUnsupported)
	}
	for name := range v {
		if !keywords[name] && !(root && name == "$defs") {
			return b.refuse(codeSchemaUnsupported)
		}
	}
	if c, present := v["const"]; present {
		return b.literals([]any{c}, ps)
	}
	if raw, present := v["enum"]; present {
		values, ok := raw.([]any)
		if !ok || len(values) == 0 {
			return b.refuse(codeSchemaUnsupported)
		}
		return b.literals(values, ps)
	}
	var parts []*typeNode
	if base := b.base(v, ps); base != nil {
		parts = append(parts, base)
	}
	if raw, present := v["$ref"]; present {
		parts = append(parts, b.ref(raw, ps))
	}
	if raw, present := v["anyOf"]; present {
		branches := b.branches(raw, ps)
		if len(branches) > 0 {
			parts = append(parts, union(branches))
		}
	}
	if raw, present := v["allOf"]; present {
		parts = append(parts, b.branches(raw, ps)...)
	}
	if b.code != "" {
		return nil
	}
	// JsonValue adds nothing to an intersection: every type here is a JSON
	// value's.
	narrowed := parts[:0]
	for _, part := range parts {
		if part != typeJSON {
			narrowed = append(narrowed, part)
		}
	}
	switch len(narrowed) {
	case 0:
		return b.json(ps)
	case 1:
		return narrowed[0]
	}
	return &typeNode{op: opIntersection, parts: narrowed}
}

// json is the type of a position the schema does not narrow. A pattern below
// it whose last token is "*" or an array index may end at an array item, and
// is refused.
func (b *builder) json(ps patterns) *typeNode {
	if ps.lastMayBeItem() {
		return b.refuse(codeSecretInArray)
	}
	return typeJSON
}

// literals is the union of the values' literal types. A pattern below a
// value that is an object or an array would end inside it, and is refused; a
// member named __proto__ is refused too.
func (b *builder) literals(values []any, ps patterns) *typeNode {
	parts := make([]*typeNode, 0, len(values))
	for _, value := range values {
		switch value.(type) {
		case map[string]any, []any:
			if len(ps) > 0 {
				return b.refuse(codeSecretLiteral)
			}
			if holdsProto(value) {
				return b.refuse(codeMemberProto)
			}
		}
		parts = append(parts, &typeNode{op: opLiteral, value: value})
	}
	return union(parts)
}

// prototypeMembers are the names a schema property cannot have: __proto__,
// which an object literal reads as its prototype, and the other members of
// Object.prototype. An entry without an optional member of one of those
// names reads it from Object.prototype, a function its type does not admit,
// and the file does not compile. A required one would, but the names are
// refused alike. A value's member or a literal's is written: the object holds
// it, so it is never read from the prototype.
var prototypeMembers = map[string]bool{
	"__proto__": true, "constructor": true, "toString": true, "toLocaleString": true, "valueOf": true,
	"hasOwnProperty": true, "isPrototypeOf": true, "propertyIsEnumerable": true,
	"__defineGetter__": true, "__defineSetter__": true, "__lookupGetter__": true, "__lookupSetter__": true,
}

// holdsProto says whether a value holds a member named __proto__ at any
// depth.
func holdsProto(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for name, member := range v {
			if name == "__proto__" || holdsProto(member) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if holdsProto(item) {
				return true
			}
		}
	}
	return false
}

// base is the type that type and the object and array keywords give, or nil
// when they do not narrow the value: no type, and no object or array
// keyword.
func (b *builder) base(v map[string]any, ps patterns) *typeNode {
	names := map[string]bool{}
	switch raw := v["type"].(type) {
	case nil:
		if _, present := v["type"]; present {
			return b.refuse(codeSchemaUnsupported)
		}
		if !hasAny(v, objectKeywords) && !hasAny(v, arrayKeywords) {
			return nil
		}
		for _, name := range []string{"null", "boolean", "number", "string", "array", "object"} {
			names[name] = true
		}
	case string:
		names[raw] = true
	case []any:
		if len(raw) == 0 {
			return b.refuse(codeSchemaUnsupported)
		}
		for _, item := range raw {
			name, ok := item.(string)
			if !ok {
				return b.refuse(codeSchemaUnsupported)
			}
			names[name] = true
		}
	default:
		return b.refuse(codeSchemaUnsupported)
	}
	for name := range names {
		switch name {
		case "null", "boolean", "number", "integer", "string", "array", "object":
		default:
			return b.refuse(codeSchemaUnsupported)
		}
	}
	var parts []*typeNode
	if names["null"] {
		parts = append(parts, typeNull)
	}
	if names["boolean"] {
		parts = append(parts, typeBoolean)
	}
	if names["number"] || names["integer"] {
		parts = append(parts, typeNumber)
	}
	if names["string"] {
		parts = append(parts, typeString)
	}
	if names["array"] {
		parts = append(parts, b.array(v, ps))
	}
	if names["object"] {
		parts = append(parts, b.object(v, ps))
	}
	if b.code != "" {
		return nil
	}
	return union(parts)
}

func hasAny(v map[string]any, names []string) bool {
	for _, name := range names {
		if _, present := v[name]; present {
			return true
		}
	}
	return false
}

// array is readonly E[], E the items' type, or JsonValue without items.
func (b *builder) array(v map[string]any, ps patterns) *typeNode {
	below := ps.items()
	if below.ends() {
		return b.refuse(codeSecretInArray)
	}
	items, present := v["items"]
	if !present {
		return &typeNode{op: opArray, elem: b.json(below)}
	}
	if _, ok := items.([]any); ok {
		return b.refuse(codeSchemaUnsupported)
	}
	return &typeNode{op: opArray, elem: b.schema(items, below, false)}
}

// object is the object type of properties, required and
// additionalProperties. A member at a secret position is left out, and a
// property named after a member of Object.prototype is refused. When a
// pattern "*" ends here every member is a secret, and the type has no index
// signature.
func (b *builder) object(v map[string]any, ps patterns) *typeNode {
	properties := map[string]any{}
	if raw, present := v["properties"]; present {
		m, ok := raw.(map[string]any)
		if !ok {
			return b.refuse(codeSchemaUnsupported)
		}
		properties = m
	}
	required := map[string]bool{}
	if raw, present := v["required"]; present {
		names, ok := raw.([]any)
		if !ok {
			return b.refuse(codeSchemaUnsupported)
		}
		for _, item := range names {
			name, ok := item.(string)
			if !ok {
				return b.refuse(codeSchemaUnsupported)
			}
			required[name] = true
		}
	}
	t := &typeNode{op: opObject}
	optional := false
	for _, name := range sortedNames(properties) {
		below := ps.member(name)
		if below.ends() {
			continue
		}
		if prototypeMembers[name] {
			return b.refuse(codeMemberProto)
		}
		member := typeMember{name: name, optional: !required[name], t: b.schema(properties[name], below, false)}
		optional = optional || member.optional
		t.members = append(t.members, member)
	}
	additional, present := v["additionalProperties"]
	if !present {
		additional = true
	}
	if closed, ok := additional.(bool); ok && !closed {
		return t
	}
	if _, ok := additional.(map[string]any); !ok {
		if _, ok := additional.(bool); !ok {
			return b.refuse(codeSchemaUnsupported)
		}
	}
	// The patterns below a member the properties do not declare: those whose
	// first token is "*", and those whose first token is a literal that is
	// not declared. One of the latter that ends here leaves out that one
	// member; the index signature still holds the others, and still
	// describes that one.
	var below patterns
	endsUndeclared := false
	for _, p := range ps {
		head := p[0]
		if head.wildcard {
			if len(p) == 1 {
				return t
			}
			below = append(below, p[1:])
		} else if _, declared := properties[head.literal]; !declared {
			if len(p) == 1 {
				endsUndeclared = true
			} else {
				below = append(below, p[1:])
			}
		}
	}
	index := b.schema(additional, below, false)
	if b.code != "" {
		return nil
	}
	// Every declared member's type must be one the index signature admits.
	// JsonValue admits each, since every type here is a JSON value's, and an
	// optional member's type is read with undefined, which JsonValue's object
	// members admit too. Another type is joined with the members' types,
	// each written once.
	parts := []*typeNode{index}
	if index != typeJSON {
		for _, member := range t.members {
			if !slices.Contains(parts, member.t) {
				parts = append(parts, member.t)
			}
		}
	}
	if optional {
		parts = append(parts, typeUndefined)
	}
	t.index = union(parts)
	// The value at a secret member that the properties do not declare is one
	// the index signature's type admits. A literal in that type, the
	// additional members' or a declared member's joined to it, could be the
	// secret itself, so the kind is refused rather than the type narrowed.
	if endsUndeclared && holdsLiteral(t.index, map[*typeNode]bool{}) {
		return b.refuse(codeSecretLiteral)
	}
	return t
}

// holdsLiteral says whether a type holds a literal type at any depth. A node
// can be shared, so seen holds those already walked and the walk is linear
// in the nodes built.
func holdsLiteral(t *typeNode, seen map[*typeNode]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	switch t.op {
	case opLiteral:
		return true
	case opArray:
		return holdsLiteral(t.elem, seen)
	case opObject:
		for _, member := range t.members {
			if holdsLiteral(member.t, seen) {
				return true
			}
		}
		return t.index != nil && holdsLiteral(t.index, seen)
	case opUnion, opIntersection:
		for _, part := range t.parts {
			if holdsLiteral(part, seen) {
				return true
			}
		}
	}
	return false
}

// ref is the type of the definition a $ref names, written where it is used.
// A $ref that is not #/$defs/<name> of a defined name, and a cycle, are
// refused.
func (b *builder) ref(raw any, ps patterns) *typeNode {
	text, ok := raw.(string)
	name, isRef := strings.CutPrefix(text, "#/$defs/")
	if !ok || !isRef || name == "" || strings.Contains(name, "/") {
		return b.refuse(codeSchemaUnsupported)
	}
	name, ok = decodeToken(name)
	definition, defined := b.defs[name]
	if !ok || !defined || b.active[name] {
		return b.refuse(codeSchemaUnsupported)
	}
	b.active[name] = true
	t := b.schema(definition, ps, false)
	delete(b.active, name)
	return t
}

// decodeToken decodes ~0 and ~1, refusing every other ~ escape.
func decodeToken(raw string) (string, bool) {
	var out strings.Builder
	for i := 0; i < len(raw); i++ {
		if raw[i] != '~' {
			out.WriteByte(raw[i])
			continue
		}
		if i+1 == len(raw) || raw[i+1] != '0' && raw[i+1] != '1' {
			return "", false
		}
		if raw[i+1] == '0' {
			out.WriteByte('~')
		} else {
			out.WriteByte('/')
		}
		i++
	}
	return out.String(), true
}

// branches returns the types of a composition's branches, each at the same
// position.
func (b *builder) branches(raw any, ps patterns) []*typeNode {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 || len(items) > maxBranches {
		b.refuse(codeSchemaUnsupported)
		return nil
	}
	out := make([]*typeNode, 0, len(items))
	for _, item := range items {
		out = append(out, b.schema(item, ps, false))
	}
	return out
}

// union is the union of parts; one part is itself.
func union(parts []*typeNode) *typeNode {
	if len(parts) == 1 {
		return parts[0]
	}
	return &typeNode{op: opUnion, parts: parts}
}
