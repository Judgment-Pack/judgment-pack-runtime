package registry

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// Generating a facts contract from a kind (RFC 0037 §4, ADR-0095).
//
// The kind's schema is read into a shape: the types a position may hold, the
// closed lists of values it may take, its members with whether each is always
// present when the position is an object, and the shape of its other members
// and of its elements. allOf and $ref narrow a shape, anyOf widens it (each
// branch read with the rest of the schema), and not is ignored, so the
// contract may list a value no entry holds and never leaves out one an entry
// can hold. A position the schema leaves open (no type, no additionalProperties
// or no items) is listed once per type, and once per type at <position>/*, and
// nothing deeper is listed: a pack's pointer below that * row is listed.

// FactsContractVersion is the facts contract format this runtime writes and
// reads.
const FactsContractVersion = "1.0.0"

// Contract bounds (ADR-0095).
const (
	MaxContractBytes = 8 * 1024 * 1024
	MaxContractFacts = 65536
	// maxShapeWork bounds one generation's work: the shapes it builds and
	// the reading of the schema they are built from, each schema node
	// collected, each closed-list value checked against a schema, and each
	// value merged (ADR-0095, amendment).
	maxShapeWork = 1 << 20
)

// factType is a set of contract types, one bit each, in code-point order of
// their names.
type factType uint8

const (
	typeArray factType = 1 << iota
	typeBoolean
	typeDecimal
	typeNull
	typeObject
	typeString
	allTypes = typeArray | typeBoolean | typeDecimal | typeNull | typeObject | typeString
)

var factTypeNames = []struct {
	bit  factType
	name string
}{{typeArray, "array"}, {typeBoolean, "boolean"}, {typeDecimal, "decimal"}, {typeNull, "null"}, {typeObject, "object"}, {typeString, "string"}}

func factTypeNamed(name string) factType {
	for _, t := range factTypeNames {
		if t.name == name {
			return t.bit
		}
	}
	return 0
}

func (t factType) single() (string, bool) {
	for _, entry := range factTypeNames {
		if t == entry.bit {
			return entry.name, true
		}
	}
	return "", false
}

// schemaTypes maps JSON Schema's type names to contract types: integer and
// number are both decimal.
func schemaTypes(names []string) factType {
	var out factType
	for _, name := range names {
		switch name {
		case "integer", "number":
			out |= typeDecimal
		default:
			out |= factTypeNamed(name)
		}
	}
	return out
}

// shape is what a position of a facts document may hold. A nil *shape is a
// position that holds nothing. Shapes are shared and never changed once made.
type shape struct {
	types factType
	// values holds a closed list for a scalar type; a type with no key is
	// open.
	values map[factType][]any
	// open is a position that may hold anything: every type, and nothing
	// known below it.
	open    bool
	secret  bool
	members map[string]member
	// others is the shape of a member not in members; nil when there is none.
	others *shape
	// element is the shape of an array element; nil when there is none.
	element *shape
}

type member struct {
	shape  *shape
	always bool
}

var openShape = &shape{types: allTypes, open: true}

// expanded is an open shape one level down: every type, with open members and
// elements, so that a pattern can name something under it.
func expanded() *shape {
	return &shape{types: allTypes, others: openShape, element: openShape}
}

func secretShape() *shape {
	return &shape{types: typeObject, secret: true, members: map[string]member{
		"store": {shape: &shape{types: typeString, values: map[factType][]any{typeString: {StoreGCPSecretManager}}}, always: true},
		"scope": {shape: &shape{types: typeString}, always: true},
	}}
}

func scalarShape(t factType, values ...any) *shape {
	s := &shape{types: t}
	if len(values) > 0 {
		s.values = map[factType][]any{t: values}
	}
	return s
}

type generator struct {
	work     int
	exceeded bool
	// unmapped is a closed-list number with no normal decimal, which an
	// admitted kind cannot hold: admission keeps every number as a literal
	// normalDecimal reads. Were one met, the contract is not generated,
	// rather than generated with the list widened past it.
	unmapped bool
	// listed is a lower bound on the encoded size of the facts listed so
	// far, charged against MaxContractBytes as they are listed.
	listed int
}

// minFactBytes is the size of the shortest item a contract can hold, less
// its pointer: what each listed fact adds to the contract at the least.
var minFactBytes = len(`{"pointer":"","present":"always","type":"null"}`)

// maxFactsDepth is the deepest position a contract lists, in pointer tokens.
// An entry's resolved value nests no deeper than the resolved document that
// holds it (MaxDepth, which step 8 holds it to: an overlay can carry a value
// deeper than the registry holds it), and the deepest a value sits in a facts
// document is under /references/<pointer>/value, three tokens down, so no
// facts document holds anything deeper.
const maxFactsDepth = MaxDepth + 3

func (g *generator) charge() bool {
	return g.chargeUnits(1)
}

// chargeUnits charges work that grows with what it reads, such as the nodes
// a flatten visits or the atoms an expansion copies, to the same bound.
func (g *generator) chargeUnits(units int) bool {
	g.work += units
	if g.work > maxShapeWork {
		g.exceeded = true
	}
	return !g.exceeded
}

// flatten collects the schemas that all apply at a position: each node, what
// its $ref and allOf name, and the anyOf groups among them. never is a false
// schema among them. A node reached twice, through two $refs to one
// definition or two allOf branches naming it, is collected once, since a
// schema that applies twice applies once. Each node it visits is charged to
// the generation's work (ADR-0095, amendment), and it stops once the work
// passes its bound; the generation is then refused, so what it collected is
// never read.
func (g *generator) flatten(nodes []*schemaNode) (atoms []*schemaNode, groups [][]*schemaNode, never bool) {
	seen := map[*schemaNode]bool{}
	var visit func(nodes []*schemaNode)
	visit = func(nodes []*schemaNode) {
		for _, node := range nodes {
			if node == nil || seen[node] || !g.charge() {
				continue
			}
			seen[node] = true
			if node.boolean != nil {
				if !*node.boolean {
					never = true
				}
				continue
			}
			atoms = append(atoms, node)
			if node.refNode != nil {
				visit([]*schemaNode{node.refNode})
			}
			visit(node.allOf)
			if node.anyOf != nil {
				groups = append(groups, node.anyOf)
			}
		}
	}
	visit(nodes)
	return atoms, groups, never
}

// fromSchemas is the shape of a position where every one of nodes applies.
func (g *generator) fromSchemas(nodes []*schemaNode) *shape {
	atoms, groups, never := g.flatten(nodes)
	if never || g.exceeded {
		return nil
	}
	return g.expand(atoms, groups)
}

// expand reads the anyOf groups one at a time: the union, over the first
// group's branches, of the position read with that branch added. The atoms
// and groups each branch copies are charged to the work.
func (g *generator) expand(atoms []*schemaNode, groups [][]*schemaNode) *shape {
	if !g.charge() {
		return nil
	}
	if len(groups) == 0 {
		return g.base(atoms)
	}
	var out *shape
	for _, branch := range groups[0] {
		branchAtoms, branchGroups, never := g.flatten([]*schemaNode{branch})
		if never {
			continue
		}
		if !g.chargeUnits(len(atoms) + len(branchAtoms) + len(groups) + len(branchGroups)) {
			return nil
		}
		out = g.union(out, g.expand(append(slices.Clone(atoms), branchAtoms...), append(slices.Clone(groups[1:]), branchGroups...)))
	}
	return out
}

// base is the shape where atoms all apply and none has an anyOf left to read.
func (g *generator) base(atoms []*schemaNode) *shape {
	if len(atoms) == 0 {
		return openShape
	}
	// A closed list of values: the position holds one of them, the ones
	// every schema here allows, and nothing else. Each list is read once,
	// into a set keyed by its values' RFC 8785 forms (hasDuplicate says why
	// that is jsonEqual for admitted values).
	var literals []any
	closed := false
	for _, atom := range atoms {
		var list []any
		switch {
		case atom.hasEnum:
			list = atom.enum
		case atom.hasConst:
			list = []any{atom.constValue}
		default:
			continue
		}
		if !g.chargeUnits(len(list) + len(literals)) {
			return nil
		}
		if atom.hasEnum && atom.hasConst {
			list = slices.DeleteFunc(slices.Clone(list), func(v any) bool { return !jsonEqual(v, atom.constValue) })
		}
		if !closed {
			literals, closed = slices.Clone(list), true
			continue
		}
		keys := map[string]bool{}
		for _, w := range list {
			if key, err := jcs.EncodeDocument(w); err == nil {
				keys[string(key)] = true
			}
		}
		literals = slices.DeleteFunc(literals, func(v any) bool {
			key, err := jcs.EncodeDocument(v)
			if err != nil {
				return !slices.ContainsFunc(list, func(w any) bool { return jsonEqual(v, w) })
			}
			return !keys[string(key)]
		})
	}
	if closed {
		// Every node that applies here is an atom, with one branch of each
		// anyOf, so each is checked for its own keywords only: a full
		// validation of each would walk every node under its $refs again.
		var out *shape
		for _, literal := range literals {
			allowed := true
			for _, atom := range atoms {
				if !g.charge() {
					return nil
				}
				var findings []valueFinding
				// The literal's own nodes are checked under what is left of
				// the generation's work bound, and charged to it.
				check := &validation{work: maxShapeWork - g.work}
				atom.validateOwn(check, literal, nil, &findings)
				if check.exceeded || !g.chargeUnits(maxShapeWork-g.work-check.work) {
					g.exceeded = true
					return nil
				}
				if hasError(findings) {
					allowed = false
					break
				}
			}
			if allowed {
				out = g.union(out, g.literal(literal))
			}
		}
		return out
	}
	types := allTypes
	for _, atom := range atoms {
		if atom.types != nil {
			types &= schemaTypes(atom.types)
		}
	}
	s := &shape{types: types}
	if types&typeObject != 0 && !g.object(s, atoms) {
		s.types &^= typeObject
		s.members, s.others = nil, nil
	}
	if types&typeArray != 0 {
		var items []*schemaNode
		for _, atom := range atoms {
			if atom.items != nil {
				items = append(items, atom.items)
			}
		}
		if len(items) == 0 {
			s.element = openShape
		} else {
			s.element = g.fromSchemas(items)
		}
	}
	if s.types == 0 {
		return nil
	}
	if s.types == allTypes && len(s.members) == 0 && s.others == openShape && s.element == openShape {
		// Schemas that constrain nothing here: the position is open.
		return openShape
	}
	return s
}

// object fills an object position's members, and reports false when no
// object satisfies every schema here (a required member no schema allows).
func (g *generator) object(s *shape, atoms []*schemaNode) bool {
	declared := map[string]bool{}
	required := map[string]bool{}
	var others []*schemaNode
	for _, atom := range atoms {
		for name := range atom.properties {
			declared[name] = true
		}
		for _, name := range atom.required {
			required[name] = true
		}
		if atom.additional != nil {
			others = append(others, atom.additional)
		}
	}
	names := sortedKeys(declared)
	for _, name := range sortedKeys(required) {
		if !declared[name] {
			names = append(names, name)
		}
	}
	s.members = map[string]member{}
	for _, name := range names {
		var applies []*schemaNode
		for _, atom := range atoms {
			if child, ok := atom.properties[name]; ok {
				applies = append(applies, child)
			} else if atom.additional != nil {
				applies = append(applies, atom.additional)
			}
		}
		child := openShape
		if len(applies) > 0 {
			child = g.fromSchemas(applies)
		}
		if child == nil {
			if required[name] {
				return false
			}
			continue
		}
		s.members[name] = member{shape: child, always: required[name]}
	}
	if len(others) == 0 {
		s.others = openShape
	} else {
		s.others = g.fromSchemas(others)
	}
	return true
}

// literal is the exact shape of one value.
func (g *generator) literal(value any) *shape {
	if !g.charge() {
		return nil
	}
	switch typed := value.(type) {
	case nil:
		return scalarShape(typeNull)
	case bool:
		return scalarShape(typeBoolean, typed)
	case string:
		return scalarShape(typeString, typed)
	case json.Number:
		text, ok := normalDecimal(typed)
		if !ok {
			g.unmapped = true
			return nil
		}
		return scalarShape(typeDecimal, text)
	case []any:
		s := &shape{types: typeArray}
		for _, item := range typed {
			s.element = g.union(s.element, g.literal(item))
		}
		return s
	case map[string]any:
		s := &shape{types: typeObject, members: map[string]member{}}
		for _, name := range sortedKeys(typed) {
			if child := g.literal(typed[name]); child != nil {
				s.members[name] = member{shape: child, always: true}
			}
		}
		return s
	}
	return nil
}

// union is a shape that holds whatever a or b holds. A member is always
// present in it when it is always present in each side that may be an
// object.
func (g *generator) union(a, b *shape) *shape {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if !g.charge() {
		return nil
	}
	if a.open {
		return g.widen(b)
	}
	if b.open {
		return g.widen(a)
	}
	out := &shape{types: a.types | b.types}
	for _, t := range []factType{typeBoolean, typeDecimal, typeString} {
		listA, closedA := a.values[t]
		listB, closedB := b.values[t]
		var list []any
		switch {
		case a.types&t != 0 && b.types&t != 0:
			if !closedA || !closedB {
				continue
			}
			// A shape's values are strings and booleans, which jsonEqual
			// compares as ==, so a set merges the lists in one pass.
			if !g.chargeUnits(len(listA) + len(listB)) {
				return nil
			}
			list = slices.Clone(listA)
			held := make(map[any]bool, len(listA)+len(listB))
			for _, w := range listA {
				held[w] = true
			}
			for _, v := range listB {
				if !held[v] {
					held[v] = true
					list = append(list, v)
				}
			}
		case a.types&t != 0 && closedA:
			list = listA
		case b.types&t != 0 && closedB:
			list = listB
		default:
			continue
		}
		if out.values == nil {
			out.values = map[factType][]any{}
		}
		out.values[t] = list
	}
	objectA, objectB := a.types&typeObject != 0, b.types&typeObject != 0
	out.secret = (a.secret || b.secret) && (a.secret || !objectA) && (b.secret || !objectB)
	if out.types&typeObject != 0 {
		out.members = map[string]member{}
		names := map[string]bool{}
		for name := range a.members {
			names[name] = objectA
		}
		for name := range b.members {
			names[name] = names[name] || objectB
		}
		for _, name := range sortedKeys(names) {
			if !names[name] {
				continue
			}
			side := func(s *shape, isObject bool) (*shape, bool) {
				if !isObject {
					return nil, true
				}
				if m, ok := s.members[name]; ok {
					return m.shape, m.always
				}
				return s.others, false
			}
			shapeA, alwaysA := side(a, objectA)
			shapeB, alwaysB := side(b, objectB)
			if merged := g.union(shapeA, shapeB); merged != nil {
				out.members[name] = member{shape: merged, always: alwaysA && alwaysB}
			}
		}
		var othersA, othersB *shape
		if objectA {
			othersA = a.others
		}
		if objectB {
			othersB = b.others
		}
		out.others = g.union(othersA, othersB)
	}
	if out.types&typeArray != 0 {
		var elementA, elementB *shape
		if a.types&typeArray != 0 {
			elementA = a.element
		}
		if b.types&typeArray != 0 {
			elementB = b.element
		}
		out.element = g.union(elementA, elementB)
	}
	return out
}

// widen is the union of s with a position that may hold anything: every
// type, no closed list, and s's members, all optional, each widened in turn,
// so that what s says is below it is still listed.
func (g *generator) widen(s *shape) *shape {
	if s == nil || s.open || !g.charge() {
		return openShape
	}
	out := &shape{types: allTypes, members: make(map[string]member, len(s.members))}
	if s.types&typeObject != 0 {
		for name, m := range s.members {
			out.members[name] = member{shape: g.widen(m.shape)}
		}
		out.others = g.widen(s.others)
	} else {
		out.others = openShape
	}
	if s.types&typeArray != 0 {
		out.element = g.widen(s.element)
	} else {
		out.element = openShape
	}
	return out
}

// withSecret is s with the positions tokens names holding a secret
// reference's {"store", "scope"} in place of its string. A position the
// pattern names exactly is replaced; an array index the pattern names is one
// element of several, so the element shape gains the secret's beside its own.
func (g *generator) withSecret(s *shape, tokens []patternToken) *shape {
	if s == nil || !g.charge() {
		return nil
	}
	if len(tokens) == 0 {
		return secretShape()
	}
	if s.open {
		s = expanded()
	}
	token, rest := tokens[0], tokens[1:]
	out := *s
	if s.types&typeObject != 0 {
		out.members = make(map[string]member, len(s.members)+1)
		for name, m := range s.members {
			if token.any || name == token.literal {
				m = member{shape: g.withSecret(m.shape, rest), always: m.always}
			}
			out.members[name] = m
		}
		switch {
		case token.any:
			out.others = g.withSecret(s.others, rest)
		case s.others != nil:
			if _, declared := s.members[token.literal]; !declared {
				out.members[token.literal] = member{shape: g.withSecret(s.others, rest)}
			}
		}
	}
	if s.types&typeArray != 0 && s.element != nil {
		switch {
		case token.any:
			out.element = g.withSecret(s.element, rest)
		case isIndexToken(token.literal):
			out.element = g.union(s.element, g.withSecret(s.element, rest))
		}
	}
	return &out
}

// descendants is s with every position under it: what a reference with a
// pointer may carry as its value.
func (g *generator) descendants(s *shape) *shape {
	out := s
	var visit func(*shape)
	visit = func(node *shape) {
		if node == nil || node.open || g.exceeded {
			return
		}
		children := make([]*shape, 0, len(node.members)+2)
		for _, name := range sortedKeys(node.members) {
			children = append(children, node.members[name].shape)
		}
		children = append(children, node.others, node.element)
		for _, child := range children {
			if child != nil {
				out = g.union(out, child)
				visit(child)
			}
		}
	}
	visit(s)
	return out
}

// mappedKind is the shape of a kind's mapped value: its schema's, with each
// secret position holding a secret's store and scope.
func (g *generator) mappedKind(kind *Kind) *shape {
	s := openShape
	if kind.schema != nil {
		s = g.fromSchemas([]*schemaNode{kind.schema})
	}
	for _, secret := range kind.secrets {
		s = g.withSecret(s, secret.tokens)
	}
	return s
}

// alwaysAt reports whether every value the shape allows has a position at
// tokens: each step is a member always present in an object, and an object
// is all the position can be.
func alwaysAt(s *shape, tokens []patternToken) bool {
	for _, token := range tokens {
		if s == nil || s.open || s.types != typeObject || token.any {
			return false
		}
		m, ok := s.members[token.literal]
		if !ok || !m.always {
			return false
		}
		s = m.shape
	}
	return s != nil
}

func isLiteral(pattern pointerPattern) bool {
	return !slices.ContainsFunc(pattern.tokens, func(token patternToken) bool { return token.any })
}

func literalPath(pattern pointerPattern) []string {
	path := make([]string, len(pattern.tokens))
	for i, token := range pattern.tokens {
		path[i] = token.literal
	}
	return path
}

// references is the shape of a facts document's "references" member for a
// kind whose value has the given shape.
func (g *generator) references(r *Result, kind *Kind, value *shape) *shape {
	targets := map[string]*shape{}
	target := func(kinds []string) *shape {
		var names []any
		var valueShape *shape
		for _, name := range kinds {
			pinned := r.pinnedKind(name)
			if pinned == nil {
				continue
			}
			names = append(names, name)
			mapped, ok := targets[name]
			if !ok {
				mapped = g.descendants(g.mappedKind(pinned))
				targets[name] = mapped
			}
			valueShape = g.union(valueShape, mapped)
		}
		if len(names) == 0 || valueShape == nil {
			return nil
		}
		return &shape{types: typeObject, members: map[string]member{
			"kind":  {shape: scalarShape(typeString, names...), always: true},
			"id":    {shape: scalarShape(typeString), always: true},
			"value": {shape: valueShape, always: true},
		}}
	}
	out := &shape{types: typeObject, members: map[string]member{}}
	var wildcardKinds []string
	wildcardNamed := map[string]bool{}
	for _, pattern := range kind.references {
		if isLiteral(pattern.pattern) {
			continue
		}
		for _, name := range pattern.kinds {
			if !wildcardNamed[name] {
				wildcardNamed[name] = true
				wildcardKinds = append(wildcardKinds, name)
			}
		}
	}
	for _, pattern := range kind.references {
		if !isLiteral(pattern.pattern) {
			continue
		}
		var kinds []string
		named := map[string]bool{}
		for _, other := range kind.references {
			if other.pattern.overlaps(pattern.pattern) {
				for _, name := range other.kinds {
					if !named[name] {
						named[name] = true
						kinds = append(kinds, name)
					}
				}
			}
		}
		if s := target(kinds); s != nil {
			out.members[pointerOf(literalPath(pattern.pattern))] = member{shape: s, always: alwaysAt(value, pattern.pattern.tokens)}
		}
	}
	if len(wildcardKinds) > 0 {
		out.others = target(wildcardKinds)
	}
	return out
}

// contractFactItem is one item of a contract's facts.
type contractFactItem struct {
	pointer string
	typeOf  string
	item    map[string]any
}

// escapeContractToken writes a member name as a contract pointer token: ~ and
// / escaped as RFC 6901 does, and a name that is exactly * as ~2, since * is
// the wildcard.
func escapeContractToken(name string) string {
	if name == "*" {
		return "~2"
	}
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(name)
}

// flattenShape lists every position under s, each once per type, down to
// maxFactsDepth. An open position is listed with one row of every type under
// it, at <pointer>/*, and nothing deeper: a pack's pointer below that row is
// listed (ADR-0095). depth is the number of tokens in pointer.
func (g *generator) flattenShape(s *shape, pointer string, depth int, always bool, out *[]contractFactItem) {
	if s == nil || g.exceeded || depth > maxFactsDepth {
		return
	}
	present := "optional"
	if always {
		present = "always"
	}
	for _, t := range factTypeNames {
		if s.types&t.bit == 0 {
			continue
		}
		if len(*out) >= MaxContractFacts {
			g.exceeded = true
			return
		}
		item := map[string]any{"pointer": pointer, "type": t.name, "present": present}
		if list, closed := s.values[t.bit]; closed && t.bit != typeNull {
			item["values"] = list
		}
		if t.bit == typeObject && s.secret {
			item["secretReference"] = true
		}
		if t.bit == typeArray && !s.open && s.element != nil && !s.element.open {
			if name, single := s.element.types.single(); single {
				item["items"] = name
				if list, closed := s.element.values[s.element.types]; closed && s.element.types != typeNull {
					item["values"] = list
				}
			}
		}
		if g.listed += len(pointer) + minFactBytes; g.listed > MaxContractBytes {
			g.exceeded = true
			return
		}
		*out = append(*out, contractFactItem{pointer: pointer, typeOf: t.name, item: item})
	}
	if s.open {
		if s != openRows {
			g.flattenShape(openRows, pointer+"/*", depth+1, false, out)
		}
		return
	}
	if s.types&typeObject != 0 {
		for _, name := range sortedKeys(s.members) {
			m := s.members[name]
			g.flattenShape(m.shape, pointer+"/"+escapeContractToken(name), depth+1, m.always, out)
		}
	}
	var wild *shape
	if s.types&typeObject != 0 {
		wild = s.others
	}
	if s.types&typeArray != 0 {
		wild = g.union(wild, s.element)
	}
	g.flattenShape(wild, pointer+"/*", depth+1, false, out)
}

// openRows is the <pointer>/* of an open position: every type, and nothing
// listed under it.
var openRows = &shape{types: allTypes, open: true}

// FactsContract generates the facts contract of one pinned kind of a valid
// registry (RFC 0037 §4), in RFC 8785 form. Its id and version are the
// kind's, and its source the kind's id, version and digest. A contract over
// its bounds is reported as FACTS-CONTRACT-LIMIT and not returned.
func (r *Result) FactsContract(kindName string) ([]byte, []Diagnostic, error) {
	if !r.Report.Valid || r.registry == nil {
		return nil, nil, ErrInvalid
	}
	kind := r.pinnedKind(kindName)
	if kind == nil {
		return nil, nil, ErrNoKind
	}
	limit := []Diagnostic{factsDiagnostic("FACTS-CONTRACT-LIMIT", Location{Document: DocumentContract, Kind: kind.Name})}
	g := &generator{}
	value := g.mappedKind(kind)
	environments := make([]any, len(r.registry.environments))
	for i, environment := range r.registry.environments {
		environments[i] = environment
	}
	fixed := func(names ...string) *shape {
		s := &shape{types: typeObject, members: map[string]member{}}
		for _, name := range names {
			s.members[name] = member{shape: scalarShape(typeString), always: true}
		}
		return s
	}
	entryShape := fixed("id", "owner")
	entryShape.members["kind"] = member{shape: scalarShape(typeString, kind.Name), always: true}
	root := &shape{types: typeObject, members: map[string]member{
		"registry":    {shape: fixed("id", "version", "digest"), always: true},
		"environment": {shape: scalarShape(typeString, environments...), always: true},
		"entry":       {shape: entryShape, always: true},
		"references":  {shape: g.references(r, kind, value), always: true},
	}}
	if value != nil {
		root.members["value"] = member{shape: value, always: true}
	}
	var items []contractFactItem
	for _, name := range sortedKeys(root.members) {
		m := root.members[name]
		g.flattenShape(m.shape, "/"+escapeContractToken(name), 1, m.always, &items)
	}
	if g.unmapped {
		return nil, nil, errUnmapped
	}
	if g.exceeded {
		return nil, limit, nil
	}
	slices.SortFunc(items, func(a, b contractFactItem) int {
		if c := strings.Compare(a.pointer, b.pointer); c != 0 {
			return c
		}
		return strings.Compare(a.typeOf, b.typeOf)
	})
	facts := make([]any, len(items))
	for i, item := range items {
		facts[i] = item.item
	}
	document := map[string]any{
		"factsContractVersion": FactsContractVersion,
		"id":                   kind.ID,
		"version":              kind.Version,
		"source":               map[string]any{"kind": map[string]any{"id": kind.ID, "version": kind.Version, "digest": kind.Digest}},
		"facts":                facts,
	}
	encoded, err := jcs.EncodeDocument(document)
	if err != nil {
		return nil, nil, err
	}
	if len(encoded) > MaxContractBytes {
		return nil, limit, nil
	}
	return encoded, nil, nil
}
