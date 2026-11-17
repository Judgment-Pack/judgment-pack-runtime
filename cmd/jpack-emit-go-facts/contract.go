package main

import (
	"regexp"
	"slices"
	"strings"
)

// Reading a facts contract (companion §10.3) into the positions the
// generator writes Go for.

// contract is one facts contract of the request.
type contract struct {
	id      string
	version string
	// kind is the kind the contract was generated from; nil for a contract
	// written by hand (source {"authored": true}).
	kind *kindSource
	root *position
	// names, set by nameContract.
	prefix string
	stem   string
}

type kindSource struct {
	id, version, digest string
}

// token is one reference token of a contract pointer.
type token struct {
	// wild is the token *: any member name or array index.
	wild bool
	// name is the member name, unescaped (~0 is ~, ~1 is /, ~2 is *).
	name string
	// escaped is the token as the contract writes it.
	escaped string
}

// position is one position of a facts document under a contract: the root,
// a row of the contract's facts, or an array element the contract gives only
// through its array's items.
type position struct {
	// pointer is the contract's pointer, "" for the root; a synthesized
	// element's is its array's pointer and "/*".
	pointer string
	tokens  []token
	typ     string
	always  bool
	// values is the closed list, in the contract's order: strings as
	// written, decimals in normal form (duplicates after normalization
	// dropped), booleans as "true" and "false".
	values    []string
	hasValues bool
	secret    bool
	// items is an array row's element type.
	items string
	// members are an object's literal members, sorted by escaped token.
	members []*position
	// others is an object's * member: the object is then a map.
	others *position
	// element is an array's element.
	element *position

	// Names, set by nameContract.
	typeName string // a struct's or a string enumeration's type
	field    string // the field in the parent struct
	param    string // the constructor argument, for a member always present
}

func (p *position) isStruct() bool { return p.typ == "object" && p.others == nil }
func (p *position) isMap() bool    { return p.typ == "object" && p.others != nil }
func (p *position) isEnum() bool   { return p.typ == "string" && p.hasValues }

var (
	// absoluteURI is RFC 3986's absolute-URI held to printable ASCII: a
	// scheme, a colon and no white space or fragment.
	absoluteURI = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*:[!-"$-~]*$`)
	// semver is SemVer 2.0.0 with no build metadata.
	semver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
		`(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$`)
	digest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

const maxURIBytes = 2048

var factTypes = map[string]bool{"array": true, "boolean": true, "decimal": true, "null": true, "object": true, "string": true}

var rowMembers = map[string]bool{"pointer": true, "type": true, "present": true, "items": true, "values": true, "secretReference": true}

// readContract reads one contract of the request and builds its positions.
// A contract outside §10.3's form is errContract; one in it that holds a
// position this emitter does not generate is errUnsupported.
func readContract(value any) (*contract, error) {
	m, ok := value.(map[string]any)
	if !ok || !exactMembers(m, "factsContractVersion", "id", "version", "source", "facts") {
		return nil, errContract
	}
	c := &contract{}
	c.id, _ = m["id"].(string)
	c.version, _ = m["version"].(string)
	if m["factsContractVersion"] != contractVersion || !validURI(c.id) || !semver.MatchString(c.version) {
		return nil, errContract
	}
	source, ok := m["source"].(map[string]any)
	switch {
	case ok && exactMembers(source, "authored") && source["authored"] == true:
	case ok && exactMembers(source, "kind"):
		kind, ok := source["kind"].(map[string]any)
		if !ok || !exactMembers(kind, "id", "version", "digest") {
			return nil, errContract
		}
		k := &kindSource{}
		k.id, _ = kind["id"].(string)
		k.version, _ = kind["version"].(string)
		k.digest, _ = kind["digest"].(string)
		if !validURI(k.id) || !semver.MatchString(k.version) || !digest.MatchString(k.digest) {
			return nil, errContract
		}
		c.kind = k
	default:
		return nil, errContract
	}
	facts, ok := m["facts"].([]any)
	if !ok || len(facts) > maxContractFacts {
		return nil, errContract
	}
	rows := make([]*position, 0, len(facts))
	for i, item := range facts {
		row, err := readRow(item)
		if err != nil {
			return nil, err
		}
		// Strictly increasing by pointer, then type, in code-point order
		// (Go compares strings by their UTF-8 bytes, which is that order).
		if i > 0 {
			last := rows[i-1]
			if c := strings.Compare(last.pointer, row.pointer); c > 0 || c == 0 && last.typ >= row.typ {
				return nil, errContract
			}
		}
		rows = append(rows, row)
	}
	root, err := buildPositions(rows)
	if err != nil {
		return nil, err
	}
	c.root = root
	return c, nil
}

func validURI(s string) bool {
	return len(s) <= maxURIBytes && absoluteURI.MatchString(s)
}

// readRow reads one item of a contract's facts.
func readRow(value any) (*position, error) {
	m, ok := value.(map[string]any)
	if !ok {
		return nil, errContract
	}
	for name := range m {
		if !rowMembers[name] {
			return nil, errContract
		}
	}
	p := &position{}
	var present string
	p.pointer, _ = m["pointer"].(string)
	p.typ, _ = m["type"].(string)
	present, _ = m["present"].(string)
	if !factTypes[p.typ] || present != "always" && present != "optional" {
		return nil, errContract
	}
	p.always = present == "always"
	tokens, ok := parsePointer(p.pointer)
	if !ok {
		return nil, errContract
	}
	p.tokens = tokens
	if raw, present := m["items"]; present {
		items, ok := raw.(string)
		if !ok || p.typ != "array" || !factTypes[items] {
			return nil, errContract
		}
		p.items = items
	}
	if raw, present := m["secretReference"]; present {
		if raw != true || p.typ != "object" {
			return nil, errContract
		}
		p.secret = true
	}
	if raw, present := m["values"]; present {
		list, ok := raw.([]any)
		valueType := p.typ
		if p.typ == "array" {
			valueType = p.items
		}
		if !ok || len(list) == 0 || valueType == "" || valueType == "null" || p.typ == "object" {
			return nil, errContract
		}
		values, err := readValues(list, valueType)
		if err != nil {
			return nil, err
		}
		p.values, p.hasValues = values, true
	}
	return p, nil
}

// readValues reads a closed list of values of one type: distinct values of
// the type, as §10.3 requires. Lists of arrays or objects (an array row whose
// items is array or object) are not generated.
func readValues(list []any, valueType string) ([]string, error) {
	var out []string
	written := map[string]bool{}
	seen := map[string]bool{}
	for _, item := range list {
		var text string
		switch valueType {
		case "string":
			s, ok := item.(string)
			if !ok {
				return nil, errContract
			}
			text = s
		case "decimal":
			s, ok := item.(string)
			normal, isDecimal := normalDecimal(s)
			if !ok || !isDecimal {
				return nil, errContract
			}
			if written[s] {
				return nil, errContract
			}
			written[s] = true
			if seen[normal] {
				continue
			}
			text = normal
		case "boolean":
			b, ok := item.(bool)
			if !ok {
				return nil, errContract
			}
			text = "false"
			if b {
				text = "true"
			}
		default:
			return nil, errUnsupported
		}
		if seen[text] {
			return nil, errContract
		}
		seen[text] = true
		out = append(out, text)
	}
	return out, nil
}

// parsePointer reads a contract pointer: "/" and at most 35 tokens, each *
// or a member name in which ~0, ~1 and ~2 are the only escapes.
func parsePointer(pointer string) ([]token, bool) {
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	parts := strings.Split(pointer[1:], "/")
	if len(parts) > maxPointerTokens {
		return nil, false
	}
	tokens := make([]token, len(parts))
	for i, part := range parts {
		if part == "*" {
			tokens[i] = token{wild: true, escaped: part}
			continue
		}
		var name strings.Builder
		for j := 0; j < len(part); j++ {
			if part[j] != '~' {
				name.WriteByte(part[j])
				continue
			}
			if j+1 == len(part) {
				return nil, false
			}
			switch part[j+1] {
			case '0':
				name.WriteByte('~')
			case '1':
				name.WriteByte('/')
			case '2':
				name.WriteByte('*')
			default:
				return nil, false
			}
			j++
		}
		tokens[i] = token{name: name.String(), escaped: part}
	}
	return tokens, true
}

// normalDecimal reads a string of Core §2.2's decimal grammar and writes it
// in RFC 0037 §2's normal form: no trailing zero after the point, no point
// when nothing follows it, and no sign on zero.
func normalDecimal(s string) (string, bool) {
	rest, negative := strings.CutPrefix(s, "-")
	integer, fraction, point := strings.Cut(rest, ".")
	if !allDigits(integer) || len(integer) > 1 && integer[0] == '0' || point && !allDigits(fraction) {
		return "", false
	}
	normal := integer
	if fraction = strings.TrimRight(fraction, "0"); fraction != "" {
		normal += "." + fraction
	}
	if negative && normal != "0" {
		normal = "-" + normal
	}
	return normal, true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// buildPositions arranges a contract's rows, already in pointer order, under
// the root. Each row's parent must be listed before it. What this emitter
// does not generate is refused (errUnsupported), never left out:
//   - a pointer listed with more than one type (an anyOf or open position);
//   - a row under a string, decimal, boolean or null, or a literal token
//     under an array;
//   - an object with both literal members and a * member, and * at the root;
//   - an array without items, an element row of another type than items,
//     element values that differ from the array's, and an array of objects
//     or arrays whose element row is not listed;
//   - a secret reference other than exactly scope and store, each an
//     always-present string;
//   - values on a type other than string, decimal and boolean.
func buildPositions(rows []*position) (*position, error) {
	root := &position{typ: "object", always: true}
	byPointer := map[string]*position{"": root}
	for _, row := range rows {
		if _, listed := byPointer[row.pointer]; listed {
			return nil, errUnsupported
		}
		parent, listed := byPointer[row.pointer[:strings.LastIndexByte(row.pointer, '/')]]
		if !listed {
			return nil, errUnsupported
		}
		last := row.tokens[len(row.tokens)-1]
		switch {
		case parent.typ == "object" && last.wild:
			if parent == root || len(parent.members) > 0 {
				return nil, errUnsupported
			}
			parent.others = row
		case parent.typ == "object":
			if parent.others != nil {
				return nil, errUnsupported
			}
			parent.members = append(parent.members, row)
		case parent.typ == "array" && last.wild:
			parent.element = row
		default:
			return nil, errUnsupported
		}
		byPointer[row.pointer] = row
	}
	if err := complete(root); err != nil {
		return nil, err
	}
	return root, nil
}

// complete checks each position below p and gives each array its element.
func complete(p *position) error {
	switch p.typ {
	case "object":
		slices.SortStableFunc(p.members, func(a, b *position) int {
			return strings.Compare(a.tokens[len(a.tokens)-1].escaped, b.tokens[len(b.tokens)-1].escaped)
		})
		if p.secret && !secretMembers(p) {
			return errUnsupported
		}
		for _, m := range p.members {
			if err := complete(m); err != nil {
				return err
			}
		}
		if p.others != nil {
			return complete(p.others)
		}
	case "array":
		if p.items == "" {
			return errUnsupported
		}
		e := p.element
		switch {
		case e == nil && (p.items == "object" || p.items == "array"):
			return errUnsupported
		case e == nil:
			tokens := append(slices.Clone(p.tokens), token{wild: true, escaped: "*"})
			e = &position{pointer: p.pointer + "/*", tokens: tokens, typ: p.items, values: p.values, hasValues: p.hasValues}
			p.element = e
		case e.typ != p.items:
			return errUnsupported
		case p.hasValues && e.hasValues && !sameSet(p.values, e.values):
			return errUnsupported
		case p.hasValues && !e.hasValues:
			e.values, e.hasValues = p.values, true
		}
		p.values, p.hasValues = nil, false
		return complete(e)
	}
	return nil
}

// secretMembers says whether a secret reference's object holds exactly
// scope and store, each a string the contract always holds.
func secretMembers(p *position) bool {
	if p.others != nil || len(p.members) != 2 {
		return false
	}
	for i, name := range []string{"scope", "store"} {
		m := p.members[i]
		if m.tokens[len(m.tokens)-1].name != name || m.typ != "string" || !m.always {
			return false
		}
	}
	return true
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, v := range a {
		if !slices.Contains(b, v) {
			return false
		}
	}
	return true
}
