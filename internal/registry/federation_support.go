package registry

// What a federation assembly of registry format 1.1.0 (internal/federation)
// reuses from the reader of format 1.0.0, whose rules it keeps
// unchanged (registry-1.1.0 §1): the admission of one document under its own
// selector and bound, the reading of a kind declaration and the reach of its
// schema, pointer patterns, entry references and JSON Pointers. Each is an
// existing function of this package, exported as it is. Nothing here changes
// what validate, resolve or facts write.

// AdmitDocument runs step 1's admission on one document: the size bound,
// strict I-JSON within the depth bound (a repeated member is
// REG-DOC-DUPLICATE), the number rules and the exact selector. It returns the
// root object, or nil and the REG-DOC-* diagnostics, at location, that
// refuse it.
func AdmitDocument(data []byte, maxBytes int, selectorMember, selector string, location Location) (map[string]any, []Diagnostic) {
	collected := &collector{}
	root := admit(data, maxBytes, selectorMember, selector, location, collected)
	return root, collected.sorted()
}

// ReadKind runs step 2 on one admitted kind declaration, located at location.
// It returns the kind, or nil when the declaration is unusable, with the
// diagnostics of reading it.
func ReadKind(root map[string]any, location Location) (*Kind, []Diagnostic) {
	collected := &collector{}
	kind := readKind(root, location, collected)
	return kind, collected.sorted()
}

// Reaches reports whether some value the kind's schema allows has a position
// the pattern matches, by §3.5's rule.
func (k *Kind) Reaches(pattern PointerPattern) bool {
	return k.schema.reachable(pattern.pattern.tokens)
}

// PointerPattern is a pointer pattern of §3.5: a JSON Pointer whose token "*"
// matches any member name or array index, "~2" being a literal "*".
type PointerPattern struct {
	pattern pointerPattern
}

// ParsePointerPattern parses a pointer pattern. The empty pattern is refused,
// as a kind's are.
func ParsePointerPattern(text string) (PointerPattern, error) {
	pattern, err := parsePointerPattern(text)
	return PointerPattern{pattern: pattern}, err
}

// Overlaps reports whether some concrete pointer matches both patterns: an
// equal number of tokens, each pair equal or one of them "*".
func (p PointerPattern) Overlaps(other PointerPattern) bool {
	return p.pattern.overlaps(other.pattern)
}

// Position is one place in a value that a pattern matches: its JSON Pointer
// and the value it holds.
type Position struct {
	Pointer string
	Value   any
}

// Positions returns every place in value that the pattern matches, in
// document order (members by sorted name).
func (p PointerPattern) Positions(value any) []Position {
	found := p.pattern.positions(value)
	out := make([]Position, len(found))
	for i, position := range found {
		out[i] = Position{Pointer: pointerOf(position.path), Value: position.value}
	}
	return out
}

// ParseEntryReference parses an entry reference (§6.2) and returns the kind
// and the id it names.
func ParseEntryReference(text string) (kind, id string, ok bool) {
	reference, ok := parseEntryReference(text)
	return reference.kind, reference.id, ok
}

// ParsePointer parses an RFC 6901 JSON Pointer into its decoded tokens.
func ParsePointer(pointer string) ([]string, error) {
	return parsePointer(pointer)
}

// Lookup returns the value at tokens in value, and whether there is one.
func Lookup(value any, tokens []string) (any, bool) {
	return lookup(value, tokens)
}

// ValidEnvironment is the grammar of an environment name (§4).
func ValidEnvironment(text string) bool { return environmentRegex.MatchString(text) }

// ValidEntryID is the grammar of an entry id (§4): 1 to 128 printable ASCII
// characters other than "/" and "#".
func ValidEntryID(text string) bool { return validEntryID(text) }

// JSONEqual is §7.2's JSON equality: numbers by exact value, objects
// regardless of member order, arrays item by item.
func JSONEqual(a, b any) bool { return jsonEqual(a, b) }
