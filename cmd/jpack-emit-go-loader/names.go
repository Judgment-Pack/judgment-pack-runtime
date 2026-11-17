package main

import (
	"strings"
)

// The naming rule (ADR-0102), the facts emitter's (ADR-0100) applied to a
// kind's schema. Every Go name is formed from the declaration's text by one
// deterministic rule, and a declaration whose names cannot be formed, or
// collide, is refused: no name is ever made up to avoid a collision.
//
//   - A word of a text is a maximal run of ASCII letters and digits; every
//     other code point, non-ASCII letters included, separates words. The
//     part of a text is its words, each with its first letter upper-cased,
//     run together: "db-role" is DbRole, "connectionLimit" ConnectionLimit.
//   - A kind's type is the part of its name. A struct's or an enumeration's
//     type below it is the type of the position above it followed by the
//     part of the member name, or by Item for the element of an array or a
//     map: /grants/*/role is DbRoleGrantsItemRole.
//   - A member's field is the part of its name, with an "X" before it when
//     it begins with a digit.
//   - An enumeration's constant is its type followed by the part of the
//     value: DbRolePrivilegesItemRead.
//   - A kind's Registry methods are its type, which decodes one entry, and
//     its type followed by IDs, which lists the entries.
//   - The file of a kind is its name's words, joined by "_", and "_kind.go",
//     so that no file name ends in a GOOS or GOARCH suffix or in _test, and
//     none is loader.go.
//
// Every name formed is then held to identifier: an exported Go identifier of
// ASCII letters and digits. A part that is empty (a member name or a value
// with no ASCII letter or digit) is findName. These collide, and are
// findCollide: two package-level names of the request (types and constants,
// over every kind, and the names loader.go exports); two fields of one
// struct; two Registry methods; and two kinds' files.

// supportNames are the package-level names loader.go exports.
var supportNames = []string{"ErrKind", "ErrNoEntry", "ErrSecret", "ErrValue", "Load", "LoadFile", "Registry"}

// isWordRune says whether r belongs to a word: an ASCII letter or digit.
func isWordRune(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9'
}

// words splits a text into its maximal runs of ASCII letters and digits.
func words(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !isWordRune(r) })
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

// identifier is the identifier check: name is an exported Go identifier of
// ASCII letters and digits, the only names the generated source holds
// outside a string literal. No Go keyword or predeclared name is exported, so
// none passes it.
func identifier(name string) bool {
	if name == "" || name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		if c := rune(name[i]); !isWordRune(c) {
			return false
		}
	}
	return true
}

// namer collects the names of one request.
type namer struct {
	taken   map[string]bool // package-level names
	methods map[string]bool // the Registry's methods
	files   map[string]bool
}

func newNamer() *namer {
	n := &namer{taken: map[string]bool{}, methods: map[string]bool{}, files: map[string]bool{}}
	for _, name := range supportNames {
		n.taken[name] = true
	}
	return n
}

// claim takes a name in one of the namer's sets: findName when it is not an
// identifier, findCollide when it is taken.
func claim(set map[string]bool, name string) error {
	if !identifier(name) {
		return findName
	}
	if set[name] {
		return findCollide
	}
	set[name] = true
	return nil
}

// nameKind names one kind's type, methods, file, and every type, constant and
// field below it.
func (n *namer) nameKind(k *kind) error {
	k.typeName = part(k.name)
	k.stem = strings.ToLower(strings.Join(words(k.name), "_"))
	if n.files[k.stem] {
		return findCollide
	}
	n.files[k.stem] = true
	if err := claim(n.methods, k.typeName); err != nil {
		return err
	}
	if err := claim(n.methods, k.typeName+"IDs"); err != nil {
		return err
	}
	return n.namePosition(k.root, k.typeName)
}

// namePosition names p, whose type is name if it has one, and every
// position below it.
func (n *namer) namePosition(p *position, name string) error {
	switch p.shape {
	case shapeStruct:
		p.typeName = name
		if err := claim(n.taken, name); err != nil {
			return err
		}
		fields := map[string]bool{}
		for _, m := range p.members {
			namePart := part(m.name)
			if namePart == "" {
				return findName
			}
			field := namePart
			if field[0] >= '0' && field[0] <= '9' {
				field = "X" + field
			}
			if err := claim(fields, field); err != nil {
				return err
			}
			m.field = field
			if err := n.namePosition(m.at, name+namePart); err != nil {
				return err
			}
		}
	case shapeEnum:
		p.typeName = name
		if err := claim(n.taken, name); err != nil {
			return err
		}
		for _, value := range p.values {
			valuePart := part(value)
			if valuePart == "" {
				return findName
			}
			if err := claim(n.taken, name+valuePart); err != nil {
				return err
			}
		}
	case shapeArray, shapeMap:
		return n.namePosition(p.element, name+"Item")
	}
	return nil
}
