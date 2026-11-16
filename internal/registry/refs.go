package registry

import (
	"regexp"
	"strings"
)

var (
	projectIDPattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
	secretNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)
	versionPattern    = regexp.MustCompile(`^[1-9][0-9]{0,8}$`)
	digitsPattern     = regexp.MustCompile(`^[0-9]+$`)
)

// StoreGCPSecretManager is the one store of format 1.0.0 (RFC 0035 §3).
const StoreGCPSecretManager = "gcp-sm"

// entryReference is a parsed entry:<kind>/<id>[#pointer].
type entryReference struct {
	kind       string
	id         string
	pointer    []string
	hasPointer bool
}

// parseEntryReference parses an entry reference (RFC 0035 §2).
func parseEntryReference(text string) (entryReference, bool) {
	rest, ok := strings.CutPrefix(text, "entry:")
	if !ok {
		return entryReference{}, false
	}
	target, pointer, hasPointer := strings.Cut(rest, "#")
	kind, id, ok := strings.Cut(target, "/")
	if !ok || !kindNamePattern.MatchString(kind) || !validEntryID(id) {
		return entryReference{}, false
	}
	reference := entryReference{kind: kind, id: id, hasPointer: hasPointer}
	if hasPointer {
		if len(pointer) > 512 {
			return entryReference{}, false
		}
		tokens, err := parsePointer(pointer)
		if err != nil {
			return entryReference{}, false
		}
		reference.pointer = tokens
	}
	return reference, true
}

// validEntryID is RFC 0035's entry-id: 1 to 128 of U+0021..U+007E, no / or #.
func validEntryID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c < 0x21 || c > 0x7e || c == '/' || c == '#' {
			return false
		}
	}
	return true
}

// secretReference is a parsed, valid secret reference.
type secretReference struct {
	store   string
	project string
}

// parseSecretReference checks a value at a secret position. It returns the
// one code RFC 0035 §3's precedence gives a failure, or "" for a valid
// reference.
func parseSecretReference(value any) (secretReference, string) {
	text, ok := value.(string)
	if !ok {
		return secretReference{}, "REF-SECRET-FORM"
	}
	rest, ok := strings.CutPrefix(text, "secret:")
	if !ok {
		return secretReference{}, "REF-SECRET-FORM"
	}
	store, locator, ok := strings.Cut(rest, ":")
	if !ok {
		return secretReference{}, "REF-SECRET-FORM"
	}
	if store != StoreGCPSecretManager {
		return secretReference{}, "REF-SECRET-STORE"
	}
	segments := strings.Split(locator, "/")
	if len(segments) != 6 || segments[0] != "projects" || segments[2] != "secrets" || segments[4] != "versions" {
		return secretReference{}, "REF-SECRET-FORM"
	}
	project, name, version := segments[1], segments[3], segments[5]
	if secretNamePattern.MatchString(name) && version != "" &&
		(!digitsPattern.MatchString(version) || digitsPattern.MatchString(project)) {
		return secretReference{}, "REF-SECRET-PIN"
	}
	if !projectIDPattern.MatchString(project) || !secretNamePattern.MatchString(name) || !versionPattern.MatchString(version) {
		return secretReference{}, "REF-SECRET-FORM"
	}
	return secretReference{store: store, project: project}, ""
}
