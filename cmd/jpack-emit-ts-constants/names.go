package main

import (
	"regexp"
	"strings"
)

// The naming rule. Only a kind name becomes a TypeScript identifier. Every
// other text of the request (a member name, an entry id, a string value)
// reaches the source only as a string literal (jsString), so no request text
// can end a literal or a comment, or name anything.
//
// A kind name matches ^[a-z][a-z0-9-]{0,62}$. Its words are its runs of
// letters and digits between hyphens. Its type is the words, each with its
// first letter upper-cased, run together ("db-role" gives DbRole); its
// constant is the same with the first word as written (dbRole); and the
// as-const object the constant aliases is the constant and "Entries"
// (dbRoleEntries). A name that is reserved is refused, and so is a name two
// kinds give, or that the generated code uses itself: neither is ever
// renamed.

// kindNames are the names one kind gives.
type kindNames struct {
	typeName, constName, entriesName string
}

func namesOf(kind string) kindNames {
	words := strings.FieldsFunc(kind, func(r rune) bool { return r == '-' })
	var pascal, camel strings.Builder
	for i, word := range words {
		upper := strings.ToUpper(word[:1]) + word[1:]
		pascal.WriteString(upper)
		if i == 0 {
			camel.WriteString(word)
		} else {
			camel.WriteString(upper)
		}
	}
	return kindNames{typeName: pascal.String(), constName: camel.String(), entriesName: camel.String() + "Entries"}
}

// identifierPattern is the form every generated name has. A kind name that
// matches kindPattern always gives one; the check is kept so that no other
// text ever becomes a name.
var identifierPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

// supportNames are the names the generated code uses besides the kinds'
// own: the JSON value alias it declares, and the two library types the
// constant's alias names. A kind type of one of these would shadow it.
var supportNames = map[string]bool{"JsonValue": true, "Readonly": true, "Record": true}

// reservedWords are the names a module-level const cannot have. They are
// ECMAScript's reserved words, those of strict mode code (every module is
// strict), the literals, the two names strict mode code cannot bind (eval and
// arguments), and the names TypeScript reserves at the top level of a module
// or as a built-in global (require, exports, module, undefined, globalThis).
// A kind's constant is the kind's first word as written, so a kind of one
// word can give any of them.
var reservedWords = map[string]bool{
	"await": true, "break": true, "case": true, "catch": true, "class": true, "const": true, "continue": true,
	"debugger": true, "default": true, "delete": true, "do": true, "else": true, "enum": true, "export": true,
	"extends": true, "false": true, "finally": true, "for": true, "function": true, "if": true, "import": true,
	"in": true, "instanceof": true, "new": true, "null": true, "return": true, "super": true, "switch": true,
	"this": true, "throw": true, "true": true, "try": true, "typeof": true, "var": true, "void": true,
	"while": true, "with": true, "yield": true,
	"implements": true, "interface": true, "let": true, "package": true, "private": true, "protected": true,
	"public": true, "static": true,
	"eval": true, "arguments": true,
	"require": true, "exports": true, "module": true, "undefined": true, "globalThis": true,
}

// checkNames gives each pinned kind its names, and refuses a name that is
// reserved (codeNameReserved) or that a kind earlier in name order, or the
// generated code, already holds (codeNameCollision). The diagnostic is at the
// kind that refuses.
func checkNames(doc *resolved) (map[string]kindNames, []diagnostic, error) {
	var diagnostics []diagnostic
	names := map[string]kindNames{}
	taken := map[string]bool{}
	for name := range supportNames {
		taken[name] = true
	}
	for _, p := range doc.pins {
		n := namesOf(p.kind)
		for _, name := range []string{n.typeName, n.constName, n.entriesName} {
			if !identifierPattern.MatchString(name) {
				return nil, nil, errInternal
			}
		}
		switch {
		case reservedWords[n.constName] || supportNames[n.typeName]:
			diagnostics = append(diagnostics, diagnostic{code: codeNameReserved, location: kindLocation(doc, p.kind)})
		case taken[n.typeName] || taken[n.constName] || taken[n.entriesName]:
			diagnostics = append(diagnostics, diagnostic{code: codeNameCollision, location: kindLocation(doc, p.kind)})
		}
		taken[n.typeName], taken[n.constName], taken[n.entriesName] = true, true, true
		names[p.kind] = n
	}
	return names, diagnostics, nil
}
