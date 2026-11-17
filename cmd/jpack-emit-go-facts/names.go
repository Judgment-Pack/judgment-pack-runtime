package main

import (
	"strings"
)

// The naming rule (ADR-0100). Every Go name is formed from the contract's
// text by one deterministic rule, and a request whose names cannot be formed,
// or collide, is refused: no name is ever made up to avoid a collision.
//
//   - A word of a text is a maximal run of ASCII letters and digits; every
//     other code point, non-ASCII letters included, separates words. The
//     part of a text is its words, each with its first letter upper-cased,
//     run together: "db-role" is DbRole, "connectionLimit" ConnectionLimit,
//     "~1database" (the member "/database") Database.
//   - The contract's prefix is the part of the last segment of its id, once
//     a query is cut off and the id is split at "/" and ":". It must not be
//     empty or begin with a digit. The facts document's struct is the prefix
//     and "Facts".
//   - A position's type is the prefix followed by the part of each token of
//     its pointer, with * as "Item": /value/password is DbRoleValuePassword,
//     /value/privileges/* DbRoleValuePrivilegesItem. A struct's constructor
//     is "New" and its type.
//   - A member's field is the part of its token, with an "X" before it when
//     it begins with a digit.
//   - A string enumeration's constant is its type followed by the part of the
//     value: DbRoleEnvironmentProd.
//   - A constructor's argument is its field with the first letter
//     lower-cased, or the field itself when that is a Go keyword or a
//     predeclared name.
//   - The file of a contract is its id's last segment, lower-cased, its words
//     joined by "_", and "_facts.go", so that no file name ends in a GOOS or
//     GOARCH suffix or in _test.
//
// A part that is empty (a token, a value or an id segment with no ASCII
// letter or digit) is errName. These collide, and are errCollide: two
// package-level names of the request (types, constructors and constants,
// over every contract, and the names facts_support.go declares); two fields
// of one struct; a field named Build on the facts document's struct; a
// constructor argument named as its own type; and two contracts' files.

// supportNames are the package-level names facts_support.go exports.
var supportNames = []string{"BuildError", "Decimal", "DecimalFromInt64", "ErrNotDecimal", "MustParseDecimal", "ParseDecimal", "Ptr"}

// goPredeclared are the names of Go's universe block; with goKeywords, the
// names an argument does not take.
var goPredeclared = map[string]bool{
	"any": true, "bool": true, "byte": true, "comparable": true, "complex64": true, "complex128": true,
	"error": true, "float32": true, "float64": true, "int": true, "int8": true, "int16": true,
	"int32": true, "int64": true, "rune": true, "string": true, "uint": true, "uint8": true,
	"uint16": true, "uint32": true, "uint64": true, "uintptr": true, "true": true, "false": true,
	"iota": true, "nil": true, "append": true, "cap": true, "clear": true, "close": true,
	"complex": true, "copy": true, "delete": true, "imag": true, "len": true, "make": true,
	"max": true, "min": true, "new": true, "panic": true, "print": true, "println": true,
	"real": true, "recover": true,
}

// words splits a text into its maximal runs of ASCII letters and digits.
func words(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9')
	})
}

// part is a text's words, each with its first letter upper-cased.
func part(s string) string {
	var b strings.Builder
	for _, w := range words(s) {
		b.WriteString(strings.ToUpper(w[:1]))
		b.WriteString(w[1:])
	}
	return b.String()
}

func tokenPart(t token) string {
	if t.wild {
		return "Item"
	}
	return part(t.name)
}

// idSegment is the last segment of a contract id.
func idSegment(id string) string {
	if i := strings.IndexByte(id, '?'); i >= 0 {
		id = id[:i]
	}
	segments := strings.FieldsFunc(id, func(r rune) bool { return r == '/' || r == ':' })
	if len(segments) == 0 {
		return ""
	}
	return segments[len(segments)-1]
}

func startsWithDigit(s string) bool { return s != "" && '0' <= s[0] && s[0] <= '9' }

// namer collects the package-level names of one request.
type namer struct {
	taken map[string]bool
	files map[string]bool
}

func newNamer() *namer {
	n := &namer{taken: map[string]bool{}, files: map[string]bool{}}
	for _, name := range supportNames {
		n.taken[name] = true
	}
	return n
}

// take claims a package-level name.
func (n *namer) take(name string) error {
	if n.taken[name] {
		return errCollide
	}
	n.taken[name] = true
	return nil
}

// nameContract names one contract's prefix, file, types, constructors,
// constants, fields and arguments.
func (n *namer) nameContract(c *contract) error {
	segment := idSegment(c.id)
	c.prefix = part(segment)
	if c.prefix == "" || startsWithDigit(c.prefix) {
		return errName
	}
	c.stem = strings.ToLower(strings.Join(words(segment), "_"))
	if n.files[c.stem] {
		return errCollide
	}
	n.files[c.stem] = true
	c.root.typeName = c.prefix + "Facts"
	return n.namePosition(c, c.root)
}

func (n *namer) namePosition(c *contract, p *position) error {
	if p != c.root && (p.isStruct() || p.isEnum()) {
		var b strings.Builder
		b.WriteString(c.prefix)
		for _, t := range p.tokens {
			name := tokenPart(t)
			if name == "" {
				return errName
			}
			b.WriteString(name)
		}
		p.typeName = b.String()
	}
	switch {
	case p.isStruct():
		if err := n.take(p.typeName); err != nil {
			return err
		}
		if err := n.take("New" + p.typeName); err != nil {
			return err
		}
		fields := map[string]bool{}
		if p == c.root {
			fields["Build"] = true
		}
		for _, m := range p.members {
			field := part(m.tokens[len(m.tokens)-1].name)
			if field == "" {
				return errName
			}
			if startsWithDigit(field) {
				field = "X" + field
			}
			if fields[field] {
				return errCollide
			}
			fields[field] = true
			m.field = field
			m.param = strings.ToLower(field[:1]) + field[1:]
			if goKeywords[m.param] || goPredeclared[m.param] {
				m.param = field
			}
			if m.param == p.typeName {
				return errCollide
			}
			if err := n.namePosition(c, m); err != nil {
				return err
			}
		}
	case p.isMap():
		return n.namePosition(c, p.others)
	case p.typ == "array":
		return n.namePosition(c, p.element)
	case p.isEnum():
		if err := n.take(p.typeName); err != nil {
			return err
		}
		for _, value := range p.values {
			name := part(value)
			if name == "" {
				return errName
			}
			if err := n.take(p.typeName + name); err != nil {
				return err
			}
		}
	}
	return nil
}
