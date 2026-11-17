package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// The emitter's diagnostic codes. Each refuses the run: the response holds
// no files and the host writes nothing.
const (
	// A pin has no declaration in the request.
	codeDeclarationMissing = "x-ts-declaration-missing"
	// A declaration's SHA-256 over its RFC 8785 form, or its id or version,
	// is not its pin's.
	codeDeclarationMismatch = "x-ts-declaration-mismatch"
	// A declaration is of a kind the resolved document does not pin.
	codeDeclarationUnpinned = "x-ts-declaration-unpinned"
	// A declaration is not of the kind declaration form this emitter reads.
	codeDeclarationForm = "x-ts-declaration-form"
	// A schema holds a keyword or shape this emitter does not generate,
	// "not" among them.
	codeSchemaUnsupported = "x-ts-schema-unsupported"
	// A secret position is an array item, which cannot be left out without
	// changing the array.
	codeSecretInArray = "x-ts-secret-in-array"
	// A secrets pattern can match a position whose type would hold a
	// schema's literal, an enum or const value: the value at a secret
	// position is a secret, and the literal could be it.
	codeSecretLiteral = "x-ts-secret-literal"
	// A name made from a kind name is a reserved word, or a name the
	// generated code itself uses.
	codeNameReserved = "x-ts-name-reserved"
	// Two kinds give the same name.
	codeNameCollision = "x-ts-name-collision"
	// A member name or an entry id the output would hold is __proto__, which
	// an object literal reads as its prototype.
	codeMemberProto = "x-ts-member-proto"
	// The file is past the response bounds.
	codeBounds = "x-ts-bounds"
	// The file's path is one the host refuses: the environment is a Windows
	// device name.
	codePath = "x-ts-path"
)

// diagnostic is one error diagnostic: a code and a location of the registry
// companion's §7.3 form, which names only what the request holds and never
// a value.
type diagnostic struct {
	code     string
	location map[string]any
}

// kindLocation is a location in a kind declaration: the whole declaration,
// since a location without an entry resolves only at pointer "". The kind is
// named only when the request pins it, the one way such a location
// resolves.
func kindLocation(doc *resolved, kind string) map[string]any {
	location := map[string]any{"document": "kind", "pointer": ""}
	for _, p := range doc.pins {
		if p.kind == kind {
			location["kind"] = kind
		}
	}
	return location
}

// registryLocation is the whole resolved document of the environment.
func registryLocation(environment string) map[string]any {
	return map[string]any{"document": "registry", "environment": environment, "pointer": ""}
}

// maxLocationPointerBytes is the host's bound on a location's pointer.
const maxLocationPointerBytes = 512

// valueLocation is a position in an entry's resolved value. An entry id
// outside the entry-id grammar of companion §6.2 cannot be named, and a
// pointer past the host's bound cannot be read, so either falls back to the
// whole document of the environment.
func valueLocation(environment string, e entry, path []string) map[string]any {
	pointer := pointerOf(path)
	if !entryIDGrammar(e.id) || len(pointer) > maxLocationPointerBytes {
		return map[string]any{"document": "value", "environment": environment, "pointer": ""}
	}
	return map[string]any{
		"document": "value", "environment": environment, "pointer": pointer,
		"entry": map[string]any{"kind": e.kind, "id": e.id},
	}
}

// entryIDGrammar is companion §6.2's entry-id: 1 to 128 characters from
// U+0021 to U+007E, other than "/" and "#".
func entryIDGrammar(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if c := id[i]; c < 0x21 || c > 0x7e || c == '/' || c == '#' {
			return false
		}
	}
	return true
}

// pointerOf writes an RFC 6901 pointer.
func pointerOf(path []string) string {
	var b strings.Builder
	for _, token := range path {
		b.WriteByte('/')
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1"))
	}
	return b.String()
}

// kindDecl is a read kind declaration: what the output depends on.
type kindDecl struct {
	pin     pin
	schema  any
	defs    map[string]any
	secrets patterns
}

// declarationMembers are a kind declaration's members (companion §3.1).
var declarationMembers = map[string]bool{
	"kindFormatVersion": true, "id": true, "version": true, "kind": true, "owner": true, "description": true,
	"entryId": true, "schema": true, "secrets": true, "references": true, "overlayable": true, "bounds": true,
	"deprecated": true,
}

// bindDeclarations checks each pin against its declaration (companion
// §11.2): every pin has one, the SHA-256 of the declaration's RFC 8785 form
// is the pin's digest and its id and version are the pin's, and every
// declaration has a pin. Only then is each declaration read. It returns the
// read declarations by kind, or the diagnostics that refuse the run.
func bindDeclarations(doc *resolved, declarations []any) (map[string]*kindDecl, []diagnostic) {
	var diagnostics []diagnostic
	byKind := map[string]map[string]any{}
	for _, item := range declarations {
		object, ok := item.(map[string]any)
		kind, _ := object["kind"].(string)
		if !ok || !kindPattern.MatchString(kind) {
			diagnostics = append(diagnostics, diagnostic{code: codeDeclarationForm, location: kindLocation(doc, "")})
			continue
		}
		if byKind[kind] != nil {
			diagnostics = append(diagnostics, diagnostic{code: codeDeclarationForm, location: kindLocation(doc, kind)})
			continue
		}
		byKind[kind] = object
	}
	pinned := map[string]bool{}
	for _, p := range doc.pins {
		pinned[p.kind] = true
		declaration := byKind[p.kind]
		if declaration == nil {
			diagnostics = append(diagnostics, diagnostic{code: codeDeclarationMissing, location: kindLocation(doc, p.kind)})
			continue
		}
		digest, ok := digestOf(declaration)
		if !ok || digest != p.digest || declaration["id"] != p.id || declaration["version"] != p.version {
			diagnostics = append(diagnostics, diagnostic{code: codeDeclarationMismatch, location: kindLocation(doc, p.kind)})
		}
	}
	for _, kind := range sortedNames(byKind) {
		if !pinned[kind] {
			diagnostics = append(diagnostics, diagnostic{code: codeDeclarationUnpinned, location: kindLocation(doc, "")})
		}
	}
	if len(diagnostics) > 0 {
		return nil, diagnostics
	}
	kinds := map[string]*kindDecl{}
	for _, p := range doc.pins {
		k, ok := readDeclaration(byKind[p.kind], p)
		if !ok {
			diagnostics = append(diagnostics, diagnostic{code: codeDeclarationForm, location: kindLocation(doc, p.kind)})
			continue
		}
		kinds[p.kind] = k
	}
	return kinds, diagnostics
}

// digestOf is the "sha256:" digest of a value's RFC 8785 form (companion
// §2.3).
func digestOf(value any) (string, bool) {
	data, err := jcs.EncodeDocument(value)
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), true
}

// readDeclaration reads what the output depends on from a declaration whose
// digest is its pin's: the selector, a schema that is an object or a
// boolean, the root's $defs, and the secrets patterns. A member the
// companion does not list refuses it, since a later format could give one a
// meaning for secrets that this emitter would not apply.
func readDeclaration(declaration map[string]any, p pin) (*kindDecl, bool) {
	for name := range declaration {
		if !declarationMembers[name] {
			return nil, false
		}
	}
	if declaration["kindFormatVersion"] != kindFormatVersion {
		return nil, false
	}
	k := &kindDecl{pin: p, schema: declaration["schema"]}
	switch schema := k.schema.(type) {
	case bool:
	case map[string]any:
		if raw, present := schema["$defs"]; present {
			defs, ok := raw.(map[string]any)
			if !ok {
				return nil, false
			}
			k.defs = defs
		}
	default:
		return nil, false
	}
	if raw, present := declaration["secrets"]; present {
		items, ok := raw.([]any)
		if !ok {
			return nil, false
		}
		for _, item := range items {
			source, ok := item.(string)
			tokens, parsed := parsePattern(source)
			if !ok || !parsed {
				return nil, false
			}
			k.secrets = append(k.secrets, tokens)
		}
	}
	return k, true
}

// token is one reference token of a pointer pattern (companion §3.5): "*",
// which matches any member name or array index, or a literal.
type token struct {
	wildcard bool
	literal  string
}

// patterns are pointer patterns, each the tokens that remain below a
// position.
type patterns [][]token

// parsePattern parses a pointer pattern: "/" and tokens, "*" alone the
// wildcard, with ~0, ~1 and ~2 (a literal "*") the only escapes, of at most
// 512 bytes.
func parsePattern(source string) ([]token, bool) {
	if !strings.HasPrefix(source, "/") || len(source) > 512 {
		return nil, false
	}
	var tokens []token
	for _, raw := range strings.Split(source[1:], "/") {
		if raw == "*" {
			tokens = append(tokens, token{wildcard: true})
			continue
		}
		var b strings.Builder
		for i := 0; i < len(raw); i++ {
			if raw[i] != '~' {
				b.WriteByte(raw[i])
				continue
			}
			if i+1 == len(raw) {
				return nil, false
			}
			switch raw[i+1] {
			case '0':
				b.WriteByte('~')
			case '1':
				b.WriteByte('/')
			case '2':
				b.WriteByte('*')
			default:
				return nil, false
			}
			i++
		}
		tokens = append(tokens, token{literal: b.String()})
	}
	return tokens, true
}

// member returns the patterns that remain below the member or array index
// named by the concrete token name: those whose first token is "*" or is
// name.
func (ps patterns) member(name string) patterns {
	var out patterns
	for _, p := range ps {
		if p[0].wildcard || p[0].literal == name {
			out = append(out, p[1:])
		}
	}
	return out
}

// ends reports whether a pattern ends at this position: the position is a
// secret position.
func (ps patterns) ends() bool {
	for _, p := range ps {
		if len(p) == 0 {
			return true
		}
	}
	return false
}

// items returns the patterns that remain below an array's items: those
// whose first token is "*" or an array index.
func (ps patterns) items() patterns {
	var out patterns
	for _, p := range ps {
		if p[0].wildcard || isIndex(p[0].literal) {
			out = append(out, p[1:])
		}
	}
	return out
}

// lastMayBeItem reports whether a pattern's last token is "*" or an array
// index: below a position whose schema does not say it is an object, its
// secret position may be an array item.
func (ps patterns) lastMayBeItem() bool {
	for _, p := range ps {
		if last := p[len(p)-1]; last.wildcard || isIndex(last.literal) {
			return true
		}
	}
	return false
}

// isIndex says whether a token is an array index (companion §3.5): a decimal
// of at most 9 digits with no leading zero, or 0.
func isIndex(token string) bool {
	if token == "" || len(token) > 9 || len(token) > 1 && token[0] == '0' {
		return false
	}
	for i := 0; i < len(token); i++ {
		if token[i] < '0' || token[i] > '9' {
			return false
		}
	}
	return true
}
