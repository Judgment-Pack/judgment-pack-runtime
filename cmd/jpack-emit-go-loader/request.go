package main

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// Reading the request's declarations and its resolved documents' pins
// (registry companion §11.2), and binding each pin to its declaration by
// digest.

// kind is one declaration of the request: what the emitter reads of it, and,
// once generate has built and named it, its type.
type kind struct {
	name    string
	id      string
	version string
	// digest is the SHA-256 of the declaration's RFC 8785 form, which the
	// pins of the kind must carry (companion §2.3).
	digest  string
	schema  any
	secrets [][]patternToken

	// Set by generate.
	root     *position
	typeName string
	stem     string
}

// patternToken is one reference token of a pointer pattern (companion §3.5):
// * for any member name or array index, or a member name, unescaped.
type patternToken struct {
	wild    bool
	literal string
}

var (
	// absoluteURI is RFC 3986's absolute-URI held to printable ASCII: a
	// scheme, a colon and no white space or fragment.
	absoluteURI = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*:[!-"$-~]*$`)
	// semver is SemVer 2.0.0 with no build metadata.
	semver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
		`(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$`)
	// kindName is a kind's local name (companion §3.1).
	kindName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	// ownerPattern is a kind owner (companion §3.1).
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9._@/\-]{1,128}$`)
	// environmentName is a registry environment (companion §4).
	environmentName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

const (
	maxURIBytes            = 2048
	maxPointerPatternBytes = 512
)

// declarationMembers are a kind declaration's members (companion §3.1), and
// whether each is required. Any other member refuses the declaration: a
// member this emitter does not know could change what a position means.
var declarationMembers = map[string]bool{
	"kindFormatVersion": true, "id": true, "version": true, "kind": true, "owner": true, "schema": true,
	"description": false, "entryId": false, "secrets": false, "references": false, "overlayable": false,
	"bounds": false, "deprecated": false,
}

// readDeclarations reads the request's declarations, which the host writes
// sorted by kind with each kind once. One out of that order is errRequest.
func readDeclarations(items []any) ([]*kind, error) {
	kinds := make([]*kind, 0, len(items))
	for i, item := range items {
		k, err := readDeclaration(item)
		if err != nil {
			return nil, err
		}
		if i > 0 && kinds[i-1].name >= k.name {
			return nil, errRequest
		}
		kinds = append(kinds, k)
	}
	return kinds, nil
}

// readDeclaration reads one kind declaration: the members the emitter uses,
// each of its companion §3.1 form, and its digest. Its schema is read by
// generate.
func readDeclaration(value any) (*kind, error) {
	m, ok := value.(map[string]any)
	if !ok {
		return nil, errDeclaration
	}
	for name := range m {
		if _, known := declarationMembers[name]; !known {
			return nil, errDeclaration
		}
	}
	for name, required := range declarationMembers {
		if _, present := m[name]; required && !present {
			return nil, errDeclaration
		}
	}
	k := &kind{schema: m["schema"]}
	k.name, _ = m["kind"].(string)
	k.id, _ = m["id"].(string)
	k.version, _ = m["version"].(string)
	owner, _ := m["owner"].(string)
	if m["kindFormatVersion"] != kindVersion || !kindName.MatchString(k.name) || !validURI(k.id) ||
		!semver.MatchString(k.version) || !ownerPattern.MatchString(owner) {
		return nil, errDeclaration
	}
	if raw, present := m["secrets"]; present {
		list, ok := raw.([]any)
		if !ok {
			return nil, errDeclaration
		}
		for _, item := range list {
			source, ok := item.(string)
			if !ok {
				return nil, errDeclaration
			}
			pattern, ok := parsePointerPattern(source)
			if !ok {
				return nil, errDeclaration
			}
			k.secrets = append(k.secrets, pattern)
		}
	}
	// The declaration's digest is over its RFC 8785 form, the form the host
	// wrote it in (companion §2.3, §11.2). It is taken here, never read from
	// the request.
	canonical, err := jcs.EncodeDocument(m)
	if err != nil {
		return nil, errDeclaration
	}
	sum := sha256.Sum256(canonical)
	k.digest = "sha256:" + hex.EncodeToString(sum[:])
	return k, nil
}

func validURI(s string) bool {
	return len(s) <= maxURIBytes && absoluteURI.MatchString(s)
}

// parsePointerPattern reads a pointer pattern (companion §3.5): "/" and
// tokens, each * or a member name in which ~0, ~1 and ~2 are the only
// escapes, of at most 512 bytes as written.
func parsePointerPattern(source string) ([]patternToken, bool) {
	if !strings.HasPrefix(source, "/") || len(source) > maxPointerPatternBytes {
		return nil, false
	}
	parts := strings.Split(source[1:], "/")
	tokens := make([]patternToken, len(parts))
	for i, part := range parts {
		if part == "*" {
			tokens[i] = patternToken{wild: true}
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
		tokens[i] = patternToken{literal: name.String()}
	}
	return tokens, true
}

// checkPins holds every resolved document of the request to the
// declarations: its pins, sorted by kind as the host writes them, are of
// exactly the declarations' kinds, one to one, each with its declaration's
// id, version and digest (companion §11.2). A pin with no declaration, or
// with another digest, and a declaration no pin names, are errPin. A
// request with declarations and no resolved document leaves every
// declaration without a pin, and is errPin too.
func checkPins(resolved []any, kinds []*kind) error {
	if len(resolved) == 0 && len(kinds) > 0 {
		return errPin
	}
	environments := map[string]bool{}
	for _, item := range resolved {
		environment, pins, err := readResolved(item)
		if err != nil {
			return err
		}
		if environments[environment] {
			return errRequest
		}
		environments[environment] = true
		if len(pins) != len(kinds) {
			return errPin
		}
		// Both are sorted by kind, each kind once, so they match one to one
		// exactly when they match item by item.
		for i, pin := range pins {
			k := kinds[i]
			if pin.kind != k.name || pin.id != k.id || pin.version != k.version || pin.digest != k.digest {
				return errPin
			}
		}
	}
	return nil
}

// pin is one member of a resolved document's kinds.
type pin struct {
	kind, id, version, digest string
}

// readResolved reads one resolved document of the request (companion §8):
// exactly its members, of their forms, its pins sorted by kind with each
// kind once, and each entry of a kind it pins. It returns the environment
// and the pins. An entry's value is not read: the generated code reads it,
// at run time.
func readResolved(value any) (string, []pin, error) {
	m, ok := value.(map[string]any)
	if !ok || !exactMembers(m, "resolvedFormatVersion", "registry", "environment", "kinds", "entries") ||
		m["resolvedFormatVersion"] != resolvedVersion {
		return "", nil, errRequest
	}
	environment, ok := m["environment"].(string)
	if !ok || !environmentName.MatchString(environment) {
		return "", nil, errRequest
	}
	registry, ok := m["registry"].(map[string]any)
	if !ok || !exactMembers(registry, "id", "version", "digest") ||
		!isString(registry["id"]) || !isString(registry["version"]) || !isString(registry["digest"]) {
		return "", nil, errRequest
	}
	items, ok := m["kinds"].([]any)
	if !ok {
		return "", nil, errRequest
	}
	pins := make([]pin, 0, len(items))
	for i, item := range items {
		object, ok := item.(map[string]any)
		if !ok || !exactMembers(object, "kind", "id", "version", "digest") {
			return "", nil, errRequest
		}
		var p pin
		var good [4]bool
		p.kind, good[0] = object["kind"].(string)
		p.id, good[1] = object["id"].(string)
		p.version, good[2] = object["version"].(string)
		p.digest, good[3] = object["digest"].(string)
		if !good[0] || !good[1] || !good[2] || !good[3] || i > 0 && pins[i-1].kind >= p.kind {
			return "", nil, errRequest
		}
		pins = append(pins, p)
	}
	pinned := make(map[string]bool, len(pins))
	for _, p := range pins {
		pinned[p.kind] = true
	}
	entries, ok := m["entries"].([]any)
	if !ok {
		return "", nil, errRequest
	}
	for _, item := range entries {
		object, ok := item.(map[string]any)
		if !ok || !exactMembers(object, "kind", "id", "owner", "value") ||
			!isString(object["id"]) || !isString(object["owner"]) {
			return "", nil, errRequest
		}
		entryKind, ok := object["kind"].(string)
		if !ok || !pinned[entryKind] {
			return "", nil, errRequest
		}
	}
	return environment, pins, nil
}
