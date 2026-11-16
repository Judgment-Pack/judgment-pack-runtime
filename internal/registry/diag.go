// Package registry implements the registry companion proposed by the
// specification's RFC 0034 (the profile) and RFC 0035 (references), both
// Drafts: kind declarations an adopter writes, a registry document of entries
// of those kinds with per-environment overlays, validation in the RFC's fixed
// order with a value-free report, and resolution to one document per
// environment.
//
// Nothing here fetches anything. A secret reference is checked for form, pin
// and scope and left as written; an entry reference is checked against the
// registry itself.
package registry

import (
	"cmp"
	"slices"
)

// Format selectors, each exact (RFC 0034 §1).
const (
	KindFormatVersion     = "1.0.0"
	RegistryFormatVersion = "1.0.0"
	ResolvedFormatVersion = "1.0.0"
	ReportFormatVersion   = "1.0.0"
)

// Severity of a diagnostic.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Documents a location's pointer can point into.
const (
	DocumentRegistry = "registry"
	DocumentKind     = "kind"
	DocumentValue    = "value"
	// DocumentYAML is a YAML file given to import (RFC 0034 §7). A location
	// in it is a line and a column, never a pointer.
	DocumentYAML = "yaml"
)

// EntryName names one entry.
type EntryName struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Location says where a diagnostic applies. Pointer is a JSON Pointer into
// Document: the registry document, a kind declaration (named by KindDocument,
// the sha256 of its bytes as supplied, and by Kind once its name is known), or
// an entry's resolved value in Environment. Line and Column, counted from 1
// in Unicode code points, locate a finding in a YAML file given to import;
// they are absent everywhere else.
type Location struct {
	Document     string     `json:"document"`
	KindDocument string     `json:"kindDocument,omitempty"`
	Kind         string     `json:"kind,omitempty"`
	Entry        *EntryName `json:"entry,omitempty"`
	Environment  string     `json:"environment,omitempty"`
	Pointer      string     `json:"pointer"`
	Line         int        `json:"line,omitempty"`
	Column       int        `json:"column,omitempty"`
}

// Diagnostic is one finding. Message is fixed per code and carries no value
// from any document.
type Diagnostic struct {
	Code     string   `json:"code"`
	Severity string   `json:"severity"`
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

// Report is the validation report (reportFormatVersion 1.0.0).
type Report struct {
	ReportFormatVersion string       `json:"reportFormatVersion"`
	Valid               bool         `json:"valid"`
	Registry            *Digested    `json:"registry,omitempty"`
	Diagnostics         []Diagnostic `json:"diagnostics"`
}

// Digested names the validated registry in a report by its digest only:
// its id and version are document values, which a report never carries.
type Digested struct {
	Digest string `json:"digest"`
}

// Identity names a document by id, version and digest.
type Identity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type codeInfo struct {
	severity string
	message  string
}

// Codes is the closed list of diagnostic codes, with each one's severity and
// fixed message. A code is never removed and never changes meaning within
// format 1.x.
var Codes = map[string]codeInfo{
	// Import (RFC 0034 §7): the YAML authoring subset, before step 1. The
	// §7 text names no codes; these are this reader's (ADR-0094). A YAML
	// file also gets REG-DOC-SIZE, -DEPTH, -DUPLICATE, -STRING, -NUMBER and
	// -SELECTOR, with the meaning they have for JSON.
	"REG-YAML-ENCODING":  {SeverityError, "The file is not UTF-8, has a byte-order mark, or holds a character YAML does not allow."},
	"REG-YAML-DOCUMENTS": {SeverityError, "The file does not hold exactly one YAML document."},
	"REG-YAML-DIRECTIVE": {SeverityError, "The file has a YAML directive."},
	"REG-YAML-TAG":       {SeverityError, "A node has a YAML tag."},
	"REG-YAML-ANCHOR":    {SeverityError, "A node has an anchor, or is an alias."},
	"REG-YAML-MERGE":     {SeverityError, "A mapping has a merge key."},
	"REG-YAML-SCALAR":    {SeverityError, "A plain scalar would be read as another value by YAML 1.1, the YAML 1.2 core schema or a common YAML library; quote it to make it a string."},
	"REG-YAML-EMPTY":     {SeverityError, "A node is empty; write null or an empty quoted string."},
	"REG-YAML-KEY":       {SeverityError, "A key is not a string on one line: an explicit or complex key, a key spanning lines, or a key that reads as a number, a boolean or null."},
	"REG-YAML-SYNTAX":    {SeverityError, "The file is not well-formed YAML within the subset."},
	"REG-YAML-TRUNCATED": {SeverityError, "Reading stopped here after 1000 refusals; the report lists those, and the file may hold more."},

	// Step 1: document admission.
	"REG-DOC-SIZE":      {SeverityError, "The document exceeds its size bound."},
	"REG-DOC-DEPTH":     {SeverityError, "The document nests deeper than 32."},
	"REG-DOC-JSON":      {SeverityError, "The document is not one well-formed UTF-8 JSON value."},
	"REG-DOC-DUPLICATE": {SeverityError, "An object has a duplicate member name."},
	"REG-DOC-STRING":    {SeverityError, "A string holds a lone surrogate."},
	"REG-DOC-NUMBER":    {SeverityError, "A number is outside the admitted range: an integer beyond 2^53-1, a decimal whose value does not round-trip through a double, or negative zero."},
	"REG-DOC-SELECTOR":  {SeverityError, "The document does not carry the exact format selector this reader supports."},

	// Step 2: kind declarations.
	"REG-KIND-UNKNOWN-MEMBER":      {SeverityError, "The kind declaration has a member the format does not define."},
	"REG-KIND-MISSING-MEMBER":      {SeverityError, "The kind declaration lacks a required member."},
	"REG-KIND-FORM":                {SeverityError, "A member of the kind declaration has the wrong form."},
	"REG-KIND-SCHEMA-KEYWORD":      {SeverityError, "The schema uses a keyword outside the companion's subset."},
	"REG-KIND-SCHEMA-FORM":         {SeverityError, "A schema keyword has the wrong form."},
	"REG-KIND-SCHEMA-LIMIT":        {SeverityError, "The schema exceeds a size limit: subschemas, enum values or composition branches."},
	"REG-KIND-SCHEMA-REF":          {SeverityError, "A $ref is not of the form #/$defs/<name>, names no definition, or is part of a cycle."},
	"REG-KIND-PATTERN":             {SeverityError, "A pattern is outside the companion's regular-expression subset."},
	"REG-KIND-POINTER-FORM":        {SeverityError, "A pointer pattern is not a well-formed pattern."},
	"REG-KIND-POINTER-UNREACHABLE": {SeverityError, "A pointer pattern can match no position the schema allows."},
	"REG-KIND-POINTER-CONFLICT":    {SeverityError, "A position is matched by both a secrets pattern and a references pattern."},
	"REG-KIND-DEPRECATED":          {SeverityWarning, "The registry uses a deprecated kind."},

	// Step 3: registry shape.
	"REG-REGISTRY-UNKNOWN-MEMBER": {SeverityError, "The registry has a member the format does not define."},
	"REG-REGISTRY-MISSING-MEMBER": {SeverityError, "The registry lacks a required member."},
	"REG-REGISTRY-FORM":           {SeverityError, "A member of the registry has the wrong form."},
	"REG-REGISTRY-DUPLICATE":      {SeverityError, "A list that must hold distinct items repeats one."},
	"REG-REGISTRY-ORDER":          {SeverityError, "The kinds or entries are not in canonical order."},
	"REF-SECRET-SCOPE-FORM":       {SeverityError, "The secret scopes are malformed, name an undeclared environment, or omit one."},
	"REF-SECRET-SCOPE-SHARED":     {SeverityError, "A project is listed in the secret scopes of two environments."},

	// Step 4: kind pins.
	"REG-PIN-MISSING":  {SeverityError, "No supplied kind declaration has the pinned digest."},
	"REG-PIN-MISMATCH": {SeverityError, "The kind declaration with the pinned digest has another kind, id or version."},
	"REG-PIN-UNPINNED": {SeverityWarning, "A supplied kind declaration is not pinned by the registry."},

	// Step 5: entries.
	"REG-ENTRY-KIND":        {SeverityError, "The entry's kind is not pinned by the registry."},
	"REG-ENTRY-ID":          {SeverityError, "The entry's id does not match its kind's entry-id pattern."},
	"REG-ENTRY-DUPLICATE":   {SeverityError, "Two entries have the same kind and id."},
	"REG-ENTRY-OWNER":       {SeverityError, "The entry's owner is not in the registry's owners."},
	"REG-ENTRY-ENVIRONMENT": {SeverityError, "The entry names an environment the registry does not declare."},

	// Step 6: overlays.
	"REG-OVERLAY-ENVIRONMENT":     {SeverityError, "The overlay's environment is not declared."},
	"REG-OVERLAY-DUPLICATE":       {SeverityError, "Two overlays are for the same environment."},
	"REG-OVERLAY-PATCH-DUPLICATE": {SeverityError, "Two patches of one overlay are for the same entry."},
	"REG-OVERLAY-ENTRY-MISSING":   {SeverityError, "The patch names no entry of the registry."},
	"REG-OVERLAY-ENTRY-ABSENT":    {SeverityError, "The patch names an entry that does not apply in the overlay's environment."},
	"REG-OVERLAY-POINTER":         {SeverityError, "A patch pointer is not a well-formed JSON Pointer."},
	"REG-OVERLAY-NOT-OVERLAYABLE": {SeverityError, "A patch pointer matches none of the kind's overlayable patterns."},
	"REG-OVERLAY-TARGET":          {SeverityError, "A patch pointer addresses nothing in the base value and no member the schema declares."},
	"REG-OVERLAY-NULL":            {SeverityError, "A patch sets null, which would be a deletion."},
	"REG-OVERLAY-PREFIX":          {SeverityError, "One patch pointer is a prefix of another in the same patch."},

	// Step 7: per environment, on resolved values.
	"REG-VALUE-TYPE":                  {SeverityError, "The value is not of a type the schema allows."},
	"REG-VALUE-ENUM":                  {SeverityError, "The value is not one of the schema's enumerated values."},
	"REG-VALUE-CONST":                 {SeverityError, "The value is not the schema's constant."},
	"REG-VALUE-REQUIRED":              {SeverityError, "A required member is absent."},
	"REG-VALUE-ADDITIONAL":            {SeverityError, "A member is not allowed by the schema."},
	"REG-VALUE-PROPERTY-COUNT":        {SeverityError, "The object has too few or too many members."},
	"REG-VALUE-PROPERTY-NAME":         {SeverityError, "A member name does not satisfy the schema's property names."},
	"REG-VALUE-ITEM-COUNT":            {SeverityError, "The array has too few or too many items."},
	"REG-VALUE-UNIQUE":                {SeverityError, "The array repeats an item where items must be unique."},
	"REG-VALUE-LENGTH":                {SeverityError, "The string is too short or too long."},
	"REG-VALUE-PATTERN":               {SeverityError, "The string does not match the schema's pattern."},
	"REG-VALUE-RANGE":                 {SeverityError, "The number is outside the schema's range."},
	"REG-VALUE-ANY-OF":                {SeverityError, "The value satisfies none of the schema's alternatives."},
	"REG-VALUE-NOT":                   {SeverityError, "The value satisfies a schema it must not."},
	"REG-VALUE-DEPRECATED":            {SeverityWarning, "The value sets a member the schema marks deprecated."},
	"REF-ENTRY-FORM":                  {SeverityError, "An entry reference is not well formed."},
	"REF-ENTRY-KIND":                  {SeverityError, "An entry reference names a kind its position does not allow."},
	"REF-ENTRY-MISSING":               {SeverityError, "An entry reference names no entry of the registry."},
	"REF-ENTRY-ABSENT-IN-ENVIRONMENT": {SeverityError, "An entry reference names an entry that does not apply in this environment."},
	"REF-ENTRY-POINTER":               {SeverityError, "An entry reference's pointer addresses nothing in the target's resolved value."},
	"REF-SECRET-FORM":                 {SeverityError, "A secret position does not hold a well-formed secret reference."},
	"REF-SECRET-STORE":                {SeverityError, "A secret reference names a store this format does not define."},
	"REF-SECRET-PIN":                  {SeverityError, "A secret reference is not pinned to a numbered version of a named project."},
	"REF-SECRET-SCOPE":                {SeverityError, "A secret reference names a project outside this environment's scope."},
	"REF-SECRET-UNSCOPED":             {SeverityWarning, "The registry declares no secret scopes, so this secret reference's project is unchecked."},

	// Step 8: bounds.
	"REG-BOUND-ENTRIES":     {SeverityError, "The kind has more entries than its maxEntries bound."},
	"REG-BOUND-ENTRY-BYTES": {SeverityError, "The entry's resolved value exceeds its kind's maxEntryBytes bound."},
}

type collector struct {
	diagnostics []Diagnostic
}

func (c *collector) add(code string, location Location) {
	info, ok := Codes[code]
	if !ok {
		panic("registry: unlisted diagnostic code " + code)
	}
	c.diagnostics = append(c.diagnostics, Diagnostic{Code: code, Severity: info.severity, Location: location, Message: info.message})
}

func (c *collector) errors() int {
	count := 0
	for _, diagnostic := range c.diagnostics {
		if diagnostic.Severity == SeverityError {
			count++
		}
	}
	return count
}

// sorted returns the diagnostics sorted by location, then code, with exact
// duplicates removed.
func (c *collector) sorted() []Diagnostic {
	out := slices.Clone(c.diagnostics)
	slices.SortFunc(out, compareDiagnostics)
	out = slices.CompactFunc(out, func(a, b Diagnostic) bool { return compareDiagnostics(a, b) == 0 })
	if out == nil {
		out = []Diagnostic{}
	}
	return out
}

func compareDiagnostics(a, b Diagnostic) int {
	entryKind := func(d Diagnostic) string {
		if d.Location.Entry == nil {
			return ""
		}
		return d.Location.Entry.Kind
	}
	entryID := func(d Diagnostic) string {
		if d.Location.Entry == nil {
			return ""
		}
		return d.Location.Entry.ID
	}
	return cmp.Or(
		cmp.Compare(a.Location.Document, b.Location.Document),
		cmp.Compare(a.Location.KindDocument, b.Location.KindDocument),
		cmp.Compare(a.Location.Kind, b.Location.Kind),
		cmp.Compare(entryKind(a), entryKind(b)),
		cmp.Compare(entryID(a), entryID(b)),
		cmp.Compare(a.Location.Environment, b.Location.Environment),
		cmp.Compare(a.Location.Pointer, b.Location.Pointer),
		cmp.Compare(a.Location.Line, b.Location.Line),
		cmp.Compare(a.Location.Column, b.Location.Column),
		cmp.Compare(a.Code, b.Code),
		cmp.Compare(a.Severity, b.Severity),
	)
}
