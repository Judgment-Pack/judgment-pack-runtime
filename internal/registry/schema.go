package registry

import (
	"encoding/json"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

// Schema limits (RFC 0034 §2.1). MaxSubschemas bounds a schema twice: as
// written, and expanded, with each $ref counted as the definition it names
// (expandedSize, ADR-0093 amendment).
const (
	MaxSubschemas = 4096
	MaxEnumValues = 1024
	MaxBranches   = 16
)

var schemaKeywords = map[string]bool{
	"type": true, "enum": true, "const": true,
	"properties": true, "required": true, "additionalProperties": true, "minProperties": true, "maxProperties": true, "propertyNames": true,
	"items": true, "minItems": true, "maxItems": true, "uniqueItems": true,
	"minLength": true, "maxLength": true, "pattern": true,
	"minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true,
	"anyOf": true, "allOf": true, "not": true,
	"$defs": true, "$ref": true,
	"title": true, "description": true, "default": true, "deprecated": true, "examples": true,
}

var typeNames = map[string]bool{"null": true, "boolean": true, "object": true, "array": true, "number": true, "integer": true, "string": true}

// schemaNode is one compiled subschema.
type schemaNode struct {
	boolean *bool
	types   []string
	hasEnum bool
	enum    []any
	// enumKeys holds each enum value's RFC 8785 form, so a value is looked
	// up rather than compared with every value; nil when one has no form.
	enumKeys   map[string]bool
	hasConst   bool
	constValue any
	properties map[string]*schemaNode
	required   []string
	additional *schemaNode
	minProps   int
	maxProps   int
	names      *propertyNames
	items      *schemaNode
	minItems   int
	maxItems   int
	unique     bool
	minLength  int
	maxLength  int
	pattern    *Pattern
	minimum    *big.Rat
	maximum    *big.Rat
	exMinimum  *big.Rat
	exMaximum  *big.Rat
	anyOf      []*schemaNode
	allOf      []*schemaNode
	not        *schemaNode
	ref        string
	refNode    *schemaNode
	deprecated bool
}

type propertyNames struct {
	pattern   *Pattern
	maxLength int
}

// schemaCompiler compiles one kind's schema, reporting into diagnostics at
// pointers into the kind document.
type schemaCompiler struct {
	location    Location
	diagnostics *collector
	patterns    *patternBudget
	count       int
	defs        map[string]*schemaNode
	refs        []schemaRef
	failed      bool
	// rootDepth is the length of the root schema's path: 1 for a kind's
	// /schema, 0 for a schema compiled from its own root.
	rootDepth int
}

type schemaRef struct {
	from string // the $defs name the reference sits under, or "" at the root
	name string
	path []string
}

func (c *schemaCompiler) fail(code string, path []string) {
	at := c.location
	at.Pointer = pointerOf(path)
	c.diagnostics.add(code, at)
	c.failed = true
}

// compileSchema compiles a kind's schema at path (in the kind document),
// its patterns within the kind's budget. It returns nil when the schema is
// invalid.
func compileSchema(raw any, path []string, location Location, diagnostics *collector, patterns *patternBudget) *schemaNode {
	c := &schemaCompiler{location: location, diagnostics: diagnostics, patterns: patterns, defs: map[string]*schemaNode{}, rootDepth: len(path)}
	if object, ok := raw.(map[string]any); ok {
		if defs, ok := object["$defs"]; ok {
			defsObject, ok := defs.(map[string]any)
			if !ok {
				c.fail("REG-KIND-SCHEMA-FORM", appendPath(path, "$defs"))
			} else {
				for _, name := range sortedKeys(defsObject) {
					c.defs[name] = c.compile(defsObject[name], appendPath(appendPath(path, "$defs"), name), name)
				}
			}
		}
	}
	root := c.compile(raw, path, "")
	c.resolveRefs(root)
	if c.count > MaxSubschemas || !c.failed && expandedSize(root) > MaxSubschemas {
		c.fail("REG-KIND-SCHEMA-LIMIT", path)
	}
	if c.failed {
		return nil
	}
	return root
}

func (c *schemaCompiler) compile(raw any, path []string, def string) *schemaNode {
	c.count++
	if c.count > MaxSubschemas {
		return &schemaNode{}
	}
	if boolean, ok := raw.(bool); ok {
		return &schemaNode{boolean: &boolean}
	}
	object, ok := raw.(map[string]any)
	if !ok {
		c.fail("REG-KIND-SCHEMA-FORM", path)
		return &schemaNode{}
	}
	node := &schemaNode{minProps: -1, maxProps: -1, minItems: -1, maxItems: -1, minLength: -1, maxLength: -1}
	for _, keyword := range sortedKeys(object) {
		value := object[keyword]
		at := appendPath(path, keyword)
		if !schemaKeywords[keyword] {
			c.fail("REG-KIND-SCHEMA-KEYWORD", at)
			continue
		}
		switch keyword {
		case "type":
			node.types = c.types(value, at)
		case "enum":
			items, ok := value.([]any)
			switch {
			case !ok || len(items) == 0:
				c.fail("REG-KIND-SCHEMA-FORM", at)
			case len(items) > MaxEnumValues:
				c.fail("REG-KIND-SCHEMA-LIMIT", at)
			default:
				node.hasEnum, node.enum = true, items
				node.enumKeys = canonicalKeys(items)
			}
		case "const":
			node.hasConst, node.constValue = true, value
		case "properties":
			members, ok := value.(map[string]any)
			if !ok {
				c.fail("REG-KIND-SCHEMA-FORM", at)
				continue
			}
			node.properties = map[string]*schemaNode{}
			for _, name := range sortedKeys(members) {
				node.properties[name] = c.compile(members[name], appendPath(at, name), def)
			}
		case "required":
			node.required = c.distinctStrings(value, at)
		case "additionalProperties":
			node.additional = c.compile(value, at, def)
		case "minProperties":
			node.minProps = c.count0(value, at)
		case "maxProperties":
			node.maxProps = c.count0(value, at)
		case "propertyNames":
			node.names = c.propertyNames(value, at)
		case "items":
			if _, isArray := value.([]any); isArray {
				c.fail("REG-KIND-SCHEMA-FORM", at)
				continue
			}
			node.items = c.compile(value, at, def)
		case "minItems":
			node.minItems = c.count0(value, at)
		case "maxItems":
			node.maxItems = c.count0(value, at)
		case "uniqueItems":
			unique, ok := value.(bool)
			if !ok {
				c.fail("REG-KIND-SCHEMA-FORM", at)
			}
			node.unique = unique
		case "minLength":
			node.minLength = c.count0(value, at)
		case "maxLength":
			node.maxLength = c.count0(value, at)
		case "pattern":
			node.pattern = c.pattern(value, at)
		case "minimum":
			node.minimum = c.number(value, at)
		case "maximum":
			node.maximum = c.number(value, at)
		case "exclusiveMinimum":
			node.exMinimum = c.number(value, at)
		case "exclusiveMaximum":
			node.exMaximum = c.number(value, at)
		case "anyOf":
			node.anyOf = c.branches(value, at, def)
		case "allOf":
			node.allOf = c.branches(value, at, def)
		case "not":
			node.not = c.compile(value, at, def)
		case "$defs":
			// Compiled up front, and admitted only at the schema's root: a
			// kind's /schema, or an options schema's own root.
			if len(path) != c.rootDepth {
				c.fail("REG-KIND-SCHEMA-FORM", at)
			}
		case "$ref":
			text, ok := value.(string)
			name, found := strings.CutPrefix(text, "#/$defs/")
			if !ok || !found || name == "" || strings.Contains(name, "/") {
				c.fail("REG-KIND-SCHEMA-REF", at)
				continue
			}
			decoded, err := unescapeToken(name, false)
			if err != nil {
				c.fail("REG-KIND-SCHEMA-REF", at)
				continue
			}
			node.ref = decoded
			c.refs = append(c.refs, schemaRef{from: def, name: decoded, path: at})
		case "title", "description":
			if _, ok := value.(string); !ok {
				c.fail("REG-KIND-SCHEMA-FORM", at)
			}
		case "examples":
			if _, ok := value.([]any); !ok {
				c.fail("REG-KIND-SCHEMA-FORM", at)
			}
		case "deprecated":
			deprecated, ok := value.(bool)
			if !ok {
				c.fail("REG-KIND-SCHEMA-FORM", at)
			}
			node.deprecated = deprecated
		case "default":
			// An annotation: never applied.
		}
	}
	return node
}

func (c *schemaCompiler) types(value any, at []string) []string {
	var names []string
	switch typed := value.(type) {
	case string:
		names = []string{typed}
	case []any:
		if len(typed) == 0 {
			c.fail("REG-KIND-SCHEMA-FORM", at)
			return nil
		}
		for _, item := range typed {
			// A type outside the seven is refused before the next is read,
			// so names stays short and the search through it is cheap.
			name, ok := item.(string)
			if !ok || !typeNames[name] || slices.Contains(names, name) {
				c.fail("REG-KIND-SCHEMA-FORM", at)
				return nil
			}
			names = append(names, name)
		}
	default:
		c.fail("REG-KIND-SCHEMA-FORM", at)
		return nil
	}
	for _, name := range names {
		if !typeNames[name] {
			c.fail("REG-KIND-SCHEMA-FORM", at)
			return nil
		}
	}
	return names
}

func (c *schemaCompiler) distinctStrings(value any, at []string) []string {
	items, ok := value.([]any)
	if !ok {
		c.fail("REG-KIND-SCHEMA-FORM", at)
		return nil
	}
	var out []string
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok || seen[text] {
			c.fail("REG-KIND-SCHEMA-FORM", at)
			return nil
		}
		seen[text] = true
		out = append(out, text)
	}
	return out
}

// count0 reads a non-negative integer.
func (c *schemaCompiler) count0(value any, at []string) int {
	if n, ok := nonNegativeInteger(value); ok {
		return n
	}
	c.fail("REG-KIND-SCHEMA-FORM", at)
	return -1
}

func nonNegativeInteger(value any) (int, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	rat, ok := new(big.Rat).SetString(number.String())
	if !ok || !rat.IsInt() || rat.Sign() < 0 || !rat.Num().IsInt64() || rat.Num().Int64() > maxSafeInteger {
		return 0, false
	}
	return int(rat.Num().Int64()), true
}

func (c *schemaCompiler) number(value any, at []string) *big.Rat {
	if rat, ok := ratOf(value); ok {
		return rat
	}
	c.fail("REG-KIND-SCHEMA-FORM", at)
	return nil
}

func ratOf(value any) (*big.Rat, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return nil, false
	}
	return new(big.Rat).SetString(number.String())
}

func (c *schemaCompiler) pattern(value any, at []string) *Pattern {
	text, ok := value.(string)
	if !ok {
		c.fail("REG-KIND-SCHEMA-FORM", at)
		return nil
	}
	compiled, code := c.patterns.compile(text)
	if code != "" {
		c.fail(code, at)
		return nil
	}
	return compiled
}

func (c *schemaCompiler) propertyNames(value any, at []string) *propertyNames {
	object, ok := value.(map[string]any)
	if !ok {
		c.fail("REG-KIND-SCHEMA-FORM", at)
		return nil
	}
	c.count++
	names := &propertyNames{maxLength: -1}
	for _, keyword := range sortedKeys(object) {
		switch keyword {
		case "pattern":
			names.pattern = c.pattern(object[keyword], appendPath(at, keyword))
		case "maxLength":
			names.maxLength = c.count0(object[keyword], appendPath(at, keyword))
		default:
			c.fail("REG-KIND-SCHEMA-KEYWORD", appendPath(at, keyword))
		}
	}
	return names
}

func (c *schemaCompiler) branches(value any, at []string, def string) []*schemaNode {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		c.fail("REG-KIND-SCHEMA-FORM", at)
		return nil
	}
	if len(items) > MaxBranches {
		c.fail("REG-KIND-SCHEMA-LIMIT", at)
		return nil
	}
	out := make([]*schemaNode, len(items))
	for i, item := range items {
		out[i] = c.compile(item, appendPath(at, strconv.Itoa(i)), def)
	}
	return out
}

// resolveRefs links every $ref to its definition and refuses any cycle among
// definitions, through any keyword: a schema here is never recursive, so
// validating and walking one always terminates.
func (c *schemaCompiler) resolveRefs(root *schemaNode) {
	edges := map[string][]string{}
	for _, ref := range c.refs {
		if _, ok := c.defs[ref.name]; !ok {
			c.fail("REG-KIND-SCHEMA-REF", ref.path)
			continue
		}
		edges[ref.from] = append(edges[ref.from], ref.name)
	}
	// The cycle check covers every $ref that names a definition, whatever
	// else the schema has failed (registry companion §3.3).
	state := map[string]int{} // 1 visiting, 2 done
	var cyclic func(name string) bool
	cyclic = func(name string) bool {
		switch state[name] {
		case 1:
			return true
		case 2:
			return false
		}
		state[name] = 1
		for _, next := range edges[name] {
			if cyclic(next) {
				return true
			}
		}
		state[name] = 2
		return false
	}
	for _, ref := range c.refs {
		if ref.from != "" && cyclic(ref.from) {
			c.fail("REG-KIND-SCHEMA-REF", ref.path)
			return
		}
	}
	if c.failed {
		return
	}
	var link func(node *schemaNode)
	seen := map[*schemaNode]bool{}
	link = func(node *schemaNode) {
		if node == nil || seen[node] {
			return
		}
		seen[node] = true
		if node.ref != "" {
			node.refNode = c.defs[node.ref]
		}
		for _, child := range node.properties {
			link(child)
		}
		link(node.additional)
		link(node.items)
		link(node.not)
		for _, branch := range node.anyOf {
			link(branch)
		}
		for _, branch := range node.allOf {
			link(branch)
		}
	}
	link(root)
	for _, name := range sortedKeys(c.defs) {
		link(c.defs[name])
	}
}

// expandedSize is the number of subschemas a value's validation can visit at
// one position, which bounds every walk of the schema there: validate,
// reachable, declares, and a facts contract's reading. It counts the schema
// with each $ref expanded in place, as those walks follow it: every node
// (each properties member, additionalProperties, items, not, and each anyOf
// and allOf branch, a boolean schema included), propertyNames as one, and at
// each $ref the expanded size of the definition it names. A definition's size
// is computed once and reused at every $ref to it, so the count costs the
// schema as written, and it saturates just past MaxSubschemas, so a schema of
// definitions that each name the next sixteen times is refused at once
// rather than counted to 16^k. A definition no $ref reaches is not counted
// here; the written count holds it. The schema's references are acyclic
// (resolveRefs), so the count terminates.
func expandedSize(root *schemaNode) int {
	definitions := map[*schemaNode]int{}
	var size func(node *schemaNode) int
	size = func(node *schemaNode) int {
		if node == nil {
			return 0
		}
		total := 1
		add := func(n int) { total = min(total+n, MaxSubschemas+1) }
		if node.names != nil {
			add(1)
		}
		for _, child := range node.properties {
			add(size(child))
		}
		add(size(node.additional))
		add(size(node.items))
		add(size(node.not))
		for _, branch := range node.anyOf {
			add(size(branch))
		}
		for _, branch := range node.allOf {
			add(size(branch))
		}
		if node.refNode != nil {
			n, done := definitions[node.refNode]
			if !done {
				n = size(node.refNode)
				definitions[node.refNode] = n
			}
			add(n)
		}
		return total
	}
	return size(root)
}
