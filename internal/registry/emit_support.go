package registry

import (
	"fmt"
	"slices"
	"strings"
)

// What the emitter host (spec RFC 0036, ADR-0098) reuses from the reader: the
// admission of a JSON document, the schema subset for an emitter's options,
// the identifier grammars, a §7.3 location, and a valid registry's resolved
// documents with the kinds an emitter does not read taken out. Nothing here
// changes what validate, resolve or facts write.

// AdmitValue runs step 1's admission on a document that has no selector of
// its own: the size bound, strict I-JSON within the depth bound, and the
// number rules. It returns the decoded value, or the REG-DOC-* code and the
// pointer that refuse it.
func AdmitValue(data []byte, maxBytes int) (value any, code, pointer string) {
	return AdmitValueNodes(data, maxBytes, maxBytes)
}

// AdmitValueNodes is AdmitValue with a bound on the document's JSON values
// below its size bound: a document of more than maxNodes values is refused
// (REG-DOC-SIZE, at the value past the bound) while it is decoded, before the
// rest is held in memory.
func AdmitValueNodes(data []byte, maxBytes, maxNodes int) (value any, code, pointer string) {
	collected := &collector{}
	value, admitted := admitValue(data, maxBytes, maxNodes, Location{}, collected)
	if !admitted {
		diagnostics := collected.sorted()
		if len(diagnostics) == 0 {
			return nil, "REG-DOC-JSON", ""
		}
		return nil, diagnostics[0].Code, diagnostics[0].Location.Pointer
	}
	return value, "", ""
}

// Schema is a compiled schema in the companion's subset (§3.2) that is not a
// kind's: an emitter manifest's options schema.
type Schema struct {
	node *schemaNode
}

// SchemaFinding is one finding of compiling or applying a Schema: a code and
// a JSON Pointer, into the schema when compiling and into the value when
// validating. It carries no value.
type SchemaFinding struct {
	Code    string
	Pointer string
}

// CompileSchema compiles a schema in the subset. It returns nil and the
// REG-KIND-SCHEMA-* and REG-KIND-PATTERN findings, pointed into the schema,
// when the schema is outside the subset.
func CompileSchema(raw any) (*Schema, []SchemaFinding) {
	collected := &collector{}
	node := compileSchema(raw, nil, Location{}, collected, &patternBudget{})
	if node == nil {
		var findings []SchemaFinding
		for _, diagnostic := range collected.sorted() {
			findings = append(findings, SchemaFinding{Code: diagnostic.Code, Pointer: diagnostic.Location.Pointer})
		}
		if len(findings) == 0 {
			findings = []SchemaFinding{{Code: "REG-KIND-SCHEMA-FORM"}}
		}
		return nil, findings
	}
	return &Schema{node: node}, nil
}

// Validate applies the schema to a value admitted by AdmitValue. It returns
// the error findings (REG-VALUE-*), in a fixed order, and none when the value
// holds; a deprecated position is a warning and is not returned. The check
// has the registry's validation work bound: past it, the one finding is
// REG-BOUND-VALIDATION-WORK at "".
func (s *Schema) Validate(value any) []SchemaFinding {
	var findings []valueFinding
	check := &validation{work: maxValidationWork}
	s.node.validate(check, value, nil, &findings)
	if check.exceeded {
		return []SchemaFinding{{Code: "REG-BOUND-VALIDATION-WORK"}}
	}
	var out []SchemaFinding
	for _, finding := range findings {
		if Codes[finding.code].severity != SeverityError {
			continue
		}
		out = append(out, SchemaFinding{Code: finding.code, Pointer: pointerOf(finding.path)})
	}
	slices.SortFunc(out, func(a, b SchemaFinding) int {
		return strings.Compare(a.Pointer+"\x00"+a.Code, b.Pointer+"\x00"+b.Code)
	})
	return slices.Compact(out)
}

// ValidKindName, ValidOwner, ValidVersion and IsAbsoluteURI are the grammars
// of a kind's kind, owner, version (strict SemVer 2.0.0, no build metadata)
// and id (§3.1).
func ValidKindName(text string) bool { return kindNamePattern.MatchString(text) }

// ValidOwner is the grammar of an owner (§3.1).
func ValidOwner(text string) bool { return ownerPattern.MatchString(text) }

// ValidVersion is strict SemVer 2.0.0 with no build metadata (§3.1).
func ValidVersion(text string) bool { return semverPattern.MatchString(text) }

// IsAbsoluteURI is the grammar of a kind's or a registry's id (§3.1).
func IsAbsoluteURI(text string) bool { return isAbsoluteURI(text) }

// locationDocuments are the documents a §7.3 location can name.
var locationDocuments = map[string]bool{
	DocumentRegistry: true, DocumentKind: true, DocumentValue: true, DocumentYAML: true,
	DocumentFacts: true, DocumentContract: true, DocumentPack: true,
}

var locationMembers = map[string]bool{
	"document": true, "kindDocument": true, "kind": true, "entry": true, "environment": true, "pointer": true, "line": true, "column": true,
}

// maxLocationPointerBytes bounds a location's pointer read from outside the
// reader, as RFC 0035 bounds an entry reference's pointer.
const maxLocationPointerBytes = 512

// ReadLocation reads a location of §7.3's form from a document this reader did
// not write (an emitter's diagnostic, RFC 0036 §3). Every member is checked
// against its form, and a member the table does not define refuses it. It
// returns false for anything else. A location of the right form can still
// name what is not there; the caller resolves it against what it gave (the
// emitter host resolves it against the request, with PointerResolves).
func ReadLocation(raw any) (Location, bool) {
	object, ok := raw.(map[string]any)
	if !ok {
		return Location{}, false
	}
	for name := range object {
		if !locationMembers[name] {
			return Location{}, false
		}
	}
	var location Location
	text := func(name string, valid func(string) bool, required bool) (string, bool) {
		value, present := object[name]
		if !present {
			return "", !required
		}
		s, isString := value.(string)
		return s, isString && valid(s)
	}
	var good [6]bool
	location.Document, good[0] = text("document", func(s string) bool { return locationDocuments[s] }, true)
	location.KindDocument, good[1] = text("kindDocument", digestPattern.MatchString, false)
	location.Kind, good[2] = text("kind", kindNamePattern.MatchString, false)
	location.Environment, good[3] = text("environment", environmentRegex.MatchString, false)
	location.Pointer, good[4] = text("pointer", func(s string) bool {
		if len(s) > maxLocationPointerBytes {
			return false
		}
		_, err := parsePointer(s)
		return err == nil
	}, true)
	good[5] = true
	if value, present := object["entry"]; present {
		entry, isObject := value.(map[string]any)
		kind, kindOK := entry["kind"].(string)
		id, idOK := entry["id"].(string)
		good[5] = isObject && len(entry) == 2 && kindOK && idOK && kindNamePattern.MatchString(kind) && validEntryID(id)
		location.Entry = &EntryName{Kind: kind, ID: id}
	}
	for _, g := range good {
		if !g {
			return Location{}, false
		}
	}
	_, hasLine := object["line"]
	_, hasColumn := object["column"]
	if hasLine || hasColumn {
		line, lineOK := nonNegativeInteger(object["line"])
		column, columnOK := nonNegativeInteger(object["column"])
		if location.Document != DocumentYAML || location.Pointer != "" || !lineOK || !columnOK || line < 1 || column < 1 {
			return Location{}, false
		}
		location.Line, location.Column = line, column
	}
	return location, true
}

// PointerResolves reports whether a JSON Pointer names an existing position
// in a decoded value: its tokens are read as a location's are, and each must
// be a member of the object or an index of the array it meets. Its cost is
// the pointer's length.
func PointerResolves(value any, pointer string) bool {
	tokens, err := parsePointer(pointer)
	if err != nil {
		return false
	}
	_, ok := lookup(value, tokens)
	return ok
}

// Pin is one pinned kind of a registry.
type Pin struct {
	Kind, ID, Version, Digest string
}

// Pins returns a valid registry's pinned kinds in the registry's order, or
// nil for a registry that is not valid.
func (r *Result) Pins() []Pin {
	if !r.Report.Valid || r.registry == nil {
		return nil
	}
	out := make([]Pin, 0, len(r.registry.pins))
	for _, p := range r.registry.pins {
		out = append(out, Pin{Kind: p.kind, ID: p.id, Version: p.version, Digest: p.digest})
	}
	return out
}

// ResolvedFor returns one environment's resolved document (§8) as a value,
// holding only the pins and the entries of the kinds keep admits; keep nil
// admits every kind. Its members are those resolve writes, and its registry
// digest is the whole registry's. It requires a valid registry.
func (r *Result) ResolvedFor(environment string, keep func(kind string) bool) (map[string]any, error) {
	if !r.Report.Valid || r.registry == nil {
		return nil, ErrInvalid
	}
	if !slices.Contains(r.registry.environments, environment) {
		return nil, fmt.Errorf("registry: no environment %q", environment)
	}
	return r.resolvedDocument(environment, keep), nil
}
