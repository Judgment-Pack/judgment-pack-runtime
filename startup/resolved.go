package startup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Bounds of a resolved registry (RFC 0034 §1).
const (
	// MaxResolvedBytes bounds a resolved document's size.
	MaxResolvedBytes = 8 * 1024 * 1024
	// MaxResolvedDepth bounds how many arrays and objects nest, the root
	// object counted.
	MaxResolvedDepth = 32
)

// resolvedFormatVersions are the exact selectors this module reads.
var resolvedFormatVersions = []string{"1.0.0"}

// ResolvedFormatVersions returns the exact resolvedFormatVersion selectors
// this module reads (RFC 0034 §8). A producer writes one of them or refuses.
func ResolvedFormatVersions() []string {
	return slices.Clone(resolvedFormatVersions)
}

// Errors [ReadResolved] returns, each wrapped with where in the document it
// applies, as a JSON Pointer. No error carries a value from the document.
var (
	// ErrSelector: the document's resolvedFormatVersion is missing or is not
	// exactly one of ResolvedFormatVersions.
	ErrSelector = errors.New("startup: the resolved document's resolvedFormatVersion is not one this module reads")
	// ErrEnvironment: the document was resolved for another environment than
	// the one named, or none was named.
	ErrEnvironment = errors.New("startup: the resolved document is not for the environment named")
	// ErrDocument: the document is not a resolved document of its selector.
	ErrDocument = errors.New("startup: the resolved document is malformed")
)

// Resolved is one environment's resolved registry (RFC 0034 §5).
type Resolved struct {
	// ResolvedFormatVersion is the document's selector, one of
	// ResolvedFormatVersions.
	ResolvedFormatVersion string
	// Registry identifies the registry the document was resolved from.
	Registry RegistryIdentity
	// Environment is the environment the document was resolved for.
	Environment string
	// Kinds are the kind declarations the registry pins, in canonical order.
	Kinds []KindPin
	// Entries are the entries that apply in the environment, in canonical
	// order: by kind, then by id.
	Entries []Entry
}

// RegistryIdentity is a resolved document's registry member.
type RegistryIdentity struct {
	ID      string
	Version string
	Digest  string
}

// KindPin is one member of a resolved document's kinds.
type KindPin struct {
	Kind    string
	ID      string
	Version string
	Digest  string
}

// Entry is one entry of a resolved document. Value is the entry's resolved
// value as the document holds it. Every reference in it is as the registry
// wrote it: an entry or secret reference is a string, never what it refers to.
type Entry struct {
	Kind  string
	ID    string
	Owner string
	Value json.RawMessage
}

// ReadResolvedFile reads the resolved document at path, as [ReadResolved]
// does. It refuses a path that does not lead to a regular file
// ([ErrNotRegular]), a named pipe among them, without waiting on it. The
// document holds references, not secrets, so the file is not held to
// [ReadFile]'s permission rule.
func ReadResolvedFile(path, environment string) (*Resolved, error) {
	// A named pipe at the path is refused, not waited on: the open does not
	// block (openNoWait), and the opened file must be regular.
	file, err := openFile(path)
	if err != nil {
		return nil, fmt.Errorf("startup: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("startup: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s", ErrNotRegular, path)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxResolvedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("startup: %w", err)
	}
	return ReadResolved(data, environment)
}

// ReadResolved reads one resolved document, for the environment the caller
// runs in. Before reading any member it refuses a document over
// [MaxResolvedBytes] or [MaxResolvedDepth] ([ErrLimit]), and one that is not
// one UTF-8 JSON value or has a duplicate member name anywhere
// ([ErrDocument]). Then it refuses, in this order:
//   - a resolvedFormatVersion that is missing or not exactly one of
//     [ResolvedFormatVersions] ([ErrSelector]);
//   - a member missing or unknown, of the wrong type, or entries or kinds out
//     of canonical order ([ErrDocument]);
//   - an environment other than environment, or an empty environment
//     ([ErrEnvironment]).
func ReadResolved(data []byte, environment string) (*Resolved, error) {
	if len(data) > MaxResolvedBytes {
		return nil, fmt.Errorf("%w: the resolved document is over %d bytes", ErrLimit, MaxResolvedBytes)
	}
	if err := scan(data); err != nil {
		return nil, err
	}
	top, ok := object(data)
	if !ok {
		return nil, fmt.Errorf("%w: the document is not an object", ErrSelector)
	}
	selector, ok := text(top["resolvedFormatVersion"])
	if !ok || !slices.Contains(resolvedFormatVersions, selector) {
		return nil, fmt.Errorf("%w: at /resolvedFormatVersion", ErrSelector)
	}
	resolved := &Resolved{ResolvedFormatVersion: selector}
	if err := closed(top, "", "resolvedFormatVersion", "registry", "environment", "kinds", "entries"); err != nil {
		return nil, err
	}
	if resolved.Environment, ok = text(top["environment"]); !ok {
		return nil, malformed("/environment", "is not a string")
	}
	registry, ok := object(top["registry"])
	if !ok {
		return nil, malformed("/registry", "is not an object")
	}
	identity, err := stringMembers(registry, "/registry", "id", "version", "digest")
	if err != nil {
		return nil, err
	}
	resolved.Registry = RegistryIdentity{ID: identity[0], Version: identity[1], Digest: identity[2]}
	kinds, ok := array(top["kinds"])
	if !ok {
		return nil, malformed("/kinds", "is not an array")
	}
	for index, raw := range kinds {
		at := "/kinds/" + strconv.Itoa(index)
		member, ok := object(raw)
		if !ok {
			return nil, malformed(at, "is not an object")
		}
		pin, err := stringMembers(member, at, "kind", "id", "version", "digest")
		if err != nil {
			return nil, err
		}
		if index > 0 && pin[0] <= resolved.Kinds[index-1].Kind {
			return nil, malformed(at, "is not after the kind before it")
		}
		resolved.Kinds = append(resolved.Kinds, KindPin{Kind: pin[0], ID: pin[1], Version: pin[2], Digest: pin[3]})
	}
	entries, ok := array(top["entries"])
	if !ok {
		return nil, malformed("/entries", "is not an array")
	}
	for index, raw := range entries {
		at := "/entries/" + strconv.Itoa(index)
		member, ok := object(raw)
		if !ok {
			return nil, malformed(at, "is not an object")
		}
		if err := closed(member, at, "kind", "id", "owner", "value"); err != nil {
			return nil, err
		}
		name, err := readStrings(member, at, "kind", "id", "owner")
		if err != nil {
			return nil, err
		}
		entry := Entry{Kind: name[0], ID: name[1], Owner: name[2], Value: bytes.Clone(member["value"])}
		if index > 0 && !entryBefore(resolved.Entries[index-1], entry) {
			return nil, malformed(at, "is not after the entry before it")
		}
		resolved.Entries = append(resolved.Entries, entry)
	}
	if environment == "" {
		return nil, fmt.Errorf("%w: no environment was named", ErrEnvironment)
	}
	if resolved.Environment != environment {
		return nil, fmt.Errorf("%w: expected %q", ErrEnvironment, environment)
	}
	return resolved, nil
}

// Entry returns the entry of kind with id, if the environment has it.
func (r *Resolved) Entry(kind, id string) (Entry, bool) {
	index, found := sort.Find(len(r.Entries), func(i int) int {
		return compareEntry(kind, id, r.Entries[i])
	})
	if !found {
		return Entry{}, false
	}
	return r.Entries[index], true
}

// EntriesOf returns the entries of kind, in id order.
func (r *Resolved) EntriesOf(kind string) []Entry {
	start := sort.Search(len(r.Entries), func(i int) bool { return r.Entries[i].Kind >= kind })
	end := start
	for end < len(r.Entries) && r.Entries[end].Kind == kind {
		end++
	}
	return slices.Clone(r.Entries[start:end])
}

// Kind returns the pin of kind, if the registry pins it.
func (r *Resolved) Kind(kind string) (KindPin, bool) {
	index, found := sort.Find(len(r.Kinds), func(i int) int { return strings.Compare(kind, r.Kinds[i].Kind) })
	if !found {
		return KindPin{}, false
	}
	return r.Kinds[index], true
}

// compareEntry orders (kind, id) against an entry, by kind then id, in
// code-point order (byte order, for UTF-8).
func compareEntry(kind, id string, entry Entry) int {
	if c := strings.Compare(kind, entry.Kind); c != 0 {
		return c
	}
	return strings.Compare(id, entry.ID)
}

func entryBefore(a, b Entry) bool {
	return compareEntry(a.Kind, a.ID, b) < 0
}

func malformed(at, what string) error {
	return fmt.Errorf("%w: at %s, the member %s", ErrDocument, at, what)
}

// closed checks that an object has exactly the members named.
func closed(members map[string]json.RawMessage, at string, names ...string) error {
	for _, name := range names {
		if _, ok := members[name]; !ok {
			return malformed(at+"/"+name, "is missing")
		}
	}
	if len(members) != len(names) {
		return fmt.Errorf("%w: at %s, the object has a member that is not one of its format's", ErrDocument, pointerOrRoot(at))
	}
	return nil
}

// stringMembers checks that an object has exactly the members named, and
// reads each as a string.
func stringMembers(members map[string]json.RawMessage, at string, names ...string) ([]string, error) {
	if err := closed(members, at, names...); err != nil {
		return nil, err
	}
	return readStrings(members, at, names...)
}

// readStrings reads the named members, each a string.
func readStrings(members map[string]json.RawMessage, at string, names ...string) ([]string, error) {
	values := make([]string, len(names))
	for i, name := range names {
		value, ok := text(members[name])
		if !ok {
			return nil, malformed(at+"/"+name, "is not a string")
		}
		values[i] = value
	}
	return values, nil
}

func pointerOrRoot(at string) string {
	if at == "" {
		return "the root"
	}
	return at
}

// object decodes a JSON object's members. encoding/json matches struct fields
// to member names without regard to case, so members are read into a map,
// whose keys are exact.
func object(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if !startsWith(raw, '{') {
		return nil, false
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil {
		return nil, false
	}
	return members, true
}

func array(raw json.RawMessage) ([]json.RawMessage, bool) {
	if !startsWith(raw, '[') {
		return nil, false
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil, false
	}
	return items, true
}

func text(raw json.RawMessage) (string, bool) {
	if !startsWith(raw, '"') {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func startsWith(raw json.RawMessage, delimiter byte) bool {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == delimiter
}

// scan reads the document's tokens once, before any member is read. It
// refuses what is not exactly one JSON value in UTF-8, a duplicate member
// name in any object, and nesting deeper than MaxResolvedDepth. A duplicate
// matters here: encoding/json keeps the last of two members with one name,
// and another reader may keep the first, so the two would read different
// selectors or environments from the same bytes.
//
// encoding/json decodes a lone surrogate escape ("\ud800") to U+FFFD, in a
// member name and in a value. Two names that differ only in their lone
// surrogates are then one name, which refuses the document as a duplicate,
// and a lone surrogate in a value is read as U+FFFD. Neither is checked
// further: RFC 8259 §8.2 leaves such a string's meaning open, and no
// conforming producer writes one.
func scan(data []byte) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("%w: the document is not UTF-8", ErrDocument)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	type frame struct {
		members map[string]bool // nil for an array
		wantKey bool
	}
	var stack []*frame
	read := false
	// done records one complete value in the innermost container.
	done := func() {
		if n := len(stack); n == 0 {
			read = true
		} else if stack[n-1].members != nil {
			stack[n-1].wantKey = true
		}
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF && read {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: the document is not JSON", ErrDocument)
		}
		if read {
			return fmt.Errorf("%w: the document holds more than one JSON value", ErrDocument)
		}
		if n := len(stack); n > 0 && stack[n-1].wantKey {
			if key, ok := token.(string); ok {
				if stack[n-1].members[key] {
					return fmt.Errorf("%w: an object has two members of one name", ErrDocument)
				}
				stack[n-1].members[key] = true
				stack[n-1].wantKey = false
				continue
			}
		}
		switch token {
		case json.Delim('{'), json.Delim('['):
			if len(stack) == MaxResolvedDepth {
				return fmt.Errorf("%w: the document nests deeper than %d", ErrLimit, MaxResolvedDepth)
			}
			next := &frame{}
			if token == json.Delim('{') {
				next.members, next.wantKey = map[string]bool{}, true
			}
			stack = append(stack, next)
		case json.Delim('}'), json.Delim(']'):
			stack = stack[:len(stack)-1]
			done()
		default:
			done()
		}
	}
}
